package httpapi

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
)

// NewRouter builds the API engine with an explicitly composed middleware
// chain.
//
// gin.Default() is deliberately not used: it installs a logger that prints the
// literal request path — identifiers, and any token that reached the URL — and
// a recovery handler that can emit the panic value. Both violate the telemetry
// boundary in design §10.2, so the chain is assembled here instead, in the
// order that boundary requires:
//
//	trusted proxy / client-IP normalization
//	  → trusted request-ID creation
//	    → structured request logging
//	      → panic recovery
//	        → routing and handlers
//
// Request-ID creation sits outside logging and recovery so every later error
// path, including a panic, already has the trusted identifier to report.
func NewRouter(
	cfg *config.Config,
	logger *logging.Logger,
	reporter *health.Reporter,
	authenticator auth.Authenticator,
	principals identity.PrincipalResolver,
	options ...RouterOption,
) (*gin.Engine, error) {
	r, err := newEngine(cfg, logger)
	if err != nil {
		return nil, err
	}
	if principals == nil {
		// Fail at construction rather than at the first request. A router built
		// without a principal resolver would serve protected routes with no
		// capability decision at all, which is the failure this whole link
		// exists to prevent — and it must not be possible to reach it by
		// forgetting an argument.
		return nil, fmt.Errorf("principal resolver is required")
	}
	routerConfig := routerOptions{}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("router option is required")
		}
		if err := option(&routerConfig); err != nil {
			return nil, fmt.Errorf("configuring router option: %w", err)
		}
	}

	// Probes sit outside /api/v1: no version promise, no session, no CSRF, no
	// authentication, and no idle-session extension.
	r.GET(livenessPath, livenessHandler(reporter))
	r.GET(readinessPath, readinessHandler(reporter, logger))

	// Staff mutations carry the S1B2 session and CSRF boundary. Without a
	// session foundation there is nothing to carry it, so the router refuses to
	// build rather than silently mounting Admin state changes unprotected.
	if routerConfig.staff != nil && routerConfig.sessions == nil {
		return nil, fmt.Errorf("staff foundation requires a session foundation")
	}

	v1 := r.Group("/api/v1")
	if routerConfig.sessions != nil {
		mountSessionRoutes(v1, routerConfig.sessions, authenticator, principals, logger)
	}
	if routerConfig.admission != nil {
		mountAdmissionRoutesWithBootstrap(
			v1, routerConfig.admission, routerConfig.sessions == nil,
		)
	}
	if routerConfig.staff != nil {
		mountStaffRoutes(v1, routerConfig.staff, routerConfig.sessions, authenticator, principals, logger)
	}
	if routerConfig.catalog != nil {
		if err := mountCatalogRoutes(v1, routerConfig.catalog, routerConfig.media, routerConfig.sessions, authenticator, principals, logger); err != nil {
			return nil, fmt.Errorf("mounting catalog routes: %w", err)
		}
	}
	if routerConfig.publicCatalog != nil {
		if err := mountPublicCatalogRoutes(v1, routerConfig.publicCatalog); err != nil {
			return nil, fmt.Errorf("mounting public catalogue routes: %w", err)
		}
	}

	if routerConfig.media != nil {
		mountMediaRoutes(v1, routerConfig.media, authenticator, principals, logger)
	}
	if routerConfig.learning != nil {
		if err := mountLearningRoutes(v1, routerConfig.learning, authenticator, principals, logger); err != nil {
			return nil, fmt.Errorf("mounting learning routes: %w", err)
		}
	}
	if routerConfig.access != nil {
		if err := mountAccessRoutes(v1, routerConfig.access, routerConfig.sessions, authenticator, principals, logger); err != nil {
			return nil, fmt.Errorf("mounting access routes: %w", err)
		}
	}

	return r, nil
}

func mountAdmissionRoutes(v1 *gin.RouterGroup, foundation *AdmissionFoundation) {
	mountAdmissionRoutesWithBootstrap(v1, foundation, true)
}

