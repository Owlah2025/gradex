package media

import (
	"errors"
	"strings"
	"testing"

	"github.com/hibiken/asynq"

	"github.com/Owlah2025/gradex/backend/internal/queue"
)

// fakeQueueInspector answers the read-only asynq surface the drain proof uses.
// Every durable state is represented, because the point of the matrix below is
// that no state is quietly unread: a rollback gate that inspects only the
// pending list passes while a retrying or archived enhancement task is waiting
// for a worker that will no longer exist.
type fakeQueueInspector struct {
	queues   []string
	groups   map[string][]string
	tasks    map[string]map[string][]*asynq.TaskInfo // queue -> state -> tasks
	failures map[string]error
}

func (f *fakeQueueInspector) Queues() ([]string, error) { return f.queues, f.failures["queues"] }

func (f *fakeQueueInspector) Groups(name string) ([]*asynq.GroupInfo, error) {
	if err := f.failures["groups"]; err != nil {
		return nil, err
	}
	var groups []*asynq.GroupInfo
	for _, group := range f.groups[name] {
		groups = append(groups, &asynq.GroupInfo{Group: group})
	}
	return groups, nil
}

func (f *fakeQueueInspector) list(name, state string) ([]*asynq.TaskInfo, error) {
	if err := f.failures[state]; err != nil {
		return nil, err
	}
	return f.tasks[name][state], nil
}

func (f *fakeQueueInspector) ListPendingTasks(name string, _ ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	return f.list(name, "pending")
}
func (f *fakeQueueInspector) ListActiveTasks(name string, _ ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	return f.list(name, "active")
}
func (f *fakeQueueInspector) ListScheduledTasks(name string, _ ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	return f.list(name, "scheduled")
}
func (f *fakeQueueInspector) ListRetryTasks(name string, _ ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	return f.list(name, "retry")
}
func (f *fakeQueueInspector) ListArchivedTasks(name string, _ ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	return f.list(name, "archived")
}
func (f *fakeQueueInspector) ListCompletedTasks(name string, _ ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	return f.list(name, "completed")
}
func (f *fakeQueueInspector) ListAggregatingTasks(name, group string, _ ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	if err := f.failures["aggregating"]; err != nil {
		return nil, err
	}
	return f.tasks[name]["aggregating:"+group], nil
}

func inspectorWith(state string, tasks ...*asynq.TaskInfo) *fakeQueueInspector {
	return &fakeQueueInspector{
		queues: []string{queue.DefaultQueueName},
		tasks:  map[string]map[string][]*asynq.TaskInfo{queue.DefaultQueueName: {state: tasks}},
	}
}

func task(id, taskType string) *asynq.TaskInfo { return &asynq.TaskInfo{ID: id, Type: taskType} }

// TestEnhancementDrainRefusesEveryDurableQueueState is the state matrix. Each
// asynq state that outlives a rollback must block it on its own.
func TestEnhancementDrainRefusesEveryDurableQueueState(t *testing.T) {
	for _, state := range []string{"pending", "active", "scheduled", "retry", "archived"} {
		t.Run(state, func(t *testing.T) {
			inspector := inspectorWith(state, task("intent-1", queue.TypeMediaEnhancement))
			report := EnhancementDrainReport{}
			blocking, finished, err := inspectEnhancementQueues(inspector)
			if err != nil {
				t.Fatalf("inspecting %s: %v", state, err)
			}
			report.BlockingQueueTasks, report.FinishedQueueTasks = blocking, finished
			if report.Drained() {
				t.Fatalf("a %s enhancement task did not block the rollback", state)
			}
			if len(blocking) != 1 || blocking[0].State != state || blocking[0].ID != "intent-1" {
				t.Fatalf("blocking tasks = %+v, want one %s task", blocking, state)
			}
			if !strings.Contains(report.Err().Error(), "intent-1") {
				t.Fatalf("refusal did not identify the task: %v", report.Err())
			}
		})
	}
}

// Aggregation is not configured for this deployment, but the state is read
// rather than assumed empty: a task there would still need a schema-42 worker.
func TestEnhancementDrainRefusesAggregatingTasks(t *testing.T) {
	inspector := &fakeQueueInspector{
		queues: []string{queue.DefaultQueueName},
		groups: map[string][]string{queue.DefaultQueueName: {"enhancements"}},
		tasks: map[string]map[string][]*asynq.TaskInfo{queue.DefaultQueueName: {
			"aggregating:enhancements": {task("intent-aggregating", queue.TypeMediaEnhancement)},
		}},
	}
	blocking, _, err := inspectEnhancementQueues(inspector)
	if err != nil {
		t.Fatalf("inspecting aggregating tasks: %v", err)
	}
	if len(blocking) != 1 || blocking[0].State != "aggregating" {
		t.Fatalf("blocking tasks = %+v, want one aggregating task", blocking)
	}
}

