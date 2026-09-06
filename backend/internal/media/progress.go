package media

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ProcessingStage names a phase of the media processing attempt that the
// Instructor can be told about truthfully.
//
// Only the two phases that actually exist in the FFmpeg pipeline are
// representable. TRANSCODING is the long one and is measured: FFmpeg reports
// the media time it has written, and the ffprobe duration of the exact source
// object says what the whole job is. PACKAGING covers uploading the finished
// HLS output to private storage. Everything before PROCESSING — quarantine,
// scanning, validation — is already visible in the Asset Version state and is
// deliberately not restated here as a fake percentage.
type ProcessingStage string

const (
	StageTranscoding ProcessingStage = "TRANSCODING"
	StagePackaging   ProcessingStage = "PACKAGING"
)

func (s ProcessingStage) Valid() bool {
	return s == StageTranscoding || s == StagePackaging
}

// ProgressSink receives measured progress from a processing attempt. It is
// advisory: a sink that fails must never fail the transcode, because losing an
// observation is not a reason to discard finished work.
type ProgressSink interface {
	Progress(ctx context.Context, stage ProcessingStage, percent int)
}

// ProgressProcessor is the optional capability a Processor may add to report
// real progress while it works. The worker uses it when the configured
// processor implements it and falls back to the plain Processor otherwise, so
// a test double or an alternative backend is never forced to invent numbers.
type ProgressProcessor interface {
	Processor
	TranscodeWithProgress(context.Context, ObjectVersion, ProgressSink) (TranscodeResult, error)
}

// ProcessingProgress is the persisted observation for one attempt.
type ProcessingProgress struct {
	Stage        ProcessingStage
	Percent      int
	UpdatedAt    time.Time
	AttemptToken string
}

// ---------------------------------------------------------------------------
// Throttled persistence
// ---------------------------------------------------------------------------

// Progress persistence bounds. A transcode of a one-hour video emits thousands
// of FFmpeg progress blocks; writing each one would turn a read-mostly table
// into a write-amplified one for no extra information a human can perceive.
//
// An observation is written when the percentage advances by at least
// progressPercentStep, when progressMinInterval has elapsed since the last
// write, or when the stage changes. The first and last observation of an
// attempt always write.
const (
	progressPercentStep = 1
	progressMinInterval = time.Second
)

// progressWriter persists throttled progress for exactly one attempt.
//
// It is not safe for concurrent use by design: one attempt has one processor
// goroutine reporting into it.
type progressWriter struct {
	db             *pgxpool.Pool
	assetVersionID string
	attemptToken   string

	written     bool
	lastStage   ProcessingStage
	lastPercent int
	lastWrite   time.Time
	now         func() time.Time
}

func newProgressWriter(db *pgxpool.Pool, assetVersionID, attemptToken string) *progressWriter {
	return &progressWriter{
		db: db, assetVersionID: assetVersionID, attemptToken: attemptToken,
		lastPercent: -1, now: time.Now,
	}
}

// shouldWrite is the throttle decision, kept separate from the database so it
// can be reasoned about and tested without one.
func (w *progressWriter) shouldWrite(stage ProcessingStage, percent int, at time.Time) bool {
	if !w.written {
		return true
	}
	if stage != w.lastStage {
		return true
	}
	if percent >= w.lastPercent+progressPercentStep {
		return true
	}
	return at.Sub(w.lastWrite) >= progressMinInterval
}

// Progress records one observation, subject to the throttle. Out-of-range and
// backwards values are clamped rather than rejected: a processor that reports
// nonsense must not be able to write nonsense, and must not be able to fail
// the job either.
func (w *progressWriter) Progress(ctx context.Context, stage ProcessingStage, percent int) {
	if !stage.Valid() {
		return
	}
	percent = clampPercent(percent)
	if w.written && percent < w.lastPercent && stage == w.lastStage {
		// Monotonic within an attempt: a late or jittery report never moves the
		// Instructor's bar backwards.
		percent = w.lastPercent
	}
	at := w.now().UTC()
	if !w.shouldWrite(stage, percent, at) {
		return
	}
	if err := writeProcessingProgress(ctx, w.db, w.assetVersionID, w.attemptToken, stage, percent, at); err != nil {
		// Advisory: a lost observation costs the Instructor one refresh, while
		// failing here would discard real transcoding work.
		return
	}
	w.written = true
	w.lastStage = stage
	w.lastPercent = percent
	w.lastWrite = at
}

func clampPercent(percent int) int {
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}

// writeProcessingProgress is the only statement that advances an observation.
//
// It writes only while the version is still PROCESSING under the same attempt
// token, and only forwards. A write from an abandoned attempt, or one that
// lands after the version reached READY or PROCESS_FAILED, affects no rows.
func writeProcessingProgress(
	ctx context.Context,
	db *pgxpool.Pool,
	assetVersionID, attemptToken string,
	stage ProcessingStage,
	percent int,
	at time.Time,
) error {
	_, err := db.Exec(ctx, `
		UPDATE media_asset_versions
		SET processing_stage = $2,
		    processing_progress_percent = $3,
		    processing_updated_at = $4,
		    processing_attempt_token = $5
		WHERE id = $1::uuid
		  AND state = 'PROCESSING'
		  AND processing_attempt_token = $5
		  AND (
		        processing_stage IS DISTINCT FROM $2
		        OR processing_progress_percent IS NULL
		        OR processing_progress_percent <= $3
		  )
	`, assetVersionID, string(stage), int16(percent), at, attemptToken)
	if err != nil {
		return fmt.Errorf("recording media processing progress: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// FFmpeg structured progress
// ---------------------------------------------------------------------------

// scanFFmpegProgress reads FFmpeg's machine-readable `-progress` stream and
// reports the media time written so far.
//
// The stream is key=value lines in blocks terminated by `progress=continue` or
// `progress=end`. Only that structured form is parsed; the human-oriented
// stderr status line is never interpreted, because its layout is a
// presentation detail FFmpeg is free to change.
//
// `out_time_us` is preferred. `out_time_ms` is read as a fallback and is
// treated as microseconds, which is what FFmpeg actually emits under that
// misspelled key.
func scanFFmpegProgress(reader io.Reader, onOutTime func(time.Duration)) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var pending time.Duration
	var havePending bool

	for scanner.Scan() {
		key, value, found := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if !found {
			continue
		}
		switch key {
		case "out_time_us", "out_time_ms":
			micros, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if err != nil || micros < 0 {
				// "N/A" appears before the first frame is written.
				continue
			}
			pending = time.Duration(micros) * time.Microsecond
			havePending = true
		case "progress":
			if havePending && onOutTime != nil {
				onOutTime(pending)
			}
			havePending = false
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("reading ffmpeg progress stream: %w", err)
	}
	return nil
}

// rungProgressPercent maps one rung's processed media time onto the whole
// attempt.
//
// The ladder is rendered one rung at a time over the full duration, so the
// attempt is (finished rungs + this rung's fraction) / total rungs. The result
// is capped below 100 because 100 belongs to the attempt that finished, not to
// the last rung that is nearly done.
func rungProgressPercent(rungIndex, rungCount int, processed, duration time.Duration) int {
	if rungCount <= 0 || duration <= 0 || rungIndex < 0 {
		return 0
	}
	fraction := float64(processed) / float64(duration)
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	overall := (float64(rungIndex) + fraction) / float64(rungCount) * 100
	percent := int(overall)
	if percent > 99 {
		percent = 99
	}
	return clampPercent(percent)
}
