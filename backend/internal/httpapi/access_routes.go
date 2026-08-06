package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Owlah2025/gradex/backend/internal/access"
	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

const accessMutationBodyLimit = 16 * 1024

type accessHandlers struct {
	repo  *access.Repository
	clock func() time.Time
}

type setDefaultAccessExpiryBody struct {
	Date   string `json:"date"`
	Reason string `json:"reason"`
}

type defaultAccessExpiryResponse struct {
	CourseID            string    `json:"course_id"`
	DefaultAccessEndsAt time.Time `json:"default_access_ends_at"`
	Reason              string    `json:"reason"`
}

type createInvitationBody struct {
	CourseID          string  `json:"course_id"`
	Email             string  `json:"email"`
	AdminNote         *string `json:"admin_note"`
	ExternalReference *string `json:"external_reference"`
}

type acceptInvitationBody struct {
	AcceptanceToken string `json:"acceptance_token"`
}

type adminInvitationListResponse struct {
	Invitations []access.Invitation `json:"invitations"`
	Total       int                 `json:"total"`
	Page        int                 `json:"page"`
	Limit       int                 `json:"limit"`
}

type studentInvitationListResponse struct {
	Invitations []access.StudentInvitation `json:"invitations"`
}

func mountAccessRoutes(
	v1 *gin.RouterGroup,
	foundation *AccessFoundation,
	sessionFoundation *SessionFoundation,
	authenticator auth.Authenticator,
	principals identity.PrincipalResolver,
	logger *logging.Logger,
) error {
	if foundation == nil || foundation.repository == nil {
		return errors.New("access foundation and repository are required")
	}

	clock := foundation.clock
	if clock == nil {
		clock = time.Now
	}

	h := &accessHandlers{repo: foundation.repository, clock: clock}

	// Admin mutations
	adminAccessGroup := v1.Group("/admin")
	adminAccessGroup.Use(
		sessionFoundation.requireSessionMutationSecurity(),
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapCourseAccessGrant),
	)
	{
		adminAccessGroup.PUT("/courses/:id/default-access-expiry",
			strictJSONMiddleware(func() any { return &setDefaultAccessExpiryBody{} }, accessMutationBodyLimit),
			h.setCourseDefaultAccessExpiry,
		)
		adminAccessGroup.POST("/course-access-invitations",
			strictJSONMiddleware(func() any { return &createInvitationBody{} }, accessMutationBodyLimit),
			h.createCourseAccessInvitation,
		)
	}

	// Admin reads
	adminReadGroup := v1.Group("/admin")
	adminReadGroup.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapCourseAccessGrant),
	)
	{
		adminReadGroup.GET("/course-access-invitations", h.listAdminCourseAccessInvitations)
	}

	// Student reads
	meReadGroup := v1.Group("/me")
	meReadGroup.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapLearningAccess),
	)
	{
		meReadGroup.GET("/course-access-invitations", h.listStudentCourseAccessInvitations)
		meReadGroup.GET("/course-access-invitations/:id", h.getStudentCourseAccessInvitation)
		meReadGroup.GET("/course-access", h.getStudentCourseAccessHistory)
	}

	// Student mutations
	meMutationGroup := v1.Group("/me")
	meMutationGroup.Use(
		sessionFoundation.requireSessionMutationSecurity(),
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapLearningAccess),
	)
	{
		meMutationGroup.POST("/course-access-invitations/:id/accept",
			strictJSONMiddleware(func() any { return &acceptInvitationBody{} }, accessMutationBodyLimit),
			h.acceptStudentCourseAccessInvitation,
		)
	}

	return nil
}

