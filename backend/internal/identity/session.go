package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

// Session credential sizing. 32 bytes of CSPRNG output is the security floor
// for a bearer-equivalent value; the cookie carries the base64url encoding.
const sessionCredentialBytes = 32

// SessionState mirrors the session_state enum.
type SessionState string

const (
	SessionActive  SessionState = "ACTIVE"
	SessionRevoked SessionState = "REVOKED"
	SessionExpired SessionState = "EXPIRED"
)

// RevocationReason mirrors session_revocation_reason.
type RevocationReason string

const (
	RevokedByLogout           RevocationReason = "LOGOUT"
	RevokedByLogoutAll        RevocationReason = "LOGOUT_ALL"
	RevokedByPasswordChange   RevocationReason = "PASSWORD_CHANGE"
	RevokedByPasswordReset    RevocationReason = "PASSWORD_RESET"
	RevokedByAccountSuspended RevocationReason = "ACCOUNT_SUSPENDED"
	RevokedByReuseDetected    RevocationReason = "REUSE_DETECTED"
	RevokedByAdmin            RevocationReason = "ADMIN_REVOKED"
)

// Session is the server-authoritative family record.
type Session struct {
	ID                string
	AccountID         string
	AdmittedEpoch     int
	State             SessionState
	CurrentGeneration int

	AuthenticatedAt   time.Time
	ReauthenticatedAt *time.Time
	LastActivityAt    time.Time

	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
}

// IssuedCredential is a freshly minted credential: the plaintext values the
// browser receives, and the digests the database stores.
//
// The plaintext fields are config.Secret so that logging or formatting the
// struct cannot emit a usable session cookie. Only the cookie writer exposes
// them, and only after the transaction that stored their digests has committed.
type IssuedCredential struct {
	Credential       config.Secret
	CSRFToken        config.Secret
	CredentialDigest string
	CSRFDigest       string
}

// NewSessionCredential mints a credential and CSRF token with their digests.
//
// The digest is a plain SHA-256, not a password hash. That is deliberate and is
// not the same decision as password storage: these values are 256 bits of
// CSPRNG output with no structure to guess, so the slow-hash property that
// protects a human-chosen password buys nothing here — while costing an Argon2id
// computation on every single authenticated request.
func NewSessionCredential() (IssuedCredential, error) {
	credential, err := randomToken()
	if err != nil {
		return IssuedCredential{}, fmt.Errorf("generating session credential: %w", err)
	}
	csrf, err := randomToken()
	if err != nil {
		return IssuedCredential{}, fmt.Errorf("generating CSRF token: %w", err)
	}

	return IssuedCredential{
		Credential:       config.NewSecret(credential),
		CSRFToken:        config.NewSecret(csrf),
		CredentialDigest: DigestToken(credential),
		CSRFDigest:       DigestToken(csrf),
	}, nil
}

func randomToken() (string, error) {
	b := make([]byte, sessionCredentialBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// DigestToken is the one-way transform applied before a session value touches
// the database. A leak of the sessions tables must not yield usable cookies.
func DigestToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawStdEncoding.EncodeToString(sum[:])
}

// Recent-authentication outcomes.
var (
	// ErrRecentAuthRequired means the session authenticated too long ago for a
	// sensitive operation.
	ErrRecentAuthRequired = errors.New("recent authentication is required")
	// ErrSessionNotUsable means the session is revoked, expired, or admitted
	// under a superseded Account epoch.
	ErrSessionNotUsable = errors.New("session is not usable")
	// ErrStaleGeneration means the caller presented a credential generation
	// that is no longer current.
	ErrStaleGeneration = errors.New("session credential generation is stale")
	// ErrCurrentPasswordRequired means a voluntary change did not prove the
	// existing password.
	ErrCurrentPasswordRequired = errors.New("the current password is required")
	// ErrCurrentPasswordIncorrect means it was proven wrongly.
	ErrCurrentPasswordIncorrect = errors.New("the current password is incorrect")
)

// LastPrimaryAuthentication is when the password was last actually proven.
//
// Reauthentication counts and is more recent when present. Activity does not:
// a session that has been clicking around for six hours has recent activity and
// six-hour-old authentication, and only the latter may satisfy a sensitive
// operation. Conflating them would let an unattended open tab authorize a
// password change.
func (s Session) LastPrimaryAuthentication() time.Time {
	if s.ReauthenticatedAt != nil && s.ReauthenticatedAt.After(s.AuthenticatedAt) {
		return *s.ReauthenticatedAt
	}
	return s.AuthenticatedAt
}

// Usable reports whether the session may act at all, given the Account's
// current epoch and the present time.
func (s Session) Usable(accountEpoch int, now time.Time) error {
	if s.State != SessionActive {
		return fmt.Errorf("%w: state %s", ErrSessionNotUsable, s.State)
	}
	// The epoch check is what makes suspension and logout-all immediate: the
	// Account's epoch moves, and every family admitted under the old one stops
	// being usable without any row here being rewritten.
	if s.AdmittedEpoch != accountEpoch {
		return fmt.Errorf("%w: admitted under epoch %d, Account is at %d",
			ErrSessionNotUsable, s.AdmittedEpoch, accountEpoch)
	}
	if !now.Before(s.IdleExpiresAt) {
		return fmt.Errorf("%w: idle expiry passed", ErrSessionNotUsable)
	}
	if !now.Before(s.AbsoluteExpiresAt) {
		return fmt.Errorf("%w: absolute expiry passed", ErrSessionNotUsable)
	}
	return nil
}

// PasswordChangeKind distinguishes the two flows, which have genuinely
// different preconditions rather than one being a relaxed version of the other.
type PasswordChangeKind int

const (
	// BootstrapMandatoryChange is the first change on an Account whose
	// credential is CHANGE_REQUIRED.
	//
	// The bootstrap session must still be inside the recent-authentication
	// window. It does not separately demand the current password: the operator
	// already proved that temporary value when the session was established.
	BootstrapMandatoryChange PasswordChangeKind = iota

	// VoluntaryChange is an ordinary later change. It requires the current
	// password *and* a session inside the recent-authentication window, because
	// here the threat is an attacker at an unattended authenticated session,
	// which neither check alone stops.
	VoluntaryChange
)

func (k PasswordChangeKind) String() string {
	if k == BootstrapMandatoryChange {
		return "BOOTSTRAP_MANDATORY"
	}
	return "VOLUNTARY"
}

// RequiresCurrentPassword reports whether the flow must prove the existing
// password.
func (k PasswordChangeKind) RequiresCurrentPassword() bool { return k == VoluntaryChange }

// CheckRecentAuthentication applies the recent-authentication rule to every
// password change.
func CheckRecentAuthentication(s Session, window time.Duration, now time.Time) error {
	if window <= 0 {
		// A non-positive window would silently accept any age. Refuse rather
		// than interpret a misconfiguration as "no requirement".
		return fmt.Errorf("%w: no recent-authentication window is configured", ErrRecentAuthRequired)
	}
	age := now.Sub(s.LastPrimaryAuthentication())
	if age > window {
		return fmt.Errorf("%w: last authenticated %s ago, window is %s",
			ErrRecentAuthRequired, age.Truncate(time.Second), window)
	}
	return nil
}
