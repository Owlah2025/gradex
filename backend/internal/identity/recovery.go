package identity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

const (
	passwordResetTemplateContract          = "account-password-reset-v1"
	passwordResetCompletedTemplateContract = "account-password-reset-completed-v1"

	// minimumPasswordResetRequestDuration floors how long a reset request takes
	// regardless of outcome. Non-enumeration is not achieved by returning the
	// same body: an unknown address skips secret supersession, an insert, an
	// event append, and an outbox write, so without a floor the response time
	// alone separates real accounts from absent ones.
	minimumPasswordResetRequestDuration = 75 * time.Millisecond
)

// RecoveryService owns password reset request and reset-secret consumption.
//
// It is separate from AdmissionService because the two answer different
// questions about an Account — admission decides whether one may exist, and
// recovery acts on one that already does — but it deliberately reuses the same
// identity_action_secrets machinery rather than introducing a parallel secret
// store with its own expiry and single-use rules to get wrong.
type RecoveryService struct {
	pool        *pgxpool.Pool
	outbox      *outbox.Writer
	compromised CompromisedRangeSource
	resetTTL    time.Duration
	now         func() time.Time
	random      io.Reader
	randomMu    sync.Mutex
}

type RecoveryServiceOptions struct {
	Pool        *pgxpool.Pool
	Outbox      *outbox.Writer
	Compromised CompromisedRangeSource
	ResetTTL    time.Duration
	Now         func() time.Time
	Random      io.Reader
}

func NewRecoveryService(options RecoveryServiceOptions) (*RecoveryService, error) {
	if options.Pool == nil || options.Outbox == nil || options.Compromised == nil {
		return nil, errors.New("recovery service dependencies are required")
	}
	if options.ResetTTL <= 0 || options.Now == nil || options.Random == nil {
		return nil, errors.New("recovery clock, randomness, and reset TTL are required")
	}
	return &RecoveryService{
		pool:        options.Pool,
		outbox:      options.Outbox,
		compromised: options.Compromised,
		resetTTL:    options.ResetTTL,
		now:         options.Now,
		random:      options.Random,
	}, nil
}

// PasswordResetRequest is the anonymous request to begin recovery.
type PasswordResetRequest struct {
	Email     string
	RequestID string
}

// RequestPasswordReset issues a single-use reset secret for an eligible
// Account and reports nothing about whether one existed.
//
// Every outcome — unknown address, unverified Account, suspended Account, or
// success — returns a nil error after the same minimum duration. The caller
// therefore has exactly one response to render and cannot leak Account
// existence by branching on this result.
func (s *RecoveryService) RequestPasswordReset(
	ctx context.Context,
	request PasswordResetRequest,
) error {
	normalizedEmail, err := NormalizeEmail(request.Email)
	if err != nil {
		// A malformed address cannot identify an Account, but reporting that
		// distinctly would still separate "no such address" from "not a valid
		// address" for an enumerating caller. Absorb it on the same path.
		return s.floorRequest(time.Now())
	}
	requestID, err := validateRequestID(request.RequestID)
	if err != nil {
		return err
	}
	replacement, err := s.issueResetSecret()
	if err != nil {
		return fmt.Errorf("%w: reset-secret generation", ErrAdmissionUnavailable)
	}
	reservation, err := s.outbox.ReserveProtectedPayload(ctx)
	if err != nil {
		return fmt.Errorf("%w: protected payload reservation", ErrDeliveryUnavailable)
	}

	started := time.Now()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("beginning password reset request: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	account, eligible, err := lockRecoverableAccountByEmail(ctx, tx, normalizedEmail)
	if err != nil {
		return err
	}
	if !eligible {
		return commitPasswordResetRequest(ctx, tx, started)
	}
	if err := supersedeLiveResetSecret(ctx, tx, account.id, replacement); err != nil {
		return err
	}
	if err := insertActionSecret(ctx, tx, account.id, replacement); err != nil {
		return err
	}
	if err := appendIdentitySecurityEvent(
		ctx, tx, securityEventAppend{
			eventType:      "PASSWORD_RESET_REQUESTED",
			accountID:      account.id,
			actionSecretID: replacement.ID,
			revision:       account.revision,
			requestID:      requestID,
			evidence: map[string]any{
				"schema_version": 1,
				"outcome_class":  "ELIGIBLE",
			},
		},
	); err != nil {
		return err
	}
	if err := s.appendResetOutbox(ctx, tx, resetOutboxRequest{
		accountID: account.id, revision: account.revision, email: account.email,
		locale: account.locale, secret: replacement, requestID: requestID,
		reservation: reservation,
	}); err != nil {
		return err
	}
	return commitPasswordResetRequest(ctx, tx, started)
}

func (s *RecoveryService) issueResetSecret() (IssuedActionSecret, error) {
	s.randomMu.Lock()
	defer s.randomMu.Unlock()
	return newActionSecret(actionSecretOptions{
		Purpose: ActionPasswordReset,
		Now:     s.now().UTC(), TTL: s.resetTTL, Random: s.random,
	})
}

// floorRequest spends the same minimum duration as a real request without
// touching the database, for inputs rejected before any lookup happens.
func (s *RecoveryService) floorRequest(started time.Time) error {
	if remaining := time.Until(started.Add(minimumPasswordResetRequestDuration)); remaining > 0 {
		time.Sleep(remaining)
	}
	return nil
}

func commitPasswordResetRequest(ctx context.Context, tx pgx.Tx, started time.Time) error {
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing password reset request: %w", err)
	}
	if remaining := time.Until(started.Add(minimumPasswordResetRequestDuration)); remaining > 0 {
		time.Sleep(remaining)
	}
	return nil
}

