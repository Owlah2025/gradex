//go:build integration

package media

import (
	"testing"
	"time"
)

func TestPlayableRecovery(t *testing.T) {
	f := newMediaFixture(t)
	now := time.Now()
	worker := recoveryWorker(t, f, &now, integrationProcessorFunc(successfulAttemptProcessor))

	request, _ := f.beginVideoUpload("object-v1")
	_, err := f.service.CompleteUpload(f.ctx, request)
	if err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}
	versionID := request.AssetVersionID

	// Create scan attempt and advance to PLAYABLE explicitly via SQL
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO scan_attempts (asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity, reason)
		VALUES ($1::uuid, 1, 'w', 'object-v1', 'PASSED', 's', 'r')
	`, versionID); err != nil {
		t.Fatalf("insert scan attempt: %v", err)
	}

	if _, err := f.pool.Exec(f.ctx, "UPDATE media_asset_versions SET state = 'SCANNING' WHERE id = $1::uuid", versionID); err != nil {
		t.Fatalf("update to SCANNING: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE media_asset_versions SET state = 'SCAN_PASSED', successful_scan_attempt_id = (SELECT id FROM scan_attempts WHERE asset_version_id = $1::uuid LIMIT 1) WHERE id = $1::uuid", versionID); err != nil {
		t.Fatalf("update to SCAN_PASSED: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE media_asset_versions SET state = 'PROCESSING' WHERE id = $1::uuid", versionID); err != nil {
		t.Fatalf("update to PROCESSING: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions 
		SET state = 'PLAYABLE', 
		    work_claim_token = 'op-1', work_claimed_at = now() - interval '2 hours', work_lease_expires_at = now() - interval '1 hour'
		WHERE id = $1::uuid
	`, versionID); err != nil {
		t.Fatalf("forcing playable: %v", err)
	}

	// 2. Recover
	count, err := worker.RecoverStale(f.ctx, 10)
	if err != nil {
		t.Fatalf("RecoverStale: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 recovery, got %d", count)
	}

	// 3. Verify state remains PLAYABLE, claim cleared
	var state AssetVersionState
	var token *string
	err = f.pool.QueryRow(f.ctx, "SELECT state, work_claim_token FROM media_asset_versions WHERE id = $1::uuid", versionID).Scan(&state, &token)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if state != StatePlayable {
		t.Fatalf("expected state PLAYABLE, got %v", state)
	}
	if token != nil {
		t.Fatalf("expected token NULL, got %v", *token)
	}

	// 4. Verify attempt became terminal (FAILED)
	var attemptState string
	err = f.pool.QueryRow(f.ctx, "SELECT state FROM processing_attempts WHERE asset_version_id = $1::uuid AND operation_id = 'op-1'", versionID).Scan(&attemptState)
	if err != nil {
		t.Fatalf("query attempt: %v", err)
	}
	if attemptState != "FAILED" {
		t.Fatalf("expected attempt FAILED, got %v", attemptState)
	}

	// 5. Verify no TranscodeWork scheduled (outbox is empty)
	if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM outbox_events WHERE event_type='media.transcode_requested' AND aggregate_id=$1::uuid", versionID).Scan(&count); err != nil {
		t.Fatalf("counting outbox events: %v", err)
	}
	if count > 0 {
		t.Fatalf("expected 0 transcode_requested outbox events, got %d", count)
	}

	// 6. Test stale worker cannot commit
	err = worker.CompleteTranscode(f.ctx, versionID, "op-1", TranscodeResult{
		TrustedDurationMS: 1000,
		OutputPrefix:      "media/" + versionID + "/hls/123",
		Renditions:        []Rendition{{Name: "1080p", StorageObjectKey: "media/" + versionID + "/hls/123/1080p/playlist.m3u8", Width: 1920, Height: 1080, BitrateKbps: 5000, DurationMS: 1000}},
	})
	if err == nil {
		t.Fatal("expected CompleteTranscode to fail for stale worker")
	}
}
