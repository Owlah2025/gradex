package admin

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

const (
	emailVisibilityTargetType = "TRANSACTIONAL_EMAIL_DELIVERIES"
	emailVisibilityTargetID   = "DELIVERIES"
	emailUserScanLimit        = 500
)

// ProtectedPayloadReader is deliberately narrower than the outbox writer. The
// admin read model can authenticate one destination without gaining any append
// or key-management capability.
type ProtectedPayloadReader interface {
	OpenProtectedPayload(context.Context, outbox.Event, outbox.StoredProtectedPayload, any) error
}

type protectedEmailPayload struct {
	Destination string `json:"destination"`
}

func (r *Repository) ListEmailDeliveries(
	ctx context.Context,
	request EmailDeliveriesRequest,
) (EmailDeliveriesResult, error) {
	if err := validateEmailDeliveriesRequest(request); err != nil {
		return EmailDeliveriesResult{}, err
	}
	if r == nil || r.pool == nil {
		return EmailDeliveriesResult{}, ErrRepositoryNil
	}
	if r.emailPayloadReader == nil {
		return EmailDeliveriesResult{}, ErrEmailVisibilityUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return EmailDeliveriesResult{}, fmt.Errorf("beginning email delivery read: %w", err)
	}
	defer tx.Rollback(ctx)
	result, err := r.queryEmailDeliveries(ctx, tx, request)
	if err != nil {
		return EmailDeliveriesResult{}, err
	}
	if err := WritePrivilegedReadAudit(ctx, tx, PrivilegedReadAudit{
		Principal:     request.Principal,
		CorrelationID: request.CorrelationID,
		Action:        ActionEmailDeliveriesViewed,
		Module:        "NOTIFICATIONS",
		TargetType:    emailVisibilityTargetType,
		TargetID:      emailVisibilityTargetID,
		Reason:        "privileged read: transactional email delivery visibility",
		Metadata: map[string]any{
			"filter_keys_present": emailDeliveryFilterKeys(request),
			"result_count":        len(result.Items),
			"page":                request.Page,
		},
	}); err != nil {
		return EmailDeliveriesResult{}, fmt.Errorf("auditing email delivery read: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return EmailDeliveriesResult{}, fmt.Errorf("committing email delivery read: %w", err)
	}
	return result, nil
}

func validateEmailDeliveriesRequest(request EmailDeliveriesRequest) error {
	if err := validateIdentityRead(request.Principal); err != nil {
		return err
	}
	if !request.Locale.Valid() || !validPage(request.Page, request.Limit) {
		return ErrInvalidInput
	}
	if request.State != "" && !validEmailDeliveryState(request.State) {
		return ErrInvalidInput
	}
	if utf8.RuneCountInString(strings.TrimSpace(request.Kind)) > 120 {
		return ErrInvalidInput
	}
	if request.AccountID != "" {
		if _, err := uuid.Parse(request.AccountID); err != nil {
			return ErrInvalidInput
		}
	}
	if request.OccurredFrom != nil && request.OccurredTo != nil && !request.OccurredFrom.Before(*request.OccurredTo) {
		return ErrInvalidInput
	}
	return nil
}

func validEmailDeliveryState(state string) bool {
	switch state {
	case "queued", "attempted", "delivered", "failed":
		return true
	default:
		return false
	}
}

func emailDeliveryFilterKeys(request EmailDeliveriesRequest) []string {
	keys := make([]string, 0, 5)
	if request.State != "" {
		keys = append(keys, "state")
	}
	if request.Kind != "" {
		keys = append(keys, "kind")
	}
	if request.OccurredFrom != nil {
		keys = append(keys, "from")
	}
	if request.OccurredTo != nil {
		keys = append(keys, "to")
	}
	if request.AccountID != "" {
		keys = append(keys, "accountId")
	}
	return keys
}

func (r *Repository) queryEmailDeliveries(
	ctx context.Context,
	tx pgx.Tx,
	request EmailDeliveriesRequest,
) (EmailDeliveriesResult, error) {
	where, args := emailDeliveryWhere(request)
	sqlLimit := request.Limit + 1
	offset := (request.Page - 1) * request.Limit
	if request.RecipientEmail != "" {
		sqlLimit = emailUserScanLimit
		offset = 0
	}
	args = append(args, sqlLimit, offset)
	limitPlaceholder := len(args) - 1
	offsetPlaceholder := len(args)
	rows, err := tx.Query(ctx, `
		SELECT e.id::text, e.event_type, e.schema_version, e.source_module,
		       e.aggregate_type, e.aggregate_id::text, e.aggregate_revision,
		       e.correlation_id, e.available_at,
		       d.template_contract, d.locale, d.status, d.attempt_count,
		       d.last_failure_class, d.queued_at, d.accepted_at, d.terminal_at,
		       d.updated_at, attempt.attempted_at,
		       p.key_version, p.nonce, p.ciphertext
		  FROM transactional_email_deliveries d
		  JOIN outbox_events e ON e.id = d.event_id
		  JOIN outbox_protected_payloads p ON p.event_id = d.event_id
		  LEFT JOIN LATERAL (
			SELECT MAX(a.started_at) AS attempted_at
			  FROM transactional_email_attempts a
			 WHERE a.event_id = d.event_id
		  ) attempt ON TRUE
		 WHERE `+where+`
		 ORDER BY d.updated_at DESC, e.id DESC
		 LIMIT $`+fmt.Sprint(limitPlaceholder)+` OFFSET $`+fmt.Sprint(offsetPlaceholder), args...)
	if err != nil {
		return EmailDeliveriesResult{}, fmt.Errorf("querying email delivery rows: %w", err)
	}
	defer rows.Close()

	items := make([]EmailDelivery, 0, request.Limit)
	for rows.Next() {
		item, recipient, err := r.scanEmailDelivery(ctx, rows)
		if err != nil {
			return EmailDeliveriesResult{}, err
		}
		if request.RecipientEmail != "" && !strings.EqualFold(recipient, request.RecipientEmail) {
			continue
		}
		if !request.RevealRecipient {
			item.Recipient = maskEmail(recipient)
		} else {
			item.Recipient = recipient
		}
		items = append(items, item)
		if len(items) == request.Limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return EmailDeliveriesResult{}, fmt.Errorf("iterating email delivery rows: %w", err)
	}
	hasMore := request.RecipientEmail == "" && len(items) > request.Limit
	if hasMore {
		items = items[:request.Limit]
	}
	return EmailDeliveriesResult{Items: items, Page: request.Page, Limit: request.Limit, HasMore: hasMore}, nil
}

func emailDeliveryWhere(request EmailDeliveriesRequest) (string, []any) {
	conditions := []string{"TRUE"}
	args := make([]any, 0, 6)
	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}
	switch request.State {
	case "queued":
		conditions = append(conditions, "d.status = 'QUEUED'")
	case "attempted":
		conditions = append(conditions, "d.status = 'SENDING'")
	case "delivered":
		conditions = append(conditions, "d.status = 'ACCEPTED'")
	case "failed":
		conditions = append(conditions, "d.status IN ('PERMANENT_FAILED', 'EXHAUSTED')")
	}
	if strings.TrimSpace(request.Kind) != "" {
		add("d.template_contract = $%d", strings.TrimSpace(request.Kind))
	}
	if request.AccountID != "" {
		add("e.aggregate_id = $%d::uuid", request.AccountID)
	}
	if request.OccurredFrom != nil {
		add("e.occurred_at >= $%d", request.OccurredFrom.UTC())
	}
	if request.OccurredTo != nil {
		add("e.occurred_at < $%d", request.OccurredTo.UTC())
	}
	return strings.Join(conditions, " AND "), args
}

