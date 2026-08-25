//go:build integration

package legacymigrate

import (
	"testing"

	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
)

// The T5 → T6 bridge, against real PostgreSQL.
//
// This is the join between the two halves of the tranche, and it is the case
// neither half can prove alone: a Course that entered the system under the
// legacy taxonomy, migrated by the real migrator, must afterwards be findable
// through the real academic discovery filters — under its canonical Subject,
// under the Programs its Subject implies, and under the Program the legacy
// Major explicitly targeted — while keeping the identity, publication state,
// and price it already had.
func TestT5MigratedCourseBecomesDiscoverableThroughT6Filters(t *testing.T) {
	f := newFixture(t)

	// Give the canonical Subject a curriculum mapping, which is what the T6
	// automatic-audience rule reads.
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO curriculum_subjects (curriculum_id, subject_id, institution_id, requirement_kind)
		SELECT c.id, $1::uuid, $2::uuid, 'MAJOR_CORE' FROM curricula c WHERE c.program_id = $3::uuid`,
		f.subjectID, f.institutionID, f.programID); err != nil {
		t.Fatalf("mapping subject into the curriculum: %v", err)
	}

	coded := f.legacyTerm(t, "SUBJECT", "Principles", "0418-320")
	major := f.legacyTerm(t, "MAJOR", "Computer Science", "")
	course, revision := f.legacyCourseFixture(t, "Legacy Published Course", &coded, &major)
	f.publishWithCommerce(t, course, revision)

	repository, err := catalogpublic.NewRepository(f.pool, catalogpublic.PublishedOnly)
	if err != nil {
		t.Fatalf("constructing public repository: %v", err)
	}

	// Before the migration the Course is public but carries no academic
	// identity, so no academic filter can reach it. That is correct, not a bug:
	// it is exactly why T5 exists.
	before, err := repository.Browse(f.ctx, false, 1, 20, "", false,
		catalogpublic.Filters{InstitutionSlug: "kuwait-university"})
	if err != nil {
		t.Fatalf("pre-migration browse: %v", err)
	}
	if before.Total != 0 {
		t.Fatalf("a legacy Course answered an academic filter before migrating: %+v", before)
	}
	// It is nonetheless in the ordinary catalogue throughout.
	unfiltered, err := repository.Browse(f.ctx, false, 1, 20, "", false, catalogpublic.Filters{})
	if err != nil {
		t.Fatalf("pre-migration unfiltered browse: %v", err)
	}
	if unfiltered.Total != 1 {
		t.Fatalf("the legacy Course was not publicly listed before migrating: %+v", unfiltered)
	}
	commerceBefore := f.commercialSnapshot(t, course)

	migrator, err := New(f.pool)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	plan, err := migrator.Run(f.ctx, mappingFor(
		[]SubjectMapping{{TermCode: "0418-320", TermLabelEn: "Principles", SubjectCode: "0418-320"}},
		[]MajorMapping{{TermLabelEn: "Computer Science", ProgramSlugs: []string{"computer-science"}}},
	), Options{Apply: true, ActorDescriptor: "t5-t6-bridge"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if plan.Counts.Migrate != 1 {
		t.Fatalf("migrate = %d, want 1", plan.Counts.Migrate)
	}

	// --- The migrated Course is now discoverable academically ---
	for name, filters := range map[string]catalogpublic.Filters{
		"by University": {InstitutionSlug: "kuwait-university"},
		"by Subject":    {Subject: "0418-320"},
		"by Program":    {ProgramSlug: "computer-science"},
		"by all three":  {InstitutionSlug: "kuwait-university", ProgramSlug: "computer-science", Subject: "0418-320"},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := repository.Browse(f.ctx, false, 1, 20, "", false, filters)
			if err != nil {
				t.Fatalf("browse: %v", err)
			}
			if result.Total != 1 || len(result.Items) != 1 {
				t.Fatalf("migrated Course not discoverable %s: %+v", name, result)
			}
			if result.Items[0].ID != course {
				t.Fatalf("a different Course was returned: %s", result.Items[0].ID)
			}
		})
	}

	// The legacy Major became an EXPLICIT audience target, so the Course is
	// discoverable under that Program and only that Program — inference does not
	// widen it, even though its Subject is mapped into that same curriculum.
	audience, err := repository.Detail(f.ctx, course, false)
	if err != nil || audience == nil {
		t.Fatalf("detail: %v %v", audience, err)
	}
	if len(audience.ProgramAudience) != 1 || audience.ProgramAudience[0] != "Computer Science" {
		t.Fatalf("audience = %v, want exactly the explicitly targeted Program", audience.ProgramAudience)
	}
	if audience.Subject == nil || audience.Subject.Code == nil || *audience.Subject.Code != "0418-320" {
		t.Fatalf("detail did not report the canonical Subject: %+v", audience.Subject)
	}
	if audience.University == nil || audience.University.Label != "Kuwait University" {
		t.Fatalf("detail did not report the canonical Institution: %+v", audience.University)
	}

	// --- And it is the same Course it always was ---
	for key, want := range commerceBefore {
		if got := f.commercialSnapshot(t, course)[key]; got != want {
			t.Fatalf("migration changed %s: %q -> %q", key, want, got)
		}
	}
	if audience.Price == nil || audience.Price.MinorUnits != 25000 {
		t.Fatalf("price did not survive the migration: %+v", audience.Price)
	}
}
