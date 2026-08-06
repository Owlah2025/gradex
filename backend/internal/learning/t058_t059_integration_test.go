//go:build integration

package learning

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestT058CourseScopeOrderingAggregationAndReadOnlyState(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	seedT058OrderedGraph(t, fixture)
	graph, err := fixture.repository.ReadCourseGraph(ctx, fixture.courseID)
	if err != nil {
		t.Fatalf("reading ordered Course graph: %v", err)
	}
	if got := graph.LessonIDs(); len(got) != 5 || got[0] != fixture.lessonID {
		t.Fatalf("current graph identities = %v, want five authored stable Lessons", got)
	}
	if graph.Sections[0].Position != 0 || graph.Sections[1].Position != 10 || graph.Sections[2].Position != 20 {
		t.Fatalf("Section order = %+v, want authored positions 0/10/20", graph.Sections)
	}
	if graph.Sections[1].Lessons[0].Position != 3 || graph.Sections[1].Lessons[1].Position != 9 {
		t.Fatalf("Lesson order = %+v, want authored positions 3/9", graph.Sections[1].Lessons)
	}

	enrollment, err := fixture.repository.EnrollmentForLesson(ctx, fixture.studentID, fixture.lessonID)
	if err != nil {
		t.Fatalf("resolving Course Enrollment: %v", err)
	}
	versionID := seedProgressAssetVersion(t, ctx, fixture, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa9")
	for _, write := range []ProgressWrite{
		{EnrollmentID: enrollment.ID, CourseLessonIdentityID: graph.Sections[0].Lessons[0].ID, PositionSeconds: 90, Completed: true, CompletingAssetVersionID: versionID},
		{EnrollmentID: enrollment.ID, CourseLessonIdentityID: graph.Sections[1].Lessons[0].ID, PositionSeconds: 45},
		{EnrollmentID: enrollment.ID, CourseLessonIdentityID: graph.Sections[2].Lessons[0].ID, PositionSeconds: 90, Completed: true, CompletingAssetVersionID: versionID},
	} {
		if err := fixture.repository.SaveProgress(ctx, write); err != nil {
			t.Fatalf("seeding Course progress: %v", err)
		}
	}
	before := learningReadSnapshot(t, fixture)
	progress, summary, err := fixture.repository.ReadCourseProgress(ctx, enrollment.ID, fixture.courseID, graph)
	if err != nil {
		t.Fatalf("reading Course progress: %v", err)
	}
	if len(progress) != 5 || summary.CompletedLessons != 2 || summary.TotalLessons != 5 {
		t.Fatalf("Course progress=%v summary=%+v, want five Lessons and 2/5", progress, summary)
	}
	if progress[graph.Sections[1].Lessons[0].ID].Completed {
		t.Fatal("partial playback received completion credit")
	}
	if after := learningReadSnapshot(t, fixture); before != after {
		t.Fatalf("read-model repository operations mutated authority:\nbefore=%s\nafter=%s", before, after)
	}
}

func TestT058RevisionReplacementExcludesHistoricalLessonsAndPreservesStableProgress(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	enrollment, err := fixture.repository.EnrollmentForLesson(ctx, fixture.studentID, fixture.lessonID)
	if err != nil {
		t.Fatalf("resolving Enrollment: %v", err)
	}
	versionID := seedProgressAssetVersion(t, ctx, fixture, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbb9")
	if err := fixture.repository.SaveProgress(ctx, ProgressWrite{EnrollmentID: enrollment.ID, CourseLessonIdentityID: fixture.lessonID, PositionSeconds: 80, Completed: true, CompletingAssetVersionID: versionID}); err != nil {
		t.Fatalf("saving stable Lesson Progress: %v", err)
	}
	removedLessonID := uuid.NewString()
	seedT058LessonInLiveRevision(t, fixture, removedLessonID, "historical", 1)
	if err := fixture.repository.SaveProgress(ctx, ProgressWrite{EnrollmentID: enrollment.ID, CourseLessonIdentityID: removedLessonID, PositionSeconds: 90, Completed: true, CompletingAssetVersionID: versionID}); err != nil {
		t.Fatalf("saving historical Lesson Progress: %v", err)
	}

	replacementRevision, replacementSection := uuid.NewString(), uuid.NewString()
	sectionIdentity := "66666666-6666-6666-6666-666666666666"
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, 'APPROVED', 2, 'دورة بديلة', 'Replacement')`, []any{replacementRevision, fixture.courseID}},
		{`INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'قسم بديل', 'Replacement Section', 0)`, []any{replacementSection, replacementRevision, fixture.courseID, sectionIdentity}},
		{`INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'درس ثابت', 'Stable Lesson', 0)`, []any{uuid.NewString(), replacementSection, fixture.courseID, sectionIdentity, fixture.lessonID}},
		{`UPDATE courses SET live_revision_id = $1::uuid WHERE id = $2::uuid`, []any{replacementRevision, fixture.courseID}},
	} {
		if _, err := fixture.repository.pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seeding replacement graph: %v", err)
		}
	}
	graph, err := fixture.repository.ReadCourseGraph(ctx, fixture.courseID)
	if err != nil {
		t.Fatalf("reading replacement graph: %v", err)
	}
	if ids := graph.LessonIDs(); len(ids) != 1 || ids[0] != fixture.lessonID {
		t.Fatalf("replacement graph identities=%v, want only stable Lesson", ids)
	}
	progress, summary, err := fixture.repository.ReadCourseProgress(ctx, enrollment.ID, fixture.courseID, graph)
	if err != nil {
		t.Fatalf("reading replacement progress: %v", err)
	}
	if len(progress) != 1 || summary.CompletedLessons != 1 || summary.TotalLessons != 1 || progress[fixture.lessonID].LastPositionSeconds != 80 {
		t.Fatalf("replacement progress=%v summary=%+v, want retained stable completion 1/1", progress, summary)
	}
}

func TestT059SharedInstructorCoursesRemainIsolatedAtRepositoryBoundary(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	const instructorID = "44444444-4444-4444-4444-444444444444"
	studentB := "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeee2"
	if _, err := fixture.repository.pool.Exec(ctx, `INSERT INTO accounts (id, normalized_email, email, role, status, display_name) VALUES ($1::uuid, 'student-b@example.test', 'student-b@example.test', 'STUDENT', 'ACTIVE', 'Student B')`, studentB); err != nil {
		t.Fatalf("seeding Student B: %v", err)
	}
	courseB, lessonB := uuid.NewString(), uuid.NewString()
	seedT059Course(t, fixture, instructorID, courseB, lessonB, "Shared Course", "shared lesson")
	clock := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	invA := uuid.NewString()
	if _, err := fixture.repository.pool.Exec(ctx, `INSERT INTO course_access_invitations (id, course_id, email, normalized_email, created_by_account_id, accepted_by_account_id, decided_by_account_id, state) VALUES ($1::uuid, $2::uuid, 'student-a@example.test', 'student-a@example.test', $3::uuid, $4::uuid, $3::uuid, 'APPROVED')`, invA, fixture.courseID, instructorID, fixture.studentID); err != nil {
		t.Fatalf("seeding invitation A: %v", err)
	}
	if _, err := fixture.repository.pool.Exec(ctx, `INSERT INTO entitlements (student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state) VALUES ($1::uuid, 'COURSE', $2::uuid, $2::uuid, 'MANUAL_INVITATION', $3::uuid, $4, $4, $5, 'ACTIVE')`, fixture.studentID, fixture.courseID, invA, clock.Add(time.Hour), clock.Add(-time.Hour)); err != nil {
		t.Fatalf("seeding Student A Course A Entitlement: %v", err)
	}
	var enrollmentB string
	if err := fixture.repository.pool.QueryRow(ctx, `INSERT INTO enrollments (student_account_id, course_id) VALUES ($1::uuid, $2::uuid) RETURNING id::text`, studentB, courseB).Scan(&enrollmentB); err != nil {
		t.Fatalf("enrolling Student B: %v", err)
	}
	invB := uuid.NewString()
	if _, err := fixture.repository.pool.Exec(ctx, `INSERT INTO course_access_invitations (id, course_id, email, normalized_email, created_by_account_id, accepted_by_account_id, decided_by_account_id, state) VALUES ($1::uuid, $2::uuid, 'student-b@example.test', 'student-b@example.test', $3::uuid, $4::uuid, $3::uuid, 'APPROVED')`, invB, courseB, instructorID, studentB); err != nil {
		t.Fatalf("seeding invitation B: %v", err)
	}
	if _, err := fixture.repository.pool.Exec(ctx, `INSERT INTO entitlements (student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state) VALUES ($1::uuid, 'COURSE', $2::uuid, $2::uuid, 'MANUAL_INVITATION', $3::uuid, $4, $4, $5, 'ACTIVE')`, studentB, courseB, invB, clock.Add(time.Hour), clock.Add(-time.Hour)); err != nil {
		t.Fatalf("seeding Student B Course B Entitlement: %v", err)
	}
	graphA, err := fixture.repository.ReadCourseGraph(ctx, fixture.courseID)
	if err != nil {
		t.Fatalf("reading Course A graph: %v", err)
	}
	graphB, err := fixture.repository.ReadCourseGraph(ctx, courseB)
	if err != nil {
		t.Fatalf("reading Course B graph: %v", err)
	}
	if graphA.CourseID == graphB.CourseID || graphA.LessonIDs()[0] == graphB.LessonIDs()[0] {
		t.Fatal("shared Instructor collapsed distinct Course or stable Lesson identities")
	}
	enrollmentA, err := fixture.repository.EnrollmentForLesson(ctx, fixture.studentID, fixture.lessonID)
	if err != nil {
		t.Fatalf("resolving Student A Course A Enrollment: %v", err)
	}
	enrollmentAForB, err := fixture.repository.EnrollmentForLesson(ctx, fixture.studentID, lessonB)
	if err != nil {
		t.Fatalf("resolving Student A Course B Enrollment: %v", err)
	}
	if summary, err := fixture.repository.AggregateCourseProgress(ctx, fixture.studentID, courseB, graphB); err != nil || summary.TotalLessons != 1 {
		t.Fatalf("Student A Course B scope summary=%+v err=%v", summary, err)
	}
	if _, err := fixture.repository.AggregateCourseProgress(ctx, studentB, fixture.courseID, graphA); !errors.Is(err, ErrEnrollmentNotFound) {
		t.Fatalf("Student B aggregated Course A Progress: %v", err)
	}
	if _, err := fixture.repository.pool.Exec(ctx, `INSERT INTO progress (enrollment_id, course_lesson_identity_id, max_position_seconds, last_position_seconds, updated_at) VALUES ($1::uuid, $2::uuid, 8, 8, now())`, enrollmentAForB.ID, lessonB); err != nil {
		t.Fatalf("seeding Student A Course B Progress: %v", err)
	}
	if _, err := fixture.repository.pool.Exec(ctx, `INSERT INTO progress (enrollment_id, course_lesson_identity_id, max_position_seconds, last_position_seconds, updated_at) VALUES ($1::uuid, $2::uuid, 12, 12, now())`, enrollmentB, lessonB); err != nil {
		t.Fatalf("seeding Student B Course B Progress: %v", err)
	}
	progressB, summaryB, err := fixture.repository.ReadCourseProgress(ctx, enrollmentB, courseB, graphB)
	if err != nil || summaryB.TotalLessons != 1 || progressB[lessonB].LastPositionSeconds != 12 {
		t.Fatalf("Student B Course B progress=%v summary=%+v err=%v", progressB, summaryB, err)
	}
	if summary, err := fixture.repository.AggregateCourseProgress(ctx, fixture.studentID, fixture.courseID, graphA); err != nil || summary.TotalLessons != 1 || enrollmentA.CourseID != fixture.courseID {
		t.Fatalf("Student A Course A scope summary=%+v err=%v", summary, err)
	}
}

func seedT058OrderedGraph(t *testing.T, fixture learningFixture) {
	t.Helper()
	ctx := context.Background()
	for _, section := range []struct {
		identity, row string
		position      int
		lessons       []struct {
			identity, row string
			position      int
		}
	}{
		{identity: "99999999-9999-9999-9999-999999999990", row: "99999999-9999-9999-9999-999999999991", position: 20, lessons: []struct {
			identity, row string
			position      int
		}{{"99999999-9999-9999-9999-999999999992", "99999999-9999-9999-9999-999999999993", 9}, {"99999999-9999-9999-9999-999999999994", "99999999-9999-9999-9999-999999999995", 3}}},
		{identity: "88888888-8888-8888-8888-888888888880", row: "88888888-8888-8888-8888-888888888881", position: 10, lessons: []struct {
			identity, row string
			position      int
		}{{"88888888-8888-8888-8888-888888888882", "88888888-8888-8888-8888-888888888883", 9}, {"88888888-8888-8888-8888-888888888884", "88888888-8888-8888-8888-888888888885", 3}}},
	} {
		if _, err := fixture.repository.pool.Exec(ctx, `INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)`, section.identity, fixture.courseID); err != nil {
			t.Fatalf("seeding Section identity: %v", err)
		}
		if _, err := fixture.repository.pool.Exec(ctx, `INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position) VALUES ($1::uuid, '55555555-5555-5555-5555-555555555555', $2::uuid, $3::uuid, $4, $5, $6)`, section.row, fixture.courseID, section.identity, "قسم "+section.identity[:4], "Section "+section.identity[:4], section.position); err != nil {
			t.Fatalf("seeding Section row: %v", err)
		}
		for _, lesson := range section.lessons {
			if _, err := fixture.repository.pool.Exec(ctx, `INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`, lesson.identity, fixture.courseID, section.identity); err != nil {
				t.Fatalf("seeding Lesson identity: %v", err)
			}
			if _, err := fixture.repository.pool.Exec(ctx, `INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, $7, $8)`, lesson.row, section.row, fixture.courseID, section.identity, lesson.identity, "درس "+lesson.identity[:4], "Lesson "+lesson.identity[:4], lesson.position); err != nil {
				t.Fatalf("seeding Lesson row: %v", err)
			}
		}
	}
}

func seedT058LessonInLiveRevision(t *testing.T, fixture learningFixture, lessonID, title string, position int) {
	t.Helper()
	ctx := context.Background()
	sectionIdentity := "66666666-6666-6666-6666-666666666666"
	sectionRow := "77777777-7777-7777-7777-777777777777"
	lessonRow := uuid.NewString()
	if _, err := fixture.repository.pool.Exec(ctx, `INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`, lessonID, fixture.courseID, sectionIdentity); err != nil {
		t.Fatalf("seeding historical Lesson identity: %v", err)
	}
	if _, err := fixture.repository.pool.Exec(ctx, `INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, $7, $8)`, lessonRow, sectionRow, fixture.courseID, sectionIdentity, lessonID, title, title, position); err != nil {
		t.Fatalf("seeding historical Lesson row: %v", err)
	}
}

func seedT059Course(t *testing.T, fixture learningFixture, instructorID, courseID, lessonID, title, lessonTitle string) {
	t.Helper()
	ctx := context.Background()
	revisionID, sectionIdentity, sectionRow, lessonRow := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1::uuid, $2::uuid, 'DRAFT')`, []any{courseID, instructorID}},
		{`INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, 'APPROVED', 1, $3, $3)`, []any{revisionID, courseID, title}},
		{`UPDATE courses SET live_revision_id = $1::uuid, lifecycle = 'PUBLISHED' WHERE id = $2::uuid`, []any{revisionID, courseID}},
		{`INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)`, []any{sectionIdentity, courseID}},
		{`INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`, []any{lessonID, courseID, sectionIdentity}},
		{`INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $5, 0)`, []any{sectionRow, revisionID, courseID, sectionIdentity, title}},
		{`INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, $6, 0)`, []any{lessonRow, sectionRow, courseID, sectionIdentity, lessonID, lessonTitle}},
	} {
		if _, err := fixture.repository.pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seeding shared-Instructor Course: %v", err)
		}
	}
	if _, err := fixture.repository.pool.Exec(ctx, `INSERT INTO enrollments (student_account_id, course_id) VALUES ($1::uuid, $2::uuid)`, fixture.studentID, courseID); err != nil {
		t.Fatalf("seeding shared-Instructor Enrollment: %v", err)
	}
	clock := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	invID := uuid.NewString()
	if _, err := fixture.repository.pool.Exec(ctx, `INSERT INTO course_access_invitations (id, course_id, email, normalized_email, created_by_account_id, accepted_by_account_id, decided_by_account_id, state) VALUES ($1::uuid, $2::uuid, 'student@example.test', 'student@example.test', $3::uuid, $4::uuid, $3::uuid, 'APPROVED')`, invID, courseID, instructorID, fixture.studentID); err != nil {
		t.Fatalf("seeding shared-Instructor Invitation: %v", err)
	}
	if _, err := fixture.repository.pool.Exec(ctx, `INSERT INTO entitlements (student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id, original_access_ends_at, access_ends_at, retirement_eligibility_at, state) VALUES ($1::uuid, 'COURSE', $2::uuid, $2::uuid, 'MANUAL_INVITATION', $3::uuid, $4, $4, $5, 'ACTIVE')`, fixture.studentID, courseID, invID, clock.Add(time.Hour), clock.Add(-time.Hour)); err != nil {
		t.Fatalf("seeding shared-Instructor Entitlement: %v", err)
	}
}

func learningReadSnapshot(t *testing.T, fixture learningFixture) string {
	t.Helper()
	ctx := context.Background()
	parts := make([]string, 0, 9)
	for _, query := range []string{
		`SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY x.id)::text, '[]') FROM accounts x`,
		`SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY x.id)::text, '[]') FROM courses x`,
		`SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY x.id)::text, '[]') FROM course_revisions x`,
		`SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY x.id)::text, '[]') FROM course_lesson_identities x`,
		`SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY x.id)::text, '[]') FROM lesson_files x`,
		`SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY x.id)::text, '[]') FROM media_asset_versions x`,
		`SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY x.id)::text, '[]') FROM entitlements x`,
		`SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY x.id)::text, '[]') FROM enrollments x`,
		`SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY x.enrollment_id, x.course_lesson_identity_id)::text, '[]') FROM progress x`,
	} {
		var value string
		if err := fixture.repository.pool.QueryRow(ctx, query).Scan(&value); err != nil {
			t.Fatalf("snapshotting learning authority: %v", err)
		}
		parts = append(parts, value)
	}
	return fmt.Sprintf("%v", parts)
}
