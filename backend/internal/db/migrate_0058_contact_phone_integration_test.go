//go:build integration

package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5/pgconn"
)

// D-117: 0058 must upgrade a populated schema-57 database without rewriting a
// row, keep every existing Student and purchase request valid with NULL
// contact, and refuse to roll back while it holds numbers.
func TestStudentContactPhoneMigrationUpgradesPopulatedSchema57(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	if err := m.Migrate(57); err != nil {
		t.Fatalf("migrating to 57: %v", err)
	}
	pool := openPool(t)
	ctx := context.Background()

	const (
		studentID    = "70000000-0000-0000-0000-000000000001"
		adminID      = "70000000-0000-0000-0000-000000000002"
		instructorID = "70000000-0000-0000-0000-000000000003"
		courseID     = "70000000-0000-0000-0000-000000000010"
	)
	for _, statement := range []string{
		`INSERT INTO accounts (id, normalized_email, email, role, status, display_name, email_verified_at) VALUES
		 ('` + studentID + `', 'contact-s@example.com', 'contact-s@example.com', 'STUDENT', 'ACTIVE', 'Student', now()),
		 ('` + adminID + `', 'contact-a@example.com', 'contact-a@example.com', 'ADMIN', 'ACTIVE', 'Admin', now()),
		 ('` + instructorID + `', 'contact-i@example.com', 'contact-i@example.com', 'INSTRUCTOR', 'ACTIVE', 'Instructor', now())`,
		`INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ('` + courseID + `', '` + instructorID + `', 'DRAFT')`,
		`INSERT INTO purchase_requests (reference_code, course_id, email, normalized_email, requester_account_id,
		   course_title_ar, course_title_en, price_minor_units, currency, state)
		 VALUES ('GRX-0058HISTORICAL01', '` + courseID + `', 'contact-s@example.com', 'contact-s@example.com', '` + studentID + `',
		   'مقرر', 'Course', 25000, 'KWD', 'WAITING_PAYMENT')`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("seeding schema 57: %v", err)
		}
	}
	var revisionBefore int
	if err := pool.QueryRow(ctx, `SELECT revision FROM accounts WHERE id=$1::uuid`, studentID).Scan(&revisionBefore); err != nil {
		t.Fatal(err)
	}

	if err := m.Migrate(58); err != nil {
		t.Fatalf("migrating 57 -> 58: %v", err)
	}
	version, dirty, err := m.Version()
	if err != nil || version != 58 || dirty {
		t.Fatalf("version = %d dirty=%v err=%v", version, dirty, err)
	}

	// Nothing was invented or rewritten.
	var studentPhone, snapshot *string
	var revisionAfter, requests int
	if err := pool.QueryRow(ctx, `SELECT contact_phone, revision FROM accounts WHERE id=$1::uuid`, studentID).Scan(&studentPhone, &revisionAfter); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT contact_phone FROM purchase_requests WHERE reference_code='GRX-0058HISTORICAL01'`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM purchase_requests WHERE state='WAITING_PAYMENT'`).Scan(&requests); err != nil {
		t.Fatal(err)
	}
	if studentPhone != nil || snapshot != nil || revisionAfter != revisionBefore || requests != 1 {
		t.Fatalf("upgrade changed data: phone=%v snapshot=%v revision %d->%d requests=%d",
			studentPhone, snapshot, revisionBefore, revisionAfter, requests)
	}
	var unvalidated int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_constraint WHERE conname LIKE '%contact_phone%' AND NOT convalidated`).Scan(&unvalidated); err != nil || unvalidated != 0 {
		t.Fatalf("unvalidated contact constraints = %d err=%v", unvalidated, err)
	}

	// The database refuses malformed numbers, staff numbers, and incoherent rows.
	for _, statement := range []string{
		`UPDATE accounts SET contact_phone='96550012345', contact_phone_updated_at=now() WHERE id='` + studentID + `'`,
		`UPDATE accounts SET contact_phone='+96550012345' WHERE id='` + studentID + `'`,
		`UPDATE accounts SET contact_phone='+96550012345', contact_phone_updated_at=now() WHERE id='` + adminID + `'`,
		`UPDATE purchase_requests SET contact_phone='+965 5001 2345' WHERE reference_code='GRX-0058HISTORICAL01'`,
	} {
		_, err := pool.Exec(ctx, statement)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("%s: err = %v, want check_violation", statement, err)
		}
	}

	// A missing snapshot may be filled once; a recorded one is immutable.
	if _, err := pool.Exec(ctx, `UPDATE purchase_requests SET contact_phone='+96550012345' WHERE reference_code='GRX-0058HISTORICAL01'`); err != nil {
		t.Fatalf("filling a missing snapshot: %v", err)
	}
	for _, value := range []string{"'+201001234567'", "NULL"} {
		_, err := pool.Exec(ctx, `UPDATE purchase_requests SET contact_phone=`+value+` WHERE reference_code='GRX-0058HISTORICAL01'`)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23001" {
			t.Fatalf("rewriting a snapshot to %s: err = %v, want restrict_violation", value, err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE accounts SET contact_phone='+96550012345', contact_phone_updated_at=now() WHERE id=$1::uuid`, studentID); err != nil {
		t.Fatalf("storing a valid Student number: %v", err)
	}

	// Rolling back would destroy the numbers. The canonical command's
	// preflight refuses before golang-migrate touches the marker...
	if err := CheckStudentContactPhoneRollbackSafety(ctx, pool); err == nil || !strings.Contains(err.Error(), "cannot roll back 0058") {
		t.Fatalf("preflight with numbers present: err = %v, want refusal", err)
	}
	if version, dirty, err := m.Version(); err != nil || version != 58 || dirty {
		t.Fatalf("preflight changed the marker: %d dirty=%v err=%v", version, dirty, err)
	}
	// ...and the SQL itself refuses too, for any writer that skips the command.
	err = m.Steps(-1)
	if err == nil || !strings.Contains(err.Error(), "cannot roll back 0058") {
		t.Fatalf("down with numbers present: err = %v, want refusal", err)
	}
	if _, err := pool.Exec(ctx, `SELECT contact_phone FROM accounts LIMIT 1`); err != nil {
		t.Fatalf("refused down removed the column: %v", err)
	}
}

// With no number or snapshot recorded, 0058 down restores schema 57 exactly.
func TestStudentContactPhoneMigrationDownIsCleanWhenEmpty(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	if err := m.Migrate(58); err != nil {
		t.Fatalf("migrating to 58: %v", err)
	}
	if err := m.Steps(-1); err != nil {
		t.Fatalf("0058 down on an empty contact set: %v", err)
	}
	version, dirty, err := m.Version()
	if err != nil || version != 57 || dirty {
		t.Fatalf("version after down = %d dirty=%v err=%v", version, dirty, err)
	}
	pool := openPool(t)
	var columns, functions int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM information_schema.columns WHERE column_name IN ('contact_phone', 'contact_phone_updated_at')`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM pg_proc WHERE proname='purchase_request_contact_snapshot_immutable'`).Scan(&functions); err != nil {
		t.Fatal(err)
	}
	if columns != 0 || functions != 0 {
		t.Fatalf("after down: %d contact columns, %d functions remain", columns, functions)
	}
	if err := m.Migrate(58); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("re-applying 0058: %v", err)
	}
}
