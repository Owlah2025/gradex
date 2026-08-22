package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type scriptedStore struct {
	allowed bool
	err     error
	entries []Entry
}

func (s *scriptedStore) Decide(_ context.Context, entries []Entry) (bool, error) {
	s.entries = append([]Entry(nil), entries...)
	return s.allowed, s.err
}

func admissionPolicy() Policy {
	return Policy{
		ID:       "student-registration-v1",
		Category: "PUBLIC_IDENTITY",
		Endpoint: "student-registrations",
		Window:   time.Minute,
		Rules: []Rule{
			{Dimension: DimensionEndpoint, Limit: 50, LocalLimit: 5},
			{Dimension: DimensionIdentifier, Limit: 5, LocalLimit: 1},
			{Dimension: DimensionNetwork, Limit: 20, LocalLimit: 2},
			{Dimension: DimensionAnonymous, Limit: 5, LocalLimit: 1},
			{Dimension: DimensionGlobal, Limit: 100, LocalLimit: 10},
		},
		LocalMaxKeys: 64,
	}
}

// FR-014: every configured dimension is represented by an opaque, versioned
// key. Neither a submitted identifier nor the anonymous value may be present.
func TestLimiterDerivesOpaqueKeysForEveryPolicyDimension(t *testing.T) {
	store := &scriptedStore{allowed: true}
	limiter, err := New(store, []byte(strings.Repeat("k", 32)), 50*time.Millisecond)
	if err != nil {
		t.Fatalf("constructing limiter: %v", err)
	}
	input := Input{
		Identifier:  "student@example.com",
		ClientIP:    "192.0.2.42",
		AnonymousID: "anonymous-canary",
	}

	decision := limiter.Decide(context.Background(), admissionPolicy(), input)
	if !decision.Allowed || decision.Outcome != OutcomeAllowed {
		t.Fatalf("decision = %+v, want distributed allow", decision)
	}
	if len(store.entries) != 5 {
		t.Fatalf("entry count = %d, want 5", len(store.entries))
	}
	for _, entry := range store.entries {
		if !strings.HasPrefix(entry.Key, "gradex:rl:student-registration-v1:") {
			t.Errorf("key is not versioned/namespaced: %q", entry.Key)
		}
		for _, secret := range []string{input.Identifier, input.ClientIP, input.AnonymousID} {
			if strings.Contains(entry.Key, secret) {
				t.Errorf("key %q contains private dimension %q", entry.Key, secret)
			}
		}
	}
}

// FR-014: a real quota denial is distinguishable from dependency failure.
func TestLimiterReturnsTrueDenyOnlyAfterDistributedPolicyEvaluation(t *testing.T) {
	store := &scriptedStore{allowed: false}
	limiter, err := New(store, []byte(strings.Repeat("k", 32)), time.Second)
	if err != nil {
		t.Fatalf("constructing limiter: %v", err)
	}
	decision := limiter.Decide(context.Background(), admissionPolicy(), Input{
		Identifier: "student@example.com", ClientIP: "192.0.2.42", AnonymousID: "anon",
	})
	if decision.Allowed || decision.Outcome != OutcomeDenied || decision.RetryAfter <= 0 {
		t.Fatalf("decision = %+v, want quota deny with safe retry", decision)
	}
}

// FR-014: Redis failure uses only the strict bounded fallback; it never
// fabricates a distributed quota denial.
func TestLimiterUsesStrictLocalFallbackAndFailsClosedWhenItCannotDecide(t *testing.T) {
	store := &scriptedStore{err: errors.New("redis unavailable")}
	limiter, err := New(store, []byte(strings.Repeat("k", 32)), time.Second)
	if err != nil {
		t.Fatalf("constructing limiter: %v", err)
	}
	policy := admissionPolicy()
	input := Input{Identifier: "student@example.com", ClientIP: "192.0.2.42", AnonymousID: "anon"}

	first := limiter.Decide(context.Background(), policy, input)
	if !first.Allowed || first.Outcome != OutcomeFallbackAllowed {
		t.Fatalf("first fallback decision = %+v", first)
	}
	second := limiter.Decide(context.Background(), policy, input)
	if second.Allowed || second.Outcome != OutcomeFallbackDenied {
		t.Fatalf("second fallback decision = %+v", second)
	}

	policy.LocalMaxKeys = 1
	exhausted, err := New(store, []byte(strings.Repeat("q", 32)), time.Second)
	if err != nil {
		t.Fatalf("constructing bounded limiter: %v", err)
	}
	unavailable := exhausted.Decide(context.Background(), policy, input)
	if unavailable.Allowed || unavailable.Outcome != OutcomeUnavailable {
		t.Fatalf("bounded fallback decision = %+v, want unavailable", unavailable)
	}
}

