//go:build integration

package media

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func (f *deliveryFixture) insertAssetWithState(kind AssetKind, state AssetVersionState, rungs ...string) string {
	f.t.Helper()
	assetID, versionID, scanID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	key := "quarantine/" + f.courseID + "/" + versionID + "/source"
	var lessonID, previewOriginRevisionID any = f.lesson, nil
	if kind == KindPreview {
		lessonID = nil
		previewOriginRevisionID = f.revision
	}
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO media_assets (id, kind, owner_account_id, course_id, lesson_id, preview_origin_revision_id, visibility)
		VALUES ($1::uuid, $2::media_asset_kind, $3::uuid, $4::uuid, $5::uuid, $6::uuid, $7::media_asset_visibility)
	`, assetID, kind, f.instructorID, f.courseID, lessonID, previewOriginRevisionID, visibilityForKind(kind)); err != nil {
		f.t.Fatal(err)
	}
	contentType := "video/mp4"
	if kind == KindResource || kind == KindLabMaterial {
		contentType = "application/pdf"
	}
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes)
		VALUES ($1::uuid, $2::uuid, $3::media_asset_kind, 'QUARANTINED', $4, 'v1', $5, 12)
	`, versionID, assetID, kind, key, contentType); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO scan_attempts (id, asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity)
		VALUES ($1::uuid, $2::uuid, 1, $3, 'v1', 'PASSED', 'fixture')
	`, scanID, versionID, "scan:"+versionID); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE media_asset_versions SET state = 'SCANNING' WHERE id = $1::uuid`, versionID); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions
		SET successful_scan_attempt_id = $1::uuid, state = 'SCAN_PASSED'
		WHERE id = $2::uuid
	`, scanID, versionID); err != nil {
		f.t.Fatal(err)
	}
	if state == StateScanPassed {
		return versionID
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE media_asset_versions SET state = 'PROCESSING' WHERE id = $1::uuid`, versionID); err != nil {
		f.t.Fatal(err)
	}
	for _, rungName := range rungs {
		rung, ok := hlsRungByName(rungName)
		if !ok {
			f.t.Fatalf("unknown rung %s", rungName)
		}
		storageKey := fmt.Sprintf("media/%s/hls/%s/playlist.m3u8", versionID, rungName)
		if _, err := f.pool.Exec(f.ctx, `
			INSERT INTO video_renditions (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms)
			VALUES ($1::uuid, $2, $3, $4, $5, $6, 60000)
		`, versionID, rungName, storageKey, rung.Width, rung.Height, rung.VideoKbps); err != nil {
			f.t.Fatal(err)
		}
	}
	if state == StateProcessing {
		return versionID
	}
	if state == StatePlayable {
		if _, err := f.pool.Exec(f.ctx, `
			UPDATE media_asset_versions SET state = 'PLAYABLE' WHERE id = $1::uuid
		`, versionID); err != nil {
			f.t.Fatal(err)
		}
		return versionID
	}
	if state == StateReady {
		procID := uuid.NewString()
		if _, err := f.pool.Exec(f.ctx, `
			INSERT INTO processing_attempts (id, asset_version_id, operation_id, state, output_prefix, rendition_count, trusted_duration_ms)
			VALUES ($1::uuid, $2::uuid, $3, 'SUCCEEDED', 'video/hls', $4, 60000)
		`, procID, versionID, "process:"+versionID, len(rungs)); err != nil {
			f.t.Fatal(err)
		}
		if _, err := f.pool.Exec(f.ctx, `
			UPDATE media_asset_versions
			SET successful_processing_attempt_id = $1::uuid, trusted_duration_ms = 60000, state = 'READY'
			WHERE id = $2::uuid
		`, procID, versionID); err != nil {
			f.t.Fatal(err)
		}
		return versionID
	}
	return versionID
}

func (f *deliveryFixture) insertVideoWithRungs(state AssetVersionState, rungs ...string) string {
	return f.insertAssetWithState(KindVideo, state, rungs...)
}

