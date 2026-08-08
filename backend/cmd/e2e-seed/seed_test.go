//go:build !production

package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/db/e2equery"
	"github.com/Owlah2025/gradex/backend/internal/db/e2esafety"
	"github.com/Owlah2025/gradex/backend/internal/identity"
)

const (
	defaultAdminDB   = "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable"
	defaultTargetDB  = "gradex_playwright_e2e"
	testPassword     = "StudentPassword123!"
	migrationsSource = "file://internal/db/migrations"
)

// validateSeedSafety is the single fail-closed gate every tool invocation passes through. It is a
// named function rather than an inline struct literal so invocation_test.go can prove it still
// refuses the application database, a database outside the E2E prefix, a remote host, and a missing
// reset acknowledgement.
func validateSeedSafety(allowReset, adminDSN, targetDSN, appDSN string) error {
	return e2esafety.ValidateE2EDatabaseTarget(e2esafety.SafetyConfig{
		AdminDSN:                adminDSN,
		TargetDSN:               targetDSN,
		AppDSN:                  appDSN,
		ResetAcknowledgementEnv: allowReset,
	})
}

func TestMain(m *testing.M) {
	ctx := context.Background()

	var dbName string
	var dropOnly bool
	var queryProgress bool
	var queryLearningState bool
	var studentIDParam string
	var lessonIDParam string
	var courseIDParam string
	var issueSessionFlag bool
	var emailParam string
	var accessMutationParam string
	var queryInvitationToken bool
	var invitationIDParam string
	flag.StringVar(&dbName, "dbname", "", "Target database name")
	flag.BoolVar(&dropOnly, "drop", false, "Drop target database and exit")
	flag.BoolVar(&queryProgress, "query-progress", false, "Query progress position for a student and lesson")
	flag.BoolVar(&queryLearningState, "query-learning-state", false, "Emit the Entitlement, Enrollment, Progress, and material snapshot for a student and course")
	flag.BoolVar(&queryInvitationToken, "query-invitation-token", false, "Emit the verification token for an invitation from outbox")
	flag.StringVar(&studentIDParam, "student", "", "Student ID for query")
	flag.StringVar(&lessonIDParam, "lesson", "", "Lesson ID for query")
	flag.StringVar(&courseIDParam, "course", "", "Course ID for query")
	flag.StringVar(&invitationIDParam, "invitation", "", "Invitation ID for query")
	flag.BoolVar(&issueSessionFlag, "issue-session", false, "Issue a production-valid session for a seeded Student and emit its cookie and CSRF token")
	flag.StringVar(&emailParam, "email", "", "Student email for session issuance")
	flag.StringVar(&accessMutationParam, "access-mutation", "", "Allowlisted mid-session authority mutation: expire-entitlement, revoke-entitlement, suspend-account, emergency-suspend-course")
	flag.Parse()

	// Is this the seeding tool, or an ordinary repository-wide `go test ./...`?
	//
	// This package is a tool built with `go test -c`, not a test suite, so TestMain used to resolve
	// DSNs and run the fail-closed safety validation unconditionally. Under `go test ./...` none of
	// the E2E contract is present, so that validation refused — correctly — and failed the whole
	// Backend job. The gate is not relaxed: it still runs on every invocation that asks for the
	// tool, including a partially configured one. Absent every signal, nothing environment-dependent
	// happens: no DSN is resolved, no connection opened, no database created, dropped, or migrated,
	// and no fixture seeded. See invocation_test.go.
	if !e2eToolInvocationRequested(seedInvocation{
		AllowReset:     os.Getenv("GRADEX_E2E_ALLOW_DATABASE_RESET"),
		TargetDBName:   os.Getenv("GRADEX_E2E_TARGET_DB_NAME"),
		TargetDSN:      os.Getenv("GRADEX_E2E_TARGET_DB_URL"),
		AdminDSN:       os.Getenv("GRADEX_E2E_ADMIN_DB_URL"),
		LegacyAdminDSN: os.Getenv("ADMIN_DATABASE_URL"),
		ToolFlagsSet:   countToolFlagsSet(flag.CommandLine),
	}) {
		os.Exit(m.Run())
	}

	targetDB := os.Getenv("GRADEX_E2E_TARGET_DB_NAME")
	if targetDB == "" {
		targetDB = dbName
	}
	if targetDB == "" {
		targetDB = defaultTargetDB
	}

	adminDSN := os.Getenv("GRADEX_E2E_ADMIN_DB_URL")
	if adminDSN == "" {
		adminDSN = os.Getenv("ADMIN_DATABASE_URL")
	}
	if adminDSN == "" {
		adminDSN = defaultAdminDB
	}

	targetDSN := os.Getenv("GRADEX_E2E_TARGET_DB_URL")
	if targetDSN == "" {
		targetDSN = fmt.Sprintf("postgres://gradex:gradex@localhost:5432/%s?sslmode=disable", targetDB)
	}

	appDSN := os.Getenv("DATABASE_URL")
	if appDSN == "" {
		appDSN = "postgres://gradex:gradex@localhost:5432/gradex?sslmode=disable"
	}

	allowReset := os.Getenv("GRADEX_E2E_ALLOW_DATABASE_RESET")

	// 1. Fail-closed safety validation
	if err := validateSeedSafety(allowReset, adminDSN, targetDSN, appDSN); err != nil {
		log.Fatalf("E2E database safety validation failed: %v", err)
	}

	if queryProgress {
		if studentIDParam == "" || courseIDParam == "" || lessonIDParam == "" {
			log.Fatalf("-query-progress requires -student, -course, and -lesson flags")
		}
		targetPool, err := pgxpool.New(ctx, targetDSN)
		if err != nil {
			log.Fatalf("connecting to target db for query: %v", err)
		}
		// Resolved through the authoritative Enrollment and stable Course Lesson Identity.
		// A query failure is fatal rather than a silent found:false, so a helper that cannot
		// address the schema can never be mistaken for an absent row.
		snapshot, err := e2equery.ReadProgress(ctx, targetPool, studentIDParam, courseIDParam, lessonIDParam)
		targetPool.Close()
		if err != nil {
			log.Fatalf("reading progress snapshot: %v", err)
		}
		encoded, err := json.Marshal(snapshot)
		if err != nil {
			log.Fatalf("encoding progress snapshot: %v", err)
		}
		fmt.Printf("%s", encoded)
		os.Exit(0)
	}

	if accessMutationParam != "" {
		targetPool, err := pgxpool.New(ctx, targetDSN)
		if err != nil {
			log.Fatalf("connecting to target db for access mutation: %v", err)
		}
		result, err := applyAccessMutation(ctx, targetPool, accessMutationParam, studentIDParam, courseIDParam)
		targetPool.Close()
		if err != nil {
			log.Fatalf("applying access mutation: %v", err)
		}
		encoded, err := encodeAccessMutation(result)
		if err != nil {
			log.Fatalf("encoding access mutation: %v", err)
		}
		fmt.Printf("%s", encoded)
		os.Exit(0)
	}

	if issueSessionFlag {
		if emailParam == "" {
			log.Fatalf("-issue-session requires -email")
		}
		session, err := issueSession(ctx, targetDSN, emailParam, testPassword)
		if err != nil {
			log.Fatalf("issuing session: %v", err)
		}
		encoded, err := encodeIssuedSession(session)
		if err != nil {
			log.Fatalf("encoding issued session: %v", err)
		}
		fmt.Printf("%s", encoded)
		os.Exit(0)
	}

	if queryLearningState {
		if studentIDParam == "" || courseIDParam == "" {
			log.Fatalf("-query-learning-state requires -student and -course flags")
		}
		targetPool, err := pgxpool.New(ctx, targetDSN)
		if err != nil {
			log.Fatalf("connecting to target db for learning-state query: %v", err)
		}
		snapshot, err := readLearningStateSnapshot(ctx, targetPool, studentIDParam, courseIDParam)
		targetPool.Close()
		if err != nil {
			log.Fatalf("reading learning state snapshot: %v", err)
		}
		encoded, err := json.Marshal(snapshot)
		if err != nil {
			log.Fatalf("encoding learning state snapshot: %v", err)
		}
		fmt.Printf("%s", encoded)
		os.Exit(0)
	}

	if queryInvitationToken {
		if invitationIDParam == "" {
			log.Fatalf("-query-invitation-token requires -invitation flag")
		}
		targetPool, err := pgxpool.New(ctx, targetDSN)
		if err != nil {
			log.Fatalf("connecting to target db for invitation token query: %v", err)
		}
		token, err := queryInvitationTokenFromOutbox(ctx, targetPool, invitationIDParam)
		targetPool.Close()
		if err != nil {
			log.Fatalf("reading invitation verification token: %v", err)
		}
		encoded, _ := json.Marshal(map[string]string{
			"invitation_id":      invitationIDParam,
			"verification_token": token,
		})
		fmt.Printf("%s", encoded)
		os.Exit(0)
	}

	quotedTargetDB, err := e2esafety.QuoteIdentifier(targetDB)
	if err != nil {
		log.Fatalf("invalid target database identifier %q: %v", targetDB, err)
	}

	if dropOnly {
		adminPool, err := pgxpool.New(ctx, adminDSN)
		if err != nil {
			log.Fatalf("connecting to admin db for drop: %v", err)
		}
		_, _ = adminPool.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()", targetDB)
		_, err = adminPool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", quotedTargetDB))
		adminPool.Close()
		if err != nil {
			log.Fatalf("dropping target db %s: %v", targetDB, err)
		}
		log.Printf("e2e database %s dropped successfully", targetDB)
		os.Exit(0)
	}

	// 2. Recreate target database safely
	adminPool, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		log.Fatalf("connecting to admin db: %v", err)
	}

	_, _ = adminPool.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()", targetDB)
	_, _ = adminPool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", quotedTargetDB))
	_, err = adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", quotedTargetDB))
	adminPool.Close()
	if err != nil {
		log.Fatalf("creating target db %s: %v", targetDB, err)
	}

	// 3. Run migrations 0001 -> 0014
	mig, err := migrate.New(migrationsSource, targetDSN)
	if err != nil {
		log.Fatalf("opening migrate instance: %v", err)
	}
	if err := mig.Up(); err != nil && err != migrate.ErrNoChange {
		mig.Close()
		log.Fatalf("applying migrations: %v", err)
	}
	mig.Close()

	// 4. Seed deterministic test data
	pool, err := pgxpool.New(ctx, targetDSN)
	if err != nil {
		log.Fatalf("connecting to seeded db: %v", err)
	}
	defer pool.Close()

	if err := seedFixtures(ctx, pool); err != nil {
		log.Fatalf("seeding fixtures: %v", err)
	}

	log.Printf("e2e database %s seeding completed successfully", targetDB)
	os.Exit(0)
}

