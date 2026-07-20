package httpapi

import (
	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/video"
)

func NewRouter(svc video.Service, authenticator auth.Authenticator, entitlements auth.EntitlementChecker) *gin.Engine {
	r := gin.Default()
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

	return r
}
