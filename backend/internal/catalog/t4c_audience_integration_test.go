//go:build integration

package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	t4cProgramCS  = "dddd4444-0000-0000-0000-000000000011"
	t4cProgramCPE = "dddd4444-0000-0000-0000-000000000012"
	t4cProgramEE  = "dddd4444-0000-0000-0000-000000000013"
	t4cProgramAUK = "dddd4444-0000-0000-0000-000000000014"
)

func seedT4CAudience(t *testing.T, p *pgxpool.Pool, ctx context.Context) {
	t.Helper()
	if _, err := p.Exec(ctx, `
		INSERT INTO programs (id, institution_id, slug, name_ar, name_en, degree_kind)
		VALUES
		 ($1, $5, 'cs', 'علوم الحاسوب', 'Computer Science', 'BACHELOR'),
		 ($2, $5, 'cpe', 'هندسة الحاسوب', 'Computer Engineering', 'BACHELOR'),
		 ($3, $5, 'ee', 'الهندسة الكهربائية', 'Electrical Engineering', 'BACHELOR'),
		 ($4, $6, 'auk-cs', 'علوم الحاسوب', 'AUK Computer Science', 'BACHELOR')`,
		t4cProgramCS, t4cProgramCPE, t4cProgramEE, t4cProgramAUK, t4aInstitutionKU, t4aInstitutionAUK); err != nil {
		t.Fatalf("seeding T4-C Programs: %v", err)
	}
	if _, err := p.Exec(ctx, `
		INSERT INTO curricula (id, program_id, institution_id, version_label, status)
		VALUES
		 ('eeee5555-0000-0000-0000-000000000011', $1, $5, '2026', 'ACTIVE'),
		 ('eeee5555-0000-0000-0000-000000000012', $2, $5, '2026', 'ACTIVE'),
		 ('eeee5555-0000-0000-0000-000000000013', $3, $5, '2026', 'ACTIVE'),
		 ('eeee5555-0000-0000-0000-000000000014', $4, $6, '2026', 'ACTIVE')`,
		t4cProgramCS, t4cProgramCPE, t4cProgramEE, t4cProgramAUK, t4aInstitutionKU, t4aInstitutionAUK); err != nil {
		t.Fatalf("seeding T4-C Curricula: %v", err)
	}
	if _, err := p.Exec(ctx, `
		INSERT INTO curriculum_subjects
		 (curriculum_id, subject_id, institution_id, requirement_kind, recommended_level)
		VALUES
		 ('eeee5555-0000-0000-0000-000000000011', $1, $3, 'MAJOR_CORE', 3),
		 ('eeee5555-0000-0000-0000-000000000012', $1, $3, 'SUPPORTING', 4),
		 ('eeee5555-0000-0000-0000-000000000014', $2, $4, 'MAJOR_CORE', 2)`,
		t4aSubjectA, t4aSubjectAUK, t4aInstitutionKU, t4aInstitutionAUK); err != nil {
		t.Fatalf("seeding T4-C mappings: %v", err)
	}
}

func setupT4C(t *testing.T) (*Repository, *pgxpool.Pool, context.Context, *Course) {
	t.Helper()
	freshSchema(t)
	p, ctx := pool(t)
	seedAcademicFixture(t, p, ctx)
	seedT4CAudience(t, p, ctx)
	repo := t4aRepo(t, p)
	return repo, p, ctx, createAcademicCourse(t, repo, ctx, stringPtr(t4aSubjectA))
}

func stringPtr(value string) *string { return &value }

