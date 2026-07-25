package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

// PasswordChangeRequest is the complete input to a password change.
//
// There is deliberately no HTTP handler for this yet. Link 4 builds the
// preconditions, locking, and validation; link 5 adds the atomic completion
// that clears CHANGE_REQUIRED and rotates the session. Exposing a route now
// would mean shipping an operation that can update a password and leave the old
// session valid — the exact half-completed state the two links are treated as
// one boundary to prevent.
type PasswordChangeRequest struct {
	AccountID string
	SessionID string

	// PresentedGeneration is the credential generation the caller authenticated
	// with. A stale value means the session was rotated underneath this
	// request, and the change must not commit against it.
	PresentedGeneration int

	Kind PasswordChangeKind

	// CurrentPassword is required for VoluntaryChange and ignored for
	// BootstrapMandatoryChange, where the authenticated bootstrap session is
	// itself the recent primary authentication.
	CurrentPassword config.Secret
	NewPassword     config.Secret

	Compromised CompromisedChecker
}

// PreparedPasswordChange is a validated, not-yet-applied change.
//
// It holds the new Argon2id hash and the state the transaction proved. Applying
// it — updating the credential, clearing CHANGE_REQUIRED, superseding the old
// session credential, creating the replacement generation, revoking other
// sessions, and writing evidence — is link 5, and must happen inside the same
// transaction that produced this value. A PreparedPasswordChange carried out of
// its transaction is meaningless: every precondition it records could have
// changed.
type PreparedPasswordChange struct {
	AccountID string
	SessionID string

	// NewPasswordHash is the encoded Argon2id hash. The plaintext it came from
	// no longer exists by the time this is returned.
	NewPasswordHash config.Secret

	// Generation the session is currently on, proven under lock.
	CurrentGeneration int

	// Kind drives link 5's revocation policy: a voluntary change revokes all
	// other sessions, and the bootstrap mandatory change has no others to
	// revoke.
	Kind PasswordChangeKind

	// RevokeOtherSessions is the policy decision, computed here so link 5
	// applies it rather than re-deciding it.
	RevokeOtherSessions bool
}

// PreparePasswordChange locks the Account, its credential, and the Session, then
// rechecks every precondition and produces the new hash.
//
// It performs no mutation. That is the point of the split: everything that can
// fail — a suspended Account, a superseded epoch, a stale generation, a wrong
// current password, a weak new password — fails here, before anything has
// changed, so the old password, restriction state, and session remain
// authoritative on every failure path.
//
// The caller must hold an open transaction and must apply the result inside it.
func PreparePasswordChange(
	ctx context.Context,
	tx pgx.Tx,
	req PasswordChangeRequest,
	recentAuthWindow time.Duration,
	now time.Time,
) (PreparedPasswordChange, error) {
	if req.AccountID == "" || req.SessionID == "" {
		return PreparedPasswordChange{}, errors.New("password change requires an Account and a Session")
	}

	// Lock the Account row first, then the credential, then the session, in that
	// order everywhere. A consistent lock order is what keeps two concurrent
	// changes from deadlocking instead of one simply waiting.
	var (
		role         Role
		status       AccountStatus
		accountEpoch int
	)
	err := tx.QueryRow(ctx,
		`SELECT role::text, status::text, session_epoch
		   FROM accounts WHERE id = $1::uuid FOR UPDATE`,
		req.AccountID,
	).Scan(&role, &status, &accountEpoch)
	if errors.Is(err, pgx.ErrNoRows) {
		return PreparedPasswordChange{}, ErrPrincipalNotFound
	}
	if err != nil {
		return PreparedPasswordChange{}, fmt.Errorf("locking Account: %w", err)
	}

	// Recheck status under the lock. A suspension that landed between the
	// request being admitted and this transaction starting must win.
	if status == StatusSuspended {
		return PreparedPasswordChange{}, fmt.Errorf("%w: Account is suspended", ErrSessionNotUsable)
	}

	var (
		storedHash      string
		credentialState CredentialState
	)
	err = tx.QueryRow(ctx,
		`SELECT password_hash, state::text
		   FROM password_credentials WHERE account_id = $1::uuid FOR UPDATE`,
		req.AccountID,
	).Scan(&storedHash, &credentialState)
	if errors.Is(err, pgx.ErrNoRows) {
		return PreparedPasswordChange{}, fmt.Errorf("%w: Account has no password credential", ErrPrincipalNotFound)
	}
	if err != nil {
		return PreparedPasswordChange{}, fmt.Errorf("locking credential: %w", err)
	}

	// The kind must match the credential's actual state rather than being
	// whatever the caller asserted. Otherwise a voluntary change could claim to
	// be the bootstrap mandatory one and skip proving the current password.
	switch req.Kind {
	case BootstrapMandatoryChange:
		if credentialState != CredentialChangeRequired {
			return PreparedPasswordChange{}, fmt.Errorf(
				"%w: a mandatory change requires CHANGE_REQUIRED, credential is %s",
				ErrCurrentPasswordRequired, credentialState)
		}
	case VoluntaryChange:
		// A voluntary change on a CHANGE_REQUIRED credential is allowed — it is
		// simply a caller who supplied the current password as well — so there
		// is nothing to refuse here.
	default:
		return PreparedPasswordChange{}, fmt.Errorf("unknown password change kind %d", req.Kind)
	}

	session, err := lockSession(ctx, tx, req.SessionID)
	if err != nil {
		return PreparedPasswordChange{}, err
	}
	if session.AccountID != req.AccountID {
		// Not reported as a distinct condition to any caller: a session that
		// belongs to another Account is simply not usable here.
		return PreparedPasswordChange{}, fmt.Errorf("%w: session belongs to another Account", ErrSessionNotUsable)
	}
	if err := session.Usable(accountEpoch, now); err != nil {
		return PreparedPasswordChange{}, err
	}

	// The presented generation must still be the current one. If the session
	// rotated underneath this request, committing against the old generation
	// would let a superseded credential authorize a password change.
	if req.PresentedGeneration != session.CurrentGeneration {
		return PreparedPasswordChange{}, fmt.Errorf("%w: presented %d, current is %d",
			ErrStaleGeneration, req.PresentedGeneration, session.CurrentGeneration)
	}
	if err := assertGenerationIsCurrent(ctx, tx, session.ID, session.CurrentGeneration); err != nil {
		return PreparedPasswordChange{}, err
	}

	if err := CheckRecentAuthentication(req.Kind, session, recentAuthWindow, now); err != nil {
		return PreparedPasswordChange{}, err
	}

	newHash, err := prepareCredential(
		req.NewPassword,
		req.CurrentPassword,
		storedHash,
		req.Kind.RequiresCurrentPassword(),
		req.Compromised,
	)
	if err != nil {
		return PreparedPasswordChange{}, err
	}

	return PreparedPasswordChange{
		AccountID:         req.AccountID,
		SessionID:         session.ID,
		NewPasswordHash:   newHash,
		CurrentGeneration: session.CurrentGeneration,
		Kind:              req.Kind,
		// A voluntary change revokes every other family (§4.3). The bootstrap
		// mandatory change is the Account's first session, so there is nothing
		// else to revoke and saying otherwise would imply work that never
		// happens.
		RevokeOtherSessions: req.Kind == VoluntaryChange,
	}, nil
}

