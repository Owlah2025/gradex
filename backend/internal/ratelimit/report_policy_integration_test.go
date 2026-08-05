//go:build integration

package ratelimit

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// T064 Redis parity: the production backend must enforce the same five per hour
// per Student the local fallback does. Keys are unique per run and deleted
// afterwards — no shared Redis state is flushed.

func reportRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("Redis is required for integration test: %v", err)
	}
	return client
}

// reportRedisLimiter binds the production policy to real Redis under an HMAC
// key unique to this run, so its derived keys cannot collide with any other.
func reportRedisLimiter(t *testing.T, client *redis.Client) (*Limiter, Policy) {
	t.Helper()
	unique := fmt.Sprintf("%d", time.Now().UnixNano())
	limiter, err := New(NewRedisStore(client), []byte(strings.Repeat("k", 24)+unique[:8]), 5*time.Second)
	if err != nil {
		t.Fatalf("constructing Redis report limiter: %v", err)
	}
	return limiter, ProtectedLearningReportPolicy()
}

func reportRedisCleanup(t *testing.T, client *redis.Client, limiter *Limiter, policy Policy, students ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, student := range students {
			entries, _, ok := limiter.entries(policy, Input{Identifier: student})
			if !ok {
				continue
			}
			for _, entry := range entries {
				_ = client.Del(context.Background(), entry.Key).Err()
			}
		}
	})
}

// TestRedisReportPolicyEnforcesFivePerHourPerStudent is the production-backend
// threshold, the TTL, and Student isolation in one pass.
func TestRedisReportPolicyEnforcesFivePerHourPerStudent(t *testing.T) {
	client := reportRedisClient(t)
	limiter, policy := reportRedisLimiter(t, client)
	ctx := context.Background()
	studentA := "redis-report-a-" + fmt.Sprintf("%d", time.Now().UnixNano())
	studentB := "redis-report-b-" + fmt.Sprintf("%d", time.Now().UnixNano())
	reportRedisCleanup(t, client, limiter, policy, studentA, studentB)

	for attempt := int64(1); attempt <= ProtectedLearningReportsPerHour; attempt++ {
		decision := limiter.Decide(ctx, policy, Input{Identifier: studentA})
		if !decision.Allowed || decision.Outcome != OutcomeAllowed {
			t.Fatalf("Redis attempt %d = %+v, want a distributed allow", attempt, decision)
		}
	}
	over := limiter.Decide(ctx, policy, Input{Identifier: studentA})
	if over.Allowed || over.Outcome != OutcomeDenied {
		t.Fatalf("over-quota Redis decision = %+v, want a distributed deny", over)
	}
	if over.RetryAfter != policy.Window {
		t.Fatalf("Retry-After = %s, want the policy window", over.RetryAfter)
	}

	// A second Student holds a separate key and a fresh quota.
	if decision := limiter.Decide(ctx, policy, Input{Identifier: studentB}); !decision.Allowed {
		t.Fatalf("second Student Redis decision = %+v, want an independent quota", decision)
	}

	entries, _, ok := limiter.entries(policy, Input{Identifier: studentA})
	if !ok || len(entries) != 1 {
		t.Fatalf("report entries = %+v", entries)
	}
	key := entries[0].Key
	ttl, err := client.PTTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("reading report key TTL: %v", err)
	}
	// The window is set on first increment and never extended by refusals.
	if ttl <= 0 || ttl > policy.Window {
		t.Fatalf("report key TTL = %s, want a bounded value inside the %s window", ttl, policy.Window)
	}
	if ttl < policy.Window-time.Minute {
		t.Fatalf("report key TTL = %s, want approximately the %s window", ttl, policy.Window)
	}

	// The stored key names nothing: not the Student, not a Course, not a target.
	if !strings.HasPrefix(key, "gradex:rl:learning-report-v1:identifier:") {
		t.Fatalf("report key escaped the limiter namespace: %q", key)
	}
	if strings.Contains(key, studentA) {
		t.Fatalf("report key exposed the Account: %q", key)
	}
	if len(key) > 160 {
		t.Fatalf("report key is unbounded at %d bytes", len(key))
	}
}

// TestRedisReportPolicyDoesNotOvershootUnderConcurrency proves the atomic
// script admits exactly the quota when submissions race.
func TestRedisReportPolicyDoesNotOvershootUnderConcurrency(t *testing.T) {
	client := reportRedisClient(t)
	limiter, policy := reportRedisLimiter(t, client)
	ctx := context.Background()
	student := "redis-report-race-" + fmt.Sprintf("%d", time.Now().UnixNano())
	reportRedisCleanup(t, client, limiter, policy, student)

	const attempts = 25
	var wait sync.WaitGroup
	results := make([]bool, attempts)
	for i := 0; i < attempts; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			results[index] = limiter.Decide(ctx, policy, Input{Identifier: student}).Allowed
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
		t.Fatalf("concurrent Redis attempts admitted %d, want exactly %d", admitted, ProtectedLearningReportsPerHour)
	}

	// Refusals increment the counter but must not extend the window.
	entries, _, _ := limiter.entries(policy, Input{Identifier: student})
	ttl, err := client.PTTL(ctx, entries[0].Key).Result()
	if err != nil {
		t.Fatalf("reading report key TTL: %v", err)
	}
	if ttl <= 0 || ttl > policy.Window {
		t.Fatalf("report key TTL = %s after refusals, want the original window", ttl)
	}
}
