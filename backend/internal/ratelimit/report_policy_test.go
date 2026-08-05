package ratelimit

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// T064: the report-submission throttle (FR-032, R-11).
//
// These pin the policy itself and both backends behind it. The route-level
// behaviour lives in internal/httpapi; what matters here is that five per hour
// per Student means the same thing whether Redis answers or the bounded local
// fallback does, and that the key carries nothing but the Account.

const reportPolicyStudentA = "11111111-1111-1111-1111-111111111111"
const reportPolicyStudentB = "22222222-2222-2222-2222-222222222222"

// unavailableStore is stateless, so a concurrent test exercises the limiter's
// own synchronisation rather than a recording double's.
type unavailableStore struct{}

func (unavailableStore) Decide(context.Context, []Entry) (bool, error) {
	return false, errors.New("redis unavailable")
}

func reportPolicyLimiter(t *testing.T, store Store, now time.Time) *Limiter {
	t.Helper()
	limiter, err := New(store, []byte(strings.Repeat("r", 32)), time.Second)
	if err != nil {
		t.Fatalf("constructing report limiter: %v", err)
	}
	limiter.now = func() time.Time { return now }
	limiter.local.now = func() time.Time { return now }
	return limiter
}

// TestProtectedLearningReportPolicyBoundsFiveSubmissionsPerHourPerStudent pins
// the exact authoritative numbers and shape.
func TestProtectedLearningReportPolicyBoundsFiveSubmissionsPerHourPerStudent(t *testing.T) {
	policy := ProtectedLearningReportPolicy()
	if err := policy.Validate(); err != nil {
		t.Fatalf("report policy is invalid: %v", err)
	}
	if policy.ID != "learning-report-v1" || policy.Endpoint != "learning-report" || policy.Category != "PROTECTED_LEARNING" {
		t.Fatalf("report policy identity = %+v", policy)
	}
	if policy.Window != time.Hour {
		t.Fatalf("report window = %s, want one hour", policy.Window)
	}
	if len(policy.Rules) != 1 {
		t.Fatalf("report policy has %d dimensions, want exactly the Student", len(policy.Rules))
	}
	rule := policy.Rules[0]
	if rule.Dimension != DimensionIdentifier {
		t.Fatalf("report dimension = %q, want the authenticated identifier alone", rule.Dimension)
	}
	if ProtectedLearningReportsPerHour != 5 {
		t.Fatalf("report constant = %d, want the authoritative 5", ProtectedLearningReportsPerHour)
	}
	if rule.Limit != ProtectedLearningReportsPerHour {
		t.Fatalf("report limit = %d, want 5/hour", rule.Limit)
	}
	// The local fallback is exactly as strict, never more permissive.
	if rule.LocalLimit != rule.Limit {
		t.Fatalf("local limit = %d, distributed = %d; the fallback must not widen the quota", rule.LocalLimit, rule.Limit)
	}
	// A token bucket would let a Student refill mid-hour; this is a fixed window.
	if rule.Burst != 0 {
		t.Fatalf("report burst = %d, want none", rule.Burst)
	}
}

