package httpapi

import (
	"math"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

const limiterOutcomeContextKey = "limiterOutcome"

func (f *AdmissionFoundation) requireRateDecision(
	endpoint string,
	identifier func(*gin.Context) string,
) gin.HandlerFunc {
	policy := f.endpointPolicies[endpoint]
	return func(c *gin.Context) {
		input := ratelimit.Input{
			ClientIP:    c.ClientIP(),
			AnonymousID: c.GetString(anonymousIDContextKey),
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
			retrySeconds := int(math.Ceil(decision.RetryAfter.Seconds()))
			if retrySeconds > 0 {
				c.Header("Retry-After", strconv.Itoa(retrySeconds))
			}
			writeProblem(c, problem.RateLimited())
			return
		}
		writeProblem(c, problem.RateLimitingUnavailable())
	}
}

func registrationIdentifier(c *gin.Context) string {
	request := c.MustGet(strictJSONBodyContextKey).(*studentRegistrationRequest)
	return rateLimitEmailIdentifier(request.Email)
}

func verificationRequestIdentifier(c *gin.Context) string {
	request := c.MustGet(strictJSONBodyContextKey).(*verificationRequestBody)
	return rateLimitEmailIdentifier(request.Email)
}

func verificationTokenIdentifier(c *gin.Context) string {
	request := c.MustGet(strictJSONBodyContextKey).(*verificationConsumptionBody)
	if _, err := identity.DigestActionSecret(request.Token); err != nil {
		return "invalid-verification-token"
	}
	return request.Token
}

func rateLimitEmailIdentifier(raw string) string {
	normalized, err := identity.NormalizeEmail(raw)
	if err != nil {
		return "invalid-email-address"
	}
	return strings.ToLower(normalized)
}
