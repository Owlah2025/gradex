package academic

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const subjectColumns = `id::text, institution_id::text, owning_unit_id::text, official_code,
	title_ar, title_en, retired_at, created_at, updated_at`

func scanSubject(row pgx.Row) (*Subject, error) {
	var s Subject
	if err := row.Scan(&s.ID, &s.InstitutionID, &s.OwningUnitID, &s.OfficialCode,
		&s.TitleAr, &s.TitleEn, &s.RetiredAt, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	return &s, nil
}

type CreateSubjectRequest struct {
	Actor         Actor
	InstitutionID string
	OwningUnitID  *string
	OfficialCode  *string
	TitleAr       string
	TitleEn       string
}

// CreateSubject records one canonical Institution-owned academic identity.
//
// Duplicate refusal is database-backed, not application-backed: the partial
// unique indexes on (institution_id, code_normalized) and, for code-less
// Subjects, on each normalized title, make a concurrent duplicate impossible.
// The lookup below exists only to attach the existing Subject to the error so
// the Admin surface can name it; it is never the control.
func (r *Repository) CreateSubject(ctx context.Context, req CreateSubjectRequest) (*Subject, error) {
	command, err := validateCreateSubjectRequest(req)
	if err != nil {
		return nil, err
	}

	var created *Subject
	err = r.ExecTx(ctx, func(tx pgx.Tx) error {
		var err error
		created, err = r.createSubjectTx(ctx, tx, command)
		return err
	})
	if err != nil {
		// The conflict lookup must run on a fresh connection. PostgreSQL aborts
		// the whole transaction on a constraint violation, so querying inside
		// the failed transaction returns "current transaction is aborted" and
		// the Admin would receive an unactionable generic conflict.
		return nil, r.describeSubjectConflict(ctx, err, req.InstitutionID, command.code, req.TitleAr, req.TitleEn)
	}
	return created, nil
}

type validatedSubjectCreate struct {
	actor      actor
	request    CreateSubjectRequest
	code       *string
	owningUnit *string
}

func validateCreateSubjectRequest(req CreateSubjectRequest) (validatedSubjectCreate, error) {
	act := req.Actor.internal()
	if err := act.validate(); err != nil {
		return validatedSubjectCreate{}, err
	}
	if strings.TrimSpace(req.InstitutionID) == "" {
		return validatedSubjectCreate{}, ErrNotFound
	}
	if err := validateBilingualName(req.TitleAr, req.TitleEn); err != nil {
		return validatedSubjectCreate{}, err
	}
	code := trimmedOrNil(req.OfficialCode)
	if code != nil && (len(*code) > 40 || NormalizeCode(*code) == "") {
		return validatedSubjectCreate{}, ErrInvalidInput
	}
	return validatedSubjectCreate{
		actor: act, request: req, code: code, owningUnit: trimmedOrNil(req.OwningUnitID),
	}, nil
}

// createSubjectTx is the single canonical Subject creation command. The
// ordinary Admin API and Subject-request approval both call it, so dedupe,
// Institution checks, code identity, and audit cannot drift between paths.
func (r *Repository) createSubjectTx(
	ctx context.Context,
	tx pgx.Tx,
	command validatedSubjectCreate,
) (*Subject, error) {
	institution, err := lockInstitution(ctx, tx, command.request.InstitutionID)
	if err != nil {
		return nil, err
	}
	if institution.RetiredAt != nil {
		return nil, ErrRetired
	}
	if command.owningUnit != nil {
		if err := assertUnitInInstitution(ctx, tx, *command.owningUnit, institution.ID); err != nil {
			return nil, err
		}
	}
	row := tx.QueryRow(ctx, `
		INSERT INTO subjects (institution_id, owning_unit_id, official_code, title_ar, title_en)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5)
		RETURNING `+subjectColumns,
		institution.ID, command.owningUnit, command.code,
		strings.TrimSpace(command.request.TitleAr), strings.TrimSpace(command.request.TitleEn))
	subject, err := scanSubject(row)
	if err != nil {
		return nil, classifyConstraint(err)
	}
	if err := writeAudit(ctx, tx, auditRequest{
		Actor: command.actor, Action: "ACADEMIC_SUBJECT_CREATED",
		TargetType: "ACADEMIC_SUBJECT", TargetID: subject.ID,
		Reason: "Academic Subject created by Admin",
		Metadata: map[string]any{
			"institution_id": subject.InstitutionID,
			"has_code":       subject.OfficialCode != nil,
			"code":           codeOrEmpty(subject.OfficialCode),
		},
	}); err != nil {
		return nil, err
	}
	return subject, nil
}

// describeSubjectConflict upgrades a bare duplicate error into one that names
// the existing Subject, so the Admin surface can offer it instead of failing
// opaquely. Any lookup failure degrades to the plain error rather than masking
// the original refusal.
func (r *Repository) describeSubjectConflict(
	ctx context.Context, err error, institutionID string, code *string, titleAr, titleEn string,
) error {
	if !errors.Is(err, ErrDuplicateSubject) {
		return err
	}
	var duplicate *DuplicateSubjectError
	if errors.As(err, &duplicate) {
		return err
	}
	existing, lookupErr := r.findConflictingSubject(ctx, institutionID, code, titleAr, titleEn)
	if lookupErr != nil || existing == nil {
		return err
	}
	return &DuplicateSubjectError{Existing: existing}
}

func codeOrEmpty(code *string) string {
	if code == nil {
		return ""
	}
	return *code
}

// findConflictingSubject reproduces the index predicates so the reported
// conflict is the same row the database refused against. It runs on the pool
// because the transaction that hit the constraint is already aborted.
func (r *Repository) findConflictingSubject(
	ctx context.Context, institutionID string, code *string, titleAr, titleEn string,
) (*Subject, error) {
	if code != nil {
		// Deliberately NOT filtered to live rows. Under D-093 7 an official
		// code stays reserved for its Subject after retirement, so the row that
		// refused the write is frequently a retired one. Reporting it is what
		// lets an Admin see that the code is taken and by which Subject, rather
		// than being told only that a duplicate exists.
		row := r.pool.QueryRow(ctx, `
			SELECT `+subjectColumns+` FROM subjects
			WHERE institution_id = $1::uuid AND code_normalized = academic_normalize_code($2)`,
			institutionID, *code)
		subject, err := scanSubject(row)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, nil
			}
			return nil, err
		}
		return subject, nil
	}
	row := r.pool.QueryRow(ctx, `
		SELECT `+subjectColumns+` FROM subjects
		WHERE institution_id = $1::uuid AND code_normalized IS NULL AND retired_at IS NULL
		  AND (title_ar_normalized = catalog_normalize_ar($2) OR title_en_normalized = catalog_normalize_ar($3))
		LIMIT 1`, institutionID, titleAr, titleEn)
	subject, err := scanSubject(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return subject, nil
}

