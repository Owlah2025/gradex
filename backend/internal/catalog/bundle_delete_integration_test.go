//go:build integration

package catalog

import (
	"context"
	"errors"
	"testing"
)

// publishBundleFixtureCourses promotes the two Courses a Bundle needs into the
// published state the membership rule requires, and prices them so the Admin
// list has a truthful member total to sum.
func publishBundleFixtureCourses(t *testing.T, repo *Repository, adminID string, courseIDs ...string) {
	t.Helper()
	ctx := context.Background()
	for index, courseID := range courseIDs {
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
		if _, err := repo.pool.Exec(ctx, `
			INSERT INTO course_price_changes (course_id, new_value_minor_units, changed_by_account_id, reason)
			VALUES ($1::uuid, $2, $3::uuid, 'fixture')
		`, courseID, int64(30000+1000*index), adminID); err != nil {
			t.Fatal(err)
		}
	}
}

func bundleFixture(t *testing.T) (*Repository, string, string, string) {
	t.Helper()
	repo, adminID, instructorID, firstCourseID := setupPricingIntegrationTest(t)
	second, err := repo.CreateCourse(context.Background(), CreateCourseRequest{
		OwnerAccountID: instructorID, TitleAr: "المقرر الثاني", TitleEn: "Second Course",
		DescriptionAr: "وصف", DescriptionEn: "Description",
	}, instructorID)
	if err != nil {
		t.Fatal(err)
	}
	publishBundleFixtureCourses(t, repo, adminID, firstCourseID, second.ID)
	return repo, adminID, firstCourseID, second.ID
}

