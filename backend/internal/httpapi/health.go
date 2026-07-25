package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/requestid"
)

// Probe route templates. They sit outside /api/v1 because they are not part of
// the product API: no versioning promise, no session, no CSRF, no
// localization, and no authentication.
const (
	livenessPath  = "/healthz"
	readinessPath = "/readyz"
)

// healthResponse is the entire probe body. There is no field for a reason,
// because these endpoints are unauthenticated and any diagnostic text would be
// readable by anyone who can reach the port. Per-check state is limited to the
// closed Status set.
type healthResponse struct {
	Status string                   `json:"status"`
	Checks map[string]health.Status `json:"checks,omitempty"`
}

// livenessHandler answers whether the process is running.
//
// It touches no dependency, so a database or Redis outage cannot cause the
// orchestrator to kill a process that is merely waiting for its dependencies
// to come back.
func livenessHandler(reporter *health.Reporter) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if !reporter.Live() {
			c.JSON(http.StatusServiceUnavailable, healthResponse{Status: "unhealthy"})
			return
		}
		c.JSON(http.StatusOK, healthResponse{Status: "ok"})
	}
}

// readinessHandler answers whether this instance may receive normal traffic.
//
// Failure detail goes to the protected log under the trusted request ID; the
// response says only which check failed, never why.
func readinessHandler(reporter *health.Reporter, logger *logging.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")

		result := reporter.Ready(c.Request.Context())

		for name, err := range result.Failures {
			logger.DependencyUnready(logging.DependencyEvent{
				RequestID: requestid.FromContext(c.Request.Context()),
				Check:     name,
				Err:       err,
			})
		}

		if !result.Ready {
			c.JSON(http.StatusServiceUnavailable, healthResponse{
				Status: "not_ready",
				Checks: result.Checks,
			})
			return
		}
		c.JSON(http.StatusOK, healthResponse{Status: "ok", Checks: result.Checks})
	}
}

// isProbePath reports whether a route template is a probe endpoint.
//
// Probes run on a short interval for the life of the process. Logging every
// successful one at info level would bury real request traffic, so the request
// logger drops successful probes to debug. Failing probes stay at their normal
// level: those are the ones worth seeing.
func isProbePath(routeTemplate string) bool {
	return routeTemplate == livenessPath || routeTemplate == readinessPath
}
