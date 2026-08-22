package academic

import (
	"context"
	"fmt"
	"strings"
)

// Instructor-facing authoring projections (D-091 §9, T4-B).
//
// An Instructor selects from the Admin-owned catalog and never creates, edits,
// retires, or maps a canonical Subject. These reads are the entire academic
// authority an Instructor holds: they are active-only, narrow, and carry no
// audit metadata and no mutation affordance.
//
// The search itself is deliberately NOT a second implementation. It reuses the
// same normalization primitives as the Admin surface — catalog_normalize_ar for
// titles, academic_normalize_code for codes — so "0418-320", "0418320" and
// "Principles of Computer Systems" resolve to one canonical Subject on both
// surfaces and cannot drift apart.

// SubjectProgramAssociation is one Program whose applicable Curriculum maps a
// Subject. It is the inferred audience, shown read-only in T4-B; persisting an
// audience override is T4-C.
type SubjectProgramAssociation struct {
	ProgramID string `json:"program_id"`
	NameAr    string `json:"name_ar"`
	NameEn    string `json:"name_en"`

	// Placement is returned ONLY where the Curriculum mapping actually carries
	// it. Kuwait University publishes a suggested study plan for Computer
	// Science and Data Science & AI but not for the other launch Programs, so
	// these stay nil there rather than being invented from a course number.
	RecommendedLevel    *int `json:"recommended_level,omitempty"`
	RecommendedSemester *int `json:"recommended_semester,omitempty"`
}

// AuthoringSubjectOption is one canonical Subject as an Instructor sees it.
type AuthoringSubjectOption struct {
	ID           string  `json:"id"`
	OfficialCode *string `json:"official_code,omitempty"`
	TitleAr      string  `json:"title_ar"`
	TitleEn      string  `json:"title_en"`

	// Descriptive academic context beneath the Subject: the owning Department
	// and its College where the catalog knows them. Never a selection step —
	// the Instructor flow is University then Subject (D-091 §9).
	UnitNameAr    *string `json:"unit_name_ar,omitempty"`
	UnitNameEn    *string `json:"unit_name_en,omitempty"`
	CollegeNameAr *string `json:"college_name_ar,omitempty"`
	CollegeNameEn *string `json:"college_name_en,omitempty"`

	// Programs is the inferred audience. An empty slice is a truthful answer —
	// a Subject with no Curriculum mapping is still a legitimate Course Subject
	// (D-091 §8) — and is never a validation failure.
	Programs []SubjectProgramAssociation `json:"programs"`
}

// SearchAuthoringSubjectsRequest scopes an Instructor Subject search.
type SearchAuthoringSubjectsRequest struct {
	InstitutionID string
	Query         string
	Limit         int
}

