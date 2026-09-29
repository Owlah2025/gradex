package httpapi

import (
	"math"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

func (f *AdminFoundation) requireAdminRateDecision(endpoint string) gin.HandlerFunc {
	policy, configured := f.endpointPolicies[endpoint]
	return func(c *gin.Context) {
		if f.limiter == nil || !configured {
			// Isolated router fixtures may mount only the read foundation. Production
			// composition supplies both policies and the shared limiter.
			c.Next()
			return
		}
		decision := f.limiter.Decide(c.Request.Context(), policy, ratelimit.Input{
			ClientIP: c.ClientIP(), Identifier: c.GetString(ctxUserIDKey),
		})
		if decision.Allowed {
			c.Next()
			return
		}
		if retry := int(math.Ceil(decision.RetryAfter.Seconds())); retry > 0 {
			c.Header("Retry-After", strconv.Itoa(retry))
		}
		if decision.Outcome == ratelimit.OutcomeDenied || decision.Outcome == ratelimit.OutcomeFallbackDenied {
			writeProblem(c, problem.RateLimited())
			return
		}
		writeProblem(c, problem.RateLimitingUnavailable())
	}
}
