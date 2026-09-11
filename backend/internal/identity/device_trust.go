package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DeviceTrustRequest completes one device-trust challenge.
//
// The device being trusted is deliberately absent from this struct. It is read
// from the challenge, which was bound to exactly one device record when it was
// issued, and the browser then has to prove it holds that device's credential.
// A caller therefore cannot nominate which device its code trusts, and cannot
// aim a code that was mailed for one browser at another.
//
// AccountID and SessionID come from the authenticated session, PresentedDigest
// from the device cookie, and only the code and the optional replacement choice
// come from the request body.
type DeviceTrustRequest struct {
	AccountID string
	SessionID string

	// PresentedDigest is the digest of the device credential this browser sent.
	PresentedDigest string

	ChallengeID string
	Code        string

	// ReplaceDeviceID is required only when the Account is already at its
	// limit. It names one of the Student's own live trusted devices.
	ReplaceDeviceID string

	RequestID string
}

type DeviceTrustResult struct {
	DeviceID          string
	ReplacedDeviceID  string
	RevokedSessions   int
	TrustedDeviceIDs  []string
	ReplacementCooled time.Time
}

// CompleteTrust verifies the emailed code and, if the policy allows, turns the
// pending device into a trusted one and binds the calling session to it.
//
// The whole thing is one transaction taken under the Account row lock. That is
// what makes the limit hold under concurrency: two browsers answering two codes
// at the same instant serialize, so the second one counts the first one's
// device and is refused, rather than both counting one and both inserting a
// second.
func (s *DeviceService) CompleteTrust(
	ctx context.Context,
	request DeviceTrustRequest,
) (DeviceTrustResult, error) {
	if _, err := uuid.Parse(request.ChallengeID); err != nil {
		return DeviceTrustResult{}, ErrOTPInvalid
	}
	code, ok := NormalizeEmailOTPInput(request.Code)
	if !ok {
		// Refused before it can spend an attempt: junk is not a guess against
		// the code space, and charging for it would let anyone burn a Student's
		// budget from a page they can already reach.
		return DeviceTrustResult{}, ErrOTPInvalid
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return DeviceTrustResult{}, fmt.Errorf("beginning device trust: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	revision, err := lockAccountForDevicePolicy(ctx, tx, request.AccountID)
	if err != nil {
		return DeviceTrustResult{}, err
	}

	live, hasLive, err := lockLiveDeviceOTP(ctx, tx, request.AccountID)
	if err != nil {
		return DeviceTrustResult{}, err
	}
	now := s.now().UTC()
	if !hasLive || live.id != request.ChallengeID || !now.Before(live.expiresAt) {
		return DeviceTrustResult{}, ErrOTPInvalid
	}
	// The browser must hold the credential of the device this code was mailed
	// for. Without this, a second browser that merely knows the code could
	// complete a challenge raised for the first one.
	device, found, err := loadLiveDeviceByID(ctx, tx, request.AccountID, live.deviceID)
	if err != nil {
		return DeviceTrustResult{}, err
	}
	if !found || request.PresentedDigest == "" ||
		!OpaqueDigestEqual(device.CredentialDigest, request.PresentedDigest) {
		return DeviceTrustResult{}, ErrOTPInvalid
	}
	if live.attemptCount >= EmailOTPMaxAttempts {
		if err := s.exhaustDeviceOTP(ctx, tx, live, request, revision); err != nil {
			return DeviceTrustResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return DeviceTrustResult{}, fmt.Errorf("committing exhausted device challenge: %w", err)
		}
		return DeviceTrustResult{}, ErrOTPAttemptsExhausted
	}
	if err := recordEmailOTPAttempt(ctx, tx, live.id, now); err != nil {
		return DeviceTrustResult{}, err
	}
	if !s.pepper.MatchesDeviceTrustOTP(request.ChallengeID, code, live.digest) {
		remaining := EmailOTPMaxAttempts - (live.attemptCount + 1)
		if remaining <= 0 {
			if err := s.exhaustDeviceOTP(ctx, tx, live, request, revision); err != nil {
				return DeviceTrustResult{}, err
			}
		}
		// The spent attempt is committed even though the answer is a refusal;
		// otherwise the budget would be free to guess against forever.
		if err := tx.Commit(ctx); err != nil {
			return DeviceTrustResult{}, fmt.Errorf("committing device trust attempt: %w", err)
		}
		if remaining <= 0 {
			return DeviceTrustResult{}, ErrOTPAttemptsExhausted
		}
		return DeviceTrustResult{}, ErrOTPInvalid
	}

	// The mailbox is proven. Only now does the device limit enter the
	// conversation, which is what keeps the Student's device labels from being
	// readable by anyone holding only a password.
	result, err := s.applyTrust(ctx, tx, request, device.ID, revision, now)
	if err != nil {
		return DeviceTrustResult{}, err
	}
	if err := consumeDeviceOTP(ctx, tx, live.id, now); err != nil {
		return DeviceTrustResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DeviceTrustResult{}, fmt.Errorf("committing device trust: %w", err)
	}

	// Redis is touched only after the durable decision commits. A lease
	// released for a device that never actually lost its trust would be a
	// self-inflicted playback interruption.
	if result.ReplacedDeviceID != "" {
		s.releasePlayback(ctx, request.AccountID, result.ReplacedDeviceID)
	}
	return result, nil
}

func (s *DeviceService) applyTrust(
	ctx context.Context,
	tx pgx.Tx,
	request DeviceTrustRequest,
	deviceID string,
	revision int,
	now time.Time,
) (DeviceTrustResult, error) {
	trustedCount, err := countTrustedDevices(ctx, tx, request.AccountID)
	if err != nil {
		return DeviceTrustResult{}, err
	}

	result := DeviceTrustResult{DeviceID: deviceID}
	if trustedCount >= s.policy.TrustedDeviceLimit {
		replaced, err := s.replaceForTrust(ctx, tx, request, deviceID, revision, now)
		if err != nil {
			return DeviceTrustResult{}, err
		}
		result.ReplacedDeviceID = replaced.deviceID
		result.RevokedSessions = replaced.revokedSessions
	}

	if err := markDeviceTrusted(ctx, tx, request.AccountID, deviceID, now); err != nil {
		return DeviceTrustResult{}, err
	}
	if err := bindSessionToDevice(ctx, tx, request.SessionID, request.AccountID, deviceID); err != nil {
		return DeviceTrustResult{}, err
	}
	if err := appendIdentitySecurityEvent(ctx, tx, securityEventAppend{
		eventType: "DEVICE_TRUSTED", accountID: request.AccountID,
		revision: revision, requestID: request.RequestID,
		evidence: map[string]any{
			"schema_version": 1,
			"device_id":      deviceID,
			"replaced":       result.ReplacedDeviceID != "",
			"device_limit":   s.policy.TrustedDeviceLimit,
		},
	}); err != nil {
		return DeviceTrustResult{}, err
	}
	return result, nil
}

type replacementOutcome struct {
	deviceID        string
	revokedSessions int
}

// replaceForTrust is the third-device path: the Student named one of their own
// devices to give up, and this is where that happens atomically with trusting
// the new one. There is deliberately no path that evicts a device the Student
// did not choose.
func (s *DeviceService) replaceForTrust(
	ctx context.Context,
	tx pgx.Tx,
	request DeviceTrustRequest,
	deviceID string,
	revision int,
	now time.Time,
) (replacementOutcome, error) {
	if request.ReplaceDeviceID == "" {
		return replacementOutcome{}, ErrDeviceLimitReached
	}
	if request.ReplaceDeviceID == deviceID {
		// Replacing the pending device with itself would "free" a slot that was
		// never occupied.
		return replacementOutcome{}, ErrDeviceUnknown
	}

	state, err := loadReplacementState(ctx, tx, request.AccountID)
	if err != nil {
		return replacementOutcome{}, err
	}
	if err := state.CheckReplacementAllowed(s.policy, now); err != nil {
		if eventErr := appendIdentitySecurityEvent(ctx, tx, securityEventAppend{
			eventType: "DEVICE_REPLACEMENT_BLOCKED", accountID: request.AccountID,
			revision: revision, requestID: request.RequestID,
			evidence: map[string]any{
				"schema_version": 1,
				"cooldown_until": state.CooldownUntil(s.policy).UTC(),
			},
		}); eventErr != nil {
			return replacementOutcome{}, eventErr
		}
		// The refusal is committed together with its evidence. Rolling back
		// here would lose the record of an attempt worth seeing repeated.
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return replacementOutcome{}, fmt.Errorf("committing blocked replacement: %w", commitErr)
		}
		return replacementOutcome{}, err
	}

	revoked, err := s.revokeDeviceInTransaction(ctx, tx, deviceRevocation{
		AccountID: request.AccountID,
		DeviceID:  request.ReplaceDeviceID,
		Reason:    DeviceReplacedByStudent,
		Revision:  revision,
		RequestID: request.RequestID,
		Now:       now,
	})
	if err != nil {
		return replacementOutcome{}, err
	}
	if err := recordReplacement(ctx, tx, request.AccountID, now); err != nil {
		return replacementOutcome{}, err
	}
	return replacementOutcome{deviceID: request.ReplaceDeviceID, revokedSessions: revoked}, nil
}

func (s *DeviceService) exhaustDeviceOTP(
	ctx context.Context,
	tx pgx.Tx,
	live liveDeviceOTP,
	request DeviceTrustRequest,
	revision int,
) error {
	if _, err := tx.Exec(ctx,
		`UPDATE identity_action_secrets
		    SET superseded_at = GREATEST(issued_at, clock_timestamp()),
		        superseded_by_id = id
		  WHERE id = $1::uuid AND consumed_at IS NULL AND superseded_at IS NULL`,
		live.id,
	); err != nil {
		return fmt.Errorf("retiring exhausted device challenge: %w", err)
	}
	return appendIdentitySecurityEvent(ctx, tx, securityEventAppend{
		eventType:      "DEVICE_TRUST_ATTEMPTS_EXHAUSTED",
		accountID:      request.AccountID,
		actionSecretID: live.id,
		revision:       revision,
		requestID:      request.RequestID,
		evidence: map[string]any{
			"schema_version": 1,
			"attempt_limit":  EmailOTPMaxAttempts,
		},
	})
}

// SQL for trust completion.

type liveDeviceOTP struct {
	id           string
	deviceID     string
	digest       []byte
	issuedAt     time.Time
	expiresAt    time.Time
	attemptCount int
}

func lockLiveDeviceOTP(ctx context.Context, tx pgx.Tx, accountID string) (liveDeviceOTP, bool, error) {
	var live liveDeviceOTP
	err := tx.QueryRow(ctx,
		`SELECT id::text, trusted_device_id::text, secret_digest, issued_at, expires_at, attempt_count
		   FROM identity_action_secrets
		  WHERE account_id = $1::uuid
		    AND purpose = 'DEVICE_TRUST_OTP'
		    AND consumed_at IS NULL
		    AND superseded_at IS NULL
		  ORDER BY issued_at DESC
		  LIMIT 1
		  FOR UPDATE`,
		accountID,
	).Scan(&live.id, &live.deviceID, &live.digest, &live.issuedAt, &live.expiresAt, &live.attemptCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return liveDeviceOTP{}, false, nil
	}
	if err != nil {
		return liveDeviceOTP{}, false, fmt.Errorf("loading device trust challenge: %w", err)
	}
	return live, true, nil
}

func consumeDeviceOTP(ctx context.Context, tx pgx.Tx, challengeID string, now time.Time) error {
	if _, err := tx.Exec(ctx,
		`UPDATE identity_action_secrets
		    SET consumed_at = GREATEST(issued_at, $2)
		  WHERE id = $1::uuid AND consumed_at IS NULL AND superseded_at IS NULL`,
		challengeID, now,
	); err != nil {
		return fmt.Errorf("consuming device trust challenge: %w", err)
	}
	return nil
}

// lockAccountForDevicePolicy is the serialization point for every decision that
// can change how many trusted devices an Account has.
func lockAccountForDevicePolicy(ctx context.Context, tx pgx.Tx, accountID string) (int, error) {
	var revision int
	err := tx.QueryRow(ctx,
		`SELECT revision FROM accounts WHERE id = $1::uuid FOR UPDATE`, accountID,
	).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrDeviceUnknown
	}
	if err != nil {
		return 0, fmt.Errorf("locking Account for device policy: %w", err)
	}
	return revision, nil
}

