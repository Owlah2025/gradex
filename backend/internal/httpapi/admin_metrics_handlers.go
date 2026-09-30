package httpapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	adminread "github.com/Owlah2025/gradex/backend/internal/admin"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

type adminMetricsHandlers struct {
	service adminread.AdminMetricsService
}

func (h *adminMetricsHandlers) getOverview(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	locale, ok := requestedLocale(c.GetHeader("Accept-Language"))
	if !ok {
		writeProblem(c, problem.ValidationFailed())
		return
	}
	if _, exists := c.GetQuery("window"); exists {
		writeProblem(c, problem.ValidationFailed())
		return
	}
	result, err := h.service.GetMetricsOverview(c.Request.Context(), adminread.MetricsOverviewRequest{
		Principal: principal, Locale: locale,
	})
	if err != nil {
		writeAdminReadError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *adminMetricsHandlers) listCourses(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	page, limit, ok := parseAdminPagination(c)
	if !ok {
		return
	}
	sort, direction, ok := parseMetricsSort(c, []string{
		"title", "instructor", "lifecycle", "enrolled", "started", "learning_active_7d", "average_progress", "completed",
	}, "title")
	if !ok {
		return
	}
	locale, ok := requestedLocale(c.GetHeader("Accept-Language"))
	if !ok {
		writeProblem(c, problem.ValidationFailed())
		return
	}
	result, err := h.service.ListMetricsCourses(c.Request.Context(), adminread.MetricsCoursesRequest{
		Principal: principal, Locale: locale, Sort: sort, Direction: direction, Page: page, Limit: limit,
	})
	if err != nil {
		writeAdminReadError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *adminMetricsHandlers) listInstructors(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	page, limit, ok := parseAdminPagination(c)
	if !ok {
		return
	}
	sort, direction, ok := parseMetricsSort(c, []string{
		"name", "published_courses", "total_enrollments", "learning_active_students_7d",
	}, "name")
	if !ok {
		return
	}
	result, err := h.service.ListMetricsInstructors(c.Request.Context(), adminread.MetricsInstructorsRequest{
		Principal: principal, Sort: sort, Direction: direction, Page: page, Limit: limit,
	})
	if err != nil {
		writeAdminReadError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *adminMetricsHandlers) getInbox(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	locale, ok := requestedLocale(c.GetHeader("Accept-Language"))
	if !ok {
		writeProblem(c, problem.ValidationFailed())
		return
	}
	limit, ok := parsePositiveQuery(c, "limit", 5)
	if !ok || limit > 10 {
		writeProblem(c, problem.ValidationFailed())
		return
	}
	result, err := h.service.GetInbox(c.Request.Context(), adminread.InboxRequest{
		Principal: principal, Locale: locale, Limit: limit,
	})
	if err != nil {
		writeAdminReadError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *adminMetricsHandlers) getSearchMetrics(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	if _, exists := c.GetQuery("window"); exists {
		writeProblem(c, problem.ValidationFailed())
		return
	}
	result, err := h.service.GetSearchMetrics(c.Request.Context(), adminread.SearchMetricsRequest{Principal: principal})
	if err != nil {
		writeAdminReadError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}

func parseMetricsSort(c *gin.Context, allowed []string, fallback string) (string, string, bool) {
	sort := strings.TrimSpace(c.Query("sort"))
	if sort == "" {
		sort = fallback
	}
	validSort := false
	for _, candidate := range allowed {
		if sort == candidate {
			validSort = true
			break
		}
	}
	direction := strings.ToLower(strings.TrimSpace(c.Query("direction")))
	if direction == "" {
		direction = "asc"
	}
	if !validSort || (direction != "asc" && direction != "desc") {
		writeProblem(c, problem.ValidationFailed())
		return "", "", false
	}
	return sort, direction, true
}
