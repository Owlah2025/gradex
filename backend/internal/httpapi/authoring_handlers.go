package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

type authoringHandlers struct {
	repo           *catalog.Repository
	assetValidator catalog.AssetVersionValidator
	logger         *logging.Logger
}

type createCourseBody struct {
	TitleAr       string `json:"title_ar"`
	TitleEn       string `json:"title_en"`
	DescriptionAr string `json:"description_ar"`
	DescriptionEn string `json:"description_en"`
}

type updateCourseBody struct {
	TitleAr               string             `json:"title_ar"`
	TitleEn               string             `json:"title_en"`
	DescriptionAr         string             `json:"description_ar"`
	DescriptionEn         string             `json:"description_en"`
	MajorTermID           *string            `json:"major_term_id"`
	SubjectTermID         *string            `json:"subject_term_id"`
	StudyYear             *catalog.StudyYear `json:"study_year"`
	PreviewAssetVersionID *string            `json:"preview_asset_version_id"`
}

type sectionBody struct {
	TitleAr  string `json:"title_ar"`
	TitleEn  string `json:"title_en"`
	Position *int   `json:"position"`
}

type lessonBody struct {
	TitleAr  string `json:"title_ar"`
	TitleEn  string `json:"title_en"`
	Position *int   `json:"position"`
}

type setVideoBody struct {
	VideoAssetVersionID string `json:"video_asset_version_id"`
}

type lessonFileBody struct {
	FileID         string                 `json:"file_id"`
	Kind           catalog.LessonFileKind `json:"kind"`
	AssetVersionID string                 `json:"asset_version_id"`
	DisplayNameAr  string                 `json:"display_name_ar"`
	DisplayNameEn  string                 `json:"display_name_en"`
	Position       *int                   `json:"position"`
}

type previewAssetBody struct {
	PreviewAssetVersionID string `json:"preview_asset_version_id"`
}

func (h *authoringHandlers) handleCatalogError(c *gin.Context, err error) {
	if errors.Is(err, catalog.ErrCourseNotFound) {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	if errors.Is(err, catalog.ErrAccountSuspended) || errors.Is(err, catalog.ErrOwnerIneligible) {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	var conflict *catalog.LifecycleConflictError
	if errors.As(err, &conflict) {
		writeProblem(c, problem.StateConflict())
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
	if errors.Is(err, catalog.ErrAssetVersionInvalid) || errors.Is(err, catalog.ErrAssetVersionNotReady) {
		writeProblem(c, problem.ValidationFailed())
		return
	}
	writeProblem(c, problem.Internal(""))
}

func (h *authoringHandlers) createCourse(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	var body createCourseBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}

	course, err := h.repo.CreateCourse(c.Request.Context(), catalog.CreateCourseRequest{
		OwnerAccountID: accountID,
		TitleAr:        body.TitleAr,
		TitleEn:        body.TitleEn,
		DescriptionAr:  body.DescriptionAr,
		DescriptionEn:  body.DescriptionEn,
	}, accountID)

	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusCreated, course)
}

func (h *authoringHandlers) createCandidate(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")

	candidate, err := h.repo.CreateCandidate(c.Request.Context(), courseID, accountID, accountID)
	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, candidate)
}

func (h *authoringHandlers) listOwnedCourses(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	courses, err := h.repo.ListOwnedCourses(c.Request.Context(), accountID)
	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, courses)
}

func (h *authoringHandlers) getOwnedCourse(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")

	course, err := h.repo.GetOwnedCourse(c.Request.Context(), courseID, accountID)
	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, course)
}

func (h *authoringHandlers) updateCourseRevision(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")
	revisionID := c.Param("revisionId")

	var body updateCourseBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}

	rev, err := h.repo.UpdateCourseRevision(c.Request.Context(), h.assetValidator, catalog.UpdateRevisionRequest{
		CourseID:              courseID,
		RevisionID:            revisionID,
		OwnerAccountID:        accountID,
		TitleAr:               body.TitleAr,
		TitleEn:               body.TitleEn,
		DescriptionAr:         body.DescriptionAr,
		DescriptionEn:         body.DescriptionEn,
		MajorTermID:           body.MajorTermID,
		SubjectTermID:         body.SubjectTermID,
		StudyYear:             body.StudyYear,
		PreviewAssetVersionID: body.PreviewAssetVersionID,
	}, accountID)

	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, rev)
}

func (h *authoringHandlers) addSection(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")
	revisionID := c.Param("revisionId")

	var body sectionBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}

	sec, err := h.repo.AddSection(c.Request.Context(), catalog.AddSectionRequest{
		CourseID:       courseID,
		RevisionID:     revisionID,
		OwnerAccountID: accountID,
		TitleAr:        body.TitleAr,
		TitleEn:        body.TitleEn,
		Position:       body.Position,
	}, accountID)

	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusCreated, sec)
}

func (h *authoringHandlers) updateSection(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")
	revisionID := c.Param("revisionId")
	sectionID := c.Param("sectionId")

	var body sectionBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}

	sec, err := h.repo.UpdateSection(c.Request.Context(), catalog.UpdateSectionRequest{
		CourseID:       courseID,
		RevisionID:     revisionID,
		SectionID:      sectionID,
		OwnerAccountID: accountID,
		TitleAr:        body.TitleAr,
		TitleEn:        body.TitleEn,
		Position:       body.Position,
	}, accountID)

	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, sec)
}

