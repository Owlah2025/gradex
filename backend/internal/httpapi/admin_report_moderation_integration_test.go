//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/learning"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

const (
	ad14AdminID      = "50000000-0000-0000-0000-000000000001"
	ad14InstructorID = "50000000-0000-0000-0000-000000000002"
	ad14StudentID    = "50000000-0000-0000-0000-000000000003"
	ad14SuspendedID  = "50000000-0000-0000-0000-000000000004"
)

type ad14ReportAck struct {
	ReportID string `json:"report_id"`
}

type ad14ReportPage struct {
	Items []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"items"`
}

type ad14ReportResponse struct {
	ID               string `json:"id"`
	Status           string `json:"status"`
	ResolutionAction string `json:"resolution_action"`
	Target           struct {
		Available       bool   `json:"available"`
		CourseLifecycle string `json:"course_lifecycle"`
	} `json:"target"`
}

func seedAD14Account(t *testing.T, f learningIntegrationFixture, accountID string, role identity.Role, status identity.AccountStatus) {
	t.Helper()
	_, err := f.pool.Exec(context.Background(), `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
		VALUES ($1::uuid, $2, $2, $3, $4, 'AD-14 fixture')
	`, accountID, accountID+"@example.test", role, status)
	if err != nil {
		t.Fatalf("seeding AD-14 account %s: %v", accountID, err)
	}
}

func ad14ModerationServer(t *testing.T, f learningIntegrationFixture, accountID string, role identity.Role, status identity.AccountStatus) *httptest.Server {
	t.Helper()
	writer, err := outbox.NewWriter("ad14-v1", bytes.Repeat([]byte{0x62}, 32))
	if err != nil {
		t.Fatalf("constructing AD-14 outbox writer: %v", err)
	}
	catalogRepository, err := catalog.NewRepository(f.pool, writer)
	if err != nil {
		t.Fatalf("constructing AD-14 catalog repository: %v", err)
	}
	reportRepository, err := learning.NewRepository(f.pool)
	if err != nil {
		t.Fatalf("constructing AD-14 report repository: %v", err)
	}
	moderation, err := NewModerationFoundation(ModerationFoundationOptions{
		Reports: reportRepository,
		Catalog: catalogRepository,
	})
	if err != nil {
		t.Fatalf("constructing AD-14 moderation foundation: %v", err)
	}
	return buildTestRouterWithAccount(t, f.pool, accountID, role, status, WithModerationFoundation(moderation))
}

func ad14Token(byteValue byte) string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{byteValue}, 32))
}

func ad14Request(t *testing.T, server *httptest.Server, token, method, path string, body string) (int, []byte) {
	t.Helper()
	request, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("creating AD-14 request: %v", err)
	}
	request.Header.Set("Origin", "https://gradex.example")
	request.Header.Set("X-CSRF-Token", token)
	request.Header.Set("Accept", "application/json, application/problem+json")
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("sending AD-14 request: %v", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("reading AD-14 response: %v", err)
	}
	return response.StatusCode, payload
}

func ad14CreateReport(t *testing.T, f learningIntegrationFixture) string {
	t.Helper()
	response := submitReport(f, courseContextOf(t, f), "inaccurate")
	if response.Code != http.StatusCreated {
		t.Fatalf("Student report submission = %d %s", response.Code, response.Body.String())
	}
	var acknowledgement ad14ReportAck
	if err := json.Unmarshal(response.Body.Bytes(), &acknowledgement); err != nil {
		t.Fatalf("decoding Student report acknowledgement: %v", err)
	}
	if acknowledgement.ReportID == "" {
		t.Fatal("Student report acknowledgement omitted report_id")
	}
	return acknowledgement.ReportID
}

func TestAD14AdminReportQueueDismissalAndHTTPContract(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	seedAD14Account(t, f, ad14AdminID, identity.RoleAdmin, identity.StatusActive)
	server := ad14ModerationServer(t, f, ad14AdminID, identity.RoleAdmin, identity.StatusActive)
	token := ad14Token(0x71)
	reportID := ad14CreateReport(t, f)

	status, body := ad14Request(t, server, token, http.MethodGet, "/api/v1/admin/reports?page=1&page_size=20", "")
	if status != http.StatusOK {
		t.Fatalf("Admin report list = %d %s", status, body)
	}
	var page ad14ReportPage
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decoding Admin report list: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != reportID || page.Items[0].Status != "OPEN" {
		t.Fatalf("Admin report list = %+v, want one open report", page)
	}
	for _, secret := range []string{f.studentID, f.courseID, "reporter_account_id", "target_revision_ref"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("Admin report list leaked %q: %s", secret, body)
		}
	}

	status, body = ad14Request(t, server, token, http.MethodGet, "/api/v1/admin/reports/"+reportID, "")
	if status != http.StatusOK {
		t.Fatalf("Admin report detail = %d %s", status, body)
	}
	var detail ad14ReportResponse
	if err := json.Unmarshal(body, &detail); err != nil {
		t.Fatalf("decoding Admin report detail: %v", err)
	}
	if detail.ID != reportID || detail.Status != "OPEN" || !detail.Target.Available {
		t.Fatalf("Admin report detail = %+v, want open available report", detail)
	}

	status, body = ad14Request(t, server, token, http.MethodPost, "/api/v1/admin/reports/"+reportID+"/resolve", `{"action":"NOPE","reason":"bad"}`)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("malformed resolution = %d %s, want 422", status, body)
	}

	status, body = ad14Request(t, server, token, http.MethodPost, "/api/v1/admin/reports/"+reportID+"/resolve", `{"action":"DISMISS","reason":"Reviewed; no platform action required."}`)
	if status != http.StatusOK {
		t.Fatalf("Admin dismissal = %d %s", status, body)
	}
	if err := json.Unmarshal(body, &detail); err != nil {
		t.Fatalf("decoding dismissed report: %v", err)
	}
	if detail.Status != "RESOLVED" || detail.ResolutionAction != "DISMISSED" {
		t.Fatalf("dismissed report = %+v", detail)
	}

	var lifecycle string
	if err := f.pool.QueryRow(context.Background(), `SELECT lifecycle::text FROM courses WHERE id = $1::uuid`, f.courseID).Scan(&lifecycle); err != nil {
		t.Fatalf("reading target lifecycle after dismissal: %v", err)
	}
	if lifecycle != "PUBLISHED" {
		t.Fatalf("dismissal changed target lifecycle to %s", lifecycle)
	}
	var moderationAudits int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE module = 'MODERATION' AND action = 'REPORT_RESOLVED' AND target_id = $1`, reportID).Scan(&moderationAudits); err != nil {
		t.Fatalf("reading report audit: %v", err)
	}
	if moderationAudits != 1 {
		t.Fatalf("report resolution audit count = %d, want 1", moderationAudits)
	}

	status, body = ad14Request(t, server, token, http.MethodGet, "/api/v1/admin/reports", "")
	if status != http.StatusOK || !strings.Contains(string(body), `"items":[]`) {
		t.Fatalf("open queue after dismissal = %d %s", status, body)
	}
	status, body = ad14Request(t, server, token, http.MethodGet, "/api/v1/admin/reports/"+reportID, "")
	if status != http.StatusOK || !strings.Contains(string(body), `"status":"RESOLVED"`) {
		t.Fatalf("resolved history detail = %d %s", status, body)
	}
	status, body = ad14Request(t, server, token, http.MethodPost, "/api/v1/admin/reports/"+reportID+"/resolve", `{"action":"DISMISS","reason":"Second decision"}`)
	if status != http.StatusConflict {
		t.Fatalf("second resolution = %d %s, want 409", status, body)
	}
	status, body = ad14Request(t, server, token, http.MethodGet, "/api/v1/admin/reports/00000000-0000-0000-0000-000000000099", "")
	if status != http.StatusNotFound {
		t.Fatalf("unknown report = %d %s, want 404", status, body)
	}
}

