package identity

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Trusted devices.
//
// A trusted device is a browser an Account has vouched for once, by email OTP,
// and which the server thereafter recognizes by an opaque credential it minted
// itself. It is deliberately *not* an authentication factor: presenting a device
// credential proves nothing about who is asking and grants no capability. It
// only answers "is this the browser the Student already vouched for", which is
// what the two-device policy needs and all it needs.
//
// What is deliberately absent is as important as what is here. There is no
// canvas hash, no font enumeration, no screen geometry, no hardware identifier,
// and no IP-address matching. Those either fail the legitimate Student — the one
// who changed network, updated their browser, or plugged in a monitor — or
// collect far more about them than a two-device limit could justify. The
// server-minted credential does the whole job and reveals nothing.

// TrustedDeviceState is the derived lifecycle of one device record.
type TrustedDeviceState string

const (
	// DevicePending exists but has not completed its OTP challenge. It occupies
	// no slot, so an abandoned challenge cannot consume one of the two.
	DevicePending TrustedDeviceState = "PENDING"
	DeviceTrusted TrustedDeviceState = "TRUSTED"
	DeviceRevoked TrustedDeviceState = "REVOKED"
)

// DeviceRevocationReason mirrors trusted_device_revocation_reason.
type DeviceRevocationReason string

const (
	DeviceRemovedByStudent  DeviceRevocationReason = "STUDENT_REMOVED"
	DeviceReplacedByStudent DeviceRevocationReason = "STUDENT_REPLACED"
	DeviceRevokedByAdmin    DeviceRevocationReason = "ADMIN_REVOKED"
	DeviceRevokedAllByAdmin DeviceRevocationReason = "ADMIN_REVOKED_ALL"
	DeviceRevokedByReset    DeviceRevocationReason = "PASSWORD_RESET"
	DeviceRevokedByRecovery DeviceRevocationReason = "SECURITY_RECOVERY"
	DeviceRevokedBySuspend  DeviceRevocationReason = "ACCOUNT_SUSPENDED"
)

// startsReplacementCooldown reports whether ending a device this way is the
// Student spending their replacement, as opposed to an operator or a recovery
// flow acting on the Account.
//
// Only the Student's own removal or replacement starts the cooldown. An Admin
// revocation and a password reset must not: those are the paths that exist to
// rescue an Account, and making them start a 24-hour wait would turn the
// anti-sharing friction into the lockout it is specifically not meant to be.
func (r DeviceRevocationReason) startsReplacementCooldown() bool {
	return r == DeviceRemovedByStudent || r == DeviceReplacedByStudent
}

func (r DeviceRevocationReason) Valid() bool {
	switch r {
	case DeviceRemovedByStudent, DeviceReplacedByStudent, DeviceRevokedByAdmin,
		DeviceRevokedAllByAdmin, DeviceRevokedByReset, DeviceRevokedByRecovery,
		DeviceRevokedBySuspend:
		return true
	}
	return false
}

// TrustedDevice is one server-side device record.
//
// CredentialDigest is present because the repository reads it; it is never
// carried into a response. LastIPAddress is forensic only and is never compared
// to decide whether a browser matches this record.
type TrustedDevice struct {
	ID               string
	AccountID        string
	CredentialDigest string

	Label          string
	BrowserFamily  string
	PlatformFamily string

	FirstSeenAt time.Time
	LastSeenAt  time.Time
	TrustedAt   *time.Time

	RevokedAt        *time.Time
	RevocationReason *DeviceRevocationReason

	LastIPAddress string
}

// State derives the lifecycle rather than storing it, so a record cannot claim
// to be trusted while carrying a revocation.
func (d TrustedDevice) State() TrustedDeviceState {
	switch {
	case d.RevokedAt != nil:
		return DeviceRevoked
	case d.TrustedAt != nil:
		return DeviceTrusted
	default:
		return DevicePending
	}
}

func (d TrustedDevice) Live() bool { return d.RevokedAt == nil }

// DevicePolicy is the tunable half of the feature. It is a value rather than a
// package-level constant so the limit and the cooldown can move by
// configuration without a code change, and so tests can prove the boundary
// rather than wait 24 hours.
type DevicePolicy struct {
	TrustedDeviceLimit  int
	ReplacementCooldown time.Duration
}

// Validate fails closed. A zero or negative limit would either lock every
// Student out or silently disable the policy, and neither is a state a
// misconfigured deployment should be able to reach by omission.
func (p DevicePolicy) Validate() error {
	if p.TrustedDeviceLimit < 1 {
		return errors.New("trusted device limit must be at least 1")
	}
	if p.TrustedDeviceLimit > 10 {
		return errors.New("trusted device limit above 10 defeats the policy")
	}
	if p.ReplacementCooldown < 0 {
		return errors.New("device replacement cooldown cannot be negative")
	}
	return nil
}

// DeviceAdmission is what the login path learns about the browser in front of
// it, before deciding what authority to hand back.
type DeviceAdmission string

const (
	// AdmitTrustedDevice means this browser already holds a live trusted record
	// for this Account. Nothing is challenged and the session is ordinary.
	AdmitTrustedDevice DeviceAdmission = "TRUSTED"
	// AdmitNewDeviceWithSlot means a free slot exists; the browser must pass an
	// email OTP before it is trusted.
	AdmitNewDeviceWithSlot DeviceAdmission = "OTP_REQUIRED"
	// AdmitNewDeviceAtLimit means the Account is full. The Student is shown
	// their devices and must choose one to remove. This is not a login failure.
	AdmitNewDeviceAtLimit DeviceAdmission = "LIMIT_REACHED"
)

