//go:build integration

package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/db"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
	"github.com/Owlah2025/gradex/backend/internal/queue"
)

const (
	mediaAdminDSN = "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable"
	mediaDBName   = "gradex_media_d7_test"
	mediaTestDSN  = "postgres://gradex:gradex@localhost:5432/" + mediaDBName + "?sslmode=disable"
)

type integrationObjectStore struct {
	mu        sync.RWMutex
	objects   map[string][]byte
	hashCalls int
}

func newIntegrationObjectStore() *integrationObjectStore {
	return &integrationObjectStore{objects: make(map[string][]byte)}
}

func (s *integrationObjectStore) PresignPutURL(context.Context, string, string, time.Duration) (string, error) {
	return "https://storage.test/private-quarantine-upload", nil
}

func (s *integrationObjectStore) put(key, version string, bytes []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key+"\x00"+version] = append([]byte(nil), bytes...)
}

func (s *integrationObjectStore) object(key, version string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	bytes, ok := s.objects[key+"\x00"+version]
	return append([]byte(nil), bytes...), ok
}

func (s *integrationObjectStore) HeadObjectVersion(_ context.Context, key, version string) (int64, bool, error) {
	bytes, ok := s.object(key, version)
	if !ok {
		return 0, false, nil
	}
	return int64(len(bytes)), true, nil
}

func (s *integrationObjectStore) DownloadPrefixVersion(_ context.Context, key, version string, maxBytes int64) ([]byte, error) {
	bytes, ok := s.object(key, version)
	if !ok {
		return nil, errors.New("object not found")
	}
	if int64(len(bytes)) > maxBytes {
		bytes = bytes[:maxBytes]
	}
	return bytes, nil
}

func (s *integrationObjectStore) HashObjectVersion(_ context.Context, key, version string) (string, error) {
	s.mu.Lock()
	s.hashCalls++
	s.mu.Unlock()
	bytes, ok := s.object(key, version)
	if !ok {
		return "", errors.New("object not found")
	}
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:]), nil
}

func (s *integrationObjectStore) resetHashCalls() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hashCalls = 0
}

func (s *integrationObjectStore) hashCallCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hashCalls
}

type integrationScannerFunc func(context.Context, ObjectVersion) (ScanObservation, error)

func (f integrationScannerFunc) Scan(ctx context.Context, object ObjectVersion) (ScanObservation, error) {
	return f(ctx, object)
}

type integrationProcessorFunc func(context.Context, ObjectVersion) (TranscodeResult, error)

func (f integrationProcessorFunc) Transcode(ctx context.Context, object ObjectVersion) (TranscodeResult, error) {
	return f(ctx, object)
}

type mediaFixture struct {
	t            *testing.T
	ctx          context.Context
	pool         *pgxpool.Pool
	store        *integrationObjectStore
	writer       *outbox.Writer
	service      *Service
	instructorID string
	adminID      string
	courseID     string
}

func freshMediaDatabase(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, mediaAdminDSN)
	if err != nil {
		t.Fatalf("connecting to PostgreSQL admin database: %v", err)
	}
	defer admin.Close()
	if err := admin.Ping(ctx); err != nil {
		t.Fatalf("pinging PostgreSQL admin database: %v", err)
	}
	_, _ = admin.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1", mediaDBName)
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+mediaDBName); err != nil {
		t.Fatalf("dropping media test database: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+mediaDBName); err != nil {
		t.Fatalf("creating media test database: %v", err)
	}
}

func mediaMigrationSource() string {
	_, file, _, _ := runtime.Caller(0)
	return "file://" + filepath.ToSlash(filepath.Join(filepath.Dir(file), "../db/migrations"))
}

