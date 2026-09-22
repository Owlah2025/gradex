//go:build integration

package media

import (
	"testing"
	"time"
)

// forcePlayable drives one freshly uploaded video Asset Version to PLAYABLE
// through the legitimate waypoints, inserts one canonical rendition row, and
// leaves the claim exactly as `claim` describes.
type playableClaim struct {
	token     string
	claimedAt string
	expiresAt string
}

func forcePlayable(t *testing.T, f *mediaFixture, objectVersion string, claim playableClaim) string {
	t.Helper()
	request, _ := f.beginVideoUpload(objectVersion)
	if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}
	versionID := request.AssetVersionID

	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO scan_attempts (asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity, reason)
		VALUES ($1::uuid, 1, 'w', $2, 'PASSED', 's', 'r')
	`, versionID, objectVersion); err != nil {
		t.Fatalf("insert scan attempt: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE media_asset_versions SET state = 'SCANNING' WHERE id = $1::uuid", versionID); err != nil {
		t.Fatalf("update to SCANNING: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions
		SET state = 'SCAN_PASSED',
		    successful_scan_attempt_id = (SELECT id FROM scan_attempts WHERE asset_version_id = $1::uuid LIMIT 1)
		WHERE id = $1::uuid
	`, versionID); err != nil {
		t.Fatalf("update to SCAN_PASSED: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE media_asset_versions SET state = 'PROCESSING' WHERE id = $1::uuid", versionID); err != nil {
		t.Fatalf("update to PROCESSING: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO video_renditions (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms)
		VALUES ($1::uuid, '720p', $2, 1280, 720, 2800, 90000)
	`, versionID, ProcessingOutputPrefix(versionID, "op-A")+"/720p/playlist.m3u8"); err != nil {
		t.Fatalf("insert canonical rendition: %v", err)
	}
	if claim.token == "" {
		if _, err := f.pool.Exec(f.ctx, "UPDATE media_asset_versions SET state = 'PLAYABLE' WHERE id = $1::uuid", versionID); err != nil {
			t.Fatalf("forcing unclaimed PLAYABLE: %v", err)
		}
		return versionID
	}
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions
		SET state = 'PLAYABLE',
		    work_claim_token = $2,
		    processing_stage = 'TRANSCODING', processing_progress_percent = 0,
		    processing_updated_at = now(), processing_attempt_token = $2,
		    active_processing_attempt_kind = 'FULL',
		    work_claimed_at = now() - `+claim.claimedAt+`,
		    work_lease_expires_at = now() `+claim.expiresAt+`
		WHERE id = $1::uuid
	`, versionID, claim.token); err != nil {
		t.Fatalf("forcing claimed PLAYABLE: %v", err)
	}
	return versionID
}

type playableSnapshot struct {
	state           AssetVersionState
	token           *string
	leaseNull       bool
	failureCategory *string
	attempts        int
	renditions      int
	outboxEvents    int
}

func snapshotPlayable(t *testing.T, f *mediaFixture, versionID string) playableSnapshot {
	t.Helper()
	var snapshot playableSnapshot
	if err := f.pool.QueryRow(f.ctx, `
		SELECT state, work_claim_token, work_lease_expires_at IS NULL, last_failure_category,
		       (SELECT count(*) FROM processing_attempts pa WHERE pa.asset_version_id = mav.id),
		       (SELECT count(*) FROM video_renditions vr WHERE vr.asset_version_id = mav.id),
		       (SELECT count(*) FROM outbox_events oe WHERE oe.aggregate_id = mav.id)
		FROM media_asset_versions mav WHERE id = $1::uuid
	`, versionID).Scan(&snapshot.state, &snapshot.token, &snapshot.leaseNull, &snapshot.failureCategory,
		&snapshot.attempts, &snapshot.renditions, &snapshot.outboxEvents); err != nil {
		t.Fatalf("snapshotting asset version: %v", err)
	}
	return snapshot
}

// TestRecoverStaleIgnoresTerminalFailedPlayable is the regression guard for the
// unbounded-row defect: a PLAYABLE version whose enhancement already failed
// holds no claim and no lease, so every recovery tick used to select it, mint a
// fresh `recovery:<uuid>` operation identity, and append another FAILED
// processing attempt. The assertion is deliberately made across repeated ticks;
// a single pass cannot observe the growth.
func TestRecoverStaleIgnoresTerminalFailedPlayable(t *testing.T) {
	f := newMediaFixture(t)
	now := time.Now()
	worker := recoveryWorker(t, f, &now, integrationProcessorFunc(successfulAttemptProcessor))

	versionID := forcePlayable(t, f, "object-terminal-v1", playableClaim{})
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO processing_attempts (asset_version_id, operation_id, state, rendition_count, error_reason)
		VALUES ($1::uuid, 'op-A', 'FAILED', 0, 'encoder failed after first rendition')
	`, versionID); err != nil {
		t.Fatalf("insert terminal failed attempt: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions SET last_failure_category = 'TRANSCODE_FAILED' WHERE id = $1::uuid
	`, versionID); err != nil {
		t.Fatalf("recording failure category: %v", err)
	}

	before := snapshotPlayable(t, f, versionID)
	if before.attempts != 1 {
		t.Fatalf("expected exactly 1 processing attempt before recovery, got %d", before.attempts)
	}

	for tick := 1; tick <= 3; tick++ {
		recovered, err := worker.RecoverStale(f.ctx, 10)
		if err != nil {
			t.Fatalf("RecoverStale tick %d: %v", tick, err)
		}
		if recovered != 0 {
			t.Fatalf("tick %d recovered %d rows; a terminal failed PLAYABLE must not be selected", tick, recovered)
		}
	}

	after := snapshotPlayable(t, f, versionID)
	if after.attempts != before.attempts {
		t.Fatalf("processing_attempts grew from %d to %d across recovery ticks", before.attempts, after.attempts)
	}
	if after.state != StatePlayable {
		t.Fatalf("expected state PLAYABLE, got %v", after.state)
	}
	if after.token != nil {
		t.Fatalf("expected claim to stay NULL, got %q", *after.token)
	}
	if !after.leaseNull {
		t.Fatal("expected lease to stay NULL")
	}
	if after.renditions != before.renditions || after.renditions != 1 {
		t.Fatalf("canonical renditions changed: %d -> %d", before.renditions, after.renditions)
	}
	if after.outboxEvents != before.outboxEvents {
		t.Fatalf("recovery scheduled work for a terminal failed PLAYABLE: %d -> %d outbox events",
			before.outboxEvents, after.outboxEvents)
	}
	if after.failureCategory == nil || *after.failureCategory != "TRANSCODE_FAILED" {
		t.Fatalf("expected last_failure_category to be preserved, got %v", after.failureCategory)
	}
}

