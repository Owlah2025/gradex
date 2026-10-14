package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

// D-118: a one-off, operator-run email campaign asking every registered
// Student to add or confirm their WhatsApp number.
//
// It reuses the transactional email pipeline end to end — the outbox, the
// worker dispatcher, the provider adapter and the Admin delivery log — so it
// has the same retries, the same sender and the same evidence as every other
// Gradex message. It is never scheduled and never triggered by a request: an
// operator runs it, first as a dry run.

const (
	ContactReminderEventType        = "identity.contact_reminder_requested"
	ContactReminderTemplateContract = "student-contact-reminder-v1"
	contactReminderAuditAction      = "CONTACT_REMINDER_EMAIL_QUEUED"
	// One advisory lock for every campaign, so two operators running the
	// command at once cannot both see a Student as not yet reminded.
	contactReminderLockKey = 7_117_118
)

var contactReminderCampaignPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,63}$`)

var ErrInvalidContactReminderCampaign = errors.New("campaign must be 3-64 lowercase letters, digits or hyphens")

type ContactReminderRequest struct {
	// Campaign names this send. A Student is emailed at most once per
	// campaign, however often the command runs.
	Campaign string
	// Send queues the emails. Without it the command only counts.
	Send bool
	Now  time.Time
}

type ContactReminderResult struct {
	Campaign       string `json:"campaign"`
	DryRun         bool   `json:"dry_run"`
	Eligible       int    `json:"eligible"`
	AlreadyQueued  int    `json:"already_queued"`
	Queued         int    `json:"queued"`
	WithNumber     int    `json:"eligible_with_number"`
	WithoutNumber  int    `json:"eligible_without_number"`
	ExcludedStatus int    `json:"excluded_unverified_or_inactive"`
}

// QueueContactReminders selects every ACTIVE Student with a verified email,
// skips those already reminded in this campaign, and — when Send is set —
// appends one outbox event per remaining Student plus an audit row, all in
// one transaction. Recipient addresses enter only the encrypted protected
// payload; the audit trail and the result carry counts and account ids.
func QueueContactReminders(
	ctx context.Context,
	pool *pgxpool.Pool,
	writer *outbox.Writer,
	request ContactReminderRequest,
) (ContactReminderResult, error) {
	if !contactReminderCampaignPattern.MatchString(request.Campaign) {
		return ContactReminderResult{}, ErrInvalidContactReminderCampaign
	}
	if pool == nil || (request.Send && writer == nil) {
		return ContactReminderResult{}, errors.New("contact reminder requires a database pool and, to send, an outbox writer")
	}
	now := request.Now
	if now.IsZero() {
		now = time.Now()
	}
	result := ContactReminderResult{Campaign: request.Campaign, DryRun: !request.Send}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return ContactReminderResult{}, fmt.Errorf("beginning contact reminder transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, contactReminderLockKey); err != nil {
		return ContactReminderResult{}, fmt.Errorf("locking contact reminder campaign: %w", err)
	}
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM accounts
		 WHERE role = 'STUDENT' AND NOT (status = 'ACTIVE' AND email_verified_at IS NOT NULL)
	`).Scan(&result.ExcludedStatus); err != nil {
		return ContactReminderResult{}, fmt.Errorf("counting excluded Students: %w", err)
	}

	type recipient struct {
		id, email, locale string
		hasNumber         bool
		reminded          bool
	}
	rows, err := tx.Query(ctx, `
		SELECT a.id::text, a.email, a.locale, a.contact_phone IS NOT NULL,
		       EXISTS (
		         SELECT 1 FROM audit_events ae
		          WHERE ae.action = $1 AND ae.target_type = 'ACCOUNT' AND ae.target_id = a.id::text
		            AND ae.metadata->>'campaign' = $2
		       )
		  FROM accounts a
		 WHERE a.role = 'STUDENT' AND a.status = 'ACTIVE' AND a.email_verified_at IS NOT NULL
		 ORDER BY a.created_at, a.id
	`, contactReminderAuditAction, request.Campaign)
	if err != nil {
		return ContactReminderResult{}, fmt.Errorf("selecting contact reminder recipients: %w", err)
	}
	recipients := make([]recipient, 0)
	for rows.Next() {
		var item recipient
		if err := rows.Scan(&item.id, &item.email, &item.locale, &item.hasNumber, &item.reminded); err != nil {
			rows.Close()
			return ContactReminderResult{}, fmt.Errorf("scanning contact reminder recipient: %w", err)
		}
		recipients = append(recipients, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return ContactReminderResult{}, fmt.Errorf("iterating contact reminder recipients: %w", err)
	}

	for _, item := range recipients {
		result.Eligible++
		if item.hasNumber {
			result.WithNumber++
		} else {
			result.WithoutNumber++
		}
		if item.reminded {
			result.AlreadyQueued++
			continue
		}
		if !request.Send {
			continue
		}
		locale := item.locale
		if locale != string(LocaleArabic) && locale != string(LocaleEnglish) {
			locale = string(LocaleArabic)
		}
		if _, err := writer.Append(ctx, tx, outbox.Event{
			ID:                uuid.NewString(),
			Type:              ContactReminderEventType,
			SchemaVersion:     1,
			SourceModule:      "IDENTITY_AND_ACCESS",
			AggregateType:     "ACCOUNT",
			AggregateID:       item.id,
			AggregateRevision: 1,
			CorrelationID:     uuid.NewString(),
			SafePayload: map[string]any{
				"account_id":        item.id,
				"campaign":          request.Campaign,
				"locale":            locale,
				"template_contract": ContactReminderTemplateContract,
			},
		}, outbox.NoticeDelivery{
			Destination:      item.email,
			Locale:           locale,
			TemplateContract: ContactReminderTemplateContract,
		}); err != nil {
			return ContactReminderResult{}, fmt.Errorf("queueing contact reminder: %w", err)
		}
		metadata, err := json.Marshal(map[string]any{"campaign": request.Campaign, "template_contract": ContactReminderTemplateContract})
		if err != nil {
			return ContactReminderResult{}, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_events (
				actor_role, actor_descriptor, action, module, target_type, target_id, reason, metadata, occurred_at
			) VALUES (
				'SYSTEM', 'gradex-contact-reminder', $1, 'NOTIFICATIONS', 'ACCOUNT', $2,
				'Operator campaign asking the Student to add or confirm a WhatsApp number', $3, $4
			)
		`, contactReminderAuditAction, item.id, metadata, now); err != nil {
			return ContactReminderResult{}, fmt.Errorf("auditing contact reminder: %w", err)
		}
		result.Queued++
	}

	if !request.Send {
		return result, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return ContactReminderResult{}, fmt.Errorf("committing contact reminders: %w", err)
	}
	return result, nil
}
