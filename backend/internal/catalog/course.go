package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type CourseLifecycle string

const (
	LifecycleDraft            CourseLifecycle = "DRAFT"
	LifecyclePendingReview    CourseLifecycle = "PENDING_REVIEW"
	LifecycleChangesRequested CourseLifecycle = "CHANGES_REQUESTED"
	LifecyclePublished        CourseLifecycle = "PUBLISHED"
	LifecycleDelisted         CourseLifecycle = "DELISTED"
	LifecycleArchived         CourseLifecycle = "ARCHIVED"
)

func (l CourseLifecycle) Valid() bool {
	switch l {
	case LifecycleDraft, LifecyclePendingReview, LifecycleChangesRequested, LifecyclePublished, LifecycleDelisted, LifecycleArchived:
		return true
	default:
		return false
	}
}

var (
	ErrAccountSuspended = errors.New("instructor account is suspended")
	ErrOwnerIneligible  = errors.New("course owner is not an active instructor")
	ErrInvalidLifecycle = errors.New("invalid course lifecycle transition")
	ErrCourseHasAccess  = errors.New("course has existing access records; archive it instead")
	ErrPendingCandidate = errors.New("course has an active candidate revision; resolve it before owner reassignment")
)

type Course struct {
	ID             string          `json:"id"`
	OwnerAccountID string          `json:"owner_account_id"`
	Lifecycle      CourseLifecycle `json:"lifecycle"`
	LiveRevisionID *string         `json:"live_revision_id,omitempty"`

	// D-093 academic identity. ClassificationModel names which authority owns
	// this Course's academic identity; InstitutionID and SubjectID are populated
	// only under ACADEMIC_CATALOG.
	ClassificationModel    ClassificationModel       `json:"classification_model"`
	InstitutionID          *string                   `json:"institution_id,omitempty"`
	SubjectID              *string                   `json:"subject_id,omitempty"`
	AcademicContext        *AcademicCourseProjection `json:"academic_context,omitempty"`
	AccessSuspendedAt      *time.Time                `json:"access_suspended_at,omitempty"`
	AccessSuspensionReason *string                   `json:"access_suspension_reason,omitempty"`
	RetiredAt              *time.Time                `json:"retired_at,omitempty"`
	CreatedAt              time.Time                 `json:"created_at"`
	UpdatedAt              time.Time                 `json:"updated_at"`
	EditableRevision       *CourseRevision           `json:"editable_revision,omitempty"`
	LiveRevision           *CourseRevision           `json:"live_revision,omitempty"`
	PriceMinorUnits        *int64                    `json:"price_minor_units,omitempty"`
}

func (c *Course) ValidateInvariants() error {
	if c.ID == "" {
		return errors.New("course ID is required")
	}
	if c.OwnerAccountID == "" {
		return errors.New("course owner account ID is required")
	}
	if !c.Lifecycle.Valid() {
		return fmt.Errorf("invalid course lifecycle: %s", c.Lifecycle)
	}
	if c.Lifecycle == LifecyclePublished && (c.LiveRevisionID == nil || *c.LiveRevisionID == "") {
		return errors.New("published course must have live_revision_id set")
	}
	if !c.ClassificationModel.Valid() {
		return fmt.Errorf("invalid course classification model: %s", c.ClassificationModel)
	}
	// The two halves of D-093 §2 and §3, restated in the domain so an in-memory
	// Course cannot describe a state the database would refuse.
	if c.ClassificationModel == ClassificationAcademicCatalog && (c.InstitutionID == nil || *c.InstitutionID == "") {
		return errors.New("academic catalog course must have institution_id set")
	}
	if c.ClassificationModel == ClassificationLegacyTaxonomy && (c.InstitutionID != nil || c.SubjectID != nil) {
		return errors.New("legacy taxonomy course must not carry academic catalog identity")
	}
	return nil
}

// LifecycleMutation is the complete input required for an audited Admin lifecycle command.
// Lifecycle changes are deliberately separate from retirement and emergency suspension.
type LifecycleMutation struct {
	CourseID        string
	AdminAccountID  string
	ActorDescriptor string
	Target          CourseLifecycle
}

type ReassignCourseOwnerRequest struct {
	CourseID        string
	AdminAccountID  string
	ActorDescriptor string
	NewOwnerID      string
}

