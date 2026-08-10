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

// activationBoundary reads the durable boundary migration 0017 stamped.
func activationBoundary(t *testing.T, pool *pgxpool.Pool) time.Time {
	t.Helper()
	var activatedAt time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT activated_at FROM transactional_email_activation WHERE id`).Scan(&activatedAt); err != nil {
		t.Fatalf("reading activation boundary: %v", err)
	}
	return activatedAt.UTC()
}

// activateAfterEvent stamps the boundary immediately after a named outbox
// event, modelling the production shape the reviewer was worried about: an
// existing database whose outbox already holds historical intents when
// delivery is switched on. The instant is computed by the database from the
// event's own occurred_at, so the test cannot flake on clock skew between the
// test process and PostgreSQL. Only a test writes this; workers never do.
func activateAfterEvent(t *testing.T, pool *pgxpool.Pool, eventID string) time.Time {
	t.Helper()
	var boundary time.Time
	if err := pool.QueryRow(context.Background(),
		`UPDATE transactional_email_activation
		    SET activated_at = (SELECT occurred_at + interval '1 microsecond'
		                          FROM outbox_events WHERE id=$1)
		  WHERE id
		  RETURNING activated_at`, eventID).Scan(&boundary); err != nil {
		t.Fatalf("setting activation boundary: %v", err)
	}
	return boundary.UTC()
}

// appendIntentAvailableAt writes a post-activation intent that is deliberately
// not due yet, so a test can prove a deferred intent is not lost.
func appendIntentAvailableAt(t *testing.T, pool *pgxpool.Pool, writer *outbox.Writer, availableAt time.Time) string {
	t.Helper()
	event := outbox.Event{
		Type: "identity.email_verification_requested", SchemaVersion: 1,
		SourceModule: "IDENTITY_AND_ACCESS", AggregateType: "ACCOUNT",
		AggregateID: uuid.NewString(), AggregateRevision: 1, CorrelationID: uuid.NewString(),
		AvailableAt: &availableAt,
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
		t.Fatalf("appending deferred email intent: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing deferred email intent: %v", err)
	}
	return eventID
}

func deliveryExists(t *testing.T, pool *pgxpool.Pool, eventID string) bool {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM transactional_email_deliveries WHERE event_id=$1`, eventID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count > 0
}

