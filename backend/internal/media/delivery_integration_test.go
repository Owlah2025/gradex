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
	mu        sync.Mutex
	keys      []string
	expiresAt []time.Time
	manifest  []byte
}

func (s *signedDeliveryStore) PresignGetURL(_ context.Context, key string, _ time.Duration) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys = append(s.keys, key)
	return "https://storage.test/signed/" + key, nil
}

func (s *signedDeliveryStore) PresignGetURLUntil(_ context.Context, key string, expiresAt time.Time) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys = append(s.keys, key)
	s.expiresAt = append(s.expiresAt, expiresAt)
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
	return []byte("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:6\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXTINF:6,\nsegment000.ts\n#EXT-X-ENDLIST\n"), nil
}

func (s *signedDeliveryStore) requestedKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.keys...)
}

func (s *signedDeliveryStore) requestedExpiries() []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Time(nil), s.expiresAt...)
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
	revision     string
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
	f.revision = revision
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
	f.video = f.readyAsset(KindVideo, "video/hls/720p/playlist.m3u8")
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
	var lessonID, previewOriginRevisionID any = f.lesson, nil
	if kind == KindPreview {
		lessonID = nil
		previewOriginRevisionID = f.revision
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO media_assets (id, kind, owner_account_id, course_id, lesson_id, preview_origin_revision_id, visibility) VALUES ($1::uuid, $2::media_asset_kind, $3::uuid, $4::uuid, $5::uuid, $6::uuid, $7::media_asset_visibility)`, assetID, kind, f.instructorID, f.courseID, lessonID, previewOriginRevisionID, visibilityForKind(kind)); err != nil {
		f.t.Fatal(err)
	}
	contentType := "application/pdf"
	if kind == KindVideo || kind == KindPreview {
		contentType = "video/mp4"
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes) VALUES ($1::uuid, $2::uuid, $3::media_asset_kind, 'QUARANTINED', $4, 'v1', $5, 12)`, versionID, assetID, kind, key, contentType); err != nil {
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
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO video_renditions (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms) VALUES ($1::uuid, '720p', $2, 1280, 720, 2800, 60000)`, versionID, outputKey); err != nil {
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

func (f *deliveryFixture) addVideoRendition(assetVersionID string, rendition Rendition) {
	f.t.Helper()
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO video_renditions (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, 60000)
	`, assetVersionID, rendition.Name, rendition.StorageObjectKey, rendition.Width, rendition.Height, rendition.BitrateKbps); err != nil {
		f.t.Fatal(err)
	}
}

