package catalogpublic

import (
	"context"
	"fmt"
	"strings"
)

// Public, anonymous academic option lists for the catalogue's filters (T6).
//
// This is deliberately the smallest possible surface. The Admin academic
// endpoints expose audit metadata, retired rows, and the whole hierarchy; none
// of that may reach an anonymous visitor, so this file does not reuse them. What
// a public filter needs is a stable public value and a name in both languages —
// nothing else is returned.
//
// Retired Institutions, Programs, and Subjects are excluded from every list, so
// a retired entity can never be offered for a new selection. A Course that still
// references one keeps its reference; retirement removes it from choosers, not
// from history.

// InstitutionFilterOption is one University a visitor may filter by.
type InstitutionFilterOption struct {
	// Slug is the public, shareable value that appears in the catalogue URL.
	Slug   string `json:"slug"`
	NameAr string `json:"name_ar"`
	NameEn string `json:"name_en"`
}

// ProgramFilterOption is one Program within a University.
type ProgramFilterOption struct {
	Slug   string `json:"slug"`
	NameAr string `json:"name_ar"`
	NameEn string `json:"name_en"`
	// CollegeNameAr/En are display context so two similarly named Programs are
	// distinguishable. They are never a selection step of their own.
	CollegeNameAr *string `json:"college_name_ar,omitempty"`
	CollegeNameEn *string `json:"college_name_en,omitempty"`
}

// SubjectFilterOption is one Subject a visitor may filter by.
type SubjectFilterOption struct {
	// Value is what goes in the URL. It is the Subject's official code when it
	// has one, which is the same identity authority the catalogue filters and
	// the T5 migration both match on. A Subject with no code has no public code
	// to share, so its identifier is used instead — the Student still never has
	// to type or know it, because this list is what produces the link.
	Value   string `json:"value"`
	Code    string `json:"code,omitempty"`
	TitleAr string `json:"title_ar"`
	TitleEn string `json:"title_en"`
}

