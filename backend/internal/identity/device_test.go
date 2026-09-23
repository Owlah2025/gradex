package identity

import (
	"errors"
	"testing"
	"time"
)

func trustedAt(t time.Time) *time.Time { return &t }

func TestDeviceStateFollowsTrustAndRevocation(t *testing.T) {
	now := time.Now().UTC()
	for _, entry := range []struct {
		device TrustedDevice
		want   TrustedDeviceState
	}{
		{TrustedDevice{}, DevicePending},
		{TrustedDevice{TrustedAt: trustedAt(now)}, DeviceTrusted},
		{TrustedDevice{TrustedAt: trustedAt(now), RevokedAt: trustedAt(now)}, DeviceRevoked},
	} {
		if got := entry.device.State(); got != entry.want {
			t.Fatalf("device state = %s, want %s", got, entry.want)
		}
	}
}

func TestAutomaticReplacementReasonDoesNotStartManualCooldown(t *testing.T) {
	if !DeviceAutoReplaced.Valid() || DeviceAutoReplaced.startsReplacementCooldown() {
		t.Fatal("automatic rotation must be auditable without starting manual cooldown")
	}
}

// The cooldown is friction against sharing, and it has to lift on its own.
func TestReplacementCooldownLiftsOnItsOwn(t *testing.T) {
	policy := DevicePolicy{TrustedDeviceLimit: 2, ReplacementCooldown: 24 * time.Hour}
	replaced := time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)
	state := ReplacementState{LastReplacementAt: &replaced}

	if err := state.CheckReplacementAllowed(policy, replaced.Add(time.Hour)); !errors.Is(err, ErrDeviceReplacementCooldown) {
		t.Fatalf("one hour later = %v, want a cooldown refusal", err)
	}
	if err := state.CheckReplacementAllowed(policy, replaced.Add(24*time.Hour)); err != nil {
		t.Fatalf("exactly 24 hours later = %v, want the cooldown to have lifted", err)
	}
}

// An operator override releases the wait without deleting the history that
// caused it.
func TestAdminClearanceReleasesTheCooldown(t *testing.T) {
	policy := DevicePolicy{TrustedDeviceLimit: 2, ReplacementCooldown: 24 * time.Hour}
	replaced := time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)
	cleared := replaced.Add(time.Hour)
	state := ReplacementState{LastReplacementAt: &replaced, CooldownClearedAt: &cleared}

	if err := state.CheckReplacementAllowed(policy, cleared); err != nil {
		t.Fatalf("after an operator override = %v, want it allowed", err)
	}
	// A clearance that predates the replacement is stale and must not apply, or
	// one override would exempt an Account forever.
	stale := replaced.Add(-time.Hour)
	state.CooldownClearedAt = &stale
	if err := state.CheckReplacementAllowed(policy, replaced.Add(time.Hour)); !errors.Is(err, ErrDeviceReplacementCooldown) {
		t.Fatalf("a stale clearance = %v, want the cooldown still in force", err)
	}
}

// Losing a device must not permanently lock the Account: with no configured
// cooldown, or with no history, a replacement is always available.
func TestNoCooldownIsConfiguredOrRecorded(t *testing.T) {
	now := time.Now().UTC()
	if err := (ReplacementState{}).CheckReplacementAllowed(
		DevicePolicy{TrustedDeviceLimit: 2, ReplacementCooldown: 24 * time.Hour}, now,
	); err != nil {
		t.Fatalf("an Account that never replaced anything = %v, want allowed", err)
	}
	replaced := now.Add(-time.Minute)
	if err := (ReplacementState{LastReplacementAt: &replaced}).CheckReplacementAllowed(
		DevicePolicy{TrustedDeviceLimit: 2, ReplacementCooldown: 0}, now,
	); err != nil {
		t.Fatalf("a deployment with the cooldown disabled = %v, want allowed", err)
	}
}

// A misconfigured policy is refused rather than silently reinterpreted.
func TestDevicePolicyFailsClosedOnBadValues(t *testing.T) {
	for name, policy := range map[string]DevicePolicy{
		"a zero limit disables the whole control":  {TrustedDeviceLimit: 0},
		"a negative limit locks everyone out":      {TrustedDeviceLimit: -1},
		"an absurd limit defeats the policy":       {TrustedDeviceLimit: 99},
		"a negative cooldown is not a shorter one": {TrustedDeviceLimit: 2, ReplacementCooldown: -time.Hour},
	} {
		t.Run(name, func(t *testing.T) {
			if err := policy.Validate(); err == nil {
				t.Fatal("the policy was accepted")
			}
		})
	}
	if err := (DevicePolicy{TrustedDeviceLimit: 2, ReplacementCooldown: 24 * time.Hour}).Validate(); err != nil {
		t.Fatalf("the production policy was rejected: %v", err)
	}
}

