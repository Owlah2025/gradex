package identity

// Capability is a typed authorization subject. Handlers name the capability
// they require; the policy decides. Nothing authorizes by comparing role
// strings at a call site, which is how a role check gets forgotten on one route
// out of thirty.
//
// The set is closed. A new protected operation adds a constant here and gets a
// decision in authorize(), rather than inventing an ad-hoc check.
type Capability string

const (
	// Available to a restricted bootstrap principal. These two exist so the
	// bootstrap Account can complete the one thing it is permitted to do and
	// nothing else.
	CapPasswordChange   Capability = "PASSWORD_CHANGE"
	CapSessionTerminate Capability = "SESSION_TERMINATE"

	// The capability classes §4.5 names as denied to a restricted principal.
	CapAdminOperations     Capability = "ADMIN_OPERATIONS"
	CapFinancialOperations Capability = "FINANCIAL_OPERATIONS"
	CapSecurityOperations  Capability = "SECURITY_OPERATIONS"
	CapRetentionOperations Capability = "RETENTION_OPERATIONS"
	CapProviderOperations  Capability = "PROVIDER_OPERATIONS"
	CapContentManagement   Capability = "CONTENT_MANAGEMENT"

	// Entitlement-gated Student learning. The capability grants the *class* of
	// action; whether this Student may reach this Course is a separate
	// Entitlement decision owned by S4.
	CapLearningAccess Capability = "LEARNING_ACCESS"
)

// AllCapabilities is the closed set, used by tests to prove the policy is total
// — that every capability has an explicit decision for every principal shape,
// rather than falling through to a default that happens to be right.
var AllCapabilities = []Capability{
	CapPasswordChange,
	CapSessionTerminate,
	CapAdminOperations,
	CapFinancialOperations,
	CapSecurityOperations,
	CapRetentionOperations,
	CapProviderOperations,
	CapContentManagement,
	CapLearningAccess,
}

// DenyReason is the typed reason a decision was negative.
//
// Design §6.1 keeps these in security monitoring, not in the response: telling
// a caller *why* it was denied distinguishes "suspended" from "wrong role" from
// "no such Account", which is exactly the Account-state disclosure §5 forbids.
// The HTTP layer must log this and return the uniform refusal.
type DenyReason string

const (
	DenyNone                   DenyReason = ""
	DenyIncoherentPrincipal    DenyReason = "INCOHERENT_PRINCIPAL"
	DenyAccountSuspended       DenyReason = "ACCOUNT_SUSPENDED"
	DenyAccountUnverified      DenyReason = "ACCOUNT_UNVERIFIED"
	DenyPasswordChangeRequired DenyReason = "PASSWORD_CHANGE_REQUIRED"
	DenyRoleLacksCapability    DenyReason = "ROLE_LACKS_CAPABILITY"
	DenyUnknownCapability      DenyReason = "UNKNOWN_CAPABILITY"
	// DenyPrincipalNotFound is produced by the resolution step rather than by
	// Authorize, but it is a policy outcome and belongs in the same typed set
	// so monitoring sees one vocabulary of refusal reasons.
	DenyPrincipalNotFound DenyReason = "PRINCIPAL_NOT_FOUND"
)

// Decision is the policy's answer.
type Decision struct {
	Allowed bool
	Reason  DenyReason
}

func allow() Decision            { return Decision{Allowed: true, Reason: DenyNone} }
func deny(r DenyReason) Decision { return Decision{Allowed: false, Reason: r} }

// Authorize is the single authorization decision point, and it is
// deny-by-default: every path that does not explicitly allow returns a denial.
//
// Precedence matters and is deliberate:
//
//  1. An incoherent principal is denied outright.
//  2. Suspension overrides everything, including the restricted principal's own
//     password-change permission. A suspended Account must not be able to act.
//  3. The restricted (PASSWORD_CHANGE_REQUIRED) state overrides role. This is
//     the §4.5 rule: until the password is changed, the bootstrap Admin has no
//     Admin, financial, security, retention, provider, or content-management
//     capability, regardless of carrying the ADMIN role.
//  4. An unverified Account may only change its password or end its session.
//  5. Only then does role grant anything.
//
// Reordering 2 and 3 would let a suspended bootstrap Account change its
// password; reordering 3 and 5 would give the bootstrap Admin full Admin
// authority the moment it authenticated, which is the exact defect the
// restricted state exists to prevent.
func Authorize(p Principal, c Capability) Decision {
	if err := p.Valid(); err != nil {
		return deny(DenyIncoherentPrincipal)
	}
	if !known(c) {
		return deny(DenyUnknownCapability)
	}

	if p.Status == StatusSuspended {
		return deny(DenyAccountSuspended)
	}

	if p.Restricted() {
		switch c {
		case CapPasswordChange, CapSessionTerminate:
			return allow()
		default:
			return deny(DenyPasswordChangeRequired)
		}
	}

	if p.Status == StatusPendingVerification {
		switch c {
		case CapPasswordChange, CapSessionTerminate:
			return allow()
		default:
			return deny(DenyAccountUnverified)
		}
	}

	// Every Account in good standing can manage its own credential and end its
	// own session, whatever its role.
	switch c {
	case CapPasswordChange, CapSessionTerminate:
		return allow()
	}

	switch p.Role {
	case RoleAdmin:
		switch c {
		case CapAdminOperations, CapFinancialOperations, CapSecurityOperations,
			CapRetentionOperations, CapProviderOperations, CapContentManagement:
			return allow()
		}
		// Deliberately not CapLearningAccess. Admin access to protected content
		// is a separately audited preview operation (domain design §17), not an
		// ambient consequence of being an Admin.
		return deny(DenyRoleLacksCapability)

	case RoleInstructor:
		switch c {
		case CapContentManagement:
			// The class of action only. Whether this Instructor owns this
			// Course is an ownership decision the handler still makes.
			return allow()
		}
		return deny(DenyRoleLacksCapability)

	case RoleStudent:
		switch c {
		case CapLearningAccess:
			// Again the class only; the Entitlement decision is separate.
			return allow()
		}
		return deny(DenyRoleLacksCapability)
	}

	// Unreachable while Role.Valid() covers the enum, and deliberately a denial
	// rather than a panic: a new role added without a policy branch must fail
	// closed, not crash the process or fall through to allow.
	return deny(DenyRoleLacksCapability)
}

func known(c Capability) bool {
	for _, k := range AllCapabilities {
		if k == c {
			return true
		}
	}
	return false
}
