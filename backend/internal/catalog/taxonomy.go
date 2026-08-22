package catalog

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrTaxonomyTermNotFound     = errors.New("taxonomy term not found")
	ErrInvalidTaxonomyTerm      = errors.New("invalid taxonomy term")
	ErrTaxonomyTermReferenced   = errors.New("taxonomy term is referenced by a course")
	ErrTaxonomyTermRetired      = errors.New("taxonomy term is already retired")
	ErrTaxonomyTermUnavailable  = errors.New("taxonomy term is unavailable for assignment")
	ErrTaxonomyTermKindMismatch = errors.New("taxonomy term kind does not match assignment")
	ErrTaxonomyRevisionInvalid  = errors.New("revision is not an allowed taxonomy override target")
)

type CreateTaxonomyTermRequest struct {
	AdminAccountID  string
	ActorDescriptor string
	Kind            TaxonomyKind
	LabelAr         string
	LabelEn         string
	AcademicCode    *string
}

type RenameTaxonomyTermRequest struct {
	TermID          string
	AdminAccountID  string
	ActorDescriptor string
	LabelAr         string
	LabelEn         string
}

type RetireTaxonomyTermRequest struct {
	TermID          string
	AdminAccountID  string
	ActorDescriptor string
}

type DeleteTaxonomyTermRequest struct {
	TermID          string
	AdminAccountID  string
	ActorDescriptor string
}

type AssignTaxonomyRequest struct {
	CourseID        string
	RevisionID      string
	AdminAccountID  string
	ActorDescriptor string
	MajorTermID     string
	SubjectTermID   string
}

type taxonomyAuditRequest struct {
	AdminAccountID  string
	ActorDescriptor string
	Action          string
	TermID          string
	Reason          string
	Metadata        map[string]any
}

