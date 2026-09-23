package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrDeviceLimitInvariantViolation = errors.New("trusted device count exceeds policy")

type DeviceLimitInvariantViolation struct {
	TrustedCount int
	Limit        int
}

func (v *DeviceLimitInvariantViolation) Error() string {
	return fmt.Sprintf("%s: count=%d limit=%d", ErrDeviceLimitInvariantViolation, v.TrustedCount, v.Limit)
}

func (v *DeviceLimitInvariantViolation) Unwrap() error { return ErrDeviceLimitInvariantViolation }

func (s *DeviceService) recordLimitInvariantViolation(
	ctx context.Context, accountID, requestID string, violation *DeviceLimitInvariantViolation,
) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO audit_events
		  (actor_role, actor_descriptor, action, module, target_type, target_id,
		   reason, metadata, correlation_id)
		 VALUES ('SYSTEM', 'gradex-device-policy', 'DEVICE_LIMIT_INVARIANT_VIOLATION',
		         'IDENTITY_AND_ACCESS', 'ACCOUNT', $1,
		         'Trusted device count exceeded configured limit',
		         jsonb_build_object('trusted_count', $2::int, 'device_limit', $3::int), $4)`,
		accountID, violation.TrustedCount, violation.Limit, requestID)
	if err != nil {
		return fmt.Errorf("auditing device-limit invariant violation: %w", err)
	}
	return nil
}

type newDeviceAdmissionState struct {
	existing     TrustedDevice
	found        bool
	trustedCount int
	now          time.Time
}

// admitNewDevice runs under the Account row lock shared by login and adoption.
func (s *DeviceService) admitNewDevice(
	ctx context.Context, tx pgx.Tx, request DeviceAdmissionRequest,
	state newDeviceAdmissionState,
) (DeviceAdmissionResult, error) {
	if state.trustedCount > s.policy.TrustedDeviceLimit {
		return DeviceAdmissionResult{}, &DeviceLimitInvariantViolation{
			TrustedCount: state.trustedCount, Limit: s.policy.TrustedDeviceLimit,
		}
	}
	trustedAt, err := nextDeviceTrustedAt(ctx, tx, request.AccountID, state.now)
	if err != nil {
		return DeviceAdmissionResult{}, err
	}

	evictedID := ""
	if state.trustedCount >= s.policy.TrustedDeviceLimit {
		evictedID, err = latestTrustedDeviceID(ctx, tx, request.AccountID)
		if err != nil {
			return DeviceAdmissionResult{}, err
		}
		_, err = s.revokeDeviceInTransaction(ctx, tx, deviceRevocation{
			AccountID: request.AccountID, DeviceID: evictedID,
			Reason: DeviceAutoReplaced, Revision: request.Revision,
			RequestID: request.RequestID, Now: state.now,
		})
		if err != nil {
			return DeviceAdmissionResult{}, err
		}
	}

	deviceID := state.existing.ID
	if state.found {
		if err := touchDevice(ctx, tx, deviceID, request, state.now); err != nil {
			return DeviceAdmissionResult{}, err
		}
	} else {
		deviceID, err = insertPendingDevice(ctx, tx, request, state.now)
		if err != nil {
			return DeviceAdmissionResult{}, err
		}
	}
	if err := markDeviceTrusted(ctx, tx, request.AccountID, deviceID, trustedAt); err != nil {
		return DeviceAdmissionResult{}, err
	}
	if err := appendIdentitySecurityEvent(ctx, tx, securityEventAppend{
		eventType: "DEVICE_TRUSTED", accountID: request.AccountID,
		revision: request.Revision, requestID: request.RequestID,
		evidence: automaticDeviceTrustEvidence(deviceID, evictedID, s.policy.TrustedDeviceLimit),
	}); err != nil {
		return DeviceAdmissionResult{}, err
	}
	if evictedID != "" {
		if err := appendAutomaticReplacementAudit(ctx, tx, automaticReplacementAudit{
			accountID: request.AccountID, evictedID: evictedID, admittedID: deviceID,
			limit: s.policy.TrustedDeviceLimit, requestID: request.RequestID,
		}); err != nil {
			return DeviceAdmissionResult{}, err
		}
	}
	return DeviceAdmissionResult{
		Admission: AdmitTrustedDevice, TrustState: DeviceTrustEstablished,
		DeviceID: deviceID, EvictedDeviceID: evictedID,
	}, nil
}

type automaticReplacementAudit struct {
	accountID  string
	evictedID  string
	admittedID string
	limit      int
	requestID  string
}

func appendAutomaticReplacementAudit(ctx context.Context, tx pgx.Tx, audit automaticReplacementAudit) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO audit_events
		  (actor_role, actor_descriptor, action, module, target_type, target_id,
		   reason, metadata, correlation_id)
		 VALUES ('SYSTEM', 'gradex-device-policy', 'AUTO_DEVICE_REPLACED',
		         'IDENTITY_AND_ACCESS', 'TRUSTED_DEVICE', $1,
		         'Automatic trusted-device slot rotation',
		         jsonb_build_object('account_id', $2::text,
		                            'evicted_device_id', $3::text,
		                            'admitted_device_id', $1::text,
		                            'device_limit', $4::int,
		                            'replacement_mode', 'AUTOMATIC'), $5)`,
		audit.admittedID, audit.accountID, audit.evictedID, audit.limit, audit.requestID)
	if err != nil {
		return fmt.Errorf("auditing automatic device replacement: %w", err)
	}
	return nil
}

func automaticDeviceTrustEvidence(deviceID, evictedID string, limit int) map[string]any {
	evidence := map[string]any{
		"schema_version": 1, "device_id": deviceID,
		"admission_mode": "AUTOMATIC", "device_limit": limit,
	}
	if evictedID != "" {
		evidence["replacement_mode"] = "AUTOMATIC"
		evidence["evicted_device_id"] = evictedID
	}
	return evidence
}

func latestTrustedDeviceID(ctx context.Context, tx pgx.Tx, accountID string) (string, error) {
	var deviceID string
	err := tx.QueryRow(ctx,
		`SELECT id::text FROM identity_trusted_devices
		  WHERE account_id = $1::uuid AND trusted_at IS NOT NULL AND revoked_at IS NULL
		  ORDER BY trusted_at DESC, id DESC LIMIT 1 FOR UPDATE`,
		accountID,
	).Scan(&deviceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("%w: no device to replace", ErrDeviceTrustUnavailable)
	}
	if err != nil {
		return "", fmt.Errorf("selecting device to replace: %w", err)
	}
	return deviceID, nil
}

// The Account lock serializes admissions. Advance trusted_at on tied clocks so
// later admissions always sort after earlier ones, even with fixed test clocks.
func nextDeviceTrustedAt(ctx context.Context, tx pgx.Tx, accountID string, now time.Time) (time.Time, error) {
	var latest *time.Time
	if err := tx.QueryRow(ctx,
		`SELECT max(trusted_at) FROM identity_trusted_devices WHERE account_id = $1::uuid`,
		accountID,
	).Scan(&latest); err != nil {
		return time.Time{}, fmt.Errorf("reading latest device admission: %w", err)
	}
	if latest != nil && !now.After(*latest) {
		return latest.Add(time.Microsecond), nil
	}
	return now, nil
}
