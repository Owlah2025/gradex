package httpapi

import (
	"errors"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/requestid"
)

// ctxSafeErrorCodeKey carries the Problem Details code a handler produced, so
// the request log can record it without re-deriving it from the response body.
const (
	ctxSafeErrorCodeKey             = "safeErrorCode"
	admissionFailureStageContextKey = "admissionFailureStage"
)

const (
	admissionFailureStageStructure       = logging.AdmissionFailureStructure
	admissionFailureStageBrowserSecurity = logging.AdmissionFailureBrowserSecurity
	admissionFailureStageRateDecision    = logging.AdmissionFailureRateDecision
	admissionFailureStageDomain          = logging.AdmissionFailureDomain
)

func setAdmissionFailureStage(c *gin.Context, stage logging.AdmissionFailureStage) {
	c.Set(admissionFailureStageContextKey, stage)
}

func admissionFailureStageOf(c *gin.Context) logging.AdmissionFailureStage {
	value, exists := c.Get(admissionFailureStageContextKey)
	if !exists {
		return ""
	}
	stage, _ := value.(logging.AdmissionFailureStage)
	return stage
}

// unmatchedRoute stands in for the route template when no route matched. The
// literal path must never reach a log: it carries identifiers, and on a
// mistyped or hostile request it can carry a token.
const unmatchedRoute = "unmatched"

// writeProblem is the single place an error response is produced. It stamps
// the trusted request ID into the body, records the safe code for the request
// log, and aborts the chain.
func writeProblem(c *gin.Context, p problem.Problem) {
	p = p.WithRequestID(requestid.FromContext(c.Request.Context()))
	c.Set(ctxSafeErrorCodeKey, p.Code)
	_ = problem.Write(c.Writer, p)
	c.Abort()
}

// requestIDMiddleware creates the trusted correlation identifier for this
// attempt.
//
// The identifier is always freshly generated. A client-supplied X-Request-ID
// is untrusted input (§2.1): it is sanitized and kept only as a parent
// correlation hint, and can never become the value Gradex reports.
//
// The response header is set before the chain runs, so the identifier reaches
// the client even when a handler commits the response itself or panics partway
// through.
func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := requestid.New()

		ctx := requestid.WithTrusted(c.Request.Context(), id)
		if parent := requestid.SanitizeParent(c.GetHeader(requestid.HeaderName)); parent != "" {
			ctx = requestid.WithParent(ctx, parent)
		}
		c.Request = c.Request.WithContext(ctx)

		c.Header(requestid.HeaderName, id)
		c.Next()
	}
}

// requestLogger records one structured line per completed attempt.
//
// It is installed outside recovery so a recovered panic is still logged with
// its final 500 status rather than disappearing from the request log.
func requestLogger(logger *logging.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		size := c.Writer.Size()
		if size < 0 {
			// Gin reports -1 when no body was written.
			size = 0
		}

		ctx := c.Request.Context()
		logger.RequestCompleted(logging.RequestEvent{
			RequestID:             requestid.FromContext(ctx),
			ParentRequestID:       requestid.ParentFromContext(ctx),
			Method:                c.Request.Method,
			RouteTemplate:         routeTemplateOf(c),
			Status:                c.Writer.Status(),
			DurationMillis:        time.Since(start).Milliseconds(),
			ResponseSize:          size,
			SafeErrorCode:         c.GetString(ctxSafeErrorCodeKey),
			LimiterOutcome:        c.GetString(limiterOutcomeContextKey),
			AdmissionFailureStage: admissionFailureStageOf(c),
			Routine:               isProbePath(routeTemplateOf(c)),
		})
	}
}

// recovery turns a panic into a generic Problem Details 500.
//
// The panic value itself is never logged or returned: it routinely holds
// whatever the handler was working with, which may be request data or a
// credential. Its Go type and a bounded stack go to the protected log,
// correlated by request ID; the client gets a fixed safe body.
func recovery(logger *logging.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			r := recover()
			if r == nil {
				return
			}

			// A handler that deliberately abandoned the response — a closed
			// client connection, for instance — must stay abandoned rather
			// than be converted into a response.
			if err, ok := r.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(r)
			}

			ctx := c.Request.Context()
			committed := c.Writer.Written()

			logger.PanicRecovered(logging.PanicEvent{
				RequestID:         requestid.FromContext(ctx),
				ParentRequestID:   requestid.ParentFromContext(ctx),
				Method:            c.Request.Method,
				RouteTemplate:     routeTemplateOf(c),
				ErrorClass:        logging.ErrorClassOf(r),
				Stack:             string(debug.Stack()),
				ResponseCommitted: committed,
			})

			if committed {
				// Headers or bytes already went out. Appending a second
				// response would produce an invalid message, so the request is
				// simply abandoned.
				c.Abort()
				return
			}

			writeProblem(c, problem.Internal(requestid.FromContext(ctx)))
		}()

		c.Next()
	}
}

// routeTemplateOf returns the matched route pattern, never the literal path.
func routeTemplateOf(c *gin.Context) string {
	if route := c.FullPath(); route != "" {
		return route
	}
	return unmatchedRoute
}

// notFoundHandler answers an unmatched route with the standard envelope.
func notFoundHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		writeProblem(c, problem.NotFound())
	}
}

// methodNotAllowedHandler answers a known path carrying an unsupported method,
// with the Allow companion header §2.3 requires.
func methodNotAllowedHandler(engine *gin.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		if allowed := allowedMethods(engine, c.Request.URL.Path); allowed != "" {
			c.Header("Allow", allowed)
		}
		writeProblem(c, problem.MethodNotAllowed())
	}
}

// allowedMethods reports which methods are registered for the route matching
// path. It names methods only — nothing about the path itself is disclosed.
func allowedMethods(engine *gin.Engine, path string) string {
	seen := map[string]bool{}
	var methods []string
	for _, route := range engine.Routes() {
		if seen[route.Method] || !routePatternMatches(route.Path, path) {
			continue
		}
		seen[route.Method] = true
		methods = append(methods, route.Method)
	}
	return strings.Join(methods, ", ")
}

// routePatternMatches compares a Gin route pattern against a concrete path.
// ":param" matches one non-empty segment; "*catchall" matches the remainder.
func routePatternMatches(pattern, path string) bool {
	patternParts := strings.Split(strings.Trim(pattern, "/"), "/")
	pathParts := strings.Split(strings.Trim(path, "/"), "/")

	for i, want := range patternParts {
		if strings.HasPrefix(want, "*") {
			return true
		}
		if i >= len(pathParts) {
			return false
		}
		if strings.HasPrefix(want, ":") {
			if pathParts[i] == "" {
				return false
			}
			continue
		}
		if want != pathParts[i] {
			return false
		}
	}
	return len(patternParts) == len(pathParts)
}