// DecideDeviceAdmission is the pure decision, separated from the transaction
// that acts on it so the ordering can be tested exhaustively.
//
// It counts only trusted, live records. A pending record — a device that
// started a challenge and never finished — is deliberately not counted, or an
// abandoned login on a library computer would consume a slot the Student never
// agreed to give it.
func DecideDeviceAdmission(existing TrustedDevice, found bool, trustedCount int, policy DevicePolicy) DeviceAdmission {
	if found && existing.State() == DeviceTrusted {
		return AdmitTrustedDevice
	}
	if trustedCount < policy.TrustedDeviceLimit {
		return AdmitNewDeviceWithSlot
	}
	return AdmitNewDeviceAtLimit
}

// Replacement cooldown.

// ErrDeviceReplacementCooldown means the Student replaced a device too
// recently. It is a distinct error because the response says so plainly and
// tells them when it lifts: this is friction against sharing, not a security
// refusal to be kept vague.
var ErrDeviceReplacementCooldown = errors.New("device replacement is in cooldown")

// ReplacementState is the per-Account cooldown bookkeeping.
type ReplacementState struct {
	LastReplacementAt *time.Time
	CooldownClearedAt *time.Time
}

// CooldownUntil reports when the Student may replace a device again, or the
// zero time if they may do so now.
//
// An Admin override moves CooldownClearedAt forward rather than deleting
// history, so the trail still shows a replacement happened and that an operator
// released it.
func (s ReplacementState) CooldownUntil(policy DevicePolicy) time.Time {
	if s.LastReplacementAt == nil || policy.ReplacementCooldown <= 0 {
		return time.Time{}
	}
	if s.CooldownClearedAt != nil && !s.CooldownClearedAt.Before(*s.LastReplacementAt) {
		return time.Time{}
	}
	return s.LastReplacementAt.Add(policy.ReplacementCooldown)
}

// CheckReplacementAllowed is the guard the replacement transaction applies.
func (s ReplacementState) CheckReplacementAllowed(policy DevicePolicy, now time.Time) error {
	until := s.CooldownUntil(policy)
	if until.IsZero() || !now.Before(until) {
		return nil
	}
	return fmt.Errorf("%w until %s", ErrDeviceReplacementCooldown, until.UTC().Format(time.RFC3339))
}

// Device labels.
//
// The label exists so a Student can tell "Chrome on Windows" from "Safari on
// iPhone" when choosing which device to remove. It is derived from the
// User-Agent, which is exactly the wrong thing to identify a device by and
// exactly the right thing to *name* one: a browser update changes the string
// and must not change which record matches, so identity stays with the
// credential digest and only the display text is refreshed.
//
// Two families are extracted and nothing else. Versions, build numbers, device
// models, and the rest of the header are discarded rather than stored, because
// a two-device limit cannot justify retaining them.

const (
	unknownBrowserFamily  = "Unknown browser"
	unknownPlatformFamily = "Unknown platform"
	maxDeviceLabelLength  = 64
)

// DeviceIdentity is the coarse, display-only description of a browser.
type DeviceIdentity struct {
	Label          string
	BrowserFamily  string
	PlatformFamily string
}

// DeriveDeviceIdentity reads only what a human needs to recognize their own
// device. Order matters: Edge and Opera both claim Chrome, and Chrome claims
// Safari, so the more specific families are tested first.
func DeriveDeviceIdentity(userAgent string) DeviceIdentity {
	agent := userAgent
	if len(agent) > 512 {
		agent = agent[:512]
	}
	browser := browserFamily(agent)
	platform := platformFamily(agent)
	label := browser + " on " + platform
	if len(label) > maxDeviceLabelLength {
		label = label[:maxDeviceLabelLength]
	}
	return DeviceIdentity{Label: label, BrowserFamily: browser, PlatformFamily: platform}
}

func browserFamily(agent string) string {
	lowered := strings.ToLower(agent)
	switch {
	case strings.Contains(lowered, "edg/"), strings.Contains(lowered, "edga/"):
		return "Edge"
	case strings.Contains(lowered, "opr/"), strings.Contains(lowered, "opera"):
		return "Opera"
	case strings.Contains(lowered, "firefox/"), strings.Contains(lowered, "fxios"):
		return "Firefox"
	case strings.Contains(lowered, "samsungbrowser"):
		return "Samsung Internet"
	case strings.Contains(lowered, "chrome/"), strings.Contains(lowered, "crios"):
		return "Chrome"
	case strings.Contains(lowered, "safari/"):
		return "Safari"
	case lowered == "":
		return unknownBrowserFamily
	}
	return unknownBrowserFamily
}

func platformFamily(agent string) string {
	lowered := strings.ToLower(agent)
	switch {
	case strings.Contains(lowered, "iphone"):
		return "iPhone"
	case strings.Contains(lowered, "ipad"):
		return "iPad"
	case strings.Contains(lowered, "android"):
		return "Android"
	case strings.Contains(lowered, "windows"):
		return "Windows"
	case strings.Contains(lowered, "mac os"), strings.Contains(lowered, "macintosh"):
		return "macOS"
	case strings.Contains(lowered, "cros"):
		return "ChromeOS"
	case strings.Contains(lowered, "linux"):
		return "Linux"
	}
	return unknownPlatformFamily
}