func (h *accessHandlers) setCourseDefaultAccessExpiry(c *gin.Context) {
	adminAccountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")

	if _, err := uuid.Parse(courseID); err != nil {
		writeProblem(c, problem.NotFound())
		return
	}

	body := c.MustGet(strictJSONBodyContextKey).(*setDefaultAccessExpiryBody)

	if strings.TrimSpace(body.Date) == "" {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code:      "DATE_REQUIRED",
			Detail:    "Expiry date (YYYY-MM-DD) is required",
			Location:  problem.LocationBody,
			Parameter: "date",
		}))
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

	now := time.Now()
	if h != nil && h.clock != nil {
		now = h.clock()
	}

	utcExpiry, err := access.ConvertKuwaitDateToUTCExpiry(body.Date, now)
	if err != nil {
		if errors.Is(err, access.ErrExpiryInPast) {
			writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
				Code:      "EXPIRY_IN_PAST",
				Detail:    "Default access expiry must be in the future",
				Location:  problem.LocationBody,
				Parameter: "date",
			}))
			return
		}
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code:      "INVALID_DATE_FORMAT",
			Detail:    "Date must be a valid Kuwait local date in YYYY-MM-DD format",
			Location:  problem.LocationBody,
			Parameter: "date",
		}))
		return
	}

	err = h.repo.SetCourseDefaultAccessExpiry(c.Request.Context(), access.SetCourseDefaultAccessExpiryParams{
		CourseID:            courseID,
		AdminAccountID:      adminAccountID,
		ActorDescriptor:     adminAccountID,
		DefaultAccessEndsAt: utcExpiry,
		Reason:              body.Reason,
	})
	if err != nil {
		if errors.Is(err, access.ErrCourseNotFound) {
			writeProblem(c, problem.NotFound())
			return
		}
		if errors.Is(err, access.ErrReasonRequired) || errors.Is(err, access.ErrExpiryRequired) {
			writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
				Code:      "VALIDATION_FAILED",
				Detail:    err.Error(),
				Location:  problem.LocationBody,
				Parameter: "date",
			}))
			return
		}
		writeProblem(c, problem.Internal(""))
		return
	}

	c.JSON(http.StatusOK, defaultAccessExpiryResponse{
		CourseID:            courseID,
		DefaultAccessEndsAt: utcExpiry,
		Reason:              body.Reason,
	})
}

func (h *accessHandlers) createCourseAccessInvitation(c *gin.Context) {
	adminAccountID := c.GetString(ctxUserIDKey)
	body := c.MustGet(strictJSONBodyContextKey).(*createInvitationBody)

	if _, err := uuid.Parse(body.CourseID); err != nil {
		writeProblem(c, problem.NotFound())
		return
	}

	email := strings.TrimSpace(body.Email)
	if email == "" || !strings.Contains(email, "@") {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code:      "EMAIL_REQUIRED",
			Detail:    "A valid email address is required",
			Location:  problem.LocationBody,
			Parameter: "email",
		}))
		return
	}

	now := time.Now()
	if h != nil && h.clock != nil {
		now = h.clock()
	}

	inv, _, err := h.repo.CreateInvitation(c.Request.Context(), access.CreateInvitationParams{
		CourseID:          body.CourseID,
		Email:             email,
		AdminAccountID:    adminAccountID,
		AdminNote:         body.AdminNote,
		ExternalReference: body.ExternalReference,
		Now:               now,
	})
	if err != nil {
		if errors.Is(err, access.ErrCourseNotFound) {
			writeProblem(c, problem.NotFound())
			return
		}
		if errors.Is(err, access.ErrIneligibleRecipient) {
			writeProblem(c, problem.New(http.StatusConflict, "ineligible-recipient",
				"Ineligible recipient",
				"The target email belongs to an account that is ineligible for course access invitations."))
			return
		}
		if errors.Is(err, access.ErrDuplicateInvitation) {
			writeProblem(c, problem.New(http.StatusConflict, "duplicate-invitation",
				"Duplicate invitation",
				"A non-terminal invitation already exists for this email and course."))
			return
		}
		writeProblem(c, problem.Internal(""))
		return
	}

	c.JSON(http.StatusCreated, inv)
}

