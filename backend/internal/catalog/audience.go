package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type AudienceMode string

const (
	AudienceAutomatic  AudienceMode = "AUTOMATIC"
	AudienceCustomized AudienceMode = "CUSTOMIZED"
)

var (
	ErrAudienceRequiresSubject = errors.New("course audience requires a canonical subject")
	ErrAudienceTargetInvalid   = errors.New("audience target is outside the subject's eligible programs")
	ErrAudienceTargetDuplicate = errors.New("audience target is duplicated")
)

// ProgramAudienceItem is the semantic Program projection shown to Instructors
// and Admin reviewers. Placement is present only when the active Curriculum
// mapping publishes it; no Course number or Student profile is consulted.
type ProgramAudienceItem struct {
	ProgramID           string `json:"program_id"`
	NameAr              string `json:"name_ar"`
	NameEn              string `json:"name_en"`
	RecommendedLevel    *int   `json:"recommended_level,omitempty"`
	RecommendedSemester *int   `json:"recommended_semester,omitempty"`
}

type RevisionAudience struct {
	Mode     AudienceMode          `json:"mode"`
	Programs []ProgramAudienceItem `json:"programs"`
}

type SetRevisionAudienceRequest struct {
	CourseID        string
	RevisionID      string
	OwnerAccountID  string
	ProgramIDs      []string
	ActorDescriptor string
}

type ResetRevisionAudienceRequest struct {
	CourseID        string
	RevisionID      string
	OwnerAccountID  string
	ActorDescriptor string
}

type audienceQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func loadRevisionAudience(ctx context.Context, q audienceQueryer, rev *CourseRevision) error {
	if rev == nil {
		return nil
	}
	var model string
	var institutionID, subjectID *string
	if err := q.QueryRow(ctx, `
		SELECT classification_model::text, institution_id::text, subject_id::text
		FROM courses WHERE id = $1::uuid`, rev.CourseID,
	).Scan(&model, &institutionID, &subjectID); err != nil {
		return fmt.Errorf("loading course audience context: %w", err)
	}
	if ClassificationModel(model) != ClassificationAcademicCatalog {
		rev.Audience = nil
		return nil
	}

	audience, err := readRevisionAudience(ctx, q, revisionAudienceContext{
		revisionID: rev.ID, institutionID: institutionID, subjectID: subjectID,
	})
	if err != nil {
		return err
	}
	rev.Audience = audience
	return nil
}

type revisionAudienceContext struct {
	revisionID    string
	institutionID *string
	subjectID     *string
}

