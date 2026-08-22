package identity

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testPasswordGate(t *testing.T, concurrency, queue int, wait time.Duration) *PasswordVerificationGate {
	t.Helper()
	gate, err := NewPasswordVerificationGate(PasswordVerificationGateOptions{
		Concurrency: concurrency, Queue: queue, QueueWait: wait,
	})
	if err != nil {
		t.Fatalf("creating gate: %v", err)
	}
	return gate
}

func gateCounts(gate *PasswordVerificationGate) (active, queued int) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.active, gate.waiters.Len()
}

func observedContext(
	parent context.Context,
	events chan<- PasswordVerificationEvent,
) context.Context {
	return WithPasswordVerificationObserver(parent, func(event PasswordVerificationEvent) {
		events <- event
	})
}

func waitForEvent(t *testing.T, events <-chan PasswordVerificationEvent, want PasswordVerificationEvent) {
	t.Helper()
	select {
	case got := <-events:
		if got != want {
			t.Fatalf("event = %s, want %s", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", want)
	}
}

func TestPasswordVerificationGateImmediateAdmission(t *testing.T) {
	gate := testPasswordGate(t, 1, 1, time.Second)
	events := make(chan PasswordVerificationEvent, 1)
	called := false
	if err := gate.Verify(observedContext(context.Background(), events), func() error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("verification: %v", err)
	}
	if !called {
		t.Fatal("immediately admitted verification was not invoked")
	}
	waitForEvent(t, events, PasswordVerificationAdmitted)
	if active, queued := gateCounts(gate); active != 0 || queued != 0 {
		t.Fatalf("gate state = active %d queued %d, want zero", active, queued)
	}
}

func TestPasswordVerificationGateBoundsConcurrency(t *testing.T) {
	gate := testPasswordGate(t, 2, 2, time.Second)
	release := make(chan struct{})
	started := make(chan struct{}, 3)
	var active, maximum atomic.Int32
	verify := func() error {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		started <- struct{}{}
		<-release
		return nil
	}

	var wg sync.WaitGroup
	for attempt := 0; attempt < 2; attempt++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := gate.Verify(context.Background(), verify); err != nil {
				t.Errorf("verification: %v", err)
			}
		}()
	}
	<-started
	<-started
	queuedEvents := make(chan PasswordVerificationEvent, 2)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := gate.Verify(observedContext(context.Background(), queuedEvents), verify); err != nil {
			t.Errorf("queued verification: %v", err)
		}
	}()
	waitForEvent(t, queuedEvents, PasswordVerificationQueued)
	if currentActive, queued := gateCounts(gate); currentActive != 2 || queued != 1 {
		t.Fatalf("gate state = active %d queued %d, want 2/1", currentActive, queued)
	}
	close(release)
	wg.Wait()
	if maximum.Load() != 2 {
		t.Fatalf("maximum concurrency = %d, want 2", maximum.Load())
	}
	if currentActive, queued := gateCounts(gate); currentActive != 0 || queued != 0 {
		t.Fatalf("final gate state = active %d queued %d, want zero", currentActive, queued)
	}
}

