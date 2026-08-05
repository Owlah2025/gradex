//go:build integration

package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestT059SharedInstructorCourseHomesDoNotLeakGraphProgressOrMaterials(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	seedD064Resource(t, f)
	courseB, lessonB := seedT059HTTPCourse(t, f)
	seedT059LabMaterial(t, f, courseB, lessonB)
	seedLearningProgress(t, f, f.lessonID, 60, true)

	var ownerA, ownerB string
	if err := f.pool.QueryRow(context.Background(), `SELECT owner_account_id::text FROM courses WHERE id = $1::uuid`, f.courseID).Scan(&ownerA); err != nil {
		t.Fatalf("reading Course A Instructor: %v", err)
	}
	if err := f.pool.QueryRow(context.Background(), `SELECT owner_account_id::text FROM courses WHERE id = $1::uuid`, courseB).Scan(&ownerB); err != nil {
		t.Fatalf("reading Course B Instructor: %v", err)
	}
	if ownerA != ownerB {
		t.Fatalf("Courses do not share Instructor: A=%s B=%s", ownerA, ownerB)
	}

	for _, tc := range []struct {
		name, courseID, ownLesson, foreignLesson, ownMaterial, foreignMaterial string
	}{
		{name: "Course A", courseID: f.courseID, ownLesson: f.lessonID, foreignLesson: lessonB, ownMaterial: `"resource"`, foreignMaterial: `"lab_material"`},
		{name: "Course B", courseID: courseB, ownLesson: lessonB, foreignLesson: f.lessonID, ownMaterial: `"lab_material"`, foreignMaterial: `"resource"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := f.request(http.MethodGet, "/api/v1/learn/courses/"+tc.courseID, "")
			assertReadSuccess(t, response)
			body := response.Body.String()
			if !strings.Contains(body, `"course_id":"`+tc.courseID+`"`) || !strings.Contains(body, `"lesson_id":"`+tc.ownLesson+`"`) || strings.Contains(body, `"lesson_id":"`+tc.foreignLesson+`"`) {
				t.Fatalf("Course Home leaked graph identity: %s", body)
			}
			if !strings.Contains(body, tc.ownMaterial) || strings.Contains(body, tc.foreignMaterial) {
				t.Fatalf("Course Home leaked material kind: %s", body)
			}
		})
	}
}

func seedT059HTTPCourse(t *testing.T, f learningIntegrationFixture) (string, string) {
	t.Helper()
	ctx := context.Background()
	var instructorID string
	if err := f.pool.QueryRow(ctx, `SELECT owner_account_id::text FROM courses WHERE id = $1::uuid`, f.courseID).Scan(&instructorID); err != nil {
		t.Fatalf("reading shared Instructor: %v", err)
	}
	courseID, revisionID, sectionIdentity, sectionRow, lessonID, lessonRow := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1::uuid, $2::uuid, 'DRAFT')`, []any{courseID, instructorID}},
		{`INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, 'APPROVED', 1, 'Shared Course', 'Shared Course')`, []any{revisionID, courseID}},
		{`UPDATE courses SET live_revision_id = $1::uuid, lifecycle = 'PUBLISHED' WHERE id = $2::uuid`, []any{revisionID, courseID}},
		{`INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)`, []any{sectionIdentity, courseID}},
		{`INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`, []any{lessonID, courseID, sectionIdentity}},
		{`INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'Shared Section', 'Shared Section', 0)`, []any{sectionRow, revisionID, courseID, sectionIdentity}},
		{`INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'Shared Lesson', 'Shared Lesson', 0)`, []any{lessonRow, sectionRow, courseID, sectionIdentity, lessonID}},
		{`INSERT INTO enrollments (student_account_id, course_id) VALUES ($1::uuid, $2::uuid)`, []any{f.studentID, courseID}},
		{`INSERT INTO entitlements (student_account_id, scope_kind, scope_id, course_id, grant_source, original_access_ends_at, access_ends_at, retirement_eligibility_at, state) VALUES ($1::uuid, 'COURSE', $2::uuid, $2::uuid, 'MANUAL_INVITATION', $3, $3, $4, 'ACTIVE')`, []any{f.studentID, courseID, f.clock.Now().Add(time.Hour), f.clock.Now().Add(-time.Hour)}},
	} {
		if _, err := f.pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seeding shared-Instructor Course: %v", err)
		}
	}
	return courseID, lessonID
}

func seedT059LabMaterial(t *testing.T, f learningIntegrationFixture, courseID, lessonIdentityID string) {
	t.Helper()
	ctx := context.Background()
	var lessonRow, instructorID string
	if err := f.pool.QueryRow(ctx, `SELECT cl.id::text, c.owner_account_id::text FROM course_lessons cl JOIN courses c ON c.id = cl.course_id WHERE cl.lesson_identity_id = $1::uuid`, lessonIdentityID).Scan(&lessonRow, &instructorID); err != nil {
		t.Fatalf("resolving Course B Lesson row: %v", err)
	}
	assetID, versionID, scanID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO media_assets (id, kind, owner_account_id, course_id, lesson_id, visibility) VALUES ($1::uuid, 'LAB_MATERIAL', $2::uuid, $3::uuid, $4::uuid, 'PROTECTED')`, []any{assetID, instructorID, courseID, lessonRow}},
		{`INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes) VALUES ($1::uuid, $2::uuid, 'LAB_MATERIAL', 'QUARANTINED', 'lab/t059.zip', 'v1', 'application/zip', 12)`, []any{versionID, assetID}},
		{`INSERT INTO scan_attempts (id, asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity) VALUES ($1::uuid, $2::uuid, 1, $3, 'v1', 'PASSED', 't059-fixture')`, []any{scanID, versionID, "scan:" + versionID}},
		{`UPDATE media_asset_versions SET state = 'SCANNING' WHERE id = $1::uuid`, []any{versionID}},
		{`UPDATE media_asset_versions SET successful_scan_attempt_id = $1::uuid, state = 'SCAN_PASSED' WHERE id = $2::uuid`, []any{scanID, versionID}},
		{`UPDATE media_asset_versions SET state = 'READY' WHERE id = $1::uuid`, []any{versionID}},
		{`INSERT INTO lesson_files (lesson_id, kind, asset_version_id, display_name_ar, display_name_en, position) VALUES ($1::uuid, 'LAB_MATERIAL', $2::uuid, 'مختبر مشترك', 'Shared Lab', 0)`, []any{lessonRow, versionID}},
	} {
		if _, err := f.pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seeding Course B Lab Material: %v", err)
		}
	}
}
