package httpapi

import (
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Owlah2025/gradex/backend/internal/playback"
)

// testPlaybackCoordinator is the one-video-per-account authority these fixtures
// use.
//
// It is a real coordinator over a real Redis, not a stub, and deliberately so:
// protected playback now fails closed without one, and a stub that always said
// "yes" would quietly re-open exactly the concurrency hole the coordinator
// exists to close. Tests that are about something else — entitlement,
// revalidation, reporting — simply want a working slot, which this gives them.
//
// A dedicated Redis database keeps a run from disturbing anything else, and
// every fixture gets its own account keys anyway.
func testPlaybackCoordinator(t *testing.T) *playback.Coordinator {
	t.Helper()
	client := redis.NewClient(&redis.Options{
		Addr: testRedisAddr(), DB: 13,
		DialTimeout: 2 * time.Second,
	})
	t.Cleanup(func() { _ = client.Close() })
	coordinator, err := playback.NewCoordinator(client, playback.Settings{
		TTL: 75 * time.Second, HeartbeatInterval: 25 * time.Second,
	})
	if err != nil {
		t.Fatalf("constructing the test playback coordinator: %v", err)
	}
	return coordinator
}

func testRedisAddr() string { return "127.0.0.1:6379" }
