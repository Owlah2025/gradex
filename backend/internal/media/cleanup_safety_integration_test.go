//go:build integration

package media

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestIntegrationCleanupSafety_DurablePostgreSQLReferenceCheck(t *testing.T) {
	f := newMediaFixture(t)
	versionID := uuid.NewString()

	// Seed a media_asset and media_asset_version for foreign key constraints
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO media_assets (id, course_id, owner_account_id, kind)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'VIDEO')
	`, versionID, f.courseID, f.instructorID); err != nil {
		t.Fatalf("seeding media_asset: %v", err)
	}

	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO media_asset_versions (
			id, logical_asset_id, kind, state, storage_object_key, storage_object_version,
			content_type, size_bytes
		) VALUES (
			$1::uuid, $1::uuid, 'VIDEO', 'PROCESSING', 'quarantine/source.mp4', 'v1',
			'video/mp4', 1024
		)
	`, versionID); err != nil {
		t.Fatalf("seeding media_asset_version: %v", err)
	}

	opA := "op-alpha-integration"
	opB := "op-beta-integration"
	prefixA := ProcessingOutputPrefix(versionID, opA)
	prefixB := ProcessingOutputPrefix(versionID, opB)

	// Step 1: Before inserting renditions, both opA and opB are unreferenced
	refA, err := HasAttemptRenditionReferences(f.ctx, f.pool, versionID, opA)
	if err != nil {
		t.Fatalf("HasAttemptRenditionReferences opA: %v", err)
	}
	if refA {
		t.Fatal("expected opA to be unreferenced before insertion")
	}

	refB, err := HasRenditionReferences(f.ctx, f.pool, versionID, prefixB)
	if err != nil {
		t.Fatalf("HasAttemptRenditionReferences opB: %v", err)
	}
	if refB {
		t.Fatal("expected opB to be unreferenced before insertion")
	}

	// Step 2: Insert a durable rendition under opA prefix
	renditionKeyA := prefixA + "/720p/playlist.m3u8"
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO video_renditions (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms)
		VALUES ($1::uuid, '720p', $2, 1280, 720, 2800, 60000)
	`, versionID, renditionKeyA); err != nil {
		t.Fatalf("inserting video_rendition for opA: %v", err)
	}

	// Step 3: Now opA is referenced
	refAAfter, err := HasAttemptRenditionReferences(f.ctx, f.pool, versionID, opA)
	if err != nil {
		t.Fatalf("HasAttemptRenditionReferences opA after insert: %v", err)
	}
	if !refAAfter {
		t.Fatal("expected opA to be reported referenced after insertion in PostgreSQL")
	}

	// Step 4: opB remains unreferenced (Different prefix safety)
	refBAfter, err := HasAttemptRenditionReferences(f.ctx, f.pool, versionID, opB)
	if err != nil {
		t.Fatalf("HasAttemptRenditionReferences opB after opA insert: %v", err)
	}
	if refBAfter {
		t.Fatal("expected opB to remain unreferenced when only opA is in video_renditions")
	}

	// Step 5: Test boundary safety in real PostgreSQL
	// op-1 should NOT match op-10
	prefixOp1 := fmt.Sprintf("media/%s/hls/op-1", versionID)
	prefixOp10 := fmt.Sprintf("media/%s/hls/op-10", versionID)
	renditionKeyOp10 := prefixOp10 + "/720p/playlist.m3u8"

	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO video_renditions (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms)
		VALUES ($1::uuid, '1080p', $2, 1920, 1080, 5000, 60000)
	`, versionID, renditionKeyOp10); err != nil {
		t.Fatalf("inserting video_rendition for op-10: %v", err)
	}

	refOp1, err := HasRenditionReferences(f.ctx, f.pool, versionID, prefixOp1)
	if err != nil {
		t.Fatalf("evaluating op-1 in PostgreSQL: %v", err)
	}
	if refOp1 {
		t.Fatal("PostgreSQL prefix check falsely matched op-1 against op-10")
	}

	refOp10, err := HasRenditionReferences(f.ctx, f.pool, versionID, prefixOp10)
	if err != nil {
		t.Fatalf("evaluating op-10 in PostgreSQL: %v", err)
	}
	if !refOp10 {
		t.Fatal("PostgreSQL prefix check failed to match op-10")
	}
}

