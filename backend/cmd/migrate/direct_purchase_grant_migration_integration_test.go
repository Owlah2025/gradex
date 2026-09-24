//go:build integration

package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	directGrantDBName = "gradex_direct_grant_migration_test"
	directGrantDSN    = "postgres://gradex:gradex@localhost:5432/" + directGrantDBName + "?sslmode=disable"
)

// The 0044 widening has one job: make a direct purchase grant representable
// without making anything that was representable at 43 stop being so. Both
// halves are asserted here against a real database, because a CHECK constraint
// is only as good as what PostgreSQL actually accepts and refuses.
func TestDirectPurchaseGrantMigrationWidensWithoutLosingHistoricalShapes(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, migrator := freshDirectGrantDatabase(t, ctx)

	seedDirectGrantFixtures(t, ctx, pool)

	// 1. The shape this migration exists to allow: purchase-request provenance,
	//    no invitation.
	if _, err := pool.Exec(ctx, `
		INSERT INTO entitlements (
			id, student_account_id, scope_kind, scope_id, course_id, grant_source,
			source_invitation_id, source_purchase_request_id, original_access_ends_at,
			access_ends_at, retirement_eligibility_at, state, revoked_at, revision, created_at, updated_at
		) VALUES (
			'40000000-0000-0000-0000-000000000001'::uuid, $1::uuid, 'COURSE', $2::uuid, $2::uuid,
			'PURCHASE_REQUEST', NULL, $3::uuid, now() + interval '30 days', now() + interval '30 days',
			now() + interval '60 days', 'ACTIVE', NULL, 1, now(), now()
		)
	`, directGrantStudentID, directGrantCourseID, directGrantRequestID); err != nil {
		t.Fatalf("direct purchase entitlement was refused after 0044: %v", err)
	}

	// 2. The historical shape stays legal. A row like this is in production now,
	//    and the migration must not have invalidated it.
	if _, err := pool.Exec(ctx, `
		INSERT INTO entitlements (
			id, student_account_id, scope_kind, scope_id, course_id, grant_source,
			source_invitation_id, source_purchase_request_id, original_access_ends_at,
			access_ends_at, retirement_eligibility_at, state, revoked_at, revision, created_at, updated_at
		) VALUES (
			'40000000-0000-0000-0000-000000000002'::uuid, $1::uuid, 'COURSE', $2::uuid, $2::uuid,
			'PURCHASE_REQUEST', $3::uuid, NULL, now() + interval '30 days', now() + interval '30 days',
			now() + interval '60 days', 'REVOKED', now(), 1, now(), now()
		)
	`, directGrantOtherStudentID, directGrantCourseID, directGrantInvitationID); err != nil {
		t.Fatalf("historical invitation-backed entitlement was refused after 0044: %v", err)
	}

	// 3. Provenance stays unambiguous. Neither source, or both at once, is still
	//    refused — widening must not have become "anything goes".
	var noInvitation, noRequest *string
	historicalInvitation := directGrantInvitationID
	directRequest := directGrantRequestID
	for name, provenance := range map[string][2]*string{
		"no provenance at all": {noInvitation, noRequest},
		"both provenances":     {&historicalInvitation, &directRequest},
	} {
		_, err := pool.Exec(ctx, `
			INSERT INTO entitlements (
				id, student_account_id, scope_kind, scope_id, course_id, grant_source,
				source_invitation_id, source_purchase_request_id, original_access_ends_at,
				access_ends_at, retirement_eligibility_at, state, revoked_at, revision, created_at, updated_at
			) VALUES (
				'40000000-0000-0000-0000-000000000003'::uuid, $1::uuid, 'COURSE', $2::uuid, $2::uuid,
				'PURCHASE_REQUEST', $3::uuid, $4::uuid, now() + interval '30 days', now() + interval '30 days',
				now() + interval '60 days', 'REVOKED', now(), 1, now(), now()
			)
		`, directGrantStudentID, directGrantCourseID, provenance[0], provenance[1])
		if err == nil {
			t.Fatalf("a PURCHASE_REQUEST entitlement with %s was accepted", name)
		}
		if !strings.Contains(err.Error(), "ent_purchase_source_valid") {
			t.Fatalf("%s was refused by the wrong rule: %v", name, err)
		}
	}

	// 4. A grant source that never owned a purchase request still may not carry
	//    one. The bundle rule was widened for PURCHASE_REQUEST only.
	_, err := pool.Exec(ctx, `
		INSERT INTO entitlements (
			id, student_account_id, scope_kind, scope_id, course_id, grant_source,
			source_invitation_id, source_purchase_request_id, original_access_ends_at,
			access_ends_at, retirement_eligibility_at, state, revoked_at, revision, created_at, updated_at
		) VALUES (
			'40000000-0000-0000-0000-000000000004'::uuid, $1::uuid, 'COURSE', $2::uuid, $2::uuid,
			'MANUAL_INVITATION', $3::uuid, $4::uuid, now() + interval '30 days', now() + interval '30 days',
			now() + interval '60 days', 'REVOKED', now(), 1, now(), now()
		)
	`, directGrantStudentID, directGrantCourseID, directGrantInvitationID, directGrantRequestID)
	if err == nil || !strings.Contains(err.Error(), "ent_bundle_purchase_source_valid") {
		t.Fatalf("MANUAL_INVITATION carrying a purchase request was not refused by the bundle rule: %v", err)
	}

	// 5. A COURSE request may now be ACCESS_GRANTED with no invitation, and the
	//    payment evidence is still mandatory in that state.
	if _, err := pool.Exec(ctx, `
		UPDATE purchase_requests
		   SET state = 'ACCESS_GRANTED', payment_confirmed_by_account_id = $1::uuid,
		       payment_confirmed_at = now(), access_ends_at_snapshot = now() + interval '30 days',
		       access_granted_at = now()
		 WHERE id = $2::uuid
	`, directGrantAdminID, directGrantRequestID); err != nil {
		t.Fatalf("directly granted COURSE request was refused after 0044: %v", err)
	}
	_, err = pool.Exec(ctx, `
		UPDATE purchase_requests SET state = 'ACCESS_GRANTED', payment_confirmed_by_account_id = NULL,
		       payment_confirmed_at = NULL, access_granted_at = now()
		 WHERE id = $1::uuid
	`, directGrantRequestID)
	if err == nil || !strings.Contains(err.Error(), "purchase_requests_transition_coherent") {
		t.Fatalf("a granted request without payment evidence was accepted: %v", err)
	}

	// 6. Rolling back underneath live direct-grant data is refused rather than
	//    silently dropping access that schema 43 cannot represent.
	pool.Close()
	downErr := migrator.Steps(-1)
	if downErr == nil {
		t.Fatalf("0044 rolled back while a direct purchase entitlement existed")
	}
	if !strings.Contains(downErr.Error(), "cannot roll back 0044") {
		t.Fatalf("0044 rollback failed for an unexpected reason: %v", downErr)
	}
}

