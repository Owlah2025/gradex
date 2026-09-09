//go:build integration

package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

func recoveryWorker(t *testing.T, f *mediaFixture, now *time.Time, processor Processor) *Worker {
	t.Helper()
	scanner := mustScanner(t, integrationScannerFunc(func(_ context.Context, object ObjectVersion) (ScanObservation, error) {
		return ScanObservation{
			AssetVersionID: object.AssetVersionID, StorageObjectVersion: object.StorageObjectVersion,
			Outcome: ScanPassed, ScannerIdentity: "recovery-integration-scanner",
		}, nil
	}))
	worker, err := NewWorker(WorkerOptions{
		DB: f.pool, Scanner: scanner, Process: processor, Outbox: f.writer,
		ProcessingTimeout: time.Second, WorkLeaseDuration: 2 * time.Second,
		Now: func() time.Time { return *now },
	})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	return worker
}

func successfulAttemptProcessor(_ context.Context, object ObjectVersion) (TranscodeResult, error) {
	prefix := processingOutputPrefix(object.AssetVersionID, object.ProcessingOperationID)
	return TranscodeResult{
		TrustedDurationMS: 90_000,
		OutputPrefix:      prefix,
		Renditions: []Rendition{{
			Name: "720p", StorageObjectKey: prefix + "/720p/playlist.m3u8",
			Width: 1280, Height: 720, BitrateKbps: 2800, DurationMS: 90_000,
		}},
	}, nil
}