type recoverableAccount struct {
	id       string
	email    string
	locale   Locale
	revision int
}

// lockRecoverableAccountByEmail locks the Account row and reports eligibility.
//
// Eligibility follows the authoritative Account and password-credential
// lifecycle, not today's journey. Role is deliberately not part of the
// predicate: restricting recovery to Students because the current slice is
// Student-focused would leave an Instructor or Admin with no self-service path,
// and role is not a property recovery needs to know.
//
// A credential in CHANGE_REQUIRED is still recoverable. That state means the
// holder must set a new password, which is exactly what recovery does, so
// excluding it would strip the recovery path from the accounts most likely to
// need one.
//
// An Account with no password_credentials row has nothing to reset, so it is
// ineligible however active it looks.
func lockRecoverableAccountByEmail(
	ctx context.Context,
	tx pgx.Tx,
	normalizedEmail string,
) (recoverableAccount, bool, error) {
	var account recoverableAccount
	var status string
	var emailVerifiedAt *time.Time
	var credentialState *string
	err := tx.QueryRow(ctx,
		`SELECT a.id::text, a.email, a.locale, a.revision, a.status,
		        a.email_verified_at, c.state::text
		   FROM accounts a
		   LEFT JOIN password_credentials c ON c.account_id = a.id
		  WHERE a.normalized_email = $1
		  FOR UPDATE OF a`,
		normalizedEmail,
	).Scan(
		&account.id, &account.email, &account.locale,
		&account.revision, &status, &emailVerifiedAt, &credentialState,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return recoverableAccount{}, false, nil
	}
	if err != nil {
		return recoverableAccount{}, false, fmt.Errorf("locking recoverable Account: %w", err)
	}
	eligible := status == "ACTIVE" && emailVerifiedAt != nil && credentialState != nil
	return account, eligible, nil
}

func supersedeLiveResetSecret(
	ctx context.Context,
	tx pgx.Tx,
	accountID string,
	replacement IssuedActionSecret,
) error {
	var currentID string
	err := tx.QueryRow(ctx,
		`SELECT id::text
		   FROM identity_action_secrets
		  WHERE account_id = $1::uuid
		    AND purpose = 'PASSWORD_RESET'
		    AND consumed_at IS NULL
		    AND superseded_at IS NULL
		  FOR UPDATE`,
		accountID,
	).Scan(&currentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("locking live reset secret: %w", err)
	}
	// superseded_at is clamped to at least the superseded row's own issued_at.
	//
	// The replacement's timestamp is taken before this transaction begins and
	// before it waits on the Account row lock, so lock order and generation
	// order can invert under concurrency: a request that generated its secret
	// earlier may acquire the lock later and try to stamp an older
	// superseded_at onto a newer row. That violates the
	// identity_action_secrets_superseded_after_issue constraint and surfaces as
	// a 500 on an otherwise ordinary second reset request.
	if _, err := tx.Exec(ctx,
		`UPDATE identity_action_secrets
		    SET superseded_at = GREATEST(issued_at, $1), superseded_by_id = $2::uuid
		  WHERE id = $3::uuid`,
		replacement.IssuedAt, replacement.ID, currentID,
	); err != nil {
		return fmt.Errorf("superseding reset secret: %w", err)
	}
	return nil
}