func (f *deliveryFixture) attachLessonVideo(versionID string) {
	f.t.Helper()
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE course_lessons SET video_asset_version_id = $1::uuid WHERE lesson_identity_id = $2::uuid
	`, versionID, f.lesson); err != nil {
		f.t.Fatal(err)
	}
}

// ============================================================================
// SECTION 25: AUTHORIZATION MATRIX
// ============================================================================

// 1. VIDEO + PROCESSING + 0 renditions -> rejected
func TestSection25_Case1_ProcessingWithZeroRenditionsRejected(t *testing.T) {
	f := newDeliveryFixture(t)
	versionID := f.insertVideoWithRungs(StateProcessing)
	f.attachLessonVideo(versionID)

	_, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, DeviceID: testDeviceID(f.student), LessonID: f.lesson, AssetVersionID: versionID,
	})
	if !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("expected ErrProtectedUnavailable, got %v", err)
	}
}

// 2. VIDEO + PLAYABLE + 0 renditions -> rejected fail-safe
func TestSection25_Case2_PlayableWithZeroRenditionsRejected(t *testing.T) {
	f := newDeliveryFixture(t)
	versionID := f.insertVideoWithRungs(StatePlayable) // 0 rungs
	f.attachLessonVideo(versionID)

	_, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, DeviceID: testDeviceID(f.student), LessonID: f.lesson, AssetVersionID: versionID,
	})
	if !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("expected ErrProtectedUnavailable, got %v", err)
	}
}

// 3. VIDEO + PLAYABLE + 1 rendition + authorized user -> playback issuance succeeds
func TestSection25_Case3_PlayableWithOneRenditionAuthorizedUserSucceeds(t *testing.T) {
	f := newDeliveryFixture(t)
	versionID := f.insertVideoWithRungs(StatePlayable, "720p")
	f.attachLessonVideo(versionID)

	auth, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, DeviceID: testDeviceID(f.student), LessonID: f.lesson, AssetVersionID: versionID,
	})
	if err != nil {
		t.Fatalf("IssuePlayback failed: %v", err)
	}
	if auth.PlaybackSession == "" {
		t.Fatal("expected non-empty PlaybackSession")
	}
	expectedManifestURL := "/api/v1/media/playback-manifests/" + auth.PlaybackSession + "/index.m3u8"
	if auth.ManifestURL != expectedManifestURL {
		t.Fatalf("manifest url = %s, want %s", auth.ManifestURL, expectedManifestURL)
	}
	if !auth.ExpiresAt.After(f.now) {
		t.Fatalf("expected expires_at in future, got %v", auth.ExpiresAt)
	}
	if auth.Watermark == nil || auth.Watermark.Code == "" {
		t.Fatal("expected non-empty Watermark")
	}
	if auth.Heartbeat == nil || auth.Heartbeat.IntervalSeconds <= 0 {
		t.Fatal("expected valid Heartbeat")
	}

	// TrustedVideoDuration must also succeed for streamable PLAYABLE video
	duration, err := f.delivery.TrustedVideoDuration(f.ctx, f.lesson, versionID)
	if err != nil {
		t.Fatalf("TrustedVideoDuration failed: %v", err)
	}
	if duration != 60*time.Second {
		t.Fatalf("duration = %v, want 60s", duration)
	}
}

// 4. VIDEO + PLAYABLE + multiple renditions -> succeeds
func TestSection25_Case4_PlayableWithMultipleRenditionsSucceeds(t *testing.T) {
	f := newDeliveryFixture(t)
	versionID := f.insertVideoWithRungs(StatePlayable, "720p", "480p")
	f.attachLessonVideo(versionID)

	auth, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, DeviceID: testDeviceID(f.student), LessonID: f.lesson, AssetVersionID: versionID,
	})
	if err != nil {
		t.Fatalf("IssuePlayback failed: %v", err)
	}
	if auth.PlaybackSession == "" {
		t.Fatal("expected non-empty PlaybackSession")
	}
}

// 5. VIDEO + READY -> continues succeeding
func TestSection25_Case5_ReadyVideoSucceeds(t *testing.T) {
	f := newDeliveryFixture(t)
	versionID := f.insertVideoWithRungs(StateReady, "720p", "480p", "240p")
	f.attachLessonVideo(versionID)

	auth, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, DeviceID: testDeviceID(f.student), LessonID: f.lesson, AssetVersionID: versionID,
	})
	if err != nil {
		t.Fatalf("IssuePlayback failed: %v", err)
	}
	if auth.PlaybackSession == "" {
		t.Fatal("expected non-empty PlaybackSession")
	}
}

// 6. PLAYABLE + unauthorized student -> rejected
func TestSection25_Case6_PlayableUnauthorizedStudentRejected(t *testing.T) {
	f := newDeliveryFixture(t)
	versionID := f.insertVideoWithRungs(StatePlayable, "720p")
	f.attachLessonVideo(versionID)

	otherStudent := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name, locale, email_verified_at)
		VALUES ($1::uuid, 'other@test.local', 'other@test.local', 'STUDENT', 'ACTIVE', 'Other Student', 'en', now())
	`, otherStudent); err != nil {
		t.Fatal(err)
	}

	_, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: otherStudent, DeviceID: testDeviceID(otherStudent), LessonID: f.lesson, AssetVersionID: versionID,
	})
	if !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("expected ErrProtectedUnavailable, got %v", err)
	}
}

