//go:build integration

package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/Owlah2025/gradex/backend/internal/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"sync"
	"testing"
	"time"
)

type multipartTestStore struct {
	*integrationObjectStore
	guard                    sync.Mutex
	parts                    []storage.MultipartPart
	bytes                    []byte
	version                  string
	completes, signs, aborts int
	failComplete, failAbort  bool
	failHash                 bool
	hashStarted, hashRelease chan struct{}
	providerIDs              map[string]string
	creates                  int
	failCreate               bool
}

func (s *multipartTestStore) HashObjectVersion(ctx context.Context, key, version string) (string, error) {
	if s.failHash {
		return "", errors.New("temporary object storage outage")
	}
	if s.hashStarted != nil {
		close(s.hashStarted)
		select {
		case <-s.hashRelease:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return s.integrationObjectStore.HashObjectVersion(ctx, key, version)
}

func (s *multipartTestStore) CreateMultipartUpload(_ context.Context, key, _ string) (string, error) {
	s.guard.Lock()
	defer s.guard.Unlock()
	if s.providerIDs == nil {
		s.providerIDs = map[string]string{}
	}
	id := uuid.NewString()
	s.providerIDs[key] = id
	s.creates++
	if s.failCreate {
		s.failCreate = false
		return "", errors.New("provider created upload but response lost")
	}
	return id, nil
}
func (s *multipartTestStore) FindMultipartUpload(_ context.Context, key string) (string, error) {
	s.guard.Lock()
	defer s.guard.Unlock()
	return s.providerIDs[key], nil
}
func (s *multipartTestStore) PresignUploadPartURL(context.Context, storage.MultipartPartUpload) (string, error) {
	s.guard.Lock()
	defer s.guard.Unlock()
	s.signs++
	return "https://storage.test/part", nil
}
func (s *multipartTestStore) ListMultipartParts(context.Context, string, string) ([]storage.MultipartPart, error) {
	s.guard.Lock()
	defer s.guard.Unlock()
	return append([]storage.MultipartPart{}, s.parts...), nil
}
func (s *multipartTestStore) MultipartObjectIdentity(context.Context, string) (string, error) {
	s.guard.Lock()
	defer s.guard.Unlock()
	return s.version, nil
}
func (s *multipartTestStore) CompleteMultipartUpload(_ context.Context, key, _ string, _ []int32, _ []string) (string, error) {
	s.guard.Lock()
	defer s.guard.Unlock()
	s.completes++
	s.version = "etag:\"assembled\""
	s.put(key, s.version, s.bytes)
	if s.failComplete {
		s.failComplete = false
		return "", errors.New("provider committed but response lost")
	}
	return s.version, nil
}
func (s *multipartTestStore) AbortMultipartUpload(context.Context, string, string) error {
	s.guard.Lock()
	defer s.guard.Unlock()
	s.aborts++
	if s.failAbort {
		return errors.New("storage temporarily unavailable")
	}
	return nil
}
func (s *multipartTestStore) AbortMultipartKey(ctx context.Context, key string) error {
	return s.AbortMultipartUpload(ctx, key, "unbound")
}
func (s *multipartTestStore) DeleteMultipartObject(_ context.Context, key, version string) error {
	s.guard.Lock()
	defer s.guard.Unlock()
	s.version = ""
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key+"\x00"+version)
	return nil
}

func newMultipartFixture(t *testing.T) (*mediaFixture, *multipartTestStore, CompleteMultipartRequest) {
	t.Helper()
	f := newMediaFixture(t)
	content, bytes := uploadBytesForKind(KindVideo)
	store := &multipartTestStore{integrationObjectStore: f.store, bytes: bytes, parts: []storage.MultipartPart{{PartNumber: 1, ETag: "\"part-1\"", SizeBytes: int64(len(bytes))}}}
	f.service.store = store
	ticket, err := f.service.BeginMultipartUpload(f.ctx, UploadRequest{ClientRequestID: uuid.NewString(), OwnerAccountID: f.instructorID, CourseID: f.courseID, Kind: KindVideo, ContentType: content, SizeBytes: int64(len(bytes))})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(bytes)
	req := CompleteMultipartRequest{OwnerAccountID: f.instructorID, AssetVersionID: ticket.AssetVersionID, UploadID: ticket.UploadID, StorageObjectKey: ticket.StorageObjectKey, ContentType: content, SizeBytes: int64(len(bytes)), SHA256Hex: hex.EncodeToString(sum[:]), Parts: []MultipartCompletedPart{{PartNumber: 1, ETag: "\"part-1\""}}}
	return f, store, req
}

func TestMultipartOtherOwnerCannotSignCompleteReadOrCancel(t *testing.T) {
	f, store, req := newMultipartFixture(t)
	req.OwnerAccountID = f.adminID
	session := MultipartSessionRequest{req.OwnerAccountID, req.AssetVersionID}
	if _, err := f.service.PresignUploadPart(f.ctx, session, 1); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("sign=%v", err)
	}
	if _, err := f.service.GetMultipartUpload(f.ctx, session); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("read=%v", err)
	}
	if _, err := f.service.CompleteMultipartUpload(f.ctx, req); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("complete=%v", err)
	}
	if err := f.service.AbortMultipartUpload(f.ctx, session); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("cancel=%v", err)
	}
	if store.signs+store.completes+store.aborts != 0 {
		t.Fatal("unauthorized request reached storage")
	}
	if got := mediaState(t, f.pool, req.AssetVersionID); got != StateUploaded {
		t.Fatal(got)
	}
}

