package httpapi

import (
	"errors"
	"net/http"
	"strconv"
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
	defaultAdminPageLimit = 25
	maxAdminPage          = 10_000
	maxAdminQueryLength   = 200
)

type adminHandlers struct {
	service          AdminService
	userService      AdminUserService
	recentAuthWindow time.Duration
}

func (h *adminHandlers) listAccounts(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	req, ok := parseAccountDirectoryRequest(c, principal)
	if !ok {
		return
	}
	result, err := h.service.SearchAccounts(c.Request.Context(), req)
	if err != nil {
		writeAdminReadError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"accounts": result.Accounts,
		"total":    result.Total,
		"page":     result.Page,
		"limit":    result.Limit,
		"has_more": hasMore(result.Total, result.Page, result.Limit),
	})
}

func (h *adminHandlers) getAccount(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	accountID := c.Param("accountId")
	if _, err := uuid.Parse(accountID); err != nil {
		writeProblem(c, problem.NotFound())
		return
	}
	locale, ok := requestedLocale(c.GetHeader("Accept-Language"))
	if !ok {
		writeProblem(c, problem.ValidationFailed())
		return
	}
	entry, err := h.service.GetAccount(c.Request.Context(), adminread.AccountIdentityRequest{
		Principal:     principal,
		CorrelationID: requestid.FromContext(c.Request.Context()),
		Locale:        locale,
		AccountID:     accountID,
	})
	if err != nil {
		writeAdminReadError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"identity": entry})
}

func (h *adminHandlers) listAuditEvents(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	req, ok := parseAuditEventRequest(c, principal)
	if !ok {
		return
	}
	result, err := h.service.ListAuditEvents(c.Request.Context(), req)
	if err != nil {
		writeAdminReadError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"audit_events": result.Events,
		"page":         result.Page,
		"limit":        result.Limit,
		"has_more":     result.HasMore,
		"as_of":        result.AsOf,
	})
}

func parseAccountDirectoryRequest(c *gin.Context, principal identity.Principal) (adminread.AccountDirectoryRequest, bool) {
	page, limit, ok := parseAdminPagination(c)
	if !ok {
		return adminread.AccountDirectoryRequest{}, false
	}
	return parseAccountDirectoryFilters(c, principal, page, limit)
}

func parseAccountDirectoryFilters(
	c *gin.Context,
	principal identity.Principal,
	page, limit int,
) (adminread.AccountDirectoryRequest, bool) {
	query := strings.TrimSpace(c.Query("q"))
	if len([]rune(query)) > maxAdminQueryLength {
		writeProblem(c, problem.ValidationFailed())
		return adminread.AccountDirectoryRequest{}, false
	}

	role, ok := parseRoleFilter(c.Query("role"))
	if !ok {
		writeProblem(c, problem.ValidationFailed())
		return adminread.AccountDirectoryRequest{}, false
	}
	status, ok := parseStatusFilter(c.Query("status"))
	if !ok {
		writeProblem(c, problem.ValidationFailed())
		return adminread.AccountDirectoryRequest{}, false
	}

	institutionID := strings.TrimSpace(c.Query("institutionId"))
	if institutionID != "" {
		if _, err := uuid.Parse(institutionID); err != nil {
			writeProblem(c, problem.ValidationFailed())
			return adminread.AccountDirectoryRequest{}, false
		}
	}

	joinedFrom, ok := parseAdminDate(c, c.Query("joinedFrom"), false)
	if !ok {
		return adminread.AccountDirectoryRequest{}, false
	}
	joinedTo, ok := parseAdminDate(c, c.Query("joinedTo"), true)
	if !ok {
		return adminread.AccountDirectoryRequest{}, false
	}
	if joinedFrom != nil && joinedTo != nil && !joinedFrom.Before(*joinedTo) {
		writeProblem(c, problem.ValidationFailed())
		return adminread.AccountDirectoryRequest{}, false
	}

	locale, ok := requestedLocale(c.GetHeader("Accept-Language"))
	if !ok {
		writeProblem(c, problem.ValidationFailed())
		return adminread.AccountDirectoryRequest{}, false
	}
	return adminread.AccountDirectoryRequest{
		Principal:     principal,
		CorrelationID: requestid.FromContext(c.Request.Context()),
		Locale:        locale,
		Query:         query,
		Role:          role,
		Status:        status,
		InstitutionID: institutionID,
		JoinedFrom:    joinedFrom,
		JoinedTo:      joinedTo,
		Page:          page,
		Limit:         limit,
	}, true
}

