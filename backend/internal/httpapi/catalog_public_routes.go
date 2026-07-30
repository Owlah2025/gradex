package httpapi

import (
	"errors"
	"net/http"

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
	if err := h.repository.List(c.Request.Context()); err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}
	c.Status(http.StatusOK)
}

func (h *publicCatalogHandlers) detail(c *gin.Context) {
	visible, err := h.repository.Detail(c.Request.Context(), c.Param("idOrSlug"))
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}
	if !visible {
		writePublicCatalogNotFound(c)
		return
	}
	c.Status(http.StatusOK)
}

func writePublicCatalogNotFound(c *gin.Context) {
	p := catalogpublic.NotFound()
	c.Writer.Header().Del("X-Request-ID")
	c.Set(ctxSafeErrorCodeKey, p.Code)
	_ = problem.Write(c.Writer, p)
	c.Abort()
}
