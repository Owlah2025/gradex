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
	ErrTaxonomyTermRetired = errors.New("assigned taxonomy term is retired")
	ErrReasonRequired      = errors.New("reason is required for change request")
)

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

func (r *Repository) GetReviewCourseGraph(ctx context.Context, courseID string) (*Course, error) {
	if courseID == "" {
		return nil, ErrCourseNotFound
	}
	query := `
		SELECT id, owner_account_id, lifecycle, live_revision_id,
		       access_suspended_at, access_suspension_reason, retired_at,
		       created_at, updated_at
		FROM courses
		WHERE id = $1::uuid
	`
	var c Course
	err := r.pool.QueryRow(ctx, query, courseID).Scan(
		&c.ID, &c.OwnerAccountID, &c.Lifecycle, &c.LiveRevisionID,
		&c.AccessSuspendedAt, &c.AccessSuspensionReason, &c.RetiredAt,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCourseNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting review course: %w", err)
	}

	rev, err := r.getPendingOrLatestRevisionGraph(ctx, courseID)
	if err != nil {
		return nil, fmt.Errorf("loading revision graph for review: %w", err)
	}
	c.EditableRevision = rev

	if c.LiveRevisionID != nil && *c.LiveRevisionID != "" {
		liveRev, err := r.loadRevisionGraphByID(ctx, *c.LiveRevisionID)
		if err == nil {
			c.LiveRevision = liveRev
		}
	}

	return &c, nil
}

func (r *Repository) getPendingOrLatestRevisionGraph(ctx context.Context, courseID string) (*CourseRevision, error) {
	query := `
		SELECT id FROM course_revisions
		WHERE course_id = $1::uuid AND state = 'PENDING_REVIEW'
		ORDER BY revision_number DESC
		LIMIT 1
	`
	var revID string
	err := r.pool.QueryRow(ctx, query, courseID).Scan(&revID)
	if errors.Is(err, pgx.ErrNoRows) {
		rev, err := r.getLatestRevision(ctx, courseID)
		if err != nil || rev == nil {
			return nil, err
		}
		revID = rev.ID
	} else if err != nil {
		return nil, err
	}

	return r.loadRevisionGraphByID(ctx, revID)
}