func (h *authoringHandlers) deleteSection(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")
	revisionID := c.Param("revisionId")
	sectionID := c.Param("sectionId")

	err := h.repo.DeleteSection(c.Request.Context(), catalog.DeleteSectionRequest{
		CourseID:       courseID,
		RevisionID:     revisionID,
		SectionID:      sectionID,
		OwnerAccountID: accountID,
	}, accountID)

	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *authoringHandlers) addLesson(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")
	revisionID := c.Param("revisionId")
	sectionID := c.Param("sectionId")

	var body lessonBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}

	les, err := h.repo.AddLesson(c.Request.Context(), catalog.AddLessonRequest{
		CourseID:       courseID,
		RevisionID:     revisionID,
		SectionID:      sectionID,
		OwnerAccountID: accountID,
		TitleAr:        body.TitleAr,
		TitleEn:        body.TitleEn,
		Position:       body.Position,
	}, accountID)

	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusCreated, les)
}

func (h *authoringHandlers) updateLesson(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")
	revisionID := c.Param("revisionId")
	lessonID := c.Param("lessonId")

	var body lessonBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}

	les, err := h.repo.UpdateLesson(c.Request.Context(), catalog.UpdateLessonRequest{
		CourseID:       courseID,
		RevisionID:     revisionID,
		LessonID:       lessonID,
		OwnerAccountID: accountID,
		TitleAr:        body.TitleAr,
		TitleEn:        body.TitleEn,
		Position:       body.Position,
	}, accountID)

	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, les)
}

func (h *authoringHandlers) deleteLesson(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")
	revisionID := c.Param("revisionId")
	lessonID := c.Param("lessonId")

	err := h.repo.DeleteLesson(c.Request.Context(), catalog.DeleteLessonRequest{
		CourseID:       courseID,
		RevisionID:     revisionID,
		LessonID:       lessonID,
		OwnerAccountID: accountID,
	}, accountID)

	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *authoringHandlers) setLessonVideo(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")
	revisionID := c.Param("revisionId")
	lessonID := c.Param("lessonId")

	var body setVideoBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}

	les, err := h.repo.SetLessonVideo(c.Request.Context(), h.assetValidator, catalog.SetVideoRequest{
		CourseID:            courseID,
		RevisionID:          revisionID,
		LessonID:            lessonID,
		VideoAssetVersionID: body.VideoAssetVersionID,
		OwnerAccountID:      accountID,
	}, accountID)

	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, les)
}

func (h *authoringHandlers) addLessonFile(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")
	revisionID := c.Param("revisionId")
	lessonID := c.Param("lessonId")

	var body lessonFileBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}

	lf, err := h.repo.AddLessonFile(c.Request.Context(), h.assetValidator, catalog.LessonFileRequest{
		CourseID:       courseID,
		RevisionID:     revisionID,
		LessonID:       lessonID,
		Kind:           body.Kind,
		AssetVersionID: body.AssetVersionID,
		DisplayNameAr:  body.DisplayNameAr,
		DisplayNameEn:  body.DisplayNameEn,
		Position:       body.Position,
		OwnerAccountID: accountID,
	}, accountID)

	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusCreated, lf)
}

func (h *authoringHandlers) deleteLessonFile(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")
	revisionID := c.Param("revisionId")
	lessonID := c.Param("lessonId")
	fileID := c.Query("file_id")
	if fileID == "" {
		var body lessonFileBody
		_ = c.ShouldBindJSON(&body)
		fileID = body.FileID
	}
	if fileID == "" {
		writeProblem(c, problem.Malformed())
		return
	}

	err := h.repo.DeleteLessonFile(c.Request.Context(), catalog.DeleteLessonFileRequest{
		CourseID:       courseID,
		RevisionID:     revisionID,
		LessonID:       lessonID,
		FileID:         fileID,
		OwnerAccountID: accountID,
	}, accountID)

	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *authoringHandlers) setPreviewAsset(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")
	revisionID := c.Param("revisionId")

	var body previewAssetBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}

	rev, err := h.repo.SetPreviewAsset(c.Request.Context(), h.assetValidator, catalog.PreviewAssetRequest{
		CourseID:              courseID,
		RevisionID:            revisionID,
		PreviewAssetVersionID: body.PreviewAssetVersionID,
		OwnerAccountID:        accountID,
	}, accountID)

	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, rev)
}

func (h *authoringHandlers) clearPreviewAsset(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")
	revisionID := c.Param("revisionId")

	rev, err := h.repo.ClearPreviewAsset(c.Request.Context(), catalog.ClearPreviewAssetRequest{
		CourseID:       courseID,
		RevisionID:     revisionID,
		OwnerAccountID: accountID,
	}, accountID)

	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, rev)
}

func (h *authoringHandlers) submitCourse(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")
	revisionID := c.Param("revisionId")

	course, err := h.repo.SubmitCourse(c.Request.Context(), h.assetValidator, catalog.SubmitCourseRequest{
		CourseID: courseID, RevisionID: revisionID,
		OwnerAccountID: accountID, ActorDescriptor: accountID,
	})
	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, course)
}

func (h *authoringHandlers) listTaxonomyTerms(c *gin.Context) {
	var kind *catalog.TaxonomyKind
	if rawKind := c.Query("kind"); rawKind != "" {
		value := catalog.TaxonomyKind(rawKind)
		if value.Valid() {
			kind = &value
		}
	}
	terms, err := h.repo.ListTaxonomyTerms(c.Request.Context(), kind)
	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, terms)
}
