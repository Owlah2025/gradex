//go:build integration

package identity

import (
	"errors"
	"testing"
	"time"
)

func TestRevokeAllSessionsInvalidatesExistingCredentialByEpoch(t *testing.T) {
	freshSchema(t)
	pool, ctx := pool(t)
	targetID := "10000000-0000-0000-0000-000000000851"
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
		VALUES ($1::uuid, 'epoch-target@example.com', 'epoch-target@example.com', 'STUDENT', 'ACTIVE', 'Epoch Target')`, targetID); err != nil {
		t.Fatalf("seeding epoch target: %v", err)
	}
	hash, err := HashPassword("EpochPassword123!")
	if err != nil {
		t.Fatalf("hashing epoch password: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO password_credentials (account_id, password_hash, state) VALUES ($1::uuid, $2, 'ACTIVE')`, targetID, hash.Expose()); err != nil {
		t.Fatalf("seeding epoch password: %v", err)
	}
	now := time.Now().UTC()
	_, credential := createTestSession(t, pool, targetID, now)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning session revocation: %v", err)
	}
	if _, _, err := RevokeAllSessionsInTransaction(ctx, tx, targetID, RevokedByAdmin, now); err != nil {
		t.Fatalf("revoking all sessions: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing session revocation: %v", err)
	}
	repository := testSessionRepository(t, pool, now)
	_, err = repository.Resolve(ctx, SessionResolutionRequest{
		CredentialDigest: credential.CredentialDigest,
		UseKind:          UseReadOnly,
		RequestID:        "session-revocation-test",
	})
	if !errors.Is(err, ErrAuthenticationRequired) {
		t.Fatalf("resolved revoked credential with %v, want ErrAuthenticationRequired", err)
	}
}