// lockSession reads and locks one session family.
func lockSession(ctx context.Context, tx pgx.Tx, sessionID string) (Session, error) {
	var s Session
	err := tx.QueryRow(ctx,
		`SELECT id::text, account_id::text, admitted_epoch, state::text, current_generation,
		        authenticated_at, reauthenticated_at, last_activity_at,
		        idle_expires_at, absolute_expires_at
		   FROM sessions WHERE id = $1::uuid FOR UPDATE`,
		sessionID,
	).Scan(&s.ID, &s.AccountID, &s.AdmittedEpoch, &s.State, &s.CurrentGeneration,
		&s.AuthenticatedAt, &s.ReauthenticatedAt, &s.LastActivityAt,
		&s.IdleExpiresAt, &s.AbsoluteExpiresAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, fmt.Errorf("%w: no such session", ErrSessionNotUsable)
	}
	if err != nil {
		if isInvalidUUID(err) {
			return Session{}, fmt.Errorf("%w: malformed session identifier", ErrSessionNotUsable)
		}
		return Session{}, fmt.Errorf("locking session: %w", err)
	}
	return s, nil
}

// assertGenerationIsCurrent confirms the family's current generation row is
// actually in the CURRENT state.
//
// The sessions row and the session_credentials row could in principle disagree;
// the partial unique index makes two CURRENT rows impossible, but not a
// current_generation pointing at a superseded one. Checking both is what makes
// "only the current unsuperseded generation authenticates" true rather than
// assumed.
func assertGenerationIsCurrent(ctx context.Context, tx pgx.Tx, sessionID string, generation int) error {
	var state string
	err := tx.QueryRow(ctx,
		`SELECT state::text FROM session_credentials
		  WHERE session_id = $1::uuid AND generation = $2 FOR UPDATE`,
		sessionID, generation,
	).Scan(&state)

	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: generation %d has no credential row", ErrStaleGeneration, generation)
	}
	if err != nil {
		return fmt.Errorf("locking session credential: %w", err)
	}
	if state != "CURRENT" {
		return fmt.Errorf("%w: generation %d is %s", ErrStaleGeneration, generation, state)
	}
	return nil
}
