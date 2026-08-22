//go:build integration

package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// T4-A (MVP-F20) Course Academic Identity Foundation.
//
// These cases prove the D-093 identity model against real Postgres: the
// classification discriminator, the Institution/Subject invariant, the Subject
// lifecycle, post-publication immutability at both the domain and database
// layers, and the coexistence rule that keeps every legacy Course working.
//
// SCOPE. Program audience behaviour (T4-C) and the Subject request workflow
// (T4-D) are not exercised here beyond their schema constraints, which live in
// t4a_future_schema_integration_test.go and are labelled SCHEMA_PROVEN_ONLY.

const (
	t4aInstitutionKU  = "aaaa1111-0000-0000-0000-000000000001"
	t4aInstitutionAUK = "aaaa1111-0000-0000-0000-000000000002"
	t4aSubjectA       = "bbbb2222-0000-0000-0000-000000000001"
	t4aSubjectB       = "bbbb2222-0000-0000-0000-000000000002"
	t4aSubjectAUK     = "bbbb2222-0000-0000-0000-000000000003"
	t4aInstructor     = "cccc3333-0000-0000-0000-000000000001"
	t4aAdmin          = "cccc3333-0000-0000-0000-000000000002"
)

// seedAcademicFixture creates two Institutions and three Subjects so that
// cross-Institution refusals are provable rather than assumed.
func seedAcademicFixture(t *testing.T, p *pgxpool.Pool, ctx context.Context) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := p.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seeding academic fixture: %v", err)
		}
	}
	exec(`INSERT INTO institutions (id, country_code, slug, name_ar, name_en)
	      VALUES ($1, 'KW', 'kuwait-university', 'جامعة الكويت', 'Kuwait University'),
	             ($2, 'KW', 'auk', 'الأمريكية', 'AUK')`, t4aInstitutionKU, t4aInstitutionAUK)
	exec(`INSERT INTO subjects (id, institution_id, official_code, title_ar, title_en)
	      VALUES ($1, $4, '0418-320', 'مبادئ نظم الحاسوب', 'Principles of Computer Systems'),
	             ($2, $4, '0418-321', 'نظم التشغيل', 'Operating Systems'),
	             ($3, $5, '0418-320', 'مادة أخرى', 'Another Institution Subject')`,
		t4aSubjectA, t4aSubjectB, t4aSubjectAUK, t4aInstitutionKU, t4aInstitutionAUK)
	exec(`INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
	      VALUES ($1, 'ins@t4a.test', 'ins@t4a.test', 'INSTRUCTOR', 'ACTIVE', 'T4A Instructor'),
	             ($2, 'adm@t4a.test', 'adm@t4a.test', 'ADMIN', 'ACTIVE', 'T4A Admin')`,
		t4aInstructor, t4aAdmin)
}

func t4aRepo(t *testing.T, p *pgxpool.Pool) *Repository {
	t.Helper()
	repo, err := NewRepository(p, testOutboxWriter(t))
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	return repo
}

func createAcademicCourse(t *testing.T, repo *Repository, ctx context.Context, subject *string) *Course {
	t.Helper()
	course, err := repo.CreateCourse(ctx, CreateCourseRequest{
		OwnerAccountID: t4aInstructor,
		TitleAr:        "كورس أكاديمي", TitleEn: "Academic Course",
		Academic: &AcademicCourseContext{InstitutionID: t4aInstitutionKU, SubjectID: subject},
	}, "T4A Instructor")
	if err != nil {
		t.Fatalf("creating academic course: %v", err)
	}
	return course
}

// --- 1..3, 17. Classification model -------------------------------------

