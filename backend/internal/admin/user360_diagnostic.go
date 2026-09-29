package admin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/entitlement"
	"github.com/Owlah2025/gradex/backend/internal/identity"
)

func (r *Repository) ListCourseOptions(ctx context.Context, req CourseOptionsRequest) ([]CourseOption, error) {
	if err := validateIdentityRead(req.Principal); err != nil {
		return nil, err
	}
	if !req.Locale.Valid() || len([]rune(req.Query)) > maxUser360QueryLength {
		return nil, ErrInvalidInput
	}
	limit := req.Limit
	if limit < 1 || limit > 50 {
		limit = 25
	}
	query := strings.TrimSpace(req.Query)
	rows, err := r.pool.Query(ctx, `
		SELECT c.id::text, COALESCE(live.title_ar, draft.title_ar, ''),
		       COALESCE(live.title_en, draft.title_en, ''), c.lifecycle::text
		  FROM courses c
		  LEFT JOIN course_revisions live ON live.id = c.live_revision_id
		  LEFT JOIN LATERAL (
			SELECT cr.title_ar, cr.title_en FROM course_revisions cr
			 WHERE cr.course_id = c.id ORDER BY cr.revision_number DESC LIMIT 1
		  ) draft ON TRUE
		 WHERE c.lifecycle <> 'ARCHIVED'
		   AND ($1 = '' OR LOWER(COALESCE(live.title_en, draft.title_en, '')) LIKE '%' || LOWER($1) || '%'
		        OR COALESCE(live.title_ar, draft.title_ar, '') LIKE '%' || $1 || '%')
		 ORDER BY c.updated_at DESC, c.id DESC LIMIT $2`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("querying admin course options: %w", err)
	}
	defer rows.Close()
	result := make([]CourseOption, 0, limit)
	for rows.Next() {
		var option CourseOption
		var titleAr, titleEn string
		if err := rows.Scan(&option.ID, &titleAr, &titleEn, &option.Lifecycle); err != nil {
			return nil, fmt.Errorf("scanning admin course option: %w", err)
		}
		option.Title = localizedTitle(req.Locale, titleAr, titleEn)
		result = append(result, option)
	}
	return result, rows.Err()
}

