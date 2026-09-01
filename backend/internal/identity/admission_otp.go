package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

// VerificationChallenge is what a caller may know about a pending verification
// without holding the code.
//
// Every field here is deliberately non-secret. ChallengeID names which
// challenge a code is being presented against and grants nothing on its own —
// presenting it without the code is exactly as useful as presenting nothing.
// MaskedEmail is derived from an address the caller already supplied. The two
// timestamps describe the metering the server is going to apply anyway, and
// showing them is what lets the screen render an honest countdown instead of
// guessing.
type VerificationChallenge struct {
	ChallengeID       string
	MaskedEmail       string
	ExpiresAt         time.Time
	ResendAvailableAt time.Time
}

func challengeOf(otp IssuedEmailOTP, email string) VerificationChallenge {
	return VerificationChallenge{
		ChallengeID:       otp.ChallengeID,
		MaskedEmail:       MaskEmail(email),
		ExpiresAt:         otp.ExpiresAt,
		ResendAvailableAt: otp.ResendAvailableAt(),
	}
}

// liveChallenge describes a challenge that already exists, for a caller asking
// again inside the cooldown. Nothing is issued and nothing is superseded: this
// is the handle for the code that was already sent.
func liveChallenge(live liveEmailOTP, email string) VerificationChallenge {
	return VerificationChallenge{
		ChallengeID:       live.id,
		MaskedEmail:       MaskEmail(email),
		ExpiresAt:         live.expiresAt,
		ResendAvailableAt: live.issuedAt.Add(EmailOTPResendCooldown),
	}
}

// syntheticChallenge is the answer for an address or challenge that is not an
// eligible pending Student.
//
// It is not a decoy for its own sake: without it, "this address is already
// registered" and "this address is new" would be two visibly different
// responses on a public route, which is precisely the account-enumeration leak
// the rest of admission is built to avoid. The identifier is a fresh UUID that
// exists in no table, so verifying against it always refuses, and no code is
// mailed anywhere.
func (s *AdmissionService) syntheticChallenge(email string) VerificationChallenge {
	now := s.now().UTC()
	return VerificationChallenge{
		ChallengeID:       uuid.NewString(),
		MaskedEmail:       MaskEmail(email),
		ExpiresAt:         now.Add(s.otpTTL),
		ResendAvailableAt: now.Add(EmailOTPResendCooldown),
	}
}

type liveEmailOTP struct {
	id           string
	accountID    string
	digest       []byte
	issuedAt     time.Time
	expiresAt    time.Time
	attemptCount int
}

// lockLiveEmailOTP takes the current unconsumed, unsuperseded challenge for an
// Account. The caller must already hold the Account row lock: every path in
// this file takes Account before secret, so a resend and a verification racing
// on the same Account queue instead of deadlocking.
func lockLiveEmailOTP(
	ctx context.Context,
	tx pgx.Tx,
	accountID string,
) (liveEmailOTP, bool, error) {
	var live liveEmailOTP
	live.accountID = accountID
	err := tx.QueryRow(ctx,
		`SELECT id::text, secret_digest, issued_at, expires_at, attempt_count
		   FROM identity_action_secrets
		  WHERE account_id = $1::uuid
		    AND purpose = 'EMAIL_VERIFICATION_OTP'
		    AND consumed_at IS NULL
		    AND superseded_at IS NULL
		  FOR UPDATE`,
		accountID,
	).Scan(&live.id, &live.digest, &live.issuedAt, &live.expiresAt, &live.attemptCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return liveEmailOTP{}, false, nil
	}
	if err != nil {
		return liveEmailOTP{}, false, fmt.Errorf("locking live verification challenge: %w", err)
	}
	return live, true, nil
}

type reissueRequest struct {
	account     pendingStudent
	replacement IssuedEmailOTP
	requestID   string
	reservation outbox.ProtectedPayloadReservation
	reason      string
}