// T4A-1/T4A-2: the migration classifies every pre-existing Course as legacy and
// changes nothing else about it.
func TestT4AExistingCourseIsClassifiedLegacyAndUnchanged(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)
	_, courseID := seedInstructorAndCourse(t, p, ctx)

	var model string
	var institution, subject *string
	if err := p.QueryRow(ctx, `
		SELECT classification_model::text, institution_id::text, subject_id::text
		FROM courses WHERE id = $1::uuid`, courseID).Scan(&model, &institution, &subject); err != nil {
		t.Fatalf("reading course classification: %v", err)
	}
	if model != string(ClassificationLegacyTaxonomy) {
		t.Fatalf("got classification %s, want LEGACY_TAXONOMY", model)
	}
	if institution != nil || subject != nil {
		t.Fatalf("legacy course carries academic identity: institution=%v subject=%v", institution, subject)
	}

	// The legacy revision-scoped taxonomy columns still exist and are still
	// writable for a legacy Course. T5, not T4, removes them.
	if _, err := p.Exec(ctx, `
		INSERT INTO taxonomy_terms (id, kind, label_ar, label_en)
		VALUES ('dddd4444-0000-0000-0000-000000000001', 'MAJOR', 'تخصص', 'Major')`); err != nil {
		t.Fatalf("seeding legacy term: %v", err)
	}
	if _, err := p.Exec(ctx, `
		UPDATE course_revisions SET major_term_id = 'dddd4444-0000-0000-0000-000000000001'
		WHERE course_id = $1::uuid`, courseID); err != nil {
		t.Fatalf("legacy taxonomy write must still work: %v", err)
	}
}

// T4A-3: the default create path is unchanged, which is what keeps T4-A
// independently deployable ahead of T4-B.
func TestT4ADefaultCreatePathStaysLegacy(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)
	seedAcademicFixture(t, p, ctx)
	repo := t4aRepo(t, p)

	course, err := repo.CreateCourse(ctx, CreateCourseRequest{
		OwnerAccountID: t4aInstructor, TitleAr: "قديم", TitleEn: "Legacy",
	}, "T4A Instructor")
	if err != nil {
		t.Fatalf("CreateCourse: %v", err)
	}
	if course.ClassificationModel != ClassificationLegacyTaxonomy {
		t.Fatalf("got %s, want LEGACY_TAXONOMY", course.ClassificationModel)
	}
	if course.InstitutionID != nil || course.SubjectID != nil {
		t.Fatalf("legacy course must not carry academic identity")
	}
}

// T4A-4/T4A-5: an Academic Course requires an Institution and may draft without
// a Subject.
func TestT4AAcademicCourseRequiresInstitutionAndAllowsNullSubject(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)
	seedAcademicFixture(t, p, ctx)
	repo := t4aRepo(t, p)

	if _, err := repo.CreateCourse(ctx, CreateCourseRequest{
		OwnerAccountID: t4aInstructor, TitleAr: "أ", TitleEn: "A",
		Academic: &AcademicCourseContext{InstitutionID: ""},
	}, "T4A Instructor"); !errors.Is(err, ErrInstitutionRequired) {
		t.Fatalf("got %v, want ErrInstitutionRequired", err)
	}

	course := createAcademicCourse(t, repo, ctx, nil)
	if course.ClassificationModel != ClassificationAcademicCatalog {
		t.Fatalf("got %s, want ACADEMIC_CATALOG", course.ClassificationModel)
	}
	if course.InstitutionID == nil || *course.InstitutionID != t4aInstitutionKU {
		t.Fatalf("academic course lost its Institution")
	}
	if course.SubjectID != nil {
		t.Fatalf("subject-less draft must have a NULL Subject")
	}
	if course.EditableRevision == nil || course.EditableRevision.RevisionNumber != 1 {
		t.Fatalf("initial revision was not created atomically with the Course")
	}

	// And it is distinguishable from a legacy Course precisely because of the
	// discriminator, not because of nullability: both have a NULL Subject.
	if course.ClassificationModel == ClassificationLegacyTaxonomy {
		t.Fatalf("subject-less academic draft must not read as legacy")
	}
}

