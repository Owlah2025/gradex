package httpapi

import (
	"bytes"
	"encoding/csv"
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

const maxAccountExportRows = 5000

func (h *adminHandlers) listEmailDeliveries(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	request, ok := parseEmailDeliveriesRequest(c, principal)
	if !ok {
		return
	}
	result, err := h.service.ListEmailDeliveries(c.Request.Context(), request)
	if err != nil {
		writeAdminReadError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}

func parseEmailDeliveriesRequest(c *gin.Context, principal identity.Principal) (adminread.EmailDeliveriesRequest, bool) {
	page, limit, ok := parseAdminPagination(c)
	if !ok {
		return adminread.EmailDeliveriesRequest{}, false
	}
	locale, ok := requestedLocale(c.GetHeader("Accept-Language"))
	if !ok {
		writeProblem(c, problem.ValidationFailed())
		return adminread.EmailDeliveriesRequest{}, false
	}
	from, ok := parseAdminDate(c, c.Query("from"), false)
	if !ok {
		return adminread.EmailDeliveriesRequest{}, false
	}
	to, ok := parseAdminDate(c, c.Query("to"), true)
	if !ok {
		return adminread.EmailDeliveriesRequest{}, false
	}
	if from != nil && to != nil && !from.Before(*to) {
		writeProblem(c, problem.ValidationFailed())
		return adminread.EmailDeliveriesRequest{}, false
	}
	accountID := strings.TrimSpace(c.Query("accountId"))
	if accountID != "" {
		if _, err := uuid.Parse(accountID); err != nil {
			writeProblem(c, problem.ValidationFailed())
			return adminread.EmailDeliveriesRequest{}, false
		}
	}
	return adminread.EmailDeliveriesRequest{
		Principal: principal, CorrelationID: requestid.FromContext(c.Request.Context()), Locale: locale,
		State: strings.TrimSpace(c.Query("state")), Kind: strings.TrimSpace(c.Query("kind")), AccountID: accountID,
		OccurredFrom: from, OccurredTo: to, Page: page, Limit: limit,
	}, true
}

func (h *adminHandlers) listMediaFailures(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	page, limit, ok := parseAdminPagination(c)
	if !ok {
		return
	}
	locale, ok := requestedLocale(c.GetHeader("Accept-Language"))
	if !ok {
		writeProblem(c, problem.ValidationFailed())
		return
	}
	result, err := h.service.ListMediaFailures(c.Request.Context(), adminread.MediaFailuresRequest{
		Principal: principal, Locale: locale, State: strings.TrimSpace(c.Query("state")), Page: page, Limit: limit,
	})
	if err != nil {
		writeAdminReadError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}

func (h *adminHandlers) exportAccounts(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	if !requireRecentAdminAuthenticationWithWindow(c, time.Now().UTC(), h.recentAuthWindow) {
		return
	}
	limit := maxAccountExportRows
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maxAccountExportRows {
			writeProblem(c, problem.ValidationFailed())
			return
		}
		limit = parsed
	}
	request, ok := parseAccountDirectoryFilters(c, principal, 1, limit)
	if !ok {
		return
	}
	result, err := h.service.ExportAccounts(c.Request.Context(), request)
	if err != nil {
		writeAdminReadError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", `attachment; filename="gradex-user-directory.csv"`)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", renderAccountExportCSV(result.Accounts))
}

func renderAccountExportCSV(accounts []adminread.AccountDirectoryEntry) []byte {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	_ = writer.Write([]string{"id", "display_name", "email", "role", "status", "locale", "email_verified", "created_at", "institution", "last_sign_in_activity_at"})
	for _, account := range accounts {
		lastActivity := ""
		if account.LastSignInActivityAt != nil {
			lastActivity = account.LastSignInActivityAt.UTC().Format(time.RFC3339)
		}
		_ = writer.Write([]string{
			safeCSVCell(account.ID), safeCSVCell(account.DisplayName), safeCSVCell(account.Email),
			safeCSVCell(string(account.Role)), safeCSVCell(string(account.Status)), safeCSVCell(string(account.Locale)),
			safeCSVCell(strconv.FormatBool(account.EmailVerified)), safeCSVCell(account.CreatedAt.UTC().Format(time.RFC3339)),
			safeCSVCell(account.InstitutionLabel), safeCSVCell(lastActivity),
		})
	}
	writer.Flush()
	return buffer.Bytes()
}

func safeCSVCell(value string) string {
	if value == "" {
		return value
	}
	if strings.ContainsRune("=+-@\t\r", []rune(value)[0]) {
		return "'" + value
	}
	return value
}
