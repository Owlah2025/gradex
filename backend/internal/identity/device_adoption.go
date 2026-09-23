package identity

import (
	"context"
	"fmt"
	"time"
)

// Historical unbound sessions keep their stored state so authorization fails
// closed. The controlled device cutover revokes active legacy families before
// the automatic-admission API is enabled; no adoption route is mounted.

// ResolveTrustState answers what the device state of one live session is,
// re-derived from the device record rather than trusted from the session row.
//
// Re-deriving is what makes device revocation take effect on the next request:
// the session row still names the device it was bound to, and this read
// notices the device is no longer live.
func (s *DeviceService) ResolveTrustState(
	ctx context.Context,
	sessionID string,
) (SessionDeviceTrust, string, error) {
	var state SessionDeviceTrust
	var deviceID *string
	var deviceLive *bool
	err := s.pool.QueryRow(ctx,
		`SELECT s.device_trust_state::text, s.trusted_device_id::text,
		        (d.id IS NOT NULL AND d.revoked_at IS NULL AND d.trusted_at IS NOT NULL)
		   FROM sessions s
		   LEFT JOIN identity_trusted_devices d ON d.id = s.trusted_device_id
		  WHERE s.id = $1::uuid`,
		sessionID,
	).Scan(&state, &deviceID, &deviceLive)
	if err != nil {
		return "", "", fmt.Errorf("resolving session device state: %w", err)
	}
	if state == DeviceTrustEstablished && (deviceLive == nil || !*deviceLive) {
		// The binding survives on the row but the device does not. Fail closed
		// to pending rather than to trusted.
		return DeviceTrustPending, "", nil
	}
	if deviceID == nil {
		return state, "", nil
	}
	return state, *deviceID, nil
}

// TouchSessionDevice refreshes the last-active stamp the Devices screen shows.
// It is best-effort and never blocks a request: a device whose stamp is a few
// minutes stale is a cosmetic problem, and a failed write here must not turn
// into a failed page load.
func (s *DeviceService) TouchSessionDevice(ctx context.Context, deviceID string, now time.Time) {
	if deviceID == "" {
		return
	}
	_, _ = s.pool.Exec(ctx,
		`UPDATE identity_trusted_devices
		    SET last_seen_at = $2, updated_at = $2
		  WHERE id = $1::uuid AND revoked_at IS NULL AND last_seen_at < $2 - interval '1 minute'`,
		deviceID, now,
	)
}