func (r *Repository) scanEmailDelivery(ctx context.Context, rows pgx.Rows) (EmailDelivery, string, error) {
	var event outbox.Event
	var availableAt time.Time
	var templateContract, locale, status string
	var attemptCount int
	var lastFailure *string
	var queuedAt, updatedAt time.Time
	var acceptedAt, terminalAt, attemptedAt *time.Time
	var payload outbox.StoredProtectedPayload
	var nonce, ciphertext []byte
	if err := rows.Scan(
		&event.ID, &event.Type, &event.SchemaVersion, &event.SourceModule,
		&event.AggregateType, &event.AggregateID, &event.AggregateRevision,
		&event.CorrelationID, &availableAt, &templateContract, &locale, &status,
		&attemptCount, &lastFailure, &queuedAt, &acceptedAt, &terminalAt,
		&updatedAt, &attemptedAt, &payload.KeyVersion, &nonce, &ciphertext,
	); err != nil {
		return EmailDelivery{}, "", fmt.Errorf("scanning email delivery row: %w", err)
	}
	event.AvailableAt = &availableAt
	payload.Nonce = nonce
	payload.Ciphertext = ciphertext
	var protected protectedEmailPayload
	if err := r.emailPayloadReader.OpenProtectedPayload(ctx, event, payload, &protected); err != nil {
		return EmailDelivery{}, "", fmt.Errorf("opening email delivery destination: %w", err)
	}
	item := EmailDelivery{
		ID: event.ID, Kind: templateContract, Locale: identity.Locale(locale),
		State: normalizedEmailState(status), Recipient: protected.Destination,
		QueuedAt: queuedAt, AttemptedAt: attemptedAt, DeliveredAt: acceptedAt,
		FailedAt: terminalAt, UpdatedAt: updatedAt, AttemptCount: attemptCount,
	}
	if lastFailure != nil {
		item.LastErrorClass = *lastFailure
	}
	return item, protected.Destination, nil
}

func normalizedEmailState(status string) string {
	switch status {
	case "QUEUED":
		return "queued"
	case "SENDING":
		return "attempted"
	case "ACCEPTED":
		return "delivered"
	default:
		return "failed"
	}
}

func maskEmail(value string) string {
	at := strings.LastIndexByte(value, '@')
	if at <= 0 || at == len(value)-1 {
		return "***"
	}
	local := []rune(value[:at])
	return string(local[0]) + "***@" + value[at+1:]
}
