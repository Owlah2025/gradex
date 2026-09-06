//go:build integration

package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
)

// D-097: Admin review gates a Course's FIRST publication only. Every later
// revision of that Course is published by its own Instructor, through the same
// atomic promotion, with none of the safety gates relaxed.
//
// The fixture below carries one Course all the way through its first
// publication so that the subsequent-publication rules can be exercised
// against genuinely published state rather than against a simulated pointer.

type publicationFixture struct {
	p         *pgxpool.Pool
	repo      *Repository
	public    *catalogpublic.Repository
	validator AssetVersionValidator
	ctx       context.Context

	ownerID    string
	strangerID string
	adminID    string
	courseID   string
	// firstRevisionID is the revision the Admin approved into the catalogue.
	firstRevisionID string
	sectionID       string
	lessonID        string
	majorID         string
	subjectID       string
	// spareVideoID is a second READY asset, for proving a new Lesson video
	// publishes without an Admin decision.
	spareVideoID string
	// processingVideoID is deliberately not READY.
	processingVideoID string
	legacySectionID   string
}

func newPublicationFixture(t *testing.T) *publicationFixture {
	t.Helper()
	freshSchema(t)
	p, _ := pool(t)
	ctx := context.Background()

	ownerID, courseID := seedInstructorAndCourse(t, p, ctx)
	repo, err := NewRepository(p, testOutboxWriter(t))
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	publicRepo, err := catalogpublic.NewRepository(p, catalogpublic.PublishedOnly)
	if err != nil {
		t.Fatalf("catalogpublic.NewRepository: %v", err)
	}

	f := &publicationFixture{
		p: p, repo: repo, public: publicRepo,
		validator: NewDBAssetVersionValidator(p), ctx: ctx,
		ownerID:    ownerID,
		strangerID: "99999999-9999-9999-9999-99999999aaaa",
		adminID:    "99999999-9999-9999-9999-999999999999",
		courseID:   courseID,
		majorID:    "10000000-0000-0000-0000-0000000000b1",
		subjectID:  "10000000-0000-0000-0000-0000000000b2",
	}

	if _, err := p.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name) VALUES
		($1, 'admin@example.com', 'admin@example.com', 'ADMIN', 'ACTIVE', 'Admin'),
		($2, 'other@example.com', 'other@example.com', 'INSTRUCTOR', 'ACTIVE', 'Other Instructor')
	`, f.adminID, f.strangerID); err != nil {
		t.Fatalf("seeding accounts: %v", err)
	}
	if _, err := p.Exec(ctx, `
		INSERT INTO taxonomy_terms (id, kind, label_ar, label_en, academic_code) VALUES
		($1, 'MAJOR', 'تخصص', 'Major', NULL),
		($2, 'SUBJECT', 'مادة', 'Subject', 'SUBJ-1')
	`, f.majorID, f.subjectID); err != nil {
		t.Fatalf("seeding taxonomy: %v", err)
	}

	// Legacy asset-owning graph. The catalog Asset Version validator reads the
	// `videos` table, so READY and non-READY rows are seeded there.
	legacyCourseID := "60000000-0000-0000-0000-0000000000b1"
	legacyLessonID := "80000000-0000-0000-0000-0000000000b1"
	f.legacySectionID = "70000000-0000-0000-0000-0000000000b1"
	firstVideoID := "20000000-0000-0000-0000-0000000000b1"
	f.spareVideoID = "20000000-0000-0000-0000-0000000000b2"
	f.processingVideoID = "20000000-0000-0000-0000-0000000000b3"
	if _, err := p.Exec(ctx, `INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1,$2,'DRAFT')`, legacyCourseID, ownerID); err != nil {
		t.Fatalf("seeding asset course: %v", err)
	}
	if _, err := p.Exec(ctx, `INSERT INTO sections (id, course_id, title, "order") VALUES ($1,$2,'Assets',0)`, f.legacySectionID, legacyCourseID); err != nil {
		t.Fatalf("seeding asset section: %v", err)
	}
	// One video per legacy lesson, so each Asset Version needs its own carrier.
	legacyLessonB := "80000000-0000-0000-0000-0000000000b2"
	legacyLessonC := "80000000-0000-0000-0000-0000000000b3"
	if _, err := p.Exec(ctx, `
		INSERT INTO lessons (id, section_id, title, "order") VALUES ($1,$4,'Assets A',0),($2,$4,'Assets B',1),($3,$4,'Assets C',2)
	`, legacyLessonID, legacyLessonB, legacyLessonC, f.legacySectionID); err != nil {
		t.Fatalf("seeding asset lessons: %v", err)
	}
	if _, err := p.Exec(ctx, `
		INSERT INTO videos (id, lesson_id, status) VALUES ($1,$4,'READY'),($2,$5,'READY'),($3,$6,'PROCESSING')
	`, firstVideoID, f.spareVideoID, f.processingVideoID, legacyLessonID, legacyLessonB, legacyLessonC); err != nil {
		t.Fatalf("seeding asset versions: %v", err)
	}

	if err := p.QueryRow(ctx, `SELECT id FROM course_revisions WHERE course_id = $1::uuid`, courseID).Scan(&f.firstRevisionID); err != nil {
		t.Fatalf("querying draft revision: %v", err)
	}

	year := StudyYearYear1
	if _, err := repo.UpdateCourseRevision(ctx, f.validator, UpdateRevisionRequest{
		CourseID: courseID, RevisionID: f.firstRevisionID, OwnerAccountID: ownerID,
		TitleAr: "دورة", TitleEn: "Course",
		DescriptionAr: "وصف", DescriptionEn: "Description",
		MajorTermID: &f.majorID, SubjectTermID: &f.subjectID, StudyYear: &year,
	}, ownerID); err != nil {
		t.Fatalf("UpdateCourseRevision: %v", err)
	}
	section, err := repo.AddSection(ctx, AddSectionRequest{
		CourseID: courseID, RevisionID: f.firstRevisionID, OwnerAccountID: ownerID,
		TitleAr: "قسم", TitleEn: "Section",
	}, ownerID)
	if err != nil {
		t.Fatalf("AddSection: %v", err)
	}
	f.sectionID = section.SectionIdentityID
	lesson, err := repo.AddLesson(ctx, AddLessonRequest{
		CourseID: courseID, RevisionID: f.firstRevisionID, SectionID: f.sectionID,
		OwnerAccountID: ownerID, TitleAr: "درس", TitleEn: "Lesson",
	}, ownerID)
	if err != nil {
		t.Fatalf("AddLesson: %v", err)
	}
	f.lessonID = lesson.LessonIdentityID
	if _, err := repo.SetLessonVideo(ctx, f.validator, SetVideoRequest{
		CourseID: courseID, RevisionID: f.firstRevisionID, LessonID: f.lessonID,
		VideoAssetVersionID: firstVideoID, OwnerAccountID: ownerID,
	}, ownerID); err != nil {
		t.Fatalf("SetLessonVideo: %v", err)
	}
	if _, err := repo.SetCoursePrice(ctx, SetCoursePriceRequest{
		CourseID: courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
		PriceMinorUnits: 25_000, Reason: "Launch price",
	}); err != nil {
		t.Fatalf("SetCoursePrice: %v", err)
	}
	return f
}

func (f *publicationFixture) submit(t *testing.T, revisionID string) error {
	t.Helper()
	_, err := f.repo.SubmitCourse(f.ctx, f.validator, SubmitCourseRequest{
		CourseID: f.courseID, RevisionID: revisionID,
		OwnerAccountID: f.ownerID, ActorDescriptor: f.ownerID,
	})
	return err
}

func (f *publicationFixture) approve(t *testing.T, revisionID string) error {
	t.Helper()
	_, err := f.repo.ApproveCourse(f.ctx, f.validator, ApproveCourseRequest{
		CourseID: f.courseID, RevisionID: revisionID,
		AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
	})
	return err
}

func (f *publicationFixture) publish(t *testing.T, revisionID, actorID string) error {
	t.Helper()
	_, err := f.repo.PublishRevision(f.ctx, f.validator, PublishRevisionRequest{
		CourseID: f.courseID, RevisionID: revisionID,
		OwnerAccountID: actorID, ActorDescriptor: actorID,
	})
	return err
}

// firstPublish carries the fixture Course through the Admin gate exactly once.
func (f *publicationFixture) firstPublish(t *testing.T) {
	t.Helper()
	if err := f.submit(t, f.firstRevisionID); err != nil {
		t.Fatalf("SubmitCourse: %v", err)
	}
	if err := f.approve(t, f.firstRevisionID); err != nil {
		t.Fatalf("ApproveCourse: %v", err)
	}
}

func (f *publicationFixture) startCandidate(t *testing.T) *CourseRevision {
	t.Helper()
	candidate, err := f.repo.CreateCandidate(f.ctx, f.courseID, f.ownerID, f.ownerID)
	if err != nil {
		t.Fatalf("CreateCandidate: %v", err)
	}
	return candidate
}

func (f *publicationFixture) liveRevisionID(t *testing.T) string {
	t.Helper()
	var live string
	if err := f.p.QueryRow(f.ctx,
		`SELECT COALESCE(live_revision_id::text, '') FROM courses WHERE id = $1::uuid`, f.courseID,
	).Scan(&live); err != nil {
		t.Fatalf("reading live revision pointer: %v", err)
	}
	return live
}

func (f *publicationFixture) publicTitle(t *testing.T) string {
	t.Helper()
	detail, err := f.public.Detail(f.ctx, f.courseID, false)
	if err != nil {
		t.Fatalf("public Detail: %v", err)
	}
	if detail == nil {
		t.Fatal("course is not publicly visible")
	}
	return detail.Title
}

func (f *publicationFixture) reviewQueueLen(t *testing.T) int {
	t.Helper()
	queue, err := f.repo.ListReviewQueue(f.ctx)
	if err != nil {
		t.Fatalf("ListReviewQueue: %v", err)
	}
	return len(queue)
}

// -------------------------------------------------------------------------
// First publication stays an Admin decision.
// -------------------------------------------------------------------------

func TestInstructorCannotSelfPublishANeverPublishedCourse(t *testing.T) {
	f := newPublicationFixture(t)

	err := f.publish(t, f.firstRevisionID, f.ownerID)
	if !errors.Is(err, ErrFirstPublicationRequiresReview) {
		t.Fatalf("PublishRevision on a never-published course = %v, want ErrFirstPublicationRequiresReview", err)
	}
	if live := f.liveRevisionID(t); live != "" {
		t.Fatalf("live_revision_id = %q after a refused self-publication, want empty", live)
	}

	// The refusal is not a side effect of the revision being a DRAFT: it holds
	// for a revision that has been through submission too.
	if err := f.submit(t, f.firstRevisionID); err != nil {
		t.Fatalf("SubmitCourse: %v", err)
	}
	if err := f.publish(t, f.firstRevisionID, f.ownerID); !errors.Is(err, ErrFirstPublicationRequiresReview) {
		t.Fatalf("PublishRevision on a submitted first revision = %v, want ErrFirstPublicationRequiresReview", err)
	}
	if live := f.liveRevisionID(t); live != "" {
		t.Fatalf("live_revision_id = %q, want the course still unpublished", live)
	}
}

func TestAdminApprovalPublishesTheExactFirstRevision(t *testing.T) {
	f := newPublicationFixture(t)
	f.firstPublish(t)

	if live := f.liveRevisionID(t); live != f.firstRevisionID {
		t.Fatalf("live_revision_id = %q, want the approved first revision %q", live, f.firstRevisionID)
	}
	if title := f.publicTitle(t); title != "Course" {
		t.Fatalf("public title = %q, want %q", title, "Course")
	}

	var actorRole, action string
	var firstPublication bool
	if err := f.p.QueryRow(f.ctx, `
		SELECT actor_role, action, (metadata->>'first_publication')::boolean
		FROM audit_events
		WHERE target_id = $1 AND action IN ('COURSE_PUBLISHED', 'COURSE_REVISION_PUBLISHED')
		ORDER BY occurred_at DESC LIMIT 1
	`, f.courseID).Scan(&actorRole, &action, &firstPublication); err != nil {
		t.Fatalf("reading publication audit: %v", err)
	}
	if actorRole != "ADMIN" || action != "COURSE_PUBLISHED" || !firstPublication {
		t.Fatalf("publication audit = %s/%s first=%v, want ADMIN/COURSE_PUBLISHED first=true", actorRole, action, firstPublication)
	}
}

// -------------------------------------------------------------------------
// Subsequent publications belong to the Instructor.
// -------------------------------------------------------------------------

func TestInstructorPublishesSubsequentRevisionWithoutAdminReview(t *testing.T) {
	f := newPublicationFixture(t)
	f.firstPublish(t)

	candidate := f.startCandidate(t)
	if candidate.ID == f.firstRevisionID {
		t.Fatal("CreateCandidate returned the live revision")
	}
	year := StudyYearYear1
	if _, err := f.repo.UpdateCourseRevision(f.ctx, f.validator, UpdateRevisionRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, OwnerAccountID: f.ownerID,
		TitleAr: "دورة محدثة", TitleEn: "Revised Course",
		DescriptionAr: "وصف", DescriptionEn: "Description",
		MajorTermID: &f.majorID, SubjectTermID: &f.subjectID, StudyYear: &year,
	}, f.ownerID); err != nil {
		t.Fatalf("UpdateCourseRevision: %v", err)
	}

	// Nothing has leaked: the public read still serves the approved revision.
	if title := f.publicTitle(t); title != "Course" {
		t.Fatalf("public title while editing = %q, want the live revision's %q", title, "Course")
	}
	if live := f.liveRevisionID(t); live != f.firstRevisionID {
		t.Fatalf("live_revision_id moved during editing: %q", live)
	}

	if err := f.publish(t, candidate.ID, f.ownerID); err != nil {
		t.Fatalf("PublishRevision: %v", err)
	}

	if live := f.liveRevisionID(t); live != candidate.ID {
		t.Fatalf("live_revision_id = %q, want the published candidate %q", live, candidate.ID)
	}
	if title := f.publicTitle(t); title != "Revised Course" {
		t.Fatalf("public title after publish = %q, want %q", title, "Revised Course")
	}
	if n := f.reviewQueueLen(t); n != 0 {
		t.Fatalf("review queue holds %d items after an Instructor publication, want 0", n)
	}

	var state, previousState string
	if err := f.p.QueryRow(f.ctx, `SELECT state::text FROM course_revisions WHERE id=$1::uuid`, candidate.ID).Scan(&state); err != nil {
		t.Fatalf("reading candidate state: %v", err)
	}
	if err := f.p.QueryRow(f.ctx, `SELECT state::text FROM course_revisions WHERE id=$1::uuid`, f.firstRevisionID).Scan(&previousState); err != nil {
		t.Fatalf("reading previous state: %v", err)
	}
	if state != string(RevisionApproved) || previousState != string(RevisionSuperseded) {
		t.Fatalf("states after publish = candidate %s / previous %s, want APPROVED / SUPERSEDED", state, previousState)
	}

	var actorRole, action, actorAccount string
	var firstPublication bool
	var revisionID string
	if err := f.p.QueryRow(f.ctx, `
		SELECT actor_role, action, actor_account_id::text, metadata->>'revision_id',
		       (metadata->>'first_publication')::boolean
		FROM audit_events
		WHERE target_id = $1 AND action = 'COURSE_REVISION_PUBLISHED'
		ORDER BY occurred_at DESC LIMIT 1
	`, f.courseID).Scan(&actorRole, &action, &actorAccount, &revisionID, &firstPublication); err != nil {
		t.Fatalf("reading instructor publication audit: %v", err)
	}
	if actorRole != "INSTRUCTOR" || actorAccount != f.ownerID || revisionID != candidate.ID || firstPublication {
		t.Fatalf("audit = %s/%s revision %s first=%v, want INSTRUCTOR/%s/%s first=false",
			actorRole, actorAccount, revisionID, firstPublication, f.ownerID, candidate.ID)
	}
}

func TestSubmittingAnAlreadyPublishedCourseIsRefused(t *testing.T) {
	f := newPublicationFixture(t)
	f.firstPublish(t)
	candidate := f.startCandidate(t)

	if err := f.submit(t, candidate.ID); !errors.Is(err, ErrAlreadyPublished) {
		t.Fatalf("SubmitCourse on a published course = %v, want ErrAlreadyPublished", err)
	}
	if n := f.reviewQueueLen(t); n != 0 {
		t.Fatalf("review queue holds %d items, want 0 — a routine edit must never enqueue", n)
	}
	var state string
	if err := f.p.QueryRow(f.ctx, `SELECT state::text FROM course_revisions WHERE id=$1::uuid`, candidate.ID).Scan(&state); err != nil {
		t.Fatalf("reading candidate state: %v", err)
	}
	if state != string(RevisionDraft) {
		t.Fatalf("candidate state = %s after a refused submission, want DRAFT", state)
	}
}

func TestNewLessonVideoAndThumbnailFollowTheSubsequentPublicationRule(t *testing.T) {
	f := newPublicationFixture(t)
	f.firstPublish(t)
	candidate := f.startCandidate(t)

	lesson, err := f.repo.AddLesson(f.ctx, AddLessonRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, SectionID: f.sectionID,
		OwnerAccountID: f.ownerID, TitleAr: "درس ثانٍ", TitleEn: "Second Lesson",
	}, f.ownerID)
	if err != nil {
		t.Fatalf("AddLesson: %v", err)
	}

	// A Lesson with no video at all is incomplete, exactly as it is at
	// submission. Publication is a completeness gate whoever presses it.
	if err := f.publish(t, candidate.ID, f.ownerID); err == nil {
		t.Fatal("PublishRevision succeeded with a video-less lesson, want a validation refusal")
	}

	// Attaching a video that is still processing does not satisfy it either.
	if _, err := f.repo.SetLessonVideo(f.ctx, f.validator, SetVideoRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, LessonID: lesson.LessonIdentityID,
		VideoAssetVersionID: f.processingVideoID, OwnerAccountID: f.ownerID,
	}, f.ownerID); err == nil {
		t.Fatal("SetLessonVideo accepted a non-READY asset version")
	}

	if _, err := f.repo.SetLessonVideo(f.ctx, f.validator, SetVideoRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, LessonID: lesson.LessonIdentityID,
		VideoAssetVersionID: f.spareVideoID, OwnerAccountID: f.ownerID,
	}, f.ownerID); err != nil {
		t.Fatalf("SetLessonVideo with a READY asset: %v", err)
	}

	if err := f.publish(t, candidate.ID, f.ownerID); err != nil {
		t.Fatalf("PublishRevision with a READY new lesson video: %v", err)
	}
	if live := f.liveRevisionID(t); live != candidate.ID {
		t.Fatalf("live_revision_id = %q, want %q", live, candidate.ID)
	}
	if n := f.reviewQueueLen(t); n != 0 {
		t.Fatalf("review queue holds %d items, want 0", n)
	}
}

// -------------------------------------------------------------------------
// The gates that must not have moved.
// -------------------------------------------------------------------------

func TestAnotherInstructorCannotPublishSomeoneElsesCourse(t *testing.T) {
	f := newPublicationFixture(t)
	f.firstPublish(t)
	candidate := f.startCandidate(t)

	if err := f.publish(t, candidate.ID, f.strangerID); !errors.Is(err, ErrCourseNotFound) {
		t.Fatalf("PublishRevision by a non-owner = %v, want ErrCourseNotFound", err)
	}
	if live := f.liveRevisionID(t); live != f.firstRevisionID {
		t.Fatalf("live_revision_id = %q after a refused publication, want %q", live, f.firstRevisionID)
	}
}

func TestStaleCandidateCannotOverwriteANewerLiveRevision(t *testing.T) {
	f := newPublicationFixture(t)
	f.firstPublish(t)

	stale := f.startCandidate(t)
	// The stale candidate is based on the first revision. Publishing it moves
	// the pointer; a second attempt with the same now-superseded base must be
	// refused rather than reverting the Course.
	if err := f.publish(t, stale.ID, f.ownerID); err != nil {
		t.Fatalf("first PublishRevision: %v", err)
	}
	newer := f.startCandidate(t)
	if err := f.publish(t, newer.ID, f.ownerID); err != nil {
		t.Fatalf("second PublishRevision: %v", err)
	}

	// Re-publishing the earlier revision is refused: it is APPROVED, not an
	// open candidate, and its base no longer matches the live pointer.
	err := f.publish(t, stale.ID, f.ownerID)
	var conflict *LifecycleConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("republishing a superseded revision = %v, want *LifecycleConflictError", err)
	}
	if live := f.liveRevisionID(t); live != newer.ID {
		t.Fatalf("live_revision_id = %q, want the newest published revision %q", live, newer.ID)
	}
}

func TestPublicationFailureLeavesThePreviousLiveRevisionUnchanged(t *testing.T) {
	f := newPublicationFixture(t)
	f.firstPublish(t)
	candidate := f.startCandidate(t)

	for _, stage := range []approvalFailureStage{
		approvalAfterSupersede, approvalAfterApprove, approvalAfterPointer,
		approvalAfterAudit, approvalAfterOutbox,
	} {
		ctx := withApprovalFailure(f.ctx, stage)
		if _, err := f.repo.PublishRevision(ctx, f.validator, PublishRevisionRequest{
			CourseID: f.courseID, RevisionID: candidate.ID,
			OwnerAccountID: f.ownerID, ActorDescriptor: f.ownerID,
		}); err == nil {
			t.Fatalf("injected failure at %s did not fail the publication", stage)
		}
		if live := f.liveRevisionID(t); live != f.firstRevisionID {
			t.Fatalf("after failure at %s live_revision_id = %q, want %q", stage, live, f.firstRevisionID)
		}
		var candidateState, previousState string
		if err := f.p.QueryRow(f.ctx, `SELECT state::text FROM course_revisions WHERE id=$1::uuid`, candidate.ID).Scan(&candidateState); err != nil {
			t.Fatalf("reading candidate state: %v", err)
		}
		if err := f.p.QueryRow(f.ctx, `SELECT state::text FROM course_revisions WHERE id=$1::uuid`, f.firstRevisionID).Scan(&previousState); err != nil {
			t.Fatalf("reading previous state: %v", err)
		}
		if candidateState != string(RevisionDraft) || previousState != string(RevisionApproved) {
			t.Fatalf("after failure at %s states = %s / %s, want DRAFT / APPROVED", stage, candidateState, previousState)
		}
	}

	// The same candidate still publishes cleanly once nothing is injected.
	if err := f.publish(t, candidate.ID, f.ownerID); err != nil {
		t.Fatalf("PublishRevision after the injected failures: %v", err)
	}
}

func TestAdminLifecycleRestrictionsSurviveInstructorPublication(t *testing.T) {
	f := newPublicationFixture(t)
	f.firstPublish(t)
	candidate := f.startCandidate(t)

	if _, err := f.repo.TransitionCourseLifecycle(f.ctx, LifecycleMutation{
		CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
		Target: LifecycleDelisted,
	}); err != nil {
		t.Fatalf("TransitionCourseLifecycle: %v", err)
	}

	err := f.publish(t, candidate.ID, f.ownerID)
	var conflict *LifecycleConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("publishing into a delisted course = %v, want *LifecycleConflictError", err)
	}
	var lifecycle, live string
	if err := f.p.QueryRow(f.ctx,
		`SELECT lifecycle::text, live_revision_id::text FROM courses WHERE id=$1::uuid`, f.courseID,
	).Scan(&lifecycle, &live); err != nil {
		t.Fatalf("reading course: %v", err)
	}
	if lifecycle != string(LifecycleDelisted) || live != f.firstRevisionID {
		t.Fatalf("course = %s/%s, want DELISTED and the unchanged live pointer %s", lifecycle, live, f.firstRevisionID)
	}
}

// A Course migrated in already published — its pointer set without this
// build's approval path ever running — must be recognised as having published.
// `courses.live_revision_id` is the durable fact the rule reads, so this is the
// exact shape a backfilled or legacy row presents.
func TestPreviouslyPublishedCourseIsRecognisedWithoutReapproval(t *testing.T) {
	f := newPublicationFixture(t)
	f.firstPublish(t)

	// Drive the Course into a lifecycle it could only reach after publication
	// and back again, proving the rule does not read the lifecycle at all.
	for _, target := range []CourseLifecycle{LifecycleDelisted, LifecyclePublished} {
		if _, err := f.repo.TransitionCourseLifecycle(f.ctx, LifecycleMutation{
			CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID, Target: target,
		}); err != nil {
			t.Fatalf("TransitionCourseLifecycle(%s): %v", target, err)
		}
	}

	candidate := f.startCandidate(t)
	if err := f.publish(t, candidate.ID, f.ownerID); err != nil {
		t.Fatalf("PublishRevision after a delist/relist cycle: %v", err)
	}
	if live := f.liveRevisionID(t); live != candidate.ID {
		t.Fatalf("live_revision_id = %q, want %q", live, candidate.ID)
	}
}