func TestPasswordVerificationGateQueueIsBounded(t *testing.T) {
	gate := testPasswordGate(t, 1, 1, time.Second)
	release := make(chan struct{})
	started := make(chan struct{})
	go func() {
		_ = gate.Verify(context.Background(), func() error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started

	events := make(chan PasswordVerificationEvent, 2)
	waiting := make(chan error, 1)
	go func() {
		waiting <- gate.Verify(observedContext(context.Background(), events), func() error { return nil })
	}()
	waitForEvent(t, events, PasswordVerificationQueued)
	overflowEvents := make(chan PasswordVerificationEvent, 1)
	if err := gate.Verify(observedContext(context.Background(), overflowEvents), func() error { return nil }); !errors.Is(err, ErrPasswordVerificationSaturated) {
		t.Fatalf("overflow = %v, want saturated", err)
	}
	waitForEvent(t, overflowEvents, PasswordVerificationSaturated)
	close(release)
	if err := <-waiting; err != nil {
		t.Fatalf("queued verification: %v", err)
	}
	if active, queued := gateCounts(gate); active != 0 || queued != 0 {
		t.Fatalf("final gate state = active %d queued %d, want zero", active, queued)
	}
}

func TestPasswordVerificationGateAdmitsWaitersFIFO(t *testing.T) {
	gate := testPasswordGate(t, 1, 3, time.Second)
	releaseActive := make(chan struct{})
	activeStarted := make(chan struct{})
	go func() {
		_ = gate.Verify(context.Background(), func() error {
			close(activeStarted)
			<-releaseActive
			return nil
		})
	}()
	<-activeStarted

	admitted := make(chan int, 3)
	releases := []chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{})}
	done := make(chan error, 3)
	for index := range releases {
		events := make(chan PasswordVerificationEvent, 2)
		id := index + 1
		go func() {
			done <- gate.Verify(observedContext(context.Background(), events), func() error {
				admitted <- id
				<-releases[index]
				return nil
			})
		}()
		waitForEvent(t, events, PasswordVerificationQueued)
	}

	close(releaseActive)
	for want, release := range releases {
		if got := <-admitted; got != want+1 {
			t.Fatalf("FIFO admission %d = waiter %d, want waiter %d", want+1, got, want+1)
		}
		close(release)
	}
	for range releases {
		if err := <-done; err != nil {
			t.Fatalf("FIFO verification: %v", err)
		}
	}
	if active, queued := gateCounts(gate); active != 0 || queued != 0 {
		t.Fatalf("final gate state = active %d queued %d, want zero", active, queued)
	}
}

func TestPasswordVerificationGateCanceledWaiterDoesNotBlockFIFO(t *testing.T) {
	for _, canceledIndex := range []int{0, 1} {
		name := "first"
		if canceledIndex == 1 {
			name = "middle"
		}
		t.Run(name, func(t *testing.T) {
			gate := testPasswordGate(t, 1, 3, time.Second)
			releaseActive := make(chan struct{})
			activeStarted := make(chan struct{})
			go func() {
				_ = gate.Verify(context.Background(), func() error {
					close(activeStarted)
					<-releaseActive
					return nil
				})
			}()
			<-activeStarted

			contexts := make([]context.Context, 3)
			cancels := make([]context.CancelFunc, 3)
			done := make([]chan error, 3)
			admitted := make(chan int, 3)
			releaseWaiter := make([]chan struct{}, 3)
			for index := 0; index < 3; index++ {
				contexts[index], cancels[index] = context.WithCancel(context.Background())
				done[index] = make(chan error, 1)
				releaseWaiter[index] = make(chan struct{})
				events := make(chan PasswordVerificationEvent, 2)
				id := index
				go func() {
					done[id] <- gate.Verify(observedContext(contexts[id], events), func() error {
						admitted <- id
						<-releaseWaiter[id]
						return nil
					})
				}()
				waitForEvent(t, events, PasswordVerificationQueued)
			}

			cancels[canceledIndex]()
			if err := <-done[canceledIndex]; !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled waiter error = %v, want context.Canceled", err)
			}
			close(releaseActive)
			for _, want := range []int{0, 1, 2} {
				if want == canceledIndex {
					continue
				}
				if got := <-admitted; got != want {
					t.Fatalf("admitted waiter %d, want %d", got, want)
				}
				close(releaseWaiter[want])
				if err := <-done[want]; err != nil {
					t.Fatalf("waiter %d: %v", want, err)
				}
			}
			for index, cancel := range cancels {
				if index != canceledIndex {
					cancel()
				}
			}
			if active, queued := gateCounts(gate); active != 0 || queued != 0 {
				t.Fatalf("final gate state = active %d queued %d, want zero", active, queued)
			}
		})
	}
}