type UpdateSubjectRequest struct {
	Actor        Actor
	SubjectID    string
	TitleAr      *string
	TitleEn      *string
	OfficialCode *string
	ClearCode    bool
	// SetOwningUnit is tri-state, matching the other entities.
	SetOwningUnit *string
}

func (r *Repository) UpdateSubject(ctx context.Context, req UpdateSubjectRequest) (*Subject, error) {
	act := req.Actor.internal()
	if err := act.validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.SubjectID) == "" {
		return nil, ErrNotFound
	}
	if req.TitleAr != nil && strings.TrimSpace(*req.TitleAr) == "" {
		return nil, ErrInvalidInput
	}
	if req.TitleEn != nil && strings.TrimSpace(*req.TitleEn) == "" {
		return nil, ErrInvalidInput
	}

	var updated *Subject
	var conflictInstitution, conflictTitleAr, conflictTitleEn string
	var conflictCode *string
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		current, err := lockSubject(ctx, tx, req.SubjectID)
		if err != nil {
			return err
		}
		titleAr, titleEn := current.TitleAr, current.TitleEn
		code := current.OfficialCode
		owningUnit := current.OwningUnitID
		if req.TitleAr != nil {
			titleAr = strings.TrimSpace(*req.TitleAr)
		}
		if req.TitleEn != nil {
			titleEn = strings.TrimSpace(*req.TitleEn)
		}
		// D-093 7 (amended). The normalized official code is canonical Subject
		// identity. Once established it may be reformatted but never renumbered
		// and never withdrawn, because a published Course points at this Subject
		// and releasing the code would let a different Subject claim the same
		// academic identity. Genuine university renumbering needs supersession or
		// lineage semantics and is deliberately not reachable from here.
		//
		// The database enforces the same rule independently (0026), so this is
		// the semantic half rather than the only check.
		if err := assertSubjectCodeIdentityPreserved(current, req); err != nil {
			return err
		}
		if req.ClearCode {
			code = nil
		} else if trimmed := trimmedOrNil(req.OfficialCode); trimmed != nil {
			if len(*trimmed) > 40 || NormalizeCode(*trimmed) == "" {
				return ErrInvalidInput
			}
			code = trimmed
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
			UPDATE subjects
			SET title_ar = $1, title_en = $2, official_code = $3, owning_unit_id = $4::uuid, updated_at = now()
			WHERE id = $5::uuid
			RETURNING `+subjectColumns,
			titleAr, titleEn, code, owningUnit, current.ID)
		subject, err := scanSubject(row)
		if err != nil {
			conflictInstitution, conflictCode = current.InstitutionID, code
			conflictTitleAr, conflictTitleEn = titleAr, titleEn
			return classifyConstraint(err)
		}
		updated = subject
		return writeAudit(ctx, tx, auditRequest{
			Actor: act, Action: "ACADEMIC_SUBJECT_UPDATED",
			TargetType: "ACADEMIC_SUBJECT", TargetID: subject.ID,
			Reason: "Academic Subject updated by Admin",
			Metadata: map[string]any{
				"institution_id": subject.InstitutionID,
				"has_code":       subject.OfficialCode != nil,
				"code":           codeOrEmpty(subject.OfficialCode),
			},
		})
	})
	if err != nil {
		if conflictInstitution == "" {
			return nil, err
		}
		return nil, r.describeSubjectConflict(ctx, err, conflictInstitution, conflictCode, conflictTitleAr, conflictTitleEn)
	}
	return updated, nil
}

// assertSubjectCodeIdentityPreserved applies the amended D-093 7 rule.
//
//	established code -> same normalized code   reformatting, allowed
//	established code -> different normalized   renumbering, refused
//	established code -> NULL                   withdrawal, refused
//	no code, active  -> first code             allowed, subject to reservation
//	no code, retired -> first code             refused: retirement freezes identity
//
// A codeless Subject is identified by its titles, so granting it a first code is
// a correction rather than a renumbering: it establishes an identity that did not
// exist instead of replacing one that did. The uniqueness index still refuses a
// first code that collides with any reserved code, including one held by a
// retired Subject.
func assertSubjectCodeIdentityPreserved(current *Subject, req UpdateSubjectRequest) error {
	proposed := trimmedOrNil(req.OfficialCode)
	if !req.ClearCode && proposed == nil {
		// The caller is not touching the code at all.
		return nil
	}

	if current.OfficialCode == nil {
		// No identity established yet.
		if req.ClearCode {
			// Clearing an absent code is a no-op, not a violation.
			return nil
		}
		if current.RetiredAt != nil {
			return ErrSubjectCodeImmutable
		}
		return nil
	}

	if req.ClearCode {
		return ErrSubjectCodeImmutable
	}
	if NormalizeCode(*proposed) != NormalizeCode(*current.OfficialCode) {
		return ErrSubjectCodeImmutable
	}
	return nil
}

// RetireSubject is soft. A retired Subject stays resolvable for every existing
// curriculum mapping — academic history is never destroyed — and only leaves
// active selection.
func (r *Repository) RetireSubject(ctx context.Context, req RetireRequest) (*Subject, error) {
	act := req.Actor.internal()
	if err := act.validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.ID) == "" {
		return nil, ErrNotFound
	}
	var retired *Subject
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		current, err := lockSubject(ctx, tx, req.ID)
		if err != nil {
			return err
		}
		if current.RetiredAt != nil {
			return ErrRetired
		}
		var mappings int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM curriculum_subjects WHERE subject_id = $1::uuid
		`, current.ID).Scan(&mappings); err != nil {
			return fmt.Errorf("counting subject mappings: %w", err)
		}
		row := tx.QueryRow(ctx, `
			UPDATE subjects SET retired_at = now(), updated_at = now()
			WHERE id = $1::uuid RETURNING `+subjectColumns, current.ID)
		subject, err := scanSubject(row)
		if err != nil {
			return classifyConstraint(err)
		}
		retired = subject
		return writeAudit(ctx, tx, auditRequest{
			Actor: act, Action: "ACADEMIC_SUBJECT_RETIRED",
			TargetType: "ACADEMIC_SUBJECT", TargetID: subject.ID,
			Reason: "Academic Subject retired by Admin",
			Metadata: map[string]any{
				"institution_id": subject.InstitutionID,
				"code":           codeOrEmpty(subject.OfficialCode),
				// Recorded so an investigator can see the retirement did not
				// silently strand plan mappings.
				"retained_curriculum_mappings": mappings,
			},
		})
	})
	if err != nil {
		return nil, err
	}
	return retired, nil
}