// TestRecoverStaleIgnoresActivePlayable proves the selection still refuses a
// PLAYABLE version whose claim has not expired: that asset belongs to a live
// worker producing the rest of its ladder.
func TestRecoverStaleIgnoresActivePlayable(t *testing.T) {
	f := newMediaFixture(t)
	now := time.Now()
	worker := recoveryWorker(t, f, &now, integrationProcessorFunc(successfulAttemptProcessor))

	versionID := forcePlayable(t, f, "object-active-v1", playableClaim{
		token: "op-live", claimedAt: "interval '1 minute'", expiresAt: "+ interval '1 hour'",
	})
	before := snapshotPlayable(t, f, versionID)

	recovered, err := worker.RecoverStale(f.ctx, 10)
	if err != nil {
		t.Fatalf("RecoverStale: %v", err)
	}
	if recovered != 0 {
		t.Fatalf("expected an actively claimed PLAYABLE to be ignored, recovered %d", recovered)
	}

	after := snapshotPlayable(t, f, versionID)
	if after.state != StatePlayable {
		t.Fatalf("expected state PLAYABLE, got %v", after.state)
	}
	if after.token == nil || *after.token != "op-live" {
		t.Fatalf("expected the live claim to survive, got %v", after.token)
	}
	if after.leaseNull {
		t.Fatal("expected the live lease to survive")
	}
	if after.attempts != before.attempts {
		t.Fatalf("processing_attempts changed for a live PLAYABLE: %d -> %d", before.attempts, after.attempts)
	}
	if after.renditions != before.renditions {
		t.Fatalf("canonical renditions changed: %d -> %d", before.renditions, after.renditions)
	}
}
