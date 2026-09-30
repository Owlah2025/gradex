//go:build !production

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Dedicated V2 browser fixtures. These rows are disposable and are never used by
// the older acceptance suites, so every mutation below has one owner and one
// starting state regardless of Playwright file ordering.
const (
	v2StudentID     = "a7000000-0000-0000-0000-000000000001"
	v2AdminTargetID = "a7000000-0000-0000-0000-000000000002"
	v2GrantTargetID = "a7000000-0000-0000-0000-000000000003"
	v2InstructorID  = "a7000000-0000-0000-0000-000000000004"
	uxjPendingID    = "a7000000-0000-0000-0000-000000000005"

	v2StudentEmail     = "v2-student@example.test"
	v2AdminTargetEmail = "v2-admin-target@example.test"
	v2GrantTargetEmail = "v2-grant-target@example.test"
	v2InstructorEmail  = "v2-instructor@example.test"
	uxjPendingEmail    = "uxj-pending@example.test"

	v2CourseID          = "c7000000-0000-0000-0000-000000000001"
	v2RevisionID        = "f7000000-0000-0000-0000-000000000001"
	v2SectionIdentityID = "17000000-0000-0000-0000-000000000001"
	v2SectionID         = "27000000-0000-0000-0000-000000000001"
	v2LessonIdentity1ID = "37000000-0000-0000-0000-000000000001"
	v2LessonIdentity2ID = "37000000-0000-0000-0000-000000000002"
	v2Lesson1ID         = "47000000-0000-0000-0000-000000000001"
	v2Lesson2ID         = "47000000-0000-0000-0000-000000000002"
	v2Asset1ID          = "57000000-0000-0000-0000-000000000001"
	v2Asset2ID          = "57000000-0000-0000-0000-000000000002"
	v2AssetVersion1ID   = "67000000-0000-0000-0000-000000000001"
	v2AssetVersion2ID   = "67000000-0000-0000-0000-000000000002"
	v2Scan1ID           = "77000000-0000-0000-0000-000000000001"
	v2Scan2ID           = "77000000-0000-0000-0000-000000000002"
	v2Processing1ID     = "87000000-0000-0000-0000-000000000001"
	v2Processing2ID     = "87000000-0000-0000-0000-000000000002"

	v2DraftCourseID   = "c7000000-0000-0000-0000-000000000002"
	v2DraftRevisionID = "f7000000-0000-0000-0000-000000000002"

	v2StudentInvitationID     = "c7000000-0000-0000-0000-000000000101"
	v2AdminTargetInviteID     = "c7000000-0000-0000-0000-000000000102"
	uxjPendingInvitationID    = "c7000000-0000-0000-0000-000000000103"
	v2GrantPendingInviteID    = "c7000000-0000-0000-0000-000000000104"
	v2StudentEntitlementID    = "e7000000-0000-0000-0000-000000000001"
	v2AdminTargetEntitlement  = "e7000000-0000-0000-0000-000000000002"
	v2StudentEnrollmentID     = "b7000000-0000-0000-0000-000000000001"
	v2AdminTargetEnrollmentID = "b7000000-0000-0000-0000-000000000002"
)

