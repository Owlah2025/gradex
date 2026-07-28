package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

type reviewHandlers struct {
	repo           *catalog.Repository
	assetValidator catalog.AssetVersionValidator
	logger         *logging.Logger
}

type requestChangesBody struct {
	Reason string `json:"reason"`
}

func (h *reviewHandlers) handleReviewError(c *gin.Context, err error) {
	if errors.Is(err, catalog.ErrCourseNotFound) {
		writeProblem(c, problem.NotFound())
		return
	}
	var valErr *catalog.SubmissionValidationError
	if errors.As(err, &valErr) {
		var violations []problem.SubmissionViolation
		for _, v := range valErr.Violations {
			violations = append(violations, problem.SubmissionViolation{
				Code:      v.Code,
				Target:    v.Target,
				Dimension: v.Dimension,
			})
		}
		writeProblem(c, problem.SubmissionIncomplete(violations...))
		return
	}
	if errors.Is(err, catalog.ErrReasonRequired) {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code:      "REASON_REQUIRED",
			Detail:    "A reason is required when requesting changes",
			Location:  problem.LocationBody,
			Parameter: "reason",
		}))
		return
	}
	if errors.Is(err, catalog.ErrAssetVersionInvalid) || errors.Is(err, catalog.ErrAssetVersionNotReady) || errors.Is(err, catalog.ErrAccountSuspended) {
		writeProblem(c, problem.ValidationFailed())
		return
	}
	var conflict *catalog.LifecycleConflictError
	if errors.As(err, &conflict) {
		writeProblem(c, problem.StateConflict())
		return
	}
	writeProblem(c, problem.Internal(""))
}

func (h *reviewHandlers) listQueue(c *gin.Context) {
	queue, err := h.repo.ListReviewQueue(c.Request.Context())
	if err != nil {
		h.handleReviewError(c, err)
		return
	}
	c.JSON(http.StatusOK, queue)
}

func (h *reviewHandlers) getCourseRevisionGraph(c *gin.Context) {
	courseID := c.Param("id")
	revisionID := c.Param("revisionId")
	course, err := h.repo.GetCourseRevisionGraph(c.Request.Context(), courseID, revisionID)
	if err != nil {
		h.handleReviewError(c, err)
		return
	}
	c.JSON(http.StatusOK, course)
}

func (h *reviewHandlers) approveCourse(c *gin.Context) {
	adminAccountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")
	revisionID := c.Param("revisionId")
	actorDescriptor := adminAccountID

	course, err := h.repo.ApproveCourse(c.Request.Context(), h.assetValidator, catalog.ApproveCourseRequest{
		CourseID: courseID, RevisionID: revisionID,
		AdminAccountID: adminAccountID, ActorDescriptor: actorDescriptor,
	})
	if err != nil {
		h.handleReviewError(c, err)
		return
	}
	c.JSON(http.StatusOK, course)
}

func (h *reviewHandlers) requestChanges(c *gin.Context) {
	adminAccountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")
	revisionID := c.Param("revisionId")
	actorDescriptor := adminAccountID

	var body requestChangesBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}
	if strings.TrimSpace(body.Reason) == "" {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code:      "REASON_REQUIRED",
			Detail:    "Reason is required",
			Location:  problem.LocationBody,
			Parameter: "reason",
		}))
		return
	}

	course, err := h.repo.RequestChanges(c.Request.Context(), catalog.RequestChangesRequest{
		CourseID: courseID, RevisionID: revisionID, AdminAccountID: adminAccountID,
		Reason: body.Reason, ActorDescriptor: actorDescriptor,
	})
	if err != nil {
		h.handleReviewError(c, err)
		return
	}
	c.JSON(http.StatusOK, course)
}

func (h *reviewHandlers) previewLesson(c *gin.Context) {
	adminAccountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")
	revisionID := c.Param("revisionId")
	lessonID := c.Param("lessonId")
	actorDescriptor := adminAccountID

	videoAssetVersionID, err := h.repo.PreviewAdminLesson(c.Request.Context(), catalog.AdminPreviewRequest{
		CourseID: courseID, RevisionID: revisionID, LessonID: lessonID,
		AdminAccountID: adminAccountID, ActorDescriptor: actorDescriptor,
	})
	if err != nil {
		h.handleReviewError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"course_id":              courseID,
		"revision_id":            revisionID,
		"lesson_id":              lessonID,
		"video_asset_version_id": videoAssetVersionID,
		"playback_url":           "/api/v1/videos/" + videoAssetVersionID + "/manifest/index.m3u8",
	})
}
