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
	if routerConfig.admission != nil {
		v1.GET("/session/bootstrap", routerConfig.admission.security.bootstrapHandler())
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

type routerOptions struct {
	admission *AdmissionFoundation
}

// RouterOption adds a validated optional product boundary to the router.
type RouterOption func(*routerOptions) error

// WithAdmissionFoundation mounts only the safe anonymous bootstrap. Student
// commands remain absent until their domain service and ordered middleware
// chain are supplied.
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
