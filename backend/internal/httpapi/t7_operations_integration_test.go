//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
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

	var auditMetadata string
	if err := pool.QueryRow(context.Background(), `
		SELECT metadata::text FROM audit_events
		 WHERE action = 'ADMIN_EMAIL_DELIVERIES_VIEWED'
		 ORDER BY occurred_at DESC LIMIT 1`).Scan(&auditMetadata); err != nil {
		t.Fatalf("reading email visibility audit: %v", err)
	}
	if strings.Contains(auditMetadata, "alice@example.com") || !strings.Contains(auditMetadata, "filter_keys_present") {
		t.Fatalf("email visibility audit metadata contains PII or lacks filter metadata: %s", auditMetadata)
	}

	denied := doPricingRequest(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/admin/email-deliveries", instructorToken, "", instructorToken, nil)
	denied.Body.Close()
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("instructor email visibility status = %d, want 403", denied.StatusCode)
	}
}

func TestT7EmailVisibilityReportsPaginationAndKeepsTheProbeRow(t *testing.T) {
	ts, pool, _, _, _, _, adminToken, _ := setupAdminPricingAPIServer(t)
	accountIDs := []string{
		"10000000-0000-0000-0000-000000000003",
		"10000000-0000-0000-0000-000000000004",
		"10000000-0000-0000-0000-000000000005",
	}
	seedAdminDirectoryAccounts(t, pool, "10000000-0000-0000-0000-000000000100", accountIDs)
	for _, destination := range []string{"alice@example.com", "bob@example.com", "charlie@example.com"} {
		seedTransactionalEmailDelivery(t, pool, accountIDs[0], destination)
	}

	response := doPricingRequest(t, ts.Client(), http.MethodGet,
		ts.URL+"/api/v1/admin/email-deliveries?limit=2", adminToken, "", adminToken, nil)
	var page struct {
		Items   []struct{} `json:"items"`
		HasMore bool       `json:"has_more"`
	}
	decodeAdminResponse(t, response, http.StatusOK, &page)
	if len(page.Items) != 2 || !page.HasMore {
		t.Fatalf("email delivery pagination = items:%d has_more:%t, want 2/true", len(page.Items), page.HasMore)
	}
}

