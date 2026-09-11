package identity

// The session-scoped half of device policy.
//
// Principal deliberately does not carry device trust, and that is not an
// oversight. Principal's own documentation explains why it holds only facts
// derived from the Account: a capability copied onto a session row becomes a
// stale second authority that suspension has to hunt down. Device trust is the
// opposite kind of fact — it is true of *one browser*, not of the Account — so
// it cannot live on Principal without lying about its scope.
//
// It therefore stays a separate input to a separate, narrowing decision.
// Authorize answers "may this Account do this at all"; AuthorizeSessionDevice
// then answers "may it do so from this browser". Account-level refusals still
// win, because the first decision runs first and its denial is returned
// unchanged: a suspended Student on a perfectly trusted device is refused for
// suspension, which is what monitoring needs to see.

// SessionDeviceTrust mirrors the session_device_trust_state enum.
type SessionDeviceTrust string

const (
	// DeviceTrustNotApplicable is an Instructor or Admin family. Device policy
	// is a Student control and these roles are untouched by it.
	DeviceTrustNotApplicable SessionDeviceTrust = "NOT_APPLICABLE"

	// DeviceTrustPending is a browser that authenticated but has not completed
	// device trust. It holds a real session — it has to, in order to answer an
	// OTP and choose a device to remove — but that session is narrowed to
	// exactly those steps.
	DeviceTrustPending SessionDeviceTrust = "PENDING_DEVICE_TRUST"

	// DeviceTrustEstablished is an ordinary device-bound family.
	DeviceTrustEstablished SessionDeviceTrust = "TRUSTED"

	// DeviceTrustLegacyUnbound is a family created before this feature shipped.
	//
	// It exists so the deployment does not log every Student out. A legacy
	// family keeps ordinary non-protected authority and loses exactly one
	// thing: protected learning, until the browser adopts a device. That is the
	// narrowest rule that stops a pre-deployment session from being a permanent
	// hole in the policy, and it costs a Student who is merely reading their
	// dashboard nothing.
	DeviceTrustLegacyUnbound SessionDeviceTrust = "LEGACY_UNBOUND"
)

func (t SessionDeviceTrust) Valid() bool {
	switch t {
	case DeviceTrustNotApplicable, DeviceTrustPending,
		DeviceTrustEstablished, DeviceTrustLegacyUnbound:
		return true
	}
	return false
}

// deviceSelfServiceCapabilities are the only things a pending browser may do:
// finish the challenge, choose a device to remove, change the password if it is
// required of them, or give up and log out. Nothing else — and in particular
// not CapLearningAccess, which is the whole point.
func deviceSelfServiceCapability(c Capability) bool {
	switch c {
	case CapDeviceManagement, CapPasswordChange, CapSessionTerminate:
		return true
	}
	return false
}

// AuthorizeSessionDevice is the single decision point for device-scoped
// authority. It is deny-by-default in the same sense Authorize is: an unknown
// or incoherent trust state narrows to the self-service set rather than
// widening to anything.
func AuthorizeSessionDevice(p Principal, trust SessionDeviceTrust, c Capability) Decision {
	decision := Authorize(p, c)
	if !decision.Allowed {
		return decision
	}

	// Device policy is STUDENT-only, by construction rather than by
	// configuration. An Instructor or Admin session is returned unchanged
	// whatever its recorded trust state, including the LEGACY_UNBOUND value
	// their pre-deployment rows carry.
	if p.Role != RoleStudent {
		return decision
	}

	switch trust {
	case DeviceTrustEstablished:
		return decision

	case DeviceTrustLegacyUnbound:
		// The narrow rollout rule: everything except protected learning, which
		// is the surface the policy exists to protect.
		if c == CapLearningAccess {
			return deny(DenyDeviceAdoptionRequired)
		}
		return decision

	case DeviceTrustPending:
		if deviceSelfServiceCapability(c) {
			return decision
		}
		return deny(DenyDeviceTrustRequired)

	default:
		// NOT_APPLICABLE on a Student family, or a value this build does not
		// know, is a coherence fault. Failing closed to the self-service set
		// keeps the Student able to recover their own Account without granting
		// anything the policy is meant to gate.
		if deviceSelfServiceCapability(c) {
			return decision
		}
		return deny(DenyDeviceTrustRequired)
	}
}
