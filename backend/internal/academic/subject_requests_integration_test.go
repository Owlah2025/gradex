//go:build integration

package academic

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type subjectRequestFixture struct {
	*profileFixture
	ctx              context.Context
	instructor       string
	otherInstructor  string
	admin            string
	subjectA         string
	subjectB         string
	otherInstitution string
	otherSubject     string
}

func newSubjectRequestFixture(t *testing.T) *subjectRequestFixture {
	t.Helper()
	base := newProfileFixture(t)
	f := &subjectRequestFixture{profileFixture: base, ctx: context.Background()}
	account := func(email, role string) string {
		var id string
		if err := f.pool.QueryRow(f.ctx, `
			INSERT INTO accounts (normalized_email, email, role, status, display_name)
			VALUES ($1, $1, $2, 'ACTIVE', $3) RETURNING id::text`,
			email, role, email).Scan(&id); err != nil {
			t.Fatalf("seeding %s: %v", role, err)
		}
		return id
	}
	f.instructor = account("t4d-instructor@example.test", "INSTRUCTOR")
	f.otherInstructor = account("t4d-other@example.test", "INSTRUCTOR")
	f.admin = account("t4d-admin@example.test", "ADMIN")
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO subjects (institution_id, official_code, title_ar, title_en)
		VALUES ($1, 'T4D-101', 'المادة أ', 'Subject A') RETURNING id::text`, f.institution).Scan(&f.subjectA); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO subjects (institution_id, official_code, title_ar, title_en)
		VALUES ($1, 'T4D-102', 'المادة ب', 'Subject B') RETURNING id::text`, f.institution).Scan(&f.subjectB); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO institutions (country_code, slug, name_ar, name_en)
		VALUES ('KW', 't4d-other-university', 'جامعة أخرى', 'Other University') RETURNING id::text`).Scan(&f.otherInstitution); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO subjects (institution_id, official_code, title_ar, title_en)
		VALUES ($1, 'OTHER-101', 'مادة أخرى', 'Other Subject') RETURNING id::text`, f.otherInstitution).Scan(&f.otherSubject); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *subjectRequestFixture) course(t *testing.T, owner string) (string, string) {
	t.Helper()
	var courseID, revisionID string
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO courses (owner_account_id, lifecycle, classification_model, institution_id)
		VALUES ($1, 'DRAFT', 'ACADEMIC_CATALOG', $2) RETURNING id::text`,
		owner, f.institution).Scan(&courseID); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO course_revisions (course_id, state, revision_number, title_ar, title_en, description_ar, description_en)
		VALUES ($1, 'DRAFT', 1, 'كورس', 'Course', '', '') RETURNING id::text`, courseID).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	return courseID, revisionID
}

func (f *subjectRequestFixture) request(t *testing.T, courseID, code string) *SubjectRequest {
	t.Helper()
	request, err := f.repo.CreateSubjectRequest(f.ctx, CreateSubjectRequestWorkflow{
		RequesterAccountID: f.instructor, ActorDescriptor: "T4D Instructor",
		InstitutionID: f.institution, CourseID: &courseID,
		ProposedOfficialCode: &code,
		ProposedTitleAr:      "مادة مطلوبة", ProposedTitleEn: "Requested Subject",
		Note: stringPointer("Needed for the Course draft"),
	})
	if err != nil {
		t.Fatalf("creating Subject request: %v", err)
	}
	return request
}

func stringPointer(value string) *string { return &value }

func (f *subjectRequestFixture) actor() Actor {
	return Actor{AdminAccountID: f.admin, ActorDescriptor: "T4D Admin"}
}

func expectSubjectRequestAudit(t *testing.T, f *subjectRequestFixture, requestID, action string) {
	t.Helper()
	var count int
	var containsNote bool
	if err := f.pool.QueryRow(f.ctx, `
		SELECT count(*), COALESCE(bool_or(metadata ? 'note'), false)
		FROM audit_events
		WHERE target_id = $1 AND action = $2`, requestID, action).Scan(&count, &containsNote); err != nil {
		t.Fatal(err)
	}
	if count != 1 || containsNote {
		t.Fatalf("audit %s count/contains-note = %d/%t", action, count, containsNote)
	}
}

