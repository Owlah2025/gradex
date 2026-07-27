//go:build integration

package identity

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

const sessionTestPassword = "correct session login passphrase 9"

func sessionTestSettings(t *testing.T) config.SessionSettings {
	t.Helper()
	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV": "development", "REDIS_ADDR": "localhost:6379",
		"S3_ENDPOINT": "http://localhost:9000", "S3_BUCKET": "gradex-test",
	}), config.MapSecretResolver{
		"DATABASE_URL": "postgres://x", "S3_ACCESS_KEY": "a",
		"S3_SECRET_KEY": "b", "PLAYBACK_TOKEN_SECRET": "c",
	})
	if err != nil {
		t.Fatalf("loading session settings: %v", err)
	}
	return cfg.Sessions()
}

func sessionRepository(
	t *testing.T,
	pool *pgxpool.Pool,
	now time.Time,
) *SessionRepository {
	t.Helper()
	repository, err := NewSessionRepository(SessionRepositoryOptions{
		Pool: pool, Settings: sessionTestSettings(t),
		CSRFKey: bytes.Repeat([]byte{0x61}, 32),
		Now:     func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("constructing session repository: %v", err)
	}
	return repository
}

func insertSessionAccount(
	t *testing.T,
	pool *pgxpool.Pool,
	email string,
	status AccountStatus,
	verified bool,
) string {
	t.Helper()
	passwordHash, err := HashPassword(sessionTestPassword)
	if err != nil {
		t.Fatalf("hashing fixture password: %v", err)
	}
	var verifiedAt *time.Time
	if verified {
		now := time.Now().UTC()
		verifiedAt = &now
	}
	var accountID string
	err = pool.QueryRow(context.Background(),
		`WITH account AS (
		   INSERT INTO accounts
		     (normalized_email, email, role, status, display_name, email_verified_at)
		   VALUES ($1, $1, 'STUDENT', $2, 'Session Student', $3)
		   RETURNING id
		 )
		 INSERT INTO password_credentials (account_id, password_hash, state)
		 SELECT id, $4, 'ACTIVE' FROM account
		 RETURNING account_id::text`,
		email, status, verifiedAt, passwordHash.Expose(),
	).Scan(&accountID)
	if err != nil {
		t.Fatalf("creating session Account: %v", err)
	}
	return accountID
}

func loginSession(
	t *testing.T,
	repository *SessionRepository,
	email string,
) SessionGrant {
	t.Helper()
	grant, err := repository.Login(context.Background(), LoginRequest{
		Email: email, Password: config.NewSecret(sessionTestPassword),
		RequestID: "request-login",
	})
	if err != nil {
		t.Fatalf("logging in: %v", err)
	}
	return grant
}

func mutationFor(grant SessionGrant, requestID string) SessionMutation {
	return SessionMutation{
		CredentialDigest: DigestToken(grant.Credential.Expose()),
		CSRFDigest:       DigestToken(grant.CSRFToken.Expose()),
		RequestID:        requestID,
	}
}

func TestLoginCreatesDigestOnlyFamily(t *testing.T) {
	pool := admissionPool(t)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	repository := sessionRepository(t, pool, now)
	insertSessionAccount(t, pool, "active@example.com", StatusActive, true)

	grant := loginSession(t, repository, "active@example.com")
	if grant.Session.Role != RoleStudent ||
		!grant.Session.IdleExpiresAt.Equal(now.Add(7*24*time.Hour)) ||
		!grant.Session.AbsoluteExpiresAt.Equal(now.Add(30*24*time.Hour)) {
		t.Fatalf("Student session profile is wrong: %#v", grant.Session)
	}

	var credentialDigest, csrfDigest string
	if err := pool.QueryRow(context.Background(),
		`SELECT credential_digest, csrf_digest FROM session_credentials`,
	).Scan(&credentialDigest, &csrfDigest); err != nil {
		t.Fatalf("reading stored generation: %v", err)
	}
	if credentialDigest == grant.Credential.Expose() ||
		csrfDigest == grant.CSRFToken.Expose() {
		t.Fatal("session or CSRF plaintext was stored")
	}

	view, err := repository.Resolve(
		context.Background(), credentialDigest, UseReadOnly, "request-resolve",
	)
	if err != nil {
		t.Fatalf("resolving current session: %v", err)
	}
	if view.CSRFToken.Expose() != grant.CSRFToken.Expose() {
		t.Fatal("session read did not rehydrate the memory-only CSRF token")
	}

	var families, events int
	if err := pool.QueryRow(context.Background(),
		`SELECT (SELECT count(*) FROM sessions),
		        (SELECT count(*) FROM identity_security_events
		          WHERE event_type = 'SESSION_CREATED')`,
	).Scan(&families, &events); err != nil {
		t.Fatalf("counting login facts: %v", err)
	}
	if families != 1 || events != 1 {
		t.Fatalf("login facts = %d families/%d events, want 1/1", families, events)
	}
}

func TestHiddenLoginFailuresCreateNoFamily(t *testing.T) {
	pool := admissionPool(t)
	repository := sessionRepository(
		t, pool, time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC),
	)
	insertSessionAccount(t, pool, "active@example.com", StatusActive, true)
	insertSessionAccount(t, pool, "unverified@example.com", StatusPendingVerification, false)
	insertSessionAccount(t, pool, "suspended@example.com", StatusSuspended, true)

	failures := map[string]LoginRequest{
		"unknown email": {
			Email: "unknown@example.com", Password: config.NewSecret(sessionTestPassword),
		},
		"wrong password": {
			Email: "active@example.com", Password: config.NewSecret("wrong login passphrase 9"),
		},
		"unverified Account": {
			Email: "unverified@example.com", Password: config.NewSecret(sessionTestPassword),
		},
		"suspended Account": {
			Email: "suspended@example.com", Password: config.NewSecret(sessionTestPassword),
		},
	}
	for name, request := range failures {
		t.Run(name, func(t *testing.T) {
			request.RequestID = "request-hidden"
			if _, err := repository.Login(context.Background(), request); !errors.Is(
				err, ErrAuthenticationFailed,
			) {
				t.Errorf("failure = %v, want ErrAuthenticationFailed", err)
			}
		})
	}

	var families int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions`).Scan(
		&families,
	); err != nil {
		t.Fatalf("counting hidden-failure families: %v", err)
	}
	if families != 0 {
		t.Fatalf("hidden failures created %d session families", families)
	}
}

func TestRenewalRotatesBothSecretsAndStaleUseRevokesFamily(t *testing.T) {
	pool := admissionPool(t)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	repository := sessionRepository(t, pool, now)
	insertSessionAccount(t, pool, "renew@example.com", StatusActive, true)
	original := loginSession(t, repository, "renew@example.com")

	replacement, err := repository.Renew(
		context.Background(), mutationFor(original, "request-renew"),
	)
	if err != nil {
		t.Fatalf("renewing: %v", err)
	}
	if replacement.Credential.Expose() == original.Credential.Expose() ||
		replacement.CSRFToken.Expose() == original.CSRFToken.Expose() {
		t.Fatal("renewal did not rotate both session and CSRF values")
	}

	_, err = repository.Resolve(
		context.Background(),
		DigestToken(original.Credential.Expose()),
		UseReadOnly,
		"request-stale-first",
	)
	if !errors.Is(err, ErrSessionReplaced) {
		t.Fatalf("first immediate stale read = %v, want ErrSessionReplaced", err)
	}
	_, err = repository.Resolve(
		context.Background(),
		DigestToken(original.Credential.Expose()),
		UseReadOnly,
		"request-stale-repeat",
	)
	if !errors.Is(err, ErrSessionReuseDetected) {
		t.Fatalf("repeated stale read = %v, want ErrSessionReuseDetected", err)
	}
	_, err = repository.Resolve(
		context.Background(),
		DigestToken(replacement.Credential.Expose()),
		UseReadOnly,
		"request-revoked-winner",
	)
	if !errors.Is(err, ErrAuthenticationRequired) {
		t.Fatalf("replacement survived family revocation: %v", err)
	}

	var state, reason string
	if err := pool.QueryRow(context.Background(),
		`SELECT state::text, revocation_reason::text FROM sessions`,
	).Scan(&state, &reason); err != nil {
		t.Fatalf("reading revoked family: %v", err)
	}
	if state != "REVOKED" || reason != "REUSE_DETECTED" {
		t.Fatalf("family = %s/%s, want REVOKED/REUSE_DETECTED", state, reason)
	}
}

func TestConcurrentRenewalHasOneCredentialWinner(t *testing.T) {
	pool := admissionPool(t)
	repository := sessionRepository(
		t, pool, time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC),
	)
	insertSessionAccount(t, pool, "race@example.com", StatusActive, true)
	original := loginSession(t, repository, "race@example.com")
	mutation := mutationFor(original, "request-renew-race")

	type outcome struct {
		grant SessionGrant
		err   error
	}
	start := make(chan struct{})
	outcomes := make(chan outcome, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for range 2 {
		go func() {
			ready.Done()
			<-start
			grant, err := repository.Renew(context.Background(), mutation)
			outcomes <- outcome{grant: grant, err: err}
		}()
	}
	ready.Wait()
	close(start)

	var successes, reuseDetections int
	for range 2 {
		result := <-outcomes
		switch {
		case result.err == nil && !result.grant.Credential.IsEmpty():
			successes++
		case errors.Is(result.err, ErrSessionReuseDetected):
			reuseDetections++
		default:
			t.Errorf("unexpected renewal outcome: %v", result.err)
		}
	}
	if successes != 1 || reuseDetections != 1 {
		t.Fatalf("renewal race = %d winners/%d reuse detections, want 1/1",
			successes, reuseDetections)
	}
	var state, reason string
	if err := pool.QueryRow(context.Background(),
		`SELECT state::text, revocation_reason::text FROM sessions`,
	).Scan(&state, &reason); err != nil {
		t.Fatalf("reading renewal-race family: %v", err)
	}
	if state != "REVOKED" || reason != "REUSE_DETECTED" {
		t.Fatalf("renewal-race family = %s/%s", state, reason)
	}
}

func TestMutationRecheckDeniesSuspendedAuthority(t *testing.T) {
	pool := admissionPool(t)
	repository := sessionRepository(
		t, pool, time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC),
	)
	accountID := insertSessionAccount(t, pool, "logout@example.com", StatusActive, true)
	grant := loginSession(t, repository, "logout@example.com")

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("beginning mutation transaction: %v", err)
	}
	if _, err := tx.Exec(context.Background(),
		`UPDATE accounts SET status = 'SUSPENDED' WHERE id = $1::uuid`,
		accountID,
	); err != nil {
		t.Fatalf("suspending Account: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("committing suspension: %v", err)
	}

	tx, err = pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("beginning recheck transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := repository.RecheckForMutation(
		context.Background(), tx, grant.Session,
		DigestToken(grant.CSRFToken.Expose()),
	); !errors.Is(err, ErrAuthenticationRequired) {
		t.Fatalf("suspended mutation recheck = %v, want ErrAuthenticationRequired", err)
	}
}

func TestLogoutRevokesBeforeSubsequentDenial(t *testing.T) {
	pool := admissionPool(t)
	repository := sessionRepository(
		t, pool, time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC),
	)
	insertSessionAccount(t, pool, "logout@example.com", StatusActive, true)
	grant := loginSession(t, repository, "logout@example.com")
	if err := repository.Logout(
		context.Background(), mutationFor(grant, "request-logout"),
	); err != nil {
		t.Fatalf("logging out: %v", err)
	}
	if _, err := repository.Resolve(
		context.Background(), DigestToken(grant.Credential.Expose()),
		UseReadOnly, "request-after-logout",
	); !errors.Is(err, ErrAuthenticationRequired) {
		t.Fatalf("post-logout resolution = %v, want ErrAuthenticationRequired", err)
	}
	var reason string
	var logoutEvents int
	if err := pool.QueryRow(context.Background(),
		`SELECT s.revocation_reason::text,
		        (SELECT count(*) FROM identity_security_events
		          WHERE event_type = 'SESSION_LOGGED_OUT')
		   FROM sessions s`,
	).Scan(&reason, &logoutEvents); err != nil {
		t.Fatalf("reading logout evidence: %v", err)
	}
	if reason != "LOGOUT" || logoutEvents != 1 {
		t.Fatalf("logout evidence = %s/%d events, want LOGOUT/1", reason, logoutEvents)
	}
}
