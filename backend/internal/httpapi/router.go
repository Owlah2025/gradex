package httpapi

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/video"
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
	svc video.Service,
	authenticator auth.Authenticator,
	entitlements auth.EntitlementChecker,
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

	h := &videoHandlers{svc: svc}

	v1 := r.Group("/api/v1")
	if routerConfig.sessions != nil {
		mountSessionRoutes(v1, routerConfig.sessions)
	}
	if routerConfig.admission != nil {
		mountAdmissionRoutesWithBootstrap(
			v1, routerConfig.admission, routerConfig.sessions == nil,
		)
	}

	// Every protected group runs authentication → capability policy → ownership
	// or Entitlement, in that order. The capability step is what refuses a
	// restricted or suspended principal before any route-specific check runs,
	// so a route cannot be protected by ownership alone and accidentally admit
	// an Account that should not be acting at all.
	instructor := v1.Group("/lessons/:lessonID/video")
	instructor.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapContentManagement),
		requireInstructor(entitlements),
	)
	{
		instructor.POST("/upload-url", h.requestUpload)
		instructor.POST("/complete", h.completeUpload)
		instructor.POST("/retry", h.retry)
		instructor.POST("/publish", h.publish)
	}

	student := v1.Group("/lessons/:lessonID")
	student.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapLearningAccess),
		requireStudentAccess(entitlements),
	)
	{
		student.GET("/video/playback-url", h.playbackURL)
		student.POST("/progress", h.postProgress)
	}

	// Public (token-authorized, not header-authorized): the HLS player fetches
	// these directly and won't carry a custom auth header. The manifest token
	// embedded in the URL by GetPlaybackURL is the actual authorization — see
	// internal/video/token.go and playback.go's ServeManifest.
	v1.GET("/videos/:videoID/manifest/*filepath", h.manifest)

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
	admission *AdmissionFoundation
	sessions  *SessionFoundation
}

// RouterOption adds a validated optional product boundary to the router.
type RouterOption func(*routerOptions) error

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

func mountSessionRoutes(v1 *gin.RouterGroup, foundation *SessionFoundation) {
	handlers := &sessionHandlers{
		repository: foundation.repository, authenticator: foundation.authenticator,
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
