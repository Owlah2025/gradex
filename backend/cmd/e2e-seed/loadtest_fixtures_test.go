//go:build !production

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/identity"
)

const (
	loadtestRegisteredAccountTarget = 5000
	loadtestActiveStudentCount      = 500
	loadtestCourseID                = "c0000000-0000-0000-0000-000000000001"
	loadtestLessonID                = "30000000-0000-0000-0000-000000000001"
	loadtestAssetVersionID          = "60000000-0000-0000-0000-000000000001"
	loadtestAdminID                 = "a0000000-0000-0000-0000-000000000000"
)

type loadtestStudent struct {
	Index     int    `json:"index"`
	AccountID string `json:"account_id"`
	Email     string `json:"email"`
}

type loadtestFixtureManifest struct {
	SchemaVersion      int               `json:"schema_version"`
	RegisteredAccounts int               `json:"registered_accounts"`
	ActiveStudents     int               `json:"active_students"`
	CourseID           string            `json:"course_id"`
	LessonID           string            `json:"lesson_id"`
	AssetVersionID     string            `json:"asset_version_id"`
	Students           []loadtestStudent `json:"students"`
}

type loadtestSeedValues struct {
	additionalAccounts int
	passwordHash       string
	now                time.Time
	expiresAt          time.Time
}

func loadtestStudentID(index int) string {
	return fmt.Sprintf("a9000000-0000-0000-0000-%012d", index)
}

func loadtestStudentEmail(index int) string {
	return fmt.Sprintf("student-loadtest-%04d@example.test", index)
}

func newLoadtestFixtureManifest() loadtestFixtureManifest {
	students := make([]loadtestStudent, loadtestActiveStudentCount)
	for index := range students {
		students[index] = loadtestStudent{
			Index: index, AccountID: loadtestStudentID(index), Email: loadtestStudentEmail(index),
		}
	}
	return loadtestFixtureManifest{
		SchemaVersion: 1, RegisteredAccounts: loadtestRegisteredAccountTarget,
		ActiveStudents: loadtestActiveStudentCount, CourseID: loadtestCourseID,
		LessonID: loadtestLessonID, AssetVersionID: loadtestAssetVersionID, Students: students,
	}
}

