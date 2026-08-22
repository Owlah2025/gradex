package academic

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const institutionColumns = `id::text, country_code, slug, name_ar, name_en,
	max_academic_level, has_foundation_stage, retired_at, created_at, updated_at`

func scanInstitution(row pgx.Row) (*Institution, error) {
	var i Institution
	if err := row.Scan(&i.ID, &i.CountryCode, &i.Slug, &i.NameAr, &i.NameEn,
		&i.MaxAcademicLevel, &i.HasFoundationStage, &i.RetiredAt, &i.CreatedAt, &i.UpdatedAt); err != nil {
		return nil, err
	}
	return &i, nil
}

type CreateInstitutionRequest struct {
	Actor              Actor
	CountryCode        string
	Slug               string
	NameAr             string
	NameEn             string
	MaxAcademicLevel   int
	HasFoundationStage bool
}

func (r *Repository) CreateInstitution(ctx context.Context, req CreateInstitutionRequest) (*Institution, error) {
	act := req.Actor.internal()
	if err := act.validate(); err != nil {
		return nil, err
	}
	country := strings.ToUpper(strings.TrimSpace(req.CountryCode))
	if len(country) != 2 {
		return nil, ErrInvalidInput
	}
	if err := validateSlug(req.Slug); err != nil {
		return nil, err
	}
	if err := validateBilingualName(req.NameAr, req.NameEn); err != nil {
		return nil, err
	}
	// A missing level bound must fail closed rather than silently assume the
	// four-year shape D-091 explicitly rejects.
	if req.MaxAcademicLevel < 1 || req.MaxAcademicLevel > 12 {
		return nil, ErrInvalidInput
	}

	var created *Institution
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			INSERT INTO institutions (country_code, slug, name_ar, name_en, max_academic_level, has_foundation_stage)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING `+institutionColumns,
			country, strings.TrimSpace(req.Slug), strings.TrimSpace(req.NameAr),
			strings.TrimSpace(req.NameEn), req.MaxAcademicLevel, req.HasFoundationStage)
		institution, err := scanInstitution(row)
		if err != nil {
			return classifyConstraint(err)
		}
		created = institution
		return writeAudit(ctx, tx, auditRequest{
			Actor: act, Action: "ACADEMIC_INSTITUTION_CREATED",
			TargetType: "ACADEMIC_INSTITUTION", TargetID: institution.ID,
			Reason: "Academic Institution created by Admin",
			Metadata: map[string]any{
				"slug": institution.Slug, "country_code": institution.CountryCode,
				"max_academic_level": institution.MaxAcademicLevel,
			},
		})
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

type UpdateInstitutionRequest struct {
	Actor              Actor
	InstitutionID      string
	NameAr             *string
	NameEn             *string
	MaxAcademicLevel   *int
	HasFoundationStage *bool
}

// UpdateInstitution changes presentation and level bounds only. Slug and
// country are identity and are not editable: renaming an institution's slug
// would silently repoint every reference that quotes it.
func (r *Repository) UpdateInstitution(ctx context.Context, req UpdateInstitutionRequest) (*Institution, error) {
	act := req.Actor.internal()
	if err := act.validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.InstitutionID) == "" {
		return nil, ErrNotFound
	}
	if req.MaxAcademicLevel != nil && (*req.MaxAcademicLevel < 1 || *req.MaxAcademicLevel > 12) {
		return nil, ErrInvalidInput
	}
	if req.NameAr != nil && strings.TrimSpace(*req.NameAr) == "" {
		return nil, ErrInvalidInput
	}
	if req.NameEn != nil && strings.TrimSpace(*req.NameEn) == "" {
		return nil, ErrInvalidInput
	}

	var updated *Institution
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		current, err := lockInstitution(ctx, tx, req.InstitutionID)
		if err != nil {
			return err
		}
		nameAr, nameEn := current.NameAr, current.NameEn
		maxLevel, foundation := current.MaxAcademicLevel, current.HasFoundationStage
		if req.NameAr != nil {
			nameAr = strings.TrimSpace(*req.NameAr)
		}
		if req.NameEn != nil {
			nameEn = strings.TrimSpace(*req.NameEn)
		}
		if req.MaxAcademicLevel != nil {
			maxLevel = *req.MaxAcademicLevel
		}
		if req.HasFoundationStage != nil {
			foundation = *req.HasFoundationStage
		}

		// Lowering the bound below a recommendation that already exists would
		// leave stored data outside its own institution's range. Refuse rather
		// than orphan the mapping.
		if maxLevel < current.MaxAcademicLevel {
			var breaching int
			if err := tx.QueryRow(ctx, `
				SELECT count(*) FROM curriculum_subjects
				WHERE institution_id = $1::uuid AND recommended_level > $2
			`, current.ID, maxLevel).Scan(&breaching); err != nil {
				return fmt.Errorf("counting curriculum level breaches: %w", err)
			}
			if breaching > 0 {
				return ErrLevelOutOfRange
			}
		}

		row := tx.QueryRow(ctx, `
			UPDATE institutions
			SET name_ar = $1, name_en = $2, max_academic_level = $3, has_foundation_stage = $4, updated_at = now()
			WHERE id = $5::uuid
			RETURNING `+institutionColumns,
			nameAr, nameEn, maxLevel, foundation, current.ID)
		institution, err := scanInstitution(row)
		if err != nil {
			return classifyConstraint(err)
		}
		updated = institution
		return writeAudit(ctx, tx, auditRequest{
			Actor: act, Action: "ACADEMIC_INSTITUTION_UPDATED",
			TargetType: "ACADEMIC_INSTITUTION", TargetID: institution.ID,
			Reason: "Academic Institution updated by Admin",
			Metadata: map[string]any{
				"slug": institution.Slug, "max_academic_level": institution.MaxAcademicLevel,
				"has_foundation_stage": institution.HasFoundationStage,
			},
		})
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

type RetireRequest struct {
	Actor Actor
	ID    string
}

// RetireInstitution is soft. Historical relationships stay resolvable; the
// institution simply leaves active selection. Nothing cascades.
func (r *Repository) RetireInstitution(ctx context.Context, req RetireRequest) (*Institution, error) {
	act := req.Actor.internal()
	if err := act.validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.ID) == "" {
		return nil, ErrNotFound
	}
	var retired *Institution
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		current, err := lockInstitution(ctx, tx, req.ID)
		if err != nil {
			return err
		}
		if current.RetiredAt != nil {
			return ErrRetired
		}
		row := tx.QueryRow(ctx, `
			UPDATE institutions SET retired_at = now(), updated_at = now()
			WHERE id = $1::uuid RETURNING `+institutionColumns, current.ID)
		institution, err := scanInstitution(row)
		if err != nil {
			return classifyConstraint(err)
		}
		retired = institution
		return writeAudit(ctx, tx, auditRequest{
			Actor: act, Action: "ACADEMIC_INSTITUTION_RETIRED",
			TargetType: "ACADEMIC_INSTITUTION", TargetID: institution.ID,
			Reason:   "Academic Institution retired by Admin",
			Metadata: map[string]any{"slug": institution.Slug},
		})
	})
	if err != nil {
		return nil, err
	}
	return retired, nil
}

func lockInstitution(ctx context.Context, tx pgx.Tx, id string) (*Institution, error) {
	row := tx.QueryRow(ctx, `SELECT `+institutionColumns+` FROM institutions WHERE id = $1::uuid FOR UPDATE`, id)
	institution, err := scanInstitution(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, classifyConstraint(err)
	}
	return institution, nil
}

// ListInstitutions returns active institutions by default. includeRetired is an
// explicit Admin choice, never a default, so a retired institution cannot drift
// back into an ordinary selection list.
func (r *Repository) ListInstitutions(ctx context.Context, includeRetired bool) ([]Institution, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+institutionColumns+` FROM institutions
		WHERE ($1::bool OR retired_at IS NULL)
		ORDER BY name_en ASC`, includeRetired)
	if err != nil {
		return nil, fmt.Errorf("listing institutions: %w", err)
	}
	defer rows.Close()

	institutions := []Institution{}
	for rows.Next() {
		var i Institution
		if err := rows.Scan(&i.ID, &i.CountryCode, &i.Slug, &i.NameAr, &i.NameEn,
			&i.MaxAcademicLevel, &i.HasFoundationStage, &i.RetiredAt, &i.CreatedAt, &i.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning institution: %w", err)
		}
		institutions = append(institutions, i)
	}
	return institutions, rows.Err()
}

func (r *Repository) GetInstitution(ctx context.Context, id string) (*Institution, error) {
	if strings.TrimSpace(id) == "" {
		return nil, ErrNotFound
	}
	row := r.pool.QueryRow(ctx, `SELECT `+institutionColumns+` FROM institutions WHERE id = $1::uuid`, id)
	institution, err := scanInstitution(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, classifyConstraint(err)
	}
	return institution, nil
}
