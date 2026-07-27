package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

// NotificationIntentWriter handles durable notification intent creation for catalog events.
type NotificationIntentWriter struct {
	writer *outbox.Writer
}

// NewNotificationIntentWriter constructs a new NotificationIntentWriter.
// Standing clause: required dependency validated at construction.
func NewNotificationIntentWriter(writer *outbox.Writer) (*NotificationIntentWriter, error) {
	if writer == nil {
		return nil, errors.New("outbox writer is required")
	}
	return &NotificationIntentWriter{writer: writer}, nil
}

// WriteIntent writes a notification intent event into outbox inside the specified transaction tx.
// It is mandatory and executes inside the business transaction.
func (n *NotificationIntentWriter) WriteIntent(
	ctx context.Context,
	tx pgx.Tx,
	event outbox.Event,
	protected any,
) (string, error) {
	if n == nil || n.writer == nil {
		return "", errors.New("notification intent writer is uninitialized")
	}
	if tx == nil {
		return "", errors.New("transaction is required for notification intent writing")
	}

	if event.SourceModule == "" {
		event.SourceModule = "CATALOG_AND_AUTHORING"
	}

	id, err := n.writer.Append(ctx, tx, event, protected)
	if err != nil {
		return "", fmt.Errorf("writing notification intent: %w", err)
	}
	return id, nil
}
