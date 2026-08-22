package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

// loginAdmission gives only the password-login route enough lifetime to drain
// the bounded Argon2 queue. It does not change the server-wide write timeout or
// the lifetime of any unrelated endpoint.
func (f *SessionFoundation) loginAdmission() gin.HandlerFunc {
	return func(c *gin.Context) {
		deadline := time.Now().Add(f.loginRequestTimeout)
		ctx, cancel := context.WithDeadline(c.Request.Context(), deadline)
		defer cancel()
		ctx = identity.WithPasswordVerificationObserver(ctx, func(event identity.PasswordVerificationEvent) {
			if event == identity.PasswordVerificationQueued {
				c.Set(passwordVerificationQueuedContextKey, true)
			}
			c.Set(passwordVerificationOutcomeContextKey, string(event))
		})
		ctx = identity.WithLoginTimingObserver(ctx, func(event identity.LoginTimingEvent) {
			micros := event.Duration.Microseconds()
			switch event.Stage {
			case identity.LoginStageCandidateLookup:
				c.Set(loginCandidateMicrosContextKey, micros)
			case identity.LoginStageGateWait:
				c.Set(passwordQueueMicrosContextKey, micros)
			case identity.LoginStagePasswordVerify:
				c.Set(passwordVerifyMicrosContextKey, micros)
			case identity.LoginStageSessionPrepare:
				c.Set(sessionPrepareMicrosContextKey, micros)
			case identity.LoginStageSessionWrite:
				c.Set(sessionWriteMicrosContextKey, micros)
			}
		})
		c.Request = c.Request.WithContext(ctx)

		controller := http.NewResponseController(c.Writer)
		if err := controller.SetWriteDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
			writeProblem(c, problem.AuthenticationUnavailable())
			return
		}
		c.Next()
	}
}
