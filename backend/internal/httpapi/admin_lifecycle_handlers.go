package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

type adminLifecycleHandlers struct{ repo *catalog.Repository }

type reassignOwnerBody struct {
	OwnerAccountID string `json:"owner_account_id"`
}
type suspensionBody struct {
	Cause  catalog.AccessSuspensionCause `json:"cause"`
	Reason string                        `json:"reason"`
}
type restoreAccessBody struct {
	Reason string `json:"reason"`
}

func (h *adminLifecycleHandlers) handleLifecycleError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, catalog.ErrCourseNotFound):
		writeProblem(c, problem.NotFound())
	case errors.Is(err, catalog.ErrCourseHasAccess), errors.Is(err, catalog.ErrPendingCandidate), errors.Is(err, catalog.ErrCourseAccessAlreadySuspended), errors.Is(err, catalog.ErrCourseAccessNotSuspended):
		writeProblem(c, problem.StateConflict())
	case errors.Is(err, catalog.ErrInvalidLifecycle):
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{Code: "INVALID_LIFECYCLE", Detail: "The requested lifecycle transition is not allowed", Location: problem.LocationBody}))
	case errors.Is(err, catalog.ErrOwnerIneligible), errors.Is(err, catalog.ErrAccountSuspended):
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{Code: "OWNER_INELIGIBLE", Detail: "New owner must be an active Instructor", Location: problem.LocationBody, Parameter: "owner_account_id"}))
	case errors.Is(err, catalog.ErrReasonRequired):
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{Code: "REASON_REQUIRED", Detail: "Reason is required", Location: problem.LocationBody, Parameter: "reason"}))
	default:
		var conflict *catalog.LifecycleConflictError
		if errors.As(err, &conflict) {
			writeProblem(c, problem.StateConflict())
			return
		}
		writeProblem(c, problem.Internal(""))
	}
}

func (h *adminLifecycleHandlers) transition(target catalog.CourseLifecycle) gin.HandlerFunc {
	return func(c *gin.Context) {
		course, err := h.repo.TransitionCourseLifecycle(c.Request.Context(), catalog.LifecycleMutation{CourseID: c.Param("id"), AdminAccountID: c.GetString(ctxUserIDKey), ActorDescriptor: c.GetString(ctxUserIDKey), Target: target})
		if err != nil {
			h.handleLifecycleError(c, err)
			return
		}
		c.JSON(http.StatusOK, course)
	}
}

func (h *adminLifecycleHandlers) retire(c *gin.Context) {
	course, err := h.repo.RetireCourse(c.Request.Context(), catalog.LifecycleMutation{CourseID: c.Param("id"), AdminAccountID: c.GetString(ctxUserIDKey), ActorDescriptor: c.GetString(ctxUserIDKey)})
	if err != nil {
		h.handleLifecycleError(c, err)
		return
	}
	c.JSON(http.StatusOK, course)
}

func (h *adminLifecycleHandlers) delete(c *gin.Context) {
	err := h.repo.DeleteCourse(c.Request.Context(), catalog.LifecycleMutation{CourseID: c.Param("id"), AdminAccountID: c.GetString(ctxUserIDKey), ActorDescriptor: c.GetString(ctxUserIDKey)})
	if err != nil {
		h.handleLifecycleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *adminLifecycleHandlers) reassignOwner(c *gin.Context) {
	var body reassignOwnerBody
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.OwnerAccountID) == "" {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{Code: "OWNER_REQUIRED", Detail: "New owner account ID is required", Location: problem.LocationBody, Parameter: "owner_account_id"}))
		return
	}
	course, err := h.repo.ReassignCourseOwner(c.Request.Context(), catalog.ReassignCourseOwnerRequest{CourseID: c.Param("id"), NewOwnerID: body.OwnerAccountID, AdminAccountID: c.GetString(ctxUserIDKey), ActorDescriptor: c.GetString(ctxUserIDKey)})
	if err != nil {
		h.handleLifecycleError(c, err)
		return
	}
	c.JSON(http.StatusOK, course)
}

func (h *adminLifecycleHandlers) suspend(c *gin.Context) {
	var body suspensionBody
	if err := c.ShouldBindJSON(&body); err != nil || !body.Cause.Valid() || strings.TrimSpace(body.Reason) == "" {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{Code: "SUSPENSION_CAUSE_AND_REASON_REQUIRED", Detail: "A valid cause and non-empty reason are required", Location: problem.LocationBody}))
		return
	}
	course, err := h.repo.SuspendCourseAccess(c.Request.Context(), catalog.SuspendCourseAccessRequest{CourseID: c.Param("id"), AdminAccountID: c.GetString(ctxUserIDKey), ActorDescriptor: c.GetString(ctxUserIDKey), Cause: body.Cause, Reason: body.Reason})
	if err != nil {
		h.handleLifecycleError(c, err)
		return
	}
	c.JSON(http.StatusOK, course)
}

func (h *adminLifecycleHandlers) restoreAccess(c *gin.Context) {
	var body restoreAccessBody
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Reason) == "" {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{Code: "REASON_REQUIRED", Detail: "Reason is required", Location: problem.LocationBody, Parameter: "reason"}))
		return
	}
	course, err := h.repo.RestoreCourseAccess(c.Request.Context(), catalog.RestoreCourseAccessRequest{CourseID: c.Param("id"), AdminAccountID: c.GetString(ctxUserIDKey), ActorDescriptor: c.GetString(ctxUserIDKey), Reason: body.Reason})
	if err != nil {
		h.handleLifecycleError(c, err)
		return
	}
	c.JSON(http.StatusOK, course)
}

// directory is the Admin lifecycle read surface. Without it the lifecycle commands are
// unreachable through the product for every state the public catalogue hides — a delisted Course
// cannot be relisted from a catalogue that, correctly, no longer lists it.
func (h *adminLifecycleHandlers) directory(c *gin.Context) {
	summaries, err := h.repo.ListCourseLifecycleDirectory(c.Request.Context(), c.Query("q"))
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": summaries})
}
