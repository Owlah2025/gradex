package identity

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Operator capabilities over another Account's devices.
//
// These exist because the replacement cooldown is friction against sharing, not
// an account-recovery barrier. A Student who lost both devices, or who hit the
// 24-hour window at the worst possible moment, must have a route that does not
// involve resetting their password — and the operator taking that route must
// leave a trail saying who did it.

// AdminDeviceView is the operator's read. It carries the same fields the
// Student sees plus revocation history, and still no credential digest.
type AdminDeviceView struct {
	ID               string     `json:"id"`
	Label            string     `json:"label"`
	BrowserFamily    string     `json:"browser_family"`
	PlatformFamily   string     `json:"platform_family"`
	State            string     `json:"state"`
	FirstSeenAt      time.Time  `json:"first_seen_at"`
	LastActiveAt     time.Time  `json:"last_active_at"`
	TrustedAt        *time.Time `json:"trusted_at,omitempty"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
	RevocationReason string     `json:"revocation_reason,omitempty"`
}

type AdminDeviceOverview struct {
	Devices       []AdminDeviceView `json:"devices"`
	DeviceLimit   int               `json:"device_limit"`
	CooldownUntil *time.Time        `json:"replacement_cooldown_until,omitempty"`
}

// AdminOverview lists every device record an Account has ever had, including
// revoked ones: an operator investigating a sharing report needs the history,
// not just the current two.
func (s *DeviceService) AdminOverview(
	ctx context.Context,
	accountID string,
	now time.Time,
) (AdminDeviceOverview, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, label, browser_family, platform_family,
		        first_seen_at, last_seen_at, trusted_at, revoked_at,
		        COALESCE(revocation_reason::text, '')
		   FROM identity_trusted_devices
		  WHERE account_id = $1::uuid
		  ORDER BY revoked_at NULLS FIRST, first_seen_at DESC`,
		accountID,
	)
	if err != nil {
		return AdminDeviceOverview{}, fmt.Errorf("listing devices for operator: %w", err)
	}
	defer rows.Close()

	overview := AdminDeviceOverview{
		Devices: make([]AdminDeviceView, 0), DeviceLimit: s.policy.TrustedDeviceLimit,
	}
	for rows.Next() {
		var view AdminDeviceView
		if err := rows.Scan(&view.ID, &view.Label, &view.BrowserFamily, &view.PlatformFamily,
			&view.FirstSeenAt, &view.LastActiveAt, &view.TrustedAt, &view.RevokedAt,
			&view.RevocationReason); err != nil {
			return AdminDeviceOverview{}, fmt.Errorf("scanning device for operator: %w", err)
		}
		device := TrustedDevice{TrustedAt: view.TrustedAt, RevokedAt: view.RevokedAt}
		view.State = string(device.State())
		view.FirstSeenAt = view.FirstSeenAt.UTC()
		view.LastActiveAt = view.LastActiveAt.UTC()
		overview.Devices = append(overview.Devices, view)
	}
	if err := rows.Err(); err != nil {
		return AdminDeviceOverview{}, fmt.Errorf("iterating devices for operator: %w", err)
	}

	state, err := s.replacementState(ctx, accountID)
	if err != nil {
		return AdminDeviceOverview{}, err
	}
	if until := state.CooldownUntil(s.policy); !until.IsZero() && now.Before(until) {
		cooldown := until.UTC()
		overview.CooldownUntil = &cooldown
	}
	return overview, nil
}

// AdminDeviceCommand is one audited operator action.
type AdminDeviceCommand struct {
	AccountID string
	DeviceID  string
	ActorID   string
	RequestID string
}

// AdminRevokeDevice removes one device on an Account's behalf.
//
// It deliberately does not start the replacement cooldown. The Student did not
// spend their replacement here; an operator acted, usually to *undo* a
// situation the Student is stuck in, and charging them a 24-hour wait for it
// would defeat the purpose of the override existing.
func (s *DeviceService) AdminRevokeDevice(ctx context.Context, command AdminDeviceCommand) error {
	return s.adminRevoke(ctx, command, DeviceRevokedByAdmin, false)
}

// AdminRevokeAllDevices clears every live device on an Account.
//
// Broader than the single revoke on purpose, and the breadth is explicit: every
// live device record ends, every session family bound to any of them ends, and
// the cooldown is cleared so the Student can immediately trust a device again.
// Sessions not bound to a device — a legacy family, or a staff family — are
// left alone, because this command is about devices and not about logging an
// Account out. An operator who wants that has logout-all, which already exists
// and moves the session epoch.
func (s *DeviceService) AdminRevokeAllDevices(ctx context.Context, command AdminDeviceCommand) (int, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("beginning operator device revocation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	revision, err := lockAccountForDevicePolicy(ctx, tx, command.AccountID)
	if err != nil {
		return 0, err
	}
	now := s.now().UTC()
	deviceIDs, err := liveDeviceIDs(ctx, tx, command.AccountID)
	if err != nil {
		return 0, err
	}
	for _, deviceID := range deviceIDs {
		if _, err := s.revokeDeviceInTransaction(ctx, tx, deviceRevocation{
			AccountID: command.AccountID, DeviceID: deviceID,
			Reason: DeviceRevokedAllByAdmin, Revision: revision,
			RequestID: command.RequestID, ActorID: command.ActorID, Now: now,
		}); err != nil {
			return 0, err
		}
	}
	if err := clearReplacementCooldown(ctx, tx, command.AccountID, command.ActorID, now); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("committing operator device revocation: %w", err)
	}
	for _, deviceID := range deviceIDs {
		s.releasePlayback(ctx, command.AccountID, deviceID)
	}
	return len(deviceIDs), nil
}

