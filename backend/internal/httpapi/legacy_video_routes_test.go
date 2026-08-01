package httpapi

import (
	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/video"
)

// mountLegacyVideoRoutesForTests preserves regression coverage for the
// pre-D7 handler contract without putting the legacy routes in production
// composition. The production router has no caller-visible option that can
// mount this path; this file is compiled only by the Go test build.
func mountLegacyVideoRoutesForTests(
	r *gin.Engine,
	svc video.Service,
	authenticator auth.Authenticator,
	entitlements auth.EntitlementChecker,
	principals identity.PrincipalResolver,
	logger *logging.Logger,
) {
	v1 := r.Group("/api/v1")
	h := &videoHandlers{svc: svc}
	instructor := v1.Group("/lessons/:lessonID/video")
	instructor.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapContentManagement),
		requireInstructor(entitlements),
	)
	instructor.POST("/upload-url", h.requestUpload)
	instructor.POST("/complete", h.completeUpload)
	instructor.POST("/retry", h.retry)
	instructor.POST("/publish", h.publish)

	student := v1.Group("/lessons/:lessonID")
	student.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapLearningAccess),
		requireStudentAccess(entitlements),
	)
	student.GET("/video/playback-url", h.playbackURL)
	student.POST("/progress", h.postProgress)

	v1.GET("/videos/:videoID/manifest/*filepath", h.manifest)
}
