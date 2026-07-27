//go:build integration

package identity

import (
	"bytes"
	"context"
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
	writer, err := outbox.NewWriter("test-v1", bytes.Repeat([]byte{0x51}, 32))
	if err != nil {
		t.Fatalf("constructing outbox writer: %v", err)
	}
	randomness := make([]byte, 0, 32*8)
	for offset := byte(0); offset < 8; offset++ {
		randomness = append(randomness, bytes.Repeat([]byte{randomByte + offset}, 32)...)
	}
	service, err := NewAdmissionService(AdmissionServiceOptions{
		Pool:            pool,
		Policies:        testPolicyResolver(t),
		Compromised:     clearCompromisedSource(),
		Outbox:          writer,
		VerificationTTL: time.Hour,
		Now:             func() time.Time { return now },
		Random:          bytes.NewReader(randomness),
	})
	if err != nil {
		t.Fatalf("constructing admission service: %v", err)
	}
	return service
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

	if err := service.RegisterStudent(context.Background(), studentRegistration()); err != nil {
		t.Fatalf("registering Student: %v", err)
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
	if outboxLocale != "ar" || templateContract != verificationTemplateContract {
		t.Fatalf("outbox contract = locale %q/template %q", outboxLocale, templateContract)
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

	var leaked bool
	if err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (
		   SELECT 1
		     FROM identity_action_secrets
		    WHERE position(convert_to($1, 'UTF8') in secret_digest) > 0
		)`,
		deterministicBearer(0x41),
	).Scan(&leaked); err != nil {
		t.Fatalf("checking bearer leakage: %v", err)
	}
	if leaked {
		t.Fatal("raw action bearer was stored as secret digest")
	}
	assertAdmissionCanariesAbsent(
		t,
		pool,
		"correct horse battery staple 9",
		"Student.Name@Example.com",
		deterministicBearer(0x41),
	)
}

// BR-001: an existing normalized email is a complete hidden no-op.
func TestRegisterExistingEmailCreatesNoAdditionalFacts(t *testing.T) {
	pool := admissionPool(t)
	service := admissionService(t, pool, time.Now().UTC(), 0x42)
	first := studentRegistration()
	if err := service.RegisterStudent(context.Background(), first); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	second := first
	second.Email = "student.name@example.com"
	second.DisplayName = "Different Person"
	second.RequestID = "request-registration-2"
	if err := service.RegisterStudent(context.Background(), second); err != nil {
		t.Fatalf("hidden duplicate registration: %v", err)
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
	if err := service.RegisterStudent(context.Background(), studentRegistration()); err == nil {
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
		t, pool, "correct horse battery staple 9", deterministicBearer(0x43),
	)
}

// BR-008: resend supersedes the prior live bearer and only the replacement can
// activate the pending Account.
func TestVerificationResendSupersedesAndConsumptionIsSingleUse(t *testing.T) {
	pool := admissionPool(t)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	service := admissionService(t, pool, now, 0x44)
	if err := service.RegisterStudent(context.Background(), studentRegistration()); err != nil {
		t.Fatalf("registering: %v", err)
	}
	original := deterministicBearer(0x44)
	if err := service.RequestEmailVerification(context.Background(), VerificationRequest{
		Email: "student.name@example.com", RequestID: "request-resend-1",
	}); err != nil {
		t.Fatalf("requesting replacement: %v", err)
	}
	replacement := deterministicBearer(0x45)
	if original == replacement {
		t.Fatal("test fixture did not advance action-secret randomness")
	}
	if err := service.VerifyEmail(context.Background(), original, "request-consume-old"); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("superseded token error = %v, want ErrTokenInvalid", err)
	}
	if err := service.VerifyEmail(context.Background(), replacement, "request-consume-new"); err != nil {
		t.Fatalf("consuming replacement: %v", err)
	}
	if err := service.VerifyEmail(context.Background(), replacement, "request-replay"); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("replay error = %v, want ErrTokenInvalid", err)
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
	assertAdmissionCanariesAbsent(t, pool, original, replacement)
}

func TestConcurrentVerificationActivatesExactlyOnce(t *testing.T) {
	pool := admissionPool(t)
	service := admissionService(t, pool, time.Now().UTC(), 0x45)
	if err := service.RegisterStudent(context.Background(), studentRegistration()); err != nil {
		t.Fatalf("registering: %v", err)
	}
	bearer := deterministicBearer(0x45)

	const attempts = 6
	var wait sync.WaitGroup
	var mutex sync.Mutex
	successes := 0
	invalid := 0
	for index := 0; index < attempts; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			err := service.VerifyEmail(
				context.Background(), bearer, "request-concurrent-"+string(rune('a'+index)),
			)
			mutex.Lock()
			defer mutex.Unlock()
			switch {
			case err == nil:
				successes++
			case errors.Is(err, ErrTokenInvalid):
				invalid++
			default:
				t.Errorf("concurrent verification: %v", err)
			}
		}(index)
	}
	wait.Wait()
	if successes != 1 || invalid != attempts-1 {
		t.Fatalf("concurrent outcomes = %d success/%d invalid", successes, invalid)
	}
}

func TestConcurrentVerificationRequestsLeaveOneLiveReplacement(t *testing.T) {
	pool := admissionPool(t)
	service := admissionService(t, pool, time.Now().UTC(), 0x50)
	if err := service.RegisterStudent(context.Background(), studentRegistration()); err != nil {
		t.Fatalf("registering: %v", err)
	}

	const attempts = 4
	var wait sync.WaitGroup
	for index := 0; index < attempts; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			if err := service.RequestEmailVerification(context.Background(), VerificationRequest{
				Email:     "student.name@example.com",
				RequestID: "request-resend-" + string(rune('a'+index)),
			}); err != nil {
				t.Errorf("concurrent resend: %v", err)
			}
		}(index)
	}
	wait.Wait()

	var total, live, superseded int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*),
		        count(*) FILTER (WHERE consumed_at IS NULL AND superseded_at IS NULL),
		        count(*) FILTER (WHERE superseded_at IS NOT NULL)
		   FROM identity_action_secrets`,
	).Scan(&total, &live, &superseded); err != nil {
		t.Fatalf("counting concurrent replacements: %v", err)
	}
	if total != attempts+1 || live != 1 || superseded != attempts {
		t.Fatalf("replacement lifecycle = total %d/live %d/superseded %d", total, live, superseded)
	}
}

func TestExpiredVerificationBearerIsUniformlyInvalid(t *testing.T) {
	pool := admissionPool(t)
	issuedAt := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	issuer := admissionService(t, pool, issuedAt, 0x60)
	if err := issuer.RegisterStudent(context.Background(), studentRegistration()); err != nil {
		t.Fatalf("registering: %v", err)
	}
	consumer := admissionService(t, pool, issuedAt.Add(2*time.Hour), 0x70)
	if err := consumer.VerifyEmail(
		context.Background(), deterministicBearer(0x60), "request-expired",
	); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("expired bearer error = %v, want ErrTokenInvalid", err)
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
	if err := service.RegisterStudent(context.Background(), studentRegistration()); err != nil {
		t.Fatalf("registering: %v", err)
	}
	var accountID string
	if err := pool.QueryRow(context.Background(), `SELECT id::text FROM accounts`).Scan(&accountID); err != nil {
		t.Fatalf("reading Account: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO identity_action_secrets
		   (account_id, purpose, secret_digest, issued_at, expires_at)
		 VALUES ($1::uuid, 'ACCOUNT_DELETION', decode(repeat('99', 32), 'hex'), now(), now() + interval '1 hour')`,
		accountID,
	); err == nil {
		t.Fatal("persistence accepted an action secret outside the closed purpose allowlist")
	}
}

func TestUnknownVerificationRequestAndInvalidBearerMutateNothing(t *testing.T) {
	pool := admissionPool(t)
	service := admissionService(t, pool, time.Now().UTC(), 0x46)
	if err := service.RequestEmailVerification(context.Background(), VerificationRequest{
		Email: "unknown@example.com", RequestID: "request-unknown",
	}); err != nil {
		t.Fatalf("unknown verification request: %v", err)
	}
	if err := service.VerifyEmail(context.Background(), strings.Repeat("x", 43), "request-invalid"); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("invalid bearer error = %v, want ErrTokenInvalid", err)
	}
	var facts int
	if err := pool.QueryRow(context.Background(),
		`SELECT
		   (SELECT count(*) FROM identity_action_secrets)
		 + (SELECT count(*) FROM identity_security_events)
		 + (SELECT count(*) FROM outbox_events)`,
	).Scan(&facts); err != nil {
		t.Fatalf("counting hidden facts: %v", err)
	}
	if facts != 0 {
		t.Fatalf("hidden outcomes created %d facts", facts)
	}
	assertAdmissionCanariesAbsent(
		t, pool, "unknown@example.com", strings.Repeat("x", 43),
	)
}
