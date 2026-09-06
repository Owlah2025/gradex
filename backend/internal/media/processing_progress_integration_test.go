//go:build integration

package media

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Durable processing progress (D-098).
//
// Before this, an Instructor whose upload had finished saw only PROCESSING for
// as long as the transcode took, and a page reload lost even that. These tests
// hold the persisted observation to the properties the UI depends on: bounded,
// monotonic within an attempt, scoped to its own asset, reset by a retry,
// readable after a reload, and never visible to someone who may not see the
// asset at all.

// progressReportingProcessor is a Processor that also reports progress, so the
// worker takes the same ProgressProcessor path FFmpegProcessor takes.
type progressReportingProcessor struct {
	report func(context.Context, ObjectVersion, ProgressSink)
	fail   error
}

func (p progressReportingProcessor) Transcode(ctx context.Context, object ObjectVersion) (TranscodeResult, error) {
	return p.TranscodeWithProgress(ctx, object, nil)
}

func (p progressReportingProcessor) TranscodeWithProgress(
	ctx context.Context, object ObjectVersion, sink ProgressSink,
) (TranscodeResult, error) {
	if p.report != nil && sink != nil {
		p.report(ctx, object, sink)
	}
	if p.fail != nil {
		return TranscodeResult{}, p.fail
	}
	prefix := "media/" + object.AssetVersionID + "/hls"
	return TranscodeResult{
		TrustedDurationMS: 123456,
		OutputPrefix:      prefix,
		Renditions: []Rendition{{
			Name: "720p", StorageObjectKey: prefix + "/720p/playlist.m3u8",
			Width: 1280, Height: 720, BitrateKbps: 2800, DurationMS: 123456,
		}},
	}, nil
}

type progressRow struct {
	stage     *string
	percent   *int
	updatedAt *time.Time
	token     *string
}

func readProgress(t *testing.T, f *mediaFixture, versionID string) progressRow {
	t.Helper()
	var row progressRow
	var percent *int16
	if err := f.pool.QueryRow(f.ctx, `
		SELECT processing_stage, processing_progress_percent, processing_updated_at, processing_attempt_token
		FROM media_asset_versions WHERE id = $1::uuid
	`, versionID).Scan(&row.stage, &percent, &row.updatedAt, &row.token); err != nil {
		t.Fatalf("reading processing progress: %v", err)
	}
	if percent != nil {
		value := int(*percent)
		row.percent = &value
	}
	return row
}

