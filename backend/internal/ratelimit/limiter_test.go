package ratelimit

import (
	"context"
	"errors"
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
