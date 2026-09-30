package httpapi

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/entitlement"
)

type learningAnnouncementResponse struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	CreatedAt   string `json:"created_at"`
	PublishedAt string `json:"published_at"`
}

func (h *learningHandlers) courseAnnouncements(c *gin.Context) {
	studentID := c.GetString(ctxUserIDKey)
	courseID := c.Param("courseId")
	decisions, err := h.foundation.readEvaluator.EvaluateCourseReads(c.Request.Context(), studentID, h.now().UTC())
	if err != nil {
		h.logDenial(c, entitlement.ReasonDependency)
		writeProtectedUnavailable(c)
		return
	}
	decision, ok := decisions[courseID]
	if !ok || !decision.CourseWide || decision.State != entitlement.ReadActive {
		h.logDenial(c, entitlement.ReasonNoApplicableGrant)
		writeProtectedUnavailable(c)
		return
	}
	items, err := h.foundation.announcementReader.ListPublishedCourseAnnouncements(c.Request.Context(), courseID)
	if err != nil {
		h.logDenial(c, entitlement.ReasonDependency)
		writeProtectedUnavailable(c)
		return
	}
	response := make([]learningAnnouncementResponse, 0, len(items))
	for _, item := range items {
		response = append(response, learningAnnouncementResponse{
			ID: item.ID, Title: item.Title, Body: item.Body,
			CreatedAt:   item.CreatedAt.UTC().Format(time.RFC3339),
			PublishedAt: item.PublishedAt.UTC().Format(time.RFC3339),
		})
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, response)
}