// seedLoadtestFixtures extends only an already-created disposable E2E database. The shared E2E
// safety gate runs before this function can be reached, so no remote or application database is a
// valid target. Existing functional fixtures count toward the 5,000-account baseline; at least 500
// deterministic Students are then entitled to the real published Course and exact READY video.
func seedLoadtestFixtures(ctx context.Context, pool *pgxpool.Pool, password string) (loadtestFixtureManifest, error) {
	manifest := newLoadtestFixtureManifest()
	if password == "" {
		return manifest, fmt.Errorf("load-test password is required")
	}
	passwordHash, err := identity.HashPassword(password)
	if err != nil {
		return manifest, fmt.Errorf("hashing load-test password: %w", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return manifest, fmt.Errorf("beginning load-test fixture transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	now := time.Now().UTC()
	additionalAccounts, err := loadtestAccountCapacity(ctx, tx)
	if err != nil {
		return manifest, err
	}
	values := loadtestSeedValues{
		additionalAccounts: additionalAccounts,
		passwordHash:       passwordHash.Expose(),
		now:                now,
		expiresAt:          now.Add(30 * 24 * time.Hour),
	}
	if err := insertLoadtestAccounts(ctx, tx, values); err != nil {
		return manifest, err
	}
	if err := insertLoadtestAccess(ctx, tx, values.expiresAt); err != nil {
		return manifest, err
	}
	if err := verifyLoadtestFixtureCardinality(ctx, tx); err != nil {
		return manifest, err
	}
	if err := tx.Commit(ctx); err != nil {
		return manifest, fmt.Errorf("committing load-test fixtures: %w", err)
	}
	return manifest, nil
}

func loadtestAccountCapacity(ctx context.Context, tx pgx.Tx) (int, error) {
	var existingAccounts int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM accounts`).Scan(&existingAccounts); err != nil {
		return 0, fmt.Errorf("counting existing fixture accounts: %w", err)
	}
	additionalAccounts := loadtestRegisteredAccountTarget - existingAccounts
	if additionalAccounts < loadtestActiveStudentCount {
		return 0, fmt.Errorf("existing fixtures leave room for only %d load-test accounts", additionalAccounts)
	}
	return additionalAccounts, nil
}

func insertLoadtestAccounts(ctx context.Context, tx pgx.Tx, values loadtestSeedValues) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name, email_verified_at)
		SELECT ('a9000000-0000-0000-0000-' || lpad(i::text, 12, '0'))::uuid,
		       'student-loadtest-' || lpad(i::text, 4, '0') || '@example.test',
		       'student-loadtest-' || lpad(i::text, 4, '0') || '@example.test',
		       'STUDENT', 'ACTIVE', 'Load Test Student ' || lpad(i::text, 4, '0'), $2
		FROM generate_series(0, $1 - 1) AS i
	`, values.additionalAccounts, values.now); err != nil {
		return fmt.Errorf("inserting load-test accounts: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO password_credentials (account_id, password_hash, state)
		SELECT id, $1, 'ACTIVE' FROM accounts
		WHERE normalized_email LIKE 'student-loadtest-%@example.test'
	`, values.passwordHash); err != nil {
		return fmt.Errorf("inserting load-test credentials: %w", err)
	}
	return nil
}

func insertLoadtestAccess(ctx context.Context, tx pgx.Tx, expiresAt time.Time) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO course_access_invitations
		  (id, course_id, email, normalized_email, created_by_account_id, accepted_by_account_id, decided_by_account_id, state)
		SELECT ('d9000000-0000-0000-0000-' || lpad(i::text, 12, '0'))::uuid,
		       $1::uuid, a.email, a.normalized_email, $2::uuid, a.id, $2::uuid, 'APPROVED'
		FROM generate_series(0, $3 - 1) AS i
		JOIN accounts a ON a.id = ('a9000000-0000-0000-0000-' || lpad(i::text, 12, '0'))::uuid
	`, loadtestCourseID, loadtestAdminID, loadtestActiveStudentCount); err != nil {
		return fmt.Errorf("inserting load-test invitations: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO entitlements
		  (id, student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id,
		   original_access_ends_at, access_ends_at, retirement_eligibility_at, state)
		SELECT ('e9000000-0000-0000-0000-' || lpad(i::text, 12, '0'))::uuid, a.id,
		       'COURSE', $1::uuid, $1::uuid, 'MANUAL_INVITATION',
		       ('d9000000-0000-0000-0000-' || lpad(i::text, 12, '0'))::uuid,
		       $3, $3, $3, 'ACTIVE'
		FROM generate_series(0, $2 - 1) AS i
		JOIN accounts a ON a.id = ('a9000000-0000-0000-0000-' || lpad(i::text, 12, '0'))::uuid
	`, loadtestCourseID, loadtestActiveStudentCount, expiresAt); err != nil {
		return fmt.Errorf("inserting load-test entitlements: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO enrollments (id, student_account_id, course_id)
		SELECT ('b9000000-0000-0000-0000-' || lpad(i::text, 12, '0'))::uuid, a.id, $1::uuid
		FROM generate_series(0, $2 - 1) AS i
		JOIN accounts a ON a.id = ('a9000000-0000-0000-0000-' || lpad(i::text, 12, '0'))::uuid
	`, loadtestCourseID, loadtestActiveStudentCount); err != nil {
		return fmt.Errorf("inserting load-test enrollments: %w", err)
	}
	return nil
}

func verifyLoadtestFixtureCardinality(ctx context.Context, tx pgx.Tx) error {
	var accountCount, entitlementCount, enrollmentCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM accounts`).Scan(&accountCount); err != nil {
		return fmt.Errorf("verifying load-test account count: %w", err)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM entitlements WHERE id::text LIKE 'e9000000-%'`).Scan(&entitlementCount); err != nil {
		return fmt.Errorf("verifying load-test entitlement count: %w", err)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM enrollments WHERE id::text LIKE 'b9000000-%'`).Scan(&enrollmentCount); err != nil {
		return fmt.Errorf("verifying load-test enrollment count: %w", err)
	}
	if accountCount != loadtestRegisteredAccountTarget || entitlementCount != loadtestActiveStudentCount || enrollmentCount != loadtestActiveStudentCount {
		return fmt.Errorf("load-test fixture cardinality accounts=%d entitlements=%d enrollments=%d", accountCount, entitlementCount, enrollmentCount)
	}
	return nil
}

func TestLoadtestManifestHasFiveHundredUniqueStudentsAndNoCredentialFields(t *testing.T) {
	manifest := newLoadtestFixtureManifest()
	if manifest.RegisteredAccounts != 5000 || manifest.ActiveStudents != 500 || len(manifest.Students) != 500 {
		t.Fatalf("unexpected load-test cardinality: %+v", manifest)
	}
	seen := make(map[string]bool, len(manifest.Students))
	for index, student := range manifest.Students {
		if student.Index != index || student.AccountID == "" || student.Email == "" || seen[student.AccountID] {
			t.Fatalf("invalid load-test Student %d: %+v", index, student)
		}
		seen[student.AccountID] = true
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bytes.ToLower(encoded), []byte("password")) || bytes.Contains(bytes.ToLower(encoded), []byte("cookie")) {
		t.Fatal("non-secret load-test manifest contains a credential field")
	}
}