func TestPolicyValidationRejectsUnsafeOrIncompleteDefinitions(t *testing.T) {
	tests := map[string]Policy{
		"unversioned ID":   {ID: "registration", Endpoint: "register", Window: time.Minute},
		"no window":        {ID: "registration-v1", Endpoint: "register"},
		"no dimensions":    {ID: "registration-v1", Endpoint: "register", Window: time.Minute},
		"weak local limit": {ID: "registration-v1", Endpoint: "register", Window: time.Minute, LocalMaxKeys: 1, Rules: []Rule{{Dimension: DimensionGlobal, Limit: 1, LocalLimit: 2}}},
	}
	for name, policy := range tests {
		t.Run(name, func(t *testing.T) {
			if err := policy.Validate(); err == nil {
				t.Fatal("unsafe policy was accepted")
			}
		})
	}
}

func TestProtectedLearningProgressPolicyBoundsOneStudentLessonPerMinute(t *testing.T) {
	policy := ProtectedLearningProgressPolicy()
	if err := policy.Validate(); err != nil {
		t.Fatalf("protected learning policy is invalid: %v", err)
	}
	if policy.Window != time.Minute || len(policy.Rules) != 1 ||
		policy.Rules[0].Dimension != DimensionIdentifier || policy.Rules[0].Limit != 12 {
		t.Fatalf("progress policy = %+v, want 12 writes/minute per stable student-lesson identifier", policy)
	}
}

func TestProgressSourcePolicyUsesStrictAddressTokenBucket(t *testing.T) {
	policy := ProtectedLearningProgressSourcePolicy()
	if err := policy.Validate(); err != nil {
		t.Fatalf("progress source policy is invalid: %v", err)
	}
	if !policy.FailClosed || policy.Window != time.Minute || len(policy.Rules) != 1 {
		t.Fatalf("progress source policy = %+v", policy)
	}
	rule := policy.Rules[0]
	if rule.Dimension != DimensionSourceAddr || rule.Limit != ProtectedLearningProgressSourceRequestsPerMinute || rule.Burst != ProtectedLearningProgressSourceBurst {
		t.Fatalf("progress source rule = %+v", rule)
	}
}

func TestPublicPreviewPolicyIsFailClosedAndDoesNotKeyOnCourseOrAssetInventory(t *testing.T) {
	policy := PublicPreviewPolicy()
	if err := policy.Validate(); err != nil {
		t.Fatalf("public preview policy is invalid: %v", err)
	}
	if !policy.FailClosed || policy.Endpoint != "public-preview" {
		t.Fatalf("public preview policy is not fail-closed: %+v", policy)
	}
	if len(policy.Rules) != 3 {
		t.Fatalf("public preview policy rules=%+v, want endpoint/source/global only", policy.Rules)
	}
	for _, rule := range policy.Rules {
		if rule.Dimension == DimensionIdentifier || rule.Dimension == DimensionAnonymous {
			t.Fatalf("public preview limiter must not turn Course or preview input into an inventory oracle: %+v", policy.Rules)
		}
	}
}