// The Admin list is a management surface, not a count: it must carry the member
// names, the summed standalone price the Bundle discounts against, and whether
// deletion is actually available.
func TestListBundlesCarriesMembersTotalsAndDeletability(t *testing.T) {
	repo, adminID, firstCourseID, secondCourseID := bundleFixture(t)
	ctx := context.Background()

	empty, err := repo.ListBundles(ctx)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty list=%#v error=%v", empty, err)
	}

	offer := int64(40000)
	created, err := repo.CreateBundle(ctx, CreateBundleRequest{
		TitleAr: "باقة", TitleEn: "Bundle", DescriptionAr: "وصف", DescriptionEn: "Description",
		CourseIDs: []string{firstCourseID, secondCourseID}, AdminAccountID: adminID, ActorDescriptor: adminID,
		Price: &BundlePriceInput{RegularMinorUnits: 50000, OfferMinorUnits: &offer, Reason: "Bundle offer"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.MemberTotalMinorUnits == nil || *created.MemberTotalMinorUnits != 61000 {
		t.Fatalf("created member total=%v", created.MemberTotalMinorUnits)
	}
	if !created.Deletable {
		t.Fatal("a Bundle nobody has requested must be deletable")
	}

	items, err := repo.ListBundles(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("list=%#v error=%v", items, err)
	}
	listed := items[0]
	if len(listed.Members) != 2 || listed.Members[0].CourseID != firstCourseID {
		t.Fatalf("listed members=%#v", listed.Members)
	}
	if listed.Members[0].TitleEn == "" || listed.Members[0].InstructorDisplayName == "" {
		t.Fatalf("listed member identity is empty: %#v", listed.Members[0])
	}
	if listed.MemberTotalMinorUnits == nil || *listed.MemberTotalMinorUnits != 61000 {
		t.Fatalf("listed member total=%v", listed.MemberTotalMinorUnits)
	}
	if !listed.Deletable || listed.CourseCount != 2 {
		t.Fatalf("listed=%#v", listed)
	}
}

// A price edit on a published Bundle is allowed by the domain and must be the
// value the next authoritative read returns -- no revision flow, no drift.
func TestBundlePriceEditSurvivesReadAndRejectsBadOffers(t *testing.T) {
	repo, adminID, firstCourseID, secondCourseID := bundleFixture(t)
	ctx := context.Background()
	offer := int64(40000)
	bundle, err := repo.CreateBundle(ctx, CreateBundleRequest{
		TitleAr: "باقة", TitleEn: "Bundle", DescriptionAr: "وصف", DescriptionEn: "Description",
		CourseIDs: []string{firstCourseID, secondCourseID}, AdminAccountID: adminID, ActorDescriptor: adminID,
		Price: &BundlePriceInput{RegularMinorUnits: 50000, OfferMinorUnits: &offer, Reason: "Bundle offer"},
	})
	if err != nil {
		t.Fatal(err)
	}
	published, err := repo.TransitionBundle(ctx, TransitionBundleRequest{
		BundleID: bundle.ID, ExpectedRevision: bundle.Revision, Target: BundlePublished,
		AdminAccountID: adminID, ActorDescriptor: adminID,
	})
	if err != nil {
		t.Fatal(err)
	}
	// A lifecycle reply must not come back with an empty membership.
	if len(published.Members) != 2 || published.CourseCount != 2 || published.Price == nil {
		t.Fatalf("published reply=%#v", published)
	}

	// A price change with no reason is refused; the authoritative price is unchanged.
	if _, err := repo.UpdateBundle(ctx, UpdateBundleRequest{
		BundleID: bundle.ID, ExpectedRevision: published.Revision,
		TitleAr: "باقة", TitleEn: "Bundle", DescriptionAr: "وصف", DescriptionEn: "Description",
		CourseIDs:      []string{firstCourseID, secondCourseID},
		Price:          &BundlePriceInput{RegularMinorUnits: 45000, Reason: "  "},
		AdminAccountID: adminID, ActorDescriptor: adminID,
	}); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("reasonless price change error=%v", err)
	}
	// An offer at or above the regular price is not a discount.
	badOffer := int64(45000)
	if _, err := repo.UpdateBundle(ctx, UpdateBundleRequest{
		BundleID: bundle.ID, ExpectedRevision: published.Revision,
		TitleAr: "باقة", TitleEn: "Bundle", DescriptionAr: "وصف", DescriptionEn: "Description",
		CourseIDs:      []string{firstCourseID, secondCourseID},
		Price:          &BundlePriceInput{RegularMinorUnits: 45000, OfferMinorUnits: &badOffer, Reason: "bad"},
		AdminAccountID: adminID, ActorDescriptor: adminID,
	}); !errors.Is(err, ErrInvalidOfferPrice) {
		t.Fatalf("non-discount offer error=%v", err)
	}

	newOffer := int64(33000)
	updated, err := repo.UpdateBundle(ctx, UpdateBundleRequest{
		BundleID: bundle.ID, ExpectedRevision: published.Revision,
		TitleAr: "باقة", TitleEn: "Bundle", DescriptionAr: "وصف", DescriptionEn: "Description",
		CourseIDs:      []string{firstCourseID, secondCourseID},
		Price:          &BundlePriceInput{RegularMinorUnits: 45000, OfferMinorUnits: &newOffer, Reason: "Seasonal"},
		AdminAccountID: adminID, ActorDescriptor: adminID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Price == nil || updated.Price.RegularMinorUnits != 45000 || updated.Price.EffectiveMinorUnits != 33000 {
		t.Fatalf("updated price=%#v", updated.Price)
	}
	if updated.Lifecycle != BundlePublished {
		t.Fatalf("a price edit must not move the lifecycle: %v", updated.Lifecycle)
	}
	// The authoritative read agrees with the mutation reply.
	reread, err := repo.GetBundle(ctx, bundle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reread.Price == nil || reread.Price.EffectiveMinorUnits != 33000 || reread.Revision != updated.Revision {
		t.Fatalf("re-read=%#v", reread)
	}
}

func TestDeleteBundleRemovesUnreferencedBundlesAndRefusesReferencedOnes(t *testing.T) {
	repo, adminID, firstCourseID, secondCourseID := bundleFixture(t)
	ctx := context.Background()
	offer := int64(40000)
	newBundle := func(titleEn string) *Bundle {
		t.Helper()
		created, err := repo.CreateBundle(ctx, CreateBundleRequest{
			TitleAr: "باقة", TitleEn: titleEn, DescriptionAr: "وصف", DescriptionEn: "Description",
			CourseIDs: []string{firstCourseID, secondCourseID}, AdminAccountID: adminID, ActorDescriptor: adminID,
			Price: &BundlePriceInput{RegularMinorUnits: 50000, OfferMinorUnits: &offer, Reason: "Bundle offer"},
		})
		if err != nil {
			t.Fatal(err)
		}
		return created
	}

	// A stale revision cannot delete.
	stale := newBundle("Stale")
	if err := repo.DeleteBundle(ctx, DeleteBundleRequest{
		BundleID: stale.ID, ExpectedRevision: stale.Revision + 5,
		AdminAccountID: adminID, ActorDescriptor: adminID,
	}); !errors.Is(err, ErrBundleVersionConflict) {
		t.Fatalf("stale delete error=%v", err)
	}

	// A draft nobody has requested deletes outright, taking its own rows with it.
	if err := repo.DeleteBundle(ctx, DeleteBundleRequest{
		BundleID: stale.ID, ExpectedRevision: stale.Revision,
		AdminAccountID: adminID, ActorDescriptor: adminID,
	}); err != nil {
		t.Fatalf("deleting an unreferenced draft: %v", err)
	}
	if _, err := repo.GetBundle(ctx, stale.ID); !errors.Is(err, ErrBundleNotFound) {
		t.Fatalf("deleted Bundle is still readable: %v", err)
	}
	for _, table := range []string{"bundle_courses", "bundle_price_changes"} {
		var remaining int
		if err := repo.pool.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE bundle_id=$1::uuid`, stale.ID).Scan(&remaining); err != nil {
			t.Fatal(err)
		}
		if remaining != 0 {
			t.Fatalf("%s kept %d orphan rows", table, remaining)
		}
	}
	// The deletion itself is on the record.
	var audited int
	if err := repo.pool.QueryRow(ctx, `
		SELECT count(*) FROM audit_events WHERE action='BUNDLE_DELETED' AND target_id=$1
	`, stale.ID).Scan(&audited); err != nil {
		t.Fatal(err)
	}
	if audited != 1 {
		t.Fatalf("BUNDLE_DELETED audit rows=%d", audited)
	}
	// Deleting it twice is a not-found, not a second deletion.
	if err := repo.DeleteBundle(ctx, DeleteBundleRequest{
		BundleID: stale.ID, ExpectedRevision: stale.Revision,
		AdminAccountID: adminID, ActorDescriptor: adminID,
	}); !errors.Is(err, ErrBundleNotFound) {
		t.Fatalf("second delete error=%v", err)
	}

	// A published Bundle is still deletable while no commerce points at it, and
	// the public listing loses it immediately.
	live := newBundle("Live")
	published, err := repo.TransitionBundle(ctx, TransitionBundleRequest{
		BundleID: live.ID, ExpectedRevision: live.Revision, Target: BundlePublished,
		AdminAccountID: adminID, ActorDescriptor: adminID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteBundle(ctx, DeleteBundleRequest{
		BundleID: live.ID, ExpectedRevision: published.Revision,
		AdminAccountID: adminID, ActorDescriptor: adminID,
	}); err != nil {
		t.Fatalf("deleting an unreferenced published Bundle: %v", err)
	}

	// A Bundle a Student has requested is refused, and survives the refusal.
	referenced := newBundle("Referenced")
	if _, err := repo.pool.Exec(ctx, `
		INSERT INTO purchase_requests (
			reference_code, email, normalized_email, price_minor_units, currency, state,
			target_kind, bundle_id, bundle_revision, bundle_title_ar, bundle_title_en
		) VALUES ('GRX-BUNDLE-1', 'student@example.com', 'student@example.com', 50000, 'KWD',
		          'WAITING_PAYMENT', 'BUNDLE', $1::uuid, $2, 'باقة', 'Referenced')
	`, referenced.ID, referenced.Revision); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteBundle(ctx, DeleteBundleRequest{
		BundleID: referenced.ID, ExpectedRevision: referenced.Revision,
		AdminAccountID: adminID, ActorDescriptor: adminID,
	}); !errors.Is(err, ErrBundleReferenced) {
		t.Fatalf("referenced delete error=%v", err)
	}
	survivor, err := repo.GetBundle(ctx, referenced.ID)
	if err != nil {
		t.Fatalf("referenced Bundle did not survive the refusal: %v", err)
	}
	if survivor.Deletable {
		t.Fatal("a referenced Bundle must not advertise deletion")
	}
	// Archive remains the supported end state for it.
	archived, err := repo.TransitionBundle(ctx, TransitionBundleRequest{
		BundleID: referenced.ID, ExpectedRevision: survivor.Revision, Target: BundleArchived,
		AdminAccountID: adminID, ActorDescriptor: adminID,
	})
	if err != nil || archived.Lifecycle != BundleArchived {
		t.Fatalf("archived=%#v error=%v", archived, err)
	}
}
