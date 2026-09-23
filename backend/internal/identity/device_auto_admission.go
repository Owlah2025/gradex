package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

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
		return DeviceAdmissionResult{}, fmt.Errorf("%w: trusted device count exceeds policy", ErrDeviceTrustUnavailable)
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
	return DeviceAdmissionResult{
		Admission: AdmitTrustedDevice, TrustState: DeviceTrustEstablished,
		DeviceID: deviceID, EvictedDeviceID: evictedID,
	}, nil
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
