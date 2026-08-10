//go:build integration

package httpapi

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	transactionalemail "github.com/Owlah2025/gradex/backend/internal/email"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

func transactionalEmailHarness(
	t *testing.T,
	pool *pgxpool.Pool,
	keyVersion string,
	key []byte,
) (*transactionalemail.Dispatcher, *transactionalemail.FakeSender) {
	t.Helper()
	writer, err := outbox.NewWriter(keyVersion, key)
	if err != nil {
		t.Fatalf("constructing transactional email outbox reader: %v", err)
	}
	repository, err := transactionalemail.NewRepository(pool)
	if err != nil {
		t.Fatalf("constructing transactional email repository: %v", err)
	}
	renderer, err := transactionalemail.NewRenderer(transactionalemail.RendererOptions{
		PublicOrigin: "https://gradex.example",
		FromAddress:  "notify@gradex.example",
		FromName:     "Gradex",
	})
	if err != nil {
		t.Fatalf("constructing transactional email renderer: %v", err)
	}
	sender := transactionalemail.NewFakeSender()
	dispatcher, err := transactionalemail.NewDispatcher(transactionalemail.DispatcherOptions{
		Repository: repository, Outbox: writer, Renderer: renderer, Sender: sender,
		LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("constructing transactional email dispatcher: %v", err)
	}
	return dispatcher, sender
}

func dispatchTransactionalEmail(t *testing.T, dispatcher *transactionalemail.Dispatcher) {
	t.Helper()
	dispatched, err := dispatcher.DispatchPending(context.Background(), 20)
	if err != nil {
		t.Fatalf("dispatching transactional email: %v", err)
	}
	if dispatched == 0 {
		t.Fatal("transactional email dispatcher found no durable intent")
	}
}

func actionCredential(t *testing.T, messages []transactionalemail.CapturedMessage, path string) string {
	t.Helper()
	for _, captured := range messages {
		for _, field := range strings.Fields(captured.Message.Text) {
			if !strings.HasPrefix(field, "https://") || !strings.Contains(field, path) {
				continue
			}
			parsed, err := url.Parse(field)
			if err != nil {
				t.Fatal("transactional email contained a malformed action URL")
			}
			credential, err := url.ParseQuery(parsed.Fragment)
			if err != nil || credential.Get("token") == "" {
				t.Fatal("transactional email action URL contained no fragment credential")
			}
			return credential.Get("token")
		}
	}
	t.Fatalf("no transactional email contained action path %q", path)
	return ""
}