// learningStateSnapshot is the test-runner-side view of the authority and learning state that a
// denial attempt must leave untouched. It carries no credential, no signed target, and no storage
// detail, and it is never handed to a browser context.
type learningStateSnapshot struct {
	Entitlement struct {
		Found                bool    `json:"found"`
		Count                int     `json:"count"`
		State                string  `json:"state"`
		AccessEndsAt         string  `json:"access_ends_at"`
		OriginalAccessEndsAt string  `json:"original_access_ends_at"`
		RevokedAt            *string `json:"revoked_at"`
		Revision             int64   `json:"revision"`
	} `json:"entitlement"`
	Enrollment struct {
		Found     bool   `json:"found"`
		Count     int    `json:"count"`
		CreatedAt string `json:"created_at"`
	} `json:"enrollment"`
	Progress               []learningProgressSnapshot `json:"progress"`
	MaterialKinds          map[string]int             `json:"material_kinds"`
	VideoAssetVersionState string                     `json:"video_asset_version_state"`
}

type learningProgressSnapshot struct {
	LessonIdentityID    string `json:"lesson_identity_id"`
	MaxPositionSeconds  int    `json:"max_position_seconds"`
	LastPositionSeconds int    `json:"last_position_seconds"`
	Completed           bool   `json:"completed"`
	CompletedAt         string `json:"completed_at"`
	UpdatedAt           string `json:"updated_at"`
}