func TestAD14ConcurrentUnavailableAndCanonicalDelistResolution(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	seedAD14Account(t, f, ad14AdminID, identity.RoleAdmin, identity.StatusActive)
	server := ad14ModerationServer(t, f, ad14AdminID, identity.RoleAdmin, identity.StatusActive)
	token := ad14Token(0x72)

	first := ad14CreateReport(t, f)
	status, body := ad14Request(t, server, token, http.MethodPost, "/api/v1/admin/reports/"+first+"/resolve", `{"action":"DISMISS","reason":"Prepare the concurrency case."}`)
	if status != http.StatusOK {
		t.Fatalf("preparing concurrency case = %d %s", status, body)
	}
	second := ad14CreateReport(t, f)
	results := make(chan int, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	for range 2 {
		go func() {
			defer wait.Done()
			status, _ := ad14Request(t, server, token, http.MethodPost, "/api/v1/admin/reports/"+second+"/resolve", `{"action":"DISMISS","reason":"Concurrent review"}`)
			results <- status
		}()
	}
	wait.Wait()
	statuses := []int{<-results, <-results}
	sort.Ints(statuses)
	if !equalInts(statuses, []int{http.StatusOK, http.StatusConflict}) {
		t.Fatalf("concurrent resolution statuses = %v, want [200 409]", statuses)
	}

	unavailable := ad14CreateReport(t, f)
	if _, err := f.pool.Exec(context.Background(), `UPDATE content_reports SET target_revision_ref = $1::uuid WHERE id = $2::uuid`, "ffffffff-ffff-ffff-ffff-ffffffffffff", unavailable); err != nil {
		t.Fatalf("making report target unavailable: %v", err)
	}
	status, body = ad14Request(t, server, token, http.MethodGet, "/api/v1/admin/reports/"+unavailable, "")
	if status != http.StatusOK || !strings.Contains(string(body), `"available":false`) {
		t.Fatalf("unavailable target detail = %d %s", status, body)
	}
	status, body = ad14Request(t, server, token, http.MethodPost, "/api/v1/admin/reports/"+unavailable+"/resolve", `{"action":"DISMISS","reason":"Target no longer available; closing report."}`)
	if status != http.StatusOK {
		t.Fatalf("unavailable target dismissal = %d %s", status, body)
	}

	delistReport := ad14CreateReport(t, f)
	status, body = ad14Request(t, server, token, http.MethodPost, "/api/v1/admin/reports/"+delistReport+"/resolve", `{"action":"DELIST","reason":"Reported content requires removal from public discovery."}`)
	if status != http.StatusOK {
		t.Fatalf("canonical delist resolution = %d %s", status, body)
	}
	var lifecycle string
	if err := f.pool.QueryRow(context.Background(), `SELECT lifecycle::text FROM courses WHERE id = $1::uuid`, f.courseID).Scan(&lifecycle); err != nil {
		t.Fatalf("reading lifecycle after canonical delist: %v", err)
	}
	if lifecycle != "DELISTED" {
		t.Fatalf("canonical delist left lifecycle %s", lifecycle)
	}
	var lifecycleAudits, reportAudits int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE action = 'COURSE_DELISTED' AND target_id = $1`, f.courseID).Scan(&lifecycleAudits); err != nil {
		t.Fatalf("reading lifecycle audit: %v", err)
	}
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE module = 'MODERATION' AND action = 'REPORT_RESOLVED' AND target_id = $1`, delistReport).Scan(&reportAudits); err != nil {
		t.Fatalf("reading moderation audit: %v", err)
	}
	if lifecycleAudits != 1 || reportAudits != 1 {
		t.Fatalf("audit counts lifecycle=%d report=%d, want 1/1", lifecycleAudits, reportAudits)
	}
}