func TestPasswordVerificationGateTimedOutWaiterIsRemoved(t *testing.T) {
	gate := testPasswordGate(t, 1, 1, 10*time.Millisecond)
	release := make(chan struct{})
	started := make(chan struct{})
	activeDone := make(chan struct{})
	go func() {
		defer close(activeDone)
		_ = gate.Verify(context.Background(), func() error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started
	events := make(chan PasswordVerificationEvent, 2)
	err := gate.Verify(observedContext(context.Background(), events), func() error {
		t.Fatal("timed-out waiter invoked verification")
		return nil
	})
	if !errors.Is(err, ErrPasswordVerificationTimeout) {
		t.Fatalf("timeout error = %v, want ErrPasswordVerificationTimeout", err)
	}
	waitForEvent(t, events, PasswordVerificationQueued)
	waitForEvent(t, events, PasswordVerificationTimeout)
	if active, queued := gateCounts(gate); active != 1 || queued != 0 {
		t.Fatalf("gate state = active %d queued %d, want 1/0", active, queued)
	}
	close(release)
	<-activeDone
	if active, queued := gateCounts(gate); active != 0 || queued != 0 {
		t.Fatalf("final gate state = active %d queued %d, want zero", active, queued)
	}
}

type cancelOnNthErrContext struct {
	context.Context
	calls    atomic.Int32
	cancelAt int32
	done     chan struct{}
	once     sync.Once
}

func newCancelOnNthErrContext(cancelAt int32) *cancelOnNthErrContext {
	return &cancelOnNthErrContext{
		Context: context.Background(), cancelAt: cancelAt, done: make(chan struct{}),
	}
}

func (c *cancelOnNthErrContext) Done() <-chan struct{} { return c.done }

func (c *cancelOnNthErrContext) Err() error {
	if c.calls.Add(1) >= c.cancelAt {
		c.once.Do(func() { close(c.done) })
		return context.Canceled
	}
	return nil
}

func TestPasswordVerificationGateRechecksCancellationAfterImmediateAcquire(t *testing.T) {
	gate := testPasswordGate(t, 1, 1, time.Second)
	ctx := newCancelOnNthErrContext(3)
	events := make(chan PasswordVerificationEvent, 1)
	called := false
	err := gate.Verify(observedContext(ctx, events), func() error {
		called = true
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("race error = %v, want context.Canceled", err)
	}
	if called {
		t.Fatal("verification started after cancellation became observable")
	}
	waitForEvent(t, events, PasswordVerificationCanceled)
	if active, queued := gateCounts(gate); active != 0 || queued != 0 {
		t.Fatalf("final gate state = active %d queued %d, want zero", active, queued)
	}
}

func TestPasswordVerificationGateRejectsAlreadyCanceledImmediateRequest(t *testing.T) {
	gate := testPasswordGate(t, 1, 1, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	events := make(chan PasswordVerificationEvent, 1)
	called := false
	err := gate.Verify(observedContext(ctx, events), func() error {
		called = true
		return nil
	})
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("canceled immediate request = err %v called %v", err, called)
	}
	waitForEvent(t, events, PasswordVerificationCanceled)
	if active, queued := gateCounts(gate); active != 0 || queued != 0 {
		t.Fatalf("final gate state = active %d queued %d, want zero", active, queued)
	}
}

func TestPasswordVerificationGateRechecksCancellationAfterQueuedHandoff(t *testing.T) {
	gate := testPasswordGate(t, 1, 1, time.Second)
	releaseActive := make(chan struct{})
	activeStarted := make(chan struct{})
	activeDone := make(chan struct{})
	go func() {
		defer close(activeDone)
		_ = gate.Verify(context.Background(), func() error {
			close(activeStarted)
			<-releaseActive
			return nil
		})
	}()
	<-activeStarted

	// Err becomes canceled on the claim check: the release path observes the
	// waiter as live and hands it the slot, then claimAdmission rejects it
	// before the verification callback can begin. Done closes on that claim
	// check, so the test deterministically exercises handoff rather than an
	// earlier select cancellation.
	ctx := newCancelOnNthErrContext(4)
	events := make(chan PasswordVerificationEvent, 2)
	called := false
	done := make(chan error, 1)
	go func() {
		done <- gate.Verify(observedContext(ctx, events), func() error {
			called = true
			return nil
		})
	}()
	waitForEvent(t, events, PasswordVerificationQueued)
	close(releaseActive)
	<-activeDone
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("handoff race error = %v, want context.Canceled", err)
	}
	if called {
		t.Fatal("queued verification started after cancellation became observable")
	}
	waitForEvent(t, events, PasswordVerificationCanceled)
	if active, queued := gateCounts(gate); active != 0 || queued != 0 {
		t.Fatalf("final gate state = active %d queued %d, want zero", active, queued)
	}
}