// Labels are for humans. They must never become identity: a browser update
// changes the User-Agent, and the Student must still be recognised.
func TestDeviceLabelsAreCoarseAndDisplayOnly(t *testing.T) {
	cases := map[string]DeviceIdentity{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36": {
			Label: "Chrome on Windows", BrowserFamily: "Chrome", PlatformFamily: "Windows",
		},
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1": {
			Label: "Safari on iPhone", BrowserFamily: "Safari", PlatformFamily: "iPhone",
		},
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0": {
			Label: "Edge on Windows", BrowserFamily: "Edge", PlatformFamily: "Windows",
		},
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Firefox/121.0": {
			Label: "Firefox on macOS", BrowserFamily: "Firefox", PlatformFamily: "macOS",
		},
		"": {Label: "Unknown browser on Unknown platform", BrowserFamily: "Unknown browser", PlatformFamily: "Unknown platform"},
	}
	for agent, want := range cases {
		got := DeriveDeviceIdentity(agent)
		if got != want {
			t.Fatalf("DeriveDeviceIdentity(%.40q) = %+v, want %+v", agent, got, want)
		}
	}

	// Nothing version-specific survives. A build number in a stored label would
	// be gratuitous retention and would change on every browser update.
	identity := DeriveDeviceIdentity(
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/120.0.6099.109 Safari/537.36")
	if identity.Label != "Chrome on Windows" {
		t.Fatalf("label = %q, want no version detail", identity.Label)
	}

	// An absurd header cannot produce an unbounded label.
	long := make([]byte, 4096)
	for i := range long {
		long[i] = 'A'
	}
	if got := DeriveDeviceIdentity(string(long)); len(got.Label) > maxDeviceLabelLength {
		t.Fatalf("label length = %d, want at most %d", len(got.Label), maxDeviceLabelLength)
	}
}

// Only the Student's own removal spends their replacement. An operator
// revocation and a password reset are rescue paths and must not start a wait.
func TestOnlyStudentActionStartsTheCooldown(t *testing.T) {
	starts := map[DeviceRevocationReason]bool{
		DeviceRemovedByStudent:  true,
		DeviceReplacedByStudent: true,
		DeviceRevokedByAdmin:    false,
		DeviceRevokedAllByAdmin: false,
		DeviceRevokedByReset:    false,
		DeviceRevokedByRecovery: false,
		DeviceRevokedBySuspend:  false,
	}
	for reason, want := range starts {
		if got := reason.startsReplacementCooldown(); got != want {
			t.Fatalf("%s starts cooldown = %v, want %v", reason, got, want)
		}
		if !reason.Valid() {
			t.Fatalf("%s is not a known reason", reason)
		}
	}
}

// The opaque device credential is a credential, not an identifier: full
// entropy, digested at rest, and shape-checked before it can reach a query.
func TestOpaqueCredentialProperties(t *testing.T) {
	first, firstDigest, err := NewOpaqueCredential()
	if err != nil {
		t.Fatalf("minting: %v", err)
	}
	second, secondDigest, err := NewOpaqueCredential()
	if err != nil {
		t.Fatalf("minting: %v", err)
	}
	if first.Expose() == second.Expose() || firstDigest == secondDigest {
		t.Fatal("two credentials collided")
	}
	if !ValidOpaqueCredential(first.Expose()) {
		t.Fatal("a freshly minted credential failed its own shape check")
	}
	if DigestOpaqueCredential(first.Expose()) != firstDigest {
		t.Fatal("the returned digest is not the digest of the returned credential")
	}
	if first.Expose() == firstDigest {
		t.Fatal("the digest is the credential")
	}
	if !OpaqueDigestEqual(firstDigest, firstDigest) || OpaqueDigestEqual(firstDigest, secondDigest) {
		t.Fatal("digest comparison is wrong")
	}
	for _, malformed := range []string{"", "short", first.Expose() + "=", first.Expose() + "A", "!!!"} {
		if ValidOpaqueCredential(malformed) {
			t.Fatalf("%q passed the shape check", malformed)
		}
	}
}

// A device credential and a session credential must not be interchangeable in
// meaning even though they share one cryptographic primitive.
func TestDeviceCredentialsAreNotSessionCredentials(t *testing.T) {
	credential, digest, err := NewOpaqueCredential()
	if err != nil {
		t.Fatalf("minting: %v", err)
	}
	// The shared primitive is genuinely shared — the session digest of the same
	// value is the same digest — which is the point: one reviewed derivation,
	// two unrelated meanings held apart by where each value is stored and what
	// each is allowed to authorize.
	if DigestToken(credential.Expose()) != digest {
		t.Fatal("the two callers of the primitive derive different digests")
	}
}
