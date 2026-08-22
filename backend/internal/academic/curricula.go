package academic

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const curriculumColumns = `id::text, program_id::text, institution_id::text, version_label,
	effective_from_year, status, retired_at, created_at, updated_at`

func scanCurriculum(row pgx.Row) (*Curriculum, error) {
	var c Curriculum
	if err := row.Scan(&c.ID, &c.ProgramID, &c.InstitutionID, &c.VersionLabel,
		&c.EffectiveFromYear, &c.Status, &c.RetiredAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}

type CreateCurriculumRequest struct {
	Actor             Actor
	ProgramID         string
	VersionLabel      string
	EffectiveFromYear *int
	// SupersedeActive makes replacing the current plan an explicit act. Without
	// it, creating a second ACTIVE curriculum is refused rather than silently
	// demoting the existing one.
	SupersedeActive bool
}

// CreateCurriculum records one versioned academic plan. Exactly one ACTIVE
// curriculum per Program is a partial unique index, so concurrent creation
// cannot produce two.
func (r *Repository) CreateCurriculum(ctx context.Context, req CreateCurriculumRequest) (*Curriculum, error) {
	act := req.Actor.internal()
	if err := act.validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.ProgramID) == "" {
		return nil, ErrNotFound
	}
	label := strings.TrimSpace(req.VersionLabel)
	if label == "" || len(label) > 40 {
		return nil, ErrInvalidInput
	}
	if req.EffectiveFromYear != nil && (*req.EffectiveFromYear < 1900 || *req.EffectiveFromYear > 2200) {
		return nil, ErrInvalidInput
	}

	var created *Curriculum
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		program, err := lockProgram(ctx, tx, req.ProgramID)
		if err != nil {
			return err
		}
		if program.RetiredAt != nil {
			return ErrRetired
		}
		if req.SupersedeActive {
			if _, err := tx.Exec(ctx, `
				UPDATE curricula SET status = 'SUPERSEDED', updated_at = now()
				WHERE program_id = $1::uuid AND status = 'ACTIVE' AND retired_at IS NULL
			`, program.ID); err != nil {
				return classifyConstraint(err)
			}
		}
		row := tx.QueryRow(ctx, `
			INSERT INTO curricula (program_id, institution_id, version_label, effective_from_year, status)
			VALUES ($1::uuid, $2::uuid, $3, $4, 'ACTIVE')
			RETURNING `+curriculumColumns,
			program.ID, program.InstitutionID, label, req.EffectiveFromYear)
		curriculum, err := scanCurriculum(row)
		if err != nil {
			return classifyConstraint(err)
		}
		created = curriculum
		return writeAudit(ctx, tx, auditRequest{
			Actor: act, Action: "ACADEMIC_CURRICULUM_CREATED",
			TargetType: "ACADEMIC_CURRICULUM", TargetID: curriculum.ID,
			Reason: "Academic Curriculum created by Admin",
			Metadata: map[string]any{
				"program_id": curriculum.ProgramID, "institution_id": curriculum.InstitutionID,
				"version_label": curriculum.VersionLabel, "superseded_previous": req.SupersedeActive,
			},
		})
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

type UpdateCurriculumRequest struct {
	Actor             Actor
	CurriculumID      string
	VersionLabel      *string
	EffectiveFromYear *int
	ClearEffectiveY   bool
}

// UpdateCurriculum edits plan metadata only. Status transitions run through
// CreateCurriculum(SupersedeActive) or RetireCurriculum, so the one-active
// invariant always has a single owner.
func (r *Repository) UpdateCurriculum(ctx context.Context, req UpdateCurriculumRequest) (*Curriculum, error) {
	act := req.Actor.internal()
	if err := act.validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.CurriculumID) == "" {
		return nil, ErrNotFound
	}
	if req.EffectiveFromYear != nil && (*req.EffectiveFromYear < 1900 || *req.EffectiveFromYear > 2200) {
		return nil, ErrInvalidInput
	}

	var updated *Curriculum
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		current, err := lockCurriculum(ctx, tx, req.CurriculumID)
		if err != nil {
			return err
		}
		label := current.VersionLabel
		if req.VersionLabel != nil {
			label = strings.TrimSpace(*req.VersionLabel)
			if label == "" || len(label) > 40 {
				return ErrInvalidInput
			}
		}
		year := current.EffectiveFromYear
		if req.ClearEffectiveY {
			year = nil
		} else if req.EffectiveFromYear != nil {
			year = req.EffectiveFromYear
		}
		row := tx.QueryRow(ctx, `
			UPDATE curricula SET version_label = $1, effective_from_year = $2, updated_at = now()
			WHERE id = $3::uuid RETURNING `+curriculumColumns, label, year, current.ID)
		curriculum, err := scanCurriculum(row)
		if err != nil {
			return classifyConstraint(err)
		}
		updated = curriculum
		return writeAudit(ctx, tx, auditRequest{
			Actor: act, Action: "ACADEMIC_CURRICULUM_UPDATED",
			TargetType: "ACADEMIC_CURRICULUM", TargetID: curriculum.ID,
			Reason: "Academic Curriculum updated by Admin",
			Metadata: map[string]any{
				"program_id": curriculum.ProgramID, "version_label": curriculum.VersionLabel,
			},
		})
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// RetireCurriculum soft-retires a plan and demotes it out of ACTIVE in the same
// transaction, so the partial unique index never sees a retired ACTIVE row.
func (r *Repository) RetireCurriculum(ctx context.Context, req RetireRequest) (*Curriculum, error) {
	act := req.Actor.internal()
	if err := act.validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.ID) == "" {
		return nil, ErrNotFound
	}
	var retired *Curriculum
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		current, err := lockCurriculum(ctx, tx, req.ID)
		if err != nil {
			return err
		}
		if current.RetiredAt != nil {
			return ErrRetired
		}
		row := tx.QueryRow(ctx, `
			UPDATE curricula SET retired_at = now(), status = 'SUPERSEDED', updated_at = now()
			WHERE id = $1::uuid RETURNING `+curriculumColumns, current.ID)
		curriculum, err := scanCurriculum(row)
		if err != nil {
			return classifyConstraint(err)
		}
		retired = curriculum
		return writeAudit(ctx, tx, auditRequest{
			Actor: act, Action: "ACADEMIC_CURRICULUM_RETIRED",
			TargetType: "ACADEMIC_CURRICULUM", TargetID: curriculum.ID,
			Reason: "Academic Curriculum retired by Admin",
			Metadata: map[string]any{
				"program_id": curriculum.ProgramID, "version_label": curriculum.VersionLabel,
			},
		})
	})
	if err != nil {
		return nil, err
	}
	return retired, nil
}

