//go:build integration

package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type taxonomyFixture struct {
	repo       *Repository
	pool       *pgxpool.Pool
	ctx        context.Context
	adminID    string
	instructor string
	courseID   string
	revisionID string
	majorID    string
	subjectID  string
}

func newTaxonomyFixture(t *testing.T) taxonomyFixture {
	t.Helper()
	freshSchema(t)
	pool, ctx := pool(t)
	adminID := "71000000-0000-0000-0000-000000000001"
	instructorID := "71000000-0000-0000-0000-000000000002"
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, role, status, email, normalized_email, display_name)
		VALUES
			($1::uuid, 'ADMIN', 'ACTIVE', 'taxonomy-admin@example.com', 'taxonomy-admin@example.com', 'Admin'),
			($2::uuid, 'INSTRUCTOR', 'ACTIVE', 'taxonomy-instructor@example.com', 'taxonomy-instructor@example.com', 'Instructor')
	`, adminID, instructorID); err != nil {
		t.Fatalf("seeding taxonomy accounts: %v", err)
	}
	repo, err := NewRepository(pool, testOutboxWriter(t))
	if err != nil {
		t.Fatalf("creating taxonomy repository: %v", err)
	}
	courseID, revisionID := createTaxonomyCourse(t, repo, ctx, instructorID, "Taxonomy Course")
	major, err := repo.CreateTaxonomyTerm(ctx, CreateTaxonomyTermRequest{AdminAccountID: adminID, ActorDescriptor: adminID, Kind: TaxonomyMajor, LabelAr: "علوم", LabelEn: "Science"})
	if err != nil {
		t.Fatalf("creating major term: %v", err)
	}
	subject, err := repo.CreateTaxonomyTerm(ctx, CreateTaxonomyTermRequest{AdminAccountID: adminID, ActorDescriptor: adminID, Kind: TaxonomySubject, LabelAr: "فيزياء", LabelEn: "Physics"})
	if err != nil {
		t.Fatalf("creating subject term: %v", err)
	}
	return taxonomyFixture{repo: repo, pool: pool, ctx: ctx, adminID: adminID, instructor: instructorID, courseID: courseID, revisionID: revisionID, majorID: major.ID, subjectID: subject.ID}
}

func createTaxonomyCourse(t *testing.T, repo *Repository, ctx context.Context, ownerID, title string) (string, string) {
	t.Helper()
	course, err := repo.CreateCourse(ctx, CreateCourseRequest{OwnerAccountID: ownerID, TitleAr: "دورة", TitleEn: title, DescriptionAr: "وصف", DescriptionEn: "Description"}, ownerID)
	if err != nil {
		t.Fatalf("creating course %q: %v", title, err)
	}
	owned, err := repo.GetOwnedCourse(ctx, course.ID, ownerID)
	if err != nil || owned.EditableRevision == nil {
		t.Fatalf("loading candidate for %q: course=%+v err=%v", title, owned, err)
	}
	return course.ID, owned.EditableRevision.ID
}

func assignTaxonomy(t *testing.T, f taxonomyFixture, courseID, revisionID, majorID, subjectID string) {
	t.Helper()
	if _, err := f.repo.AssignTaxonomyToRevision(f.ctx, AssignTaxonomyRequest{CourseID: courseID, RevisionID: revisionID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID, MajorTermID: majorID, SubjectTermID: subjectID}); err != nil {
		t.Fatalf("assigning taxonomy: %v", err)
	}
}

func TestTaxonomyReferencesRemainStableAcrossRenameRetirementAndDeleteRefusal(t *testing.T) {
	f := newTaxonomyFixture(t)
	assignTaxonomy(t, f, f.courseID, f.revisionID, f.majorID, f.subjectID)
	secondCourseID, secondRevisionID := createTaxonomyCourse(t, f.repo, f.ctx, f.instructor, "Second Taxonomy Course")
	assignTaxonomy(t, f, secondCourseID, secondRevisionID, f.majorID, f.subjectID)

	renamed, err := f.repo.RenameTaxonomyTerm(f.ctx, RenameTaxonomyTermRequest{TermID: f.majorID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID, LabelAr: "علوم محدثة", LabelEn: "Updated Science"})
	if err != nil {
		t.Fatalf("renaming term: %v", err)
	}
	if renamed.ID != f.majorID || renamed.LabelEn != "Updated Science" {
		t.Fatalf("rename result = %+v", renamed)
	}

	var referenceCount int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM course_revisions WHERE major_term_id = $1::uuid`, f.majorID).Scan(&referenceCount); err != nil || referenceCount != 2 {
		t.Fatalf("major reference count = %d (err=%v), want 2", referenceCount, err)
	}
	for _, revisionID := range []string{f.revisionID, secondRevisionID} {
		var assignedMajor, assignedSubject string
		if err := f.pool.QueryRow(f.ctx, `
			SELECT major_term_id::text, subject_term_id::text
			FROM course_revisions
			WHERE id = $1::uuid
		`, revisionID).Scan(&assignedMajor, &assignedSubject); err != nil || assignedMajor != f.majorID || assignedSubject != f.subjectID {
			t.Fatalf("rename rewrote revision %s assignment to %s/%s (err=%v), want %s/%s", revisionID, assignedMajor, assignedSubject, err, f.majorID, f.subjectID)
		}
	}
	var createdAudits, renamedAudits int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE action = 'TAXONOMY_TERM_CREATED' AND target_id = $1`, f.majorID).Scan(&createdAudits); err != nil || createdAudits != 1 {
		t.Fatalf("major creation audit rows = %d (err=%v), want 1", createdAudits, err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE action = 'TAXONOMY_TERM_RENAMED' AND target_id = $1`, f.majorID).Scan(&renamedAudits); err != nil || renamedAudits != 1 {
		t.Fatalf("major rename audit rows = %d (err=%v), want 1", renamedAudits, err)
	}
	if err := f.repo.DeleteTaxonomyTerm(f.ctx, DeleteTaxonomyTermRequest{TermID: f.majorID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID}); !errors.Is(err, ErrTaxonomyTermReferenced) {
		t.Fatalf("referenced term delete error = %v, want %v", err, ErrTaxonomyTermReferenced)
	}

	retired, err := f.repo.RetireTaxonomyTerm(f.ctx, RetireTaxonomyTermRequest{TermID: f.majorID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID})
	if err != nil || retired.RetiredAt == nil {
		t.Fatalf("retiring referenced term = %+v, %v", retired, err)
	}
	var assignmentsAfterRetirement int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM course_revisions WHERE major_term_id = $1::uuid`, f.majorID).Scan(&assignmentsAfterRetirement); err != nil || assignmentsAfterRetirement != 2 {
		t.Fatalf("retired term assignments = %d (err=%v), want 2", assignmentsAfterRetirement, err)
	}
	var retiredAudits int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE action = 'TAXONOMY_TERM_RETIRED' AND target_id = $1`, f.majorID).Scan(&retiredAudits); err != nil || retiredAudits != 1 {
		t.Fatalf("major retirement audit rows = %d (err=%v), want 1", retiredAudits, err)
	}
	var displayedLabel string
	var displayedTermRetired bool
	if err := f.pool.QueryRow(f.ctx, `
		SELECT terms.label_en, terms.retired_at IS NOT NULL
		FROM course_revisions AS revisions
		JOIN taxonomy_terms AS terms ON terms.id = revisions.major_term_id
		WHERE revisions.id = $1::uuid
	`, f.revisionID).Scan(&displayedLabel, &displayedTermRetired); err != nil || displayedLabel != "Updated Science" || !displayedTermRetired {
		t.Fatalf("retired assigned term display = %q/%v (err=%v), want Updated Science/true", displayedLabel, displayedTermRetired, err)
	}
	if _, err := f.repo.AssignTaxonomyToRevision(f.ctx, AssignTaxonomyRequest{CourseID: f.courseID, RevisionID: f.revisionID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID, MajorTermID: f.majorID, SubjectTermID: f.subjectID}); !errors.Is(err, ErrTaxonomyTermUnavailable) {
		t.Fatalf("assigning retired term error = %v, want %v", err, ErrTaxonomyTermUnavailable)
	}
}

