package catalogpublic

import (
	"fmt"
	"strings"
)

// Academic discovery filtering for the public catalogue (T6).
//
// # WHAT THIS FILE IS AND IS NOT
//
// It is a read shape. Every predicate here narrows an already-visible set of
// Courses; none of them widens it. PublishedOnly remains the single publication
// authority — an academic filter is composed with it, never in place of it — and
// nothing in this file can grant access, create an Entitlement, or alter a
// Course. Discovery relevance and access authority are different concerns and
// this package only owns the first.

// Filters is the academic narrowing a public catalogue request asks for.
//
// Every field is a public, human-shareable value: an Institution slug, a Program
// slug, and a Subject reference. Deliberately no UUID is required to express a
// filter, because a Student must be able to reach a Course without knowing which
// internal row owns it.
type Filters struct {
	// InstitutionSlug narrows to one University.
	InstitutionSlug string
	// ProgramSlug narrows to the Courses that Program's Students should see.
	// See ProgramAudiencePredicate for what "should see" means.
	ProgramSlug string
	// Level is the academic level a Subject is recommended at within a study
	// plan. It is real canonical manifest data (curriculum_subjects.
	// recommended_level), never derived from a Course title or a revision
	// field, and a Subject the Founder has recorded no level for simply does
	// not match a level filter rather than being given an invented one.
	Level string
	// Subject is a canonical Subject's official code. A Subject that carries no
	// official code has no public code to filter by, so its identifier is
	// accepted here as a fallback; option endpoints hand back whichever of the
	// two the Subject actually has.
	Subject string
	// RelevantProgramSlug is a ranking input, never a filter. It carries the
	// Program a Student's own academic profile names so results relevant to
	// their studies sort first. It removes nothing from the result set.
	RelevantProgramSlug string
}

// Any reports whether any narrowing was requested.
func (f Filters) Any() bool {
	return strings.TrimSpace(f.InstitutionSlug) != "" ||
		strings.TrimSpace(f.ProgramSlug) != "" ||
		strings.TrimSpace(f.Level) != "" ||
		strings.TrimSpace(f.Subject) != ""
}

// Ranked reports whether a profile-relevance ordering was requested. It is kept
// separate from Any because a ranked response is personalised and must not be
// cached publicly, while a filtered one may be.
func (f Filters) Ranked() bool { return strings.TrimSpace(f.RelevantProgramSlug) != "" }

// InstitutionPredicate narrows to one University by public slug.
//
// A retired Institution matches nothing rather than erroring: a stale shared
// link must produce an ordinary empty catalogue, not a failure.
func InstitutionPredicate(courseAlias, parameter string) string {
	return fmt.Sprintf(`%s.institution_id = (
		SELECT id FROM institutions WHERE slug = %s AND retired_at IS NULL
	)`, courseAlias, parameter)
}

// SubjectPredicate narrows to Courses whose canonical Course-level Subject is
// the named one.
//
// Course.subject_id is stable Course identity (D-093 §4), so this is an equality
// on one column and can never duplicate a Course. Matching is on the normalized
// official code, the same identity authority T5 migrates on; the identifier
// branch exists only for a Subject that has no code at all.
func SubjectPredicate(courseAlias, parameter string) string {
	return fmt.Sprintf(`%s.subject_id = (
		SELECT id FROM subjects
		WHERE retired_at IS NULL
		  AND (
		    (code_normalized IS NOT NULL AND code_normalized = academic_normalize_code(%s))
		    OR id::text = %s
		  )
		LIMIT 1
	)`, courseAlias, parameter, parameter)
}