func lockCurriculum(ctx context.Context, tx pgx.Tx, id string) (*Curriculum, error) {
	row := tx.QueryRow(ctx, `SELECT `+curriculumColumns+` FROM curricula WHERE id = $1::uuid FOR UPDATE`, id)
	curriculum, err := scanCurriculum(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, classifyConstraint(err)
	}
	return curriculum, nil
}

func (r *Repository) ListCurricula(ctx context.Context, programID string, includeRetired bool) ([]Curriculum, error) {
	if strings.TrimSpace(programID) == "" {
		return nil, ErrNotFound
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+curriculumColumns+` FROM curricula
		WHERE program_id = $1::uuid AND ($2::bool OR retired_at IS NULL)
		ORDER BY status ASC, version_label DESC`, programID, includeRetired)
	if err != nil {
		return nil, classifyConstraint(fmt.Errorf("listing curricula: %w", err))
	}
	defer rows.Close()

	curricula := []Curriculum{}
	for rows.Next() {
		var c Curriculum
		if err := rows.Scan(&c.ID, &c.ProgramID, &c.InstitutionID, &c.VersionLabel,
			&c.EffectiveFromYear, &c.Status, &c.RetiredAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning curriculum: %w", err)
		}
		curricula = append(curricula, c)
	}
	return curricula, rows.Err()
}

type MapSubjectRequest struct {
	Actor               Actor
	CurriculumID        string
	SubjectID           string
	RequirementKind     RequirementKind
	RecommendedLevel    *int
	RecommendedSemester *int
	Credits             *float64
}

