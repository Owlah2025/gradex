//go:build integration

package httpapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

func TestT7AccountExportIsBoundedEscapedAuditedAndRecentAuthProtected(t *testing.T) {
	ts, pool, _, _, _, _, adminToken, instructorToken := setupAdminPricingAPIServer(t)
	ctx := context.Background()
	accountID := "10000000-0000-0000-0000-000000000777"
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
		VALUES ($1::uuid, $2, $2, 'STUDENT', 'ACTIVE', $3)`,
		accountID, "+export@example.com", "=Formula"); err != nil {
		t.Fatalf("seeding export account: %v", err)
	}

	response := doPricingRequest(t, ts.Client(), http.MethodGet,
		ts.URL+"/api/v1/admin/accounts/export?q=Formula", adminToken, "", adminToken, nil)
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatalf("reading export response: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("export status = %d, body = %s", response.StatusCode, body)
	}
	if got := response.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("export cache control = %q, want no-store", got)
	}
	if !strings.Contains(response.Header.Get("Content-Disposition"), "gradex-user-directory.csv") {
		t.Fatalf("export disposition = %q", response.Header.Get("Content-Disposition"))
	}
	for _, want := range []string{"'=Formula", "'+export@example.com"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("export body missing escaped cell %q: %s", want, body)
		}
	}

	var auditMetadata string
	if err := pool.QueryRow(ctx, `
		SELECT metadata::text FROM audit_events
		 WHERE action = 'ADMIN_EXPORT_CREATED' ORDER BY occurred_at DESC LIMIT 1`).Scan(&auditMetadata); err != nil {
		t.Fatalf("reading export audit: %v", err)
	}
	if strings.Contains(auditMetadata, "Formula") || strings.Contains(auditMetadata, "export@example.com") || !strings.Contains(auditMetadata, `"row_count": 1`) {
		t.Fatalf("export audit metadata = %s", auditMetadata)
	}

	denied := doPricingRequest(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/admin/accounts/export", instructorToken, "", instructorToken, nil)
	denied.Body.Close()
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("instructor export status = %d, want 403", denied.StatusCode)
	}

	ts.Close()
	pool.Close()
	staleServer, _, _, _, _, _, staleToken, _ := setupAdminPricingAPIServerWithRecentAuthWindow(t, time.Nanosecond)
	stale := doPricingRequest(t, staleServer.Client(), http.MethodGet, staleServer.URL+"/api/v1/admin/accounts/export", staleToken, "", staleToken, nil)
	defer stale.Body.Close()
	if stale.StatusCode != http.StatusForbidden {
		t.Fatalf("stale export status = %d, want 403", stale.StatusCode)
	}
}

func TestT7EmailVisibilityMasksGlobalRecipientsAndShowsFullUserContext(t *testing.T) {
	ts, pool, _, _, _, _, adminToken, instructorToken := setupAdminPricingAPIServer(t)
	accountID := "10000000-0000-0000-0000-000000000003"
	seedAdminDirectoryAccounts(t, pool, "10000000-0000-0000-0000-000000000100", []string{accountID, "10000000-0000-0000-0000-000000000004", "10000000-0000-0000-0000-000000000005"})
	eventID := seedTransactionalEmailDelivery(t, pool, accountID, "alice@example.com")
	_ = eventID

	global := doPricingRequest(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/admin/email-deliveries", adminToken, "", adminToken, nil)
	var page struct {
		Items []struct {
			Recipient string `json:"recipient"`
		} `json:"items"`
	}
	decodeAdminResponse(t, global, http.StatusOK, &page)
	if len(page.Items) != 1 || page.Items[0].Recipient != "a***@example.com" {
		t.Fatalf("global email delivery page = %+v", page)
	}

	detail := doPricingRequest(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/admin/accounts/"+accountID, adminToken, "", adminToken, nil)
	var user struct {
		Emails []struct {
			Recipient string `json:"recipient"`
		} `json:"emails"`
	}
	decodeAdminResponse(t, detail, http.StatusOK, &user)
	if len(user.Emails) != 1 || user.Emails[0].Recipient != "alice@example.com" {
		t.Fatalf("User 360 email context = %+v", user.Emails)
	}

	denied := doPricingRequest(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/admin/email-deliveries", instructorToken, "", instructorToken, nil)
	denied.Body.Close()
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("instructor email visibility status = %d, want 403", denied.StatusCode)
	}
}

func TestT7MediaFailuresRouteUsesAdminOperationsCapability(t *testing.T) {
	ts, _, _, _, _, _, adminToken, instructorToken := setupAdminPricingAPIServer(t)
	admin := doPricingRequest(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/admin/media/failures", adminToken, "", adminToken, nil)
	admin.Body.Close()
	if admin.StatusCode != http.StatusOK {
		t.Fatalf("admin media failures status = %d, want 200", admin.StatusCode)
	}
	instructor := doPricingRequest(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/admin/media/failures", instructorToken, "", instructorToken, nil)
	instructor.Body.Close()
	if instructor.StatusCode != http.StatusForbidden {
		t.Fatalf("instructor media failures status = %d, want 403", instructor.StatusCode)
	}
}

func TestT7SearchMetricsPrioritizeZeroResultQueriesAndDeclareRetention(t *testing.T) {
	ts, pool, _, _, _, _, adminToken, _ := setupAdminPricingAPIServer(t)
	ctx := context.Background()
	for _, event := range []struct {
		query   string
		results int
		locale  string
	}{
		{query: "biology", results: 2, locale: "en"},
		{query: "biology", results: 0, locale: "en"},
		{query: "quantum", results: 0, locale: "en"},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO catalog_search_events (normalized_query, result_count, locale) VALUES ($1, $2, $3)`, event.query, event.results, event.locale); err != nil {
			t.Fatalf("seeding search metric event: %v", err)
		}
	}
	response := doPricingRequest(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/admin/metrics/search", adminToken, "", adminToken, nil)
	var result struct {
		TopQueries []struct {
			Query string `json:"query"`
		} `json:"top_queries"`
		ZeroResultQueries []struct {
			Query string `json:"query"`
		} `json:"zero_result_queries"`
		RetentionDays int `json:"retention_days"`
	}
	decodeAdminResponse(t, response, http.StatusOK, &result)
	if len(result.TopQueries) == 0 || result.TopQueries[0].Query != "biology" {
		t.Fatalf("top search queries = %+v", result.TopQueries)
	}
	if len(result.ZeroResultQueries) == 0 || result.ZeroResultQueries[0].Query != "quantum" {
		t.Fatalf("zero-result search queries = %+v", result.ZeroResultQueries)
	}
	if result.RetentionDays != 180 {
		t.Fatalf("search retention days = %d, want 180", result.RetentionDays)
	}
}

