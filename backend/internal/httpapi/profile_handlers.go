package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/instructorprofile"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

const instructorProfileBodyLimit int64 = 64 * 1024

type profileHandlers struct {
	foundation *ProfileFoundation
}

type instructorProfileDraftBody struct {
	Revision     int      `json:"revision"`
	PublicSlug   string   `json:"public_slug"`
	HeadlineAr   string   `json:"headline_ar"`
	HeadlineEn   string   `json:"headline_en"`
	BioAr        string   `json:"bio_ar"`
	BioEn        string   `json:"bio_en"`
	ExpertiseIDs []string `json:"expertise_ids"`
}

type instructorProfileSubmissionBody struct {
	Revision int `json:"revision"`
}

type moderationReasonBody struct {
	Reason string `json:"reason"`
}

type accountProfileBody struct {
	DisplayName *string          `json:"display_name"`
	Locale      *identity.Locale `json:"locale"`
}

func (h *profileHandlers) getInstructorProfile(c *gin.Context) {
	profile, err := h.foundation.instructor.GetOwn(c.Request.Context(), c.GetString(ctxUserIDKey))
	if err != nil {
		writeInstructorProfileError(c, err)
		return
	}
	c.JSON(http.StatusOK, profile)
}

func (h *profileHandlers) saveInstructorProfile(c *gin.Context) {
	body := c.MustGet(strictJSONBodyContextKey).(*instructorProfileDraftBody)
	if body.Revision < 0 {
		writeProblem(c, problem.ValidationFailed())
		return
	}
	profile, err := h.foundation.instructor.SaveDraft(c.Request.Context(), instructorprofile.SaveDraftRequest{
		AccountID:        c.GetString(ctxUserIDKey),
		ExpectedRevision: body.Revision,
		PublicSlug:       body.PublicSlug,
		HeadlineAr:       body.HeadlineAr,
		HeadlineEn:       body.HeadlineEn,
		BioAr:            body.BioAr,
		BioEn:            body.BioEn,
		ExpertiseIDs:     body.ExpertiseIDs,
	})
	if err != nil {
		writeInstructorProfileError(c, err)
		return
	}
	c.JSON(http.StatusOK, profile)
}

func (h *profileHandlers) submitInstructorProfile(c *gin.Context) {
	body := c.MustGet(strictJSONBodyContextKey).(*instructorProfileSubmissionBody)
	if body.Revision < 1 {
		writeProblem(c, problem.ValidationFailed())
		return
	}
	profile, err := h.foundation.instructor.Submit(c.Request.Context(), instructorprofile.SubmitRequest{
		AccountID:        c.GetString(ctxUserIDKey),
		ExpectedRevision: body.Revision,
	})
	if err != nil {
		writeInstructorProfileError(c, err)
		return
	}
	c.JSON(http.StatusOK, profile)
}

func (h *profileHandlers) listInstructorProfiles(c *gin.Context) {
	stateText := strings.TrimSpace(c.Query("state"))
	state := instructorprofile.PublicationState(stateText)
	if stateText != "" && !state.Valid() {
		writeProblem(c, problem.ValidationFailed())
		return
	}
	page := queryInt(c.Query("page"), 1)
	limit := queryInt(c.Query("limit"), 25)
	result, err := h.foundation.instructor.List(c.Request.Context(), instructorprofile.ListRequest{
		State: state,
		Page:  page,
		Limit: limit,
	})
	if err != nil {
		writeInstructorProfileError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *profileHandlers) getAdminInstructorProfile(c *gin.Context) {
	accountID := c.Param("accountId")
	if _, err := uuid.Parse(accountID); err != nil {
		writeProblem(c, problem.NotFound())
		return
	}
	profile, err := h.foundation.instructor.GetAdmin(c.Request.Context(), accountID)
	if err != nil {
		writeInstructorProfileError(c, err)
		return
	}
	c.JSON(http.StatusOK, profile)
}

func (h *profileHandlers) approveInstructorProfile(c *gin.Context) {
	h.decideInstructorProfile(c, instructorprofile.DecisionRequest{
		AccountID:      c.Param("accountId"),
		AdminAccountID: c.GetString(ctxUserIDKey),
		Reason:         c.MustGet(strictJSONBodyContextKey).(*moderationReasonBody).Reason,
	}, h.foundation.instructor.Approve)
}

func (h *profileHandlers) requestInstructorChanges(c *gin.Context) {
	h.decideInstructorProfile(c, instructorprofile.DecisionRequest{
		AccountID:      c.Param("accountId"),
		AdminAccountID: c.GetString(ctxUserIDKey),
		Reason:         c.MustGet(strictJSONBodyContextKey).(*moderationReasonBody).Reason,
	}, h.foundation.instructor.RequestChanges)
}

func (h *profileHandlers) hideInstructorProfile(c *gin.Context) {
	h.decideInstructorProfile(c, instructorprofile.DecisionRequest{
		AccountID:      c.Param("accountId"),
		AdminAccountID: c.GetString(ctxUserIDKey),
		Reason:         c.MustGet(strictJSONBodyContextKey).(*moderationReasonBody).Reason,
	}, h.foundation.instructor.Hide)
}

type profileDecision func(context.Context, instructorprofile.DecisionRequest) (*instructorprofile.Profile, error)

func (h *profileHandlers) decideInstructorProfile(
	c *gin.Context,
	request instructorprofile.DecisionRequest,
	decide profileDecision,
) {
	if _, err := uuid.Parse(request.AccountID); err != nil {
		writeProblem(c, problem.NotFound())
		return
	}
	if !requireRecentAdminAuthentication(c, time.Now().UTC()) {
		return
	}
	profile, err := decide(c.Request.Context(), request)
	if err != nil {
		writeInstructorProfileError(c, err)
		return
	}
	c.JSON(http.StatusOK, profile)
}

func (h *profileHandlers) publicInstructorProfile(c *gin.Context) {
	snapshot, accountID, err := h.foundation.instructor.Published(c.Request.Context(), c.Param("slug"))
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}
	if snapshot == nil {
		writeAnonymousProblem(c, instructorprofileNotFoundProblem())
		return
	}
	courses, err := h.foundation.catalog.CoursesByOwner(
		c.Request.Context(), accountID, publicCatalogArabic(c),
	)
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}
	c.Header("Cache-Control", publicCatalogCacheControl)
	response := gin.H{
		"public_slug":  snapshot.PublicSlug,
		"display_name": snapshot.DisplayName,
		"headline_ar":  snapshot.HeadlineAr,
		"headline_en":  snapshot.HeadlineEn,
		"bio_ar":       snapshot.BioAr,
		"bio_en":       snapshot.BioEn,
		"expertise":    snapshot.Expertise,
		"avatar_url":   snapshot.AvatarURL,
		"courses":      courses,
	}
	c.JSON(http.StatusOK, response)
}

