//go:build integration

package identity

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

// D-118: the contact reminder campaign reaches every ACTIVE, verified Student
// exactly once per campaign, through the transactional outbox, and touches no
// Student data.
func TestContactReminderCampaignQueuesOncePerStudent(t *testing.T) {
	freshSchema(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name, locale, email_verified_at, contact_phone, contact_phone_updated_at) VALUES
		('91000000-0000-0000-0000-000000000001', 'with-number@example.com', 'with-number@example.com', 'STUDENT', 'ACTIVE', 'With Number', 'ar', now(), '+96550012345', now()),
		('91000000-0000-0000-0000-000000000002', 'no-number@example.com', 'no-number@example.com', 'STUDENT', 'ACTIVE', 'No Number', 'en', now(), NULL, NULL),
		('91000000-0000-0000-0000-000000000003', 'unverified@example.com', 'unverified@example.com', 'STUDENT', 'PENDING_VERIFICATION', 'Unverified', 'ar', NULL, NULL, NULL),
		('91000000-0000-0000-0000-000000000004', 'suspended@example.com', 'suspended@example.com', 'STUDENT', 'SUSPENDED', 'Suspended', 'ar', now(), NULL, NULL),
		('91000000-0000-0000-0000-000000000005', 'staff@example.com', 'staff@example.com', 'ADMIN', 'ACTIVE', 'Staff', 'en', now(), NULL, NULL)
	`); err != nil {
		t.Fatalf("seeding accounts: %v", err)
	}
	writer, err := outbox.NewWriter("key-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	queued := func() (events, audits int) {
		t.Helper()
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE event_type=$1`, ContactReminderEventType).Scan(&events); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action='CONTACT_REMINDER_EMAIL_QUEUED'`).Scan(&audits); err != nil {
			t.Fatal(err)
		}
		return events, audits
	}
	var revisionsBefore string
	if err := pool.QueryRow(ctx, `SELECT string_agg(revision::text||coalesce(contact_phone,''), ',' ORDER BY id) FROM accounts`).Scan(&revisionsBefore); err != nil {
		t.Fatal(err)
	}

	if _, err := QueueContactReminders(ctx, pool, writer, ContactReminderRequest{Campaign: "Bad Name!", Send: true}); !errors.Is(err, ErrInvalidContactReminderCampaign) {
		t.Fatalf("invalid campaign err = %v", err)
	}

	dry, err := QueueContactReminders(ctx, pool, nil, ContactReminderRequest{Campaign: "whatsapp-contact-test"})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !dry.DryRun || dry.Eligible != 2 || dry.Queued != 0 || dry.WithNumber != 1 || dry.WithoutNumber != 1 || dry.ExcludedStatus != 2 {
		t.Fatalf("dry run = %+v", dry)
	}
	if events, audits := queued(); events != 0 || audits != 0 {
		t.Fatalf("dry run wrote %d events, %d audits", events, audits)
	}

	sent, err := QueueContactReminders(ctx, pool, writer, ContactReminderRequest{Campaign: "whatsapp-contact-test", Send: true, Now: time.Now()})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if sent.DryRun || sent.Eligible != 2 || sent.Queued != 2 || sent.AlreadyQueued != 0 {
		t.Fatalf("send = %+v", sent)
	}
	if events, audits := queued(); events != 2 || audits != 2 {
		t.Fatalf("send wrote %d events, %d audits; want 2 and 2", events, audits)
	}
	// The safe payload is what the dispatcher matches on; the address lives
	// only in the encrypted protected payload.
	rows, err := pool.Query(ctx, `SELECT safe_payload::text FROM outbox_events WHERE event_type=$1`, ContactReminderEventType)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var safe string
		if err := rows.Scan(&safe); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(safe, `"template_contract": "student-contact-reminder-v1"`) || strings.Contains(safe, "@") {
			t.Errorf("safe payload = %s", safe)
		}
	}
	rows.Close()

	again, err := QueueContactReminders(ctx, pool, writer, ContactReminderRequest{Campaign: "whatsapp-contact-test", Send: true})
	if err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if again.Queued != 0 || again.AlreadyQueued != 2 {
		t.Fatalf("rerun = %+v, want nothing queued", again)
	}
	if events, _ := queued(); events != 2 {
		t.Fatalf("rerun produced %d events, want 2", events)
	}

	// A new campaign is a new send.
	next, err := QueueContactReminders(ctx, pool, writer, ContactReminderRequest{Campaign: "whatsapp-contact-test-2", Send: true})
	if err != nil || next.Queued != 2 {
		t.Fatalf("second campaign = %+v err=%v", next, err)
	}

	var revisionsAfter string
	if err := pool.QueryRow(ctx, `SELECT string_agg(revision::text||coalesce(contact_phone,''), ',' ORDER BY id) FROM accounts`).Scan(&revisionsAfter); err != nil {
		t.Fatal(err)
	}
	if revisionsAfter != revisionsBefore {
		t.Fatalf("campaign modified accounts: %s -> %s", revisionsBefore, revisionsAfter)
	}
}