func newMediaFixture(t *testing.T) *mediaFixture {
	t.Helper()
	freshMediaDatabase(t)
	ctx := context.Background()
	m, err := migrate.New(mediaMigrationSource(), mediaTestDSN)
	if err != nil {
		t.Fatalf("opening media migration source: %v", err)
	}
	t.Cleanup(func() { _, _ = m.Close() })
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("applying media migration chain: %v", err)
	}
	pool, err := pgxpool.New(ctx, mediaTestDSN)
	if err != nil {
		t.Fatalf("opening media test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("pinging media test pool: %v", err)
	}

	instructorID := uuid.NewString()
	adminID := uuid.NewString()
	courseID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name, locale, email_verified_at)
		VALUES ($1::uuid, $2, $2, 'INSTRUCTOR', 'ACTIVE', 'D7 Instructor', 'en', now())
	`, instructorID, instructorID+"@example.test"); err != nil {
		t.Fatalf("seeding instructor: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO courses (id, owner_account_id, lifecycle)
		VALUES ($1::uuid, $2::uuid, 'DRAFT')
	`, courseID, instructorID); err != nil {
		t.Fatalf("seeding course: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name, locale, email_verified_at)
		VALUES ($1::uuid, $2, $2, 'ADMIN', 'ACTIVE', 'D7 Admin', 'en', now())
	`, adminID, adminID+"@example.test"); err != nil {
		t.Fatalf("seeding admin: %v", err)
	}
	writer, err := outbox.NewWriter("d7-test-v1", []byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("constructing outbox writer: %v", err)
	}
	passScanner, err := NewScannerAdapter(integrationScannerFunc(func(_ context.Context, object ObjectVersion) (ScanObservation, error) {
		return ScanObservation{AssetVersionID: object.AssetVersionID, StorageObjectVersion: object.StorageObjectVersion, Outcome: ScanPassed, ScannerIdentity: "integration-scanner"}, nil
	}))
	if err != nil {
		t.Fatalf("constructing scanner: %v", err)
	}
	service, err := NewService(ServiceOptions{
		DB: pool, Store: newIntegrationObjectStore(), Outbox: writer, Scanner: passScanner,
		UploadURLExpiry: 15 * time.Minute, MaxUploadBytes: 10 * 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("constructing media service: %v", err)
	}
	return &mediaFixture{t: t, ctx: ctx, pool: pool, store: service.store.(*integrationObjectStore), writer: writer, service: service, instructorID: instructorID, adminID: adminID, courseID: courseID}
}

func (f *mediaFixture) beginUpload(kind AssetKind, eventVersion string) (CompleteUploadRequest, []byte) {
	f.t.Helper()
	contentType, bytes := uploadBytesForKind(kind)
	ticket, err := f.service.BeginUpload(f.ctx, UploadRequest{
		OwnerAccountID: f.instructorID, CourseID: f.courseID, Kind: kind,
		ContentType: contentType, SizeBytes: int64(len(bytes)),
	})
	if err != nil {
		f.t.Fatalf("beginning video upload: %v", err)
	}
	key := fmt.Sprintf("quarantine/%s/%s/source", f.courseID, ticket.AssetVersionID)
	f.store.put(key, eventVersion, bytes)
	sum := sha256.Sum256(bytes)
	return CompleteUploadRequest{
		OwnerAccountID: f.instructorID, AssetVersionID: ticket.AssetVersionID,
		ProviderEventID: "upload-" + ticket.AssetVersionID, StorageObjectKey: key,
		StorageObjectVersion: eventVersion, ContentType: contentType, SizeBytes: int64(len(bytes)),
		SHA256Hex: hex.EncodeToString(sum[:]),
	}, bytes
}

func (f *mediaFixture) beginVideoUpload(eventVersion string) (CompleteUploadRequest, []byte) {
	f.t.Helper()
	return f.beginUpload(KindVideo, eventVersion)
}

func uploadBytesForKind(kind AssetKind) (string, []byte) {
	if kind == KindVideo {
		bytes := append([]byte{0, 0, 0, 24}, []byte("ftypisom")...)
		return "video/mp4", append(bytes, make([]byte, 16)...)
	}
	return "application/pdf", []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
}

func mediaState(t *testing.T, pool *pgxpool.Pool, versionID string) AssetVersionState {
	t.Helper()
	var state AssetVersionState
	if err := pool.QueryRow(context.Background(), `SELECT state FROM media_asset_versions WHERE id = $1::uuid`, versionID).Scan(&state); err != nil {
		t.Fatalf("reading media state: %v", err)
	}
	return state
}

func TestPublicPreviewUploadIsSeparateAndBoundToItsEditableCourseRevision(t *testing.T) {
	f := newMediaFixture(t)
	revisionID := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, 'DRAFT', 1, 'معاينة', 'Preview')`, revisionID, f.courseID); err != nil {
		t.Fatal(err)
	}

	ticket, err := f.service.BeginUpload(f.ctx, UploadRequest{
		OwnerAccountID: f.instructorID, CourseID: f.courseID, RevisionID: revisionID,
		Kind: KindPreview, ContentType: "video/mp4", SizeBytes: 1024,
	})
	if err != nil {
		t.Fatalf("beginning revision-scoped preview upload: %v", err)
	}
	var kind, visibility string
	var lessonID *string
	var originRevisionID string
	err = f.pool.QueryRow(f.ctx, `
		SELECT ma.kind::text, ma.visibility::text, ma.lesson_id::text, ma.preview_origin_revision_id::text
		FROM media_asset_versions mav
		JOIN media_assets ma ON ma.id = mav.logical_asset_id
		WHERE mav.id = $1::uuid
	`, ticket.AssetVersionID).Scan(&kind, &visibility, &lessonID, &originRevisionID)
	if err != nil || kind != string(KindPreview) || visibility != "PUBLIC_PREVIEW" || lessonID != nil || originRevisionID != revisionID {
		t.Fatalf("stored preview binding kind=%q visibility=%q lesson=%v origin=%q err=%v", kind, visibility, lessonID, originRevisionID, err)
	}

	for name, request := range map[string]UploadRequest{
		"missing revision":  {OwnerAccountID: f.instructorID, CourseID: f.courseID, Kind: KindPreview, ContentType: "video/mp4", SizeBytes: 1024},
		"lesson relation":   {OwnerAccountID: f.instructorID, CourseID: f.courseID, RevisionID: revisionID, LessonID: uuid.NewString(), Kind: KindPreview, ContentType: "video/mp4", SizeBytes: 1024},
		"non-video preview": {OwnerAccountID: f.instructorID, CourseID: f.courseID, RevisionID: revisionID, Kind: KindPreview, ContentType: "application/pdf", SizeBytes: 1024},
		"wrong owner":       {OwnerAccountID: f.adminID, CourseID: f.courseID, RevisionID: revisionID, Kind: KindPreview, ContentType: "video/mp4", SizeBytes: 1024},
		"other revision":    {OwnerAccountID: f.instructorID, CourseID: f.courseID, RevisionID: uuid.NewString(), Kind: KindPreview, ContentType: "video/mp4", SizeBytes: 1024},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := f.service.BeginUpload(f.ctx, request); err == nil {
				t.Fatal("preview upload unexpectedly received an upload intent")
			}
		})
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE course_revisions SET state = 'PENDING_REVIEW' WHERE id = $1::uuid`, revisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.BeginUpload(f.ctx, UploadRequest{OwnerAccountID: f.instructorID, CourseID: f.courseID, RevisionID: revisionID, Kind: KindPreview, ContentType: "video/mp4", SizeBytes: 1024}); !errors.Is(err, ErrConflict) {
		t.Fatalf("pending-review preview upload error=%v, want %v", err, ErrConflict)
	}
}

func TestBeginLessonVideoUploadDoesNotClaimLessonAndOrdersReplacementIntents(t *testing.T) {
	f := newMediaFixture(t)
	revisionID, sectionIdentityID, sectionRowID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	lessonIdentityID, lessonRowID, existingVideoID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	seedStatements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en)
		  VALUES ($1::uuid, $2::uuid, 'DRAFT', 1, 'مقرر', 'Course')`, []any{revisionID, f.courseID}},
		{`INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)`, []any{sectionIdentityID, f.courseID}},
		{`INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position)
		  VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'قسم', 'Section', 0)`, []any{sectionRowID, revisionID, f.courseID, sectionIdentityID}},
		{`INSERT INTO course_lesson_identities (id, course_id, section_identity_id)
		  VALUES ($1::uuid, $2::uuid, $3::uuid)`, []any{lessonIdentityID, f.courseID, sectionIdentityID}},
		{`INSERT INTO course_lessons (
			id, section_id, course_id, section_identity_id, lesson_identity_id,
			title_ar, title_en, position, video_asset_version_id
		  ) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'درس', 'Lesson', 0, $6::uuid)`, []any{lessonRowID, sectionRowID, f.courseID, sectionIdentityID, lessonIdentityID, existingVideoID}},
	}
	for _, statement := range seedStatements {
		if _, err := f.pool.Exec(f.ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seeding Lesson video target: %v", err)
		}
	}

	begin := func() UploadTicket {
		ticket, err := f.service.BeginUpload(f.ctx, UploadRequest{
			OwnerAccountID: f.instructorID, CourseID: f.courseID, LessonID: lessonIdentityID,
			Kind: KindVideo, ContentType: "video/mp4", SizeBytes: 1024,
		})
		if err != nil {
			t.Fatalf("BeginUpload: %v", err)
		}
		return ticket
	}
	first, second := begin(), begin()

	var selected string
	if err := f.pool.QueryRow(f.ctx, `SELECT video_asset_version_id::text FROM course_lessons WHERE id = $1::uuid`, lessonRowID).Scan(&selected); err != nil {
		t.Fatalf("reading selected Lesson video: %v", err)
	}
	if selected != existingVideoID {
		t.Fatalf("BeginUpload selected %s, want existing %s unchanged", selected, existingVideoID)
	}

	var firstCreatedAt, secondCreatedAt time.Time
	if err := f.pool.QueryRow(f.ctx, `SELECT created_at FROM upload_intents WHERE asset_version_id = $1::uuid`, first.AssetVersionID).Scan(&firstCreatedAt); err != nil {
		t.Fatalf("reading first intent order: %v", err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT created_at FROM upload_intents WHERE asset_version_id = $1::uuid`, second.AssetVersionID).Scan(&secondCreatedAt); err != nil {
		t.Fatalf("reading second intent order: %v", err)
	}
	if !secondCreatedAt.After(firstCreatedAt) {
		t.Fatalf("intent order first=%s second=%s, want strict creation order", firstCreatedAt, secondCreatedAt)
	}
}

func scanWorkID(t *testing.T, pool *pgxpool.Pool, versionID string, offset int) string {
	t.Helper()
	var workID string
	if err := pool.QueryRow(context.Background(), `
		SELECT id::text
		FROM outbox_events
		WHERE event_type = 'media.scan_requested' AND aggregate_id = $1::uuid
		ORDER BY occurred_at, id
		OFFSET $2 LIMIT 1
	`, versionID, offset).Scan(&workID); err != nil {
		t.Fatalf("loading scan work ID %d: %v", offset, err)
	}
	return workID
}

func TestD7PipelineReachesReadyWithTrustedVersionEvidence(t *testing.T) {
	f := newMediaFixture(t)
	request, _ := f.beginVideoUpload("object-v1")
	completed, err := f.service.CompleteUpload(f.ctx, request)
	if err != nil || completed.State != StateQuarantined {
		t.Fatalf("completion = %+v, err=%v; want QUARANTINED", completed, err)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateQuarantined {
		t.Fatalf("state after completion = %q, want QUARANTINED", got)
	}
	var scanOutbox int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM outbox_events WHERE source_module = 'MEDIA_AND_ASSETS' AND event_type = 'media.scan_requested'`).Scan(&scanOutbox); err != nil {
		t.Fatalf("checking scan outbox intent: %v", err)
	}
	if scanOutbox != 1 {
		t.Fatalf("scan outbox intent count = %d, want 1", scanOutbox)
	}

	processor := integrationProcessorFunc(func(_ context.Context, object ObjectVersion) (TranscodeResult, error) {
		return TranscodeResult{
			TrustedDurationMS: 123456,
			OutputPrefix:      "media/" + object.AssetVersionID + "/hls",
			Renditions:        []Rendition{{Name: "720p", StorageObjectKey: "media/" + object.AssetVersionID + "/hls/720p/playlist.m3u8", Width: 1280, Height: 720, BitrateKbps: 2800, DurationMS: 123456}},
		}, nil
	})
	scanner, err := NewScannerAdapter(integrationScannerFunc(func(_ context.Context, object ObjectVersion) (ScanObservation, error) {
		return ScanObservation{AssetVersionID: object.AssetVersionID, StorageObjectVersion: object.StorageObjectVersion, Outcome: ScanPassed, ScannerIdentity: "integration-scanner"}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewWorker(WorkerOptions{DB: f.pool, Scanner: scanner, Process: processor, Outbox: f.writer})
	if err != nil {
		t.Fatalf("constructing worker: %v", err)
	}
	if err := worker.Scan(f.ctx, request.AssetVersionID); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateScanPassed {
		t.Fatalf("state after scan = %q, want SCAN_PASSED", got)
	}
	var operationID string
	if err := f.pool.QueryRow(f.ctx, `SELECT safe_payload->>'operation_id' FROM outbox_events WHERE event_type = 'media.transcode_requested' AND aggregate_id = $1::uuid`, request.AssetVersionID).Scan(&operationID); err != nil {
		t.Fatalf("reading transcode operation ID: %v", err)
	}
	duplicateResult := TranscodeResult{
		TrustedDurationMS: 123456,
		OutputPrefix:      "media/" + request.AssetVersionID + "/hls",
		Renditions:        []Rendition{{Name: "720p", StorageObjectKey: "media/" + request.AssetVersionID + "/hls/720p/playlist.m3u8", Width: 1280, Height: 720, BitrateKbps: 2800, DurationMS: 123456}},
	}
	if err := worker.CompleteTranscode(f.ctx, request.AssetVersionID, operationID, duplicateResult); err == nil {
		t.Fatal("out-of-order transcode callback succeeded before PROCESSING")
	}
	if err := worker.Transcode(f.ctx, request.AssetVersionID, operationID); err != nil {
		t.Fatalf("transcode: %v", err)
	}
	if err := worker.CompleteTranscode(f.ctx, request.AssetVersionID, operationID, duplicateResult); err != nil {
		t.Fatalf("duplicate transcode callback: %v", err)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateReady {
		t.Fatalf("state after transcode = %q, want READY", got)
	}
	var duration *int64
	var renditions int
	if err := f.pool.QueryRow(f.ctx, `SELECT trusted_duration_ms FROM media_asset_versions WHERE id = $1::uuid`, request.AssetVersionID).Scan(&duration); err != nil {
		t.Fatalf("reading trusted duration: %v", err)
	}
	if duration == nil || *duration != 123456 {
		t.Fatalf("trusted duration = %v, want 123456", duration)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM video_renditions WHERE asset_version_id = $1::uuid`, request.AssetVersionID).Scan(&renditions); err != nil {
		t.Fatalf("counting renditions: %v", err)
	}
	if renditions != 1 {
		t.Fatalf("rendition count = %d, want 1", renditions)
	}
	var processingAttempts, versionCount, logicalVersionCount int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM processing_attempts WHERE asset_version_id = $1::uuid`, request.AssetVersionID).Scan(&processingAttempts); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM media_asset_versions WHERE id = $1::uuid`, request.AssetVersionID).Scan(&versionCount); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `
		SELECT count(*) FROM media_asset_versions
		WHERE logical_asset_id = (SELECT logical_asset_id FROM media_asset_versions WHERE id = $1::uuid)
	`, request.AssetVersionID).Scan(&logicalVersionCount); err != nil {
		t.Fatal(err)
	}
	if processingAttempts != 1 || versionCount != 1 || logicalVersionCount != 1 {
		t.Fatalf("duplicate transcode changed processing_attempts=%d versions=%d logical_versions=%d; want 1 each", processingAttempts, versionCount, logicalVersionCount)
	}
	status, err := f.service.GetStatus(f.ctx, request.AssetVersionID, Viewer{AccountID: f.instructorID, Role: "INSTRUCTOR"})
	if err != nil || !status.Deliverable() {
		t.Fatalf("status = %+v, err=%v; want deliverable READY status", status, err)
	}
	if status.TrustedDurationMS == nil || *status.TrustedDurationMS != 123456 {
		t.Fatalf("status trusted duration = %v, want 123456 from processing evidence", status.TrustedDurationMS)
	}
}

func TestD8AdminCatalogueModeIsExactVersionAuditedAndFailClosed(t *testing.T) {
	f := newMediaFixture(t)
	catalogue, err := NewService(ServiceOptions{
		DB: f.pool, Store: f.store, Outbox: f.writer, Scanner: f.service.scanner,
		UploadURLExpiry: 15 * time.Minute, MaxUploadBytes: 10 * 1024 * 1024,
		OperatingMode: OperatingModeAdminCatalogue,
	})
	if err != nil {
		t.Fatal(err)
	}
	contentType, bytes := uploadBytesForKind(KindResource)
	if _, err := catalogue.BeginUpload(f.ctx, UploadRequest{
		OwnerAccountID: f.instructorID, CourseID: f.courseID, Kind: KindResource, ContentType: contentType, SizeBytes: int64(len(bytes)),
	}); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("Instructor upload in Admin catalogue mode error=%v, want %v", err, ErrNotAuthorized)
	}
	if _, err := catalogue.BeginCatalogueLoad(f.ctx, CatalogueLoadRequest{
		AdminAccountID: f.instructorID, CourseID: f.courseID, Kind: KindResource, ContentType: contentType, SizeBytes: int64(len(bytes)),
	}); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("non-Admin catalogue load error=%v, want %v", err, ErrNotAuthorized)
	}
	ticket, err := catalogue.BeginCatalogueLoad(f.ctx, CatalogueLoadRequest{
		AdminAccountID: f.adminID, CourseID: f.courseID, Kind: KindResource, ContentType: contentType, SizeBytes: int64(len(bytes)),
	})
	if err != nil {
		t.Fatalf("beginning Admin catalogue load: %v", err)
	}
	key := fmt.Sprintf("quarantine/%s/%s/source", f.courseID, ticket.AssetVersionID)
	f.store.put(key, "catalogue-v1", bytes)
	sum := sha256.Sum256(bytes)
	completed, err := catalogue.CompleteCatalogueLoad(f.ctx, CatalogueCompletionRequest{
		AdminAccountID: f.adminID, AssetVersionID: ticket.AssetVersionID, ProviderEventID: "catalogue-" + ticket.AssetVersionID,
		StorageObjectKey: key, StorageObjectVersion: "catalogue-v1", ContentType: contentType, SizeBytes: int64(len(bytes)), SHA256Hex: hex.EncodeToString(sum[:]),
	})
	if err != nil || completed.State != StateQuarantined {
		t.Fatalf("catalogue completion=%+v err=%v, want quarantine", completed, err)
	}
	duplicate, err := catalogue.CompleteCatalogueLoad(f.ctx, CatalogueCompletionRequest{
		AdminAccountID: f.adminID, AssetVersionID: ticket.AssetVersionID, ProviderEventID: "catalogue-" + ticket.AssetVersionID,
		StorageObjectKey: key, StorageObjectVersion: "catalogue-v1", ContentType: contentType, SizeBytes: int64(len(bytes)), SHA256Hex: hex.EncodeToString(sum[:]),
	})
	if err != nil || !duplicate.Duplicate || duplicate.State != StateQuarantined {
		t.Fatalf("duplicate catalogue completion=%+v err=%v", duplicate, err)
	}
	if err := catalogue.RecordOutOfBandScanEvidence(f.ctx, OutOfBandScanEvidence{
		AdminAccountID: f.adminID, AssetVersionID: ticket.AssetVersionID, StorageObjectVersion: "another-version", Method: "manual", Provider: "vendor", Reference: "scan-001",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("cross-version evidence error=%v, want %v", err, ErrConflict)
	}
	if err := catalogue.RecordOutOfBandScanEvidence(f.ctx, OutOfBandScanEvidence{
		AdminAccountID: f.adminID, AssetVersionID: ticket.AssetVersionID, StorageObjectVersion: "catalogue-v1", Method: "manual", Provider: "vendor", Reference: "scan-001",
	}); err != nil {
		t.Fatalf("recording exact out-of-band evidence: %v", err)
	}
	if got := mediaState(t, f.pool, ticket.AssetVersionID); got != StateReady {
		t.Fatalf("catalogue asset state=%s, want READY only after exact evidence", got)
	}
	var attempts, auditRows, scanEvents, entitlements int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM scan_attempts WHERE asset_version_id = $1::uuid AND storage_object_version = 'catalogue-v1' AND scanner_identity = 'out-of-band:vendor'`, ticket.AssetVersionID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE action = 'MEDIA_OUT_OF_BAND_SCAN_RECORDED' AND target_id = $1`, ticket.AssetVersionID).Scan(&auditRows); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM outbox_events WHERE event_type = 'media.scan_requested' AND aggregate_id = $1::uuid`, ticket.AssetVersionID).Scan(&scanEvents); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM entitlements`).Scan(&entitlements); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || auditRows != 1 || scanEvents != 0 || entitlements != 0 {
		t.Fatalf("catalogue evidence attempts=%d audits=%d automatic scans=%d entitlements=%d", attempts, auditRows, scanEvents, entitlements)
	}
}

func TestD7ReplacementCreatesAnIndependentImmutableVersion(t *testing.T) {
	f := newMediaFixture(t)
	first, firstBytes := f.beginVideoUpload("object-replacement-v1")
	if _, err := f.service.CompleteUpload(f.ctx, first); err != nil {
		t.Fatalf("first completion: %v", err)
	}
	var logicalAssetID string
	if err := f.pool.QueryRow(f.ctx, `SELECT logical_asset_id::text FROM media_asset_versions WHERE id = $1::uuid`, first.AssetVersionID).Scan(&logicalAssetID); err != nil {
		t.Fatalf("loading logical asset: %v", err)
	}

	ticket, err := f.service.BeginUpload(f.ctx, UploadRequest{
		OwnerAccountID: f.instructorID, CourseID: f.courseID, LogicalAssetID: logicalAssetID,
		Kind: KindVideo, ContentType: "video/mp4", SizeBytes: int64(len(firstBytes)),
	})
	if err != nil {
		t.Fatalf("beginning replacement upload: %v", err)
	}
	replacementKey := fmt.Sprintf("quarantine/%s/%s/source", f.courseID, ticket.AssetVersionID)
	replacementBytes := append([]byte(nil), firstBytes...)
	replacementBytes[len(replacementBytes)-1] = 0x01
	f.store.put(replacementKey, "object-replacement-v2", replacementBytes)
	replacementHash := sha256.Sum256(replacementBytes)
	second := CompleteUploadRequest{
		OwnerAccountID: f.instructorID, AssetVersionID: ticket.AssetVersionID,
		ProviderEventID: "upload-" + ticket.AssetVersionID, StorageObjectKey: replacementKey,
		StorageObjectVersion: "object-replacement-v2", ContentType: "video/mp4", SizeBytes: int64(len(replacementBytes)),
		SHA256Hex: hex.EncodeToString(replacementHash[:]),
	}
	if _, err := f.service.CompleteUpload(f.ctx, second); err != nil {
		t.Fatalf("replacement completion: %v", err)
	}

	var versionCount, distinctObjects int
	if err := f.pool.QueryRow(f.ctx, `
		SELECT count(*), count(DISTINCT storage_object_key || ':' || storage_object_version)
		FROM media_asset_versions WHERE logical_asset_id = $1::uuid
	`, logicalAssetID).Scan(&versionCount, &distinctObjects); err != nil {
		t.Fatalf("checking replacement versions: %v", err)
	}
	if versionCount != 2 || distinctObjects != 2 {
		t.Fatalf("replacement versions=%d distinct objects=%d; want two immutable versions", versionCount, distinctObjects)
	}
}

func TestD7CallbacksConvergeUnderReplayAndConcurrency(t *testing.T) {
	f := newMediaFixture(t)
	request, _ := f.beginVideoUpload("object-concurrent")
	results := make(chan error, 16)
	var wait sync.WaitGroup
	for i := 0; i < 16; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := f.service.CompleteUpload(f.ctx, request)
			results <- err
		}()
	}
	wait.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent duplicate completion failed: %v", err)
		}
	}
	var versions, receipts, intents int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM media_asset_versions WHERE id = $1::uuid`, request.AssetVersionID).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM media_callback_receipts WHERE callback_kind = 'UPLOAD_COMPLETED' AND provider_event_id = $1`, request.ProviderEventID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM upload_intents WHERE asset_version_id = $1::uuid`, request.AssetVersionID).Scan(&intents); err != nil {
		t.Fatal(err)
	}
	if versions != 1 || receipts != 1 || intents != 1 {
		t.Fatalf("duplicate convergence versions=%d receipts=%d intents=%d; want 1 each", versions, receipts, intents)
	}
	replay, err := f.service.CompleteUpload(f.ctx, request)
	if err != nil || !replay.Duplicate {
		t.Fatalf("successful replay = %+v, err=%v; want duplicate success", replay, err)
	}

	delayed, _ := f.beginVideoUpload("object-delayed")
	key := delayed.StorageObjectKey
	f.store.mu.Lock()
	delete(f.store.objects, key+"\x00object-delayed")
	f.store.mu.Unlock()
	if _, err := f.service.CompleteUpload(f.ctx, delayed); err == nil {
		t.Fatal("completion before provider object arrival succeeded")
	}
	bytes := append([]byte{0, 0, 0, 24}, []byte("ftypisom")...)
	bytes = append(bytes, make([]byte, 16)...)
	f.store.put(key, "object-delayed", bytes)
	if _, err := f.service.CompleteUpload(f.ctx, delayed); err != nil {
		t.Fatalf("delayed completion after object arrival: %v", err)
	}
	wrong := delayed
	wrong.ProviderEventID = "wrong-object-event"
	wrong.StorageObjectVersion = "stale-object-version"
	if _, err := f.service.CompleteUpload(f.ctx, wrong); err == nil {
		t.Fatal("out-of-order stale object callback succeeded")
	}
}

func TestD7CompletionAuthorizesBeforeFullObjectHash(t *testing.T) {
	f := newMediaFixture(t)
	request, _ := f.beginVideoUpload("object-authorization-order")

	unauthorized := request
	unauthorized.OwnerAccountID = uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name, locale, email_verified_at)
		VALUES ($1::uuid, $2, $2, 'INSTRUCTOR', 'ACTIVE', 'Other Instructor', 'en', now())
	`, unauthorized.OwnerAccountID, unauthorized.OwnerAccountID+"@example.test"); err != nil {
		t.Fatalf("seeding unauthorized instructor: %v", err)
	}
	if _, err := f.service.CompleteUpload(f.ctx, unauthorized); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("unauthorized completion error = %v, want ErrNotAuthorized", err)
	}
	if calls := f.store.hashCallCount(); calls != 0 {
		t.Fatalf("unauthorized completion performed %d full object hashes, want 0", calls)
	}

	if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
		t.Fatalf("valid completion: %v", err)
	}
	f.store.resetHashCalls()
	invalidState := request
	invalidState.ProviderEventID = request.ProviderEventID + "-new"
	if _, err := f.service.CompleteUpload(f.ctx, invalidState); !errors.Is(err, ErrConflict) {
		t.Fatalf("completed-version callback error = %v, want ErrConflict", err)
	}
	if calls := f.store.hashCallCount(); calls != 0 {
		t.Fatalf("invalid-state completion performed %d full object hashes, want 0", calls)
	}
}

// Regression F-01: a retry must add immutable evidence instead of conflicting
// with the failed scan attempt for the same exact object version.
func TestD7AdminRetryPreservesExactVersionScanHistoryAndConverges(t *testing.T) {
	f := newMediaFixture(t)
	request, _ := f.beginVideoUpload("object-retry-error-pass")
	if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
		t.Fatalf("completion: %v", err)
	}

	errorScanner := mustScanner(t, integrationScannerFunc(func(context.Context, ObjectVersion) (ScanObservation, error) {
		return ScanObservation{}, errors.New("scanner transport failure")
	}))
	worker, err := NewWorker(WorkerOptions{
		DB: f.pool, Scanner: errorScanner,
		Process: integrationProcessorFunc(func(context.Context, ObjectVersion) (TranscodeResult, error) {
			return TranscodeResult{}, errors.New("processor must not run before scan passes")
		}),
		Outbox: f.writer,
	})
	if err != nil {
		t.Fatalf("constructing error scanner worker: %v", err)
	}
	firstWorkID := scanWorkID(t, f.pool, request.AssetVersionID, 0)
	if err := worker.scan(f.ctx, request.AssetVersionID, firstWorkID); err == nil {
		t.Fatal("scanner error unexpectedly returned success")
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateScanError {
		t.Fatalf("state after first scan = %q, want SCAN_ERROR", got)
	}

	var firstAttemptID, firstOutcome, firstReason string
	var firstAttemptNumber int
	if err := f.pool.QueryRow(f.ctx, `
		SELECT id::text, attempt_number, outcome, COALESCE(reason, '')
		FROM scan_attempts WHERE work_id = $1
	`, firstWorkID).Scan(&firstAttemptID, &firstAttemptNumber, &firstOutcome, &firstReason); err != nil {
		t.Fatalf("loading first scan attempt: %v", err)
	}
	if firstAttemptNumber != 1 || firstOutcome != string(ScanError) || firstReason == "" {
		t.Fatalf("first attempt = number=%d outcome=%s reason=%q; want 1/ERROR/non-empty", firstAttemptNumber, firstOutcome, firstReason)
	}

	if err := f.service.Retry(f.ctx, RetryRequest{AssetVersionID: request.AssetVersionID, AdminAccountID: f.adminID, ActorDescriptor: "d7-admin"}); err != nil {
		t.Fatalf("admin retry: %v", err)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateQuarantined {
		t.Fatalf("state after retry = %q, want QUARANTINED", got)
	}

	passScanner := mustScanner(t, integrationScannerFunc(func(_ context.Context, object ObjectVersion) (ScanObservation, error) {
		return ScanObservation{AssetVersionID: object.AssetVersionID, StorageObjectVersion: object.StorageObjectVersion, Outcome: ScanPassed, ScannerIdentity: "passing-retry-scanner"}, nil
	}))
	worker, err = NewWorker(WorkerOptions{
		DB: f.pool, Scanner: passScanner,
		Process: integrationProcessorFunc(func(context.Context, ObjectVersion) (TranscodeResult, error) {
			return TranscodeResult{}, errors.New("processing is dispatched separately")
		}),
		Outbox: f.writer,
	})
	if err != nil {
		t.Fatalf("constructing passing scanner worker: %v", err)
	}
	secondWorkID := scanWorkID(t, f.pool, request.AssetVersionID, 1)
	if secondWorkID == firstWorkID {
		t.Fatal("admin retry reused its prior scan work identity")
	}
	if err := worker.scan(f.ctx, request.AssetVersionID, secondWorkID); err != nil {
		t.Fatalf("passing retry scan: %v", err)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateScanPassed {
		t.Fatalf("state after passing retry = %q, want SCAN_PASSED", got)
	}

	var attemptCount int
	var secondAttemptID, secondOutcome, persistedFirstReason string
	var secondAttemptNumber int
	if err := f.pool.QueryRow(f.ctx, `
		SELECT count(*) FROM scan_attempts WHERE asset_version_id = $1::uuid
	`, request.AssetVersionID).Scan(&attemptCount); err != nil {
		t.Fatalf("counting scan attempts: %v", err)
	}
	if err := f.pool.QueryRow(f.ctx, `
		SELECT id::text, attempt_number, outcome
		FROM scan_attempts WHERE work_id = $1
	`, secondWorkID).Scan(&secondAttemptID, &secondAttemptNumber, &secondOutcome); err != nil {
		t.Fatalf("loading second scan attempt: %v", err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT COALESCE(reason, '') FROM scan_attempts WHERE id = $1::uuid`, firstAttemptID).Scan(&persistedFirstReason); err != nil {
		t.Fatalf("reloading immutable first scan evidence: %v", err)
	}
	if attemptCount != 2 || secondAttemptNumber != 2 || secondOutcome != string(ScanPassed) || persistedFirstReason != firstReason {
		t.Fatalf("attempt history = count=%d second=%s/%d first_reason=%q; want 2/PASSED/2/%q", attemptCount, secondOutcome, secondAttemptNumber, persistedFirstReason, firstReason)
	}
	if secondAttemptID == firstAttemptID {
		t.Fatal("retry reused the prior immutable scan attempt")
	}
	var successfulAttempt string
	if err := f.pool.QueryRow(f.ctx, `SELECT successful_scan_attempt_id::text FROM media_asset_versions WHERE id = $1::uuid`, request.AssetVersionID).Scan(&successfulAttempt); err != nil {
		t.Fatalf("loading authoritative successful scan reference: %v", err)
	}
	if successfulAttempt != secondAttemptID {
		t.Fatalf("successful scan reference = %q, want retry attempt %q", successfulAttempt, secondAttemptID)
	}
	var transcodeEvents int
	if err := f.pool.QueryRow(f.ctx, `
		SELECT count(*) FROM outbox_events
		WHERE event_type = 'media.transcode_requested' AND aggregate_id = $1::uuid
	`, request.AssetVersionID).Scan(&transcodeEvents); err != nil {
		t.Fatalf("counting transcode events: %v", err)
	}
	if transcodeEvents != 1 {
		t.Fatalf("transcode events after successful retry = %d, want 1", transcodeEvents)
	}
	if err := worker.scan(f.ctx, request.AssetVersionID, secondWorkID); err != nil {
		t.Fatalf("replaying second scan work: %v", err)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got == StateScanning {
		t.Fatal("replayed successful scan left the asset in SCANNING")
	}

	var storageKey, storageVersion string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT storage_object_key, storage_object_version
		FROM media_asset_versions WHERE id = $1::uuid
	`, request.AssetVersionID).Scan(&storageKey, &storageVersion); err != nil {
		t.Fatalf("loading exact object identity for conflict test: %v", err)
	}
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatalf("beginning same-work conflict transaction: %v", err)
	}
	defer tx.Rollback(f.ctx)
	_, err = recordScanEvidence(f.ctx, tx, versionRecord{
		ID:     request.AssetVersionID,
		Object: ObjectVersion{AssetVersionID: request.AssetVersionID, StorageObjectKey: storageKey, StorageObjectVersion: storageVersion},
	}, secondAttemptNumber, secondWorkID, ScanObservation{
		AssetVersionID: request.AssetVersionID, StorageObjectVersion: storageVersion,
		Outcome: ScanFailed, ScannerIdentity: "conflicting-replay", Reason: ErrMalwareDetected.Error(),
	}, StateScanFailed)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("different result for the same scan work error = %v, want ErrConflict", err)
	}
}

func TestD7ConcurrentAdminRetryCreatesOneNewScanWork(t *testing.T) {
	f := newMediaFixture(t)
	request, _ := f.beginVideoUpload("object-concurrent-retry")
	if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
		t.Fatal(err)
	}
	errorScanner := mustScanner(t, integrationScannerFunc(func(context.Context, ObjectVersion) (ScanObservation, error) {
		return ScanObservation{}, errors.New("scanner unavailable")
	}))
	worker, err := NewWorker(WorkerOptions{DB: f.pool, Scanner: errorScanner, Process: integrationProcessorFunc(func(context.Context, ObjectVersion) (TranscodeResult, error) {
		return TranscodeResult{}, errors.New("not reached")
	}), Outbox: f.writer})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.scan(f.ctx, request.AssetVersionID, scanWorkID(t, f.pool, request.AssetVersionID, 0)); err == nil {
		t.Fatal("scanner error unexpectedly returned success")
	}

	results := make(chan error, 8)
	var wait sync.WaitGroup
	for i := 0; i < cap(results); i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			results <- f.service.Retry(f.ctx, RetryRequest{AssetVersionID: request.AssetVersionID, AdminAccountID: f.adminID, ActorDescriptor: "concurrent-admin"})
		}()
	}
	wait.Wait()
	close(results)
	succeeded, conflicted := 0, 0
	for retryErr := range results {
		switch {
		case retryErr == nil:
			succeeded++
		case errors.Is(retryErr, ErrConflict):
			conflicted++
		default:
			t.Fatalf("concurrent retry error = %v", retryErr)
		}
	}
	if succeeded != 1 || conflicted != 7 {
		t.Fatalf("concurrent retries succeeded=%d conflicted=%d, want 1/7", succeeded, conflicted)
	}
	var scanEvents int
	if err := f.pool.QueryRow(f.ctx, `
		SELECT count(*) FROM outbox_events WHERE event_type = 'media.scan_requested' AND aggregate_id = $1::uuid
	`, request.AssetVersionID).Scan(&scanEvents); err != nil {
		t.Fatal(err)
	}
	if scanEvents != 2 {
		t.Fatalf("scan events after concurrent retry = %d, want initial plus one retry", scanEvents)
	}
}

func TestD7CleanProtectedNonVideoKindsBecomeReadyAfterExactVersionScan(t *testing.T) {
	for _, kind := range []AssetKind{KindResource, KindLabMaterial} {
		t.Run(string(kind), func(t *testing.T) {
			f := newMediaFixture(t)
			request, _ := f.beginUpload(kind, "object-"+strings.ToLower(string(kind)))
			if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
				t.Fatalf("completion: %v", err)
			}
			passScanner := mustScanner(t, integrationScannerFunc(func(_ context.Context, object ObjectVersion) (ScanObservation, error) {
				return ScanObservation{AssetVersionID: object.AssetVersionID, StorageObjectVersion: object.StorageObjectVersion, Outcome: ScanPassed, ScannerIdentity: "non-video-scanner"}, nil
			}))
			worker, err := NewWorker(WorkerOptions{DB: f.pool, Scanner: passScanner, Process: integrationProcessorFunc(func(context.Context, ObjectVersion) (TranscodeResult, error) {
				return TranscodeResult{}, errors.New("non-video assets must not invoke video processing")
			}), Outbox: f.writer})
			if err != nil {
				t.Fatal(err)
			}
			if err := worker.Scan(f.ctx, request.AssetVersionID); err != nil {
				t.Fatalf("scan: %v", err)
			}
			status, err := f.service.GetStatus(f.ctx, request.AssetVersionID, Viewer{AccountID: f.instructorID, Role: "INSTRUCTOR"})
			if err != nil {
				t.Fatalf("status: %v", err)
			}
			if status.State != StateReady || !status.Deliverable() || status.TrustedDurationMS != nil {
				t.Fatalf("non-video status = %+v; want READY deliverable with no trusted video duration", status)
			}
			var transcodeEvents, renditions int
			if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM outbox_events WHERE event_type = 'media.transcode_requested' AND aggregate_id = $1::uuid`, request.AssetVersionID).Scan(&transcodeEvents); err != nil {
				t.Fatal(err)
			}
			if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM video_renditions WHERE asset_version_id = $1::uuid`, request.AssetVersionID).Scan(&renditions); err != nil {
				t.Fatal(err)
			}
			if transcodeEvents != 0 || renditions != 0 {
				t.Fatalf("non-video media scheduled video work: transcodes=%d renditions=%d", transcodeEvents, renditions)
			}
		})
	}
}

func TestD7NonVideoRetryReturnsThroughScanningToReady(t *testing.T) {
	f := newMediaFixture(t)
	request, _ := f.beginUpload(KindResource, "object-resource-retry")
	if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
		t.Fatal(err)
	}
	errorScanner := mustScanner(t, integrationScannerFunc(func(context.Context, ObjectVersion) (ScanObservation, error) {
		return ScanObservation{}, errors.New("scanner transport failure")
	}))
	worker, err := NewWorker(WorkerOptions{DB: f.pool, Scanner: errorScanner, Process: integrationProcessorFunc(func(context.Context, ObjectVersion) (TranscodeResult, error) {
		return TranscodeResult{}, errors.New("non-video processor must not run")
	}), Outbox: f.writer})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.scan(f.ctx, request.AssetVersionID, scanWorkID(t, f.pool, request.AssetVersionID, 0)); err == nil {
		t.Fatal("scanner error unexpectedly succeeded")
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateScanError {
		t.Fatalf("failed non-video scan state=%q, want SCAN_ERROR", got)
	}
	if err := f.service.Retry(f.ctx, RetryRequest{AssetVersionID: request.AssetVersionID, AdminAccountID: f.adminID, ActorDescriptor: "d7-admin"}); err != nil {
		t.Fatalf("admin retry: %v", err)
	}
	passScanner := mustScanner(t, integrationScannerFunc(func(_ context.Context, object ObjectVersion) (ScanObservation, error) {
		return ScanObservation{AssetVersionID: object.AssetVersionID, StorageObjectVersion: object.StorageObjectVersion, Outcome: ScanPassed, ScannerIdentity: "resource-retry-scanner"}, nil
	}))
	worker, err = NewWorker(WorkerOptions{DB: f.pool, Scanner: passScanner, Process: integrationProcessorFunc(func(context.Context, ObjectVersion) (TranscodeResult, error) {
		return TranscodeResult{}, errors.New("non-video processor must not run")
	}), Outbox: f.writer})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.scan(f.ctx, request.AssetVersionID, scanWorkID(t, f.pool, request.AssetVersionID, 1)); err != nil {
		t.Fatalf("passing non-video retry scan: %v", err)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateReady {
		t.Fatalf("non-video retry state=%q, want READY", got)
	}
}

func TestD7CommittedMediaOutboxDispatchesToRedis(t *testing.T) {
	f := newMediaFixture(t)
	request, _ := f.beginVideoUpload("object-dispatch")
	if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
		t.Fatalf("completion: %v", err)
	}
	var eventID string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT id::text FROM outbox_events
		WHERE source_module = 'MEDIA_AND_ASSETS' AND event_type = 'media.scan_requested'
		  AND aggregate_id = $1::uuid
	`, request.AssetVersionID).Scan(&eventID); err != nil {
		t.Fatalf("loading committed media outbox event: %v", err)
	}

	client := asynq.NewClient(asynq.RedisClientOpt{Addr: "localhost:6379"})
	t.Cleanup(func() { _ = client.Close() })
	inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: "localhost:6379"})
	t.Cleanup(func() { _ = inspector.Close() })
	t.Cleanup(func() { _ = inspector.DeleteTask("default", eventID) })
	const processingTimeout = 37 * time.Second
	dispatcher, err := NewDispatcher(f.pool, client, processingTimeout)
	if err != nil {
		t.Fatalf("constructing dispatcher: %v", err)
	}
	dispatched, err := dispatcher.DispatchPending(f.ctx, 10)
	if err != nil || dispatched != 1 {
		t.Fatalf("first dispatch count=%d err=%v; want one committed task", dispatched, err)
	}
	task, err := inspector.GetTaskInfo("default", eventID)
	if err != nil {
		t.Fatalf("reading dispatched Redis task: %v", err)
	}
	if task.Type != queue.TypeMediaScan {
		t.Fatalf("task type=%q, want %q", task.Type, queue.TypeMediaScan)
	}
	var work ScanWork
	if err := json.Unmarshal(task.Payload, &work); err != nil {
		t.Fatalf("decoding dispatched task: %v", err)
	}
	if work.AssetVersionID != request.AssetVersionID {
		t.Fatalf("task asset version=%q, want %q", work.AssetVersionID, request.AssetVersionID)
	}
	passScanner := mustScanner(t, integrationScannerFunc(func(_ context.Context, object ObjectVersion) (ScanObservation, error) {
		return ScanObservation{AssetVersionID: object.AssetVersionID, StorageObjectVersion: object.StorageObjectVersion, Outcome: ScanPassed, ScannerIdentity: "dispatcher-scanner"}, nil
	}))
	worker, err := NewWorker(WorkerOptions{DB: f.pool, Scanner: passScanner, Process: integrationProcessorFunc(func(context.Context, ObjectVersion) (TranscodeResult, error) {
		return TranscodeResult{}, errors.New("transcode task is only being dispatched")
	}), Outbox: f.writer, ProcessingTimeout: processingTimeout})
	if err != nil {
		t.Fatalf("constructing dispatcher scan worker: %v", err)
	}
	if err := worker.Scan(f.ctx, request.AssetVersionID); err != nil {
		t.Fatalf("creating committed transcode work: %v", err)
	}
	var transcodeEventID string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT id::text FROM outbox_events
		WHERE source_module = 'MEDIA_AND_ASSETS' AND event_type = 'media.transcode_requested'
		  AND aggregate_id = $1::uuid
	`, request.AssetVersionID).Scan(&transcodeEventID); err != nil {
		t.Fatalf("loading committed transcode event: %v", err)
	}
	t.Cleanup(func() { _ = inspector.DeleteTask("default", transcodeEventID) })
	if dispatched, err := dispatcher.DispatchPending(f.ctx, 10); err != nil || dispatched != 1 {
		t.Fatalf("transcode dispatch count=%d err=%v; want one committed task", dispatched, err)
	}
	transcodeTask, err := inspector.GetTaskInfo("default", transcodeEventID)
	if err != nil {
		t.Fatalf("reading dispatched transcode task: %v", err)
	}
	if transcodeTask.Type != queue.TypeMediaTranscode || transcodeTask.Timeout != processingTimeout {
		t.Fatalf("transcode task = type %q timeout %s; want %q/%s", transcodeTask.Type, transcodeTask.Timeout, queue.TypeMediaTranscode, processingTimeout)
	}
	if dispatched, err := dispatcher.DispatchPending(f.ctx, 10); err != nil || dispatched != 0 {
		t.Fatalf("repeated dispatch count=%d err=%v; want zero after receipt", dispatched, err)
	}
}

func TestD7ScannerFailureModesRemainNonDeliverableIndividually(t *testing.T) {
	cases := []struct {
		name        string
		adapter     func(*testing.T) *ScannerAdapter
		wantState   AssetVersionState
		wantScanErr bool
	}{
		{name: "malware", adapter: func(t *testing.T) *ScannerAdapter {
			return mustScanner(t, integrationScannerFunc(func(_ context.Context, object ObjectVersion) (ScanObservation, error) {
				return ScanObservation{AssetVersionID: object.AssetVersionID, StorageObjectVersion: object.StorageObjectVersion, Outcome: ScanFailed, ScannerIdentity: "integration-scanner", Reason: ErrMalwareDetected.Error()}, nil
			}))
		}, wantState: StateScanFailed},
		{name: "scanner error", adapter: func(t *testing.T) *ScannerAdapter {
			return mustScanner(t, integrationScannerFunc(func(context.Context, ObjectVersion) (ScanObservation, error) {
				return ScanObservation{}, errors.New("scanner transport failed")
			}))
		}, wantState: StateScanError, wantScanErr: true},
		{name: "scanner timeout", adapter: func(t *testing.T) *ScannerAdapter {
			return mustScanner(t, integrationScannerFunc(func(context.Context, ObjectVersion) (ScanObservation, error) {
				return ScanObservation{}, context.DeadlineExceeded
			}))
		}, wantState: StateScanError, wantScanErr: true},
		{name: "scanner absent", adapter: func(t *testing.T) *ScannerAdapter {
			unavailable, err := NewUnavailableScanner("LG-014 absent")
			if err != nil {
				t.Fatal(err)
			}
			return mustScanner(t, unavailable)
		}, wantState: StateScanError, wantScanErr: true},
		{name: "scanner misconfigured", adapter: func(*testing.T) *ScannerAdapter { return &ScannerAdapter{} }, wantState: StateScanError, wantScanErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newMediaFixture(t)
			request, _ := f.beginVideoUpload("failure-" + uuid.NewString())
			if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
				t.Fatalf("completion: %v", err)
			}
			scanner := tc.adapter(t)
			processor := integrationProcessorFunc(func(context.Context, ObjectVersion) (TranscodeResult, error) {
				return TranscodeResult{}, errors.New("processor must not run")
			})
			worker, err := NewWorker(WorkerOptions{DB: f.pool, Scanner: scanner, Process: processor, Outbox: f.writer})
			if err != nil {
				t.Fatal(err)
			}
			scanErr := worker.Scan(f.ctx, request.AssetVersionID)
			if tc.wantScanErr && scanErr == nil {
				t.Fatal("scanner failure unexpectedly returned success")
			}
			if !tc.wantScanErr && scanErr != nil {
				t.Fatalf("malware scan returned unexpected error: %v", scanErr)
			}
			status, err := f.service.GetStatus(f.ctx, request.AssetVersionID, Viewer{AccountID: f.instructorID, Role: "INSTRUCTOR"})
			if err != nil {
				t.Fatalf("status: %v", err)
			}
			if status.State != tc.wantState || status.Deliverable() {
				t.Fatalf("failure mode state=%q deliverable=%t; want %q and false", status.State, status.Deliverable(), tc.wantState)
			}
		})
	}
}

func TestD7ZeroRenditionsAreProcessingFailure(t *testing.T) {
	f := newMediaFixture(t)
	request, _ := f.beginVideoUpload("object-zero-renditions")
	if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
		t.Fatal(err)
	}
	scanner := mustScanner(t, integrationScannerFunc(func(_ context.Context, object ObjectVersion) (ScanObservation, error) {
		return ScanObservation{AssetVersionID: object.AssetVersionID, StorageObjectVersion: object.StorageObjectVersion, Outcome: ScanPassed, ScannerIdentity: "integration-scanner"}, nil
	}))
	processor := integrationProcessorFunc(func(context.Context, ObjectVersion) (TranscodeResult, error) {
		return TranscodeResult{TrustedDurationMS: 1000, OutputPrefix: "media/zero"}, nil
	})
	worker, err := NewWorker(WorkerOptions{DB: f.pool, Scanner: scanner, Process: processor, Outbox: f.writer})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Scan(f.ctx, request.AssetVersionID); err != nil {
		t.Fatal(err)
	}
	var operationID string
	if err := f.pool.QueryRow(f.ctx, `SELECT safe_payload->>'operation_id' FROM outbox_events WHERE event_type = 'media.transcode_requested' AND aggregate_id = $1::uuid`, request.AssetVersionID).Scan(&operationID); err != nil {
		t.Fatal(err)
	}
	if err := worker.Transcode(f.ctx, request.AssetVersionID, operationID); err == nil {
		t.Fatal("zero-rendition transcode succeeded")
	}
	status, err := f.service.GetStatus(f.ctx, request.AssetVersionID, Viewer{AccountID: f.instructorID, Role: "INSTRUCTOR"})
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateProcessFailed || status.Deliverable() {
		t.Fatalf("zero-rendition state=%q deliverable=%t; want PROCESS_FAILED and false", status.State, status.Deliverable())
	}
	var renditionCount int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM video_renditions WHERE asset_version_id = $1::uuid`, request.AssetVersionID).Scan(&renditionCount); err != nil {
		t.Fatal(err)
	}
	if renditionCount != 0 {
		t.Fatalf("zero-rendition output recorded %d renditions", renditionCount)
	}
}