func TestMultipartLostProviderResponseAndConcurrentRetriesConverge(t *testing.T) {
	f, store, req := newMultipartFixture(t)
	store.failComplete = true
	if _, err := f.service.CompleteMultipartUpload(f.ctx, req); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("lost response=%v", err)
	}
	var status string
	if err := f.pool.QueryRow(f.ctx, `SELECT multipart_status FROM upload_intents WHERE asset_version_id=$1::uuid`, req.AssetVersionID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "COMPLETING" {
		t.Fatal(status)
	}
	errorsOut := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, err := f.service.CompleteMultipartUpload(f.ctx, req); errorsOut <- err }()
	}
	for i := 0; i < 2; i++ {
		if err := <-errorsOut; err != nil {
			t.Fatal(err)
		}
	}
	if got := mediaState(t, f.pool, req.AssetVersionID); got != StateUploaded {
		t.Fatalf("unverified state=%s", got)
	}
	if claimed, err := f.service.VerifyPendingMultipartUpload(f.ctx); !claimed || err != nil {
		t.Fatalf("verification=%t %v", claimed, err)
	}
	var receipts, work int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM media_callback_receipts WHERE asset_version_id=$1::uuid`, req.AssetVersionID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id=$1::uuid`, req.AssetVersionID).Scan(&work); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 || work != 1 || store.completes != 1 || store.hashCallCount() != 1 {
		t.Fatalf("receipts=%d work=%d provider=%d hash=%d", receipts, work, store.completes, store.hashCallCount())
	}
	req.ProviderEventID = "client-retry-new-event"
	retry, err := f.service.CompleteMultipartUpload(f.ctx, req)
	if err != nil || !retry.Duplicate {
		t.Fatalf("repeated=%+v err=%v", retry, err)
	}
	req.SHA256Hex = hex.EncodeToString(make([]byte, 32))
	if _, err := f.service.CompleteMultipartUpload(f.ctx, req); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed evidence=%v", err)
	}
}

func TestMultipartCancellationRetriesStorageFailureAndBlocksCompletion(t *testing.T) {
	f, store, req := newMultipartFixture(t)
	session := MultipartSessionRequest{f.instructorID, req.AssetVersionID}
	store.failAbort = true
	if err := f.service.AbortMultipartUpload(f.ctx, session); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := f.service.PresignUploadPart(f.ctx, session, 1); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err := f.service.CompleteMultipartUpload(f.ctx, req); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	store.failAbort = false
	if count, err := CleanupAbandonedMultipartUploads(f.ctx, f.pool, store, 10); err != nil || count != 1 {
		t.Fatalf("cleanup=%d %v", count, err)
	}
	if err := f.service.AbortMultipartUpload(f.ctx, session); err != nil {
		t.Fatal(err)
	}
	var status string
	var completed bool
	if err := f.pool.QueryRow(f.ctx, `SELECT multipart_status,completed_at IS NOT NULL FROM upload_intents WHERE asset_version_id=$1::uuid`, req.AssetVersionID).Scan(&status, &completed); err != nil {
		t.Fatal(err)
	}
	if status != "ABORTED" || completed {
		t.Fatalf("status=%s completed=%v", status, completed)
	}
}

