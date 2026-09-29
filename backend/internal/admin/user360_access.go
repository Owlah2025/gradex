package admin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/identity"
)

func queryEntitlements(ctx context.Context, tx pgx.Tx, locale identity.Locale, accountID string, courseID ...string) ([]UserEntitlement, error) {
	courseFilter := ""
	limitPlaceholder := "$3"
	queryArgs := []any{accountID, string(locale), user360EntitlementLimit}
	if len(courseID) > 0 && strings.TrimSpace(courseID[0]) != "" {
		courseFilter = " AND e.course_id = $3::uuid"
		limitPlaceholder = "$4"
		queryArgs = []any{accountID, string(locale), courseID[0], user360EntitlementLimit}
	}
	rows, err := tx.Query(ctx, `
		SELECT e.id::text, e.course_id::text, COALESCE(live.title_ar, draft.title_ar, ''),
		       COALESCE(live.title_en, draft.title_en, ''), e.scope_kind, e.scope_id::text,
		       e.grant_source, e.original_access_ends_at, e.access_ends_at, e.revoked_at,
		       e.state, e.revision, e.created_at,
		       CASE e.grant_source
		         WHEN 'MANUAL_INVITATION' THEN COALESCE(NULLIF(i.external_reference, ''), '')
		         WHEN 'PURCHASE_REQUEST' THEN COALESCE(pr.reference_code, '')
		         WHEN 'BUNDLE_PURCHASE' THEN COALESCE(CASE WHEN $2 = 'ar' THEN b.title_ar ELSE b.title_en END, '')
		         ELSE ''
		       END,
		       COALESCE(grantor.display_name, grantor.email, '')
		  FROM entitlements e
		  JOIN courses c ON c.id = e.course_id
		  LEFT JOIN course_revisions live ON live.id = c.live_revision_id
		  LEFT JOIN LATERAL (
			SELECT cr.title_ar, cr.title_en FROM course_revisions cr
			 WHERE cr.course_id = c.id ORDER BY cr.revision_number DESC LIMIT 1
		  ) draft ON TRUE
		  LEFT JOIN course_access_invitations i ON i.id = e.source_invitation_id
		  LEFT JOIN purchase_requests pr ON pr.id = e.source_purchase_request_id
		  LEFT JOIN bundles b ON b.id = pr.bundle_id
		  LEFT JOIN accounts grantor ON grantor.id = COALESCE(i.created_by_account_id, pr.payment_confirmed_by_account_id)
		 WHERE e.student_account_id = $1::uuid`+courseFilter+`
		 ORDER BY e.created_at DESC, e.id DESC
		 LIMIT `+limitPlaceholder, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying account entitlements: %w", err)
	}
	defer rows.Close()

	result := make([]UserEntitlement, 0, user360EntitlementLimit)
	ids := make([]string, 0, user360EntitlementLimit)
	now := time.Now().UTC()
	for rows.Next() {
		var item UserEntitlement
		var titleAr, titleEn string
		if err := rows.Scan(
			&item.ID, &item.CourseID, &titleAr, &titleEn, &item.ScopeKind, &item.ScopeID,
			&item.GrantSource, &item.OriginalAccessEndsAt, &item.AccessEndsAt, &item.RevokedAt,
			&item.State, &item.Revision, &item.GrantedAt, &item.SourceReferenceLabel, &item.GrantedByDisplayName,
		); err != nil {
			return nil, fmt.Errorf("scanning account entitlement: %w", err)
		}
		item.CourseTitle = localizedTitle(locale, titleAr, titleEn)
		if item.State == "ACTIVE" && !now.Before(item.AccessEndsAt.UTC()) {
			item.State = "EXPIRED"
		}
		item.Adjustments = []UserEntitlementAdjustment{}
		result = append(result, item)
		ids = append(ids, item.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating account entitlements: %w", err)
	}
	if len(ids) == 0 {
		return result, nil
	}
	adjustments, err := queryEntitlementAdjustments(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	for index := range result {
		result[index].Adjustments = adjustments[result[index].ID]
	}
	return result, nil
}

func queryEntitlementAdjustments(ctx context.Context, tx pgx.Tx, ids []string) (map[string][]UserEntitlementAdjustment, error) {
	rows, err := tx.Query(ctx, `
		SELECT id::text, entitlement_id::text, old_access_ends_at, new_access_ends_at,
		       reason, actor_account_id::text, support_reference, adjusted_at
		  FROM entitlement_adjustments
		 WHERE entitlement_id = ANY($1::uuid[])
		 ORDER BY adjusted_at ASC, id ASC`, ids)
	if err != nil {
		return nil, fmt.Errorf("querying entitlement adjustments: %w", err)
	}
	defer rows.Close()
	result := make(map[string][]UserEntitlementAdjustment, len(ids))
	for rows.Next() {
		var adjustment UserEntitlementAdjustment
		if err := rows.Scan(&adjustment.ID, &adjustment.EntitlementID, &adjustment.OldAccessEndsAt,
			&adjustment.NewAccessEndsAt, &adjustment.Reason, &adjustment.ActorAccountID,
			&adjustment.SupportReference, &adjustment.AdjustedAt); err != nil {
			return nil, fmt.Errorf("scanning entitlement adjustment: %w", err)
		}
		result[adjustment.EntitlementID] = append(result[adjustment.EntitlementID], adjustment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating entitlement adjustments: %w", err)
	}
	for _, id := range ids {
		if result[id] == nil {
			result[id] = []UserEntitlementAdjustment{}
		}
	}
	return result, nil
}

func queryInvitations(ctx context.Context, tx pgx.Tx, locale identity.Locale, email string) ([]UserInvitation, error) {
	rows, err := tx.Query(ctx, `
		SELECT i.id::text, i.course_id::text, COALESCE(live.title_ar, draft.title_ar, ''),
		       COALESCE(live.title_en, draft.title_en, ''), i.state,
		       COALESCE(author.display_name, author.email, ''), i.created_at, i.decided_at,
		       i.accepted_at, COALESCE(i.external_reference, '')
		  FROM course_access_invitations i
		  JOIN courses c ON c.id = i.course_id
		  LEFT JOIN course_revisions live ON live.id = c.live_revision_id
		  LEFT JOIN LATERAL (
			SELECT cr.title_ar, cr.title_en FROM course_revisions cr
			 WHERE cr.course_id = c.id ORDER BY cr.revision_number DESC LIMIT 1
		  ) draft ON TRUE
		  LEFT JOIN accounts author ON author.id = i.created_by_account_id
		 WHERE i.normalized_email = $1
		 ORDER BY i.created_at DESC, i.id DESC
		 LIMIT $2`, strings.ToLower(strings.TrimSpace(email)), user360HistoryLimit)
	if err != nil {
		return nil, fmt.Errorf("querying account invitations: %w", err)
	}
	defer rows.Close()
	result := make([]UserInvitation, 0, user360HistoryLimit)
	for rows.Next() {
		var item UserInvitation
		var titleAr, titleEn string
		if err := rows.Scan(&item.ID, &item.CourseID, &titleAr, &titleEn, &item.State,
			&item.CreatedByName, &item.CreatedAt, &item.DecidedAt, &item.AcceptedAt, &item.ExternalReference); err != nil {
			return nil, fmt.Errorf("scanning account invitation: %w", err)
		}
		item.CourseTitle = localizedTitle(locale, titleAr, titleEn)
		result = append(result, item)
	}
	return result, rows.Err()
}

func queryPurchaseRequests(ctx context.Context, tx pgx.Tx, locale identity.Locale, accountID, email string) ([]UserPurchaseRequest, error) {
	rows, err := tx.Query(ctx, `
		SELECT pr.id::text, pr.reference_code, COALESCE(live.title_ar, draft.title_ar, ''),
		       COALESCE(live.title_en, draft.title_en, ''),
		       COALESCE(CASE WHEN $3 = 'ar' THEN b.title_ar ELSE b.title_en END, ''),
		       pr.state, pr.requested_at, pr.access_granted_at
		  FROM purchase_requests pr
		  LEFT JOIN courses c ON c.id = pr.course_id
		  LEFT JOIN course_revisions live ON live.id = c.live_revision_id
		  LEFT JOIN LATERAL (
			SELECT cr.title_ar, cr.title_en FROM course_revisions cr
			 WHERE cr.course_id = c.id ORDER BY cr.revision_number DESC LIMIT 1
		  ) draft ON TRUE
		  LEFT JOIN bundles b ON b.id = pr.bundle_id
		 WHERE pr.requester_account_id = $1::uuid OR pr.normalized_email = $2
		 ORDER BY pr.requested_at DESC, pr.id DESC
		 LIMIT $4`, accountID, strings.ToLower(strings.TrimSpace(email)), string(locale), user360HistoryLimit)
	if err != nil {
		return nil, fmt.Errorf("querying account purchase requests: %w", err)
	}
	defer rows.Close()
	result := make([]UserPurchaseRequest, 0, user360HistoryLimit)
	for rows.Next() {
		var item UserPurchaseRequest
		var titleAr, titleEn string
		if err := rows.Scan(&item.ID, &item.Reference, &titleAr, &titleEn, &item.BundleTitle,
			&item.State, &item.RequestedAt, &item.AccessGrantedAt); err != nil {
			return nil, fmt.Errorf("scanning account purchase request: %w", err)
		}
		item.CourseTitle = localizedTitle(locale, titleAr, titleEn)
		result = append(result, item)
	}
	return result, rows.Err()
}
