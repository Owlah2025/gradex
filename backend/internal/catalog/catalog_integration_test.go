//go:build integration

package catalog

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

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

const (
	adminDSN   = "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable"
	testDBName = "gradex_catalog_test"
	testDSN    = "postgres://gradex:gradex@localhost:5432/" + testDBName + "?sslmode=disable"
	sourceURL  = "file://../db/migrations"
	opTimeout  = 30 * time.Second
)

func freshSchema(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	admin, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Fatalf("connecting to admin db: %v", err)
	}
	defer admin.Close()

	_, _ = admin.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, testDBName)
	_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+testDBName)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+testDBName); err != nil {
		t.Fatalf("creating test db: %v", err)
	}

	m, err := migrate.New(sourceURL, testDSN)
	if err != nil {
		t.Fatalf("creating migrator: %v", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrating up: %v", err)
	}
}

func pool(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	t.Cleanup(cancel)

	p, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("opening pool: %v", err)
	}
	t.Cleanup(p.Close)
	return p, ctx
}

func testOutboxWriter(t *testing.T) *outbox.Writer {
	t.Helper()
	w, err := outbox.NewWriter("key-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("outbox.NewWriter: %v", err)
	}
	return w
}

func seedInstructorAndCourse(t *testing.T, pool *pgxpool.Pool, ctx context.Context) (accountID string, courseID string) {
	t.Helper()
	accountID = "11111111-1111-1111-1111-111111111111"
	courseID = "22222222-2222-2222-2222-222222222222"

	_, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
		VALUES ($1, 'instructor@example.com', 'instructor@example.com', 'INSTRUCTOR', 'ACTIVE', 'Test Instructor')
	`, accountID)
	if err != nil {
		t.Fatalf("seeding account: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO courses (id, owner_account_id, lifecycle)
		VALUES ($1, $2, 'DRAFT')
	`, courseID, accountID)
	if err != nil {
		t.Fatalf("seeding course: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO course_revisions (course_id, state, revision_number, title_ar, title_en, description_ar, description_en)
		VALUES ($1, 'DRAFT', 1, 'العنوان', 'Title', 'الوصف', 'Description')
	`, courseID)
	if err != nil {
		t.Fatalf("seeding revision: %v", err)
	}

	return accountID, courseID
}

func TestRepositoryLockCourseAndConflict(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)

	accountID, courseID := seedInstructorAndCourse(t, p, ctx)

	repo, err := NewRepository(p, testOutboxWriter(t))
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}

	// Test IsCourseOwner
	isOwner, err := repo.IsCourseOwner(ctx, courseID, accountID)
	if err != nil || !isOwner {
		t.Fatalf("IsCourseOwner returned (%v, %v), want (true, nil)", isOwner, err)
	}

	notOwner, err := repo.IsCourseOwner(ctx, courseID, "99999999-9999-9999-9999-999999999999")
	if err != nil || notOwner {
		t.Fatalf("IsCourseOwner returned (%v, %v), want (false, nil)", notOwner, err)
	}

	// Test LockCourse
	err = repo.ExecTx(ctx, func(tx pgx.Tx) error {
		row, err := repo.LockCourse(ctx, tx, courseID)
		if err != nil {
			return err
		}
		if row.Lifecycle != "DRAFT" {
			t.Errorf("got lifecycle %s, want DRAFT", row.Lifecycle)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ExecTx LockCourse DRAFT failed: %v", err)
	}

	err = repo.ExecTx(ctx, func(tx pgx.Tx) error {
		_, err := repo.LockCourse(ctx, tx, courseID, string(LifecyclePublished))
		return err
	})
	if err == nil {
		t.Fatal("LockCourse expected an error for a conflicting lifecycle")
	}
	var conflictErr *LifecycleConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("LockCourse conflict error = %v, want LifecycleConflictError", err)
	}
}

func TestAuditWritingIntegration(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)

	accountID, courseID := seedInstructorAndCourse(t, p, ctx)

	repo, err := NewRepository(p, testOutboxWriter(t))
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}

	event := AuditEvent{
		ActorAccountID:  &accountID,
		ActorRole:       "INSTRUCTOR",
		ActorDescriptor: "instructor@example.com",
		Action:          "COURSE_SUBMITTED",
		TargetType:      "COURSE",
		TargetID:        courseID,
		Reason:          "Course submitted for review",
		Metadata:        map[string]any{"sections_count": 2},
	}

	err = repo.ExecTx(ctx, func(tx pgx.Tx) error {
		return WriteAuditEvent(ctx, tx, event)
	})
	if err != nil {
		t.Fatalf("WriteAuditEvent failed: %v", err)
	}

	var count int
	var module string
	err = p.QueryRow(ctx, `SELECT count(*), module FROM audit_events WHERE action = 'COURSE_SUBMITTED' GROUP BY module`).Scan(&count, &module)
	if err != nil {
		t.Fatalf("querying audit_events: %v", err)
	}
	if count != 1 || module != "CATALOG_AND_AUTHORING" {
		t.Errorf("audit row count = %d, module = %s; want 1, CATALOG_AND_AUTHORING", count, module)
	}
}

func TestNotificationIntentWritingIntegration(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)

	accountID, courseID := seedInstructorAndCourse(t, p, ctx)

	outboxWriter := testOutboxWriter(t)
	repo, err := NewRepository(p, outboxWriter)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}

	outboxWriter2, err := outbox.NewWriter("key-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("outbox.NewWriter: %v", err)
	}

	notifier, err := NewNotificationIntentWriter(outboxWriter2)
	if err != nil {
		t.Fatalf("NewNotificationIntentWriter: %v", err)
	}

	event := outbox.Event{
		Type:              "course.submitted",
		SchemaVersion:     1,
		SourceModule:      "CATALOG_AND_AUTHORING",
		AggregateType:     "COURSE",
		AggregateID:       courseID,
		AggregateRevision: 1,
		SafePayload:       map[string]any{"course_id": courseID},
		CorrelationID:     "corr-123",
	}

	protectedPayload := map[string]any{
		"instructor_account_id": accountID,
	}

	var outboxID string
	err = repo.ExecTx(ctx, func(tx pgx.Tx) error {
		var err error
		outboxID, err = notifier.WriteIntent(ctx, tx, event, protectedPayload)
		return err
	})
	if err != nil {
		t.Fatalf("WriteIntent failed: %v", err)
	}

	var eventsCount, payloadsCount int
	err = p.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE id = $1::uuid`, outboxID).Scan(&eventsCount)
	if err != nil {
		t.Fatalf("querying outbox_events: %v", err)
	}
	err = p.QueryRow(ctx, `SELECT count(*) FROM outbox_protected_payloads WHERE event_id = $1::uuid`, outboxID).Scan(&payloadsCount)
	if err != nil {
		t.Fatalf("querying outbox_protected_payloads: %v", err)
	}

	if eventsCount != 1 || payloadsCount != 1 {
		t.Errorf("outbox events = %d, protected payloads = %d; want 1, 1", eventsCount, payloadsCount)
	}
}
