package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/video"
)

// These handlers are retained only to exercise the pre-D7 compatibility
// contract in tests. Production composition has no legacy video route or
// concrete service implementation.
type videoHandlers struct{ svc video.Service }

func problemForError(err error) problem.Problem {
	switch {
	case errors.Is(err, video.ErrNotFound):
		return problem.NotFound()
	case errors.Is(err, video.ErrValidation):
		return problem.ValidationFailed()
	case errors.Is(err, video.ErrUnavailable):
		return problem.DependencyUnavailable()
	case errors.Is(err, video.ErrConcurrentModification):
		return problem.StateConflict()
	case errors.Is(err, video.ErrConflict):
		return problem.UnsupportedStateTransition()
	default:
		return problem.Internal("")
	}
}

func fail(c *gin.Context, err error) { writeProblem(c, problemForError(err)) }

func (h *videoHandlers) requestUpload(c *gin.Context) {
	var request struct {
		Filename    string `json:"filename" binding:"required"`
		ContentType string `json:"content_type" binding:"required"`
	}
	if !bindJSON(c, &request) {
		return
	}
	ticket, err := h.svc.RequestUpload(c.Request.Context(), c.Param("lessonID"), request.Filename, request.ContentType)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"upload_url": ticket.UploadURL, "raw_key": ticket.RawKey, "expires_at": ticket.ExpiresAt})
}

func (h *videoHandlers) completeUpload(c *gin.Context) {
	if err := h.svc.CompleteUpload(c.Request.Context(), c.Param("lessonID")); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "QUEUED"})
}

func (h *videoHandlers) retry(c *gin.Context) {
	if err := h.svc.Retranscode(c.Request.Context(), c.Param("lessonID")); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "QUEUED"})
}

func (h *videoHandlers) publish(c *gin.Context) {
	if err := h.svc.Publish(c.Request.Context(), c.Param("lessonID")); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "PUBLISHED"})
}

func (h *videoHandlers) playbackURL(c *gin.Context) {
	signed, err := h.svc.GetPlaybackURL(c.Request.Context(), c.Param("lessonID"), c.GetString(ctxUserIDKey))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"playback_url": signed.URL, "expires_at": signed.ExpiresAt, "last_position_seconds": signed.LastPositionSeconds})
}

func (h *videoHandlers) manifest(c *gin.Context) {
	content, contentType, err := h.svc.ServeManifest(c.Request.Context(), c.Param("videoID"), strings.TrimPrefix(c.Param("filepath"), "/"), c.Query("token"))
	if err != nil {
		fail(c, err)
		return
	}
	c.Data(http.StatusOK, contentType, content)
}

func (h *videoHandlers) postProgress(c *gin.Context) {
	var request struct {
		PositionSeconds *float64 `json:"position_seconds" binding:"required"`
	}
	if !bindJSON(c, &request) {
		return
	}
	progress, err := h.svc.UpdateProgress(c.Request.Context(), c.Param("lessonID"), c.GetString(ctxUserIDKey), *request.PositionSeconds)
	if err != nil {
		if errors.Is(err, video.ErrValidation) {
			writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
				Code: "INVALID_VALUE", Detail: "Position must be zero or greater.", Location: problem.LocationBody, Pointer: "#/position_seconds",
			}))
			return
		}
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"max_position_seconds": progress.MaxPositionSeconds, "completed": progress.Completed})
}