// T4A-6/T4A-7: Subject must belong to the Course's Institution.
func TestT4ACrossInstitutionSubjectRefused(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)
	seedAcademicFixture(t, p, ctx)
	repo := t4aRepo(t, p)

	foreign := t4aSubjectAUK
	if _, err := repo.CreateCourse(ctx, CreateCourseRequest{
		OwnerAccountID: t4aInstructor, TitleAr: "أ", TitleEn: "A",
		Academic: &AcademicCourseContext{InstitutionID: t4aInstitutionKU, SubjectID: &foreign},
	}, "T4A Instructor"); !errors.Is(err, ErrSubjectUnavailable) {
		t.Fatalf("create with cross-institution subject: got %v, want ErrSubjectUnavailable", err)
	}

	course := createAcademicCourse(t, repo, ctx, nil)
	if _, err := repo.SetCourseSubject(ctx, SetCourseSubjectRequest{
		CourseID: course.ID, OwnerAccountID: t4aInstructor, SubjectID: t4aSubjectAUK,
	}); !errors.Is(err, ErrSubjectUnavailable) {
		t.Fatalf("assign cross-institution subject: got %v, want ErrSubjectUnavailable", err)
	}

	// The database refuses it independently of the domain check.
	_, err := p.Exec(ctx, `UPDATE courses SET subject_id = $1::uuid WHERE id = $2::uuid`,
		t4aSubjectAUK, course.ID)
	if err == nil {
		t.Fatalf("direct cross-institution subject write must be refused by the composite FK")
	}
}

// T4A-8/T4A-9/T4A-10: assignment eligibility and pre-publication mutability.
func TestT4ASubjectAssignmentLifecycleBeforePublication(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)
	seedAcademicFixture(t, p, ctx)
	repo := t4aRepo(t, p)
	course := createAcademicCourse(t, repo, ctx, nil)

	// Active Subject assigns.
	updated, err := repo.SetCourseSubject(ctx, SetCourseSubjectRequest{
		CourseID: course.ID, OwnerAccountID: t4aInstructor, SubjectID: t4aSubjectA,
		ActorDescriptor: "T4A Instructor",
	})
	if err != nil {
		t.Fatalf("assigning active subject: %v", err)
	}
	if updated.SubjectID == nil || *updated.SubjectID != t4aSubjectA {
		t.Fatalf("subject was not assigned")
	}

	// Changing it before publication is allowed.
	if _, err := repo.SetCourseSubject(ctx, SetCourseSubjectRequest{
		CourseID: course.ID, OwnerAccountID: t4aInstructor, SubjectID: t4aSubjectB,
		ActorDescriptor: "T4A Instructor",
	}); err != nil {
		t.Fatalf("changing subject before publication: %v", err)
	}

	// A retired Subject cannot be newly assigned.
	if _, err := p.Exec(ctx, `UPDATE subjects SET retired_at = now() WHERE id = $1::uuid`, t4aSubjectA); err != nil {
		t.Fatalf("retiring subject: %v", err)
	}
	if _, err := repo.SetCourseSubject(ctx, SetCourseSubjectRequest{
		CourseID: course.ID, OwnerAccountID: t4aInstructor, SubjectID: t4aSubjectA,
		ActorDescriptor: "T4A Instructor",
	}); !errors.Is(err, ErrSubjectUnavailable) {
		t.Fatalf("retired subject assignment: got %v, want ErrSubjectUnavailable", err)
	}

	// A non-owner cannot assign at all.
	if _, err := repo.SetCourseSubject(ctx, SetCourseSubjectRequest{
		CourseID: course.ID, OwnerAccountID: t4aAdmin, SubjectID: t4aSubjectB,
		ActorDescriptor: "T4A Admin",
	}); !errors.Is(err, ErrCourseNotFound) {
		t.Fatalf("non-owner assignment: got %v, want ErrCourseNotFound", err)
	}
}

