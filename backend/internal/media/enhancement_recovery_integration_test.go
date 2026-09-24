//go:build integration

package media

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Owlah2025/gradex/backend/internal/db"
)

type enhancementIntegrationProcessor struct {
	mu        sync.Mutex
	probe     EnhancementProbe
	failAfter int
	probes    int
	encodes   int
	lastOp    string
	missing   [][]string
}

func (p *enhancementIntegrationProcessor) Transcode(context.Context, ObjectVersion) (TranscodeResult, error) {
	return TranscodeResult{}, errors.New("full processor path is not used by enhancement tests")
}

func (p *enhancementIntegrationProcessor) ProbeExpected(context.Context, ObjectVersion) (EnhancementProbe, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.probes++
	return p.probe, nil
}

func (p *enhancementIntegrationProcessor) TranscodeMissing(ctx context.Context, object ObjectVersion, missing []string, _ ProgressSink, sink VerifiedRenditionSink) (TranscodeResult, error) {
	p.mu.Lock()
	p.encodes++
	p.lastOp = object.ProcessingOperationID
	p.missing = append(p.missing, append([]string(nil), missing...))
	failAfter := p.failAfter
	p.mu.Unlock()
	prefix := processingOutputPrefix(object.AssetVersionID, object.ProcessingOperationID)
	result := TranscodeResult{OutputPrefix: prefix, TrustedDurationMS: p.probe.TrustedDurationMS, ExpectedRenditions: append([]string(nil), p.probe.ExpectedRenditions...)}
	for index, name := range missing {
		if failAfter >= 0 && index >= failAfter {
			return result, fmt.Errorf("synthetic enhancement failure at %s", name)
		}
		rung, ok := hlsRungByName(name)
		if !ok {
			return result, fmt.Errorf("unknown test rung %s", name)
		}
		rendition := Rendition{Name: name, StorageObjectKey: prefix + "/" + name + "/playlist.m3u8", Width: rung.Width, Height: rung.Height, BitrateKbps: rung.VideoKbps, DurationMS: p.probe.TrustedDurationMS}
		if err := sink.PersistVerifiedRendition(ctx, rendition); err != nil {
			return result, err
		}
		result.Renditions = append(result.Renditions, rendition)
	}
	return result, nil
}

func seedPlayableEnhancementAsset(t *testing.T, names []string) (*mediaFixture, *Worker, string) {
	return seedPlayableEnhancementAssetWithLegacy(t, names, false)
}

func seedPlayableEnhancementAssetWithLegacy(t *testing.T, names []string, legacy1080 bool) (*mediaFixture, *Worker, string) {
	t.Helper()
	f := newMediaFixture(t)
	request, _ := f.beginVideoUpload("enhancement-source-v1")
	if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}
	markValidated(t, f, request.AssetVersionID)
	processor := &enhancementIntegrationProcessor{probe: EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}, failAfter: -1}
	worker := recoveryWorker(t, f, timePtr(time.Now().UTC()), processor)
	fullOperation := uuid.NewString()
	if _, applied, err := worker.beginTranscode(f.ctx, request.AssetVersionID, fullOperation); err != nil || !applied {
		t.Fatalf("beginTranscode: applied=%t err=%v", applied, err)
	}
	for _, name := range names {
		rung, ok := hlsRungByName(name)
		if !ok {
			t.Fatalf("unknown seed rung %s", name)
		}
		if legacy1080 && name == "1080p" {
			if _, err := f.pool.Exec(f.ctx, `
				INSERT INTO video_renditions (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms)
				VALUES ($1::uuid, $2, $3, $4, $5, $6, 90000)
			`, request.AssetVersionID, name, processingOutputPrefix(request.AssetVersionID, fullOperation)+"/"+name+"/playlist.m3u8", rung.Width, rung.Height, rung.VideoKbps); err != nil {
				t.Fatalf("seeding legacy 1080: %v", err)
			}
			if _, err := f.pool.Exec(f.ctx, `UPDATE media_asset_versions SET state='PLAYABLE' WHERE id=$1::uuid`, request.AssetVersionID); err != nil {
				t.Fatalf("promoting legacy 1080: %v", err)
			}
			continue
		}
		if err := worker.PersistVerifiedRendition(f.ctx, request.AssetVersionID, fullOperation, Rendition{
			Name: name, StorageObjectKey: processingOutputPrefix(request.AssetVersionID, fullOperation) + "/" + name + "/playlist.m3u8",
			Width: rung.Width, Height: rung.Height, BitrateKbps: rung.VideoKbps, DurationMS: 90_000,
		}); err != nil {
			t.Fatalf("seed rendition %s: %v", name, err)
		}
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE media_asset_versions SET work_claim_token=NULL, work_claimed_at=NULL, work_lease_expires_at=NULL, active_processing_attempt_kind=NULL WHERE id=$1::uuid`, request.AssetVersionID); err != nil {
		t.Fatalf("clearing seed claim: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO processing_attempts (asset_version_id, operation_id, state, attempt_kind, rendition_count, error_reason)
		VALUES ($1::uuid, $2, 'FAILED', 'FULL', 0, 'seeded partial playable asset')
	`, request.AssetVersionID, fullOperation); err != nil {
		t.Fatalf("seeding terminal playable state: %v", err)
	}
	return f, worker, request.AssetVersionID
}

