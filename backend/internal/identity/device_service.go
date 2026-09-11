package identity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

// Device-trust outcomes that callers distinguish.
var (
	// ErrDeviceUnknown means the named device is not a live record of this
	// Account. It is one error for "no such device", "already revoked", and
	// "belongs to someone else" so a caller cannot enumerate device ids.
	ErrDeviceUnknown = errors.New("trusted device is unknown")

	// ErrDeviceLimitReached means every slot is taken and the caller did not
	// nominate one to replace.
	ErrDeviceLimitReached = errors.New("trusted device limit is reached")

	// ErrDeviceTrustUnavailable is the fail-closed answer when device policy
	// cannot be evaluated.
	ErrDeviceTrustUnavailable = errors.New("device trust is unavailable")
)

type DeviceServiceOptions struct {
	Pool     *pgxpool.Pool
	Outbox   *outbox.Writer
	Policy   DevicePolicy
	Pepper   config.Secret
	OTPTTL   time.Duration
	Now      func() time.Time
	Random   io.Reader
	Playback DevicePlaybackReleaser
}

// DevicePlaybackReleaser drops a revoked device's playback lease.
//
// An interface, and optional, because the identity package must not depend on
// Redis to compile or to be tested. When it is absent, a revoked device's lease
// simply expires on its own TTL instead of being released immediately; the
// device is still refused at the next authorization either way.
type DevicePlaybackReleaser interface {
	ReleaseDevice(ctx context.Context, accountID, deviceID string) error
}

// DeviceService owns trusted-device records, their OTP challenges, and the
// replacement cooldown.
type DeviceService struct {
	pool     *pgxpool.Pool
	outbox   *outbox.Writer
	policy   DevicePolicy
	pepper   EmailOTPPepper
	otpTTL   time.Duration
	now      func() time.Time
	random   io.Reader
	randomMu sync.Mutex
	playback DevicePlaybackReleaser
}

func NewDeviceService(options DeviceServiceOptions) (*DeviceService, error) {
	if options.Pool == nil || options.Outbox == nil {
		return nil, errors.New("device service pool and outbox are required")
	}
	if options.Now == nil || options.Random == nil {
		return nil, errors.New("device service clock and randomness are required")
	}
	if options.OTPTTL <= 0 {
		return nil, errors.New("device trust OTP TTL is required")
	}
	if err := options.Policy.Validate(); err != nil {
		return nil, err
	}
	pepper, err := NewEmailOTPPepper(options.Pepper)
	if err != nil {
		return nil, err
	}
	return &DeviceService{
		pool: options.Pool, outbox: options.Outbox, policy: options.Policy,
		pepper: pepper, otpTTL: options.OTPTTL, now: options.Now,
		random: options.Random, playback: options.Playback,
	}, nil
}

func (s *DeviceService) Policy() DevicePolicy { return s.policy }

// DeviceChallenge is what a browser awaiting trust is told. It carries a masked
// address so the Student can confirm which mailbox to check, and never the
// address itself, the code, or anything about the device credential.
type DeviceChallenge struct {
	ChallengeID       string
	MaskedEmail       string
	ExpiresAt         time.Time
	ResendAvailableAt time.Time
}

// DeviceAdmissionRequest is the login-time device question.
type DeviceAdmissionRequest struct {
	AccountID string
	Revision  int
	Role      Role
	Email     string
	Locale    Locale

	// PresentedDigest is the digest of the device credential this browser sent,
	// or empty when it sent none. The plaintext never reaches this package.
	PresentedDigest string

	UserAgent     string
	SourceAddress string
	RequestID     string

	Reservation outbox.ProtectedPayloadReservation
}

// DeviceAdmissionResult is what the login transaction learned.
type DeviceAdmissionResult struct {
	Admission  DeviceAdmission
	TrustState SessionDeviceTrust
	DeviceID   string
	Challenge  *DeviceChallenge
}

