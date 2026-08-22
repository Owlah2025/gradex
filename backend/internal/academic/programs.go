package academic

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

const programColumns = `id::text, institution_id::text, owning_unit_id::text, slug,
	name_ar, name_en, degree_kind, retired_at, created_at, updated_at`

var degreeKindPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

func scanProgram(row pgx.Row) (*Program, error) {
	var p Program
	if err := row.Scan(&p.ID, &p.InstitutionID, &p.OwningUnitID, &p.Slug,
		&p.NameAr, &p.NameEn, &p.DegreeKind, &p.RetiredAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	return &p, nil
}

type CreateProgramRequest struct {
	Actor         Actor
	InstitutionID string
	OwningUnitID  *string
	Slug          string
	NameAr        string
	NameEn        string
	DegreeKind    string
}

// CreateProgram records a degree specialisation. A Program is not a Department:
// Kuwait University's Mathematics Department owns both Mathematics and
// Financial Mathematics, so the owning unit is a reference, not an identity.
func (r *Repository) CreateProgram(ctx context.Context, req CreateProgramRequest) (*Program, error) {
	act := req.Actor.internal()
	if err := act.validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.InstitutionID) == "" {
		return nil, ErrNotFound
	}
	if err := validateSlug(req.Slug); err != nil {
		return nil, err
	}
	if err := validateBilingualName(req.NameAr, req.NameEn); err != nil {
		return nil, err
	}
	degree := strings.ToUpper(strings.TrimSpace(req.DegreeKind))
	if !degreeKindPattern.MatchString(degree) || len(degree) > 40 {
		return nil, ErrInvalidInput
	}
	owningUnit := trimmedOrNil(req.OwningUnitID)

	var created *Program
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		institution, err := lockInstitution(ctx, tx, req.InstitutionID)
		if err != nil {
			return err
		}
		if institution.RetiredAt != nil {
			return ErrRetired
		}
		if owningUnit != nil {
			if err := assertUnitInInstitution(ctx, tx, *owningUnit, institution.ID); err != nil {
				return err
			}
		}
		row := tx.QueryRow(ctx, `
			INSERT INTO programs (institution_id, owning_unit_id, slug, name_ar, name_en, degree_kind)
			VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6)
			RETURNING `+programColumns,
			institution.ID, owningUnit, strings.TrimSpace(req.Slug),
			strings.TrimSpace(req.NameAr), strings.TrimSpace(req.NameEn), degree)
		program, err := scanProgram(row)
		if err != nil {
			return classifyConstraint(err)
		}
		created = program
		return writeAudit(ctx, tx, auditRequest{
			Actor: act, Action: "ACADEMIC_PROGRAM_CREATED",
			TargetType: "ACADEMIC_PROGRAM", TargetID: program.ID,
			Reason: "Academic Program created by Admin",
			Metadata: map[string]any{
				"institution_id": program.InstitutionID, "slug": program.Slug,
				"degree_kind": program.DegreeKind, "has_owning_unit": program.OwningUnitID != nil,
			},
		})
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

type UpdateProgramRequest struct {
	Actor      Actor
	ProgramID  string
	NameAr     *string
	NameEn     *string
	DegreeKind *string
	// SetOwningUnit is tri-state, matching UpdateUnitRequest.ReparentTo.
	SetOwningUnit *string
}

func (r *Repository) UpdateProgram(ctx context.Context, req UpdateProgramRequest) (*Program, error) {
	act := req.Actor.internal()
	if err := act.validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.ProgramID) == "" {
		return nil, ErrNotFound
	}
	if req.NameAr != nil && strings.TrimSpace(*req.NameAr) == "" {
		return nil, ErrInvalidInput
	}
	if req.NameEn != nil && strings.TrimSpace(*req.NameEn) == "" {
		return nil, ErrInvalidInput
	}

	var updated *Program
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		current, err := lockProgram(ctx, tx, req.ProgramID)
		if err != nil {
			return err
		}
		nameAr, nameEn, degree := current.NameAr, current.NameEn, current.DegreeKind
		owningUnit := current.OwningUnitID
		if req.NameAr != nil {
			nameAr = strings.TrimSpace(*req.NameAr)
		}
		if req.NameEn != nil {
			nameEn = strings.TrimSpace(*req.NameEn)
		}
		if req.DegreeKind != nil {
			degree = strings.ToUpper(strings.TrimSpace(*req.DegreeKind))
			if !degreeKindPattern.MatchString(degree) || len(degree) > 40 {
				return ErrInvalidInput
			}
		}
		if req.SetOwningUnit != nil {
			owningUnit = trimmedOrNil(req.SetOwningUnit)
			if owningUnit != nil {
				if err := assertUnitInInstitution(ctx, tx, *owningUnit, current.InstitutionID); err != nil {
					return err
				}
			}
		}
		row := tx.QueryRow(ctx, `
			UPDATE programs
			SET name_ar = $1, name_en = $2, degree_kind = $3, owning_unit_id = $4::uuid, updated_at = now()
			WHERE id = $5::uuid
			RETURNING `+programColumns,
			nameAr, nameEn, degree, owningUnit, current.ID)
		program, err := scanProgram(row)
		if err != nil {
			return classifyConstraint(err)
		}
		updated = program
		return writeAudit(ctx, tx, auditRequest{
			Actor: act, Action: "ACADEMIC_PROGRAM_UPDATED",
			TargetType: "ACADEMIC_PROGRAM", TargetID: program.ID,
			Reason: "Academic Program updated by Admin",
			Metadata: map[string]any{
				"institution_id": program.InstitutionID, "degree_kind": program.DegreeKind,
				"owning_unit_changed": req.SetOwningUnit != nil,
			},
		})
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// RetireProgram is soft and refuses while an active Curriculum still belongs to
// it, so a plan can never be orphaned by a retirement.
func (r *Repository) RetireProgram(ctx context.Context, req RetireRequest) (*Program, error) {
	act := req.Actor.internal()
	if err := act.validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.ID) == "" {
		return nil, ErrNotFound
	}
	var retired *Program
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		current, err := lockProgram(ctx, tx, req.ID)
		if err != nil {
			return err
		}
		if current.RetiredAt != nil {
			return ErrRetired
		}
		var liveCurricula int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM curricula WHERE program_id = $1::uuid AND retired_at IS NULL
		`, current.ID).Scan(&liveCurricula); err != nil {
			return fmt.Errorf("counting program curricula: %w", err)
		}
		if liveCurricula > 0 {
			return ErrStillReferenced
		}
		row := tx.QueryRow(ctx, `
			UPDATE programs SET retired_at = now(), updated_at = now()
			WHERE id = $1::uuid RETURNING `+programColumns, current.ID)
		program, err := scanProgram(row)
		if err != nil {
			return classifyConstraint(err)
		}
		retired = program
		return writeAudit(ctx, tx, auditRequest{
			Actor: act, Action: "ACADEMIC_PROGRAM_RETIRED",
			TargetType: "ACADEMIC_PROGRAM", TargetID: program.ID,
			Reason:   "Academic Program retired by Admin",
			Metadata: map[string]any{"institution_id": program.InstitutionID, "slug": program.Slug},
		})
	})
	if err != nil {
		return nil, err
	}
	return retired, nil
}

func lockProgram(ctx context.Context, tx pgx.Tx, id string) (*Program, error) {
	row := tx.QueryRow(ctx, `SELECT `+programColumns+` FROM programs WHERE id = $1::uuid FOR UPDATE`, id)
	program, err := scanProgram(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, classifyConstraint(err)
	}
	return program, nil
}

func (r *Repository) ListPrograms(ctx context.Context, institutionID string, includeRetired bool) ([]Program, error) {
	if strings.TrimSpace(institutionID) == "" {
		return nil, ErrNotFound
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+programColumns+` FROM programs
		WHERE institution_id = $1::uuid AND ($2::bool OR retired_at IS NULL)
		ORDER BY name_en ASC`, institutionID, includeRetired)
	if err != nil {
		return nil, classifyConstraint(fmt.Errorf("listing programs: %w", err))
	}
	defer rows.Close()

	programs := []Program{}
	for rows.Next() {
		var p Program
		if err := rows.Scan(&p.ID, &p.InstitutionID, &p.OwningUnitID, &p.Slug,
			&p.NameAr, &p.NameEn, &p.DegreeKind, &p.RetiredAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning program: %w", err)
		}
		programs = append(programs, p)
	}
	return programs, rows.Err()
}
