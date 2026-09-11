//go:build integration

package playback

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// The lease is a concurrency control, so it is tested against a real Redis
// rather than a fake. A fake would happily implement the semantics this package
// is asserting and prove nothing about whether the Lua actually executes
// atomically, which is the only property that matters here.

const (
	testAccount = "11111111-1111-4111-8111-111111111111"
	deviceA     = "aaaaaaaa-1111-4111-8111-aaaaaaaaaaaa"
	deviceB     = "bbbbbbbb-2222-4222-8222-bbbbbbbbbbbb"
)

func testRedisAddr() string {
	if addr := os.Getenv("TEST_REDIS_ADDR"); addr != "" {
		return addr
	}
	return "127.0.0.1:6379"
}

// newTestClient uses a dedicated Redis database so a run cannot disturb
// whatever else is using the default one.
func newTestClient(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: testRedisAddr(), DB: 14})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis is not reachable at %s: %v", testRedisAddr(), err)
	}
	t.Cleanup(func() {
		_ = client.Del(context.Background(), leaseKey(testAccount))
		_ = client.Close()
	})
	_ = client.Del(ctx, leaseKey(testAccount))
	return client
}

func testCoordinator(t *testing.T, client redis.Scripter, ttl, heartbeat time.Duration) *Coordinator {
	t.Helper()
	coordinator, err := NewCoordinator(client, Settings{TTL: ttl, HeartbeatInterval: heartbeat})
	if err != nil {
		t.Fatalf("building coordinator: %v", err)
	}
	return coordinator
}

func acquireLease(t *testing.T, c *Coordinator, device, lease, lesson string) (Acquisition, error) {
	t.Helper()
	return c.Acquire(context.Background(), Lease{
		LeaseID: lease, AccountID: testAccount, DeviceID: device,
		SessionID: "session-" + device, LessonID: lesson,
	})
}

// Requirement 12/13: one device acquires, the other is refused while that lease
// is alive. This is the account-sharing control itself.
func TestSecondDeviceIsRefusedWhileTheFirstHoldsTheLease(t *testing.T) {
	client := newTestClient(t)
	coordinator := testCoordinator(t, client, 75*time.Second, 25*time.Second)

	if _, err := acquireLease(t, coordinator, deviceA, "lease-a1", "lesson-1"); err != nil {
		t.Fatalf("device A could not acquire: %v", err)
	}
	_, err := acquireLease(t, coordinator, deviceB, "lease-b1", "lesson-2")
	if !errors.Is(err, ErrHeldByAnotherDevice) {
		t.Fatalf("device B error = %v, want ErrHeldByAnotherDevice", err)
	}
}

// Requirement 14: the holder renews its own lease.
func TestTheHolderRenewsItsOwnLease(t *testing.T) {
	client := newTestClient(t)
	coordinator := testCoordinator(t, client, 75*time.Second, 25*time.Second)

	acquisition, err := acquireLease(t, coordinator, deviceA, "lease-a1", "lesson-1")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	expiresAt, err := coordinator.Renew(context.Background(), testAccount, deviceA, "lease-a1")
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if !expiresAt.After(acquisition.Lease.IssuedAt) {
		t.Fatalf("renewed expiry %s is not after issue %s", expiresAt, acquisition.Lease.IssuedAt)
	}
}

// Requirement 15: once the lease lapses, the other device may start.
func TestTheOtherDeviceMayAcquireAfterTheLeaseExpires(t *testing.T) {
	client := newTestClient(t)
	// A one-second TTL keeps the test honest without making it slow: expiry is
	// Redis's own, not a clock this package controls.
	coordinator := testCoordinator(t, client, time.Second, 400*time.Millisecond)

	if _, err := acquireLease(t, coordinator, deviceA, "lease-a1", "lesson-1"); err != nil {
		t.Fatalf("device A acquire: %v", err)
	}
	if _, err := acquireLease(t, coordinator, deviceB, "lease-b1", "lesson-2"); !errors.Is(err, ErrHeldByAnotherDevice) {
		t.Fatalf("device B before expiry = %v, want a conflict", err)
	}
	time.Sleep(1300 * time.Millisecond)
	if _, err := acquireLease(t, coordinator, deviceB, "lease-b2", "lesson-2"); err != nil {
		t.Fatalf("device B after expiry: %v", err)
	}
}