// TestReportPolicyKeysCarryOnlyTheAccountAndStayOpaque proves the key exposes
// no Account identifier and separates Students.
func TestReportPolicyKeysCarryOnlyTheAccountAndStayOpaque(t *testing.T) {
	limiter := reportPolicyLimiter(t, &scriptedStore{allowed: true}, time.Unix(1_800_000_000, 0).UTC())
	policy := ProtectedLearningReportPolicy()

	first, _, ok := limiter.entries(policy, Input{Identifier: reportPolicyStudentA})
	if !ok || len(first) != 1 {
		t.Fatalf("report entries = %+v ok=%t", first, ok)
	}
	again, _, _ := limiter.entries(policy, Input{Identifier: reportPolicyStudentA})
	other, _, _ := limiter.entries(policy, Input{Identifier: reportPolicyStudentB})

	if first[0].Key != again[0].Key {
		t.Fatal("the same Student produced two different keys")
	}
	if first[0].Key == other[0].Key {
		t.Fatal("two Students shared one report quota key")
	}
	if !strings.HasPrefix(first[0].Key, "gradex:rl:learning-report-v1:identifier:") {
		t.Fatalf("report key escaped the limiter namespace: %q", first[0].Key)
	}
	if strings.Contains(first[0].Key, reportPolicyStudentA) {
		t.Fatalf("report key exposed the Account identifier: %q", first[0].Key)
	}
	if first[0].Limit != ProtectedLearningReportsPerHour || first[0].Window != time.Hour {
		t.Fatalf("report entry = %+v", first[0])
	}

	// An absent identity cannot be keyed, so no decision is possible.
	if _, _, ok := limiter.entries(policy, Input{}); ok {
		t.Fatal("the report policy produced a key without an authenticated Student")
	}
}

// TestReportPolicyDistributedDecisionsCarryTheWindowAsRetryAfter covers the
// Redis-backed path's outcomes without Redis.
func TestReportPolicyDistributedDecisionsCarryTheWindowAsRetryAfter(t *testing.T) {
	policy := ProtectedLearningReportPolicy()
	now := time.Unix(1_800_000_000, 0).UTC()

	allowed := reportPolicyLimiter(t, &scriptedStore{allowed: true}, now).
		Decide(context.Background(), policy, Input{Identifier: reportPolicyStudentA})
	if !allowed.Allowed || allowed.Outcome != OutcomeAllowed {
		t.Fatalf("admitted decision = %+v", allowed)
	}

	denied := reportPolicyLimiter(t, &scriptedStore{allowed: false}, now).
		Decide(context.Background(), policy, Input{Identifier: reportPolicyStudentA})
	if denied.Allowed || denied.Outcome != OutcomeDenied {
		t.Fatalf("refused decision = %+v", denied)
	}
	if denied.RetryAfter != time.Hour {
		t.Fatalf("Retry-After = %s, want the policy window", denied.RetryAfter)
	}
}

// TestReportPolicyLocalFallbackEnforcesTheSameQuotaAndResets is the local
// parity evidence: the identical five, the identical window, per Student, with
// a deterministic reset. It also proves an unavailable Redis narrows the quota
// to one process rather than removing it.
func TestReportPolicyLocalFallbackEnforcesTheSameQuotaAndResets(t *testing.T) {
	now := time.Date(2026, time.August, 4, 9, 0, 0, 0, time.UTC)
	limiter := reportPolicyLimiter(t, &scriptedStore{err: errors.New("redis unavailable")}, now)
	limiter.now = func() time.Time { return now }
	limiter.local.now = func() time.Time { return now }
	policy := ProtectedLearningReportPolicy()

	decide := func(student string) Decision {
		return limiter.Decide(context.Background(), policy, Input{Identifier: student})
	}

	for attempt := int64(1); attempt <= ProtectedLearningReportsPerHour; attempt++ {
		decision := decide(reportPolicyStudentA)
		if !decision.Allowed || decision.Outcome != OutcomeFallbackAllowed {
			t.Fatalf("attempt %d = %+v, want a fallback allow", attempt, decision)
		}
	}

	// The first refusal, and every refusal after it inside the window.
	for attempt := 0; attempt < 3; attempt++ {
		decision := decide(reportPolicyStudentA)
		if decision.Allowed || decision.Outcome != OutcomeFallbackDenied {
			t.Fatalf("over-quota attempt %d = %+v, want a fallback deny", attempt, decision)
		}
		if decision.RetryAfter != time.Hour {
			t.Fatalf("Retry-After = %s, want the policy window", decision.RetryAfter)
		}
	}

	// Another Student is untouched by the first one's exhausted quota.
	if decision := decide(reportPolicyStudentB); !decision.Allowed {
		t.Fatalf("second Student decision = %+v, want an independent quota", decision)
	}

	// Just inside the window the refusal stands.
	now = now.Add(time.Hour - time.Second)
	if decision := decide(reportPolicyStudentA); decision.Allowed {
		t.Fatal("quota reset before the authoritative window elapsed")
	}

	// Past it, the window resets. No real hour is waited.
	now = now.Add(2 * time.Second)
	if decision := decide(reportPolicyStudentA); !decision.Allowed {
		t.Fatalf("decision after the window = %+v, want an admitted request", decision)
	}
}

