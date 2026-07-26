//go:build integration

package ratelimit

import (
	"context"
	"fmt"
	"strings"
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
