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

var (
	ErrReasonRequired = errors.New("reason is required for change request")
)

type approvalFailureStage string

const (
	approvalAfterSupersede approvalFailureStage = "after_supersede"
	approvalAfterApprove   approvalFailureStage = "after_approve"
	approvalAfterPointer   approvalFailureStage = "after_pointer"
	approvalAfterAudit     approvalFailureStage = "after_audit"
	approvalAfterOutbox    approvalFailureStage = "after_outbox"
)

type approvalFailureKey struct{}

type approvalHold struct {
	reached chan<- struct{}
	release <-chan struct{}
}

type approvalHoldKey struct{}

func withApprovalFailure(ctx context.Context, stage approvalFailureStage) context.Context {
	return context.WithValue(ctx, approvalFailureKey{}, stage)
}

func withApprovalHold(ctx context.Context, hold approvalHold) context.Context {
	return context.WithValue(ctx, approvalHoldKey{}, hold)
}

func failApprovalAt(ctx context.Context, stage approvalFailureStage) error {
	if requested, _ := ctx.Value(approvalFailureKey{}).(approvalFailureStage); requested == stage {
		return fmt.Errorf("injected approval failure at %s", stage)
	}
	return nil
}

func holdApprovalAfterValidation(ctx context.Context) error {
	hold, ok := ctx.Value(approvalHoldKey{}).(approvalHold)
	if !ok {
		return nil
	}
	if hold.reached != nil {
		hold.reached <- struct{}{}
	}
	if hold.release == nil {
		return nil
	}
	select {
	case <-hold.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func sameNullableRevision(left, right *string) bool {
	if left == nil || *left == "" {
		return right == nil || *right == ""
	}
	return right != nil && *right == *left
}

type ReviewQueueItem struct {
	CourseID        string          `json:"course_id"`
	OwnerAccountID  string          `json:"owner_account_id"`
	RevisionID      string          `json:"revision_id"`
	RevisionNumber  int             `json:"revision_number"`
	TitleAr         string          `json:"title_ar"`
	TitleEn         string          `json:"title_en"`
	SubmittedAt     *time.Time      `json:"submitted_at"`
	CourseLifecycle CourseLifecycle `json:"course_lifecycle"`
	IsFirstPublish  bool            `json:"is_first_publish"`
}

type ApproveCourseRequest struct {
	CourseID        string
	RevisionID      string
	AdminAccountID  string
	ActorDescriptor string
}

type RequestChangesRequest struct {
	CourseID        string
	RevisionID      string
	AdminAccountID  string
	Reason          string
	ActorDescriptor string
}

type AdminPreviewRequest struct {
	CourseID        string
	RevisionID      string
	LessonID        string
	AdminAccountID  string
	ActorDescriptor string
}

func (r *Repository) ListReviewQueue(ctx context.Context) ([]ReviewQueueItem, error) {
	query := `
		SELECT c.id, c.owner_account_id, r.id, r.revision_number, r.title_ar, r.title_en,
		       r.submitted_at, c.lifecycle, (c.live_revision_id IS NULL) AS is_first_publish
		FROM course_revisions r
		JOIN courses c ON c.id = r.course_id
		WHERE r.state = 'PENDING_REVIEW'
		ORDER BY r.submitted_at ASC NULLS LAST, r.created_at ASC
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying review queue: %w", err)
	}
	defer rows.Close()

	var queue []ReviewQueueItem
	for rows.Next() {
		var item ReviewQueueItem
		if err := rows.Scan(
			&item.CourseID, &item.OwnerAccountID, &item.RevisionID, &item.RevisionNumber,
			&item.TitleAr, &item.TitleEn, &item.SubmittedAt, &item.CourseLifecycle, &item.IsFirstPublish,
		); err != nil {
			return nil, fmt.Errorf("scanning review queue item: %w", err)
		}
		queue = append(queue, item)
	}
	if queue == nil {
		queue = []ReviewQueueItem{}
	}
	return queue, nil
}

func (r *Repository) GetCourseRevisionGraph(ctx context.Context, courseID string, revisionID string) (*Course, error) {
	if courseID == "" || revisionID == "" {
		return nil, ErrCourseNotFound
	}
	query := `
		SELECT id, owner_account_id, lifecycle, live_revision_id,
		       classification_model, institution_id, subject_id,
		       access_suspended_at, access_suspension_reason, retired_at,
		       created_at, updated_at
		FROM courses
		WHERE id = $1::uuid
	`
	var c Course
	err := r.pool.QueryRow(ctx, query, courseID).Scan(
		&c.ID, &c.OwnerAccountID, &c.Lifecycle, &c.LiveRevisionID,
		&c.ClassificationModel, &c.InstitutionID, &c.SubjectID,
		&c.AccessSuspendedAt, &c.AccessSuspensionReason, &c.RetiredAt,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCourseNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting review course: %w", err)
	}

	rev, err := r.loadRevisionGraphByID(ctx, revisionID)
	if err != nil {
		return nil, fmt.Errorf("loading candidate revision %s: %w", revisionID, err)
	}
	if rev == nil || rev.CourseID != courseID {
		return nil, ErrCourseNotFound
	}
	c.EditableRevision = rev

	if c.LiveRevisionID != nil && *c.LiveRevisionID != "" {
		liveRevisionID := *c.LiveRevisionID
		liveRev, err := r.loadRevisionGraphByID(ctx, liveRevisionID)
		if err != nil {
			return nil, fmt.Errorf("loading live revision %s: %w", liveRevisionID, err)
		}
		if liveRev == nil {
			return nil, fmt.Errorf("live revision %s is missing", liveRevisionID)
		}
		c.LiveRevision = liveRev
	}
	if err := loadCourseAcademicProjection(ctx, r.pool, &c); err != nil {
		return nil, err
	}

	return &c, nil
}

func (r *Repository) loadRevisionGraphByID(ctx context.Context, revID string) (*CourseRevision, error) {
	query := `
		SELECT cr.id, cr.course_id, cr.based_on_revision_id, cr.state, cr.revision_number,
		       cr.title_ar, cr.title_en, cr.description_ar, cr.description_en,
		       cr.major_term_id, cr.subject_term_id, cr.study_year, cr.preview_asset_version_id,
		       preview.state::text,
		       cr.submitted_at, cr.reviewed_at, cr.reviewed_by_account_id, cr.review_reason,
		       cr.created_at, cr.updated_at
		FROM course_revisions cr
		LEFT JOIN media_asset_versions preview ON preview.id = cr.preview_asset_version_id
		WHERE cr.id = $1::uuid
	`
	var rev CourseRevision
	err := r.pool.QueryRow(ctx, query, revID).Scan(
		&rev.ID, &rev.CourseID, &rev.BasedOnRevisionID, &rev.State, &rev.RevisionNumber, &rev.TitleAr, &rev.TitleEn, &rev.DescriptionAr, &rev.DescriptionEn,
		&rev.MajorTermID, &rev.SubjectTermID, &rev.StudyYear, &rev.PreviewAssetVersionID, &rev.PreviewAssetState,
		&rev.SubmittedAt, &rev.ReviewedAt, &rev.ReviewedByAccountID, &rev.ReviewReason, &rev.CreatedAt, &rev.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if err := loadRevisionGraphBatch(ctx, r.pool, &rev); err != nil {
		return nil, fmt.Errorf("loading revision graph: %w", err)
	}
	return &rev, nil
}

// RequestChanges requests changes on or rejects a pending course revision (BR-072, T035).
func (r *Repository) RequestChanges(
	ctx context.Context,
	req RequestChangesRequest,
) (*Course, error) {
	courseID := req.CourseID
	revisionID := req.RevisionID
	adminAccountID := req.AdminAccountID
	reason := req.Reason
	actorDescriptor := req.ActorDescriptor
	if courseID == "" || revisionID == "" || adminAccountID == "" {
		return nil, errors.New("courseID, revisionID, and adminAccountID are required")
	}
	if strings.TrimSpace(reason) == "" {
		return nil, ErrReasonRequired
	}
	if r.outboxWriter == nil {
		return nil, errors.New("outbox writer is required")
	}

	var course Course
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		row, err := r.LockCourse(ctx, tx, courseID)
		if err != nil {
			return err
		}

		var rev CourseRevision
		queryRev := `
			SELECT id, course_id, based_on_revision_id, state, revision_number FROM course_revisions
			WHERE id = $1::uuid AND course_id = $2::uuid
			FOR UPDATE
		`
		err = tx.QueryRow(ctx, queryRev, revisionID, courseID).Scan(
			&rev.ID, &rev.CourseID, &rev.BasedOnRevisionID, &rev.State, &rev.RevisionNumber,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return &LifecycleConflictError{
				CourseID: courseID,
				Actual:   row.Lifecycle,
				Expected: []string{"PENDING_REVIEW"},
			}
		}
		if err != nil {
			return fmt.Errorf("locking revision: %w", err)
		}

		if rev.State != RevisionPendingReview {
			return &LifecycleConflictError{
				CourseID: courseID,
				Actual:   string(rev.State),
				Expected: []string{string(RevisionPendingReview)},
			}
		}
		if !sameNullableRevision(row.LiveRevisionID, rev.BasedOnRevisionID) {
			return &LifecycleConflictError{
				CourseID: courseID,
				Actual:   string(rev.State),
				Expected: []string{"PENDING_REVIEW_MATCHING_BASE"},
			}
		}

		now := time.Now().UTC()
		isFirstPublish := (row.LiveRevisionID == nil || *row.LiveRevisionID == "")

		var auditAction string

		if isFirstPublish {
			auditAction = "COURSE_CHANGES_REQUESTED"

			_, err = tx.Exec(ctx, `
				UPDATE course_revisions
				SET state = 'CHANGES_REQUESTED', reviewed_at = $1, reviewed_by_account_id = $2::uuid, review_reason = $3, updated_at = $1
				WHERE id = $4::uuid
			`, now, adminAccountID, reason, rev.ID)
			if err != nil {
				return fmt.Errorf("updating revision state: %w", err)
			}
			_, err = tx.Exec(ctx, `
				UPDATE courses
				SET lifecycle = 'CHANGES_REQUESTED', updated_at = $1
				WHERE id = $2::uuid
			`, now, courseID)
			if err != nil {
				return fmt.Errorf("updating course lifecycle: %w", err)
			}
			course.Lifecycle = LifecycleChangesRequested
		} else {
			// Published course revision rejection: state = REJECTED, course lifecycle & pointer UNCHANGED (FR-052)
			auditAction = "COURSE_REVISION_REJECTED"

			_, err = tx.Exec(ctx, `
				UPDATE course_revisions
				SET state = 'REJECTED', reviewed_at = $1, reviewed_by_account_id = $2::uuid, review_reason = $3, updated_at = $1
				WHERE id = $4::uuid
			`, now, adminAccountID, reason, rev.ID)
			if err != nil {
				return fmt.Errorf("updating revision state: %w", err)
			}
			course.Lifecycle = CourseLifecycle(row.Lifecycle)
			course.LiveRevisionID = row.LiveRevisionID
		}

		audit := AuditEvent{
			ActorAccountID:  &adminAccountID,
			ActorRole:       "ADMIN",
			ActorDescriptor: actorDescriptor,
			Action:          auditAction,
			TargetType:      "COURSE",
			TargetID:        courseID,
			TargetRevision:  &rev.RevisionNumber,
			Reason:          reason,
			Metadata:        map[string]any{"revision_id": rev.ID, "is_first_publish": isFirstPublish},
		}
		if err := WriteAuditEvent(ctx, tx, audit); err != nil {
			return err
		}

		writer, err := NewNotificationIntentWriter(r.outboxWriter)
		if err != nil {
			return fmt.Errorf("constructing notification intent writer: %w", err)
		}
		eventType := "catalog.course_changes_requested"
		if !isFirstPublish {
			eventType = "catalog.course_revision_rejected"
		}
		event := outbox.Event{
			Type:              eventType,
			SchemaVersion:     1,
			SourceModule:      "CATALOG_AND_AUTHORING",
			AggregateType:     "COURSE",
			AggregateID:       courseID,
			AggregateRevision: 1,
			CorrelationID:     courseID,
			SafePayload:       map[string]any{"course_id": courseID},
		}
		protected := map[string]any{
			"course_id":        courseID,
			"owner_account_id": row.OwnerAccountID,
			"revision_id":      rev.ID,
			"reason":           reason,
		}
		if _, err := writer.WriteIntent(ctx, tx, event, protected); err != nil {
			return fmt.Errorf("writing rejection intent: %w", err)
		}

		course.ID = courseID
		course.OwnerAccountID = row.OwnerAccountID
		course.CreatedAt = row.CreatedAt
		course.UpdatedAt = now
		return nil
	})

	if err != nil {
		return nil, err
	}
	return &course, nil
}

// PreviewAdminLesson satisfies BR-081 & FR-016: Admin video preview creates NO enrollment and NO entitlement, and is audited on a distinct path.
func (r *Repository) PreviewAdminLesson(
	ctx context.Context,
	req AdminPreviewRequest,
) (string, error) {
	courseID := req.CourseID
	revisionID := req.RevisionID
	lessonID := req.LessonID
	adminAccountID := req.AdminAccountID
	actorDescriptor := req.ActorDescriptor
	if courseID == "" || revisionID == "" || lessonID == "" || adminAccountID == "" {
		return "", errors.New("courseID, revisionID, lessonID, and adminAccountID are required")
	}

	var videoAssetVersionID *string
	err := r.pool.QueryRow(ctx, `
		SELECT l.video_asset_version_id
		FROM course_lessons l
		JOIN course_sections s ON s.id = l.section_id
		WHERE s.revision_id = $1::uuid AND s.course_id = $2::uuid
		  AND l.lesson_identity_id = $3::uuid
	`, revisionID, courseID, lessonID).Scan(&videoAssetVersionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrCourseNotFound
	}
	if err != nil {
		return "", fmt.Errorf("querying lesson video: %w", err)
	}

	if videoAssetVersionID == nil || *videoAssetVersionID == "" {
		return "", errors.New("lesson has no attached video asset version")
	}

	err = r.ExecTx(ctx, func(tx pgx.Tx) error {
		audit := AuditEvent{
			ActorAccountID:  &adminAccountID,
			ActorRole:       "ADMIN",
			ActorDescriptor: actorDescriptor,
			Action:          "ADMIN_CONTENT_PREVIEWED",
			TargetType:      "LESSON",
			TargetID:        lessonID,
			Reason:          "Admin content preview",
			Metadata:        map[string]any{"course_id": courseID, "revision_id": revisionID, "video_asset_version_id": *videoAssetVersionID},
		}
		return WriteAuditEvent(ctx, tx, audit)
	})
	if err != nil {
		return "", err
	}

	return *videoAssetVersionID, nil
}

func (r *Repository) loadRevisionGraphByIDTx(ctx context.Context, tx pgx.Tx, revID string) (*CourseRevision, error) {
	query := `
		SELECT cr.id, cr.course_id, cr.based_on_revision_id, cr.state, cr.revision_number,
		       cr.title_ar, cr.title_en, cr.description_ar, cr.description_en,
		       cr.major_term_id, cr.subject_term_id, cr.study_year, cr.preview_asset_version_id,
		       preview.state::text,
		       cr.submitted_at, cr.reviewed_at, cr.reviewed_by_account_id, cr.review_reason,
		       cr.created_at, cr.updated_at
		FROM course_revisions cr
		LEFT JOIN media_asset_versions preview ON preview.id = cr.preview_asset_version_id
		WHERE cr.id = $1::uuid
	`
	var rev CourseRevision
	err := tx.QueryRow(ctx, query, revID).Scan(
		&rev.ID, &rev.CourseID, &rev.BasedOnRevisionID, &rev.State, &rev.RevisionNumber, &rev.TitleAr, &rev.TitleEn, &rev.DescriptionAr, &rev.DescriptionEn,
		&rev.MajorTermID, &rev.SubjectTermID, &rev.StudyYear, &rev.PreviewAssetVersionID, &rev.PreviewAssetState,
		&rev.SubmittedAt, &rev.ReviewedAt, &rev.ReviewedByAccountID, &rev.ReviewReason, &rev.CreatedAt, &rev.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if err := loadRevisionGraphBatch(ctx, tx, &rev); err != nil {
		return nil, fmt.Errorf("loading revision graph: %w", err)
	}
	return &rev, nil
}
