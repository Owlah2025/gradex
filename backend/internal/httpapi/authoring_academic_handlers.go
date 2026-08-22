package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/academic"
)

// Instructor academic reads for Subject-first authoring (D-091 §9, T4-B).
//
// These are the entire academic authority an Instructor holds. They are behind
// CapContentManagement, never CapAcademicCatalog, so an Instructor can find a
// canonical Subject and can never create, amend, retire, or map one. That is the
// same shape T3 used to give Students an option projection over the same catalog
// without granting them Admin capability.
//
// Everything here is read-only by construction: the group mounts no mutation
// route at all, so there is no handler to reach even with a forged method.
type authoringAcademicHandlers struct{ repo *academic.Repository }

// listInstitutions returns the universities an Instructor may author against.
//
// Retired institutions are excluded by the projection. Nothing is hardcoded:
// Kuwait University is the only launch Institution, but it is data, and a second
// institution appears here the moment the catalog holds one.
func (h *authoringAcademicHandlers) listInstitutions(c *gin.Context) {
	options, err := h.repo.ListInstitutionOptions(c.Request.Context())
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, options)
}

// searchSubjects finds active canonical Subjects within one Institution by
// official code, normalized code, or either title.
func (h *authoringAcademicHandlers) searchSubjects(c *gin.Context) {
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	options, err := h.repo.SearchAuthoringSubjects(c.Request.Context(), academic.SearchAuthoringSubjectsRequest{
		InstitutionID: c.Param("institutionId"),
		Query:         c.Query("q"),
		Limit:         limit,
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, options)
}

// getSubject resolves one Subject in the same shape as the search, so an
// authoring surface can render a Course's stored Subject — including a retired
// historical one on a published Course — without the Instructor searching again.
func (h *authoringAcademicHandlers) getSubject(c *gin.Context) {
	option, err := h.repo.GetAuthoringSubject(c.Request.Context(),
		c.Param("institutionId"), c.Param("subjectId"))
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, option)
}