func (r *Repository) loadRevisionGraphByID(ctx context.Context, revID string) (*CourseRevision, error) {
	query := `
		SELECT id, course_id, state, revision_number, title_ar, title_en, description_ar, description_en,
		       major_term_id, subject_term_id, study_year, preview_asset_version_id,
		       submitted_at, reviewed_at, reviewed_by_account_id, review_reason, created_at, updated_at
		FROM course_revisions
		WHERE id = $1::uuid
	`
	var rev CourseRevision
	err := r.pool.QueryRow(ctx, query, revID).Scan(
		&rev.ID, &rev.CourseID, &rev.State, &rev.RevisionNumber, &rev.TitleAr, &rev.TitleEn, &rev.DescriptionAr, &rev.DescriptionEn,
		&rev.MajorTermID, &rev.SubjectTermID, &rev.StudyYear, &rev.PreviewAssetVersionID,
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

// ApproveCourse approves a pending review course revision inside one transaction (T027, FR-025, concurrency case 3).
func (r *Repository) ApproveCourse(
	ctx context.Context,
	validator AssetVersionValidator,
	courseID string,
	adminAccountID string,
	actorDescriptor string,
) (*Course, error) {
	if courseID == "" || adminAccountID == "" {
		return nil, errors.New("courseID and adminAccountID are required")
	}

	var course Course
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		// 1. SELECT ... FOR UPDATE course row. Re-assert expected state inside transaction.
		row, err := r.LockCourse(ctx, tx, courseID)
		if err != nil {
			return err
		}

		// 2. Concurrency Case 3 / FR-025: Re-read owner account status inside approving transaction.
		if err := r.checkOwnerActive(ctx, tx, row.OwnerAccountID); err != nil {
			return err
		}

		// Get the candidate revision in PENDING_REVIEW
		var revID string
		var prevLiveRevID *string = row.LiveRevisionID
		err = tx.QueryRow(ctx, `
			SELECT id FROM course_revisions
			WHERE course_id = $1::uuid AND state = 'PENDING_REVIEW'
			FOR UPDATE
		`, courseID).Scan(&revID)
		if errors.Is(err, pgx.ErrNoRows) {
			return &LifecycleConflictError{
				CourseID: courseID,
				Actual:   row.Lifecycle,
				Expected: []string{"PENDING_REVIEW"},
			}
		}
		if err != nil {
			return fmt.Errorf("locking pending revision: %w", err)
		}

		rev, err := r.loadRevisionGraphByIDTx(ctx, tx, revID)
		if err != nil || rev == nil {
			return fmt.Errorf("loading pending revision graph: %w", err)
		}

		// 3. Revalidate every referenced Asset Version present and processed NOW (FR-025).
		if rev.PreviewAssetVersionID != nil && *rev.PreviewAssetVersionID != "" && validator != nil {
			if err := validator.ValidateAssetVersion(ctx, *rev.PreviewAssetVersionID); err != nil {
				return fmt.Errorf("preview asset version validation failed: %w", err)
			}
		}
		for _, sec := range rev.Sections {
			for _, les := range sec.Lessons {
				if les.VideoAssetVersionID != nil && *les.VideoAssetVersionID != "" && validator != nil {
					if err := validator.ValidateAssetVersion(ctx, *les.VideoAssetVersionID); err != nil {
						return fmt.Errorf("lesson %s video asset version validation failed: %w", les.ID, err)
					}
				}
				for _, lf := range les.Files {
					if lf.AssetVersionID != "" && validator != nil {
						if err := validator.ValidateAssetVersion(ctx, lf.AssetVersionID); err != nil {
							return fmt.Errorf("lesson file %s asset version validation failed: %w", lf.ID, err)
						}
					}
				}
			}
		}

		// 4. Re-check taxonomy terms: ensure assigned terms are not retired.
		if rev.MajorTermID != nil && *rev.MajorTermID != "" {
			if err := r.checkTaxonomyTermNotRetired(ctx, tx, *rev.MajorTermID); err != nil {
				return err
			}
		}
		if rev.SubjectTermID != nil && *rev.SubjectTermID != "" {
			if err := r.checkTaxonomyTermNotRetired(ctx, tx, *rev.SubjectTermID); err != nil {
				return err
			}
		}

		// 5. Swap pointer: set candidate revision APPROVED, previous live revision SUPERSEDED, course PUBLISHED.
		now := time.Now().UTC()
		_, err = tx.Exec(ctx, `
			UPDATE course_revisions
			SET state = 'APPROVED', reviewed_at = $1, reviewed_by_account_id = $2::uuid, updated_at = $1
			WHERE id = $3::uuid
		`, now, adminAccountID, revID)
		if err != nil {
			return fmt.Errorf("approving revision: %w", err)
		}

		if prevLiveRevID != nil && *prevLiveRevID != "" && *prevLiveRevID != revID {
			_, err = tx.Exec(ctx, `
				UPDATE course_revisions
				SET state = 'SUPERSEDED', updated_at = $1
				WHERE id = $2::uuid
			`, now, *prevLiveRevID)
			if err != nil {
				return fmt.Errorf("superseding previous live revision: %w", err)
			}
		}

		_, err = tx.Exec(ctx, `
			UPDATE courses
			SET live_revision_id = $1::uuid, lifecycle = 'PUBLISHED', updated_at = $2
			WHERE id = $3::uuid
		`, revID, now, courseID)
		if err != nil {
			return fmt.Errorf("updating course live revision pointer: %w", err)
		}

		// 6. Write COURSE_PUBLISHED audit row.
		audit := AuditEvent{
			ActorAccountID:  &adminAccountID,
			ActorRole:       "ADMIN",
			ActorDescriptor: actorDescriptor,
			Action:          "COURSE_PUBLISHED",
			TargetType:      "COURSE",
			TargetID:        courseID,
			TargetRevision:  &rev.RevisionNumber,
			Reason:          "Course revision approved and published",
			Metadata:        map[string]any{"revision_id": revID},
		}
		if err := WriteAuditEvent(ctx, tx, audit); err != nil {
			return err
		}

		// 7. Write Instructor notification intent.
		if r.outboxWriter != nil {
			writer, err := NewNotificationIntentWriter(r.outboxWriter)
			if err != nil {
				return fmt.Errorf("constructing notification intent writer: %w", err)
			}
			event := outbox.Event{
				Type:              "catalog.course_published",
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
				"revision_id":      revID,
				"published_at":     now,
			}
			if _, err := writer.WriteIntent(ctx, tx, event, protected); err != nil {
				return fmt.Errorf("writing publication intent: %w", err)
			}
		}

		course.ID = courseID
		course.OwnerAccountID = row.OwnerAccountID
		course.Lifecycle = LifecyclePublished
		course.LiveRevisionID = &revID
		course.CreatedAt = row.CreatedAt
		course.UpdatedAt = now
		course.LiveRevision = rev
		return nil
	})

	if err != nil {
		return nil, err
	}
	return &course, nil
}

