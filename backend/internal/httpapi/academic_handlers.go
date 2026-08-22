package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/academic"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

// academicHandlers exposes semantic Admin commands over the Academic Catalog.
// There is no generic table CRUD: every route names a domain action, and every
// mutation carries the acting Admin.
type academicHandlers struct{ repo *academic.Repository }

func (h *academicHandlers) actor(c *gin.Context) academic.Actor {
	id := c.GetString(ctxUserIDKey)
	return academic.Actor{AdminAccountID: id, ActorDescriptor: id}
}

// writeAcademicError maps a domain error onto the canonical problem contract.
// A duplicate Subject returns 409 with the existing Subject attached so the
// Admin surface can name it rather than reporting an opaque failure.
func writeAcademicError(c *gin.Context, err error) {
	var courseConflict *academic.SubjectRequestCourseConflictError
	if errors.As(err, &courseConflict) {
		c.Header("Content-Type", "application/problem+json")
		c.JSON(http.StatusConflict, gin.H{
			"type":  "https://api.gradex.com/problems/state-conflict",
			"title": "State conflict", "status": http.StatusConflict,
			"code":   "COURSE_SUBJECT_ALREADY_SELECTED",
			"detail": courseConflict.Error(), "subject_request": courseConflict.Request,
		})
		return
	}
	var duplicate *academic.DuplicateSubjectError
	if errors.As(err, &duplicate) && duplicate.Existing != nil {
		c.Header("Content-Type", "application/problem+json")
		c.JSON(http.StatusConflict, gin.H{
			"type":             "https://api.gradex.com/problems/state-conflict",
			"title":            "State conflict",
			"status":           http.StatusConflict,
			"code":             "SUBJECT_ALREADY_EXISTS",
			"existing_subject": duplicate.Existing,
		})
		return
	}
	switch {
	case errors.Is(err, academic.ErrNotFound):
		writeProblem(c, problem.NotFound())
	case errors.Is(err, academic.ErrAdminRequired):
		writeProblem(c, problem.NotAuthorized())
	case errors.Is(err, academic.ErrInvalidInput):
		writeProblem(c, problem.ValidationFailed())
	case errors.Is(err, academic.ErrSubjectRequestRejectReason):
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "SUBJECT_REQUEST_REJECTION_REASON_REQUIRED", Location: problem.LocationBody,
			Detail: "a rejection reason is required",
		}))
	case errors.Is(err, academic.ErrSubjectRequestInstructorOnly),
		errors.Is(err, academic.ErrSubjectRequestOwnerMismatch):
		writeProblem(c, problem.NotAuthorized())
	case errors.Is(err, academic.ErrCrossInstitution):
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "CROSS_INSTITUTION_RELATIONSHIP", Location: problem.LocationBody,
			Detail: "an academic relationship may not cross institutions",
		}))
	case errors.Is(err, academic.ErrHierarchyCycle):
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "ACADEMIC_UNIT_CYCLE", Location: problem.LocationBody,
			Detail: "the academic unit hierarchy may not contain a cycle",
		}))
	case errors.Is(err, academic.ErrSubjectCodeImmutable):
		// D-093 §7 (amended). A semantic refusal, never a 500: the Admin is told
		// that the code is canonical identity, not that something broke.
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "SUBJECT_CODE_IMMUTABLE", Location: problem.LocationBody,
			Detail: "a subject's official code is canonical identity; it may be reformatted but not renumbered or removed",
		}))
	case errors.Is(err, academic.ErrLevelOutOfRange):
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "RECOMMENDED_LEVEL_OUT_OF_RANGE", Location: problem.LocationBody,
			Detail: "the recommended level exceeds the institution maximum",
		}))
	case errors.Is(err, academic.ErrDuplicateSubject),
		errors.Is(err, academic.ErrCurriculumActive),
		errors.Is(err, academic.ErrVersionLabelTaken),
		errors.Is(err, academic.ErrMappingDuplicate),
		errors.Is(err, academic.ErrSlugTaken),
		errors.Is(err, academic.ErrRetired),
		errors.Is(err, academic.ErrStillReferenced),
		errors.Is(err, academic.ErrSubjectRequestPendingExists),
		errors.Is(err, academic.ErrSubjectRequestNotPending),
		errors.Is(err, academic.ErrSubjectRequestCourseInvalid):
		writeProblem(c, problem.StateConflict())
	default:
		writeProblem(c, problem.Internal(""))
	}
}