func readRevisionAudience(ctx context.Context, q audienceQueryer, academic revisionAudienceContext) (*RevisionAudience, error) {
	audience := &RevisionAudience{Mode: AudienceAutomatic, Programs: []ProgramAudienceItem{}}
	if academic.institutionID == nil || *academic.institutionID == "" || academic.subjectID == nil || *academic.subjectID == "" {
		return audience, nil
	}

	var customized bool
	if err := q.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM course_program_targets WHERE revision_id = $1::uuid)`,
		academic.revisionID,
	).Scan(&customized); err != nil {
		return nil, fmt.Errorf("checking revision audience mode: %w", err)
	}

	var query string
	var args []any
	if customized {
		audience.Mode = AudienceCustomized
		// Explicit rows are revision history. Once approved they remain visible
		// even if the live Academic Catalog later retires a Program or changes a
		// mapping; only authoritative placement disappears with the mapping.
		query = `
			SELECT p.id::text, p.name_ar, p.name_en,
			       cs.recommended_level, cs.recommended_semester
			FROM course_program_targets target
			JOIN programs p ON p.id = target.program_id
			LEFT JOIN curricula c ON c.program_id = p.id
			  AND c.status = 'ACTIVE' AND c.retired_at IS NULL
			LEFT JOIN curriculum_subjects cs ON cs.curriculum_id = c.id
			  AND cs.subject_id = $1::uuid AND cs.institution_id = $2::uuid
			WHERE target.revision_id = $3::uuid
			  AND target.institution_id = $2::uuid`
		args = []any{*academic.subjectID, *academic.institutionID, academic.revisionID}
	} else {
		query = `
			SELECT p.id::text, p.name_ar, p.name_en,
			       cs.recommended_level, cs.recommended_semester
			FROM curriculum_subjects cs
			JOIN curricula c ON c.id = cs.curriculum_id
			JOIN programs p ON p.id = c.program_id
			WHERE cs.subject_id = $1::uuid
			  AND cs.institution_id = $2::uuid
			  AND c.status = 'ACTIVE'
			  AND c.retired_at IS NULL
			  AND p.retired_at IS NULL`
		args = []any{*academic.subjectID, *academic.institutionID}
	}
	query += ` ORDER BY p.name_en ASC`

	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("loading effective revision audience: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item ProgramAudienceItem
		if err := rows.Scan(&item.ProgramID, &item.NameAr, &item.NameEn,
			&item.RecommendedLevel, &item.RecommendedSemester); err != nil {
			return nil, fmt.Errorf("scanning effective revision audience: %w", err)
		}
		audience.Programs = append(audience.Programs, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading effective revision audience: %w", err)
	}
	return audience, nil
}

func normalizedAudienceTargets(programIDs []string) ([]string, error) {
	if len(programIDs) == 0 {
		return nil, ErrAudienceTargetInvalid
	}
	seen := make(map[string]struct{}, len(programIDs))
	result := make([]string, 0, len(programIDs))
	for _, raw := range programIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			return nil, ErrAudienceTargetInvalid
		}
		if _, exists := seen[id]; exists {
			return nil, ErrAudienceTargetDuplicate
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result, nil
}

func (r *Repository) SetRevisionAudience(
	ctx context.Context,
	req SetRevisionAudienceRequest,
) (*RevisionAudience, error) {
	targets, err := normalizedAudienceTargets(req.ProgramIDs)
	if err != nil {
		return nil, err
	}
	var audience *RevisionAudience
	err = r.ExecTx(ctx, func(tx pgx.Tx) error {
		course, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		if course.OwnerAccountID != req.OwnerAccountID {
			return ErrCourseNotFound
		}
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}
		if ClassificationModel(course.ClassificationModel) != ClassificationAcademicCatalog {
			return ErrAcademicContextRequired
		}
		if course.InstitutionID == nil || course.SubjectID == nil {
			return ErrAudienceRequiresSubject
		}
		revision, err := r.LockCandidate(ctx, tx, req.CourseID, req.RevisionID)
		if err != nil {
			return err
		}

		rows, err := tx.Query(ctx, `
			SELECT p.id::text
			FROM curriculum_subjects cs
			JOIN curricula c ON c.id = cs.curriculum_id
			JOIN programs p ON p.id = c.program_id
			WHERE cs.subject_id = $1::uuid
			  AND cs.institution_id = $2::uuid
			  AND c.status = 'ACTIVE' AND c.retired_at IS NULL
			  AND p.retired_at IS NULL
			  AND p.id = ANY($3::uuid[])
			ORDER BY p.id`, *course.SubjectID, *course.InstitutionID, targets)
		if err != nil {
			return ErrAudienceTargetInvalid
		}
		eligible := make(map[string]struct{}, len(targets))
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return fmt.Errorf("scanning eligible audience target: %w", err)
			}
			eligible[id] = struct{}{}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("reading eligible audience targets: %w", err)
		}
		if len(eligible) != len(targets) {
			return ErrAudienceTargetInvalid
		}

		if _, err := tx.Exec(ctx,
			`DELETE FROM course_program_targets WHERE revision_id = $1::uuid`, revision.ID); err != nil {
			return fmt.Errorf("clearing previous audience override: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO course_program_targets (revision_id, course_id, program_id, institution_id)
			SELECT $1::uuid, $2::uuid, target_id, $3::uuid
			FROM unnest($4::uuid[]) AS target_id`,
			revision.ID, course.ID, *course.InstitutionID, targets); err != nil {
			return fmt.Errorf("writing audience override: %w", err)
		}
		if err := writeInstructorAudit(ctx, tx, instructorAuditRequest{
			accountID: req.OwnerAccountID, actorDescriptor: req.ActorDescriptor,
			action: "COURSE_AUDIENCE_CUSTOMIZED", targetType: "COURSE_REVISION", targetID: revision.ID,
			reason:   "Instructor customized the revision audience",
			metadata: map[string]any{"course_id": course.ID, "program_count": len(targets)},
		}); err != nil {
			return err
		}
		audience, err = readRevisionAudience(ctx, tx, revisionAudienceContext{
			revisionID: revision.ID, institutionID: course.InstitutionID, subjectID: course.SubjectID,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return audience, nil
}

func (r *Repository) ResetRevisionAudience(
	ctx context.Context,
	req ResetRevisionAudienceRequest,
) (*RevisionAudience, error) {
	var audience *RevisionAudience
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		course, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		if course.OwnerAccountID != req.OwnerAccountID {
			return ErrCourseNotFound
		}
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}
		if ClassificationModel(course.ClassificationModel) != ClassificationAcademicCatalog {
			return ErrAcademicContextRequired
		}
		revision, err := r.LockCandidate(ctx, tx, req.CourseID, req.RevisionID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM course_program_targets WHERE revision_id = $1::uuid`, revision.ID); err != nil {
			return fmt.Errorf("resetting automatic audience: %w", err)
		}
		if err := writeInstructorAudit(ctx, tx, instructorAuditRequest{
			accountID: req.OwnerAccountID, actorDescriptor: req.ActorDescriptor,
			action: "COURSE_AUDIENCE_AUTOMATIC", targetType: "COURSE_REVISION", targetID: revision.ID,
			reason:   "Instructor restored automatic revision audience",
			metadata: map[string]any{"course_id": course.ID},
		}); err != nil {
			return err
		}
		audience, err = readRevisionAudience(ctx, tx, revisionAudienceContext{
			revisionID: revision.ID, institutionID: course.InstitutionID, subjectID: course.SubjectID,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return audience, nil
}

// invalidExplicitAudience reports whether any stored override has stopped being
// a subset of the Course Subject's current active Curriculum mappings. Zero
// rows is automatic mode and is always valid, including for an unmapped Subject.
func invalidExplicitAudience(
	ctx context.Context,
	tx pgx.Tx,
	revisionID, institutionID, subjectID string,
) (bool, error) {
	var invalid bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM course_program_targets target
			WHERE target.revision_id = $1::uuid
			  AND NOT EXISTS (
				SELECT 1
				FROM programs p
				JOIN curricula c ON c.program_id = p.id
				JOIN curriculum_subjects cs ON cs.curriculum_id = c.id
				WHERE p.id = target.program_id
				  AND p.institution_id = $2::uuid
				  AND p.retired_at IS NULL
				  AND c.institution_id = $2::uuid
				  AND c.status = 'ACTIVE' AND c.retired_at IS NULL
				  AND cs.institution_id = $2::uuid
				  AND cs.subject_id = $3::uuid
			  )
		)`, revisionID, institutionID, subjectID).Scan(&invalid)
	return invalid, err
}

