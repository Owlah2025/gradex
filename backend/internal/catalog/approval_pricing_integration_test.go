//go:build integration

package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// approvalPricingFixture is a Course whose submitted revision is complete in
// every respect except the Admin-owned Course launch price. It exists to prove
// that the price is an independent publication precondition rather than a
// by-product of an otherwise incomplete graph.
type approvalPricingFixture struct {
	p          *pgxpool.Pool
	repo       *Repository
	validator  AssetVersionValidator
	ctx        context.Context
	ownerID    string
	adminID    string
	courseID   string
	revisionID string
	sectionID  string
}

func newApprovalPricingFixture(t *testing.T) *approvalPricingFixture {
	t.Helper()
	freshSchema(t)
	p, _ := pool(t)
	ctx := context.Background()

	ownerID, courseID := seedInstructorAndCourse(t, p, ctx)
	repo, err := NewRepository(p, testOutboxWriter(t))
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}

	f := &approvalPricingFixture{
		p: p, repo: repo, validator: NewDBAssetVersionValidator(p), ctx: ctx,
		ownerID:  ownerID,
		adminID:  "99999999-9999-9999-9999-999999999999",
		courseID: courseID,
	}

	majorID := "10000000-0000-0000-0000-0000000000a1"
	subjectID := "10000000-0000-0000-0000-0000000000a2"
	videoID := "20000000-0000-0000-0000-0000000000a1"

	if _, err := p.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
		VALUES ($1, 'admin@example.com', 'admin@example.com', 'ADMIN', 'ACTIVE', 'Admin')
	`, f.adminID); err != nil {
		t.Fatalf("seeding admin: %v", err)
	}
	if _, err := p.Exec(ctx, `
		INSERT INTO taxonomy_terms (id, kind, label_ar, label_en, academic_code) VALUES
		($1, 'MAJOR', 'تخصص', 'Major', NULL),
		($2, 'SUBJECT', 'مادة', 'Subject', 'SUBJ-1')
	`, majorID, subjectID); err != nil {
		t.Fatalf("seeding taxonomy: %v", err)
	}

	legacyCourseID := "60000000-0000-0000-0000-0000000000a1"
	legacySectionID := "70000000-0000-0000-0000-0000000000a1"
	legacyLessonID := "80000000-0000-0000-0000-0000000000a1"
	if _, err := p.Exec(ctx,
		`INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1, $2, 'DRAFT')`,
		legacyCourseID, ownerID,
	); err != nil {
		t.Fatalf("seeding asset course: %v", err)
	}
	if _, err := p.Exec(ctx,
		`INSERT INTO sections (id, course_id, title, "order") VALUES ($1, $2, 'Asset owners', 0)`,
		legacySectionID, legacyCourseID,
	); err != nil {
		t.Fatalf("seeding asset section: %v", err)
	}
	if _, err := p.Exec(ctx,
		`INSERT INTO lessons (id, section_id, title, "order") VALUES ($1, $2, 'Asset lesson', 0)`,
		legacyLessonID, legacySectionID,
	); err != nil {
		t.Fatalf("seeding asset lesson: %v", err)
	}
	if _, err := p.Exec(ctx,
		`INSERT INTO videos (id, lesson_id, status) VALUES ($1, $2, 'READY')`,
		videoID, legacyLessonID,
	); err != nil {
		t.Fatalf("seeding asset version: %v", err)
	}

	if err := p.QueryRow(ctx,
		`SELECT id FROM course_revisions WHERE course_id = $1::uuid`, courseID,
	).Scan(&f.revisionID); err != nil {
		t.Fatalf("querying draft revision: %v", err)
	}

	year := StudyYearYear1
	if _, err := repo.UpdateCourseRevision(ctx, f.validator, UpdateRevisionRequest{
		CourseID: courseID, RevisionID: f.revisionID, OwnerAccountID: ownerID,
		TitleAr: "دورة", TitleEn: "Course",
		DescriptionAr: "وصف", DescriptionEn: "Description",
		MajorTermID: &majorID, SubjectTermID: &subjectID, StudyYear: &year,
	}, ownerID); err != nil {
		t.Fatalf("UpdateCourseRevision: %v", err)
	}

	section, err := repo.AddSection(ctx, AddSectionRequest{
		CourseID: courseID, RevisionID: f.revisionID, OwnerAccountID: ownerID,
		TitleAr: "قسم", TitleEn: "Section",
	}, ownerID)
	if err != nil {
		t.Fatalf("AddSection: %v", err)
	}
	f.sectionID = section.SectionIdentityID

	lesson, err := repo.AddLesson(ctx, AddLessonRequest{
		CourseID: courseID, RevisionID: f.revisionID, SectionID: section.SectionIdentityID,
		OwnerAccountID: ownerID, TitleAr: "درس", TitleEn: "Lesson",
	}, ownerID)
	if err != nil {
		t.Fatalf("AddLesson: %v", err)
	}
	if _, err := repo.SetLessonVideo(ctx, f.validator, SetVideoRequest{
		CourseID: courseID, RevisionID: f.revisionID, LessonID: lesson.LessonIdentityID,
		VideoAssetVersionID: videoID, OwnerAccountID: ownerID,
	}, ownerID); err != nil {
		t.Fatalf("SetLessonVideo: %v", err)
	}
	if _, err := repo.SubmitCourse(ctx, f.validator, SubmitCourseRequest{
		CourseID: courseID, RevisionID: f.revisionID,
		OwnerAccountID: ownerID, ActorDescriptor: ownerID,
	}); err != nil {
		t.Fatalf("SubmitCourse: %v", err)
	}
	return f
}

func (f *approvalPricingFixture) approve(t *testing.T) error {
	t.Helper()
	_, err := f.repo.ApproveCourse(f.ctx, f.validator, ApproveCourseRequest{
		CourseID: f.courseID, RevisionID: f.revisionID,
		AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
	})
	return err
}

// approvalLifecycleSnapshot is deliberately a comparable value: nullable
// columns collapse to "" so a whole-struct equality check proves the rejected
// approval changed nothing at all.
type approvalLifecycleSnapshot struct {
	lifecycle      string
	liveRevisionID string
	revisionState  string
	reviewedAt     string
	reviewedBy     string
	reviewReason   string
	auditEvents    int
	priceRows      int
}

func (f *approvalPricingFixture) snapshot(t *testing.T) approvalLifecycleSnapshot {
	t.Helper()
	var s approvalLifecycleSnapshot
	if err := f.p.QueryRow(f.ctx, `
		SELECT c.lifecycle::text, COALESCE(c.live_revision_id::text, ''),
		       r.state::text, COALESCE(r.reviewed_at::text, ''),
		       COALESCE(r.reviewed_by_account_id::text, ''), COALESCE(r.review_reason, '')
		FROM courses c
		JOIN course_revisions r ON r.id = $2::uuid
		WHERE c.id = $1::uuid
	`, f.courseID, f.revisionID).Scan(
		&s.lifecycle, &s.liveRevisionID, &s.revisionState,
		&s.reviewedAt, &s.reviewedBy, &s.reviewReason,
	); err != nil {
		t.Fatalf("reading lifecycle snapshot: %v", err)
	}
	if err := f.p.QueryRow(f.ctx,
		`SELECT count(*) FROM audit_events WHERE target_id = $1`, f.courseID,
	).Scan(&s.auditEvents); err != nil {
		t.Fatalf("counting audit events: %v", err)
	}
	if err := f.p.QueryRow(f.ctx,
		`SELECT count(*) FROM course_price_changes WHERE course_id = $1::uuid`, f.courseID,
	).Scan(&s.priceRows); err != nil {
		t.Fatalf("counting price changes: %v", err)
	}
	return s
}

func priceRequiredViolation(t *testing.T, err error, courseID string) {
	t.Helper()
	var valErr *SubmissionValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("approval error = %v, want *SubmissionValidationError", err)
	}
	for _, v := range valErr.Violations {
		if v.Code == "COURSE_PRICE_REQUIRED" && v.Target == "course:"+courseID {
			return
		}
	}
	t.Fatalf("violations %+v do not contain COURSE_PRICE_REQUIRED for course:%s", valErr.Violations, courseID)
}

// TestApprovalWithoutCourseLaunchPriceIsRefusedAndAtomic is the server-side
// publication invariant: a complete, submitted revision still cannot become
// live without an Admin Course price, and the refusal leaves every lifecycle,
// review and pricing row exactly as it found them.
func TestApprovalWithoutCourseLaunchPriceIsRefusedAndAtomic(t *testing.T) {
	f := newApprovalPricingFixture(t)
	before := f.snapshot(t)

	err := f.approve(t)
	if err == nil {
		t.Fatal("ApproveCourse succeeded without a Course launch price")
	}
	priceRequiredViolation(t, err, f.courseID)

	after := f.snapshot(t)
	if after != before {
		t.Fatalf("rejected approval mutated state:\nbefore %+v\nafter  %+v", before, after)
	}
	if after.revisionState != string(RevisionPendingReview) {
		t.Fatalf("revision state = %q, want %q", after.revisionState, RevisionPendingReview)
	}
	if after.lifecycle == "PUBLISHED" || after.liveRevisionID != "" {
		t.Fatalf("course was published without a price: %+v", after)
	}
	if after.reviewedAt != "" || after.reviewedBy != "" {
		t.Fatalf("review state advanced on a rejected approval: %+v", after)
	}
}

// TestSectionPriceDoesNotSatisfyCourseLaunchPrice keeps Section pricing from
// standing in for the Course price. Section is not an acquirable scope, so a
// Section price is not a launch price.
func TestSectionPriceDoesNotSatisfyCourseLaunchPrice(t *testing.T) {
	f := newApprovalPricingFixture(t)

	if _, err := f.repo.SetSectionPrice(f.ctx, SetSectionPriceRequest{
		CourseID: f.courseID, SectionIdentityID: f.sectionID,
		AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
		PriceMinorUnits: 10000, Reason: "Section price",
	}); err != nil {
		t.Fatalf("SetSectionPrice: %v", err)
	}

	err := f.approve(t)
	if err == nil {
		t.Fatal("ApproveCourse succeeded with only a Section price")
	}
	priceRequiredViolation(t, err, f.courseID)

	after := f.snapshot(t)
	if after.revisionState != string(RevisionPendingReview) || after.liveRevisionID != "" {
		t.Fatalf("Section price advanced publication: %+v", after)
	}
}

// TestApprovalSucceedsOnceCourseLaunchPriceIsSet completes the Founder path:
// the same rejected revision publishes normally after the Admin sets a price,
// and the earlier refusal left nothing behind that blocks it.
func TestApprovalSucceedsOnceCourseLaunchPriceIsSet(t *testing.T) {
	f := newApprovalPricingFixture(t)

	if err := f.approve(t); err == nil {
		t.Fatal("ApproveCourse succeeded without a Course launch price")
	}

	if _, err := f.repo.SetCoursePrice(f.ctx, SetCoursePriceRequest{
		CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
		PriceMinorUnits: 25000, Reason: "Launch price",
	}); err != nil {
		t.Fatalf("SetCoursePrice: %v", err)
	}

	if err := f.approve(t); err != nil {
		t.Fatalf("ApproveCourse after pricing: %v", err)
	}

	after := f.snapshot(t)
	if after.lifecycle != "PUBLISHED" {
		t.Fatalf("course lifecycle = %q, want PUBLISHED", after.lifecycle)
	}
	if after.liveRevisionID != f.revisionID {
		t.Fatalf("live revision = %q, want %s", after.liveRevisionID, f.revisionID)
	}
	if after.revisionState != string(RevisionApproved) {
		t.Fatalf("revision state = %q, want %q", after.revisionState, RevisionApproved)
	}
	if after.reviewedAt == "" || after.reviewedBy == "" {
		t.Fatalf("approved revision has no review stamp: %+v", after)
	}
	if after.priceRows != 1 {
		t.Fatalf("price rows = %d, want 1", after.priceRows)
	}
}

// TestZeroCourseLaunchPriceIsAPrice pins the authoritative pricing rule the
// invariant is written against: a price is a non-negative integer amount in
// fils (ErrInvalidPrice, BR-019). No repository authority sets a positive
// minimum, so a recorded zero price satisfies the publication precondition
// while a missing price does not.
func TestZeroCourseLaunchPriceIsAPrice(t *testing.T) {
	f := newApprovalPricingFixture(t)

	if _, err := f.repo.SetCoursePrice(f.ctx, SetCoursePriceRequest{
		CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
		PriceMinorUnits: 0, Reason: "Free launch price",
	}); err != nil {
		t.Fatalf("SetCoursePrice(0): %v", err)
	}
	if err := f.approve(t); err != nil {
		t.Fatalf("ApproveCourse with a zero price: %v", err)
	}
	if after := f.snapshot(t); after.lifecycle != "PUBLISHED" {
		t.Fatalf("course lifecycle = %q, want PUBLISHED", after.lifecycle)
	}
}
