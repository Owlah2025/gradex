package identity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

const (
	verificationTemplateContract       = "student-email-verification-v1"
	minimumVerificationRequestDuration = 75 * time.Millisecond
)

type AdmissionService struct {
	pool        *pgxpool.Pool
	policies    PolicySetResolver
	compromised CompromisedRangeSource
	outbox      *outbox.Writer
	tokenTTL    time.Duration
	now         func() time.Time
	random      io.Reader
	randomMu    sync.Mutex
}

func NewAdmissionService(options AdmissionServiceOptions) (*AdmissionService, error) {
	if options.Pool == nil || options.Policies == nil ||
		options.Compromised == nil || options.Outbox == nil {
		return nil, errors.New("admission service dependencies are required")
	}
	if options.VerificationTTL <= 0 || options.Now == nil || options.Random == nil {
		return nil, errors.New("admission clock, randomness, and verification TTL are required")
	}
	return &AdmissionService{
		pool:        options.Pool,
		policies:    options.Policies,
		compromised: options.Compromised,
		outbox:      options.Outbox,
		tokenTTL:    options.VerificationTTL,
		now:         options.Now,
		random:      options.Random,
	}, nil
}

type preparedRegistration struct {
	displayName         string
	correspondenceEmail string
	normalizedEmail     string
	locale              Locale
	policySet           RegistrationPolicySet
	credentialHash      config.Secret
	actionSecret        IssuedActionSecret
	outboxReservation   outbox.ProtectedPayloadReservation
	requestID           string
}

