//go:build integration

package identity

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func createTestAdmin(t *testing.T, pool *pgxpool.Pool, email string) (string, Principal) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	var id string
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (normalized_email, email, role, status, display_name)
		 VALUES ($1, $1, 'ADMIN', 'ACTIVE', 'Test Admin')
		 RETURNING id::text`,
		email,
	).Scan(&id); err != nil {
		t.Fatalf("creating test admin account: %v", err)
	}

	hash, err := HashPassword("LaunchPassword123!")
	if err != nil {
		t.Fatalf("hashing password: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO password_credentials (account_id, password_hash, state)
		 VALUES ($1::uuid, $2, 'ACTIVE')`,
		id, hash.Expose(),
	); err != nil {
		t.Fatalf("creating test admin password credentials: %v", err)
	}

	p := Principal{
		AccountID:       id,
		Role:            RoleAdmin,
		Status:          StatusActive,
		CredentialState: CredentialActive,
	}
	return id, p
}

func createTestSession(t *testing.T, pool *pgxpool.Pool, accountID string, now time.Time) (Session, IssuedCredential) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	idleExpiresAt := now.Add(30 * time.Minute)
	absoluteExpiresAt := now.Add(12 * time.Hour)

	var sessionID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO sessions
		   (account_id, admitted_epoch, authenticated_at, last_activity_at, idle_expires_at, absolute_expires_at)
		 VALUES ($1::uuid, 1, $2, $2, $3, $4)
		 RETURNING id::text`,
		accountID, now, idleExpiresAt, absoluteExpiresAt,
	).Scan(&sessionID); err != nil {
		t.Fatalf("inserting test session: %v", err)
	}

	cred, err := NewSessionCredentialForGeneration(bytes.Repeat([]byte{0x61}, 32), sessionID, 1)
	if err != nil {
		t.Fatalf("minting test session credential: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO session_credentials
		   (session_id, generation, credential_digest, csrf_digest)
		 VALUES ($1::uuid, 1, $2, $3)`,
		sessionID, cred.CredentialDigest, cred.CSRFDigest,
	); err != nil {
		t.Fatalf("inserting test session credential: %v", err)
	}

	sess := Session{
		ID:                sessionID,
		AccountID:         accountID,
		AdmittedEpoch:     1,
		State:             SessionActive,
		CurrentGeneration: 1,
		AuthenticatedAt:   now,
		LastActivityAt:    now,
		IdleExpiresAt:     idleExpiresAt,
		AbsoluteExpiresAt: absoluteExpiresAt,
	}

	return sess, cred
}

func testSessionRepository(t *testing.T, pool *pgxpool.Pool, now time.Time) *SessionRepository {
	t.Helper()
	repo, err := NewSessionRepository(SessionRepositoryOptions{
		Pool:     pool,
		Settings: sessionTestSettings(t),
		CSRFKey:  bytes.Repeat([]byte{0x61}, 32),
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("constructing session repository: %v", err)
	}
	return repo
}

// Proof 4a — immediate enforcement
// Suspend an authenticated Account, issue the next protected request, assert denial.
// Mutation check: removing live ACTIVE-account check in sessionRecord.usable breaks this proof.
func TestSuspensionProof4a_ImmediateEnforcement(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)

	now := time.Now().UTC()
	adminID, adminP := createTestAdmin(t, p, "admin4a@example.com")
	adminSess, cred := createTestSession(t, p, adminID, now)

	sessionRepo := testSessionRepository(t, p, now)

	// Before suspension: session is usable and resolves.
	resolved, err := sessionRepo.Resolve(ctx, cred.CredentialDigest, UseReadOnly, "req-4a-before")
	if err != nil {
		t.Fatalf("resolving session before suspension: %v", err)
	}
	if resolved.Session.AccountID != adminID {
		t.Fatalf("resolved account ID = %s, want %s", resolved.Session.AccountID, adminID)
	}

	// Suspend account via transaction.
	conn, err := p.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquiring connection: %v", err)
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	res, err := SuspendAccount(ctx, tx, SuspendAccountRequest{
		ActorPrincipal:   adminP,
		ActorSession:     adminSess,
		RecentAuthWindow: 10 * time.Minute,
		SubjectAccountID: adminID,
		Reason:           "Security violation",
		Now:              now,
		RequestID:        "req-4a",
	})
	if err != nil {
		t.Fatalf("suspending account: %v", err)
	}
	if res.AlreadySuspended {
		t.Fatal("expected AlreadySuspended to be false")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing suspension: %v", err)
	}

	// Next request after suspension: session resolution MUST fail.
	_, err = sessionRepo.Resolve(ctx, cred.CredentialDigest, UseReadOnly, "req-4a-after")
	if !errors.Is(err, ErrAuthenticationRequired) {
		t.Fatalf("ResolveSession after suspension returned %v, want ErrAuthenticationRequired", err)
	}

	// Mutation Check: Proof 4a must fail if live ACTIVE-account check is bypassed.
	// We verify that a session record with accountStatus = SUSPENDED fails usable() because of StatusActive check.
	sr := sessionRecord{
		accountStatus:      StatusSuspended, // <--- If this check were omitted in usable(), usable() would pass below
		sessionEpoch:       1,
		admittedEpoch:      1,
		sessionState:       SessionActive,
		familyGeneration:   1,
		credentialRowState: "CURRENT",
		session: AuthenticatedSession{
			Generation:        1,
			IdleExpiresAt:     now.Add(time.Hour),
			AbsoluteExpiresAt: now.Add(time.Hour),
		},
	}
	if err := sr.usable(now); !errors.Is(err, ErrAuthenticationRequired) {
		t.Fatalf("mutation check: usable() returned %v, want ErrAuthenticationRequired when accountStatus is SUSPENDED", err)
	}
}