func timePtr(value time.Time) *time.Time { return &value }

func renditionOperation(t *testing.T, f *mediaFixture, assetVersionID, name string) *string {
	t.Helper()
	var operation *string
	if err := f.pool.QueryRow(f.ctx, `SELECT processing_operation_id FROM video_renditions WHERE asset_version_id=$1::uuid AND name=$2`, assetVersionID, name).Scan(&operation); err != nil {
		t.Fatalf("reading %s provenance: %v", name, err)
	}
	return operation
}

func requireRenditionOperation(t *testing.T, f *mediaFixture, assetVersionID, name, want string) {
	t.Helper()
	got := renditionOperation(t, f, assetVersionID, name)
	if got == nil || *got != want {
		t.Fatalf("%s operation=%v, want %s", name, got, want)
	}
}

func requireAttempt(t *testing.T, f *mediaFixture, assetVersionID, operationID, state, kind string) string {
	t.Helper()
	var attemptID string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT id::text FROM processing_attempts
		WHERE asset_version_id=$1::uuid AND operation_id=$2 AND state=$3::media_processing_state
		  AND attempt_kind=$4::media_processing_attempt_kind
	`, assetVersionID, operationID, state, kind).Scan(&attemptID); err != nil {
		t.Fatalf("finding %s %s attempt %s: %v", state, kind, operationID, err)
	}
	return attemptID
}

func TestManualEnhancementCompletesMissingCanonicalRungs(t *testing.T) {
	f, worker, assetVersionID := seedPlayableEnhancementAsset(t, []string{"1080p"})
	prior1080 := renditionOperation(t, f, assetVersionID, "1080p")
	if err := worker.RetryEnhancements(f.ctx, assetVersionID); err != nil {
		t.Fatalf("RetryEnhancements: %v", err)
	}
	processor := worker.process.(*enhancementIntegrationProcessor)
	if got := fmt.Sprint(processor.missing[0]); got != "[720p 480p 240p]" {
		t.Fatalf("missing set=%s, want [720p 480p 240p]", got)
	}
	if got := mediaState(t, f.pool, assetVersionID); got != StateReady {
		t.Fatalf("state = %s, want READY", got)
	}
	var terminalClaim, terminalKind *string
	if err := f.pool.QueryRow(f.ctx, `SELECT work_claim_token, active_processing_attempt_kind::text FROM media_asset_versions WHERE id=$1::uuid`, assetVersionID).Scan(&terminalClaim, &terminalKind); err != nil || terminalClaim != nil || terminalKind != nil {
		t.Fatalf("READY claim=%v active kind=%v err=%v, want both NULL", terminalClaim, terminalKind, err)
	}
	var full, enhancement, finalization int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FILTER (WHERE attempt_kind='FULL'), count(*) FILTER (WHERE attempt_kind='ENHANCEMENT'), count(*) FILTER (WHERE attempt_kind='FINALIZATION') FROM processing_attempts WHERE asset_version_id=$1::uuid`, assetVersionID).Scan(&full, &enhancement, &finalization); err != nil {
		t.Fatal(err)
	}
	if full != 1 || enhancement != 1 || finalization != 0 {
		t.Fatalf("attempt kinds full=%d enhancement=%d finalization=%d", full, enhancement, finalization)
	}
	var count int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM video_renditions WHERE asset_version_id=$1::uuid`, assetVersionID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 4 {
		t.Fatalf("canonical rendition count=%d, want 4", count)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM video_renditions WHERE asset_version_id=$1::uuid AND name='1080p'`, assetVersionID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("1080 canonical row count=%d err=%v, want original row only", count, err)
	}
	operationB := processor.lastOp
	attemptB := requireAttempt(t, f, assetVersionID, operationB, "SUCCEEDED", "ENHANCEMENT")
	var renditionCount int
	var outputPrefix, successfulID string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT pa.rendition_count, pa.output_prefix, mav.successful_processing_attempt_id::text
		FROM processing_attempts pa JOIN media_asset_versions mav ON mav.id=pa.asset_version_id
		WHERE pa.id=$1::uuid
	`, attemptB).Scan(&renditionCount, &outputPrefix, &successfulID); err != nil {
		t.Fatal(err)
	}
	if renditionCount != 3 || outputPrefix != processingOutputPrefix(assetVersionID, operationB) || successfulID != attemptB {
		t.Fatalf("success evidence count=%d prefix=%s linked=%s, want 3/%s/%s", renditionCount, outputPrefix, successfulID, processingOutputPrefix(assetVersionID, operationB), attemptB)
	}
	for _, name := range []string{"720p", "480p", "240p"} {
		requireRenditionOperation(t, f, assetVersionID, name, operationB)
	}
	prior := renditionOperation(t, f, assetVersionID, "1080p")
	if prior == nil || prior1080 == nil || *prior != *prior1080 {
		t.Fatalf("1080 provenance changed from %v to %v", prior1080, prior)
	}
	if err := worker.RetryEnhancements(f.ctx, assetVersionID); !errors.Is(err, ErrEnhancementNotEligible) {
		t.Fatalf("queued duplicate after READY error=%v, want %v", err, ErrEnhancementNotEligible)
	}
}

func TestManualEnhancementFailureKeepsCommittedRenditionsForNextRetry(t *testing.T) {
	f, worker, assetVersionID := seedPlayableEnhancementAsset(t, []string{"1080p"})
	processor := worker.process.(*enhancementIntegrationProcessor)
	processor.failAfter = 1
	if err := worker.RetryEnhancements(f.ctx, assetVersionID); err == nil {
		t.Fatal("expected partial enhancement failure")
	}
	if got := mediaState(t, f.pool, assetVersionID); got != StatePlayable {
		t.Fatalf("state after partial failure=%s, want PLAYABLE", got)
	}
	var count int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM video_renditions WHERE asset_version_id=$1::uuid`, assetVersionID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("renditions after partial failure=%d, want 2", count)
	}
	operationB := processor.lastOp
	requireAttempt(t, f, assetVersionID, operationB, "FAILED", "ENHANCEMENT")
	requireRenditionOperation(t, f, assetVersionID, "720p", operationB)
	processor.failAfter = -1
	if err := worker.RetryEnhancements(f.ctx, assetVersionID); err != nil {
		t.Fatalf("second enhancement retry: %v", err)
	}
	if got := fmt.Sprint(processor.missing[1]); got != "[480p 240p]" {
		t.Fatalf("second missing set=%s, want [480p 240p]", got)
	}
	if got := mediaState(t, f.pool, assetVersionID); got != StateReady {
		t.Fatalf("state after second retry=%s, want READY", got)
	}
	var failed, succeeded int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FILTER (WHERE state='FAILED' AND attempt_kind='ENHANCEMENT'), count(*) FILTER (WHERE state='SUCCEEDED' AND attempt_kind='ENHANCEMENT') FROM processing_attempts WHERE asset_version_id=$1::uuid`, assetVersionID).Scan(&failed, &succeeded); err != nil {
		t.Fatal(err)
	}
	if failed != 1 || succeeded != 1 {
		t.Fatalf("enhancement attempts failed=%d succeeded=%d", failed, succeeded)
	}
	operationC := processor.lastOp
	if operationC == operationB {
		t.Fatal("second retry reused failed operation ID")
	}
	requireAttempt(t, f, assetVersionID, operationC, "SUCCEEDED", "ENHANCEMENT")
	requireRenditionOperation(t, f, assetVersionID, "720p", operationB)
	for _, name := range []string{"480p", "240p"} {
		requireRenditionOperation(t, f, assetVersionID, name, operationC)
	}
}

func TestManualEnhancementFinalizesCompleteCanonicalLadderWithoutEncoding(t *testing.T) {
	f, worker, assetVersionID := seedPlayableEnhancementAsset(t, []string{"1080p", "720p", "480p", "240p"})
	processor := worker.process.(*enhancementIntegrationProcessor)
	if err := worker.RetryEnhancements(f.ctx, assetVersionID); err != nil {
		t.Fatalf("finalization retry: %v", err)
	}
	if processor.encodes != 0 {
		t.Fatalf("enhancement encoder calls=%d, want 0", processor.encodes)
	}
	if got := mediaState(t, f.pool, assetVersionID); got != StateReady {
		t.Fatalf("state=%s, want READY", got)
	}
	var count int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM processing_attempts WHERE asset_version_id=$1::uuid AND attempt_kind='FINALIZATION' AND state='SUCCEEDED' AND rendition_count=0 AND output_prefix IS NULL`, assetVersionID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("successful finalization count=%d, want 1", count)
	}
}

