//go:build integration

package learning

import (
	"context"
	"errors"
	"testing"
)

func TestReadCourseGraphUsesApprovedLiveRevisionAndAuthoredOrder(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	const (
		sectionIdentity = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa1"
		sectionRow      = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbb1"
		lessonIdentity  = "cccccccc-cccc-cccc-cccc-ccccccccccc1"
		lessonRow       = "dddddddd-dddd-dddd-dddd-ddddddddddd1"
	)
	for _, seed := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)`, []any{sectionIdentity, fixture.courseID}},
		{`INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`, []any{lessonIdentity, fixture.courseID, sectionIdentity}},
		{`INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position)
			VALUES ($1::uuid, '55555555-5555-5555-5555-555555555555', $2::uuid, $3::uuid, 'قسم ثان', 'Second section', 1)`, []any{sectionRow, fixture.courseID, sectionIdentity}},
		{`INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'درس ثان', 'Second lesson', 0)`, []any{lessonRow, sectionRow, fixture.courseID, sectionIdentity, lessonIdentity}},
	} {
		if _, err := fixture.repository.pool.Exec(ctx, seed.query, seed.args...); err != nil {
			t.Fatalf("seeding ordered graph: %v", err)
		}
	}

	graph, err := fixture.repository.ReadCourseGraph(ctx, fixture.courseID)
	if err != nil {
		t.Fatalf("reading live graph: %v", err)
	}
	if graph.CourseID != fixture.courseID || graph.RevisionID != "55555555-5555-5555-5555-555555555555" || graph.RevisionNo != 1 {
		t.Fatalf("graph identity = %+v", graph)
	}
	if len(graph.Sections) != 2 || graph.Sections[0].ID != "66666666-6666-6666-6666-666666666666" || graph.Sections[1].ID != sectionIdentity {
		t.Fatalf("sections were not returned in authored order: %+v", graph.Sections)
	}
	if ids := graph.LessonIDs(); len(ids) != 2 || ids[0] != fixture.lessonID || ids[1] != lessonIdentity {
		t.Fatalf("lesson identities = %#v, want stable authored order", ids)
	}
}

func TestReadCourseGraphFollowsLiveRevisionAndRetainsStableLessonIdentity(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	const replacementRevision = "88888888-8888-8888-8888-888888888888"
	const replacementSection = "99999999-9999-9999-9999-999999999999"
	for _, seed := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en)
			VALUES ($1::uuid, $2::uuid, 'APPROVED', 2, 'دورة جديدة', 'Updated course')`, []any{replacementRevision, fixture.courseID}},
		{`INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position)
			VALUES ($1::uuid, $2::uuid, $3::uuid, '66666666-6666-6666-6666-666666666666', 'قسم جديد', 'Updated section', 0)`, []any{replacementSection, replacementRevision, fixture.courseID}},
		{`INSERT INTO course_lessons (section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position)
			VALUES ($1::uuid, $2::uuid, '66666666-6666-6666-6666-666666666666', $3::uuid, 'درس جديد', 'Updated lesson', 0)`, []any{replacementSection, fixture.courseID, fixture.lessonID}},
		{`UPDATE courses SET live_revision_id = $1::uuid WHERE id = $2::uuid`, []any{replacementRevision, fixture.courseID}},
	} {
		if _, err := fixture.repository.pool.Exec(ctx, seed.query, seed.args...); err != nil {
			t.Fatalf("seeding replacement revision: %v", err)
		}
	}

	graph, err := fixture.repository.ReadCourseGraph(ctx, fixture.courseID)
	if err != nil {
		t.Fatalf("reading replacement graph: %v", err)
	}
	if graph.RevisionID != replacementRevision || graph.TitleEn != "Updated course" {
		t.Fatalf("graph did not follow live revision: %+v", graph)
	}
	if ids := graph.LessonIDs(); len(ids) != 1 || ids[0] != fixture.lessonID {
		t.Fatalf("replacement changed stable Lesson identity: %#v", ids)
	}
}

func TestReadCourseGraphFailsClosedForMissingLiveGraph(t *testing.T) {
	fixture := newLearningFixture(t)
	if _, err := fixture.repository.pool.Exec(context.Background(), `UPDATE courses SET live_revision_id = NULL, lifecycle = 'DRAFT' WHERE id = $1::uuid`, fixture.courseID); err != nil {
		t.Fatalf("removing live revision: %v", err)
	}
	if _, err := fixture.repository.ReadCourseGraph(context.Background(), fixture.courseID); !errors.Is(err, ErrCourseGraphNotFound) {
		t.Fatalf("missing live graph error = %v, want ErrCourseGraphNotFound", err)
	}
}