// RequestChanges requests changes on a pending course revision (BR-072).
func (r *Repository) RequestChanges(
	ctx context.Context,
	courseID string,
	adminAccountID string,
	reason string,
	actorDescriptor string,
) (*Course, error) {
	if courseID == "" || adminAccountID == "" {
		return nil, errors.New("courseID and adminAccountID are required")
	}
	if strings.TrimSpace(reason) == "" {
		return nil, ErrReasonRequired
	}

	var course Course
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		row, err := r.LockCourse(ctx, tx, courseID)
		if err != nil {
			return err
		}

		var revID string
		var revNumber int
		err = tx.QueryRow(ctx, `
			SELECT id, revision_number FROM course_revisions
			WHERE course_id = $1::uuid AND state = 'PENDING_REVIEW'
			FOR UPDATE
		`, courseID).Scan(&revID, &revNumber)
		if errors.Is(err, pgx.ErrNoRows) {
			return &LifecycleConflictError{
				CourseID: courseID,
				Actual:   row.Lifecycle,
				Expected: []string{"PENDING_REVIEW"},
			}
		}
		if err != nil {
			return fmt.Errorf("locking pending revision: %w", err)
		}

		now := time.Now().UTC()
		isFirstPublish := (row.LiveRevisionID == nil || *row.LiveRevisionID == "")

		if isFirstPublish {
			// First publication moves to CHANGES_REQUESTED and stays hidden
			_, err = tx.Exec(ctx, `
				UPDATE course_revisions
				SET state = 'CHANGES_REQUESTED', reviewed_at = $1, reviewed_by_account_id = $2::uuid, review_reason = $3, updated_at = $1
				WHERE id = $4::uuid
			`, now, adminAccountID, reason, revID)
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
			// Pending revision is rejected; currently Published version stays live and unchanged (FR-021)
			_, err = tx.Exec(ctx, `
				UPDATE course_revisions
				SET state = 'REJECTED', reviewed_at = $1, reviewed_by_account_id = $2::uuid, review_reason = $3, updated_at = $1
				WHERE id = $4::uuid
			`, now, adminAccountID, reason, revID)
			if err != nil {
				return fmt.Errorf("updating revision state: %w", err)
			}
			// Course lifecycle remains PUBLISHED
			course.Lifecycle = LifecyclePublished
			course.LiveRevisionID = row.LiveRevisionID
		}

		auditAction := "COURSE_CHANGES_REQUESTED"
		if !isFirstPublish {
			auditAction = "REVISION_REJECTED"
		}

		audit := AuditEvent{
			ActorAccountID:  &adminAccountID,
			ActorRole:       "ADMIN",
			ActorDescriptor: actorDescriptor,
			Action:          auditAction,
			TargetType:      "COURSE",
			TargetID:        courseID,
			TargetRevision:  &revNumber,
			Reason:          reason,
			Metadata:        map[string]any{"revision_id": revID, "is_first_publish": isFirstPublish},
		}
		if err := WriteAuditEvent(ctx, tx, audit); err != nil {
			return err
		}

		if r.outboxWriter != nil {
			writer, err := NewNotificationIntentWriter(r.outboxWriter)
			if err == nil {
				event := outbox.Event{
					Type:              "catalog.course_changes_requested",
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
					"revision_id":      revID,
					"reason":           reason,
				}
				_, _ = writer.WriteIntent(ctx, tx, event, protected)
			}
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
	courseID string,
	lessonID string,
	adminAccountID string,
	actorDescriptor string,
) (string, error) {
	if courseID == "" || lessonID == "" || adminAccountID == "" {
		return "", errors.New("courseID, lessonID, and adminAccountID are required")
	}

	var videoAssetVersionID *string
	err := r.pool.QueryRow(ctx, `
		SELECT l.video_asset_version_id
		FROM course_lessons l
		JOIN course_sections s ON s.id = l.section_id
		JOIN course_revisions r ON r.id = s.revision_id
		WHERE r.course_id = $1::uuid AND l.id = $2::uuid
	`, courseID, lessonID).Scan(&videoAssetVersionID)
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
			Action:          "ADMIN_VIDEO_PREVIEWED",
			TargetType:      "LESSON",
			TargetID:        lessonID,
			Reason:          "Admin content preview",
			Metadata:        map[string]any{"course_id": courseID, "video_asset_version_id": *videoAssetVersionID},
		}
		return WriteAuditEvent(ctx, tx, audit)
	})
	if err != nil {
		return "", err
	}

	return *videoAssetVersionID, nil
}

func (r *Repository) checkTaxonomyTermNotRetired(ctx context.Context, tx pgx.Tx, termID string) error {
	var retiredAt *time.Time
	err := tx.QueryRow(ctx, `SELECT retired_at FROM taxonomy_terms WHERE id = $1::uuid`, termID).Scan(&retiredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTaxonomyTermRetired
	}
	if err != nil {
		return fmt.Errorf("checking taxonomy term status: %w", err)
	}
	if retiredAt != nil {
		return ErrTaxonomyTermRetired
	}
	return nil
}

func (r *Repository) loadRevisionGraphByIDTx(ctx context.Context, tx pgx.Tx, revID string) (*CourseRevision, error) {
	query := `
		SELECT id, course_id, state, revision_number, title_ar, title_en, description_ar, description_en,
		       major_term_id, subject_term_id, study_year, preview_asset_version_id,
		       submitted_at, reviewed_at, reviewed_by_account_id, review_reason, created_at, updated_at
		FROM course_revisions
		WHERE id = $1::uuid
	`
	var rev CourseRevision
	err := tx.QueryRow(ctx, query, revID).Scan(
		&rev.ID, &rev.CourseID, &rev.State, &rev.RevisionNumber, &rev.TitleAr, &rev.TitleEn, &rev.DescriptionAr, &rev.DescriptionEn,
		&rev.MajorTermID, &rev.SubjectTermID, &rev.StudyYear, &rev.PreviewAssetVersionID,
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
