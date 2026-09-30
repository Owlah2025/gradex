package admin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/learning"
)

func (r *Repository) GetInbox(ctx context.Context, request InboxRequest) (InboxResult, error) {
	if err := validateInboxRequest(request); err != nil {
		return InboxResult{}, err
	}
	if r == nil || r.pool == nil {
		return InboxResult{}, ErrRepositoryNil
	}

	sections := make([]InboxSection, 0, 6)
	if identity.Authorize(request.Principal, identity.CapCatalogPublish).Allowed {
		section, queryErr := r.queryCourseReviewInbox(ctx, request)
		if queryErr != nil {
			return InboxResult{}, queryErr
		}
		sections = append(sections, section)
	}
	if identity.Authorize(request.Principal, identity.CapCourseAccessGrant).Allowed {
		purchase, queryErr := r.queryPurchaseInbox(ctx, request)
		if queryErr != nil {
			return InboxResult{}, queryErr
		}
		invitations, queryErr := r.queryInvitationInbox(ctx, request)
		if queryErr != nil {
			return InboxResult{}, queryErr
		}
		sections = append(sections, purchase, invitations)
	}
	if identity.Authorize(request.Principal, identity.CapAdminOperations).Allowed {
		reports, queryErr := r.queryReportInbox(ctx, request)
		if queryErr != nil {
			return InboxResult{}, queryErr
		}
		mediaFailures, queryErr := r.queryMediaFailureInbox(ctx, request)
		if queryErr != nil {
			return InboxResult{}, queryErr
		}
		sections = append(sections, reports, mediaFailures)
	}
	if identity.Authorize(request.Principal, identity.CapAcademicCatalog).Allowed {
		section, queryErr := r.querySubjectRequestInbox(ctx, request)
		if queryErr != nil {
			return InboxResult{}, queryErr
		}
		sections = append(sections, section)
	}
	return InboxResult{Sections: sections}, nil
}

func validateInboxRequest(request InboxRequest) error {
	if err := request.Principal.Valid(); err != nil {
		return ErrUnauthorized
	}
	if !request.Locale.Valid() || request.Limit < 1 || request.Limit > 10 {
		return ErrInvalidInput
	}
	return nil
}

func (r *Repository) queryCourseReviewInbox(ctx context.Context, request InboxRequest) (InboxSection, error) {
	return r.queryInboxRows(ctx, `
		SELECT count(*) OVER (), r.course_id::text, r.title_ar, r.title_en,
		       COALESCE(r.submitted_at, r.created_at),
		       GREATEST(0, EXTRACT(EPOCH FROM (now() - COALESCE(r.submitted_at, r.created_at))))::bigint
		FROM course_revisions r
		JOIN courses c ON c.id = r.course_id
		WHERE r.state = 'PENDING_REVIEW'
		ORDER BY COALESCE(r.submitted_at, r.created_at) ASC, r.id ASC
		LIMIT $1`, request.Limit, "course_review", "/admin/courses/:courseId/review", request.Locale)
}

func (r *Repository) queryPurchaseInbox(ctx context.Context, request InboxRequest) (InboxSection, error) {
	return r.queryInboxRows(ctx, `
		SELECT count(*) OVER (), p.id::text,
		       COALESCE(NULLIF(a.display_name, ''), NULLIF(p.email, ''), 'Purchase request'),
		       COALESCE(NULLIF(a.display_name, ''), NULLIF(p.email, ''), 'Purchase request'), p.requested_at,
		       GREATEST(0, EXTRACT(EPOCH FROM (now() - p.requested_at)))::bigint
		FROM purchase_requests p
		LEFT JOIN accounts a ON a.id = p.requester_account_id
		WHERE p.state = 'WAITING_PAYMENT'
		ORDER BY p.requested_at ASC, p.id ASC
		LIMIT $1`, request.Limit, "purchase_request", "/admin/course-access", request.Locale)
}

func (r *Repository) queryInvitationInbox(ctx context.Context, request InboxRequest) (InboxSection, error) {
	return r.queryInboxRows(ctx, `
		SELECT count(*) OVER (), i.id::text,
		       COALESCE(NULLIF(a.display_name, ''), NULLIF(i.email, ''), 'Access invitation'),
		       COALESCE(NULLIF(a.display_name, ''), NULLIF(i.email, ''), 'Access invitation'), i.created_at,
		       GREATEST(0, EXTRACT(EPOCH FROM (now() - i.created_at)))::bigint
		FROM course_access_invitations i
		LEFT JOIN accounts a ON a.id = i.accepted_by_account_id
		WHERE i.state = 'PENDING_ADMIN_APPROVAL'
		ORDER BY i.created_at ASC, i.id ASC
		LIMIT $1`, request.Limit, "access_invitation", "/admin/course-access", request.Locale)
}