// reissueEmailOTP supersedes whatever challenge is live and installs a new one
// in the same transaction.
//
// Supersession is what makes "resend invalidates the previous code" true rather
// than aspirational, and it is also what resets the attempt budget: the counter
// lives on the row, so a new row is a new budget and the old row can never be
// guessed against again.
func (s *AdmissionService) reissueEmailOTP(
	ctx context.Context,
	tx pgx.Tx,
	request reissueRequest,
) error {
	if err := supersedeLiveEmailOTP(ctx, tx, request.account.id, request.replacement.ChallengeID); err != nil {
		return err
	}
	if err := insertEmailOTPSecret(ctx, tx, request.account.id, request.replacement); err != nil {
		return err
	}
	if err := appendIdentitySecurityEvent(ctx, tx, securityEventAppend{
		eventType:      "EMAIL_VERIFICATION_REISSUED",
		accountID:      request.account.id,
		actionSecretID: request.replacement.ChallengeID,
		revision:       request.account.revision,
		requestID:      request.requestID,
		evidence: map[string]any{
			"schema_version":      1,
			"outcome_class":       "ELIGIBLE",
			"verification_method": "EMAIL_OTP",
			"reissue_reason":      request.reason,
		},
	}); err != nil {
		return err
	}
	return s.appendVerificationCodeOutbox(ctx, tx, verificationCodeOutboxRequest{
		accountID:   request.account.id,
		revision:    request.account.revision,
		email:       request.account.email,
		locale:      request.account.locale,
		otp:         request.replacement,
		requestID:   request.requestID,
		reservation: request.reservation,
	})
}

// supersedeLiveEmailOTP stamps the replacement time from the statement clock
// after the Account lock, for the same reason the link path does: the
// replacement's issued_at predates this transaction's wait on the lock, so two
// concurrent resends would otherwise stamp an older superseded_at onto a newer
// row and invert generation order against lock order.
func supersedeLiveEmailOTP(
	ctx context.Context,
	tx pgx.Tx,
	accountID string,
	replacementID string,
) error {
	if _, err := tx.Exec(ctx,
		`UPDATE identity_action_secrets
		    SET superseded_at = GREATEST(issued_at, clock_timestamp()),
		        superseded_by_id = $1::uuid
		  WHERE account_id = $2::uuid
		    AND purpose = 'EMAIL_VERIFICATION_OTP'
		    AND consumed_at IS NULL
		    AND superseded_at IS NULL`,
		replacementID, accountID,
	); err != nil {
		return fmt.Errorf("superseding verification challenge: %w", err)
	}
	return nil
}

type verificationCodeOutboxRequest struct {
	accountID   string
	revision    int
	email       string
	locale      Locale
	otp         IssuedEmailOTP
	requestID   string
	reservation outbox.ProtectedPayloadReservation
}

// appendVerificationCodeOutbox records the intent to mail one code.
//
// The safe payload — the half that is stored in clear and shows up in
// operational queries — carries the challenge id, the locale, the contract, and
// the expiry, and nothing else. The code itself exists only inside the
// authenticated ciphertext, so no log line, no admin query, and no event export
// can reproduce it.
func (s *AdmissionService) appendVerificationCodeOutbox(
	ctx context.Context,
	tx pgx.Tx,
	request verificationCodeOutboxRequest,
) error {
	_, err := s.outbox.AppendReserved(ctx, tx, outbox.ReservedAppend{
		Event: outbox.Event{
			Type:              "identity.email_verification_code_requested",
			SchemaVersion:     1,
			SourceModule:      "IDENTITY_AND_ACCESS",
			AggregateType:     "ACCOUNT",
			AggregateID:       request.accountID,
			AggregateRevision: request.revision,
			CorrelationID:     request.requestID,
			SafePayload: map[string]any{
				"purpose":           "EMAIL_VERIFICATION_OTP",
				"challenge_id":      request.otp.ChallengeID,
				"locale":            request.locale,
				"template_contract": verificationCodeTemplateContract,
				"code_expires_at":   request.otp.ExpiresAt,
			},
		},
		Protected: outbox.VerificationCodeDelivery{
			Destination:      request.email,
			Locale:           string(request.locale),
			TemplateContract: verificationCodeTemplateContract,
			Code:             request.otp.Code.Expose(),
			ExpiresAt:        request.otp.ExpiresAt,
		},
		Reservation: request.reservation,
	})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDeliveryUnavailable, err)
	}
	return nil
}