func TestMultipartAbandonmentDeletesAssembledUnverifiedObject(t *testing.T) {
	f := newMediaFixture(t)
	content, body := uploadBytesForKind(KindVideo)
	store := &multipartTestStore{integrationObjectStore: f.store, bytes: body}
	f.service.now = func() time.Time { return time.Now().Add(-multipartLifetime - time.Hour) }
	request := UploadRequest{ClientRequestID: uuid.NewString(), OwnerAccountID: f.instructorID, CourseID: f.courseID, Kind: KindVideo, ContentType: content, SizeBytes: int64(len(body))}
	record := newUploadRecord(request, f.service.now, multipartLifetime)
	record.multipart = true
	// Seed a historical intent at creation time; immutable terms are never rewritten.
	if err := f.service.persistUpload(f.ctx, request, record); err != nil {
		t.Fatal(err)
	}
	id, err := store.CreateMultipartUpload(f.ctx, record.objectKey, content)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteMultipartUpload(f.ctx, record.objectKey, id, []int32{1}, []string{"\"part-1\""}); err != nil {
		t.Fatal(err)
	}
	if count, err := CleanupAbandonedMultipartUploads(f.ctx, f.pool, store, 10); count != 1 || err != nil {
		t.Fatalf("%d %v", count, err)
	}
	if store.version != "" || store.aborts != 1 {
		t.Fatal("provider state leaked")
	}
	if count, err := CleanupAbandonedMultipartUploads(f.ctx, f.pool, store, 10); count != 0 || err != nil {
		t.Fatalf("repeat %d %v", count, err)
	}
}

func TestMultipartRejectsInvalidProviderPartsBeforeAssembly(t *testing.T) {
	f, store, req := newMultipartFixture(t)
	for _, part := range []storage.MultipartPart{
		{PartNumber: 1, ETag: "\"different\"", SizeBytes: req.SizeBytes},
		{PartNumber: 1, ETag: "\"part-1\"", SizeBytes: req.SizeBytes + 1},
		{PartNumber: 2, ETag: "\"part-1\"", SizeBytes: req.SizeBytes},
	} {
		store.parts = []storage.MultipartPart{part}
		if _, err := f.service.CompleteMultipartUpload(f.ctx, req); !errors.Is(err, ErrValidation) {
			t.Fatalf("part=%+v err=%v", part, err)
		}
	}
	if store.completes != 0 {
		t.Fatal("invalid parts assembled")
	}
}

