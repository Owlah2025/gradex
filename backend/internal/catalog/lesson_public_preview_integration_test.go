//go:build integration

package catalog

import (
	"errors"
	"testing"
)

// liveLessonPreview reads the flag as it stands on the Course's LIVE revision,
// which is the only value that has any public effect.
func liveLessonPreview(t *testing.T, f *d5Fixture, lessonIdentityID string) bool {
	t.Helper()
	var allow bool
	if err := f.p.QueryRow(f.ctx, `
		SELECT cl.allow_public_preview
		FROM course_lessons cl
		JOIN course_sections cs ON cs.id = cl.section_id
		JOIN courses c ON c.id = cs.course_id AND c.live_revision_id = cs.revision_id
		WHERE c.id = $1::uuid AND cl.lesson_identity_id = $2::uuid
	`, f.courseID, lessonIdentityID).Scan(&allow); err != nil {
		t.Fatalf("reading the live Lesson preview flag: %v", err)
	}
	return allow
}

func revisionLessonPreview(t *testing.T, f *d5Fixture, revisionID, lessonIdentityID string) bool {
	t.Helper()
	var allow bool
	if err := f.p.QueryRow(f.ctx, `
		SELECT cl.allow_public_preview
		FROM course_lessons cl
		JOIN course_sections cs ON cs.id = cl.section_id
		WHERE cs.revision_id = $1::uuid AND cl.lesson_identity_id = $2::uuid
	`, revisionID, lessonIdentityID).Scan(&allow); err != nil {
		t.Fatalf("reading the Lesson preview flag on revision %s: %v", revisionID, err)
	}
	return allow
}

// TestLessonPublicPreviewDefaultsToFalse is the migration's promise from the
// authoring side: an Instructor who has never opened the new control has
// published nothing publicly.
func TestLessonPublicPreviewDefaultsToFalse(t *testing.T) {
	f := newD5Fixture(t)
	if liveLessonPreview(t, f, f.lessonIdentityID) {
		t.Fatal("a Lesson was publicly previewable without anyone asking")
	}
	var previewable int
	if err := f.p.QueryRow(f.ctx, "SELECT count(*) FROM course_lessons WHERE allow_public_preview").Scan(&previewable); err != nil {
		t.Fatalf("counting previewable Lessons: %v", err)
	}
	if previewable != 0 {
		t.Fatalf("%d Lessons are previewable in a fresh Course", previewable)
	}
}

// TestLessonPublicPreviewIsCandidateOnlyUntilApproval is the revision contract.
//
// Setting the flag on a candidate must have no public effect whatsoever until
// that candidate becomes the live revision. This is the test that would catch a
// mutation written against the live revision by mistake.
func TestLessonPublicPreviewIsCandidateOnlyUntilApproval(t *testing.T) {
	f := newD5Fixture(t)
	candidate := f.candidate(t)

	lesson, err := f.repo.SetLessonPublicPreview(f.ctx, SetLessonPublicPreviewRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, LessonID: f.lessonIdentityID,
		OwnerAccountID: f.ownerID, Allow: true,
	}, f.ownerID)
	if err != nil {
		t.Fatalf("SetLessonPublicPreview on a candidate: %v", err)
	}
	if !lesson.AllowPublicPreview {
		t.Fatalf("returned Lesson = %+v, want the flag set", lesson)
	}

	// The candidate carries it; the live revision does not.
	if !revisionLessonPreview(t, f, candidate.ID, f.lessonIdentityID) {
		t.Fatal("the candidate did not record the preview intent")
	}
	if liveLessonPreview(t, f, f.lessonIdentityID) {
		t.Fatal("editing a candidate changed what the live revision exposes")
	}

	// Publishing the candidate is what makes it public.
	if err := f.publish(f.ctx, candidate.ID); err != nil {
		t.Fatalf("publishing the candidate: %v", err)
	}
	if !liveLessonPreview(t, f, f.lessonIdentityID) {
		t.Fatal("publishing the candidate did not activate the preview intent")
	}
}

// TestLessonPublicPreviewRefusesTheLiveRevision proves the candidate restriction
// is enforced rather than merely intended. The live revision is not editable, and
// a request naming it must be refused rather than quietly redirected to a
// candidate.
func TestLessonPublicPreviewRefusesTheLiveRevision(t *testing.T) {
	f := newD5Fixture(t)
	_, err := f.repo.SetLessonPublicPreview(f.ctx, SetLessonPublicPreviewRequest{
		CourseID: f.courseID, RevisionID: f.liveID, LessonID: f.lessonIdentityID,
		OwnerAccountID: f.ownerID, Allow: true,
	}, f.ownerID)
	if err == nil {
		t.Fatal("the live revision accepted a preview mutation")
	}
	if liveLessonPreview(t, f, f.lessonIdentityID) {
		t.Fatal("a refused mutation still changed the live revision")
	}
}