// lockAudienceDependencies closes the approval-time race with Program
// retirement, Curriculum supersession/retirement, and Subject unmapping.
func lockAudienceDependencies(
	ctx context.Context,
	tx pgx.Tx,
	graph *CourseRevision,
	course *CourseRow,
) error {
	if graph == nil || course == nil || course.SubjectID == nil || course.InstitutionID == nil {
		return nil
	}
	rows, err := tx.Query(ctx, `
		SELECT target.program_id::text
		FROM course_program_targets target
		WHERE target.revision_id = $1::uuid
		ORDER BY target.program_id
		FOR SHARE`, graph.ID)
	if err != nil {
		return fmt.Errorf("locking audience targets: %w", err)
	}
	var programIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scanning locked audience target: %w", err)
		}
		programIDs = append(programIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reading locked audience targets: %w", err)
	}
	if len(programIDs) == 0 {
		return nil
	}

	for _, query := range []string{
		`SELECT id FROM programs WHERE id = ANY($1::uuid[]) ORDER BY id FOR SHARE`,
		`SELECT id FROM curricula WHERE program_id = ANY($1::uuid[]) AND status = 'ACTIVE' ORDER BY id FOR SHARE`,
	} {
		locked, err := tx.Query(ctx, query, programIDs)
		if err != nil {
			return fmt.Errorf("locking audience dependency: %w", err)
		}
		if err := drainLockedIDs(locked); err != nil {
			return err
		}
	}
	locked, err := tx.Query(ctx, `
		SELECT cs.id
		FROM curriculum_subjects cs
		JOIN curricula c ON c.id = cs.curriculum_id
		WHERE c.program_id = ANY($1::uuid[])
		  AND c.status = 'ACTIVE'
		  AND cs.subject_id = $2::uuid
		ORDER BY cs.id
		FOR SHARE OF cs`, programIDs, *course.SubjectID)
	if err != nil {
		return fmt.Errorf("locking audience curriculum mappings: %w", err)
	}
	return drainLockedIDs(locked)
}