func TestT4DInstructorOwnCreateReadAndPendingUniqueness(t *testing.T) {
	f := newSubjectRequestFixture(t)
	courseID, _ := f.course(t, f.instructor)
	created := f.request(t, courseID, "T4D-201")
	if created.Status != SubjectRequestPending || created.CourseTitleEn == nil || *created.CourseTitleEn != "Course" {
		t.Fatalf("created request projection = %#v", created)
	}
	expectSubjectRequestAudit(t, f, created.ID, "SUBJECT_REQUEST_CREATED")
	listed, err := f.repo.ListSubjectRequests(f.ctx, ListSubjectRequestsRequest{
		RequesterAccountID: f.instructor, CourseID: &courseID,
	})
	if err != nil || len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("own requests = %#v, err=%v", listed, err)
	}
	if _, err := f.repo.CreateSubjectRequest(f.ctx, CreateSubjectRequestWorkflow{
		RequesterAccountID: f.instructor, InstitutionID: f.institution, CourseID: &courseID,
		ProposedTitleAr: "مكرر", ProposedTitleEn: "Duplicate pending",
	}); !errors.Is(err, ErrSubjectRequestPendingExists) {
		t.Fatalf("second pending error = %v", err)
	}
	if _, err := f.repo.CreateSubjectRequest(f.ctx, CreateSubjectRequestWorkflow{
		RequesterAccountID: f.otherInstructor, InstitutionID: f.institution, CourseID: &courseID,
		ProposedTitleAr: "غير مصرح", ProposedTitleEn: "Unauthorized",
	}); !errors.Is(err, ErrSubjectRequestOwnerMismatch) {
		t.Fatalf("other Instructor error = %v", err)
	}
	if _, err := f.repo.CreateSubjectRequest(f.ctx, CreateSubjectRequestWorkflow{
		RequesterAccountID: f.student, InstitutionID: f.institution,
		ProposedTitleAr: "طالب", ProposedTitleEn: "Student",
	}); !errors.Is(err, ErrSubjectRequestInstructorOnly) {
		t.Fatalf("Student error = %v", err)
	}
}

func TestT4DLinkExistingAndApproveNew(t *testing.T) {
	f := newSubjectRequestFixture(t)
	courseID, _ := f.course(t, f.instructor)
	request := f.request(t, courseID, "T4D-202")
	linked, err := f.repo.LinkSubjectRequest(f.ctx, LinkSubjectRequest{
		Actor: f.actor(), RequestID: request.ID, SubjectID: f.subjectB,
	})
	if err != nil || linked.Status != SubjectRequestLinkedExisting {
		t.Fatalf("link result = %#v, err=%v", linked, err)
	}
	var courseSubject string
	if err := f.pool.QueryRow(f.ctx, `SELECT subject_id::text FROM courses WHERE id = $1`, courseID).Scan(&courseSubject); err != nil {
		t.Fatal(err)
	}
	if courseSubject != f.subjectB {
		t.Fatalf("Course Subject = %s, want linked Subject", courseSubject)
	}
	expectSubjectRequestAudit(t, f, request.ID, "SUBJECT_REQUEST_LINKED_EXISTING")

	newCourseID, _ := f.course(t, f.instructor)
	newRequest := f.request(t, newCourseID, "T4D-NEW-301")
	approved, err := f.repo.ApproveSubjectRequestAsNew(f.ctx, ApproveSubjectRequestAsNew{
		Actor: f.actor(), RequestID: newRequest.ID,
	})
	if err != nil || approved.Status != SubjectRequestApprovedNew || approved.ResolvedSubjectID == nil {
		t.Fatalf("approve-new result = %#v, err=%v", approved, err)
	}
	var count int
	if err := f.pool.QueryRow(f.ctx, `
		SELECT count(*) FROM subjects
		WHERE institution_id = $1 AND code_normalized = academic_normalize_code('T4D-NEW-301')`,
		f.institution).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("approve-new created %d canonical Subjects", count)
	}
	expectSubjectRequestAudit(t, f, newRequest.ID, "SUBJECT_REQUEST_APPROVED_NEW")

	duplicateCourseID, _ := f.course(t, f.instructor)
	duplicateRequest := f.request(t, duplicateCourseID, "T4D-NEW-301")
	_, err = f.repo.ApproveSubjectRequestAsNew(f.ctx, ApproveSubjectRequestAsNew{
		Actor: f.actor(), RequestID: duplicateRequest.ID,
	})
	var duplicate *DuplicateSubjectError
	if !errors.As(err, &duplicate) || duplicate.Existing == nil {
		t.Fatalf("duplicate approval error = %v", err)
	}
	var status string
	if err := f.pool.QueryRow(f.ctx, `SELECT status::text FROM subject_requests WHERE id = $1`, duplicateRequest.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != string(SubjectRequestPending) {
		t.Fatalf("duplicate request status = %s, want PENDING", status)
	}
}

