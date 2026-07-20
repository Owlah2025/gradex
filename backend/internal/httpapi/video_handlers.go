package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/video"
)

type videoHandlers struct {
	svc video.Service
}

// statusForError maps the video package's sentinel errors to the HTTP error
// contract from docs/superpowers/specs/2026-07-17-video-streaming-design.md §5:
// 403 forbidden, 404 not found, 409 conflict, 500 unexpected.
func statusForError(err error) int {
	switch {
	case errors.Is(err, video.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, video.ErrConflict):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func (h *videoHandlers) requestUpload(c *gin.Context) {
	lessonID := c.Param("lessonID")
	var req struct {
		Filename    string `json:"filename" binding:"required"`
		ContentType string `json:"content_type" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errorResponse(c, http.StatusBadRequest, err)
		return
	}

	ticket, err := h.svc.RequestUpload(c.Request.Context(), lessonID, req.Filename, req.ContentType)
	if err != nil {
		errorResponse(c, statusForError(err), err)
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
		errorResponse(c, statusForError(err), err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "QUEUED"})
}

func (h *videoHandlers) retry(c *gin.Context) {
	lessonID := c.Param("lessonID")
	if err := h.svc.Retranscode(c.Request.Context(), lessonID); err != nil {
		errorResponse(c, statusForError(err), err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "QUEUED"})
}

func (h *videoHandlers) publish(c *gin.Context) {
	lessonID := c.Param("lessonID")
	if err := h.svc.Publish(c.Request.Context(), lessonID); err != nil {
		errorResponse(c, statusForError(err), err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "PUBLISHED"})
}

func (h *videoHandlers) playbackURL(c *gin.Context) {
	lessonID := c.Param("lessonID")
	viewerID := c.GetString(ctxUserIDKey)

	signed, err := h.svc.GetPlaybackURL(c.Request.Context(), lessonID, viewerID)
	if err != nil {
		errorResponse(c, statusForError(err), err)
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
		errorResponse(c, statusForError(err), err)
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
	if err := c.ShouldBindJSON(&req); err != nil {
		errorResponse(c, http.StatusBadRequest, err)
		return
	}

	progress, err := h.svc.UpdateProgress(c.Request.Context(), lessonID, viewerID, *req.PositionSeconds)
	if err != nil {
		errorResponse(c, statusForError(err), err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"max_position_seconds": progress.MaxPositionSeconds,
		"completed":            progress.Completed,
	})
}