// Requirement 16: simultaneous acquisition has exactly one winner. Run with
// -race, this is the assertion that the decision is genuinely atomic rather
// than a check followed by a set.
func TestSimultaneousAcquisitionHasExactlyOneWinner(t *testing.T) {
	client := newTestClient(t)
	coordinator := testCoordinator(t, client, 75*time.Second, 25*time.Second)

	const attempts = 24
	var wg sync.WaitGroup
	results := make([]error, attempts)
	start := make(chan struct{})
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			device, lease := deviceA, "lease-a"
			if index%2 == 1 {
				device, lease = deviceB, "lease-b"
			}
			<-start
			_, err := acquireLease(t, coordinator, device, lease+string(rune('A'+index)), "lesson-1")
			results[index] = err
		}(i)
	}
	close(start)
	wg.Wait()

	winners := map[string]int{}
	for index, err := range results {
		device := deviceA
		if index%2 == 1 {
			device = deviceB
		}
		switch {
		case err == nil:
			winners[device]++
		case errors.Is(err, ErrHeldByAnotherDevice):
		default:
			t.Fatalf("attempt %d failed unexpectedly: %v", index, err)
		}
	}
	// Exactly one *device* wins. Several attempts from that same device may
	// each succeed, because same-device replacement is deliberate; what must
	// never happen is both devices holding playback.
	if len(winners) != 1 {
		t.Fatalf("winning devices = %v, want exactly one", winners)
	}
}

// Requirement 17: two API instances are two clients over one Redis, and they
// must not each grant a lease.
func TestTwoInstancesCannotBothGrantPlayback(t *testing.T) {
	first := newTestClient(t)
	second := redis.NewClient(&redis.Options{Addr: testRedisAddr(), DB: 14})
	t.Cleanup(func() { _ = second.Close() })

	instanceOne := testCoordinator(t, first, 75*time.Second, 25*time.Second)
	instanceTwo := testCoordinator(t, second, 75*time.Second, 25*time.Second)

	if _, err := acquireLease(t, instanceOne, deviceA, "lease-a1", "lesson-1"); err != nil {
		t.Fatalf("instance one acquire: %v", err)
	}
	if _, err := acquireLease(t, instanceTwo, deviceB, "lease-b1", "lesson-2"); !errors.Is(err, ErrHeldByAnotherDevice) {
		t.Fatalf("instance two = %v, want a conflict across instances", err)
	}
	// And the second instance can see the first instance's lease as valid.
	if err := instanceTwo.Validate(context.Background(), testAccount, deviceA, "lease-a1"); err != nil {
		t.Fatalf("instance two validating instance one's lease: %v", err)
	}
}

// Requirement 5: a second tab on the same device replaces the first lease and
// is never told the account is playing "on another device".
func TestSameDeviceSecondPlaybackReplacesTheFirstLease(t *testing.T) {
	client := newTestClient(t)
	coordinator := testCoordinator(t, client, 75*time.Second, 25*time.Second)

	if _, err := acquireLease(t, coordinator, deviceA, "lease-a1", "lesson-1"); err != nil {
		t.Fatalf("first tab: %v", err)
	}
	acquisition, err := acquireLease(t, coordinator, deviceA, "lease-a2", "lesson-2")
	if err != nil {
		t.Fatalf("second tab on the same device was refused: %v", err)
	}
	if !acquisition.ReplacedSameDevice {
		t.Fatal("the second tab did not report replacing the first lease")
	}
	if acquisition.ReplacedLeaseID != "lease-a1" {
		t.Fatalf("replaced lease = %q, want lease-a1", acquisition.ReplacedLeaseID)
	}
	// And the first tab now discovers it is stale rather than keeping the slot.
	if err := coordinator.Validate(context.Background(), testAccount, deviceA, "lease-a1"); !errors.Is(err, ErrLeaseNotHeld) {
		t.Fatalf("superseded lease validation = %v, want ErrLeaseNotHeld", err)
	}
}