// admitInTransaction resolves the browser against this Account's devices and
// decides what authority the session being created may carry.
//
// It runs inside the login transaction, after the Account row is locked. That
// lock is what makes the two-device limit a real invariant rather than a
// hopeful count: two simultaneous logins from two new browsers serialize on the
// same row, so the second one observes the first one's device and is told the
// Account is full, instead of both reading "1 trusted" and both inserting.
func (s *DeviceService) admitInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	request DeviceAdmissionRequest,
) (DeviceAdmissionResult, error) {
	// Device policy is a Student control. Instructor and Admin families are
	// marked as out of scope rather than exempted by a flag, so no later change
	// to the policy can accidentally start applying to staff.
	if request.Role != RoleStudent {
		return DeviceAdmissionResult{
			Admission: AdmitTrustedDevice, TrustState: DeviceTrustNotApplicable,
		}, nil
	}

	existing, found, err := loadLiveDeviceByDigest(ctx, tx, request.AccountID, request.PresentedDigest)
	if err != nil {
		return DeviceAdmissionResult{}, err
	}
	trustedCount, err := countTrustedDevices(ctx, tx, request.AccountID)
	if err != nil {
		return DeviceAdmissionResult{}, err
	}

	now := s.now().UTC()
	switch DecideDeviceAdmission(existing, found, trustedCount, s.policy) {
	case AdmitTrustedDevice:
		if err := touchDevice(ctx, tx, existing.ID, request, now); err != nil {
			return DeviceAdmissionResult{}, err
		}
		return DeviceAdmissionResult{
			Admission: AdmitTrustedDevice, TrustState: DeviceTrustEstablished,
			DeviceID: existing.ID,
		}, nil

	case AdmitNewDeviceWithSlot:
		device, challenge, err := s.beginTrust(ctx, tx, request, existing, found, now, "SLOT_AVAILABLE")
		if err != nil {
			return DeviceAdmissionResult{}, err
		}
		return DeviceAdmissionResult{
			Admission: AdmitNewDeviceWithSlot, TrustState: DeviceTrustPending,
			DeviceID: device, Challenge: challenge,
		}, nil

	default:
		// The Account is full. The Student still gets a challenge, because the
		// ordering matters: they prove the mailbox first and only then are
		// shown their devices and asked to remove one. Reversing that would let
		// anyone holding a password enumerate the Account's device labels.
		device, challenge, err := s.beginTrust(ctx, tx, request, existing, found, now, "LIMIT_REACHED")
		if err != nil {
			return DeviceAdmissionResult{}, err
		}
		if err := appendIdentitySecurityEvent(ctx, tx, securityEventAppend{
			eventType: "DEVICE_LIMIT_REACHED", accountID: request.AccountID,
			revision: request.Revision, requestID: request.RequestID,
			evidence: map[string]any{
				"schema_version": 1,
				"trusted_count":  trustedCount,
				"device_limit":   s.policy.TrustedDeviceLimit,
			},
		}); err != nil {
			return DeviceAdmissionResult{}, err
		}
		return DeviceAdmissionResult{
			Admission: AdmitNewDeviceAtLimit, TrustState: DeviceTrustPending,
			DeviceID: device, Challenge: challenge,
		}, nil
	}
}

