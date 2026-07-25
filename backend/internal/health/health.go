// Package health implements the process liveness and readiness probes.
//
// The two answer different questions and must not be conflated. Liveness says
// the process is running and should not be killed. Readiness says this
// instance may safely receive normal traffic, which additionally depends on
// its required dependencies and on schema compatibility.
//
// Probe responses are deliberately thin. A check's error goes to the protected
// structured log under the trusted request ID; the response carries only per
// check "ok" or "failed", because a probe endpoint is unauthenticated and its
// body would otherwise disclose hosts, database names, SQL errors, and
// migration state to anyone who can reach it.
package health

import (
	"context"
	"sync"
	"time"
)

// Status is a check outcome. It is a closed set so a response can never carry
// a free-text diagnostic.
type Status string

const (
	StatusOK Status = "ok"
	// StatusFailed means the check did not pass. Why it did not pass is a log
	// concern, never a response concern.
	StatusFailed Status = "failed"
	// StatusSkipped means this deployment does not require the dependency, so
	// its state cannot affect readiness. An intentionally disabled optional
	// provider must not make the learning API unavailable.
	StatusSkipped Status = "skipped"
)

// ProbeFunc performs one dependency check. It must honour ctx, mutate nothing,
// and do no retrying of its own: a probe reports the state it observes now,
// and the orchestrator decides what to do about a flap.
type ProbeFunc func(ctx context.Context) error

// Check is one named readiness dependency.
type Check struct {
	Name string
	// Required false records the check without letting it fail readiness.
	Required bool
	Probe    ProbeFunc
}

// Result is one readiness evaluation.
type Result struct {
	Ready  bool
	Checks map[string]Status
	// Failures carries each failed check's error for the caller to log. It is
	// never serialized into a response.
	Failures map[string]error
}

// Reporter evaluates readiness and holds the lifecycle gate.
type Reporter struct {
	checks  []Check
	timeout time.Duration

	mu       sync.RWMutex
	started  bool
	draining bool
}

// New builds a reporter. timeout bounds one whole evaluation, not each check:
// probes run concurrently against a single deadline so a hung dependency
// cannot extend the probe past what the orchestrator will wait for.
func New(timeout time.Duration, checks ...Check) *Reporter {
	return &Reporter{checks: checks, timeout: timeout}
}

// MarkStarted opens the readiness gate once configuration validation and
// startup initialization are complete. Readiness is false before this.
func (r *Reporter) MarkStarted() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started = true
}

// MarkDraining closes the readiness gate at the start of graceful shutdown, so
// the instance stops receiving new traffic while it finishes in-flight work.
// It is one-way: a draining process never becomes ready again.
func (r *Reporter) MarkDraining() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.draining = true
}

// Live reports process liveness. It touches no dependency and returns
// immediately, so a database or Redis outage never causes the orchestrator to
// kill an otherwise healthy process.
//
// It stays true while draining: a process finishing in-flight requests should
// not be killed, only removed from the traffic pool.
func (r *Reporter) Live() bool { return true }

// Ready evaluates the lifecycle gate and then every check.
//
// The gate short-circuits: before startup completes and after draining begins
// there is nothing a dependency probe could prove, and probing during shutdown
// would only add load to dependencies the process is disconnecting from.
func (r *Reporter) Ready(ctx context.Context) Result {
	r.mu.RLock()
	started, draining := r.started, r.draining
	r.mu.RUnlock()

	if !started || draining {
		checks := make(map[string]Status, len(r.checks))
		for _, c := range r.checks {
			checks[c.Name] = StatusSkipped
		}
		return Result{Ready: false, Checks: checks, Failures: map[string]error{}}
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	var (
		mu       sync.Mutex
		statuses = make(map[string]Status, len(r.checks))
		failures = make(map[string]error, len(r.checks))
		wg       sync.WaitGroup
	)

	// Concurrency is bounded by the fixed, small set of registered checks —
	// this is not a fan-out over request data.
	for _, c := range r.checks {
		if c.Probe == nil {
			mu.Lock()
			statuses[c.Name] = StatusSkipped
			mu.Unlock()
			continue
		}
		wg.Add(1)
		go func(c Check) {
			defer wg.Done()
			err := c.Probe(ctx)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				statuses[c.Name] = StatusFailed
				failures[c.Name] = err
				return
			}
			statuses[c.Name] = StatusOK
		}(c)
	}
	wg.Wait()

	ready := true
	for _, c := range r.checks {
		if c.Required && statuses[c.Name] != StatusOK {
			ready = false
		}
	}
	return Result{Ready: ready, Checks: statuses, Failures: failures}
}
