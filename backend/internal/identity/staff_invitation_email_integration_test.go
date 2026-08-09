//go:build integration

package identity

import (
	"bytes"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/config"
	transactionalemail "github.com/Owlah2025/gradex/backend/internal/email"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

// staffInvitationEmailKey is shared by the producer and the dispatcher in this
// test, because the dispatcher must decrypt the very payload the real identity
// transaction wrote. A separate key would prove nothing.
var staffInvitationEmailKey = bytes.Repeat([]byte{0x51}, 32)

// staffEmailPipeline builds the real delivery pipeline over the same outbox
// writer the invitation producer uses.
func staffEmailPipeline(t *testing.T, p *pgxpool.Pool, writer *outbox.Writer) (*transactionalemail.Dispatcher, *transactionalemail.FakeSender) {
	t.Helper()
	repository, err := transactionalemail.NewRepository(p)
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

// staffActionCredential pulls the bearer out of the action link exactly as an
// invitee's mail client would present it: from the delivered message body.
func staffActionCredential(t *testing.T, messages []transactionalemail.CapturedMessage) string {
	t.Helper()
	for _, captured := range messages {
		for _, field := range strings.Fields(captured.Message.Text) {
			if !strings.HasPrefix(field, "https://") || !strings.Contains(field, "/staff/accept") {
				continue
			}
			parsed, err := url.Parse(field)
			if err != nil {
				t.Fatal("staff invitation email contained a malformed action URL")
			}
			credential, err := url.ParseQuery(parsed.Fragment)
			if err != nil || credential.Get("token") == "" {
				t.Fatal("staff invitation action URL carried no fragment credential")
			}
			return credential.Get("token")
		}
	}
	t.Fatal("no delivered email contained the /staff/accept action link")
	return ""
}

// TestStaffInvitationEmailReachesTheInviteeAndCompletes is the regression the
// independent review demanded.
//
// The bug it guards was invisible to every prior test because the existing
// email coverage hand-seeded template_contract into the safe payload. The real
// identity producer never wrote that field, so discovery — which joins on it —
// matched nothing and no staff invitation was ever mailed. Nothing here seeds
// that field: the payload asserted below is whatever the real transaction
// actually committed.
func TestStaffInvitationEmailReachesTheInviteeAndCompletes(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)
	now := time.Now().UTC()

	writer, err := outbox.NewWriter("test-v1", staffInvitationEmailKey)
	if err != nil {
		t.Fatalf("constructing invitation outbox writer: %v", err)
	}
	adminID, adminPrincipal := createTestAdmin(t, p, "email_inviter_admin@example.com")
	adminSession, _ := createTestSession(t, p, adminID, now)

	// 1. An Admin creates a staff invitation through the real producer.
	const inviteeEmail = "invited.instructor@example.com"
	conn, err := p.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquiring connection: %v", err)
	}
	defer conn.Release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning invitation transaction: %v", err)
	}
	issued, err := CreateStaffInvitation(ctx, tx, CreateStaffInvitationRequest{
		Outbox:           writer,
		ActorPrincipal:   adminPrincipal,
		ActorSession:     adminSession,
		RecentAuthWindow: 10 * time.Minute,
		Email:            inviteeEmail,
		Role:             RoleInstructor,
		Locale:           LocaleEnglish,
		Now:              now,
		RequestID:        "req-staff-email-acceptance",
	})
	if err != nil {
		t.Fatalf("creating staff invitation: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing staff invitation: %v", err)
	}

	// 2. The committed event carries the discovery contract in its safe
	//    payload. This assertion is the direct inverse of the proven defect.
	var eventID, contract, locale string
	var safePayload string
	if err := p.QueryRow(ctx,
		`SELECT id::text, safe_payload->>'template_contract', safe_payload->>'locale', safe_payload::text
		   FROM outbox_events
		  WHERE event_type='identity.staff_invitation_created' AND aggregate_id=$1::uuid`,
		issued.Invitation.ID,
	).Scan(&eventID, &contract, &locale, &safePayload); err != nil {
		t.Fatalf("reading the staff invitation outbox event: %v", err)
	}
	if contract != "staff-invitation-v1" {
		t.Fatalf("safe payload template_contract = %q, want staff-invitation-v1", contract)
	}
	if locale != "en" {
		t.Fatalf("safe payload locale = %q, want en", locale)
	}
	// The safe payload is readable by anything that can read the table, so the
	// bearer must not be in it. Only the encrypted payload may carry it.
	if strings.Contains(safePayload, issued.BearerToken) || strings.Contains(safePayload, inviteeEmail) {
		t.Fatal("staff invitation safe payload exposed the bearer or the recipient")
	}

	// 3. Discovery finds the real event and creates a ledger row.
	dispatcher, sender := staffEmailPipeline(t, p, writer)
	dispatched, err := dispatcher.DispatchPending(ctx, 20)
	if err != nil {
		t.Fatalf("dispatching staff invitation email: %v", err)
	}
	if dispatched != 1 {
		t.Fatalf("dispatched = %d, want 1 real staff invitation delivery", dispatched)
	}
	var ledgerContract, status string
	var attempts int
	if err := p.QueryRow(ctx,
		`SELECT template_contract, status, attempt_count
		   FROM transactional_email_deliveries WHERE event_id=$1::uuid`, eventID,
	).Scan(&ledgerContract, &status, &attempts); err != nil {
		t.Fatalf("reading the staff invitation delivery ledger: %v", err)
	}
	if ledgerContract != "staff-invitation-v1" || status != "ACCEPTED" || attempts != 1 {
		t.Fatalf("ledger = %s/%s/%d, want staff-invitation-v1/ACCEPTED/1", ledgerContract, status, attempts)
	}

	// 4. The rendered email addresses the intended invitee and carries the
	//    action link.
	messages := sender.Messages()
	if len(messages) != 1 {
		t.Fatalf("delivered messages = %d, want 1", len(messages))
	}
	delivered := messages[0]
	if delivered.Message.Recipient != inviteeEmail {
		t.Fatalf("recipient = %q, want the invited address", delivered.Message.Recipient)
	}
	if delivered.IdempotencyKey != "gradex/"+eventID {
		t.Fatalf("idempotency key = %q, want the immutable event id", delivered.IdempotencyKey)
	}
	bearer := staffActionCredential(t, messages)

	// 5. Neither the ledger nor the attempt evidence may hold the credential.
	var evidence string
	if err := p.QueryRow(ctx,
		`SELECT coalesce(string_agg(a.failure_class || ':' || coalesce(a.provider_code,'') || ':' ||
		                            coalesce(a.provider_message_id,''), '|'), '')
		   FROM transactional_email_attempts a WHERE a.event_id=$1::uuid`, eventID,
	).Scan(&evidence); err != nil {
		t.Fatalf("reading attempt evidence: %v", err)
	}
	if strings.Contains(evidence, bearer) || strings.Contains(evidence, inviteeEmail) {
		t.Fatal("delivery attempt evidence exposed the bearer or the recipient")
	}

	// 6. Retrying dispatch produces no second email and no second ledger row.
	repeat, err := dispatcher.DispatchPending(ctx, 20)
	if err != nil {
		t.Fatalf("re-dispatching: %v", err)
	}
	if repeat != 0 {
		t.Fatalf("re-dispatch = %d, want 0", repeat)
	}
	if len(sender.Messages()) != 1 {
		t.Fatal("re-dispatch produced a duplicate staff invitation email")
	}
	var invitationsAfterRetry int
	if err := p.QueryRow(ctx,
		`SELECT count(*) FROM staff_invitations WHERE id=$1::uuid AND state='PENDING'`,
		issued.Invitation.ID,
	).Scan(&invitationsAfterRetry); err != nil {
		t.Fatal(err)
	}
	if invitationsAfterRetry != 1 {
		t.Fatal("email retry changed invitation domain state")
	}

	// 7. The invitee previews the invitation with the credential from the email.
	previewTx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewStaffInvitation(ctx, previewTx, bearer, now)
	if err != nil {
		t.Fatalf("previewing with the emailed credential: %v", err)
	}
	if preview.State != InvitationPending || preview.InvitedRole != RoleInstructor {
		t.Fatalf("preview = %s/%s, want PENDING/INSTRUCTOR", preview.State, preview.InvitedRole)
	}
	if err := previewTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	// 8. Acceptance completes with the emailed credential.
	acceptTx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result, err := CompleteStaffInvitation(ctx, acceptTx, CompleteStaffInvitationRequest{
		Bearer:      bearer,
		DisplayName: "Invited Instructor",
		Password:    config.NewSecret("a sufficiently long staff passphrase"),
		Compromised: clearCompromisedSource(),
		Now:         now.Add(time.Minute),
		RequestID:   "req-staff-email-accept",
	})
	if err != nil {
		t.Fatalf("completing the invitation with the emailed credential: %v", err)
	}
	if result.InvitedRole != RoleInstructor || result.AccountID == "" {
		t.Fatalf("completion = %+v, want an INSTRUCTOR account", result)
	}
	if err := acceptTx.Commit(ctx); err != nil {
		t.Fatalf("committing acceptance: %v", err)
	}

	// 9. The emailed credential is single-use: a replay is refused.
	replayTx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = replayTx.Rollback(ctx) }()
	if _, err := CompleteStaffInvitation(ctx, replayTx, CompleteStaffInvitationRequest{
		Bearer:      bearer,
		DisplayName: "Replay Attempt",
		Password:    config.NewSecret("another sufficiently long passphrase"),
		Compromised: clearCompromisedSource(),
		Now:         now.Add(2 * time.Minute),
		RequestID:   "req-staff-email-replay",
	}); !errors.Is(err, ErrInvitationAlreadyUsed) {
		t.Fatalf("replaying the emailed credential returned %v, want ErrInvitationAlreadyUsed", err)
	}
}

