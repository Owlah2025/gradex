//go:build !production

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// T042 fixtures and the mid-session authority mutations they exist for.
//
// T042 proves that access ending *during* an authenticated session stops the next playback
// issuance and the next Progress write. Every scenario therefore begins fully authorised and is
// taken away mid-session — seeding a scenario as already unauthorised would be T043's evidence,
// not this one.
//
// Each condition gets its own Student so a mutation cannot leak into another scenario. Emergency
// Course access suspension additionally gets its own Course, because suspending a Course affects
// every Student enrolled in it and would otherwise contaminate the rest of the run.
const (
	accessEndsExpiryStudentID    = "a3000000-0000-0000-0000-000000000001"
	accessEndsRevokedStudentID   = "a3000000-0000-0000-0000-000000000002"
	accessEndsSuspendedStudentID = "a3000000-0000-0000-0000-000000000003"
	accessEndsEmergencyStudentID = "a3000000-0000-0000-0000-000000000004"

	accessEndsEmergencyCourseID         = "c3000000-0000-0000-0000-000000000001"
	accessEndsEmergencyRevisionID       = "f3000000-0000-0000-0000-000000000001"
	accessEndsEmergencySectionIdentity  = "13000000-0000-0000-0000-000000000001"
	accessEndsEmergencySectionID        = "23000000-0000-0000-0000-000000000001"
	accessEndsEmergencyLessonIdentityID = "33000000-0000-0000-0000-000000000001"
	accessEndsEmergencyLessonID         = "43000000-0000-0000-0000-000000000001"
	accessEndsEmergencyAssetID          = "53000000-0000-0000-0000-000000000001"
	accessEndsEmergencyAssetVersionID   = "63000000-0000-0000-0000-000000000001"
	accessEndsEmergencyScanID           = "73000000-0000-0000-0000-000000000001"
	accessEndsEmergencyProcID           = "83000000-0000-0000-0000-000000000001"
)

func accessEndsStudentEmail(slot int) string {
	return fmt.Sprintf("student-access-ends-%d@example.test", slot)
}

