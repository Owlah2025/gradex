package identity

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

// Rollout adoption for session families that predate device policy.
//
// The alternative designs were both worse. Revoking every live Student session
// at deployment would be a mass logout on a live product for a control the
// Student has not been told about yet. Treating a NULL device binding as
// trusted would leave every pre-deployment session as a permanent exemption
// from the policy, which an account-sharing pair could simply keep alive.
//
// So a legacy family keeps ordinary authority and loses exactly one capability
// — protected learning — until the browser holding it adopts a device. The
// Student notices at the moment they open a lesson, which is also the moment
// the explanation makes sense to them.

// DeviceAdoptionRequest is a legacy session offering its browser for binding.
type DeviceAdoptionRequest struct {
	AccountID string
	SessionID string
	Role      Role
	Email     string
	Locale    Locale

	PresentedDigest string
	UserAgent       string
	SourceAddress   string
	RequestID       string

	Reservation outbox.ProtectedPayloadReservation
}

// AdoptSession binds a legacy family to a device, challenging the browser first
// when it is not already trusted.
//
// A browser that already holds a live trusted record for this Account is bound
// immediately and without a code. That is not a shortcut around the policy: the
// Student proved this exact browser once already, the record is still live, and
// mailing them a second code to re-prove the same browser would be friction
// with no security content. A browser that is *not* already trusted goes
// through the identical admission decision a new login does, including the
// limit and the replacement flow.
func (s *DeviceService) AdoptSession(
	ctx context.Context,
	request DeviceAdoptionRequest,
) (DeviceAdmissionResult, error) {
	if request.Role != RoleStudent {
		return DeviceAdmissionResult{
			Admission: AdmitTrustedDevice, TrustState: DeviceTrustNotApplicable,
		}, nil
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return DeviceAdmissionResult{}, fmt.Errorf("beginning device adoption: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	revision, err := lockAccountForDevicePolicy(ctx, tx, request.AccountID)
	if err != nil {
		return DeviceAdmissionResult{}, err
	}

	result, err := s.admitInTransaction(ctx, tx, DeviceAdmissionRequest{
		AccountID: request.AccountID, Revision: revision, Role: request.Role,
		Email: request.Email, Locale: request.Locale,
		PresentedDigest: request.PresentedDigest, UserAgent: request.UserAgent,
		SourceAddress: request.SourceAddress, RequestID: request.RequestID,
		Reservation: request.Reservation,
	})
	if err != nil {
		return DeviceAdmissionResult{}, err
	}

	if result.Admission == AdmitTrustedDevice {
		if err := bindSessionToDevice(ctx, tx, request.SessionID, request.AccountID, result.DeviceID); err != nil {
			return DeviceAdmissionResult{}, err
		}
		if err := appendIdentitySecurityEvent(ctx, tx, securityEventAppend{
			eventType: "DEVICE_ADOPTED_LEGACY_SESSION", accountID: request.AccountID,
			revision: revision, requestID: request.RequestID,
			evidence: map[string]any{
				"schema_version": 1,
				"device_id":      result.DeviceID,
			},
		}); err != nil {
			return DeviceAdmissionResult{}, err
		}
	}
	// A legacy family awaiting a code is deliberately left as it is rather than
	// downgraded to PENDING_DEVICE_TRUST. Downgrading would strip the Student's
	// ordinary browsing mid-session as a side effect of opening a lesson, and
	// the family already cannot reach protected learning while unbound — which
	// is the only thing the downgrade would have added.
	if err := tx.Commit(ctx); err != nil {
		return DeviceAdmissionResult{}, fmt.Errorf("committing device adoption: %w", err)
	}
	return result, nil
}

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

// accountContact reads the facts a device challenge needs about an Account.
//
// Read here rather than passed in by the HTTP layer, so no handler has to hold
// a Student's email address in order to ask for a code to be mailed to it.
type accountContact struct {
	email    string
	locale   Locale
	role     Role
	revision int
}

func (s *DeviceService) accountContact(ctx context.Context, accountID string) (accountContact, error) {
	var contact accountContact
	if err := s.pool.QueryRow(ctx,
		`SELECT email, locale, role::text, revision FROM accounts WHERE id = $1::uuid`,
		accountID,
	).Scan(&contact.email, &contact.locale, &contact.role, &contact.revision); err != nil {
		return accountContact{}, fmt.Errorf("loading Account contact: %w", err)
	}
	return contact, nil
}

// ResendForAccount is the handler-facing resend.
func (s *DeviceService) ResendForAccount(ctx context.Context, accountID, userAgent, requestID string) (DeviceChallenge, error) {
	contact, err := s.accountContact(ctx, accountID)
	if err != nil {
		return DeviceChallenge{}, err
	}
	return s.ResendDeviceTrustOTP(ctx, ResendDeviceTrustRequest{
		AccountID: accountID, Email: contact.email, Locale: contact.locale,
		UserAgent: userAgent, RequestID: requestID,
	})
}

// AdoptForSession is the handler-facing legacy adoption.
func (s *DeviceService) AdoptForSession(
	ctx context.Context,
	accountID, sessionID string,
	device DeviceContext,
	requestID string,
) (DeviceAdmissionResult, error) {
	contact, err := s.accountContact(ctx, accountID)
	if err != nil {
		return DeviceAdmissionResult{}, err
	}
	reservation, err := s.reserveChallengePayload(ctx)
	if err != nil {
		return DeviceAdmissionResult{}, err
	}
	return s.AdoptSession(ctx, DeviceAdoptionRequest{
		AccountID: accountID, SessionID: sessionID, Role: contact.role,
		Email: contact.email, Locale: contact.locale,
		PresentedDigest: device.CredentialDigest, UserAgent: device.UserAgent,
		SourceAddress: device.SourceAddress, RequestID: requestID,
		Reservation: reservation,
	})
}