type resetOutboxRequest struct {
	accountID   string
	revision    int
	email       string
	locale      Locale
	secret      IssuedActionSecret
	requestID   string
	reservation outbox.ProtectedPayloadReservation
}

// appendResetOutbox reuses outbox.VerificationDelivery because the protected
// shape recovery needs is identical — destination, locale, template contract,
// one-time token, expiry. The template contract, not the Go type, is what
// distinguishes a reset message from a verification message downstream.
func (s *RecoveryService) appendResetOutbox(
	ctx context.Context,
	tx pgx.Tx,
	request resetOutboxRequest,
) error {
	_, err := s.outbox.AppendReserved(ctx, tx, outbox.ReservedAppend{
		Event: outbox.Event{
			Type:              "identity.password_reset_requested",
			SchemaVersion:     1,
			SourceModule:      "IDENTITY_AND_ACCESS",
			AggregateType:     "ACCOUNT",
			AggregateID:       request.accountID,
			AggregateRevision: request.revision,
			CorrelationID:     request.requestID,
			SafePayload: map[string]any{
				"purpose":           request.secret.Purpose,
				"action_secret_id":  request.secret.ID,
				"locale":            request.locale,
				"template_contract": passwordResetTemplateContract,
				"secret_expires_at": request.secret.ExpiresAt,
			},
		},
		Protected: outbox.VerificationDelivery{
			Destination:       request.email,
			Locale:            string(request.locale),
			TemplateContract:  passwordResetTemplateContract,
			VerificationToken: request.secret.Bearer.Expose(),
			ExpiresAt:         request.secret.ExpiresAt,
		},
		Reservation: request.reservation,
	})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDeliveryUnavailable, err)
	}
	return nil
}

// lockResetSecret locks a reset secret by digest and reports whether it is
// still usable. Purpose is part of the predicate, so a verification secret
// presented to a recovery endpoint does not resolve here.
//
// It returns the owning Account so the caller never has to trust an Account
// identifier supplied alongside the bearer.
func lockResetSecret(
	ctx context.Context,
	tx pgx.Tx,
	digest []byte,
	now time.Time,
) (secretID string, accountID string, valid bool, err error) {
	var expiresAt time.Time
	var consumedAt, supersededAt *time.Time
	err = tx.QueryRow(ctx,
		`SELECT id::text, account_id::text, expires_at, consumed_at, superseded_at
		   FROM identity_action_secrets
		  WHERE secret_digest = $1 AND purpose = 'PASSWORD_RESET'
		  FOR UPDATE`,
		digest,
	).Scan(&secretID, &accountID, &expiresAt, &consumedAt, &supersededAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("locking reset secret: %w", err)
	}
	valid = consumedAt == nil && supersededAt == nil && now.Before(expiresAt)
	return secretID, accountID, valid, nil
}

// consumeResetSecret marks the secret spent and records the attempt.
//
// The caller must already hold the row lock from lockResetSecret. Consumption
// and whatever the secret authorises must commit in one transaction: a secret
// marked spent without its effect applied strands the Account, and an effect
// applied without consumption leaves a replayable secret.
func consumeResetSecret(ctx context.Context, tx pgx.Tx, secretID string, now time.Time) error {
	if _, err := tx.Exec(ctx,
		`UPDATE identity_action_secrets
		    SET consumed_at = $1,
		        attempt_count = attempt_count + 1,
		        first_attempt_at = COALESCE(first_attempt_at, $1),
		        last_attempt_at = $1
		  WHERE id = $2::uuid`,
		now, secretID,
	); err != nil {
		return fmt.Errorf("consuming reset secret: %w", err)
	}
	return nil
}

