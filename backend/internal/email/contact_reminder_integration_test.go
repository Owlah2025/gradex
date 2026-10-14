//go:build integration

package email

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/identity"
)

// D-118: a reminder queued by the operator command is discovered by the
// ordinary dispatcher and delivered once, Arabic and high priority.
func TestDispatcherDeliversQueuedContactReminder(t *testing.T) {
	pool := freshEmailDatabase(t)
	ctx := context.Background()
	repository, writer, renderer := emailTestComponents(t, pool)
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name, locale, email_verified_at)
		VALUES ('92000000-0000-0000-0000-000000000001', 'reminder@example.com', 'Reminder@Example.com', 'STUDENT', 'ACTIVE', 'Reminder', 'en', now())
	`); err != nil {
		t.Fatal(err)
	}
	result, err := identity.QueueContactReminders(ctx, pool, writer, identity.ContactReminderRequest{Campaign: "dispatch-test", Send: true})
	if err != nil || result.Queued != 1 {
		t.Fatalf("queue = %+v err=%v", result, err)
	}
	now := time.Now().UTC().Add(time.Second)
	sender := NewFakeSender()
	dispatcher, err := NewDispatcher(DispatcherOptions{Repository: repository, Outbox: writer, Renderer: renderer, Sender: sender, LeaseDuration: time.Minute, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if count, err := dispatcher.DispatchPending(ctx, 10); err != nil || count != 1 {
		t.Fatalf("dispatch = (%d, %v), want (1, nil)", count, err)
	}
	if count, err := dispatcher.DispatchPending(ctx, 10); err != nil || count != 0 {
		t.Fatalf("second dispatch = (%d, %v), want (0, nil)", count, err)
	}
	messages := sender.Messages()
	if len(messages) != 1 {
		t.Fatalf("delivered %d messages", len(messages))
	}
	message := messages[0].Message
	if message.Recipient != "Reminder@Example.com" || !message.HighPriority ||
		!strings.Contains(message.Subject, "مطلوب") ||
		!strings.Contains(message.HTML, "https://gradex.example/ar/learn/profile?focus=contact") {
		t.Fatalf("delivered message = %+v", message.Subject)
	}
	var status, template string
	if err := pool.QueryRow(ctx, `SELECT status, template_contract FROM transactional_email_deliveries`).Scan(&status, &template); err != nil {
		t.Fatal(err)
	}
	if status != "ACCEPTED" || template != TemplateContactReminder {
		t.Fatalf("delivery = %s/%s", status, template)
	}
}
