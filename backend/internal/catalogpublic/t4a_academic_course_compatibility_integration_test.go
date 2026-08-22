//go:build integration

package catalogpublic

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// T4-A public catalogue compatibility.
//
// An ACADEMIC_CATALOG Course carries no legacy taxonomy at all, so the question
// this answers is whether the existing public projection and search survive a
// Course whose major_term_id, subject_term_id and study_year are NULL — not
// whether academic filtering works, which is T6 and is deliberately absent.
//
// The T4-A architecture trace predicted this needs no production change: the
// projection LEFT JOINs the taxonomy tables and NULL-guards every label, and
// course_revisions.search_text is generated from titles and descriptions alone.
// This case is what turns that prediction into an observed result.

func seedAcademicPublishedCourse(t *testing.T, pool *pgxpool.Pool, ctx context.Context) string {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
		VALUES ('22222222-2222-2222-2222-222222222222', 'academic@example.test',
		        'academic@example.test', 'INSTRUCTOR', 'ACTIVE', 'Academic Owner')`); err != nil {
		t.Fatalf("seeding owner: %v", err)
	}
	var institutionID, subjectID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO institutions (country_code, slug, name_ar, name_en)
		VALUES ('KW', 'kuwait-university', 'جامعة الكويت', 'Kuwait University') RETURNING id::text`).
		Scan(&institutionID); err != nil {
		t.Fatalf("seeding institution: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO subjects (institution_id, official_code, title_ar, title_en)
		VALUES ($1::uuid, '0418-320', 'مبادئ نظم الحاسوب', 'Principles of Computer Systems')
		RETURNING id::text`, institutionID).Scan(&subjectID); err != nil {
		t.Fatalf("seeding subject: %v", err)
	}

	var courseID, revisionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO courses (owner_account_id, lifecycle, classification_model, institution_id, subject_id)
		VALUES ('22222222-2222-2222-2222-222222222222', 'DRAFT', 'ACADEMIC_CATALOG', $1::uuid, $2::uuid)
		RETURNING id::text`, institutionID, subjectID).Scan(&courseID); err != nil {
		t.Fatalf("seeding academic course: %v", err)
	}
	// Deliberately no major_term_id, subject_term_id, or study_year: an Academic
	// Course must never populate the legacy vocabulary to become publishable.
	if err := pool.QueryRow(ctx, `
		INSERT INTO course_revisions (course_id, state, revision_number, title_ar, title_en, description_ar, description_en)
		VALUES ($1::uuid, 'APPROVED', 1, 'مبادئ نظم الحاسوب', 'Principles of Computer Systems',
		        'وصف', 'Course description') RETURNING id::text`, courseID).Scan(&revisionID); err != nil {
		t.Fatalf("seeding revision: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE courses SET lifecycle = 'PUBLISHED', live_revision_id = $1::uuid WHERE id = $2::uuid`,
		revisionID, courseID); err != nil {
		t.Fatalf("publishing academic course: %v", err)
	}
	return courseID
}

func TestT4AAcademicCourseIsPublishableListableAndSearchable(t *testing.T) {
	freshCatalogPublicSchema(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, catalogPublicTestDSN)
	if err != nil {
		t.Fatalf("opening test database: %v", err)
	}
	t.Cleanup(pool.Close)

	// A legacy Course and an Academic Course, published side by side. Both models
	// must work at once, because T5 has not run.
	legacyID := seedVisibleDetailCourse(t, pool, ctx)
	academicID := seedAcademicPublishedCourse(t, pool, ctx)

	repository, err := NewRepository(pool, PublishedOnly)
	if err != nil {
		t.Fatalf("constructing repository: %v", err)
	}

	listed, err := repository.List(ctx, false, 1, 50)
	if err != nil {
		t.Fatalf("listing public courses: %v", err)
	}
	seen := map[string]bool{}
	for _, item := range listed.Items {
		seen[item.ID] = true
	}
	if !seen[legacyID] {
		t.Fatalf("legacy Course disappeared from the public catalogue")
	}
	if !seen[academicID] {
		t.Fatalf("academic Course is not visible in the public catalogue")
	}

	// The Academic Course projects with NULL legacy labels rather than blank
	// strings or an error, which is what lets T4-E present it semantically later.
	for _, item := range listed.Items {
		if item.ID != academicID {
			continue
		}
		if item.Major != nil || item.StudyYear != nil {
			t.Fatalf("academic Course carries legacy labels: major=%v year=%v", item.Major, item.StudyYear)
		}
		if item.University == nil || item.University.Label != "Kuwait University" {
			t.Fatalf("academic University projection = %#v", item.University)
		}
		if item.Subject == nil || item.Subject.Label != "Principles of Computer Systems" ||
			item.Subject.Code == nil || *item.Subject.Code != "0418-320" {
			t.Fatalf("academic Subject projection = %#v", item.Subject)
		}
	}

	// Existing `q` search must not crash on a Course with no taxonomy, and must
	// still find it by its title through the generated search_text column.
	found, err := repository.Search(ctx, false, 1, 50, "Principles of Computer Systems")
	if err != nil {
		t.Fatalf("public search over a mixed corpus failed: %v", err)
	}
	hit := false
	for _, item := range found.Items {
		if item.ID == academicID {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("academic Course is not findable by title; got %d results", found.Total)
	}
	codeHits, err := repository.Search(ctx, false, 1, 50, "0418320")
	if err != nil {
		t.Fatalf("academic Subject-code search failed: %v", err)
	}
	codeFound := false
	for _, item := range codeHits.Items {
		if item.ID == academicID {
			codeFound = true
		}
	}
	if !codeFound {
		t.Fatalf("academic Course is not findable by normalized Subject code")
	}

	// And the legacy Course is still findable by its own taxonomy labels, so the
	// joined-field half of the predicate is intact for the model that uses it.
	legacyHits, err := repository.Search(ctx, false, 1, 50, "Title")
	if err != nil {
		t.Fatalf("legacy search failed: %v", err)
	}
	legacyFound := false
	for _, item := range legacyHits.Items {
		if item.ID == legacyID {
			legacyFound = true
		}
	}
	if !legacyFound {
		t.Fatalf("legacy Course is no longer findable")
	}
}