// scannedVideo carries one upload to SCAN_PASSED and returns the Asset Version,
// the committed transcode operation identity, and a worker bound to processor.
func scannedVideo(t *testing.T, f *mediaFixture, objectVersion string, processor Processor) (string, string, *Worker) {
	t.Helper()
	request, _ := f.beginVideoUpload(objectVersion)
	if _, err := f.service.CompleteUpload(f.ctx, request); err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}
	scanner, err := NewScannerAdapter(integrationScannerFunc(func(_ context.Context, object ObjectVersion) (ScanObservation, error) {
		return ScanObservation{
			AssetVersionID: object.AssetVersionID, StorageObjectVersion: object.StorageObjectVersion,
			Outcome: ScanPassed, ScannerIdentity: "integration-scanner",
		}, nil
	}))
	if err != nil {
		t.Fatalf("NewScannerAdapter: %v", err)
	}
	worker, err := NewWorker(WorkerOptions{DB: f.pool, Scanner: scanner, Process: processor, Outbox: f.writer})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	if err := worker.Scan(f.ctx, request.AssetVersionID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	var operationID string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT safe_payload->>'operation_id' FROM outbox_events
		WHERE event_type = 'media.transcode_requested' AND aggregate_id = $1::uuid
		ORDER BY id DESC LIMIT 1
	`, request.AssetVersionID).Scan(&operationID); err != nil {
		t.Fatalf("reading transcode operation ID: %v", err)
	}
	return request.AssetVersionID, operationID, worker
}

func TestProcessingProgressStartsAtZeroAdvancesAndSettlesAtReady(t *testing.T) {
	f := newMediaFixture(t)

	var observed []progressRow
	processor := progressReportingProcessor{
		report: func(ctx context.Context, object ObjectVersion, sink ProgressSink) {
			// The claim already opened the observation, so "starts at zero" is
			// proven from the claim rather than from the first report.
			observed = append(observed, readProgress(t, f, object.AssetVersionID))
			for _, percent := range []int{7, 41, 88} {
				sink.Progress(ctx, StageTranscoding, percent)
				// Past the throttle interval, so each step really is written
				// and the sequence below is the sequence the worker persisted.
				time.Sleep(progressMinInterval + 50*time.Millisecond)
				observed = append(observed, readProgress(t, f, object.AssetVersionID))
			}
			sink.Progress(ctx, StagePackaging, 99)
			observed = append(observed, readProgress(t, f, object.AssetVersionID))
		},
	}

	versionID, operationID, worker := scannedVideo(t, f, "progress-v1", processor)
	if err := worker.Transcode(f.ctx, versionID, operationID); err != nil {
		t.Fatalf("Transcode: %v", err)
	}

	wantStages := []ProcessingStage{
		StageTranscoding, StageTranscoding, StageTranscoding, StageTranscoding, StagePackaging,
	}
	wantPercents := []int{0, 7, 41, 88, 99}
	if len(observed) != len(wantPercents) {
		t.Fatalf("captured %d observations, want %d", len(observed), len(wantPercents))
	}
	last := -1
	for i, row := range observed {
		if row.stage == nil || row.percent == nil || row.updatedAt == nil || row.token == nil {
			t.Fatalf("observation %d is incomplete: %+v", i, row)
		}
		if *row.stage != string(wantStages[i]) {
			t.Fatalf("observation %d stage = %s, want %s", i, *row.stage, wantStages[i])
		}
		if *row.percent != wantPercents[i] {
			t.Fatalf("observation %d percent = %d, want %d", i, *row.percent, wantPercents[i])
		}
		if *row.percent < 0 || *row.percent > 100 {
			t.Fatalf("observation %d percent %d is outside 0..100", i, *row.percent)
		}
		if *row.percent < last {
			t.Fatalf("observation %d went backwards: %d after %d", i, *row.percent, last)
		}
		last = *row.percent
		if *row.token != operationID {
			t.Fatalf("observation %d token = %s, want the attempt %s", i, *row.token, operationID)
		}
	}

	// READY closes the observation at 100 in the same statement that makes the
	// asset deliverable, so nothing reads a READY asset as partly processed.
	if got := mediaState(t, f.pool, versionID); got != StateReady {
		t.Fatalf("state = %s, want READY", got)
	}
	final := readProgress(t, f, versionID)
	if final.percent == nil || *final.percent != 100 {
		t.Fatalf("READY percent = %v, want 100", final.percent)
	}
}

func TestProcessingProgressIsThrottledClampedAndMonotonic(t *testing.T) {
	f := newMediaFixture(t)

	var writesDuringBurst int
	processor := progressReportingProcessor{
		report: func(ctx context.Context, object ObjectVersion, sink ProgressSink) {
			before := observationTimestamp(t, f, object.AssetVersionID)
			// A real transcode emits thousands of these. Reported back-to-back
			// inside the throttle interval at one unchanging percentage, only
			// the first advance may reach the database.
			for i := 0; i < 500; i++ {
				sink.Progress(ctx, StageTranscoding, 10)
			}
			after := observationTimestamp(t, f, object.AssetVersionID)
			if !after.Equal(before) {
				writesDuringBurst++
			}
			row := readProgress(t, f, object.AssetVersionID)
			if row.percent == nil || *row.percent != 10 {
				t.Errorf("throttled percent = %v, want 10", row.percent)
			}

			// A backwards report never moves the Instructor's bar back.
			sink.Progress(ctx, StageTranscoding, 3)
			if row = readProgress(t, f, object.AssetVersionID); row.percent == nil || *row.percent != 10 {
				t.Errorf("percent after a backwards report = %v, want it held at 10", row.percent)
			}
			// Out-of-range reports are clamped rather than written or refused.
			sink.Progress(ctx, StageTranscoding, 4000)
			if row = readProgress(t, f, object.AssetVersionID); row.percent == nil || *row.percent != 100 {
				t.Errorf("percent after an over-range report = %v, want it clamped to 100", row.percent)
			}
			// An unknown stage is not persistable at all.
			sink.Progress(ctx, ProcessingStage("MASHING"), 50)
			if row = readProgress(t, f, object.AssetVersionID); row.stage == nil || *row.stage != string(StageTranscoding) {
				t.Errorf("stage after an unknown report = %v, want TRANSCODING", row.stage)
			}
		},
	}

	versionID, operationID, worker := scannedVideo(t, f, "progress-v2", processor)
	if err := worker.Transcode(f.ctx, versionID, operationID); err != nil {
		t.Fatalf("Transcode: %v", err)
	}
	// 500 identical reports produced at most the single advance from 0 to 10.
	if writesDuringBurst > 1 {
		t.Fatalf("burst of 500 reports produced %d distinct writes, want at most 1", writesDuringBurst)
	}
}

func observationTimestamp(t *testing.T, f *mediaFixture, versionID string) time.Time {
	t.Helper()
	row := readProgress(t, f, versionID)
	if row.updatedAt == nil {
		return time.Time{}
	}
	return *row.updatedAt
}

func TestProcessingFailureIsRepresentedAsFailedNotAsProgress(t *testing.T) {
	f := newMediaFixture(t)
	failure := errors.New("fixture transcoder failed")
	processor := progressReportingProcessor{
		fail: failure,
		report: func(ctx context.Context, _ ObjectVersion, sink ProgressSink) {
			sink.Progress(ctx, StageTranscoding, 63)
		},
	}

	versionID, operationID, worker := scannedVideo(t, f, "progress-v3", processor)
	if err := worker.Transcode(f.ctx, versionID, operationID); !errors.Is(err, failure) {
		t.Fatalf("Transcode error = %v, want the processing failure", err)
	}
	if got := mediaState(t, f.pool, versionID); got != StateProcessFailed {
		t.Fatalf("state = %s, want PROCESS_FAILED", got)
	}
	// The last measured point is retained — it says how far the attempt got —
	// while the state makes it unambiguous that nothing is still running.
	row := readProgress(t, f, versionID)
	if row.percent == nil || *row.percent != 63 {
		t.Fatalf("percent after failure = %v, want the last measured 63", row.percent)
	}

	status, err := f.service.GetStatus(f.ctx, versionID, Viewer{AccountID: f.instructorID, Role: "INSTRUCTOR"})
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if status.State != StateProcessFailed {
		t.Fatalf("reported state = %s, want PROCESS_FAILED", status.State)
	}
	if status.Deliverable() {
		t.Fatal("a failed asset must never report as deliverable")
	}
}

func TestRetryClearsTheAbandonedAttemptsProgress(t *testing.T) {
	f := newMediaFixture(t)
	failure := errors.New("fixture transcoder failed")
	processor := progressReportingProcessor{
		fail: failure,
		report: func(ctx context.Context, _ ObjectVersion, sink ProgressSink) {
			sink.Progress(ctx, StageTranscoding, 63)
		},
	}
	versionID, operationID, worker := scannedVideo(t, f, "progress-v4", processor)
	if err := worker.Transcode(f.ctx, versionID, operationID); !errors.Is(err, failure) {
		t.Fatalf("Transcode error = %v, want the processing failure", err)
	}

	if err := f.service.Retry(f.ctx, RetryRequest{
		AssetVersionID: versionID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
	}); err != nil {
		t.Fatalf("Retry: %v", err)
	}
	row := readProgress(t, f, versionID)
	if row.stage != nil || row.percent != nil || row.updatedAt != nil || row.token != nil {
		t.Fatalf("retry left the abandoned attempt's observation behind: %+v", row)
	}
	status, err := f.service.GetStatus(f.ctx, versionID, Viewer{AccountID: f.instructorID, Role: "INSTRUCTOR"})
	if err != nil {
		t.Fatalf("GetStatus after retry: %v", err)
	}
	if status.ProcessingStage != nil || status.ProcessingProgressPercent != nil {
		t.Fatalf("status after retry = %+v, want no observation", status)
	}
}

func TestConcurrentVideosCarryIndependentProgress(t *testing.T) {
	f := newMediaFixture(t)

	// Two assets reporting different percentages. A single shared percentage
	// anywhere would show one lesson's progress on the other's control.
	percentByAsset := map[string]int{}
	processor := progressReportingProcessor{
		report: func(ctx context.Context, object ObjectVersion, sink ProgressSink) {
			sink.Progress(ctx, StageTranscoding, percentByAsset[object.AssetVersionID])
			row := readProgress(t, f, object.AssetVersionID)
			if row.percent == nil || *row.percent != percentByAsset[object.AssetVersionID] {
				t.Errorf("asset %s read back %v, want its own %d",
					object.AssetVersionID, row.percent, percentByAsset[object.AssetVersionID])
			}
		},
	}

	first, firstOp, worker := scannedVideo(t, f, "progress-v5a", processor)
	second, secondOp, _ := scannedVideo(t, f, "progress-v5b", processor)
	percentByAsset[first] = 22
	percentByAsset[second] = 77

	if err := worker.Transcode(f.ctx, first, firstOp); err != nil {
		t.Fatalf("Transcode first: %v", err)
	}
	if err := worker.Transcode(f.ctx, second, secondOp); err != nil {
		t.Fatalf("Transcode second: %v", err)
	}

	for _, pair := range []struct{ versionID, operationID string }{{first, firstOp}, {second, secondOp}} {
		row := readProgress(t, f, pair.versionID)
		if row.percent == nil || *row.percent != 100 {
			t.Fatalf("asset %s percent = %v, want 100", pair.versionID, row.percent)
		}
		if row.token == nil || *row.token != pair.operationID {
			t.Fatalf("asset %s token = %v, want its own attempt %s", pair.versionID, row.token, pair.operationID)
		}
	}
}

// The crash-and-retry case: an abandoned worker wakes up and reports on an
// attempt that is no longer current. Its write must land nowhere.
func TestStaleAttemptProgressIsIgnored(t *testing.T) {
	f := newMediaFixture(t)
	versionID, operationID, worker := scannedVideo(t, f, "progress-v6", progressReportingProcessor{})
	if err := worker.Transcode(f.ctx, versionID, operationID); err != nil {
		t.Fatalf("Transcode: %v", err)
	}

	stale := newProgressWriter(f.pool, versionID, uuid.NewString())
	stale.Progress(f.ctx, StageTranscoding, 5)
	row := readProgress(t, f, versionID)
	if row.percent == nil || *row.percent != 100 {
		t.Fatalf("percent after a stale write = %v, want the completed 100", row.percent)
	}
	if row.token == nil || *row.token != operationID {
		t.Fatalf("token after a stale write = %v, want the real attempt %s", row.token, operationID)
	}
}

// The reload path. After a refresh the status read is all the browser has, so
// it must carry the latest persisted observation — and only to a viewer who is
// entitled to the asset in the first place.
func TestStatusReadCarriesMidFlightProgressOnlyToEntitledViewers(t *testing.T) {
	f := newMediaFixture(t)

	reached := make(chan struct{})
	release := make(chan struct{})
	processor := progressReportingProcessor{
		report: func(ctx context.Context, _ ObjectVersion, sink ProgressSink) {
			sink.Progress(ctx, StageTranscoding, 44)
			close(reached)
			<-release
		},
	}
	versionID, operationID, worker := scannedVideo(t, f, "progress-v7", processor)

	done := make(chan error, 1)
	go func() { done <- worker.Transcode(f.ctx, versionID, operationID) }()
	<-reached

	if got := mediaState(t, f.pool, versionID); got != StateProcessing {
		t.Fatalf("state = %s, want the attempt still PROCESSING", got)
	}

	owner, err := f.service.GetStatus(f.ctx, versionID, Viewer{AccountID: f.instructorID, Role: "INSTRUCTOR"})
	if err != nil {
		t.Fatalf("GetStatus as the owner: %v", err)
	}
	if owner.ProcessingStage == nil || *owner.ProcessingStage != StageTranscoding {
		t.Fatalf("owner stage = %v, want TRANSCODING", owner.ProcessingStage)
	}
	if owner.ProcessingProgressPercent == nil || *owner.ProcessingProgressPercent != 44 {
		t.Fatalf("owner percent = %v, want 44", owner.ProcessingProgressPercent)
	}
	if owner.ProcessingUpdatedAt == nil {
		t.Fatal("the owner's observation carries no timestamp")
	}

	admin, err := f.service.GetStatus(f.ctx, versionID, Viewer{AccountID: f.adminID, Role: "ADMIN"})
	if err != nil {
		t.Fatalf("GetStatus as an Admin: %v", err)
	}
	if admin.ProcessingProgressPercent == nil || *admin.ProcessingProgressPercent != 44 {
		t.Fatalf("admin percent = %v, want 44", admin.ProcessingProgressPercent)
	}

	// Progress is asset state, so it sits behind exactly the authorization the
	// asset itself sits behind. Another Instructor learns nothing at all.
	strangerID := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name, locale, email_verified_at)
		VALUES ($1::uuid, $2, $2, 'INSTRUCTOR', 'ACTIVE', 'Other Instructor', 'en', now())
	`, strangerID, strangerID+"@example.test"); err != nil {
		t.Fatalf("seeding another instructor: %v", err)
	}
	if _, err := f.service.GetStatus(f.ctx, versionID, Viewer{AccountID: strangerID, Role: "INSTRUCTOR"}); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("GetStatus as another Instructor = %v, want ErrNotAuthorized", err)
	}
	if _, err := f.service.GetStatus(f.ctx, versionID, Viewer{AccountID: uuid.NewString(), Role: "STUDENT"}); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("GetStatus as a Student = %v, want ErrNotAuthorized", err)
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Transcode: %v", err)
	}
	if got := mediaState(t, f.pool, versionID); got != StateReady {
		t.Fatalf("state after release = %s, want READY", got)
	}
}
