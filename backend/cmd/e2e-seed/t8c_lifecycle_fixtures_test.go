//go:build !production

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// T8C / AD-12 lifecycle fixtures.
//
// # ONE COURSE PER LIFECYCLE ACTION
//
// Retirement and archival are terminal: a retired Course cannot be retired again, and an archived
// Course can transition nowhere. Chaining the four browser cases onto one Course would therefore
// make case order load-bearing and make a single failure cascade. Each case owns a Course instead.
//
// # THE FIXTURE ESTABLISHES PREREQUISITES, NEVER THE OUTCOME
//
// Every Course here is seeded PUBLISHED with an approved live revision, a section and a lesson —
// the ordinary product state a lifecycle decision is taken against. No fixture sets
// `lifecycle = 'DELISTED'`, `access_suspended_at`, or `retired_at`: those are exactly the
// mutations the Admin UI under test has to perform.
const (
	t8cDelistCourseID     = "c8000000-0000-0000-0000-000000000001"
	t8cSuspensionCourseID = "c8000000-0000-0000-0000-000000000002"
	t8cRetirementCourseID = "c8000000-0000-0000-0000-000000000003"
	t8cArchiveCourseID    = "c8000000-0000-0000-0000-000000000004"

	t8cDelistCourseTitleEn     = "T8C Delist Relist Course"
	t8cSuspensionCourseTitleEn = "T8C Access Suspension Course"
	t8cRetirementCourseTitleEn = "T8C Retirement Course"
	t8cArchiveCourseTitleEn    = "T8C Archive Course"

	// The entitled Student for the suspension case. It is its own Account rather than a rotating
	// slot: the case proves a Course-level authority, so the Student must be bound to the T8C
	// suspension Course and to nothing else.
	t8cSuspensionStudentID    = "a0000000-0000-0000-0000-0000000008c0"
	t8cSuspensionStudentEmail = "t8c-suspension-student@example.test"
)

type t8cLifecycleCourse struct {
	courseID string
	titleAr  string
	titleEn  string
}

func seedT8CLifecycleFixtures(
	ctx context.Context,
	tx pgx.Tx,
	instructorID string,
	adminAccountID string,
	passwordHash string,
	now time.Time,
	accessEndsAt time.Time,
) error {
	courses := []t8cLifecycleCourse{
		{t8cDelistCourseID, "دورة T8C لإلغاء الإدراج وإعادته", t8cDelistCourseTitleEn},
		{t8cSuspensionCourseID, "دورة T8C لإيقاف الوصول", t8cSuspensionCourseTitleEn},
		{t8cRetirementCourseID, "دورة T8C للتقاعد", t8cRetirementCourseTitleEn},
		{t8cArchiveCourseID, "دورة T8C للأرشفة", t8cArchiveCourseTitleEn},
	}

	lessonIdentities := make(map[string]string, len(courses))
	for index, course := range courses {
		lessonIdentityID, err := seedT8CPublishedCourse(ctx, tx, course, instructorID, adminAccountID, index)
		if err != nil {
			return err
		}
		lessonIdentities[course.courseID] = lessonIdentityID
	}

	// The suspension case needs a Student who already holds valid access, because the contract it
	// proves is about an existing Entitlement: suspension must block the read without touching
	// the grant, and restoration must return the read without creating a new one.
	if _, err := tx.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name, email_verified_at)
		VALUES ($1, $2, $2, 'STUDENT', 'ACTIVE', 'T8C Suspension Student', $3)
	`, t8cSuspensionStudentID, t8cSuspensionStudentEmail, now); err != nil {
		return fmt.Errorf("insert T8C suspension student: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO password_credentials (account_id, password_hash, state)
		VALUES ($1, $2, 'ACTIVE')
	`, t8cSuspensionStudentID, passwordHash); err != nil {
		return fmt.Errorf("insert T8C suspension student credentials: %w", err)
	}

	invitationID := "c8000000-0000-0000-0000-0000000000c1"
	if _, err := tx.Exec(ctx, `
		INSERT INTO course_access_invitations (id, course_id, email, normalized_email, created_by_account_id, accepted_by_account_id, decided_by_account_id, state, created_at)
		VALUES ($1, $2, $3, $3, $4, $5, $4, 'APPROVED', $6)
	`, invitationID, t8cSuspensionCourseID, t8cSuspensionStudentEmail, adminAccountID, t8cSuspensionStudentID, now); err != nil {
		return fmt.Errorf("insert T8C suspension invitation: %w", err)
	}

	entitlementID := "c8000000-0000-0000-0000-0000000000e1"
	if _, err := tx.Exec(ctx, `
		INSERT INTO entitlements (id, student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state)
		VALUES ($1, $2, 'COURSE', $3, $3, 'MANUAL_INVITATION', $4, $5, $5, $6, 'ACTIVE')
	`, entitlementID, t8cSuspensionStudentID, t8cSuspensionCourseID, invitationID, accessEndsAt, now); err != nil {
		return fmt.Errorf("insert T8C suspension entitlement: %w", err)
	}

	enrollmentID := "c8000000-0000-0000-0000-0000000000b1"
	if _, err := tx.Exec(ctx, `
		INSERT INTO enrollments (id, student_account_id, course_id)
		VALUES ($1, $2, $3)
	`, enrollmentID, t8cSuspensionStudentID, t8cSuspensionCourseID); err != nil {
		return fmt.Errorf("insert T8C suspension enrollment: %w", err)
	}

	// Durable history underneath the Entitlement. Suspension must not remove it, and restoration
	// must not need to recreate it.
	if _, err := tx.Exec(ctx, `
		INSERT INTO progress (enrollment_id, course_lesson_identity_id, max_position_seconds, last_position_seconds, last_watched_at)
		VALUES ($1, $2, 42, 42, $3)
	`, enrollmentID, lessonIdentities[t8cSuspensionCourseID], now); err != nil {
		return fmt.Errorf("insert T8C suspension progress: %w", err)
	}

	return nil
}