func TestIntegrationCleanupSafety_RecoveryRefusedForReferencedPrefix(t *testing.T) {
	f := newMediaFixture(t)
	request, _ := f.beginVideoUpload("object-v1")
	if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}

	now := time.Now().UTC()
	store := newMockStorageClient()
	proc := &mockCleanerProcessor{store: store}

	worker := recoveryWorker(t, f, &now, proc)
	if err := worker.Scan(f.ctx, request.AssetVersionID); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	operationID := transcodeOperationID(t, f, request.AssetVersionID, 0)
	if _, applied, err := worker.beginTranscode(f.ctx, request.AssetVersionID, operationID); err != nil || !applied {
		t.Fatalf("beginTranscode: applied=%t err=%v", applied, err)
	}

	prefix := ProcessingOutputPrefix(request.AssetVersionID, operationID)
	store.put(prefix+"/720p/playlist.m3u8", []byte("#EXTM3U"))

	// Simulate that a progressive or partial rendition was persisted into PostgreSQL
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO video_renditions (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms)
		VALUES ($1::uuid, '720p', $2, 1280, 720, 2800, 60000)
	`, request.AssetVersionID, prefix+"/720p/playlist.m3u8"); err != nil {
		t.Fatalf("inserting referenced rendition: %v", err)
	}

	// Expire the lease in PostgreSQL and run recovery
	expireLease(t, f, request.AssetVersionID)
	now = now.Add(3 * time.Second)

	recovered, err := worker.RecoverStale(f.ctx, 10)
	if err != nil || recovered != 1 {
		t.Fatalf("RecoverStale recovered=%d err=%v", recovered, err)
	}

	// Crucial invariant: Recovery must succeed in fencing and state recovery,
	// but must NOT have deleted the referenced objects!
	if !store.hasPrefix(prefix) {
		t.Fatalf("recovery deleted referenced prefix %s! invariant violated", prefix)
	}
}

func TestIntegrationCleanupSafety_NormalFailureCleansUnreferencedAndRetainsReferenced(t *testing.T) {
	// Case 1: Unreferenced normal transcode failure cleans up prefix
	f := newMediaFixture(t)
	request1, _ := f.beginVideoUpload("object-v1")
	if _, err := f.service.CompleteUpload(f.ctx, request1); err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}
	now := time.Now().UTC()
	store1 := newMockStorageClient()
	failingProc1 := &mockFailingCleanerProcessor{
		store: store1,
		err:   fmt.Errorf("%w: invalid container syntax", ErrInvalidMedia),
	}
	worker1 := recoveryWorker(t, f, &now, failingProc1)
	if err := worker1.Scan(f.ctx, request1.AssetVersionID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	operationID1 := transcodeOperationID(t, f, request1.AssetVersionID, 0)
	prefix1 := ProcessingOutputPrefix(request1.AssetVersionID, operationID1)
	store1.put(prefix1+"/partial.tmp", []byte("unreferenced data"))

	err1 := worker1.Transcode(f.ctx, request1.AssetVersionID, operationID1)
	if err1 == nil {
		t.Fatal("expected Transcode to fail")
	}
	// Since prefix1 was unreferenced, it should be safely cleaned up
	if store1.hasPrefix(prefix1) {
		t.Fatalf("expected unreferenced failed attempt prefix %s to be cleaned up", prefix1)
	}

	// Case 2: Referenced normal transcode failure RETAINS prefix
	request2, _ := f.beginVideoUpload("object-v2")
	if _, err := f.service.CompleteUpload(f.ctx, request2); err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}
	store2 := newMockStorageClient()
	failingProc2 := &mockFailingCleanerProcessor{
		store: store2,
		err:   fmt.Errorf("%w: invalid container syntax", ErrInvalidMedia),
	}
	worker2 := recoveryWorker(t, f, &now, failingProc2)
	if err := worker2.Scan(f.ctx, request2.AssetVersionID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	operationID2 := transcodeOperationID(t, f, request2.AssetVersionID, 0)
	prefix2 := ProcessingOutputPrefix(request2.AssetVersionID, operationID2)
	renditionKey2 := prefix2 + "/720p/playlist.m3u8"
	store2.put(renditionKey2, []byte("#EXTM3U"))

	// Insert durable reference in PostgreSQL
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO video_renditions (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms)
		VALUES ($1::uuid, '720p', $2, 1280, 720, 2800, 60000)
	`, request2.AssetVersionID, renditionKey2); err != nil {
		t.Fatalf("inserting referenced rendition: %v", err)
	}

	err2 := worker2.Transcode(f.ctx, request2.AssetVersionID, operationID2)
	if err2 == nil {
		t.Fatal("expected Transcode to fail")
	}
	// Since prefix2 has durable references, cleanup must be REFUSED and objects retained
	if !store2.hasPrefix(prefix2) {
		t.Fatalf("expected referenced failed attempt prefix %s to be retained, but it was deleted", prefix2)
	}
}

type mockFailingCleanerProcessor struct {
	store *mockStorageClient
	err   error
}

func (p *mockFailingCleanerProcessor) Transcode(context.Context, ObjectVersion) (TranscodeResult, error) {
	return TranscodeResult{}, p.err
}

func (p *mockFailingCleanerProcessor) CleanupAttempt(ctx context.Context, assetVersionID, operationID string) error {
	return p.store.DeletePrefix(ctx, ProcessingOutputPrefix(assetVersionID, operationID))
}
