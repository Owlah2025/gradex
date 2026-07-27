package httpapi

import (
	"math"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

func (f *StaffFoundation) requireStaffRateDecision(
	endpoint string,
	identifier func(*gin.Context) string,
) gin.HandlerFunc {
	policy := f.endpointPolicies[endpoint]
	return func(c *gin.Context) {
		input := ratelimit.Input{ClientIP: c.ClientIP()}
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
		writeProblem(c, problem.Internal(""))
	}
}

func staffPreviewBearerIdentifier(c *gin.Context) string {
	request := c.MustGet(strictJSONBodyContextKey).(*invitationPreviewRequest)
	if _, err := identity.DigestActionSecret(request.Bearer); err != nil {
		return "invalid-staff-bearer"
	}
	return request.Bearer
}

func staffCompletionBearerIdentifier(c *gin.Context) string {
	request := c.MustGet(strictJSONBodyContextKey).(*completeInvitationRequest)
	if _, err := identity.DigestActionSecret(request.Bearer); err != nil {
		return "invalid-staff-bearer"
	}
	return request.Bearer
}

func staffInvitationEmailIdentifier(c *gin.Context) string {
	request := c.MustGet(strictJSONBodyContextKey).(*createInvitationRequest)
	return rateLimitEmailIdentifier(request.Email)
}