func (f *deliveryFixture) readyPreviewForRevision(revisionID string) string {
	f.t.Helper()
	previous := f.revision
	f.revision = revisionID
	defer func() { f.revision = previous }()
	return f.readyAsset(KindPreview, "preview/"+revisionID+"/source.mp4")
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

func (f *deliveryFixture) studentLearningFacts() [3]int {
	f.t.Helper()
	var facts [3]int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM entitlements WHERE student_account_id = $1::uuid AND course_id = $2::uuid`, f.student, f.courseID).Scan(&facts[0]); err != nil {
		f.t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM enrollments WHERE student_account_id = $1::uuid AND course_id = $2::uuid`, f.student, f.courseID).Scan(&facts[1]); err != nil {
		f.t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM progress p JOIN enrollments e ON e.id = p.enrollment_id WHERE e.student_account_id = $1::uuid AND e.course_id = $2::uuid`, f.student, f.courseID).Scan(&facts[2]); err != nil {
		f.t.Fatal(err)
	}
	return facts
}

func TestD8ProtectedDeliveryUsesExactReadyVersionAndPerRequestEvaluation(t *testing.T) {
	f := newDeliveryFixture(t)
	f.addVideoRendition(f.video, Rendition{
		Name: "240p", StorageObjectKey: "video/hls/240p/playlist.m3u8", Width: 426, Height: 240, BitrateKbps: 400,
	})
	issued, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{StudentID: f.student, LessonID: f.lesson, AssetVersionID: f.video})
	if err != nil || issued.AssetVersionID != f.video || !strings.HasPrefix(issued.ManifestURL, "/api/v1/media/playback-manifests/") || issued.ExpiresAt.Sub(f.now) != 5*time.Minute {
		t.Fatalf("playback issuance=%+v err=%v", issued, err)
	}
	manifest, err := f.delivery.IssuePlaybackManifest(f.ctx, f.student, issued.PlaybackSession)
	master := string(manifest.Contents)
	if err != nil || !strings.Contains(master, "#EXT-X-STREAM-INF:BANDWIDTH=2928000,RESOLUTION=1280x720") ||
		!strings.Contains(master, "#EXT-X-STREAM-INF:BANDWIDTH=496000,RESOLUTION=426x240") {
		t.Fatalf("playback master=%q err=%v", manifest.Contents, err)
	}
	protected720p := "/api/v1/media/playback-manifests/" + issued.PlaybackSession + "/renditions/720p/index.m3u8"
	if !strings.Contains(master, protected720p) || strings.Contains(master, "storage.test") || strings.Contains(master, "video/hls") {
		t.Fatalf("playback master exposed a non-protected rendition target: %q", master)
	}
	variant, err := f.delivery.IssuePlaybackRenditionManifest(f.ctx, f.student, issued.PlaybackSession, "720p")
	if err != nil || !strings.Contains(string(variant.Contents), "https://storage.test/signed/video/hls/720p/segment000.ts") {
		t.Fatalf("playback variant=%q err=%v", variant.Contents, err)
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
	if expiries := f.store.requestedExpiries(); len(expiries) != 1 || expiries[0].After(issued.ExpiresAt) {
		t.Fatalf("segment capability expiries=%v exceed playback expiry %s", expiries, issued.ExpiresAt)
	}
}

func TestAdminReviewDeliveryUsesExactPendingRevisionAndAdminBoundSession(t *testing.T) {
	f := newDeliveryFixture(t)
	admin := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `UPDATE course_revisions SET state = 'PENDING_REVIEW' WHERE course_id = $1::uuid`, f.courseID); err != nil {
		t.Fatalf("making revision reviewable: %v", err)
	}

	issued, err := f.delivery.IssueAdminReviewPlayback(f.ctx, AdminReviewPlaybackRequest{
		AdminAccountID: admin, CourseID: f.courseID, RevisionID: mustRevisionID(t, f), LessonID: f.lesson, AssetVersionID: f.video,
	})
	if err != nil || issued.AssetVersionID != f.video || !strings.HasPrefix(issued.ManifestURL, "/api/v1/admin/review/playback-manifests/") {
		t.Fatalf("admin review playback issuance=%+v err=%v", issued, err)
	}
	manifest, err := f.delivery.IssueAdminReviewPlaybackManifest(f.ctx, admin, issued.PlaybackSession)
	if err != nil || !strings.Contains(string(manifest.Contents), "/renditions/720p/index.m3u8") {
		t.Fatalf("admin review master=%q err=%v", manifest.Contents, err)
	}
	variant, err := f.delivery.IssueAdminReviewPlaybackRenditionManifest(f.ctx, admin, issued.PlaybackSession, "720p")
	if err != nil || !strings.Contains(string(variant.Contents), "https://storage.test/signed/video/hls/720p/segment000.ts") {
		t.Fatalf("admin review variant=%q err=%v", variant.Contents, err)
	}
	if _, err := f.delivery.IssueAdminReviewPlaybackManifest(f.ctx, uuid.NewString(), issued.PlaybackSession); !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("cross-Admin session error=%v, want %v", err, ErrProtectedUnavailable)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE course_revisions SET state = 'APPROVED' WHERE course_id = $1::uuid`, f.courseID); err != nil {
		t.Fatalf("approving revision after issuance: %v", err)
	}
	if _, err := f.delivery.IssueAdminReviewPlaybackManifest(f.ctx, admin, issued.PlaybackSession); !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("approved revision retained review playback: %v", err)
	}
}

func mustRevisionID(t *testing.T, f *deliveryFixture) string {
	t.Helper()
	var revisionID string
	if err := f.pool.QueryRow(f.ctx, `SELECT id::text FROM course_revisions WHERE course_id = $1::uuid`, f.courseID).Scan(&revisionID); err != nil {
		t.Fatalf("finding review revision: %v", err)
	}
	return revisionID
}

