package httpapi

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/requestid"
)

const (
	ctxUserIDKey    = "userID"
	ctxPrincipalKey = "principal"
)

// requireCapability resolves the caller's Principal and applies the
// deny-by-default policy. Must run after requireAuth.
//
// This is the middleware that makes the restricted bootstrap principal real:
// the bootstrap Administrator authenticates successfully and is then refused
// every capability except changing its password and ending its session, because
// its credential is CHANGE_REQUIRED. Nothing about that depends on the route
// knowing what a bootstrap Account is.
//
// The principal is resolved per request rather than read from the session, so a
// suspension or a completed password change takes effect on the next request
// instead of whenever a session happens to be rebuilt.
//
// Denials are uniform to the caller and typed in the log. §5 forbids letting a
// response distinguish "suspended" from "wrong role" from "no such Account";
// §6.1 puts the typed reason in security monitoring instead.
func requireCapability(
	resolver identity.PrincipalResolver,
	logger *logging.Logger,
	capability identity.Capability,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID := c.GetString(ctxUserIDKey)

		principal, err := resolver.ResolvePrincipal(c.Request.Context(), accountID)
		if err != nil {
			if errors.Is(err, identity.ErrPrincipalNotFound) {
				logger.AuthorizationDenied(logging.AuthorizationEvent{
					RequestID:     requestid.FromContext(c.Request.Context()),
					Method:        c.Request.Method,
					RouteTemplate: c.FullPath(),
					Capability:    string(capability),
					DenyReason:    string(identity.DenyPrincipalNotFound),
				})
				writeProblem(c, problem.NotAuthorized())
				return
			}
			// A resolution fault is not a denial. Reporting it as one would
			// make a database outage look like an authorization decision in
			// every dashboard that counts refusals.
			logger.AuthorizationFault(logging.AuthorizationFaultEvent{
				RequestID:     requestid.FromContext(c.Request.Context()),
				Method:        c.Request.Method,
				RouteTemplate: c.FullPath(),
				Capability:    string(capability),
				Err:           err,
			})
			writeProblem(c, problem.Internal(""))
			return
		}

		decision := identity.Authorize(principal, capability)
		if !decision.Allowed {
			logger.AuthorizationDenied(logging.AuthorizationEvent{
				RequestID:     requestid.FromContext(c.Request.Context()),
				Method:        c.Request.Method,
				RouteTemplate: c.FullPath(),
				Capability:    string(capability),
				DenyReason:    string(decision.Reason),
			})
			writeProblem(c, problem.NotAuthorized())
			return
		}

		c.Set(ctxPrincipalKey, principal)
		c.Next()
	}
}

// principalFrom returns the Principal established by requireCapability.
func principalFrom(c *gin.Context) (identity.Principal, bool) {
	v, ok := c.Get(ctxPrincipalKey)
	if !ok {
		return identity.Principal{}, false
	}
	p, ok := v.(identity.Principal)
	return p, ok
}

// requireAuth resolves the caller's identity but does not check any
// entitlement — used as the shared first step by both the instructor and
// student middleware groups.
//
// The authenticator's error is not reported: it describes why authentication
// failed, and §5 keeps hidden Account state out of public responses. The
// response is the uniform challenge for every cause.
func requireAuth(authenticator auth.Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, err := authenticator.UserFromRequest(c)
		if err != nil {
			writeProblem(c, problem.Unauthenticated())
			return
		}
		c.Set(ctxUserIDKey, userID)
		c.Next()
	}
}

// requireInstructor checks the authenticated user owns the :lessonID lesson
// as its instructor. Must run after requireAuth.
//
// A lookup failure and a denial are reported differently — one is a fault, the
// other a decision — but neither says which lesson, who owns it, or whether it
// exists. §6.1 keeps typed policy reasons in security monitoring, not in the
// response.
func requireInstructor(entitlements auth.EntitlementChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString(ctxUserIDKey)
		lessonID := c.Param("lessonID")
		ok, err := entitlements.IsInstructorForLesson(c.Request.Context(), userID, lessonID)
		if err != nil {
			writeProblem(c, problem.Internal(""))
			return
		}
		if !ok {
			writeProblem(c, problem.NotAuthorized())
			return
		}
		c.Next()
	}
}

// requireStudentAccess checks the authenticated user has purchased/enrolled
// in the course containing :lessonID. Must run after requireAuth.
func requireStudentAccess(entitlements auth.EntitlementChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString(ctxUserIDKey)
		lessonID := c.Param("lessonID")
		ok, err := entitlements.HasAccess(c.Request.Context(), userID, lessonID)
		if err != nil {
			writeProblem(c, problem.Internal(""))
			return
		}
		if !ok {
			writeProblem(c, problem.NotAuthorized())
			return
		}
		c.Next()
	}
}
