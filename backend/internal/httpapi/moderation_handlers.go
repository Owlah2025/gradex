package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/learning"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

const moderationResolutionBodyLimit int64 = 4096

type adminReportResolutionBody struct {
	Action string `json:"action" binding:"required"`
	Reason string `json:"reason" binding:"required"`
}

type adminReportTargetResponse struct {
	Available       bool   `json:"available"`
	TargetType      string `json:"target_type"`
	TargetLabelAR   string `json:"target_label_ar,omitempty"`
	TargetLabelEN   string `json:"target_label_en,omitempty"`
	CourseLabelAR   string `json:"course_label_ar,omitempty"`
	CourseLabelEN   string `json:"course_label_en,omitempty"`
	CourseLifecycle string `json:"course_lifecycle,omitempty"`
	AccessSuspended bool   `json:"access_suspended"`
	Retired         bool   `json:"retired"`
}

type adminReportResponse struct {
	ID                  string                    `json:"id"`
	ReporterDisplayName string                    `json:"reporter_display_name,omitempty"`
	TargetType          string                    `json:"target_type"`
	Reason              string                    `json:"reason"`
	Explanation         string                    `json:"explanation"`
	CreatedAt           string                    `json:"created_at"`
	Status              string                    `json:"status"`
	Target              adminReportTargetResponse `json:"target"`
	ResolvedAt          *string                   `json:"resolved_at,omitempty"`
	ResolutionAction    *string                   `json:"resolution_action,omitempty"`
	ResolutionReason    *string                   `json:"resolution_reason,omitempty"`
}

type adminReportPageResponse struct {
	Items    []adminReportResponse `json:"items"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
	HasNext  bool                  `json:"has_next"`
}

type adminReportHandlers struct {
	reports moderationReportRepository
	catalog *catalog.Repository
}

func (h *adminReportHandlers) list(c *gin.Context) {
	page, pageSize := adminReportPagination(c)
	reports, err := h.reports.ListAdminReports(c.Request.Context(), learning.AdminReportPageRequest{Page: page, PageSize: pageSize})
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, adminReportPageResponse{
		Items:    adminReportResponses(reports.Items),
		Page:     reports.Page,
		PageSize: reports.PageSize,
		HasNext:  reports.HasNext,
	})
}

func (h *adminReportHandlers) detail(c *gin.Context) {
	report, err := h.reports.GetAdminReport(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.writeReportError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, newAdminReportResponse(report))
}

func (h *adminReportHandlers) resolve(c *gin.Context) {
	body := c.MustGet(strictJSONBodyContextKey).(*adminReportResolutionBody)
	action := learning.AdminReportAction(strings.TrimSpace(body.Action))
	if !validAdminReportAction(action) {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "INVALID_ACTION", Detail: "The resolution action is invalid", Location: problem.LocationBody, Parameter: "action",
		}))
		return
	}

	var execute learning.AdminReportActionExecutor
	if action == learning.AdminReportDelist {
		adminID := c.GetString(ctxUserIDKey)
		execute = func(ctx context.Context, tx pgx.Tx, target learning.AdminReportTarget) error {
			return h.delistCourse(ctx, tx, target, adminID)
		}
	}
	report, err := h.reports.ResolveAdminReport(c.Request.Context(), learning.AdminReportResolution{
		ReportID:       c.Param("id"),
		AdminAccountID: c.GetString(ctxUserIDKey),
		Action:         action,
		Reason:         body.Reason,
	}, execute)
	if err != nil {
		h.writeReportError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, newAdminReportResponse(report))
}

func (h *adminReportHandlers) delistCourse(ctx context.Context, tx pgx.Tx, target learning.AdminReportTarget, adminID string) error {
	if !target.Available || target.CourseID == "" {
		return learning.ErrAdminReportActionUnavailable
	}
	_, err := h.catalog.TransitionCourseLifecycleTx(ctx, tx, catalog.LifecycleMutation{
		CourseID:        target.CourseID,
		AdminAccountID:  adminID,
		ActorDescriptor: adminID,
		Target:          catalog.LifecycleDelisted,
	})
	return err
}

func (h *adminReportHandlers) writeReportError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, learning.ErrAdminReportNotFound):
		writeProblem(c, problem.NotFound())
	case errors.Is(err, learning.ErrAdminReportAlreadyResolved), errors.Is(err, learning.ErrAdminReportActionUnavailable):
		writeProblem(c, problem.StateConflict())
	case errors.Is(err, learning.ErrAdminReportResolutionInvalid):
		writeProblem(c, problem.ValidationFailed())
	case errors.Is(err, catalog.ErrCourseNotFound):
		writeProblem(c, problem.NotFound())
	default:
		lifecycle := &adminLifecycleHandlers{repo: h.catalog}
		var conflict *catalog.LifecycleConflictError
		if errors.As(err, &conflict) || errors.Is(err, catalog.ErrCourseHasAccess) || errors.Is(err, catalog.ErrPendingCandidate) {
			writeProblem(c, problem.StateConflict())
			return
		}
		if errors.Is(err, catalog.ErrInvalidLifecycle) || errors.Is(err, catalog.ErrReasonRequired) {
			lifecycle.handleLifecycleError(c, err)
			return
		}
		writeProblem(c, problem.Internal(""))
	}
}

func adminReportPagination(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	return page, pageSize
}

func validAdminReportAction(action learning.AdminReportAction) bool {
	return action == learning.AdminReportDismiss || action == learning.AdminReportDelist
}

func adminReportResponses(reports []learning.AdminReport) []adminReportResponse {
	responses := make([]adminReportResponse, 0, len(reports))
	for _, report := range reports {
		responses = append(responses, newAdminReportResponse(report))
	}
	return responses
}

func newAdminReportResponse(report learning.AdminReport) adminReportResponse {
	labelAR, labelEN := report.Target.Labels()
	response := adminReportResponse{
		ID:                  report.ID,
		ReporterDisplayName: report.ReporterDisplayName,
		TargetType:          string(report.TargetKind),
		Reason:              string(report.Reason),
		Explanation:         report.Explanation,
		CreatedAt:           report.CreatedAt.UTC().Format(time.RFC3339Nano),
		Status:              string(report.Status),
		Target: adminReportTargetResponse{
			Available:       report.Target.Available,
			TargetType:      string(report.Target.Kind),
			TargetLabelAR:   labelAR,
			TargetLabelEN:   labelEN,
			CourseLabelAR:   report.Target.CourseTitleAR,
			CourseLabelEN:   report.Target.CourseTitleEN,
			CourseLifecycle: report.Target.CourseLifecycle,
			AccessSuspended: report.Target.AccessSuspendedAt != nil,
			Retired:         report.Target.RetiredAt != nil,
		},
		ResolvedAt:       formatReportTime(report.ResolvedAt),
		ResolutionAction: report.ResolutionAction,
		ResolutionReason: report.ResolutionReason,
	}
	return response
}

func formatReportTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339Nano)
	return &formatted
}