func boolQuery(c *gin.Context, key string) bool {
	value, err := strconv.ParseBool(c.Query(key))
	return err == nil && value
}

// ---------- Institutions ----------

type createInstitutionBody struct {
	CountryCode        string `json:"country_code"`
	Slug               string `json:"slug"`
	NameAr             string `json:"name_ar"`
	NameEn             string `json:"name_en"`
	MaxAcademicLevel   int    `json:"max_academic_level"`
	HasFoundationStage bool   `json:"has_foundation_stage"`
}

func (h *academicHandlers) listInstitutions(c *gin.Context) {
	items, err := h.repo.ListInstitutions(c.Request.Context(), boolQuery(c, "include_retired"))
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *academicHandlers) createInstitution(c *gin.Context) {
	var body createInstitutionBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}
	created, err := h.repo.CreateInstitution(c.Request.Context(), academic.CreateInstitutionRequest{
		Actor: h.actor(c), CountryCode: body.CountryCode, Slug: body.Slug,
		NameAr: body.NameAr, NameEn: body.NameEn,
		MaxAcademicLevel: body.MaxAcademicLevel, HasFoundationStage: body.HasFoundationStage,
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusCreated, created)
}

type updateInstitutionBody struct {
	NameAr             *string `json:"name_ar"`
	NameEn             *string `json:"name_en"`
	MaxAcademicLevel   *int    `json:"max_academic_level"`
	HasFoundationStage *bool   `json:"has_foundation_stage"`
}