// TestReportPolicyLocalStateIsBoundedAndReclaimed proves the fallback does not
// accumulate one permanent entry per Student.
func TestReportPolicyLocalStateIsBoundedAndReclaimed(t *testing.T) {
	now := time.Date(2026, time.August, 4, 9, 0, 0, 0, time.UTC)
	limiter := reportPolicyLimiter(t, &scriptedStore{err: errors.New("redis unavailable")}, now)
	limiter.now = func() time.Time { return now }
	limiter.local.now = func() time.Time { return now }
	policy := ProtectedLearningReportPolicy()

	for i := 0; i < 50; i++ {
		limiter.Decide(context.Background(), policy, Input{Identifier: reportPolicyStudentA[:35] + string(rune('a'+i%26))})
	}
	limiter.local.mu.Lock()
	held := len(limiter.local.entries)
	limiter.local.mu.Unlock()
	if held == 0 {
		t.Fatal("the local fallback recorded no state at all")
	}

	// Every entry expires with the window and is reclaimed on the next decision.
	now = now.Add(policy.Window + time.Second)
	limiter.Decide(context.Background(), policy, Input{Identifier: reportPolicyStudentB})
	limiter.local.mu.Lock()
	remaining := len(limiter.local.entries)
	limiter.local.mu.Unlock()
	if remaining != 1 {
		t.Fatalf("local entries after the window = %d, want only the newest", remaining)
	}
}

// TestReportPolicyLocalFallbackDoesNotOvershootUnderConcurrency proves the
// fallback admits exactly the quota when requests arrive together.
func TestReportPolicyLocalFallbackDoesNotOvershootUnderConcurrency(t *testing.T) {
	now := time.Date(2026, time.August, 4, 9, 0, 0, 0, time.UTC)
	limiter := reportPolicyLimiter(t, unavailableStore{}, now)
	policy := ProtectedLearningReportPolicy()

	const attempts = 40
	var wait sync.WaitGroup
	results := make([]bool, attempts)
	for i := 0; i < attempts; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			results[index] = limiter.Decide(context.Background(), policy, Input{Identifier: reportPolicyStudentA}).Allowed
		}(i)
	}
	wait.Wait()

	admitted := 0
	for _, allowed := range results {
		if allowed {
			admitted++
		}
	}
	if int64(admitted) != ProtectedLearningReportsPerHour {
		t.Fatalf("concurrent attempts admitted %d, want exactly %d", admitted, ProtectedLearningReportsPerHour)
	}
}

// TestReportPolicyRefusesWhenNoBackendCanDecide proves the throttle never
// silently admits: an exhausted local bound is unavailable, not permitted.
func TestReportPolicyRefusesWhenNoBackendCanDecide(t *testing.T) {
	now := time.Date(2026, time.August, 4, 9, 0, 0, 0, time.UTC)
	limiter := reportPolicyLimiter(t, &scriptedStore{err: errors.New("redis unavailable")}, now)
	policy := ProtectedLearningReportPolicy()
	policy.LocalMaxKeys = 1

	if decision := limiter.Decide(context.Background(), policy, Input{Identifier: reportPolicyStudentA}); !decision.Allowed {
		t.Fatalf("first decision = %+v", decision)
	}
	decision := limiter.Decide(context.Background(), policy, Input{Identifier: reportPolicyStudentB})
	if decision.Allowed || decision.Outcome != OutcomeUnavailable {
		t.Fatalf("decision beyond the local bound = %+v, want unavailable rather than allowed", decision)
	}
}