func parseAuditEventRequest(c *gin.Context, principal identity.Principal) (adminread.AuditEventRequest, bool) {
	page, limit, ok := parseAdminPagination(c)
	if !ok {
		return adminread.AuditEventRequest{}, false
	}
	actorAccountID := strings.TrimSpace(c.Query("actorAccountId"))
	if actorAccountID != "" {
		if _, err := uuid.Parse(actorAccountID); err != nil {
			writeProblem(c, problem.ValidationFailed())
			return adminread.AuditEventRequest{}, false
		}
	}

	occurredFrom, ok := parseAdminDate(c, c.Query("from"), false)
	if !ok {
		return adminread.AuditEventRequest{}, false
	}
	occurredTo, ok := parseAdminDate(c, c.Query("to"), true)
	if !ok {
		return adminread.AuditEventRequest{}, false
	}
	if occurredFrom != nil && occurredTo != nil && !occurredFrom.Before(*occurredTo) {
		writeProblem(c, problem.ValidationFailed())
		return adminread.AuditEventRequest{}, false
	}
	asOf, ok := parseAdminTimestamp(c, c.Query("asOf"))
	if !ok {
		return adminread.AuditEventRequest{}, false
	}

	return adminread.AuditEventRequest{
		Principal:      principal,
		CorrelationID:  requestid.FromContext(c.Request.Context()),
		ActorAccountID: actorAccountID,
		ActorQuery:     strings.TrimSpace(c.Query("actor")),
		TargetType:     strings.TrimSpace(c.Query("targetType")),
		TargetID:       strings.TrimSpace(c.Query("targetId")),
		Action:         strings.TrimSpace(c.Query("action")),
		Module:         strings.TrimSpace(c.Query("module")),
		OccurredFrom:   occurredFrom,
		OccurredTo:     occurredTo,
		AsOf:           asOf,
		Page:           page,
		Limit:          limit,
	}, true
}

func parseAdminPagination(c *gin.Context) (int, int, bool) {
	page, ok := parsePositiveQuery(c, "page", 1)
	if !ok || page > maxAdminPage {
		writeProblem(c, problem.ValidationFailed())
		return 0, 0, false
	}
	limit, ok := parsePositiveQuery(c, "limit", defaultAdminPageLimit)
	if !ok || limit > 50 {
		writeProblem(c, problem.ValidationFailed())
		return 0, 0, false
	}
	return page, limit, true
}

func parsePositiveQuery(c *gin.Context, name string, fallback int) (int, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return fallback, true
	}
	value, err := strconv.Atoi(raw)
	return value, err == nil && value >= 1
}

func parseRoleFilter(raw string) (identity.Role, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", true
	}
	role := identity.Role(value)
	return role, role.Valid()
}

func parseStatusFilter(raw string) (identity.AccountStatus, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", true
	}
	status := identity.AccountStatus(value)
	return status, status.Valid()
}

func parseAdminDate(c *gin.Context, raw string, endOfDate bool) (*time.Time, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, true
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err == nil {
		// Date-only bounds are UTC days for non-browser callers. Browser clients
		// send offset-aware RFC3339 bounds so an administrator's local day is kept.
		if endOfDate {
			parsed = parsed.AddDate(0, 0, 1)
		}
		return &parsed, true
	}
	parsed, err = time.Parse(time.RFC3339, value)
	if err != nil {
		writeProblem(c, problem.ValidationFailed())
		return nil, false
	}
	return &parsed, true
}

func parseAdminTimestamp(c *gin.Context, raw string) (*time.Time, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, true
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		writeProblem(c, problem.ValidationFailed())
		return nil, false
	}
	return &parsed, true
}

func hasMore(total, page, limit int) bool {
	return page*limit < total
}

func writeAdminReadError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, adminread.ErrInvalidInput):
		writeProblem(c, problem.ValidationFailed())
	case errors.Is(err, adminread.ErrAccountNotFound):
		writeProblem(c, problem.NotFound())
	case errors.Is(err, adminread.ErrUnauthorized):
		writeProblem(c, problem.NotAuthorized())
	case errors.Is(err, adminread.ErrExportTooLarge):
		writeProblem(c, problem.ExportTooLarge())
	default:
		writeProblem(c, problem.Internal(requestid.FromContext(c.Request.Context())))
	}
}
