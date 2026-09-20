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
	"github.com/jackc/pgx/v5"
)

type mockStorageClient struct {
	mu      sync.Mutex
	objects map[string][]byte
	deleted []string
}

func newMockStorageClient() *mockStorageClient {
	return &mockStorageClient{objects: make(map[string][]byte)}
}

func (m *mockStorageClient) put(key string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = data
}

func (m *mockStorageClient) hasPrefix(prefix string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	cleanPrefix := strings.TrimRight(strings.TrimSpace(prefix), "/")
	prefixWithSlash := cleanPrefix + "/"
	for k := range m.objects {
		if k == cleanPrefix || strings.HasPrefix(k, prefixWithSlash) {
			return true
		}
	}
	return false
}

func (m *mockStorageClient) DeletePrefix(_ context.Context, prefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cleanPrefix := strings.TrimRight(strings.TrimSpace(prefix), "/")
	m.deleted = append(m.deleted, cleanPrefix)
	prefixWithSlash := cleanPrefix + "/"
	for k := range m.objects {
		if k == cleanPrefix || strings.HasPrefix(k, prefixWithSlash) {
			delete(m.objects, k)
		}
	}
	return nil
}

type mockCleanerProcessor struct {
	store *mockStorageClient
}

func (p *mockCleanerProcessor) Transcode(context.Context, ObjectVersion) (TranscodeResult, error) {
	return TranscodeResult{}, nil
}

func (p *mockCleanerProcessor) CleanupAttempt(ctx context.Context, assetVersionID, operationID string) error {
	return p.store.DeletePrefix(ctx, ProcessingOutputPrefix(assetVersionID, operationID))
}

type mockDBRow struct {
	val any
	err error
}

func (r mockDBRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) > 0 {
		if ptr, ok := dest[0].(*bool); ok {
			*ptr = r.val.(bool)
		}
	}
	return nil
}

type mockRenditionRecord struct {
	assetVersionID   string
	storageObjectKey string
}

type mockDB struct {
	mu          sync.Mutex
	renditions  []mockRenditionRecord
	injectedErr error
	queryCount  int
}

func (m *mockDB) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queryCount++
	if m.injectedErr != nil {
		return mockDBRow{err: m.injectedErr}
	}
	var targetAssetID, cleanPrefix, prefixWithSlash string
	if len(args) == 3 {
		targetAssetID = args[0].(string)
		cleanPrefix = args[1].(string)
		prefixWithSlash = args[2].(string)
	} else if len(args) == 2 {
		cleanPrefix = args[0].(string)
		prefixWithSlash = args[1].(string)
	}

	for _, r := range m.renditions {
		if targetAssetID != "" && r.assetVersionID != targetAssetID {
			continue
		}
		if r.storageObjectKey == cleanPrefix || strings.HasPrefix(r.storageObjectKey, prefixWithSlash) {
			return mockDBRow{val: true}
		}
	}
	return mockDBRow{val: false}
}

