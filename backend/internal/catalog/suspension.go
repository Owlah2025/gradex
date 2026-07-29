package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

type AccessSuspensionCause string

const (
	SuspensionCauseLegal            AccessSuspensionCause = "LEGAL"
	SuspensionCauseSecurity         AccessSuspensionCause = "SECURITY"
	SuspensionCauseMalware          AccessSuspensionCause = "MALWARE"
	SuspensionCauseSevereModeration AccessSuspensionCause = "SEVERE_MODERATION"
)

func (c AccessSuspensionCause) Valid() bool {
	switch c {
	case SuspensionCauseLegal, SuspensionCauseSecurity, SuspensionCauseMalware, SuspensionCauseSevereModeration:
		return true
	default:
		return false
	}
}

var ErrCourseAccessAlreadySuspended = errors.New("course access is already suspended")
var ErrCourseAccessNotSuspended = errors.New("course access is not suspended")

type SuspendCourseAccessRequest struct {
	CourseID        string
	AdminAccountID  string
	ActorDescriptor string
	Cause           AccessSuspensionCause
	Reason          string
}

type RestoreCourseAccessRequest struct {
	CourseID        string
	AdminAccountID  string
	ActorDescriptor string
	Reason          string
}

// SuspendCourseAccess is orthogonal to Course lifecycle and leaves all access records intact.
func (r *Repository) SuspendCourseAccess(ctx context.Context, req SuspendCourseAccessRequest) (*Course, error) {
	if req.CourseID == "" {
		return nil, ErrCourseNotFound
	}
	if req.AdminAccountID == "" {
		return nil, errors.New("admin account ID is required")
	}
	if !req.Cause.Valid() {
		return nil, errors.New("valid suspension cause is required")
	}
	if strings.TrimSpace(req.Reason) == "" {
		return nil, ErrReasonRequired
	}

	var result Course
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		row, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		if row.AccessSuspendedAt != nil {
			return ErrCourseAccessAlreadySuspended
		}
		now := time.Now().UTC()
		if _, err := tx.Exec(ctx, `
			UPDATE courses
			SET access_suspended_at = $1, access_suspension_reason = $2, updated_at = $1
			WHERE id = $3::uuid
		`, now, req.Reason, req.CourseID); err != nil {
			return fmt.Errorf("suspending course access: %w", err)
		}
		if err := writeAdminCourseAudit(ctx, tx, req.AdminAccountID, req.ActorDescriptor, "COURSE_ACCESS_SUSPENDED", req.CourseID, req.Reason, map[string]any{"cause": req.Cause, "suspended_at": now}); err != nil {
			return err
		}
		if err := r.writeCourseAccessNotification(ctx, tx, "catalog.course_access_suspended", req.CourseID, req.AdminAccountID, req.Cause, req.Reason, now); err != nil {
			return err
		}
		result = courseFromRow(row)
		result.AccessSuspendedAt = &now
		result.AccessSuspensionReason = &req.Reason
		result.UpdatedAt = now
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *Repository) RestoreCourseAccess(ctx context.Context, req RestoreCourseAccessRequest) (*Course, error) {
	if req.CourseID == "" {
		return nil, ErrCourseNotFound
	}
	if req.AdminAccountID == "" {
		return nil, errors.New("admin account ID is required")
	}
	if strings.TrimSpace(req.Reason) == "" {
		return nil, ErrReasonRequired
	}

	var result Course
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		row, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		if row.AccessSuspendedAt == nil {
			return ErrCourseAccessNotSuspended
		}
		now := time.Now().UTC()
		if _, err := tx.Exec(ctx, `
			UPDATE courses
			SET access_suspended_at = NULL, access_suspension_reason = NULL, updated_at = $1
			WHERE id = $2::uuid
		`, now, req.CourseID); err != nil {
			return fmt.Errorf("restoring course access: %w", err)
		}
		if err := writeAdminCourseAudit(ctx, tx, req.AdminAccountID, req.ActorDescriptor, "COURSE_ACCESS_RESTORED", req.CourseID, req.Reason, map[string]any{"restored_at": now}); err != nil {
			return err
		}
		if err := r.writeCourseAccessNotification(ctx, tx, "catalog.course_access_restored", req.CourseID, req.AdminAccountID, "", req.Reason, now); err != nil {
			return err
		}
		result = courseFromRow(row)
		result.AccessSuspendedAt = nil
		result.AccessSuspensionReason = nil
		result.UpdatedAt = now
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *Repository) writeCourseAccessNotification(ctx context.Context, tx pgx.Tx, eventType, courseID, adminID string, cause AccessSuspensionCause, reason string, occurredAt time.Time) error {
	writer, err := NewNotificationIntentWriter(r.outboxWriter)
	if err != nil {
		return fmt.Errorf("constructing notification intent writer: %w", err)
	}
	event := outbox.Event{Type: eventType, SchemaVersion: 1, SourceModule: "CATALOG_AND_AUTHORING", AggregateType: "COURSE", AggregateID: courseID, AggregateRevision: 1, CorrelationID: courseID, SafePayload: map[string]any{"course_id": courseID}}
	protected := map[string]any{"course_id": courseID, "admin_account_id": adminID, "cause": cause, "reason": reason, "occurred_at": occurredAt}
	if _, err := writer.WriteIntent(ctx, tx, event, protected); err != nil {
		return fmt.Errorf("writing course access notification intent: %w", err)
	}
	return nil
}

// CourseAccessState exposes live Course state for the single future S4 entitlement decision.
// It deliberately contains no grant or entitlement evaluation.
type CourseAccessState struct {
	CourseID          string          `json:"course_id"`
	Lifecycle         CourseLifecycle `json:"lifecycle"`
	RetiredAt         *time.Time      `json:"retired_at,omitempty"`
	AccessSuspendedAt *time.Time      `json:"access_suspended_at,omitempty"`
}

func (r *Repository) ReadCourseAccessState(ctx context.Context, courseID string) (*CourseAccessState, error) {
	if courseID == "" {
		return nil, ErrCourseNotFound
	}
	return r.readCourseAccessState(ctx, `SELECT id::text, lifecycle::text, retired_at, access_suspended_at FROM courses WHERE id = $1::uuid`, courseID)
}

// ReadCourseAccessStateForLesson supports current S2 identities and the legacy compatibility lesson seam.
func (r *Repository) ReadCourseAccessStateForLesson(ctx context.Context, lessonID string) (*CourseAccessState, error) {
	if lessonID == "" {
		return nil, ErrCourseNotFound
	}
	query := `
		SELECT c.id::text, c.lifecycle::text, c.retired_at, c.access_suspended_at
		FROM course_lessons cl JOIN courses c ON c.id = cl.course_id
		WHERE cl.id = $1::uuid OR cl.lesson_identity_id = $1::uuid
		UNION ALL
		SELECT c.id::text, c.lifecycle::text, c.retired_at, c.access_suspended_at
		FROM lessons l JOIN sections s ON s.id = l.section_id JOIN courses c ON c.id = s.course_id
		WHERE l.id = $1::uuid
		LIMIT 1`
	return r.readCourseAccessState(ctx, query, lessonID)
}

func (r *Repository) readCourseAccessState(ctx context.Context, query, identifier string) (*CourseAccessState, error) {
	var state CourseAccessState
	err := r.pool.QueryRow(ctx, query, identifier).Scan(&state.CourseID, &state.Lifecycle, &state.RetiredAt, &state.AccessSuspendedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCourseNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading course access state: %w", err)
	}
	return &state, nil
}