// 7. PLAYABLE + no entitlement -> rejected
func TestSection25_Case7_PlayableNoEntitlementRejected(t *testing.T) {
	f := newDeliveryFixture(t)
	versionID := f.insertVideoWithRungs(StatePlayable, "720p")
	f.attachLessonVideo(versionID)

	// Expire student's entitlement
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE entitlements SET access_ends_at = $1 WHERE student_account_id = $2::uuid
	`, f.now.Add(-2*time.Hour), f.student); err != nil {
		t.Fatal(err)
	}

	_, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, DeviceID: testDeviceID(f.student), LessonID: f.lesson, AssetVersionID: versionID,
	})
	if !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("expected ErrProtectedUnavailable, got %v", err)
	}
}

// 8. PLAYABLE candidate/non-live revision -> rejected
func TestSection25_Case8_PlayableCandidateOrNonLiveRevisionRejected(t *testing.T) {
	f := newDeliveryFixture(t)
	draftRevID := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en)
		VALUES ($1::uuid, $2::uuid, 'DRAFT', 2, 'مسودة', 'Draft')
	`, draftRevID, f.courseID); err != nil {
		t.Fatal(err)
	}

	draftSecIdentity := uuid.NewString()
	draftSecRow := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)
	`, draftSecIdentity, f.courseID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'قسم', 'Section', 0)
	`, draftSecRow, draftRevID, f.courseID, draftSecIdentity); err != nil {
		t.Fatal(err)
	}

	draftLesIdentity := uuid.NewString()
	draftLesRow := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, $3::uuid)
	`, draftLesIdentity, f.courseID, draftSecIdentity); err != nil {
		t.Fatal(err)
	}

	versionID := f.insertVideoWithRungs(StatePlayable, "720p")
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position, video_asset_version_id)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'درس', 'Lesson', 0, $6::uuid)
	`, draftLesRow, draftSecRow, f.courseID, draftSecIdentity, draftLesIdentity, versionID); err != nil {
		t.Fatal(err)
	}

	_, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, DeviceID: testDeviceID(f.student), LessonID: draftLesIdentity, AssetVersionID: versionID,
	})
	if !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("expected ErrProtectedUnavailable, got %v", err)
	}
}

