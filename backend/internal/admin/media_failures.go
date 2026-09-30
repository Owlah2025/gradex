package admin

import (
	"context"
	"fmt"

	"github.com/Owlah2025/gradex/backend/internal/identity"
)

func (r *Repository) ListMediaFailures(
	ctx context.Context,
	request MediaFailuresRequest,
) (MediaFailuresResult, error) {
	if err := validateMediaFailuresRequest(request); err != nil {
		return MediaFailuresResult{}, err
	}
	if r == nil || r.pool == nil {
		return MediaFailuresResult{}, ErrRepositoryNil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT count(*) OVER (), mav.id::text, mav.state::text,
		       CASE WHEN mav.state IN ('PROCESS_FAILED', 'SCAN_ERROR', 'SCAN_FAILED') THEN 'failed' ELSE 'stuck' END,
		       mav.kind::text,
		       COALESCE(live.title_ar, latest.title_ar, ''),
		       COALESCE(live.title_en, latest.title_en, ''),
		       COALESCE(cl.title_ar, ''), COALESCE(cl.title_en, ''),
		       COALESCE(owner.display_name, ''), mav.last_failure_category,
		       mav.processing_stage, mav.processing_progress_percent,
		       mav.created_at, COALESCE(mav.processing_updated_at, mav.created_at),
		       CASE
		         WHEN mav.state IN ('PROCESS_FAILED', 'SCAN_ERROR', 'SCAN_FAILED') THEN 'retry'
		         WHEN mav.state = 'PLAYABLE' AND recovery.state = 'NEEDS_OPERATOR' THEN 'retry-enhancements'
		         ELSE ''
		       END
		  FROM media_asset_versions mav
		  JOIN media_assets ma ON ma.id = mav.logical_asset_id
		  JOIN accounts owner ON owner.id = ma.owner_account_id
		  JOIN courses c ON c.id = ma.course_id
		  LEFT JOIN course_lessons cl ON cl.id = ma.lesson_id
		  LEFT JOIN course_revisions live ON live.id = c.live_revision_id AND live.course_id = c.id
		  LEFT JOIN LATERAL (
			SELECT cr.title_ar, cr.title_en
			  FROM course_revisions cr
			 WHERE cr.course_id = c.id
			 ORDER BY cr.revision_number DESC
			 LIMIT 1
		  ) latest ON TRUE
		  LEFT JOIN media_auto_enhancement_recovery recovery
		    ON recovery.asset_version_id = mav.id
		 WHERE ma.retired_at IS NULL
		   AND (`+mediaFailureWhere(request.State)+`)
		 ORDER BY COALESCE(mav.processing_updated_at, mav.created_at) ASC, mav.id ASC
		 LIMIT $1 OFFSET $2`, request.Limit+1, (request.Page-1)*request.Limit)
	if err != nil {
		return MediaFailuresResult{}, fmt.Errorf("querying media failure rows: %w", err)
	}
	defer rows.Close()

	items := make([]MediaFailure, 0, request.Limit)
	totalRows := 0
	for rows.Next() {
		var total int
		var item MediaFailure
		var arabicCourse, englishCourse, arabicLesson, englishLesson string
		var failureCategory, processingStage *string
		var kind string
		if err := rows.Scan(
			&total, &item.AssetVersionID, &item.MediaState, &item.State, &kind,
			&arabicCourse, &englishCourse, &arabicLesson, &englishLesson,
			&item.OwnerDisplayName, &failureCategory, &processingStage,
			&item.ProcessingProgress, &item.CreatedAt, &item.UpdatedAt, &item.RetryAction,
		); err != nil {
			return MediaFailuresResult{}, fmt.Errorf("scanning media failure row: %w", err)
		}
		totalRows = total
		item.Kind = kind
		item.CourseTitle = localizedInstitution(request.Locale, &arabicCourse, &englishCourse)
		item.LessonTitle = localizedInstitution(request.Locale, &arabicLesson, &englishLesson)
		if failureCategory != nil {
			item.FailureCategory = *failureCategory
		}
		if processingStage != nil {
			item.ProcessingStage = *processingStage
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return MediaFailuresResult{}, fmt.Errorf("iterating media failure rows: %w", err)
	}
	hasMore := totalRows > request.Page*request.Limit
	if hasMore {
		items = items[:request.Limit]
	}
	return MediaFailuresResult{Items: items, Page: request.Page, Limit: request.Limit, HasMore: hasMore}, nil
}

func validateMediaFailuresRequest(request MediaFailuresRequest) error {
	decision := identity.Authorize(request.Principal, identity.CapAdminOperations)
	if !decision.Allowed {
		return fmt.Errorf("%w: %s", ErrUnauthorized, decision.Reason)
	}
	if !request.Locale.Valid() || !validPage(request.Page, request.Limit) {
		return ErrInvalidInput
	}
	if request.State != "" && request.State != "failed" && request.State != "stuck" {
		return ErrInvalidInput
	}
	return nil
}

func mediaFailureWhere(state string) string {
	failed := "mav.state IN ('PROCESS_FAILED', 'SCAN_ERROR', 'SCAN_FAILED')"
	stuck := "((mav.state IN ('SCANNING', 'PROCESSING') AND (mav.work_lease_expires_at IS NULL OR mav.work_lease_expires_at <= now())) OR (mav.state = 'PLAYABLE' AND recovery.state = 'NEEDS_OPERATOR'))"
	switch state {
	case "failed":
		return failed
	case "stuck":
		return stuck
	default:
		return failed + " OR " + stuck
	}
}

var _ interface {
	ListMediaFailures(context.Context, MediaFailuresRequest) (MediaFailuresResult, error)
} = (*Repository)(nil)