func TestManualEnhancementClaimContentionHasOneWinner(t *testing.T) {
	f, first, assetVersionID := seedPlayableEnhancementAsset(t, []string{"1080p"})
	secondProcessor := &enhancementIntegrationProcessor{probe: EnhancementProbe{ExpectedRenditions: []string{"1080p", "720p", "480p", "240p"}, TrustedDurationMS: 90_000}, failAfter: -1}
	second := recoveryWorker(t, f, timePtr(time.Now().UTC()), secondProcessor)
	errs := make(chan error, 2)
	go func() { errs <- first.RetryEnhancements(f.ctx, assetVersionID) }()
	go func() { errs <- second.RetryEnhancements(f.ctx, assetVersionID) }()
	var successes, expectedRefusals int
	for range 2 {
		switch err := <-errs; {
		case err == nil:
			successes++
		case errors.Is(err, ErrEnhancementActive), errors.Is(err, ErrEnhancementNotEligible):
			expectedRefusals++
		default:
			t.Fatalf("unexpected contention result: %v", err)
		}
	}
	if successes != 1 || expectedRefusals != 1 {
		t.Fatalf("contention successes=%d refusals=%d, want 1/1", successes, expectedRefusals)
	}
	var attempts int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM processing_attempts WHERE asset_version_id=$1::uuid AND attempt_kind='ENHANCEMENT' AND state='SUCCEEDED'`, assetVersionID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("successful enhancement attempts=%d, want 1", attempts)
	}
}

func TestManualEnhancementRequestQueuesWithoutClaimingOrChangingState(t *testing.T) {
	f, _, assetVersionID := seedPlayableEnhancementAsset(t, []string{"1080p"})
	if err := f.service.RetryEnhancements(f.ctx, RetryRequest{AssetVersionID: assetVersionID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID}); err != nil {
		t.Fatalf("first manual request: %v", err)
	}
	if err := f.service.RetryEnhancements(f.ctx, RetryRequest{AssetVersionID: assetVersionID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID}); err != nil {
		t.Fatalf("duplicate manual request: %v", err)
	}
	if got := mediaState(t, f.pool, assetVersionID); got != StatePlayable {
		t.Fatalf("queued state=%s, want PLAYABLE", got)
	}
	var claim *string
	if err := f.pool.QueryRow(f.ctx, `SELECT work_claim_token FROM media_asset_versions WHERE id=$1::uuid`, assetVersionID).Scan(&claim); err != nil {
		t.Fatal(err)
	}
	if claim != nil {
		t.Fatal("manual queue request acquired a worker claim")
	}
	var queued, audits int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM outbox_events WHERE event_type='media.enhancement_requested' AND aggregate_id=$1::uuid`, assetVersionID).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE action='MEDIA_ENHANCEMENT_RETRY_REQUESTED' AND target_id=$1`, assetVersionID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if queued != 2 || audits != 2 {
		t.Fatalf("queued=%d audits=%d, want 2/2", queued, audits)
	}
}

func TestExpiredEnhancementClaimRecordsKindAndClosesRollbackFloor(t *testing.T) {
	for _, committedRung := range []bool{false, true} {
		name := "before_first_rung"
		if committedRung {
			name = "after_one_rung"
		}
		t.Run(name, func(t *testing.T) {
			f, worker, versionID := seedPlayableEnhancementAsset(t, []string{"1080p"})
			operationID := uuid.NewString()
			var store *mockStorageClient
			if _, claimed, err := worker.beginEnhancement(f.ctx, versionID, operationID, ""); err != nil || !claimed {
				t.Fatalf("beginEnhancement claimed=%t err=%v", claimed, err)
			}
			if committedRung {
				rung, _ := hlsRungByName("720p")
				err := worker.PersistVerifiedRendition(f.ctx, versionID, operationID, Rendition{
					Name: "720p", StorageObjectKey: processingOutputPrefix(versionID, operationID) + "/720p/playlist.m3u8",
					Width: rung.Width, Height: rung.Height, BitrateKbps: rung.VideoKbps, DurationMS: 90_000,
				})
				if err != nil {
					t.Fatalf("persisting committed enhancement rung: %v", err)
				}
				store = newMockStorageClient()
				store.put(processingOutputPrefix(versionID, operationID)+"/720p/playlist.m3u8", []byte("#EXTM3U"))
				store.put(processingOutputPrefix(versionID, operationID)+"/480p/partial.tmp", []byte("partial"))
				worker.process = &mockCleanerProcessor{store: store}
			}
			expireLease(t, f, versionID)
			if recovered, err := worker.RecoverStale(f.ctx, 10); err != nil || recovered != 1 {
				t.Fatalf("RecoverStale recovered=%d err=%v", recovered, err)
			}
			requireAttempt(t, f, versionID, operationID, "FAILED", "ENHANCEMENT")
			if err := db.CheckEnhancementRecoveryRollbackSafety(f.ctx, f.pool); err == nil {
				t.Fatal("schema 41 -> 40 rollback stayed open after failed enhancement")
			}
			var state AssetVersionState
			var claim, kind *string
			var successful *string
			if err := f.pool.QueryRow(f.ctx, `
				SELECT state, work_claim_token, active_processing_attempt_kind::text,
				       successful_processing_attempt_id::text
				FROM media_asset_versions WHERE id=$1::uuid
			`, versionID).Scan(&state, &claim, &kind, &successful); err != nil {
				t.Fatal(err)
			}
			if state != StatePlayable || claim != nil || kind != nil || successful != nil {
				t.Fatalf("stale enhancement terminal state=%s claim=%v kind=%v success=%v", state, claim, kind, successful)
			}
			if committedRung {
				requireRenditionOperation(t, f, versionID, "720p", operationID)
				if !store.hasPrefix(processingOutputPrefix(versionID, operationID) + "/720p/playlist.m3u8") {
					t.Fatal("stale recovery cleanup deleted referenced enhancement output")
				}
			}
		})
	}
}

func TestExpiredFullPlayableClaimRemainsFull(t *testing.T) {
	f, worker, _, versionID, operationID := processingVideoReadyForRenditions(t)
	rung, _ := hlsRungByName("720p")
	if err := worker.PersistVerifiedRendition(f.ctx, versionID, operationID, Rendition{
		Name: "720p", StorageObjectKey: processingOutputPrefix(versionID, operationID) + "/720p/playlist.m3u8",
		Width: rung.Width, Height: rung.Height, BitrateKbps: rung.VideoKbps, DurationMS: 90_000,
	}); err != nil {
		t.Fatal(err)
	}
	expireLease(t, f, versionID)
	if recovered, err := worker.RecoverStale(f.ctx, 10); err != nil || recovered != 1 {
		t.Fatalf("RecoverStale recovered=%d err=%v", recovered, err)
	}
	requireAttempt(t, f, versionID, operationID, "FAILED", "FULL")
	if state := mediaState(t, f.pool, versionID); state != StatePlayable {
		t.Fatalf("stale FULL state=%s, want PLAYABLE", state)
	}
}

func TestFinalizationCrashClassification(t *testing.T) {
	for _, classified := range []bool{false, true} {
		name := "before_classification"
		if classified {
			name = "after_classification"
		}
		t.Run(name, func(t *testing.T) {
			f, worker, versionID := seedPlayableEnhancementAsset(t, []string{"1080p", "720p", "480p", "240p"})
			operationID := uuid.NewString()
			if _, claimed, err := worker.beginEnhancement(f.ctx, versionID, operationID, ""); err != nil || !claimed {
				t.Fatalf("beginEnhancement claimed=%t err=%v", claimed, err)
			}
			kind := "ENHANCEMENT"
			if classified {
				if err := worker.beginFinalization(f.ctx, versionID, operationID); err != nil {
					t.Fatal(err)
				}
				kind = "FINALIZATION"
			}
			expireLease(t, f, versionID)
			if recovered, err := worker.RecoverStale(f.ctx, 10); err != nil || recovered != 1 {
				t.Fatalf("RecoverStale recovered=%d err=%v", recovered, err)
			}
			requireAttempt(t, f, versionID, operationID, "FAILED", kind)
			if state := mediaState(t, f.pool, versionID); state != StatePlayable {
				t.Fatalf("state=%s, want PLAYABLE", state)
			}
		})
	}
}

func TestLegacyNullRenditionProvenanceParticipatesInEnhancement(t *testing.T) {
	f, worker, versionID := seedPlayableEnhancementAssetWithLegacy(t, []string{"1080p"}, true)
	if provenance := renditionOperation(t, f, versionID, "1080p"); provenance != nil {
		t.Fatalf("legacy 1080 provenance=%v, want NULL", provenance)
	}
	if err := worker.RetryEnhancements(f.ctx, versionID); err != nil {
		t.Fatalf("enhancing legacy ladder: %v", err)
	}
	if state := mediaState(t, f.pool, versionID); state != StateReady {
		t.Fatalf("state=%s, want READY", state)
	}
	if provenance := renditionOperation(t, f, versionID, "1080p"); provenance != nil {
		t.Fatalf("legacy 1080 was backfilled: %v", provenance)
	}
	operationID := worker.process.(*enhancementIntegrationProcessor).lastOp
	for _, name := range []string{"720p", "480p", "240p"} {
		requireRenditionOperation(t, f, versionID, name, operationID)
	}
}

func TestFailedEnhancementCleanupPreservesCanonicalOutput(t *testing.T) {
	f, worker, versionID := seedPlayableEnhancementAsset(t, []string{"1080p"})
	store := newMockStorageClient()
	worker.process = &mockCleanerProcessor{store: store}
	operationID := uuid.NewString()
	if _, claimed, err := worker.beginEnhancement(f.ctx, versionID, operationID, ""); err != nil || !claimed {
		t.Fatalf("beginEnhancement claimed=%t err=%v", claimed, err)
	}
	prefix := processingOutputPrefix(versionID, operationID)
	canonicalKey := prefix + "/720p/playlist.m3u8"
	partialKey := prefix + "/480p/partial.tmp"
	store.put(canonicalKey, []byte("#EXTM3U"))
	store.put(partialKey, []byte("partial"))
	rung, _ := hlsRungByName("720p")
	if err := worker.PersistVerifiedRendition(f.ctx, versionID, operationID, Rendition{
		Name: "720p", StorageObjectKey: canonicalKey,
		Width: rung.Width, Height: rung.Height, BitrateKbps: rung.VideoKbps, DurationMS: 90_000,
	}); err != nil {
		t.Fatal(err)
	}
	if err := worker.failEnhancement(f.ctx, versionID, operationID, errors.New("partial encode failed")); err == nil {
		t.Fatal("expected enhancement failure")
	}
	requireAttempt(t, f, versionID, operationID, "FAILED", "ENHANCEMENT")
	if !store.hasPrefix(canonicalKey) || !store.hasPrefix(partialKey) {
		t.Fatal("cleanup deleted referenced enhancement prefix")
	}
	if cleaned, err := worker.CleanupAttemptSafe(f.ctx, versionID, operationID); err != nil || cleaned {
		t.Fatalf("CleanupAttemptSafe cleaned=%t err=%v, want retained", cleaned, err)
	}
	requireRenditionOperation(t, f, versionID, "720p", operationID)
}

func TestLostEnhancementClaimCannotPersistOrFinalize(t *testing.T) {
	t.Run("canonical persistence", func(t *testing.T) {
		f, worker, versionID := seedPlayableEnhancementAsset(t, []string{"1080p"})
		operationID := uuid.NewString()
		if _, claimed, err := worker.beginEnhancement(f.ctx, versionID, operationID, ""); err != nil || !claimed {
			t.Fatalf("beginEnhancement claimed=%t err=%v", claimed, err)
		}
		expireLease(t, f, versionID)
		rung, _ := hlsRungByName("720p")
		err := worker.PersistVerifiedRendition(f.ctx, versionID, operationID, Rendition{
			Name: "720p", StorageObjectKey: processingOutputPrefix(versionID, operationID) + "/720p/playlist.m3u8",
			Width: rung.Width, Height: rung.Height, BitrateKbps: rung.VideoKbps, DurationMS: 90_000,
		})
		if !errors.Is(err, ErrLeaseExpired) {
			t.Fatalf("stale persistence error=%v, want expired lease", err)
		}
		var count int
		if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM video_renditions WHERE asset_version_id=$1::uuid AND name='720p'`, versionID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("stale canonical row count=%d err=%v", count, err)
		}
	})
	t.Run("READY finalization", func(t *testing.T) {
		f, worker, versionID := seedPlayableEnhancementAsset(t, []string{"1080p", "720p", "480p", "240p"})
		operationID := uuid.NewString()
		if _, claimed, err := worker.beginEnhancement(f.ctx, versionID, operationID, ""); err != nil || !claimed {
			t.Fatalf("beginEnhancement claimed=%t err=%v", claimed, err)
		}
		if err := worker.beginFinalization(f.ctx, versionID, operationID); err != nil {
			t.Fatal(err)
		}
		expireLease(t, f, versionID)
		err := worker.completeFinalization(f.ctx, versionID, operationID, 90_000, []string{"1080p", "720p", "480p", "240p"})
		if !errors.Is(err, ErrConcurrentModification) {
			t.Fatalf("stale completion error=%v, want claim fence", err)
		}
		if state := mediaState(t, f.pool, versionID); state != StatePlayable {
			t.Fatalf("state=%s, want PLAYABLE", state)
		}
		var succeeded int
		if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM processing_attempts WHERE asset_version_id=$1::uuid AND operation_id=$2 AND state='SUCCEEDED'`, versionID, operationID).Scan(&succeeded); err != nil || succeeded != 0 {
			t.Fatalf("false SUCCEEDED attempts=%d err=%v", succeeded, err)
		}
	})
}