func TestNonReadySelectedLessonVideoIsUnavailableToStudentsAndAdminPreview(t *testing.T) {
	f := newDeliveryFixture(t)
	assetID, versionID := uuid.NewString(), uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO media_assets (id, kind, owner_account_id, course_id, lesson_id, visibility)
		VALUES ($1::uuid, 'VIDEO', $2::uuid, $3::uuid, $4::uuid, 'PROTECTED')
	`, assetID, f.instructorID, f.courseID, f.lesson); err != nil {
		t.Fatalf("seeding processing video asset: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO media_asset_versions (
			id, logical_asset_id, kind, state, storage_object_key, storage_object_version,
			content_type, size_bytes
		) VALUES ($1::uuid, $2::uuid, 'VIDEO', 'QUARANTINED', $3, 'fixture-v1', 'video/mp4', 1024)
	`, versionID, assetID, "quarantine/"+f.courseID+"/"+versionID+"/source"); err != nil {
		t.Fatalf("seeding processing video version: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE course_lessons SET video_asset_version_id = $1::uuid
		WHERE course_id = $2::uuid AND lesson_identity_id = $3::uuid
	`, versionID, f.courseID, f.lesson); err != nil {
		t.Fatalf("selecting processing video: %v", err)
	}

	if _, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, LessonID: f.lesson, AssetVersionID: versionID,
	}); !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("student processing playback error = %v, want %v", err, ErrProtectedUnavailable)
	}

	admin := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
		VALUES ($1::uuid, $2, $2, 'ADMIN', 'ACTIVE', 'Review Admin')
	`, admin, admin+"@example.test"); err != nil {
		t.Fatalf("seeding review Admin: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE course_revisions SET state = 'PENDING_REVIEW' WHERE id = $1::uuid`, f.revision); err != nil {
		t.Fatalf("making revision reviewable: %v", err)
	}
	if _, err := f.delivery.IssueAdminReviewPlayback(f.ctx, AdminReviewPlaybackRequest{
		AdminAccountID: admin, CourseID: f.courseID, RevisionID: f.revision, LessonID: f.lesson,
	}); !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("Admin processing preview error = %v, want %v", err, ErrProtectedUnavailable)
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
	if _, err := f.delivery.IssuePlaybackRenditionManifest(f.ctx, uuid.NewString(), issued.PlaybackSession, "720p"); !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("cross-Student variant session error=%v, want %v", err, ErrProtectedUnavailable)
	}
	for _, selector := range []string{"1080p", "../720p", "720p/../../private", "%2e%2e", "720p%2Fprivate"} {
		if _, err := f.delivery.IssuePlaybackRenditionManifest(f.ctx, f.student, issued.PlaybackSession, selector); !errors.Is(err, ErrProtectedUnavailable) {
			t.Fatalf("unsafe rendition selector %q error=%v, want %v", selector, err, ErrProtectedUnavailable)
		}
	}

	for name, manifest := range map[string]string{
		"parent traversal": "#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXTINF:6,\n../other/segment.ts\n#EXT-X-ENDLIST\n",
		"absolute target":  "#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXTINF:6,\nhttps://public.example/segment.ts\n#EXT-X-ENDLIST\n",
		"tag URI":          "#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n#EXTINF:6,\nsegment.ts\n#EXT-X-ENDLIST\n",
		"master playlist":  "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=2928000,RESOLUTION=1280x720\n720p/playlist.m3u8\n",
	} {
		t.Run(name, func(t *testing.T) {
			f.store.setManifest(manifest)
			before := len(f.store.requestedKeys())
			if _, err := f.delivery.IssuePlaybackRenditionManifest(f.ctx, f.student, issued.PlaybackSession, "720p"); !errors.Is(err, ErrProtectedUnavailable) {
				t.Fatalf("unsafe manifest error=%v, want %v", err, ErrProtectedUnavailable)
			}
			if got := len(f.store.requestedKeys()); got != before {
				t.Fatalf("unsafe manifest signed %d storage references", got-before)
			}
		})
	}
	f.now = issued.ExpiresAt
	if _, err := f.delivery.IssuePlaybackManifest(f.ctx, f.student, issued.PlaybackSession); !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("expired master session error=%v, want %v", err, ErrProtectedUnavailable)
	}
	if _, err := f.delivery.IssuePlaybackRenditionManifest(f.ctx, f.student, issued.PlaybackSession, "720p"); !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("expired variant session error=%v, want %v", err, ErrProtectedUnavailable)
	}
}

