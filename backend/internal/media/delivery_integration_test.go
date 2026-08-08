//go:build integration

package media

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Owlah2025/gradex/backend/internal/entitlement"
)

type signedDeliveryStore struct {
	mu       sync.Mutex
	keys     []string
	manifest []byte
}

func (s *signedDeliveryStore) PresignGetURL(_ context.Context, key string, _ time.Duration) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys = append(s.keys, key)
	return "https://storage.test/signed/" + key, nil
}

func (s *signedDeliveryStore) DownloadObject(_ context.Context, key string) ([]byte, error) {
	if !strings.HasSuffix(key, ".m3u8") {
		return nil, fmt.Errorf("unexpected manifest key %q", key)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.manifest != nil {
		return append([]byte(nil), s.manifest...), nil
	}
	return []byte("#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXTINF:6,\nsegment000.ts\n#EXT-X-ENDLIST\n"), nil
}

func (s *signedDeliveryStore) requestedKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.keys...)
}

func (s *signedDeliveryStore) setManifest(contents string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.manifest = []byte(contents)
}

type deliveryFixture struct {
	*mediaFixture
	student      string
	lesson       string
	section      string
	video        string
	resource     string
	lab          string
	preview      string
	store        *signedDeliveryStore
	delivery     *DeliveryService
	now          time.Time
	studentEmail string
}

func newDeliveryFixture(t *testing.T) *deliveryFixture {
	t.Helper()
	base := newMediaFixture(t)
	f := &deliveryFixture{mediaFixture: base, student: uuid.NewString(), section: uuid.NewString(), now: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC), store: &signedDeliveryStore{}}
	f.studentEmail = "student-d8@example.test"
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO accounts (id, normalized_email, email, role, status, display_name, locale, email_verified_at) VALUES ($1::uuid, $2, $2, 'STUDENT', 'ACTIVE', 'D8 Student', 'en', now())`, f.student, f.studentEmail); err != nil {
		t.Fatal(err)
	}
	revision := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, 'APPROVED', 1, 'دورة', 'Course')`, revision, f.courseID); err != nil {
		t.Fatal(err)
	}
	sectionRow := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)`, f.section, f.courseID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'قسم', 'Section', 0)`, sectionRow, revision, f.courseID, f.section); err != nil {
		t.Fatal(err)
	}
	f.lesson = uuid.NewString()
	lessonRow := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`, f.lesson, f.courseID, f.section); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'درس', 'Lesson', 0)`, lessonRow, sectionRow, f.courseID, f.section, f.lesson); err != nil {
		t.Fatal(err)
	}
	f.video = f.readyAsset(KindVideo, "video/hls/playlist.m3u8")
	f.resource = f.readyAsset(KindResource, "resource/file.pdf")
	f.lab = f.readyAsset(KindLabMaterial, "lab/file.zip")
	f.preview = f.readyAsset(KindPreview, "preview/playlist.m3u8")
	if _, err := f.pool.Exec(f.ctx, `UPDATE course_lessons SET video_asset_version_id = $1::uuid WHERE id = $2::uuid`, f.video, lessonRow); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO lesson_files (lesson_id, kind, asset_version_id, display_name_ar, display_name_en, position) VALUES ($1::uuid, 'RESOURCE', $2::uuid, 'مرجع', 'Resource', 0), ($1::uuid, 'LAB_MATERIAL', $3::uuid, 'مختبر', 'Lab', 0)`, lessonRow, f.resource, f.lab); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE course_revisions SET preview_asset_version_id = $1::uuid WHERE id = $2::uuid`, f.preview, revision); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE courses SET lifecycle = 'PUBLISHED', live_revision_id = $1::uuid WHERE id = $2::uuid`, revision, f.courseID); err != nil {
		t.Fatal(err)
	}
	f.seedGrant(uuid.NewString(), entitlement.ScopeCourse, f.courseID, f.now.Add(time.Hour))
	repo, err := entitlement.NewRepository(f.pool)
	if err != nil {
		t.Fatal(err)
	}
	evaluator, err := entitlement.NewEvaluator(repo)
	if err != nil {
		t.Fatal(err)
	}
	f.delivery, err = NewDeliveryService(DeliveryOptions{DB: f.pool, Store: f.store, Evaluator: evaluator, SignatureLifetime: 5 * time.Minute, BuyerTagKey: []byte("01234567890123456789012345678901"), Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *deliveryFixture) readyAsset(kind AssetKind, outputKey string) string {
	f.t.Helper()
	assetID, versionID, scanID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	key := "quarantine/" + f.courseID + "/" + versionID + "/source"
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO media_assets (id, kind, owner_account_id, course_id, lesson_id, visibility) VALUES ($1::uuid, $2::media_asset_kind, $3::uuid, $4::uuid, $5::uuid, $6::media_asset_visibility)`, assetID, kind, f.instructorID, f.courseID, f.lesson, visibilityForKind(kind)); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes) VALUES ($1::uuid, $2::uuid, $3::media_asset_kind, 'QUARANTINED', $4, 'v1', 'application/pdf', 12)`, versionID, assetID, kind, key); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO scan_attempts (id, asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity) VALUES ($1::uuid, $2::uuid, 1, $3, 'v1', 'PASSED', 'fixture')`, scanID, versionID, "scan:"+versionID); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE media_asset_versions SET state = 'SCANNING' WHERE id = $1::uuid`, versionID); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE media_asset_versions SET successful_scan_attempt_id = $1::uuid, state = 'SCAN_PASSED' WHERE id = $2::uuid`, scanID, versionID); err != nil {
		f.t.Fatal(err)
	}
	if kind != KindVideo {
		if _, err := f.pool.Exec(f.ctx, `UPDATE media_asset_versions SET state = 'READY' WHERE id = $1::uuid`, versionID); err != nil {
			f.t.Fatal(err)
		}
		return versionID
	}
	processingID := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO processing_attempts (id, asset_version_id, operation_id, state, output_prefix, rendition_count, trusted_duration_ms) VALUES ($1::uuid, $2::uuid, $3, 'SUCCEEDED', 'video/hls', 1, 60000)`, processingID, versionID, "process:"+versionID); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO video_renditions (asset_version_id, name, storage_object_key, duration_ms) VALUES ($1::uuid, '720p', $2, 60000)`, versionID, outputKey); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE media_asset_versions SET state = 'PROCESSING' WHERE id = $1::uuid`, versionID); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE media_asset_versions SET successful_processing_attempt_id = $1::uuid, trusted_duration_ms = 60000, state = 'READY' WHERE id = $2::uuid`, processingID, versionID); err != nil {
		f.t.Fatal(err)
	}
	return versionID
}

