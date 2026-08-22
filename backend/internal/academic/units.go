package academic

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const unitColumns = `id::text, institution_id::text, parent_unit_id::text, kind, slug,
	name_ar, name_en, retired_at, created_at, updated_at`

func scanUnit(row pgx.Row) (*AcademicUnit, error) {
	var u AcademicUnit
	if err := row.Scan(&u.ID, &u.InstitutionID, &u.ParentUnitID, &u.Kind, &u.Slug,
		&u.NameAr, &u.NameEn, &u.RetiredAt, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, err
	}
	return &u, nil
}

type CreateUnitRequest struct {
	Actor         Actor
	InstitutionID string
	ParentUnitID  *string
	Kind          UnitKind
	Slug          string
	NameAr        string
	NameEn        string
}

// CreateAcademicUnit attaches a node to the Institution tree. A nil parent is
// legitimate and common: AASU has no department layer and AUM has departments
// that sit under no college.
func (r *Repository) CreateAcademicUnit(ctx context.Context, req CreateUnitRequest) (*AcademicUnit, error) {
	act := req.Actor.internal()
	if err := act.validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.InstitutionID) == "" {
		return nil, ErrNotFound
	}
	if !req.Kind.Valid() {
		return nil, ErrInvalidInput
	}
	if err := validateSlug(req.Slug); err != nil {
		return nil, err
	}
	if err := validateBilingualName(req.NameAr, req.NameEn); err != nil {
		return nil, err
	}
	parent := trimmedOrNil(req.ParentUnitID)

	var created *AcademicUnit
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		institution, err := lockInstitution(ctx, tx, req.InstitutionID)
		if err != nil {
			return err
		}
		if institution.RetiredAt != nil {
			return ErrRetired
		}
		// The composite foreign key already makes a cross-Institution parent
		// impossible. Reading it first turns that into a precise domain error
		// instead of a driver constraint message.
		if parent != nil {
			if err := assertUnitInInstitution(ctx, tx, *parent, institution.ID); err != nil {
				return err
			}
		}
		row := tx.QueryRow(ctx, `
			INSERT INTO academic_units (institution_id, parent_unit_id, kind, slug, name_ar, name_en)
			VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6)
			RETURNING `+unitColumns,
			institution.ID, parent, string(req.Kind), strings.TrimSpace(req.Slug),
			strings.TrimSpace(req.NameAr), strings.TrimSpace(req.NameEn))
		unit, err := scanUnit(row)
		if err != nil {
			return classifyConstraint(err)
		}
		created = unit
		return writeAudit(ctx, tx, auditRequest{
			Actor: act, Action: "ACADEMIC_UNIT_CREATED",
			TargetType: "ACADEMIC_UNIT", TargetID: unit.ID,
			Reason: "Academic Unit created by Admin",
			Metadata: map[string]any{
				"institution_id": unit.InstitutionID, "kind": string(unit.Kind),
				"slug": unit.Slug, "has_parent": unit.ParentUnitID != nil,
			},
		})
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

type UpdateUnitRequest struct {
	Actor  Actor
	UnitID string
	NameAr *string
	NameEn *string
	Kind   *UnitKind
	// ReparentTo is tri-state: nil leaves the parent alone, a pointer to an
	// empty string detaches the unit to the Institution root, and a pointer to
	// an identifier re-parents it.
	ReparentTo *string
}