func TestT4CAutomaticSubsetAndReset(t *testing.T) {
	repo, p, ctx, course := setupT4C(t)
	revision := course.EditableRevision
	if revision == nil {
		t.Fatal("new Course has no editable revision")
	}
	loaded, err := repo.GetOwnedCourse(ctx, course.ID, t4aInstructor)
	if err != nil {
		t.Fatalf("loading automatic audience: %v", err)
	}
	if loaded.EditableRevision.Audience == nil || loaded.EditableRevision.Audience.Mode != AudienceAutomatic {
		t.Fatalf("new Course audience = %#v, want AUTOMATIC", loaded.EditableRevision.Audience)
	}
	if len(loaded.EditableRevision.Audience.Programs) != 2 {
		t.Fatalf("automatic audience has %d Programs, want 2", len(loaded.EditableRevision.Audience.Programs))
	}
	var targetRows int
	if err := p.QueryRow(ctx, `SELECT count(*) FROM course_program_targets WHERE revision_id = $1`, revision.ID).Scan(&targetRows); err != nil {
		t.Fatal(err)
	}
	if targetRows != 0 {
		t.Fatalf("automatic audience materialized %d targets", targetRows)
	}

	custom, err := repo.SetRevisionAudience(ctx, SetRevisionAudienceRequest{
		CourseID: course.ID, RevisionID: revision.ID, OwnerAccountID: t4aInstructor,
		ProgramIDs: []string{t4cProgramCPE}, ActorDescriptor: "T4C Instructor",
	})
	if err != nil {
		t.Fatalf("customizing valid subset: %v", err)
	}
	if custom.Mode != AudienceCustomized || len(custom.Programs) != 1 || custom.Programs[0].ProgramID != t4cProgramCPE {
		t.Fatalf("custom audience = %#v", custom)
	}

	for name, programID := range map[string]string{
		"unrelated same-Institution Program": t4cProgramEE,
		"cross-Institution Program":          t4cProgramAUK,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := repo.SetRevisionAudience(ctx, SetRevisionAudienceRequest{
				CourseID: course.ID, RevisionID: revision.ID, OwnerAccountID: t4aInstructor,
				ProgramIDs: []string{programID}, ActorDescriptor: "T4C Instructor",
			})
			if !errors.Is(err, ErrAudienceTargetInvalid) {
				t.Fatalf("error = %v, want ErrAudienceTargetInvalid", err)
			}
		})
	}

	automatic, err := repo.ResetRevisionAudience(ctx, ResetRevisionAudienceRequest{
		CourseID: course.ID, RevisionID: revision.ID, OwnerAccountID: t4aInstructor,
		ActorDescriptor: "T4C Instructor",
	})
	if err != nil {
		t.Fatalf("resetting audience: %v", err)
	}
	if automatic.Mode != AudienceAutomatic || len(automatic.Programs) != 2 {
		t.Fatalf("reset audience = %#v", automatic)
	}
	if err := p.QueryRow(ctx, `SELECT count(*) FROM course_program_targets WHERE revision_id = $1`, revision.ID).Scan(&targetRows); err != nil {
		t.Fatal(err)
	}
	if targetRows != 0 {
		t.Fatalf("reset left %d targets", targetRows)
	}
}

