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

const (
	publicCatalogCacheControl  = "public, max-age=60"
	maxPublicCatalogQueryBytes = 10 * 1024
	// Filter values are slugs and Subject codes, never prose, so they are held
	// to a far tighter bound than the free-text search query.
	maxPublicCatalogFilterBytes = 200
)

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

	// The smallest read-only academic surface a public filter needs. It is a
	// separate group from the Admin and Student academic endpoints on purpose:
	// those are authenticated and expose retired rows and audit metadata, and
	// none of that may reach an anonymous visitor.
	catalog.GET("/academic-options/institutions", handlers.institutionOptions)
	catalog.GET("/academic-options/institutions/:slug/programs", handlers.programOptions)
	catalog.GET("/academic-options/institutions/:slug/subjects", handlers.subjectOptions)
	catalog.GET("/academic-options/institutions/:slug/levels", handlers.levelOptions)
	return nil
}

func (h *publicCatalogHandlers) institutionOptions(c *gin.Context) {
	options, err := h.repository.ListInstitutionFilters(c.Request.Context())
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": options})
}

func (h *publicCatalogHandlers) programOptions(c *gin.Context) {
	options, err := h.repository.ListProgramFilters(c.Request.Context(), c.Param("slug"))
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": options})
}

func (h *publicCatalogHandlers) levelOptions(c *gin.Context) {
	levels, err := h.repository.ListLevelFilters(
		c.Request.Context(), c.Param("slug"), publicCatalogFilterValue(c, "program"))
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": levels})
}

func (h *publicCatalogHandlers) subjectOptions(c *gin.Context) {
	options, err := h.repository.ListSubjectFilters(
		c.Request.Context(), c.Param("slug"), publicCatalogFilterValue(c, "program"))
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": options})
}

func publicCatalogCache() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", publicCatalogCacheControl)
		appendVary(c, "Accept-Language")
		c.Next()
	}
}

func appendVary(c *gin.Context, value string) {
	values := make([]string, 0)
	for _, header := range c.Writer.Header().Values("Vary") {
		for _, existing := range strings.Split(header, ",") {
			existing = strings.TrimSpace(existing)
			if existing == "" {
				continue
			}
			if strings.EqualFold(existing, value) {
				return
			}
			values = append(values, existing)
		}
	}
	values = append(values, value)
	c.Header("Vary", strings.Join(values, ", "))
}

func (h *publicCatalogHandlers) list(c *gin.Context) {
	page, pageSize := publicCatalogPagination(c)
	query, searching := publicCatalogSearchQuery(c)
	filters := publicCatalogFilters(c)

	// A ranked response is personalised, so the shared 60-second public cache
	// entry set by the group middleware would leak one Student's ordering to
	// every other visitor. Ranking flips the response to private.
	if filters.Ranked() {
		c.Header("Cache-Control", "private, no-store")
	}

	result, err := h.repository.Browse(
		c.Request.Context(), publicCatalogArabic(c), page, pageSize, query, searching, filters)
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}
	c.JSON(http.StatusOK, result)
}

// publicCatalogFilters reads the academic narrowing from the query string.
//
// Every value is treated as opaque text and bound as a parameter. An unknown,
// retired, or malformed value therefore matches nothing and yields an empty
// catalogue — which is the correct answer to a stale shared link, and is why no
// validation error is raised here.
func publicCatalogFilters(c *gin.Context) catalogpublic.Filters {
	return catalogpublic.Filters{
		InstitutionSlug:     publicCatalogFilterValue(c, "institution"),
		ProgramSlug:         publicCatalogFilterValue(c, "program"),
		Level:               publicCatalogFilterValue(c, "level"),
		Subject:             publicCatalogFilterValue(c, "subject"),
		RelevantProgramSlug: publicCatalogFilterValue(c, "relevant_to_program"),
	}
}

// publicCatalogFilterValue bounds one filter value. The cap is the same one the
// search query already uses, so no query parameter can be used to push an
// oversized string into a statement.
func publicCatalogFilterValue(c *gin.Context, name string) string {
	value := strings.TrimSpace(c.Query(name))
	if len(value) > maxPublicCatalogFilterBytes {
		return ""
	}
	return value
}

func publicCatalogSearchQuery(c *gin.Context) (string, bool) {
	query := c.Query("q")
	if len(query) > maxPublicCatalogQueryBytes {
		return "", true
	}
	_, supplied := c.GetQuery("q")
	return query, supplied
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