// 9. Non-video PLAYABLE fixture -> rejected
func TestSection25_Case9_NonVideoPlayableFixtureRejected(t *testing.T) {
	f := newDeliveryFixture(t)
	resourceID := f.insertAssetWithState(KindResource, StatePlayable)
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO lesson_files (lesson_id, kind, asset_version_id, display_name_ar, display_name_en, position)
		SELECT cl.id, 'RESOURCE', $1::uuid, 'مرجع', 'Resource', 10
		FROM course_lessons cl WHERE cl.lesson_identity_id = $2::uuid
	`, resourceID, f.lesson); err != nil {
		t.Fatal(err)
	}

	_, err := f.delivery.IssueDownload(f.ctx, DownloadRequest{
		StudentID: f.student, LessonID: f.lesson, AssetVersionID: resourceID, Kind: KindResource,
	})
	if !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("expected ErrProtectedUnavailable for PLAYABLE resource, got %v", err)
	}
}

// 10. Public preview PLAYABLE -> rejected
func TestSection25_Case10_PublicPreviewPlayableRejected(t *testing.T) {
	f := newDeliveryFixture(t)
	previewID := f.insertAssetWithState(KindPreview, StatePlayable)
	if _, err := f.pool.Exec(f.ctx, `UPDATE course_revisions SET preview_asset_version_id = $1::uuid WHERE id = $2::uuid`, previewID, f.revision); err != nil {
		t.Fatal(err)
	}

	_, err := f.delivery.IssuePreview(f.ctx, previewID)
	if !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("expected ErrProtectedUnavailable for PLAYABLE preview, got %v", err)
	}

	_, err = f.delivery.IssueCoursePreview(f.ctx, f.courseID)
	if !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("expected ErrProtectedUnavailable for course with PLAYABLE preview, got %v", err)
	}
}

// ============================================================================
// SECTION 26: HLS MATRIX
// ============================================================================

// 1. One-rendition dynamic master syntax is valid
func TestSection26_Case1_OneRenditionMasterSyntaxValid(t *testing.T) {
	f := newDeliveryFixture(t)
	versionID := f.insertVideoWithRungs(StatePlayable, "720p")
	f.attachLessonVideo(versionID)

	auth, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, DeviceID: testDeviceID(f.student), LessonID: f.lesson, AssetVersionID: versionID,
	})
	if err != nil {
		t.Fatalf("IssuePlayback failed: %v", err)
	}

	manifest, err := f.delivery.IssuePlaybackManifest(f.ctx, f.playbackSessionRequest(auth.PlaybackSession))
	if err != nil {
		t.Fatalf("IssuePlaybackManifest failed: %v", err)
	}
	contents := string(manifest.Contents)
	if !strings.HasPrefix(contents, "#EXTM3U\n#EXT-X-VERSION:3\n") {
		t.Fatalf("unexpected master header: %s", contents)
	}
	expectedStream := "#EXT-X-STREAM-INF:BANDWIDTH=2928000,RESOLUTION=1280x720\n" +
		"/api/v1/media/playback-manifests/" + auth.PlaybackSession + "/renditions/720p/index.m3u8"
	if !strings.Contains(contents, expectedStream) {
		t.Fatalf("master omitted expected stream inf %q in:\n%s", expectedStream, contents)
	}
}

// 2. Master includes only currently persisted rows
func TestSection26_Case2_MasterIncludesOnlyCurrentlyPersistedRows(t *testing.T) {
	f := newDeliveryFixture(t)
	versionID := f.insertVideoWithRungs(StatePlayable, "720p")
	f.attachLessonVideo(versionID)

	auth, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, DeviceID: testDeviceID(f.student), LessonID: f.lesson, AssetVersionID: versionID,
	})
	if err != nil {
		t.Fatalf("IssuePlayback failed: %v", err)
	}

	manifest, err := f.delivery.IssuePlaybackManifest(f.ctx, f.playbackSessionRequest(auth.PlaybackSession))
	if err != nil {
		t.Fatalf("IssuePlaybackManifest failed: %v", err)
	}
	contents := string(manifest.Contents)
	if strings.Contains(contents, "480p") || strings.Contains(contents, "240p") || strings.Contains(contents, "1080p") {
		t.Fatalf("master contains unpersisted renditions:\n%s", contents)
	}
}

// 3. Adding a second rendition evolves new master response
func TestSection26_Case3_AddingSecondRenditionEvolvesNewMasterResponse(t *testing.T) {
	f := newDeliveryFixture(t)
	versionID := f.insertVideoWithRungs(StatePlayable, "720p")
	f.attachLessonVideo(versionID)

	auth, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, DeviceID: testDeviceID(f.student), LessonID: f.lesson, AssetVersionID: versionID,
	})
	if err != nil {
		t.Fatalf("IssuePlayback failed: %v", err)
	}

	// First call has only 720p
	m1, err := f.delivery.IssuePlaybackManifest(f.ctx, f.playbackSessionRequest(auth.PlaybackSession))
	if err != nil {
		t.Fatalf("m1 failed: %v", err)
	}
	if strings.Contains(string(m1.Contents), "480p") {
		t.Fatal("m1 should not contain 480p")
	}

	// Persist second rendition (480p)
	rung480, _ := hlsRungByName("480p")
	storageKey480 := fmt.Sprintf("media/%s/hls/480p/playlist.m3u8", versionID)
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO video_renditions (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms)
		VALUES ($1::uuid, '480p', $2, $3, $4, $5, 60000)
	`, versionID, storageKey480, rung480.Width, rung480.Height, rung480.VideoKbps); err != nil {
		t.Fatal(err)
	}

	// Second call with same session token dynamically evolves to include 480p
	m2, err := f.delivery.IssuePlaybackManifest(f.ctx, f.playbackSessionRequest(auth.PlaybackSession))
	if err != nil {
		t.Fatalf("m2 failed: %v", err)
	}
	contents2 := string(m2.Contents)
	if !strings.Contains(contents2, "720p") || !strings.Contains(contents2, "480p") {
		t.Fatalf("m2 must contain both 720p and 480p:\n%s", contents2)
	}
}