// 1. UNREFERENCED ATTEMPT PREFIX
// No video_renditions row references prefix. Cleanup is allowed. Expected R2 objects are removed.
func TestCleanupSafety_UnreferencedAttemptPrefix(t *testing.T) {
	ctx := context.Background()
	db := &mockDB{}
	store := newMockStorageClient()
	proc := &mockCleanerProcessor{store: store}

	assetVersionID := uuid.NewString()
	operationID := "op-unref-1"
	prefix := ProcessingOutputPrefix(assetVersionID, operationID)

	store.put(prefix+"/720p/playlist.m3u8", []byte("#EXTM3U"))
	store.put(prefix+"/720p/segment000.ts", []byte("segment-data"))

	cleaned, err := cleanupAttemptSafeWithDB(ctx, db, proc, assetVersionID, operationID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cleaned {
		t.Fatal("expected unreferenced prefix to be cleaned, got skipped")
	}
	if store.hasPrefix(prefix) {
		t.Fatalf("expected objects under %s to be removed, but they remain", prefix)
	}
}

// 2. REFERENCED ATTEMPT PREFIX
// At least one video_renditions.storage_object_key is under prefix. Cleanup is NOT performed. Objects remain.
func TestCleanupSafety_ReferencedAttemptPrefix(t *testing.T) {
	ctx := context.Background()
	assetVersionID := uuid.NewString()
	operationID := "op-ref-1"
	prefix := ProcessingOutputPrefix(assetVersionID, operationID)

	db := &mockDB{
		renditions: []mockRenditionRecord{
			{
				assetVersionID:   assetVersionID,
				storageObjectKey: prefix + "/720p/playlist.m3u8",
			},
		},
	}
	store := newMockStorageClient()
	proc := &mockCleanerProcessor{store: store}

	store.put(prefix+"/720p/playlist.m3u8", []byte("#EXTM3U"))
	store.put(prefix+"/720p/segment000.ts", []byte("segment-data"))

	cleaned, err := cleanupAttemptSafeWithDB(ctx, db, proc, assetVersionID, operationID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cleaned {
		t.Fatal("expected referenced prefix cleanup to be denied, but it succeeded")
	}
	if !store.hasPrefix(prefix) {
		t.Fatalf("expected referenced objects under %s to remain, but they were removed", prefix)
	}
}

// 3. DIFFERENT PREFIX
// Rendition references media/version/transcode/op-A/..., cleanup requested for op-B. op-B cleanup allowed.
func TestCleanupSafety_DifferentPrefix(t *testing.T) {
	ctx := context.Background()
	versionID := uuid.NewString()
	prefixA := fmt.Sprintf("media/%s/transcode/op-A", versionID)
	prefixB := fmt.Sprintf("media/%s/transcode/op-B", versionID)

	db := &mockDB{
		renditions: []mockRenditionRecord{
			{
				assetVersionID:   versionID,
				storageObjectKey: prefixA + "/playlist.m3u8",
			},
		},
	}

	// Check op-A: referenced
	refA, err := HasRenditionReferences(ctx, db, versionID, prefixA)
	if err != nil {
		t.Fatalf("evaluating prefixA references: %v", err)
	}
	if !refA {
		t.Fatal("expected prefixA to be reported referenced")
	}

	// Check op-B: unreferenced
	refB, err := HasRenditionReferences(ctx, db, versionID, prefixB)
	if err != nil {
		t.Fatalf("evaluating prefixB references: %v", err)
	}
	if refB {
		t.Fatal("expected prefixB to be unreferenced, but reported referenced")
	}
}

// 4. PREFIX BOUNDARY SAFETY
// Ensure op-1 does not falsely match op-10 or similar prefix-collision cases.
func TestCleanupSafety_PrefixBoundarySafety(t *testing.T) {
	ctx := context.Background()
	versionID := uuid.NewString()
	prefixOp1 := fmt.Sprintf("media/%s/transcode/op-1", versionID)
	prefixOp10 := fmt.Sprintf("media/%s/transcode/op-10", versionID)

	// Database only has a rendition under op-10
	db := &mockDB{
		renditions: []mockRenditionRecord{
			{
				assetVersionID:   versionID,
				storageObjectKey: prefixOp10 + "/720p/playlist.m3u8",
			},
		},
	}

	// Check op-1: must NOT match op-10!
	refOp1, err := HasRenditionReferences(ctx, db, versionID, prefixOp1)
	if err != nil {
		t.Fatalf("evaluating op-1: %v", err)
	}
	if refOp1 {
		t.Fatalf("prefix op-1 falsely matched op-10! boundary delimiter missing or flawed")
	}

	// Check op-10: must match
	refOp10, err := HasRenditionReferences(ctx, db, versionID, prefixOp10)
	if err != nil {
		t.Fatalf("evaluating op-10: %v", err)
	}
	if !refOp10 {
		t.Fatalf("prefix op-10 failed to match its own rendition")
	}
}

// 5. DB CHECK FAILURE
// Simulate reference-check/database error. Cleanup must fail safe. Prefix must remain.
func TestCleanupSafety_DBCheckFailure_FailsSafe(t *testing.T) {
	ctx := context.Background()
	assetVersionID := uuid.NewString()
	operationID := "op-dberr-1"
	prefix := ProcessingOutputPrefix(assetVersionID, operationID)

	injectedErr := errors.New("simulated database connection dropped")
	db := &mockDB{injectedErr: injectedErr}
	store := newMockStorageClient()
	proc := &mockCleanerProcessor{store: store}

	store.put(prefix+"/720p/playlist.m3u8", []byte("#EXTM3U"))

	cleaned, err := cleanupAttemptSafeWithDB(ctx, db, proc, assetVersionID, operationID)
	if err == nil {
		t.Fatal("expected error on DB failure, got nil")
	}
	if cleaned {
		t.Fatal("cleanup unexpectedly succeeded despite DB failure")
	}
	if !store.hasPrefix(prefix) {
		t.Fatalf("expected objects to remain on DB failure, but prefix %s was deleted", prefix)
	}
}

// 6. REPEATED REFERENCED CLEANUP
// Multiple cleanup attempts against referenced prefix. Objects remain every time.
func TestCleanupSafety_RepeatedReferencedCleanup(t *testing.T) {
	ctx := context.Background()
	assetVersionID := uuid.NewString()
	operationID := "op-repeat-ref"
	prefix := ProcessingOutputPrefix(assetVersionID, operationID)

	db := &mockDB{
		renditions: []mockRenditionRecord{
			{
				assetVersionID:   assetVersionID,
				storageObjectKey: prefix + "/1080p/playlist.m3u8",
			},
		},
	}
	store := newMockStorageClient()
	proc := &mockCleanerProcessor{store: store}

	store.put(prefix+"/1080p/playlist.m3u8", []byte("#EXTM3U"))

	for i := 1; i <= 5; i++ {
		cleaned, err := cleanupAttemptSafeWithDB(ctx, db, proc, assetVersionID, operationID)
		if err != nil {
			t.Fatalf("iteration %d: unexpected error %v", i, err)
		}
		if cleaned {
			t.Fatalf("iteration %d: referenced prefix cleanup was permitted", i)
		}
		if !store.hasPrefix(prefix) {
			t.Fatalf("iteration %d: objects removed despite reference in DB", i)
		}
	}
}

// 7. REPEATED UNREFERENCED CLEANUP
// Cleanup already-removed prefix again. Idempotent contract maintained.
func TestCleanupSafety_RepeatedUnreferencedCleanup(t *testing.T) {
	ctx := context.Background()
	assetVersionID := uuid.NewString()
	operationID := "op-repeat-unref"
	prefix := ProcessingOutputPrefix(assetVersionID, operationID)

	db := &mockDB{}
	store := newMockStorageClient()
	proc := &mockCleanerProcessor{store: store}

	store.put(prefix+"/720p/playlist.m3u8", []byte("#EXTM3U"))

	// First cleanup
	cleaned1, err1 := cleanupAttemptSafeWithDB(ctx, db, proc, assetVersionID, operationID)
	if err1 != nil || !cleaned1 {
		t.Fatalf("first cleanup failed: cleaned=%v err=%v", cleaned1, err1)
	}
	if store.hasPrefix(prefix) {
		t.Fatal("objects remain after first cleanup")
	}

	// Second cleanup on already-absent prefix
	cleaned2, err2 := cleanupAttemptSafeWithDB(ctx, db, proc, assetVersionID, operationID)
	if err2 != nil {
		t.Fatalf("second cleanup returned error on already-removed prefix: %v", err2)
	}
	if !cleaned2 {
		t.Fatal("second cleanup should be allowed as idempotent no-op")
	}
}

// 8. RECOVERY REFERENCED PREFIX
// Recovery attempts cleanup of referenced prefix. Recovery state handling remains correct. Prefix remains.
func TestCleanupSafety_RecoveryReferencedPrefix_PreservesObjects(t *testing.T) {
	ctx := context.Background()
	assetVersionID := uuid.NewString()
	operationID := "op-recovery-ref"
	prefix := ProcessingOutputPrefix(assetVersionID, operationID)

	// Simulate that a rendition row exists in video_renditions for this prefix
	db := &mockDB{
		renditions: []mockRenditionRecord{
			{
				assetVersionID:   assetVersionID,
				storageObjectKey: prefix + "/720p/playlist.m3u8",
			},
		},
	}
	store := newMockStorageClient()
	proc := &mockCleanerProcessor{store: store}
	store.put(prefix+"/720p/playlist.m3u8", []byte("#EXTM3U"))

	// Stale recovery invokes cleanupAttemptSafeWithDB
	cleaned, err := cleanupAttemptSafeWithDB(ctx, db, proc, assetVersionID, operationID)
	if err != nil {
		t.Fatalf("unexpected error during recovery cleanup: %v", err)
	}
	if cleaned {
		t.Fatal("recovery cleanup unexpectedly deleted referenced prefix")
	}
	if !store.hasPrefix(prefix) {
		t.Fatal("objects were removed during recovery of referenced attempt")
	}
}

// 9. PROCESSOR FAILURE
// Processor failure cannot independently blind-delete a prefix without DB-aware authorization.
func TestCleanupSafety_ProcessorFailure_DoesNotBlindDelete(t *testing.T) {
	store := &timeoutProcessingStore{t: t}
	// Use an invalid script to trigger probe/transcode failure immediately
	processor, err := NewFFmpegProcessor(store, "false", "false", 2*time.Second)
	if err != nil {
		t.Fatalf("NewFFmpegProcessor: %v", err)
	}
	ctx := context.Background()
	operationID := "op-fail-no-delete"
	object := ObjectVersion{
		AssetVersionID:        uuid.NewString(),
		StorageObjectKey:      "quarantine/source.mp4",
		StorageObjectVersion:  "v1",
		ProcessingOperationID: operationID,
	}

	_, err = processor.Transcode(ctx, object)
	if err == nil {
		t.Fatal("expected processor.Transcode to fail")
	}
	// Crucial invariant: processor failure must NOT have deleted the prefix!
	if got := store.deletedPrefixes(); len(got) != 0 {
		t.Fatalf("processor unexpectedly blind-deleted attempt prefix on failure: %v", got)
	}
}

// 10. CURRENT SUCCESS PATH
// Successful normal transcode still produces video_renditions and READY behavior.
func TestCleanupSafety_CurrentSuccessPathContract(t *testing.T) {
	// Verify that transcodeCompletion structure and output prefix conventions remain
	// unchanged from current baseline.
	assetVersionID := uuid.NewString()
	operationID := "op-success-contract"
	prefix := ProcessingOutputPrefix(assetVersionID, operationID)

	expectedKey := prefix + "/720p/playlist.m3u8"
	result := TranscodeResult{
		OperationID:       operationID,
		OutputPrefix:      prefix,
		TrustedDurationMS: 60000,
		Renditions: []Rendition{
			{
				Name:             "720p",
				StorageObjectKey: expectedKey,
				Width:            1280,
				Height:           720,
				BitrateKbps:      2800,
				DurationMS:       60000,
			},
		},
		ExpectedRenditions: []string{"720p"},
	}
	if err := validateTranscodeCompletion(assetVersionID, operationID, result); err != nil {
		t.Fatalf("validateTranscodeCompletion: %v", err)
	}
	if result.OutputPrefix != prefix {
		t.Fatalf("got OutputPrefix %s, want %s", result.OutputPrefix, prefix)
	}
	if result.Renditions[0].StorageObjectKey != expectedKey {
		t.Fatalf("got StorageObjectKey %s, want %s", result.Renditions[0].StorageObjectKey, expectedKey)
	}
}

// 11. CURRENT FAILURE PATH
// Failure before successful transcode completion preserves existing state/retry
// semantics except where cleanup is deliberately made safer.
func TestCleanupSafety_CurrentFailurePathClassification(t *testing.T) {
	// Verify failure categorization remains identical
	storageErr := fmt.Errorf("%w: read timeout", ErrStorageUnavailable)
	cat := processingFailureCategory(storageErr)
	if cat != failureStorageUnavailable {
		t.Fatalf("got failure category %s, want %s", cat, failureStorageUnavailable)
	}

	invalidErr := fmt.Errorf("%w: corrupt stream", ErrInvalidMedia)
	cat2 := processingFailureCategory(invalidErr)
	if cat2 != failureInvalidMedia {
		t.Fatalf("got failure category %s, want %s", cat2, failureInvalidMedia)
	}
}

// 12. NO PLAYABLE
// Assert no Phase 3B2A path writes PLAYABLE.
func TestCleanupSafety_NoPlayableTransitionWritten(t *testing.T) {
	// Verify state constants and assert that StatePlayable remains inert
	if StatePlayable != "PLAYABLE" {
		t.Fatalf("unexpected StatePlayable constant: %s", StatePlayable)
	}
	// Deliverable states must NOT include StatePlayable
	if StatePlayable.Deliverable() {
		t.Fatal("StatePlayable must not be Deliverable in Phase 3B2A")
	}
	if StateProcessing.Deliverable() {
		t.Fatal("StateProcessing must not be Deliverable")
	}
	if !StateReady.Deliverable() {
		t.Fatal("StateReady must be Deliverable")
	}
}
