//go:build integration

package identity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

func admissionPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	freshSchema(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("opening admission pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func admissionService(
	t *testing.T,
	pool *pgxpool.Pool,
	now time.Time,
	randomByte byte,
) *AdmissionService {
	t.Helper()
	return admissionServiceWithCompromised(t, pool, now, randomByte, clearCompromisedSource())
}

func admissionServiceWithCompromised(
	t *testing.T,
	pool *pgxpool.Pool,
	now time.Time,
	randomByte byte,
	compromised CompromisedRangeSource,
) *AdmissionService {
	t.Helper()
	return admissionServiceWithResolver(
		t, pool, now, randomByte, compromised, testPolicyResolver(t),
	)
}

func admissionServiceWithResolver(
	t *testing.T,
	pool *pgxpool.Pool,
	now time.Time,
	randomByte byte,
	compromised CompromisedRangeSource,
	policies PolicySetResolver,
) *AdmissionService {
	t.Helper()
	writer, err := outbox.NewWriter("test-v1", bytes.Repeat([]byte{0x51}, 32))
	if err != nil {
		t.Fatalf("constructing outbox writer: %v", err)
	}
	service, err := NewAdmissionService(AdmissionServiceOptions{
		Pool:            pool,
		Policies:        policies,
		Compromised:     compromised,
		Outbox:          writer,
		Sessions:        sessionRepository(t, pool, now),
		VerificationTTL: time.Hour,
		EmailOTPTTL:     10 * time.Minute,
		EmailOTPPepper:  config.NewSecret(strings.Repeat("p", 32)),
		Now:             func() time.Time { return now },
		Random:          bytes.NewReader(deterministicCodeSource(randomByte)),
	})
	if err != nil {
		t.Fatalf("constructing admission service: %v", err)
	}
	return service
}

// deterministicCodeSource makes each successive code drawn from one service
// predictable and distinct.
//
// Six bytes are consumed per code, and every byte below 250 maps to
// `byte % 10`, so a run of one repeated value produces one repeated digit. The
// chunks therefore advance by one per draw: draw n is the digit
// `(seed + n) % 10` six times, which differs from draw n+1 by construction.
// That is what lets a resend test assert "the replacement is a different code"
// without reaching into the generator.
func deterministicCodeSource(seed byte) []byte {
	source := make([]byte, 0, 6*64)
	for offset := byte(0); offset < 64; offset++ {
		source = append(source, bytes.Repeat([]byte{seed + offset}, 6)...)
	}
	return source
}

// deterministicCode is the code deterministicCodeSource yields for one draw.
func deterministicCode(seed byte, draw int) string {
	digit := rune('0' + int(seed+byte(draw))%10)
	return strings.Repeat(string(digit), 6)
}

// insertLegacyVerificationLink writes a pre-OTP emailed bearer exactly as the
// link flow did before the cutover.
//
// It is written directly rather than through the service because nothing in
// production issues these any more — which is the point. The legacy window has
// to be proved against rows that already exist in a live database, not against
// a code path kept alive only so a test can call it.
func insertLegacyVerificationLink(
	t *testing.T,
	pool *pgxpool.Pool,
	accountID string,
	bearer string,
	issuedAt time.Time,
	ttl time.Duration,
) {
	t.Helper()
	digest, err := DigestActionSecret(bearer)
	if err != nil {
		t.Fatalf("digesting legacy bearer: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO identity_action_secrets
		   (account_id, purpose, secret_digest, issued_at, expires_at, created_at)
		 VALUES ($1::uuid, 'EMAIL_VERIFICATION', $2, $3, $4, $3)`,
		accountID, digest, issuedAt, issuedAt.Add(ttl),
	); err != nil {
		t.Fatalf("inserting legacy verification link: %v", err)
	}
}

func onlyAccountID(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `SELECT id::text FROM accounts`).Scan(&id); err != nil {
		t.Fatalf("reading Account: %v", err)
	}
	return id
}

// mustRegister registers and returns the verification challenge the Student is
// expected to be holding.
func mustRegister(t *testing.T, service *AdmissionService, registration StudentRegistration) VerificationChallenge {
	t.Helper()
	challenge, err := service.RegisterStudent(context.Background(), registration)
	if err != nil {
		t.Fatalf("registering Student: %v", err)
	}
	return challenge
}

func studentRegistration() StudentRegistration {
	return StudentRegistration{
		DisplayName: "نورة أحمد",
		Email:       " Student.Name@Example.com ",
		Password:    config.NewSecret("correct horse battery staple 9"),
		Locale:      LocaleArabic,
		PolicySetID: "registration-v1",
		RequestID:   "request-registration-1",
	}
}

func deterministicBearer(fill byte) string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32))
}

func assertAdmissionCanariesAbsent(
	t *testing.T,
	pool *pgxpool.Pool,
	canaries ...string,
) {
	t.Helper()
	for _, canary := range canaries {
		var leaked bool
		if err := pool.QueryRow(context.Background(),
			`SELECT
			   EXISTS (
			     SELECT 1 FROM password_credentials
			      WHERE position($1 in password_hash) > 0
			   )
			   OR EXISTS (
			     SELECT 1 FROM identity_action_secrets
			      WHERE position(convert_to($1, 'UTF8') in secret_digest) > 0
			   )
			   OR EXISTS (
			     SELECT 1 FROM identity_security_events
			      WHERE position($1 in evidence::text) > 0
			   )
			   OR EXISTS (
			     SELECT 1 FROM outbox_events
			      WHERE position($1 in safe_payload::text) > 0
			   )
			   OR EXISTS (
			     SELECT 1 FROM outbox_protected_payloads
			      WHERE position(convert_to($1, 'UTF8') in ciphertext) > 0
			   )`,
			canary,
		).Scan(&leaked); err != nil {
			t.Fatalf("checking admission canary %q: %v", canary, err)
		}
		if leaked {
			t.Errorf("admission canary reached a forbidden storage surface: %q", canary)
		}
	}
}

// BR-001/002/105/120: registration creates exactly one pending Student and
// co-commits credential, exact policy evidence, digest-only secret, security
// event, and protected outbox intent without issuing a session.
func TestRegisterStudentCommitsCompletePendingIdentity(t *testing.T) {
	pool := admissionPool(t)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	service := admissionService(t, pool, now, 0x41)

	challenge := mustRegister(t, service, studentRegistration())
	// The mask is lower-cased: two spellings of one address must mask
	// identically, or the response echoes the caller's own casing back as a
	// feature that distinguishes a registered address from a new one.
	if challenge.ChallengeID == "" || challenge.MaskedEmail != "st***@e***.com" {
		t.Fatalf("registration challenge = %+v", challenge)
	}
	if !challenge.ExpiresAt.Equal(now.Add(10*time.Minute)) ||
		!challenge.ResendAvailableAt.Equal(now.Add(EmailOTPResendCooldown)) {
		t.Fatalf("challenge timing = expires %v resend %v", challenge.ExpiresAt, challenge.ResendAvailableAt)
	}

	var role, status, email, normalized, credentialState string
	var verifiedAt *time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT a.role, a.status, a.email, a.normalized_email, a.email_verified_at, c.state
		   FROM accounts a
		   JOIN password_credentials c ON c.account_id = a.id`,
	).Scan(&role, &status, &email, &normalized, &verifiedAt, &credentialState); err != nil {
		t.Fatalf("reading registered identity: %v", err)
	}
	if role != "STUDENT" || status != "PENDING_VERIFICATION" ||
		email != "Student.Name@Example.com" || normalized != "student.name@example.com" ||
		verifiedAt != nil || credentialState != "ACTIVE" {
		t.Fatalf("registered identity has wrong state: %q %q %q %q %v %q",
			role, status, email, normalized, verifiedAt, credentialState)
	}

	var policies, secrets, securityEvents, outboxEvents, payloads, sessions int
	if err := pool.QueryRow(context.Background(),
		`SELECT
		   (SELECT count(*) FROM policy_acceptances),
		   (SELECT count(*) FROM identity_action_secrets),
		   (SELECT count(*) FROM identity_security_events),
		   (SELECT count(*) FROM outbox_events),
		   (SELECT count(*) FROM outbox_protected_payloads),
		   (SELECT count(*) FROM sessions)`,
	).Scan(&policies, &secrets, &securityEvents, &outboxEvents, &payloads, &sessions); err != nil {
		t.Fatalf("counting registration facts: %v", err)
	}
	if policies != 2 || secrets != 1 || securityEvents != 1 ||
		outboxEvents != 1 || payloads != 1 || sessions != 0 {
		t.Fatalf("registration facts = policies %d secrets %d security %d outbox %d payloads %d sessions %d",
			policies, secrets, securityEvents, outboxEvents, payloads, sessions)
	}
	var outboxLocale, templateContract string
	if err := pool.QueryRow(context.Background(),
		`SELECT safe_payload->>'locale', safe_payload->>'template_contract'
		   FROM outbox_events`,
	).Scan(&outboxLocale, &templateContract); err != nil {
		t.Fatalf("reading verification outbox contract: %v", err)
	}
	if outboxLocale != "ar" || templateContract != verificationCodeTemplateContract {
		t.Fatalf("outbox contract = locale %q/template %q", outboxLocale, templateContract)
	}
	// The safe payload names the challenge and the expiry, and never the code.
	var safePayload string
	if err := pool.QueryRow(context.Background(),
		`SELECT safe_payload::text FROM outbox_events`).Scan(&safePayload); err != nil {
		t.Fatalf("reading verification safe payload: %v", err)
	}
	if !strings.Contains(safePayload, challenge.ChallengeID) {
		t.Fatalf("safe payload does not name the challenge: %s", safePayload)
	}
	if strings.Contains(safePayload, deterministicCode(0x41, 0)) {
		t.Fatal("the verification code reached the clear outbox payload")
	}
	var acceptedSet, acceptedLocale, acceptedVersions string
	if err := pool.QueryRow(context.Background(),
		`SELECT min(policy_set_id), min(locale),
		        string_agg(policy_kind || ':' || policy_version, ',' ORDER BY policy_kind)
		   FROM policy_acceptances`,
	).Scan(&acceptedSet, &acceptedLocale, &acceptedVersions); err != nil {
		t.Fatalf("reading exact policy evidence: %v", err)
	}
	if acceptedSet != "registration-v1" || acceptedLocale != "ar" ||
		acceptedVersions != "PRIVACY_NOTICE:privacy-v1,TERMS_OF_SERVICE:terms-v1" {
		t.Fatalf("policy evidence = %q %q %q", acceptedSet, acceptedLocale, acceptedVersions)
	}

	// The stored digest must be the keyed HMAC, never the code and never an
	// unkeyed hash of it. Both are checked: a bare SHA-256 would be an offline
	// dictionary over one million entries.
	var storedDigest []byte
	var storedPurpose string
	if err := pool.QueryRow(context.Background(),
		`SELECT secret_digest, purpose FROM identity_action_secrets`,
	).Scan(&storedDigest, &storedPurpose); err != nil {
		t.Fatalf("reading stored verification challenge: %v", err)
	}
	if storedPurpose != "EMAIL_VERIFICATION_OTP" {
		t.Fatalf("stored purpose = %q", storedPurpose)
	}
	code := deterministicCode(0x41, 0)
	if bytes.Contains(storedDigest, []byte(code)) {
		t.Fatal("the plaintext code was stored in the digest column")
	}
	plainHash := sha256.Sum256([]byte(code))
	if bytes.Equal(storedDigest, plainHash[:]) {
		t.Fatal("the stored digest is an unkeyed hash of the code")
	}
	pepper, err := NewEmailOTPPepper(config.NewSecret(strings.Repeat("p", 32)))
	if err != nil {
		t.Fatal(err)
	}
	if !pepper.MatchesEmailOTP(challenge.ChallengeID, code, storedDigest) {
		t.Fatal("the stored digest does not verify the issued code")
	}
	assertAdmissionCanariesAbsent(
		t,
		pool,
		"correct horse battery staple 9",
		"Student.Name@Example.com",
		code,
	)
}

func TestApprovedPolicyAcceptancesPersistExactVersionsAcrossResolverChanges(t *testing.T) {
	pool := admissionPool(t)
	approved, err := NewApprovedPolicySetResolver("https://gradex.example", ApprovedPolicySetID)
	if err != nil {
		t.Fatalf("constructing approved resolver: %v", err)
	}
	firstService := admissionServiceWithResolver(
		t, pool, time.Now().UTC(), 0x61, clearCompromisedSource(), approved,
	)
	first := studentRegistration()
	first.Email = "approved-policy@example.com"
	first.Locale = LocaleEnglish
	first.PolicySetID = ApprovedPolicySetID
	first.RequestID = "approved-policy-registration"
	if _, err := firstService.RegisterStudent(context.Background(), first); err != nil {
		t.Fatalf("registering under approved policy: %v", err)
	}

	futureVersion := "2026-09-01-v2"
	futureID := "gradex-legal-2026-09-01-v2"
	futurePolicy := func(locale Locale, prefix string) RegistrationPolicySet {
		return RegistrationPolicySet{
			ID: futureID, Version: futureVersion, EffectiveDate: "2026-09-01",
			MinimumAge: 18, PrimaryLocale: LocaleArabic, Locale: locale,
			Policies: []RegistrationPolicy{
				{Kind: PolicyPrivacyNotice, Version: futureVersion, Label: "Privacy", URL: prefix + "/privacy"},
				{Kind: PolicyTermsOfService, Version: futureVersion, Label: "Terms", URL: prefix + "/terms"},
			},
		}
	}
	future, err := NewStaticPolicySetResolver(
		futurePolicy(LocaleEnglish, "/en"),
		futurePolicy(LocaleArabic, "/ar"),
	)
	if err != nil {
		t.Fatalf("constructing future policy resolver: %v", err)
	}
	secondService := admissionServiceWithResolver(
		t, pool, time.Now().UTC(), 0x71, clearCompromisedSource(), future,
	)
	second := studentRegistration()
	second.Email = "future-policy@example.com"
	second.PolicySetID = futureID
	second.RequestID = "future-policy-registration"
	if _, err := secondService.RegisterStudent(context.Background(), second); err != nil {
		t.Fatalf("registering under future policy: %v", err)
	}

	rows, err := pool.Query(context.Background(), `
		SELECT a.normalized_email, min(p.policy_set_id),
		       string_agg(p.policy_version, ',' ORDER BY p.policy_kind)
		FROM policy_acceptances p
		JOIN accounts a ON a.id = p.account_id
		GROUP BY a.normalized_email
		ORDER BY a.normalized_email`)
	if err != nil {
		t.Fatalf("querying historical acceptances: %v", err)
	}
	defer rows.Close()
	got := map[string][2]string{}
	for rows.Next() {
		var email, setID, versions string
		if err := rows.Scan(&email, &setID, &versions); err != nil {
			t.Fatalf("scanning historical acceptance: %v", err)
		}
		got[email] = [2]string{setID, versions}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading historical acceptances: %v", err)
	}
	if got["approved-policy@example.com"] != [2]string{ApprovedPolicySetID, ApprovedPolicySetVersion + "," + ApprovedPolicySetVersion} {
		t.Fatalf("approved acceptance was rewritten: %v", got["approved-policy@example.com"])
	}
	if got["future-policy@example.com"] != [2]string{futureID, futureVersion + "," + futureVersion} {
		t.Fatalf("future acceptance mismatch: %v", got["future-policy@example.com"])
	}
}

// BR-001: an existing normalized email is a complete hidden no-op.
func TestRegisterExistingEmailCreatesNoAdditionalFacts(t *testing.T) {
	pool := admissionPool(t)
	service := admissionService(t, pool, time.Now().UTC(), 0x42)
	first := studentRegistration()
	firstChallenge := mustRegister(t, service, first)
	second := first
	second.Email = "student.name@example.com"
	second.DisplayName = "Different Person"
	second.RequestID = "request-registration-2"
	secondChallenge := mustRegister(t, service, second)

	// The duplicate is answered with a challenge of the same shape so the two
	// outcomes are indistinguishable to the caller — and with a *different*
	// identifier that names nothing, so holding it grants no access to the
	// existing Account's live challenge.
	if secondChallenge.ChallengeID == "" || secondChallenge.ChallengeID == firstChallenge.ChallengeID {
		t.Fatalf("duplicate challenge = %+v, first = %+v", secondChallenge, firstChallenge)
	}
	if _, err := service.VerifyEmailOTP(
		context.Background(), secondChallenge.ChallengeID, deterministicCode(0x42, 1), "request-synthetic",
	); !errors.Is(err, ErrOTPInvalid) {
		t.Fatal("the synthetic duplicate challenge was usable")
	}

	var accounts, credentials, acceptances, secrets, events, outboxRows int
	if err := pool.QueryRow(context.Background(),
		`SELECT
		   (SELECT count(*) FROM accounts),
		   (SELECT count(*) FROM password_credentials),
		   (SELECT count(*) FROM policy_acceptances),
		   (SELECT count(*) FROM identity_action_secrets),
		   (SELECT count(*) FROM identity_security_events),
		   (SELECT count(*) FROM outbox_events)`,
	).Scan(&accounts, &credentials, &acceptances, &secrets, &events, &outboxRows); err != nil {
		t.Fatalf("counting duplicate outcome: %v", err)
	}
	if accounts != 1 || credentials != 1 || acceptances != 2 ||
		secrets != 1 || events != 1 || outboxRows != 1 {
		t.Fatalf("duplicate mutated facts: %d %d %d %d %d %d",
			accounts, credentials, acceptances, secrets, events, outboxRows)
	}
	assertAdmissionCanariesAbsent(t, pool, "Different Person")
}

func TestRegistrationFarthestWriteFailureRollsBackEverything(t *testing.T) {
	pool := admissionPool(t)
	if _, err := pool.Exec(context.Background(), `
		CREATE FUNCTION reject_admission_payload() RETURNS TRIGGER AS $$
		BEGIN
		  RAISE EXCEPTION 'forced final-write failure';
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER reject_admission_payload
		  BEFORE INSERT ON outbox_protected_payloads
		  FOR EACH ROW EXECUTE FUNCTION reject_admission_payload();
	`); err != nil {
		t.Fatalf("installing final-write tripwire: %v", err)
	}
	service := admissionService(t, pool, time.Now().UTC(), 0x43)
	if _, err := service.RegisterStudent(context.Background(), studentRegistration()); err == nil {
		t.Fatal("registration succeeded despite final outbox-payload failure")
	}

	var facts int
	if err := pool.QueryRow(context.Background(),
		`SELECT
		   (SELECT count(*) FROM accounts)
		 + (SELECT count(*) FROM password_credentials)
		 + (SELECT count(*) FROM policy_acceptances)
		 + (SELECT count(*) FROM identity_action_secrets)
		 + (SELECT count(*) FROM identity_security_events)
		 + (SELECT count(*) FROM outbox_events)
		 + (SELECT count(*) FROM outbox_protected_payloads)`,
	).Scan(&facts); err != nil {
		t.Fatalf("counting rollback facts: %v", err)
	}
	if facts != 0 {
		t.Fatalf("failed registration left %d durable facts", facts)
	}
	assertAdmissionCanariesAbsent(
		t, pool, "correct horse battery staple 9", deterministicCode(0x43, 0),
	)
}

// BR-008: a resend supersedes the prior live code, and a proven code is
// single-use.
func TestVerificationResendSupersedesAndConsumptionIsSingleUse(t *testing.T) {
	pool := admissionPool(t)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	service := admissionService(t, pool, now, 0x44)
	challenge := mustRegister(t, service, studentRegistration())
	original := deterministicCode(0x44, 0)

	// The cooldown is real, so the resend has to happen on a later clock. A
	// second service on the same pool is how every other timing test in this
	// package moves the clock forward.
	later := admissionService(t, pool, now.Add(2*time.Minute), 0x45)
	replacement, err := later.ResendEmailVerificationOTP(
		context.Background(), challenge.ChallengeID, "request-resend-1",
	)
	if err != nil {
		t.Fatalf("requesting replacement: %v", err)
	}
	replacementCode := deterministicCode(0x45, 0)
	if original == replacementCode {
		t.Fatal("test fixture did not advance verification-code randomness")
	}
	if replacement.ChallengeID == challenge.ChallengeID {
		t.Fatal("resend reused the superseded challenge identifier")
	}

	if _, err := later.VerifyEmailOTP(
		context.Background(), challenge.ChallengeID, original, "request-consume-old",
	); !errors.Is(err, ErrOTPInvalid) {
		t.Fatalf("superseded code error = %v, want ErrOTPInvalid", err)
	}
	grant, err := later.VerifyEmailOTP(
		context.Background(), replacement.ChallengeID, replacementCode, "request-consume-new",
	)
	if err != nil {
		t.Fatalf("consuming replacement: %v", err)
	}
	if grant.Session.AccountID == "" || grant.Credential.IsEmpty() || grant.CSRFToken.IsEmpty() {
		t.Fatal("verification returned an incomplete session grant")
	}
	if _, err := later.VerifyEmailOTP(
		context.Background(), replacement.ChallengeID, replacementCode, "request-replay",
	); !errors.Is(err, ErrOTPInvalid) {
		t.Fatalf("replay error = %v, want ErrOTPInvalid", err)
	}

	var status string
	var revision int
	var verifiedAt *time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT status, revision, email_verified_at FROM accounts`,
	).Scan(&status, &revision, &verifiedAt); err != nil {
		t.Fatalf("reading verified Account: %v", err)
	}
	if status != "ACTIVE" || revision != 2 || verifiedAt == nil {
		t.Fatalf("verified state = %q revision %d at %v", status, revision, verifiedAt)
	}
	assertAdmissionCanariesAbsent(t, pool, original, replacementCode)
}

// TestSuccessfulVerificationCreatesOneOrdinarySession is the A4 contract: the
// Student who proves a code is authenticated, using the same session machinery
// as an ordinary sign-in and no weaker variant of it.
func TestSuccessfulVerificationCreatesOneOrdinarySession(t *testing.T) {
	pool := admissionPool(t)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	service := admissionService(t, pool, now, 0x44)
	challenge := mustRegister(t, service, studentRegistration())

	grant, err := service.VerifyEmailOTP(
		context.Background(), challenge.ChallengeID, deterministicCode(0x44, 0), "request-verify",
	)
	if err != nil {
		t.Fatalf("verifying: %v", err)
	}

	var families, generations int
	var sessionAccount string
	var generation int
	var sessionState string
	if err := pool.QueryRow(context.Background(),
		`SELECT (SELECT count(*) FROM sessions),
		        (SELECT count(*) FROM session_credentials),
		        (SELECT account_id::text FROM sessions),
		        (SELECT max(generation) FROM session_credentials),
		        (SELECT state::text FROM sessions)`,
	).Scan(&families, &generations, &sessionAccount, &generation, &sessionState); err != nil {
		t.Fatalf("reading created session: %v", err)
	}
	if families != 1 || generations != 1 || generation != 1 || sessionState != "ACTIVE" {
		t.Fatalf("session state = families %d generations %d generation %d state %q",
			families, generations, generation, sessionState)
	}
	if sessionAccount != grant.Session.AccountID {
		t.Fatalf("session belongs to %q, grant says %q", sessionAccount, grant.Session.AccountID)
	}
	if grant.Session.Role != RoleStudent {
		t.Fatalf("granted role = %q, want STUDENT", grant.Session.Role)
	}

	// The session credential and CSRF token must not be recoverable from the
	// database, exactly as after a password login.
	assertAdmissionCanariesAbsent(t, pool, grant.Credential.Expose(), grant.CSRFToken.Expose())
}

// TestVerificationRefusesToAuthenticateBeforeTheCodeIsProven closes the
// ordering hazard: no session may exist for an Account still awaiting
// verification.
func TestVerificationRefusesToAuthenticateBeforeTheCodeIsProven(t *testing.T) {
	pool := admissionPool(t)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	service := admissionService(t, pool, now, 0x44)
	challenge := mustRegister(t, service, studentRegistration())

	if _, err := service.VerifyEmailOTP(
		context.Background(), challenge.ChallengeID, "000000", "request-wrong",
	); !errors.Is(err, ErrOTPInvalid) {
		t.Fatalf("wrong code error = %v, want ErrOTPInvalid", err)
	}
	var sessions int
	var status string
	if err := pool.QueryRow(context.Background(),
		`SELECT (SELECT count(*) FROM sessions), (SELECT status::text FROM accounts)`,
	).Scan(&sessions, &status); err != nil {
		t.Fatalf("reading post-failure state: %v", err)
	}
	if sessions != 0 || status != "PENDING_VERIFICATION" {
		t.Fatalf("a failed code left sessions %d, status %q", sessions, status)
	}
}

// TestVerificationAttemptBudgetIsEnforcedAndRecorded proves the compensating
// control that makes a six-digit space survivable: online guessing is metered,
// the meter persists across requests, and exhausting it retires the challenge
// rather than leaving it available for the next guess.
func TestVerificationAttemptBudgetIsEnforcedAndRecorded(t *testing.T) {
	pool := admissionPool(t)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	service := admissionService(t, pool, now, 0x44)
	challenge := mustRegister(t, service, studentRegistration())
	correct := deterministicCode(0x44, 0)
	wrong := "000000"
	if wrong == correct {
		wrong = "111111"
	}

	for attempt := 1; attempt <= EmailOTPMaxAttempts; attempt++ {
		_, err := service.VerifyEmailOTP(
			context.Background(), challenge.ChallengeID, wrong,
			"request-guess-"+string(rune('a'+attempt)),
		)
		wantExhausted := attempt == EmailOTPMaxAttempts
		if wantExhausted && !errors.Is(err, ErrOTPAttemptsExhausted) {
			t.Fatalf("attempt %d error = %v, want ErrOTPAttemptsExhausted", attempt, err)
		}
		if !wantExhausted && !errors.Is(err, ErrOTPInvalid) {
			t.Fatalf("attempt %d error = %v, want ErrOTPInvalid", attempt, err)
		}
		var recorded int
		if err := pool.QueryRow(context.Background(),
			`SELECT attempt_count FROM identity_action_secrets WHERE id = $1::uuid`,
			challenge.ChallengeID,
		).Scan(&recorded); err != nil {
			t.Fatalf("reading attempt count: %v", err)
		}
		if recorded != attempt {
			t.Fatalf("after attempt %d the recorded count is %d", attempt, recorded)
		}
	}

	// Even the correct code cannot revive an exhausted challenge. The refusal
	// is the uniform one rather than a second "exhausted": the challenge has
	// been retired, so from here it is indistinguishable from a challenge that
	// never existed — which is what keeps a retired handle from confirming that
	// it once named a real pending Account.
	if _, err := service.VerifyEmailOTP(
		context.Background(), challenge.ChallengeID, correct, "request-late-correct",
	); !errors.Is(err, ErrOTPInvalid) {
		t.Fatalf("correct code after exhaustion = %v, want ErrOTPInvalid", err)
	}
	var live int
	var status string
	if err := pool.QueryRow(context.Background(),
		`SELECT (SELECT count(*) FROM identity_action_secrets
		          WHERE consumed_at IS NULL AND superseded_at IS NULL),
		        (SELECT status::text FROM accounts)`,
	).Scan(&live, &status); err != nil {
		t.Fatalf("reading exhausted challenge state: %v", err)
	}
	if live != 0 || status != "PENDING_VERIFICATION" {
		t.Fatalf("exhausted challenge left live %d, status %q", live, status)
	}
	var exhaustedEvents int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM identity_security_events
		  WHERE event_type = 'EMAIL_VERIFICATION_ATTEMPTS_EXHAUSTED'`,
	).Scan(&exhaustedEvents); err != nil {
		t.Fatalf("reading exhaustion evidence: %v", err)
	}
	if exhaustedEvents == 0 {
		t.Fatal("exhausting the attempt budget produced no security evidence")
	}
}

// TestExhaustedChallengeIsRecoverableWithANewCode proves the budget is a
// speed bump for an attacker rather than a lockout for the Student.
func TestExhaustedChallengeIsRecoverableWithANewCode(t *testing.T) {
	pool := admissionPool(t)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	service := admissionService(t, pool, now, 0x44)
	challenge := mustRegister(t, service, studentRegistration())
	for attempt := 0; attempt < EmailOTPMaxAttempts; attempt++ {
		_, _ = service.VerifyEmailOTP(
			context.Background(), challenge.ChallengeID, "000000",
			"request-burn-"+string(rune('a'+attempt)),
		)
	}

	// The challenge is retired, so a challenge-keyed resend can no longer act
	// on it; the address path is the documented recovery.
	recovery := admissionService(t, pool, now.Add(2*time.Minute), 0x47)
	replacement, err := recovery.RequestEmailVerification(context.Background(), VerificationRequest{
		Email: "student.name@example.com", RequestID: "request-recover",
	})
	if err != nil {
		t.Fatalf("recovering after exhaustion: %v", err)
	}
	grant, err := recovery.VerifyEmailOTP(
		context.Background(), replacement.ChallengeID, deterministicCode(0x47, 0), "request-recovered",
	)
	if err != nil {
		t.Fatalf("verifying the recovery code: %v", err)
	}
	if grant.Session.AccountID == "" {
		t.Fatal("recovery verification returned no session")
	}
}

// TestResendCooldownRefusesASecondCodeTooSoon bounds both mailbox flooding and
// the rate at which a guesser can trade a spent budget for a fresh one.
func TestResendCooldownRefusesASecondCodeTooSoon(t *testing.T) {
	pool := admissionPool(t)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	service := admissionService(t, pool, now, 0x44)
	challenge := mustRegister(t, service, studentRegistration())

	tooSoon := admissionService(t, pool, now.Add(EmailOTPResendCooldown-time.Second), 0x48)
	if _, err := tooSoon.ResendEmailVerificationOTP(
		context.Background(), challenge.ChallengeID, "request-too-soon",
	); !errors.Is(err, ErrOTPResendTooSoon) {
		t.Fatalf("early resend error = %v, want ErrOTPResendTooSoon", err)
	}
	// The refused resend must not have replaced anything.
	var secrets int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM identity_action_secrets`).Scan(&secrets); err != nil {
		t.Fatalf("counting secrets: %v", err)
	}
	if secrets != 1 {
		t.Fatalf("a refused resend created %d secrets", secrets-1)
	}

	inTime := admissionService(t, pool, now.Add(EmailOTPResendCooldown), 0x49)
	if _, err := inTime.ResendEmailVerificationOTP(
		context.Background(), challenge.ChallengeID, "request-in-time",
	); err != nil {
		t.Fatalf("resend after cooldown: %v", err)
	}
}

// TestExpiredVerificationCodeIsUniformlyInvalid keeps expiry indistinguishable
// from a wrong code.
func TestExpiredVerificationCodeIsUniformlyInvalid(t *testing.T) {
	pool := admissionPool(t)
	issuedAt := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	issuer := admissionService(t, pool, issuedAt, 0x60)
	challenge := mustRegister(t, issuer, studentRegistration())

	consumer := admissionService(t, pool, issuedAt.Add(11*time.Minute), 0x70)
	if _, err := consumer.VerifyEmailOTP(
		context.Background(), challenge.ChallengeID, deterministicCode(0x60, 0), "request-expired",
	); !errors.Is(err, ErrOTPInvalid) {
		t.Fatalf("expired code error = %v, want ErrOTPInvalid", err)
	}
}

// TestLegacyVerificationLinkStillActivatesDuringTheMigrationWindow is the A5
// contract. Gradex was live when the code flow shipped, so a link already
// delivered to a Student's mailbox has to keep working until it expires.
func TestLegacyVerificationLinkStillActivatesDuringTheMigrationWindow(t *testing.T) {
	pool := admissionPool(t)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	service := admissionService(t, pool, now, 0x44)
	mustRegister(t, service, studentRegistration())
	accountID := onlyAccountID(t, pool)

	bearer := deterministicBearer(0x41)
	insertLegacyVerificationLink(t, pool, accountID, bearer, now, time.Hour)

	if err := service.VerifyEmail(context.Background(), bearer, "request-legacy"); err != nil {
		t.Fatalf("consuming a legacy verification link: %v", err)
	}
	var status string
	var verifiedAt *time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT status::text, email_verified_at FROM accounts`,
	).Scan(&status, &verifiedAt); err != nil {
		t.Fatalf("reading legacy-verified Account: %v", err)
	}
	if status != "ACTIVE" || verifiedAt == nil {
		t.Fatalf("legacy link left the Account at %q", status)
	}
	// The legacy path deliberately does not authenticate. It never did, and
	// making it do so now would turn every unexpired mailbox link into a
	// session-minting credential.
	var sessions int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions`).Scan(&sessions); err != nil {
		t.Fatalf("counting sessions: %v", err)
	}
	if sessions != 0 {
		t.Fatalf("a legacy verification link created %d sessions", sessions)
	}
}

// TestVerifyingByCodeRetiresAnUnexpiredLegacyLink keeps the migration window
// from leaving a second live credential behind an Account that is already
// verified.
func TestVerifyingByCodeRetiresAnUnexpiredLegacyLink(t *testing.T) {
	pool := admissionPool(t)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	service := admissionService(t, pool, now, 0x44)
	challenge := mustRegister(t, service, studentRegistration())
	accountID := onlyAccountID(t, pool)
	bearer := deterministicBearer(0x41)
	insertLegacyVerificationLink(t, pool, accountID, bearer, now, time.Hour)

	if _, err := service.VerifyEmailOTP(
		context.Background(), challenge.ChallengeID, deterministicCode(0x44, 0), "request-code",
	); err != nil {
		t.Fatalf("verifying by code: %v", err)
	}
	var liveLinks int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM identity_action_secrets
		  WHERE purpose = 'EMAIL_VERIFICATION'
		    AND consumed_at IS NULL AND superseded_at IS NULL`,
	).Scan(&liveLinks); err != nil {
		t.Fatalf("counting live legacy links: %v", err)
	}
	if liveLinks != 0 {
		t.Fatal("a live legacy verification link survived a completed code verification")
	}
}