// 4. Deterministic ordering (height DESC)
func TestSection26_Case4_DeterministicOrderingHeightDesc(t *testing.T) {
	f := newDeliveryFixture(t)
	// Insert in non-sorted order: 240p then 720p
	versionID := f.insertVideoWithRungs(StatePlayable, "240p", "720p")
	f.attachLessonVideo(versionID)

	auth, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, DeviceID: testDeviceID(f.student), LessonID: f.lesson, AssetVersionID: versionID,
	})
	if err != nil {
		t.Fatalf("IssuePlayback failed: %v", err)
	}

	manifest, err := f.delivery.IssuePlaybackManifest(f.ctx, f.playbackSessionRequest(auth.PlaybackSession))
	if err != nil {
		t.Fatalf("IssuePlaybackManifest failed: %v", err)
	}
	contents := string(manifest.Contents)
	idx720 := strings.Index(contents, "720p")
	idx240 := strings.Index(contents, "240p")
	if idx720 == -1 || idx240 == -1 || idx720 >= idx240 {
		t.Fatalf("expected 720p before 240p in master, got idx720=%d, idx240=%d:\n%s", idx720, idx240, contents)
	}
}

// 5. Rendition playlist access works for PLAYABLE
func TestSection26_Case5_RenditionPlaylistAccessWorksForPlayable(t *testing.T) {
	f := newDeliveryFixture(t)
	versionID := f.insertVideoWithRungs(StatePlayable, "720p")
	f.attachLessonVideo(versionID)

	auth, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, DeviceID: testDeviceID(f.student), LessonID: f.lesson, AssetVersionID: versionID,
	})
	if err != nil {
		t.Fatalf("IssuePlayback failed: %v", err)
	}

	req := f.playbackRenditionRequest(auth.PlaybackSession, "720p")
	renditionManifest, err := f.delivery.IssuePlaybackRenditionManifest(f.ctx, req)
	if err != nil {
		t.Fatalf("IssuePlaybackRenditionManifest failed: %v", err)
	}
	if !strings.HasPrefix(string(renditionManifest.Contents), "#EXTM3U") {
		t.Fatalf("expected valid m3u8 playlist, got:\n%s", string(renditionManifest.Contents))
	}
}