func (h *profileHandlers) getAccountProfile(c *gin.Context) {
	profile, err := h.foundation.account.Get(c.Request.Context(), c.GetString(ctxUserIDKey))
	if err != nil {
		writeAccountProfileError(c, err)
		return
	}
	c.JSON(http.StatusOK, profile)
}

func (h *profileHandlers) updateAccountProfile(c *gin.Context) {
	body := c.MustGet(strictJSONBodyContextKey).(*accountProfileBody)
	profile, err := h.foundation.account.Update(c.Request.Context(), identity.AccountProfileUpdateRequest{
		AccountID:   c.GetString(ctxUserIDKey),
		DisplayName: body.DisplayName,
		Locale:      body.Locale,
	})
	if err != nil {
		writeAccountProfileError(c, err)
		return
	}
	c.JSON(http.StatusOK, profile)
}

func writeInstructorProfileError(c *gin.Context, err error) {
	var incomplete *instructorprofile.SubmissionIncompleteError
	switch {
	case errors.Is(err, instructorprofile.ErrNotInstructor):
		writeProblem(c, problem.NotAuthorized())
	case errors.Is(err, instructorprofile.ErrProfileNotFound):
		writeProblem(c, problem.NotFound())
	case errors.Is(err, instructorprofile.ErrSlugTaken):
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "PUBLIC_SLUG_TAKEN", Detail: "This public URL is already used.",
			Location: problem.LocationBody, Parameter: "public_slug",
		}))
	case errors.Is(err, instructorprofile.ErrSubjectNotFound):
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "EXPERTISE_SUBJECT_NOT_FOUND", Detail: "Choose subjects from the academic catalogue.",
			Location: problem.LocationBody, Parameter: "expertise_ids",
		}))
	case errors.Is(err, instructorprofile.ErrRevisionConflict), errors.Is(err, instructorprofile.ErrInvalidTransition):
		writeProblem(c, problem.StateConflict())
	case errors.Is(err, instructorprofile.ErrReasonRequired):
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "REASON_REQUIRED", Detail: "A reason is required.",
			Location: problem.LocationBody, Parameter: "reason",
		}))
	case errors.As(err, &incomplete):
		violations := make([]problem.Violation, 0, len(incomplete.Violations))
		for _, field := range incomplete.Violations {
			violations = append(violations, problem.Violation{
				Code: "PROFILE_INCOMPLETE", Detail: "Complete this field before submitting.",
				Location: problem.LocationBody, Parameter: field,
			})
		}
		writeProblem(c, problem.ValidationFailed().WithViolations(violations...))
	case errors.Is(err, instructorprofile.ErrInvalidInput):
		writeProblem(c, problem.ValidationFailed())
	default:
		writeProblem(c, problem.Internal(""))
	}
}

func writeAccountProfileError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, identity.ErrAccountProfileNotFound):
		writeProblem(c, problem.NotAuthorized())
	case errors.Is(err, identity.ErrAccountProfileInvalid), errors.Is(err, identity.ErrInvalidLocale):
		writeProblem(c, problem.ValidationFailed())
	case errors.Is(err, identity.ErrInvalidDisplayName):
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "INVALID_DISPLAY_NAME", Detail: "Use 2 to 50 supported letters.",
			Location: problem.LocationBody, Parameter: "display_name",
		}))
	default:
		writeProblem(c, problem.Internal(""))
	}
}

func instructorprofileNotFoundProblem() problem.Problem {
	return problem.NotFound()
}

func queryInt(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}
