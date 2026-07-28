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
	if errors.Is(err, catalog.ErrAccountSuspended) {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	var conflict *catalog.LifecycleConflictError
	if errors.As(err, &conflict) {
		writeProblem(c, problem.StateConflict())
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

func (h *authoringHandlers) updateCourse(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")

	var body updateCourseBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}

	rev, err := h.repo.UpdateCourseRevision(c.Request.Context(), h.assetValidator, catalog.UpdateRevisionRequest{
		CourseID:              courseID,
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

	var body sectionBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}

	sec, err := h.repo.AddSection(c.Request.Context(), catalog.AddSectionRequest{
		CourseID:       courseID,
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
	sectionID := c.Param("sectionId")

	var body sectionBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}

	sec, err := h.repo.UpdateSection(c.Request.Context(), catalog.UpdateSectionRequest{
		CourseID:       courseID,
		OwnerAccountID: accountID,
		SectionID:      sectionID,
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
	sectionID := c.Param("sectionId")

	err := h.repo.DeleteSection(c.Request.Context(), courseID, sectionID, accountID, accountID)
	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *authoringHandlers) addLesson(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")
	sectionID := c.Param("sectionId")

	var body lessonBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}

	les, err := h.repo.AddLesson(c.Request.Context(), catalog.AddLessonRequest{
		CourseID:       courseID,
		OwnerAccountID: accountID,
		SectionID:      sectionID,
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
	lessonID := c.Param("lessonId")

	var body lessonBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}

	les, err := h.repo.UpdateLesson(c.Request.Context(), catalog.UpdateLessonRequest{
		CourseID:       courseID,
		OwnerAccountID: accountID,
		LessonID:       lessonID,
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
	lessonID := c.Param("lessonId")

	err := h.repo.DeleteLesson(c.Request.Context(), courseID, lessonID, accountID, accountID)
	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *authoringHandlers) setLessonVideo(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")
	lessonID := c.Param("lessonId")

	var body setVideoBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}

	err := h.repo.SetLessonVideo(c.Request.Context(), h.assetValidator, courseID, lessonID, body.VideoAssetVersionID, accountID, accountID)
	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *authoringHandlers) addLessonFile(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")
	lessonID := c.Param("lessonId")

	var body lessonFileBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}

	lf, err := h.repo.AddOrUpdateLessonFile(c.Request.Context(), h.assetValidator, catalog.LessonFileRequest{
		CourseID:       courseID,
		OwnerAccountID: accountID,
		LessonID:       lessonID,
		Kind:           body.Kind,
		AssetVersionID: body.AssetVersionID,
		DisplayNameAr:  body.DisplayNameAr,
		DisplayNameEn:  body.DisplayNameEn,
		Position:       body.Position,
	}, accountID)

	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, lf)
}

func (h *authoringHandlers) deleteLessonFile(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")
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

	err := h.repo.DeleteLessonFile(c.Request.Context(), courseID, lessonID, fileID, accountID, accountID)
	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *authoringHandlers) setPreviewAsset(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")

	var body previewAssetBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}

	err := h.repo.SetPreviewAsset(c.Request.Context(), h.assetValidator, courseID, body.PreviewAssetVersionID, accountID, accountID)
	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *authoringHandlers) clearPreviewAsset(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")

	err := h.repo.ClearPreviewAsset(c.Request.Context(), courseID, accountID, accountID)
	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *authoringHandlers) listTaxonomyTerms(c *gin.Context) {
	var kind *catalog.TaxonomyKind
	k := c.Query("kind")
	if k != "" {
		tk := catalog.TaxonomyKind(k)
		if tk.Valid() {
			kind = &tk
		}
	}

	terms, err := h.repo.ListTaxonomyTerms(c.Request.Context(), kind)
	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, terms)
}