func rfc3339OrEmpty(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func readLearningStateSnapshot(ctx context.Context, pool *pgxpool.Pool, studentID, courseID string) (learningStateSnapshot, error) {
	var snapshot learningStateSnapshot
	snapshot.Progress = []learningProgressSnapshot{}
	snapshot.MaterialKinds = map[string]int{}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM entitlements WHERE student_account_id = $1 AND course_id = $2`, studentID, courseID).Scan(&snapshot.Entitlement.Count); err != nil {
		return snapshot, fmt.Errorf("counting entitlements: %w", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM enrollments WHERE student_account_id = $1 AND course_id = $2`, studentID, courseID).Scan(&snapshot.Enrollment.Count); err != nil {
		return snapshot, fmt.Errorf("counting enrollments: %w", err)
	}

	var accessEndsAt, originalAccessEndsAt time.Time
	var revokedAt *time.Time
	err := pool.QueryRow(ctx, `
		SELECT state, access_ends_at, original_access_ends_at, revoked_at, revision
		FROM entitlements
		WHERE student_account_id = $1 AND course_id = $2
	`, studentID, courseID).Scan(
		&snapshot.Entitlement.State,
		&accessEndsAt,
		&originalAccessEndsAt,
		&revokedAt,
		&snapshot.Entitlement.Revision,
	)
	// Only a query that ran and matched nothing may report absence. Treating any error as
	// absence is how a helper lets an assertion pass without ever reading the database.
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return snapshot, fmt.Errorf("querying entitlement: %w", err)
	}
	if err == nil {
		snapshot.Entitlement.Found = true
		snapshot.Entitlement.AccessEndsAt = accessEndsAt.UTC().Format(time.RFC3339Nano)
		snapshot.Entitlement.OriginalAccessEndsAt = originalAccessEndsAt.UTC().Format(time.RFC3339Nano)
		if revokedAt != nil {
			formatted := rfc3339OrEmpty(revokedAt)
			snapshot.Entitlement.RevokedAt = &formatted
		}
	}

	var enrollmentID string
	var enrollmentCreatedAt time.Time
	err = pool.QueryRow(ctx, `
		SELECT id, created_at FROM enrollments WHERE student_account_id = $1 AND course_id = $2
	`, studentID, courseID).Scan(&enrollmentID, &enrollmentCreatedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return snapshot, fmt.Errorf("querying enrollment: %w", err)
	}
	if err == nil {
		snapshot.Enrollment.Found = true
		snapshot.Enrollment.CreatedAt = enrollmentCreatedAt.UTC().Format(time.RFC3339Nano)

		rows, queryErr := pool.Query(ctx, `
			SELECT course_lesson_identity_id, max_position_seconds, last_position_seconds, completed_at, updated_at
			FROM progress
			WHERE enrollment_id = $1
			ORDER BY course_lesson_identity_id
		`, enrollmentID)
		if queryErr != nil {
			return snapshot, fmt.Errorf("querying progress: %w", queryErr)
		}
		defer rows.Close()
		for rows.Next() {
			var row learningProgressSnapshot
			var completedAt *time.Time
			var updatedAt time.Time
			if scanErr := rows.Scan(&row.LessonIdentityID, &row.MaxPositionSeconds, &row.LastPositionSeconds, &completedAt, &updatedAt); scanErr != nil {
				return snapshot, fmt.Errorf("scanning progress: %w", scanErr)
			}
			row.Completed = completedAt != nil
			row.CompletedAt = rfc3339OrEmpty(completedAt)
			row.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
			snapshot.Progress = append(snapshot.Progress, row)
		}
		if rows.Err() != nil {
			return snapshot, fmt.Errorf("iterating progress: %w", rows.Err())
		}
	}

	materialRows, err := pool.Query(ctx, `
		SELECT lf.kind, count(*)
		FROM lesson_files lf
		JOIN course_lessons cl ON cl.id = lf.lesson_id
		WHERE cl.course_id = $1
		GROUP BY lf.kind
		ORDER BY lf.kind
	`, courseID)
	if err != nil {
		return snapshot, fmt.Errorf("querying lesson materials: %w", err)
	}
	defer materialRows.Close()
	for materialRows.Next() {
		var kind string
		var count int
		if err := materialRows.Scan(&kind, &count); err != nil {
			return snapshot, fmt.Errorf("scanning lesson materials: %w", err)
		}
		snapshot.MaterialKinds[kind] = count
	}
	if materialRows.Err() != nil {
		return snapshot, fmt.Errorf("iterating lesson materials: %w", materialRows.Err())
	}

	var videoState string
	err = pool.QueryRow(ctx, `
		SELECT mav.state
		FROM media_asset_versions mav
		JOIN media_assets ma ON ma.id = mav.logical_asset_id
		WHERE ma.course_id = $1 AND ma.kind = 'VIDEO'
		ORDER BY mav.id
		LIMIT 1
	`, courseID).Scan(&videoState)
	if err == nil {
		snapshot.VideoAssetVersionState = videoState
	}

	return snapshot, nil
}

