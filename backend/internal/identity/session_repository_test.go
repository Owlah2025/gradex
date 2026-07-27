package identity

import (
	"bytes"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

func TestSessionRepositoryBuildsCurrentStrengthDummyCredential(t *testing.T) {
	repository, err := NewSessionRepository(SessionRepositoryOptions{
		Pool: &pgxpool.Pool{}, Settings: config.SessionSettings{},
		CSRFKey: bytes.Repeat([]byte{0x41}, 32), Now: time.Now,
	})
	if err != nil {
		t.Fatalf("constructing repository: %v", err)
	}
	needsRehash, err := NeedsRehash(repository.dummyHash)
	if err != nil {
		t.Fatalf("reading dummy hash: %v", err)
	}
	if needsRehash {
		t.Fatal("unknown-email verification would use weaker Argon2id work")
	}
}

func TestSessionRepositoryRejectsShortCSRFKey(t *testing.T) {
	_, err := NewSessionRepository(SessionRepositoryOptions{
		Pool: &pgxpool.Pool{}, Settings: config.SessionSettings{},
		CSRFKey: bytes.Repeat([]byte{0x41}, 31), Now: time.Now,
	})
	if err == nil {
		t.Fatal("repository accepted a session CSRF key below 32 bytes")
	}
}