// T4A-11/T4A-12: the review lock, and the correction window it reopens.
func TestT4ASubjectLockedDuringPendingReviewAndReopensOnChangesRequested(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)
	seedAcademicFixture(t, p, ctx)
	repo := t4aRepo(t, p)
	course := createAcademicCourse(t, repo, ctx, &[]string{t4aSubjectA}[0])

	revID := course.EditableRevision.ID
	if _, err := p.Exec(ctx, `
		UPDATE course_revisions SET state = 'PENDING_REVIEW', submitted_at = now() WHERE id = $1::uuid`,
		revID); err != nil {
		t.Fatalf("moving revision to PENDING_REVIEW: %v", err)
	}

	if _, err := repo.SetCourseSubject(ctx, SetCourseSubjectRequest{
		CourseID: course.ID, OwnerAccountID: t4aInstructor, SubjectID: t4aSubjectB,
	}); !errors.Is(err, ErrSubjectLockedForReview) {
		t.Fatalf("subject during review: got %v, want ErrSubjectLockedForReview", err)
	}

	// Admin requests changes; the Course has never published, so the Subject
	// becomes editable again.
	if _, err := p.Exec(ctx, `
		UPDATE course_revisions SET state = 'CHANGES_REQUESTED', review_reason = 'wrong subject'
		WHERE id = $1::uuid`, revID); err != nil {
		t.Fatalf("requesting changes: %v", err)
	}
	if _, err := repo.SetCourseSubject(ctx, SetCourseSubjectRequest{
		CourseID: course.ID, OwnerAccountID: t4aInstructor, SubjectID: t4aSubjectB,
		ActorDescriptor: "T4A Instructor",
	}); err != nil {
		t.Fatalf("subject change after Request Changes must be allowed: %v", err)
	}
}

// T4A-13/T4A-14/T4A-15: first publication locks the Subject, in the domain and
// in the database.
func TestT4APublicationLocksSubjectInDomainAndDatabase(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)
	seedAcademicFixture(t, p, ctx)
	repo := t4aRepo(t, p)
	course := createAcademicCourse(t, repo, ctx, &[]string{t4aSubjectA}[0])
	revID := course.EditableRevision.ID

	// Publish through the same shape the approval commit uses.
	if _, err := p.Exec(ctx, `UPDATE course_revisions SET state = 'APPROVED' WHERE id = $1::uuid`, revID); err != nil {
		t.Fatalf("approving revision: %v", err)
	}
	if _, err := p.Exec(ctx, `
		UPDATE courses SET live_revision_id = $1::uuid, lifecycle = 'PUBLISHED' WHERE id = $2::uuid`,
		revID, course.ID); err != nil {
		t.Fatalf("publishing course: %v", err)
	}

	// Domain refusal.
	if _, err := repo.SetCourseSubject(ctx, SetCourseSubjectRequest{
		CourseID: course.ID, OwnerAccountID: t4aInstructor, SubjectID: t4aSubjectB,
	}); !errors.Is(err, ErrSubjectImmutable) {
		t.Fatalf("post-publication domain mutation: got %v, want ErrSubjectImmutable", err)
	}

	// Database refusal, bypassing the domain entirely. This is the case that
	// makes the lock a property of the data rather than of the handler.
	if _, err := p.Exec(ctx, `UPDATE courses SET subject_id = $1::uuid WHERE id = $2::uuid`,
		t4aSubjectB, course.ID); err == nil {
		t.Fatalf("direct SQL subject change on a published course must be refused by the trigger")
	}
	if _, err := p.Exec(ctx, `UPDATE courses SET subject_id = NULL WHERE id = $1::uuid`, course.ID); err == nil {
		t.Fatalf("clearing the subject of a published course must be refused by the trigger")
	}

	var current string
	if err := p.QueryRow(ctx, `SELECT subject_id::text FROM courses WHERE id = $1::uuid`, course.ID).Scan(&current); err != nil {
		t.Fatalf("re-reading subject: %v", err)
	}
	if current != t4aSubjectA {
		t.Fatalf("subject drifted to %s; it must remain %s", current, t4aSubjectA)
	}
}