func TestAggregateCourseProgressIsStudentScopedAndUsesStableGraph(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	graph, err := fixture.repository.ReadCourseGraph(ctx, fixture.courseID)
	if err != nil {
		t.Fatalf("reading graph: %v", err)
	}
	const otherStudent = "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeee1"
	if _, err := fixture.repository.pool.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
		VALUES ($1::uuid, 'other@example.test', 'other@example.test', 'STUDENT', 'ACTIVE', 'Other student')
	`, otherStudent); err != nil {
		t.Fatalf("seeding second Student account: %v", err)
	}
	if _, err := fixture.repository.pool.Exec(ctx, `
		INSERT INTO enrollments (student_account_id, course_id) VALUES ($1::uuid, $2::uuid)
	`, otherStudent, fixture.courseID); err != nil {
		t.Fatalf("seeding second Student enrollment: %v", err)
	}
	versionID := seedProgressAssetVersion(t, ctx, fixture, "ffffffff-ffff-ffff-ffff-fffffffffff1")
	otherEnrollment, err := fixture.repository.EnrollmentID(ctx, otherStudent, fixture.courseID)
	if err != nil {
		t.Fatalf("resolving second enrollment: %v", err)
	}
	if err := fixture.repository.SaveProgress(ctx, ProgressWrite{
		EnrollmentID: otherEnrollment, CourseLessonIdentityID: fixture.lessonID,
		PositionSeconds: 60, Completed: true, CompletingAssetVersionID: versionID,
	}); err != nil {
		t.Fatalf("saving other Student progress: %v", err)
	}
	studentEnrollment, err := fixture.repository.EnrollmentID(ctx, fixture.studentID, fixture.courseID)
	if err != nil {
		t.Fatalf("resolving fixture enrollment: %v", err)
	}
	if err := fixture.repository.SaveProgress(ctx, ProgressWrite{
		EnrollmentID: studentEnrollment, CourseLessonIdentityID: fixture.lessonID,
		PositionSeconds: 99,
	}); err != nil {
		t.Fatalf("saving partial Student progress: %v", err)
	}

	studentSummary, err := fixture.repository.AggregateCourseProgress(ctx, fixture.studentID, fixture.courseID, graph)
	if err != nil {
		t.Fatalf("aggregating Student progress: %v", err)
	}
	if studentSummary.CompletedLessons != 0 || studentSummary.TotalLessons != 1 {
		t.Fatalf("Student summary = %+v, want 0/1", studentSummary)
	}
	otherSummary, err := fixture.repository.AggregateCourseProgress(ctx, otherStudent, fixture.courseID, graph)
	if err != nil {
		t.Fatalf("aggregating second Student progress: %v", err)
	}
	if otherSummary.CompletedLessons != 1 || otherSummary.TotalLessons != 1 {
		t.Fatalf("second Student summary = %+v, want 1/1", otherSummary)
	}
}

func TestCourseGraphAndProgressReadsDoNotMutateLearningAuthority(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	var before string
	if err := fixture.repository.pool.QueryRow(ctx, `
		SELECT COALESCE(md5(string_agg(row_text, '|' ORDER BY row_text)), '') FROM (
			SELECT id::text || ':' || student_account_id::text || ':' || course_id::text AS row_text FROM enrollments
			UNION ALL
			SELECT id::text || ':' || enrollment_id::text || ':' || course_lesson_identity_id::text AS row_text FROM progress
		) rows
	`).Scan(&before); err != nil {
		t.Fatalf("snapshotting authority: %v", err)
	}
	graph, err := fixture.repository.ReadCourseGraph(ctx, fixture.courseID)
	if err != nil {
		t.Fatalf("reading graph: %v", err)
	}
	if _, err := fixture.repository.AggregateCourseProgress(ctx, fixture.studentID, fixture.courseID, graph); err != nil {
		t.Fatalf("aggregating progress: %v", err)
	}
	var after string
	if err := fixture.repository.pool.QueryRow(ctx, `
		SELECT COALESCE(md5(string_agg(row_text, '|' ORDER BY row_text)), '') FROM (
			SELECT id::text || ':' || student_account_id::text || ':' || course_id::text AS row_text FROM enrollments
			UNION ALL
			SELECT id::text || ':' || enrollment_id::text || ':' || course_lesson_identity_id::text AS row_text FROM progress
		) rows
	`).Scan(&after); err != nil {
		t.Fatalf("snapshotting authority after reads: %v", err)
	}
	if before != after {
		t.Fatalf("read routes changed Enrollment/Progress authority: before=%s after=%s", before, after)
	}
}