// MapSubjectToCurriculum is the many-to-many write that lets one canonical
// Subject serve many Programs. The composite foreign keys in 0023 make a
// cross-Institution mapping structurally impossible; the explicit check here
// only turns that into a precise error.
func (r *Repository) MapSubjectToCurriculum(ctx context.Context, req MapSubjectRequest) (*CurriculumSubject, error) {
	act := req.Actor.internal()
	if err := act.validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.CurriculumID) == "" || strings.TrimSpace(req.SubjectID) == "" {
		return nil, ErrNotFound
	}
	if !req.RequirementKind.Valid() {
		return nil, ErrInvalidInput
	}
	if req.RecommendedLevel != nil && *req.RecommendedLevel < 1 {
		return nil, ErrInvalidInput
	}
	if req.RecommendedSemester != nil && (*req.RecommendedSemester < 1 || *req.RecommendedSemester > 3) {
		return nil, ErrInvalidInput
	}
	if req.Credits != nil && (*req.Credits < 0 || *req.Credits > 30) {
		return nil, ErrInvalidInput
	}

	var mapped *CurriculumSubject
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		curriculum, err := lockCurriculum(ctx, tx, req.CurriculumID)
		if err != nil {
			return err
		}
		if curriculum.RetiredAt != nil {
			return ErrRetired
		}
		subject, err := lockSubject(ctx, tx, req.SubjectID)
		if err != nil {
			return err
		}
		if subject.InstitutionID != curriculum.InstitutionID {
			return ErrCrossInstitution
		}
		if subject.RetiredAt != nil {
			return ErrRetired
		}
		row := tx.QueryRow(ctx, `
			INSERT INTO curriculum_subjects (
				curriculum_id, subject_id, institution_id, requirement_kind,
				recommended_level, recommended_semester, credits
			) VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7)
			RETURNING id::text, curriculum_id::text, subject_id::text, institution_id::text,
				requirement_kind, recommended_level, recommended_semester, credits, created_at, updated_at`,
			curriculum.ID, subject.ID, curriculum.InstitutionID, string(req.RequirementKind),
			req.RecommendedLevel, req.RecommendedSemester, req.Credits)
		var cs CurriculumSubject
		if err := row.Scan(&cs.ID, &cs.CurriculumID, &cs.SubjectID, &cs.InstitutionID,
			&cs.RequirementKind, &cs.RecommendedLevel, &cs.RecommendedSemester,
			&cs.Credits, &cs.CreatedAt, &cs.UpdatedAt); err != nil {
			return classifyConstraint(err)
		}
		cs.SubjectOfficialCode = subject.OfficialCode
		cs.SubjectTitleAr, cs.SubjectTitleEn = subject.TitleAr, subject.TitleEn
		mapped = &cs
		return writeAudit(ctx, tx, auditRequest{
			Actor: act, Action: "ACADEMIC_CURRICULUM_SUBJECT_MAPPED",
			TargetType: "ACADEMIC_CURRICULUM", TargetID: curriculum.ID,
			Reason: "Subject mapped into Curriculum by Admin",
			Metadata: map[string]any{
				"subject_id": subject.ID, "requirement_kind": string(req.RequirementKind),
				"institution_id": curriculum.InstitutionID,
			},
		})
	})
	if err != nil {
		return nil, err
	}
	return mapped, nil
}

type UnmapSubjectRequest struct {
	Actor        Actor
	CurriculumID string
	SubjectID    string
}

// UnmapSubjectFromCurriculum removes a plan edge. The Subject itself is never
// touched: unmapping a Subject from one plan must not affect any other plan or
// destroy the canonical identity.
func (r *Repository) UnmapSubjectFromCurriculum(ctx context.Context, req UnmapSubjectRequest) error {
	act := req.Actor.internal()
	if err := act.validate(); err != nil {
		return err
	}
	if strings.TrimSpace(req.CurriculumID) == "" || strings.TrimSpace(req.SubjectID) == "" {
		return ErrNotFound
	}
	return r.ExecTx(ctx, func(tx pgx.Tx) error {
		curriculum, err := lockCurriculum(ctx, tx, req.CurriculumID)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			DELETE FROM curriculum_subjects WHERE curriculum_id = $1::uuid AND subject_id = $2::uuid
		`, curriculum.ID, req.SubjectID)
		if err != nil {
			return classifyConstraint(err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return writeAudit(ctx, tx, auditRequest{
			Actor: act, Action: "ACADEMIC_CURRICULUM_SUBJECT_UNMAPPED",
			TargetType: "ACADEMIC_CURRICULUM", TargetID: curriculum.ID,
			Reason: "Subject unmapped from Curriculum by Admin",
			Metadata: map[string]any{
				"subject_id": req.SubjectID, "institution_id": curriculum.InstitutionID,
			},
		})
	})
}

func (r *Repository) ListCurriculumSubjects(ctx context.Context, curriculumID string) ([]CurriculumSubject, error) {
	if strings.TrimSpace(curriculumID) == "" {
		return nil, ErrNotFound
	}
	rows, err := r.pool.Query(ctx, `
		SELECT cs.id::text, cs.curriculum_id::text, cs.subject_id::text, cs.institution_id::text,
			cs.requirement_kind, cs.recommended_level, cs.recommended_semester, cs.credits,
			cs.created_at, cs.updated_at, s.official_code, s.title_ar, s.title_en
		FROM curriculum_subjects cs
		JOIN subjects s ON s.id = cs.subject_id
		WHERE cs.curriculum_id = $1::uuid
		ORDER BY cs.recommended_level ASC NULLS LAST, s.official_code ASC NULLS LAST, s.title_en ASC`,
		curriculumID)
	if err != nil {
		return nil, classifyConstraint(fmt.Errorf("listing curriculum subjects: %w", err))
	}
	defer rows.Close()

	mappings := []CurriculumSubject{}
	for rows.Next() {
		var cs CurriculumSubject
		if err := rows.Scan(&cs.ID, &cs.CurriculumID, &cs.SubjectID, &cs.InstitutionID,
			&cs.RequirementKind, &cs.RecommendedLevel, &cs.RecommendedSemester, &cs.Credits,
			&cs.CreatedAt, &cs.UpdatedAt, &cs.SubjectOfficialCode, &cs.SubjectTitleAr, &cs.SubjectTitleEn); err != nil {
			return nil, fmt.Errorf("scanning curriculum subject: %w", err)
		}
		mappings = append(mappings, cs)
	}
	return mappings, rows.Err()
}