func (h *academicHandlers) updateInstitution(c *gin.Context) {
	var body updateInstitutionBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}
	updated, err := h.repo.UpdateInstitution(c.Request.Context(), academic.UpdateInstitutionRequest{
		Actor: h.actor(c), InstitutionID: c.Param("institutionId"),
		NameAr: body.NameAr, NameEn: body.NameEn,
		MaxAcademicLevel: body.MaxAcademicLevel, HasFoundationStage: body.HasFoundationStage,
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (h *academicHandlers) retireInstitution(c *gin.Context) {
	retired, err := h.repo.RetireInstitution(c.Request.Context(), academic.RetireRequest{
		Actor: h.actor(c), ID: c.Param("institutionId"),
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, retired)
}

// ---------- Academic units ----------

type createUnitBody struct {
	ParentUnitID *string `json:"parent_unit_id"`
	Kind         string  `json:"kind"`
	Slug         string  `json:"slug"`
	NameAr       string  `json:"name_ar"`
	NameEn       string  `json:"name_en"`
}

func (h *academicHandlers) listUnits(c *gin.Context) {
	items, err := h.repo.ListAcademicUnits(c.Request.Context(), c.Param("institutionId"), boolQuery(c, "include_retired"))
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *academicHandlers) createUnit(c *gin.Context) {
	var body createUnitBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}
	created, err := h.repo.CreateAcademicUnit(c.Request.Context(), academic.CreateUnitRequest{
		Actor: h.actor(c), InstitutionID: c.Param("institutionId"),
		ParentUnitID: body.ParentUnitID, Kind: academic.UnitKind(body.Kind),
		Slug: body.Slug, NameAr: body.NameAr, NameEn: body.NameEn,
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusCreated, created)
}

type updateUnitBody struct {
	NameAr *string `json:"name_ar"`
	NameEn *string `json:"name_en"`
	Kind   *string `json:"kind"`
	// ReparentTo is tri-state on the wire too: omit to leave the parent alone,
	// send "" to detach to the institution root, send an id to re-parent.
	ReparentTo *string `json:"reparent_to"`
}

func (h *academicHandlers) updateUnit(c *gin.Context) {
	var body updateUnitBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}
	var kind *academic.UnitKind
	if body.Kind != nil {
		parsed := academic.UnitKind(*body.Kind)
		kind = &parsed
	}
	updated, err := h.repo.UpdateAcademicUnit(c.Request.Context(), academic.UpdateUnitRequest{
		Actor: h.actor(c), UnitID: c.Param("unitId"),
		NameAr: body.NameAr, NameEn: body.NameEn, Kind: kind, ReparentTo: body.ReparentTo,
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (h *academicHandlers) retireUnit(c *gin.Context) {
	retired, err := h.repo.RetireAcademicUnit(c.Request.Context(), academic.RetireRequest{
		Actor: h.actor(c), ID: c.Param("unitId"),
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, retired)
}

// ---------- Programs ----------

type createProgramBody struct {
	OwningUnitID *string `json:"owning_unit_id"`
	Slug         string  `json:"slug"`
	NameAr       string  `json:"name_ar"`
	NameEn       string  `json:"name_en"`
	DegreeKind   string  `json:"degree_kind"`
}

func (h *academicHandlers) listPrograms(c *gin.Context) {
	items, err := h.repo.ListPrograms(c.Request.Context(), c.Param("institutionId"), boolQuery(c, "include_retired"))
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *academicHandlers) createProgram(c *gin.Context) {
	var body createProgramBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}
	created, err := h.repo.CreateProgram(c.Request.Context(), academic.CreateProgramRequest{
		Actor: h.actor(c), InstitutionID: c.Param("institutionId"),
		OwningUnitID: body.OwningUnitID, Slug: body.Slug,
		NameAr: body.NameAr, NameEn: body.NameEn, DegreeKind: body.DegreeKind,
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusCreated, created)
}

type updateProgramBody struct {
	NameAr        *string `json:"name_ar"`
	NameEn        *string `json:"name_en"`
	DegreeKind    *string `json:"degree_kind"`
	SetOwningUnit *string `json:"set_owning_unit"`
}

func (h *academicHandlers) updateProgram(c *gin.Context) {
	var body updateProgramBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}
	updated, err := h.repo.UpdateProgram(c.Request.Context(), academic.UpdateProgramRequest{
		Actor: h.actor(c), ProgramID: c.Param("programId"),
		NameAr: body.NameAr, NameEn: body.NameEn,
		DegreeKind: body.DegreeKind, SetOwningUnit: body.SetOwningUnit,
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (h *academicHandlers) retireProgram(c *gin.Context) {
	retired, err := h.repo.RetireProgram(c.Request.Context(), academic.RetireRequest{
		Actor: h.actor(c), ID: c.Param("programId"),
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, retired)
}

// ---------- Curricula ----------

type createCurriculumBody struct {
	VersionLabel      string `json:"version_label"`
	EffectiveFromYear *int   `json:"effective_from_year"`
	SupersedeActive   bool   `json:"supersede_active"`
}

func (h *academicHandlers) listCurricula(c *gin.Context) {
	items, err := h.repo.ListCurricula(c.Request.Context(), c.Param("programId"), boolQuery(c, "include_retired"))
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *academicHandlers) createCurriculum(c *gin.Context) {
	var body createCurriculumBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}
	created, err := h.repo.CreateCurriculum(c.Request.Context(), academic.CreateCurriculumRequest{
		Actor: h.actor(c), ProgramID: c.Param("programId"),
		VersionLabel: body.VersionLabel, EffectiveFromYear: body.EffectiveFromYear,
		SupersedeActive: body.SupersedeActive,
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusCreated, created)
}

type updateCurriculumBody struct {
	VersionLabel      *string `json:"version_label"`
	EffectiveFromYear *int    `json:"effective_from_year"`
	ClearEffectiveY   bool    `json:"clear_effective_from_year"`
}

func (h *academicHandlers) updateCurriculum(c *gin.Context) {
	var body updateCurriculumBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}
	updated, err := h.repo.UpdateCurriculum(c.Request.Context(), academic.UpdateCurriculumRequest{
		Actor: h.actor(c), CurriculumID: c.Param("curriculumId"),
		VersionLabel: body.VersionLabel, EffectiveFromYear: body.EffectiveFromYear,
		ClearEffectiveY: body.ClearEffectiveY,
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (h *academicHandlers) retireCurriculum(c *gin.Context) {
	retired, err := h.repo.RetireCurriculum(c.Request.Context(), academic.RetireRequest{
		Actor: h.actor(c), ID: c.Param("curriculumId"),
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, retired)
}

// ---------- Curriculum ↔ Subject mapping ----------

type mapSubjectBody struct {
	SubjectID           string   `json:"subject_id"`
	RequirementKind     string   `json:"requirement_kind"`
	RecommendedLevel    *int     `json:"recommended_level"`
	RecommendedSemester *int     `json:"recommended_semester"`
	Credits             *float64 `json:"credits"`
}

func (h *academicHandlers) listCurriculumSubjects(c *gin.Context) {
	items, err := h.repo.ListCurriculumSubjects(c.Request.Context(), c.Param("curriculumId"))
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *academicHandlers) mapSubject(c *gin.Context) {
	var body mapSubjectBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}
	mapped, err := h.repo.MapSubjectToCurriculum(c.Request.Context(), academic.MapSubjectRequest{
		Actor: h.actor(c), CurriculumID: c.Param("curriculumId"), SubjectID: body.SubjectID,
		RequirementKind:  academic.RequirementKind(body.RequirementKind),
		RecommendedLevel: body.RecommendedLevel, RecommendedSemester: body.RecommendedSemester,
		Credits: body.Credits,
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusCreated, mapped)
}

func (h *academicHandlers) unmapSubject(c *gin.Context) {
	err := h.repo.UnmapSubjectFromCurriculum(c.Request.Context(), academic.UnmapSubjectRequest{
		Actor: h.actor(c), CurriculumID: c.Param("curriculumId"), SubjectID: c.Param("subjectId"),
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ---------- Subjects ----------

type createSubjectBody struct {
	OwningUnitID *string `json:"owning_unit_id"`
	OfficialCode *string `json:"official_code"`
	TitleAr      string  `json:"title_ar"`
	TitleEn      string  `json:"title_en"`
}

func (h *academicHandlers) listSubjects(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, err := h.repo.ListSubjects(c.Request.Context(), academic.ListSubjectsRequest{
		InstitutionID: c.Param("institutionId"), Query: c.Query("q"),
		IncludeRetired: boolQuery(c, "include_retired"), Limit: limit,
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *academicHandlers) createSubject(c *gin.Context) {
	var body createSubjectBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}
	created, err := h.repo.CreateSubject(c.Request.Context(), academic.CreateSubjectRequest{
		Actor: h.actor(c), InstitutionID: c.Param("institutionId"),
		OwningUnitID: body.OwningUnitID, OfficialCode: body.OfficialCode,
		TitleAr: body.TitleAr, TitleEn: body.TitleEn,
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusCreated, created)
}

type updateSubjectBody struct {
	TitleAr       *string `json:"title_ar"`
	TitleEn       *string `json:"title_en"`
	OfficialCode  *string `json:"official_code"`
	ClearCode     bool    `json:"clear_official_code"`
	SetOwningUnit *string `json:"set_owning_unit"`
}

func (h *academicHandlers) updateSubject(c *gin.Context) {
	var body updateSubjectBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}
	updated, err := h.repo.UpdateSubject(c.Request.Context(), academic.UpdateSubjectRequest{
		Actor: h.actor(c), SubjectID: c.Param("subjectId"),
		TitleAr: body.TitleAr, TitleEn: body.TitleEn,
		OfficialCode: body.OfficialCode, ClearCode: body.ClearCode,
		SetOwningUnit: body.SetOwningUnit,
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (h *academicHandlers) retireSubject(c *gin.Context) {
	retired, err := h.repo.RetireSubject(c.Request.Context(), academic.RetireRequest{
		Actor: h.actor(c), ID: c.Param("subjectId"),
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, retired)
}
