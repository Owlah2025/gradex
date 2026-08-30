//go:build !production

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/identity"
)

const (
	betaFixtureSchemaVersion      = 2
	betaRegisteredAccountCount    = 110
	betaStudentCount              = 104
	betaEntitledStudentCount      = 50
	betaLoginIdentityCount        = 100
	betaAdminCount                = 1
	betaInstructorCount           = 5
	betaPublishedCourseCount      = 8
	betaSectionsPerCourse         = 2
	betaLessonsPerCourse          = 4
	betaVideoDurationMilliseconds = 30000
	betaStoragePrefixEnvironment  = "GRADEX_LOADTEST_STORAGE_PREFIX"
	betaRunIDEnvironment          = "GRADEX_LOADTEST_RUN_ID"
)

var betaRunIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{2,63}$`)

type betaCourseFixture struct {
	CourseID         string   `json:"course_id"`
	RevisionID       string   `json:"revision_id"`
	OwnerAccountID   string   `json:"owner_account_id"`
	LessonID         string   `json:"lesson_id"`
	AssetVersionID   string   `json:"asset_version_id"`
	StorageObjectKey string   `json:"storage_object_key"`
	SectionIDs       []string `json:"section_ids"`
	LessonIDs        []string `json:"lesson_ids"`
}

type betaStudentFixture struct {
	Index          int    `json:"index"`
	AccountID      string `json:"account_id"`
	Email          string `json:"email"`
	Entitled       bool   `json:"entitled"`
	CourseID       string `json:"course_id,omitempty"`
	RevisionID     string `json:"revision_id,omitempty"`
	LessonID       string `json:"lesson_id,omitempty"`
	AssetVersionID string `json:"asset_version_id,omitempty"`
}

type betaOperatorFixture struct {
	Role      string   `json:"role"`
	Index     int      `json:"index"`
	AccountID string   `json:"account_id"`
	Email     string   `json:"email"`
	CourseIDs []string `json:"course_ids"`
}

type betaFixtureManifest struct {
	SchemaVersion      int                   `json:"schema_version"`
	Profile            string                `json:"profile"`
	RunID              string                `json:"run_id"`
	RegisteredAccounts int                   `json:"registered_accounts"`
	Students           []betaStudentFixture  `json:"students"`
	Courses            []betaCourseFixture   `json:"courses"`
	Operators          []betaOperatorFixture `json:"operators"`
	Fingerprint        string                `json:"fingerprint"`
}

type betaSeedContext struct {
	adminID       string
	instructorID  []string
	courses       []betaCourseFixture
	passwordHash  string
	now           time.Time
	expiresAt     time.Time
	storagePrefix string
}

func seedBetaLoadtestFixtures(ctx context.Context, pool *pgxpool.Pool, password string) (betaFixtureManifest, error) {
	manifest := betaFixtureManifest{}
	if password == "" {
		return manifest, fmt.Errorf("beta load-test password is required")
	}
	storagePrefix, runID, err := betaStorageScope()
	if err != nil {
		return manifest, err
	}
	passwordHash, err := identity.HashPassword(password)
	if err != nil {
		return manifest, fmt.Errorf("hashing beta load-test password: %w", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return manifest, fmt.Errorf("beginning beta fixture transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	now := time.Now().UTC()
	seed := betaSeedContext{
		adminID:      betaAccountID("a2100000", 0),
		instructorID: make([]string, betaInstructorCount),
		passwordHash: passwordHash.Expose(),
		now:          now, expiresAt: now.Add(30 * 24 * time.Hour), storagePrefix: storagePrefix,
	}
	for index := range seed.instructorID {
		seed.instructorID[index] = betaAccountID("a2200000", index)
	}
	if err := insertBetaAccounts(ctx, tx, seed); err != nil {
		return manifest, err
	}
	if err := insertBetaCourses(ctx, tx, seed); err != nil {
		return manifest, err
	}
	if err := insertBetaStudentAccess(ctx, tx, seed); err != nil {
		return manifest, err
	}
	if err := verifyBetaFixtureCardinality(ctx, tx); err != nil {
		return manifest, err
	}
	if err := tx.Commit(ctx); err != nil {
		return manifest, fmt.Errorf("committing beta fixture transaction: %w", err)
	}
	manifest = newBetaFixtureManifest(runID, seed)
	return withBetaFingerprint(manifest)
}

func betaStorageScope() (string, string, error) {
	runID := os.Getenv(betaRunIDEnvironment)
	if !betaRunIDPattern.MatchString(runID) {
		return "", "", fmt.Errorf("%s must be a valid run id", betaRunIDEnvironment)
	}
	want := "capacity/" + runID + "/"
	storagePrefix := os.Getenv(betaStoragePrefixEnvironment)
	if storagePrefix == "" {
		storagePrefix = want
	}
	if storagePrefix != want {
		return "", "", fmt.Errorf("%s must equal %q", betaStoragePrefixEnvironment, want)
	}
	return storagePrefix, runID, nil
}

func insertBetaAccounts(ctx context.Context, tx pgx.Tx, seed betaSeedContext) error {
	if err := insertBetaAccount(ctx, tx, seed.adminID, "admin-beta@example.test", "Beta Admin", "ADMIN", seed.passwordHash, seed.now); err != nil {
		return err
	}
	for index, accountID := range seed.instructorID {
		if err := insertBetaAccount(ctx, tx, accountID, betaInstructorEmail(index), fmt.Sprintf("Beta Instructor %02d", index), "INSTRUCTOR", seed.passwordHash, seed.now); err != nil {
			return err
		}
	}
	for index := 0; index < betaStudentCount; index++ {
		if err := insertBetaAccount(ctx, tx, betaAccountID("a2300000", index), betaStudentEmail(index), fmt.Sprintf("Beta Student %03d", index), "STUDENT", seed.passwordHash, seed.now); err != nil {
			return err
		}
	}
	return nil
}

func insertBetaAccount(ctx context.Context, tx pgx.Tx, accountID, email, displayName, role, passwordHash string, now time.Time) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name, email_verified_at)
		VALUES ($1, $2, $2, $3::account_role, 'ACTIVE', $4, $5)
	`, accountID, email, role, displayName, now); err != nil {
		return fmt.Errorf("inserting beta %s account: %w", role, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO password_credentials (account_id, password_hash, state)
		VALUES ($1, $2, 'ACTIVE')
	`, accountID, passwordHash); err != nil {
		return fmt.Errorf("inserting beta %s credential: %w", role, err)
	}
	return nil
}

func insertBetaCourses(ctx context.Context, tx pgx.Tx, seed betaSeedContext) error {
	seed.courses = make([]betaCourseFixture, 0, betaPublishedCourseCount)
	for courseIndex := 0; courseIndex < betaPublishedCourseCount; courseIndex++ {
		courseID := betaAccountID("c2100000", courseIndex)
		revisionID := betaAccountID("d2100000", courseIndex)
		ownerID := seed.instructorID[courseIndex%len(seed.instructorID)]
		if _, err := tx.Exec(ctx, `INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1, $2, 'DRAFT')`, courseID, ownerID); err != nil {
			return fmt.Errorf("inserting beta Course %d: %w", courseIndex, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en, description_ar, description_en)
			VALUES ($1, $2, 'APPROVED', 1, $3, $4, $5, $6)
		`, revisionID, courseID, fmt.Sprintf("دورة بيتا %02d", courseIndex), fmt.Sprintf("Beta Course %02d", courseIndex), "وصف بيتا تجريبي", "Synthetic beta capacity fixture course."); err != nil {
			return fmt.Errorf("inserting beta revision %d: %w", courseIndex, err)
		}
		if _, err := tx.Exec(ctx, `UPDATE courses SET live_revision_id = $1, lifecycle = 'PUBLISHED' WHERE id = $2`, revisionID, courseID); err != nil {
			return fmt.Errorf("publishing beta Course %d: %w", courseIndex, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO course_price_changes (course_id, new_value_minor_units, changed_by_account_id, reason)
			VALUES ($1, 25000, $2, 'Synthetic limited-paid-beta fixture')
		`, courseID, seed.adminID); err != nil {
			return fmt.Errorf("pricing beta Course %d: %w", courseIndex, err)
		}
		course, err := insertBetaCourseGraph(ctx, tx, seed, courseIndex, courseID, revisionID, ownerID)
		if err != nil {
			return err
		}
		seed.courses = append(seed.courses, course)
	}
	return nil
}

func insertBetaCourseGraph(ctx context.Context, tx pgx.Tx, seed betaSeedContext, courseIndex int, courseID, revisionID, ownerID string) (betaCourseFixture, error) {
	sectionIDs := make([]string, betaSectionsPerCourse)
	lessonIDs := make([]string, betaLessonsPerCourse)
	sectionIdentityIDs := make([]string, betaSectionsPerCourse)
	lessonIdentityIDs := make([]string, betaLessonsPerCourse)
	for sectionIndex := 0; sectionIndex < betaSectionsPerCourse; sectionIndex++ {
		sectionIdentityIDs[sectionIndex] = betaAccountID("e2100000", courseIndex*betaSectionsPerCourse+sectionIndex)
		sectionIDs[sectionIndex] = betaAccountID("e2200000", courseIndex*betaSectionsPerCourse+sectionIndex)
		if _, err := tx.Exec(ctx, `INSERT INTO course_section_identities (id, course_id) VALUES ($1, $2)`, sectionIdentityIDs[sectionIndex], courseID); err != nil {
			return betaCourseFixture{}, fmt.Errorf("inserting beta Section identity: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, sectionIDs[sectionIndex], revisionID, courseID, sectionIdentityIDs[sectionIndex], fmt.Sprintf("القسم %d", sectionIndex+1), fmt.Sprintf("Section %d", sectionIndex+1), sectionIndex+1); err != nil {
			return betaCourseFixture{}, fmt.Errorf("inserting beta Section: %w", err)
		}
	}

	for lessonIndex := 0; lessonIndex < betaLessonsPerCourse; lessonIndex++ {
		sectionIndex := lessonIndex / 2
		lessonIdentityIDs[lessonIndex] = betaAccountID("e2300000", courseIndex*betaLessonsPerCourse+lessonIndex)
		lessonIDs[lessonIndex] = betaAccountID("e2400000", courseIndex*betaLessonsPerCourse+lessonIndex)
		if _, err := tx.Exec(ctx, `
			INSERT INTO course_lesson_identities (id, course_id, section_identity_id)
			VALUES ($1, $2, $3)
		`, lessonIdentityIDs[lessonIndex], courseID, sectionIdentityIDs[sectionIndex]); err != nil {
			return betaCourseFixture{}, fmt.Errorf("inserting beta Lesson identity: %w", err)
		}
	}

	videoLessonID := lessonIDs[0]
	assetID := betaAccountID("51000000", courseIndex)
	assetVersionID := betaAccountID("61000000", courseIndex)
	scanAttemptID := betaAccountID("71000000", courseIndex)
	processingAttemptID := betaAccountID("81000000", courseIndex)
	storageKey := fmt.Sprintf("%scourse-%02d/master.m3u8", seed.storagePrefix, courseIndex)
	if _, err := tx.Exec(ctx, `
		INSERT INTO media_assets (id, kind, owner_account_id, course_id, lesson_id, visibility)
		VALUES ($1, 'VIDEO', $2, $3, $4, 'PROTECTED')
	`, assetID, ownerID, courseID, videoLessonID); err != nil {
		return betaCourseFixture{}, fmt.Errorf("inserting beta media asset: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes)
		VALUES ($1, $2, 'VIDEO', 'SCANNING', $3, 'v1', 'application/vnd.apple.mpegurl', 1048576)
	`, assetVersionID, assetID, storageKey); err != nil {
		return betaCourseFixture{}, fmt.Errorf("inserting beta media version: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO scan_attempts (id, asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity)
		VALUES ($1, $2, 1, $3, 'v1', 'PASSED', 'limited-paid-beta-fixture')
	`, scanAttemptID, assetVersionID, fmt.Sprintf("beta-scan-%02d", courseIndex)); err != nil {
		return betaCourseFixture{}, fmt.Errorf("inserting beta scan evidence: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO processing_attempts (id, asset_version_id, operation_id, state, output_prefix, rendition_count, trusted_duration_ms)
		VALUES ($1, $2, $3, 'SUCCEEDED', $4, 1, $5)
	`, processingAttemptID, assetVersionID, fmt.Sprintf("beta-transcode-%02d", courseIndex), seed.storagePrefix, betaVideoDurationMilliseconds); err != nil {
		return betaCourseFixture{}, fmt.Errorf("inserting beta processing evidence: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO video_renditions (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms)
		VALUES ($1, '720p', $2, 1280, 720, 2800, $3)
	`, assetVersionID, storageKey, betaVideoDurationMilliseconds); err != nil {
		return betaCourseFixture{}, fmt.Errorf("inserting beta rendition: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE media_asset_versions SET state = 'SCAN_PASSED', successful_scan_attempt_id = $2 WHERE id = $1`, assetVersionID, scanAttemptID); err != nil {
		return betaCourseFixture{}, fmt.Errorf("marking beta media scan passed: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE media_asset_versions SET state = 'PROCESSING' WHERE id = $1`, assetVersionID); err != nil {
		return betaCourseFixture{}, fmt.Errorf("marking beta media processing: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE media_asset_versions SET state = 'READY', successful_processing_attempt_id = $2, trusted_duration_ms = $3 WHERE id = $1`, assetVersionID, processingAttemptID, betaVideoDurationMilliseconds); err != nil {
		return betaCourseFixture{}, fmt.Errorf("marking beta media ready: %w", err)
	}

	for lessonIndex := 0; lessonIndex < betaLessonsPerCourse; lessonIndex++ {
		sectionIndex := lessonIndex / 2
		videoVersion := any(nil)
		if lessonIndex == 0 {
			videoVersion = assetVersionID
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position, video_asset_version_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`, lessonIDs[lessonIndex], sectionIDs[sectionIndex], courseID, sectionIdentityIDs[sectionIndex], lessonIdentityIDs[lessonIndex], fmt.Sprintf("الدرس %d", lessonIndex+1), fmt.Sprintf("Lesson %d", lessonIndex+1), lessonIndex%2+1, videoVersion); err != nil {
			return betaCourseFixture{}, fmt.Errorf("inserting beta Lesson: %w", err)
		}
	}
	return betaCourseFixture{
		CourseID: courseID, RevisionID: revisionID, OwnerAccountID: ownerID, LessonID: videoLessonID,
		AssetVersionID: assetVersionID, StorageObjectKey: storageKey, SectionIDs: sectionIDs, LessonIDs: lessonIDs,
	}, nil
}

func insertBetaStudentAccess(ctx context.Context, tx pgx.Tx, seed betaSeedContext) error {
	for index := 0; index < betaEntitledStudentCount; index++ {
		studentID := betaAccountID("a2300000", index)
		course := seed.courses[index%len(seed.courses)]
		invitationID := betaAccountID("c2200000", index)
		if _, err := tx.Exec(ctx, `
			INSERT INTO course_access_invitations (id, normalized_email, email, course_id, created_by_account_id, decided_by_account_id, accepted_by_account_id, state, created_at, accepted_at, decided_at)
			VALUES ($1, $2, $2, $3, $4, $4, $5, 'APPROVED', $6, $6, $6)
		`, invitationID, betaStudentEmail(index), course.CourseID, seed.adminID, studentID, seed.now); err != nil {
			return fmt.Errorf("inserting beta access invitation %d: %w", index, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO entitlements (id, student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state)
			VALUES ($1, $2, 'COURSE', $3, $3, 'MANUAL_INVITATION', $4, $5, $5, $5, 'ACTIVE')
		`, betaAccountID("92000000", index), studentID, course.CourseID, invitationID, seed.expiresAt); err != nil {
			return fmt.Errorf("inserting beta entitlement %d: %w", index, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO enrollments (id, student_account_id, course_id) VALUES ($1, $2, $3)`, betaAccountID("b2100000", index), studentID, course.CourseID); err != nil {
			return fmt.Errorf("inserting beta enrollment %d: %w", index, err)
		}
	}
	return nil
}

func verifyBetaFixtureCardinality(ctx context.Context, tx pgx.Tx) error {
	checks := []struct {
		name  string
		query string
		args  []any
		want  int
	}{
		{"accounts", `SELECT count(*) FROM accounts`, nil, betaRegisteredAccountCount},
		{"students", `SELECT count(*) FROM accounts WHERE role = 'STUDENT'`, nil, betaStudentCount},
		{"instructors", `SELECT count(*) FROM accounts WHERE role = 'INSTRUCTOR'`, nil, betaInstructorCount},
		{"admins", `SELECT count(*) FROM accounts WHERE role = 'ADMIN'`, nil, betaAdminCount},
		{"courses", `SELECT count(*) FROM courses WHERE lifecycle = 'PUBLISHED'`, nil, betaPublishedCourseCount},
		{"entitlements", `SELECT count(*) FROM entitlements WHERE id::text LIKE '92000000-%'`, nil, betaEntitledStudentCount},
		{"enrollments", `SELECT count(*) FROM enrollments WHERE id::text LIKE 'b2100000-%'`, nil, betaEntitledStudentCount},
		{"ready videos", `SELECT count(*) FROM media_asset_versions WHERE id::text LIKE '61000000-%' AND state = 'READY'`, nil, betaPublishedCourseCount},
	}
	for _, check := range checks {
		var got int
		if err := tx.QueryRow(ctx, check.query, check.args...).Scan(&got); err != nil {
			return fmt.Errorf("checking beta %s: %w", check.name, err)
		}
		if got != check.want {
			return fmt.Errorf("beta %s = %d, want %d", check.name, got, check.want)
		}
	}
	return nil
}

func newBetaFixtureManifest(runID string, seed betaSeedContext) betaFixtureManifest {
	manifest := betaFixtureManifest{
		SchemaVersion:      betaFixtureSchemaVersion,
		Profile:            "limited-paid-beta",
		RunID:              runID,
		RegisteredAccounts: betaRegisteredAccountCount,
		Courses:            append([]betaCourseFixture(nil), seed.courses...),
		Operators:          []betaOperatorFixture{{Role: "ADMIN", Index: 0, AccountID: seed.adminID, Email: "admin-beta@example.test"}},
	}
	for index, instructorID := range seed.instructorID {
		courseIDs := make([]string, 0)
		for _, course := range seed.courses {
			if course.OwnerAccountID == instructorID {
				courseIDs = append(courseIDs, course.CourseID)
			}
		}
		manifest.Operators = append(manifest.Operators, betaOperatorFixture{Role: "INSTRUCTOR", Index: index, AccountID: instructorID, Email: betaInstructorEmail(index), CourseIDs: courseIDs})
	}
	manifest.Students = make([]betaStudentFixture, 0, betaStudentCount)
	for index := 0; index < betaStudentCount; index++ {
		student := betaStudentFixture{Index: index, AccountID: betaAccountID("a2300000", index), Email: betaStudentEmail(index), Entitled: index < betaEntitledStudentCount}
		if student.Entitled {
			course := seed.courses[index%len(seed.courses)]
			student.CourseID, student.RevisionID, student.LessonID, student.AssetVersionID = course.CourseID, course.RevisionID, course.LessonID, course.AssetVersionID
		}
		manifest.Students = append(manifest.Students, student)
	}
	return manifest
}

func withBetaFingerprint(manifest betaFixtureManifest) (betaFixtureManifest, error) {
	manifest.Fingerprint = ""
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return betaFixtureManifest{}, fmt.Errorf("encoding beta fixture fingerprint input: %w", err)
	}
	digest := sha256.Sum256(encoded)
	manifest.Fingerprint = "sha256:" + hex.EncodeToString(digest[:])
	return manifest, nil
}

func betaAccountID(prefix string, index int) string {
	return fmt.Sprintf("%s-0000-0000-0000-%012d", prefix, index)
}

func betaStudentEmail(index int) string { return fmt.Sprintf("student-beta-%03d@example.test", index) }

func betaInstructorEmail(index int) string {
	return fmt.Sprintf("instructor-beta-%02d@example.test", index)
}

func TestBetaFixtureManifestHasExactAccountAndCohortMath(t *testing.T) {
	seed := betaSeedContext{adminID: betaAccountID("a2100000", 0), instructorID: make([]string, betaInstructorCount), courses: make([]betaCourseFixture, betaPublishedCourseCount)}
	manifest := newBetaFixtureManifest("run-20260824", seed)
	if manifest.RegisteredAccounts != betaRegisteredAccountCount || len(manifest.Students) != betaStudentCount || len(manifest.Operators) != betaAdminCount+betaInstructorCount || len(manifest.Courses) != betaPublishedCourseCount {
		t.Fatalf("beta fixture shape = accounts %d, students %d, operators %d, courses %d", manifest.RegisteredAccounts, len(manifest.Students), len(manifest.Operators), len(manifest.Courses))
	}
	entitled := 0
	accounts := map[string]bool{}
	for _, student := range manifest.Students {
		if student.Entitled {
			entitled++
		}
		accounts[student.AccountID] = true
	}
	if entitled != betaEntitledStudentCount || len(accounts) != betaStudentCount {
		t.Fatalf("beta student cohorts entitled=%d unique=%d", entitled, len(accounts))
	}
}
