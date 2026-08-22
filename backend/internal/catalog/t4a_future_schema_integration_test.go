//go:build integration

package catalog

import (
	"testing"
)

// SCHEMA_PROVEN_ONLY.
//
// course_program_targets and subject_requests ship their schema in migration
// 0025 so that the T4 shape is designed once rather than accreting a second
// migration over tables that will by then hold data. Their BEHAVIOUR is not
// implemented and is not proven here:
//
//   - Program audience inference, customization, reset, revision cloning, and
//     the subset-of-inferred-audience rule are T4-C.
//   - The Instructor request flow, the Admin queue, and approve /
//     link-to-existing / reject resolution are T4-D.
//
// These cases prove only that the constraints refuse what they are meant to
// refuse, so that the later slices build on a shape that has been exercised.
// Nothing here should be read as evidence that T4-C or T4-D works.

func TestT4ASchemaOnlyProgramTargetConstraints(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)
	seedAcademicFixture(t, p, ctx)
	repo := t4aRepo(t, p)

	course := createAcademicCourse(t, repo, ctx, &[]string{t4aSubjectA}[0])
	revID := course.EditableRevision.ID

	if _, err := p.Exec(ctx, `
		INSERT INTO programs (id, institution_id, slug, name_ar, name_en, degree_kind) VALUES
		  ('eeee5555-0000-0000-0000-000000000001', $1, 'computer-science', 'علوم الحاسوب', 'Computer Science', 'BSC'),
		  ('eeee5555-0000-0000-0000-000000000002', $2, 'foreign-program', 'برنامج', 'Foreign Program', 'BSC')`,
		t4aInstitutionKU, t4aInstitutionAUK); err != nil {
		t.Fatalf("seeding programs: %v", err)
	}

	insert := func(program, institution string) error {
		_, err := p.Exec(ctx, `
			INSERT INTO course_program_targets (revision_id, course_id, program_id, institution_id)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid)`,
			revID, course.ID, program, institution)
		return err
	}

	// A valid target is accepted.
	if err := insert("eeee5555-0000-0000-0000-000000000001", t4aInstitutionKU); err != nil {
		t.Fatalf("valid program target refused: %v", err)
	}
	// Duplicates are unrepresentable rather than deduplicated in Go.
	if err := insert("eeee5555-0000-0000-0000-000000000001", t4aInstitutionKU); err == nil {
		t.Fatalf("duplicate (revision, program) target must be refused")
	}
	// A Program from another Institution cannot be targeted.
	if err := insert("eeee5555-0000-0000-0000-000000000002", t4aInstitutionAUK); err == nil {
		t.Fatalf("cross-institution program target must be refused")
	}
	// A dangling Program cannot be targeted.
	if err := insert("eeee5555-0000-0000-0000-00000000ffff", t4aInstitutionKU); err == nil {
		t.Fatalf("dangling program target must be refused")
	}
	// A revision that does not belong to the named Course cannot be targeted.
	other := createAcademicCourse(t, repo, ctx, nil)
	if _, err := p.Exec(ctx, `
		INSERT INTO course_program_targets (revision_id, course_id, program_id, institution_id)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid)`,
		revID, other.ID, "eeee5555-0000-0000-0000-000000000001", t4aInstitutionKU); err == nil {
		t.Fatalf("target whose revision belongs to another Course must be refused")
	}

	// Zero rows is the inferred-audience state and there is no mode column that
	// could contradict it, so an explicit empty audience is unrepresentable.
	var cols int
	if err := p.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'course_program_targets'
		  AND column_name IN ('mode', 'is_explicit', 'audience_mode', 'override_enabled')`).Scan(&cols); err != nil {
		t.Fatalf("inspecting columns: %v", err)
	}
	if cols != 0 {
		t.Fatalf("course_program_targets carries a mode column; zero rows must be the only inferred-audience representation")
	}
}

func TestT4ASchemaOnlySubjectRequestConstraints(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)
	seedAcademicFixture(t, p, ctx)
	repo := t4aRepo(t, p)
	course := createAcademicCourse(t, repo, ctx, nil)

	pending := func(courseID *string) error {
		_, err := p.Exec(ctx, `
			INSERT INTO subject_requests (requester_account_id, institution_id, course_id, proposed_title_ar, proposed_title_en)
			VALUES ($1::uuid, $2::uuid, $3::uuid, 'مادة مطلوبة', 'Requested Subject')`,
			t4aInstructor, t4aInstitutionKU, courseID)
		return err
	}

	if err := pending(&course.ID); err != nil {
		t.Fatalf("valid pending request refused: %v", err)
	}
	// One open request per Course: two would make "which resolution assigns the
	// Subject" undecidable.
	if err := pending(&course.ID); err == nil {
		t.Fatalf("second PENDING request for the same Course must be refused")
	}
	// A request without a Course is legitimate.
	if err := pending(nil); err != nil {
		t.Fatalf("course-less request refused: %v", err)
	}

	// The attached Course must be in the request's Institution.
	if _, err := p.Exec(ctx, `
		INSERT INTO subject_requests (requester_account_id, institution_id, course_id, proposed_title_ar, proposed_title_en)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'x', 'Cross Institution')`,
		t4aInstructor, t4aInstitutionAUK, course.ID); err == nil {
		t.Fatalf("request whose Institution differs from its Course must be refused")
	}

	// A rejection carries a reason.
	if _, err := p.Exec(ctx, `
		INSERT INTO subject_requests (requester_account_id, institution_id, proposed_title_ar, proposed_title_en, status, resolved_at)
		VALUES ($1::uuid, $2::uuid, 'x', 'No Reason', 'REJECTED', now())`,
		t4aInstructor, t4aInstitutionKU); err == nil {
		t.Fatalf("REJECTED without a reason must be refused")
	}

	// A resolution that names a Subject and one that does not are two halves of
	// one fact; neither can be written alone.
	if _, err := p.Exec(ctx, `
		INSERT INTO subject_requests (requester_account_id, institution_id, proposed_title_ar, proposed_title_en, status, resolved_at)
		VALUES ($1::uuid, $2::uuid, 'x', 'No Subject', 'LINKED_EXISTING', now())`,
		t4aInstructor, t4aInstitutionKU); err == nil {
		t.Fatalf("LINKED_EXISTING without a resolved Subject must be refused")
	}

	// A resolved Subject must belong to the request's Institution.
	if _, err := p.Exec(ctx, `
		INSERT INTO subject_requests (requester_account_id, institution_id, proposed_title_ar, proposed_title_en, status, resolved_subject_id, resolved_at)
		VALUES ($1::uuid, $2::uuid, 'x', 'Foreign Subject', 'LINKED_EXISTING', $3::uuid, now())`,
		t4aInstructor, t4aInstitutionKU, t4aSubjectAUK); err == nil {
		t.Fatalf("resolved Subject from another Institution must be refused")
	}
}
