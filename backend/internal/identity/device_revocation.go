package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// deviceRevocation is one device ending, whoever ended it.
type deviceRevocation struct {
	AccountID string
	DeviceID  string
	Reason    DeviceRevocationReason
	Revision  int
	RequestID string
	ActorID   string
	Now       time.Time
}

// revokeDeviceInTransaction ends one device and the session families that
// device created, and nothing else.
//
// The precision matters. Revoking a device must not touch the Student's other
// trusted device, and it must not move accounts.session_epoch — that counter is
// the global "everything stops now" control behind logout-all, suspension, and
// password reset, and spending it to remove one browser would log the Student
// out of the browser they are still using. So the revocation is expressed as
// exactly what it is: these families, by this device.
func (s *DeviceService) revokeDeviceInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	revocation deviceRevocation,
) (int, error) {
	if !revocation.Reason.Valid() {
		return 0, fmt.Errorf("%w: unknown revocation reason", ErrDeviceTrustUnavailable)
	}
	tag, err := tx.Exec(ctx,
		`UPDATE identity_trusted_devices
		    SET revoked_at = GREATEST(first_seen_at, $3),
		        revocation_reason = $4::trusted_device_revocation_reason,
		        updated_at = $3
		  WHERE id = $1::uuid AND account_id = $2::uuid AND revoked_at IS NULL`,
		revocation.DeviceID, revocation.AccountID, revocation.Now, string(revocation.Reason),
	)
	if err != nil {
		return 0, fmt.Errorf("revoking trusted device: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return 0, ErrDeviceUnknown
	}

	revoked, err := revokeSessionsForDevice(ctx, tx, revocation)
	if err != nil {
		return 0, err
	}
	if err := appendIdentitySecurityEvent(ctx, tx, securityEventAppend{
		eventType: deviceRevocationEventType(revocation.Reason),
		accountID: revocation.AccountID,
		revision:  revocation.Revision,
		requestID: revocation.RequestID,
		evidence: map[string]any{
			"schema_version":    1,
			"device_id":         revocation.DeviceID,
			"revocation_reason": string(revocation.Reason),
			"revoked_sessions":  revoked,
			// Present only for operator action, and only ever an Account id.
			// No device credential, session credential, or code appears here.
			"actor_account_id": revocation.ActorID,
		},
	}); err != nil {
		return 0, err
	}
	return revoked, nil
}

// deviceRevocationEventType separates operator action from the Student's own.
// A single DEVICE_REVOKED type would make "an Admin removed a device" and "the
// Student removed a device" indistinguishable in the trail, which is exactly
// the distinction an audit exists to preserve.
func deviceRevocationEventType(reason DeviceRevocationReason) string {
	switch reason {
	case DeviceRevokedByAdmin, DeviceRevokedAllByAdmin:
		return "ADMIN_DEVICE_REVOKED"
	default:
		return "DEVICE_REVOKED"
	}
}

// revokeSessionsForDevice ends exactly the families bound to one device.
//
// Families the same Student holds on their other device are untouched, and the
// Student is not logged out globally. The mechanism is the ordinary family
// revocation the session authority already uses, so a revoked family fails the
// same Usable() check as any other and needs no new code path at read time.
func revokeSessionsForDevice(ctx context.Context, tx pgx.Tx, revocation deviceRevocation) (int, error) {
	tag, err := tx.Exec(ctx,
		`UPDATE sessions
		    SET state = 'REVOKED',
		        revoked_at = $2,
		        revocation_reason = 'ADMIN_REVOKED'
		  WHERE trusted_device_id = $1::uuid AND state = 'ACTIVE'`,
		revocation.DeviceID, revocation.Now,
	)
	if err != nil {
		return 0, fmt.Errorf("revoking sessions for trusted device: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (s *DeviceService) releasePlayback(ctx context.Context, accountID, deviceID string) {
	if s.playback == nil {
		return
	}
	// A failure here is deliberately not fatal to a revocation that has already
	// committed. The lease carries its own TTL and the device is refused at the
	// next authorization regardless, so the worst case is that the account's
	// playback slot frees a minute later than it could have.
	_ = s.playback.ReleaseDevice(ctx, accountID, deviceID)
}

// DeviceSummary is the Student-facing view of one device. It deliberately omits
// the credential digest, the session ids, the challenge, and the IP address.
type DeviceSummary struct {
	ID             string    `json:"id"`
	Label          string    `json:"label"`
	BrowserFamily  string    `json:"browser_family"`
	PlatformFamily string    `json:"platform_family"`
	TrustedAt      time.Time `json:"trusted_at"`
	LastActiveAt   time.Time `json:"last_active_at"`
	CurrentDevice  bool      `json:"current_device"`
}

// DeviceOverview is the whole Devices screen in one read.
type DeviceOverview struct {
	Devices          []DeviceSummary `json:"devices"`
	DeviceLimit      int             `json:"device_limit"`
	CooldownUntil    *time.Time      `json:"replacement_cooldown_until,omitempty"`
	CurrentDeviceID  string          `json:"-"`
	PendingDeviceID  string          `json:"-"`
	ReplacementReady bool            `json:"replacement_ready"`
}

// Overview lists an Account's live trusted devices.
func (s *DeviceService) Overview(
	ctx context.Context,
	accountID, currentDeviceID string,
	now time.Time,
) (DeviceOverview, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, label, browser_family, platform_family, trusted_at, last_seen_at
		   FROM identity_trusted_devices
		  WHERE account_id = $1::uuid AND trusted_at IS NOT NULL AND revoked_at IS NULL
		  ORDER BY trusted_at`,
		accountID,
	)
	if err != nil {
		return DeviceOverview{}, fmt.Errorf("listing trusted devices: %w", err)
	}
	defer rows.Close()

	overview := DeviceOverview{
		Devices: make([]DeviceSummary, 0, s.policy.TrustedDeviceLimit),
		// The limit is published so the browser renders "2 of 2" from the
		// server's policy rather than from a number compiled into the bundle.
		DeviceLimit:     s.policy.TrustedDeviceLimit,
		CurrentDeviceID: currentDeviceID,
	}
	for rows.Next() {
		var summary DeviceSummary
		var trustedAt *time.Time
		if err := rows.Scan(&summary.ID, &summary.Label, &summary.BrowserFamily,
			&summary.PlatformFamily, &trustedAt, &summary.LastActiveAt); err != nil {
			return DeviceOverview{}, fmt.Errorf("scanning trusted device: %w", err)
		}
		if trustedAt != nil {
			summary.TrustedAt = trustedAt.UTC()
		}
		summary.LastActiveAt = summary.LastActiveAt.UTC()
		summary.CurrentDevice = summary.ID == currentDeviceID
		overview.Devices = append(overview.Devices, summary)
	}
	if err := rows.Err(); err != nil {
		return DeviceOverview{}, fmt.Errorf("iterating trusted devices: %w", err)
	}

	state, err := s.replacementState(ctx, accountID)
	if err != nil {
		return DeviceOverview{}, err
	}
	if until := state.CooldownUntil(s.policy); !until.IsZero() && now.Before(until) {
		cooldown := until.UTC()
		overview.CooldownUntil = &cooldown
	} else {
		overview.ReplacementReady = true
	}
	return overview, nil
}

func (s *DeviceService) replacementState(ctx context.Context, accountID string) (ReplacementState, error) {
	var state ReplacementState
	err := s.pool.QueryRow(ctx,
		`SELECT last_replacement_at, cooldown_cleared_at
		   FROM identity_device_replacement_state WHERE account_id = $1::uuid`,
		accountID,
	).Scan(&state.LastReplacementAt, &state.CooldownClearedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReplacementState{}, nil
	}
	if err != nil {
		return ReplacementState{}, fmt.Errorf("loading device replacement state: %w", err)
	}
	return state, nil
}

// RemoveRequest is a Student removing one of their own devices.
type RemoveRequest struct {
	AccountID string
	DeviceID  string
	RequestID string
}

// Remove is the Student's own device removal.
//
// It starts the replacement cooldown, because this is the Student spending
// their replacement. Removing the device they are currently using is allowed
// and ends that browser's sessions — the interface says so plainly beforehand
// rather than the server quietly refusing, because a Student whose only
// remaining device was lost needs exactly this path to work.
func (s *DeviceService) Remove(ctx context.Context, request RemoveRequest) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("beginning device removal: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	revision, err := lockAccountForDevicePolicy(ctx, tx, request.AccountID)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	state, err := loadReplacementState(ctx, tx, request.AccountID)
	if err != nil {
		return err
	}
	if err := state.CheckReplacementAllowed(s.policy, now); err != nil {
		if eventErr := appendIdentitySecurityEvent(ctx, tx, securityEventAppend{
			eventType: "DEVICE_REPLACEMENT_BLOCKED", accountID: request.AccountID,
			revision: revision, requestID: request.RequestID,
			evidence: map[string]any{
				"schema_version": 1,
				"device_id":      request.DeviceID,
				"cooldown_until": state.CooldownUntil(s.policy).UTC(),
			},
		}); eventErr != nil {
			return eventErr
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return fmt.Errorf("committing blocked removal: %w", commitErr)
		}
		return err
	}

	if _, err := s.revokeDeviceInTransaction(ctx, tx, deviceRevocation{
		AccountID: request.AccountID, DeviceID: request.DeviceID,
		Reason: DeviceRemovedByStudent, Revision: revision,
		RequestID: request.RequestID, Now: now,
	}); err != nil {
		return err
	}
	if err := recordReplacement(ctx, tx, request.AccountID, now); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing device removal: %w", err)
	}
	s.releasePlayback(ctx, request.AccountID, request.DeviceID)
	return nil
}
