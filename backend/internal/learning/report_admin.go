package learning

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	defaultAdminReportPageSize = 20
	maxAdminReportPageSize     = 100
)

var (
	ErrAdminReportNotFound          = errors.New("admin report not found")
	ErrAdminReportAlreadyResolved   = errors.New("admin report is already resolved")
	ErrAdminReportResolutionInvalid = errors.New("admin report resolution is invalid")
	ErrAdminReportActionUnavailable = errors.New("admin report action is unavailable")
)

type AdminReportStatus string

const (
	AdminReportOpen     AdminReportStatus = "OPEN"
	AdminReportResolved AdminReportStatus = "RESOLVED"
)

type AdminReportAction string

const (
	AdminReportDismiss AdminReportAction = "DISMISS"
	AdminReportDelist  AdminReportAction = "DELIST"
)

func (a AdminReportAction) valid() bool {
	return a == AdminReportDismiss || a == AdminReportDelist
}

func (a AdminReportAction) storedValue() string {
	if a == AdminReportDelist {
		return "DELISTED"
	}
	return "DISMISSED"
}

type AdminReportTarget struct {
	Kind              ReportTargetKind
	CourseID          string
	Available         bool
	CourseTitleAR     string
	CourseTitleEN     string
	LessonTitleAR     string
	LessonTitleEN     string
	FileNameAR        string
	FileNameEN        string
	CourseLifecycle   string
	AccessSuspendedAt *time.Time
	RetiredAt         *time.Time
}

func (t AdminReportTarget) Labels() (string, string) {
	switch t.Kind {
	case ReportTargetCourse:
		return t.CourseTitleAR, t.CourseTitleEN
	case ReportTargetLesson, ReportTargetVideo:
		return t.LessonTitleAR, t.LessonTitleEN
	default:
		return t.FileNameAR, t.FileNameEN
	}
}

type AdminReport struct {
	ID                  string
	ReporterDisplayName string
	TargetKind          ReportTargetKind
	Reason              ReportReason
	Explanation         string
	CreatedAt           time.Time
	Status              AdminReportStatus
	ResolvedAt          *time.Time
	ResolvedByAccountID *string
	ResolutionAction    *string
	ResolutionReason    *string
	Target              AdminReportTarget
}

type AdminReportPageRequest struct {
	Page     int
	PageSize int
}

type AdminReportPage struct {
	Items    []AdminReport
	Page     int
	PageSize int
	HasNext  bool
}

type AdminReportResolution struct {
	ReportID       string
	AdminAccountID string
	Action         AdminReportAction
	Reason         string
}

// AdminReportActionExecutor runs the existing canonical target command inside the report
// transaction. A nil executor is the valid dismissal path and leaves the target unchanged.
type AdminReportActionExecutor func(context.Context, pgx.Tx, AdminReportTarget) error

const adminReportSelect = `
SELECT r.id::text,
       coalesce(reporter.display_name, ''),
       r.target_kind,
       r.reason,
       coalesce(r.explanation, ''),
       r.created_at,
       r.resolved_at,
       r.resolved_by_account_id::text,
       r.resolution_action,
       r.resolution_reason,
       target.course_id,
       target.course_title_ar,
       target.course_title_en,
       target.lesson_title_ar,
       target.lesson_title_en,
       target.file_name_ar,
       target.file_name_en,
       target.lifecycle,
       target.access_suspended_at,
       target.retired_at
FROM content_reports r
LEFT JOIN accounts reporter ON reporter.id = r.reporter_account_id
LEFT JOIN LATERAL (
    SELECT c.id::text AS course_id,
           cr.title_ar AS course_title_ar,
           cr.title_en AS course_title_en,
           cl.title_ar AS lesson_title_ar,
           cl.title_en AS lesson_title_en,
           lf.display_name_ar AS file_name_ar,
           lf.display_name_en AS file_name_en,
           c.lifecycle::text AS lifecycle,
           c.access_suspended_at,
           c.retired_at
    FROM courses c
    JOIN course_revisions cr ON cr.course_id = c.id
    LEFT JOIN course_sections cs
      ON cs.revision_id = cr.id AND cs.course_id = c.id
    LEFT JOIN course_lessons cl
      ON cl.section_id = cs.id AND cl.course_id = c.id
    LEFT JOIN lesson_files lf ON lf.lesson_id = cl.id
    WHERE (
        (r.target_kind = 'COURSE' AND c.id = r.target_id AND cr.id = r.target_revision_ref)
        OR (r.target_kind = 'LESSON' AND cl.lesson_identity_id = r.target_id AND cr.id = r.target_revision_ref)
        OR (r.target_kind = 'VIDEO' AND cl.lesson_identity_id = r.target_id AND cl.video_asset_version_id = r.target_revision_ref)
        OR (r.target_kind = 'RESOURCE' AND cl.lesson_identity_id = r.target_id AND lf.kind = 'RESOURCE' AND lf.asset_version_id = r.target_revision_ref)
        OR (r.target_kind = 'LAB_MATERIAL' AND cl.lesson_identity_id = r.target_id AND lf.kind = 'LAB_MATERIAL' AND lf.asset_version_id = r.target_revision_ref)
    )
    ORDER BY (c.live_revision_id = cr.id) DESC, cr.revision_number DESC, c.id
    LIMIT 1
) target ON TRUE
`