// AdminResetReplacementCooldown releases the 24-hour window without touching
// any device.
func (s *DeviceService) AdminResetReplacementCooldown(ctx context.Context, command AdminDeviceCommand) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("beginning cooldown reset: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	revision, err := lockAccountForDevicePolicy(ctx, tx, command.AccountID)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	if err := clearReplacementCooldown(ctx, tx, command.AccountID, command.ActorID, now); err != nil {
		return err
	}
	if err := appendIdentitySecurityEvent(ctx, tx, securityEventAppend{
		eventType: "ADMIN_DEVICE_COOLDOWN_RESET", accountID: command.AccountID,
		revision: revision, requestID: command.RequestID,
		evidence: map[string]any{
			"schema_version":   1,
			"actor_account_id": command.ActorID,
		},
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing cooldown reset: %w", err)
	}
	return nil
}

func (s *DeviceService) adminRevoke(
	ctx context.Context,
	command AdminDeviceCommand,
	reason DeviceRevocationReason,
	clearCooldown bool,
) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("beginning operator device revocation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	revision, err := lockAccountForDevicePolicy(ctx, tx, command.AccountID)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	if _, err := s.revokeDeviceInTransaction(ctx, tx, deviceRevocation{
		AccountID: command.AccountID, DeviceID: command.DeviceID, Reason: reason,
		Revision: revision, RequestID: command.RequestID, ActorID: command.ActorID, Now: now,
	}); err != nil {
		return err
	}
	if clearCooldown {
		if err := clearReplacementCooldown(ctx, tx, command.AccountID, command.ActorID, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing operator device revocation: %w", err)
	}
	s.releasePlayback(ctx, command.AccountID, command.DeviceID)
	return nil
}

// RevokeAllForSecurityRecovery is the hook password reset and account recovery
// call.
//
// Recovery is the one moment where "somebody else may be holding this Account"
// is the working assumption, so every trusted device ends with it. The cooldown
// is cleared in the same step: a Student who has just proven control of their
// mailbox must be able to trust their device immediately, not tomorrow.
//
// It runs inside the caller's transaction, so a recovery that rolls back does
// not leave an Account with no devices and no explanation.
func (s *DeviceService) RevokeAllForSecurityRecovery(
	ctx context.Context,
	tx pgx.Tx,
	accountID string,
	revision int,
	reason DeviceRevocationReason,
	requestID string,
	now time.Time,
) ([]string, error) {
	deviceIDs, err := liveDeviceIDs(ctx, tx, accountID)
	if err != nil {
		return nil, err
	}
	for _, deviceID := range deviceIDs {
		if _, err := s.revokeDeviceInTransaction(ctx, tx, deviceRevocation{
			AccountID: accountID, DeviceID: deviceID, Reason: reason,
			Revision: revision, RequestID: requestID, Now: now,
		}); err != nil {
			return nil, err
		}
	}
	if err := clearReplacementCooldown(ctx, tx, accountID, "", now); err != nil {
		return nil, err
	}
	return deviceIDs, nil
}

// ReleasePlaybackForDevices is called after a recovery transaction commits.
func (s *DeviceService) ReleasePlaybackForDevices(ctx context.Context, accountID string, deviceIDs []string) {
	for _, deviceID := range deviceIDs {
		s.releasePlayback(ctx, accountID, deviceID)
	}
}

func liveDeviceIDs(ctx context.Context, tx pgx.Tx, accountID string) ([]string, error) {
	rows, err := tx.Query(ctx,
		`SELECT id::text FROM identity_trusted_devices
		  WHERE account_id = $1::uuid AND revoked_at IS NULL
		  ORDER BY first_seen_at
		  FOR UPDATE`,
		accountID,
	)
	if err != nil {
		return nil, fmt.Errorf("loading live devices: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0, 2)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning live device: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating live devices: %w", err)
	}
	return ids, nil
}

// clearReplacementCooldown moves the clearance marker forward rather than
// deleting the replacement history, so the trail still shows both that a
// replacement happened and that the wait it caused was released.
//
// An empty actorID means no person did it: password reset and security recovery
// clear the cooldown on the Account's own behalf, and naming a nonexistent
// operator would put a false fact in the audit trail.
func clearReplacementCooldown(ctx context.Context, tx pgx.Tx, accountID, actorID string, now time.Time) error {
	if _, err := tx.Exec(ctx,
		`INSERT INTO identity_device_replacement_state
		   (account_id, cooldown_cleared_at, cooldown_cleared_by, created_at, updated_at)
		 VALUES ($1::uuid, $2, NULLIF($3, '')::uuid, $2, $2)
		 ON CONFLICT (account_id) DO UPDATE
		    SET cooldown_cleared_at = EXCLUDED.cooldown_cleared_at,
		        cooldown_cleared_by = EXCLUDED.cooldown_cleared_by,
		        updated_at = EXCLUDED.updated_at`,
		accountID, now, actorID,
	); err != nil {
		return fmt.Errorf("clearing device replacement cooldown: %w", err)
	}
	return nil
}