func TestTaxonomyDeletionAndAuditAreAtomic(t *testing.T) {
	f := newTaxonomyFixture(t)
	unused, err := f.repo.CreateTaxonomyTerm(f.ctx, CreateTaxonomyTermRequest{AdminAccountID: f.adminID, ActorDescriptor: f.adminID, Kind: TaxonomyMajor, LabelAr: "رياضيات", LabelEn: "Mathematics"})
	if err != nil {
		t.Fatalf("creating unreferenced term: %v", err)
	}
	if err := f.repo.DeleteTaxonomyTerm(f.ctx, DeleteTaxonomyTermRequest{TermID: unused.ID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID}); err != nil {
		t.Fatalf("deleting unreferenced term: %v", err)
	}
	var deletedRows, deletionAudits int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM taxonomy_terms WHERE id = $1::uuid`, unused.ID).Scan(&deletedRows); err != nil || deletedRows != 0 {
		t.Fatalf("deleted term rows = %d (err=%v), want 0", deletedRows, err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE action = 'TAXONOMY_TERM_DELETED' AND target_id = $1`, unused.ID).Scan(&deletionAudits); err != nil || deletionAudits != 1 {
		t.Fatalf("deletion audit rows = %d (err=%v), want 1", deletionAudits, err)
	}

	if _, err := f.repo.RetireTaxonomyTerm(f.ctx, RetireTaxonomyTermRequest{TermID: f.subjectID, AdminAccountID: "71000000-0000-0000-0000-000000000099", ActorDescriptor: "missing-admin"}); err == nil {
		t.Fatal("retirement with an audit FK failure succeeded")
	}
	var retired bool
	if err := f.pool.QueryRow(f.ctx, `SELECT retired_at IS NOT NULL FROM taxonomy_terms WHERE id = $1::uuid`, f.subjectID).Scan(&retired); err != nil || retired {
		t.Fatalf("failed-audit retirement persisted=%v err=%v, want false", retired, err)
	}
	var failedRetirementAudits int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE action = 'TAXONOMY_TERM_RETIRED' AND target_id = $1`, f.subjectID).Scan(&failedRetirementAudits); err != nil || failedRetirementAudits != 0 {
		t.Fatalf("failed-audit retirement audit rows = %d (err=%v), want 0", failedRetirementAudits, err)
	}
}

func TestTaxonomyAssignmentsRequireExactEligibleRevisionAndMatchingKinds(t *testing.T) {
	f := newTaxonomyFixture(t)
	validator := NewDBAssetVersionValidator(f.pool)
	majorID, subjectID := f.majorID, f.subjectID
	if _, err := f.repo.UpdateCourseRevision(f.ctx, validator, UpdateRevisionRequest{CourseID: f.courseID, RevisionID: f.revisionID, OwnerAccountID: f.instructor, MajorTermID: &majorID, SubjectTermID: &subjectID}, f.instructor); err != nil {
		t.Fatalf("owner explicit candidate assignment: %v", err)
	}
	if _, err := f.repo.UpdateCourseRevision(f.ctx, validator, UpdateRevisionRequest{CourseID: f.courseID, RevisionID: "71000000-0000-0000-0000-000000000099", OwnerAccountID: f.instructor, MajorTermID: &majorID, SubjectTermID: &subjectID}, f.instructor); !errors.Is(err, ErrCourseNotFound) {
		t.Fatalf("implicit/latest substitute error = %v, want %v", err, ErrCourseNotFound)
	}
	if _, err := f.repo.UpdateCourseRevision(f.ctx, validator, UpdateRevisionRequest{CourseID: f.courseID, RevisionID: f.revisionID, OwnerAccountID: "71000000-0000-0000-0000-000000000099", MajorTermID: &majorID, SubjectTermID: &subjectID}, f.instructor); !errors.Is(err, ErrCourseNotFound) {
		t.Fatalf("non-owner assignment error = %v, want %v", err, ErrCourseNotFound)
	}
	if _, err := f.repo.AssignTaxonomyToRevision(f.ctx, AssignTaxonomyRequest{CourseID: f.courseID, RevisionID: f.revisionID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID, MajorTermID: f.subjectID, SubjectTermID: f.majorID}); !errors.Is(err, ErrTaxonomyTermKindMismatch) {
		t.Fatalf("crossed taxonomy kinds error = %v, want %v", err, ErrTaxonomyTermKindMismatch)
	}

	secondCourseID, secondRevisionID := createTaxonomyCourse(t, f.repo, f.ctx, f.instructor, "Cross Course")
	if _, err := f.repo.AssignTaxonomyToRevision(f.ctx, AssignTaxonomyRequest{CourseID: f.courseID, RevisionID: secondRevisionID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID, MajorTermID: f.majorID, SubjectTermID: f.subjectID}); !errors.Is(err, ErrCourseNotFound) {
		t.Fatalf("cross-course revision override error = %v, want %v", err, ErrCourseNotFound)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE course_revisions SET state = 'REJECTED', review_reason = 'terminal' WHERE id = $1::uuid`, secondRevisionID); err != nil {
		t.Fatalf("making candidate terminal: %v", err)
	}
	if _, err := f.repo.AssignTaxonomyToRevision(f.ctx, AssignTaxonomyRequest{CourseID: secondCourseID, RevisionID: secondRevisionID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID, MajorTermID: f.majorID, SubjectTermID: f.subjectID}); !errors.Is(err, ErrTaxonomyRevisionInvalid) {
		t.Fatalf("terminal revision override error = %v, want %v", err, ErrTaxonomyRevisionInvalid)
	}

	liveCourseID, liveRevisionID := createTaxonomyCourse(t, f.repo, f.ctx, f.instructor, "Live Course")
	if _, err := f.pool.Exec(f.ctx, `UPDATE course_revisions SET state = 'APPROVED' WHERE id = $1::uuid`, liveRevisionID); err != nil {
		t.Fatalf("approving live fixture revision: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE courses SET lifecycle = 'PUBLISHED', live_revision_id = $1::uuid WHERE id = $2::uuid`, liveRevisionID, liveCourseID); err != nil {
		t.Fatalf("publishing live fixture course: %v", err)
	}
	if _, err := f.repo.AssignTaxonomyToRevision(f.ctx, AssignTaxonomyRequest{CourseID: liveCourseID, RevisionID: liveRevisionID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID, MajorTermID: f.majorID, SubjectTermID: f.subjectID}); err != nil {
		t.Fatalf("live revision override: %v", err)
	}
}