func TestT4CCandidateCloneAndLiveIsolation(t *testing.T) {
	repo, p, ctx, course := setupT4C(t)
	liveID := course.EditableRevision.ID
	if _, err := repo.SetRevisionAudience(ctx, SetRevisionAudienceRequest{
		CourseID: course.ID, RevisionID: liveID, OwnerAccountID: t4aInstructor,
		ProgramIDs: []string{t4cProgramCS, t4cProgramCPE}, ActorDescriptor: "T4C Instructor",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exec(ctx, `UPDATE course_revisions SET state = 'APPROVED' WHERE id = $1`, liveID); err != nil {
		t.Fatalf("approving fixture revision: %v", err)
	}
	if _, err := p.Exec(ctx,
		`UPDATE courses SET lifecycle = 'PUBLISHED', live_revision_id = $1 WHERE id = $2`,
		liveID, course.ID); err != nil {
		t.Fatalf("publishing fixture Course: %v", err)
	}
	candidate, err := repo.CreateCandidate(ctx, course.ID, t4aInstructor, "T4C Instructor")
	if err != nil {
		t.Fatalf("creating candidate: %v", err)
	}
	if candidate.Audience == nil || candidate.Audience.Mode != AudienceCustomized || len(candidate.Audience.Programs) != 2 {
		t.Fatalf("cloned audience = %#v", candidate.Audience)
	}
	if _, err := repo.SetRevisionAudience(ctx, SetRevisionAudienceRequest{
		CourseID: course.ID, RevisionID: candidate.ID, OwnerAccountID: t4aInstructor,
		ProgramIDs: []string{t4cProgramCS}, ActorDescriptor: "T4C Instructor",
	}); err != nil {
		t.Fatalf("editing candidate audience: %v", err)
	}
	var liveCount, candidateCount int
	if err := p.QueryRow(ctx, `SELECT count(*) FROM course_program_targets WHERE revision_id = $1`, liveID).Scan(&liveCount); err != nil {
		t.Fatal(err)
	}
	if err := p.QueryRow(ctx, `SELECT count(*) FROM course_program_targets WHERE revision_id = $1`, candidate.ID).Scan(&candidateCount); err != nil {
		t.Fatal(err)
	}
	if liveCount != 2 || candidateCount != 1 {
		t.Fatalf("live/candidate targets = %d/%d, want 2/1", liveCount, candidateCount)
	}
}

func TestT4CApprovalValidationFailsClosedAfterMappingDisappears(t *testing.T) {
	repo, p, ctx, course := setupT4C(t)
	revision := course.EditableRevision
	if _, err := repo.SetRevisionAudience(ctx, SetRevisionAudienceRequest{
		CourseID: course.ID, RevisionID: revision.ID, OwnerAccountID: t4aInstructor,
		ProgramIDs: []string{t4cProgramCS}, ActorDescriptor: "T4C Instructor",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exec(ctx, `
		DELETE FROM curriculum_subjects
		WHERE curriculum_id = 'eeee5555-0000-0000-0000-000000000011'
		  AND subject_id = $1`, t4aSubjectA); err != nil {
		t.Fatalf("removing mapping: %v", err)
	}
	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	row, err := repo.LockCourse(ctx, tx, course.ID)
	if err != nil {
		t.Fatal(err)
	}
	validation, err := validateCourseForSubmission(ctx, submissionValidationRequest{
		tx: tx, validator: newTxAssetVersionValidator(tx), courseID: course.ID,
		revision: revision, course: row,
	})
	if err != nil {
		t.Fatalf("validation error: %v", err)
	}
	if validation == nil {
		t.Fatal("mapping removal did not fail validation")
	}
	found := false
	for _, violation := range validation.Violations {
		if violation.Code == "ACADEMIC_AUDIENCE_TARGET_UNAVAILABLE" {
			found = true
		}
	}
	if !found {
		t.Fatalf("violations = %#v, want ACADEMIC_AUDIENCE_TARGET_UNAVAILABLE", validation.Violations)
	}
}

func TestT4CApproveCourseRevalidatesAudienceAndLeavesLiveStateUnchanged(t *testing.T) {
	f := newApprovalPricingFixture(t)
	institutionID := "aaaa1111-0000-0000-0000-000000000091"
	subjectID := "bbbb2222-0000-0000-0000-000000000091"
	programID := "dddd4444-0000-0000-0000-000000000091"
	curriculumID := "eeee5555-0000-0000-0000-000000000091"
	if _, err := f.p.Exec(f.ctx, `
		INSERT INTO institutions (id, country_code, slug, name_ar, name_en)
		VALUES ($1, 'KW', 'approval-audience-university', 'جامعة', 'Approval University')`, institutionID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(f.ctx, `
		INSERT INTO subjects (id, institution_id, official_code, title_ar, title_en)
		VALUES ($1, $2, 'AUD-101', 'مادة', 'Audience Subject')`, subjectID, institutionID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(f.ctx, `
		INSERT INTO programs (id, institution_id, slug, name_ar, name_en, degree_kind)
		VALUES ($1, $2, 'audience-program', 'تخصص', 'Audience Program', 'BSC')`, programID, institutionID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(f.ctx, `
		INSERT INTO curricula (id, program_id, institution_id, version_label, status)
		VALUES ($1, $2, $3, '2026', 'ACTIVE')`, curriculumID, programID, institutionID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(f.ctx, `
		INSERT INTO curriculum_subjects (curriculum_id, subject_id, institution_id, requirement_kind)
		VALUES ($1, $2, $3, 'MAJOR_CORE')`, curriculumID, subjectID, institutionID); err != nil {
		t.Fatal(err)
	}
	// Convert this complete submitted fixture to the Academic model atomically;
	// the legacy terms are cleared because an Academic revision never owns them.
	if _, err := f.p.Exec(f.ctx, `
		UPDATE course_revisions
		SET major_term_id = NULL, subject_term_id = NULL, study_year = NULL
		WHERE id = $1`, f.revisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(f.ctx, `
		UPDATE courses
		SET classification_model = 'ACADEMIC_CATALOG', institution_id = $1, subject_id = $2
		WHERE id = $3`, institutionID, subjectID, f.courseID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(f.ctx, `
		INSERT INTO course_program_targets (revision_id, course_id, program_id, institution_id)
		VALUES ($1, $2, $3, $4)`, f.revisionID, f.courseID, programID, institutionID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.SetCoursePrice(f.ctx, SetCoursePriceRequest{
		CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
		PriceMinorUnits: 25000, Reason: "T4-C approval revalidation fixture",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(f.ctx, `
		DELETE FROM curriculum_subjects WHERE curriculum_id = $1 AND subject_id = $2`,
		curriculumID, subjectID); err != nil {
		t.Fatal(err)
	}
	before := f.snapshot(t)
	err := f.approve(t)
	var validation *SubmissionValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("ApproveCourse error = %v, want SubmissionValidationError", err)
	}
	found := false
	for _, violation := range validation.Violations {
		if violation.Code == "ACADEMIC_AUDIENCE_TARGET_UNAVAILABLE" {
			found = true
		}
	}
	if !found {
		t.Fatalf("approval violations = %#v", validation.Violations)
	}
	after := f.snapshot(t)
	if after != before {
		t.Fatalf("failed approval mutated live state\nbefore=%+v\nafter=%+v", before, after)
	}
}