func (r *Repository) UpdateAcademicUnit(ctx context.Context, req UpdateUnitRequest) (*AcademicUnit, error) {
	act := req.Actor.internal()
	if err := act.validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.UnitID) == "" {
		return nil, ErrNotFound
	}
	if req.Kind != nil && !req.Kind.Valid() {
		return nil, ErrInvalidInput
	}
	if req.NameAr != nil && strings.TrimSpace(*req.NameAr) == "" {
		return nil, ErrInvalidInput
	}
	if req.NameEn != nil && strings.TrimSpace(*req.NameEn) == "" {
		return nil, ErrInvalidInput
	}

	var updated *AcademicUnit
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		current, err := lockUnit(ctx, tx, req.UnitID)
		if err != nil {
			return err
		}
		nameAr, nameEn, kind := current.NameAr, current.NameEn, current.Kind
		parent := current.ParentUnitID
		if req.NameAr != nil {
			nameAr = strings.TrimSpace(*req.NameAr)
		}
		if req.NameEn != nil {
			nameEn = strings.TrimSpace(*req.NameEn)
		}
		if req.Kind != nil {
			kind = *req.Kind
		}
		if req.ReparentTo != nil {
			parent = trimmedOrNil(req.ReparentTo)
			if parent != nil {
				if *parent == current.ID {
					return ErrHierarchyCycle
				}
				if err := assertUnitInInstitution(ctx, tx, *parent, current.InstitutionID); err != nil {
					return err
				}
			}
		}

		row := tx.QueryRow(ctx, `
			UPDATE academic_units
			SET name_ar = $1, name_en = $2, kind = $3, parent_unit_id = $4::uuid, updated_at = now()
			WHERE id = $5::uuid
			RETURNING `+unitColumns,
			nameAr, nameEn, string(kind), parent, current.ID)
		unit, err := scanUnit(row)
		if err != nil {
			return classifyConstraint(err)
		}
		updated = unit
		return writeAudit(ctx, tx, auditRequest{
			Actor: act, Action: "ACADEMIC_UNIT_UPDATED",
			TargetType: "ACADEMIC_UNIT", TargetID: unit.ID,
			Reason: "Academic Unit updated by Admin",
			Metadata: map[string]any{
				"institution_id": unit.InstitutionID, "kind": string(unit.Kind),
				"reparented": req.ReparentTo != nil, "has_parent": unit.ParentUnitID != nil,
			},
		})
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// RetireAcademicUnit is soft and refuses while active children, Programs, or
// Subjects still hang off the unit. Academic history is never cascade-deleted.
func (r *Repository) RetireAcademicUnit(ctx context.Context, req RetireRequest) (*AcademicUnit, error) {
	act := req.Actor.internal()
	if err := act.validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.ID) == "" {
		return nil, ErrNotFound
	}
	var retired *AcademicUnit
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		current, err := lockUnit(ctx, tx, req.ID)
		if err != nil {
			return err
		}
		if current.RetiredAt != nil {
			return ErrRetired
		}
		var dependents int
		if err := tx.QueryRow(ctx, `
			SELECT
				(SELECT count(*) FROM academic_units WHERE parent_unit_id = $1::uuid AND retired_at IS NULL)
			  + (SELECT count(*) FROM programs WHERE owning_unit_id = $1::uuid AND retired_at IS NULL)
			  + (SELECT count(*) FROM subjects WHERE owning_unit_id = $1::uuid AND retired_at IS NULL)
		`, current.ID).Scan(&dependents); err != nil {
			return fmt.Errorf("counting academic unit dependents: %w", err)
		}
		if dependents > 0 {
			return ErrStillReferenced
		}
		row := tx.QueryRow(ctx, `
			UPDATE academic_units SET retired_at = now(), updated_at = now()
			WHERE id = $1::uuid RETURNING `+unitColumns, current.ID)
		unit, err := scanUnit(row)
		if err != nil {
			return classifyConstraint(err)
		}
		retired = unit
		return writeAudit(ctx, tx, auditRequest{
			Actor: act, Action: "ACADEMIC_UNIT_RETIRED",
			TargetType: "ACADEMIC_UNIT", TargetID: unit.ID,
			Reason:   "Academic Unit retired by Admin",
			Metadata: map[string]any{"institution_id": unit.InstitutionID, "slug": unit.Slug},
		})
	})
	if err != nil {
		return nil, err
	}
	return retired, nil
}

func lockUnit(ctx context.Context, tx pgx.Tx, id string) (*AcademicUnit, error) {
	row := tx.QueryRow(ctx, `SELECT `+unitColumns+` FROM academic_units WHERE id = $1::uuid FOR UPDATE`, id)
	unit, err := scanUnit(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, classifyConstraint(err)
	}
	return unit, nil
}

// assertUnitInInstitution turns the composite-foreign-key guarantee into an
// explicit, testable domain check. The database still refuses independently.
func assertUnitInInstitution(ctx context.Context, tx pgx.Tx, unitID, institutionID string) error {
	var owner string
	var retiredAt *string
	err := tx.QueryRow(ctx, `
		SELECT institution_id::text, retired_at::text FROM academic_units WHERE id = $1::uuid
	`, unitID).Scan(&owner, &retiredAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return classifyConstraint(err)
	}
	if owner != institutionID {
		return ErrCrossInstitution
	}
	if retiredAt != nil {
		return ErrRetired
	}
	return nil
}

func (r *Repository) ListAcademicUnits(ctx context.Context, institutionID string, includeRetired bool) ([]AcademicUnit, error) {
	if strings.TrimSpace(institutionID) == "" {
		return nil, ErrNotFound
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+unitColumns+` FROM academic_units
		WHERE institution_id = $1::uuid AND ($2::bool OR retired_at IS NULL)
		ORDER BY kind ASC, name_en ASC`, institutionID, includeRetired)
	if err != nil {
		return nil, classifyConstraint(fmt.Errorf("listing academic units: %w", err))
	}
	defer rows.Close()

	units := []AcademicUnit{}
	for rows.Next() {
		var u AcademicUnit
		if err := rows.Scan(&u.ID, &u.InstitutionID, &u.ParentUnitID, &u.Kind, &u.Slug,
			&u.NameAr, &u.NameEn, &u.RetiredAt, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning academic unit: %w", err)
		}
		units = append(units, u)
	}
	return units, rows.Err()
}