// Requirement 3: an old lease id on the same device cannot renew the newer one.
func TestAnOldLeaseCannotRenewANewerLeaseOnTheSameDevice(t *testing.T) {
	client := newTestClient(t)
	coordinator := testCoordinator(t, client, 75*time.Second, 25*time.Second)

	if _, err := acquireLease(t, coordinator, deviceA, "lease-a1", "lesson-1"); err != nil {
		t.Fatalf("first tab: %v", err)
	}
	if _, err := acquireLease(t, coordinator, deviceA, "lease-a2", "lesson-2"); err != nil {
		t.Fatalf("second tab: %v", err)
	}
	if _, err := coordinator.Renew(context.Background(), testAccount, deviceA, "lease-a1"); !errors.Is(err, ErrLeaseNotHeld) {
		t.Fatalf("stale renew = %v, want ErrLeaseNotHeld", err)
	}
	// The live lease is untouched by the stale attempt.
	if err := coordinator.Validate(context.Background(), testAccount, deviceA, "lease-a2"); err != nil {
		t.Fatalf("live lease after a stale renew: %v", err)
	}
}

// Requirement 3: and it cannot release it either, which is the more damaging of
// the two — a closed tab handing away the slot the Student is actually using.
func TestAnOldLeaseCannotReleaseANewerLease(t *testing.T) {
	client := newTestClient(t)
	coordinator := testCoordinator(t, client, 75*time.Second, 25*time.Second)

	if _, err := acquireLease(t, coordinator, deviceA, "lease-a1", "lesson-1"); err != nil {
		t.Fatalf("first tab: %v", err)
	}
	if _, err := acquireLease(t, coordinator, deviceA, "lease-a2", "lesson-2"); err != nil {
		t.Fatalf("second tab: %v", err)
	}
	if err := coordinator.Release(context.Background(), testAccount, deviceA, "lease-a1"); err != nil {
		t.Fatalf("stale release should be a no-op, got: %v", err)
	}
	if err := coordinator.Validate(context.Background(), testAccount, deviceA, "lease-a2"); err != nil {
		t.Fatalf("the live lease was released by a stale caller: %v", err)
	}
}

// Requirement 4: validation never creates a lease. A stale authorization must
// not be able to resurrect itself by asking for a manifest.
func TestValidationNeverRecreatesALease(t *testing.T) {
	client := newTestClient(t)
	coordinator := testCoordinator(t, client, 75*time.Second, 25*time.Second)

	if err := coordinator.Validate(context.Background(), testAccount, deviceA, "lease-ghost"); !errors.Is(err, ErrLeaseNotHeld) {
		t.Fatalf("validating a nonexistent lease = %v, want ErrLeaseNotHeld", err)
	}
	exists, err := client.Exists(context.Background(), leaseKey(testAccount)).Result()
	if err != nil {
		t.Fatalf("reading the lease key: %v", err)
	}
	if exists != 0 {
		t.Fatal("validating a nonexistent lease created one")
	}
	// The other device is therefore still free to start.
	if _, err := acquireLease(t, coordinator, deviceB, "lease-b1", "lesson-2"); err != nil {
		t.Fatalf("device B after a ghost validation: %v", err)
	}
}