// seedAccessEndsScenarios creates four independently mutable authority bundles. Scenarios 1-3
// mutate Student-scoped authority and can therefore share the deterministic Course; scenario 4
// mutates Course-scoped authority and gets its own.
func seedAccessEndsScenarios(
	ctx context.Context,
	tx pgx.Tx,
	sharedCourseID string,
	sharedLessonIdentityID string,
	instructorID string,
	passwordHash string,
	now time.Time,
	accessEndsAt time.Time,
) error {
	if err := seedAccessEndsEmergencyCourse(ctx, tx, instructorID); err != nil {
		return err
	}

	scenarios := []struct {
		slot             int
		accountID        string
		displayName      string
		courseID         string
		lessonIdentityID string
	}{
		{1, accessEndsExpiryStudentID, "Access Ends Expiry Student", sharedCourseID, sharedLessonIdentityID},
		{2, accessEndsRevokedStudentID, "Access Ends Revocation Student", sharedCourseID, sharedLessonIdentityID},
		{3, accessEndsSuspendedStudentID, "Access Ends Suspension Student", sharedCourseID, sharedLessonIdentityID},
		{4, accessEndsEmergencyStudentID, "Access Ends Emergency Student", accessEndsEmergencyCourseID, accessEndsEmergencyLessonIdentityID},
	}

	for _, scenario := range scenarios {
		email := accessEndsStudentEmail(scenario.slot)
		enrollmentID := fmt.Sprintf("b3000000-0000-0000-0000-%012d", scenario.slot)
		entitlementID := fmt.Sprintf("e3000000-0000-0000-0000-%012d", scenario.slot)

		if _, err := tx.Exec(ctx, `
			INSERT INTO accounts (id, normalized_email, email, role, status, display_name, email_verified_at)
			VALUES ($1, $2, $2, 'STUDENT', 'ACTIVE', $3, $4)
		`, scenario.accountID, email, scenario.displayName, now); err != nil {
			return fmt.Errorf("insert access-ends student %d: %w", scenario.slot, err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO password_credentials (account_id, password_hash, state)
			VALUES ($1, $2, 'ACTIVE')
		`, scenario.accountID, passwordHash); err != nil {
			return fmt.Errorf("insert access-ends student %d credentials: %w", scenario.slot, err)
		}

		invID := uuid.NewString()
		if _, err := tx.Exec(ctx, `
			INSERT INTO course_access_invitations (id, course_id, email, normalized_email, created_by_account_id, accepted_by_account_id, decided_by_account_id, state)
			VALUES ($1, $2, 'student@example.test', 'student@example.test', $3, $3, $3, 'APPROVED')
		`, invID, scenario.courseID, scenario.accountID); err != nil {
			return fmt.Errorf("insert access-ends invitation %d: %w", scenario.slot, err)
		}

		// Active for real: a future access_ends_at, ACTIVE state, no revocation.
		if _, err := tx.Exec(ctx, `
			INSERT INTO entitlements (id, student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state)
			VALUES ($1, $2, 'COURSE', $3, $3, 'MANUAL_INVITATION', $4, $5, $5, $5, 'ACTIVE')
		`, entitlementID, scenario.accountID, scenario.courseID, invID, accessEndsAt); err != nil {
			return fmt.Errorf("insert access-ends entitlement %d: %w", scenario.slot, err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO enrollments (id, student_account_id, course_id)
			VALUES ($1, $2, $3)
		`, enrollmentID, scenario.accountID, scenario.courseID); err != nil {
			return fmt.Errorf("insert access-ends enrollment %d: %w", scenario.slot, err)
		}

		// An initial Progress row below the completion threshold, so the accepted baseline is a
		// real row whose immutability after denial is meaningful.
		if _, err := tx.Exec(ctx, `
			INSERT INTO progress (enrollment_id, course_lesson_identity_id, max_position_seconds, last_position_seconds, last_watched_at)
			VALUES ($1, $2, 4, 4, $3)
		`, enrollmentID, scenario.lessonIdentityID, now); err != nil {
			return fmt.Errorf("insert access-ends progress %d: %w", scenario.slot, err)
		}
	}

	return nil
}

// seedAccessEndsEmergencyCourse builds the dedicated Course whose access is suspended mid-session:
// an approved live revision, one section, one Lesson on a stable identity, and a READY video Asset
// Version bound to the same deterministic HLS fixture the other Lessons use.
func seedAccessEndsEmergencyCourse(ctx context.Context, tx pgx.Tx, instructorID string) error {
	steps := []struct {
		what string
		sql  string
		args []any
	}{
		{"course", `INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1, $2, 'DRAFT')`,
			[]any{accessEndsEmergencyCourseID, instructorID}},
		{"revision", `INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en, description_ar, description_en)
			VALUES ($1, $2, 'APPROVED', 1, 'دورة تعليق الوصول', 'Emergency Suspension Course', 'وصف', 'Course used for emergency access suspension evidence.')`,
			[]any{accessEndsEmergencyRevisionID, accessEndsEmergencyCourseID}},
		{"live revision", `UPDATE courses SET live_revision_id = $1, lifecycle = 'PUBLISHED' WHERE id = $2`,
			[]any{accessEndsEmergencyRevisionID, accessEndsEmergencyCourseID}},
		{"section identity", `INSERT INTO course_section_identities (id, course_id) VALUES ($1, $2)`,
			[]any{accessEndsEmergencySectionIdentity, accessEndsEmergencyCourseID}},
		{"section", `INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position)
			VALUES ($1, $2, $3, $4, 'القسم الأول', 'Section 1', 1)`,
			[]any{accessEndsEmergencySectionID, accessEndsEmergencyRevisionID, accessEndsEmergencyCourseID, accessEndsEmergencySectionIdentity}},
		{"lesson identity", `INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1, $2, $3)`,
			[]any{accessEndsEmergencyLessonIdentityID, accessEndsEmergencyCourseID, accessEndsEmergencySectionIdentity}},
		{"media asset", `INSERT INTO media_assets (id, kind, owner_account_id, course_id, visibility) VALUES ($1, 'VIDEO', $2, $3, 'PROTECTED')`,
			[]any{accessEndsEmergencyAssetID, instructorID, accessEndsEmergencyCourseID}},
		{"asset version", `INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes)
			VALUES ($1, $2, 'VIDEO', 'SCANNING', 'test/master.m3u8', 'emergency-v1', 'application/vnd.apple.mpegurl', 1048576)`,
			[]any{accessEndsEmergencyAssetVersionID, accessEndsEmergencyAssetID}},
		{"scan attempt", `INSERT INTO scan_attempts (id, asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity)
			VALUES ($1, $2, 1, 'work-emergency', 'emergency-v1', 'PASSED', 'test-scanner')`,
			[]any{accessEndsEmergencyScanID, accessEndsEmergencyAssetVersionID}},
		{"processing attempt", `INSERT INTO processing_attempts (id, asset_version_id, operation_id, state, output_prefix, rendition_count, trusted_duration_ms)
			VALUES ($1, $2, 'op-emergency', 'SUCCEEDED', 'output/', 2, 30000)`,
			[]any{accessEndsEmergencyProcID, accessEndsEmergencyAssetVersionID}},
		{"rendition", `INSERT INTO video_renditions (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms) VALUES ($1, '720p', 'test/master.m3u8', 1280, 720, 2800, 30000)`,
			[]any{accessEndsEmergencyAssetVersionID}},
		{"scan passed", `UPDATE media_asset_versions SET state = 'SCAN_PASSED', successful_scan_attempt_id = $2 WHERE id = $1`,
			[]any{accessEndsEmergencyAssetVersionID, accessEndsEmergencyScanID}},
		{"processing", `UPDATE media_asset_versions SET state = 'PROCESSING' WHERE id = $1`,
			[]any{accessEndsEmergencyAssetVersionID}},
		{"ready", `UPDATE media_asset_versions SET state = 'READY', successful_processing_attempt_id = $2, trusted_duration_ms = 30000 WHERE id = $1`,
			[]any{accessEndsEmergencyAssetVersionID, accessEndsEmergencyProcID}},
		{"lesson", `INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position, video_asset_version_id)
			VALUES ($1, $2, $3, $4, $5, 'الدرس الأول', 'Lesson 1: Emergency', 1, $6)`,
			[]any{accessEndsEmergencyLessonID, accessEndsEmergencySectionID, accessEndsEmergencyCourseID, accessEndsEmergencySectionIdentity, accessEndsEmergencyLessonIdentityID, accessEndsEmergencyAssetVersionID}},
	}

	for _, step := range steps {
		if _, err := tx.Exec(ctx, step.sql, step.args...); err != nil {
			return fmt.Errorf("emergency course %s: %w", step.what, err)
		}
	}
	return nil
}

// Mid-session authority mutation.
//
// These run test-runner-side against the isolated per-run database. Browser code never reaches
// the database, no production mutation endpoint is added, and production evaluation is untouched
// — the mutation only changes the authority rows the real evaluator then reads. Each operation is
// allowlisted and parameterised; no SQL text ever comes from a caller.
type accessMutationResult struct {
	Operation    string `json:"operation"`
	RowsAffected int64  `json:"rows_affected"`
	AppliedAt    string `json:"applied_at"`
}

type accessMutation struct {
	sql          string
	needsStudent bool
	needsCourse  bool
}

var accessMutations = map[string]accessMutation{
	// Expiry: the access window closes now. State stays ACTIVE — expiry is a time comparison the
	// evaluator makes, not a stored verdict.
	"expire-entitlement": {
		sql:          `UPDATE entitlements SET original_access_ends_at = $3, access_ends_at = $3 WHERE student_account_id = $1::uuid AND course_id = $2::uuid`,
		needsStudent: true, needsCourse: true,
	},
	"revoke-entitlement": {
		sql:          `UPDATE entitlements SET state = 'REVOKED', revoked_at = $3 WHERE student_account_id = $1::uuid AND course_id = $2::uuid`,
		needsStudent: true, needsCourse: true,
	},
	"suspend-account": {
		sql:          `UPDATE accounts SET status = 'SUSPENDED' WHERE id = $1::uuid`,
		needsStudent: true,
	},
	"emergency-suspend-course": {
		sql:         `UPDATE courses SET access_suspended_at = $2, access_suspension_reason = 'e2e-access-ends' WHERE id = $1::uuid`,
		needsCourse: true,
	},
}

func applyAccessMutation(ctx context.Context, pool *pgxpool.Pool, operation, studentID, courseID string) (accessMutationResult, error) {
	mutation, allowed := accessMutations[operation]
	if !allowed {
		return accessMutationResult{}, fmt.Errorf("mutation %q is not an allowlisted access-ending operation", operation)
	}
	if mutation.needsStudent {
		if err := requireFixtureUUID("student", studentID); err != nil {
			return accessMutationResult{}, err
		}
	}
	if mutation.needsCourse {
		if err := requireFixtureUUID("course", courseID); err != nil {
			return accessMutationResult{}, err
		}
	}

	now := time.Now().UTC()
	var tag interface{ RowsAffected() int64 }
	var err error
	switch {
	case mutation.needsStudent && mutation.needsCourse:
		tag, err = pool.Exec(ctx, mutation.sql, studentID, courseID, now)
	case mutation.needsStudent:
		tag, err = pool.Exec(ctx, mutation.sql, studentID)
	default:
		tag, err = pool.Exec(ctx, mutation.sql, courseID, now)
	}
	if err != nil {
		return accessMutationResult{}, fmt.Errorf("applying %s: %w", operation, err)
	}

	affected := tag.RowsAffected()
	// A mutation that matched nothing is a fixture error, never a silent success: the scenario
	// would then "pass" against authority that was never actually taken away.
	if affected != 1 {
		return accessMutationResult{}, fmt.Errorf("%s affected %d rows, expected exactly 1", operation, affected)
	}

	return accessMutationResult{Operation: operation, RowsAffected: affected, AppliedAt: now.Format(time.RFC3339Nano)}, nil
}

func requireFixtureUUID(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s identifier is required", name)
	}
	if _, err := uuid.Parse(value); err != nil {
		return fmt.Errorf("%s identifier %q is not a valid UUID", name, value)
	}
	return nil
}

func encodeAccessMutation(result accessMutationResult) ([]byte, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encoding mutation result: %w", err)
	}
	return encoded, nil
}