func (s *AdmissionService) RegisterStudent(
	ctx context.Context,
	request StudentRegistration,
) error {
	prepared, err := s.prepareRegistration(ctx, request)
	if err != nil {
		return err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("beginning Student registration: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	accountID, created, err := insertPendingStudent(ctx, tx, prepared)
	if err != nil {
		return err
	}
	if !created {
		return tx.Commit(ctx)
	}
	if err := s.insertRegistrationFacts(ctx, tx, accountID, prepared); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing Student registration: %w", err)
	}
	return nil
}

func (s *AdmissionService) prepareRegistration(
	ctx context.Context,
	request StudentRegistration,
) (preparedRegistration, error) {
	displayName, err := ValidateDisplayName(request.DisplayName)
	if err != nil {
		return preparedRegistration{}, err
	}
	correspondenceEmail := strings.TrimSpace(request.Email)
	normalizedEmail, err := NormalizeEmail(correspondenceEmail)
	if err != nil {
		return preparedRegistration{}, err
	}
	if !request.Locale.Valid() {
		return preparedRegistration{}, ErrInvalidLocale
	}
	requestID, err := validateRequestID(request.RequestID)
	if err != nil {
		return preparedRegistration{}, err
	}
	policySet, err := s.policies.Resolve(ctx, request.PolicySetID, request.Locale)
	if err != nil {
		if errors.Is(err, ErrPolicySetStale) {
			return preparedRegistration{}, err
		}
		return preparedRegistration{}, fmt.Errorf("%w: current policy set", ErrAdmissionUnavailable)
	}
	credentialHash, err := hashNewPassword(ctx, request.Password, s.compromised)
	if err != nil {
		if errors.Is(err, ErrPasswordPolicy) {
			return preparedRegistration{}, err
		}
		return preparedRegistration{}, fmt.Errorf("%w: credential screening", ErrAdmissionUnavailable)
	}
	actionSecret, err := s.issueActionSecret()
	if err != nil {
		return preparedRegistration{}, fmt.Errorf("%w: action-secret generation", ErrAdmissionUnavailable)
	}
	reservation, err := s.outbox.ReserveProtectedPayload(ctx)
	if err != nil {
		return preparedRegistration{}, fmt.Errorf("%w: protected payload reservation", ErrDeliveryUnavailable)
	}
	return preparedRegistration{
		displayName:         displayName,
		correspondenceEmail: correspondenceEmail,
		normalizedEmail:     normalizedEmail,
		locale:              request.Locale,
		policySet:           policySet,
		credentialHash:      credentialHash,
		actionSecret:        actionSecret,
		outboxReservation:   reservation,
		requestID:           requestID,
	}, nil
}

func (s *AdmissionService) issueActionSecret() (IssuedActionSecret, error) {
	s.randomMu.Lock()
	defer s.randomMu.Unlock()
	return newActionSecret(actionSecretOptions{
		Purpose: ActionEmailVerification,
		Now:     s.now().UTC(), TTL: s.tokenTTL, Random: s.random,
	})
}

func insertPendingStudent(
	ctx context.Context,
	tx pgx.Tx,
	registration preparedRegistration,
) (string, bool, error) {
	accountID := uuid.NewString()
	err := tx.QueryRow(ctx,
		`INSERT INTO accounts
		   (id, normalized_email, email, role, status, display_name, locale)
		 VALUES ($1::uuid, $2, $3, 'STUDENT', 'PENDING_VERIFICATION', $4, $5)
		 ON CONFLICT (normalized_email) DO NOTHING
		 RETURNING id::text`,
		accountID,
		registration.normalizedEmail,
		registration.correspondenceEmail,
		registration.displayName,
		registration.locale,
	).Scan(&accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("inserting pending Student: %w", err)
	}
	return accountID, true, nil
}

func (s *AdmissionService) insertRegistrationFacts(
	ctx context.Context,
	tx pgx.Tx,
	accountID string,
	registration preparedRegistration,
) error {
	if _, err := tx.Exec(ctx,
		`INSERT INTO password_credentials (account_id, password_hash, state)
		 VALUES ($1::uuid, $2, 'ACTIVE')`,
		accountID, registration.credentialHash.Expose(),
	); err != nil {
		return fmt.Errorf("inserting Student credential: %w", err)
	}
	if err := insertPolicyAcceptances(ctx, tx, accountID, registration); err != nil {
		return err
	}
	if err := insertActionSecret(ctx, tx, accountID, registration.actionSecret); err != nil {
		return err
	}
	if err := appendIdentitySecurityEvent(
		ctx, tx, securityEventAppend{
			eventType:      "STUDENT_REGISTRATION_ACCEPTED",
			accountID:      accountID,
			actionSecretID: registration.actionSecret.ID,
			revision:       1,
			requestID:      registration.requestID,
			evidence: map[string]any{
				"schema_version": 1,
				"policy_set_id":  registration.policySet.ID,
				"locale":         registration.locale,
			},
		},
	); err != nil {
		return err
	}
	return s.appendVerificationOutbox(ctx, tx, verificationOutboxRequest{
		accountID:   accountID,
		revision:    1,
		email:       registration.correspondenceEmail,
		locale:      registration.locale,
		secret:      registration.actionSecret,
		requestID:   registration.requestID,
		reservation: registration.outboxReservation,
	})
}

func insertPolicyAcceptances(
	ctx context.Context,
	tx pgx.Tx,
	accountID string,
	registration preparedRegistration,
) error {
	for _, policy := range registration.policySet.Policies {
		if _, err := tx.Exec(ctx,
			`INSERT INTO policy_acceptances
			   (account_id, policy_set_id, policy_kind, policy_version, locale, request_id)
			 VALUES ($1::uuid, $2, $3, $4, $5, $6)`,
			accountID,
			registration.policySet.ID,
			policy.Kind,
			policy.Version,
			registration.locale,
			registration.requestID,
		); err != nil {
			return fmt.Errorf("inserting policy acceptance: %w", err)
		}
	}
	return nil
}

func insertActionSecret(
	ctx context.Context,
	tx pgx.Tx,
	accountID string,
	secret IssuedActionSecret,
) error {
	if _, err := tx.Exec(ctx,
		`INSERT INTO identity_action_secrets
		   (id, account_id, purpose, secret_digest, issued_at, expires_at, created_at)
		 VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $5)`,
		secret.ID,
		accountID,
		secret.Purpose,
		secret.Digest,
		secret.IssuedAt,
		secret.ExpiresAt,
	); err != nil {
		return fmt.Errorf("inserting Identity action secret: %w", err)
	}
	return nil
}

type verificationOutboxRequest struct {
	accountID   string
	revision    int
	email       string
	locale      Locale
	secret      IssuedActionSecret
	requestID   string
	reservation outbox.ProtectedPayloadReservation
}

func (s *AdmissionService) appendVerificationOutbox(
	ctx context.Context,
	tx pgx.Tx,
	request verificationOutboxRequest,
) error {
	_, err := s.outbox.AppendReserved(ctx, tx, outbox.ReservedAppend{
		Event: outbox.Event{
			Type:              "identity.email_verification_requested",
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
				"template_contract": verificationTemplateContract,
				"secret_expires_at": request.secret.ExpiresAt,
			},
		},
		Protected: outbox.VerificationDelivery{
			Destination:       request.email,
			Locale:            string(request.locale),
			TemplateContract:  verificationTemplateContract,
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

func (s *AdmissionService) RequestEmailVerification(
	ctx context.Context,
	request VerificationRequest,
) error {
	normalizedEmail, err := NormalizeEmail(request.Email)
	if err != nil {
		return err
	}
	requestID, err := validateRequestID(request.RequestID)
	if err != nil {
		return err
	}
	replacement, err := s.issueActionSecret()
	if err != nil {
		return fmt.Errorf("%w: action-secret generation", ErrAdmissionUnavailable)
	}
	reservation, err := s.outbox.ReserveProtectedPayload(ctx)
	if err != nil {
		return fmt.Errorf("%w: protected payload reservation", ErrDeliveryUnavailable)
	}

	started := time.Now()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("beginning verification request: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	account, eligible, err := lockPendingStudentByEmail(ctx, tx, normalizedEmail)
	if err != nil {
		return err
	}
	if !eligible {
		return commitVerificationRequest(ctx, tx, started)
	}
	if err := supersedeLiveVerificationSecret(ctx, tx, account.id, replacement); err != nil {
		return err
	}
	if err := insertActionSecret(ctx, tx, account.id, replacement); err != nil {
		return err
	}
	if err := appendIdentitySecurityEvent(
		ctx, tx, securityEventAppend{
			eventType:      "EMAIL_VERIFICATION_REISSUED",
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
	if err := s.appendVerificationOutbox(ctx, tx, verificationOutboxRequest{
		accountID: account.id, revision: account.revision, email: account.email,
		locale: account.locale, secret: replacement, requestID: requestID,
		reservation: reservation,
	}); err != nil {
		return err
	}
	return commitVerificationRequest(ctx, tx, started)
}

func commitVerificationRequest(
	ctx context.Context,
	tx pgx.Tx,
	started time.Time,
) error {
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing verification request: %w", err)
	}
	if remaining := time.Until(started.Add(minimumVerificationRequestDuration)); remaining > 0 {
		time.Sleep(remaining)
	}
	return nil
}

type pendingStudent struct {
	id       string
	email    string
	locale   Locale
	revision int
}

func lockPendingStudentByEmail(
	ctx context.Context,
	tx pgx.Tx,
	normalizedEmail string,
) (pendingStudent, bool, error) {
	var account pendingStudent
	var role string
	var status string
	err := tx.QueryRow(ctx,
		`SELECT id::text, email, locale, revision, role, status
		   FROM accounts
		  WHERE normalized_email = $1
		  FOR UPDATE`,
		normalizedEmail,
	).Scan(&account.id, &account.email, &account.locale, &account.revision, &role, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return pendingStudent{}, false, nil
	}
	if err != nil {
		return pendingStudent{}, false, fmt.Errorf("locking verification Account: %w", err)
	}
	return account, role == "STUDENT" && status == "PENDING_VERIFICATION", nil
}

func supersedeLiveVerificationSecret(
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
		    AND purpose = 'EMAIL_VERIFICATION'
		    AND consumed_at IS NULL
		    AND superseded_at IS NULL
		  FOR UPDATE`,
		accountID,
	).Scan(&currentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("locking live verification secret: %w", err)
	}
	// Stamped from the statement clock after the Account row lock, for the same
	// reason as the reset path in recovery.go: the replacement timestamp
	// predates both this transaction and its wait on the lock, so concurrent
	// resend requests invert generation order against lock order and stamp an
	// older superseded_at onto a newer row. This was latent in S1B1 rather than
	// introduced by S1B3 — the existing concurrency test freezes the clock, so
	// every issued_at is equal and the inversion never occurs there. GREATEST
	// remains only as a clock-skew backstop for the constraint.
	if _, err := tx.Exec(ctx,
		`UPDATE identity_action_secrets
		    SET superseded_at = GREATEST(issued_at, clock_timestamp()),
		        superseded_by_id = $1::uuid
		  WHERE id = $2::uuid`,
		replacement.ID, currentID,
	); err != nil {
		return fmt.Errorf("superseding verification secret: %w", err)
	}
	return nil
}

func (s *AdmissionService) VerifyEmail(
	ctx context.Context,
	bearer string,
	requestID string,
) error {
	requestID, err := validateRequestID(requestID)
	if err != nil {
		return err
	}
	digest, err := DigestActionSecret(bearer)
	if err != nil {
		return ErrTokenInvalid
	}
	var accountID string
	err = s.pool.QueryRow(ctx,
		`SELECT account_id::text
		   FROM identity_action_secrets
		  WHERE secret_digest = $1 AND purpose = 'EMAIL_VERIFICATION'`,
		digest,
	).Scan(&accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTokenInvalid
	}
	if err != nil {
		return fmt.Errorf("resolving verification secret: %w", err)
	}
	return s.consumeVerification(ctx, accountID, digest, requestID)
}

func (s *AdmissionService) consumeVerification(
	ctx context.Context,
	accountID string,
	digest []byte,
	requestID string,
) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("beginning email verification: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	now := s.now().UTC()
	account, valid, err := lockVerificationAccount(ctx, tx, accountID)
	if err != nil {
		return err
	}
	secretID, secretValid, err := lockVerificationSecret(ctx, tx, digest, now)
	if err != nil {
		return err
	}
	if !valid || !secretValid {
		return ErrTokenInvalid
	}
	if err := activateStudent(ctx, tx, activationRequest{
		accountID:       account.id,
		secretID:        secretID,
		currentRevision: account.revision,
		requestID:       requestID,
		now:             now,
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing email verification: %w", err)
	}
	return nil
}

func lockVerificationAccount(
	ctx context.Context,
	tx pgx.Tx,
	accountID string,
) (pendingStudent, bool, error) {
	var account pendingStudent
	var role, status string
	err := tx.QueryRow(ctx,
		`SELECT id::text, revision, role, status
		   FROM accounts WHERE id = $1::uuid FOR UPDATE`,
		accountID,
	).Scan(&account.id, &account.revision, &role, &status)
	if err != nil {
		return pendingStudent{}, false, fmt.Errorf("locking verification Account: %w", err)
	}
	return account, role == "STUDENT" && status == "PENDING_VERIFICATION", nil
}

type activationRequest struct {
	accountID       string
	secretID        string
	currentRevision int
	requestID       string
	now             time.Time
}

func lockVerificationSecret(
	ctx context.Context,
	tx pgx.Tx,
	digest []byte,
	now time.Time,
) (string, bool, error) {
	var id string
	var expiresAt time.Time
	var consumedAt, supersededAt *time.Time
	err := tx.QueryRow(ctx,
		`SELECT id::text, expires_at, consumed_at, superseded_at
		   FROM identity_action_secrets
		  WHERE secret_digest = $1 AND purpose = 'EMAIL_VERIFICATION'
		  FOR UPDATE`,
		digest,
	).Scan(&id, &expiresAt, &consumedAt, &supersededAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("locking verification secret: %w", err)
	}
	valid := consumedAt == nil && supersededAt == nil && now.Before(expiresAt)
	return id, valid, nil
}

func activateStudent(
	ctx context.Context,
	tx pgx.Tx,
	request activationRequest,
) error {
	if _, err := tx.Exec(ctx,
		`UPDATE accounts
		    SET status = 'ACTIVE',
		        email_verified_at = $1,
		        revision = revision + 1,
		        updated_at = $1
		  WHERE id = $2::uuid AND revision = $3`,
		request.now, request.accountID, request.currentRevision,
	); err != nil {
		return fmt.Errorf("activating Student: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE identity_action_secrets
		    SET consumed_at = $1,
		        attempt_count = attempt_count + 1,
		        first_attempt_at = COALESCE(first_attempt_at, $1),
		        last_attempt_at = $1
		  WHERE id = $2::uuid`,
		request.now, request.secretID,
	); err != nil {
		return fmt.Errorf("consuming verification secret: %w", err)
	}
	return appendIdentitySecurityEvent(
		ctx, tx, securityEventAppend{
			eventType:      "STUDENT_EMAIL_VERIFIED",
			accountID:      request.accountID,
			actionSecretID: request.secretID,
			revision:       request.currentRevision + 1,
			requestID:      request.requestID,
			evidence: map[string]any{
				"schema_version": 1,
				"outcome_class":  "VERIFIED",
			},
		},
	)
}