// ProgramAudiencePredicate is the whole T6 audience rule, in one place.
//
// A Course reaches a Program's Students one of exactly two ways, and the two are
// mutually exclusive by construction:
//
//	EXPLICIT  — the live revision carries course_program_targets rows. Those rows
//	            ARE the audience. The Course is discoverable under exactly the
//	            Programs they name and no others, even when its Subject appears
//	            in other Programs' curricula. An Instructor who narrowed the
//	            audience deliberately must not have that narrowing widened by
//	            inference.
//
//	AUTOMATIC — the live revision carries no target rows at all. Zero rows means
//	            "use the audience the Subject implies" (0025 §4), never "no
//	            audience", so the Course is discoverable under every Program
//	            whose active Curriculum maps its Subject.
//
// Both branches are EXISTS subqueries rather than joins. That is the reason a
// Subject mapped into five curricula still yields ONE row for its Course: EXISTS
// answers yes or no, it does not multiply rows the way a join to
// curriculum_subjects would.
//
// This is inference for reading only. Nothing here writes a target row, and a
// Student browsing the catalogue never mutates a Course.
func ProgramAudiencePredicate(courseAlias, revisionAlias, parameter string) string {
	program := fmt.Sprintf(`(
		SELECT id FROM programs WHERE slug = %s AND retired_at IS NULL
	)`, parameter)

	explicit := fmt.Sprintf(`EXISTS (
		SELECT 1 FROM course_program_targets cpt
		WHERE cpt.revision_id = %s.id AND cpt.program_id = %s
	)`, revisionAlias, program)

	hasAnyTarget := fmt.Sprintf(`EXISTS (
		SELECT 1 FROM course_program_targets cpt WHERE cpt.revision_id = %s.id
	)`, revisionAlias)

	inferred := fmt.Sprintf(`EXISTS (
		SELECT 1
		FROM curriculum_subjects cs
		JOIN curricula cu ON cu.id = cs.curriculum_id
		WHERE cs.subject_id = %s.subject_id
		  AND cu.program_id = %s
		  AND cu.retired_at IS NULL
		  AND cu.status = 'ACTIVE'
	)`, courseAlias, program)

	return "(" + explicit + " OR (NOT " + hasAnyTarget + " AND " + inferred + "))"
}

// RelevanceExpression orders results for a Student whose profile names a
// Program. It is deterministic and readable on purpose: four named tiers, no
// learned weights, no stored score, and no behaviour that changes what the
// Student is allowed to do.
//
// A Course that ranks last is still returned. Ranking never hides anything.
func RelevanceExpression(courseAlias, revisionAlias, parameter string) string {
	return fmt.Sprintf(`CASE
		WHEN EXISTS (
			SELECT 1 FROM course_program_targets cpt
			JOIN programs p ON p.id = cpt.program_id
			WHERE cpt.revision_id = %s.id AND p.slug = %s AND p.retired_at IS NULL
		) THEN 0
		WHEN NOT EXISTS (
			SELECT 1 FROM course_program_targets cpt WHERE cpt.revision_id = %s.id
		) AND EXISTS (
			SELECT 1 FROM curriculum_subjects cs
			JOIN curricula cu ON cu.id = cs.curriculum_id
			JOIN programs p ON p.id = cu.program_id
			WHERE cs.subject_id = %s.subject_id AND p.slug = %s
			  AND p.retired_at IS NULL AND cu.retired_at IS NULL AND cu.status = 'ACTIVE'
		) THEN 1
		WHEN %s.institution_id = (
			SELECT institution_id FROM programs WHERE slug = %s AND retired_at IS NULL
		) THEN 2
		ELSE 3
	END`,
		revisionAlias, parameter,
		revisionAlias,
		courseAlias, parameter,
		courseAlias, parameter)
}

// LevelPredicate narrows to Courses whose canonical Subject a study plan
// recommends at one academic level.
//
// A level is a fact about a Subject WITHIN a Curriculum, not about a Course, so
// this reads the curriculum mappings rather than any Course or revision column.
// When a Program is also selected the level is read from that Program's active
// Curriculum; otherwise any active Curriculum in the catalog qualifies, which
// is what lets "second-year subjects at this university" mean something before
// a Program has been chosen.
//
// EXISTS again, so a Subject recommended at the same level by several study
// plans still yields one Course row.
func LevelPredicate(courseAlias, levelParameter, programParameter string) string {
	program := ""
	if programParameter != "" {
		program = fmt.Sprintf(` AND cu.program_id = (
			SELECT id FROM programs WHERE slug = %s AND retired_at IS NULL
		)`, programParameter)
	}
	return fmt.Sprintf(`EXISTS (
		SELECT 1
		FROM curriculum_subjects cs
		JOIN curricula cu ON cu.id = cs.curriculum_id
		WHERE cs.subject_id = %s.subject_id
		  AND cs.recommended_level = %s
		  AND cu.retired_at IS NULL
		  AND cu.status = 'ACTIVE'%s
	)`, courseAlias, levelParameter, program)
}