func TestConcurrentVerificationActivatesExactlyOnce(t *testing.T) {
	pool := admissionPool(t)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	service := admissionService(t, pool, now, 0x45)
	challenge := mustRegister(t, service, studentRegistration())
	code := deterministicCode(0x45, 0)

	const attempts = 6
	var wait sync.WaitGroup
	var mutex sync.Mutex
	successes := 0
	refused := 0
	for index := 0; index < attempts; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			_, err := service.VerifyEmailOTP(
				context.Background(), challenge.ChallengeID, code,
				"request-concurrent-"+string(rune('a'+index)),
			)
			mutex.Lock()
			defer mutex.Unlock()
			switch {
			case err == nil:
				successes++
			case errors.Is(err, ErrOTPInvalid), errors.Is(err, ErrOTPAttemptsExhausted):
				refused++
			default:
				t.Errorf("concurrent verification: %v", err)
			}
		}(index)
	}
	wait.Wait()
	if successes != 1 || refused != attempts-1 {
		t.Fatalf("concurrent outcomes = %d success/%d refused", successes, refused)
	}
	var sessions int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions`).Scan(&sessions); err != nil {
		t.Fatalf("counting sessions: %v", err)
	}
	if sessions != 1 {
		t.Fatalf("concurrent verification created %d sessions", sessions)
	}
}

func TestConcurrentVerificationRequestsLeaveOneLiveReplacement(t *testing.T) {
	pool := admissionPool(t)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	service := admissionService(t, pool, now, 0x50)
	mustRegister(t, service, studentRegistration())

	// Past the cooldown, so the concurrency under test is supersession rather
	// than the cooldown refusal.
	resender := admissionService(t, pool, now.Add(2*time.Minute), 0x52)
	const attempts = 4
	var wait sync.WaitGroup
	var mutex sync.Mutex
	accepted := 0
	tooSoon := 0
	for index := 0; index < attempts; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			_, err := resender.RequestEmailVerification(context.Background(), VerificationRequest{
				Email:     "student.name@example.com",
				RequestID: "request-resend-" + string(rune('a'+index)),
			})
			mutex.Lock()
			defer mutex.Unlock()
			switch {
			case err == nil:
				accepted++
			case errors.Is(err, ErrOTPResendTooSoon):
				tooSoon++
			default:
				t.Errorf("concurrent resend: %v", err)
			}
		}(index)
	}
	wait.Wait()
	if accepted == 0 {
		t.Fatal("no concurrent resend was accepted")
	}

	// Every request is accepted — this route cannot refuse on a cooldown only a
	// registered address can be inside, because that difference would be an
	// account-existence oracle — but at most one of a concurrent burst issues a
	// new challenge. The rest find the replacement already live inside its own
	// cooldown and return it unchanged.
	//
	// Whatever the interleaving, exactly one challenge is live: the
	// one-live-secret-per-purpose index is the invariant, and a second live row
	// would mean two codes could each activate the Account.
	var total, live, superseded int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*),
		        count(*) FILTER (WHERE consumed_at IS NULL AND superseded_at IS NULL),
		        count(*) FILTER (WHERE superseded_at IS NOT NULL)
		   FROM identity_action_secrets`,
	).Scan(&total, &live, &superseded); err != nil {
		t.Fatalf("counting concurrent replacements: %v", err)
	}
	if live != 1 || total != superseded+1 || total > accepted+1 {
		t.Fatalf("replacement lifecycle = total %d/live %d/superseded %d after %d accepted",
			total, live, superseded, accepted)
	}
	if superseded == 0 {
		t.Fatal("no concurrent resend past the cooldown replaced the registration challenge")
	}
}