// T4A-16: retiring the historical Subject must not disable the published Course.
func TestT4ARetiredHistoricalSubjectKeepsPublishedCourseOperational(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)
	seedAcademicFixture(t, p, ctx)
	repo := t4aRepo(t, p)
	course := createAcademicCourse(t, repo, ctx, &[]string{t4aSubjectA}[0])
	revID := course.EditableRevision.ID

	if _, err := p.Exec(ctx, `UPDATE course_revisions SET state = 'APPROVED' WHERE id = $1::uuid`, revID); err != nil {
		t.Fatalf("approving: %v", err)
	}
	if _, err := p.Exec(ctx, `
		UPDATE courses SET live_revision_id = $1::uuid, lifecycle = 'PUBLISHED' WHERE id = $2::uuid`,
		revID, course.ID); err != nil {
		t.Fatalf("publishing: %v", err)
	}
	if _, err := p.Exec(ctx, `UPDATE subjects SET retired_at = now() WHERE id = $1::uuid`, t4aSubjectA); err != nil {
		t.Fatalf("retiring subject: %v", err)
	}

	// Readable, with its Subject identity intact.
	graph, err := repo.GetLiveCourseGraph(ctx, course.ID)
	if err != nil {
		t.Fatalf("published course must stay readable after Subject retirement: %v", err)
	}
	if graph.SubjectID == nil || *graph.SubjectID != t4aSubjectA {
		t.Fatalf("published course lost its historical Subject identity")
	}

	// And a LATER content revision still validates: retirement bars a first
	// publication, never an existing Course's continued operation.
	err = repo.ExecTx(ctx, func(tx pgx.Tx) error {
		row, err := repo.LockCourse(ctx, tx, course.ID)
		if err != nil {
			return err
		}
		violations := validateAcademicIdentityForSubmission(ctx, submissionValidationRequest{
			tx: tx, courseID: course.ID, course: row,
		})
		if len(violations) != 0 {
			t.Errorf("later content revision blocked by retired historical Subject: %+v", violations)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("revalidating published course: %v", err)
	}
}

// --- Coexistence ---------------------------------------------------------

// T4A-18: an Academic Course cannot use the legacy classification vocabulary,
// on either write path, and not merely in the UI.
func TestT4AAcademicCourseCannotUseLegacyTaxonomyMutation(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)
	seedAcademicFixture(t, p, ctx)
	repo := t4aRepo(t, p)
	course := createAcademicCourse(t, repo, ctx, &[]string{t4aSubjectA}[0])

	if _, err := p.Exec(ctx, `
		INSERT INTO taxonomy_terms (id, kind, label_ar, label_en) VALUES
		  ('dddd4444-0000-0000-0000-000000000001', 'MAJOR', 'تخصص', 'Major'),
		  ('dddd4444-0000-0000-0000-000000000002', 'SUBJECT', 'مادة', 'Subject')`); err != nil {
		t.Fatalf("seeding legacy terms: %v", err)
	}
	major := "dddd4444-0000-0000-0000-000000000001"
	subject := "dddd4444-0000-0000-0000-000000000002"
	year := StudyYear("YEAR_1")

	// The shared Instructor revision route.
	_, err := repo.UpdateCourseRevision(ctx, &noopAssetValidator{}, UpdateRevisionRequest{
		CourseID: course.ID, RevisionID: course.EditableRevision.ID, OwnerAccountID: t4aInstructor,
		MajorTermID: &major, SubjectTermID: &subject, StudyYear: &year,
	}, "T4A Instructor")
	if !errors.Is(err, ErrLegacyTaxonomyOnAcademicCourse) {
		t.Fatalf("instructor legacy taxonomy on academic course: got %v, want refusal", err)
	}

	// The Admin per-Course override route.
	_, err = repo.AssignTaxonomyToRevision(ctx, AssignTaxonomyRequest{
		CourseID: course.ID, RevisionID: course.EditableRevision.ID,
		AdminAccountID: t4aAdmin, MajorTermID: major, SubjectTermID: subject,
	})
	if !errors.Is(err, ErrLegacyTaxonomyOnAcademicCourse) {
		t.Fatalf("admin taxonomy override on academic course: got %v, want refusal", err)
	}

	// Title-only edits on the same shared route still work: the refusal is
	// scoped to the legacy fields, not to the whole call.
	if _, err := repo.UpdateCourseRevision(ctx, &noopAssetValidator{}, UpdateRevisionRequest{
		CourseID: course.ID, RevisionID: course.EditableRevision.ID, OwnerAccountID: t4aInstructor,
		TitleEn: "Renamed",
	}, "T4A Instructor"); err != nil {
		t.Fatalf("ordinary revision edit on an academic course must still work: %v", err)
	}
}