func mountAdmissionRoutesWithBootstrap(
	v1 *gin.RouterGroup,
	foundation *AdmissionFoundation,
	includeBootstrap bool,
) {
	handlers := &identityHandlers{
		service: foundation.service, recovery: foundation.recovery,
		policies: foundation.policies,
	}
	if includeBootstrap {
		v1.GET(
			"/session/bootstrap",
			foundation.requireRateDecision("session-bootstrap", nil),
			foundation.security.bootstrapHandler(),
		)
	}
	v1.GET(
		"/registration-policy-set",
		foundation.security.requireAnonymous(),
		foundation.requireRateDecision("registration-policy-set", nil),
		handlers.currentPolicySet,
	)
	v1.POST(
		"/student-registrations",
		strictJSONMiddleware(func() any { return &studentRegistrationRequest{} }, registrationBodyLimit),
		foundation.security.requireAdmission(),
		foundation.requireRateDecision("student-registrations", registrationIdentifier),
		handlers.registerBoundStudent,
	)
	v1.POST(
		"/email-verification-requests",
		strictJSONMiddleware(func() any { return &verificationRequestBody{} }, verificationRequestBodyLimit),
		foundation.security.requireAdmission(),
		foundation.requireRateDecision("email-verification-requests", verificationRequestIdentifier),
		handlers.requestBoundVerification,
	)
	v1.POST(
		"/email-verifications",
		strictJSONMiddleware(func() any { return &verificationConsumptionBody{} }, verificationConsumptionBodyLimit),
		foundation.security.requireAdmission(),
		foundation.requireRateDecision("email-verifications", verificationTokenIdentifier),
		handlers.consumeBoundVerification,
	)
	v1.POST(
		"/password-reset-requests",
		strictJSONMiddleware(func() any { return &passwordResetRequestBody{} }, passwordResetRequestBodyLimit),
		foundation.security.requireAdmission(),
		foundation.requireRateDecision("password-reset-requests", passwordResetIdentifier),
		handlers.requestBoundPasswordReset,
	)
	v1.POST(
		"/password-resets",
		strictJSONMiddleware(func() any { return &passwordResetCompletionBody{} }, passwordResetCompletionBodyLimit),
		foundation.security.requireAdmission(),
		foundation.requireRateDecision("password-resets", passwordResetTokenIdentifier),
		handlers.completeBoundPasswordReset,
	)
}

type routerOptions struct {
	admission     *AdmissionFoundation
	sessions      *SessionFoundation
	staff         *StaffFoundation
	catalog       *CatalogFoundation
	publicCatalog *PublicCatalogFoundation
	media         *MediaFoundation
	learning      *LearningFoundation
	access        *AccessFoundation
}

// RouterOption adds a validated optional product boundary to the router.
type RouterOption func(*routerOptions) error

// WithStaffFoundation mounts staff lifecycle, invitation, and suspension routes.
func WithStaffFoundation(foundation *StaffFoundation) RouterOption {
	return func(options *routerOptions) error {
		if foundation == nil {
			return fmt.Errorf("staff foundation is required")
		}
		if options.staff != nil {
			return fmt.Errorf("staff lifecycle is already configured")
		}
		options.staff = foundation
		return nil
	}
}

// WithAdmissionFoundation mounts the anonymous bootstrap, policy read, and
// Student admission commands only after their complete fail-closed dependency
// set and ordered middleware chain have been constructed.
func WithAdmissionFoundation(foundation *AdmissionFoundation) RouterOption {
	return func(options *routerOptions) error {
		if foundation == nil {
			return fmt.Errorf("admission foundation is required")
		}
		if options.admission != nil {
			return fmt.Errorf("anonymous admission is already configured")
		}
		options.admission = foundation
		return nil
	}
}

// WithSessionFoundation mounts the anonymous login bootstrap and complete
// server-managed session boundary.
func WithSessionFoundation(foundation *SessionFoundation) RouterOption {
	return func(options *routerOptions) error {
		if foundation == nil {
			return fmt.Errorf("session foundation is required")
		}
		if options.sessions != nil {
			return fmt.Errorf("authenticated sessions are already configured")
		}
		options.sessions = foundation
		return nil
	}
}

func mountSessionRoutes(
	v1 *gin.RouterGroup,
	foundation *SessionFoundation,
	authenticator auth.Authenticator,
	principals identity.PrincipalResolver,
	logger *logging.Logger,
) {
	handlers := &sessionHandlers{
		repository: foundation.repository, authenticator: foundation.authenticator,
		compromised: foundation.compromised,
	}
	v1.GET(
		"/session/bootstrap",
		foundation.requireSessionRateDecision("session-bootstrap", nil),
		foundation.security.bootstrapHandler(),
	)
	v1.POST(
		"/sessions",
		strictJSONMiddleware(func() any { return &sessionLoginRequest{} }, sessionLoginBodyLimit),
		foundation.security.requireAdmission(),
		foundation.requireSessionRateDecision("sessions", loginRateIdentifier),
		handlers.login,
	)
	v1.GET(
		"/session",
		foundation.requireSessionRateDecision("session-resolution", sessionRateIdentifier),
		handlers.resolve,
	)
	v1.POST(
		"/session-renewals",
		foundation.requireSessionMutationSecurity(),
		foundation.requireSessionRateDecision("session-renewals", sessionRateIdentifier),
		handlers.renew,
	)
	v1.DELETE(
		"/session",
		foundation.requireSessionMutationSecurity(),
		foundation.requireSessionRateDecision("session-logout", sessionRateIdentifier),
		handlers.logout,
	)

	// The authenticated password change.
	//
	// PASSWORD_CHANGE is the capability §4.5 grants a restricted principal and
	// SESSION_TERMINATE is the other; every other capability is refused while
	// the credential is CHANGE_REQUIRED. Gating this route on PASSWORD_CHANGE
	// is therefore the ordinary capability decision, not an exemption from it —
	// the same middleware runs, and it is the policy that says yes. Mounting it
	// behind ADMIN_OPERATIONS or any product capability instead would make the
	// restricted state unescapable, which is the defect this route closes.
	//
	// requireAuth still runs first, so an unauthenticated caller is refused
	// before any principal is resolved.
	v1.POST(
		"/password-changes",
		strictJSONMiddleware(func() any { return &passwordChangeRequest{} }, passwordChangeBodyLimit),
		foundation.requireSessionMutationSecurity(),
		foundation.requireSessionRateDecision("password-changes", sessionRateIdentifier),
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapPasswordChange),
		handlers.changePassword,
	)
}