// ResendEmailVerificationOTP replaces the code behind one challenge.
//
// It is keyed on the challenge rather than the address so the verification
// screen never has to ask for the address again. An unknown or ineligible
// challenge answers with a synthetic replacement for the same reason
// registration does: a caller holding a stale handle must not be able to tell
// whether it ever named a real pending Account.
func (s *AdmissionService) ResendEmailVerificationOTP(
	ctx context.Context,
	challengeID string,
	requestID string,
) (VerificationChallenge, error) {
	requestID, err := validateRequestID(requestID)
	if err != nil {
		return VerificationChallenge{}, err
	}
	if _, err := uuid.Parse(challengeID); err != nil {
		return VerificationChallenge{}, ErrOTPInvalid
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
		return VerificationChallenge{}, fmt.Errorf("beginning verification resend: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	accountID, found, err := accountForChallenge(ctx, tx, challengeID)
	if err != nil {
		return VerificationChallenge{}, err
	}
	if !found {
		if err := commitVerificationRequest(ctx, tx, started); err != nil {
			return VerificationChallenge{}, err
		}
		return s.syntheticChallenge(""), nil
	}
	account, eligible, err := lockPendingStudentByID(ctx, tx, accountID)
	if err != nil {
		return VerificationChallenge{}, err
	}
	if !eligible {
		if err := commitVerificationRequest(ctx, tx, started); err != nil {
			return VerificationChallenge{}, err
		}
		return s.syntheticChallenge(""), nil
	}
	live, hasLive, err := lockLiveEmailOTP(ctx, tx, account.id)
	if err != nil {
		return VerificationChallenge{}, err
	}
	// A challenge that is already superseded or consumed cannot buy a resend:
	// otherwise a stale handle would keep the chain alive forever and every
	// exhausted attempt budget would be one request away from a fresh one.
	if !hasLive || live.id != challengeID {
		if err := commitVerificationRequest(ctx, tx, started); err != nil {
			return VerificationChallenge{}, err
		}
		return s.syntheticChallenge(""), nil
	}
	if s.now().UTC().Before(live.issuedAt.Add(EmailOTPResendCooldown)) {
		if err := commitVerificationRequest(ctx, tx, started); err != nil {
			return VerificationChallenge{}, err
		}
		return VerificationChallenge{}, ErrOTPResendTooSoon
	}
	if err := s.reissueEmailOTP(ctx, tx, reissueRequest{
		account: account, replacement: replacement, requestID: requestID,
		reservation: reservation, reason: "CHALLENGE_RESEND",
	}); err != nil {
		return VerificationChallenge{}, err
	}
	if err := commitVerificationRequest(ctx, tx, started); err != nil {
		return VerificationChallenge{}, err
	}
	// No masked address: the caller supplied none, and echoing the stored one
	// would make an eligible resend distinguishable from a stale-handle one.
	// The screen already holds the mask from the challenge it is replacing.
	return challengeOf(replacement, ""), nil
}

// accountForChallenge resolves the Account a challenge belongs to without
// taking a lock, so the caller can then lock Account before secret. Reading the
// owner is not a decision; every decision below re-reads under the lock.
func accountForChallenge(
	ctx context.Context,
	tx pgx.Tx,
	challengeID string,
) (string, bool, error) {
	var accountID string
	err := tx.QueryRow(ctx,
		`SELECT account_id::text
		   FROM identity_action_secrets
		  WHERE id = $1::uuid AND purpose = 'EMAIL_VERIFICATION_OTP'`,
		challengeID,
	).Scan(&accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("resolving verification challenge owner: %w", err)
	}
	return accountID, true, nil
}

func lockPendingStudentByID(
	ctx context.Context,
	tx pgx.Tx,
	accountID string,
) (pendingStudent, bool, error) {
	var account pendingStudent
	var role, status string
	err := tx.QueryRow(ctx,
		`SELECT id::text, email, locale, revision, role, status
		   FROM accounts WHERE id = $1::uuid FOR UPDATE`,
		accountID,
	).Scan(&account.id, &account.email, &account.locale, &account.revision, &role, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return pendingStudent{}, false, nil
	}
	if err != nil {
		return pendingStudent{}, false, fmt.Errorf("locking verification Account: %w", err)
	}
	return account, role == "STUDENT" && status == "PENDING_VERIFICATION", nil
}

// VerifyEmailOTP consumes a code and, on success, authenticates the Student.
//
// Activation, consumption of the challenge, invalidation of any still-live
// legacy verification link, and creation of the session all happen in one
// transaction. Splitting them was the alternative and it has no safe ordering:
// activate-then-authenticate can leave a verified Student anonymous, and
// authenticate-then-activate mints a session for an Account that is still
// pending.
//
// Failed attempts are recorded even though the call fails, which is why the
// attempt counter is written in its own committed transaction before the
// outcome is decided. A guessing budget that only persists on success is not a
// budget.
func (s *AdmissionService) VerifyEmailOTP(
	ctx context.Context,
	challengeID string,
	rawCode string,
	requestID string,
) (SessionGrant, error) {
	requestID, err := validateRequestID(requestID)
	if err != nil {
		return SessionGrant{}, err
	}
	if _, err := uuid.Parse(challengeID); err != nil {
		return SessionGrant{}, ErrOTPInvalid
	}
	code, ok := NormalizeEmailOTPInput(rawCode)
	if !ok {
		// Malformed input is refused before it can spend an attempt. It is not
		// a guess against the code space, and charging for it would let a
		// bystander exhaust a Student's budget with junk.
		return SessionGrant{}, ErrOTPInvalid
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return SessionGrant{}, fmt.Errorf("beginning email verification: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	accountID, found, err := accountForChallenge(ctx, tx, challengeID)
	if err != nil {
		return SessionGrant{}, err
	}
	if !found {
		return SessionGrant{}, ErrOTPInvalid
	}
	account, eligible, err := lockPendingStudentByID(ctx, tx, accountID)
	if err != nil {
		return SessionGrant{}, err
	}
	live, hasLive, err := lockLiveEmailOTP(ctx, tx, accountID)
	if err != nil {
		return SessionGrant{}, err
	}
	now := s.now().UTC()
	if !eligible || !hasLive || live.id != challengeID || !now.Before(live.expiresAt) {
		return SessionGrant{}, ErrOTPInvalid
	}
	if live.attemptCount >= EmailOTPMaxAttempts {
		// Already spent. Retire the challenge so nothing further can be tried
		// against it and the Student's only route forward is a new code.
		if err := exhaustEmailOTP(ctx, tx, live, account, requestID, now); err != nil {
			return SessionGrant{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return SessionGrant{}, fmt.Errorf("committing exhausted verification challenge: %w", err)
		}
		return SessionGrant{}, ErrOTPAttemptsExhausted
	}

	if err := recordEmailOTPAttempt(ctx, tx, live.id, now); err != nil {
		return SessionGrant{}, err
	}
	if !s.otpPepper.MatchesEmailOTP(challengeID, code, live.digest) {
		remaining := EmailOTPMaxAttempts - (live.attemptCount + 1)
		if remaining <= 0 {
			if err := exhaustEmailOTP(ctx, tx, live, account, requestID, now); err != nil {
				return SessionGrant{}, err
			}
		}
		// The attempt is committed. The caller still receives a refusal.
		if err := tx.Commit(ctx); err != nil {
			return SessionGrant{}, fmt.Errorf("committing verification attempt: %w", err)
		}
		if remaining <= 0 {
			return SessionGrant{}, ErrOTPAttemptsExhausted
		}
		return SessionGrant{}, ErrOTPInvalid
	}

	if err := activateStudent(ctx, tx, activationRequest{
		accountID:       account.id,
		secretID:        live.id,
		currentRevision: account.revision,
		requestID:       requestID,
		now:             now,
	}); err != nil {
		return SessionGrant{}, err
	}
	// A legacy verification link for the same Account is now redundant. Leaving
	// it live would keep a second usable credential in a mailbox for an Account
	// that is already verified.
	if err := supersedeLegacyVerificationLink(ctx, tx, account.id, live.id); err != nil {
		return SessionGrant{}, err
	}
	grant, err := s.sessions.IssueSessionInTransaction(ctx, tx, account.id, requestID)
	if err != nil {
		return SessionGrant{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SessionGrant{}, fmt.Errorf("committing email verification: %w", err)
	}
	return grant, nil
}

// recordEmailOTPAttempt charges one guess against the challenge budget.
func recordEmailOTPAttempt(ctx context.Context, tx pgx.Tx, secretID string, now time.Time) error {
	if _, err := tx.Exec(ctx,
		`UPDATE identity_action_secrets
		    SET attempt_count = attempt_count + 1,
		        first_attempt_at = COALESCE(first_attempt_at, $1),
		        last_attempt_at = $1
		  WHERE id = $2::uuid`,
		now, secretID,
	); err != nil {
		return fmt.Errorf("recording verification attempt: %w", err)
	}
	return nil
}

// exhaustEmailOTP retires a challenge whose budget is spent.
//
// It supersedes rather than consumes: consumption means "this proved an
// identity", and nothing was proved here. Superseding it also frees the
// one-live-challenge slot so the Student's next request installs a fresh
// challenge with a fresh budget.
func exhaustEmailOTP(
	ctx context.Context,
	tx pgx.Tx,
	live liveEmailOTP,
	account pendingStudent,
	requestID string,
	now time.Time,
) error {
	if _, err := tx.Exec(ctx,
		`UPDATE identity_action_secrets
		    SET superseded_at = GREATEST(issued_at, clock_timestamp()),
		        superseded_by_id = id
		  WHERE id = $1::uuid AND consumed_at IS NULL AND superseded_at IS NULL`,
		live.id,
	); err != nil {
		return fmt.Errorf("retiring exhausted verification challenge: %w", err)
	}
	return appendIdentitySecurityEvent(ctx, tx, securityEventAppend{
		eventType:      "EMAIL_VERIFICATION_ATTEMPTS_EXHAUSTED",
		accountID:      account.id,
		actionSecretID: live.id,
		revision:       account.revision,
		requestID:      requestID,
		evidence: map[string]any{
			"schema_version":      1,
			"outcome_class":       "EXHAUSTED",
			"verification_method": "EMAIL_OTP",
			"attempt_limit":       EmailOTPMaxAttempts,
		},
	})
}

// supersedeLegacyVerificationLink retires a pre-OTP bearer for an Account that
// has just verified by code. It is a no-op for every Account registered after
// the cutover, and it is what keeps the legacy window from leaving a live link
// behind a verified Account.
func supersedeLegacyVerificationLink(
	ctx context.Context,
	tx pgx.Tx,
	accountID string,
	supersedingID string,
) error {
	if _, err := tx.Exec(ctx,
		`UPDATE identity_action_secrets
		    SET superseded_at = GREATEST(issued_at, clock_timestamp()),
		        superseded_by_id = $1::uuid
		  WHERE account_id = $2::uuid
		    AND purpose = 'EMAIL_VERIFICATION'
		    AND consumed_at IS NULL
		    AND superseded_at IS NULL`,
		supersedingID, accountID,
	); err != nil {
		return fmt.Errorf("superseding legacy verification link: %w", err)
	}
	return nil
}