func TestPurchaseRequestsPolicyBindsNormalizedEmailAndSourceAddress(t *testing.T) {
	policy := PurchaseRequestsPolicy()
	if err := policy.Validate(); err != nil {
		t.Fatalf("purchase policy is invalid: %v", err)
	}
	if policy.ID != "purchase-requests-v1" || !policy.FailClosed {
		t.Fatalf("purchase policy identity = %+v", policy)
	}
	if _, ok := policyRule(policy, DimensionSourceAddr); !ok {
		t.Fatal("purchase policy has no source-address budget")
	}
	if _, ok := policyRule(policy, DimensionIdentifier); !ok {
		t.Fatal("purchase policy has no normalized-email budget")
	}
	store := &scriptedStore{err: errors.New("limiter unavailable")}
	limiter, err := New(store, []byte(strings.Repeat("p", 32)), time.Second)
	if err != nil {
		t.Fatalf("constructing limiter: %v", err)
	}
	// The live policy is fail-closed. This local proof isolates the bounded
	// quota algorithm while retaining the exact dimensions and limits.
	policy.FailClosed = false
	for attempt := 0; attempt < 3; attempt++ {
		decision := limiter.Decide(context.Background(), policy, Input{
			Identifier: fmt.Sprintf("buyer-%d@example.test", attempt), AnonymousID: fmt.Sprintf("browser-%d", attempt), ClientIP: "192.0.2.90",
		})
		if !decision.Allowed {
			t.Fatalf("source attempt %d denied before source budget: %+v", attempt+1, decision)
		}
	}
	if decision := limiter.Decide(context.Background(), policy, Input{Identifier: "buyer-four@example.test", AnonymousID: "browser-four", ClientIP: "192.0.2.90"}); decision.Allowed || decision.Outcome != OutcomeFallbackDenied {
		t.Fatalf("source abuse decision = %+v, want fallback deny", decision)
	}

	limiter, err = New(store, []byte(strings.Repeat("q", 32)), time.Second)
	if err != nil {
		t.Fatalf("constructing email limiter: %v", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		decision := limiter.Decide(context.Background(), policy, Input{
			Identifier: "same-buyer@example.test", AnonymousID: fmt.Sprintf("browser-%d", attempt), ClientIP: fmt.Sprintf("192.0.2.%d", attempt+1),
		})
		if !decision.Allowed {
			t.Fatalf("email attempt %d denied before identifier budget: %+v", attempt+1, decision)
		}
	}
	if decision := limiter.Decide(context.Background(), policy, Input{Identifier: "same-buyer@example.test", AnonymousID: "browser-three", ClientIP: "192.0.2.3"}); decision.Allowed || decision.Outcome != OutcomeFallbackDenied {
		t.Fatalf("normalized-email abuse decision = %+v, want fallback deny", decision)
	}
}

func TestProgressSourceBurstIsDeterministicAndLimiterFailureIsStrict(t *testing.T) {
	store := &scriptedStore{err: errors.New("redis unavailable")}
	limiter, err := New(store, []byte(strings.Repeat("s", 32)), time.Second)
	if err != nil {
		t.Fatalf("constructing limiter: %v", err)
	}
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	limiter.now = func() time.Time { return now }
	limiter.local.now = func() time.Time { return now }

	strict := ProtectedLearningProgressSourcePolicy()
	if decision := limiter.Decide(context.Background(), strict, Input{ClientIP: "192.0.2.7"}); decision.Outcome != OutcomeUnavailable {
		t.Fatalf("strict source failure decision = %+v, want unavailable", decision)
	}
	if decision := limiter.Decide(context.Background(), strict, Input{ClientIP: "invalid"}); decision.Outcome != OutcomeUnavailable {
		t.Fatalf("invalid source decision = %+v, want unavailable", decision)
	}

	strict.FailClosed = false
	for attempt := int64(0); attempt < ProtectedLearningProgressSourceBurst; attempt++ {
		decision := limiter.Decide(context.Background(), strict, Input{ClientIP: "192.0.2.7"})
		if !decision.Allowed || decision.Outcome != OutcomeFallbackAllowed {
			t.Fatalf("burst request %d decision = %+v, want fallback allow", attempt+1, decision)
		}
	}
	if decision := limiter.Decide(context.Background(), strict, Input{ClientIP: "192.0.2.7"}); decision.Allowed || decision.Outcome != OutcomeFallbackDenied {
		t.Fatalf("burst overflow decision = %+v, want fallback deny", decision)
	}
	now = now.Add(50 * time.Millisecond)
	if decision := limiter.Decide(context.Background(), strict, Input{ClientIP: "192.0.2.7"}); !decision.Allowed {
		t.Fatalf("one replenished token decision = %+v, want allow", decision)
	}
}

func TestSourceAddressNormalizesIPv4AndIPv6AsRequired(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"ipv4", "192.0.2.17", "192.0.2.17"},
		{"ipv4 mapped", "::ffff:192.0.2.17", "192.0.2.17"},
		{"ipv6 first host", "2001:db8:1:2::1", "2001:db8:1:2::/64"},
		{"ipv6 same network", "2001:0db8:0001:0002:ffff::1", "2001:db8:1:2::/64"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := sourceAddress(tc.raw)
			if !ok || got != tc.want {
				t.Fatalf("sourceAddress(%q) = %q, %v; want %q, true", tc.raw, got, ok, tc.want)
			}
		})
	}
	if _, ok := sourceAddress("not-an-address"); ok {
		t.Fatal("invalid source address was accepted")
	}
}