func seedV2Fixtures(
	ctx context.Context,
	tx pgx.Tx,
	adminID, passwordHash string,
	now, accessEndsAt time.Time,
) error {
	accounts := []struct {
		id, email, role, name string
	}{
		{v2StudentID, v2StudentEmail, "STUDENT", "V2 Student"},
		{v2AdminTargetID, v2AdminTargetEmail, "STUDENT", "V2 Admin Target"},
		{v2GrantTargetID, v2GrantTargetEmail, "STUDENT", "V2 Grant Target"},
		{v2InstructorID, v2InstructorEmail, "INSTRUCTOR", "V2 Instructor"},
		{uxjPendingID, uxjPendingEmail, "STUDENT", "UXJ Pending Student"},
	}
	for _, account := range accounts {
		if _, err := tx.Exec(ctx, `
			INSERT INTO accounts (id, normalized_email, email, role, status, display_name, email_verified_at)
			VALUES ($1::uuid, $2, $2, $3, 'ACTIVE', $4, $5)`,
			account.id, account.email, account.role, account.name, now); err != nil {
			return fmt.Errorf("insert V2 account %s: %w", account.email, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO password_credentials (account_id, password_hash, state)
			VALUES ($1::uuid, $2, 'ACTIVE')`, account.id, passwordHash); err != nil {
			return fmt.Errorf("insert V2 credentials %s: %w", account.email, err)
		}
	}

	if err := seedV2CompletionCourse(ctx, tx, v2InstructorID, adminID, accessEndsAt); err != nil {
		return err
	}
	if err := seedV2ApprovedAccess(ctx, tx, v2StudentID, v2StudentEmail, v2StudentInvitationID, v2StudentEntitlementID, v2StudentEnrollmentID, adminID, now, accessEndsAt); err != nil {
		return err
	}
	if err := seedV2ApprovedAccess(ctx, tx, v2AdminTargetID, v2AdminTargetEmail, v2AdminTargetInviteID, v2AdminTargetEntitlement, v2AdminTargetEnrollmentID, adminID, now, accessEndsAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO course_access_invitations (
			id, normalized_email, email, course_id, created_by_account_id,
			accepted_by_account_id, state, admin_note, created_at, accepted_at
		) VALUES ($1::uuid, $2, $2, $3::uuid, $4::uuid, $5::uuid,
			'PENDING_ADMIN_APPROVAL', 'Dedicated V2 grant fixture', $6, $6)`,
		v2GrantPendingInviteID, v2GrantTargetEmail, v2CourseID, adminID, v2GrantTargetID, now.Add(-2*time.Minute)); err != nil {
		return fmt.Errorf("seed V2 pending grant invitation: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO progress (enrollment_id, course_lesson_identity_id, max_position_seconds, last_position_seconds, last_watched_at)
		VALUES ($1::uuid, $2::uuid, 12, 12, $3)`, v2AdminTargetEnrollmentID, v2LessonIdentity1ID, now); err != nil {
		return fmt.Errorf("seed V2 Admin progress: %w", err)
	}

	// The Instructor journey owns a separate empty candidate. Its first review
	// submission is therefore independent of every existing authoring fixture.
	if _, err := tx.Exec(ctx, `
		INSERT INTO courses (id, owner_account_id, lifecycle, classification_model, institution_id, subject_id)
		VALUES ($1::uuid, $2::uuid, 'DRAFT', 'ACADEMIC_CATALOG', $3::uuid, $4::uuid)`,
		v2DraftCourseID, v2InstructorID,
		"91000000-0000-0000-0000-000000000001",
		"92000000-0000-0000-0000-000000000004"); err != nil {
		return fmt.Errorf("insert V2 draft course: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en, description_ar, description_en)
		VALUES ($1::uuid, $2::uuid, 'DRAFT', 1, 'مسودة V2 للتأليف', 'V2 Instructor Authoring Draft', 'وصف مسودة V2', 'Dedicated V2 authoring fixture.')`,
		v2DraftRevisionID, v2DraftCourseID); err != nil {
		return fmt.Errorf("insert V2 draft revision: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO course_price_changes (course_id, new_value_minor_units, changed_by_account_id, reason)
		VALUES ($1::uuid, 15000, $2::uuid, 'V2 instructor review fixture price')`, v2DraftCourseID, adminID); err != nil {
		return fmt.Errorf("price V2 draft course: %w", err)
	}

	// UX-J needs a row that is pending before the browser opens the queue. It is
	// deliberately unrelated to the V2 grant target and is never auto-approved.
	if _, err := tx.Exec(ctx, `
		INSERT INTO course_access_invitations (
			id, normalized_email, email, course_id, created_by_account_id,
			accepted_by_account_id, state, admin_note, created_at
		) VALUES ($1::uuid, $2, $2, $3::uuid, $4::uuid, $5::uuid,
			'PENDING_ADMIN_APPROVAL', 'Dedicated UX-J phone-task fixture', $6)`,
		uxjPendingInvitationID, uxjPendingEmail, v2CourseID, adminID, uxjPendingID, now.Add(-time.Minute)); err != nil {
		return fmt.Errorf("seed UX-J pending access request: %w", err)
	}
	return nil
}

func seedV2CompletionCourse(ctx context.Context, tx pgx.Tx, instructorID, adminID string, accessEndsAt time.Time) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO courses (id, owner_account_id, lifecycle, default_access_ends_at)
		VALUES ($1::uuid, $2::uuid, 'DRAFT', $3)`, v2CourseID, instructorID, accessEndsAt); err != nil {
		return fmt.Errorf("insert V2 completion course: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en, description_ar, description_en)
		VALUES ($1::uuid, $2::uuid, 'APPROVED', 1, 'مقرر الإكمال V2', 'V2 Completion Course', 'وصف مقرر الإكمال', 'Two-lesson durable completion fixture.')`,
		v2RevisionID, v2CourseID); err != nil {
		return fmt.Errorf("insert V2 completion revision: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE courses SET lifecycle = 'PUBLISHED', live_revision_id = $1::uuid WHERE id = $2::uuid`, v2RevisionID, v2CourseID); err != nil {
		return fmt.Errorf("publish V2 completion course: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO course_price_changes (course_id, new_value_minor_units, changed_by_account_id, reason)
		VALUES ($1::uuid, 18000, $2::uuid, 'V2 completion fixture price')`, v2CourseID, adminID); err != nil {
		return fmt.Errorf("price V2 completion course: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO course_section_identities (id, course_id)
		VALUES ($1::uuid, $2::uuid)`, v2SectionIdentityID, v2CourseID); err != nil {
		return fmt.Errorf("insert V2 section identity: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'أساسيات الإكمال', 'Completion Foundations', 1)`,
		v2SectionID, v2RevisionID, v2CourseID, v2SectionIdentityID); err != nil {
		return fmt.Errorf("insert V2 section: %w", err)
	}
	for _, lesson := range []struct {
		identity, id, titleAr, titleEn, asset, version, scan, processing string
	}{
		{v2LessonIdentity1ID, v2Lesson1ID, "الدرس الأول", "V2 Progress Lesson", v2Asset1ID, v2AssetVersion1ID, v2Scan1ID, v2Processing1ID},
		{v2LessonIdentity2ID, v2Lesson2ID, "الدرس الثاني", "V2 Completion Lesson", v2Asset2ID, v2AssetVersion2ID, v2Scan2ID, v2Processing2ID},
	} {
		if _, err := tx.Exec(ctx, `
			INSERT INTO course_lesson_identities (id, course_id, section_identity_id)
			VALUES ($1::uuid, $2::uuid, $3::uuid)`, lesson.identity, v2CourseID, v2SectionIdentityID); err != nil {
			return fmt.Errorf("insert V2 lesson identity %s: %w", lesson.titleEn, err)
		}
		if err := seedV2ReadyVideo(ctx, tx, instructorID, v2CourseID, lesson.asset, lesson.version, lesson.scan, lesson.processing); err != nil {
			return err
		}
		position := 1
		if lesson.identity == v2LessonIdentity2ID {
			position = 2
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position, video_asset_version_id)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, $7, $8, $9::uuid)`,
			lesson.id, v2SectionID, v2CourseID, v2SectionIdentityID, lesson.identity, lesson.titleAr, lesson.titleEn, position, lesson.version); err != nil {
			return fmt.Errorf("insert V2 lesson %s: %w", lesson.titleEn, err)
		}
	}
	return nil
}

func seedV2ReadyVideo(ctx context.Context, tx pgx.Tx, ownerID, courseID, assetID, versionID, scanID, processingID string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO media_assets (id, kind, owner_account_id, course_id, visibility)
		VALUES ($1::uuid, 'VIDEO', $2::uuid, $3::uuid, 'PROTECTED')`, assetID, ownerID, courseID); err != nil {
		return fmt.Errorf("insert V2 media asset %s: %w", assetID, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes)
		VALUES ($1::uuid, $2::uuid, 'VIDEO', 'SCANNING', $3, 'v1', 'application/vnd.apple.mpegurl', 1048576)`,
		versionID, assetID, "v2/"+versionID+"/master.m3u8"); err != nil {
		return fmt.Errorf("insert V2 media version %s: %w", versionID, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO scan_attempts (id, asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity)
		VALUES ($1::uuid, $2::uuid, 1, $3, 'v1', 'PASSED', 'v2-test-scanner')`,
		scanID, versionID, "v2-scan-"+versionID); err != nil {
		return fmt.Errorf("insert V2 scan attempt %s: %w", scanID, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO processing_attempts (id, asset_version_id, operation_id, state, output_prefix, rendition_count, trusted_duration_ms)
		VALUES ($1::uuid, $2::uuid, $3, 'SUCCEEDED', $4, 1, 30000)`,
		processingID, versionID, "v2-process-"+versionID, "v2/"+versionID+"/renditions/"); err != nil {
		return fmt.Errorf("insert V2 processing attempt %s: %w", processingID, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO video_renditions (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms)
		VALUES ($1::uuid, '720p', $2, 1280, 720, 2800, 30000)`,
		versionID, "v2/"+versionID+"/renditions/720p.m3u8"); err != nil {
		return fmt.Errorf("insert V2 rendition %s: %w", versionID, err)
	}
	if _, err := tx.Exec(ctx, `UPDATE media_asset_versions SET state = 'SCAN_PASSED', successful_scan_attempt_id = $2::uuid WHERE id = $1::uuid`, versionID, scanID); err != nil {
		return fmt.Errorf("mark V2 scan passed %s: %w", versionID, err)
	}
	if _, err := tx.Exec(ctx, `UPDATE media_asset_versions SET state = 'PROCESSING' WHERE id = $1::uuid`, versionID); err != nil {
		return fmt.Errorf("mark V2 processing %s: %w", versionID, err)
	}
	if _, err := tx.Exec(ctx, `UPDATE media_asset_versions SET state = 'READY', successful_processing_attempt_id = $2::uuid, trusted_duration_ms = 30000 WHERE id = $1::uuid`, versionID, processingID); err != nil {
		return fmt.Errorf("mark V2 ready %s: %w", versionID, err)
	}
	return nil
}

func seedV2ApprovedAccess(
	ctx context.Context,
	tx pgx.Tx,
	studentID, email, invitationID, entitlementID, enrollmentID, adminID string,
	now, accessEndsAt time.Time,
) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO course_access_invitations (
			id, course_id, email, normalized_email, created_by_account_id,
			accepted_by_account_id, decided_by_account_id, state, created_at, accepted_at, decided_at
		) VALUES ($1::uuid, $2::uuid, $3, $3, $4::uuid, $5::uuid, $4::uuid, 'APPROVED', $6, $6, $6)`,
		invitationID, v2CourseID, email, adminID, studentID, now); err != nil {
		return fmt.Errorf("insert V2 approved invitation %s: %w", email, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO entitlements (
			id, student_account_id, scope_kind, scope_id, course_id, grant_source,
			source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state
		) VALUES ($1::uuid, $2::uuid, 'COURSE', $3::uuid, $3::uuid, 'MANUAL_INVITATION', $4::uuid, $5, $5, $5, 'ACTIVE')`,
		entitlementID, studentID, v2CourseID, invitationID, accessEndsAt); err != nil {
		return fmt.Errorf("insert V2 entitlement %s: %w", email, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO enrollments (id, student_account_id, course_id)
		VALUES ($1::uuid, $2::uuid, $3::uuid)`, enrollmentID, studentID, v2CourseID); err != nil {
		return fmt.Errorf("insert V2 enrollment %s: %w", email, err)
	}
	return nil
}