// seedT8CPublishedCourse creates one ordinary published Course: an approved revision promoted to
// live, one section and one lesson. It returns the Course's lesson identity so an entitled
// Student's Progress can be attached to it.
func seedT8CPublishedCourse(
	ctx context.Context,
	tx pgx.Tx,
	course t8cLifecycleCourse,
	instructorID string,
	adminAccountID string,
	index int,
) (string, error) {
	revisionID := fmt.Sprintf("c8000000-0000-0000-0000-f00000%06d", index+1)
	sectionIdentityID := fmt.Sprintf("c8000000-0000-0000-0000-5ec700%06d", index+1)
	sectionID := fmt.Sprintf("c8000000-0000-0000-0000-5ec500%06d", index+1)
	lessonIdentityID := fmt.Sprintf("c8000000-0000-0000-0000-1e5100%06d", index+1)
	lessonID := fmt.Sprintf("c8000000-0000-0000-0000-1e5500%06d", index+1)

	if _, err := tx.Exec(ctx, `
		INSERT INTO courses (id, owner_account_id, lifecycle)
		VALUES ($1, $2, 'DRAFT')
	`, course.courseID, instructorID); err != nil {
		return "", fmt.Errorf("insert T8C course %s: %w", course.titleEn, err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en, description_ar, description_en)
		VALUES ($1, $2, 'APPROVED', 1, $3, $4, 'وصف دورة T8C', 'T8C lifecycle fixture Course description.')
	`, revisionID, course.courseID, course.titleAr, course.titleEn); err != nil {
		return "", fmt.Errorf("insert T8C revision for %s: %w", course.titleEn, err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE courses SET live_revision_id = $1, lifecycle = 'PUBLISHED' WHERE id = $2
	`, revisionID, course.courseID); err != nil {
		return "", fmt.Errorf("publish T8C course %s: %w", course.titleEn, err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO course_price_changes (course_id, new_value_minor_units, changed_by_account_id, reason)
		VALUES ($1, 19000, $2, 'T8C lifecycle fixture price')
	`, course.courseID, adminAccountID); err != nil {
		return "", fmt.Errorf("insert T8C price for %s: %w", course.titleEn, err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO course_section_identities (id, course_id) VALUES ($1, $2)
	`, sectionIdentityID, course.courseID); err != nil {
		return "", fmt.Errorf("insert T8C section identity for %s: %w", course.titleEn, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position)
		VALUES ($1, $2, $3, $4, 'القسم الأول', 'Section 1', 1)
	`, sectionID, revisionID, course.courseID, sectionIdentityID); err != nil {
		return "", fmt.Errorf("insert T8C section for %s: %w", course.titleEn, err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1, $2, $3)
	`, lessonIdentityID, course.courseID, sectionIdentityID); err != nil {
		return "", fmt.Errorf("insert T8C lesson identity for %s: %w", course.titleEn, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position, video_asset_version_id)
		VALUES ($1, $2, $3, $4, $5, 'الدرس الأول', 'Lesson 1', 1, NULL)
	`, lessonID, sectionID, course.courseID, sectionIdentityID, lessonIdentityID); err != nil {
		return "", fmt.Errorf("insert T8C lesson for %s: %w", course.titleEn, err)
	}

	return lessonIdentityID, nil
}