func TestAD14AdminReportAuthorizationBoundaries(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	seedAD14Account(t, f, ad14AdminID, identity.RoleAdmin, identity.StatusActive)
	seedAD14Account(t, f, ad14InstructorID, identity.RoleInstructor, identity.StatusActive)
	seedAD14Account(t, f, ad14StudentID, identity.RoleStudent, identity.StatusActive)
	seedAD14Account(t, f, ad14SuspendedID, identity.RoleAdmin, identity.StatusSuspended)
	reportID := ad14CreateReport(t, f)

	adminServer := ad14ModerationServer(t, f, ad14AdminID, identity.RoleAdmin, identity.StatusActive)
	instructorServer := ad14ModerationServer(t, f, ad14InstructorID, identity.RoleInstructor, identity.StatusActive)
	studentServer := ad14ModerationServer(t, f, ad14StudentID, identity.RoleStudent, identity.StatusActive)
	suspendedServer := ad14ModerationServer(t, f, ad14SuspendedID, identity.RoleAdmin, identity.StatusSuspended)

	for _, testCase := range []struct {
		name   string
		server *httptest.Server
		token  string
		want   int
	}{
		{"Instructor", instructorServer, ad14Token(0x73), http.StatusForbidden},
		{"Student", studentServer, ad14Token(0x74), http.StatusForbidden},
		{"Suspended Admin", suspendedServer, ad14Token(0x75), http.StatusForbidden},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			status, _ := ad14Request(t, testCase.server, testCase.token, http.MethodGet, "/api/v1/admin/reports", "")
			if status != testCase.want {
				t.Fatalf("%s queue status = %d, want %d", testCase.name, status, testCase.want)
			}
			status, _ = ad14Request(t, testCase.server, testCase.token, http.MethodGet, "/api/v1/admin/reports/"+reportID, "")
			if status != testCase.want {
				t.Fatalf("%s detail status = %d, want %d", testCase.name, status, testCase.want)
			}
			status, _ = ad14Request(t, testCase.server, testCase.token, http.MethodPost, "/api/v1/admin/reports/"+reportID+"/resolve", `{"action":"DISMISS","reason":"not allowed"}`)
			if status != testCase.want {
				t.Fatalf("%s resolve status = %d, want %d", testCase.name, status, testCase.want)
			}
		})
	}

	status, _ := ad14Request(t, adminServer, ad14Token(0x76), http.MethodGet, "/api/v1/admin/reports/00000000-0000-0000-0000-000000000098", "")
	if status != http.StatusNotFound {
		t.Fatalf("Admin unknown report status = %d, want 404", status)
	}
	anonymousRequest, err := http.NewRequest(http.MethodGet, adminServer.URL+"/api/v1/admin/reports", nil)
	if err != nil {
		t.Fatalf("creating anonymous request: %v", err)
	}
	response, err := adminServer.Client().Do(anonymousRequest)
	if err != nil {
		t.Fatalf("sending anonymous request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous queue status = %d, want 401", response.StatusCode)
	}
}

func equalInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
