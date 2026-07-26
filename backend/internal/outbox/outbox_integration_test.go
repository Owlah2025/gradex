//go:build integration

package outbox

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	outboxAdminDSN = "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable"
	outboxTestDB   = "gradex_outbox_test"
	outboxTestDSN  = "postgres://gradex:gradex@localhost:5432/" + outboxTestDB + "?sslmode=disable"
)

func freshOutboxDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	admin, err := pgxpool.New(ctx, outboxAdminDSN)
	if err != nil {
		t.Fatalf("connecting to admin database: %v", err)
	}
	defer admin.Close()
	_, _ = admin.Exec(ctx,
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`,
		outboxTestDB,
	)
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+outboxTestDB); err != nil {
		t.Fatalf("dropping database: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+outboxTestDB); err != nil {
		t.Fatalf("creating database: %v", err)
	}

	m, err := migrate.New("file://../db/migrations", outboxTestDSN)
	if err != nil {
		t.Fatalf("opening migrations: %v", err)
	}
	t.Cleanup(func() { _, _ = m.Close() })
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrating: %v", err)
	}

	pool, err := pgxpool.New(ctx, outboxTestDSN)
	if err != nil {
		t.Fatalf("opening pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestAppendCommitsEventAndProtectedPayloadTogether(t *testing.T) {
	pool := freshOutboxDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	writer, err := NewWriter("test-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("constructing writer: %v", err)
	}
	event := Event{
		Type:              "identity.email_verification_requested",
		SchemaVersion:     1,
		SourceModule:      "IDENTITY_AND_ACCESS",
		AggregateType:     "ACCOUNT",
		AggregateID:       "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		AggregateRevision: 1,
		CorrelationID:     "request-1",
		SafePayload: map[string]any{
			"purpose": "EMAIL_VERIFICATION",
		},
	}
	delivery := VerificationDelivery{
		Destination:       "Student@Example.com",
		Locale:            "en",
		TemplateContract:  "student-email-verification-v1",
		VerificationToken: "BEARER_CANARY_123",
		ExpiresAt:         time.Now().UTC().Add(time.Hour),
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("beginning transaction: %v", err)
	}
	eventID, err := writer.Append(ctx, tx, event, delivery)
	if err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing: %v", err)
	}

	var eventCount, payloadCount int
	if err := pool.QueryRow(ctx,
		`SELECT
		   (SELECT count(*) FROM outbox_events WHERE id = $1),
		   (SELECT count(*) FROM outbox_protected_payloads WHERE event_id = $1)`,
		eventID,
	).Scan(&eventCount, &payloadCount); err != nil {
		t.Fatalf("counting rows: %v", err)
	}
	if eventCount != 1 || payloadCount != 1 {
		t.Fatalf("row counts = event %d/payload %d, want 1/1", eventCount, payloadCount)
	}
}

func TestAppendRollsBackBothRows(t *testing.T) {
	pool := freshOutboxDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	writer, err := NewWriter("test-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("constructing writer: %v", err)
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("beginning transaction: %v", err)
	}
	eventID, err := writer.Append(ctx, tx, Event{
		Type:              "identity.email_verification_requested",
		SchemaVersion:     1,
		SourceModule:      "IDENTITY_AND_ACCESS",
		AggregateType:     "ACCOUNT",
		AggregateID:       "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		AggregateRevision: 1,
		CorrelationID:     "request-rollback",
		SafePayload:       map[string]any{"purpose": "EMAIL_VERIFICATION"},
	}, VerificationDelivery{
		Destination:       "Student@Example.com",
		Locale:            "en",
		TemplateContract:  "student-email-verification-v1",
		VerificationToken: "BEARER_CANARY_123",
		ExpiresAt:         time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rolling back: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT
		   (SELECT count(*) FROM outbox_events WHERE id = $1)
		 + (SELECT count(*) FROM outbox_protected_payloads WHERE event_id = $1)`,
		eventID,
	).Scan(&count); err != nil {
		t.Fatalf("counting rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("rollback left %d outbox rows", count)
	}
}
