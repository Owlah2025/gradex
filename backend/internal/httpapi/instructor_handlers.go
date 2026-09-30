package httpapi

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

const announcementBodyLimit = 16 << 10

type courseAnnouncementBody struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func (h *authoringHandlers) instructorDashboard(c *gin.Context) {
	response, err := h.repo.ReadInstructorDashboard(c.Request.Context(), c.GetString(ctxUserIDKey), nowUTC())
	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, response)
}

func (h *authoringHandlers) getCourseAnalytics(c *gin.Context) {
	response, err := h.repo.ReadCourseAnalytics(c.Request.Context(), catalog.CourseAnalyticsRequest{
		CourseID: c.Param("id"), OwnerAccountID: c.GetString(ctxUserIDKey), Now: nowUTC(),
	})
	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, response)
}

func (h *authoringHandlers) listOwnedAnnouncements(c *gin.Context) {
	items, err := h.repo.ListOwnedCourseAnnouncements(c.Request.Context(), c.Param("id"), c.GetString(ctxUserIDKey))
	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, items)
}

func (h *authoringHandlers) createAnnouncement(c *gin.Context) {
	body := c.MustGet(strictJSONBodyContextKey).(*courseAnnouncementBody)
	body.Title = strings.TrimSpace(body.Title)
	body.Body = strings.TrimSpace(body.Body)
	violations := make([]problem.Violation, 0, 2)
	if utf8.RuneCountInString(body.Title) == 0 || utf8.RuneCountInString(body.Title) > 140 {
		violations = append(violations, problem.Violation{
			Code: "ANNOUNCEMENT_TITLE_LENGTH", Location: problem.LocationBody, Pointer: "/title",
			Detail: "title must contain between 1 and 140 characters",
		})
	}
	if utf8.RuneCountInString(body.Body) == 0 || utf8.RuneCountInString(body.Body) > 4000 {
		violations = append(violations, problem.Violation{
			Code: "ANNOUNCEMENT_BODY_LENGTH", Location: problem.LocationBody, Pointer: "/body",
			Detail: "body must contain between 1 and 4000 characters",
		})
	}
	if len(violations) > 0 {
		writeProblem(c, problem.ValidationFailed().WithViolations(violations...))
		return
	}
	announcement, err := h.repo.CreateCourseAnnouncement(c.Request.Context(), catalog.CreateAnnouncementRequest{
		CourseID: c.Param("id"), AuthorAccountID: c.GetString(ctxUserIDKey),
		Title: body.Title, Body: body.Body, Now: nowUTC(),
	})
	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, announcement)
}

func (h *authoringHandlers) requireAnnouncementRateDecision(c *gin.Context) {
	if h.limiter == nil {
		c.Next()
		return
	}
	decision := h.limiter.Decide(c.Request.Context(), h.announcementPolicy, ratelimit.Input{
		Identifier: c.GetString(ctxUserIDKey), ClientIP: c.ClientIP(),
	})
	if decision.Allowed {
		c.Next()
		return
	}
	if seconds := int(math.Ceil(decision.RetryAfter.Seconds())); seconds > 0 {
		c.Header("Retry-After", strconv.Itoa(seconds))
	}
	if decision.Outcome == ratelimit.OutcomeDenied || decision.Outcome == ratelimit.OutcomeFallbackDenied {
		writeProblem(c, problem.RateLimited())
		return
	}
	writeProblem(c, problem.RateLimitingUnavailable())
}

func nowUTC() time.Time { return time.Now().UTC() }
