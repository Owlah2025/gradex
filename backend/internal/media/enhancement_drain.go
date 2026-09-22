package media

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/db"
	"github.com/Owlah2025/gradex/backend/internal/queue"
)

// The enhancement drain proof for a supervised 42 -> 41 downgrade.
//
// Two durable stores can hold schema-42 enhancement work, and a rollback gate
// that reads only one of them is not a gate:
//
//   - PostgreSQL holds committed media.enhancement_requested outbox intents. An
//     undispatched one blocks the whole schema-41 media dispatcher — see
//     db.CheckNoPendingEnhancementIntents.
//   - Redis holds media:enhancement tasks in every asynq state. A dispatched
//     intent has a dispatch receipt and therefore disappears from the outbox
//     query while its task is still pending, retrying, scheduled, or archived.
//     "Dispatched" is not "done", so both questions must be asked.
//
// Everything here is read-only. Nothing is deleted, cancelled, or rescheduled:
// an operator resolves the work, and the rollback refuses until they have.

// EnhancementQueueTask is one media:enhancement task found in a durable asynq
// state, identified without reproducing its payload.
type EnhancementQueueTask struct {
	Queue string
	ID    string
	State string
}

// EnhancementDrainReport is the evidence a 42 -> 41 rollback gate acts on.
type EnhancementDrainReport struct {
	// PendingOutboxEvents is the exact number of undispatched
	// media.enhancement_requested events; PendingOutboxSample is bounded.
	PendingOutboxEvents int
	PendingOutboxSample []db.PendingEnhancementIntent
	// BlockingQueueTasks are enhancement tasks in a state that survives a
	// rollback and would be handed to a worker that cannot run it.
	BlockingQueueTasks []EnhancementQueueTask
	// FinishedQueueTasks counts enhancement tasks asynq has already completed.
	// A finished task needs no schema-42 producer, so it does not block: the
	// historical ENHANCEMENT/FINALIZATION attempt it produced stays
	// representable on schema 41.
	FinishedQueueTasks int
	// InspectedQueues and InspectedStates record what was actually read, so a
	// pass cannot be mistaken for coverage it did not have.
	InspectedQueues []string
	InspectedStates []string
}

// enhancementDrainStates is every durable asynq state a media:enhancement task
// can occupy and still need a schema-42 worker afterwards.
//
// GradeX configures no GroupAggregator, so the "aggregating" state has no
// groups; it is still enumerated rather than assumed empty. "completed" is read
// but reported separately, because asynq retains a completed task only when a
// Retention option was set and a completed task has no remaining work.
var enhancementDrainStates = []string{"pending", "active", "scheduled", "retry", "archived", "aggregating", "completed"}

// EnhancementQueueInspector is the read-only asynq surface this proof needs.
// *asynq.Inspector satisfies it; the interface exists so the gate can be tested
// against each queue state without a live Redis for every case.
type EnhancementQueueInspector interface {
	Queues() ([]string, error)
	Groups(queue string) ([]*asynq.GroupInfo, error)
	ListPendingTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error)
	ListActiveTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error)
	ListScheduledTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error)
	ListRetryTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error)
	ListArchivedTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error)
	ListCompletedTasks(queue string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error)
	ListAggregatingTasks(queue, group string, opts ...asynq.ListOption) ([]*asynq.TaskInfo, error)
}

// enhancementDrainPageSize pages the inspector rather than requesting one large
// window, so a long queue is read completely instead of silently truncated.
const enhancementDrainPageSize = 100

// InspectEnhancementDrain reads both durable stores and reports what it found.
//
// A returned error means the proof could not be obtained. That is never
// equivalent to "drained": the caller must refuse on an error exactly as it
// refuses on pending work.
func InspectEnhancementDrain(ctx context.Context, pool *pgxpool.Pool, inspector EnhancementQueueInspector) (EnhancementDrainReport, error) {
	report := EnhancementDrainReport{InspectedStates: enhancementDrainStates}
	if pool == nil {
		return report, errors.New("database pool is required")
	}
	if inspector == nil {
		return report, errors.New("queue inspector is required")
	}

	total, pending, err := db.PendingEnhancementIntents(ctx, pool)
	if err != nil {
		return report, err
	}
	report.PendingOutboxEvents = total
	report.PendingOutboxSample = pending

	report.InspectedQueues, err = inspectedQueues(inspector)
	if err != nil {
		return report, err
	}
	report.BlockingQueueTasks, report.FinishedQueueTasks, err = inspectEnhancementQueues(inspector)
	if err != nil {
		return report, err
	}
	return report, nil
}

func inspectedQueues(inspector EnhancementQueueInspector) ([]string, error) {
	queues, err := inspector.Queues()
	if err != nil {
		return nil, fmt.Errorf("listing queues: %w", err)
	}
	// The worker serves only "default", but a task can only be found in a queue
	// that is actually read, so the configured queue is included even when Redis
	// has not yet materialized it.
	queues = withDefaultQueue(queues)
	sort.Strings(queues)
	return queues, nil
}

