package httpapi

import (
	"math"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

func (f *SessionFoundation) requireSessionRateDecision(
	endpoint string,
	identifier func(*gin.Context) string,
) gin.HandlerFunc {
	policy := f.endpointPolicies[endpoint]
	return func(c *gin.Context) {
		input := ratelimit.Input{ClientIP: c.ClientIP()}
		if endpoint == "sessions" {
			input.AnonymousID = c.GetString(anonymousIDContextKey)
		}
		if identifier != nil {
			input.Identifier = identifier(c)
		}
		decision := f.limiter.Decide(c.Request.Context(), policy, input)
		c.Set(limiterOutcomeContextKey, string(decision.Outcome))
		if decision.Allowed {
			c.Next()
			return
		}
		c.Header("Cache-Control", "no-store")
		if decision.Outcome == ratelimit.OutcomeDenied ||
			decision.Outcome == ratelimit.OutcomeFallbackDenied {
			if seconds := int(math.Ceil(decision.RetryAfter.Seconds())); seconds > 0 {
				c.Header("Retry-After", strconv.Itoa(seconds))
			}
			writeProblem(c, problem.RateLimited())
			return
		}
		writeProblem(c, problem.AuthenticationUnavailable())
	}
}

func loginRateIdentifier(c *gin.Context) string {
	request := c.MustGet(strictJSONBodyContextKey).(*sessionLoginRequest)
	return rateLimitEmailIdentifier(request.Email)
}

func sessionRateIdentifier(c *gin.Context) string {
	if digest := c.GetString(sessionCredentialDigestContextKey); digest != "" {
		return digest
	}
	digest, err := auth.SessionCredentialDigest(c.Request)
	if err != nil {
		return identity.DigestToken("invalid-session-authority")
	}
	return digest
}
