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
	Role      Role

	// NewPasswordHash is the encoded Argon2id hash. The plaintext it came from
	// no longer exists by the time this is returned.
	NewPasswordHash config.Secret

	// Generation the session is currently on, proven under lock.
	CurrentGeneration int

	// Kind records which password-change flow was completed for Audit evidence.
	Kind PasswordChangeKind
}

// PasswordChangePolicy contains the security windows established when the
// successfully changed credential re-establishes the current session family.
//
// The caller selects the role-specific values from typed configuration. Keeping
// them explicit here prevents Identity from silently applying one universal
// lifetime to Student, Instructor, and Admin sessions.
type PasswordChangePolicy struct {
	RecentAuthWindow time.Duration
	IdleExpiry       time.Duration
	AbsoluteExpiry   time.Duration
}

// PasswordChangeResult contains the only plaintext values produced by a
// successful change. They are returned only after Commit, so the transport can
// replace the host-only session cookie and in-memory CSRF token without ever
// issuing credentials for a rolled-back mutation.
type PasswordChangeResult struct {
	SessionID         string
	Generation        int
	Credential        config.Secret
	CSRFToken         config.Secret
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
}

type passwordChangeCommit struct {
	prepared          PreparedPasswordChange
	replacement       IssuedCredential
	nextGeneration    int
	now               time.Time
	idleExpiresAt     time.Time
	absoluteExpiresAt time.Time
	targetRevision    int
}

