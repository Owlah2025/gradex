package identity

import "fmt"

// Role is an Account's single immutable role. The database enforces both the
// closed set and the immutability; this mirrors it so authorization does not
// have to work with free-form strings.
type Role string

const (
	RoleStudent    Role = "STUDENT"
	RoleInstructor Role = "INSTRUCTOR"
	RoleAdmin      Role = "ADMIN"
)

func (r Role) Valid() bool {
	switch r {
	case RoleStudent, RoleInstructor, RoleAdmin:
		return true
	}
	return false
}

// AccountStatus is the Account's lifecycle state.
type AccountStatus string

const (
	StatusPendingVerification AccountStatus = "PENDING_VERIFICATION"
	StatusActive              AccountStatus = "ACTIVE"
	StatusSuspended           AccountStatus = "SUSPENDED"
)

func (s AccountStatus) Valid() bool {
	switch s {
	case StatusPendingVerification, StatusActive, StatusSuspended:
		return true
	}
	return false
}

// CredentialState is the password credential's own state, deliberately separate
// from AccountStatus.
//
// This separation is the whole reason link 1 shaped the schema the way it did:
// the bootstrap Administrator is ACTIVE *and* required to change its password,
// which are two independent facts. Authorization reads both.
type CredentialState string

const (
	CredentialActive         CredentialState = "ACTIVE"
	CredentialChangeRequired CredentialState = "CHANGE_REQUIRED"
)

func (c CredentialState) Valid() bool {
	switch c {
	case CredentialActive, CredentialChangeRequired:
		return true
	}
	return false
}

// Principal is the authenticated caller as authorization sees it.
//
// It is derived from Account and credential state on every request rather than
// carried in the session. A capability column on `sessions` would be a
// second, stale copy of authority: suspending an Account or clearing
// PASSWORD_CHANGE_REQUIRED would have to find and rewrite every session row to
// take effect, and any it missed would keep granting access. Deriving means the
// next request after a state change already sees the new state — which is also
// what makes immediate suspension enforceable in S1C.
type Principal struct {
	AccountID       string
	Role            Role
	Status          AccountStatus
	CredentialState CredentialState
}

// Restricted reports whether this principal is in the constrained
// password-change state.
func (p Principal) Restricted() bool { return p.CredentialState == CredentialChangeRequired }

// Valid reports whether the principal is internally coherent. An incoherent
// principal is never authorized: it means a row was read that the schema should
// not have permitted, and guessing at intent would be worse than denying.
func (p Principal) Valid() error {
	if p.AccountID == "" {
		return fmt.Errorf("principal has no Account ID")
	}
	if !p.Role.Valid() {
		return fmt.Errorf("principal has an unknown role %q", p.Role)
	}
	if !p.Status.Valid() {
		return fmt.Errorf("principal has an unknown status %q", p.Status)
	}
	if !p.CredentialState.Valid() {
		return fmt.Errorf("principal has an unknown credential state %q", p.CredentialState)
	}
	return nil
}
