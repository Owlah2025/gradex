package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type AuditEvent struct {
	ActorAccountID  *string
	ActorRole       string
	ActorDescriptor string
	Action          string
	TargetType      string
	TargetID        string
	TargetRevision  *int
	Reason          string
	Metadata        map[string]any
	CorrelationID   *string
	OperationID     *string
}

// WriteAuditEvent writes an audit event to audit_events table inside an existing transaction tx.
// It is mandatory and executes within the same transaction as the change under module = CATALOG_AND_AUTHORING.
func WriteAuditEvent(ctx context.Context, tx pgx.Tx, event AuditEvent) error {
	if tx == nil {
		return errors.New("transaction is required for audit writing")
	}
	if event.Action == "" {
		return errors.New("audit action is required")
	}
	if event.ActorRole == "" {
		return errors.New("audit actor role is required")
	}
	if event.ActorDescriptor == "" {
		return errors.New("audit actor descriptor is required")
	}
	if event.TargetType == "" {
		return errors.New("audit target type is required")
	}
	if event.TargetID == "" {
		return errors.New("audit target ID is required")
	}
	if event.Reason == "" {
		return errors.New("audit reason is required")
	}

	metadataJSON := []byte("{}")
	if len(event.Metadata) > 0 {
		var err error
		metadataJSON, err = json.Marshal(event.Metadata)
		if err != nil {
			return fmt.Errorf("marshaling audit metadata: %w", err)
		}
	}

	query := `
		INSERT INTO audit_events (
			actor_account_id, actor_role, actor_descriptor,
			action, module, target_type, target_id, target_revision,
			reason, metadata, correlation_id, operation_id
		) VALUES (
			$1, $2, $3,
			$4, 'CATALOG_AND_AUTHORING', $5, $6, $7,
			$8, $9, $10, $11
		)
	`
	_, err := tx.Exec(ctx, query,
		event.ActorAccountID, event.ActorRole, event.ActorDescriptor,
		event.Action, event.TargetType, event.TargetID, event.TargetRevision,
		event.Reason, metadataJSON, event.CorrelationID, event.OperationID,
	)
	if err != nil {
		return fmt.Errorf("writing audit event: %w", err)
	}
	return nil
}
