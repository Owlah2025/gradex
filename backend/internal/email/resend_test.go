package email

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

func validTestMessage() Message {
	return Message{From: "Gradex <notify@gradex.example>", Recipient: "student@example.com", ReplyTo: "support@gradex.example", Subject: "Subject", Text: "Plain body", HTML: "<p>HTML body</p>"}
}

func TestResendSenderUsesOfficialSendContractAndStableIdempotency(t *testing.T) {
	const apiCanary = "RESEND_API_KEY_CANARY"
	var calls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/emails" {
			t.Errorf("request=%s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+apiCanary {
			t.Error("authorization header mismatch")
		}
		if got := r.Header.Get("Idempotency-Key"); got != "gradex/11111111-1111-4111-8111-111111111111" {
			t.Errorf("idempotency=%q", got)
		}
		if r.Header.Get("User-Agent") != "gradex-transactional-email/1" {
			t.Error("missing safe user agent")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		for _, key := range []string{"from", "to", "subject", "text", "html", "reply_to"} {
			if _, ok := body[key]; !ok {
				t.Errorf("payload lacks %s", key)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"49a3999c-0ce1-4ea6-ab68-afcd6dc2e794"}`))
	}))
	defer server.Close()
	sender, err := NewResendSender(ResendOptions{APIKey: config.NewSecret(apiCanary), Timeout: time.Second, HTTPClient: server.Client(), Endpoint: server.URL + "/emails"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := sender.Send(t.Context(), validTestMessage(), "gradex/11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatal(err)
	}
	if result.ProviderMessageID != "49a3999c-0ce1-4ea6-ab68-afcd6dc2e794" || calls != 1 {
		t.Fatalf("result=%+v calls=%d", result, calls)
	}
}

func TestResendSenderClassifiesOnlySafeErrors(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		retry      bool
		retryAfter string
	}{{"rate limit", 429, `{"name":"rate_limit_exceeded","message":"SECRET_BODY_CANARY"}`, true, "17"}, {"concurrent idempotency", 409, `{"name":"concurrent_idempotent_requests"}`, true, ""}, {"invalid recipient", 422, `{"name":"validation_error","message":"student@example.com invalid"}`, false, ""}, {"invalid key", 403, `{"name":"invalid_api_key","message":"RESEND_API_KEY_CANARY"}`, false, ""}, {"provider fault", 500, `{"name":"application_error"}`, true, ""}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.retryAfter != "" {
					w.Header().Set("Retry-After", tc.retryAfter)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			sender, err := NewResendSender(ResendOptions{APIKey: config.NewSecret("RESEND_API_KEY_CANARY"), Timeout: time.Second, HTTPClient: server.Client(), Endpoint: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			_, err = sender.Send(t.Context(), validTestMessage(), "gradex/event")
			failure, ok := AsSendFailure(err)
			if !ok {
				t.Fatalf("error=%T %v", err, err)
			}
			if failure.Transient() != tc.retry {
				t.Errorf("transient=%t want %t", failure.Transient(), tc.retry)
			}
			if tc.retryAfter != "" && failure.RetryAfter != 17*time.Second {
				t.Errorf("retry-after=%s", failure.RetryAfter)
			}
			for _, secret := range []string{"SECRET_BODY_CANARY", "RESEND_API_KEY_CANARY", "student@example.com"} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("error exposed %q", secret)
				}
			}
		})
	}
}

func TestResendSenderTimeoutAndMalformedSuccess(t *testing.T) {
	t.Run("timeout is transient", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(30 * time.Millisecond)
			_, _ = w.Write([]byte(`{"id":"late"}`))
		}))
		defer server.Close()
		sender, _ := NewResendSender(ResendOptions{APIKey: config.NewSecret("key"), Timeout: 5 * time.Millisecond, HTTPClient: server.Client(), Endpoint: server.URL})
		_, err := sender.Send(t.Context(), validTestMessage(), "gradex/event")
		failure, ok := AsSendFailure(err)
		if !ok || !failure.Transient() || failure.Class != "timeout" {
			t.Fatalf("failure=%+v err=%v", failure, err)
		}
	})
	t.Run("malformed success is permanent", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"unexpected":true}`)) }))
		defer server.Close()
		sender, _ := NewResendSender(ResendOptions{APIKey: config.NewSecret("key"), Timeout: time.Second, HTTPClient: server.Client(), Endpoint: server.URL})
		_, err := sender.Send(t.Context(), validTestMessage(), "gradex/event")
		failure, ok := AsSendFailure(err)
		if !ok || failure.Transient() || failure.Class != "malformed_response" {
			t.Fatalf("failure=%+v err=%v", failure, err)
		}
	})
}

func TestResendSenderRejectsHeaderInjectionBeforeNetwork(t *testing.T) {
	sender, err := NewResendSender(ResendOptions{APIKey: config.NewSecret("RESEND_API_KEY_CANARY"), Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	message := validTestMessage()
	message.Recipient = "student@example.com\nBcc: attacker@example.com"
	_, err = sender.Send(t.Context(), message, "gradex/event")
	failure, ok := AsSendFailure(err)
	if !ok || failure.Kind != FailurePermanent || failure.Class != "invalid_message" {
		t.Fatalf("header injection failure = %#v", err)
	}
}