// With no direct-grant rows present, the rollback is allowed and restores the
// schema-43 rule, so the migration is genuinely reversible before it is used.
func TestDirectPurchaseGrantMigrationRollsBackWhenUnused(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, migrator := freshDirectGrantDatabase(t, ctx)
	pool.Close()

	if err := migrator.Steps(-1); err != nil {
		t.Fatalf("rolling back an unused 0044: %v", err)
	}
	verify, err := pgxpool.New(ctx, directGrantDSN)
	if err != nil {
		t.Fatalf("reopening after rollback: %v", err)
	}
	defer verify.Close()
	var restored, widened int
	if err := verify.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM pg_constraint WHERE conname = 'ent_purchase_needs_invitation'),
		  (SELECT count(*) FROM pg_constraint WHERE conname = 'ent_purchase_source_valid')
	`).Scan(&restored, &widened); err != nil {
		t.Fatalf("reading constraints after rollback: %v", err)
	}
	if restored != 1 || widened != 0 {
		t.Fatalf("after rollback schema-43 rule=%d widened rule=%d, want 1/0", restored, widened)
	}
}

const (
	directGrantAdminID        = "50000000-0000-0000-0000-000000000001"
	directGrantInstructorID   = "50000000-0000-0000-0000-000000000002"
	directGrantStudentID      = "50000000-0000-0000-0000-000000000003"
	directGrantOtherStudentID = "50000000-0000-0000-0000-000000000004"
	directGrantCourseID       = "60000000-0000-0000-0000-000000000001"
	directGrantRequestID      = "70000000-0000-0000-0000-000000000001"
	directGrantInvitationID   = "80000000-0000-0000-0000-000000000001"
	directGrantSecretID       = "90000000-0000-0000-0000-000000000001"
)

func freshDirectGrantDatabase(t *testing.T, ctx context.Context) (*pgxpool.Pool, *migrate.Migrate) {
	t.Helper()
	admin, err := pgxpool.New(ctx, migrateCommandAdminDSN)
	if err != nil {
		t.Fatalf("opening admin pool: %v", err)
	}
	t.Cleanup(func() { admin.Close() })
	for _, statement := range []string{
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='" + directGrantDBName + "'",
		"DROP DATABASE IF EXISTS " + directGrantDBName,
		"CREATE DATABASE " + directGrantDBName,
	} {
		if _, err := admin.Exec(ctx, statement); err != nil {
			t.Fatalf("preparing disposable database (%s): %v", statement, err)
		}
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = admin.Exec(cleanupCtx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='"+directGrantDBName+"'")
		_, _ = admin.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+directGrantDBName)
	})

	m, err := migrate.New("file://../../internal/db/migrations", directGrantDSN)
	if err != nil {
		t.Fatalf("opening migrator: %v", err)
	}
	t.Cleanup(func() { _, _ = m.Close() })
	if err := m.Up(); err != nil {
		t.Fatalf("migrating up: %v", err)
	}
	pool, err := pgxpool.New(ctx, directGrantDSN)
	if err != nil {
		t.Fatalf("opening pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool, m
}

func seedDirectGrantFixtures(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO accounts (id, normalized_email, email, role, status, display_name, email_verified_at) VALUES
			($1::uuid, 'dg-admin@example.com', 'dg-admin@example.com', 'ADMIN', 'ACTIVE', 'DG Admin', now()),
			($2::uuid, 'dg-instructor@example.com', 'dg-instructor@example.com', 'INSTRUCTOR', 'ACTIVE', 'DG Instructor', now()),
			($3::uuid, 'dg-student@example.com', 'dg-student@example.com', 'STUDENT', 'ACTIVE', 'DG Student', now()),
			($4::uuid, 'dg-other@example.com', 'dg-other@example.com', 'STUDENT', 'ACTIVE', 'DG Other', now())`,
			[]any{directGrantAdminID, directGrantInstructorID, directGrantStudentID, directGrantOtherStudentID}},
		{`INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1::uuid, $2::uuid, 'DRAFT')`,
			[]any{directGrantCourseID, directGrantInstructorID}},
		{`INSERT INTO identity_action_secrets (id, account_id, purpose, secret_digest, issued_at, expires_at)
		  VALUES ($1::uuid, NULL, 'COURSE_ACCESS_INVITATION', sha256('dg-seed'::bytea), now(), now() + interval '7 days')`,
			[]any{directGrantSecretID}},
		{`INSERT INTO course_access_invitations (
			id, normalized_email, email, course_id, created_by_account_id, state, action_secret_id,
			accepted_by_account_id, accepted_at, decided_by_account_id, decided_at, created_at
		  ) VALUES ($1::uuid, 'dg-other@example.com', 'dg-other@example.com', $2::uuid, $3::uuid, 'APPROVED', $4::uuid,
		            $5::uuid, now(), $3::uuid, now(), now())`,
			[]any{directGrantInvitationID, directGrantCourseID, directGrantAdminID, directGrantSecretID, directGrantOtherStudentID}},
		{`INSERT INTO purchase_requests (
			id, reference_code, course_id, email, normalized_email, requester_account_id,
			course_title_ar, course_title_en, price_minor_units, currency, state, requested_at, created_at, updated_at
		  ) VALUES ($1::uuid, 'GRX-DIRECTGRANT00001', $2::uuid, 'dg-student@example.com', 'dg-student@example.com',
		            $3::uuid, 'مقرر', 'Course', 25000, 'KWD', 'WAITING_PAYMENT', now(), now(), now())`,
			[]any{directGrantRequestID, directGrantCourseID, directGrantStudentID}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("seeding direct-grant fixture: %v", err)
		}
	}
}