// inspectEnhancementQueues returns the enhancement tasks that block a rollback
// and the count of those that have already finished.
func inspectEnhancementQueues(inspector EnhancementQueueInspector) ([]EnhancementQueueTask, int, error) {
	queues, err := inspectedQueues(inspector)
	if err != nil {
		return nil, 0, err
	}
	var blocking []EnhancementQueueTask
	finished := 0
	for _, name := range queues {
		for _, state := range enhancementDrainStates {
			tasks, err := listEnhancementTasks(inspector, name, state)
			if err != nil {
				return nil, 0, err
			}
			for _, task := range tasks {
				if task == nil || task.Type != queue.TypeMediaEnhancement {
					continue
				}
				if state == "completed" {
					finished++
					continue
				}
				blocking = append(blocking, EnhancementQueueTask{Queue: name, ID: task.ID, State: state})
			}
		}
	}
	return blocking, finished, nil
}

func withDefaultQueue(queues []string) []string {
	for _, name := range queues {
		if name == queue.DefaultQueueName {
			return queues
		}
	}
	return append(queues, queue.DefaultQueueName)
}

func listEnhancementTasks(inspector EnhancementQueueInspector, name, state string) ([]*asynq.TaskInfo, error) {
	if state == "aggregating" {
		groups, err := inspector.Groups(name)
		if err != nil {
			if errors.Is(err, asynq.ErrQueueNotFound) {
				return nil, nil
			}
			return nil, fmt.Errorf("listing %s aggregation groups: %w", name, err)
		}
		var tasks []*asynq.TaskInfo
		for _, group := range groups {
			if group == nil {
				continue
			}
			page, err := pageEnhancementTasks(name, state, func(opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
				return inspector.ListAggregatingTasks(name, group.Group, opts...)
			})
			if err != nil {
				return nil, err
			}
			tasks = append(tasks, page...)
		}
		return tasks, nil
	}

	var list func(opts ...asynq.ListOption) ([]*asynq.TaskInfo, error)
	switch state {
	case "pending":
		list = func(opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
			return inspector.ListPendingTasks(name, opts...)
		}
	case "active":
		list = func(opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
			return inspector.ListActiveTasks(name, opts...)
		}
	case "scheduled":
		list = func(opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
			return inspector.ListScheduledTasks(name, opts...)
		}
	case "retry":
		list = func(opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
			return inspector.ListRetryTasks(name, opts...)
		}
	case "archived":
		list = func(opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
			return inspector.ListArchivedTasks(name, opts...)
		}
	case "completed":
		list = func(opts ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
			return inspector.ListCompletedTasks(name, opts...)
		}
	default:
		return nil, fmt.Errorf("unknown enhancement queue state %q", state)
	}
	return pageEnhancementTasks(name, state, list)
}

func pageEnhancementTasks(name, state string, list func(opts ...asynq.ListOption) ([]*asynq.TaskInfo, error)) ([]*asynq.TaskInfo, error) {
	var tasks []*asynq.TaskInfo
	for page := 1; ; page++ {
		got, err := list(asynq.PageSize(enhancementDrainPageSize), asynq.Page(page))
		if err != nil {
			// A queue Redis has never seen holds nothing; anything else is a
			// failure to obtain the proof.
			if errors.Is(err, asynq.ErrQueueNotFound) {
				return tasks, nil
			}
			return nil, fmt.Errorf("listing %s %s tasks: %w", name, state, err)
		}
		tasks = append(tasks, got...)
		if len(got) < enhancementDrainPageSize {
			return tasks, nil
		}
	}
}

// Drained reports whether a 42 -> 41 downgrade may proceed.
func (r EnhancementDrainReport) Drained() bool {
	return r.PendingOutboxEvents == 0 && len(r.BlockingQueueTasks) == 0
}

// Err describes why the rollback must not proceed, or nil when it may.
func (r EnhancementDrainReport) Err() error {
	if r.Drained() {
		return nil
	}
	var reasons []string
	if r.PendingOutboxEvents > 0 {
		described := make([]string, 0, len(r.PendingOutboxSample))
		for _, intent := range r.PendingOutboxSample {
			described = append(described, intent.EventID)
		}
		reasons = append(reasons, fmt.Sprintf(
			"%d undispatched media.enhancement_requested outbox event(s) (%s) would stop the schema-41 media dispatcher on every batch",
			r.PendingOutboxEvents, strings.Join(described, ", ")))
	}
	if len(r.BlockingQueueTasks) > 0 {
		described := make([]string, 0, len(r.BlockingQueueTasks))
		for _, task := range r.BlockingQueueTasks {
			described = append(described, task.Queue+"/"+task.State+" "+task.ID)
		}
		reasons = append(reasons, fmt.Sprintf(
			"%d %s task(s) remain in a durable queue state (%s)",
			len(r.BlockingQueueTasks), queue.TypeMediaEnhancement, strings.Join(described, ", ")))
	}
	return fmt.Errorf("enhancement work is not drained: %s. Resolve it with an operator decision; the rollback gate deletes nothing",
		strings.Join(reasons, "; "))
}

// Summary is the operator-facing record of what was inspected and found. It
// carries no payloads and no credentials.
func (r EnhancementDrainReport) Summary() string {
	return fmt.Sprintf(
		"enhancement drain: queues=[%s] states=[%s] pending_outbox=%d blocking_queue_tasks=%d finished_queue_tasks=%d drained=%t",
		strings.Join(r.InspectedQueues, " "), strings.Join(r.InspectedStates, " "),
		r.PendingOutboxEvents, len(r.BlockingQueueTasks), r.FinishedQueueTasks, r.Drained())
}