// CreateTaxonomyTerm persists a new immutable taxonomy identity and its mandatory audit evidence.
func (r *Repository) CreateTaxonomyTerm(ctx context.Context, req CreateTaxonomyTermRequest) (*TaxonomyTerm, error) {
	academicCode, err := validateTaxonomyTermInput(req.Kind, req.LabelAr, req.LabelEn, req.AcademicCode)
	if err != nil {
		return nil, err
	}
	if err := validateTaxonomyAdmin(req.AdminAccountID); err != nil {
		return nil, err
	}

	var term TaxonomyTerm
	err = r.ExecTx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO taxonomy_terms (kind, label_ar, label_en, academic_code)
			VALUES ($1::taxonomy_kind, $2, $3, $4)
			RETURNING id, kind, label_ar, label_en, academic_code, retired_at, created_at, updated_at
		`, req.Kind, req.LabelAr, req.LabelEn, academicCode).Scan(
			&term.ID, &term.Kind, &term.LabelAr, &term.LabelEn, &term.AcademicCode,
			&term.RetiredAt, &term.CreatedAt, &term.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("creating taxonomy term: %w", err)
		}
		return writeTaxonomyAudit(ctx, tx, taxonomyAuditRequest{
			AdminAccountID: req.AdminAccountID, ActorDescriptor: req.ActorDescriptor,
			Action: "TAXONOMY_TERM_CREATED", TermID: term.ID, Reason: "Taxonomy term created by Admin", Metadata: map[string]any{
				"kind":          term.Kind,
				"label_ar":      term.LabelAr,
				"label_en":      term.LabelEn,
				"academic_code": term.AcademicCode,
			},
		})
	})
	if err != nil {
		return nil, err
	}
	return &term, nil
}

// RenameTaxonomyTerm changes labels only. Existing revision references continue to point at the same ID.
func (r *Repository) RenameTaxonomyTerm(ctx context.Context, req RenameTaxonomyTermRequest) (*TaxonomyTerm, error) {
	if err := validateTaxonomyMutation(req.TermID, req.AdminAccountID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.LabelAr) == "" || strings.TrimSpace(req.LabelEn) == "" {
		return nil, ErrInvalidTaxonomyTerm
	}

	var term TaxonomyTerm
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		current, err := lockTaxonomyTerm(ctx, tx, req.TermID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if _, err := tx.Exec(ctx, `
			UPDATE taxonomy_terms
			SET label_ar = $1, label_en = $2, updated_at = $3
			WHERE id = $4::uuid
		`, req.LabelAr, req.LabelEn, now, req.TermID); err != nil {
			return fmt.Errorf("renaming taxonomy term: %w", err)
		}
		term = *current
		term.LabelAr = req.LabelAr
		term.LabelEn = req.LabelEn
		term.UpdatedAt = now
		return writeTaxonomyAudit(ctx, tx, taxonomyAuditRequest{
			AdminAccountID: req.AdminAccountID, ActorDescriptor: req.ActorDescriptor,
			Action: "TAXONOMY_TERM_RENAMED", TermID: term.ID, Reason: "Taxonomy term renamed by Admin", Metadata: map[string]any{
				"previous_label_ar": current.LabelAr,
				"previous_label_en": current.LabelEn,
				"label_ar":          term.LabelAr,
				"label_en":          term.LabelEn,
			},
		})
	})
	if err != nil {
		return nil, err
	}
	return &term, nil
}

// RetireTaxonomyTerm marks a term unavailable for future assignment while preserving existing references.
func (r *Repository) RetireTaxonomyTerm(ctx context.Context, req RetireTaxonomyTermRequest) (*TaxonomyTerm, error) {
	if err := validateTaxonomyMutation(req.TermID, req.AdminAccountID); err != nil {
		return nil, err
	}

	var term TaxonomyTerm
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		current, err := lockTaxonomyTerm(ctx, tx, req.TermID)
		if err != nil {
			return err
		}
		if current.RetiredAt != nil {
			return ErrTaxonomyTermRetired
		}
		now := time.Now().UTC()
		if _, err := tx.Exec(ctx, `
			UPDATE taxonomy_terms SET retired_at = $1, updated_at = $1 WHERE id = $2::uuid
		`, now, req.TermID); err != nil {
			return fmt.Errorf("retiring taxonomy term: %w", err)
		}
		term = *current
		term.RetiredAt = &now
		term.UpdatedAt = now
		return writeTaxonomyAudit(ctx, tx, taxonomyAuditRequest{
			AdminAccountID: req.AdminAccountID, ActorDescriptor: req.ActorDescriptor,
			Action: "TAXONOMY_TERM_RETIRED", TermID: term.ID, Reason: "Taxonomy term retired by Admin", Metadata: map[string]any{
				"retired_at": now,
			},
		})
	})
	if err != nil {
		return nil, err
	}
	return &term, nil
}

// DeleteTaxonomyTerm refuses any referenced term. The term lock serializes the reference count and deletion.
func (r *Repository) DeleteTaxonomyTerm(ctx context.Context, req DeleteTaxonomyTermRequest) error {
	if err := validateTaxonomyMutation(req.TermID, req.AdminAccountID); err != nil {
		return err
	}

	return r.ExecTx(ctx, func(tx pgx.Tx) error {
		term, err := lockTaxonomyTerm(ctx, tx, req.TermID)
		if err != nil {
			return err
		}
		var references int
		if err := tx.QueryRow(ctx, `
			SELECT count(*)
			FROM course_revisions
			WHERE major_term_id = $1::uuid OR subject_term_id = $1::uuid
		`, req.TermID).Scan(&references); err != nil {
			return fmt.Errorf("counting taxonomy term references: %w", err)
		}
		if references != 0 {
			return ErrTaxonomyTermReferenced
		}
		if _, err := tx.Exec(ctx, `DELETE FROM taxonomy_terms WHERE id = $1::uuid`, req.TermID); err != nil {
			return fmt.Errorf("deleting taxonomy term: %w", err)
		}
		return writeTaxonomyAudit(ctx, tx, taxonomyAuditRequest{
			AdminAccountID: req.AdminAccountID, ActorDescriptor: req.ActorDescriptor,
			Action: "TAXONOMY_TERM_DELETED", TermID: term.ID, Reason: "Taxonomy term deleted by Admin", Metadata: map[string]any{
				"kind":     term.Kind,
				"label_ar": term.LabelAr,
				"label_en": term.LabelEn,
			},
		})
	})
}

// AssignTaxonomyToRevision applies an Admin override to one explicitly named live or candidate revision.
func (r *Repository) AssignTaxonomyToRevision(ctx context.Context, req AssignTaxonomyRequest) (*CourseRevision, error) {
	if req.CourseID == "" || req.RevisionID == "" {
		return nil, ErrCourseNotFound
	}
	if err := validateTaxonomyAdmin(req.AdminAccountID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.MajorTermID) == "" || strings.TrimSpace(req.SubjectTermID) == "" {
		return nil, ErrInvalidTaxonomyTerm
	}

	var revision *CourseRevision
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		course, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		target, err := lockExactTaxonomyRevision(ctx, tx, req.CourseID, req.RevisionID)
		if err != nil {
			return err
		}
		if !taxonomyOverrideAllowed(course, target) {
			return ErrTaxonomyRevisionInvalid
		}
		// D-093 6. The Admin per-Course classification override is a legacy
		// compatibility surface. It stays available for LEGACY_TAXONOMY Courses
		// until T5 migrates them, and is refused for Academic Courses, whose
		// Subject is corrected by the Instructor through Request Changes while
		// the Course is still eligible to change it.
		if err := rejectLegacyTaxonomyOnAcademicCourse(course, &req.MajorTermID, &req.SubjectTermID, nil); err != nil {
			return err
		}
		if err := validateTaxonomyAssignments(ctx, tx, &req.MajorTermID, &req.SubjectTermID); err != nil {
			return err
		}
		now := time.Now().UTC()
		if _, err := tx.Exec(ctx, `
			UPDATE course_revisions
			SET major_term_id = $1::uuid, subject_term_id = $2::uuid, updated_at = $3
			WHERE id = $4::uuid AND course_id = $5::uuid
		`, req.MajorTermID, req.SubjectTermID, now, req.RevisionID, req.CourseID); err != nil {
			return fmt.Errorf("overriding course taxonomy: %w", err)
		}
		if err := writeAdminTaxonomyAssignmentAudit(ctx, tx, req, target); err != nil {
			return err
		}
		revision, err = r.loadRevisionGraphByIDTx(ctx, tx, req.RevisionID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return revision, nil
}

func validateTaxonomyAssignments(ctx context.Context, tx pgx.Tx, majorTermID, subjectTermID *string) error {
	type assignment struct {
		termID string
		kind   TaxonomyKind
	}
	assignments := make([]assignment, 0, 2)
	if majorTermID != nil {
		assignments = append(assignments, assignment{termID: *majorTermID, kind: TaxonomyMajor})
	}
	if subjectTermID != nil {
		assignments = append(assignments, assignment{termID: *subjectTermID, kind: TaxonomySubject})
	}
	sort.Slice(assignments, func(i, j int) bool { return assignments[i].termID < assignments[j].termID })
	for _, assignment := range assignments {
		if strings.TrimSpace(assignment.termID) == "" {
			return ErrInvalidTaxonomyTerm
		}
		if err := lockAssignableTaxonomyTerm(ctx, tx, assignment.termID, assignment.kind); err != nil {
			return err
		}
	}
	return nil
}

func lockAssignableTaxonomyTerm(ctx context.Context, tx pgx.Tx, termID string, expectedKind TaxonomyKind) error {
	var kind TaxonomyKind
	var retired bool
	err := tx.QueryRow(ctx, `
		SELECT kind, retired_at IS NOT NULL
		FROM taxonomy_terms
		WHERE id = $1::uuid
		FOR SHARE
	`, termID).Scan(&kind, &retired)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTaxonomyTermUnavailable
	}
	if err != nil {
		return fmt.Errorf("locking taxonomy term assignment: %w", err)
	}
	if retired {
		return ErrTaxonomyTermUnavailable
	}
	if kind != expectedKind {
		return ErrTaxonomyTermKindMismatch
	}
	return nil
}

type taxonomyRevisionTarget struct {
	ID    string
	State RevisionState
}

func lockExactTaxonomyRevision(ctx context.Context, tx pgx.Tx, courseID, revisionID string) (*taxonomyRevisionTarget, error) {
	var target taxonomyRevisionTarget
	err := tx.QueryRow(ctx, `
		SELECT id, state
		FROM course_revisions
		WHERE id = $1::uuid AND course_id = $2::uuid
		FOR UPDATE
	`, revisionID, courseID).Scan(&target.ID, &target.State)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCourseNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("locking taxonomy override revision: %w", err)
	}
	return &target, nil
}

func taxonomyOverrideAllowed(course *CourseRow, target *taxonomyRevisionTarget) bool {
	if course.LiveRevisionID != nil && *course.LiveRevisionID == target.ID {
		return true
	}
	return target.State == RevisionDraft || target.State == RevisionChangesRequested || target.State == RevisionPendingReview
}

func writeAdminTaxonomyAssignmentAudit(ctx context.Context, tx pgx.Tx, req AssignTaxonomyRequest, target *taxonomyRevisionTarget) error {
	return writeAdminCourseAudit(ctx, tx, req.AdminAccountID, req.ActorDescriptor,
		"COURSE_REVISION_UPDATED", req.CourseID, "Course taxonomy overridden by Admin", map[string]any{
			"revision_id":       target.ID,
			"major_term_id":     req.MajorTermID,
			"subject_term_id":   req.SubjectTermID,
			"taxonomy_override": true,
		})
}

func validateTaxonomyTermInput(kind TaxonomyKind, labelAr, labelEn string, academicCode *string) (*string, error) {
	if !kind.Valid() || strings.TrimSpace(labelAr) == "" || strings.TrimSpace(labelEn) == "" {
		return nil, ErrInvalidTaxonomyTerm
	}
	if academicCode == nil {
		return nil, nil
	}
	if kind != TaxonomySubject {
		return nil, ErrInvalidTaxonomyTerm
	}
	code := strings.TrimSpace(*academicCode)
	if code == "" {
		return nil, ErrInvalidTaxonomyTerm
	}
	return &code, nil
}

func validateTaxonomyMutation(termID, adminAccountID string) error {
	if termID == "" {
		return ErrTaxonomyTermNotFound
	}
	return validateTaxonomyAdmin(adminAccountID)
}

func validateTaxonomyAdmin(adminAccountID string) error {
	if adminAccountID == "" {
		return errors.New("admin account ID is required")
	}
	return nil
}

func lockTaxonomyTerm(ctx context.Context, tx pgx.Tx, termID string) (*TaxonomyTerm, error) {
	var term TaxonomyTerm
	err := tx.QueryRow(ctx, `
		SELECT id, kind, label_ar, label_en, academic_code, retired_at, created_at, updated_at
		FROM taxonomy_terms
		WHERE id = $1::uuid
		FOR UPDATE
	`, termID).Scan(
		&term.ID, &term.Kind, &term.LabelAr, &term.LabelEn, &term.AcademicCode,
		&term.RetiredAt, &term.CreatedAt, &term.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTaxonomyTermNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("locking taxonomy term: %w", err)
	}
	return &term, nil
}

func writeTaxonomyAudit(ctx context.Context, tx pgx.Tx, req taxonomyAuditRequest) error {
	if strings.TrimSpace(req.ActorDescriptor) == "" {
		req.ActorDescriptor = req.AdminAccountID
	}
	return WriteAuditEvent(ctx, tx, AuditEvent{
		ActorAccountID:  &req.AdminAccountID,
		ActorRole:       "ADMIN",
		ActorDescriptor: req.ActorDescriptor,
		Action:          req.Action,
		TargetType:      "TAXONOMY_TERM",
		TargetID:        req.TermID,
		Reason:          req.Reason,
		Metadata:        req.Metadata,
	})
}