// TransitionCourseLifecycle applies only the BR-090 presentation graph. It never changes a
// revision, price, access fixture, or retirement/suspension state.
func (r *Repository) TransitionCourseLifecycle(ctx context.Context, req LifecycleMutation) (*Course, error) {
	if req.CourseID == "" {
		return nil, ErrCourseNotFound
	}
	if req.AdminAccountID == "" {
		return nil, errors.New("admin account ID is required")
	}
	if !req.Target.Valid() {
		return nil, ErrInvalidLifecycle
	}

	var result Course
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		row, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		if !allowsLifecycleTransition(CourseLifecycle(row.Lifecycle), req.Target) {
			return &LifecycleConflictError{CourseID: req.CourseID, Actual: row.Lifecycle, Expected: allowedLifecycleTargets(CourseLifecycle(row.Lifecycle))}
		}

		now := time.Now().UTC()
		if _, err := tx.Exec(ctx, `UPDATE courses SET lifecycle = $1, updated_at = $2 WHERE id = $3::uuid`, req.Target, now, req.CourseID); err != nil {
			return fmt.Errorf("updating course lifecycle: %w", err)
		}
		if err := writeAdminCourseAudit(ctx, tx, req.AdminAccountID, req.ActorDescriptor, lifecycleAuditAction(req.Target), req.CourseID, lifecycleAuditReason(req.Target), map[string]any{"from": row.Lifecycle, "to": req.Target}); err != nil {
			return err
		}
		result = courseFromRow(row)
		result.Lifecycle = req.Target
		result.UpdatedAt = now
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func allowsLifecycleTransition(from, to CourseLifecycle) bool {
	switch from {
	case LifecyclePublished:
		return to == LifecycleDelisted || to == LifecycleArchived
	case LifecycleDelisted:
		return to == LifecyclePublished || to == LifecycleArchived
	default:
		return false
	}
}

func allowedLifecycleTargets(from CourseLifecycle) []string {
	switch from {
	case LifecyclePublished:
		return []string{string(LifecycleDelisted), string(LifecycleArchived)}
	case LifecycleDelisted:
		return []string{string(LifecyclePublished), string(LifecycleArchived)}
	default:
		return []string{}
	}
}

func lifecycleAuditAction(target CourseLifecycle) string {
	switch target {
	case LifecycleDelisted:
		return "COURSE_DELISTED"
	case LifecyclePublished:
		return "COURSE_RELISTED"
	case LifecycleArchived:
		return "COURSE_ARCHIVED"
	default:
		return "COURSE_LIFECYCLE_CHANGED"
	}
}

func lifecycleAuditReason(target CourseLifecycle) string {
	switch target {
	case LifecycleDelisted:
		return "Course delisted by Admin"
	case LifecyclePublished:
		return "Course relisted by Admin"
	case LifecycleArchived:
		return "Course archived by Admin"
	default:
		return "Course lifecycle changed by Admin"
	}
}

// ReassignCourseOwner changes only the Course owner. Candidate revisions remain attached to the
// Course and are not rewritten, so reassignment can never silently attribute existing work to a new owner.
func (r *Repository) ReassignCourseOwner(ctx context.Context, req ReassignCourseOwnerRequest) (*Course, error) {
	if req.CourseID == "" || req.NewOwnerID == "" {
		return nil, ErrCourseNotFound
	}
	if req.AdminAccountID == "" {
		return nil, errors.New("admin account ID is required")
	}

	var result Course
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		row, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		if err := r.checkOwnerActive(ctx, tx, req.NewOwnerID); err != nil {
			return err
		}

		var candidateID string
		var candidateState string
		if err := tx.QueryRow(ctx, `
			SELECT id::text, state::text
			FROM course_revisions
			WHERE course_id = $1::uuid AND state IN ('DRAFT', 'CHANGES_REQUESTED', 'PENDING_REVIEW')
			ORDER BY revision_number DESC
			LIMIT 1
		`, req.CourseID).Scan(&candidateID, &candidateState); err == nil {
			return fmt.Errorf("%w: %s (%s)", ErrPendingCandidate, candidateID, candidateState)
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("reading explicit candidate during owner reassignment: %w", err)
		}

		now := time.Now().UTC()
		if _, err := tx.Exec(ctx, `UPDATE courses SET owner_account_id = $1::uuid, updated_at = $2 WHERE id = $3::uuid`, req.NewOwnerID, now, req.CourseID); err != nil {
			return fmt.Errorf("reassigning course owner: %w", err)
		}
		if err := writeAdminCourseAudit(ctx, tx, req.AdminAccountID, req.ActorDescriptor, "COURSE_OWNER_REASSIGNED", req.CourseID, "Course ownership reassigned by Admin", map[string]any{
			"previous_owner_account_id": row.OwnerAccountID,
			"new_owner_account_id":      req.NewOwnerID,
		}); err != nil {
			return err
		}
		result = courseFromRow(row)
		result.OwnerAccountID = req.NewOwnerID
		result.UpdatedAt = now
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// RetireCourse records the future-acquisition boundary only. S4 owns entitlement comparison.
func (r *Repository) RetireCourse(ctx context.Context, req LifecycleMutation) (*Course, error) {
	if req.CourseID == "" {
		return nil, ErrCourseNotFound
	}
	if req.AdminAccountID == "" {
		return nil, errors.New("admin account ID is required")
	}

	var result Course
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		row, err := r.LockCourse(ctx, tx, req.CourseID, string(LifecyclePublished), string(LifecycleDelisted))
		if err != nil {
			return err
		}
		if row.RetiredAt != nil {
			return &LifecycleConflictError{CourseID: req.CourseID, Actual: "RETIRED", Expected: []string{"not retired"}}
		}
		now := time.Now().UTC()
		if _, err := tx.Exec(ctx, `UPDATE courses SET retired_at = $1, updated_at = $1 WHERE id = $2::uuid`, now, req.CourseID); err != nil {
			return fmt.Errorf("retiring course: %w", err)
		}
		if err := writeAdminCourseAudit(ctx, tx, req.AdminAccountID, req.ActorDescriptor, "COURSE_RETIRED", req.CourseID, "Course retired by Admin", map[string]any{"retired_at": now}); err != nil {
			return err
		}
		result = courseFromRow(row)
		result.RetiredAt = &now
		result.UpdatedAt = now
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteCourse refuses to delete a Course with compatibility access records. With no such records,
// dependent revision data is removed in FK order before the Course itself.
func (r *Repository) DeleteCourse(ctx context.Context, req LifecycleMutation) error {
	if req.CourseID == "" {
		return ErrCourseNotFound
	}
	if req.AdminAccountID == "" {
		return errors.New("admin account ID is required")
	}
	return r.ExecTx(ctx, func(tx pgx.Tx) error {
		if _, err := r.LockCourse(ctx, tx, req.CourseID); err != nil {
			return err
		}
		var hasAccess bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM fake_entitlements fe
				JOIN lessons l ON l.id = fe.lesson_id
				JOIN sections s ON s.id = l.section_id
				WHERE s.course_id = $1::uuid
			)
		`, req.CourseID).Scan(&hasAccess); err != nil {
			return fmt.Errorf("checking course access records: %w", err)
		}
		if hasAccess {
			return ErrCourseHasAccess
		}

		if _, err := tx.Exec(ctx, `DELETE FROM course_price_changes WHERE course_id = $1::uuid`, req.CourseID); err != nil {
			return fmt.Errorf("deleting course price history: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM course_revisions WHERE course_id = $1::uuid`, req.CourseID); err != nil {
			return fmt.Errorf("deleting course revisions: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM course_section_identities WHERE course_id = $1::uuid`, req.CourseID); err != nil {
			return fmt.Errorf("deleting stable course identities: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM courses WHERE id = $1::uuid`, req.CourseID); err != nil {
			return fmt.Errorf("deleting course: %w", err)
		}
		return writeAdminCourseAudit(ctx, tx, req.AdminAccountID, req.ActorDescriptor, "COURSE_DELETED", req.CourseID, "Course permanently deleted by Admin with zero access records", nil)
	})
}

func writeAdminCourseAudit(ctx context.Context, tx pgx.Tx, adminID, descriptor, action, courseID, reason string, metadata map[string]any) error {
	if strings.TrimSpace(descriptor) == "" {
		descriptor = adminID
	}
	return WriteAuditEvent(ctx, tx, AuditEvent{ActorAccountID: &adminID, ActorRole: "ADMIN", ActorDescriptor: descriptor, Action: action, TargetType: "COURSE", TargetID: courseID, Reason: reason, Metadata: metadata})
}

func courseFromRow(row *CourseRow) Course {
	return Course{
		ID: row.ID, OwnerAccountID: row.OwnerAccountID, Lifecycle: CourseLifecycle(row.Lifecycle),
		LiveRevisionID:      row.LiveRevisionID,
		ClassificationModel: ClassificationModel(row.ClassificationModel),
		InstitutionID:       row.InstitutionID,
		SubjectID:           row.SubjectID,
		AccessSuspendedAt:   row.AccessSuspendedAt, AccessSuspensionReason: row.AccessSuspensionReason,
		RetiredAt: row.RetiredAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}