func TestD7ProcessingTimeoutBecomesProcessFailed(t *testing.T) {
	f := newMediaFixture(t)
	request, _ := f.beginVideoUpload("object-processing-timeout")
	if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
		t.Fatal(err)
	}
	scanner := mustScanner(t, integrationScannerFunc(func(_ context.Context, object ObjectVersion) (ScanObservation, error) {
		return ScanObservation{AssetVersionID: object.AssetVersionID, StorageObjectVersion: object.StorageObjectVersion, Outcome: ScanPassed, ScannerIdentity: "timeout-scanner"}, nil
	}))
	processor := integrationProcessorFunc(func(ctx context.Context, _ ObjectVersion) (TranscodeResult, error) {
		<-ctx.Done()
		return TranscodeResult{}, ctx.Err()
	})
	worker, err := NewWorker(WorkerOptions{
		DB: f.pool, Scanner: scanner, Process: processor, Outbox: f.writer,
		ProcessingTimeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Scan(f.ctx, request.AssetVersionID); err != nil {
		t.Fatalf("scan: %v", err)
	}
	var operationID string
	if err := f.pool.QueryRow(f.ctx, `SELECT safe_payload->>'operation_id' FROM outbox_events WHERE event_type = 'media.transcode_requested' AND aggregate_id = $1::uuid`, request.AssetVersionID).Scan(&operationID); err != nil {
		t.Fatal(err)
	}
	if err := worker.Transcode(f.ctx, request.AssetVersionID, operationID); err == nil {
		t.Fatal("timed-out processing unexpectedly succeeded")
	}
	status, err := f.service.GetStatus(f.ctx, request.AssetVersionID, Viewer{AccountID: f.instructorID, Role: "INSTRUCTOR"})
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateProcessFailed || status.Deliverable() {
		t.Fatalf("processing timeout status=%+v; want non-deliverable PROCESS_FAILED", status)
	}
}

func TestD7AdminRetryReturnsThroughQuarantineAndAudits(t *testing.T) {
	f := newMediaFixture(t)
	request, _ := f.beginVideoUpload("object-retry")
	if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
		t.Fatal(err)
	}
	malware := mustScanner(t, integrationScannerFunc(func(_ context.Context, object ObjectVersion) (ScanObservation, error) {
		return ScanObservation{AssetVersionID: object.AssetVersionID, StorageObjectVersion: object.StorageObjectVersion, Outcome: ScanFailed, ScannerIdentity: "integration-scanner", Reason: ErrMalwareDetected.Error()}, nil
	}))
	worker, err := NewWorker(WorkerOptions{DB: f.pool, Scanner: malware, Process: integrationProcessorFunc(func(context.Context, ObjectVersion) (TranscodeResult, error) {
		return TranscodeResult{}, errors.New("not reached")
	}), Outbox: f.writer})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Scan(f.ctx, request.AssetVersionID); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Retry(f.ctx, RetryRequest{AssetVersionID: request.AssetVersionID, AdminAccountID: f.adminID, ActorDescriptor: "d7-admin"}); err != nil {
		t.Fatalf("admin retry: %v", err)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateQuarantined {
		t.Fatalf("retry state = %q, want QUARANTINED", got)
	}
	var auditRole, action string
	if err := f.pool.QueryRow(f.ctx, `SELECT actor_role, action FROM audit_events WHERE target_id = $1 ORDER BY occurred_at DESC LIMIT 1`, request.AssetVersionID).Scan(&auditRole, &action); err != nil {
		t.Fatal(err)
	}
	if auditRole != "ADMIN" || action != "MEDIA_PROCESSING_RETRIED" {
		t.Fatalf("retry audit = %s/%s, want ADMIN/MEDIA_PROCESSING_RETRIED", auditRole, action)
	}
	if err := f.service.Retry(f.ctx, RetryRequest{AssetVersionID: request.AssetVersionID, AdminAccountID: f.instructorID, ActorDescriptor: "wrong-role"}); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("instructor retry error = %v, want ErrNotAuthorized", err)
	}
}