func TestT7EmailVisibilityKeepsUndecryptableRowsReadable(t *testing.T) {
	ts, pool, _, _, _, _, adminToken, _ := setupAdminPricingAPIServer(t)
	accountID := "10000000-0000-0000-0000-000000000003"
	seedAdminDirectoryAccounts(t, pool, "10000000-0000-0000-0000-000000000100", []string{accountID, "10000000-0000-0000-0000-000000000004", "10000000-0000-0000-0000-000000000005"})
	seedTransactionalEmailDeliveryWithKey(t, pool, accountID, "alice@example.com", 0x43)

	global := doPricingRequest(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/admin/email-deliveries", adminToken, "", adminToken, nil)
	var page struct {
		Items []struct {
			Recipient string `json:"recipient"`
		} `json:"items"`
	}
	decodeAdminResponse(t, global, http.StatusOK, &page)
	if len(page.Items) != 1 || page.Items[0].Recipient != "unavailable" {
		t.Fatalf("undecryptable global email row = %+v", page.Items)
	}

	detail := doPricingRequest(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/admin/accounts/"+accountID, adminToken, "", adminToken, nil)
	var user struct {
		Emails []struct {
			Recipient string `json:"recipient"`
		} `json:"emails"`
	}
	decodeAdminResponse(t, detail, http.StatusOK, &user)
	if len(user.Emails) != 1 || user.Emails[0].Recipient != "unavailable" {
		t.Fatalf("undecryptable User 360 email row = %+v", user.Emails)
	}
}

func TestT7User360IncludesEmailHistoryOlderThanAccountCreation(t *testing.T) {
	ts, pool, _, _, _, _, adminToken, _ := setupAdminPricingAPIServer(t)
	accountIDs := []string{
		"10000000-0000-0000-0000-000000000003",
		"10000000-0000-0000-0000-000000000004",
		"10000000-0000-0000-0000-000000000005",
	}
	seedAdminDirectoryAccounts(t, pool, "10000000-0000-0000-0000-000000000100", accountIDs)
	seedTransactionalEmailDelivery(t, pool, accountIDs[0], "alice@example.com")
	for index := 0; index < 500; index++ {
		seedTransactionalEmailDelivery(t, pool, accountIDs[1+index%2], fmt.Sprintf("other-%d@example.com", index))
	}

	detail := doPricingRequest(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/admin/accounts/"+accountIDs[0], adminToken, "", adminToken, nil)
	var user struct {
		Emails []struct {
			Recipient string `json:"recipient"`
		} `json:"emails"`
	}
	decodeAdminResponse(t, detail, http.StatusOK, &user)
	if len(user.Emails) != 1 || user.Emails[0].Recipient != "alice@example.com" {
		t.Fatalf("old User 360 email history = %+v", user.Emails)
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

func TestT7MediaFailuresExposeScannerFailuresAndStaleScanning(t *testing.T) {
	ts, pool, _, ownerID, courseID, _, adminToken, _ := setupAdminPricingAPIServer(t)
	scanErrorID := seedAdminMediaFailure(t, pool, ownerID, courseID, "SCAN_ERROR", "SCAN_UNAVAILABLE")
	staleScanningID := seedAdminMediaFailure(t, pool, ownerID, courseID, "SCANNING", "")

	response := doPricingRequest(t, ts.Client(), http.MethodGet,
		ts.URL+"/api/v1/admin/media/failures", adminToken, "", adminToken, nil)
	var result struct {
		Items []struct {
			AssetVersionID string `json:"asset_version_id"`
			MediaState     string `json:"media_state"`
			State          string `json:"state"`
			RetryAction    string `json:"retry_action"`
		} `json:"items"`
	}
	decodeAdminResponse(t, response, http.StatusOK, &result)
	assertMediaFailure := func(id, mediaState, state, retryAction string) {
		t.Helper()
		for _, item := range result.Items {
			if item.AssetVersionID == id {
				if item.MediaState != mediaState || item.State != state || item.RetryAction != retryAction {
					t.Fatalf("media failure %s = %+v, want state=%s/%s action=%s", id, item, mediaState, state, retryAction)
				}
				return
			}
		}
		t.Fatalf("media failure %s missing from %+v", id, result.Items)
	}
	assertMediaFailure(scanErrorID, "SCAN_ERROR", "failed", "retry")
	assertMediaFailure(staleScanningID, "SCANNING", "stuck", "")
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
		{query: "biology", results: 3, locale: "en"},
		{query: "biology", results: 1, locale: "en"},
		{query: "quantum", results: 0, locale: "en"},
		{query: "quantum", results: 0, locale: "en"},
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

func TestT7AccountExportUsesTheDedicatedTightRatePolicy(t *testing.T) {
	store := &countingExportRateStore{}
	ts, _, _, _, _, _, adminToken, _ := setupAdminPricingAPIServerWithRateStore(t, store)
	for attempt := 1; attempt <= 6; attempt++ {
		response := doPricingRequest(t, ts.Client(), http.MethodGet,
			ts.URL+"/api/v1/admin/accounts/export", adminToken, "", adminToken, nil)
		response.Body.Close()
		want := http.StatusOK
		if attempt == 6 {
			want = http.StatusTooManyRequests
		}
		if response.StatusCode != want {
			t.Fatalf("export attempt %d status = %d, want %d", attempt, response.StatusCode, want)
		}
	}
	for _, entry := range store.firstEntries {
		if strings.Contains(entry.Key, ":identifier:") {
			if entry.Limit != 5 || entry.Window != time.Hour {
				t.Fatalf("export identifier rate entry = %+v, want 5/hour", entry)
			}
			return
		}
	}
	t.Fatalf("export rate store did not receive an identifier dimension: %+v", store.firstEntries)
}

type countingExportRateStore struct {
	calls        int
	firstEntries []ratelimit.Entry
}

func (s *countingExportRateStore) Decide(_ context.Context, entries []ratelimit.Entry) (bool, error) {
	s.calls++
	if s.calls == 1 {
		s.firstEntries = append([]ratelimit.Entry(nil), entries...)
	}
	return s.calls <= 5, nil
}

func seedAdminMediaFailure(t *testing.T, pool *pgxpool.Pool, ownerID, courseID, state, failureCategory string) string {
	t.Helper()
	ctx := context.Background()
	assetID := uuid.NewString()
	versionID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		INSERT INTO media_assets (id, kind, owner_account_id, course_id, visibility)
		VALUES ($1::uuid, 'VIDEO', $2::uuid, $3::uuid, 'PROTECTED')`, assetID, ownerID, courseID); err != nil {
		t.Fatalf("seeding media asset: %v", err)
	}
	if state == "SCANNING" {
		if _, err := pool.Exec(ctx, `
			INSERT INTO media_asset_versions
			(id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes,
			 work_claim_token, work_claimed_at, work_lease_expires_at)
			VALUES ($1::uuid, $2::uuid, 'VIDEO', 'SCANNING', $3, 'fixture-v1', 'video/mp4', 12,
			 'fixture-claim', now() - interval '2 hours', now() - interval '1 hour')`,
			versionID, assetID, "quarantine/"+versionID); err != nil {
			t.Fatalf("seeding stale scanning version: %v", err)
		}
		return versionID
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO media_asset_versions
		(id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes,
		 last_failure_category)
		VALUES ($1::uuid, $2::uuid, 'VIDEO', $3::media_asset_version_state, $4, 'fixture-v1', 'video/mp4', 12, $5)`,
		versionID, assetID, state, "quarantine/"+versionID, failureCategory); err != nil {
		t.Fatalf("seeding media failure version: %v", err)
	}
	return versionID
}

func seedTransactionalEmailDelivery(t *testing.T, pool *pgxpool.Pool, accountID, destination string) string {
	return seedTransactionalEmailDeliveryWithKey(t, pool, accountID, destination, 0x42)
}

func seedTransactionalEmailDeliveryWithKey(t *testing.T, pool *pgxpool.Pool, accountID, destination string, keyByte byte) string {
	t.Helper()
	ctx := context.Background()
	writer, err := outbox.NewWriter("key-v1", bytes.Repeat([]byte{keyByte}, 32))
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
