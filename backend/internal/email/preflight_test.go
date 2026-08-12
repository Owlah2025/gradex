package email

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

// The preflight is the only place Gradex asks the provider whether the sending
// domain it is configured to send from is actually verified. Every answer it
// can receive has to reach the operator as a distinct, actionable result.

func inspectorAgainst(t *testing.T, handler http.HandlerFunc) (*SendingDomainInspector, *httptest.Server) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	inspector, err := NewSendingDomainInspector(SendingDomainInspectorOptions{
		APIKey:     config.NewSecret("re_preflight_key_canary"),
		Timeout:    5 * time.Second,
		HTTPClient: server.Client(),
		Endpoint:   strings.Replace(server.URL, "http://", "https://", 1) + "/domains",
	})
	if err != nil {
		t.Fatalf("NewSendingDomainInspector: %v", err)
	}
	return inspector, server
}

func TestPreflightReportsVerifiedSendingDomain(t *testing.T) {
	var authorization, method, accept string
	inspector, _ := inspectorAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		authorization, method, accept = r.Header.Get("Authorization"), r.Method, r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[
			{"id":"1","name":"other.example.com","status":"pending","region":"eu-west-1"},
			{"id":"2","name":"Notifications.Gradex.app","status":"verified","region":"eu-west-1"}
		]}`))
	})

	status, err := inspector.Inspect(context.Background(), "no-reply@notifications.gradex.app")
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !status.Verified || !status.Known {
		t.Fatalf("status = %+v, want a known verified domain", status)
	}
	if status.Domain != "notifications.gradex.app" || status.Status != "verified" || status.Region != "eu-west-1" {
		t.Fatalf("unexpected status detail: %+v", status)
	}
	if method != http.MethodGet {
		t.Fatalf("method = %q, want a read-only GET", method)
	}
	if authorization != "Bearer re_preflight_key_canary" || accept != "application/json" {
		t.Fatalf("provider request headers = %q / %q", authorization, accept)
	}
}

func TestPreflightSeparatesUnverifiedFromAbsentDomains(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantKnown  bool
		wantStatus string
	}{
		{"pending verification", `{"data":[{"name":"notifications.gradex.app","status":"pending"}]}`, true, "pending"},
		{"verification failed", `{"data":[{"name":"notifications.gradex.app","status":"failed"}]}`, true, "failed"},
		{"not started", `{"data":[{"name":"notifications.gradex.app","status":"not_started"}]}`, true, "not_started"},
		{"absent from the account", `{"data":[{"name":"someone-else.example","status":"verified"}]}`, false, "absent"},
		{"empty account", `{"data":[]}`, false, "absent"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inspector, _ := inspectorAgainst(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(test.body))
			})
			status, err := inspector.Inspect(context.Background(), "no-reply@notifications.gradex.app")
			if err != nil {
				t.Fatalf("Inspect: %v", err)
			}
			if status.Verified {
				t.Fatalf("status %+v reported verified", status)
			}
			if status.Known != test.wantKnown || status.Status != test.wantStatus {
				t.Fatalf("status = %+v, want known %t status %q", status, test.wantKnown, test.wantStatus)
			}
		})
	}
}

func TestPreflightFailuresNeverEchoProviderBodyOrKey(t *testing.T) {
	const leak = "re_preflight_key_canary account_owner@example.com"
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"unauthorized", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"` + leak + `"}`))
		}},
		{"server error", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"` + leak + `"}`))
		}},
		{"malformed body", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("not json " + leak))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inspector, _ := inspectorAgainst(t, test.handler)
			_, err := inspector.Inspect(context.Background(), "no-reply@notifications.gradex.app")
			if err == nil {
				t.Fatal("Inspect succeeded on a provider failure")
			}
			if strings.Contains(err.Error(), "re_preflight_key_canary") || strings.Contains(err.Error(), "account_owner@example.com") {
				t.Fatalf("provider failure leaked credential or account detail: %v", err)
			}
		})
	}
}

func TestPreflightRefusesUnsafeProviderEndpoints(t *testing.T) {
	for _, endpoint := range []string{
		"http://api.resend.com/domains",
		"https://user:pass@api.resend.com/domains",
		"://broken",
	} {
		if _, err := NewSendingDomainInspector(SendingDomainInspectorOptions{
			APIKey: config.NewSecret("re_key"), Timeout: time.Second, Endpoint: endpoint,
		}); err == nil {
			t.Fatalf("endpoint %q was accepted", endpoint)
		}
	}
	if _, err := NewSendingDomainInspector(SendingDomainInspectorOptions{
		APIKey: config.Secret{}, Timeout: time.Second,
	}); err == nil {
		t.Fatal("an empty API key was accepted")
	}
}

// A stable Idempotency-Key only prevents a duplicate send while the provider
// still remembers it. Resend expires a key after 24 hours, so the retry budget
// has to finish inside that window or the guarantee silently weakens.
func TestRetryBudgetStaysInsideProviderIdempotencyWindow(t *testing.T) {
	budget := RetryBudget()
	if budget <= 0 {
		t.Fatalf("retry budget = %s, want a positive span", budget)
	}
	if budget >= ProviderIdempotencyWindow {
		t.Fatalf("retry budget %s reaches the provider idempotency window %s; a retry could deliver a second copy",
			budget, ProviderIdempotencyWindow)
	}
}

func TestSenderDomainRejectsAddressesWithoutADomain(t *testing.T) {
	for _, address := range []string{"", "no-reply", "@gradex.app", "no-reply@"} {
		if _, err := SenderDomain(address); err == nil {
			t.Fatalf("address %q was accepted", address)
		}
	}
	domain, err := SenderDomain("No-Reply@Notifications.Gradex.App")
	if err != nil || domain != "notifications.gradex.app" {
		t.Fatalf("SenderDomain = %q, %v", domain, err)
	}
}
