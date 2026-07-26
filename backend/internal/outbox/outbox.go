package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	eventTypePattern     = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
	uppercaseTypePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

var forbiddenSafePayloadKeys = map[string]struct{}{
	"destination":        {},
	"email":              {},
	"identifier_hmac":    {},
	"password":           {},
	"password_hash":      {},
	"protected_link":     {},
	"secret_digest":      {},
	"token":              {},
	"verification_token": {},
}

// Append inserts both outbox rows through the caller-owned transaction. The
// caller commits them with source state or rolls all of them back.
func (w *Writer) Append(
	ctx context.Context,
	tx pgx.Tx,
	event Event,
	protected any,
) (string, error) {
	if tx == nil {
		return "", errors.New("outbox append requires a transaction")
	}
	if strings.TrimSpace(event.ID) == "" {
		event.ID = uuid.NewString()
	}
	if err := w.validateEvent(event); err != nil {
		return "", err
	}

	safePayload, err := json.Marshal(event.SafePayload)
	if err != nil {
		return "", fmt.Errorf("encoding safe outbox payload: %w", err)
	}
	sealed, err := w.protect(ctx, event, protected)
	if err != nil {
		return "", err
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO outbox_events
		   (id, event_type, schema_version, source_module, aggregate_type,
		    aggregate_id, aggregate_revision, safe_payload, available_at, correlation_id)
		 VALUES (
		   $1::uuid, $2, $3, $4, $5, $6::uuid, $7, $8,
		   COALESCE($9::timestamptz, now()), $10
		 )`,
		event.ID,
		event.Type,
		event.SchemaVersion,
		event.SourceModule,
		event.AggregateType,
		event.AggregateID,
		event.AggregateRevision,
		safePayload,
		event.AvailableAt,
		event.CorrelationID,
	); err != nil {
		return "", fmt.Errorf("inserting outbox event: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO outbox_protected_payloads
		   (event_id, key_version, nonce, ciphertext)
		 VALUES ($1::uuid, $2, $3, $4)`,
		event.ID,
		sealed.KeyVersion,
		sealed.Nonce,
		sealed.Ciphertext,
	); err != nil {
		return "", fmt.Errorf("inserting protected outbox payload: %w", err)
	}
	return event.ID, nil
}

func (w *Writer) validateEvent(event Event) error {
	if _, err := uuid.Parse(event.ID); event.ID != "" && err != nil {
		return errors.New("outbox event ID must be an opaque UUID")
	}
	if !eventTypePattern.MatchString(event.Type) {
		return errors.New("outbox event type is invalid")
	}
	if event.SchemaVersion < 1 {
		return errors.New("outbox schema version must be positive")
	}
	if event.SourceModule != "IDENTITY_AND_ACCESS" {
		return errors.New("outbox source module is unsupported")
	}
	if !uppercaseTypePattern.MatchString(event.AggregateType) {
		return errors.New("outbox aggregate type is invalid")
	}
	if _, err := uuid.Parse(event.AggregateID); err != nil {
		return errors.New("outbox aggregate ID must be an opaque UUID")
	}
	if event.AggregateRevision < 1 {
		return errors.New("outbox aggregate revision must be positive")
	}
	if strings.TrimSpace(event.CorrelationID) == "" {
		return errors.New("outbox correlation ID is required")
	}

	encoded, err := json.Marshal(event.SafePayload)
	if err != nil {
		return fmt.Errorf("encoding safe payload for validation: %w", err)
	}
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		return fmt.Errorf("reading safe payload for validation: %w", err)
	}
	if value == nil {
		return errors.New("safe payload must be a JSON object")
	}
	object, ok := value.(map[string]any)
	if !ok {
		return errors.New("safe payload must be a JSON object")
	}
	if key, found := findForbiddenKey(object); found {
		return fmt.Errorf("safe payload contains protected field %q", key)
	}
	return nil
}

func findForbiddenKey(value any) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := strings.ToLower(strings.TrimSpace(key))
			if _, forbidden := forbiddenSafePayloadKeys[normalized]; forbidden {
				return key, true
			}
			if key, found := findForbiddenKey(child); found {
				return key, true
			}
		}
	case []any:
		for _, child := range typed {
			if key, found := findForbiddenKey(child); found {
				return key, true
			}
		}
	}
	return "", false
}