func TestPlaybackRenditionRechecksEntitlementAfterMaster(t *testing.T) {
	f := newDeliveryFixture(t)
	issued, err := f.delivery.IssuePlayback(f.ctx, PlaybackRequest{
		StudentID: f.student, LessonID: f.lesson, AssetVersionID: f.video,
	})
	if err != nil {
		t.Fatalf("issuing playback: %v", err)
	}
	if _, err := f.delivery.IssuePlaybackManifest(f.ctx, f.student, issued.PlaybackSession); err != nil {
		t.Fatalf("fetching master before revocation: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE entitlements SET access_ends_at = $1 WHERE student_account_id = $2::uuid`, f.now, f.student); err != nil {
		t.Fatal(err)
	}
	if _, err := f.delivery.IssuePlaybackRenditionManifest(f.ctx, f.student, issued.PlaybackSession, "720p"); !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("variant after entitlement revocation error=%v, want %v", err, ErrProtectedUnavailable)
	}
	if len(f.store.requestedKeys()) != 0 {
		t.Fatalf("revoked variant minted storage capabilities: %v", f.store.requestedKeys())
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

func TestST15LessonFileDownloadProjectsEveryLiveAttachmentAndPreservesRevisionIsolation(t *testing.T) {
	f := newDeliveryFixture(t)
	fileID := func(assetVersionID string) string {
		var id string
		if err := f.pool.QueryRow(f.ctx, `
			SELECT lf.id::text
			FROM lesson_files lf
			JOIN course_lessons cl ON cl.id = lf.lesson_id
			JOIN course_sections cs ON cs.id = cl.section_id
			WHERE cs.revision_id = $1::uuid AND lf.asset_version_id = $2::uuid
		`, f.revision, assetVersionID).Scan(&id); err != nil {
			t.Fatalf("finding lesson file for %s: %v", assetVersionID, err)
		}
		return id
	}
	resourceAFile, labAFile := fileID(f.resource), fileID(f.lab)

	// The Student can select each distinct live attachment. A duplicate display
	// name is not an identity: the attachment relationship selects the bytes.
	resourceExtra := f.readyAsset(KindResource, "resource/extra.pdf")
	var liveLessonRow string
	if err := f.pool.QueryRow(f.ctx, `SELECT id::text FROM course_lessons WHERE lesson_identity_id = $1::uuid AND course_id = $2::uuid`, f.lesson, f.courseID).Scan(&liveLessonRow); err != nil {
		t.Fatalf("finding live lesson row: %v", err)
	}
	var resourceExtraFile string
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO lesson_files (lesson_id, kind, asset_version_id, display_name_ar, display_name_en, position)
		VALUES ($1::uuid, 'RESOURCE', $2::uuid, 'مرجع', 'Resource', 1) RETURNING id::text
	`, liveLessonRow, resourceExtra).Scan(&resourceExtraFile); err != nil {
		t.Fatalf("adding second live resource: %v", err)
	}
	for name, request := range map[string]LessonFileDownloadRequest{
		"resource A":              {StudentID: f.student, CourseID: f.courseID, LessonID: f.lesson, FileID: resourceAFile, Locale: "en"},
		"resource duplicate-name": {StudentID: f.student, CourseID: f.courseID, LessonID: f.lesson, FileID: resourceExtraFile, Locale: "ar"},
		"lab":                     {StudentID: f.student, CourseID: f.courseID, LessonID: f.lesson, FileID: labAFile, Locale: "en"},
	} {
		t.Run(name, func(t *testing.T) {
			issued, err := f.delivery.IssueLessonFileDownload(f.ctx, request)
			if err != nil || issued.URL == "" || issued.ExpiresAt.Sub(f.now) != 5*time.Minute {
				t.Fatalf("issuance=%+v err=%v", issued, err)
			}
			if request.FileID == labAFile {
				if issued.BuyerTag == "" || strings.Contains(issued.BuyerTag, f.student) || strings.Contains(issued.BuyerTag, f.studentEmail) {
					t.Fatalf("Lab Material buyer tag=%q, want opaque non-empty tag", issued.BuyerTag)
				}
			} else if issued.BuyerTag != "" {
				t.Fatalf("Resource issuance included a Lab buyer tag: %q", issued.BuyerTag)
			}
		})
	}
	for name, studentID := range map[string]string{
		"anonymous":  "",
		"unentitled": uuid.NewString(),
	} {
		t.Run(name, func(t *testing.T) {
			issued, err := f.delivery.IssueLessonFileDownload(f.ctx, LessonFileDownloadRequest{
				StudentID: studentID, CourseID: f.courseID, LessonID: f.lesson, FileID: labAFile, Locale: "en",
			})
			if !errors.Is(err, ErrProtectedUnavailable) || issued.BuyerTag != "" {
				t.Fatalf("denied Lab authorization=%+v err=%v, want unavailable without buyer tag", issued, err)
			}
		})
	}
	materials, err := f.delivery.MaterialKinds(f.ctx, []string{f.lesson})
	if err != nil || len(materials[f.lesson]) != 3 {
		t.Fatalf("materials=%+v err=%v, want both Resources and Lab Material", materials, err)
	}
	if got := materials[f.lesson][0]; got.DisplayNameEn != "Resource" || got.ContentType != "application/pdf" || got.SizeBytes != 12 || got.FileID == "" {
		t.Fatalf("resource projection=%+v", got)
	}

	// Candidate B carries replacement Resource and Lab attachments, but both
	// fail closed until B becomes the Course's approved live revision.
	candidateB, candidateBLesson := seedST15CandidateRevision(t, f, "DRAFT")
	resourceB := f.readyAsset(KindResource, "resource/b.pdf")
	labB := f.readyAsset(KindLabMaterial, "lab/b.zip")
	var resourceBFile, labBFile string
	for _, attachment := range []struct {
		kind, asset, nameAr, nameEn string
		destination                 *string
	}{
		{string(KindResource), resourceB, "مرجع ب", "Resource B", &resourceBFile},
		{string(KindLabMaterial), labB, "مختبر ب", "Lab B", &labBFile},
	} {
		if err := f.pool.QueryRow(f.ctx, `
			INSERT INTO lesson_files (lesson_id, kind, asset_version_id, display_name_ar, display_name_en, position)
			VALUES ($1::uuid, $2::lesson_file_kind, $3::uuid, $4, $5, 0) RETURNING id::text
		`, candidateBLesson, attachment.kind, attachment.asset, attachment.nameAr, attachment.nameEn).Scan(attachment.destination); err != nil {
			t.Fatalf("adding candidate attachment %s: %v", attachment.kind, err)
		}
	}
	for name, request := range map[string]LessonFileDownloadRequest{
		"candidate resource": {StudentID: f.student, CourseID: f.courseID, LessonID: f.lesson, FileID: resourceBFile, Locale: "en"},
		"candidate lab":      {StudentID: f.student, CourseID: f.courseID, LessonID: f.lesson, FileID: labBFile, Locale: "en"},
		"wrong course":       {StudentID: f.student, CourseID: uuid.NewString(), LessonID: f.lesson, FileID: resourceAFile, Locale: "en"},
		"wrong lesson":       {StudentID: f.student, CourseID: f.courseID, LessonID: uuid.NewString(), FileID: resourceAFile, Locale: "en"},
		"unknown attachment": {StudentID: f.student, CourseID: f.courseID, LessonID: f.lesson, FileID: uuid.NewString(), Locale: "en"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := f.delivery.IssueLessonFileDownload(f.ctx, request); !errors.Is(err, ErrProtectedUnavailable) {
				t.Fatalf("candidate/inventory probe error=%v, want %v", err, ErrProtectedUnavailable)
			}
		})
	}
	if _, err := f.delivery.IssueLessonFileDownload(f.ctx, LessonFileDownloadRequest{StudentID: f.student, CourseID: f.courseID, LessonID: f.lesson, FileID: resourceAFile, Locale: "en"}); err != nil {
		t.Fatalf("live A stopped serving while B was a candidate: %v", err)
	}

	if _, err := f.pool.Exec(f.ctx, `UPDATE course_revisions SET state = 'APPROVED' WHERE id = $1::uuid`, candidateB); err != nil {
		t.Fatalf("approving candidate B: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE courses SET live_revision_id = $1::uuid WHERE id = $2::uuid`, candidateB, f.courseID); err != nil {
		t.Fatalf("activating candidate B: %v", err)
	}
	for name, request := range map[string]LessonFileDownloadRequest{
		"resource B": {StudentID: f.student, CourseID: f.courseID, LessonID: f.lesson, FileID: resourceBFile, Locale: "en"},
		"lab B":      {StudentID: f.student, CourseID: f.courseID, LessonID: f.lesson, FileID: labBFile, Locale: "en"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := f.delivery.IssueLessonFileDownload(f.ctx, request); err != nil {
				t.Fatalf("approved candidate B not deliverable: %v", err)
			}
		})
	}
	if _, err := f.delivery.IssueLessonFileDownload(f.ctx, LessonFileDownloadRequest{StudentID: f.student, CourseID: f.courseID, LessonID: f.lesson, FileID: resourceAFile, Locale: "en"}); !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("superseded resource A error=%v, want %v", err, ErrProtectedUnavailable)
	}

	// Candidate C removes both rows. B remains live until C is approved, then
	// neither superseded attachment is selectable through the current graph.
	candidateC, _ := seedST15CandidateRevision(t, f, "DRAFT")
	if _, err := f.delivery.IssueLessonFileDownload(f.ctx, LessonFileDownloadRequest{StudentID: f.student, CourseID: f.courseID, LessonID: f.lesson, FileID: labBFile, Locale: "en"}); err != nil {
		t.Fatalf("live B stopped serving while C was a candidate: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE course_revisions SET state = 'APPROVED' WHERE id = $1::uuid`, candidateC); err != nil {
		t.Fatalf("approving candidate C: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE courses SET live_revision_id = $1::uuid WHERE id = $2::uuid`, candidateC, f.courseID); err != nil {
		t.Fatalf("activating candidate C: %v", err)
	}
	for _, fileID := range []string{resourceBFile, labBFile} {
		if _, err := f.delivery.IssueLessonFileDownload(f.ctx, LessonFileDownloadRequest{StudentID: f.student, CourseID: f.courseID, LessonID: f.lesson, FileID: fileID, Locale: "en"}); !errors.Is(err, ErrProtectedUnavailable) {
			t.Fatalf("removed approved attachment %s error=%v, want %v", fileID, err, ErrProtectedUnavailable)
		}
	}
}

func seedST15CandidateRevision(t *testing.T, f *deliveryFixture, state string) (revisionID, lessonRowID string) {
	t.Helper()
	revisionID, lessonRowID = uuid.NewString(), uuid.NewString()
	sectionRowID := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO course_revisions (id, course_id, state, revision_number, based_on_revision_id, title_ar, title_en)
		SELECT $1::uuid, course_id, $2, revision_number + 1, id, title_ar, title_en
		FROM course_revisions WHERE id = (SELECT live_revision_id FROM courses WHERE id = $3::uuid)
	`, revisionID, state, f.courseID); err != nil {
		t.Fatalf("creating candidate revision: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'قسم', 'Section', 0)
	`, sectionRowID, revisionID, f.courseID, f.section); err != nil {
		t.Fatalf("creating candidate section: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'درس', 'Lesson', 0)
	`, lessonRowID, sectionRowID, f.courseID, f.section, f.lesson); err != nil {
		t.Fatalf("creating candidate lesson: %v", err)
	}
	return revisionID, lessonRowID
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
		t.Fatalf("issuing pre-expiry master: %v", err)
	}
	if _, err := f.delivery.IssuePlaybackRenditionManifest(f.ctx, f.student, issued.PlaybackSession, "720p"); err != nil {
		t.Fatalf("issuing pre-expiry variant: %v", err)
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
	if _, err := f.delivery.IssuePlaybackRenditionManifest(f.ctx, f.student, issued.PlaybackSession, "720p"); !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("post-expiry rendition error=%v, want %v", err, ErrProtectedUnavailable)
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
	factsBefore := f.studentLearningFacts()
	preview, err := f.delivery.IssuePreview(f.ctx, f.preview)
	if err != nil || preview.AssetVersionID != f.preview {
		t.Fatalf("published preview=%+v err=%v", preview, err)
	}
	if _, err := f.delivery.IssuePreview(f.ctx, f.video); err == nil {
		t.Fatal("protected Lesson video was reachable through public preview")
	}
	if coursePreview, err := f.delivery.IssueCoursePreview(f.ctx, f.courseID); err != nil || coursePreview.AssetVersionID != f.preview {
		t.Fatalf("course-scoped preview=%+v err=%v", coursePreview, err)
	}
	if factsAfter := f.studentLearningFacts(); factsAfter != factsBefore {
		t.Fatalf("public preview mutated entitlement, enrollment, or progress: before=%v after=%v", factsBefore, factsAfter)
	}
	if _, err := f.delivery.IssueCoursePreview(f.ctx, uuid.NewString()); !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("another Course preview error=%v, want %v", err, ErrProtectedUnavailable)
	}

	candidateB := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO course_revisions (id, course_id, based_on_revision_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, $3::uuid, 'DRAFT', 2, 'ب', 'B')`, candidateB, f.courseID, f.revision); err != nil {
		t.Fatal(err)
	}
	previewB := f.readyPreviewForRevision(candidateB)
	if _, err := f.pool.Exec(f.ctx, `UPDATE course_revisions SET preview_asset_version_id = $1::uuid WHERE id = $2::uuid`, previewB, candidateB); err != nil {
		t.Fatal(err)
	}
	if _, err := f.delivery.IssuePreview(f.ctx, previewB); !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("candidate preview error=%v, want %v", err, ErrProtectedUnavailable)
	}
	if coursePreview, err := f.delivery.IssueCoursePreview(f.ctx, f.courseID); err != nil || coursePreview.AssetVersionID != f.preview {
		t.Fatalf("candidate B leaked through course endpoint=%+v err=%v", coursePreview, err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE course_revisions SET state = 'SUPERSEDED' WHERE id = $1::uuid`, f.revision); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE course_revisions SET state = 'APPROVED' WHERE id = $1::uuid`, candidateB); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE courses SET live_revision_id = $1::uuid WHERE id = $2::uuid`, candidateB, f.courseID); err != nil {
		t.Fatal(err)
	}
	if coursePreview, err := f.delivery.IssueCoursePreview(f.ctx, f.courseID); err != nil || coursePreview.AssetVersionID != previewB {
		t.Fatalf("approved B course preview=%+v err=%v", coursePreview, err)
	}
	if _, err := f.delivery.IssuePreview(f.ctx, f.preview); !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("superseded A preview remained authorized: %v", err)
	}

	candidateC := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO course_revisions (id, course_id, based_on_revision_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, $3::uuid, 'DRAFT', 3, 'ج', 'C')`, candidateC, f.courseID, candidateB); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE course_revisions SET state = 'SUPERSEDED' WHERE id = $1::uuid`, candidateB); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE course_revisions SET state = 'APPROVED' WHERE id = $1::uuid`, candidateC); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE courses SET live_revision_id = $1::uuid WHERE id = $2::uuid`, candidateC, f.courseID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.delivery.IssueCoursePreview(f.ctx, f.courseID); !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("approved no-preview revision issued a preview: %v", err)
	}
	if _, err := f.delivery.IssuePreview(f.ctx, previewB); !errors.Is(err, ErrProtectedUnavailable) {
		t.Fatalf("removed preview handle remained authorized: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE courses SET lifecycle = 'DRAFT' WHERE id = $1::uuid`, f.courseID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.delivery.IssuePreview(f.ctx, f.preview); err == nil {
		t.Fatal("unpublished preview was issued")
	}
	f.t.Logf("signed keys: %s", fmt.Sprint(f.store.requestedKeys()))
}

