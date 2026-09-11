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
	ctxUserIDKey = "userID"
	// ctxTrustedDeviceKey is the trusted device the calling session is bound
	// to. Present only on routes that have already established device trust,
	// and always resolved server-side from the session.
	ctxTrustedDeviceKey = "trustedDeviceID"
	ctxPrincipalKey     = "principal"
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

		// Device policy narrows the account-level decision rather than
		// replacing it. Applying it here rather than route by route is what
		// makes "a browser that has not completed device trust holds only the
		// self-service capabilities" true everywhere at once, including on
		// routes written before this feature existed. Instructor and Admin
		// principals are returned unchanged by construction.
		decision := identity.AuthorizeSessionDevice(principal, auth.DeviceTrustFromContext(c), capability)
		if !decision.Allowed {
			logger.AuthorizationDenied(logging.AuthorizationEvent{
				RequestID:     requestid.FromContext(c.Request.Context()),
				Method:        c.Request.Method,
				RouteTemplate: c.FullPath(),
				Capability:    string(capability),
				DenyReason:    string(decision.Reason),
			})
			switch decision.Reason {
			case identity.DenyDeviceTrustRequired:
				writeProblem(c, problem.DeviceTrustRequired())
			case identity.DenyDeviceAdoptionRequired:
				writeProblem(c, problem.DeviceAdoptionRequired())
			default:
				writeProblem(c, problem.NotAuthorized())
			}
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
		c.Set(ctxTrustedDeviceKey, auth.TrustedDeviceFromContext(c))
		c.Next()
	}
}
