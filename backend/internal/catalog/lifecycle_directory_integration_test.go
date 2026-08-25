//go:build integration

package catalog

import (
	"context"
	"testing"
)

// T8C / AD-12. The Admin lifecycle directory exists to be the one read that survives every
// lifecycle state, because the public catalogue deliberately hides four of them. If a delisted or
// archived Course dropped out of this read, relisting it would be unreachable through the product
// even though the command itself works.
func TestLifecycleDirectoryReturnsCoursesInEveryLifecycleState(t *testing.T) {
	f := newD5Fixture(t)
	ctx := context.Background()

	// Give the Course the published live revision the directory reads its title from.
	if _, err := f.p.Exec(ctx, `
		UPDATE courses SET lifecycle = 'PUBLISHED', live_revision_id = (
			SELECT id FROM course_revisions WHERE course_id = $1::uuid ORDER BY revision_number DESC LIMIT 1
		) WHERE id = $1::uuid
	`, f.courseID); err != nil {
		t.Fatalf("publishing fixture course: %v", err)
	}

	titleEn := ""
	if err := f.p.QueryRow(ctx, `
		SELECT title_en FROM course_revisions WHERE course_id = $1::uuid ORDER BY revision_number DESC LIMIT 1
	`, f.courseID).Scan(&titleEn); err != nil {
		t.Fatalf("reading fixture title: %v", err)
	}

	find := func(t *testing.T) CourseLifecycleSummary {
		t.Helper()
		summaries, err := f.repo.ListCourseLifecycleDirectory(ctx, titleEn)
		if err != nil {
			t.Fatalf("listing lifecycle directory: %v", err)
		}
		for _, summary := range summaries {
			if summary.ID == f.courseID {
				return summary
			}
		}
		t.Fatalf("Course %s is not in the Admin lifecycle directory", f.courseID)
		return CourseLifecycleSummary{}
	}

	published := find(t)
	if published.Lifecycle != LifecyclePublished || published.TitleEn != titleEn || published.OwnerDisplayName == "" {
		t.Fatalf("published summary = %+v, want PUBLISHED with title and owner", published)
	}
	if published.AccessSuspendedAt != nil || published.RetiredAt != nil {
		t.Fatalf("published summary carries suspension or retirement: %+v", published)
	}

	if _, err := f.repo.TransitionCourseLifecycle(ctx, LifecycleMutation{
		CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID, Target: LifecycleDelisted,
	}); err != nil {
		t.Fatalf("delisting course: %v", err)
	}
	if delisted := find(t); delisted.Lifecycle != LifecycleDelisted {
		t.Fatalf("delisted summary lifecycle = %s, want DELISTED", delisted.Lifecycle)
	}

	if _, err := f.repo.SuspendCourseAccess(ctx, SuspendCourseAccessRequest{
		CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
		Cause: SuspensionCauseSecurity, Reason: "T8C directory suspension",
	}); err != nil {
		t.Fatalf("suspending course access: %v", err)
	}
	suspended := find(t)
	if suspended.AccessSuspendedAt == nil {
		t.Fatal("suspended summary has no access_suspended_at")
	}
	if suspended.AccessSuspensionReason == nil || *suspended.AccessSuspensionReason != "T8C directory suspension" {
		t.Fatalf("suspended summary reason = %v, want the recorded reason", suspended.AccessSuspensionReason)
	}

	if _, err := f.repo.RestoreCourseAccess(ctx, RestoreCourseAccessRequest{
		CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID, Reason: "T8C directory restoration",
	}); err != nil {
		t.Fatalf("restoring course access: %v", err)
	}
	if restored := find(t); restored.AccessSuspendedAt != nil || restored.AccessSuspensionReason != nil {
		t.Fatalf("restored summary still carries suspension: %+v", restored)
	}

	if _, err := f.repo.RetireCourse(ctx, LifecycleMutation{
		CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
	}); err != nil {
		t.Fatalf("retiring course: %v", err)
	}
	retired := find(t)
	if retired.RetiredAt == nil || retired.Lifecycle != LifecycleDelisted {
		t.Fatalf("retired summary = %+v, want retired_at set and DELISTED preserved", retired)
	}

	if _, err := f.repo.TransitionCourseLifecycle(ctx, LifecycleMutation{
		CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID, Target: LifecycleArchived,
	}); err != nil {
		t.Fatalf("archiving course: %v", err)
	}
	archived := find(t)
	if archived.Lifecycle != LifecycleArchived || archived.RetiredAt == nil {
		t.Fatalf("archived summary = %+v, want ARCHIVED with retirement preserved", archived)
	}

	// Archival is terminal, so nothing transitions out of it — the refusal is the contract that
	// makes the browser case's "no un-archive" assertion a domain rule and not a UI opinion.
	if _, err := f.repo.TransitionCourseLifecycle(ctx, LifecycleMutation{
		CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID, Target: LifecyclePublished,
	}); err == nil {
		t.Fatal("relisting an archived Course was allowed")
	}

	// A search that matches nothing is an empty directory, never the whole catalogue.
	empty, err := f.repo.ListCourseLifecycleDirectory(ctx, "no-course-carries-this-title")
	if err != nil {
		t.Fatalf("listing with a non-matching search: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("non-matching search returned %d Courses", len(empty))
	}
}
