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

type authoringHandlers struct {
	repo           *catalog.Repository
	assetValidator catalog.AssetVersionValidator
	logger         *logging.Logger
}

// createCourseBody carries the academic context an ordinary Instructor Course
// now requires (D-093 1, T4-B).
//
// There is deliberately no classification field. The server derives
// ACADEMIC_CATALOG from the presence of institution_id, so an Instructor cannot
// name a classification, cannot request a legacy Course, and cannot move a
// Course between models by changing a payload. Legacy construction remains
// available to fixtures and to T5 through the domain, which is not a product
// path.
type createCourseBody struct {
	TitleAr       string `json:"title_ar"`
	TitleEn       string `json:"title_en"`
	DescriptionAr string `json:"description_ar"`
	DescriptionEn string `json:"description_en"`

	InstitutionID string `json:"institution_id"`
	SubjectID     string `json:"subject_id"`
}

type updateCourseBody struct {
	TitleAr       string             `json:"title_ar"`
	TitleEn       string             `json:"title_en"`
	DescriptionAr string             `json:"description_ar"`
	DescriptionEn string             `json:"description_en"`
	MajorTermID   *string            `json:"major_term_id"`
	SubjectTermID *string            `json:"subject_term_id"`
	StudyYear     *catalog.StudyYear `json:"study_year"`
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
	if errors.Is(err, catalog.ErrInvalidTaxonomyTerm) || errors.Is(err, catalog.ErrTaxonomyTermUnavailable) || errors.Is(err, catalog.ErrTaxonomyTermKindMismatch) {
		writeProblem(c, problem.ValidationFailed())
		return
	}
	// D-093 academic identity refusals are product states an Instructor can act
	// on, so each names itself rather than arriving as an opaque failure.
	if errors.Is(err, catalog.ErrSubjectUnavailable) {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "ACADEMIC_SUBJECT_UNAVAILABLE", Location: problem.LocationBody,
			Detail: "that Subject is not available: it must be an active Subject of the Course's university",
		}))
		return
	}
	if errors.Is(err, catalog.ErrInstitutionRequired) {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "ACADEMIC_INSTITUTION_REQUIRED", Location: problem.LocationBody,
			Detail: "a new Course must name the university it is taught at",
		}))
		return
	}
	if errors.Is(err, catalog.ErrAcademicContextRequired) {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "COURSE_NOT_ACADEMIC", Location: problem.LocationBody,
			Detail: "this Course uses the legacy classification and has no academic Subject",
		}))
		return
	}
	if errors.Is(err, catalog.ErrSubjectImmutable) {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "ACADEMIC_SUBJECT_IMMUTABLE", Location: problem.LocationBody,
			Detail: "this Course has been published; its Subject is part of its identity and cannot change",
		}))
		return
	}
	if errors.Is(err, catalog.ErrSubjectLockedForReview) {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "ACADEMIC_SUBJECT_LOCKED_FOR_REVIEW", Location: problem.LocationBody,
			Detail: "the Subject cannot change while this Course is under review",
		}))
		return
	}
	if errors.Is(err, catalog.ErrLegacyTaxonomyOnAcademicCourse) {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "LEGACY_TAXONOMY_ON_ACADEMIC_COURSE", Location: problem.LocationBody,
			Detail: "this Course uses the Academic Catalog and does not carry the legacy classification",
		}))
		return
	}
	if errors.Is(err, catalog.ErrAudienceRequiresSubject) {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "ACADEMIC_AUDIENCE_REQUIRES_SUBJECT", Location: problem.LocationBody,
			Detail: "choose a canonical Subject before customizing the Course audience",
		}))
		return
	}
	if errors.Is(err, catalog.ErrAudienceTargetInvalid) || errors.Is(err, catalog.ErrAudienceTargetDuplicate) {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "ACADEMIC_AUDIENCE_TARGET_INVALID", Location: problem.LocationBody,
			Detail: "every customized Program must be a unique active Program mapped to the Course Subject",
		}))
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

	// T4-B closes ordinary legacy creation. Omitting the academic context is a
	// validation failure rather than a silent fall-back to LEGACY_TAXONOMY,
	// because a silent fall-back is exactly how the old confusing taxonomy
	// Course would keep being created after the redesign shipped.
	if strings.TrimSpace(body.InstitutionID) == "" {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "ACADEMIC_INSTITUTION_REQUIRED", Location: problem.LocationBody,
			Detail: "a new Course must name the university it is taught at",
		}))
		return
	}
	// T4-D activates the subject-less Academic draft when the Instructor raises
	// a missing-Subject request. Normal creation still supplies a Subject; an
	// empty value remains ACADEMIC_CATALOG because Institution is always present.
	var subjectID *string
	if value := strings.TrimSpace(body.SubjectID); value != "" {
		subjectID = &value
	}
	course, err := h.repo.CreateCourse(c.Request.Context(), catalog.CreateCourseRequest{
		OwnerAccountID: accountID,
		TitleAr:        body.TitleAr,
		TitleEn:        body.TitleEn,
		DescriptionAr:  body.DescriptionAr,
		DescriptionEn:  body.DescriptionEn,
		Academic: &catalog.AcademicCourseContext{
			InstitutionID: strings.TrimSpace(body.InstitutionID),
			SubjectID:     subjectID,
		},
	}, accountID)

	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusCreated, course)
}

// setCourseSubject corrects the canonical Subject of an Academic Course that has
// never been published. Every lifecycle rule — never published, no candidate in
// review, active Subject, same Institution — is enforced by the T4-A domain
// command and by the database, not here.
type setCourseSubjectBody struct {
	SubjectID string `json:"subject_id"`
}

func (h *authoringHandlers) setCourseSubject(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	var body setCourseSubjectBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}
	course, err := h.repo.SetCourseSubject(c.Request.Context(), catalog.SetCourseSubjectRequest{
		CourseID:        c.Param("id"),
		OwnerAccountID:  accountID,
		SubjectID:       strings.TrimSpace(body.SubjectID),
		ActorDescriptor: accountID,
	})
	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, course)
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

type setRevisionAudienceBody struct {
	ProgramIDs []string `json:"program_ids"`
}

func (h *authoringHandlers) setRevisionAudience(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	var body setRevisionAudienceBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}
	audience, err := h.repo.SetRevisionAudience(c.Request.Context(), catalog.SetRevisionAudienceRequest{
		CourseID: c.Param("id"), RevisionID: c.Param("revisionId"),
		OwnerAccountID: accountID, ProgramIDs: body.ProgramIDs, ActorDescriptor: accountID,
	})
	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, audience)
}

func (h *authoringHandlers) resetRevisionAudience(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	audience, err := h.repo.ResetRevisionAudience(c.Request.Context(), catalog.ResetRevisionAudienceRequest{
		CourseID: c.Param("id"), RevisionID: c.Param("revisionId"),
		OwnerAccountID: accountID, ActorDescriptor: accountID,
	})
	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, audience)
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
		CourseID:       courseID,
		RevisionID:     revisionID,
		OwnerAccountID: accountID,
		TitleAr:        body.TitleAr,
		TitleEn:        body.TitleEn,
		DescriptionAr:  body.DescriptionAr,
		DescriptionEn:  body.DescriptionEn,
		MajorTermID:    body.MajorTermID,
		SubjectTermID:  body.SubjectTermID,
		StudyYear:      body.StudyYear,
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
