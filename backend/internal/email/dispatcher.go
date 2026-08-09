package email

import (
	"context"
	"errors"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

type EventPhase string

const (
	PhaseQueued           EventPhase = "queued"
	PhaseAttemptStarted   EventPhase = "attempt_started"
	PhaseAccepted         EventPhase = "provider_accepted"
	PhaseTransientFailure EventPhase = "transient_failure"
	PhasePermanentFailure EventPhase = "permanent_failure"
	PhaseRetryScheduled   EventPhase = "retry_scheduled"
	PhaseExhausted        EventPhase = "exhausted"
)

type LifecycleEvent struct {
	Phase        EventPhase
	EventID      string
	Template     string
	Locale       string
	Provider     string
	Attempt      int
	FailureClass string
	ProviderCode string
	RetryAt      *time.Time
}

type Observer interface{ ObserveTransactionalEmail(LifecycleEvent) }

type DispatcherOptions struct {
	Repository    *Repository
	Outbox        *outbox.Writer
	Renderer      *Renderer
	Sender        Sender
	Observer      Observer
	LeaseDuration time.Duration
	Now           func() time.Time
}

type Dispatcher struct {
	repository    *Repository
	outbox        *outbox.Writer
	renderer      *Renderer
	sender        Sender
	observer      Observer
	leaseDuration time.Duration
	now           func() time.Time
}

func NewDispatcher(options DispatcherOptions) (*Dispatcher, error) {
	if options.Repository == nil || options.Outbox == nil || options.Renderer == nil || options.Sender == nil {
		return nil, errors.New("transactional email dispatcher dependencies are required")
	}
	if options.LeaseDuration <= 0 {
		return nil, errors.New("transactional email lease duration must be positive")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Dispatcher{options.Repository, options.Outbox, options.Renderer, options.Sender, options.Observer, options.LeaseDuration, now}, nil
}

func (d *Dispatcher) DispatchPending(ctx context.Context, limit int) (int, error) {
	now := d.now().UTC()
	claims, err := d.repository.Claim(ctx, ClaimOptions{Provider: d.sender.Provider(), Now: now, LeaseDuration: d.leaseDuration, Limit: limit})
	if err != nil {
		return 0, err
	}
	for _, claim := range claims {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if err := d.dispatchClaim(ctx, claim); err != nil {
			return 0, err
		}
	}
	return len(claims), nil
}

func (d *Dispatcher) dispatchClaim(ctx context.Context, claim Claim) error {
	d.observe(LifecycleEvent{Phase: PhaseAttemptStarted, EventID: claim.Event.ID, Template: claim.Template, Locale: claim.Locale, Provider: d.sender.Provider(), Attempt: claim.AttemptNumber})
	message, failure := d.message(ctx, claim)
	if failure != nil {
		return d.fail(ctx, claim, failure)
	}
	result, err := d.sender.Send(ctx, message, "gradex/"+claim.Event.ID)
	if err != nil {
		failure, ok := AsSendFailure(err)
		if !ok {
			failure = &SendFailure{Kind: FailureTransient, Class: "adapter", Code: "adapter"}
		}
		return d.fail(ctx, claim, failure)
	}
	return d.accept(ctx, claim, result)
}

func (d *Dispatcher) message(ctx context.Context, claim Claim) (Message, *SendFailure) {
	var payload DeliveryPayload
	if err := d.outbox.OpenProtectedPayload(ctx, claim.Event, claim.Protected, &payload); err != nil {
		return Message{}, &SendFailure{Kind: FailurePermanent, Class: "protected_payload", Code: "protected_payload"}
	}
	message, err := d.renderer.Render(RenderRequest{Event: claim.Event, Template: claim.Template, Locale: claim.Locale, Payload: payload})
	if err != nil {
		return Message{}, &SendFailure{Kind: FailurePermanent, Class: "render", Code: "render"}
	}
	return message, nil
}

func (d *Dispatcher) accept(ctx context.Context, claim Claim, result SendResult) error {
	if err := d.repository.Accept(ctx, claim, result.ProviderMessageID, d.now().UTC()); err != nil {
		return err
	}
	d.observe(LifecycleEvent{Phase: PhaseAccepted, EventID: claim.Event.ID, Template: claim.Template, Locale: claim.Locale, Provider: d.sender.Provider(), Attempt: claim.AttemptNumber})
	return nil
}

func (d *Dispatcher) fail(ctx context.Context, claim Claim, failure *SendFailure) error {
	completedAt := d.now().UTC()
	if err := d.repository.Fail(ctx, claim, failure, completedAt); err != nil {
		return err
	}
	event := LifecycleEvent{EventID: claim.Event.ID, Template: claim.Template, Locale: claim.Locale, Provider: d.sender.Provider(), Attempt: claim.AttemptNumber, FailureClass: safeClass(failure.Class), ProviderCode: safeClass(failure.Code)}
	if failure.Transient() && claim.AttemptNumber < MaxAttempts {
		event.Phase = PhaseTransientFailure
		d.observe(event)
		delay := retryDelay(claim.AttemptNumber)
		if failure.RetryAfter > delay {
			delay = failure.RetryAfter
		}
		retryAt := completedAt.Add(delay)
		event.Phase = PhaseRetryScheduled
		event.RetryAt = &retryAt
		d.observe(event)
	} else if failure.Transient() {
		event.Phase = PhaseExhausted
		d.observe(event)
	} else {
		event.Phase = PhasePermanentFailure
		d.observe(event)
	}
	return nil
}

func (d *Dispatcher) observe(event LifecycleEvent) {
	if d.observer != nil {
		d.observer.ObserveTransactionalEmail(event)
	}
}
