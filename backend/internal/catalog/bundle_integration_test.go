//go:build integration

package catalog

import (
	"context"
	"errors"
	"testing"
)

func TestBundleAdministrationEnforcesMembershipPriceAndLifecycle(t *testing.T) {
	repo, adminID, instructorID, firstCourseID := setupPricingIntegrationTest(t)
	ctx := context.Background()
	second, err := repo.CreateCourse(ctx, CreateCourseRequest{
		OwnerAccountID: instructorID, TitleAr: "المقرر الثاني", TitleEn: "Second Course",
		DescriptionAr: "وصف", DescriptionEn: "Description",
	}, instructorID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateBundle(ctx, CreateBundleRequest{
		TitleAr: "باقة", TitleEn: "Bundle", CourseIDs: []string{firstCourseID},
		AdminAccountID: adminID, ActorDescriptor: adminID,
	}); !errors.Is(err, ErrBundleMemberCount) {
		t.Fatalf("one-member Bundle error=%v", err)
	}
	if _, err := repo.CreateBundle(ctx, CreateBundleRequest{
		TitleAr: "باقة", TitleEn: "Bundle", CourseIDs: []string{firstCourseID, firstCourseID},
		AdminAccountID: adminID, ActorDescriptor: adminID,
	}); !errors.Is(err, ErrBundleMemberCount) {
		t.Fatalf("duplicate-member Bundle error=%v", err)
	}
	if _, err := repo.CreateBundle(ctx, CreateBundleRequest{
		TitleAr: "باقة", TitleEn: "Bundle", CourseIDs: []string{firstCourseID, second.ID},
		AdminAccountID: adminID, ActorDescriptor: adminID,
	}); !errors.Is(err, ErrBundleMemberInvalid) {
		t.Fatalf("draft-member Bundle error=%v", err)
	}
	for _, courseID := range []string{firstCourseID, second.ID} {
		var revisionID string
		if err := repo.pool.QueryRow(ctx, `SELECT id::text FROM course_revisions WHERE course_id=$1::uuid`, courseID).Scan(&revisionID); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.pool.Exec(ctx, `UPDATE course_revisions SET state='APPROVED' WHERE id=$1::uuid`, revisionID); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.pool.Exec(ctx, `UPDATE courses SET lifecycle='PUBLISHED',live_revision_id=$1::uuid WHERE id=$2::uuid`, revisionID, courseID); err != nil {
			t.Fatal(err)
		}
	}
	offer := int64(40000)
	bundle, err := repo.CreateBundle(ctx, CreateBundleRequest{
		TitleAr: "باقة", TitleEn: "Bundle", DescriptionAr: "وصف", DescriptionEn: "Description",
		CourseIDs: []string{second.ID, firstCourseID}, AdminAccountID: adminID, ActorDescriptor: adminID,
		Price: &BundlePriceInput{RegularMinorUnits: 50000, OfferMinorUnits: &offer, Reason: "Bundle offer"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Lifecycle != BundleDraft || len(bundle.Members) != 2 || bundle.Members[0].CourseID != second.ID {
		t.Fatalf("created Bundle=%#v", bundle)
	}
	published, err := repo.TransitionBundle(ctx, TransitionBundleRequest{
		BundleID: bundle.ID, ExpectedRevision: bundle.Revision, Target: BundlePublished,
		AdminAccountID: adminID, ActorDescriptor: adminID,
	})
	if err != nil || published.Lifecycle != BundlePublished || published.Revision != 2 {
		t.Fatalf("published Bundle=%#v error=%v", published, err)
	}
	if _, err := repo.UpdateBundle(ctx, UpdateBundleRequest{
		BundleID: bundle.ID, ExpectedRevision: 1, TitleAr: "قديم", TitleEn: "Stale",
		CourseIDs: []string{firstCourseID, second.ID}, AdminAccountID: adminID, ActorDescriptor: adminID,
	}); !errors.Is(err, ErrBundleVersionConflict) {
		t.Fatalf("stale update error=%v", err)
	}
}