func (r *Repository) DiagnoseAccess(ctx context.Context, req AccessDiagnosticRequest) (AccessDiagnostic, error) {
	if err := validateIdentityRead(req.Principal); err != nil {
		return AccessDiagnostic{}, err
	}
	if !req.Locale.Valid() || req.Now.IsZero() {
		return AccessDiagnostic{}, ErrInvalidInput
	}
	if _, err := uuid.Parse(req.AccountID); err != nil {
		return AccessDiagnostic{}, ErrAccountNotFound
	}
	if _, err := uuid.Parse(req.CourseID); err != nil {
		return AccessDiagnostic{}, ErrCourseNotFound
	}
	var (
		status, courseLifecycle, courseTitleAr, courseTitleEn, lessonID, liveRevisionID string
		verified                                                                        bool
		courseSuspended                                                                 bool
		courseRetiredAt                                                                 *time.Time
	)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return AccessDiagnostic{}, fmt.Errorf("beginning access diagnostic: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = tx.QueryRow(ctx, `
		SELECT a.status::text, a.email_verified_at IS NOT NULL,
		       c.lifecycle::text, COALESCE(c.live_revision_id::text, ''), c.access_suspended_at IS NOT NULL, c.retired_at,
		       COALESCE(live.title_ar, draft.title_ar, ''), COALESCE(live.title_en, draft.title_en, ''),
		       COALESCE(target.lesson_identity_id::text, '')
		  FROM accounts a
		  CROSS JOIN courses c
		  LEFT JOIN course_revisions live ON live.id = c.live_revision_id
		  LEFT JOIN LATERAL (
			SELECT cr.title_ar, cr.title_en FROM course_revisions cr
			 WHERE cr.course_id = c.id ORDER BY cr.revision_number DESC LIMIT 1
		  ) draft ON TRUE
		  LEFT JOIN LATERAL (
			SELECT cl.lesson_identity_id
			  FROM course_revisions cr
			  JOIN course_sections cs ON cs.revision_id = cr.id AND cs.course_id = c.id
			  JOIN course_lessons cl ON cl.section_id = cs.id AND cl.course_id = c.id
			 WHERE cr.id = c.live_revision_id AND cr.state = 'APPROVED'
			 ORDER BY cs.position, cs.id, cl.position, cl.id LIMIT 1
		  ) target ON TRUE
		 WHERE a.id = $1::uuid AND c.id = $2::uuid`, req.AccountID, req.CourseID).Scan(
		&status, &verified, &courseLifecycle, &liveRevisionID, &courseSuspended, &courseRetiredAt,
		&courseTitleAr, &courseTitleEn, &lessonID,
	)
	if err == pgx.ErrNoRows {
		return AccessDiagnostic{}, ErrAccountNotFound
	}
	if err != nil {
		return AccessDiagnostic{}, fmt.Errorf("loading access diagnostic facts: %w", err)
	}

	entitlements, err := queryEntitlements(ctx, tx, req.Locale, req.AccountID, req.CourseID)
	if err != nil {
		return AccessDiagnostic{}, err
	}
	published := liveRevisionID != "" && lessonID != ""
	decision := entitlement.Decision{Reason: entitlement.ReasonDependency}
	courseWide := false
	if published {
		decision, courseWide = r.diagnosticDecision(ctx, req.AccountID, req.CourseID, lessonID, req.Now, entitlements)
	}
	primary := DiagnosticNotPublished
	if published {
		primary = diagnosticPrimary(decision, status, verified, courseSuspended, courseRetiredAt, entitlements, req.Now, courseWide)
	}
	facts := diagnosticFacts(status, verified, courseLifecycle, courseSuspended, courseRetiredAt, decision, entitlements, published)
	result := AccessDiagnostic{
		AccountID: req.AccountID, CourseID: req.CourseID, CourseTitle: localizedTitle(req.Locale, courseTitleAr, courseTitleEn),
		PrimaryReasonCode: primary, ReasonCode: primary, EvaluatorReason: string(decision.Reason), Allowed: decision.Allowed && primary == DiagnosticAllowed,
		Facts: facts, Entitlements: entitlements,
	}
	if err := WritePrivilegedReadAudit(ctx, tx, PrivilegedReadAudit{
		Principal: req.Principal, CorrelationID: req.CorrelationID, Action: ActionAccessDiagnosed,
		Module: catalog.AuditModuleIdentityAndAccess, TargetType: User360TargetType, TargetID: req.AccountID,
		Reason:   AuditReasonAccessDiagnosed,
		Metadata: map[string]any{"course_id": req.CourseID, "reason_code": primary, "evaluator_reason": string(decision.Reason)},
	}); err != nil {
		return AccessDiagnostic{}, fmt.Errorf("auditing access diagnostic: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return AccessDiagnostic{}, fmt.Errorf("committing access diagnostic: %w", err)
	}
	return result, nil
}

// diagnosticPrimary receives the retirement timestamp only as supporting fact data;
// the evaluator's retirement reason, not lifecycle context, determines the primary code.
func diagnosticPrimary(
	decision entitlement.Decision,
	status string,
	verified bool,
	suspended bool,
	_retiredAt *time.Time,
	entitlements []UserEntitlement,
	now time.Time,
	courseWide bool,
) string {
	if status == string(identity.StatusSuspended) {
		return DiagnosticAccountSuspended
	}
	if !verified {
		return DiagnosticAccountUnverified
	}
	if suspended && decision.Reason == entitlement.ReasonCourseSuspended {
		return DiagnosticCourseSuspended
	}
	switch decision.Reason {
	case entitlement.ReasonAllowed:
		return DiagnosticAllowed
	case entitlement.ReasonExpired:
		return "EXPIRED"
	case entitlement.ReasonRetired:
		return DiagnosticCourseRetired
	case entitlement.ReasonCourseSuspended:
		return DiagnosticCourseSuspended
	case entitlement.ReasonNoApplicableGrant:
		if len(entitlements) == 0 {
			return DiagnosticNoEntitlement
		}
		active := false
		revoked := false
		expired := false
		for _, item := range entitlements {
			if item.State == "REVOKED" || item.RevokedAt != nil {
				revoked = true
			}
			if item.State == "EXPIRED" || !now.Before(item.AccessEndsAt.UTC()) {
				expired = true
			}
			if item.State == "ACTIVE" && now.Before(item.AccessEndsAt.UTC()) {
				active = true
			}
		}
		if revoked && !active {
			return DiagnosticRevoked
		}
		if expired && !active {
			return "EXPIRED"
		}
		if active && !courseWide {
			return DiagnosticSectionOnly
		}
		return DiagnosticNoEntitlement
	default:
		return string(decision.Reason)
	}
}

func (r *Repository) diagnosticDecision(
	ctx context.Context,
	accountID, courseID, lessonID string,
	now time.Time,
	entitlements []UserEntitlement,
) (entitlement.Decision, bool) {
	classifications, err := r.evaluator.EvaluateCourseReads(ctx, accountID, now)
	if err != nil {
		return entitlement.Decision{Reason: entitlement.ReasonDependency}, false
	}
	if read, ok := classifications[courseID]; ok {
		return entitlement.Decision{Allowed: read.State == entitlement.ReadActive, Reason: read.Reason}, read.CourseWide
	}
	return r.evaluator.Evaluate(ctx, accountID, lessonID, now), courseWideFromEntitlements(entitlements)
}

func courseWideFromEntitlements(entitlements []UserEntitlement) bool {
	for _, item := range entitlements {
		if item.ScopeKind == string(entitlement.ScopeCourse) && item.State != "REVOKED" {
			return true
		}
	}
	return false
}

func diagnosticFacts(
	status string,
	verified bool,
	lifecycle string,
	suspended bool,
	retiredAt *time.Time,
	decision entitlement.Decision,
	entitlements []UserEntitlement,
	published bool,
) []DiagnosticFact {
	facts := []DiagnosticFact{
		{Code: "ACCOUNT_STATUS", Value: status},
		{Code: "EMAIL_VERIFIED", Value: fmt.Sprintf("%t", verified)},
		{Code: "COURSE_LIFECYCLE", Value: lifecycle},
		{Code: "ENTITLEMENT_COUNT", Value: fmt.Sprintf("%d", len(entitlements))},
	}
	if published {
		facts = append(facts, DiagnosticFact{Code: "EVALUATOR_REASON", Value: string(decision.Reason)})
	} else {
		facts = append(facts, DiagnosticFact{Code: "COURSE_PUBLICATION", Value: DiagnosticNotPublished})
	}
	if suspended {
		facts = append(facts, DiagnosticFact{Code: "COURSE_ACCESS_SUSPENDED", Value: "true"})
	}
	if retiredAt != nil {
		facts = append(facts, DiagnosticFact{Code: "COURSE_RETIRED", Value: retiredAt.UTC().Format(time.RFC3339)})
	}
	return facts
}