func seedTransactionalEmailDelivery(t *testing.T, pool *pgxpool.Pool, accountID, destination string) string {
	t.Helper()
	ctx := context.Background()
	writer, err := outbox.NewWriter("key-v1", []byte("BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"))
	if err != nil {
		t.Fatalf("constructing email visibility writer: %v", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning email visibility fixture: %v", err)
	}
	event := outbox.Event{
		Type: "identity.email_verification_requested", SchemaVersion: 1,
		SourceModule: "IDENTITY_AND_ACCESS", AggregateType: "ACCOUNT",
		AggregateID: accountID, AggregateRevision: 1, CorrelationID: uuid.NewString(),
		SafePayload: map[string]any{"locale": "en", "template_contract": "student-email-verification-v1"},
	}
	eventID, err := writer.Append(ctx, tx, event, outbox.VerificationDelivery{
		Destination: destination, Locale: "en", TemplateContract: "student-email-verification-v1",
		VerificationToken: "test-token", ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("appending email visibility outbox event: %v", err)
	}
	now := time.Now().UTC()
	leaseToken := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		INSERT INTO transactional_email_deliveries
		(event_id, template_contract, locale, status, attempt_count, next_attempt_at, provider,
		 provider_message_id, queued_at, accepted_at, created_at, updated_at)
		VALUES ($1::uuid, 'student-email-verification-v1', 'en', 'ACCEPTED', 1, $2, 'mailpit',
		 'provider-test-1', $2, $2, $2, $2)`, eventID, now); err != nil {
		t.Fatalf("seeding email delivery ledger: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO transactional_email_attempts
		(event_id, attempt_number, lease_token, outcome, provider_message_id, started_at, finished_at)
		VALUES ($1::uuid, 1, $2::uuid, 'ACCEPTED', 'provider-test-1', $3, $3)`, eventID, leaseToken, now); err != nil {
		t.Fatalf("seeding email attempt: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing email visibility fixture: %v", err)
	}
	return eventID
}