func markDeviceTrusted(ctx context.Context, tx pgx.Tx, accountID, deviceID string, now time.Time) error {
	tag, err := tx.Exec(ctx,
		`UPDATE identity_trusted_devices
		    SET trusted_at = COALESCE(trusted_at, GREATEST(first_seen_at, $3)),
		        last_seen_at = $3, updated_at = $3
		  WHERE id = $1::uuid AND account_id = $2::uuid AND revoked_at IS NULL`,
		deviceID, accountID, now,
	)
	if err != nil {
		return fmt.Errorf("trusting device: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrDeviceUnknown
	}
	return nil
}

// bindSessionToDevice is what makes "account -> active session -> trusted
// device" answerable. It upgrades exactly the calling family and no other: a
// Student's second browser keeps whatever binding it already had.
func bindSessionToDevice(ctx context.Context, tx pgx.Tx, sessionID, accountID, deviceID string) error {
	tag, err := tx.Exec(ctx,
		`UPDATE sessions
		    SET trusted_device_id = $3::uuid, device_trust_state = 'TRUSTED'
		  WHERE id = $1::uuid AND account_id = $2::uuid AND state = 'ACTIVE'`,
		sessionID, accountID, deviceID,
	)
	if err != nil {
		return fmt.Errorf("binding session to trusted device: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSessionNotUsable
	}
	return nil
}

func loadReplacementState(ctx context.Context, tx pgx.Tx, accountID string) (ReplacementState, error) {
	var state ReplacementState
	err := tx.QueryRow(ctx,
		`SELECT last_replacement_at, cooldown_cleared_at
		   FROM identity_device_replacement_state
		  WHERE account_id = $1::uuid
		  FOR UPDATE`,
		accountID,
	).Scan(&state.LastReplacementAt, &state.CooldownClearedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// No row means no replacement has ever happened, which is the correct
		// state for every Account at deployment.
		return ReplacementState{}, nil
	}
	if err != nil {
		return ReplacementState{}, fmt.Errorf("loading device replacement state: %w", err)
	}
	return state, nil
}

func recordReplacement(ctx context.Context, tx pgx.Tx, accountID string, now time.Time) error {
	if _, err := tx.Exec(ctx,
		`INSERT INTO identity_device_replacement_state
		   (account_id, last_replacement_at, created_at, updated_at)
		 VALUES ($1::uuid, $2, $2, $2)
		 ON CONFLICT (account_id) DO UPDATE
		    SET last_replacement_at = EXCLUDED.last_replacement_at,
		        updated_at = EXCLUDED.updated_at`,
		accountID, now,
	); err != nil {
		return fmt.Errorf("recording device replacement: %w", err)
	}
	return nil
}

// loadLiveDeviceByID reads one of this Account's live device records. The
// Account scoping is in the query rather than checked afterwards, so a device id
// belonging to somebody else is indistinguishable from one that does not exist.
func loadLiveDeviceByID(ctx context.Context, tx pgx.Tx, accountID, deviceID string) (TrustedDevice, bool, error) {
	if deviceID == "" {
		return TrustedDevice{}, false, nil
	}
	device, err := scanDevice(tx.QueryRow(ctx,
		`SELECT `+deviceColumns+`
		   FROM identity_trusted_devices d
		  WHERE d.id = $1::uuid AND d.account_id = $2::uuid AND d.revoked_at IS NULL
		  FOR UPDATE`,
		deviceID, accountID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return TrustedDevice{}, false, nil
	}
	if err != nil {
		return TrustedDevice{}, false, fmt.Errorf("loading trusted device by identity: %w", err)
	}
	return device, true, nil
}

// ResendDeviceTrustRequest asks for a replacement code on the live challenge.
type ResendDeviceTrustRequest struct {
	AccountID string
	Email     string
	Locale    Locale
	UserAgent string
	RequestID string
}

// ResendDeviceTrustOTP replaces the code behind the live device challenge.
//
// The cooldown is enforced against the challenge that is actually live, so a
// caller cannot trade a burnt attempt budget for a fresh one faster than the
// mailbox can be flooded. Supersession is what makes "the previous code stops
// working" true rather than aspirational, and it resets the budget by moving to
// a new row rather than by editing a counter.
func (s *DeviceService) ResendDeviceTrustOTP(
	ctx context.Context,
	request ResendDeviceTrustRequest,
) (DeviceChallenge, error) {
	reservation, err := s.reserveChallengePayload(ctx)
	if err != nil {
		return DeviceChallenge{}, err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return DeviceChallenge{}, fmt.Errorf("beginning device code resend: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	revision, err := lockAccountForDevicePolicy(ctx, tx, request.AccountID)
	if err != nil {
		return DeviceChallenge{}, err
	}
	live, hasLive, err := lockLiveDeviceOTP(ctx, tx, request.AccountID)
	if err != nil {
		return DeviceChallenge{}, err
	}
	if !hasLive {
		return DeviceChallenge{}, ErrOTPInvalid
	}
	now := s.now().UTC()
	if now.Before(live.issuedAt.Add(EmailOTPResendCooldown)) {
		return DeviceChallenge{}, ErrOTPResendTooSoon
	}

	otp, err := s.issueDeviceOTP(now)
	if err != nil {
		return DeviceChallenge{}, err
	}
	if err := supersedeLiveDeviceOTP(ctx, tx, request.AccountID, otp.ChallengeID); err != nil {
		return DeviceChallenge{}, err
	}
	if err := insertDeviceOTPSecret(ctx, tx, request.AccountID, live.deviceID, otp); err != nil {
		return DeviceChallenge{}, err
	}
	if err := appendIdentitySecurityEvent(ctx, tx, securityEventAppend{
		eventType: "DEVICE_TRUST_CHALLENGED", accountID: request.AccountID,
		actionSecretID: otp.ChallengeID, revision: revision, requestID: request.RequestID,
		evidence: map[string]any{"schema_version": 1, "reason": "RESEND"},
	}); err != nil {
		return DeviceChallenge{}, err
	}
	if err := s.appendDeviceCodeOutbox(ctx, tx, DeviceAdmissionRequest{
		AccountID: request.AccountID, Revision: revision, Email: request.Email,
		Locale: request.Locale, RequestID: request.RequestID, Reservation: reservation,
	}, otp); err != nil {
		return DeviceChallenge{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DeviceChallenge{}, fmt.Errorf("committing device code resend: %w", err)
	}
	return DeviceChallenge{
		ChallengeID:       otp.ChallengeID,
		MaskedEmail:       MaskEmail(request.Email),
		ExpiresAt:         otp.ExpiresAt,
		ResendAvailableAt: otp.ResendAvailableAt(),
	}, nil
}
