//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
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

	pageOne := getAdminAccountPage(t, client, ts.URL, adminToken, 1)
	pageTwo := getAdminAccountPage(t, client, ts.URL, adminToken, 2)
	if pageOne.Total != 3 || pageTwo.Total != 3 || len(pageOne.Accounts) != 1 || len(pageTwo.Accounts) != 1 {
		t.Fatalf("account pages = page1=%+v page2=%+v", pageOne, pageTwo)
	}
	if pageOne.Accounts[0].ID != studentIDs[2] || pageTwo.Accounts[0].ID != studentIDs[1] {
		t.Fatalf("account ordering = page1 %s page2 %s", pageOne.Accounts[0].ID, pageTwo.Accounts[0].ID)
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
		Total int `json:"total"`
	}
	decodeAdminResponse(t, auditResponse, http.StatusOK, &auditResult)
	if auditResult.Total < 2 || len(auditResult.Events) < 2 {
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
		t.Fatalf("response status = %d, want %d", response.StatusCode, wantStatus)
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decoding admin response: %v", err)
	}
}
