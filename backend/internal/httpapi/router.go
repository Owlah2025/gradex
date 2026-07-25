package httpapi

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/config"
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
	svc video.Service,
	authenticator auth.Authenticator,
	entitlements auth.EntitlementChecker,
) (*gin.Engine, error) {
	r, err := newEngine(cfg, logger)
	if err != nil {
		return nil, err
	}

	h := &videoHandlers{svc: svc}

	v1 := r.Group("/api/v1")

	instructor := v1.Group("/lessons/:lessonID/video")
	instructor.Use(requireAuth(authenticator), requireInstructor(entitlements))
	{
		instructor.POST("/upload-url", h.requestUpload)
		instructor.POST("/complete", h.completeUpload)
		instructor.POST("/retry", h.retry)
		instructor.POST("/publish", h.publish)
	}

	student := v1.Group("/lessons/:lessonID")
	student.Use(requireAuth(authenticator), requireStudentAccess(entitlements))
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