const adminReportListQuery = adminReportSelect + `
WHERE r.resolved_at IS NULL
ORDER BY r.created_at ASC, r.id ASC
LIMIT $1 OFFSET $2
`

const adminReportDetailQuery = adminReportSelect + `
WHERE r.id = $1::uuid
`

const adminReportLockedDetailQuery = adminReportDetailQuery + `
FOR UPDATE OF r
`

func (r *Repository) ListAdminReports(ctx context.Context, request AdminReportPageRequest) (AdminReportPage, error) {
	if r == nil || r.pool == nil {
		return AdminReportPage{}, errors.New("learning database is required")
	}
	request = normalizeAdminReportPageRequest(request)
	offset := (request.Page - 1) * request.PageSize
	rows, err := r.pool.Query(ctx, adminReportListQuery, request.PageSize+1, offset)
	if err != nil {
		return AdminReportPage{}, fmt.Errorf("querying admin report queue: %w", err)
	}
	defer rows.Close()
	items, err := scanAdminReports(rows)
	if err != nil {
		return AdminReportPage{}, err
	}
	hasNext := len(items) > request.PageSize
	if hasNext {
		items = items[:request.PageSize]
	}
	return AdminReportPage{Items: items, Page: request.Page, PageSize: request.PageSize, HasNext: hasNext}, nil
}

func (r *Repository) GetAdminReport(ctx context.Context, reportID string) (AdminReport, error) {
	if r == nil || r.pool == nil {
		return AdminReport{}, errors.New("learning database is required")
	}
	return readAdminReport(ctx, r.pool, adminReportDetailQuery, reportID)
}

