package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	adminread "github.com/Owlah2025/gradex/backend/internal/admin"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/requestid"
)

const (
	adminNoteBodyLimit     int64 = 8 * 1024
	adminMutationBodyLimit int64 = 4 * 1024
)

type adminNoteBody struct {
	Body string `json:"body"`
}

type sessionRevocationBody struct {
	Reason string `json:"reason"`
}

func (h *adminHandlers) getUser360(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok || h.userService == nil {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	accountID := c.Param("accountId")
	locale, ok := requestedLocale(c.GetHeader("Accept-Language"))
	if !ok {
		writeProblem(c, problem.ValidationFailed())
		return
	}
	result, err := h.userService.GetUser360(c.Request.Context(), adminread.User360Request{
		Principal: principal, CorrelationID: requestid.FromContext(c.Request.Context()),
		Locale: locale, AccountID: accountID,
	})
	if err != nil {
		writeAdminUserError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}

func (h *adminHandlers) listNotes(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok || h.userService == nil {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	limit, ok := parsePositiveQuery(c, "limit", 20)
	if !ok || limit > 50 {
		writeProblem(c, problem.ValidationFailed())
		return
	}
	result, err := h.userService.ListNotes(c.Request.Context(), adminread.NoteListRequest{
		Principal: principal, AccountID: c.Param("accountId"), Limit: limit,
	})
	if err != nil {
		writeAdminUserError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}

func (h *adminHandlers) addNote(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok || h.userService == nil {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	body := c.MustGet(strictJSONBodyContextKey).(*adminNoteBody)
	note, err := h.userService.AddNote(c.Request.Context(), adminread.AddNoteRequest{
		Principal: principal, CorrelationID: requestid.FromContext(c.Request.Context()),
		AccountID: c.Param("accountId"), Body: body.Body,
	})
	if err != nil {
		writeAdminUserError(c, err)
		return
	}
	c.JSON(http.StatusCreated, note)
}

func (h *adminHandlers) revokeSessions(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok || h.userService == nil {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	session, ok := sessionFromContext(c)
	if !ok {
		writeProblem(c, problem.Unauthenticated())
		return
	}
	body := c.MustGet(strictJSONBodyContextKey).(*sessionRevocationBody)
	result, err := h.userService.RevokeAccountSessions(c.Request.Context(), adminread.SessionRevocationRequest{
		Principal: principal, ActorSession: session, RecentAuthWindow: cAdminRecentAuthWindow(h),
		CorrelationID: requestid.FromContext(c.Request.Context()), AccountID: c.Param("accountId"),
		Reason: body.Reason, Now: time.Now().UTC(),
	})
	if err != nil {
		writeAdminUserError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func cAdminRecentAuthWindow(h *adminHandlers) time.Duration {
	if h.recentAuthWindow > 0 {
		return h.recentAuthWindow
	}
	return 15 * time.Minute
}

func (h *adminHandlers) listSecurityEvents(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok || h.userService == nil {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	page, limit, ok := parseAdminPagination(c)
	if !ok {
		return
	}
	result, err := h.userService.ListSecurityEvents(c.Request.Context(), adminread.SecurityEventsRequest{
		Principal: principal, AccountID: c.Param("accountId"), Page: page, Limit: limit,
	})
	if err != nil {
		writeAdminUserError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"events": result.Events, "total": result.Total, "page": result.Page,
		"limit": result.Limit, "has_more": hasMore(result.Total, result.Page, result.Limit),
	})
}

func (h *adminHandlers) listCourseOptions(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok || h.userService == nil {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	locale, ok := requestedLocale(c.GetHeader("Accept-Language"))
	if !ok {
		writeProblem(c, problem.ValidationFailed())
		return
	}
	options, err := h.userService.ListCourseOptions(c.Request.Context(), adminread.CourseOptionsRequest{
		Principal: principal, Locale: locale, Query: strings.TrimSpace(c.Query("q")), Limit: 25,
	})
	if err != nil {
		writeAdminUserError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"courses": options})
}

func (h *adminHandlers) diagnoseAccess(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok || h.userService == nil {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	locale, ok := requestedLocale(c.GetHeader("Accept-Language"))
	if !ok || strings.TrimSpace(c.Query("courseId")) == "" {
		writeProblem(c, problem.ValidationFailed())
		return
	}
	if _, err := uuid.Parse(c.Query("courseId")); err != nil {
		writeProblem(c, problem.ValidationFailed())
		return
	}
	result, err := h.userService.DiagnoseAccess(c.Request.Context(), adminread.AccessDiagnosticRequest{
		Principal: principal, CorrelationID: requestid.FromContext(c.Request.Context()), Locale: locale,
		AccountID: c.Param("accountId"), CourseID: c.Query("courseId"), Now: time.Now().UTC(),
	})
	if err != nil {
		writeAdminUserError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func writeAdminUserError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, adminread.ErrInvalidInput):
		writeProblem(c, problem.ValidationFailed())
	case errors.Is(err, adminread.ErrAccountNotFound), errors.Is(err, adminread.ErrCourseNotFound):
		writeProblem(c, problem.NotFound())
	case errors.Is(err, adminread.ErrUnauthorized):
		writeProblem(c, problem.NotAuthorized())
	case errors.Is(err, identity.ErrRecentAuthRequired):
		writeProblem(c, problem.New(http.StatusForbidden, "recent-authentication-required",
			"Recent authentication required", "This operation requires recent authentication"))
	default:
		writeProblem(c, problem.Internal(requestid.FromContext(c.Request.Context())))
	}
}