func TestMED01PublicPreviewFollowsCanonicalCourseEligibility(t *testing.T) {
	f := newDeliveryFixture(t)

	assertAllowed := func(label string) {
		t.Helper()
		before := len(f.store.requestedKeys())
		for _, tc := range []struct {
			name  string
			issue func() (PreviewAuthorization, error)
		}{
			{"asset", func() (PreviewAuthorization, error) {
				return f.delivery.IssuePreview(f.ctx, f.preview)
			}},
			{"course", func() (PreviewAuthorization, error) {
				return f.delivery.IssueCoursePreview(f.ctx, f.courseID)
			}},
		} {
			issued, err := tc.issue()
			if err != nil || issued.AssetVersionID != f.preview || issued.URL == "" {
				t.Fatalf("%s %s preview=%+v err=%v; want signed current preview", label, tc.name, issued, err)
			}
			if got := issued.ExpiresAt.Sub(f.now); got != 5*time.Minute {
				t.Fatalf("%s %s preview lifetime=%s, want 5m", label, tc.name, got)
			}
		}
		if got := len(f.store.requestedKeys()); got != before+2 {
			t.Fatalf("%s preview signing calls=%d, want %d", label, got-before, 2)
		}
	}

	assertDenied := func(label string) {
		t.Helper()
		before := len(f.store.requestedKeys())
		for _, tc := range []struct {
			name  string
			issue func() (PreviewAuthorization, error)
		}{
			{"asset", func() (PreviewAuthorization, error) {
				return f.delivery.IssuePreview(f.ctx, f.preview)
			}},
			{"course", func() (PreviewAuthorization, error) {
				return f.delivery.IssueCoursePreview(f.ctx, f.courseID)
			}},
		} {
			issued, err := tc.issue()
			if !errors.Is(err, ErrProtectedUnavailable) {
				t.Fatalf("%s %s preview error=%v, want %v", label, tc.name, err, ErrProtectedUnavailable)
			}
			if issued.URL != "" || !issued.ExpiresAt.IsZero() {
				t.Fatalf("%s %s preview returned authorization on denial: %+v", label, tc.name, issued)
			}
		}
		if got := len(f.store.requestedKeys()); got != before {
			t.Fatalf("%s preview denial reached storage signing: before=%d after=%d", label, before, got)
		}
	}

	assertAllowed("published")
	if _, err := f.pool.Exec(f.ctx, `UPDATE courses SET access_suspended_at = $1, access_suspension_reason = 'MED-01 integration fixture' WHERE id = $2::uuid`, f.now, f.courseID); err != nil {
		t.Fatalf("suspending Course access: %v", err)
	}
	assertDenied("access-suspended")

	if _, err := f.pool.Exec(f.ctx, `UPDATE courses SET access_suspended_at = NULL, access_suspension_reason = NULL WHERE id = $1::uuid`, f.courseID); err != nil {
		t.Fatalf("restoring Course access: %v", err)
	}
	assertAllowed("restored")

	if _, err := f.pool.Exec(f.ctx, `UPDATE courses SET retired_at = $1 WHERE id = $2::uuid`, f.now, f.courseID); err != nil {
		t.Fatalf("retiring Course: %v", err)
	}
	assertDenied("retired")

	if _, err := f.pool.Exec(f.ctx, `UPDATE courses SET lifecycle = 'DELISTED' WHERE id = $1::uuid`, f.courseID); err != nil {
		t.Fatalf("delisting Course: %v", err)
	}
	assertDenied("delisted")

	if _, err := f.pool.Exec(f.ctx, `UPDATE courses SET lifecycle = 'ARCHIVED' WHERE id = $1::uuid`, f.courseID); err != nil {
		t.Fatalf("archiving Course: %v", err)
	}
	assertDenied("archived")
}