func lockSubject(ctx context.Context, tx pgx.Tx, id string) (*Subject, error) {
	row := tx.QueryRow(ctx, `SELECT `+subjectColumns+` FROM subjects WHERE id = $1::uuid FOR UPDATE`, id)
	subject, err := scanSubject(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, classifyConstraint(err)
	}
	return subject, nil
}

type ListSubjectsRequest struct {
	InstitutionID  string
	Query          string
	IncludeRetired bool
	Limit          int
}

// ListSubjects searches by official code or either title. Matching reuses
// catalog_normalize_ar for titles and academic_normalize_code for codes, so an
// Admin can find a Subject by typing "0410-101", "0410101", or "cs 490".
func (r *Repository) ListSubjects(ctx context.Context, req ListSubjectsRequest) ([]Subject, error) {
	if strings.TrimSpace(req.InstitutionID) == "" {
		return nil, ErrNotFound
	}
	limit := req.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := strings.TrimSpace(req.Query)
	rows, err := r.pool.Query(ctx, `
		SELECT `+subjectColumns+` FROM subjects
		WHERE institution_id = $1::uuid
		  AND ($2::bool OR retired_at IS NULL)
		  AND (
			$3 = ''
			OR title_ar_normalized LIKE '%' || catalog_normalize_ar($3) || '%'
			OR title_en_normalized LIKE '%' || catalog_normalize_ar($3) || '%'
			OR (code_normalized IS NOT NULL AND academic_normalize_code($3) <> ''
				AND code_normalized LIKE academic_normalize_code($3) || '%')
		  )
		ORDER BY official_code ASC NULLS LAST, title_en ASC
		LIMIT $4`, req.InstitutionID, req.IncludeRetired, query, limit)
	if err != nil {
		return nil, classifyConstraint(fmt.Errorf("listing subjects: %w", err))
	}
	defer rows.Close()

	subjects := []Subject{}
	for rows.Next() {
		var s Subject
		if err := rows.Scan(&s.ID, &s.InstitutionID, &s.OwningUnitID, &s.OfficialCode,
			&s.TitleAr, &s.TitleEn, &s.RetiredAt, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning subject: %w", err)
		}
		subjects = append(subjects, s)
	}
	return subjects, rows.Err()
}