// Proof 4b — family invalidation
// Every existing family is persisted as revoked with reason ACCOUNT_SUSPENDED, with evidence.
// Mutation check: removing family-revocation update breaks this proof.
func TestSuspensionProof4b_FamilyInvalidation(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)

	now := time.Now().UTC()
	adminID, adminP := createTestAdmin(t, p, "admin4b@example.com")
	adminSess, _ := createTestSession(t, p, adminID, now)
	_, _ = createTestSession(t, p, adminID, now) // second session family

	conn, err := p.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquiring connection: %v", err)
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = SuspendAccount(ctx, tx, SuspendAccountRequest{
		ActorPrincipal:   adminP,
		ActorSession:     adminSess,
		RecentAuthWindow: 10 * time.Minute,
		SubjectAccountID: adminID,
		Reason:           "Abuse prevention",
		Now:              now,
		RequestID:        "req-4b",
	})
	if err != nil {
		t.Fatalf("suspending account: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing: %v", err)
	}

	// Verify database persistence of family revocation with reason ACCOUNT_SUSPENDED.
	rows, err := p.Query(ctx,
		`SELECT state, revocation_reason FROM sessions WHERE account_id = $1::uuid`,
		adminID,
	)
	if err != nil {
		t.Fatalf("querying sessions: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
		var state, reason string
		if err := rows.Scan(&state, &reason); err != nil {
			t.Fatalf("scanning session row: %v", err)
		}
		if state != string(SessionRevoked) {
			t.Errorf("session state = %s, want REVOKED", state)
		}
		if reason != string(RevokedByAccountSuspended) {
			t.Errorf("revocation_reason = %s, want ACCOUNT_SUSPENDED", reason)
		}
	}
	if count != 2 {
		t.Fatalf("revoked session count = %d, want 2", count)
	}

	// Verify evidence persistence.
	var eventType string
	if err := p.QueryRow(ctx,
		`SELECT event_type FROM identity_security_events WHERE account_id = $1::uuid AND event_type = 'ACCOUNT_SUSPENDED'`,
		adminID,
	).Scan(&eventType); err != nil {
		t.Fatalf("querying security event: %v", err)
	}
	if eventType != "ACCOUNT_SUSPENDED" {
		t.Fatalf("event_type = %s, want ACCOUNT_SUSPENDED", eventType)
	}

	// Mutation Check: Proof 4b must fail if family revocation update is removed.
	// If a session had NOT been updated to REVOKED / ACCOUNT_SUSPENDED, count of REVOKED/ACCOUNT_SUSPENDED rows would be 0.
}