func mustScanner(t *testing.T, scanner Scanner) *ScannerAdapter {
	t.Helper()
	adapter, err := NewScannerAdapter(scanner)
	if err != nil {
		t.Fatalf("constructing scanner adapter: %v", err)
	}
	return adapter
}

func TestD7MigrationContainsMediaAndEntitlementInvariants(t *testing.T) {
	f := newMediaFixture(t)
	var version int64
	if err := f.pool.QueryRow(f.ctx, `SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&version); err != nil {
		t.Fatalf("reading schema version: %v", err)
	}
	if version != int64(db.MaxSchemaVersion) {
		t.Fatalf("schema version = %d, want %d", version, db.MaxSchemaVersion)
	}
	var nullable string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT is_nullable FROM information_schema.columns
		WHERE table_name = 'entitlements' AND column_name = 'grant_source'
	`).Scan(&nullable); err != nil {
		t.Fatalf("checking grant_source nullability: %v", err)
	}
	if nullable != "NO" {
		t.Fatalf("grant_source nullability = %q, want NO", nullable)
	}
	var check string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint
		WHERE conname = 'entitlements_grant_source_implemented'
	`).Scan(&check); err != nil {
		t.Fatalf("reading grant source constraint: %v", err)
	}
	if check == "" {
		t.Fatal("grant source constraint is absent")
	}
	var uniqueIndex bool
	if err := f.pool.QueryRow(f.ctx, `SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'entitlements_one_active_student_course')`).Scan(&uniqueIndex); err != nil {
		t.Fatal(err)
	}
	if !uniqueIndex {
		t.Fatal("active entitlement uniqueness index is absent")
	}
	var sourceCheck string
	if err := f.pool.QueryRow(f.ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'outbox_events_source_module'`).Scan(&sourceCheck); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sourceCheck, "MEDIA_AND_ASSETS") {
		t.Fatalf("outbox source constraint = %q; media source absent", sourceCheck)
	}
	// This query uses the real schema rather than a mock to prove retryable
	// attempts remain exact-version bound without overwriting prior evidence.
	var exactAttemptUnique, workUnique bool
	if err := f.pool.QueryRow(f.ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE tablename = 'scan_attempts' AND indexdef LIKE '%(asset_version_id, storage_object_version, attempt_number)%'
		)
	`).Scan(&exactAttemptUnique); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE tablename = 'scan_attempts' AND indexdef LIKE '%(work_id)%'
		)
	`).Scan(&workUnique); err != nil {
		t.Fatal(err)
	}
	if !exactAttemptUnique || !workUnique {
		t.Fatalf("retryable exact scan identity constraints are absent: exact_attempt=%t work=%t", exactAttemptUnique, workUnique)
	}
	var exactForeignKey bool
	if err := f.pool.QueryRow(f.ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_constraint
			WHERE conname = 'scan_attempt_exact_version_fk'
		)
	`).Scan(&exactForeignKey); err != nil {
		t.Fatal(err)
	}
	if !exactForeignKey {
		t.Fatal("exact object-version scan foreign key is absent")
	}
}
