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
	mu      sync.RWMutex
	objects map[string][]byte
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
	bytes, ok := s.object(key, version)
	if !ok {
		return "", errors.New("object not found")
	}
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:]), nil
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

func (f *mediaFixture) beginVideoUpload(eventVersion string) (CompleteUploadRequest, []byte) {
	f.t.Helper()
	bytes := append([]byte{0, 0, 0, 24}, []byte("ftypisom")...)
	bytes = append(bytes, make([]byte, 16)...)
	ticket, err := f.service.BeginUpload(f.ctx, UploadRequest{
		OwnerAccountID: f.instructorID, CourseID: f.courseID, Kind: KindVideo,
		Filename: "lesson.mp4", ContentType: "video/mp4", SizeBytes: int64(len(bytes)),
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
		StorageObjectVersion: eventVersion, ContentType: "video/mp4", SizeBytes: int64(len(bytes)),
		SHA256Hex: hex.EncodeToString(sum[:]),
	}, bytes
}

func mediaState(t *testing.T, pool *pgxpool.Pool, versionID string) AssetVersionState {
	t.Helper()
	var state AssetVersionState
	if err := pool.QueryRow(context.Background(), `SELECT state FROM media_asset_versions WHERE id = $1::uuid`, versionID).Scan(&state); err != nil {
		t.Fatalf("reading media state: %v", err)
	}
	return state
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
		Kind: KindVideo, Filename: "lesson-replacement.mp4", ContentType: "video/mp4", SizeBytes: int64(len(firstBytes)),
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
	dispatcher, err := NewDispatcher(f.pool, client)
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
	// This query uses the real schema rather than a mock to prove the exact
	// scan binding is structurally unique.
	var exactUnique bool
	if err := f.pool.QueryRow(f.ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE tablename = 'scan_attempts' AND indexdef LIKE '%(asset_version_id, storage_object_version)%'
		)
	`).Scan(&exactUnique); err != nil {
		t.Fatal(err)
	}
	if !exactUnique {
		t.Fatal("exact object-version scan uniqueness is absent")
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