func TestD103ExpiredScanClaimIsRecoveredAndRetried(t *testing.T) {
	f := newMediaFixture(t)
	request, _ := f.beginVideoUpload("object-v1")
	if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}
	now := time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)
	worker := recoveryWorker(t, f, &now, integrationProcessorFunc(successfulAttemptProcessor))
	firstWork := scanWorkID(t, f.pool, request.AssetVersionID, 0)
	if _, _, applied, err := worker.beginScan(f.ctx, request.AssetVersionID, firstWork); err != nil || !applied {
		t.Fatalf("beginScan applied=%t err=%v", applied, err)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateScanning {
		t.Fatalf("state after interrupted claim=%s, want SCANNING", got)
	}

	now = now.Add(3 * time.Second)
	if recovered, err := worker.RecoverStale(f.ctx, 10); err != nil || recovered != 1 {
		t.Fatalf("RecoverStale recovered=%d err=%v", recovered, err)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateQuarantined {
		t.Fatalf("state after recovery=%s, want QUARANTINED", got)
	}
	if got := mediaEventCount(t, f, "media.scan_requested", request.AssetVersionID); got != 2 {
		t.Fatalf("scan work count=%d, want 2", got)
	}
	var outcome, category string
	if err := f.pool.QueryRow(f.ctx, `SELECT outcome::text FROM scan_attempts WHERE work_id=$1`, firstWork).Scan(&outcome); err != nil {
		t.Fatalf("loading interrupted scan evidence: %v", err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT last_failure_category FROM media_asset_versions WHERE id=$1::uuid`, request.AssetVersionID).Scan(&category); err != nil {
		t.Fatalf("loading failure category: %v", err)
	}
	if outcome != "ERROR" || category != string(failureWorkerInterrupted) {
		t.Fatalf("recovery evidence outcome=%s category=%s", outcome, category)
	}
}

func TestD103QueuedScannerOutageSchedulesBoundedRetry(t *testing.T) {
	f := newMediaFixture(t)
	request, _ := f.beginVideoUpload("object-v1")
	if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}
	now := time.Date(2026, 9, 9, 10, 30, 0, 0, time.UTC)
	scanner := mustScanner(t, integrationScannerFunc(func(context.Context, ObjectVersion) (ScanObservation, error) {
		return ScanObservation{}, errors.New("injected scanner timeout")
	}))
	worker, err := NewWorker(WorkerOptions{
		DB: f.pool, Scanner: scanner, Process: integrationProcessorFunc(successfulAttemptProcessor), Outbox: f.writer,
		ProcessingTimeout: time.Second, WorkLeaseDuration: 2 * time.Second, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	workID := scanWorkID(t, f.pool, request.AssetVersionID, 0)
	payload, err := json.Marshal(ScanWork{AssetVersionID: request.AssetVersionID, ScanWorkID: workID})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.handleScanTask(f.ctx, asynq.NewTask("media.scan", payload)); err != nil {
		t.Fatalf("queued scan returned an error after durable retry scheduling: %v", err)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateQuarantined {
		t.Fatalf("state after scanner outage=%s, want QUARANTINED", got)
	}
	if got := mediaEventCount(t, f, "media.scan_requested", request.AssetVersionID); got != 2 {
		t.Fatalf("scan work count=%d, want original plus retry", got)
	}
}

func TestD103ExpiredProcessingClaimCannotPublishAndRecoveryConverges(t *testing.T) {
	f := newMediaFixture(t)
	request, _ := f.beginVideoUpload("object-v1")
	if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}
	now := time.Date(2026, 9, 9, 11, 0, 0, 0, time.UTC)
	worker := recoveryWorker(t, f, &now, integrationProcessorFunc(successfulAttemptProcessor))
	if err := worker.Scan(f.ctx, request.AssetVersionID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	oldOperation := transcodeOperationID(t, f, request.AssetVersionID, 0)
	oldVersion, applied, err := worker.beginTranscode(f.ctx, request.AssetVersionID, oldOperation)
	if err != nil || !applied {
		t.Fatalf("beginTranscode applied=%t err=%v", applied, err)
	}
	oldResult, _ := successfulAttemptProcessor(f.ctx, oldVersion.Object)

	now = now.Add(3 * time.Second)
	if recovered, err := worker.RecoverStale(f.ctx, 10); err != nil || recovered != 1 {
		t.Fatalf("RecoverStale recovered=%d err=%v", recovered, err)
	}
	if err := worker.CompleteTranscode(f.ctx, request.AssetVersionID, oldOperation, oldResult); err == nil {
		t.Fatal("expired processing attempt published READY")
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateQuarantined {
		t.Fatalf("state after stale completion=%s, want QUARANTINED", got)
	}

	newScanWork := scanWorkID(t, f.pool, request.AssetVersionID, 1)
	if err := worker.scan(f.ctx, request.AssetVersionID, newScanWork, false); err != nil {
		t.Fatalf("recovery Scan: %v", err)
	}
	newOperation := transcodeOperationID(t, f, request.AssetVersionID, 1)
	if newOperation == oldOperation {
		t.Fatal("recovery reused the abandoned processing identity")
	}
	if err := worker.Transcode(f.ctx, request.AssetVersionID, newOperation); err != nil {
		t.Fatalf("recovery Transcode: %v", err)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateReady {
		t.Fatalf("state after recovery=%s, want READY", got)
	}
}

func TestD103TransientStorageFailureSchedulesBoundedRetry(t *testing.T) {
	f := newMediaFixture(t)
	request, _ := f.beginVideoUpload("object-v1")
	if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	worker := recoveryWorker(t, f, &now, integrationProcessorFunc(func(context.Context, ObjectVersion) (TranscodeResult, error) {
		return TranscodeResult{}, fmt.Errorf("%w: injected source read timeout", ErrStorageUnavailable)
	}))
	if err := worker.Scan(f.ctx, request.AssetVersionID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	operationID := transcodeOperationID(t, f, request.AssetVersionID, 0)
	err := worker.Transcode(f.ctx, request.AssetVersionID, operationID)
	if !errors.Is(err, ErrRetryScheduled) {
		t.Fatalf("Transcode error=%v, want ErrRetryScheduled", err)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateQuarantined {
		t.Fatalf("state after transient failure=%s, want QUARANTINED", got)
	}
	if got := mediaEventCount(t, f, "media.scan_requested", request.AssetVersionID); got != 2 {
		t.Fatalf("scan work count=%d, want original plus retry", got)
	}
}

func TestD103ExpiredProcessingClaimStopsAtAttemptBudget(t *testing.T) {
	f := newMediaFixture(t)
	request, _ := f.beginVideoUpload("object-v1")
	if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}
	now := time.Date(2026, 9, 9, 13, 0, 0, 0, time.UTC)
	worker := recoveryWorker(t, f, &now, integrationProcessorFunc(successfulAttemptProcessor))
	if err := worker.Scan(f.ctx, request.AssetVersionID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	operationID := transcodeOperationID(t, f, request.AssetVersionID, 0)
	if _, applied, err := worker.beginTranscode(f.ctx, request.AssetVersionID, operationID); err != nil || !applied {
		t.Fatalf("beginTranscode applied=%t err=%v", applied, err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE media_asset_versions SET processing_attempt_count=$2 WHERE id=$1::uuid`, request.AssetVersionID, MaxWorkAttempts); err != nil {
		t.Fatalf("setting exhausted attempt count: %v", err)
	}
	now = now.Add(3 * time.Second)
	if recovered, err := worker.RecoverStale(f.ctx, 10); err != nil || recovered != 1 {
		t.Fatalf("RecoverStale recovered=%d err=%v", recovered, err)
	}
	if got := mediaState(t, f.pool, request.AssetVersionID); got != StateProcessFailed {
		t.Fatalf("state after exhausted recovery=%s, want PROCESS_FAILED", got)
	}
	if got := mediaEventCount(t, f, "media.scan_requested", request.AssetVersionID); got != 1 {
		t.Fatalf("scan work count=%d, exhausted recovery queued more work", got)
	}
}

func transcodeOperationID(t *testing.T, f *mediaFixture, assetVersionID string, offset int) string {
	t.Helper()
	var operationID string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT safe_payload->>'operation_id' FROM outbox_events
		WHERE event_type='media.transcode_requested' AND aggregate_id=$1::uuid
		ORDER BY occurred_at,id OFFSET $2 LIMIT 1
	`, assetVersionID, offset).Scan(&operationID); err != nil {
		t.Fatalf("loading transcode operation %d: %v", offset, err)
	}
	return operationID
}

func mediaEventCount(t *testing.T, f *mediaFixture, eventType, assetVersionID string) int {
	t.Helper()
	var count int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM outbox_events WHERE event_type=$1 AND aggregate_id=$2::uuid`, eventType, assetVersionID).Scan(&count); err != nil {
		t.Fatalf("counting %s events: %v", eventType, err)
	}
	return count
}
