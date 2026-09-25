package media

import (
	"testing"
	"time"
)

// TestLessonPreviewLifetimeIsAbsolutelyCapped is the G1-3 regression.
//
// The claimed two-hour anonymous cap was not absolute. An unknown duration, and
// a duration above the cap, both took an early return that handed back the
// configured grace verbatim — so a deployment with a grace above two hours minted
// anonymous tokens above the cap, and a long Lesson got a SHORTER token than a
// short one.
//
// Every case here asserts the same invariant: 0 < lifetime <= 2h.
func TestLessonPreviewLifetimeIsAbsolutelyCapped(t *testing.T) {
	const hour = time.Hour
	cases := []struct {
		name       string
		grace      time.Duration
		durationMS int64
		want       time.Duration
	}{
		{name: "short media with normal grace", grace: 30 * time.Minute, durationMS: 10 * 60 * 1000, want: 40 * time.Minute},
		{name: "duration just under the cap", grace: 30 * time.Minute, durationMS: 119 * 60 * 1000, want: maxLessonPreviewLifetime},
		{name: "duration exactly at the cap", grace: 30 * time.Minute, durationMS: 120 * 60 * 1000, want: maxLessonPreviewLifetime},
		{name: "duration beyond the cap gets the ceiling, not the grace", grace: 30 * time.Minute, durationMS: 180 * 60 * 1000, want: maxLessonPreviewLifetime},
		{name: "grace alone above the cap is clamped", grace: 3 * hour, durationMS: 0, want: maxLessonPreviewLifetime},
		{name: "grace above the cap with a duration is clamped", grace: 3 * hour, durationMS: 10 * 60 * 1000, want: maxLessonPreviewLifetime},
		{name: "missing duration keeps the configured grace", grace: 30 * time.Minute, durationMS: 0, want: 30 * time.Minute},
		{name: "negative duration keeps the configured grace", grace: 45 * time.Minute, durationMS: -1, want: 45 * time.Minute},
		{name: "negative grace never yields a dead token", grace: -time.Hour, durationMS: 0, want: maxLessonPreviewLifetime},
		{name: "negative grace with a duration", grace: -time.Hour, durationMS: 10 * 60 * 1000, want: 10 * time.Minute},
		{name: "overflowing duration cannot wrap past the cap", grace: time.Minute, durationMS: 1 << 62, want: maxLessonPreviewLifetime},
		{name: "zero grace and zero duration", grace: 0, durationMS: 0, want: maxLessonPreviewLifetime},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := lessonPreviewLifetime(tc.grace, tc.durationMS)
			if got != tc.want {
				t.Fatalf("lessonPreviewLifetime(%s, %d) = %s, want %s", tc.grace, tc.durationMS, got, tc.want)
			}
			if got > maxLessonPreviewLifetime {
				t.Fatalf("lifetime %s exceeds the absolute anonymous cap %s", got, maxLessonPreviewLifetime)
			}
			if got <= 0 {
				t.Fatalf("lifetime %s is not a usable authorization", got)
			}
		})
	}
}

// TestLessonPreviewCapIsNotTheStudentCeiling guards against the anonymous cap
// being quietly raised to the 12-hour protected-playback lifetime. A preview
// token carries no Student, no device and no revocable playback lease, so it
// must not inherit that ceiling.
func TestLessonPreviewCapIsNotTheStudentCeiling(t *testing.T) {
	if maxLessonPreviewLifetime != 2*time.Hour {
		t.Fatalf("anonymous preview cap = %s, want 2h", maxLessonPreviewLifetime)
	}
	if maxLessonPreviewLifetime >= 12*time.Hour {
		t.Fatal("the anonymous preview cap has been raised to the Student playback ceiling")
	}
	// No reachable input may escape the cap.
	for _, grace := range []time.Duration{-time.Hour, 0, time.Minute, 2 * time.Hour, 24 * time.Hour} {
		for _, durationMS := range []int64{-1, 0, 1, 60_000, 7_200_000, 43_200_000, 1 << 62} {
			if got := lessonPreviewLifetime(grace, durationMS); got > maxLessonPreviewLifetime || got <= 0 {
				t.Fatalf("lessonPreviewLifetime(%s, %d) = %s, outside (0, %s]",
					grace, durationMS, got, maxLessonPreviewLifetime)
			}
		}
	}
}
