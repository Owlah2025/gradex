//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAdminOperationsReadsAndAudits(t *testing.T) {
	ts, pool, adminID, _, _, _, adminToken, _ := setupAdminPricingAPIServer(t)
	ctx := context.Background()
	institutionID := "10000000-0000-0000-0000-000000000100"
	studentIDs := []string{
		"10000000-0000-0000-0000-000000000003",
		"10000000-0000-0000-0000-000000000004",
		"10000000-0000-0000-0000-000000000005",
	}
	seedAdminDirectoryAccounts(t, pool, institutionID, studentIDs)

	client := ts.Client()
	searchPath := "/api/v1/admin/accounts?q=" + url.QueryEscape("alice@example.com") +
		"&role=STUDENT&institutionId=" + institutionID +
		"&joinedFrom=2026-09-28&joinedTo=2026-09-29&page=1&limit=10"
	response := doPricingRequest(t, client, http.MethodGet, ts.URL+searchPath, adminToken, "", adminToken, nil)
	var searchResult struct {
		Accounts []struct {
			ID                   string     `json:"id"`
			DisplayName          string     `json:"display_name"`
			InstitutionLabel     string     `json:"institution_label"`
			LastSignInActivityAt *time.Time `json:"last_sign_in_activity_at"`
		} `json:"accounts"`
		Total   int  `json:"total"`
		HasMore bool `json:"has_more"`
	}
	decodeAdminResponse(t, response, http.StatusOK, &searchResult)
	if len(searchResult.Accounts) != 1 || searchResult.Accounts[0].ID != studentIDs[0] ||
		searchResult.Accounts[0].DisplayName != "Alice Student" ||
		searchResult.Accounts[0].InstitutionLabel != "جامعة الكويت" ||
		searchResult.Accounts[0].LastSignInActivityAt != nil {
		t.Fatalf("filtered account result = %+v", searchResult)
	}
	if searchResult.Total != 1 || searchResult.HasMore {
		t.Fatalf("filtered pagination = total %d has_more %t", searchResult.Total, searchResult.HasMore)
	}

	var searchedMetadata string
	if err := pool.QueryRow(ctx, `SELECT metadata::text FROM audit_events
		WHERE action = 'ADMIN_USER_SEARCHED' ORDER BY occurred_at DESC LIMIT 1`).Scan(&searchedMetadata); err != nil {
		t.Fatalf("reading search audit metadata: %v", err)
	}
	if strings.Contains(searchedMetadata, "alice@example.com") {
		t.Fatalf("search audit metadata contains the raw query: %s", searchedMetadata)
	}
	for _, safeValue := range []string{`"query_kind": "email"`, `"query_length": 17`, `"result_count": 1`} {
		if !strings.Contains(searchedMetadata, safeValue) {
			t.Fatalf("search audit metadata %q missing from %s", safeValue, searchedMetadata)
		}
	}

	accountPageOne := getAdminAccountPage(t, client, ts.URL, adminToken, 1)
	accountPageTwo := getAdminAccountPage(t, client, ts.URL, adminToken, 2)
	if accountPageOne.Total != 3 || accountPageTwo.Total != 3 || len(accountPageOne.Accounts) != 1 || len(accountPageTwo.Accounts) != 1 {
		t.Fatalf("account pages = page1=%+v page2=%+v", accountPageOne, accountPageTwo)
	}
	if accountPageOne.Accounts[0].ID != studentIDs[2] || accountPageTwo.Accounts[0].ID != studentIDs[1] {
		t.Fatalf("account ordering = page1 %s page2 %s", accountPageOne.Accounts[0].ID, accountPageTwo.Accounts[0].ID)
	}

	detailResponse := doPricingRequest(t, client, http.MethodGet,
		ts.URL+"/api/v1/admin/accounts/"+studentIDs[0], adminToken, "", adminToken, nil)
	var detail struct {
		Identity struct {
			DisplayName string `json:"display_name"`
			Email       string `json:"email"`
		} `json:"identity"`
	}
	decodeAdminResponse(t, detailResponse, http.StatusOK, &detail)
	if detail.Identity.DisplayName != "Alice Student" || detail.Identity.Email != "alice@example.com" {
		t.Fatalf("account detail = %+v", detail)
	}

	auditResponse := doPricingRequest(t, client, http.MethodGet,
		ts.URL+"/api/v1/admin/audit-events?action=ADMIN_USER_SEARCHED&module=IDENTITY_AND_ACCESS&actorAccountId="+adminID+"&page=1&limit=10",
		adminToken, "", adminToken, nil)
	var auditResult struct {
		Events []struct {
			Action           string `json:"action"`
			ActorDisplayName string `json:"actor_display_name"`
			TargetType       string `json:"target_type"`
		} `json:"audit_events"`
		HasMore bool   `json:"has_more"`
		AsOf    string `json:"as_of"`
	}
	decodeAdminResponse(t, auditResponse, http.StatusOK, &auditResult)
	if len(auditResult.Events) < 2 || auditResult.HasMore {
		t.Fatalf("filtered audit events = %+v", auditResult)
	}
	for _, event := range auditResult.Events {
		if event.Action != "ADMIN_USER_SEARCHED" || event.ActorDisplayName != "Admin User" ||
			event.TargetType != "ACCOUNT_DIRECTORY" {
			t.Fatalf("filtered audit event = %+v", event)
		}
	}

	var viewedCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events
		WHERE actor_account_id = $1::uuid AND action = 'ADMIN_USER_VIEWED' AND target_id = $2`,
		adminID, studentIDs[0]).Scan(&viewedCount); err != nil {
		t.Fatalf("counting account-view audit: %v", err)
	}
	if viewedCount != 1 {
		t.Fatalf("account-view audit count = %d, want 1", viewedCount)
	}
	var viewerCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events
		WHERE actor_account_id = $1::uuid AND action = 'ADMIN_AUDIT_VIEWED'`,
		adminID).Scan(&viewerCount); err != nil {
		t.Fatalf("counting audit-view audit: %v", err)
	}
	if viewerCount != 1 {
		t.Fatalf("audit-view audit count = %d, want 1", viewerCount)
	}

	viewerCountBeforePagination := viewerCount
	pageOneResponse := doPricingRequest(t, client, http.MethodGet,
		ts.URL+"/api/v1/admin/audit-events?action=ADMIN_USER_SEARCHED&page=1&limit=1",
		adminToken, "", adminToken, nil)
	var pageOne struct {
		Events []struct {
			ID string `json:"id"`
		} `json:"audit_events"`
		HasMore bool   `json:"has_more"`
		AsOf    string `json:"as_of"`
	}
	decodeAdminResponse(t, pageOneResponse, http.StatusOK, &pageOne)
	if len(pageOne.Events) != 1 || !pageOne.HasMore || pageOne.AsOf == "" {
		t.Fatalf("audit page one = %+v", pageOne)
	}

	pageTwoResponse := doPricingRequest(t, client, http.MethodGet,
		ts.URL+"/api/v1/admin/audit-events?action=ADMIN_USER_SEARCHED&page=2&limit=1&asOf="+url.QueryEscape(pageOne.AsOf),
		adminToken, "", adminToken, nil)
	var pageTwo struct {
		Events []struct {
			ID string `json:"id"`
		} `json:"audit_events"`
		HasMore bool `json:"has_more"`
	}
	decodeAdminResponse(t, pageTwoResponse, http.StatusOK, &pageTwo)
	if len(pageTwo.Events) != 1 || pageTwo.Events[0].ID == pageOne.Events[0].ID || !pageTwo.HasMore {
		t.Fatalf("audit pages = page1=%+v page2=%+v", pageOne, pageTwo)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events
		WHERE actor_account_id = $1::uuid AND action = 'ADMIN_AUDIT_VIEWED'`, adminID).Scan(&viewerCount); err != nil {
		t.Fatalf("counting paginated audit-view audits: %v", err)
	}
	if viewerCount != viewerCountBeforePagination+2 {
		t.Fatalf("paginated audit-view audit count = %d, want %d", viewerCount, viewerCountBeforePagination+2)
	}

	invalidAdminRequests := []string{
		"/api/v1/admin/accounts?limit=51",
		"/api/v1/admin/accounts?page=0",
		"/api/v1/admin/accounts?joinedFrom=2026-09-30&joinedTo=2026-09-29",
		"/api/v1/admin/accounts?role=UNKNOWN",
		"/api/v1/admin/accounts?status=UNKNOWN",
		"/api/v1/admin/audit-events?module=UNKNOWN",
	}
	for _, path := range invalidAdminRequests {
		response := doPricingRequest(t, client, http.MethodGet, ts.URL+path, adminToken, "", adminToken, nil)
		response.Body.Close()
		if response.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("invalid admin request %s status = %d, want %d", path, response.StatusCode, http.StatusUnprocessableEntity)
		}
	}
}

type adminAccountPage struct {
	Accounts []struct {
		ID string `json:"id"`
	} `json:"accounts"`
	Total   int  `json:"total"`
	HasMore bool `json:"has_more"`
}

func getAdminAccountPage(t *testing.T, client *http.Client, baseURL, token string, page int) adminAccountPage {
	t.Helper()
	response := doPricingRequest(t, client, http.MethodGet,
		fmt.Sprintf("%s/api/v1/admin/accounts?role=STUDENT&page=%d&limit=1", baseURL, page),
		token, "", token, nil)
	var result adminAccountPage
	decodeAdminResponse(t, response, http.StatusOK, &result)
	return result
}

func seedAdminDirectoryAccounts(t *testing.T, pool *pgxpool.Pool, institutionID string, studentIDs []string) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO institutions
		(id, country_code, slug, name_ar, name_en, max_academic_level)
		VALUES ($1::uuid, 'KW', 'kuwait-university', 'جامعة الكويت', 'Kuwait University', 5)`, institutionID); err != nil {
		t.Fatalf("seeding institution: %v", err)
	}
	createdAt := []time.Time{
		time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 28, 11, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC),
	}
	emails := []string{"alice@example.com", "bob@example.com", "charlie@example.com"}
	names := []string{"Alice Student", "Bob Student", "Charlie Student"}
	for index, accountID := range studentIDs {
		if _, err := pool.Exec(ctx, `INSERT INTO accounts
			(id, normalized_email, email, role, status, display_name, locale, email_verified_at, created_at)
			VALUES ($1::uuid, $2, $2, 'STUDENT', 'ACTIVE', $3, 'en', $4, $5)`,
			accountID, emails[index], names[index], createdAt[index], createdAt[index]); err != nil {
			t.Fatalf("seeding student account %d: %v", index, err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO student_academic_profiles
		(account_id, setup_state, enrollment_status, institution_id)
		VALUES
		($1::uuid, 'COMPLETED', 'UNDECLARED', $4::uuid),
		($2::uuid, 'COMPLETED', 'UNDECLARED', $4::uuid),
		($3::uuid, 'COMPLETED', 'UNDECLARED', $4::uuid)`,
		studentIDs[0], studentIDs[1], studentIDs[2], institutionID); err != nil {
		t.Fatalf("seeding student academic profiles: %v", err)
	}
}

func decodeAdminResponse(t *testing.T, response *http.Response, wantStatus int, target any) {
	t.Helper()
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("response status = %d, want %d, body = %s", response.StatusCode, wantStatus, body)
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decoding admin response: %v", err)
	}
}
