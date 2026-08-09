//go:build integration

package email

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

const (
	emailAdminDSN = "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable"
	emailTestDB   = "gradex_email_test"
	emailTestDSN  = "postgres://gradex:gradex@localhost:5432/" + emailTestDB + "?sslmode=disable"
)

func freshEmailDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	admin, err := pgxpool.New(ctx, emailAdminDSN)
	if err != nil {
		t.Fatalf("connecting to email test admin database: %v", err)
	}
	defer admin.Close()
	_, _ = admin.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=$1`, emailTestDB)
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+emailTestDB); err != nil {
		t.Fatalf("dropping email test database: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+emailTestDB); err != nil {
		t.Fatalf("creating email test database: %v", err)
	}
	migrator, err := migrate.New("file://../db/migrations", emailTestDSN)
	if err != nil {
		t.Fatalf("opening email test migrations: %v", err)
	}
	t.Cleanup(func() { _, _ = migrator.Close() })
	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrating email test database: %v", err)
	}
	pool, err := pgxpool.New(ctx, emailTestDSN)
	if err != nil {
		t.Fatalf("opening email test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func appendVerificationIntent(t *testing.T, pool *pgxpool.Pool, writer *outbox.Writer, availableAt time.Time) string {
	t.Helper()
	event := outbox.Event{
		Type: "identity.email_verification_requested", SchemaVersion: 1,
		SourceModule: "IDENTITY_AND_ACCESS", AggregateType: "ACCOUNT",
		AggregateID: uuid.NewString(), AggregateRevision: 1, CorrelationID: uuid.NewString(),
		SafePayload: map[string]any{"purpose": "EMAIL_VERIFICATION", "locale": "en", "template_contract": TemplateVerifyEmail},
	}
	delivery := outbox.VerificationDelivery{
		Destination: "student@example.com", Locale: "en", TemplateContract: TemplateVerifyEmail,
		VerificationToken: "PRIVATE_CREDENTIAL_CANARY", ExpiresAt: availableAt.Add(time.Hour),
	}
	ctx := context.Background()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	eventID, err := writer.Append(ctx, tx, event, delivery)
	if err != nil {
		t.Fatalf("appending email intent: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing email intent: %v", err)
	}
	return eventID
}

func emailTestComponents(t *testing.T, pool *pgxpool.Pool) (*Repository, *outbox.Writer, *Renderer) {
	t.Helper()
	writer, err := outbox.NewWriter("email-test-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	repository, err := NewRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := NewRenderer(RendererOptions{PublicOrigin: "https://gradex.example", FromAddress: "notify@gradex.example", FromName: "Gradex"})
	if err != nil {
		t.Fatal(err)
	}
	return repository, writer, renderer
}

func TestDispatcherAcceptsDurableIntentOnce(t *testing.T) {
	pool := freshEmailDatabase(t)
	now := time.Now().UTC().Add(time.Second)
	repository, writer, renderer := emailTestComponents(t, pool)
	eventID := appendVerificationIntent(t, pool, writer, now.Add(-time.Second))
	sender := NewFakeSender()
	dispatcher, err := NewDispatcher(DispatcherOptions{Repository: repository, Outbox: writer, Renderer: renderer, Sender: sender, LeaseDuration: time.Minute, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if count, err := dispatcher.DispatchPending(context.Background(), 10); err != nil || count != 1 {
		t.Fatalf("first dispatch = (%d, %v), want (1, nil)", count, err)
	}
	if count, err := dispatcher.DispatchPending(context.Background(), 10); err != nil || count != 0 {
		t.Fatalf("second dispatch = (%d, %v), want (0, nil)", count, err)
	}
	if messages := sender.Messages(); len(messages) != 1 || messages[0].IdempotencyKey != "gradex/"+eventID {
		t.Fatalf("captured deliveries = %d or unstable idempotency key", len(messages))
	}
	var status string
	var attempts int
	if err := pool.QueryRow(context.Background(), `SELECT status,attempt_count FROM transactional_email_deliveries WHERE event_id=$1`, eventID).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "ACCEPTED" || attempts != 1 {
		t.Fatalf("delivery = %s/%d, want ACCEPTED/1", status, attempts)
	}
}

func TestDispatcherRetriesTransientAndStopsPermanentFailures(t *testing.T) {
	pool := freshEmailDatabase(t)
	now := time.Now().UTC().Add(time.Second)
	repository, writer, renderer := emailTestComponents(t, pool)
	transientID := appendVerificationIntent(t, pool, writer, now.Add(-time.Second))
	sender := NewFakeSender()
	sender.FailNext(&SendFailure{Kind: FailureTransient, Class: "network", Code: "network"})
	dispatcher, _ := NewDispatcher(DispatcherOptions{Repository: repository, Outbox: writer, Renderer: renderer, Sender: sender, LeaseDuration: time.Minute, Now: func() time.Time { return now }})
	if count, err := dispatcher.DispatchPending(context.Background(), 10); err != nil || count != 1 {
		t.Fatalf("transient dispatch = (%d, %v)", count, err)
	}
	now = now.Add(30 * time.Second)
	if count, err := dispatcher.DispatchPending(context.Background(), 10); err != nil || count != 1 {
		t.Fatalf("retry dispatch = (%d, %v)", count, err)
	}
	var outcomes []string
	rows, err := pool.Query(context.Background(), `SELECT outcome FROM transactional_email_attempts WHERE event_id=$1 ORDER BY attempt_number`, transientID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var outcome string
		if err := rows.Scan(&outcome); err != nil {
			t.Fatal(err)
		}
		outcomes = append(outcomes, outcome)
	}
	rows.Close()
	if len(outcomes) != 2 || outcomes[0] != "TRANSIENT_FAILURE" || outcomes[1] != "ACCEPTED" {
		t.Fatalf("transient outcomes = %v", outcomes)
	}

	permanentID := appendVerificationIntent(t, pool, writer, now.Add(-time.Second))
	sender.FailNext(&SendFailure{Kind: FailurePermanent, Class: "recipient", Code: "invalid_to"})
	if count, err := dispatcher.DispatchPending(context.Background(), 10); err != nil || count != 1 {
		t.Fatalf("permanent dispatch = (%d, %v)", count, err)
	}
	now = now.Add(time.Hour)
	if count, err := dispatcher.DispatchPending(context.Background(), 10); err != nil || count != 0 {
		t.Fatalf("permanent redispatch = (%d, %v), want no retry", count, err)
	}
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM transactional_email_deliveries WHERE event_id=$1`, permanentID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "PERMANENT_FAILED" {
		t.Fatalf("permanent status = %s", status)
	}
}

