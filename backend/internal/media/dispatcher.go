package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/queue"
)

// Dispatcher turns committed media outbox rows into queue tasks. PostgreSQL
// is the source of truth: a queue outage leaves the row pending, and a crash
// between enqueue and receipt recording is safe because task IDs are the
// immutable outbox IDs and worker operations are idempotent.
type Dispatcher struct {
	db    *pgxpool.Pool
	queue *asynq.Client
}

func NewDispatcher(db *pgxpool.Pool, client *asynq.Client) (*Dispatcher, error) {
	if db == nil {
		return nil, errors.New("media dispatcher database is required")
	}
	if client == nil {
		return nil, errors.New("media dispatcher queue client is required")
	}
	return &Dispatcher{db: db, queue: client}, nil
}

// DispatchPending publishes at most limit committed media intents. It is
// safe to call concurrently from more than one dispatcher process.
func (d *Dispatcher) DispatchPending(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, errors.New("media dispatcher limit must be positive")
	}
	rows, err := d.db.Query(ctx, `
		SELECT e.id::text, e.event_type, e.safe_payload
		FROM outbox_events e
		LEFT JOIN media_outbox_dispatches md ON md.event_id = e.id
		WHERE e.source_module = 'MEDIA_AND_ASSETS'
		  AND e.available_at <= now()
		  AND md.event_id IS NULL
		ORDER BY e.occurred_at, e.id
		LIMIT $1
	`, limit)
	if err != nil {
		return 0, fmt.Errorf("loading media outbox intents: %w", err)
	}
	defer rows.Close()

	dispatched := 0
	for rows.Next() {
		var eventID, eventType string
		var payload []byte
		if err := rows.Scan(&eventID, &eventType, &payload); err != nil {
			return dispatched, fmt.Errorf("reading media outbox intent: %w", err)
		}
		if err := d.dispatchEvent(ctx, eventID, eventType, payload); err != nil {
			return dispatched, err
		}
		dispatched++
	}
	if err := rows.Err(); err != nil {
		return dispatched, fmt.Errorf("iterating media outbox intents: %w", err)
	}
	return dispatched, nil
}

func (d *Dispatcher) dispatchEvent(ctx context.Context, eventID, eventType string, payload []byte) error {
	taskType, ok := queueTypeForEvent(eventType)
	if !ok {
		return fmt.Errorf("unsupported media outbox event %q", eventType)
	}
	if !json.Valid(payload) {
		return fmt.Errorf("media outbox event %s has invalid task payload", eventID)
	}
	_, err := d.queue.EnqueueContext(ctx, asynq.NewTask(taskType, payload, asynq.MaxRetry(3)), asynq.TaskID(eventID))
	if err != nil && !errors.Is(err, asynq.ErrTaskIDConflict) {
		return fmt.Errorf("dispatching media outbox event %s: %w", eventID, err)
	}
	if _, err := d.db.Exec(ctx, `
		INSERT INTO media_outbox_dispatches (event_id)
		VALUES ($1::uuid)
		ON CONFLICT (event_id) DO NOTHING
	`, eventID); err != nil {
		return fmt.Errorf("recording media outbox dispatch %s: %w", eventID, err)
	}
	return nil
}

func queueTypeForEvent(eventType string) (string, bool) {
	switch eventType {
	case "media.scan_requested":
		return queue.TypeMediaScan, true
	case "media.transcode_requested":
		return queue.TypeMediaTranscode, true
	default:
		return "", false
	}
}