// beginTrust creates or reuses the pending record for this browser and mails it
// a code.
func (s *DeviceService) beginTrust(
	ctx context.Context,
	tx pgx.Tx,
	request DeviceAdmissionRequest,
	existing TrustedDevice,
	found bool,
	now time.Time,
	reason string,
) (string, *DeviceChallenge, error) {
	deviceID := existing.ID
	if found {
		if err := touchDevice(ctx, tx, deviceID, request, now); err != nil {
			return "", nil, err
		}
	} else {
		created, err := insertPendingDevice(ctx, tx, request, now)
		if err != nil {
			return "", nil, err
		}
		deviceID = created
	}

	otp, err := s.issueDeviceOTP(now)
	if err != nil {
		return "", nil, err
	}
	if err := supersedeLiveDeviceOTP(ctx, tx, request.AccountID, otp.ChallengeID); err != nil {
		return "", nil, err
	}
	if err := insertDeviceOTPSecret(ctx, tx, request.AccountID, deviceID, otp); err != nil {
		return "", nil, err
	}
	if err := appendIdentitySecurityEvent(ctx, tx, securityEventAppend{
		eventType: "DEVICE_TRUST_CHALLENGED", accountID: request.AccountID,
		actionSecretID: otp.ChallengeID, revision: request.Revision,
		requestID: request.RequestID,
		evidence: map[string]any{
			"schema_version": 1,
			"reason":         reason,
			"browser_family": DeriveDeviceIdentity(request.UserAgent).BrowserFamily,
		},
	}); err != nil {
		return "", nil, err
	}
	if err := s.appendDeviceCodeOutbox(ctx, tx, request, otp); err != nil {
		return "", nil, err
	}
	return deviceID, &DeviceChallenge{
		ChallengeID:       otp.ChallengeID,
		MaskedEmail:       MaskEmail(request.Email),
		ExpiresAt:         otp.ExpiresAt,
		ResendAvailableAt: otp.ResendAvailableAt(),
	}, nil
}

func (s *DeviceService) issueDeviceOTP(now time.Time) (IssuedEmailOTP, error) {
	s.randomMu.Lock()
	defer s.randomMu.Unlock()
	return newDeviceTrustOTP(deviceTrustOTPOptions{
		Pepper: s.pepper, Now: now, TTL: s.otpTTL, Random: s.random,
	})
}

func (s *DeviceService) appendDeviceCodeOutbox(
	ctx context.Context,
	tx pgx.Tx,
	request DeviceAdmissionRequest,
	otp IssuedEmailOTP,
) error {
	_, err := s.outbox.AppendReserved(ctx, tx, outbox.ReservedAppend{
		Event: outbox.Event{
			Type:              "identity.device_trust_code_requested",
			SchemaVersion:     1,
			SourceModule:      "IDENTITY_AND_ACCESS",
			AggregateType:     "ACCOUNT",
			AggregateID:       request.AccountID,
			AggregateRevision: request.Revision,
			CorrelationID:     request.RequestID,
			SafePayload: map[string]any{
				"purpose":           "DEVICE_TRUST_OTP",
				"challenge_id":      otp.ChallengeID,
				"locale":            request.Locale,
				"template_contract": deviceTrustTemplateContract,
				"code_expires_at":   otp.ExpiresAt,
			},
		},
		Protected: outbox.VerificationCodeDelivery{
			Destination:      request.Email,
			Locale:           string(request.Locale),
			TemplateContract: deviceTrustTemplateContract,
			Code:             otp.Code.Expose(),
			ExpiresAt:        otp.ExpiresAt,
		},
		Reservation: request.Reservation,
	})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDeliveryUnavailable, err)
	}
	return nil
}

// SQL.

const deviceColumns = `d.id::text, d.account_id::text, d.credential_digest, d.label,
	d.browser_family, d.platform_family, d.first_seen_at, d.last_seen_at,
	d.trusted_at, d.revoked_at, d.revocation_reason::text`

func scanDevice(row pgx.Row) (TrustedDevice, error) {
	var device TrustedDevice
	var reason *string
	if err := row.Scan(
		&device.ID, &device.AccountID, &device.CredentialDigest, &device.Label,
		&device.BrowserFamily, &device.PlatformFamily, &device.FirstSeenAt,
		&device.LastSeenAt, &device.TrustedAt, &device.RevokedAt, &reason,
	); err != nil {
		return TrustedDevice{}, err
	}
	if reason != nil {
		converted := DeviceRevocationReason(*reason)
		device.RevocationReason = &converted
	}
	return device, nil
}

