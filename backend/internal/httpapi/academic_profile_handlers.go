package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/academic"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

// Student academic profile and onboarding options (D-092, T3).
//
// Every handler here derives the account from the authenticated session. None
// accepts an account identifier from the client, so there is no shape of
// request that reads or writes another Student's profile.
//
// Nothing in this file participates in an access decision. A Student with no
// profile reaches every Course they hold an entitlement for.
type academicProfileHandlers struct{ repo *academic.Repository }

func writeProfileError(c *gin.Context, err error) {
	violation := func(code, detail string) {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: code, Location: problem.LocationBody, Detail: detail,
		}))
	}
	switch {
	case errors.Is(err, academic.ErrCurriculumNotSelectable):
		violation("CURRICULUM_NOT_SELECTABLE",
			"the study plan is resolved by Gradex and cannot be chosen")
	case errors.Is(err, academic.ErrNoActiveCurriculum):
		violation("PROGRAM_HAS_NO_ACTIVE_CURRICULUM",
			"the selected major has no active study plan yet")
	case errors.Is(err, academic.ErrFoundationUnsupported):
		violation("FOUNDATION_NOT_SUPPORTED",
			"this university does not have a foundation stage")
	case errors.Is(err, academic.ErrProgramNotSelectable):
		violation("PROGRAM_NOT_SELECTABLE",
			"the selected major is not available at the selected university")
	case errors.Is(err, academic.ErrUnitNotSelectable):
		violation("ACADEMIC_UNIT_NOT_SELECTABLE",
			"the selected college is not available at the selected university")
	case errors.Is(err, academic.ErrLevelOutOfRange):
		violation("LEVEL_OUT_OF_RANGE", "the academic level is outside this university's range")
	case errors.Is(err, academic.ErrCrossInstitution):
		violation("CROSS_INSTITUTION_SELECTION", "the selection crosses universities")
	case errors.Is(err, academic.ErrProfileInvalid):
		writeProblem(c, problem.ValidationFailed())
	case errors.Is(err, academic.ErrNotFound), errors.Is(err, academic.ErrProfileNotFound):
		writeProblem(c, problem.NotFound())
	case errors.Is(err, academic.ErrRetired):
		writeProblem(c, problem.StateConflict())
	default:
		writeProblem(c, problem.Internal(""))
	}
}

func (h *academicProfileHandlers) getProfile(c *gin.Context) {
	profile, err := h.repo.GetProfile(c.Request.Context(), c.GetString(ctxUserIDKey))
	if err != nil {
		writeProfileError(c, err)
		return
	}
	c.JSON(http.StatusOK, profile)
}

type saveProfileBody struct {
	InstitutionID    string `json:"institution_id"`
	EnrollmentStatus string `json:"enrollment_status"`
	ProgramID        string `json:"program_id"`
	AcademicUnitID   string `json:"academic_unit_id"`
	CurrentLevel     *int   `json:"current_level"`
	// CurriculumID is accepted only so a client that tries to choose one is told
	// it cannot, rather than having the field silently dropped.
	CurriculumID string `json:"curriculum_id"`
}

func (h *academicProfileHandlers) saveProfile(c *gin.Context) {
	var body saveProfileBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}
	saved, err := h.repo.SaveProfile(c.Request.Context(), academic.SaveProfileRequest{
		AccountID:            c.GetString(ctxUserIDKey),
		InstitutionID:        body.InstitutionID,
		EnrollmentStatus:     academic.EnrollmentStatus(body.EnrollmentStatus),
		ProgramID:            body.ProgramID,
		AcademicUnitID:       body.AcademicUnitID,
		CurrentLevel:         body.CurrentLevel,
		SuppliedCurriculumID: body.CurriculumID,
	})
	if err != nil {
		writeProfileError(c, err)
		return
	}
	c.JSON(http.StatusOK, saved)
}

// skipOnboarding is its own command. Encoding a deferral as an empty save would
// make SKIPPED indistinguishable from a malformed profile.
func (h *academicProfileHandlers) skipOnboarding(c *gin.Context) {
	skipped, err := h.repo.SkipOnboarding(c.Request.Context(), c.GetString(ctxUserIDKey))
	if err != nil {
		writeProfileError(c, err)
		return
	}
	c.JSON(http.StatusOK, skipped)
}

// ---------- Onboarding option projections ----------

func (h *academicProfileHandlers) listInstitutions(c *gin.Context) {
	options, err := h.repo.ListInstitutionOptions(c.Request.Context())
	if err != nil {
		writeProfileError(c, err)
		return
	}
	c.JSON(http.StatusOK, options)
}

func (h *academicProfileHandlers) listColleges(c *gin.Context) {
	options, err := h.repo.ListCollegeOptions(c.Request.Context(), c.Param("institutionId"))
	if err != nil {
		writeProfileError(c, err)
		return
	}
	c.JSON(http.StatusOK, options)
}

func (h *academicProfileHandlers) listPrograms(c *gin.Context) {
	// The college is a query parameter rather than a path segment because it
	// filters the Programs of an institution rather than owning them.
	college := c.Query("college_id")
	if college == "" {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "COLLEGE_REQUIRED", Location: problem.LocationQuery,
			Detail: "a college must be selected before its majors can be listed",
		}))
		return
	}
	options, err := h.repo.ListProgramOptions(c.Request.Context(), c.Param("institutionId"), college)
	if err != nil {
		writeProfileError(c, err)
		return
	}
	c.JSON(http.StatusOK, options)
}
