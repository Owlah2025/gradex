//go:build integration

package media

import (
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	"github.com/Owlah2025/gradex/backend/internal/db"
	"github.com/Owlah2025/gradex/backend/internal/queue"
)

const drainRedisAddr = "localhost:6379"

func drainInspector(t *testing.T) *asynq.Inspector {
	t.Helper()
	inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: drainRedisAddr})
	t.Cleanup(func() { _ = inspector.Close() })
	return inspector
}

// TestEnhancementDrainRefusesUndispatchedIntent is the outbox half of the 42 ->
// 41 gate, taken through the same report the release tooling reads.
func TestEnhancementDrainRefusesUndispatchedIntent(t *testing.T) {
	f, _, assetVersionID := seedPlayableEnhancementAsset(t, []string{"1080p"})
	if err := f.service.RetryEnhancements(f.ctx, RetryRequest{AssetVersionID: assetVersionID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID}); err != nil {
		t.Fatalf("manual enhancement request: %v", err)
	}

	report, err := InspectEnhancementDrain(f.ctx, f.pool, drainInspector(t))
	if err != nil {
		t.Fatalf("inspecting the enhancement drain: %v", err)
	}
	if report.Drained() || report.PendingOutboxEvents != 1 {
		t.Fatalf("report = %s, want one pending outbox intent blocking the rollback", report.Summary())
	}
	if !strings.Contains(report.Err().Error(), "stop the schema-41 media dispatcher") {
		t.Fatalf("refusal does not state the consequence: %v", report.Err())
	}
	// The same evidence through the database-only gate the migration command uses.
	if err := db.CheckNoPendingEnhancementIntents(f.ctx, f.pool); err == nil {
		t.Fatal("the migration preflight accepted an undispatched enhancement intent")
	}
}