// A finished task has no remaining work. The historical ENHANCEMENT or
// FINALIZATION attempt it produced stays representable on schema 41, so
// requiring its deletion would be asking an operator to destroy evidence.
func TestEnhancementDrainAllowsCompletedTasks(t *testing.T) {
	inspector := inspectorWith("completed", task("intent-done", queue.TypeMediaEnhancement))
	blocking, finished, err := inspectEnhancementQueues(inspector)
	if err != nil {
		t.Fatalf("inspecting completed tasks: %v", err)
	}
	report := EnhancementDrainReport{BlockingQueueTasks: blocking, FinishedQueueTasks: finished}
	if !report.Drained() || finished != 1 {
		t.Fatalf("completed=%d blocking=%+v; a finished enhancement task must not block", finished, blocking)
	}
}

// Ordinary media work is not enhancement work. Blocking on it would make every
// rollback window depend on an idle transcode queue, which is not a rollback
// incompatibility at all.
func TestEnhancementDrainIgnoresUnrelatedMediaTasks(t *testing.T) {
	inspector := inspectorWith("pending",
		task("scan-1", queue.TypeMediaScan), task("transcode-1", queue.TypeMediaTranscode))
	blocking, finished, err := inspectEnhancementQueues(inspector)
	if err != nil {
		t.Fatalf("inspecting unrelated tasks: %v", err)
	}
	report := EnhancementDrainReport{BlockingQueueTasks: blocking, FinishedQueueTasks: finished}
	if !report.Drained() {
		t.Fatalf("unrelated media tasks blocked the rollback: %+v", blocking)
	}
	if report.Err() != nil {
		t.Fatalf("drained report reported an error: %v", report.Err())
	}
}

// A queue Redis has never materialized holds nothing; every other failure is a
// failure to obtain the proof, and an unprovable gate must refuse.
func TestEnhancementDrainDistinguishesMissingQueueFromFailure(t *testing.T) {
	missing := inspectorWith("pending")
	missing.failures = map[string]error{"pending": asynq.ErrQueueNotFound}
	if _, _, err := inspectEnhancementQueues(missing); err != nil {
		t.Fatalf("a queue that does not exist yet must not fail the proof: %v", err)
	}

	broken := inspectorWith("retry")
	broken.failures = map[string]error{"retry": errors.New("redis unreachable")}
	if _, _, err := inspectEnhancementQueues(broken); err == nil {
		t.Fatal("an unreadable queue state was treated as drained")
	}
}

// The configured queue is inspected even when Redis has not reported it yet: a
// task can only be found in a queue that is actually read.
func TestEnhancementDrainAlwaysInspectsTheConfiguredQueue(t *testing.T) {
	inspector := &fakeQueueInspector{
		queues: nil,
		tasks: map[string]map[string][]*asynq.TaskInfo{queue.DefaultQueueName: {
			"pending": {task("intent-1", queue.TypeMediaEnhancement)},
		}},
	}
	blocking, _, err := inspectEnhancementQueues(inspector)
	if err != nil {
		t.Fatalf("inspecting an unreported queue: %v", err)
	}
	if len(blocking) != 1 {
		t.Fatalf("blocking tasks = %+v, want the enhancement task in the configured queue", blocking)
	}
}

// The summary is an operational record, so it has to state what was read, not
// only what was found.
func TestEnhancementDrainSummaryNamesWhatWasInspected(t *testing.T) {
	report := EnhancementDrainReport{
		InspectedQueues: []string{queue.DefaultQueueName},
		InspectedStates: enhancementDrainStates,
	}
	summary := report.Summary()
	for _, state := range []string{"pending", "active", "scheduled", "retry", "archived", "completed"} {
		if !strings.Contains(summary, state) {
			t.Fatalf("summary does not record the %s state: %s", state, summary)
		}
	}
	if !strings.Contains(summary, "drained=true") {
		t.Fatalf("summary = %s, want it to state the verdict", summary)
	}
}
