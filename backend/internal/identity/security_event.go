package identity

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type securityEventAppend struct {
	eventType      string
	accountID      string
	actionSecretID string
	revision       int
	requestID      string
	evidence       map[string]any
}

func appendIdentitySecurityEvent(
	ctx context.Context,
	tx pgx.Tx,
	event securityEventAppend,
) error {
	encodedEvidence, err := json.Marshal(event.evidence)
	if err != nil {
		return fmt.Errorf("encoding Identity security evidence: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO identity_security_events
		   (event_type, account_id, action_secret_id, account_revision, request_id, evidence)
		 VALUES ($1, $2::uuid, NULLIF($3, '')::uuid, $4, $5, $6)`,
		event.eventType,
		event.accountID,
		event.actionSecretID,
		event.revision,
		event.requestID,
		encodedEvidence,
	); err != nil {
		return fmt.Errorf("inserting Identity security evidence: %w", err)
	}
	return nil
}