// CompletePasswordChange is the atomic link between password replacement,
// restriction removal, and session rotation.
//
// The transaction either commits all of these effects or none:
//   - replace the Argon2id password hash and clear CHANGE_REQUIRED;
//   - supersede the admitted session credential and create its replacement;
//   - re-establish the current family under the role-specific expiry windows;
//   - revoke every other family;
//   - append privileged Identity Audit evidence.
//
// No HTTP response or cookie may be written until this function returns
// successfully. A returned credential therefore always names committed state.
func CompletePasswordChange(
	ctx context.Context,
	conn *pgx.Conn,
	req PasswordChangeRequest,
	policy PasswordChangePolicy,
	now time.Time,
) (PasswordChangeResult, error) {
	if err := validatePasswordChangePolicy(policy); err != nil {
		return PasswordChangeResult{}, err
	}
	now = now.UTC()

	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return PasswordChangeResult{}, fmt.Errorf("beginning password-change transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	prepared, err := PreparePasswordChange(ctx, tx, req, policy.RecentAuthWindow, now)
	if err != nil {
		return PasswordChangeResult{}, err
	}

	change, err := newPasswordChangeCommit(prepared, policy, now)
	if err != nil {
		return PasswordChangeResult{}, err
	}
	if err := applyPasswordChange(ctx, tx, &change); err != nil {
		return PasswordChangeResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PasswordChangeResult{}, fmt.Errorf("committing password change: %w", err)
	}
	return change.result(), nil
}

func (c passwordChangeCommit) result() PasswordChangeResult {
	return PasswordChangeResult{
		SessionID:         c.prepared.SessionID,
		Generation:        c.nextGeneration,
		Credential:        c.replacement.Credential,
		CSRFToken:         c.replacement.CSRFToken,
		IdleExpiresAt:     c.idleExpiresAt,
		AbsoluteExpiresAt: c.absoluteExpiresAt,
	}
}

func newPasswordChangeCommit(
	prepared PreparedPasswordChange,
	policy PasswordChangePolicy,
	now time.Time,
) (passwordChangeCommit, error) {
	replacement, err := NewSessionCredential()
	if err != nil {
		return passwordChangeCommit{}, fmt.Errorf("minting replacement session credential: %w", err)
	}
	return passwordChangeCommit{
		prepared:          prepared,
		replacement:       replacement,
		nextGeneration:    prepared.CurrentGeneration + 1,
		now:               now,
		idleExpiresAt:     now.Add(policy.IdleExpiry),
		absoluteExpiresAt: now.Add(policy.AbsoluteExpiry),
	}, nil
}

func validatePasswordChangePolicy(policy PasswordChangePolicy) error {
	if policy.IdleExpiry <= 0 {
		return errors.New("password change requires a positive session idle expiry")
	}
	if policy.AbsoluteExpiry <= 0 {
		return errors.New("password change requires a positive session absolute expiry")
	}
	if policy.IdleExpiry >= policy.AbsoluteExpiry {
		return errors.New("session idle expiry must be shorter than absolute expiry")
	}
	if policy.RecentAuthWindow <= 0 || policy.RecentAuthWindow >= policy.IdleExpiry {
		return errors.New("recent-authentication window must be positive and shorter than idle expiry")
	}
	return nil
}

func applyPasswordChange(ctx context.Context, tx pgx.Tx, change *passwordChangeCommit) error {
	if err := replacePasswordCredential(ctx, tx, change); err != nil {
		return err
	}
	if err := rotateSessionCredential(ctx, tx, change); err != nil {
		return err
	}
	if err := reestablishSession(ctx, tx, change); err != nil {
		return err
	}
	if err := revokeOtherSessions(ctx, tx, change); err != nil {
		return err
	}
	if err := advanceAccountRevision(ctx, tx, change); err != nil {
		return err
	}
	return appendPasswordChangeAudit(ctx, tx, change)
}

func replacePasswordCredential(ctx context.Context, tx pgx.Tx, change *passwordChangeCommit) error {
	// This is an encoded Argon2id hash, not password plaintext. Exposing the
	// wrapper here hands it directly to PostgreSQL and nowhere else.
	hash := change.prepared.NewPasswordHash
	tag, err := tx.Exec(ctx,
		`UPDATE password_credentials
		    SET password_hash = $2, state = 'ACTIVE', password_changed_at = $3
		  WHERE account_id = $1::uuid`,
		change.prepared.AccountID, hash.Expose(), change.now,
	)
	if err != nil {
		return fmt.Errorf("replacing password credential: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("replacing password credential: expected one row, changed %d", tag.RowsAffected())
	}
	return nil
}

func rotateSessionCredential(ctx context.Context, tx pgx.Tx, change *passwordChangeCommit) error {
	tag, err := tx.Exec(ctx,
		`UPDATE session_credentials
		    SET state = 'SUPERSEDED', superseded_at = $4, replaced_by_generation = $3
		  WHERE session_id = $1::uuid AND generation = $2 AND state = 'CURRENT'`,
		change.prepared.SessionID,
		change.prepared.CurrentGeneration,
		change.nextGeneration,
		change.now,
	)
	if err != nil {
		return fmt.Errorf("superseding admitted session credential: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("superseding admitted session credential: expected one row, changed %d", tag.RowsAffected())
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO session_credentials
		   (session_id, generation, credential_digest, csrf_digest, issued_at)
		 VALUES ($1::uuid, $2, $3, $4, $5)`,
		change.prepared.SessionID,
		change.nextGeneration,
		change.replacement.CredentialDigest,
		change.replacement.CSRFDigest,
		change.now,
	); err != nil {
		return fmt.Errorf("creating replacement session credential: %w", err)
	}
	return nil
}

func reestablishSession(ctx context.Context, tx pgx.Tx, change *passwordChangeCommit) error {
	tag, err := tx.Exec(ctx,
		`UPDATE sessions
		    SET current_generation = $2,
		        authenticated_at = $3,
		        reauthenticated_at = $3,
		        last_activity_at = $3,
		        idle_expires_at = $4,
		        absolute_expires_at = $5,
		        updated_at = $3
		  WHERE id = $1::uuid`,
		change.prepared.SessionID,
		change.nextGeneration,
		change.now,
		change.idleExpiresAt,
		change.absoluteExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("re-establishing current session: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("re-establishing current session: expected one row, changed %d", tag.RowsAffected())
	}
	return nil
}

func revokeOtherSessions(ctx context.Context, tx pgx.Tx, change *passwordChangeCommit) error {
	_, err := tx.Exec(ctx,
		`UPDATE sessions
		    SET state = 'REVOKED',
		        revoked_at = $3,
		        revocation_reason = 'PASSWORD_CHANGE',
		        updated_at = $3
		  WHERE account_id = $1::uuid
		    AND id <> $2::uuid
		    AND state = 'ACTIVE'`,
		change.prepared.AccountID, change.prepared.SessionID, change.now,
	)
	if err != nil {
		return fmt.Errorf("revoking other session families: %w", err)
	}
	return nil
}

func advanceAccountRevision(ctx context.Context, tx pgx.Tx, change *passwordChangeCommit) error {
	err := tx.QueryRow(ctx,
		`UPDATE accounts
		    SET revision = revision + 1, updated_at = $2
		  WHERE id = $1::uuid
		 RETURNING revision`,
		change.prepared.AccountID, change.now,
	).Scan(&change.targetRevision)
	if err != nil {
		return fmt.Errorf("advancing Account revision: %w", err)
	}
	return nil
}

func appendPasswordChangeAudit(ctx context.Context, tx pgx.Tx, change *passwordChangeCommit) error {
	if _, err := tx.Exec(ctx,
		`INSERT INTO audit_events
		   (actor_account_id, actor_role, actor_descriptor, action, module,
		    target_type, target_id, target_revision, reason, metadata)
		 VALUES ($1::uuid, $2, $1::text, 'PASSWORD_CHANGED', 'IDENTITY_AND_ACCESS',
		         'ACCOUNT', $1::text, $3, $4, jsonb_build_object(
		             'change_kind', $5::text,
		             'session_id', $6::text,
		             'previous_generation', $7::integer,
		             'replacement_generation', $8::integer,
		             'other_sessions_revoked', $9::boolean
		         ))`,
		change.prepared.AccountID,
		string(change.prepared.Role),
		change.targetRevision,
		passwordChangeAuditReason(change.prepared.Kind),
		change.prepared.Kind.String(),
		change.prepared.SessionID,
		change.prepared.CurrentGeneration,
		change.nextGeneration,
		true,
	); err != nil {
		return fmt.Errorf("recording password-change audit event: %w", err)
	}
	return nil
}

func passwordChangeAuditReason(kind PasswordChangeKind) string {
	if kind == BootstrapMandatoryChange {
		return "Completed the mandatory bootstrap Administrator password change"
	}
	return "Account changed its password after recent authentication"
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
	if status != StatusActive {
		return PreparedPasswordChange{}, fmt.Errorf("%w: Account is not active", ErrSessionNotUsable)
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

	if err := CheckRecentAuthentication(session, recentAuthWindow, now); err != nil {
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
		Role:              role,
		NewPasswordHash:   newHash,
		CurrentGeneration: session.CurrentGeneration,
		Kind:              req.Kind,
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
