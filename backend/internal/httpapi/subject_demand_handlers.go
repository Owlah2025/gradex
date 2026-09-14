package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/academic"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

// Student demand signals for unserved Subjects (D-106 §6).
//
// Every Student handler derives the account from the authenticated session and
// none accepts an account identifier from the client, so no shape of request
// reads or writes another Student's signals. The Admin handler returns counts
// only, never a roster: prioritising production needs to know what to build,
// not who asked.
//
// Nothing here participates in an access decision. A demand signal grants no
// entitlement and its absence withholds none.

type subjectDemandHandlers struct{ repo *academic.Repository }

func writeSubjectDemandError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, academic.ErrSubjectDemandAlreadyRaised):
		// A conflict, not a validation failure: the request was well formed and
		// the Student's intent is already on record.
		writeProblem(c, problem.StateConflict())
	case errors.Is(err, academic.ErrSubjectDemandNotFound),
		errors.Is(err, academic.ErrNotFound):
		writeProblem(c, problem.NotFound())
	case errors.Is(err, academic.ErrSubjectDemandNoteTooLong):
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "NOTE_TOO_LONG", Location: problem.LocationBody,
			Detail: "a note may not exceed 500 characters",
		}))
	default:
		writeProblem(c, problem.Internal(""))
	}
}

type raiseSubjectDemandBody struct {
	SubjectID string `json:"subject_id"`
	Note      string `json:"note"`
}

func (h *subjectDemandHandlers) raise(c *gin.Context) {
	var body raiseSubjectDemandBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.ValidationFailed())
		return
	}
	if body.SubjectID == "" {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "SUBJECT_REQUIRED", Location: problem.LocationBody,
			Detail: "a subject identifier is required",
		}))
		return
	}
	signal, err := h.repo.RaiseSubjectDemand(
		c.Request.Context(), c.GetString(ctxUserIDKey), body.SubjectID, body.Note)
	if err != nil {
		writeSubjectDemandError(c, err)
		return
	}
	c.JSON(http.StatusCreated, signal)
}

func (h *subjectDemandHandlers) withdraw(c *gin.Context) {
	err := h.repo.WithdrawSubjectDemand(
		c.Request.Context(), c.GetString(ctxUserIDKey), c.Param("subjectId"))
	if err != nil {
		writeSubjectDemandError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *subjectDemandHandlers) listOwn(c *gin.Context) {
	signals, err := h.repo.ListOwnSubjectDemand(c.Request.Context(), c.GetString(ctxUserIDKey))
	if err != nil {
		writeSubjectDemandError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": signals})
}

func (h *subjectDemandHandlers) listCounts(c *gin.Context) {
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
				Code: "LIMIT_INVALID", Location: problem.LocationQuery,
				Detail: "limit must be a positive integer",
			}))
			return
		}
		limit = parsed
	}
	counts, err := h.repo.ListSubjectDemandCounts(c.Request.Context(), c.Query("institution"), limit)
	if err != nil {
		writeSubjectDemandError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": counts})
}