func seedFixtures(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	passwordHash, err := identity.HashPassword(testPassword)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}

	now := time.Now().UTC()
	activeExpiry := now.Add(30 * 24 * time.Hour)
	// Truncated to a whole second so the retained expiry instant renders as an exact
	// RFC 3339 UTC value in every read model and in the machine-readable `<time>` attribute.
	expiredExpiry := now.Add(-24 * time.Hour).Truncate(time.Second)

	// Create Admin Account
	adminAccountID := "a0000000-0000-0000-0000-000000000000"
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name, email_verified_at)
		VALUES ($1, 'admin@example.test', 'admin@example.test', 'ADMIN', 'ACTIVE', 'System Admin', $2)
	`, adminAccountID, now)
	if err != nil {
		return fmt.Errorf("insert admin account: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO password_credentials (account_id, password_hash, state)
		VALUES ($1, $2, 'ACTIVE')
	`, adminAccountID, passwordHash.Expose())
	if err != nil {
		return fmt.Errorf("insert admin creds: %w", err)
	}

	// Create Student 1 (Active)
	student1ID := "a0000000-0000-0000-0000-000000000001"
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name, email_verified_at)
		VALUES ($1, 'student-active@example.test', 'student-active@example.test', 'STUDENT', 'ACTIVE', 'Active Student', $2)
	`, student1ID, now)
	if err != nil {
		return fmt.Errorf("insert student 1: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO password_credentials (account_id, password_hash, state)
		VALUES ($1, $2, 'ACTIVE')
	`, student1ID, passwordHash.Expose())
	if err != nil {
		return fmt.Errorf("insert student 1 creds: %w", err)
	}

	// Create Student 2 (Expired)
	student2ID := "a0000000-0000-0000-0000-000000000002"
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name, email_verified_at)
		VALUES ($1, 'student-expired@example.test', 'student-expired@example.test', 'STUDENT', 'ACTIVE', 'Expired Student', $2)
	`, student2ID, now)
	if err != nil {
		return fmt.Errorf("insert student 2: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO password_credentials (account_id, password_hash, state)
		VALUES ($1, $2, 'ACTIVE')
	`, student2ID, passwordHash.Expose())
	if err != nil {
		return fmt.Errorf("insert student 2 creds: %w", err)
	}

	// Create Student 3 (Unentitled)
	student3ID := "a0000000-0000-0000-0000-000000000099"
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name, email_verified_at)
		VALUES ($1, 'student-unentitled@example.test', 'student-unentitled@example.test', 'STUDENT', 'ACTIVE', 'Unentitled Student', $2)
	`, student3ID, now)
	if err != nil {
		return fmt.Errorf("insert student 3: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO password_credentials (account_id, password_hash, state)
		VALUES ($1, $2, 'ACTIVE')
	`, student3ID, passwordHash.Expose())
	if err != nil {
		return fmt.Errorf("insert student 3 creds: %w", err)
	}

	// Create Instructor
	instructorID := "a0000000-0000-0000-0000-000000000003"
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name, email_verified_at)
		VALUES ($1, 'instructor@example.test', 'instructor@example.test', 'INSTRUCTOR', 'ACTIVE', 'Dr. Instructor', $2)
	`, instructorID, now)
	if err != nil {
		return fmt.Errorf("insert instructor: %w", err)
	}

	// Create Course & Revision
	courseID := "c0000000-0000-0000-0000-000000000001"
	revisionID := "f0000000-0000-0000-0000-000000000001"
	_, err = tx.Exec(ctx, `
		INSERT INTO courses (id, owner_account_id, lifecycle)
		VALUES ($1, $2, 'DRAFT')
	`, courseID, instructorID)
	if err != nil {
		return fmt.Errorf("insert course: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en, description_ar, description_en)
		VALUES ($1, $2, 'APPROVED', 1, 'مقدمة في البرمجة CS101', 'CS101: Introduction to Programming', 'وصف الدورة بالعربية', 'Instructor-authored CS101 course description.')
	`, revisionID, courseID)
	if err != nil {
		return fmt.Errorf("insert revision: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE courses SET live_revision_id = $1, lifecycle = 'PUBLISHED' WHERE id = $2
	`, revisionID, courseID)
	if err != nil {
		return fmt.Errorf("update live revision: %w", err)
	}

	// Create Section Identities & Sections (out-of-order DB insertion to verify authored ordering)
	sectionIdentity1ID := "10000000-0000-0000-0000-000000000001"
	sectionIdentity2ID := "10000000-0000-0000-0000-000000000002"
	section1ID := "20000000-0000-0000-0000-000000000001"
	section2ID := "20000000-0000-0000-0000-000000000002"

	_, err = tx.Exec(ctx, `
		INSERT INTO course_section_identities (id, course_id)
		VALUES ($1, $3), ($2, $3)
	`, sectionIdentity1ID, sectionIdentity2ID, courseID)
	if err != nil {
		return fmt.Errorf("insert section identities: %w", err)
	}

	// Insert Section 2 before Section 1 to test non-natural DB insertion order
	_, err = tx.Exec(ctx, `
		INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position)
		VALUES
		  ($2, $3, $4, $6, 'القسم الثاني: البرمجة المتقدمة', 'Section 2: Advanced Topics', 2),
		  ($1, $3, $4, $5, 'القسم الأول: الأساسيات', 'Section 1: Basics', 1)
	`, section1ID, section2ID, revisionID, courseID, sectionIdentity1ID, sectionIdentity2ID)
	if err != nil {
		return fmt.Errorf("insert sections: %w", err)
	}

	// Create Lesson Identities & Lessons (out-of-order DB insertion)
	lessonIdentity1ID := "30000000-0000-0000-0000-000000000001"
	lessonIdentity2ID := "30000000-0000-0000-0000-000000000002"
	lessonIdentity3ID := "30000000-0000-0000-0000-000000000003"
	lesson1ID := "40000000-0000-0000-0000-000000000001"
	lesson2ID := "40000000-0000-0000-0000-000000000002"
	lesson3ID := "40000000-0000-0000-0000-000000000003"

	_, err = tx.Exec(ctx, `
		INSERT INTO course_lesson_identities (id, course_id, section_identity_id)
		VALUES
		  ($1, $4, $5),
		  ($2, $4, $5),
		  ($3, $4, $6)
	`, lessonIdentity1ID, lessonIdentity2ID, lessonIdentity3ID, courseID, sectionIdentity1ID, sectionIdentity2ID)
	if err != nil {
		return fmt.Errorf("insert lesson identities: %w", err)
	}

	// Create Media Asset & Asset Version for Lesson 1
	mediaAssetID := "50000000-0000-0000-0000-000000000001"
	assetVersionID := "60000000-0000-0000-0000-000000000001"
	scanAttemptID := "70000000-0000-0000-0000-000000000001"
	procAttemptID := "80000000-0000-0000-0000-000000000001"

	_, err = tx.Exec(ctx, `
		INSERT INTO media_assets (id, kind, owner_account_id, course_id, visibility)
		VALUES ($1, 'VIDEO', $2, $3, 'PROTECTED')
	`, mediaAssetID, instructorID, courseID)
	if err != nil {
		return fmt.Errorf("insert media asset: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes)
		VALUES ($1, $2, 'VIDEO', 'SCANNING', 'test/master.m3u8', 'v1', 'application/vnd.apple.mpegurl', 1048576)
	`, assetVersionID, mediaAssetID)
	if err != nil {
		return fmt.Errorf("insert asset version scanning: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO scan_attempts (id, asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity)
		VALUES ($1, $2, 1, 'work-1', 'v1', 'PASSED', 'test-scanner')
	`, scanAttemptID, assetVersionID)
	if err != nil {
		return fmt.Errorf("insert scan attempt: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO processing_attempts (id, asset_version_id, operation_id, state, output_prefix, rendition_count, trusted_duration_ms)
		VALUES ($1, $2, 'op-1', 'SUCCEEDED', 'output/', 2, 30000)
	`, procAttemptID, assetVersionID)
	if err != nil {
		return fmt.Errorf("insert processing attempt: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO video_renditions (asset_version_id, name, storage_object_key, duration_ms)
		VALUES ($1, '720p', 'test/master.m3u8', 30000)
	`, assetVersionID)
	if err != nil {
		return fmt.Errorf("insert video rendition: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE media_asset_versions SET state = 'SCAN_PASSED', successful_scan_attempt_id = $2 WHERE id = $1
	`, assetVersionID, scanAttemptID)
	if err != nil {
		return fmt.Errorf("update asset version scan_passed: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE media_asset_versions SET state = 'PROCESSING' WHERE id = $1
	`, assetVersionID)
	if err != nil {
		return fmt.Errorf("update asset version processing: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE media_asset_versions SET state = 'READY', successful_processing_attempt_id = $2, trusted_duration_ms = 30000 WHERE id = $1
	`, assetVersionID, procAttemptID)
	if err != nil {
		return fmt.Errorf("update asset version ready: %w", err)
	}

	// Insert Lessons in non-natural order (Lesson 2, Lesson 3, Lesson 1)
	_, err = tx.Exec(ctx, `
		INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position, video_asset_version_id)
		VALUES
		  ($2, $4, $6, $7, $9,  'الدرس الثاني: المتغيرات', 'Lesson 2: Variables', 2, $12),
		  ($3, $5, $6, $8, $10, 'الدرس الثالث: الدوال', 'Lesson 3: Functions', 1, NULL),
		  ($1, $4, $6, $7, $11, 'الدرس الأول: مرحباً بك', 'Lesson 1: Introduction', 1, $12)
	`, lesson1ID, lesson2ID, lesson3ID, section1ID, section2ID, courseID, sectionIdentity1ID, sectionIdentity2ID, lessonIdentity2ID, lessonIdentity3ID, lessonIdentity1ID, assetVersionID)
	if err != nil {
		return fmt.Errorf("insert lessons: %w", err)
	}

	// Insert Lesson Files (Resource and Lab Material attached to Lesson 1)
	resourceFileID := "f1000000-0000-0000-0000-000000000001"
	labFileID := "f1000000-0000-0000-0000-000000000002"
	resourceAssetID := "50000000-0000-0000-0000-000000000002"
	labAssetID := "50000000-0000-0000-0000-000000000003"
	resourceAssetVersionID := "60000000-0000-0000-0000-000000000002"
	labAssetVersionID := "60000000-0000-0000-0000-000000000003"
	resourceScanID := "70000000-0000-0000-0000-000000000002"
	labScanID := "70000000-0000-0000-0000-000000000003"
	resourceProcID := "80000000-0000-0000-0000-000000000002"
	labProcID := "80000000-0000-0000-0000-000000000003"

	// Create Resource Media Asset & READY Version
	_, err = tx.Exec(ctx, `
		INSERT INTO media_assets (id, kind, owner_account_id, course_id, visibility)
		VALUES ($1, 'RESOURCE', $2, $3, 'PROTECTED')
	`, resourceAssetID, instructorID, courseID)
	if err != nil {
		return fmt.Errorf("insert resource asset: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes)
		VALUES ($1, $2, 'RESOURCE', 'SCANNING', 'test/notes.pdf', 'v1', 'application/pdf', 2048576)
	`, resourceAssetVersionID, resourceAssetID)
	if err != nil {
		return fmt.Errorf("insert resource version: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO scan_attempts (id, asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity)
		VALUES ($1, $2, 1, 'work-2', 'v1', 'PASSED', 'test-scanner')
	`, resourceScanID, resourceAssetVersionID)
	if err != nil {
		return fmt.Errorf("insert resource scan attempt: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO processing_attempts (id, asset_version_id, operation_id, state, output_prefix, rendition_count, trusted_duration_ms)
		VALUES ($1, $2, 'op-2', 'SUCCEEDED', 'output/', 1, 0)
	`, resourceProcID, resourceAssetVersionID)
	if err != nil {
		return fmt.Errorf("insert resource proc attempt: %w", err)
	}

	_, err = tx.Exec(ctx, `UPDATE media_asset_versions SET state = 'SCAN_PASSED', successful_scan_attempt_id = $2 WHERE id = $1`, resourceAssetVersionID, resourceScanID)
	if err != nil {
		return fmt.Errorf("update resource scan_passed: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE media_asset_versions SET state = 'PROCESSING' WHERE id = $1`, resourceAssetVersionID)
	if err != nil {
		return fmt.Errorf("update resource processing: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE media_asset_versions SET state = 'READY', successful_processing_attempt_id = $2 WHERE id = $1`, resourceAssetVersionID, resourceProcID)
	if err != nil {
		return fmt.Errorf("update resource ready: %w", err)
	}

	// Create Lab Material Media Asset & READY Version
	_, err = tx.Exec(ctx, `
		INSERT INTO media_assets (id, kind, owner_account_id, course_id, visibility)
		VALUES ($1, 'LAB_MATERIAL', $2, $3, 'PROTECTED')
	`, labAssetID, instructorID, courseID)
	if err != nil {
		return fmt.Errorf("insert lab asset: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes)
		VALUES ($1, $2, 'LAB_MATERIAL', 'SCANNING', 'test/lab.zip', 'v1', 'application/zip', 5048576)
	`, labAssetVersionID, labAssetID)
	if err != nil {
		return fmt.Errorf("insert lab version: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO scan_attempts (id, asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity)
		VALUES ($1, $2, 1, 'work-3', 'v1', 'PASSED', 'test-scanner')
	`, labScanID, labAssetVersionID)
	if err != nil {
		return fmt.Errorf("insert lab scan attempt: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO processing_attempts (id, asset_version_id, operation_id, state, output_prefix, rendition_count, trusted_duration_ms)
		VALUES ($1, $2, 'op-3', 'SUCCEEDED', 'output/', 1, 0)
	`, labProcID, labAssetVersionID)
	if err != nil {
		return fmt.Errorf("insert lab proc attempt: %w", err)
	}

	_, err = tx.Exec(ctx, `UPDATE media_asset_versions SET state = 'SCAN_PASSED', successful_scan_attempt_id = $2 WHERE id = $1`, labAssetVersionID, labScanID)
	if err != nil {
		return fmt.Errorf("update lab scan_passed: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE media_asset_versions SET state = 'PROCESSING' WHERE id = $1`, labAssetVersionID)
	if err != nil {
		return fmt.Errorf("update lab processing: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE media_asset_versions SET state = 'READY', successful_processing_attempt_id = $2 WHERE id = $1`, labAssetVersionID, labProcID)
	if err != nil {
		return fmt.Errorf("update lab ready: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO lesson_files (id, lesson_id, kind, asset_version_id, display_name_ar, display_name_en, position)
		VALUES
		  ($1, $3, 'RESOURCE', $4, 'ملاحظات المحاضرة', 'Lecture Notes PDF', 1),
		  ($2, $3, 'LAB_MATERIAL', $5, 'كود المختبر', 'Lab Starter Code Zip', 1)
	`, resourceFileID, labFileID, lesson1ID, resourceAssetVersionID, labAssetVersionID)
	if err != nil {
		return fmt.Errorf("insert lesson files: %w", err)
	}

	// Create Entitlements
	entitlement1ID := "90000000-0000-0000-0000-000000000001"
	entitlement2ID := "90000000-0000-0000-0000-000000000002"

	inv1ID := "c0000000-0000-0000-0000-000000000001"
	inv2ID := "c0000000-0000-0000-0000-000000000002"

	_, err = tx.Exec(ctx, `
		INSERT INTO course_access_invitations (id, course_id, email, normalized_email, created_by_account_id, accepted_by_account_id, decided_by_account_id, state)
		VALUES ($1, $2, 'student1@example.test', 'student1@example.test', $3, $3, $3, 'APPROVED'),
		       ($4, $2, 'student2@example.test', 'student2@example.test', $5, $5, $5, 'APPROVED')
	`, inv1ID, courseID, student1ID, inv2ID, student2ID)
	if err != nil {
		return fmt.Errorf("insert seed invitations: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO entitlements (id, student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state)
		VALUES ($1, $2, 'COURSE', $3, $3, 'MANUAL_INVITATION', $4, $5, $5, $5, 'ACTIVE')
	`, entitlement1ID, student1ID, courseID, inv1ID, activeExpiry)
	if err != nil {
		return fmt.Errorf("insert entitlement 1: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO entitlements (id, student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state)
		VALUES ($1, $2, 'COURSE', $3, $3, 'MANUAL_INVITATION', $4, $5, $5, $5, 'ACTIVE')
	`, entitlement2ID, student2ID, courseID, inv2ID, expiredExpiry)
	if err != nil {
		return fmt.Errorf("insert entitlement 2: %w", err)
	}

	// Create Enrollments
	enrollment1ID := "b0000000-0000-0000-0000-000000000001"
	enrollment2ID := "b0000000-0000-0000-0000-000000000002"

	_, err = tx.Exec(ctx, `
		INSERT INTO enrollments (id, student_account_id, course_id)
		VALUES ($1, $2, $3), ($4, $5, $3)
	`, enrollment1ID, student1ID, courseID, enrollment2ID, student2ID)
	if err != nil {
		return fmt.Errorf("insert enrollments: %w", err)
	}

	// Create the rate-limit-safe rotating Student pool. Each repeated test execution is handed
	// its own Student so no execution inherits another's playback or Progress budget — or its
	// Progress rows.
	if err := seedRotatingStudents(ctx, tx, courseID, passwordHash.Expose(), now, activeExpiry, expiredExpiry); err != nil {
		return err
	}

	// T042's mid-session authority-ending scenarios. Every bundle starts fully authorised; the
	// test takes authority away while its browser session is open.
	if err := seedAccessEndsScenarios(ctx, tx, courseID, lessonIdentity1ID, instructorID, passwordHash.Expose(), now, activeExpiry); err != nil {
		return err
	}

	// Create Progress for Active Student (Lesson 1: Incomplete @ 5s; Lesson 2: Completed @ 300s).
	// Lesson 1 starts below the 90% completion threshold of the 30 s trusted duration, so a real
	// Progress write during the browser run can advance the monotonic maximum and the resume
	// point without crossing completion — which is what makes before/after evidence meaningful.
	_, err = tx.Exec(ctx, `
		INSERT INTO progress (enrollment_id, course_lesson_identity_id, max_position_seconds, last_position_seconds, last_watched_at)
		VALUES ($1, $2, 5, 5, $3)
	`, enrollment1ID, lessonIdentity1ID, now)
	if err != nil {
		return fmt.Errorf("insert progress 1: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO progress (enrollment_id, course_lesson_identity_id, max_position_seconds, last_position_seconds, completed_at, completing_asset_version_id, last_watched_at)
		VALUES ($1, $2, 300, 300, $3, $4, $3)
	`, enrollment1ID, lessonIdentity2ID, now, assetVersionID)
	if err != nil {
		return fmt.Errorf("insert progress 2: %w", err)
	}

	// Create Progress for Expired Student (Lesson 1: partial @ 120s; Lesson 2: completed @ 300s).
	// Both rows are durable history retained underneath the expired Entitlement; neither is an
	// authorisation input.
	_, err = tx.Exec(ctx, `
		INSERT INTO progress (enrollment_id, course_lesson_identity_id, max_position_seconds, last_position_seconds, last_watched_at)
		VALUES ($1, $2, 120, 120, $3)
	`, enrollment2ID, lessonIdentity1ID, now)
	if err != nil {
		return fmt.Errorf("insert progress 3: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO progress (enrollment_id, course_lesson_identity_id, max_position_seconds, last_position_seconds, completed_at, completing_asset_version_id, last_watched_at)
		VALUES ($1, $2, 300, 300, $3, $4, $3)
	`, enrollment2ID, lessonIdentity2ID, now, assetVersionID)
	if err != nil {
		return fmt.Errorf("insert progress 4: %w", err)
	}

	return tx.Commit(ctx)
}

func queryInvitationTokenFromOutbox(ctx context.Context, targetPool *pgxpool.Pool, invitationID string) (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", fmt.Errorf("loading config: %w", err)
	}

	admissionCfg := cfg.Admission()
	key := []byte(admissionCfg.ProtectedPayloadKey().Expose())

	var keyVersion string
	var nonce, ciphertext []byte
	var eventID, eventType, sourceModule, aggType, aggID string
	var schemaVer, aggRev int

	err = targetPool.QueryRow(ctx, `
		SELECT oe.id::text, oe.event_type, oe.schema_version, oe.source_module,
		       oe.aggregate_type, oe.aggregate_id::text, oe.aggregate_revision,
		       opp.key_version, opp.nonce, opp.ciphertext
		FROM outbox_events oe
		JOIN outbox_protected_payloads opp ON opp.event_id = oe.id
		WHERE oe.aggregate_id = $1
		ORDER BY oe.occurred_at DESC
		LIMIT 1
	`, invitationID).Scan(
		&eventID, &eventType, &schemaVer, &sourceModule,
		&aggType, &aggID, &aggRev,
		&keyVersion, &nonce, &ciphertext,
	)
	if err != nil {
		return "", fmt.Errorf("querying outbox protected payload: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("aead: %w", err)
	}

	aad, err := json.Marshal(struct {
		ID                string `json:"id"`
		Type              string `json:"type"`
		SchemaVersion     int    `json:"schema_version"`
		SourceModule      string `json:"source_module"`
		AggregateType     string `json:"aggregate_type"`
		AggregateID       string `json:"aggregate_id"`
		AggregateRevision int    `json:"aggregate_revision"`
	}{
		ID:                eventID,
		Type:              eventType,
		SchemaVersion:     schemaVer,
		SourceModule:      sourceModule,
		AggregateType:     aggType,
		AggregateID:       aggID,
		AggregateRevision: aggRev,
	})
	if err != nil {
		return "", fmt.Errorf("marshaling aad: %w", err)
	}

	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return "", fmt.Errorf("decrypting payload: %w", err)
	}

	var delivery struct {
		VerificationToken string `json:"verification_token"`
	}
	if err := json.Unmarshal(plaintext, &delivery); err != nil {
		return "", fmt.Errorf("unmarshaling delivery: %w", err)
	}
	return delivery.VerificationToken, nil
}
