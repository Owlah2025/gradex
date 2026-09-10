//go:build integration

package catalogpublic

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPublicBundlesRequirePublishedAggregateAndEligibleMembers(t *testing.T) {
	freshCatalogPublicSchema(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, catalogPublicTestDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	const adminID = "41000000-0000-0000-0000-000000000001"
	const instructorID = "41000000-0000-0000-0000-000000000002"
	const bundleID = "42000000-0000-0000-0000-000000000001"
	courses := []string{"43000000-0000-0000-0000-000000000001", "43000000-0000-0000-0000-000000000002"}
	revisions := []string{"44000000-0000-0000-0000-000000000001", "44000000-0000-0000-0000-000000000002"}
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id,email,normalized_email,role,status,display_name) VALUES
		($1::uuid,'admin-bundle@example.com','admin-bundle@example.com','ADMIN','ACTIVE','Admin'),
		($2::uuid,'instructor-bundle@example.com','instructor-bundle@example.com','INSTRUCTOR','ACTIVE','Instructor');
	`, adminID, instructorID); err != nil {
		t.Fatal(err)
	}
	for index := range courses {
		if _, err := pool.Exec(ctx, `INSERT INTO courses (id,owner_account_id,lifecycle) VALUES ($1::uuid,$2::uuid,'DRAFT')`, courses[index], instructorID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO course_revisions (id,course_id,state,revision_number,title_ar,title_en) VALUES ($1::uuid,$2::uuid,'APPROVED',1,'مقرر','Course')`, revisions[index], courses[index]); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE courses SET lifecycle='PUBLISHED',live_revision_id=$1::uuid WHERE id=$2::uuid`, revisions[index], courses[index]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO bundles (id,title_ar,title_en,description_ar,description_en,lifecycle,created_by_account_id,updated_by_account_id) VALUES ($1::uuid,'باقة','Bundle','وصف','Description','PUBLISHED',$2::uuid,$2::uuid)`, bundleID, adminID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO bundle_courses (bundle_id,course_id,position) VALUES ($1::uuid,$2::uuid,0),($1::uuid,$3::uuid,1)`, bundleID, courses[0], courses[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO bundle_price_changes (bundle_id,new_value_minor_units,offer_price_minor_units,changed_by_account_id,reason) VALUES ($1::uuid,50000,40000,$2::uuid,'Public offer')`, bundleID, adminID); err != nil {
		t.Fatal(err)
	}
	repo, err := NewRepository(pool, PublishedOnly)
	if err != nil {
		t.Fatal(err)
	}
	result, err := repo.BrowseBundles(context.Background(), false, 1, 12)
	if err != nil || result.Total != 1 || len(result.Items) != 1 || len(result.Items[0].Members) != 2 {
		t.Fatalf("published Bundle list=%#v error=%v", result, err)
	}
	if result.Items[0].Price.MinorUnits != 40000 || result.Items[0].Price.RegularMinorUnits != 50000 {
		t.Fatalf("Bundle price=%#v", result.Items[0].Price)
	}
	detail, err := repo.BundleDetail(ctx, "bundle-42000000000000000000000000000001", false)
	if err != nil || detail == nil || detail.ID != bundleID {
		t.Fatalf("Bundle detail=%#v error=%v", detail, err)
	}
	// A Bundle that is not itself PUBLISHED must never reach the public
	// surfaces, independently of whether its members are eligible. Without this
	// every lifecycle below would still be listed as long as its Courses were
	// published.
	for _, lifecycle := range []string{"DRAFT", "DELISTED", "ARCHIVED"} {
		if _, err := pool.Exec(ctx, `UPDATE bundles SET lifecycle=$1::bundle_lifecycle WHERE id=$2::uuid`, lifecycle, bundleID); err != nil {
			t.Fatal(err)
		}
		unpublished, err := repo.BrowseBundles(ctx, false, 1, 12)
		if err != nil || unpublished.Total != 0 || len(unpublished.Items) != 0 {
			t.Fatalf("%s Bundle leaked into the public list=%#v error=%v", lifecycle, unpublished, err)
		}
		unpublishedDetail, err := repo.BundleDetail(ctx, bundleID, false)
		if err != nil || unpublishedDetail != nil {
			t.Fatalf("%s Bundle leaked into detail=%#v error=%v", lifecycle, unpublishedDetail, err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE bundles SET lifecycle='PUBLISHED' WHERE id=$1::uuid`, bundleID); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `UPDATE courses SET lifecycle='DELISTED' WHERE id=$1::uuid`, courses[1]); err != nil {
		t.Fatal(err)
	}
	hidden, err := repo.BrowseBundles(ctx, false, 1, 12)
	if err != nil || hidden.Total != 0 || len(hidden.Items) != 0 {
		t.Fatalf("broken Bundle leaked=%#v error=%v", hidden, err)
	}
	missing, err := repo.BundleDetail(ctx, bundleID, false)
	if err != nil || missing != nil {
		t.Fatalf("broken Bundle detail=%#v error=%v", missing, err)
	}
}
