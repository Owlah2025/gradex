package httpapi

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/entitlement"
)

type learningAnnouncementResponse struct {
	Items    []learningAnnouncementItem `json:"items"`
	Page     int                        `json:"page"`
	PageSize int                        `json:"page_size"`
	HasMore  bool                       `json:"has_more"`
}

type learningAnnouncementItem struct {
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
	page, err := h.foundation.announcementReader.ListPublishedCourseAnnouncements(c.Request.Context(), courseID, announcementPage(c))
	if err != nil {
		h.logDenial(c, entitlement.ReasonDependency)
		writeProtectedUnavailable(c)
		return
	}
	response := make([]learningAnnouncementItem, 0, len(page.Items))
	for _, item := range page.Items {
		response = append(response, learningAnnouncementItem{
			ID: item.ID, Title: item.Title, Body: item.Body,
			CreatedAt:   item.CreatedAt.UTC().Format(time.RFC3339),
			PublishedAt: item.PublishedAt.UTC().Format(time.RFC3339),
		})
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, learningAnnouncementResponse{
		Items: response, Page: page.Page, PageSize: page.PageSize, HasMore: page.HasMore,
	})
}
