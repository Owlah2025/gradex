package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
)

func (h *authoringHandlers) listCourseRoster(c *gin.Context) {
	page, pageSize := courseRosterPagination(c)
	roster, err := h.repo.ListOwnedCourseRoster(
		c.Request.Context(), catalog.CourseRosterRequest{
			CourseID: c.Param("id"), OwnerAccountID: c.GetString(ctxUserIDKey),
			Page: page, PageSize: pageSize, Now: time.Now().UTC(),
		},
	)
	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, roster)
}

func courseRosterPagination(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	return page, pageSize
}