func (h *accessHandlers) listAdminCourseAccessInvitations(c *gin.Context) {
	var filter access.ListAdminInvitationsFilter

	if stateStr := c.Query("state"); stateStr != "" {
		st := access.State(stateStr)
		if st.Valid() {
			filter.State = &st
		}
	}
	if courseIDStr := c.Query("course_id"); courseIDStr != "" {
		filter.CourseID = &courseIDStr
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	filter.Limit = limit
	filter.Offset = (page - 1) * limit

	invitations, total, err := h.repo.ListAdminInvitations(c.Request.Context(), filter)
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}

	c.JSON(http.StatusOK, adminInvitationListResponse{
		Invitations: invitations,
		Total:       total,
		Page:        page,
		Limit:       limit,
	})
}

func (h *accessHandlers) listStudentCourseAccessInvitations(c *gin.Context) {
	studentAccountID := c.GetString(ctxUserIDKey)

	invitations, err := h.repo.ListStudentInvitations(c.Request.Context(), studentAccountID)
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}

	projections := make([]access.StudentInvitation, 0, len(invitations))
	for _, inv := range invitations {
		projections = append(projections, inv.ToStudentProjection())
	}

	c.JSON(http.StatusOK, studentInvitationListResponse{
		Invitations: projections,
	})
}

func (h *accessHandlers) getStudentCourseAccessInvitation(c *gin.Context) {
	studentAccountID := c.GetString(ctxUserIDKey)
	id := c.Param("id")

	if _, err := uuid.Parse(id); err != nil {
		writeProblem(c, problem.NotFound())
		return
	}

	inv, err := h.repo.GetStudentInvitationByID(c.Request.Context(), id, studentAccountID)
	if err != nil {
		if errors.Is(err, access.ErrInvitationNotFound) {
			writeProblem(c, problem.NotFound())
			return
		}
		writeProblem(c, problem.Internal(""))
		return
	}

	c.JSON(http.StatusOK, inv.ToStudentProjection())
}

func (h *accessHandlers) acceptStudentCourseAccessInvitation(c *gin.Context) {
	studentAccountID := c.GetString(ctxUserIDKey)
	id := c.Param("id")

	if _, err := uuid.Parse(id); err != nil {
		writeProblem(c, problem.NotFound())
		return
	}

	body := c.MustGet(strictJSONBodyContextKey).(*acceptInvitationBody)

	if strings.TrimSpace(body.AcceptanceToken) == "" {
		writeProblem(c, problem.New(http.StatusGone, "acceptance-link-expired",
			"Acceptance link expired",
			"This course access invitation link has expired, been consumed, or superseded."))
		return
	}

	now := time.Now()
	if h != nil && h.clock != nil {
		now = h.clock()
	}

	inv, err := h.repo.AcceptInvitation(c.Request.Context(), access.AcceptInvitationParams{
		InvitationID:    id,
		AcceptanceToken: body.AcceptanceToken,
		CallerAccountID: studentAccountID,
		Now:             now,
	})
	if err != nil {
		if errors.Is(err, access.ErrInvitationNotFound) {
			writeProblem(c, problem.NotFound())
			return
		}
		if errors.Is(err, access.ErrInvitationStateConflict) {
			writeProblem(c, problem.New(http.StatusConflict, "invitation-state-conflict",
				"Invitation state conflict",
				"The invitation is not in the required state for this operation."))
			return
		}
		if errors.Is(err, access.ErrAcceptanceTokenExpired) {
			writeProblem(c, problem.New(http.StatusGone, "acceptance-link-expired",
				"Acceptance link expired",
				"This course access invitation link has expired, been consumed, or superseded."))
			return
		}
		writeProblem(c, problem.Internal(""))
		return
	}

	c.JSON(http.StatusOK, inv.ToStudentProjection())
}

func (h *accessHandlers) getStudentCourseAccessHistory(c *gin.Context) {
	studentAccountID := c.GetString(ctxUserIDKey)

	invitations, err := h.repo.ListStudentInvitations(c.Request.Context(), studentAccountID)
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}

	projections := make([]access.StudentInvitation, 0, len(invitations))
	for _, inv := range invitations {
		projections = append(projections, inv.ToStudentProjection())
	}

	c.JSON(http.StatusOK, studentInvitationListResponse{
		Invitations: projections,
	})
}
