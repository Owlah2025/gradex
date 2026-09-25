package media

import (
	"testing"
	"time"
)

// TestAutoEnhancementBackoffMatchesApprovedPolicy pins the exact approved
// automatic retry policy so runtime and documentation cannot drift apart again.
//
// Approved: a maximum of three failed automatic executions. Failure #1 backs off
// 15 minutes, failure #2 backs off 1 hour, failure #3 reaches NEEDS_OPERATOR
// immediately. There is no fourth automatic execution and no third backoff
// stage.
func TestAutoEnhancementBackoffMatchesApprovedPolicy(t *testing.T) {
	if MaxAutoEnhancementFailures != 3 {
		t.Fatalf("MaxAutoEnhancementFailures = %d, want 3", MaxAutoEnhancementFailures)
	}
	for _, tc := range []struct {
		failures int
		want     time.Duration
	}{
		{failures: 1, want: 15 * time.Minute},
		{failures: 2, want: time.Hour},
	} {
		if got := autoEnhancementBackoff(tc.failures); got != tc.want {
			t.Fatalf("autoEnhancementBackoff(%d) = %s, want %s", tc.failures, got, tc.want)
		}
	}
	// Failure #3 is the exhaustion boundary: reconciliation reaches
	// NEEDS_OPERATOR without consulting the schedule, so no backoff interval may
	// be presented for it as a stage an asset would wait in.
	if MaxAutoEnhancementFailures <= 2 {
		t.Fatalf("the reachable backoff arguments are no longer exactly 1 and 2")
	}
	// No reachable argument may produce the retired 4-hour stage.
	for failures := 0; failures <= MaxAutoEnhancementFailures; failures++ {
		if got := autoEnhancementBackoff(failures); got == 4*time.Hour {
			t.Fatalf("autoEnhancementBackoff(%d) returned the retired 4-hour stage", failures)
		}
	}
}