func TestT4DRejectRequiresReasonAndKeepsCourseDraft(t *testing.T) {
	f := newSubjectRequestFixture(t)
	courseID, _ := f.course(t, f.instructor)
	request := f.request(t, courseID, "T4D-401")
	if _, err := f.repo.RejectSubjectRequest(f.ctx, RejectSubjectRequest{
		Actor: f.actor(), RequestID: request.ID,
	}); !errors.Is(err, ErrSubjectRequestRejectReason) {
		t.Fatalf("blank rejection error = %v", err)
	}
	rejected, err := f.repo.RejectSubjectRequest(f.ctx, RejectSubjectRequest{
		Actor: f.actor(), RequestID: request.ID, Reason: "The proposed title is not official.",
	})
	if err != nil || rejected.Status != SubjectRequestRejected || rejected.ResolutionReason == nil {
		t.Fatalf("rejected = %#v, err=%v", rejected, err)
	}
	var subjectID *string
	var lifecycle string
	if err := f.pool.QueryRow(f.ctx, `SELECT subject_id::text, lifecycle::text FROM courses WHERE id = $1`, courseID).Scan(&subjectID, &lifecycle); err != nil {
		t.Fatal(err)
	}
	if subjectID != nil || lifecycle != "DRAFT" {
		t.Fatalf("rejected Course subject/lifecycle = %v/%s", subjectID, lifecycle)
	}
	expectSubjectRequestAudit(t, f, request.ID, "SUBJECT_REQUEST_REJECTED")
}

func TestT4DResolutionRaceNeverOverwritesInstructorChoice(t *testing.T) {
	f := newSubjectRequestFixture(t)
	courseID, _ := f.course(t, f.instructor)
	request := f.request(t, courseID, "T4D-501")
	if _, err := f.pool.Exec(f.ctx, `UPDATE courses SET subject_id = $1 WHERE id = $2`, f.subjectA, courseID); err != nil {
		t.Fatal(err)
	}
	resolved, err := f.repo.LinkSubjectRequest(f.ctx, LinkSubjectRequest{
		Actor: f.actor(), RequestID: request.ID, SubjectID: f.subjectB,
	})
	var conflict *SubjectRequestCourseConflictError
	if !errors.As(err, &conflict) || resolved == nil || resolved.Status != SubjectRequestLinkedExisting {
		t.Fatalf("race result = %#v, err=%v", resolved, err)
	}
	var courseSubject string
	if err := f.pool.QueryRow(f.ctx, `SELECT subject_id::text FROM courses WHERE id = $1`, courseID).Scan(&courseSubject); err != nil {
		t.Fatal(err)
	}
	if courseSubject != f.subjectA {
		t.Fatalf("race overwrote Course Subject with %s", courseSubject)
	}
	if resolved.ResolutionReason == nil || !strings.Contains(*resolved.ResolutionReason, "not reassigned") {
		t.Fatalf("race reason = %v", resolved.ResolutionReason)
	}
}

func TestT4DResolutionInstitutionAndPublishedSafety(t *testing.T) {
	f := newSubjectRequestFixture(t)
	courseID, revisionID := f.course(t, f.instructor)
	request := f.request(t, courseID, "T4D-601")
	if _, err := f.repo.LinkSubjectRequest(f.ctx, LinkSubjectRequest{
		Actor: f.actor(), RequestID: request.ID, SubjectID: f.otherSubject,
	}); !errors.Is(err, ErrCrossInstitution) {
		t.Fatalf("cross-Institution resolution error = %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE course_revisions SET state = 'APPROVED' WHERE id = $1`, revisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE courses SET lifecycle = 'PUBLISHED', live_revision_id = $1 WHERE id = $2`, revisionID, courseID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.LinkSubjectRequest(f.ctx, LinkSubjectRequest{
		Actor: f.actor(), RequestID: request.ID, SubjectID: f.subjectB,
	}); !errors.Is(err, ErrSubjectRequestCourseInvalid) {
		t.Fatalf("published Course resolution error = %v", err)
	}
	var status string
	if err := f.pool.QueryRow(f.ctx, `SELECT status::text FROM subject_requests WHERE id = $1`, request.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != string(SubjectRequestPending) {
		t.Fatalf("failed published resolution changed request to %s", status)
	}
}
