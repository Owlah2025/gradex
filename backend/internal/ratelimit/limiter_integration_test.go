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

// FR-014: the Redis decision checks and consumes all layered dimensions in one
// script, so two concurrent attempts cannot both claim the final capacity.
func TestRedisStoreMakesAtomicLayeredAllowDenyDecision(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("Redis is required for integration test: %v", err)
	}

	store := NewRedisStore(client)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	entries := []Entry{
		{Key: "gradex:rl:test-v1:endpoint:" + suffix, Limit: 1, Window: time.Minute},
		{Key: "gradex:rl:test-v1:global:" + suffix, Limit: 10, Window: time.Minute},
	}
	first, err := store.Decide(ctx, entries)
	if err != nil || !first {
		t.Fatalf("first decision = %v, %v; want allow", first, err)
	}
	second, err := store.Decide(ctx, entries)
	if err != nil {
		t.Fatalf("second decision: %v", err)
	}
	if second {
		t.Fatal("second decision allowed after endpoint quota was consumed")
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Key, "gradex:rl:") {
			t.Fatalf("test key escaped limiter namespace: %q", entry.Key)
		}
		t.Cleanup(func() { _ = client.Del(context.Background(), entry.Key).Err() })
	}
}

func TestRedisConditionalChargingDoesNotDrainSharedCapacity(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("Redis is required for integration test: %v", err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	identifier := Entry{Key: "gradex:rl:conditional-v1:identifier:" + suffix, Limit: 1, Window: time.Minute}
	shared := Entry{Key: "gradex:rl:conditional-v1:global:" + suffix, Limit: 2, Window: time.Minute}
	otherIdentifier := Entry{Key: "gradex:rl:conditional-v1:identifier-other:" + suffix, Limit: 1, Window: time.Minute}
	thirdIdentifier := Entry{Key: "gradex:rl:conditional-v1:identifier-third:" + suffix, Limit: 1, Window: time.Minute}
	store := NewRedisStore(client)
	decisions := []struct {
		entries []Entry
		want    bool
	}{
		{[]Entry{identifier, shared}, true},
		{[]Entry{identifier, shared}, false},
		{[]Entry{otherIdentifier, shared}, true},
	}
	for attempt, decision := range decisions {
		entries := decision.entries
		allowed, err := store.Decide(ctx, entries)
		if err != nil {
			t.Fatalf("conditional decision: %v", err)
		}
		if allowed != decision.want {
			t.Fatalf("decision %d = %v, want %v", attempt+1, allowed, decision.want)
		}
	}
	allowed, err := store.Decide(ctx, []Entry{thirdIdentifier, shared})
	if err != nil || allowed {
		t.Fatalf("shared overflow = %v, %v; want deny", allowed, err)
	}
	if err := client.Del(ctx, shared.Key).Err(); err != nil {
		t.Fatalf("resetting test-only shared key: %v", err)
	}
	allowed, err = store.Decide(ctx, []Entry{thirdIdentifier, shared})
	if err != nil || !allowed {
		t.Fatalf("shared denial consumed identifier allowance: %v, %v", allowed, err)
	}
	t.Cleanup(func() {
		_ = client.Del(context.Background(), identifier.Key, otherIdentifier.Key, thirdIdentifier.Key, shared.Key).Err()
	})
}

func TestRedisConditionalChargingIsAtomicUnderConcurrency(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("Redis is required for integration test: %v", err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	shared := Entry{Key: "gradex:rl:atomic-v1:global:" + suffix, Limit: 25, Window: time.Minute}
	store := NewRedisStore(client)
	var wg sync.WaitGroup
	type result struct {
		allowed bool
		err     error
	}
	results := make(chan result, 100)
	keys := make([]string, 0, 101)
	for attempt := 0; attempt < 100; attempt++ {
		identifierKey := fmt.Sprintf("gradex:rl:atomic-v1:identifier:%s:%d", suffix, attempt)
		keys = append(keys, identifierKey)
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			ok, err := store.Decide(ctx, []Entry{
				{Key: key, Limit: 1, Window: time.Minute},
				shared,
			})
			results <- result{allowed: ok, err: err}
		}(identifierKey)
	}
	wg.Wait()
	close(results)
	count := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent decision: %v", result.err)
		}
		if result.allowed {
			count++
		}
	}
	if count != 25 {
		t.Fatalf("allowed = %d, want exactly 25", count)
	}
	keys = append(keys, shared.Key)
	t.Cleanup(func() { _ = client.Del(context.Background(), keys...).Err() })
}

func TestRedisStoreHonorsTokenBucketBurst(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("Redis is required for integration test: %v", err)
	}

	key := "gradex:rl:token-burst-v1:source:" + fmt.Sprintf("%d", time.Now().UnixNano())
	entry := Entry{Key: key, Limit: 1, Window: time.Hour, Burst: 2}
	store := NewRedisStore(client)
	for attempt := 1; attempt <= 2; attempt++ {
		allowed, err := store.Decide(ctx, []Entry{entry})
		if err != nil || !allowed {
			t.Fatalf("token burst attempt %d = %v, %v; want allow", attempt, allowed, err)
		}
	}
	allowed, err := store.Decide(ctx, []Entry{entry})
	if err != nil || allowed {
		t.Fatalf("token burst overflow = %v, %v; want deny", allowed, err)
	}
	t.Cleanup(func() { _ = client.Del(context.Background(), key).Err() })
}
