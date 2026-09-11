//go:build integration

package media

import (
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Owlah2025/gradex/backend/internal/playback"
)

// testPlaybackCoordinator gives the delivery fixtures a working
// one-video-per-account authority.
//
// A real coordinator over a real Redis rather than a permissive stub: protected
// playback now fails closed without one, and a stub that always granted a lease
// would quietly re-open the concurrency hole the coordinator exists to close.
// These fixtures are about entitlement, exact versions, and watermarks, and all
// they need from playback coordination is for it to work.
func testPlaybackCoordinator(t *testing.T) *playback.Coordinator {
	t.Helper()
	client := redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:6379", DB: 12, DialTimeout: 2 * time.Second,
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

// testDeviceID is the trusted device every delivery fixture plays from. Real,
// distinct per fixture Student, and never a credential.
func testDeviceID(studentID string) string { return "device-" + studentID }