func TestRepositoryRecoversStaleLeaseWithoutDuplicateClaim(t *testing.T) {
	pool := freshEmailDatabase(t)
	now := time.Now().UTC().Add(time.Second)
	repository, writer, _ := emailTestComponents(t, pool)
	eventID := appendVerificationIntent(t, pool, writer, now.Add(-time.Second))
	claims, err := repository.Claim(context.Background(), ClaimOptions{Provider: "fake", Now: now, LeaseDuration: time.Minute, Limit: 1})
	if err != nil || len(claims) != 1 {
		t.Fatalf("initial claim = %d, %v", len(claims), err)
	}
	now = now.Add(2 * time.Minute)
	var wg sync.WaitGroup
	results := make(chan int, 2)
	errorsFound := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, claimErr := repository.Claim(context.Background(), ClaimOptions{Provider: "fake", Now: now, LeaseDuration: time.Minute, Limit: 1})
			if claimErr != nil {
				errorsFound <- claimErr
				return
			}
			results <- len(got)
		}()
	}
	wg.Wait()
	close(results)
	close(errorsFound)
	for claimErr := range errorsFound {
		t.Fatal(claimErr)
	}
	total := 0
	for count := range results {
		total += count
	}
	if total != 1 {
		t.Fatalf("concurrent stale recovery produced %d claims, want 1", total)
	}
	var firstOutcome string
	if err := pool.QueryRow(context.Background(), `SELECT outcome FROM transactional_email_attempts WHERE event_id=$1 AND attempt_number=1`, eventID).Scan(&firstOutcome); err != nil {
		t.Fatal(err)
	}
	if firstOutcome != "TRANSIENT_FAILURE" {
		t.Fatalf("stale attempt outcome = %s", firstOutcome)
	}
}

func TestRepositoryExhaustsFinalStaleLease(t *testing.T) {
	pool := freshEmailDatabase(t)
	now := time.Now().UTC().Add(time.Second)
	repository, writer, _ := emailTestComponents(t, pool)
	eventID := appendVerificationIntent(t, pool, writer, now.Add(-time.Second))
	for attempt := 1; attempt <= MaxAttempts; attempt++ {
		claims, err := repository.Claim(context.Background(), ClaimOptions{Provider: "fake", Now: now, LeaseDuration: time.Minute, Limit: 1})
		if err != nil || len(claims) != 1 || claims[0].AttemptNumber != attempt {
			t.Fatalf("claim %d = (%d, %v)", attempt, len(claims), err)
		}
		now = now.Add(2 * time.Minute)
	}
	claims, err := repository.Claim(context.Background(), ClaimOptions{Provider: "fake", Now: now, LeaseDuration: time.Minute, Limit: 1})
	if err != nil || len(claims) != 0 {
		t.Fatalf("post-budget claim = (%d, %v), want no claim", len(claims), err)
	}
	var status, finalOutcome string
	if err := pool.QueryRow(context.Background(), `SELECT d.status,a.outcome
		FROM transactional_email_deliveries d JOIN transactional_email_attempts a ON a.event_id=d.event_id
		WHERE d.event_id=$1 AND a.attempt_number=$2`, eventID, MaxAttempts).Scan(&status, &finalOutcome); err != nil {
		t.Fatal(err)
	}
	if status != "EXHAUSTED" || finalOutcome != "EXHAUSTED" {
		t.Fatalf("final stale lease = %s/%s, want EXHAUSTED/EXHAUSTED", status, finalOutcome)
	}
}
