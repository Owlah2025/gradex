package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

// PasswordChangeCommand is one cookie-authenticated password change.
//
// It carries the same two digests every other session mutation carries — the
// opaque credential and the CSRF token — plus the two plaintexts the credential
// boundary consumes. Neither plaintext is retained: both are handed to the
// domain preparation and go out of scope with it.
type PasswordChangeCommand struct {
	CredentialDigest string
	CSRFDigest       string

	CurrentPassword config.Secret
	NewPassword     config.Secret

	Compromised CompromisedRangeSource
	RequestID   string
}

// ChangePassword is the application entry point for the mandatory and voluntary
// password-change flows.
//
// It adds no domain rule of its own. Everything that decides whether the change
// may happen — Account status, session usability, generation currency, the
// recent-authentication window, the current password, password policy,
// compromised-password screening, Argon2id hashing, restriction removal,
// session rotation, revocation of every other family, and Audit evidence —
// belongs to CompletePasswordChange, which this calls once.
//
// What it does own is the session-authority translation the HTTP boundary needs
// and the domain deliberately does not model: mapping the presented cookie to
// an Account and a generation, and proving the CSRF token.
//
// Kind is always VoluntaryChange, so the current password is always proven.
// The domain permits a voluntary change on a CHANGE_REQUIRED credential — it is
// simply a caller who supplied the current password as well — and requiring it
// is strictly stronger than the mandatory flow's minimum, which treats the
// bootstrap session itself as the proof. The mandatory flow's weaker
// precondition exists for a caller that cannot ask for the old password; a
// browser form can, so the stronger one is what gets mounted. An attacker at an
// unattended restricted session therefore still cannot take the Account over.
func (r *SessionRepository) ChangePassword(
	ctx context.Context,
	command PasswordChangeCommand,
) (SessionGrant, error) {
	if r == nil || r.pool == nil {
		return SessionGrant{}, ErrAuthenticationRequired
	}

	acquired, err := r.pool.Acquire(ctx)
	if err != nil {
		return SessionGrant{}, fmt.Errorf("acquiring a connection for the password change: %w", err)
	}
	defer acquired.Release()
	conn := acquired.Conn()

	admitted, err := r.admitPasswordChange(ctx, conn, command)
	if err != nil {
		return SessionGrant{}, err
	}

	policy, err := r.passwordChangePolicy(admitted.session.Role)
	if err != nil {
		return SessionGrant{}, err
	}

	result, err := CompletePasswordChange(ctx, conn, PasswordChangeRequest{
		AccountID:           admitted.session.AccountID,
		SessionID:           admitted.session.SessionID,
		PresentedGeneration: admitted.session.Generation,
		Kind:                VoluntaryChange,
		CurrentPassword:     command.CurrentPassword,
		NewPassword:         command.NewPassword,
		Compromised:         command.Compromised,
	}, policy, r.now().UTC())
	if err != nil {
		return SessionGrant{}, err
	}

	// The credential state is read back from the completed change rather than
	// carried over from admission: the whole point of the operation is that it
	// is no longer CHANGE_REQUIRED, and reporting the pre-change value would
	// send the browser straight back to the mandatory screen it just left.
	changed := admitted.session
	changed.CredentialState = CredentialActive
	changed.Generation = result.Generation
	changed.IdleExpiresAt = result.IdleExpiresAt
	changed.AbsoluteExpiresAt = result.AbsoluteExpiresAt

	return SessionGrant{
		Session:    changed,
		Credential: result.Credential,
		CSRFToken:  result.CSRFToken,
	}, nil
}

type admittedPasswordChange struct {
	session AuthenticatedSession
}

// admitPasswordChange resolves the presented cookie to a session generation and
// proves the CSRF token, in the same shape Renew and Logout use.
//
// It commits nothing on the success path. Its transaction ends before
// CompletePasswordChange opens its own on the same connection, because a
// connection carries one transaction at a time — and because the domain command
// is the authority on the change, not a step inside a transaction this layer
// controls.
//
// The gap between the two is closed by the domain, not tolerated: the change
// re-locks the Account, credential, and session and refuses unless the
// generation admitted here is still the family's current one. A CSRF token
// binds to exactly one (session, generation) pair, so a proof taken here still
// describes the generation the change commits against.
func (r *SessionRepository) admitPasswordChange(
	ctx context.Context,
	conn *pgx.Conn,
	command PasswordChangeCommand,
) (admittedPasswordChange, error) {
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return admittedPasswordChange{}, fmt.Errorf("beginning password-change admission: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	record, err := loadSessionRecord(ctx, tx, command.CredentialDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return admittedPasswordChange{}, ErrAuthenticationRequired
	}
	if err != nil {
		return admittedPasswordChange{}, fmt.Errorf("loading session for the password change: %w", err)
	}
	if record.credentialRowState == "SUPERSEDED" {
		// A superseded credential presented at a security-sensitive boundary is
		// never the benign in-flight read, so this records evidence and its
		// outcome must commit.
		semanticErr := r.recordSupersededUse(
			ctx, tx, &record, UseSecuritySensitive, command.RequestID,
		)
		err := commitSemantic(ctx, tx, semanticErr)
		committed = true
		return admittedPasswordChange{}, err
	}
	if err := record.usable(r.now().UTC()); err != nil {
		return admittedPasswordChange{}, err
	}
	if !digestEqual(command.CSRFDigest, record.csrfDigest) {
		return admittedPasswordChange{}, ErrSessionCSRFFailed
	}
	return admittedPasswordChange{session: record.session}, nil
}

// passwordChangePolicy selects the role's configured session windows and the
// server CSRF key, so a changed Admin credential does not silently inherit a
// Student's session lifetime.
func (r *SessionRepository) passwordChangePolicy(role Role) (PasswordChangePolicy, error) {
	window := r.window(role)
	policy := PasswordChangePolicy{
		RecentAuthWindow: r.settings.HighestRiskRecentAuthWindow(),
		IdleExpiry:       window.IdleExpiry(),
		AbsoluteExpiry:   window.AbsoluteExpiry(),
		SessionCSRFKey:   r.csrfKey,
	}
	if err := validatePasswordChangePolicy(policy); err != nil {
		return PasswordChangePolicy{}, err
	}
	return policy, nil
}