// 6. Segment signing works unchanged
func TestSection26_Case6_SegmentSigningWorksUnchanged(t *testing.T) {
	f := newDeliveryFixture(t)
	versionID := f.insertVideoWithRungs(StatePlayable, "720p")
	f.attachLessonVideo(versionID)

	auth, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, DeviceID: testDeviceID(f.student), LessonID: f.lesson, AssetVersionID: versionID,
	})
	if err != nil {
		t.Fatalf("IssuePlayback failed: %v", err)
	}

	req := f.playbackRenditionRequest(auth.PlaybackSession, "720p")
	renditionManifest, err := f.delivery.IssuePlaybackRenditionManifest(f.ctx, req)
	if err != nil {
		t.Fatalf("IssuePlaybackRenditionManifest failed: %v", err)
	}
	contents := string(renditionManifest.Contents)
	expectedSignedSegment := "https://storage.test/signed/media/" + versionID + "/hls/720p/segment000.ts"
	if !strings.Contains(contents, expectedSignedSegment) {
		t.Fatalf("rendition playlist omitted expected signed segment %q:\n%s", expectedSignedSegment, contents)
	}
}

// 7. Malicious/foreign playlist key rejected
func TestSection26_Case7_MaliciousForeignPlaylistKeyRejected(t *testing.T) {
	f := newDeliveryFixture(t)
	versionID := f.insertVideoWithRungs(StatePlayable, "720p")
	f.attachLessonVideo(versionID)

	auth, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, DeviceID: testDeviceID(f.student), LessonID: f.lesson, AssetVersionID: versionID,
	})
	if err != nil {
		t.Fatalf("IssuePlayback failed: %v", err)
	}

	for _, unsafeSelector := range []string{"../720p", "foo/bar", "%2e%2e", "unknown", "480p"} {
		req := f.playbackRenditionRequest(auth.PlaybackSession, unsafeSelector)
		_, err := f.delivery.IssuePlaybackRenditionManifest(f.ctx, req)
		if !errors.Is(err, ErrProtectedUnavailable) {
			t.Fatalf("selector %q: expected ErrProtectedUnavailable, got %v", unsafeSelector, err)
		}
	}
}

// 8. Cross-asset rendition access rejected
func TestSection26_Case8_CrossAssetRenditionAccessRejected(t *testing.T) {
	f := newDeliveryFixture(t)
	v1 := f.insertVideoWithRungs(StatePlayable, "720p")
	f.attachLessonVideo(v1)

	// Create a second video version with 480p
	_ = f.insertVideoWithRungs(StatePlayable, "480p")

	auth, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, DeviceID: testDeviceID(f.student), LessonID: f.lesson, AssetVersionID: v1,
	})
	if err != nil {
		t.Fatalf("IssuePlayback failed: %v", err)
	}

	// Session is bound to v1 (which has only 720p). Requesting 480p must be rejected.
	req := f.playbackRenditionRequest(auth.PlaybackSession, "480p")
	_, err = f.delivery.IssuePlaybackRenditionManifest(f.ctx, req)
	if !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("expected ErrProtectedUnavailable for cross-asset selector, got %v", err)
	}
}