// --- Dual validation ------------------------------------------------------

// T4A-17/T4A-19: each model is validated by its own rules and never by both.
func TestT4ADualValidationSeparatesTheTwoModels(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)
	seedAcademicFixture(t, p, ctx)
	repo := t4aRepo(t, p)

	codes := func(vs []SubmissionViolation) map[string]bool {
		out := map[string]bool{}
		for _, v := range vs {
			out[v.Code] = true
		}
		return out
	}

	// Academic Course with a Subject: no legacy taxonomy violation is raised,
	// and no academic violation either.
	withSubject := createAcademicCourse(t, repo, ctx, &[]string{t4aSubjectA}[0])
	// Academic Course without a Subject: submission is refused, and refused for
	// the ACADEMIC reason, never for a missing legacy dimension.
	withoutSubject := createAcademicCourse(t, repo, ctx, nil)
	// Legacy Course: FR-010 applies unchanged.
	legacy, err := repo.CreateCourse(ctx, CreateCourseRequest{
		OwnerAccountID: t4aInstructor, TitleAr: "ق", TitleEn: "Legacy",
	}, "T4A Instructor")
	if err != nil {
		t.Fatalf("creating legacy course: %v", err)
	}

	check := func(courseID string) map[string]bool {
		t.Helper()
		var found map[string]bool
		if err := repo.ExecTx(ctx, func(tx pgx.Tx) error {
			row, err := repo.LockCourse(ctx, tx, courseID)
			if err != nil {
				return err
			}
			if academicSubmissionModel(row) {
				found = codes(validateAcademicIdentityForSubmission(ctx, submissionValidationRequest{
					tx: tx, courseID: courseID, course: row,
				}))
				return nil
			}
			var fatal error
			found = codes(validateLegacyTaxonomyForSubmission(ctx, submissionValidationRequest{
				tx: tx, courseID: courseID, course: row,
				revision: &CourseRevision{},
			}, &fatal))
			return fatal
		}); err != nil {
			t.Fatalf("validating %s: %v", courseID, err)
		}
		return found
	}

	if got := check(withSubject.ID); len(got) != 0 {
		t.Fatalf("academic course with a Subject raised %v; want none", got)
	}
	if got := check(withoutSubject.ID); !got["ACADEMIC_SUBJECT_MISSING"] {
		t.Fatalf("academic course without a Subject raised %v; want ACADEMIC_SUBJECT_MISSING", got)
	} else if got["TAXONOMY_DIMENSION_MISSING"] {
		t.Fatalf("academic course was held to the legacy taxonomy gate: %v", got)
	}
	if got := check(legacy.ID); !got["TAXONOMY_DIMENSION_MISSING"] {
		t.Fatalf("legacy course lost its FR-010 gate: %v", got)
	}
}

type noopAssetValidator struct{}

func (n *noopAssetValidator) ValidateAssetVersion(context.Context, string) error { return nil }
