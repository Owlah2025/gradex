package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

type adminTaxonomyHandlers struct{ repo *catalog.Repository }

type taxonomyAssignmentBody struct {
	RevisionID    string `json:"revision_id"`
	MajorTermID   string `json:"major_term_id"`
	SubjectTermID string `json:"subject_term_id"`
}

type createTaxonomyTermBody struct {
	Kind         catalog.TaxonomyKind `json:"kind"`
	LabelAr      string               `json:"label_ar"`
	LabelEn      string               `json:"label_en"`
	AcademicCode *string              `json:"academic_code"`
}

type renameTaxonomyTermBody struct {
	LabelAr string `json:"label_ar"`
	LabelEn string `json:"label_en"`
}

func (h *adminTaxonomyHandlers) createTerm(c *gin.Context) {
	var body createTaxonomyTermBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}
	term, err := h.repo.CreateTaxonomyTerm(c.Request.Context(), catalog.CreateTaxonomyTermRequest{
		AdminAccountID: c.GetString(ctxUserIDKey), ActorDescriptor: c.GetString(ctxUserIDKey),
		Kind: body.Kind, LabelAr: body.LabelAr, LabelEn: body.LabelEn, AcademicCode: body.AcademicCode,
	})
	if err != nil {
		handleTaxonomyTermError(c, err)
		return
	}
	c.JSON(http.StatusCreated, term)
}

func (h *adminTaxonomyHandlers) renameTerm(c *gin.Context) {
	var body renameTaxonomyTermBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}
	term, err := h.repo.RenameTaxonomyTerm(c.Request.Context(), catalog.RenameTaxonomyTermRequest{
		TermID: c.Param("id"), AdminAccountID: c.GetString(ctxUserIDKey), ActorDescriptor: c.GetString(ctxUserIDKey),
		LabelAr: body.LabelAr, LabelEn: body.LabelEn,
	})
	if err != nil {
		handleTaxonomyTermError(c, err)
		return
	}
	c.JSON(http.StatusOK, term)
}

func (h *adminTaxonomyHandlers) retireTerm(c *gin.Context) {
	term, err := h.repo.RetireTaxonomyTerm(c.Request.Context(), catalog.RetireTaxonomyTermRequest{
		TermID: c.Param("id"), AdminAccountID: c.GetString(ctxUserIDKey), ActorDescriptor: c.GetString(ctxUserIDKey),
	})
	if err != nil {
		handleTaxonomyTermError(c, err)
		return
	}
	c.JSON(http.StatusOK, term)
}

func (h *adminTaxonomyHandlers) deleteTerm(c *gin.Context) {
	err := h.repo.DeleteTaxonomyTerm(c.Request.Context(), catalog.DeleteTaxonomyTermRequest{
		TermID: c.Param("id"), AdminAccountID: c.GetString(ctxUserIDKey), ActorDescriptor: c.GetString(ctxUserIDKey),
	})
	if err != nil {
		handleTaxonomyTermError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *adminTaxonomyHandlers) assignTaxonomy(c *gin.Context) {
	var body taxonomyAssignmentBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}
	if strings.TrimSpace(body.RevisionID) == "" || strings.TrimSpace(body.MajorTermID) == "" || strings.TrimSpace(body.SubjectTermID) == "" {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "REVISION_AND_TERMS_REQUIRED", Detail: "revision_id, major_term_id, and subject_term_id are required", Location: problem.LocationBody,
		}))
		return
	}

	revision, err := h.repo.AssignTaxonomyToRevision(c.Request.Context(), catalog.AssignTaxonomyRequest{
		CourseID: c.Param("id"), RevisionID: body.RevisionID, AdminAccountID: c.GetString(ctxUserIDKey),
		ActorDescriptor: c.GetString(ctxUserIDKey), MajorTermID: body.MajorTermID, SubjectTermID: body.SubjectTermID,
	})
	if err != nil {
		handleTaxonomyAssignmentError(c, err)
		return
	}
	c.JSON(http.StatusOK, revision)
}

func handleTaxonomyAssignmentError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, catalog.ErrCourseNotFound):
		writeProblem(c, problem.NotFound())
	case errors.Is(err, catalog.ErrTaxonomyRevisionInvalid):
		writeProblem(c, problem.StateConflict())
	case errors.Is(err, catalog.ErrInvalidTaxonomyTerm), errors.Is(err, catalog.ErrTaxonomyTermUnavailable), errors.Is(err, catalog.ErrTaxonomyTermKindMismatch):
		writeProblem(c, problem.ValidationFailed())
	default:
		writeProblem(c, problem.Internal(""))
	}
}

func handleTaxonomyTermError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, catalog.ErrTaxonomyTermNotFound):
		writeProblem(c, problem.NotFound())
	case errors.Is(err, catalog.ErrTaxonomyTermReferenced), errors.Is(err, catalog.ErrTaxonomyTermRetired):
		writeProblem(c, problem.StateConflict())
	case errors.Is(err, catalog.ErrInvalidTaxonomyTerm):
		writeProblem(c, problem.ValidationFailed())
	default:
		writeProblem(c, problem.Internal(""))
	}
}