// 9. No-store caching behavior verified
func TestSection26_Case9_NoStoreCachingBehavior(t *testing.T) {
	f := newDeliveryFixture(t)
	versionID := f.insertVideoWithRungs(StatePlayable, "720p")
	f.attachLessonVideo(versionID)

	auth, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, DeviceID: testDeviceID(f.student), LessonID: f.lesson, AssetVersionID: versionID,
	})
	if err != nil {
		t.Fatalf("IssuePlayback failed: %v", err)
	}

	// Verify that each manifest request dynamically evaluates state and claims
	m1, err := f.delivery.IssuePlaybackManifest(f.ctx, f.playbackSessionRequest(auth.PlaybackSession))
	if err != nil {
		t.Fatalf("IssuePlaybackManifest failed: %v", err)
	}
	if len(m1.Contents) == 0 {
		t.Fatal("empty manifest contents")
	}

	// When student access expires, the exact same token immediately refuses manifest issuance
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE entitlements SET access_ends_at = $1 WHERE student_account_id = $2::uuid
	`, f.now.Add(-time.Hour), f.student); err != nil {
		t.Fatal(err)
	}

	_, err = f.delivery.IssuePlaybackManifest(f.ctx, f.playbackSessionRequest(auth.PlaybackSession))
	if !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("expected ErrProtectedUnavailable after entitlement expiry, got %v", err)
	}
}

// 10. READY master behavior unchanged
func TestSection26_Case10_ReadyMasterBehaviorUnchanged(t *testing.T) {
	f := newDeliveryFixture(t)
	versionID := f.insertVideoWithRungs(StateReady, "720p", "480p", "240p")
	f.attachLessonVideo(versionID)

	auth, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, DeviceID: testDeviceID(f.student), LessonID: f.lesson, AssetVersionID: versionID,
	})
	if err != nil {
		t.Fatalf("IssuePlayback failed: %v", err)
	}

	manifest, err := f.delivery.IssuePlaybackManifest(f.ctx, f.playbackSessionRequest(auth.PlaybackSession))
	if err != nil {
		t.Fatalf("IssuePlaybackManifest failed: %v", err)
	}
	contents := string(manifest.Contents)
	for _, expected := range []string{"720p", "480p", "240p"} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("READY master omitted %s:\n%s", expected, contents)
		}
	}
}

// ============================================================================
// SECTION 27: FAILED PLAYABLE (SURVIVAL)
// ============================================================================

func TestSection27_FailedPlayableSurvival(t *testing.T) {
	f := newDeliveryFixture(t)
	versionID := f.insertVideoWithRungs(StatePlayable, "720p")
	f.attachLessonVideo(versionID)

	// Simulate a transcode crash / terminal FAILED processing attempt
	attemptID := uuid.NewString()
	failedOpID := "op-failed-" + uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO processing_attempts (id, asset_version_id, operation_id, state, error_reason)
		VALUES ($1::uuid, $2::uuid, $3, 'FAILED', 'process crash during 480p')
	`, attemptID, versionID, failedOpID); err != nil {
		t.Fatal(err)
	}

	// Ensure work_claim_token is NULL
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions SET work_claim_token = NULL WHERE id = $1::uuid
	`, versionID); err != nil {
		t.Fatal(err)
	}

	// Playback issuance must still succeed from the verified 720p rendition
	auth, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, DeviceID: testDeviceID(f.student), LessonID: f.lesson, AssetVersionID: versionID,
	})
	if err != nil {
		t.Fatalf("IssuePlayback failed for failed playable: %v", err)
	}
	if auth.PlaybackSession == "" {
		t.Fatal("expected non-empty PlaybackSession")
	}

	// Master manifest must succeed
	master, err := f.delivery.IssuePlaybackManifest(f.ctx, f.playbackSessionRequest(auth.PlaybackSession))
	if err != nil {
		t.Fatalf("IssuePlaybackManifest failed: %v", err)
	}
	if !strings.Contains(string(master.Contents), "720p") {
		t.Fatalf("master omitted 720p:\n%s", string(master.Contents))
	}

	// Rendition manifest must succeed
	renditionManifest, err := f.delivery.IssuePlaybackRenditionManifest(f.ctx, f.playbackRenditionRequest(auth.PlaybackSession, "720p"))
	if err != nil {
		t.Fatalf("IssuePlaybackRenditionManifest failed: %v", err)
	}
	if !strings.Contains(string(renditionManifest.Contents), "segment000.ts") {
		t.Fatalf("rendition manifest omitted segment000.ts:\n%s", string(renditionManifest.Contents))
	}
}