// TestStaffInvitationEmailRefusesWrongAndExpiredCredentials keeps the emailed
// credential bound to the same rules the identity surface already enforces.
func TestStaffInvitationEmailRefusesWrongAndExpiredCredentials(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)
	now := time.Now().UTC()

	writer, err := outbox.NewWriter("test-v1", staffInvitationEmailKey)
	if err != nil {
		t.Fatal(err)
	}
	adminID, adminPrincipal := createTestAdmin(t, p, "expiry_inviter_admin@example.com")
	adminSession, _ := createTestSession(t, p, adminID, now)

	conn, err := p.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateStaffInvitation(ctx, tx, CreateStaffInvitationRequest{
		Outbox:           writer,
		ActorPrincipal:   adminPrincipal,
		ActorSession:     adminSession,
		RecentAuthWindow: 10 * time.Minute,
		Email:            "expiring.instructor@example.com",
		Role:             RoleInstructor,
		Locale:           LocaleEnglish,
		TTL:              time.Hour,
		Now:              now,
		RequestID:        "req-staff-email-expiry",
	}); err != nil {
		t.Fatalf("creating staff invitation: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	dispatcher, sender := staffEmailPipeline(t, p, writer)
	if dispatched, err := dispatcher.DispatchPending(ctx, 20); err != nil || dispatched != 1 {
		t.Fatalf("dispatch = (%d, %v), want (1, nil)", dispatched, err)
	}
	bearer := staffActionCredential(t, sender.Messages())

	// A credential that was never issued is refused.
	wrongTx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PreviewStaffInvitation(ctx, wrongTx, bearer+"tampered", now); !errors.Is(err, ErrInvitationInvalid) {
		t.Fatalf("tampered credential returned %v, want ErrInvitationInvalid", err)
	}
	if err := wrongTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	// The emailed credential expires on the schedule the invitation set.
	expiredTx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = expiredTx.Rollback(ctx) }()
	preview, err := PreviewStaffInvitation(ctx, expiredTx, bearer, now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("previewing after expiry: %v", err)
	}
	if preview.State != InvitationExpired {
		t.Fatalf("expired preview state = %s, want EXPIRED", preview.State)
	}
	if _, err := CompleteStaffInvitation(ctx, expiredTx, CompleteStaffInvitationRequest{
		Bearer:      bearer,
		DisplayName: "Too Late",
		Password:    config.NewSecret("a sufficiently long staff passphrase"),
		Compromised: clearCompromisedSource(),
		Now:         now.Add(2 * time.Hour),
		RequestID:   "req-staff-email-expired",
	}); !errors.Is(err, ErrInvitationExpired) {
		t.Fatalf("expired completion returned %v, want ErrInvitationExpired", err)
	}
}