func (f *deliveryFixture) seedGrant(id string, scope entitlement.ScopeKind, scopeID string, ends time.Time) {
	f.t.Helper()
	invID := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO course_access_invitations (id, course_id, email, normalized_email, created_by_account_id, accepted_by_account_id, decided_by_account_id, state) VALUES ($1::uuid, $2::uuid, 'student@example.test', 'student@example.test', $3::uuid, $3::uuid, $3::uuid, 'APPROVED')`, invID, f.courseID, f.student); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO entitlements (id, student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state) VALUES ($1::uuid, $2::uuid, $3, $4::uuid, $5::uuid, 'MANUAL_INVITATION', $6::uuid, $7, $7, $8, 'ACTIVE')`, id, f.student, scope, scopeID, f.courseID, invID, ends, f.now.Add(-time.Hour)); err != nil {
		f.t.Fatal(err)
	}
}

func TestD8ProtectedDeliveryUsesExactReadyVersionAndPerRequestEvaluation(t *testing.T) {
	f := newDeliveryFixture(t)
	issued, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{StudentID: f.student, LessonID: f.lesson, AssetVersionID: f.video})
	if err != nil || issued.AssetVersionID != f.video || !strings.HasPrefix(issued.ManifestURL, "/api/v1/media/playback-manifests/") || issued.ExpiresAt.Sub(f.now) != 5*time.Minute {
		t.Fatalf("playback issuance=%+v err=%v", issued, err)
	}
	manifest, err := f.delivery.IssuePlaybackManifest(f.ctx, f.student, issued.PlaybackSession)
	if err != nil || !strings.Contains(string(manifest.Contents), "https://storage.test/signed/video/hls/segment000.ts") {
		t.Fatalf("playback manifest=%q err=%v", manifest.Contents, err)
	}
	replacement := f.readyAsset(KindVideo, "replacement/hls/playlist.m3u8")
	if _, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{StudentID: f.student, LessonID: f.lesson, AssetVersionID: replacement}); err == nil {
		t.Fatal("unreferenced replacement Asset Version received playback")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE entitlements SET access_ends_at = $1 WHERE student_account_id = $2::uuid`, f.now, f.student); err != nil {
		t.Fatal(err)
	}
	if _, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{StudentID: f.student, LessonID: f.lesson, AssetVersionID: f.video}); err == nil {
		t.Fatal("new playback issuance succeeded after effective expiry")
	}
	if len(f.store.requestedKeys()) != 1 {
		t.Fatalf("signed storage calls=%v, want one allowed exact segment", f.store.requestedKeys())
	}
}

func TestPlaybackManifestRejectsInvalidSessionsAndUnsafeReferences(t *testing.T) {
	f := newDeliveryFixture(t)
	issued, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, LessonID: f.lesson, AssetVersionID: f.video,
	})
	if err != nil {
		t.Fatalf("issuing playback: %v", err)
	}
	tampered := issued.PlaybackSession[:len(issued.PlaybackSession)-1] + "A"
	if tampered == issued.PlaybackSession {
		tampered = issued.PlaybackSession[:len(issued.PlaybackSession)-1] + "B"
	}
	if _, err := f.delivery.IssuePlaybackManifest(f.ctx, f.student, tampered); !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("tampered session error=%v, want %v", err, ErrProtectedUnavailable)
	}
	if _, err := f.delivery.IssuePlaybackManifest(f.ctx, uuid.NewString(), issued.PlaybackSession); !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("cross-Student session error=%v, want %v", err, ErrProtectedUnavailable)
	}

	for name, manifest := range map[string]string{
		"parent traversal": "#EXTM3U\n../other/segment.ts\n",
		"absolute target":  "#EXTM3U\nhttps://public.example/segment.ts\n",
		"tag URI":          "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\nsegment.ts\n",
	} {
		t.Run(name, func(t *testing.T) {
			f.store.setManifest(manifest)
			before := len(f.store.requestedKeys())
			if _, err := f.delivery.IssuePlaybackManifest(f.ctx, f.student, issued.PlaybackSession); !errors.Is(err, ErrProtectedUnavailable) {
				t.Fatalf("unsafe manifest error=%v, want %v", err, ErrProtectedUnavailable)
			}
			if got := len(f.store.requestedKeys()); got != before {
				t.Fatalf("unsafe manifest signed %d storage references", got-before)
			}
		})
	}
}

func TestD064StableMaterialEntryResolvesCurrentVersionAndRechecksAuthority(t *testing.T) {
	f := newDeliveryFixture(t)
	resource, err := f.delivery.IssueDownloadEntry(f.ctx, DownloadEntryRequest{StudentID: f.student, LessonID: f.lesson, Kind: KindResource})
	if err != nil || resource.AssetVersionID != f.resource {
		t.Fatalf("resource entry=%+v err=%v", resource, err)
	}
	lab, err := f.delivery.IssueDownloadEntry(f.ctx, DownloadEntryRequest{StudentID: f.student, LessonID: f.lesson, Kind: KindLabMaterial})
	if err != nil || lab.AssetVersionID != f.lab {
		t.Fatalf("lab entry=%+v err=%v", lab, err)
	}

	replacement := f.readyAsset(KindResource, "resource/replacement.pdf")
	var lessonRow string
	if err := f.pool.QueryRow(f.ctx, `SELECT id::text FROM course_lessons WHERE lesson_identity_id = $1::uuid`, f.lesson).Scan(&lessonRow); err != nil {
		t.Fatalf("finding lesson row: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE lesson_files SET asset_version_id = $1::uuid WHERE lesson_id = $2::uuid AND kind = 'RESOURCE'`, replacement, lessonRow); err != nil {
		t.Fatalf("replacing current resource version: %v", err)
	}
	replaced, err := f.delivery.IssueDownloadEntry(f.ctx, DownloadEntryRequest{StudentID: f.student, LessonID: f.lesson, Kind: KindResource})
	if err != nil || replaced.AssetVersionID != replacement {
		t.Fatalf("replacement entry=%+v err=%v", replaced, err)
	}

	beforeDenied := len(f.store.requestedKeys())
	if _, err := f.pool.Exec(f.ctx, `UPDATE accounts SET status = 'SUSPENDED' WHERE id = $1::uuid`, f.student); err != nil {
		t.Fatalf("suspending account: %v", err)
	}
	if _, err := f.delivery.IssueDownloadEntry(f.ctx, DownloadEntryRequest{StudentID: f.student, LessonID: f.lesson, Kind: KindResource}); err == nil {
		t.Fatal("material entry succeeded after account suspension")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE accounts SET status = 'ACTIVE' WHERE id = $1::uuid`, f.student); err != nil {
		t.Fatalf("restoring account: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE courses SET access_suspended_at = $1, access_suspension_reason = 'd064-test' WHERE id = $2::uuid`, f.now, f.courseID); err != nil {
		t.Fatalf("suspending Course access: %v", err)
	}
	if _, err := f.delivery.IssueDownloadEntry(f.ctx, DownloadEntryRequest{StudentID: f.student, LessonID: f.lesson, Kind: KindResource}); err == nil {
		t.Fatal("material entry succeeded during emergency suspension")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE courses SET access_suspended_at = NULL, access_suspension_reason = NULL WHERE id = $1::uuid`, f.courseID); err != nil {
		t.Fatalf("restoring Course access: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE courses SET retired_at = $1 WHERE id = $2::uuid`, f.now, f.courseID); err != nil {
		t.Fatalf("retiring Course: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE entitlements SET retirement_eligibility_at = $1 WHERE student_account_id = $2::uuid AND course_id = $3::uuid`, f.now, f.student, f.courseID); err != nil {
		t.Fatalf("removing retirement eligibility: %v", err)
	}
	if _, err := f.delivery.IssueDownloadEntry(f.ctx, DownloadEntryRequest{StudentID: f.student, LessonID: f.lesson, Kind: KindResource}); err == nil {
		t.Fatal("material entry succeeded for retired-ineligible Course")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE entitlements SET access_ends_at = $1 WHERE student_account_id = $2::uuid`, f.now, f.student); err != nil {
		t.Fatalf("revoking entitlement: %v", err)
	}
	if _, err := f.delivery.IssueDownloadEntry(f.ctx, DownloadEntryRequest{StudentID: f.student, LessonID: f.lesson, Kind: KindResource}); err == nil {
		t.Fatal("material entry succeeded after entitlement revocation")
	}
	if got := len(f.store.requestedKeys()); got != beforeDenied {
		t.Fatalf("signer called after denied entry: before=%d after=%d", beforeDenied, got)
	}
}

func TestD064MaterialKindsBulkReadExposesOnlyCurrentReadyKinds(t *testing.T) {
	f := newDeliveryFixture(t)
	unknownLesson := uuid.NewString()
	kinds, err := f.delivery.MaterialKinds(f.ctx, []string{f.lesson, unknownLesson})
	if err != nil {
		t.Fatalf("bulk material kinds: %v", err)
	}
	got := kinds[f.lesson]
	if len(got) != 2 || got[0].Kind != MaterialResource || got[1].Kind != MaterialLabMaterial {
		t.Fatalf("material kinds=%v, want deterministic resource/lab_material", got)
	}
	// The exact Asset Version each material resolved to is retained internally, so a report
	// context can bind the instance this read actually exposed (D-065).
	for _, material := range got {
		if material.AssetVersionID == "" {
			t.Fatalf("%s carried no exact Asset Version", material.Kind)
		}
	}
	if _, ok := kinds[unknownLesson]; ok {
		t.Fatal("bulk material kinds leaked an unknown Lesson")
	}
}

func TestD8MidPlaybackExpiryKeepsIssuedSignatureWithinItsOwnBound(t *testing.T) {
	f := newDeliveryFixture(t)
	if _, err := f.pool.Exec(f.ctx, `UPDATE entitlements SET access_ends_at = $1 WHERE student_account_id = $2::uuid`, f.now.Add(time.Minute), f.student); err != nil {
		t.Fatal(err)
	}
	issued, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{StudentID: f.student, LessonID: f.lesson, AssetVersionID: f.video})
	if err != nil {
		t.Fatalf("issuing pre-expiry playback: %v", err)
	}
	if got := issued.ExpiresAt.Sub(f.now); got != 5*time.Minute {
		t.Fatalf("issued signature lifetime=%s, want 5m", got)
	}
	if _, err := f.delivery.IssuePlaybackManifest(f.ctx, f.student, issued.PlaybackSession); err != nil {
		t.Fatalf("issuing pre-expiry manifest: %v", err)
	}

	// The storage signature was already minted. Advancing the injected clock
	// past access_ends_at cannot revoke that third-party capability, but its
	// lifetime remains bounded and a fresh server-side evaluation is denied.
	f.now = f.now.Add(2 * time.Minute)
	if !f.now.Before(issued.ExpiresAt) {
		t.Fatalf("test clock escaped issued signature lifetime: now=%s expires=%s", f.now, issued.ExpiresAt)
	}
	if _, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{StudentID: f.student, LessonID: f.lesson, AssetVersionID: f.video}); !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("post-expiry re-issuance error=%v, want %v", err, ErrProtectedUnavailable)
	}
	if _, err := f.delivery.IssuePlaybackManifest(f.ctx, f.student, issued.PlaybackSession); !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("post-expiry manifest error=%v, want %v", err, ErrProtectedUnavailable)
	}
	if len(f.store.requestedKeys()) != 1 {
		t.Fatalf("post-expiry re-issuance minted another signed URL: %v", f.store.requestedKeys())
	}
}

func TestD8ProtectedDeliveryCollapsesEveryEvaluatorDenial(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(*deliveryFixture) PlaybackRequest
	}{
		{"non-existent", func(f *deliveryFixture) PlaybackRequest {
			return PlaybackRequest{StudentID: f.student, LessonID: f.lesson, AssetVersionID: uuid.NewString()}
		}},
		{"expired", func(f *deliveryFixture) PlaybackRequest {
			if _, err := f.pool.Exec(f.ctx, `UPDATE entitlements SET access_ends_at = $1 WHERE student_account_id = $2::uuid`, f.now, f.student); err != nil {
				f.t.Fatal(err)
			}
			return PlaybackRequest{StudentID: f.student, LessonID: f.lesson, AssetVersionID: f.video}
		}},
		{"revoked", func(f *deliveryFixture) PlaybackRequest {
			if _, err := f.pool.Exec(f.ctx, `UPDATE entitlements SET state = 'REVOKED', revoked_at = $1 WHERE student_account_id = $2::uuid`, f.now, f.student); err != nil {
				f.t.Fatal(err)
			}
			return PlaybackRequest{StudentID: f.student, LessonID: f.lesson, AssetVersionID: f.video}
		}},
		{"out-of-scope", func(f *deliveryFixture) PlaybackRequest {
			other := uuid.NewString()
			if _, err := f.pool.Exec(f.ctx, `INSERT INTO accounts (id, normalized_email, email, role, status, display_name, locale, email_verified_at) VALUES ($1::uuid, $2, $2, 'STUDENT', 'ACTIVE', 'Other Student', 'en', now())`, other, other+"@example.test"); err != nil {
				f.t.Fatal(err)
			}
			return PlaybackRequest{StudentID: other, LessonID: f.lesson, AssetVersionID: f.video}
		}},
		{"account-suspended", func(f *deliveryFixture) PlaybackRequest {
			if _, err := f.pool.Exec(f.ctx, `UPDATE accounts SET status = 'SUSPENDED' WHERE id = $1::uuid`, f.student); err != nil {
				f.t.Fatal(err)
			}
			return PlaybackRequest{StudentID: f.student, LessonID: f.lesson, AssetVersionID: f.video}
		}},
		{"course-emergency-suspended", func(f *deliveryFixture) PlaybackRequest {
			if _, err := f.pool.Exec(f.ctx, `UPDATE courses SET access_suspended_at = $1, access_suspension_reason = 'fixture' WHERE id = $2::uuid`, f.now, f.courseID); err != nil {
				f.t.Fatal(err)
			}
			return PlaybackRequest{StudentID: f.student, LessonID: f.lesson, AssetVersionID: f.video}
		}},
		{"retired-ineligible", func(f *deliveryFixture) PlaybackRequest {
			if _, err := f.pool.Exec(f.ctx, `UPDATE media_assets SET retired_at = $1 WHERE id = (SELECT logical_asset_id FROM media_asset_versions WHERE id = $2::uuid)`, f.now, f.video); err != nil {
				f.t.Fatal(err)
			}
			if _, err := f.pool.Exec(f.ctx, `UPDATE entitlements SET retirement_eligibility_at = $1 WHERE student_account_id = $2::uuid`, f.now, f.student); err != nil {
				f.t.Fatal(err)
			}
			return PlaybackRequest{StudentID: f.student, LessonID: f.lesson, AssetVersionID: f.video}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newDeliveryFixture(t)
			request := tc.prepare(f)
			if _, err := f.delivery.IssuePlayback(f.ctx, request); err == nil || !errors.Is(err, ErrProtectedUnavailable) {
				t.Fatalf("IssuePlayback error=%v, want uniform %v", err, ErrProtectedUnavailable)
			}
			if len(f.store.requestedKeys()) != 0 {
				t.Fatalf("denied %s issuance reached storage signing", tc.name)
			}
		})
	}
}

func TestD8DownloadsTagOnlyLabMaterialAndNeverExposeStudentPII(t *testing.T) {
	f := newDeliveryFixture(t)
	resource, err := f.delivery.IssueDownload(f.ctx, DownloadRequest{StudentID: f.student, LessonID: f.lesson, AssetVersionID: f.resource, Kind: KindResource})
	if err != nil || resource.BuyerTag != "" {
		t.Fatalf("resource=%+v err=%v; Resources must be untagged", resource, err)
	}
	lab, err := f.delivery.IssueDownload(f.ctx, DownloadRequest{StudentID: f.student, LessonID: f.lesson, AssetVersionID: f.lab, Kind: KindLabMaterial})
	if err != nil || lab.BuyerTag == "" {
		t.Fatalf("lab=%+v err=%v; Lab must be tagged", lab, err)
	}
	if strings.Contains(lab.BuyerTag, f.student) || strings.Contains(lab.BuyerTag, f.studentEmail) {
		t.Fatalf("buyer tag leaked Student PII: %q", lab.BuyerTag)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE entitlements SET state = 'REVOKED', revoked_at = $1 WHERE student_account_id = $2::uuid AND scope_kind = 'COURSE'`, f.now, f.student); err != nil {
		t.Fatal(err)
	}
	f.seedGrant(uuid.NewString(), entitlement.ScopeSection, f.section, f.now.Add(time.Hour))
	second, err := f.delivery.IssueDownload(f.ctx, DownloadRequest{StudentID: f.student, LessonID: f.lesson, AssetVersionID: f.lab, Kind: KindLabMaterial})
	if err != nil || second.BuyerTag == lab.BuyerTag {
		t.Fatalf("per-entitlement Lab tag did not rotate: second=%+v err=%v", second, err)
	}
}

func TestD8PublicPreviewIsExactPublishedReadyAndPrivateOutsideSigning(t *testing.T) {
	f := newDeliveryFixture(t)
	preview, err := f.delivery.IssuePreview(f.ctx, f.preview)
	if err != nil || preview.AssetVersionID != f.preview {
		t.Fatalf("published preview=%+v err=%v", preview, err)
	}
	if _, err := f.delivery.IssuePreview(f.ctx, f.video); err == nil {
		t.Fatal("protected Lesson video was reachable through public preview")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE courses SET lifecycle = 'DRAFT' WHERE id = $1::uuid`, f.courseID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.delivery.IssuePreview(f.ctx, f.preview); err == nil {
		t.Fatal("unpublished preview was issued")
	}
	f.t.Logf("signed keys: %s", fmt.Sprint(f.store.requestedKeys()))
}