func (r *Repository) querySubjectRequestInbox(ctx context.Context, request InboxRequest) (InboxSection, error) {
	return r.queryInboxRows(ctx, `
		SELECT count(*) OVER (), sr.id::text,
		       COALESCE(NULLIF(sr.proposed_title_ar, ''), NULLIF(sr.proposed_title_en, ''), NULLIF(requester.display_name, ''), 'Subject request'),
		       COALESCE(NULLIF(sr.proposed_title_en, ''), NULLIF(sr.proposed_title_ar, ''), NULLIF(requester.display_name, ''), 'Subject request'), sr.created_at,
		       GREATEST(0, EXTRACT(EPOCH FROM (now() - sr.created_at)))::bigint
		FROM subject_requests sr
		JOIN accounts requester ON requester.id = sr.requester_account_id
		WHERE sr.status = 'PENDING'
		ORDER BY sr.created_at ASC, sr.id ASC
		LIMIT $1`, request.Limit, "subject_request", "/admin/academic-catalog", request.Locale)
}

func (r *Repository) queryMediaFailureInbox(ctx context.Context, request InboxRequest) (InboxSection, error) {
	return r.queryInboxRows(ctx, `
		SELECT count(*) OVER (), mav.id::text,
		       COALESCE(live.title_ar, latest.title_ar, 'فشل معالجة الوسائط'),
		       COALESCE(live.title_en, latest.title_en, 'Media processing failure'), mav.created_at,
		       GREATEST(0, EXTRACT(EPOCH FROM (now() - mav.created_at)))::bigint
		FROM media_asset_versions mav
		JOIN media_assets ma ON ma.id = mav.logical_asset_id
		JOIN courses c ON c.id = ma.course_id
		LEFT JOIN course_revisions live ON live.id = c.live_revision_id AND live.course_id = c.id
		LEFT JOIN LATERAL (
			SELECT cr.title_ar, cr.title_en
			FROM course_revisions cr
			WHERE cr.course_id = c.id
			ORDER BY cr.revision_number DESC
			LIMIT 1
		) latest ON TRUE
		LEFT JOIN media_auto_enhancement_recovery recovery ON recovery.asset_version_id = mav.id
		WHERE ma.retired_at IS NULL AND (`+mediaFailureWhere("")+`)
		ORDER BY mav.created_at ASC, mav.id ASC
		LIMIT $1`, request.Limit, "media_processing_failure", "/admin/media/failures", request.Locale)
}

func (r *Repository) queryReportInbox(ctx context.Context, request InboxRequest) (InboxSection, error) {
	if r.learning == nil {
		return InboxSection{Key: "reported_content", Count: 0, Items: []InboxItem{}}, nil
	}
	page, err := r.learning.ListAdminReports(ctx, learning.AdminReportPageRequest{Page: 1, PageSize: request.Limit})
	if err != nil {
		return InboxSection{}, fmt.Errorf("querying content report inbox: %w", err)
	}
	count, err := r.learning.CountOpenAdminReports(ctx)
	if err != nil {
		return InboxSection{}, fmt.Errorf("counting content report inbox: %w", err)
	}
	now := time.Now().UTC()
	items := make([]InboxItem, 0, len(page.Items))
	for _, report := range page.Items {
		labelArabic, labelEnglish := report.Target.Labels()
		label := labelEnglish
		if request.Locale == identity.LocaleArabic {
			label = labelArabic
		}
		if strings.TrimSpace(label) == "" {
			label = report.ReporterDisplayName
		}
		items = append(items, InboxItem{
			Kind: "content_report", Label: label, AgeSeconds: ageSeconds(now, report.CreatedAt),
			CreatedAt: report.CreatedAt, Route: "/admin/reported-content", TargetID: report.ID,
		})
	}
	return InboxSection{Key: "reported_content", Count: count, Items: items}, nil
}

func (r *Repository) queryInboxRows(
	ctx context.Context,
	query string,
	limit int,
	kind string,
	route string,
	locale identity.Locale,
) (InboxSection, error) {
	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		return InboxSection{}, fmt.Errorf("querying %s inbox: %w", kind, err)
	}
	defer rows.Close()

	items := make([]InboxItem, 0, limit)
	count := 0
	for rows.Next() {
		var total int
		var targetID, labelArabic, labelEnglish string
		var createdAt time.Time
		var age int64
		if err := rows.Scan(&total, &targetID, &labelArabic, &labelEnglish, &createdAt, &age); err != nil {
			return InboxSection{}, fmt.Errorf("scanning %s inbox: %w", kind, err)
		}
		count = total
		label := labelEnglish
		if locale == identity.LocaleArabic {
			label = labelArabic
		}
		items = append(items, InboxItem{
			Kind: kind, Label: label, AgeSeconds: age, CreatedAt: createdAt, Route: route, TargetID: targetID,
		})
	}
	if err := rows.Err(); err != nil {
		return InboxSection{}, fmt.Errorf("iterating %s inbox: %w", kind, err)
	}
	return InboxSection{Key: inboxSectionKey(kind), Count: count, Items: items}, nil
}

func inboxSectionKey(kind string) string {
	switch kind {
	case "course_review":
		return "course_review"
	case "purchase_request":
		return "purchase_requests"
	case "access_invitation":
		return "access_invitations"
	case "subject_request":
		return "subject_requests"
	case "media_processing_failure":
		return "media_processing_failures"
	default:
		return kind
	}
}

func ageSeconds(now, createdAt time.Time) int64 {
	seconds := int64(now.Sub(createdAt).Seconds())
	if seconds < 0 {
		return 0
	}
	return seconds
}

var _ interface {
	GetInbox(context.Context, InboxRequest) (InboxResult, error)
} = (*Repository)(nil)