// PasswordResetCompletion presents a reset secret together with the new
// password. There is no Account identifier: the secret is the only thing that
// names an Account here, so a caller cannot aim a valid secret at a different
// one.
type PasswordResetCompletion struct {
	Token     string
	Password  config.Secret
	RequestID string
}

// CompletePasswordReset replaces the password and invalidates every session.
//
// Everything commits in one transaction: secret consumption, credential
// replacement, revocation of every family, session-epoch advancement, Account
// revision advancement, security evidence, and notification intent. A partial
// application is the failure this ordering exists to prevent — a consumed
// secret without a new password strands the Account, and a new password
// without consumption leaves the secret replayable.
//
// Recovery issues no session. A recovered Account must log in normally, so a
// mailbox compromise cannot be converted straight into an authenticated
// browser session.
func (s *RecoveryService) CompletePasswordReset(
	ctx context.Context,
	completion PasswordResetCompletion,
) error {
	requestID, err := validateRequestID(completion.RequestID)
	if err != nil {
		return err
	}
	digest, err := DigestActionSecret(completion.Token)
	if err != nil {
		return ErrTokenInvalid
	}
	// Hashing happens before the transaction opens. Argon2id is deliberately
	// expensive, and holding the Account row lock across it would let a burst
	// of resets serialise behind each other. It also means an invalid secret
	// costs the same work as a valid one.
	credentialHash, err := hashNewPassword(ctx, completion.Password, s.compromised)
	if err != nil {
		if errors.Is(err, ErrPasswordPolicy) {
			return err
		}
		return fmt.Errorf("%w: credential screening", ErrAdmissionUnavailable)
	}
	// Reserved before the transaction opens, matching the request path: the
	// only fallible entropy read must not happen while Account rows are locked.
	reservation, err := s.outbox.ReserveProtectedPayload(ctx)
	if err != nil {
		return fmt.Errorf("%w: protected payload reservation", ErrDeliveryUnavailable)
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("beginning password reset: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	now := s.now().UTC()
	secretID, accountID, secretValid, err := lockResetSecret(ctx, tx, digest, now)
	if err != nil {
		return err
	}
	if !secretValid {
		return ErrTokenInvalid
	}
	account, eligible, err := lockRecoverableAccountByID(ctx, tx, accountID)
	if err != nil {
		return err
	}
	// An Account suspended between request and completion must not be revived
	// by a secret issued while it was still active.
	if !eligible {
		return ErrTokenInvalid
	}

	if err := replaceRecoveredCredential(ctx, tx, accountID, credentialHash, now); err != nil {
		return err
	}
	if err := revokeAllSessionFamilies(ctx, tx, accountID, now); err != nil {
		return err
	}
	revision, err := advanceRecoveredAccount(ctx, tx, accountID, now)
	if err != nil {
		return err
	}
	if err := consumeResetSecret(ctx, tx, secretID, now); err != nil {
		return err
	}
	if err := appendIdentitySecurityEvent(
		ctx, tx, securityEventAppend{
			eventType:      "PASSWORD_RESET_COMPLETED",
			accountID:      accountID,
			actionSecretID: secretID,
			revision:       revision,
			requestID:      requestID,
			evidence: map[string]any{
				"schema_version": 1,
				"outcome_class":  "COMPLETED",
			},
		},
	); err != nil {
		return err
	}
	if _, err := s.outbox.AppendReserved(ctx, tx, outbox.ReservedAppend{
		Event: outbox.Event{
			Type:              "identity.password_reset_completed",
			SchemaVersion:     1,
			SourceModule:      "IDENTITY_AND_ACCESS",
			AggregateType:     "ACCOUNT",
			AggregateID:       accountID,
			AggregateRevision: revision,
			CorrelationID:     requestID,
			SafePayload: map[string]any{
				"locale":            account.locale,
				"template_contract": passwordResetCompletedTemplateContract,
				"reset_at":          now,
			},
		},
		// A completion notice carries no actionable secret, but the destination
		// is PII, so it stays inside authenticated ciphertext.
		Protected: outbox.NoticeDelivery{
			Destination:      account.email,
			Locale:           string(account.locale),
			TemplateContract: passwordResetCompletedTemplateContract,
		},
		Reservation: reservation,
	}); err != nil {
		return fmt.Errorf("%w: %v", ErrDeliveryUnavailable, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing password reset: %w", err)
	}
	return nil
}

// lockRecoverableAccountByID re-checks eligibility against the locked row at
// completion time, using the Account the secret itself names.
func lockRecoverableAccountByID(
	ctx context.Context,
	tx pgx.Tx,
	accountID string,
) (recoverableAccount, bool, error) {
	var account recoverableAccount
	var status string
	var emailVerifiedAt *time.Time
	var credentialState *string
	err := tx.QueryRow(ctx,
		`SELECT a.id::text, a.email, a.locale, a.revision, a.status,
		        a.email_verified_at, c.state::text
		   FROM accounts a
		   LEFT JOIN password_credentials c ON c.account_id = a.id
		  WHERE a.id = $1::uuid
		  FOR UPDATE OF a`,
		accountID,
	).Scan(
		&account.id, &account.email, &account.locale,
		&account.revision, &status, &emailVerifiedAt, &credentialState,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return recoverableAccount{}, false, nil
	}
	if err != nil {
		return recoverableAccount{}, false, fmt.Errorf("locking recovered Account: %w", err)
	}
	eligible := status == "ACTIVE" && emailVerifiedAt != nil && credentialState != nil
	return account, eligible, nil
}

func replaceRecoveredCredential(
	ctx context.Context,
	tx pgx.Tx,
	accountID string,
	credentialHash config.Secret,
	now time.Time,
) error {
	// An encoded Argon2id hash, not password plaintext. Exposing the wrapper
	// here hands it to PostgreSQL and nowhere else.
	tag, err := tx.Exec(ctx,
		`UPDATE password_credentials
		    SET password_hash = $2, state = 'ACTIVE', password_changed_at = $3
		  WHERE account_id = $1::uuid`,
		accountID, credentialHash.Expose(), now,
	)
	if err != nil {
		return fmt.Errorf("replacing recovered credential: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf(
			"replacing recovered credential: expected one row, changed %d", tag.RowsAffected(),
		)
	}
	return nil
}

// revokeAllSessionFamilies revokes every family, with no surviving session.
//
// This differs from a voluntary password change, which deliberately preserves
// the caller's own family. Recovery has no authenticated caller to preserve,
// and the reason it is invoked is that control of the credential is in doubt.
func revokeAllSessionFamilies(
	ctx context.Context,
	tx pgx.Tx,
	accountID string,
	now time.Time,
) error {
	if _, err := tx.Exec(ctx,
		`UPDATE sessions
		    SET state = 'REVOKED',
		        revoked_at = $2,
		        revocation_reason = 'PASSWORD_RESET',
		        updated_at = $2
		  WHERE account_id = $1::uuid
		    AND state = 'ACTIVE'`,
		accountID, now,
	); err != nil {
		return fmt.Errorf("revoking session families on recovery: %w", err)
	}
	return nil
}

// advanceRecoveredAccount bumps both the revision and the session epoch.
//
// Revoking the rows above only invalidates families that exist now. Advancing
// the epoch invalidates every family admitted under the old one, which closes
// the window where a family created concurrently with this transaction would
// otherwise survive a reset.
func advanceRecoveredAccount(
	ctx context.Context,
	tx pgx.Tx,
	accountID string,
	now time.Time,
) (int, error) {
	var revision int
	if err := tx.QueryRow(ctx,
		`UPDATE accounts
		    SET revision = revision + 1,
		        session_epoch = session_epoch + 1,
		        updated_at = $2
		  WHERE id = $1::uuid
		 RETURNING revision`,
		accountID, now,
	).Scan(&revision); err != nil {
		return 0, fmt.Errorf("advancing recovered Account: %w", err)
	}
	return revision, nil
}
