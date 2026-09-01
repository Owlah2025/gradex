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
	// verificationTemplateContract names the pre-OTP link message. Nothing
	// issues it any more; it is retained so operational queries and tests can
	// still name the contract carried by links that were delivered before the
	// cutover and have not yet expired.
	verificationTemplateContract = "student-email-verification-v1"
	// The OTP contract is a separate template rather than a variant of the link
	// one. The dispatcher selects rendering by contract, and a message that can
	// render either a link or a code from the same contract is one branch away
	// from mailing both.
	verificationCodeTemplateContract   = "student-email-verification-otp-v1"
	minimumVerificationRequestDuration = 75 * time.Millisecond
)

type AdmissionService struct {
	pool        *pgxpool.Pool
	policies    PolicySetResolver
	compromised CompromisedRangeSource
	outbox      *outbox.Writer
	sessions    *SessionRepository
	tokenTTL    time.Duration
	otpPepper   EmailOTPPepper
	otpTTL      time.Duration
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
	// Verification now mints a session, so the session authority is a hard
	// dependency rather than an optional one: without it a Student could prove
	// their code and still be left anonymous.
	if options.Sessions == nil {
		return nil, errors.New("admission service requires the session repository")
	}
	if options.EmailOTPTTL <= 0 {
		return nil, errors.New("email verification OTP TTL is required")
	}
	pepper, err := NewEmailOTPPepper(options.EmailOTPPepper)
	if err != nil {
		return nil, err
	}
	return &AdmissionService{
		pool:        options.Pool,
		policies:    options.Policies,
		compromised: options.Compromised,
		outbox:      options.Outbox,
		sessions:    options.Sessions,
		tokenTTL:    options.VerificationTTL,
		otpPepper:   pepper,
		otpTTL:      options.EmailOTPTTL,
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
	otp                 IssuedEmailOTP
	outboxReservation   outbox.ProtectedPayloadReservation
	requestID           string
}

// RegisterStudent creates a pending Student and mails a verification code.
//
// It returns the non-secret facts the verification screen needs so the Student
// is never asked for their address a second time on the very next screen. None
// of those facts authenticates anyone: the challenge id is an opaque handle
// that only names which challenge a code is being presented against, and the
// masked address is derived from what the caller already typed.
//
// A duplicate address returns a challenge too. That is the whole
// anti-enumeration property of this route: an address already registered and a
// fresh one produce indistinguishable responses, and the synthetic challenge
// simply never matches a code.
func (s *AdmissionService) RegisterStudent(
	ctx context.Context,
	request StudentRegistration,
) (VerificationChallenge, error) {
	prepared, err := s.prepareRegistration(ctx, request)
	if err != nil {
		return VerificationChallenge{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return VerificationChallenge{}, fmt.Errorf("beginning Student registration: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	accountID, created, err := insertPendingStudent(ctx, tx, prepared)
	if err != nil {
		return VerificationChallenge{}, err
	}
	if !created {
		if err := tx.Commit(ctx); err != nil {
			return VerificationChallenge{}, fmt.Errorf("committing Student registration: %w", err)
		}
		return s.syntheticChallenge(prepared.correspondenceEmail), nil
	}
	if err := s.insertRegistrationFacts(ctx, tx, accountID, prepared); err != nil {
		return VerificationChallenge{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return VerificationChallenge{}, fmt.Errorf("committing Student registration: %w", err)
	}
	return challengeOf(prepared.otp, prepared.correspondenceEmail), nil
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
	otp, err := s.issueEmailOTP()
	if err != nil {
		return preparedRegistration{}, fmt.Errorf("%w: verification-code generation", ErrAdmissionUnavailable)
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
		otp:                 otp,
		outboxReservation:   reservation,
		requestID:           requestID,
	}, nil
}

func (s *AdmissionService) issueEmailOTP() (IssuedEmailOTP, error) {
	s.randomMu.Lock()
	defer s.randomMu.Unlock()
	return newEmailOTP(emailOTPOptions{
		Pepper: s.otpPepper, Now: s.now().UTC(), TTL: s.otpTTL, Random: s.random,
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
	if err := insertEmailOTPSecret(ctx, tx, accountID, registration.otp); err != nil {
		return err
	}
	if err := appendIdentitySecurityEvent(
		ctx, tx, securityEventAppend{
			eventType:      "STUDENT_REGISTRATION_ACCEPTED",
			accountID:      accountID,
			actionSecretID: registration.otp.ChallengeID,
			revision:       1,
			requestID:      registration.requestID,
			evidence: map[string]any{
				"schema_version": 1,
				"policy_set_id":  registration.policySet.ID,
				"locale":         registration.locale,
				// The verification method is recorded; the code never is.
				"verification_method": "EMAIL_OTP",
			},
		},
	); err != nil {
		return err
	}
	return s.appendVerificationCodeOutbox(ctx, tx, verificationCodeOutboxRequest{
		accountID:   accountID,
		revision:    1,
		email:       registration.correspondenceEmail,
		locale:      registration.locale,
		otp:         registration.otp,
		requestID:   registration.requestID,
		reservation: registration.outboxReservation,
	})
}

// insertEmailOTPSecret stores the keyed digest of one verification code.
//
// The plaintext code is not a column here and never becomes one: what is
// written is the HMAC output, which is the same 32 bytes the existing digest
// size and uniqueness constraints already govern.
func insertEmailOTPSecret(
	ctx context.Context,
	tx pgx.Tx,
	accountID string,
	otp IssuedEmailOTP,
) error {
	if _, err := tx.Exec(ctx,
		`INSERT INTO identity_action_secrets
		   (id, account_id, purpose, secret_digest, issued_at, expires_at, created_at)
		 VALUES ($1::uuid, $2::uuid, 'EMAIL_VERIFICATION_OTP', $3, $4, $5, $4)`,
		otp.ChallengeID,
		accountID,
		otp.Digest,
		otp.IssuedAt,
		otp.ExpiresAt,
	); err != nil {
		return fmt.Errorf("inserting Student verification challenge: %w", err)
	}
	return nil
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

func (s *AdmissionService) RequestEmailVerification(
	ctx context.Context,
	request VerificationRequest,
) (VerificationChallenge, error) {
	normalizedEmail, err := NormalizeEmail(request.Email)
	if err != nil {
		return VerificationChallenge{}, err
	}
	requestID, err := validateRequestID(request.RequestID)
	if err != nil {
		return VerificationChallenge{}, err
	}
	replacement, err := s.issueEmailOTP()
	if err != nil {
		return VerificationChallenge{}, fmt.Errorf("%w: verification-code generation", ErrAdmissionUnavailable)
	}
	reservation, err := s.outbox.ReserveProtectedPayload(ctx)
	if err != nil {
		return VerificationChallenge{}, fmt.Errorf("%w: protected payload reservation", ErrDeliveryUnavailable)
	}

	started := time.Now()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return VerificationChallenge{}, fmt.Errorf("beginning verification request: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	account, eligible, err := lockPendingStudentByEmail(ctx, tx, normalizedEmail)
	if err != nil {
		return VerificationChallenge{}, err
	}
	if !eligible {
		if err := commitVerificationRequest(ctx, tx, started); err != nil {
			return VerificationChallenge{}, err
		}
		return s.syntheticChallenge(request.Email), nil
	}
	// Inside the cooldown the live challenge is returned unchanged: no new code
	// is generated, nothing is superseded, and no second message is sent.
	//
	// It deliberately does not *refuse*. This route must answer identically for
	// a registered and an unregistered address, and an unregistered one has no
	// cooldown to be inside — so refusing here would turn "429 versus 202" into
	// a direct account-existence oracle. The metering that applies uniformly to
	// this route is the rate limiter, which is keyed on the normalized address
	// and knows nothing about whether an Account exists.
	//
	// The attempt budget is still protected, because the budget lives on the
	// challenge row and this path leaves that row exactly as it found it.
	live, hasLive, err := lockLiveEmailOTP(ctx, tx, account.id)
	if err != nil {
		return VerificationChallenge{}, err
	}
	if hasLive && s.now().UTC().Before(live.issuedAt.Add(EmailOTPResendCooldown)) {
		if err := commitVerificationRequest(ctx, tx, started); err != nil {
			return VerificationChallenge{}, err
		}
		return liveChallenge(live, request.Email), nil
	}
	if err := s.reissueEmailOTP(ctx, tx, reissueRequest{
		account: account, replacement: replacement, requestID: requestID,
		reservation: reservation, reason: "ADDRESS_REQUEST",
	}); err != nil {
		return VerificationChallenge{}, err
	}
	if err := commitVerificationRequest(ctx, tx, started); err != nil {
		return VerificationChallenge{}, err
	}
	// The mask is built from the address the caller typed, exactly as the
	// ineligible path does. Masking the *stored* address instead made the two
	// outcomes distinguishable whenever the two differed in case or
	// whitespace — typing STUDENT@example.com would come back masked as
	// "st***@…" for a registered address and "ST***@…" for an unknown one,
	// which is precisely the account-existence oracle this route is built to
	// avoid.
	return challengeOf(replacement, request.Email), nil
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