// SearchAuthoringSubjects returns active canonical Subjects in one Institution,
// with the academic context and inferred Program audience an Instructor needs to
// recognise the right Subject without reading an identifier.
//
// Retired Subjects never appear: a new Course may only be built on an active
// Subject. This is a convenience for the chooser, not the control — the domain
// refuses a retired Subject at assignment regardless of what any search returned.
func (r *Repository) SearchAuthoringSubjects(
	ctx context.Context, req SearchAuthoringSubjectsRequest,
) ([]AuthoringSubjectOption, error) {
	if strings.TrimSpace(req.InstitutionID) == "" {
		return nil, ErrNotFound
	}
	limit := req.Limit
	if limit <= 0 || limit > 50 {
		limit = 25
	}
	query := strings.TrimSpace(req.Query)

	// The match predicate is the same one ListSubjects uses, so the Admin and
	// Instructor surfaces cannot disagree about what a query resolves to.
	rows, err := r.pool.Query(ctx, `
		SELECT s.id::text, s.official_code, s.title_ar, s.title_en,
		       unit.name_ar, unit.name_en,
		       college.name_ar, college.name_en
		FROM subjects s
		LEFT JOIN academic_units unit
		       ON unit.id = s.owning_unit_id AND unit.retired_at IS NULL
		LEFT JOIN academic_units college
		       ON college.id = unit.parent_unit_id AND college.retired_at IS NULL
		WHERE s.institution_id = $1::uuid
		  AND s.retired_at IS NULL
		  AND (
			$2 = ''
			OR s.title_ar_normalized LIKE '%' || catalog_normalize_ar($2) || '%'
			OR s.title_en_normalized LIKE '%' || catalog_normalize_ar($2) || '%'
			OR (s.code_normalized IS NOT NULL AND academic_normalize_code($2) <> ''
				AND s.code_normalized LIKE academic_normalize_code($2) || '%')
		  )
		ORDER BY s.official_code ASC NULLS LAST, s.title_en ASC
		LIMIT $3`, req.InstitutionID, query, limit)
	if err != nil {
		return nil, classifyConstraint(fmt.Errorf("searching authoring subjects: %w", err))
	}
	defer rows.Close()

	options := []AuthoringSubjectOption{}
	ids := []string{}
	for rows.Next() {
		var o AuthoringSubjectOption
		if err := rows.Scan(&o.ID, &o.OfficialCode, &o.TitleAr, &o.TitleEn,
			&o.UnitNameAr, &o.UnitNameEn, &o.CollegeNameAr, &o.CollegeNameEn); err != nil {
			return nil, fmt.Errorf("scanning authoring subject: %w", err)
		}
		o.Programs = []SubjectProgramAssociation{}
		options = append(options, o)
		ids = append(ids, o.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(options) == 0 {
		return options, nil
	}

	associations, err := r.subjectProgramAssociations(ctx, req.InstitutionID, ids)
	if err != nil {
		return nil, err
	}
	for i := range options {
		if found, ok := associations[options[i].ID]; ok {
			options[i].Programs = found
		}
	}
	return options, nil
}

// GetAuthoringSubject returns one active Subject in the same shape as the
// search, so an authoring surface can render a Course's stored Subject without
// re-running a query the Instructor never typed.
func (r *Repository) GetAuthoringSubject(
	ctx context.Context, institutionID, subjectID string,
) (*AuthoringSubjectOption, error) {
	if strings.TrimSpace(institutionID) == "" || strings.TrimSpace(subjectID) == "" {
		return nil, ErrNotFound
	}
	var o AuthoringSubjectOption
	var retired *string
	// Retired Subjects ARE returned here, unlike in search. A published Course
	// keeps its historical Subject as identity (D-093 §7), so the authoring
	// surface must still be able to name it; what retirement prevents is new
	// selection, which the search filter and the domain both enforce.
	err := r.pool.QueryRow(ctx, `
		SELECT s.id::text, s.official_code, s.title_ar, s.title_en,
		       unit.name_ar, unit.name_en, college.name_ar, college.name_en,
		       s.retired_at::text
		FROM subjects s
		LEFT JOIN academic_units unit
		       ON unit.id = s.owning_unit_id AND unit.retired_at IS NULL
		LEFT JOIN academic_units college
		       ON college.id = unit.parent_unit_id AND college.retired_at IS NULL
		WHERE s.id = $1::uuid AND s.institution_id = $2::uuid`,
		subjectID, institutionID).Scan(&o.ID, &o.OfficialCode, &o.TitleAr, &o.TitleEn,
		&o.UnitNameAr, &o.UnitNameEn, &o.CollegeNameAr, &o.CollegeNameEn, &retired)
	if err != nil {
		return nil, classifyConstraint(fmt.Errorf("reading authoring subject: %w", err))
	}
	o.Programs = []SubjectProgramAssociation{}
	associations, err := r.subjectProgramAssociations(ctx, institutionID, []string{o.ID})
	if err != nil {
		return nil, err
	}
	if found, ok := associations[o.ID]; ok {
		o.Programs = found
	}
	return &o, nil
}

// subjectProgramAssociations resolves the inferred audience for a set of
// Subjects in one query.
//
// THE RULE (D-091 §5, §8): a Program is associated with a Subject when the
// Program's own ACTIVE, non-retired Curriculum carries a curriculum_subjects row
// for that Subject, and the Program itself is not retired. Exactly one
// Curriculum per Program is ACTIVE — the database enforces that with a partial
// unique index — so the rule is single-valued and needs no tie-breaking.
//
// Nothing else contributes. Legacy Major terms, Student profiles, Course titles,
// and an Instructor's previous choices are all deliberately absent: the
// Admin-owned Academic Catalog is the only authority for what a Subject serves.
func (r *Repository) subjectProgramAssociations(
	ctx context.Context, institutionID string, subjectIDs []string,
) (map[string][]SubjectProgramAssociation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT cs.subject_id::text, p.id::text, p.name_ar, p.name_en,
		       cs.recommended_level, cs.recommended_semester
		FROM curriculum_subjects cs
		JOIN curricula c ON c.id = cs.curriculum_id
		JOIN programs p ON p.id = c.program_id
		WHERE cs.subject_id = ANY($1::uuid[])
		  AND cs.institution_id = $2::uuid
		  AND c.status = 'ACTIVE'
		  AND c.retired_at IS NULL
		  AND p.retired_at IS NULL
		ORDER BY p.name_en ASC`, subjectIDs, institutionID)
	if err != nil {
		return nil, classifyConstraint(fmt.Errorf("resolving subject program associations: %w", err))
	}
	defer rows.Close()

	bySubject := map[string][]SubjectProgramAssociation{}
	for rows.Next() {
		var subjectID string
		var a SubjectProgramAssociation
		if err := rows.Scan(&subjectID, &a.ProgramID, &a.NameAr, &a.NameEn,
			&a.RecommendedLevel, &a.RecommendedSemester); err != nil {
			return nil, fmt.Errorf("scanning subject program association: %w", err)
		}
		bySubject[subjectID] = append(bySubject[subjectID], a)
	}
	return bySubject, rows.Err()
}