func TestProductionSessionPoliciesAdmitApprovedSameNATEnvelope(t *testing.T) {
	bootstrap := ProductionAnonymousBootstrapPolicy()
	login := ProductionLoginPolicy()
	for name, policy := range map[string]Policy{"bootstrap": bootstrap, "login": login} {
		if err := policy.Validate(); err != nil {
			t.Fatalf("%s policy: %v", name, err)
		}
		if sourceRule, ok := policyRule(policy, DimensionSourceAddr); !ok || sourceRule.Limit < 500 {
			t.Fatalf("%s source rule = %+v, present=%v; want at least 500/minute", name, sourceRule, ok)
		}
		if _, hasNetwork := policyRule(policy, DimensionNetwork); hasNetwork {
			t.Fatalf("%s policy retained the shared /24 network dimension", name)
		}
	}
	if !login.FailClosed {
		t.Fatal("production login must fail closed when distributed admission is unavailable")
	}
	if rule, _ := policyRule(login, DimensionIdentifier); rule.Limit != 6 {
		t.Fatalf("identifier limit = %d, want 6/minute", rule.Limit)
	}
	if rule, _ := policyRule(login, DimensionAnonymous); rule.Limit != 10 {
		t.Fatalf("anonymous limit = %d, want 10/minute", rule.Limit)
	}
	if rule, _ := policyRule(login, DimensionGlobal); rule.Limit != 600 {
		t.Fatalf("shared limit = %d, want 600/minute", rule.Limit)
	}
}

func TestProductionLoginAdmissionAllowsFiveHundredDistinctBrowsersFromOneIPv4(t *testing.T) {
	limiter, err := New(&scriptedStore{allowed: true}, []byte(strings.Repeat("p", 32)), time.Second)
	if err != nil {
		t.Fatalf("constructing limiter: %v", err)
	}
	policy := ProductionLoginPolicy()
	for student := 0; student < 500; student++ {
		_, local, ok := limiter.entries(policy, Input{
			Identifier:  fmt.Sprintf("student-%d@example.test", student),
			AnonymousID: fmt.Sprintf("browser-%d", student),
			ClientIP:    "192.0.2.44",
		})
		if !ok {
			t.Fatalf("student %d entries were not derived", student)
		}
		allowed, available := limiter.local.decide(local, policy.LocalMaxKeys)
		if !allowed || !available {
			t.Fatalf("student %d was rejected at admission", student+1)
		}
	}
}

func policyRule(policy Policy, dimension Dimension) (Rule, bool) {
	for _, rule := range policy.Rules {
		if rule.Dimension == dimension {
			return rule, true
		}
	}
	return Rule{}, false
}

func TestLocalConditionalChargingDoesNotDrainSharedCapacity(t *testing.T) {
	local := newLocalFallback()
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	local.now = func() time.Time { return now }
	identifier := Entry{Key: "identifier", Limit: 1, Window: time.Minute}
	shared := Entry{Key: "shared", Limit: 2, Window: time.Minute}

	if allowed, available := local.decide([]Entry{identifier, shared}, 10); !allowed || !available {
		t.Fatal("first request was not admitted")
	}
	if allowed, available := local.decide([]Entry{identifier, shared}, 10); allowed || !available {
		t.Fatal("identifier overflow was not denied")
	}
	for attempt := 0; attempt < 1; attempt++ {
		if allowed, available := local.decide([]Entry{{Key: "identifier-2", Limit: 1, Window: time.Minute}, shared}, 10); !allowed || !available {
			t.Fatal("denied identifier attempt drained shared capacity")
		}
	}
	third := Entry{Key: "identifier-3", Limit: 1, Window: time.Minute}
	if allowed, available := local.decide([]Entry{third, shared}, 10); allowed || !available {
		t.Fatal("shared overflow was not denied")
	}
	delete(local.entries, shared.Key)
	if allowed, available := local.decide([]Entry{third, shared}, 10); !allowed || !available {
		t.Fatal("shared denial consumed the unrelated identifier allowance")
	}
}