// Proof 4c — epoch protection
// session_epoch advances atomically within the suspension transaction; a family admitted concurrently against the old epoch cannot survive.
// Mutation check: removing epoch advance breaks this proof.
func TestSuspensionProof4c_EpochProtection(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)

	now := time.Now().UTC()
	adminID, adminP := createTestAdmin(t, p, "admin4c@example.com")
	adminSess, _ := createTestSession(t, p, adminID, now)

	// Check epoch before suspension (starts at 1).
	var initialEpoch int
	if err := p.QueryRow(ctx, `SELECT session_epoch FROM accounts WHERE id = $1::uuid`, adminID).Scan(&initialEpoch); err != nil {
		t.Fatalf("querying initial epoch: %v", err)
	}
	if initialEpoch != 1 {
		t.Fatalf("initial epoch = %d, want 1", initialEpoch)
	}

	conn, err := p.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquiring conn: %v", err)
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	res, err := SuspendAccount(ctx, tx, SuspendAccountRequest{
		ActorPrincipal:   adminP,
		ActorSession:     adminSess,
		RecentAuthWindow: 10 * time.Minute,
		SubjectAccountID: adminID,
		Reason:           "Epoch test",
		Now:              now,
		RequestID:        "req-4c",
	})
	if err != nil {
		t.Fatalf("suspending: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing: %v", err)
	}

	if res.Epoch != initialEpoch+1 {
		t.Fatalf("returned epoch = %d, want %d", res.Epoch, initialEpoch+1)
	}

	// Verify epoch advanced in DB.
	var dbEpoch int
	if err := p.QueryRow(ctx, `SELECT session_epoch FROM accounts WHERE id = $1::uuid`, adminID).Scan(&dbEpoch); err != nil {
		t.Fatalf("querying db epoch: %v", err)
	}
	if dbEpoch != initialEpoch+1 {
		t.Fatalf("db epoch = %d, want %d", dbEpoch, initialEpoch+1)
	}

	// Verify concurrent admission with old epoch fails Usable check against the new epoch.
	oldEpochSession := Session{
		State:             SessionActive,
		AdmittedEpoch:     initialEpoch,
		IdleExpiresAt:     now.Add(time.Hour),
		AbsoluteExpiresAt: now.Add(time.Hour),
	}
	if err := oldEpochSession.Usable(dbEpoch, now); !errors.Is(err, ErrSessionNotUsable) {
		t.Fatalf("session admitted under old epoch %d was usable against account epoch %d", initialEpoch, dbEpoch)
	}

	// Mutation Check: Proof 4c must fail if epoch is not advanced (dbEpoch stays initialEpoch).
}

func TestSuspensionIdempotency(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)

	now := time.Now().UTC()
	adminID, adminP := createTestAdmin(t, p, "admin_idem@example.com")
	adminSess, _ := createTestSession(t, p, adminID, now)

	conn, err := p.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquiring conn: %v", err)
	}
	defer conn.Release()

	// 1st suspension
	tx1, _ := conn.Begin(ctx)
	res1, err := SuspendAccount(ctx, tx1, SuspendAccountRequest{
		ActorPrincipal:   adminP,
		ActorSession:     adminSess,
		RecentAuthWindow: 10 * time.Minute,
		SubjectAccountID: adminID,
		Reason:           "First suspend",
		Now:              now,
		RequestID:        "req-idem-1",
	})
	if err != nil {
		t.Fatalf("first suspend: %v", err)
	}
	_ = tx1.Commit(ctx)
	if res1.AlreadySuspended {
		t.Fatal("first suspend reported AlreadySuspended = true")
	}

	// 2nd suspension
	tx2, _ := conn.Begin(ctx)
	res2, err := SuspendAccount(ctx, tx2, SuspendAccountRequest{
		ActorPrincipal:   adminP,
		ActorSession:     adminSess,
		RecentAuthWindow: 10 * time.Minute,
		SubjectAccountID: adminID,
		Reason:           "Second suspend",
		Now:              now.Add(time.Minute),
		RequestID:        "req-idem-2",
	})
	if err != nil {
		t.Fatalf("second suspend: %v", err)
	}
	_ = tx2.Commit(ctx)

	if !res2.AlreadySuspended {
		t.Fatal("second suspend reported AlreadySuspended = false")
	}
	if res2.Epoch != res1.Epoch {
		t.Fatalf("second suspend epoch = %d, want %d", res2.Epoch, res1.Epoch)
	}

	// Assert exactly 1 ACCOUNT_SUSPENDED evidence event exists.
	var eventCount int
	if err := p.QueryRow(ctx,
		`SELECT count(*) FROM identity_security_events WHERE account_id = $1::uuid AND event_type = 'ACCOUNT_SUSPENDED'`,
		adminID,
	).Scan(&eventCount); err != nil {
		t.Fatalf("querying event count: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("ACCOUNT_SUSPENDED event count = %d, want 1", eventCount)
	}
}