// TestWrongPurposeActionSecretIsRejectedAtPersistenceBoundary asserts that the
// action-secret purpose allowlist is closed at the database, not merely
// respected by application code.
//
// The example purpose changed in S1B3. This test previously used
// 'PASSWORD_RESET' as its out-of-allowlist value; migration 0007 admits that
// purpose for password recovery, so continuing to use it would have asserted
// the opposite of the intended property. The guarded property is unchanged —
// only the example needed to move to a purpose the schema still refuses.
func TestWrongPurposeActionSecretIsRejectedAtPersistenceBoundary(t *testing.T) {
	pool := admissionPool(t)
	service := admissionService(t, pool, time.Now().UTC(), 0x65)
	mustRegister(t, service, studentRegistration())
	accountID := onlyAccountID(t, pool)
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO identity_action_secrets
		   (account_id, purpose, secret_digest, issued_at, expires_at)
		 VALUES ($1::uuid, 'ACCOUNT_DELETION', decode(repeat('99', 32), 'hex'), now(), now() + interval '1 hour')`,
		accountID,
	); err == nil {
		t.Fatal("persistence accepted an action secret outside the closed purpose allowlist")
	}
}

// TestUnknownVerificationRequestAndInvalidCodeMutateNothing keeps the two
// public entry points from becoming an account-existence oracle.
func TestUnknownVerificationRequestAndInvalidCodeMutateNothing(t *testing.T) {
	pool := admissionPool(t)
	service := admissionService(t, pool, time.Now().UTC(), 0x46)
	challenge, err := service.RequestEmailVerification(context.Background(), VerificationRequest{
		Email: "unknown@example.com", RequestID: "request-unknown",
	})
	if err != nil {
		t.Fatalf("unknown verification request: %v", err)
	}
	// An unknown address still receives a challenge, and it is unusable.
	if challenge.ChallengeID == "" {
		t.Fatal("an unknown address produced no challenge, which distinguishes it")
	}
	if _, err := service.VerifyEmailOTP(
		context.Background(), challenge.ChallengeID, "123456", "request-invalid",
	); !errors.Is(err, ErrOTPInvalid) {
		t.Fatalf("synthetic challenge error = %v, want ErrOTPInvalid", err)
	}

	var facts int
	if err := pool.QueryRow(context.Background(),
		`SELECT
		   (SELECT count(*) FROM identity_action_secrets)
		 + (SELECT count(*) FROM identity_security_events)
		 + (SELECT count(*) FROM outbox_events)
		 + (SELECT count(*) FROM sessions)`,
	).Scan(&facts); err != nil {
		t.Fatalf("counting hidden facts: %v", err)
	}
	if facts != 0 {
		t.Fatalf("hidden outcomes created %d facts", facts)
	}
	assertAdmissionCanariesAbsent(t, pool, "unknown@example.com")
}