// loadLiveDeviceByDigest is the only place a presented device credential is
// turned into a record, and it never matches on anything else. A changed
// User-Agent, a new IP address, or a different screen has no effect here, which
// is precisely the property that keeps a travelling Student logged in.
func loadLiveDeviceByDigest(
	ctx context.Context,
	tx pgx.Tx,
	accountID, digest string,
) (TrustedDevice, bool, error) {
	if digest == "" {
		return TrustedDevice{}, false, nil
	}
	device, err := scanDevice(tx.QueryRow(ctx,
		`SELECT `+deviceColumns+`
		   FROM identity_trusted_devices d
		  WHERE d.account_id = $1::uuid
		    AND d.credential_digest = $2
		    AND d.revoked_at IS NULL
		  FOR UPDATE`,
		accountID, digest,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return TrustedDevice{}, false, nil
	}
	if err != nil {
		return TrustedDevice{}, false, fmt.Errorf("loading trusted device: %w", err)
	}
	return device, true, nil
}

func countTrustedDevices(ctx context.Context, tx pgx.Tx, accountID string) (int, error) {
	var count int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM identity_trusted_devices
		  WHERE account_id = $1::uuid AND trusted_at IS NOT NULL AND revoked_at IS NULL`,
		accountID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting trusted devices: %w", err)
	}
	return count, nil
}

func insertPendingDevice(
	ctx context.Context,
	tx pgx.Tx,
	request DeviceAdmissionRequest,
	now time.Time,
) (string, error) {
	identity := DeriveDeviceIdentity(request.UserAgent)
	deviceID := uuid.NewString()
	if _, err := tx.Exec(ctx,
		`INSERT INTO identity_trusted_devices
		   (id, account_id, credential_digest, label, browser_family, platform_family,
		    first_seen_at, last_seen_at, last_ip_address, created_at, updated_at)
		 VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $7, NULLIF($8, '')::inet, $7, $7)`,
		deviceID, request.AccountID, request.PresentedDigest, identity.Label,
		identity.BrowserFamily, identity.PlatformFamily, now, request.SourceAddress,
	); err != nil {
		return "", fmt.Errorf("inserting trusted device: %w", err)
	}
	return deviceID, nil
}

// touchDevice refreshes presentation and forensic facts only.
//
// The label is rewritten because a browser update changes the User-Agent and
// the Student should still see a name they recognize. Doing so cannot change
// which record this browser matches: identity is the credential digest, and
// that column is never written here.
func touchDevice(
	ctx context.Context,
	tx pgx.Tx,
	deviceID string,
	request DeviceAdmissionRequest,
	now time.Time,
) error {
	identity := DeriveDeviceIdentity(request.UserAgent)
	if _, err := tx.Exec(ctx,
		`UPDATE identity_trusted_devices
		    SET last_seen_at = $2, label = $3, browser_family = $4, platform_family = $5,
		        last_ip_address = COALESCE(NULLIF($6, '')::inet, last_ip_address),
		        updated_at = $2
		  WHERE id = $1::uuid AND revoked_at IS NULL`,
		deviceID, now, identity.Label, identity.BrowserFamily,
		identity.PlatformFamily, request.SourceAddress,
	); err != nil {
		return fmt.Errorf("updating trusted device: %w", err)
	}
	return nil
}

func supersedeLiveDeviceOTP(ctx context.Context, tx pgx.Tx, accountID, replacementID string) error {
	if _, err := tx.Exec(ctx,
		`UPDATE identity_action_secrets
		    SET superseded_at = GREATEST(issued_at, clock_timestamp()),
		        superseded_by_id = $1::uuid
		  WHERE account_id = $2::uuid
		    AND purpose = 'DEVICE_TRUST_OTP'
		    AND consumed_at IS NULL
		    AND superseded_at IS NULL`,
		replacementID, accountID,
	); err != nil {
		return fmt.Errorf("superseding device trust challenge: %w", err)
	}
	return nil
}

func insertDeviceOTPSecret(
	ctx context.Context,
	tx pgx.Tx,
	accountID, deviceID string,
	otp IssuedEmailOTP,
) error {
	if _, err := tx.Exec(ctx,
		`INSERT INTO identity_action_secrets
		   (id, account_id, purpose, secret_digest, issued_at, expires_at, created_at, trusted_device_id)
		 VALUES ($1::uuid, $2::uuid, 'DEVICE_TRUST_OTP', $3, $4, $5, $4, $6::uuid)`,
		otp.ChallengeID, accountID, otp.Digest, otp.IssuedAt, otp.ExpiresAt, deviceID,
	); err != nil {
		return fmt.Errorf("inserting device trust challenge: %w", err)
	}
	return nil
}

// reserveChallengePayload performs the fallible entropy read the encrypted
// outbox requires, before any transaction opens. Failing here refuses the login
// rather than committing a challenge whose code could never be mailed.
func (s *DeviceService) reserveChallengePayload(ctx context.Context) (outbox.ProtectedPayloadReservation, error) {
	reservation, err := s.outbox.ReserveProtectedPayload(ctx)
	if err != nil {
		return outbox.ProtectedPayloadReservation{},
			fmt.Errorf("%w: protected payload reservation", ErrDeliveryUnavailable)
	}
	return reservation, nil
}

// TrustFirstDeviceInTransaction trusts a browser on the strength of a
// verification code that has just been proven in the same transaction.
//
// This is the registration path. Mailing a second six-digit code, seconds after
// the Student typed the first one, to prove the same mailbox from the same
// browser, would be pure friction: the device limit is still enforced, the slot
// is still counted, and the trust decision still leaves a DEVICE_TRUSTED event
// naming which proof it rested on.
func (s *DeviceService) TrustFirstDeviceInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	request DeviceAdmissionRequest,
	now time.Time,
) (DeviceAdmissionResult, error) {
	if request.Role != RoleStudent {
		return DeviceAdmissionResult{
			Admission: AdmitTrustedDevice, TrustState: DeviceTrustNotApplicable,
		}, nil
	}
	if request.PresentedDigest == "" {
		return DeviceAdmissionResult{}, fmt.Errorf("%w: no device credential", ErrDeviceTrustUnavailable)
	}
	existing, found, err := loadLiveDeviceByDigest(ctx, tx, request.AccountID, request.PresentedDigest)
	if err != nil {
		return DeviceAdmissionResult{}, err
	}
	trustedCount, err := countTrustedDevices(ctx, tx, request.AccountID)
	if err != nil {
		return DeviceAdmissionResult{}, err
	}
	if !found && trustedCount >= s.policy.TrustedDeviceLimit {
		// Not reachable for a first registration, and deliberately refused
		// rather than allowed to exceed the limit if it ever becomes reachable.
		return DeviceAdmissionResult{}, ErrDeviceLimitReached
	}

	deviceID := existing.ID
	if found {
		if err := touchDevice(ctx, tx, deviceID, request, now); err != nil {
			return DeviceAdmissionResult{}, err
		}
	} else {
		created, err := insertPendingDevice(ctx, tx, request, now)
		if err != nil {
			return DeviceAdmissionResult{}, err
		}
		deviceID = created
	}
	if err := markDeviceTrusted(ctx, tx, request.AccountID, deviceID, now); err != nil {
		return DeviceAdmissionResult{}, err
	}
	if err := appendIdentitySecurityEvent(ctx, tx, securityEventAppend{
		eventType: "DEVICE_TRUSTED", accountID: request.AccountID,
		revision: request.Revision, requestID: request.RequestID,
		evidence: map[string]any{
			"schema_version": 1,
			"device_id":      deviceID,
			"replaced":       false,
			"device_limit":   s.policy.TrustedDeviceLimit,
			"proof":          "EMAIL_VERIFICATION_OTP",
		},
	}); err != nil {
		return DeviceAdmissionResult{}, err
	}
	return DeviceAdmissionResult{
		Admission: AdmitTrustedDevice, TrustState: DeviceTrustEstablished, DeviceID: deviceID,
	}, nil
}
