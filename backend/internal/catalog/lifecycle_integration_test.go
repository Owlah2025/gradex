//go:build integration

package catalog

import (
	"context"
	"errors"
	"testing"
)

func TestCourseLifecycleOwnerAndRetirementPreserveAuthoredState(t *testing.T) {
	f := newD5Fixture(t)
	ctx := context.Background()
	newOwnerID := "30000000-0000-0000-0000-000000000001"
	if _, err := f.p.Exec(ctx, `
		INSERT INTO accounts (id, role, status, email, normalized_email, display_name)
		VALUES ($1::uuid, 'INSTRUCTOR', 'ACTIVE', 'new-owner@example.com', 'new-owner@example.com', 'New owner')
	`, newOwnerID); err != nil {
		t.Fatalf("seeding new owner: %v", err)
	}

	candidate := f.candidate(t)
	var revisionsBefore, pricesBefore, accessBefore int
	if err := f.p.QueryRow(ctx, `SELECT count(*) FROM course_revisions WHERE course_id = $1::uuid`, f.courseID).Scan(&revisionsBefore); err != nil {
		t.Fatal(err)
	}
	if err := f.p.QueryRow(ctx, `SELECT count(*) FROM course_price_changes WHERE course_id = $1::uuid`, f.courseID).Scan(&pricesBefore); err != nil {
		t.Fatal(err)
	}
	if err := f.p.QueryRow(ctx, `SELECT count(*) FROM fake_entitlements`).Scan(&accessBefore); err != nil {
		t.Fatal(err)
	}

	if _, err := f.repo.TransitionCourseLifecycle(ctx, LifecycleMutation{CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID, Target: LifecycleDelisted}); err != nil {
		t.Fatalf("delisting course: %v", err)
	}
	if _, err := f.repo.ReassignCourseOwner(ctx, ReassignCourseOwnerRequest{CourseID: f.courseID, NewOwnerID: newOwnerID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID}); !errors.Is(err, ErrPendingCandidate) {
		t.Fatalf("reassigning owner with active candidate error = %v, want ErrPendingCandidate", err)
	}
	if _, err := f.p.Exec(ctx, `UPDATE course_revisions SET state = 'REJECTED', review_reason = 'Ownership changes require a new candidate' WHERE id = $1::uuid`, candidate.ID); err != nil {
		t.Fatalf("resolving candidate for reassignment: %v", err)
	}
	if _, err := f.repo.ReassignCourseOwner(ctx, ReassignCourseOwnerRequest{CourseID: f.courseID, NewOwnerID: newOwnerID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID}); err != nil {
		t.Fatalf("reassigning owner after candidate resolution: %v", err)
	}
	if _, err := f.repo.TransitionCourseLifecycle(ctx, LifecycleMutation{CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID, Target: LifecyclePublished}); err != nil {
		t.Fatalf("relisting course: %v", err)
	}
	retired, err := f.repo.RetireCourse(ctx, LifecycleMutation{CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID})
	if err != nil {
		t.Fatalf("retiring course: %v", err)
	}
	if retired.RetiredAt == nil {
		t.Fatal("retired course has no retired_at")
	}

	var candidateID, candidateState string
	if err := f.p.QueryRow(ctx, `SELECT id::text, state::text FROM course_revisions WHERE id = $1::uuid`, candidate.ID).Scan(&candidateID, &candidateState); err != nil {
		t.Fatalf("reading candidate: %v", err)
	}
	if candidateID != candidate.ID || candidateState != string(RevisionRejected) {
		t.Fatalf("candidate changed during owner reassignment: id=%s state=%s", candidateID, candidateState)
	}
	var revisionsAfter, pricesAfter, accessAfter int
	if err := f.p.QueryRow(ctx, `SELECT count(*) FROM course_revisions WHERE course_id = $1::uuid`, f.courseID).Scan(&revisionsAfter); err != nil {
		t.Fatal(err)
	}
	if err := f.p.QueryRow(ctx, `SELECT count(*) FROM course_price_changes WHERE course_id = $1::uuid`, f.courseID).Scan(&pricesAfter); err != nil {
		t.Fatal(err)
	}
	if err := f.p.QueryRow(ctx, `SELECT count(*) FROM fake_entitlements`).Scan(&accessAfter); err != nil {
		t.Fatal(err)
	}
	if revisionsAfter != revisionsBefore || pricesAfter != pricesBefore || accessAfter != accessBefore {
		t.Fatalf("lifecycle mutation rewrote related data: revisions %d/%d prices %d/%d access %d/%d", revisionsAfter, revisionsBefore, pricesAfter, pricesBefore, accessAfter, accessBefore)
	}

	if _, err := f.repo.TransitionCourseLifecycle(ctx, LifecycleMutation{CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID, Target: LifecycleDraft}); err == nil {
		t.Fatal("published to draft transition was allowed")
	} else {
		var conflict *LifecycleConflictError
		if !errors.As(err, &conflict) {
			t.Fatalf("published to draft transition error = %v, want LifecycleConflictError", err)
		}
	}
	if _, err := f.repo.TransitionCourseLifecycle(ctx, LifecycleMutation{CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID, Target: LifecycleArchived}); err != nil {
		t.Fatalf("archiving published course: %v", err)
	}
}

