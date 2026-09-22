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
		if err := worker.PersistVerifiedRendition(f.ctx, request.AssetVersionID, fullOperation, Rendition{
			Name: name, StorageObjectKey: processingOutputPrefix(request.AssetVersionID, fullOperation) + "/" + name + "/playlist.m3u8",
			Width: rung.Width, Height: rung.Height, BitrateKbps: rung.VideoKbps, DurationMS: 90_000,
		}); err != nil {
			t.Fatalf("seed rendition %s: %v", name, err)
		}
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE media_asset_versions SET work_claim_token=NULL, work_claimed_at=NULL, work_lease_expires_at=NULL WHERE id=$1::uuid`, request.AssetVersionID); err != nil {
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

func TestManualEnhancementCompletesMissingCanonicalRungs(t *testing.T) {
	f, worker, assetVersionID := seedPlayableEnhancementAsset(t, []string{"1080p"})
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