// TestDiscoveryIgnoresPreActivationHistory covers M-1 cases A, B and C: an
// intent that predates activation is never mailed, one created after it is,
// and a worker restart changes neither answer.
func TestDiscoveryIgnoresPreActivationHistory(t *testing.T) {
	pool := freshEmailDatabase(t)
	repository, writer, renderer := emailTestComponents(t, pool)
	ctx := context.Background()

	// A historical intent, written before delivery was ever switched on. Its
	// credential may be long expired; mailing it would be the hazard.
	historical := appendVerificationIntent(t, pool, writer, time.Now().UTC())

	// Delivery is activated now. Everything above is history.
	boundary := activateAfterEvent(t, pool, historical)

	sender := NewFakeSender()
	now := boundary.Add(time.Minute)
	dispatcher, err := NewDispatcher(DispatcherOptions{Repository: repository, Outbox: writer, Renderer: renderer, Sender: sender, LeaseDuration: time.Minute, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}

	// A. the historical intent stays historical.
	if count, err := dispatcher.DispatchPending(ctx, 10); err != nil || count != 0 {
		t.Fatalf("historical dispatch = (%d, %v), want (0, nil)", count, err)
	}
	if deliveryExists(t, pool, historical) {
		t.Fatal("pre-activation intent was enqueued for delivery")
	}
	if len(sender.Messages()) != 0 {
		t.Fatal("pre-activation intent reached the provider")
	}
	var historicalStillThere int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE id=$1`, historical).Scan(&historicalStillThere); err != nil {
		t.Fatal(err)
	}
	if historicalStillThere != 1 {
		t.Fatal("historical outbox evidence was destroyed")
	}

	// B. a genuinely new intent is delivered normally.
	fresh := appendVerificationIntent(t, pool, writer, now)
	if count, err := dispatcher.DispatchPending(ctx, 10); err != nil || count != 1 {
		t.Fatalf("post-activation dispatch = (%d, %v), want (1, nil)", count, err)
	}
	if !deliveryExists(t, pool, fresh) {
		t.Fatal("post-activation intent was not enqueued")
	}
	if messages := sender.Messages(); len(messages) != 1 || messages[0].IdempotencyKey != "gradex/"+fresh {
		t.Fatalf("post-activation delivery did not reach the provider: %d messages", len(messages))
	}

	// C. restart: new repository and dispatcher, as a redeployed worker would
	// build. The boundary must not have moved and history must stay unmailed.
	restartedRepository, err := NewRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	if got := activationBoundary(t, pool); !got.Equal(boundary) {
		t.Fatalf("activation boundary moved on restart: %s want %s", got, boundary)
	}
	restarted, err := NewDispatcher(DispatcherOptions{Repository: restartedRepository, Outbox: writer, Renderer: renderer, Sender: sender, LeaseDuration: time.Minute, Now: func() time.Time { return now.Add(time.Minute) }})
	if err != nil {
		t.Fatal(err)
	}
	if count, err := restarted.DispatchPending(ctx, 10); err != nil || count != 0 {
		t.Fatalf("restart dispatch = (%d, %v), want (0, nil)", count, err)
	}
	if deliveryExists(t, pool, historical) {
		t.Fatal("worker restart backfilled pre-activation history")
	}
	if got := activationBoundary(t, pool); !got.Equal(boundary) {
		t.Fatal("activation boundary advanced after a dispatch cycle")
	}
}

// TestDiscoveryKeepsUndiscoveredPostActivationIntentsEligible covers M-1 case
// E. The boundary is a fixed creation-time cutoff, not a progress watermark,
// so an intent that was not due during earlier polls is not skipped forever.
func TestDiscoveryKeepsUndiscoveredPostActivationIntentsEligible(t *testing.T) {
	pool := freshEmailDatabase(t)
	repository, writer, renderer := emailTestComponents(t, pool)
	ctx := context.Background()
	// Created after the migration-stamped boundary, but deliberately not due.
	created := time.Now().UTC()
	deferred := appendIntentAvailableAt(t, pool, writer, created.Add(time.Hour))

	sender := NewFakeSender()
	early := created.Add(2 * time.Minute)
	dispatcher, err := NewDispatcher(DispatcherOptions{Repository: repository, Outbox: writer, Renderer: renderer, Sender: sender, LeaseDuration: time.Minute, Now: func() time.Time { return early }})
	if err != nil {
		t.Fatal(err)
	}
	if count, err := dispatcher.DispatchPending(ctx, 10); err != nil || count != 0 {
		t.Fatalf("early dispatch = (%d, %v), want (0, nil)", count, err)
	}

	// Later, once it is due, it is still eligible: nothing consumed it.
	late := created.Add(2 * time.Hour)
	dueDispatcher, err := NewDispatcher(DispatcherOptions{Repository: repository, Outbox: writer, Renderer: renderer, Sender: sender, LeaseDuration: time.Minute, Now: func() time.Time { return late }})
	if err != nil {
		t.Fatal(err)
	}
	if count, err := dueDispatcher.DispatchPending(ctx, 10); err != nil || count != 1 {
		t.Fatalf("due dispatch = (%d, %v), want (1, nil)", count, err)
	}
	if !deliveryExists(t, pool, deferred) {
		t.Fatal("post-activation intent was lost instead of deferred")
	}
}

// TestConcurrentPollersShareOneActivationBoundary covers M-1 case D.
func TestConcurrentPollersShareOneActivationBoundary(t *testing.T) {
	pool := freshEmailDatabase(t)
	repository, writer, renderer := emailTestComponents(t, pool)
	ctx := context.Background()

	historical := appendVerificationIntent(t, pool, writer, time.Now().UTC())
	boundary := activateAfterEvent(t, pool, historical)

	now := boundary.Add(time.Minute)
	eligible := make([]string, 0, 4)
	for range 4 {
		eligible = append(eligible, appendVerificationIntent(t, pool, writer, now))
	}

	const pollers = 4
	sender := NewFakeSender()
	var group sync.WaitGroup
	errs := make(chan error, pollers)
	for range pollers {
		group.Add(1)
		go func() {
			defer group.Done()
			dispatcher, err := NewDispatcher(DispatcherOptions{Repository: repository, Outbox: writer, Renderer: renderer, Sender: sender, LeaseDuration: time.Minute, Now: func() time.Time { return now }})
			if err != nil {
				errs <- err
				return
			}
			if _, err := dispatcher.DispatchPending(ctx, 10); err != nil {
				errs <- err
			}
		}()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent poller: %v", err)
	}

	if got := activationBoundary(t, pool); !got.Equal(boundary) {
		t.Fatalf("concurrent pollers moved the boundary: %s want %s", got, boundary)
	}
	if deliveryExists(t, pool, historical) {
		t.Fatal("a concurrent poller backfilled pre-activation history")
	}
	var deliveries, boundaryRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM transactional_email_deliveries`).Scan(&deliveries); err != nil {
		t.Fatal(err)
	}
	if deliveries != len(eligible) {
		t.Fatalf("delivery rows = %d, want %d (one per eligible intent, no duplicates)", deliveries, len(eligible))
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM transactional_email_activation`).Scan(&boundaryRows); err != nil {
		t.Fatal(err)
	}
	if boundaryRows != 1 {
		t.Fatalf("activation rows = %d, want exactly 1", boundaryRows)
	}
	// Each intent is a distinct message; none was sent twice.
	if messages := sender.Messages(); len(messages) != len(eligible) {
		t.Fatalf("provider messages = %d, want %d", len(messages), len(eligible))
	}
}
