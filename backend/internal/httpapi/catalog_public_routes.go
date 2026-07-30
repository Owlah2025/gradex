package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

const publicCatalogCacheControl = "public, max-age=60"

type publicCatalogHandlers struct {
	repository *catalogpublic.Repository
}

func mountPublicCatalogRoutes(v1 *gin.RouterGroup, foundation *PublicCatalogFoundation) error {
	if foundation == nil || foundation.repository == nil {
		return errors.New("complete public catalogue foundation is required")
	}

	handlers := &publicCatalogHandlers{repository: foundation.repository}
	catalog := v1.Group("/catalog")
	catalog.Use(publicCatalogCache())
	catalog.GET("/courses", handlers.list)
	catalog.GET("/courses/:idOrSlug", handlers.detail)
	return nil
}

func publicCatalogCache() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", publicCatalogCacheControl)
		c.Next()
	}
}

func (h *publicCatalogHandlers) list(c *gin.Context) {
	page, pageSize := publicCatalogPagination(c)
	result, err := h.repository.List(c.Request.Context(), publicCatalogArabic(c), page, pageSize)
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *publicCatalogHandlers) detail(c *gin.Context) {
	course, err := h.repository.Detail(c.Request.Context(), c.Param("idOrSlug"), publicCatalogArabic(c))
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}
	if course == nil {
		writeAnonymousProblem(c, catalogpublic.NotFound())
		return
	}
	c.JSON(http.StatusOK, course)
}

func publicCatalogPagination(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func publicCatalogArabic(c *gin.Context) bool {
	return !strings.HasPrefix(strings.ToLower(c.GetHeader("Accept-Language")), "en")
}
