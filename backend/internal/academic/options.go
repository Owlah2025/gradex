package academic

import (
	"context"
	"fmt"
	"strings"
)

// Student-facing option projections (D-092, T3).
//
// These are read-only, active-only, and deliberately narrow: identifier, name,
// and the few facts a chooser needs to render itself. No audit metadata, no
// retired row, and no Admin field reaches a Student. There is no Department
// selector here on purpose — a Student chooses University, College, Program,
// and level, and the Department is derived context only.

type InstitutionOption struct {
	ID                 string `json:"id"`
	NameAr             string `json:"name_ar"`
	NameEn             string `json:"name_en"`
	CountryCode        string `json:"country_code"`
	MaxAcademicLevel   int    `json:"max_academic_level"`
	HasFoundationStage bool   `json:"has_foundation_stage"`
}

type CollegeOption struct {
	ID     string `json:"id"`
	NameAr string `json:"name_ar"`
	NameEn string `json:"name_en"`
}

type ProgramOption struct {
	ID     string `json:"id"`
	NameAr string `json:"name_ar"`
	NameEn string `json:"name_en"`
	// Department context is returned so a chooser can show it as a subtitle.
	// It is never a selection step.
	DepartmentNameAr *string `json:"department_name_ar,omitempty"`
	DepartmentNameEn *string `json:"department_name_en,omitempty"`
}

// ListInstitutionOptions returns the institutions a Student may choose. Retired
// institutions never appear.
func (r *Repository) ListInstitutionOptions(ctx context.Context) ([]InstitutionOption, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, name_ar, name_en, country_code, max_academic_level, has_foundation_stage
		FROM institutions WHERE retired_at IS NULL ORDER BY name_en ASC`)
	if err != nil {
		return nil, fmt.Errorf("listing institution options: %w", err)
	}
	defer rows.Close()
	options := []InstitutionOption{}
	for rows.Next() {
		var o InstitutionOption
		if err := rows.Scan(&o.ID, &o.NameAr, &o.NameEn, &o.CountryCode,
			&o.MaxAcademicLevel, &o.HasFoundationStage); err != nil {
			return nil, fmt.Errorf("scanning institution option: %w", err)
		}
		options = append(options, o)
	}
	return options, rows.Err()
}

// ListCollegeOptions returns the top-level Colleges of one Institution. Only
// COLLEGE units appear: a Department is never offered as a College.
func (r *Repository) ListCollegeOptions(ctx context.Context, institutionID string) ([]CollegeOption, error) {
	if strings.TrimSpace(institutionID) == "" {
		return nil, ErrNotFound
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, name_ar, name_en FROM academic_units
		WHERE institution_id = $1::uuid AND kind = 'COLLEGE' AND retired_at IS NULL
		ORDER BY name_en ASC`, institutionID)
	if err != nil {
		return nil, classifyConstraint(fmt.Errorf("listing college options: %w", err))
	}
	defer rows.Close()
	options := []CollegeOption{}
	for rows.Next() {
		var o CollegeOption
		if err := rows.Scan(&o.ID, &o.NameAr, &o.NameEn); err != nil {
			return nil, fmt.Errorf("scanning college option: %w", err)
		}
		options = append(options, o)
	}
	return options, rows.Err()
}

// ListProgramOptions returns every Program reachable beneath one College,
// walking the whole academic-unit subtree.
//
// The walk is what lets a Student pick a College and get its Programs without
// ever choosing a Department: at Kuwait University a Program hangs off a
// Department that hangs off the College, and at other institutions it may hang
// off the College directly. Both shapes resolve here.
func (r *Repository) ListProgramOptions(
	ctx context.Context, institutionID, collegeID string,
) ([]ProgramOption, error) {
	if strings.TrimSpace(institutionID) == "" || strings.TrimSpace(collegeID) == "" {
		return nil, ErrNotFound
	}
	rows, err := r.pool.Query(ctx, `
		WITH RECURSIVE subtree AS (
			SELECT id, institution_id FROM academic_units
			WHERE id = $2::uuid AND institution_id = $1::uuid AND retired_at IS NULL
			UNION ALL
			SELECT child.id, child.institution_id
			FROM academic_units child
			JOIN subtree ON child.parent_unit_id = subtree.id
			WHERE child.retired_at IS NULL
		)
		SELECT p.id::text, p.name_ar, p.name_en, unit.name_ar, unit.name_en
		FROM programs p
		JOIN subtree ON subtree.id = p.owning_unit_id
		LEFT JOIN academic_units unit ON unit.id = p.owning_unit_id
		WHERE p.institution_id = $1::uuid AND p.retired_at IS NULL
		ORDER BY p.name_en ASC`, institutionID, collegeID)
	if err != nil {
		return nil, classifyConstraint(fmt.Errorf("listing program options: %w", err))
	}
	defer rows.Close()
	options := []ProgramOption{}
	for rows.Next() {
		var o ProgramOption
		if err := rows.Scan(&o.ID, &o.NameAr, &o.NameEn, &o.DepartmentNameAr, &o.DepartmentNameEn); err != nil {
			return nil, fmt.Errorf("scanning program option: %w", err)
		}
		options = append(options, o)
	}
	return options, rows.Err()
}
