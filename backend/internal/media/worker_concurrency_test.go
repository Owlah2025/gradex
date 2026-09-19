package media

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

type concurrencyTestProcessor func(context.Context, ObjectVersion) (TranscodeResult, error)

func (p concurrencyTestProcessor) Transcode(ctx context.Context, object ObjectVersion) (TranscodeResult, error) {
	return p(ctx, object)
}

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
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_ = gate.run(context.Background(), func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered

	ctx, cancel := context.WithCancel(context.Background())
	secondStarted := make(chan struct{})
	secondResult := make(chan error, 1)
	secondEntered := false
	go func() {
		close(secondStarted)
		secondResult <- gate.run(ctx, func() error {
			secondEntered = true
			return nil
		})
	}()
	<-secondStarted
	deadline := time.Now().Add(time.Second)
	for gate.InFlight() < 2 && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if gate.InFlight() != 2 {
		t.Fatalf("gate participants before cancellation = %d, want 2", gate.InFlight())
	}
	cancel()
	if err := <-secondResult; err != context.Canceled {
		t.Fatalf("waiting on a full gate returned %v, want context.Canceled", err)
	}
	if secondEntered {
		t.Fatal("cancelled waiter entered gated work")
	}
	if got := gate.Active(); got != 1 {
		t.Fatalf("active count while first job owns the gate = %d, want 1", got)
	}
	if got := gate.InFlight(); got != 1 {
		t.Fatalf("gate participants after cancelled waiter = %d, want 1", got)
	}
	close(release)
	<-firstDone
	if got := gate.Active(); got != 0 {
		t.Fatalf("active count after first job release = %d, want 0", got)
	}
	if err := gate.run(context.Background(), func() error { return nil }); err != nil {
		t.Fatalf("reusing gate after cancelled waiter: %v", err)
	}
}

func TestConcurrencyGateReleasesAfterProcessorFailure(t *testing.T) {
	gate := newConcurrencyGate(1)
	wantErr := errors.New("processor failed")
	if err := gate.run(context.Background(), func() error { return wantErr }); !errors.Is(err, wantErr) {
		t.Fatalf("failed gated work = %v, want %v", err, wantErr)
	}
	if got := gate.Active(); got != 0 {
		t.Fatalf("active count after failed work = %d, want 0", got)
	}
	if err := gate.run(context.Background(), func() error { return nil }); err != nil {
		t.Fatalf("gate did not recover after failed work: %v", err)
	}
}

func TestNewWorkerUsesConfiguredTranscodeConcurrency(t *testing.T) {
	scanner, err := NewUnavailableScanner("test scanner")
	if err != nil {
		t.Fatalf("NewUnavailableScanner: %v", err)
	}
	adapter, err := NewScannerAdapter(scanner)
	if err != nil {
		t.Fatalf("NewScannerAdapter: %v", err)
	}
	var writer outbox.Writer
	worker, err := NewWorker(WorkerOptions{
		DB: new(pgxpool.Pool), Scanner: adapter,
		Process: concurrencyTestProcessor(func(context.Context, ObjectVersion) (TranscodeResult, error) {
			return TranscodeResult{}, nil
		}),
		Outbox: &writer, TranscodeConcurrency: 1,
	})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	if got := worker.transcodeGate.Limit(); got != 1 {
		t.Fatalf("worker transcode gate limit = %d, want 1", got)
	}
}