// TestEnhancementDrainRefusesDispatchedButQueuedWork is the case a
// database-only gate cannot see. The outbox row carries a dispatch receipt and
// disappears from the pending query, while its asynq task is still waiting for a
// schema-42 worker.
func TestEnhancementDrainRefusesDispatchedButQueuedWork(t *testing.T) {
	f, _, assetVersionID := seedPlayableEnhancementAsset(t, []string{"1080p"})
	if err := f.service.RetryEnhancements(f.ctx, RetryRequest{AssetVersionID: assetVersionID, AdminAccountID: f.adminID, ActorDescriptor: f.adminID}); err != nil {
		t.Fatalf("manual enhancement request: %v", err)
	}
	var eventID string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT id::text FROM outbox_events
		WHERE event_type='media.enhancement_requested' AND aggregate_id=$1::uuid
	`, assetVersionID).Scan(&eventID); err != nil {
		t.Fatalf("loading the committed enhancement intent: %v", err)
	}

	client := asynq.NewClient(asynq.RedisClientOpt{Addr: drainRedisAddr})
	t.Cleanup(func() { _ = client.Close() })
	inspector := drainInspector(t)
	t.Cleanup(func() { _ = inspector.DeleteTask(queue.DefaultQueueName, eventID) })
	dispatcher, err := NewDispatcher(f.pool, client, 30*time.Second)
	if err != nil {
		t.Fatalf("constructing dispatcher: %v", err)
	}
	if dispatched, err := dispatcher.DispatchPending(f.ctx, 10); err != nil || dispatched == 0 {
		t.Fatalf("dispatch count=%d err=%v; want the enhancement intent dispatched", dispatched, err)
	}

	if err := db.CheckNoPendingEnhancementIntents(f.ctx, f.pool); err != nil {
		t.Fatalf("a dispatched intent still counted as pending in the database: %v", err)
	}
	report, err := InspectEnhancementDrain(f.ctx, f.pool, inspector)
	if err != nil {
		t.Fatalf("inspecting the enhancement drain: %v", err)
	}
	if report.Drained() {
		t.Fatalf("report = %s; a dispatched intent with a queued task is not drained", report.Summary())
	}
	found := false
	for _, task := range report.BlockingQueueTasks {
		if task.ID == eventID {
			found = true
		}
	}
	if !found {
		t.Fatalf("blocking tasks = %+v, want the queued enhancement task %s", report.BlockingQueueTasks, eventID)
	}

	// Removing the queue task is an explicit operator decision, never something
	// the rollback does. Once taken, the gate passes.
	if err := inspector.DeleteTask(queue.DefaultQueueName, eventID); err != nil {
		t.Fatalf("operator cancellation of the queued task: %v", err)
	}
	report, err = InspectEnhancementDrain(f.ctx, f.pool, inspector)
	if err != nil || !report.Drained() {
		t.Fatalf("report = %s err=%v; want a drained verdict after the task was resolved", report.Summary(), err)
	}
}

// TestUnsupportedMediaOutboxEventBlocksAllLaterDispatch is the severity the
// rollback gate exists for, proven on the real dispatcher.
//
// The deployed 3C-A dispatcher has no case for media.enhancement_requested. It
// does not skip that row: dispatchEvent returns "unsupported media outbox
// event", DispatchPending returns the error, and the batch stops. Because the
// feeding query is ordered by occurred_at and selects only rows without a
// dispatch receipt, the surviving event is the first row of every later batch
// too — so one such row blocks scan and transcode dispatch for the whole module
// until an operator removes it.
//
// This build's dispatcher supports that event type, so the mechanism is proven
// with an event type it does not support. The dispatcher code path — and
// therefore the failure — is identical.
func TestUnsupportedMediaOutboxEventBlocksAllLaterDispatch(t *testing.T) {
	f, _, assetVersionID := seedPlayableEnhancementAsset(t, []string{"1080p"})

	var unsupportedID string
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO outbox_events (event_type, schema_version, source_module, aggregate_type,
		                           aggregate_id, aggregate_revision, safe_payload, correlation_id, occurred_at)
		VALUES ('media.unsupported_by_this_build', 1, 'MEDIA_AND_ASSETS', 'MEDIA_ASSET_VERSION',
		        $1::uuid, 1, jsonb_build_object('asset_version_id', $1), 'head-of-line-fixture', now() - interval '1 hour')
		RETURNING id::text
	`, assetVersionID).Scan(&unsupportedID); err != nil {
		t.Fatalf("seeding the unsupported media outbox event: %v", err)
	}
	var laterID string
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO outbox_events (event_type, schema_version, source_module, aggregate_type,
		                           aggregate_id, aggregate_revision, safe_payload, correlation_id)
		VALUES ('media.transcode_requested', 1, 'MEDIA_AND_ASSETS', 'MEDIA_ASSET_VERSION',
		        $1::uuid, 1, jsonb_build_object('asset_version_id', $1, 'operation_id', 'later-op'), 'head-of-line-fixture')
		RETURNING id::text
	`, assetVersionID).Scan(&laterID); err != nil {
		t.Fatalf("seeding the later ordinary media event: %v", err)
	}

	client := asynq.NewClient(asynq.RedisClientOpt{Addr: drainRedisAddr})
	t.Cleanup(func() { _ = client.Close() })
	inspector := drainInspector(t)
	t.Cleanup(func() { _ = inspector.DeleteTask(queue.DefaultQueueName, laterID) })
	dispatcher, err := NewDispatcher(f.pool, client, 30*time.Second)
	if err != nil {
		t.Fatalf("constructing dispatcher: %v", err)
	}

	// Every batch stops at the same row: this is head-of-line blocking, not a
	// single skipped event.
	for attempt := 1; attempt <= 3; attempt++ {
		dispatched, err := dispatcher.DispatchPending(f.ctx, 10)
		if err == nil || !strings.Contains(err.Error(), "unsupported media outbox event") {
			t.Fatalf("batch %d error = %v, want the unsupported-event abort", attempt, err)
		}
		if dispatched != 0 {
			t.Fatalf("batch %d dispatched %d events past the unsupported one", attempt, dispatched)
		}
		var receipts int
		if err := f.pool.QueryRow(f.ctx,
			"SELECT count(*) FROM media_outbox_dispatches WHERE event_id IN ($1::uuid, $2::uuid)",
			unsupportedID, laterID).Scan(&receipts); err != nil {
			t.Fatalf("counting dispatch receipts: %v", err)
		}
		if receipts != 0 {
			t.Fatalf("batch %d recorded %d dispatch receipts; the later media event must stay undispatched", attempt, receipts)
		}
	}

	// outbox_events is append-only: the blocking row cannot be deleted or edited,
	// which is exactly why proving zero before the downgrade is the only safe
	// ordering.
	if _, err := f.pool.Exec(f.ctx, "DELETE FROM outbox_events WHERE id=$1::uuid", unsupportedID); err == nil {
		t.Fatal("the append-only outbox accepted a delete of the blocking event")
	}
	// The only way past it is an explicit operator decision to record a dispatch
	// receipt, retiring the event without destroying the evidence that it existed.
	// Only then does the later ordinary media event dispatch.
	if _, err := f.pool.Exec(f.ctx,
		"INSERT INTO media_outbox_dispatches (event_id) VALUES ($1::uuid)", unsupportedID); err != nil {
		t.Fatalf("retiring the blocking event by dispatch receipt: %v", err)
	}
	if dispatched, err := dispatcher.DispatchPending(f.ctx, 10); err != nil || dispatched == 0 {
		t.Fatalf("dispatch after clearing count=%d err=%v; want the later media event dispatched", dispatched, err)
	}
	if _, err := inspector.GetTaskInfo(queue.DefaultQueueName, laterID); err != nil {
		t.Fatalf("the later media event did not reach the queue: %v", err)
	}
}