func TestLifecycleCompatibilityStateKeepsExistingAccessUntilEmergencySuspension(t *testing.T) {
	f := newD5Fixture(t)
	ctx := context.Background()
	studentID := "40000000-0000-0000-0000-000000000001"
	sectionID := "40000000-0000-0000-0000-000000000002"
	lessonID := "40000000-0000-0000-0000-000000000003"
	if _, err := f.p.Exec(ctx, `INSERT INTO accounts (id, role, status, email, normalized_email, display_name) VALUES ($1::uuid, 'STUDENT', 'ACTIVE', 'student@example.com', 'student@example.com', 'Student')`, studentID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(ctx, `INSERT INTO sections (id, course_id, title, "order") VALUES ($1::uuid, $2::uuid, 'Compatibility section', 1)`, sectionID, f.courseID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(ctx, `INSERT INTO lessons (id, section_id, title, "order") VALUES ($1::uuid, $2::uuid, 'Compatibility lesson', 1)`, lessonID, sectionID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(ctx, `INSERT INTO fake_entitlements (user_id, lesson_id, role) VALUES ($1::uuid, $2::uuid, 'student')`, studentID, lessonID); err != nil {
		t.Fatal(err)
	}

	assertCompatibilityAccess := func(wantSuspended bool) {
		t.Helper()
		state, err := f.repo.ReadCourseAccessStateForLesson(ctx, lessonID)
		if err != nil {
			t.Fatalf("reading live course state: %v", err)
		}
		if (state.AccessSuspendedAt != nil) != wantSuspended {
			t.Fatalf("suspension state = %v, want %v", state.AccessSuspendedAt, wantSuspended)
		}
		var grants int
		if err := f.p.QueryRow(ctx, `SELECT count(*) FROM fake_entitlements WHERE user_id = $1::uuid AND lesson_id = $2::uuid`, studentID, lessonID).Scan(&grants); err != nil {
			t.Fatal(err)
		}
		if grants != 1 {
			t.Fatalf("existing access fixture changed to %d grants", grants)
		}
	}

	for _, target := range []CourseLifecycle{LifecycleDelisted, LifecyclePublished} {
		if _, err := f.repo.TransitionCourseLifecycle(ctx, LifecycleMutation{CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID, Target: target}); err != nil {
			t.Fatalf("transition %s: %v", target, err)
		}
		assertCompatibilityAccess(false)
	}
	if _, err := f.repo.RetireCourse(ctx, LifecycleMutation{CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID}); err != nil {
		t.Fatal(err)
	}
	assertCompatibilityAccess(false)
	if _, err := f.repo.SuspendCourseAccess(ctx, SuspendCourseAccessRequest{CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID, Cause: SuspensionCauseSecurity, Reason: "Verified incident"}); err != nil {
		t.Fatal(err)
	}
	assertCompatibilityAccess(true)
	if _, err := f.repo.RestoreCourseAccess(ctx, RestoreCourseAccessRequest{CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID, Reason: "Incident closed"}); err != nil {
		t.Fatal(err)
	}
	assertCompatibilityAccess(false)
	if _, err := f.repo.TransitionCourseLifecycle(ctx, LifecycleMutation{CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID, Target: LifecycleArchived}); err != nil {
		t.Fatal(err)
	}
	assertCompatibilityAccess(false)

	if err := f.repo.DeleteCourse(ctx, LifecycleMutation{CourseID: f.courseID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID}); !errors.Is(err, ErrCourseHasAccess) {
		t.Fatalf("deleting course with access error = %v, want ErrCourseHasAccess", err)
	}
}

func TestDeleteCourseWithoutAccessRemovesRevisionOwnedData(t *testing.T) {
	repo, adminID, _, courseID := setupPricingIntegrationTest(t)
	if err := repo.DeleteCourse(context.Background(), LifecycleMutation{CourseID: courseID, AdminAccountID: adminID, ActorDescriptor: adminID}); err != nil {
		t.Fatalf("deleting zero-access course: %v", err)
	}
	if _, err := repo.ReadCourseAccessState(context.Background(), courseID); !errors.Is(err, ErrCourseNotFound) {
		t.Fatalf("deleted course state error = %v, want ErrCourseNotFound", err)
	}
}
