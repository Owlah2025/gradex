package media

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrencyGateLimitsParallelWork(t *testing.T) {
	t.Parallel()
	gate := newConcurrencyGate(2)
	start := make(chan struct{})
	release := make(chan struct{})
	var active atomic.Int32
	var maximum atomic.Int32
	var workers sync.WaitGroup

	for range 20 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			if err := gate.run(context.Background(), func() error {
				current := active.Add(1)
				for {
					observed := maximum.Load()
					if current <= observed || maximum.CompareAndSwap(observed, current) {
						break
					}
				}
				<-release
				active.Add(-1)
				return nil
			}); err != nil {
				t.Errorf("running gated work: %v", err)
			}
		}()
	}

	close(start)
	deadline := time.Now().Add(time.Second)
	for maximum.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := maximum.Load(); got != 2 {
		t.Fatalf("maximum parallel work before release = %d, want 2", got)
	}
	close(release)
	workers.Wait()
	if got := maximum.Load(); got != 2 {
		t.Fatalf("maximum parallel work = %d, want 2", got)
	}
}

func TestConcurrencyGateHonorsCancellationWhileWaiting(t *testing.T) {
	t.Parallel()
	gate := newConcurrencyGate(1)
	release := make(chan struct{})
	entered := make(chan struct{})
	go func() {
		_ = gate.run(context.Background(), func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := gate.run(ctx, func() error { return nil }); err != context.Canceled {
		t.Fatalf("waiting on a full gate returned %v, want context.Canceled", err)
	}
	close(release)
}
