package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/video"
)

type videoHandlers struct {
	svc video.Service
}

// problemForError maps the video package's error classes onto the public
// problem contract.
//
// The internal error is used only for its class. Its message is never read,
// formatted, or attached: video errors wrap object keys, storage paths, queue
// names, lifecycle status values, and raw provider text, none of which §2.3
// permits in a response.
//
// An unrecognised error is an unexpected fault and becomes the generic 500,
// so a newly introduced internal error fails closed rather than leaking.
func problemForError(err error) problem.Problem {
	switch {
	case errors.Is(err, video.ErrNotFound):
		return problem.NotFound()
	case errors.Is(err, video.ErrValidation):
		return problem.ValidationFailed()
	case errors.Is(err, video.ErrUnavailable):
		return problem.DependencyUnavailable()
	case errors.Is(err, video.ErrConcurrentModification):
		// Lost race: the same request may succeed on retry.
		return problem.StateConflict()
	case errors.Is(err, video.ErrConflict):
		// A command that is legal in principle but not from the resource's
		// current state. The state itself stays internal.
		return problem.UnsupportedStateTransition()
	default:
		return problem.Internal("")
	}
}

// fail writes the mapped problem for an internal error.
func fail(c *gin.Context, err error) {
	writeProblem(c, problemForError(err))
}

func (h *videoHandlers) requestUpload(c *gin.Context) {
	lessonID := c.Param("lessonID")
	var req struct {
		Filename    string `json:"filename" binding:"required"`
		ContentType string `json:"content_type" binding:"required"`
	}
	if !bindJSON(c, &req) {
		return
	}

	ticket, err := h.svc.RequestUpload(c.Request.Context(), lessonID, req.Filename, req.ContentType)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"upload_url": ticket.UploadURL,
		"raw_key":    ticket.RawKey,
		"expires_at": ticket.ExpiresAt,
	})
}

func (h *videoHandlers) completeUpload(c *gin.Context) {
	lessonID := c.Param("lessonID")
	if err := h.svc.CompleteUpload(c.Request.Context(), lessonID); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "QUEUED"})
}

func (h *videoHandlers) retry(c *gin.Context) {
	lessonID := c.Param("lessonID")
	if err := h.svc.Retranscode(c.Request.Context(), lessonID); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "QUEUED"})
}

func (h *videoHandlers) publish(c *gin.Context) {
	lessonID := c.Param("lessonID")
	if err := h.svc.Publish(c.Request.Context(), lessonID); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "PUBLISHED"})
}

func (h *videoHandlers) playbackURL(c *gin.Context) {
	lessonID := c.Param("lessonID")
	viewerID := c.GetString(ctxUserIDKey)

	signed, err := h.svc.GetPlaybackURL(c.Request.Context(), lessonID, viewerID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"playback_url":          signed.URL,
		"expires_at":            signed.ExpiresAt,
		"last_position_seconds": signed.LastPositionSeconds,
	})
}

func (h *videoHandlers) manifest(c *gin.Context) {
	videoID := c.Param("videoID")
	path := strings.TrimPrefix(c.Param("filepath"), "/")
	token := c.Query("token")

	content, contentType, err := h.svc.ServeManifest(c.Request.Context(), videoID, path, token)
	if err != nil {
		fail(c, err)
		return
	}
	c.Data(http.StatusOK, contentType, content)
}

func (h *videoHandlers) postProgress(c *gin.Context) {
	lessonID := c.Param("lessonID")
	viewerID := c.GetString(ctxUserIDKey)

	var req struct {
		PositionSeconds *float64 `json:"position_seconds" binding:"required"`
	}
	if !bindJSON(c, &req) {
		return
	}

	progress, err := h.svc.UpdateProgress(c.Request.Context(), lessonID, viewerID, *req.PositionSeconds)
	if err != nil {
		// The one place the service reports a field-level failure, so the
		// violation can name the field the client actually sent.
		if errors.Is(err, video.ErrValidation) {
			writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
				Code:     "INVALID_VALUE",
				Detail:   "Position must be zero or greater.",
				Location: problem.LocationBody,
				Pointer:  "#/position_seconds",
			}))
			return
		}
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"max_position_seconds": progress.MaxPositionSeconds,
		"completed":            progress.Completed,
	})
}