// Requirement 4: validation does not extend the TTL either, so polling
// manifests cannot hold the slot without playing anything.
func TestValidationDoesNotExtendTheLease(t *testing.T) {
	client := newTestClient(t)
	coordinator := testCoordinator(t, client, 2*time.Second, 900*time.Millisecond)

	if _, err := acquireLease(t, coordinator, deviceA, "lease-a1", "lesson-1"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	before, err := client.PTTL(context.Background(), leaseKey(testAccount)).Result()
	if err != nil {
		t.Fatalf("reading TTL: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if err := coordinator.Validate(context.Background(), testAccount, deviceA, "lease-a1"); err != nil {
		t.Fatalf("validate: %v", err)
	}
	after, err := client.PTTL(context.Background(), leaseKey(testAccount)).Result()
	if err != nil {
		t.Fatalf("reading TTL: %v", err)
	}
	if after >= before {
		t.Fatalf("TTL after validation = %s, want less than %s", after, before)
	}
}

// Requirement 8: revoking a device drops its lease and only its lease.
func TestReleasingADeviceLeavesAnotherDevicesLeaseAlone(t *testing.T) {
	client := newTestClient(t)
	coordinator := testCoordinator(t, client, 75*time.Second, 25*time.Second)

	if _, err := acquireLease(t, coordinator, deviceA, "lease-a1", "lesson-1"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	// Revoking the device that does *not* hold the lease changes nothing.
	if err := coordinator.ReleaseDevice(context.Background(), testAccount, deviceB); err != nil {
		t.Fatalf("releasing a non-holding device: %v", err)
	}
	if err := coordinator.Validate(context.Background(), testAccount, deviceA, "lease-a1"); err != nil {
		t.Fatalf("device A's lease was dropped by device B's revocation: %v", err)
	}
	// Revoking the holder frees the slot immediately.
	if err := coordinator.ReleaseDevice(context.Background(), testAccount, deviceA); err != nil {
		t.Fatalf("releasing the holding device: %v", err)
	}
	if _, err := acquireLease(t, coordinator, deviceB, "lease-b1", "lesson-2"); err != nil {
		t.Fatalf("device B after the holder was revoked: %v", err)
	}
}

// Requirement 7: an unreachable Redis fails closed. Nothing here may fall back
// to a process-local decision, because a second API instance would make a
// different one.
func TestCoordinationFailsClosedWhenRedisIsUnreachable(t *testing.T) {
	// A port nothing is listening on, with a short dial timeout so the test
	// fails fast rather than hanging.
	broken := redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:1", DialTimeout: 250 * time.Millisecond,
		ReadTimeout: 250 * time.Millisecond, MaxRetries: -1,
	})
	t.Cleanup(func() { _ = broken.Close() })
	coordinator := testCoordinator(t, broken, 75*time.Second, 25*time.Second)

	if _, err := acquireLease(t, coordinator, deviceA, "lease-a1", "lesson-1"); !errors.Is(err, ErrCoordinatorUnavailable) {
		t.Fatalf("acquire with Redis down = %v, want ErrCoordinatorUnavailable", err)
	}
	if err := coordinator.Validate(context.Background(), testAccount, deviceA, "lease-a1"); !errors.Is(err, ErrCoordinatorUnavailable) {
		t.Fatalf("validate with Redis down = %v, want ErrCoordinatorUnavailable", err)
	}
	if _, err := coordinator.Renew(context.Background(), testAccount, deviceA, "lease-a1"); !errors.Is(err, ErrCoordinatorUnavailable) {
		t.Fatalf("renew with Redis down = %v, want ErrCoordinatorUnavailable", err)
	}
}

// A released lease frees the slot for the other device without waiting out the
// TTL, which is what makes "stop watching here, start there" immediate.
func TestReleasingTheLeaseFreesTheSlotImmediately(t *testing.T) {
	client := newTestClient(t)
	coordinator := testCoordinator(t, client, 75*time.Second, 25*time.Second)

	if _, err := acquireLease(t, coordinator, deviceA, "lease-a1", "lesson-1"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := coordinator.Release(context.Background(), testAccount, deviceA, "lease-a1"); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := acquireLease(t, coordinator, deviceB, "lease-b1", "lesson-2"); err != nil {
		t.Fatalf("device B after an explicit release: %v", err)
	}
}