func TestMultipartOperatingModeAndCourseKeysFailClosed(t *testing.T) {
	f, store, req := newMultipartFixture(t)
	f.service.operatingMode = OperatingModeAdminCatalogue
	if _, err := f.service.BeginMultipartUpload(f.ctx, UploadRequest{OwnerAccountID: f.instructorID, CourseID: f.courseID, Kind: KindVideo, ContentType: "video/mp4", SizeBytes: 10}); !errors.Is(err, ErrNotAuthorized) {
		t.Fatal(err)
	}
	otherCourse := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO courses(id,owner_account_id,lifecycle) VALUES($1::uuid,$2::uuid,'DRAFT')`, otherCourse, f.instructorID); err != nil {
		t.Fatal(err)
	}
	var logicalID string
	if err := f.pool.QueryRow(f.ctx, `SELECT logical_asset_id::text FROM media_asset_versions WHERE id=$1::uuid`, req.AssetVersionID).Scan(&logicalID); err != nil {
		t.Fatal(err)
	}
	_, err := f.pool.Exec(f.ctx, `INSERT INTO media_asset_versions (id,logical_asset_id,course_id,kind,state,storage_object_key,storage_object_version,content_type,size_bytes) VALUES($1::uuid,$2::uuid,$3::uuid,'VIDEO','UPLOADED','quarantine/spoof','pending:spoof','video/mp4',10)`, uuid.NewString(), logicalID, otherCourse)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("cross-course insert=%v", err)
	}
	req.StorageObjectKey = "quarantine/" + otherCourse + "/source"
	if _, err := f.service.CompleteMultipartUpload(f.ctx, req); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if store.completes != 0 {
		t.Fatal("cross-course assembly reached provider")
	}
}

func TestMultipartVerificationLeavesDatabaseUnlockedAndCancellationWins(t *testing.T) {
	f, store, req := newMultipartFixture(t)
	if _, err := f.service.CompleteMultipartUpload(f.ctx, req); err != nil {
		t.Fatal(err)
	}
	store.hashStarted = make(chan struct{})
	store.hashRelease = make(chan struct{})
	result := make(chan error, 1)
	go func() { _, err := f.service.VerifyPendingMultipartUpload(f.ctx); result <- err }()
	select {
	case <-store.hashStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not begin hashing")
	}
	// NOWAIT proves expensive storage I/O holds no upload-intent row lock.
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(f.ctx, `SELECT 1 FROM upload_intents WHERE asset_version_id=$1::uuid FOR UPDATE NOWAIT`, req.AssetVersionID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	status, err := f.service.GetMultipartCompletion(f.ctx, MultipartSessionRequest{f.instructorID, req.AssetVersionID})
	if err != nil || status.State != StateUploaded {
		t.Fatalf("verification status=%+v %v", status, err)
	}
	if err := f.service.AbortMultipartUpload(f.ctx, MultipartSessionRequest{f.instructorID, req.AssetVersionID}); err != nil {
		t.Fatal(err)
	}
	close(store.hashRelease)
	if err := <-result; err == nil {
		t.Fatal("deleted/cancelled object was admitted")
	}
	var completed bool
	if err := f.pool.QueryRow(f.ctx, `SELECT completed_at IS NOT NULL FROM upload_intents WHERE asset_version_id=$1::uuid`, req.AssetVersionID).Scan(&completed); err != nil {
		t.Fatal(err)
	}
	if completed || mediaState(t, f.pool, req.AssetVersionID) != StateUploaded {
		t.Fatal("cancellation lost to verification")
	}
}

func TestMultipartVerificationRecoversWorkerCrashAndStorageOutage(t *testing.T) {
	f, store, req := newMultipartFixture(t)
	if _, err := f.service.CompleteMultipartUpload(f.ctx, req); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE upload_intents SET multipart_verification_claim=$2::uuid,multipart_verification_lease=now()-interval '1 second' WHERE asset_version_id=$1::uuid`, req.AssetVersionID, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	store.failHash = true
	for attempt := 0; attempt < 3; attempt++ {
		if claimed, err := f.service.VerifyPendingMultipartUpload(f.ctx); !claimed || !errors.Is(err, ErrUnavailable) {
			t.Fatalf("attempt=%d claimed=%t err=%v", attempt, claimed, err)
		}
		if _, err := f.pool.Exec(f.ctx, `UPDATE upload_intents SET multipart_verification_retry_at=now() WHERE asset_version_id=$1::uuid`, req.AssetVersionID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.service.GetMultipartCompletion(f.ctx, MultipartSessionRequest{f.instructorID, req.AssetVersionID}); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	store.failHash = false
	if _, err := f.service.CompleteMultipartUpload(f.ctx, req); err != nil {
		t.Fatal(err)
	}
	if claimed, err := f.service.VerifyPendingMultipartUpload(f.ctx); !claimed || err != nil {
		t.Fatalf("recovery=%t %v", claimed, err)
	}
	if store.completes != 1 || store.hashCallCount() != 1 {
		t.Fatal("recovery assembled another object")
	}
}

func TestMultipartVerificationRejectsClientChecksumAndCannotBeBypassed(t *testing.T) {
	f, store, req := newMultipartFixture(t)
	req.SHA256Hex = hex.EncodeToString(make([]byte, 32))
	assembled, err := f.service.CompleteMultipartUpload(f.ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	direct := CompleteUploadRequest{OwnerAccountID: f.instructorID, AssetVersionID: req.AssetVersionID, ProviderEventID: "bypass", StorageObjectKey: req.StorageObjectKey, StorageObjectVersion: assembled.StorageObjectVersion, ContentType: req.ContentType, SizeBytes: req.SizeBytes, SHA256Hex: req.SHA256Hex}
	if _, err := f.service.CompleteUpload(f.ctx, direct); !errors.Is(err, ErrConflict) {
		t.Fatalf("HTTP bypass=%v", err)
	}
	if claimed, err := f.service.VerifyPendingMultipartUpload(f.ctx); !claimed || !errors.Is(err, ErrValidation) {
		t.Fatalf("bad checksum=%t %v", claimed, err)
	}
	if _, err := f.service.GetMultipartCompletion(f.ctx, MultipartSessionRequest{f.instructorID, req.AssetVersionID}); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	if mediaState(t, f.pool, req.AssetVersionID) != StateUploaded || store.hashCallCount() != 1 {
		t.Fatal("invalid object was admitted")
	}
	var work int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id=$1::uuid`, req.AssetVersionID).Scan(&work); err != nil {
		t.Fatal(err)
	}
	if work != 0 {
		t.Fatal("invalid object scheduled media processing")
	}
}
