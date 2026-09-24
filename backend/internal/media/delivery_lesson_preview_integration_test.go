//go:build integration

package media

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// allowLessonPreview marks the fixture's live Lesson publicly previewable. It
// writes the flag directly, because the authoring path that sets it is proven in
// internal/catalog; here the flag is the precondition, not the subject.
func allowLessonPreview(t *testing.T, f *deliveryFixture, allow bool) {
	t.Helper()
	tag, err := f.pool.Exec(f.ctx,
		"UPDATE course_lessons SET allow_public_preview = $1 WHERE lesson_identity_id = $2::uuid",
		allow, f.lesson)
	if err != nil {
		t.Fatalf("setting allow_public_preview: %v", err)
	}
	if tag.RowsAffected() == 0 {
		t.Fatal("no Lesson row was updated")
	}
}

// seedCandidateLessonPreview creates a separate CANDIDATE revision carrying the
// same Lesson identity, with allow_public_preview set there and deliberately NOT
// on the live revision. Candidate intent must never be publicly reachable.
func (f *deliveryFixture) seedCandidateLessonPreview(t *testing.T) {
	t.Helper()
	candidateRevision := uuid.NewString()
	candidateSection := uuid.NewString()
	candidateLesson := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO course_revisions (id, course_id, based_on_revision_id, state, revision_number, title_ar, title_en)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'DRAFT', 2, 'دورة', 'Course')
	`, candidateRevision, f.courseID, f.revision); err != nil {
		t.Fatalf("seeding candidate revision: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'قسم', 'Section', 0)
	`, candidateSection, candidateRevision, f.courseID, f.section); err != nil {
		t.Fatalf("seeding candidate section: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id,
		                            title_ar, title_en, position, video_asset_version_id, allow_public_preview)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'درس', 'Lesson', 0, $6::uuid, true)
	`, candidateLesson, candidateSection, f.courseID, f.section, f.lesson, f.video); err != nil {
		t.Fatalf("seeding candidate lesson: %v", err)
	}
	// The live revision stays unflagged.
	allowLessonPreview(t, f, false)
	if _, err := f.pool.Exec(f.ctx,
		"UPDATE course_lessons SET allow_public_preview=true WHERE id=$1::uuid", candidateLesson); err != nil {
		t.Fatalf("restoring candidate intent: %v", err)
	}
}

// attachPlayableLessonVideo points the live Lesson at a PLAYABLE video: scanned,
// partially encoded, deliverable to an entitled Student, and never READY.
func (f *deliveryFixture) attachPlayableLessonVideo(t *testing.T) {
	t.Helper()
	assetID, versionID, scanID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO media_assets (id, kind, owner_account_id, course_id, lesson_id, visibility)
		VALUES ($1::uuid, 'VIDEO', $2::uuid, $3::uuid, $4::uuid, 'PROTECTED')
	`, assetID, f.instructorID, f.courseID, f.lesson); err != nil {
		t.Fatalf("seeding playable asset: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO media_asset_versions
		  (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes)
		VALUES ($1::uuid, $2::uuid, 'VIDEO', 'QUARANTINED', $3, 'v1', 'video/mp4', 12)
	`, versionID, assetID, "quarantine/"+f.courseID+"/"+versionID+"/source"); err != nil {
		t.Fatalf("seeding playable version: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO scan_attempts (id, asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity)
		VALUES ($1::uuid, $2::uuid, 1, $3, 'v1', 'PASSED', 'fixture')
	`, scanID, versionID, "scan:"+versionID); err != nil {
		t.Fatalf("seeding playable scan: %v", err)
	}
	for _, statement := range []string{
		"UPDATE media_asset_versions SET state='SCANNING' WHERE id=$1::uuid",
		"UPDATE media_asset_versions SET successful_scan_attempt_id='" + scanID + "'::uuid, state='SCAN_PASSED' WHERE id=$1::uuid",
		"UPDATE media_asset_versions SET state='PROCESSING' WHERE id=$1::uuid",
	} {
		if _, err := f.pool.Exec(f.ctx, statement, versionID); err != nil {
			t.Fatalf("advancing the playable version (%s): %v", statement, err)
		}
	}
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO video_renditions (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms)
		VALUES ($1::uuid, '720p', $2, 1280, 720, 2800, 60000)
	`, versionID, "video/partial/"+versionID+"/720p/playlist.m3u8"); err != nil {
		t.Fatalf("seeding playable rendition: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx,
		"UPDATE media_asset_versions SET state='PLAYABLE' WHERE id=$1::uuid", versionID); err != nil {
		t.Fatalf("promoting to PLAYABLE: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx,
		"UPDATE course_lessons SET video_asset_version_id=$1::uuid WHERE lesson_identity_id=$2::uuid",
		versionID, f.lesson); err != nil {
		t.Fatalf("attaching the playable video: %v", err)
	}
}

func lessonPreviewRequest(f *deliveryFixture) LessonPreviewRequest {
	return LessonPreviewRequest{CourseID: f.courseID, LessonID: f.lesson}
}

// TestLessonPreviewServesTheSameCanonicalRenditionsAsPaidPlayback is the central
// claim of the whole feature: an anonymous visitor and a paying Student are served
// from the same canonical HLS renditions and the same storage objects, and the
// original uploaded MP4 is never presigned for the anonymous path.
func TestLessonPreviewServesTheSameCanonicalRenditionsAsPaidPlayback(t *testing.T) {
	f := newDeliveryFixture(t)
	allowLessonPreview(t, f, true)

	issued, err := f.delivery.IssueLessonPreview(f.ctx, lessonPreviewRequest(f))
	if err != nil {
		t.Fatalf("IssueLessonPreview: %v", err)
	}
	if issued.PreviewSession == "" || issued.AssetVersionID != f.video {
		t.Fatalf("issued = %+v, want a session for the Lesson video %s", issued, f.video)
	}
	if !strings.HasPrefix(issued.ManifestURL, "/api/v1/public/lesson-previews/") {
		t.Fatalf("manifest URL = %q, want the public preview manifest route", issued.ManifestURL)
	}

	master, err := f.delivery.IssueLessonPreviewManifest(f.ctx, issued.PreviewSession)
	if err != nil {
		t.Fatalf("IssueLessonPreviewManifest: %v", err)
	}
	previewMaster := string(master.Contents)
	if !strings.Contains(previewMaster, "#EXTM3U") || !strings.Contains(previewMaster, "720p") {
		t.Fatalf("preview master = %q, want a dynamic master naming the canonical rungs", previewMaster)
	}
	// The master is generated, not stored: it names the application's own rendition
	// route rather than any storage object.
	if strings.Contains(previewMaster, "quarantine/") || strings.Contains(previewMaster, "https://storage") {
		t.Fatalf("preview master leaked a storage identity: %q", previewMaster)
	}

	// The rendition manifest is signed from the same canonical rendition rows.
	previewRendition, err := f.delivery.IssueLessonPreviewRenditionManifest(f.ctx, issued.PreviewSession, "720p")
	if err != nil {
		t.Fatalf("IssueLessonPreviewRenditionManifest: %v", err)
	}
	previewKeys := f.store.requestedKeys()

	// The same Lesson, through the entitled Student path.
	studentAuth, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, DeviceID: testDeviceID(f.student),
		LessonID: f.lesson, AssetVersionID: f.video,
	})
	if err != nil {
		t.Fatalf("IssuePlayback: %v", err)
	}
	studentMaster, err := f.delivery.IssuePlaybackManifest(f.ctx, f.playbackSessionRequest(studentAuth.PlaybackSession))
	if err != nil {
		t.Fatalf("IssuePlaybackManifest: %v", err)
	}
	if _, err := f.delivery.IssuePlaybackRenditionManifest(f.ctx,
		f.playbackRenditionRequest(studentAuth.PlaybackSession, "720p")); err != nil {
		t.Fatalf("IssuePlaybackRenditionManifest: %v", err)
	}
	allKeys := f.store.requestedKeys()
	studentKeys := allKeys[len(previewKeys):]

	// Same object keys. Not equivalent keys — the same ones, because there is one
	// transcode and one set of canonical objects.
	if len(previewKeys) == 0 || len(studentKeys) == 0 {
		t.Fatalf("signed keys: preview=%v student=%v", previewKeys, studentKeys)
	}
	for _, key := range previewKeys {
		if !containsString(studentKeys, key) {
			t.Fatalf("preview signed %q, which paid playback does not serve; the two must share canonical storage", key)
		}
		if strings.Contains(key, "quarantine/") {
			t.Fatalf("preview presigned the original uploaded object %q", key)
		}
	}
	// Both manifests describe the same ladder.
	if !strings.Contains(string(studentMaster.Contents), "720p") {
		t.Fatalf("student master = %q", studentMaster.Contents)
	}
	if len(previewRendition.Contents) == 0 {
		t.Fatal("preview rendition manifest was empty")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// TestLessonPreviewCreatesNoMediaOfItsOwn is the no-duplication proof. Marking a
// Lesson previewable and serving it is authorization metadata and delivery; it
// must create no media row, no attempt, no rendition, and no transcode intent.
func TestLessonPreviewCreatesNoMediaOfItsOwn(t *testing.T) {
	f := newDeliveryFixture(t)

	var beforeAssets, beforeVersions, beforeAttempts, beforeRenditions, beforeEvents int
	countMedia := func() (int, int, int, int, int) {
		var assets, versions, attempts, renditions, events int
		if err := f.pool.QueryRow(f.ctx, `
			SELECT (SELECT count(*) FROM media_assets WHERE course_id=$1::uuid),
			       (SELECT count(*) FROM media_asset_versions mav
			         JOIN media_assets ma ON ma.id=mav.logical_asset_id WHERE ma.course_id=$1::uuid),
			       (SELECT count(*) FROM processing_attempts pa
			         JOIN media_asset_versions mav ON mav.id=pa.asset_version_id
			         JOIN media_assets ma ON ma.id=mav.logical_asset_id WHERE ma.course_id=$1::uuid),
			       (SELECT count(*) FROM video_renditions vr
			         JOIN media_asset_versions mav ON mav.id=vr.asset_version_id
			         JOIN media_assets ma ON ma.id=mav.logical_asset_id WHERE ma.course_id=$1::uuid),
			       (SELECT count(*) FROM outbox_events
			         WHERE event_type IN ('media.transcode_requested','media.enhancement_requested','media.scan_requested'))
		`, f.courseID).Scan(&assets, &versions, &attempts, &renditions, &events); err != nil {
			t.Fatalf("counting media: %v", err)
		}
		return assets, versions, attempts, renditions, events
	}
	beforeAssets, beforeVersions, beforeAttempts, beforeRenditions, beforeEvents = countMedia()

	allowLessonPreview(t, f, true)
	issued, err := f.delivery.IssueLessonPreview(f.ctx, lessonPreviewRequest(f))
	if err != nil {
		t.Fatalf("IssueLessonPreview: %v", err)
	}
	if _, err := f.delivery.IssueLessonPreviewManifest(f.ctx, issued.PreviewSession); err != nil {
		t.Fatalf("IssueLessonPreviewManifest: %v", err)
	}

	afterAssets, afterVersions, afterAttempts, afterRenditions, afterEvents := countMedia()
	if afterAssets != beforeAssets || afterVersions != beforeVersions ||
		afterAttempts != beforeAttempts || afterRenditions != beforeRenditions ||
		afterEvents != beforeEvents {
		t.Fatalf("Lesson preview created media: assets %d->%d versions %d->%d attempts %d->%d renditions %d->%d events %d->%d",
			beforeAssets, afterAssets, beforeVersions, afterVersions, beforeAttempts, afterAttempts,
			beforeRenditions, afterRenditions, beforeEvents, afterEvents)
	}
	// And it serves the Lesson's own VIDEO asset, not a PREVIEW-kind asset.
	var kind string
	if err := f.pool.QueryRow(f.ctx,
		"SELECT kind::text FROM media_asset_versions WHERE id=$1::uuid", issued.AssetVersionID).Scan(&kind); err != nil {
		t.Fatalf("reading the served asset kind: %v", err)
	}
	if kind != "VIDEO" {
		t.Fatalf("Lesson preview served a %s asset, want the Lesson's own VIDEO", kind)
	}
}

// TestLessonPreviewAuthorizationChain walks every link the resolver re-proves.
// Each case breaks exactly one link and must be denied identically, because the
// response is inventory-safe and reveals nothing about which link failed.
func TestLessonPreviewAuthorizationChain(t *testing.T) {
	cases := []struct {
		name    string
		stage   func(t *testing.T, f *deliveryFixture)
		allowed bool
	}{
		{
			name:    "flagged Lesson of the live revision",
			stage:   func(t *testing.T, f *deliveryFixture) { allowLessonPreview(t, f, true) },
			allowed: true,
		},
		{
			name:  "Lesson not flagged",
			stage: func(t *testing.T, f *deliveryFixture) { allowLessonPreview(t, f, false) },
		},
		{
			name: "flag set on a candidate revision only",
			stage: func(t *testing.T, f *deliveryFixture) {
				// A separate candidate revision carrying the same Lesson identity, with
				// the flag set there and NOT on the live revision. Candidate intent must
				// never be publicly reachable.
				f.seedCandidateLessonPreview(t)
			},
		},
		{
			name: "Course access suspended",
			stage: func(t *testing.T, f *deliveryFixture) {
				allowLessonPreview(t, f, true)
				if _, err := f.pool.Exec(f.ctx,
					`UPDATE courses SET access_suspended_at=now(),
					   access_suspension_reason='lesson preview fixture' WHERE id=$1::uuid`,
					f.courseID); err != nil {
					t.Fatalf("suspending: %v", err)
				}
			},
		},
		{
			name: "Course retired",
			stage: func(t *testing.T, f *deliveryFixture) {
				allowLessonPreview(t, f, true)
				if _, err := f.pool.Exec(f.ctx,
					"UPDATE courses SET retired_at=now() WHERE id=$1::uuid", f.courseID); err != nil {
					t.Fatalf("retiring: %v", err)
				}
			},
		},
		{
			name: "Course delisted from publication",
			stage: func(t *testing.T, f *deliveryFixture) {
				allowLessonPreview(t, f, true)
				if _, err := f.pool.Exec(f.ctx,
					"UPDATE courses SET lifecycle='DRAFT' WHERE id=$1::uuid", f.courseID); err != nil {
					t.Fatalf("delisting: %v", err)
				}
			},
		},
		{
			name: "logical media asset retired",
			stage: func(t *testing.T, f *deliveryFixture) {
				allowLessonPreview(t, f, true)
				if _, err := f.pool.Exec(f.ctx, `
					UPDATE media_assets SET retired_at=now()
					WHERE id=(SELECT logical_asset_id FROM media_asset_versions WHERE id=$1::uuid)
				`, f.video); err != nil {
					t.Fatalf("retiring the asset: %v", err)
				}
			},
		},
		{
			name: "Lesson carries no video",
			stage: func(t *testing.T, f *deliveryFixture) {
				allowLessonPreview(t, f, true)
				if _, err := f.pool.Exec(f.ctx,
					"UPDATE course_lessons SET video_asset_version_id=NULL WHERE lesson_identity_id=$1::uuid",
					f.lesson); err != nil {
					t.Fatalf("clearing the video: %v", err)
				}
			},
		},
		{
			name: "video is PLAYABLE rather than READY",
			stage: func(t *testing.T, f *deliveryFixture) {
				// PLAYABLE is deliverable but ladder-incomplete. The partial-delivery
				// behaviour entitled Students get is deliberately NOT extended to
				// anonymous visitors.
				//
				// A READY version is immutable, so this attaches a separate PLAYABLE
				// version rather than demoting the fixture's — which is also the honest
				// shape: an incomplete asset was never READY.
				f.attachPlayableLessonVideo(t)
				allowLessonPreview(t, f, true)
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			f := newDeliveryFixture(t)
			testCase.stage(t, f)
			_, err := f.delivery.IssueLessonPreview(f.ctx, lessonPreviewRequest(f))
			if testCase.allowed {
				if err != nil {
					t.Fatalf("IssueLessonPreview: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrProtectedUnavailable) {
				t.Fatalf("IssueLessonPreview = %v, want the inventory-safe unavailable answer", err)
			}
		})
	}
}

// TestLessonPreviewTokenDomainIsSeparateFromStudentPlayback is the token
// separation proof.
//
// The separation is structural rather than checked: the two domains sign with
// different domain strings, so neither token can be presented to the other's
// endpoint even in principle, and no nil check or optional field stands between
// them.
func TestLessonPreviewTokenDomainIsSeparateFromStudentPlayback(t *testing.T) {
	f := newDeliveryFixture(t)
	allowLessonPreview(t, f, true)

	preview, err := f.delivery.IssueLessonPreview(f.ctx, lessonPreviewRequest(f))
	if err != nil {
		t.Fatalf("IssueLessonPreview: %v", err)
	}
	student, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, DeviceID: testDeviceID(f.student),
		LessonID: f.lesson, AssetVersionID: f.video,
	})
	if err != nil {
		t.Fatalf("IssuePlayback: %v", err)
	}

	// A preview token is refused by every protected Student endpoint.
	if _, err := f.delivery.IssuePlaybackManifest(f.ctx, f.playbackSessionRequest(preview.PreviewSession)); !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("Student manifest accepted a preview token: %v", err)
	}
	if _, err := f.delivery.IssuePlaybackRenditionManifest(f.ctx,
		f.playbackRenditionRequest(preview.PreviewSession, "720p")); !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("Student rendition manifest accepted a preview token: %v", err)
	}
	if _, err := f.delivery.RenewPlayback(f.ctx, f.playbackSessionRequest(preview.PreviewSession)); err == nil {
		t.Fatal("playback heartbeat accepted a preview token")
	}

	// And a Student token is refused by the preview endpoints.
	if _, err := f.delivery.IssueLessonPreviewManifest(f.ctx, student.PlaybackSession); !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("preview manifest accepted a Student playback token: %v", err)
	}
	if _, err := f.delivery.IssueLessonPreviewRenditionManifest(f.ctx, student.PlaybackSession, "720p"); !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("preview rendition manifest accepted a Student playback token: %v", err)
	}

	// The preview token grants no entitlement and names no viewer: its claims
	// carry no Student, device or lease, so nothing in it could be mistaken for one.
	claims, err := f.delivery.verifyLessonPreviewSession(preview.PreviewSession, f.now)
	if err != nil {
		t.Fatalf("verifying the preview session: %v", err)
	}
	if claims.CourseID != f.courseID || claims.LessonID != f.lesson || claims.AssetVersionID != f.video {
		t.Fatalf("preview claims = %+v", claims)
	}
}

// TestLessonPreviewTokenRejectsReplayAndTampering pins the scope of the
// capability. Every identity in the claims is re-proved, so a token cannot be
// carried across Courses, revisions, Lessons or assets.
func TestLessonPreviewTokenRejectsReplayAndTampering(t *testing.T) {
	f := newDeliveryFixture(t)
	allowLessonPreview(t, f, true)
	issued, err := f.delivery.IssueLessonPreview(f.ctx, lessonPreviewRequest(f))
	if err != nil {
		t.Fatalf("IssueLessonPreview: %v", err)
	}
	valid, err := f.delivery.verifyLessonPreviewSession(issued.PreviewSession, f.now)
	if err != nil {
		t.Fatalf("verifying: %v", err)
	}

	forged := []struct {
		name   string
		claims lessonPreviewClaims
	}{
		{name: "wrong course", claims: withCourse(valid, "10000000-0000-0000-0000-0000000000ff")},
		{name: "wrong revision", claims: withRevision(valid, "10000000-0000-0000-0000-0000000000fe")},
		{name: "wrong lesson", claims: withLesson(valid, "10000000-0000-0000-0000-0000000000fd")},
		{name: "wrong asset version", claims: withAsset(valid, "10000000-0000-0000-0000-0000000000fc")},
	}
	for _, forgery := range forged {
		t.Run(forgery.name, func(t *testing.T) {
			// Signed by this server, so the signature is genuine — and still refused,
			// because the resolver re-proves the relationship rather than trusting the
			// claims.
			token := f.delivery.signLessonPreviewSession(forgery.claims)
			if _, err := f.delivery.IssueLessonPreviewManifest(f.ctx, token); !errors.Is(err, ErrProtectedUnavailable) {
				t.Fatalf("%s was accepted: %v", forgery.name, err)
			}
		})
	}

	t.Run("tampered signature", func(t *testing.T) {
		tampered := issued.PreviewSession[:len(issued.PreviewSession)-2] + "AA"
		if _, err := f.delivery.IssueLessonPreviewManifest(f.ctx, tampered); !errors.Is(err, ErrProtectedUnavailable) {
			t.Fatalf("a tampered preview token was accepted: %v", err)
		}
	})

	t.Run("expired", func(t *testing.T) {
		expired := valid
		expired.ExpiresAt = f.now.Add(-time.Second).Unix()
		token := f.delivery.signLessonPreviewSession(expired)
		if _, err := f.delivery.IssueLessonPreviewManifest(f.ctx, token); !errors.Is(err, ErrProtectedUnavailable) {
			t.Fatalf("an expired preview token was accepted: %v", err)
		}
	})

	t.Run("lesson stops being previewable mid-session", func(t *testing.T) {
		allowLessonPreview(t, f, false)
		if _, err := f.delivery.IssueLessonPreviewManifest(f.ctx, issued.PreviewSession); !errors.Is(err, ErrProtectedUnavailable) {
			t.Fatal("a previously valid token kept serving after preview was withdrawn")
		}
	})
}

func withCourse(claims lessonPreviewClaims, value string) lessonPreviewClaims {
	claims.CourseID = value
	return claims
}

func withRevision(claims lessonPreviewClaims, value string) lessonPreviewClaims {
	claims.RevisionID = value
	return claims
}

func withLesson(claims lessonPreviewClaims, value string) lessonPreviewClaims {
	claims.LessonID = value
	return claims
}

func withAsset(claims lessonPreviewClaims, value string) lessonPreviewClaims {
	claims.AssetVersionID = value
	return claims
}

// TestLessonPreviewTTLIsBoundedByTheMeasuredDuration documents the chosen
// lifetime. It is the measured duration plus the configured grace, capped well
// below the Student ceiling, because a preview token is a shared anonymous bearer
// capability with no lease behind it and nothing to revoke it mid-stream.
func TestLessonPreviewTTLIsBoundedByTheMeasuredDuration(t *testing.T) {
	f := newDeliveryFixture(t)
	allowLessonPreview(t, f, true)
	issued, err := f.delivery.IssueLessonPreview(f.ctx, lessonPreviewRequest(f))
	if err != nil {
		t.Fatalf("IssueLessonPreview: %v", err)
	}
	// The fixture video is 60 seconds and the configured grace is 5 minutes.
	want := f.now.Add(5*time.Minute + 60*time.Second)
	if !issued.ExpiresAt.Equal(want) {
		t.Fatalf("expiry = %s, want the measured duration plus the grace (%s)", issued.ExpiresAt, want)
	}
	if issued.ExpiresAt.Sub(f.now) >= maxPlaybackLifetime {
		t.Fatalf("anonymous preview inherited the Student ceiling: %s", issued.ExpiresAt.Sub(f.now))
	}

	// An absurd declared duration is clamped rather than trusted.
	if got := lessonPreviewLifetime(5*time.Minute, int64(24*time.Hour/time.Millisecond)); got != 5*time.Minute {
		t.Fatalf("clamped lifetime = %s, want the grace alone", got)
	}
	if got := lessonPreviewLifetime(5*time.Minute, int64(maxLessonPreviewLifetime/time.Millisecond)-1); got != maxLessonPreviewLifetime {
		t.Fatalf("ceiling lifetime = %s, want %s", got, maxLessonPreviewLifetime)
	}
}

// TestLessonPreviewLeavesLessonAttachmentsProtected is the video-only rule. The
// preview flag is about the Lesson video, and a Resource or Lab Material on the
// same Lesson stays entitlement-protected.
func TestLessonPreviewLeavesLessonAttachmentsProtected(t *testing.T) {
	f := newDeliveryFixture(t)
	allowLessonPreview(t, f, true)
	if _, err := f.delivery.IssueLessonPreview(f.ctx, lessonPreviewRequest(f)); err != nil {
		t.Fatalf("IssueLessonPreview: %v", err)
	}
	// There is no anonymous download path at all, and the entitled one still
	// requires a Student. Asking for the attachments with no Student is refused.
	for _, kind := range []AssetKind{KindResource, KindLabMaterial} {
		assetVersionID := f.resource
		if kind == KindLabMaterial {
			assetVersionID = f.lab
		}
		if _, err := f.delivery.IssueDownload(f.ctx, DownloadRequest{
			LessonID: f.lesson, AssetVersionID: assetVersionID, Kind: kind,
		}); !errors.Is(err, ErrProtectedUnavailable) {
			t.Fatalf("anonymous %s download = %v, want refused", kind, err)
		}
	}
}

// TestLegacyCoursePreviewSurvivesTheLessonPreviewModel is the transition
// contract. Production has live legacy previews and they must keep working.
func TestLegacyCoursePreviewSurvivesTheLessonPreviewModel(t *testing.T) {
	f := newDeliveryFixture(t)

	// With no Lesson preview, the legacy course-level preview serves exactly as
	// before.
	legacy, err := f.delivery.IssueCoursePreview(f.ctx, f.courseID)
	if err != nil {
		t.Fatalf("legacy course preview with no Lesson preview: %v", err)
	}
	if legacy.AssetVersionID != f.preview {
		t.Fatalf("legacy preview served %s, want the PREVIEW asset %s", legacy.AssetVersionID, f.preview)
	}

	// Turning on a Lesson preview does not disturb it. The legacy pointer stays
	// populated as rollback safety, the PREVIEW asset is untouched, and the legacy
	// route keeps answering — clearing any of that is a later tranche, after
	// production observation.
	allowLessonPreview(t, f, true)
	if _, err := f.delivery.IssueLessonPreview(f.ctx, lessonPreviewRequest(f)); err != nil {
		t.Fatalf("IssueLessonPreview: %v", err)
	}
	stillLegacy, err := f.delivery.IssueCoursePreview(f.ctx, f.courseID)
	if err != nil {
		t.Fatalf("legacy course preview after a Lesson preview went live: %v", err)
	}
	if stillLegacy.AssetVersionID != f.preview {
		t.Fatalf("legacy preview changed to %s", stillLegacy.AssetVersionID)
	}
	var pointer *string
	var previewState string
	var retiredAt *time.Time
	if err := f.pool.QueryRow(f.ctx, `
		SELECT cr.preview_asset_version_id::text, mav.state::text, ma.retired_at
		FROM course_revisions cr
		JOIN media_asset_versions mav ON mav.id = cr.preview_asset_version_id
		JOIN media_assets ma ON ma.id = mav.logical_asset_id
		WHERE cr.id = $1::uuid
	`, f.revision).Scan(&pointer, &previewState, &retiredAt); err != nil {
		t.Fatalf("reading legacy preview state: %v", err)
	}
	if pointer == nil || *pointer != f.preview || previewState != "READY" || retiredAt != nil {
		t.Fatalf("legacy preview disturbed: pointer=%v state=%s retired=%v", pointer, previewState, retiredAt)
	}
	// And the legacy by-identifier route still resolves the same asset.
	if byID, err := f.delivery.IssuePreview(f.ctx, f.preview); err != nil || byID.AssetVersionID != f.preview {
		t.Fatalf("legacy preview by identifier = %+v err=%v", byID, err)
	}
}

// TestStudentPlaybackIsUnchangedByLessonPreview guards the paid path. Nothing
// about entitled playback may change because an anonymous preview exists.
func TestStudentPlaybackIsUnchangedByLessonPreview(t *testing.T) {
	f := newDeliveryFixture(t)
	allowLessonPreview(t, f, true)

	auth, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, DeviceID: testDeviceID(f.student),
		LessonID: f.lesson, AssetVersionID: f.video,
	})
	if err != nil {
		t.Fatalf("IssuePlayback: %v", err)
	}
	// Still leased, still watermarked, still device-bound.
	if auth.Watermark == nil {
		t.Fatal("entitled playback lost its watermark")
	}
	if auth.Heartbeat == nil {
		t.Fatal("entitled playback lost its heartbeat contract")
	}
	if _, err := f.delivery.IssuePlaybackManifest(f.ctx, f.playbackSessionRequest(auth.PlaybackSession)); err != nil {
		t.Fatalf("entitled manifest: %v", err)
	}
	if _, err := f.delivery.RenewPlayback(f.ctx, f.playbackSessionRequest(auth.PlaybackSession)); err != nil {
		t.Fatalf("entitled heartbeat: %v", err)
	}
	// And a different device still cannot use it.
	if _, err := f.delivery.IssuePlaybackManifest(f.ctx, PlaybackSessionRequest{
		StudentID: f.student, DeviceID: testDeviceID("another-device"), Token: auth.PlaybackSession,
	}); err == nil {
		t.Fatal("entitled playback stopped being device-bound")
	}
}