func mountStaffRoutes(
	v1 *gin.RouterGroup,
	foundation *StaffFoundation,
	sessionFoundation *SessionFoundation,
	authenticator auth.Authenticator,
	principals identity.PrincipalResolver,
	logger *logging.Logger,
) {
	staffH := &staffHandlers{
		service:          foundation.service,
		compromised:      foundation.compromised,
		recentAuthWindow: foundation.recentAuthWindow,
	}

	// Paths, methods, and capabilities below are the frozen contract in
	// specs/002-auth-rbac/s1c/spec.md §7. An independent review rejected the
	// earlier mounting because it diverged from that contract on every route;
	// the contract is the authority, so the routes moved rather than the spec.

	// Anonymous. The preview bearer travels in a header rather than a body
	// because §7 specifies GET, and a GET body is not reliably carried. The
	// handler sets no-store so the one-time secret's response is never cached.
	v1.GET(
		"/staff-invitations/preview",
		foundation.requireStaffRateDecision("staff-invitations-preview", staffPreviewBearerIdentifier),
		staffH.previewInvitation,
	)
	v1.POST(
		"/staff-invitation-completions",
		strictJSONMiddleware(func() any { return &completeInvitationRequest{} }, staffCompletionBodyLimit),
		foundation.requireStaffRateDecision("staff-invitations-complete", staffCompletionBearerIdentifier),
		staffH.completeInvitation,
	)

	// Protected read.
	staffReadGroup := v1.Group("/staff-invitations")
	staffReadGroup.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapAdminOperations),
	)
	{
		staffReadGroup.GET("", staffH.listInvitations)
		staffReadGroup.GET("/instructors", staffH.listInstructors)
	}

	// Invitation mutations carry S1B2 session mutation security, requireAuth,
	// and ADMIN_OPERATIONS per §7.
	invitationMutationGroup := v1.Group("/staff-invitations")
	invitationMutationGroup.Use(
		sessionFoundation.requireSessionMutationSecurity(),
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapAdminOperations),
	)
	{
		invitationMutationGroup.POST("",
			strictJSONMiddleware(func() any { return &createInvitationRequest{} }, staffInvitationBodyLimit),
			foundation.requireStaffRateDecision("staff-invitations-create", staffInvitationEmailIdentifier),
			staffH.createInvitation,
		)
		invitationMutationGroup.DELETE("/:id", staffH.revokeInvitation)
	}

	// Suspension is a SECURITY_OPERATIONS action, not an ADMIN_OPERATIONS one.
	// Today every Admin holds both, so this is not an escalation fix — it is the
	// difference between the gate that is documented and the gate that runs, and
	// it stops mattering only until the role set diversifies.
	suspensionGroup := v1.Group("/accounts")
	suspensionGroup.Use(
		sessionFoundation.requireSessionMutationSecurity(),
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapSecurityOperations),
	)
	{
		suspensionGroup.POST("/:id/suspension",
			strictJSONMiddleware(func() any { return &suspendStaffRequest{} }, staffSuspendBodyLimit),
			staffH.suspendStaff,
		)
		suspensionGroup.DELETE("/:id/suspension",
			strictJSONMiddleware(func() any { return &reinstateStaffRequest{} }, staffReinstateBodyLimit),
			staffH.reinstateStaff,
		)
	}
}

// newEngine composes the middleware chain and the fallback handlers, without
// registering any product route. Tests build on it directly so that what they
// exercise is the same chain NewRouter ships.
func newEngine(cfg *config.Config, logger *logging.Logger) (*gin.Engine, error) {
	r := gin.New()

	// Client-IP normalization. Gin's default is to trust every proxy, which
	// lets any client forge its apparent address through a forwarding header.
	// An empty configured list means trust none.
	if err := r.SetTrustedProxies(cfg.TrustedProxies()); err != nil {
		return nil, fmt.Errorf("configuring trusted proxies: %w", err)
	}

	// Required for methodNotAllowedHandler to run at all; without it Gin
	// answers an unsupported method with a bare 404.
	r.HandleMethodNotAllowed = true

	r.Use(requestIDMiddleware())
	r.Use(requestLogger(logger))
	r.Use(recovery(logger))

	// Unmatched routes and unsupported methods run through the same chain, so
	// they are correlated and logged like any other attempt.
	r.NoRoute(notFoundHandler())
	r.NoMethod(methodNotAllowedHandler(r))

	return r, nil
}