func (r *Repository) ResolveAdminReport(ctx context.Context, request AdminReportResolution, execute AdminReportActionExecutor) (AdminReport, error) {
	request.Reason = strings.TrimSpace(request.Reason)
	if err := validateAdminReportResolution(request); err != nil {
		return AdminReport{}, err
	}
	if r == nil || r.pool == nil {
		return AdminReport{}, errors.New("learning database is required")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return AdminReport{}, fmt.Errorf("beginning admin report resolution: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	report, err := readOpenAdminReport(ctx, tx, request.ReportID)
	if err != nil {
		return AdminReport{}, err
	}
	report, err = executeAdminReportAction(ctx, tx, report, execute)
	if err != nil {
		return AdminReport{}, err
	}

	resolvedAt := time.Now().UTC()
	if err := persistAdminReportResolution(ctx, tx, report, request, resolvedAt); err != nil {
		return AdminReport{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminReport{}, fmt.Errorf("committing admin report resolution: %w", err)
	}
	return resolvedAdminReport(report, request, resolvedAt), nil
}

func readOpenAdminReport(ctx context.Context, tx pgx.Tx, reportID string) (AdminReport, error) {
	report, err := readAdminReport(ctx, tx, adminReportLockedDetailQuery, reportID)
	if err != nil {
		return AdminReport{}, err
	}
	if report.Status != AdminReportOpen {
		return AdminReport{}, ErrAdminReportAlreadyResolved
	}
	return report, nil
}

func executeAdminReportAction(ctx context.Context, tx pgx.Tx, report AdminReport, execute AdminReportActionExecutor) (AdminReport, error) {
	if execute == nil {
		return report, nil
	}
	if err := execute(ctx, tx, report.Target); err != nil {
		return AdminReport{}, err
	}
	return readAdminReport(ctx, tx, adminReportLockedDetailQuery, report.ID)
}

func persistAdminReportResolution(ctx context.Context, tx pgx.Tx, report AdminReport, request AdminReportResolution, resolvedAt time.Time) error {
	storedAction := request.Action.storedValue()
	if _, err := tx.Exec(ctx, `
		UPDATE content_reports
		SET resolved_at = $1,
		    resolved_by_account_id = $2::uuid,
		    resolution_action = $3,
		    resolution_reason = $4
		WHERE id = $5::uuid AND resolved_at IS NULL
	`, resolvedAt, request.AdminAccountID, storedAction, request.Reason, request.ReportID); err != nil {
		return fmt.Errorf("resolving content report: %w", err)
	}
	return catalog.WriteAuditEvent(ctx, tx, catalog.AuditEvent{
		ActorAccountID:  &request.AdminAccountID,
		ActorRole:       "ADMIN",
		ActorDescriptor: request.AdminAccountID,
		Action:          "REPORT_RESOLVED",
		Module:          catalog.AuditModuleModeration,
		TargetType:      "CONTENT_REPORT",
		TargetID:        request.ReportID,
		Reason:          request.Reason,
		Metadata: map[string]any{
			"resolution_action": storedAction,
			"target_kind":       string(report.Target.Kind),
		},
	})
}

func resolvedAdminReport(report AdminReport, request AdminReportResolution, resolvedAt time.Time) AdminReport {
	storedAction := request.Action.storedValue()
	report.Status = AdminReportResolved
	report.ResolvedAt = &resolvedAt
	report.ResolvedByAccountID = &request.AdminAccountID
	report.ResolutionAction = &storedAction
	report.ResolutionReason = &request.Reason
	return report
}

func validateAdminReportResolution(request AdminReportResolution) error {
	if strings.TrimSpace(request.ReportID) == "" || strings.TrimSpace(request.AdminAccountID) == "" || !request.Action.valid() {
		return ErrAdminReportResolutionInvalid
	}
	if request.Reason == "" || len(request.Reason) > 2000 {
		return ErrAdminReportResolutionInvalid
	}
	return nil
}

func normalizeAdminReportPageRequest(request AdminReportPageRequest) AdminReportPageRequest {
	if request.Page < 1 {
		request.Page = 1
	}
	if request.PageSize < 1 || request.PageSize > maxAdminReportPageSize {
		request.PageSize = defaultAdminReportPageSize
	}
	return request
}

type adminReportRowScanner interface {
	Scan(...any) error
}

func scanAdminReports(rows pgx.Rows) ([]AdminReport, error) {
	items := make([]AdminReport, 0)
	for rows.Next() {
		item, err := scanAdminReport(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading admin report queue: %w", err)
	}
	return items, nil
}

type adminReportQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readAdminReport(ctx context.Context, queryer adminReportQueryer, query, reportID string) (AdminReport, error) {
	report, err := scanAdminReport(queryer.QueryRow(ctx, query, reportID))
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminReport{}, ErrAdminReportNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "22P02" {
		return AdminReport{}, ErrAdminReportNotFound
	}
	return report, err
}

type adminReportTargetScan struct {
	courseID, courseTitleAR, courseTitleEN *string
	lessonTitleAR, lessonTitleEN           *string
	fileNameAR, fileNameEN                 *string
	lifecycle                              *string
	accessSuspendedAt, retiredAt           *time.Time
}

func scanAdminReport(row adminReportRowScanner) (AdminReport, error) {
	var (
		report              AdminReport
		kind                ReportTargetKind
		reporterDisplayName string
		explanation         string
		resolvedBy, action  *string
		resolutionReason    *string
		target              adminReportTargetScan
	)
	err := row.Scan(
		&report.ID, &reporterDisplayName, &kind, &report.Reason, &explanation, &report.CreatedAt,
		&report.ResolvedAt, &resolvedBy, &action, &resolutionReason,
		&target.courseID, &target.courseTitleAR, &target.courseTitleEN,
		&target.lessonTitleAR, &target.lessonTitleEN, &target.fileNameAR,
		&target.fileNameEN, &target.lifecycle, &target.accessSuspendedAt,
		&target.retiredAt,
	)
	if err != nil {
		return AdminReport{}, fmt.Errorf("scanning admin report: %w", err)
	}
	report.TargetKind = kind
	report.ReporterDisplayName = reporterDisplayName
	report.Explanation = explanation
	report.CreatedAt = report.CreatedAt.UTC()
	report.ResolvedAt = utcReportTime(report.ResolvedAt)
	report.ResolvedByAccountID = resolvedBy
	report.ResolutionAction = action
	report.ResolutionReason = resolutionReason
	report.Status = AdminReportOpen
	if report.ResolvedAt != nil {
		report.Status = AdminReportResolved
	}
	report.Target = adminReportTarget(kind, target)
	return report, nil
}

func adminReportTarget(kind ReportTargetKind, scan adminReportTargetScan) AdminReportTarget {
	target := AdminReportTarget{Kind: kind, Available: scan.courseID != nil}
	if scan.courseID != nil {
		target.CourseID = *scan.courseID
	}
	if scan.courseTitleAR != nil {
		target.CourseTitleAR = *scan.courseTitleAR
	}
	if scan.courseTitleEN != nil {
		target.CourseTitleEN = *scan.courseTitleEN
	}
	if scan.lessonTitleAR != nil {
		target.LessonTitleAR = *scan.lessonTitleAR
	}
	if scan.lessonTitleEN != nil {
		target.LessonTitleEN = *scan.lessonTitleEN
	}
	if scan.fileNameAR != nil {
		target.FileNameAR = *scan.fileNameAR
	}
	if scan.fileNameEN != nil {
		target.FileNameEN = *scan.fileNameEN
	}
	if scan.lifecycle != nil {
		target.CourseLifecycle = *scan.lifecycle
	}
	target.AccessSuspendedAt = utcReportTime(scan.accessSuspendedAt)
	target.RetiredAt = utcReportTime(scan.retiredAt)
	return target
}

func utcReportTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}
