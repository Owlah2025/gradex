package media

import (
	"strings"
	"testing"
	"time"
)

// The exact shape FFmpeg writes to `-progress pipe:1`: repeated key=value
// blocks, each closed by a `progress=` line. `N/A` appears before the first
// frame is written, and the misspelled `out_time_ms` key actually carries
// microseconds.
const ffmpegProgressFixture = `bitrate=N/A
total_size=0
out_time_us=N/A
out_time_ms=N/A
out_time=N/A
speed=   0x
progress=continue
frame=48
fps=0.00
bitrate=1234.5kbits/s
total_size=98304
out_time_us=2000000
out_time_ms=2000000
out_time=00:00:02.000000
speed=3.91x
progress=continue
frame=240
fps=120.00
out_time_us=10000000
out_time_ms=10000000
out_time=00:00:10.000000
progress=continue
frame=480
out_time_us=20000000
out_time=00:00:20.000000
progress=end
`

func TestScanFFmpegProgressReportsProcessedMediaTime(t *testing.T) {
	var observed []time.Duration
	if err := scanFFmpegProgress(strings.NewReader(ffmpegProgressFixture), func(d time.Duration) {
		observed = append(observed, d)
	}); err != nil {
		t.Fatalf("scanFFmpegProgress: %v", err)
	}
	want := []time.Duration{2 * time.Second, 10 * time.Second, 20 * time.Second}
	if len(observed) != len(want) {
		t.Fatalf("observed %v, want %v", observed, want)
	}
	for i := range want {
		if observed[i] != want[i] {
			t.Fatalf("observation %d = %v, want %v", i, observed[i], want[i])
		}
	}
}

// A block whose only time value is `N/A` must report nothing rather than zero:
// "not started" and "at the very beginning" are different facts.
func TestScanFFmpegProgressIgnoresUnavailableTimes(t *testing.T) {
	var calls int
	if err := scanFFmpegProgress(strings.NewReader("out_time_us=N/A\nprogress=continue\n"), func(time.Duration) {
		calls++
	}); err != nil {
		t.Fatalf("scanFFmpegProgress: %v", err)
	}
	if calls != 0 {
		t.Fatalf("reported %d observations for an N/A block, want 0", calls)
	}
}

// The human-oriented status line is never interpreted. It carries no `=`-keyed
// structure, so nothing in it can be mistaken for progress.
func TestScanFFmpegProgressIgnoresHumanStatusOutput(t *testing.T) {
	var calls int
	human := "frame=  240 fps=120 q=28.0 size=   98304kB time=00:00:10.00 bitrate=1234.5kbits/s\n"
	if err := scanFFmpegProgress(strings.NewReader(human), func(time.Duration) { calls++ }); err != nil {
		t.Fatalf("scanFFmpegProgress: %v", err)
	}
	if calls != 0 {
		t.Fatalf("reported %d observations from a status line, want 0", calls)
	}
}

func TestRungProgressPercentSpansTheWholeLadder(t *testing.T) {
	const duration = 100 * time.Second
	cases := []struct {
		name      string
		index     int
		count     int
		processed time.Duration
		want      int
	}{
		{"first rung, nothing processed", 0, 4, 0, 0},
		{"first rung, half processed", 0, 4, 50 * time.Second, 12},
		{"second rung starts where the first ended", 1, 4, 0, 25},
		{"third rung, half processed", 2, 4, 50 * time.Second, 62},
		{"last rung nearly done never claims 100", 3, 4, duration, 99},
		{"single rung nearly done never claims 100", 0, 1, duration, 99},
		{"overrun is clamped", 0, 2, 500 * time.Second, 50},
		{"unknown duration reports nothing", 0, 4, 10 * time.Second, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			total := duration
			if tc.name == "unknown duration reports nothing" {
				total = 0
			}
			if got := rungProgressPercent(tc.index, tc.count, tc.processed, total); got != tc.want {
				t.Fatalf("rungProgressPercent = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestProgressWriterThrottlesPersistence(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()
	w := newProgressWriter(nil, "asset", "attempt")

	if !w.shouldWrite(StageTranscoding, 0, base) {
		t.Fatal("the first observation of an attempt must always be written")
	}
	// Simulate that write landing.
	w.written, w.lastStage, w.lastPercent, w.lastWrite = true, StageTranscoding, 4, base

	if w.shouldWrite(StageTranscoding, 4, base.Add(200*time.Millisecond)) {
		t.Fatal("an unchanged percentage inside the interval must not be written")
	}
	if !w.shouldWrite(StageTranscoding, 5, base.Add(10*time.Millisecond)) {
		t.Fatal("a one-point advance must be written")
	}
	if !w.shouldWrite(StageTranscoding, 4, base.Add(progressMinInterval)) {
		t.Fatal("a heartbeat at the interval must be written")
	}
	if !w.shouldWrite(StagePackaging, 4, base.Add(10*time.Millisecond)) {
		t.Fatal("a stage change must be written")
	}
}

func TestProgressWriterHoldsProgressMonotonicWithinAnAttempt(t *testing.T) {
	w := newProgressWriter(nil, "asset", "attempt")
	w.written, w.lastStage, w.lastPercent = true, StageTranscoding, 40
	// A nil pool would panic on a real write, so reaching writeProcessingProgress
	// is itself the failure this asserts against.
	w.now = func() time.Time { return time.Unix(0, 0) }
	w.lastWrite = time.Unix(0, 0)
	w.Progress(t.Context(), StageTranscoding, 12)
	if w.lastPercent != 40 {
		t.Fatalf("lastPercent = %d, want the observation to be held at 40", w.lastPercent)
	}
}

func TestProgressWriterRejectsUnknownStages(t *testing.T) {
	w := newProgressWriter(nil, "asset", "attempt")
	// A nil pool means any attempt to persist would panic.
	w.Progress(t.Context(), ProcessingStage("MASHING"), 50)
	if w.written {
		t.Fatal("an unknown stage must never be persisted")
	}
}

func TestClampPercentBoundsTheReportedRange(t *testing.T) {
	for _, tc := range []struct{ in, want int }{{-5, 0}, {0, 0}, {50, 50}, {100, 100}, {140, 100}} {
		if got := clampPercent(tc.in); got != tc.want {
			t.Fatalf("clampPercent(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestProcessingStageValidity(t *testing.T) {
	if !StageTranscoding.Valid() || !StagePackaging.Valid() {
		t.Fatal("the pipeline's own stages must be valid")
	}
	if ProcessingStage("SCANNING").Valid() {
		t.Fatal("a stage the processing pipeline does not have must not be valid")
	}
}
