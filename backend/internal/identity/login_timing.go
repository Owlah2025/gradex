package identity

import (
	"context"
	"time"
)

// LoginStage identifies a credential-safe portion of the login path. Timing
// observers receive durations only; no account or credential data crosses the
// observability boundary.
type LoginStage string

const (
	LoginStageCandidateLookup LoginStage = "CANDIDATE_LOOKUP"
	LoginStageGateWait        LoginStage = "GATE_WAIT"
	LoginStagePasswordVerify  LoginStage = "PASSWORD_VERIFY"
	LoginStageSessionPrepare  LoginStage = "SESSION_PREPARE"
	LoginStageSessionWrite    LoginStage = "SESSION_WRITE"
)

type LoginTimingEvent struct {
	Stage    LoginStage
	Duration time.Duration
}

type loginTimingObserverKey struct{}

func WithLoginTimingObserver(
	ctx context.Context,
	observe func(LoginTimingEvent),
) context.Context {
	return context.WithValue(ctx, loginTimingObserverKey{}, observe)
}

func observeLoginTiming(ctx context.Context, stage LoginStage, started time.Time) {
	observe, ok := ctx.Value(loginTimingObserverKey{}).(func(LoginTimingEvent))
	if !ok {
		return
	}
	observe(LoginTimingEvent{Stage: stage, Duration: time.Since(started)})
}
