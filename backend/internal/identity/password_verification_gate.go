package identity

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrPasswordVerificationSaturated = errors.New("password verification admission is saturated")
	ErrPasswordVerificationTimeout   = errors.New("password verification admission timed out")
)

type PasswordVerificationEvent string

const (
	PasswordVerificationQueued    PasswordVerificationEvent = "QUEUED"
	PasswordVerificationAdmitted  PasswordVerificationEvent = "ADMITTED"
	PasswordVerificationSaturated PasswordVerificationEvent = "SATURATED"
	PasswordVerificationTimeout   PasswordVerificationEvent = "TIMEOUT"
	PasswordVerificationCanceled  PasswordVerificationEvent = "CANCELED"
)

type passwordVerificationObserverKey struct{}

func WithPasswordVerificationObserver(
	ctx context.Context,
	observe func(PasswordVerificationEvent),
) context.Context {
	return context.WithValue(ctx, passwordVerificationObserverKey{}, observe)
}

func observePasswordVerification(ctx context.Context, event PasswordVerificationEvent) {
	if observe, ok := ctx.Value(passwordVerificationObserverKey{}).(func(PasswordVerificationEvent)); ok {
		observe(event)
	}
}

type PasswordVerificationGateOptions struct {
	Concurrency int
	Queue       int
	QueueWait   time.Duration
}

type passwordVerificationWaiter struct {
	ctx      context.Context
	ready    chan struct{}
	queued   bool
	admitted bool
	element  *list.Element
}

// PasswordVerificationGate bounds active Argon2id work and owns an explicit
// FIFO of waiting requests. Callers wait in their request goroutine; the gate
// creates no background workers or cancellation goroutines.
type PasswordVerificationGate struct {
	mu          sync.Mutex
	concurrency int
	queueLimit  int
	queueWait   time.Duration
	active      int
	waiters     list.List
}

func NewPasswordVerificationGate(options PasswordVerificationGateOptions) (*PasswordVerificationGate, error) {
	if options.Concurrency <= 0 || options.Queue <= 0 || options.QueueWait <= 0 {
		return nil, errors.New("password verification gate values must be positive")
	}
	return &PasswordVerificationGate{
		concurrency: options.Concurrency,
		queueLimit:  options.Queue,
		queueWait:   options.QueueWait,
	}, nil
}

func (g *PasswordVerificationGate) Verify(ctx context.Context, verify func() error) error {
	if err := ctx.Err(); err != nil {
		observePasswordVerificationCancellation(ctx)
		return err
	}

	g.mu.Lock()
	if err := ctx.Err(); err != nil {
		g.mu.Unlock()
		observePasswordVerificationCancellation(ctx)
		return err
	}
	if g.active < g.concurrency && g.waiters.Len() == 0 {
		g.active++
		g.mu.Unlock()
		return g.runAdmitted(ctx, verify)
	}
	g.pruneCanceledWaitersLocked()
	if g.active < g.concurrency && g.waiters.Len() == 0 {
		g.active++
		g.mu.Unlock()
		return g.runAdmitted(ctx, verify)
	}
	if g.waiters.Len() >= g.queueLimit {
		g.mu.Unlock()
		observePasswordVerification(ctx, PasswordVerificationSaturated)
		return ErrPasswordVerificationSaturated
	}
	waiter := &passwordVerificationWaiter{
		ctx: ctx, ready: make(chan struct{}), queued: true,
	}
	waiter.element = g.waiters.PushBack(waiter)
	g.mu.Unlock()
	observePasswordVerification(ctx, PasswordVerificationQueued)

	timer := time.NewTimer(g.queueWait)
	defer stopPasswordVerificationTimer(timer)
	select {
	case <-waiter.ready:
		if !g.claimAdmission(waiter) {
			observePasswordVerificationCancellation(ctx)
			return ctx.Err()
		}
		return g.runAdmitted(ctx, verify)
	case <-ctx.Done():
		g.cancelWaiter(waiter)
		observePasswordVerificationCancellation(ctx)
		return ctx.Err()
	case <-timer.C:
		g.cancelWaiter(waiter)
		if err := ctx.Err(); err != nil {
			observePasswordVerificationCancellation(ctx)
			return err
		}
		observePasswordVerification(ctx, PasswordVerificationTimeout)
		return ErrPasswordVerificationTimeout
	}
}

func (g *PasswordVerificationGate) runAdmitted(ctx context.Context, verify func() error) error {
	if err := ctx.Err(); err != nil {
		g.release()
		observePasswordVerificationCancellation(ctx)
		return err
	}
	observePasswordVerification(ctx, PasswordVerificationAdmitted)
	defer g.release()
	return verify()
}

func (g *PasswordVerificationGate) claimAdmission(waiter *passwordVerificationWaiter) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !waiter.admitted {
		return false
	}
	if waiter.ctx.Err() != nil {
		waiter.admitted = false
		g.releaseLocked()
		return false
	}
	return true
}

func (g *PasswordVerificationGate) cancelWaiter(waiter *passwordVerificationWaiter) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if waiter.admitted {
		waiter.admitted = false
		g.releaseLocked()
		return
	}
	if !waiter.queued {
		return
	}
	g.waiters.Remove(waiter.element)
	waiter.element = nil
	waiter.queued = false
	g.admitWaitersLocked()
}

func (g *PasswordVerificationGate) release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.releaseLocked()
}

func (g *PasswordVerificationGate) releaseLocked() {
	g.active--
	g.admitWaitersLocked()
}

func (g *PasswordVerificationGate) admitWaitersLocked() {
	for g.active < g.concurrency && g.waiters.Len() > 0 {
		front := g.waiters.Front()
		waiter := front.Value.(*passwordVerificationWaiter)
		g.waiters.Remove(front)
		waiter.element = nil
		waiter.queued = false
		if waiter.ctx.Err() != nil {
			close(waiter.ready)
			continue
		}
		waiter.admitted = true
		g.active++
		close(waiter.ready)
	}
}

func (g *PasswordVerificationGate) pruneCanceledWaitersLocked() {
	for element := g.waiters.Front(); element != nil; {
		next := element.Next()
		waiter := element.Value.(*passwordVerificationWaiter)
		if waiter.ctx.Err() != nil {
			g.waiters.Remove(element)
			waiter.element = nil
			waiter.queued = false
			close(waiter.ready)
		}
		element = next
	}
}

func observePasswordVerificationCancellation(ctx context.Context) {
	if errors.Is(ctx.Err(), context.Canceled) {
		observePasswordVerification(ctx, PasswordVerificationCanceled)
		return
	}
	observePasswordVerification(ctx, PasswordVerificationTimeout)
}

func stopPasswordVerificationTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
