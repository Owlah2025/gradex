//go:build integration

package catalog

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/entitlement"
	"github.com/Owlah2025/gradex/backend/internal/media"
	"github.com/Owlah2025/gradex/backend/internal/playback"
)

// The D-105 reachability tests assert what a Student and an Admin reviewer can
// actually do once a revision carrying a PLAYABLE video goes live, so they need
// the real delivery service reading the rows the real catalog lifecycle wrote.
// Entitlement is the production evaluator over the production tables; only the
// two purely infrastructural collaborators — object signing and the
// cross-instance playback lease — are stood in for, because neither takes part
// in the readiness decision under test.

type streamReadySigningStore struct{}

func (streamReadySigningStore) PresignGetURL(_ context.Context, key string, lifetime time.Duration) (string, error) {
	return "https://signed.invalid/" + key + "?ttl=" + lifetime.String(), nil
}

func (streamReadySigningStore) DownloadObject(_ context.Context, key string) ([]byte, error) {
	// Rendition manifests are rewritten from this text; the master is generated
	// from persisted rendition rows and never downloaded.
	return []byte("#EXTM3U\n#EXT-X-ENDLIST\n"), nil
}

// streamReadyPlaybackCoordinator is an in-process stand-in for the Redis-backed
// one-video-per-account authority. It keeps the real semantics the delivery
// code depends on — a lease exists only after Acquire, and Validate refuses one
// that was replaced — without requiring Redis in this suite.
type streamReadyPlaybackCoordinator struct {
	mu     sync.Mutex
	leases map[string]playback.Lease
}

func newStreamReadyPlaybackCoordinator() *streamReadyPlaybackCoordinator {
	return &streamReadyPlaybackCoordinator{leases: map[string]playback.Lease{}}
}

func (c *streamReadyPlaybackCoordinator) key(accountID, deviceID string) string {
	return accountID + "\x00" + deviceID
}

func (c *streamReadyPlaybackCoordinator) Acquire(_ context.Context, lease playback.Lease) (playback.Acquisition, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UTC()
	lease.IssuedAt, lease.LastHeartbeatAt = now, now
	lease.ExpiresAt = now.Add(c.Settings().TTL)
	key := c.key(lease.AccountID, lease.DeviceID)
	previous, replaced := c.leases[key]
	c.leases[key] = lease
	return playback.Acquisition{
		Lease: lease, ReplacedSameDevice: replaced, ReplacedLeaseID: previous.LeaseID,
	}, nil
}

func (c *streamReadyPlaybackCoordinator) Validate(_ context.Context, accountID, deviceID, leaseID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	lease, ok := c.leases[c.key(accountID, deviceID)]
	if !ok || lease.LeaseID != leaseID {
		return playback.ErrLeaseNotHeld
	}
	return nil
}

func (c *streamReadyPlaybackCoordinator) Renew(ctx context.Context, accountID, deviceID, leaseID string) (time.Time, error) {
	if err := c.Validate(ctx, accountID, deviceID, leaseID); err != nil {
		return time.Time{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	lease := c.leases[c.key(accountID, deviceID)]
	lease.LastHeartbeatAt = time.Now().UTC()
	lease.ExpiresAt = lease.LastHeartbeatAt.Add(c.Settings().TTL)
	c.leases[c.key(accountID, deviceID)] = lease
	return lease.ExpiresAt, nil
}

func (c *streamReadyPlaybackCoordinator) Release(ctx context.Context, accountID, deviceID, leaseID string) error {
	if err := c.Validate(ctx, accountID, deviceID, leaseID); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.leases, c.key(accountID, deviceID))
	return nil
}

func (c *streamReadyPlaybackCoordinator) Settings() playback.Settings {
	return playback.Settings{TTL: 5 * time.Minute, HeartbeatInterval: time.Minute}
}

func streamReadyDeliveryService(t *testing.T, f *d5Fixture) *media.DeliveryService {
	t.Helper()
	repository, err := entitlement.NewRepository(f.p)
	if err != nil {
		t.Fatalf("entitlement.NewRepository: %v", err)
	}
	evaluator, err := entitlement.NewEvaluator(repository)
	if err != nil {
		t.Fatalf("entitlement.NewEvaluator: %v", err)
	}
	delivery, err := media.NewDeliveryService(media.DeliveryOptions{
		DB: f.p, Store: streamReadySigningStore{}, Evaluator: evaluator,
		SignatureLifetime: 10 * time.Minute,
		BuyerTagKey:       bytes.Repeat([]byte{0x7a}, 32),
		Playback:          newStreamReadyPlaybackCoordinator(),
	})
	if err != nil {
		t.Fatalf("media.NewDeliveryService: %v", err)
	}
	return delivery
}