func TestReinstatement(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)

	now := time.Now().UTC()
	adminID, adminP := createTestAdmin(t, p, "admin_reinstate@example.com")
	adminSess, cred := createTestSession(t, p, adminID, now)

	conn, err := p.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquiring conn: %v", err)
	}
	defer conn.Release()

	// Suspend
	tx1, _ := conn.Begin(ctx)
	_, _ = SuspendAccount(ctx, tx1, SuspendAccountRequest{
		ActorPrincipal:   adminP,
		ActorSession:     adminSess,
		RecentAuthWindow: 10 * time.Minute,
		SubjectAccountID: adminID,
		Reason:           "Suspend before reinstate",
		Now:              now,
		RequestID:        "req-sus",
	})
	_ = tx1.Commit(ctx)

	// Reinstate
	tx2, _ := conn.Begin(ctx)
	res, err := ReinstateAccount(ctx, tx2, ReinstateAccountRequest{
		ActorPrincipal:   adminP,
		ActorSession:     adminSess,
		RecentAuthWindow: 10 * time.Minute,
		SubjectAccountID: adminID,
		Reason:           "Reinstating account",
		Now:              now.Add(5 * time.Minute),
		RequestID:        "req-reinstate",
	})
	if err != nil {
		t.Fatalf("reinstating: %v", err)
	}
	_ = tx2.Commit(ctx)

	if res.AlreadyActive {
		t.Fatal("expected AlreadyActive = false")
	}

	// Verify Account status is ACTIVE in DB.
	var status string
	if err := p.QueryRow(ctx, `SELECT status FROM accounts WHERE id = $1::uuid`, adminID).Scan(&status); err != nil {
		t.Fatalf("querying account status: %v", err)
	}
	if status != string(StatusActive) {
		t.Fatalf("status = %s, want ACTIVE", status)
	}

	// Verify ACCOUNT_REINSTATED evidence exists.
	var eventType string
	if err := p.QueryRow(ctx,
		`SELECT event_type FROM identity_security_events WHERE account_id = $1::uuid AND event_type = 'ACCOUNT_REINSTATED'`,
		adminID,
	).Scan(&eventType); err != nil {
		t.Fatalf("querying security event: %v", err)
	}
	if eventType != "ACCOUNT_REINSTATED" {
		t.Fatalf("event_type = %s, want ACCOUNT_REINSTATED", eventType)
	}

	// Verify revoked sessions are NOT restored (session remains REVOKED).
	sessionRepo := testSessionRepository(t, p, now.Add(6*time.Minute))
	_, err = sessionRepo.Resolve(ctx, cred.CredentialDigest, UseReadOnly, "req-reinstate-check")
	if !errors.Is(err, ErrAuthenticationRequired) {
		t.Fatalf("reinstated user's old session resolved unexpectedly: %v", err)
	}
}

func TestSuspensionRecentAuthEnforcement(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)

	now := time.Now().UTC()
	adminID, adminP := createTestAdmin(t, p, "admin_stale@example.com")

	// Create a session authenticated 30 minutes ago (stale window of 10 minutes)
	staleAuthTime := now.Add(-30 * time.Minute)
	staleSess, _ := createTestSession(t, p, adminID, staleAuthTime)

	conn, err := p.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquiring conn: %v", err)
	}
	defer conn.Release()

	tx, _ := conn.Begin(ctx)
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = SuspendAccount(ctx, tx, SuspendAccountRequest{
		ActorPrincipal:   adminP,
		ActorSession:     staleSess,
		RecentAuthWindow: 10 * time.Minute,
		SubjectAccountID: adminID,
		Reason:           "Stale auth suspend attempt",
		Now:              now,
		RequestID:        "req-stale",
	})
	if !errors.Is(err, ErrRecentAuthRequired) {
		t.Fatalf("stale recent-auth suspend returned %v, want ErrRecentAuthRequired", err)
	}
}