// ListInstitutionFilters returns every active University.
func (r *Repository) ListInstitutionFilters(ctx context.Context) ([]InstitutionFilterOption, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT slug, name_ar, name_en FROM institutions
		WHERE retired_at IS NULL ORDER BY name_en ASC`)
	if err != nil {
		return nil, fmt.Errorf("listing public institution filters: %w", err)
	}
	defer rows.Close()
	options := []InstitutionFilterOption{}
	for rows.Next() {
		var option InstitutionFilterOption
		if err := rows.Scan(&option.Slug, &option.NameAr, &option.NameEn); err != nil {
			return nil, fmt.Errorf("scanning institution filter: %w", err)
		}
		options = append(options, option)
	}
	return options, rows.Err()
}

// ListProgramFilters returns the active Programs of one University, addressed by
// its public slug. An unknown or retired slug yields an empty list rather than an
// error: a stale shared link is an ordinary empty state, not a failure.
func (r *Repository) ListProgramFilters(
	ctx context.Context, institutionSlug string,
) ([]ProgramFilterOption, error) {
	if strings.TrimSpace(institutionSlug) == "" {
		return []ProgramFilterOption{}, nil
	}
	// The College is the owning unit's parent when the Program hangs off a
	// Department, and the owning unit itself when it hangs off a College
	// directly. Both shapes exist in the launch catalog, and reporting the
	// Department as a College would show a Student a level they never chose.
	rows, err := r.pool.Query(ctx, `
		SELECT p.slug, p.name_ar, p.name_en,
		       COALESCE(parent.name_ar, unit.name_ar),
		       COALESCE(parent.name_en, unit.name_en)
		FROM programs p
		JOIN institutions i ON i.id = p.institution_id AND i.retired_at IS NULL
		LEFT JOIN academic_units unit ON unit.id = p.owning_unit_id AND unit.retired_at IS NULL
		LEFT JOIN academic_units parent ON parent.id = unit.parent_unit_id AND parent.retired_at IS NULL
		WHERE i.slug = $1 AND p.retired_at IS NULL
		ORDER BY p.name_en ASC`, institutionSlug)
	if err != nil {
		return nil, fmt.Errorf("listing public program filters: %w", err)
	}
	defer rows.Close()
	options := []ProgramFilterOption{}
	for rows.Next() {
		var option ProgramFilterOption
		if err := rows.Scan(&option.Slug, &option.NameAr, &option.NameEn,
			&option.CollegeNameAr, &option.CollegeNameEn); err != nil {
			return nil, fmt.Errorf("scanning program filter: %w", err)
		}
		options = append(options, option)
	}
	return options, rows.Err()
}

// ListSubjectFilters returns the Subjects a visitor may filter by within one
// University, optionally narrowed to one Program.
//
// The Program branch reads the canonical curriculum mappings, which is the same
// authority the automatic-audience predicate uses. A Subject mapped into several
// of that Program's curricula still appears once: DISTINCT is applied for the
// same reason ProgramAudiencePredicate uses EXISTS.
//
// Only approved canonical Subjects are listed. A pending Subject Request is not a
// Subject and never becomes a public filter.
func (r *Repository) ListSubjectFilters(
	ctx context.Context, institutionSlug, programSlug string,
) ([]SubjectFilterOption, error) {
	if strings.TrimSpace(institutionSlug) == "" {
		return []SubjectFilterOption{}, nil
	}
	query := `
		SELECT DISTINCT s.id::text, COALESCE(s.official_code, ''), s.title_ar, s.title_en
		FROM subjects s
		JOIN institutions i ON i.id = s.institution_id AND i.retired_at IS NULL
		WHERE i.slug = $1 AND s.retired_at IS NULL
		ORDER BY s.title_en ASC`
	arguments := []any{institutionSlug}
	if strings.TrimSpace(programSlug) != "" {
		query = `
			SELECT DISTINCT s.id::text, COALESCE(s.official_code, ''), s.title_ar, s.title_en
			FROM subjects s
			JOIN institutions i ON i.id = s.institution_id AND i.retired_at IS NULL
			JOIN curriculum_subjects cs ON cs.subject_id = s.id
			JOIN curricula cu ON cu.id = cs.curriculum_id
				AND cu.retired_at IS NULL AND cu.status = 'ACTIVE'
			JOIN programs p ON p.id = cu.program_id AND p.retired_at IS NULL
			WHERE i.slug = $1 AND p.slug = $2 AND s.retired_at IS NULL
			ORDER BY s.title_en ASC`
		arguments = append(arguments, programSlug)
	}
	rows, err := r.pool.Query(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("listing public subject filters: %w", err)
	}
	defer rows.Close()
	options := []SubjectFilterOption{}
	for rows.Next() {
		var identifier, code, titleAr, titleEn string
		if err := rows.Scan(&identifier, &code, &titleAr, &titleEn); err != nil {
			return nil, fmt.Errorf("scanning subject filter: %w", err)
		}
		option := SubjectFilterOption{Value: code, Code: code, TitleAr: titleAr, TitleEn: titleEn}
		if code == "" {
			option.Value = identifier
		}
		options = append(options, option)
	}
	return options, rows.Err()
}

// ListLevelFilters returns the academic levels a study plan actually records
// for one University, optionally narrowed to one Program.
//
// Only recorded levels are offered. The Founder's manifest carries a
// recommended level for some Subjects and not others, and offering a level no
// Subject is recorded at would be inventing a study plan the university does
// not have.
func (r *Repository) ListLevelFilters(
	ctx context.Context, institutionSlug, programSlug string,
) ([]int, error) {
	if strings.TrimSpace(institutionSlug) == "" {
		return []int{}, nil
	}
	query := `
		SELECT DISTINCT cs.recommended_level
		FROM curriculum_subjects cs
		JOIN curricula cu ON cu.id = cs.curriculum_id
			AND cu.retired_at IS NULL AND cu.status = 'ACTIVE'
		JOIN programs p ON p.id = cu.program_id AND p.retired_at IS NULL
		JOIN institutions i ON i.id = cs.institution_id AND i.retired_at IS NULL
		WHERE i.slug = $1 AND cs.recommended_level IS NOT NULL`
	arguments := []any{institutionSlug}
	if strings.TrimSpace(programSlug) != "" {
		query += ` AND p.slug = $2`
		arguments = append(arguments, programSlug)
	}
	query += ` ORDER BY 1 ASC`

	rows, err := r.pool.Query(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("listing public level filters: %w", err)
	}
	defer rows.Close()
	levels := []int{}
	for rows.Next() {
		var level int
		if err := rows.Scan(&level); err != nil {
			return nil, fmt.Errorf("scanning level filter: %w", err)
		}
		levels = append(levels, level)
	}
	return levels, rows.Err()
}