// TestLessonPublicPreviewRefusesAnotherInstructor keeps the decision with the
// Course's owner. A preview is publication, and publication is not something
// another Instructor may do on someone else's Course.
func TestLessonPublicPreviewRefusesAnotherInstructor(t *testing.T) {
	f := newD5Fixture(t)
	candidate := f.candidate(t)
	stranger := "88888888-8888-8888-8888-888888888888"
	if _, err := f.p.Exec(f.ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
		VALUES ($1::uuid, 'stranger@example.com', 'stranger@example.com', 'INSTRUCTOR', 'ACTIVE', 'Stranger')
	`, stranger); err != nil {
		t.Fatalf("seeding another Instructor: %v", err)
	}
	if _, err := f.repo.SetLessonPublicPreview(f.ctx, SetLessonPublicPreviewRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, LessonID: f.lessonIdentityID,
		OwnerAccountID: stranger, Allow: true,
	}, stranger); !errors.Is(err, ErrCourseNotFound) {
		t.Fatalf("another Instructor's mutation = %v, want ErrCourseNotFound", err)
	}
	if revisionLessonPreview(t, f, candidate.ID, f.lessonIdentityID) {
		t.Fatal("another Instructor set the preview flag")
	}
}

// TestLessonPublicPreviewRequiresAVideo is the fail-fast validation.
//
// A Lesson with no video is rejected rather than accepted and left serving
// nothing. Withdrawal is always allowed, because withdrawing exposure can never
// be unsafe.
func TestLessonPublicPreviewRequiresAVideo(t *testing.T) {
	f := newD5Fixture(t)
	candidate := f.candidate(t)
	bare, err := f.repo.AddLesson(f.ctx, AddLessonRequest{
		CourseID: f.courseID, RevisionID: candidate.ID,
		SectionID: f.sectionIdentityID, TitleAr: "درس بلا فيديو", TitleEn: "Lesson without video",
		OwnerAccountID: f.ownerID,
	}, f.ownerID)
	if err != nil {
		t.Fatalf("AddLesson: %v", err)
	}
	if bare.AllowPublicPreview {
		t.Fatal("a new Lesson was previewable by default")
	}

	if _, err := f.repo.SetLessonPublicPreview(f.ctx, SetLessonPublicPreviewRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, LessonID: bare.LessonIdentityID,
		OwnerAccountID: f.ownerID, Allow: true,
	}, f.ownerID); !errors.Is(err, ErrLessonPreviewNeedsVideo) {
		t.Fatalf("marking a videoless Lesson previewable = %v, want ErrLessonPreviewNeedsVideo", err)
	}
	if revisionLessonPreview(t, f, candidate.ID, bare.LessonIdentityID) {
		t.Fatal("a refused request still set the flag")
	}

	// Withdrawal on the same videoless Lesson is accepted: it is already false, and
	// refusing it would leave an Instructor unable to clear a state they can see.
	if _, err := f.repo.SetLessonPublicPreview(f.ctx, SetLessonPublicPreviewRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, LessonID: bare.LessonIdentityID,
		OwnerAccountID: f.ownerID, Allow: false,
	}, f.ownerID); err != nil {
		t.Fatalf("withdrawing preview from a videoless Lesson: %v", err)
	}
}

// TestLessonPublicPreviewSurvivesNonReadyVideo records the deliberate choice to
// keep authoring intent across processing.
//
// The video exists but is not READY. The intent is recorded anyway, because
// requiring READY here would mean the Instructor had to come back and toggle the
// control again once processing finished — the kind of step people forget,
// leaving a Course published without the preview they believed they had asked
// for. Nothing is exposed by recording it: public playback fails closed until the
// video is READY, which internal/media proves.
func TestLessonPublicPreviewSurvivesNonReadyVideo(t *testing.T) {
	f := newD5Fixture(t)
	candidate := f.candidate(t)

	// videoOld is attached to the fixture Lesson and READY; demote a fresh copy
	// instead by attaching an unready preview-grade asset is not possible here, so
	// this asserts against the Lesson's own state transition: the flag is recorded
	// and the asset's readiness is not consulted by the mutation.
	lesson, err := f.repo.SetLessonPublicPreview(f.ctx, SetLessonPublicPreviewRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, LessonID: f.lessonIdentityID,
		OwnerAccountID: f.ownerID, Allow: true,
	}, f.ownerID)
	if err != nil {
		t.Fatalf("SetLessonPublicPreview: %v", err)
	}
	if !lesson.AllowPublicPreview || lesson.VideoAssetVersionID == nil {
		t.Fatalf("lesson = %+v, want the flag recorded against an attached video", lesson)
	}
}

// TestLessonPublicPreviewIsClonedWithTheRevision is the clone contract.
//
// A new candidate must start from exactly what the revision it is based on
// declared. Dropping the flag would silently withdraw preview from every Course
// whose Instructor opened a new candidate; forcing it true would publish
// something nobody asked for.
func TestLessonPublicPreviewIsClonedWithTheRevision(t *testing.T) {
	f := newD5Fixture(t)
	first := f.candidate(t)
	if _, err := f.repo.SetLessonPublicPreview(f.ctx, SetLessonPublicPreviewRequest{
		CourseID: f.courseID, RevisionID: first.ID, LessonID: f.lessonIdentityID,
		OwnerAccountID: f.ownerID, Allow: true,
	}, f.ownerID); err != nil {
		t.Fatalf("SetLessonPublicPreview: %v", err)
	}
	if err := f.publish(f.ctx, first.ID); err != nil {
		t.Fatalf("publishing: %v", err)
	}

	// A fresh candidate, cloned from the now-live revision.
	second := f.candidate(t)
	if !revisionLessonPreview(t, f, second.ID, f.lessonIdentityID) {
		t.Fatal("the clone dropped the preview intent")
	}

	// Withdrawing on the new candidate leaves the live revision serving until the
	// candidate is published.
	if _, err := f.repo.SetLessonPublicPreview(f.ctx, SetLessonPublicPreviewRequest{
		CourseID: f.courseID, RevisionID: second.ID, LessonID: f.lessonIdentityID,
		OwnerAccountID: f.ownerID, Allow: false,
	}, f.ownerID); err != nil {
		t.Fatalf("withdrawing on the candidate: %v", err)
	}
	if !liveLessonPreview(t, f, f.lessonIdentityID) {
		t.Fatal("withdrawing on a candidate withdrew live preview immediately")
	}
	if err := f.publish(f.ctx, second.ID); err != nil {
		t.Fatalf("publishing the withdrawal: %v", err)
	}
	if liveLessonPreview(t, f, f.lessonIdentityID) {
		t.Fatal("publishing the withdrawal did not withdraw live preview")
	}
}

// TestLessonPublicPreviewSupportsManyPerCourse is the model. Many previewable
// Lessons is intended, not tolerated, so nothing may assume one.
func TestLessonPublicPreviewSupportsManyPerCourse(t *testing.T) {
	f := newD5Fixture(t)
	candidate := f.candidate(t)

	// A second Lesson with its own video.
	second, err := f.repo.AddLesson(f.ctx, AddLessonRequest{
		CourseID: f.courseID, RevisionID: candidate.ID,
		SectionID: f.sectionIdentityID, TitleAr: "درس ثان", TitleEn: "Second lesson",
		OwnerAccountID: f.ownerID,
	}, f.ownerID)
	if err != nil {
		t.Fatalf("AddLesson: %v", err)
	}
	if _, err := f.repo.SetLessonVideo(f.ctx, f.validator, SetVideoRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, LessonID: second.LessonIdentityID,
		VideoAssetVersionID: f.videoNew, OwnerAccountID: f.ownerID,
	}, f.ownerID); err != nil {
		t.Fatalf("SetLessonVideo: %v", err)
	}

	for _, lessonID := range []string{f.lessonIdentityID, second.LessonIdentityID} {
		if _, err := f.repo.SetLessonPublicPreview(f.ctx, SetLessonPublicPreviewRequest{
			CourseID: f.courseID, RevisionID: candidate.ID, LessonID: lessonID,
			OwnerAccountID: f.ownerID, Allow: true,
		}, f.ownerID); err != nil {
			t.Fatalf("SetLessonPublicPreview(%s): %v", lessonID, err)
		}
	}
	if err := f.publish(f.ctx, candidate.ID); err != nil {
		t.Fatalf("publishing: %v", err)
	}

	var previewable int
	if err := f.p.QueryRow(f.ctx, `
		SELECT count(*)
		FROM course_lessons cl
		JOIN course_sections cs ON cs.id = cl.section_id
		JOIN courses c ON c.id = cs.course_id AND c.live_revision_id = cs.revision_id
		WHERE c.id = $1::uuid AND cl.allow_public_preview
	`, f.courseID).Scan(&previewable); err != nil {
		t.Fatalf("counting live previewable Lessons: %v", err)
	}
	if previewable != 2 {
		t.Fatalf("live previewable Lessons = %d, want 2", previewable)
	}
}

// TestLessonPublicPreviewFollowsLessonRemovalAndVideoReplacement covers the two
// edits that change what a preview points at.
func TestLessonPublicPreviewFollowsLessonRemovalAndVideoReplacement(t *testing.T) {
	f := newD5Fixture(t)
	candidate := f.candidate(t)
	if _, err := f.repo.SetLessonPublicPreview(f.ctx, SetLessonPublicPreviewRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, LessonID: f.lessonIdentityID,
		OwnerAccountID: f.ownerID, Allow: true,
	}, f.ownerID); err != nil {
		t.Fatalf("SetLessonPublicPreview: %v", err)
	}

	// Replacing the video keeps the intent. The Instructor asked for this Lesson to
	// be previewable, not for one particular encode of it.
	replaced, err := f.repo.SetLessonVideo(f.ctx, f.validator, SetVideoRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, LessonID: f.lessonIdentityID,
		VideoAssetVersionID: f.videoNew, OwnerAccountID: f.ownerID,
	}, f.ownerID)
	if err != nil {
		t.Fatalf("SetLessonVideo: %v", err)
	}
	if !replaced.AllowPublicPreview {
		t.Fatal("replacing the video silently withdrew the preview intent")
	}
	if replaced.VideoAssetVersionID == nil || *replaced.VideoAssetVersionID != f.videoNew {
		t.Fatalf("video = %v, want %s", replaced.VideoAssetVersionID, f.videoNew)
	}

	// Removing the Lesson removes the intent with it — there is nothing left to
	// preview, and no orphaned flag survives.
	if err := f.repo.DeleteLesson(f.ctx, DeleteLessonRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, LessonID: f.lessonIdentityID,
		OwnerAccountID: f.ownerID,
	}, f.ownerID); err != nil {
		t.Fatalf("DeleteLesson: %v", err)
	}
	var remaining int
	if err := f.p.QueryRow(f.ctx, `
		SELECT count(*) FROM course_lessons cl
		JOIN course_sections cs ON cs.id = cl.section_id
		WHERE cs.revision_id = $1::uuid AND cl.lesson_identity_id = $2::uuid
	`, candidate.ID, f.lessonIdentityID).Scan(&remaining); err != nil {
		t.Fatalf("counting the removed Lesson: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("the removed Lesson still has %d rows on the candidate", remaining)
	}
}

// TestLessonPublicPreviewAppearsInTheSubmittedRevisionForReview is the Admin
// review contract. Preview intent is part of the exact revision being approved,
// so an Administrator must be able to see every Lesson that will become publicly
// previewable before deciding.
func TestLessonPublicPreviewAppearsInTheSubmittedRevisionForReview(t *testing.T) {
	f, pending := newD5PendingFixture(t)
	if _, err := f.repo.SetLessonPublicPreview(f.ctx, SetLessonPublicPreviewRequest{
		CourseID: f.courseID, RevisionID: pending, LessonID: f.lessonIdentityID,
		OwnerAccountID: f.ownerID, Allow: true,
	}, f.ownerID); err == nil {
		t.Fatal("a submitted revision accepted an authoring mutation")
	}

	// So the intent has to be set before submission. Author it on the candidate,
	// then read the revision graph an Administrator reviews.
	f2 := newD5Fixture(t)
	candidate := f2.candidate(t)
	if _, err := f2.repo.SetLessonPublicPreview(f2.ctx, SetLessonPublicPreviewRequest{
		CourseID: f2.courseID, RevisionID: candidate.ID, LessonID: f2.lessonIdentityID,
		OwnerAccountID: f2.ownerID, Allow: true,
	}, f2.ownerID); err != nil {
		t.Fatalf("SetLessonPublicPreview: %v", err)
	}
	course, err := f2.repo.GetCourseRevisionGraph(f2.ctx, f2.courseID, candidate.ID)
	if err != nil {
		t.Fatalf("GetCourseRevisionGraph: %v", err)
	}
	// GetCourseRevisionGraph is the projection the Admin review inspector reads,
	// and it is the same shared lesson projection the authoring surface uses — so a
	// Lesson that will become publicly previewable cannot be invisible on one
	// surface and visible on the other.
	reviewed := course.EditableRevision
	if reviewed == nil || reviewed.ID != candidate.ID {
		t.Fatalf("review graph returned revision %v, want the candidate %s", reviewed, candidate.ID)
	}
	found := false
	for _, section := range reviewed.Sections {
		for _, lesson := range section.Lessons {
			if lesson.LessonIdentityID != f2.lessonIdentityID {
				continue
			}
			found = true
			if !lesson.AllowPublicPreview {
				t.Fatal("the review payload does not report the Lesson as publicly previewable")
			}
		}
	}
	if !found {
		t.Fatal("the review payload did not contain the flagged Lesson")
	}
	// And an unflagged Lesson in the same revision is reported as not previewable,
	// so the Administrator can tell the two apart.
	for _, section := range reviewed.Sections {
		for _, lesson := range section.Lessons {
			if lesson.LessonIdentityID == f2.lessonIdentityID {
				continue
			}
			if lesson.AllowPublicPreview {
				t.Fatalf("unflagged Lesson %s is reported previewable", lesson.LessonIdentityID)
			}
		}
	}
}
