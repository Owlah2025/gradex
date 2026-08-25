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
	repo                *access.Repository
	clock               func() time.Time
	salesWhatsAppNumber string
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

type adjustEntitlementExpiryBody struct {
	Date             string `json:"date"`
	Reason           string `json:"reason"`
	SupportReference string `json:"support_reference"`
	ExpectedRevision int64  `json:"expected_revision"`
}

type revokeEntitlementBody struct {
	Reason           string `json:"reason"`
	SupportReference string `json:"support_reference"`
	ExpectedRevision int64  `json:"expected_revision"`
}

type rejectInvitationBody struct {
	Reason string `json:"reason"`
}

type createPurchaseRequestBody struct {
	CourseID string `json:"course_id"`
	Email    string `json:"email"`
}

type createPurchaseRequestResponse struct {
	Reference   string `json:"reference"`
	WhatsAppURL string `json:"whatsapp_url"`
}

type adminPurchaseRequestListResponse struct {
	PurchaseRequests []access.PurchaseRequest `json:"purchase_requests"`
	Total            int                      `json:"total"`
	Page             int                      `json:"page"`
	Limit            int                      `json:"limit"`
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
	admissionFoundation *AdmissionFoundation,
	sessionFoundation *SessionFoundation,
	authenticator auth.Authenticator,
	principals identity.PrincipalResolver,
	logger *logging.Logger,
) error {
	if foundation == nil || foundation.repository == nil || admissionFoundation == nil {
		return errors.New("access and admission foundations are required")
	}

	clock := foundation.clock
	if clock == nil {
		clock = time.Now
	}

	h := &accessHandlers{
		repo: foundation.repository, clock: clock, salesWhatsAppNumber: foundation.salesWhatsAppNumber,
	}

	// A purchase request is public by design. It creates no authority for the
	// caller and accepts no client-owned price or payment state.
	v1.POST("/purchase-requests",
		strictJSONMiddleware(func() any { return &createPurchaseRequestBody{} }, accessMutationBodyLimit),
		admissionFoundation.security.requireAdmission(),
		admissionFoundation.requireRateDecision("purchase-requests", purchaseRequestIdentifier),
		h.createPurchaseRequest,
	)

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
		adminAccessGroup.POST("/course-access-invitations/:id/approve",
			h.approveCourseAccessInvitation,
		)
		adminAccessGroup.POST("/course-access-invitations/:id/reject",
			strictJSONMiddleware(func() any { return &rejectInvitationBody{} }, accessMutationBodyLimit),
			h.rejectCourseAccessInvitation,
		)
		adminAccessGroup.POST("/course-access-invitations/:id/cancel",
			h.cancelCourseAccessInvitation,
		)
		adminAccessGroup.POST("/course-access-invitations/:id/resend",
			h.resendCourseAccessInvitation,
		)
		adminAccessGroup.POST("/purchase-requests/:id/confirm-payment",
			h.confirmPurchaseRequestPayment,
		)
		adminAccessGroup.POST("/purchase-requests/:id/cancel",
			h.cancelPurchaseRequest,
		)
		// AD07 elevated-Admin entitlement operations (BR-026). Neither route can
		// mint an Entitlement: both address one that Admin Approval already
		// created.
		adminAccessGroup.PUT("/entitlements/:id/expiry",
			strictJSONMiddleware(func() any { return &adjustEntitlementExpiryBody{} }, accessMutationBodyLimit),
			h.adjustEntitlementExpiry,
		)
		adminAccessGroup.POST("/entitlements/:id/revocation",
			strictJSONMiddleware(func() any { return &revokeEntitlementBody{} }, accessMutationBodyLimit),
			h.revokeEntitlement,
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
		adminReadGroup.GET("/purchase-requests", h.listAdminPurchaseRequests)
		adminReadGroup.GET("/entitlements/:id", h.getAdminEntitlement)
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

func (h *accessHandlers) createPurchaseRequest(c *gin.Context) {
	body := c.MustGet(strictJSONBodyContextKey).(*createPurchaseRequestBody)
	if _, err := uuid.Parse(body.CourseID); err != nil {
		writeProblem(c, problem.NotFound())
		return
	}
	if _, err := identity.NormalizeEmail(body.Email); err != nil {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "INVALID_EMAIL", Detail: "A valid email address is required",
			Location: problem.LocationBody, Parameter: "email",
		}))
		return
	}
	locale, ok := requestedLocale(c.GetHeader("Accept-Language"))
	if !ok {
		writeProblem(c, problem.ValidationFailed())
		return
	}
	now := time.Now()
	if h != nil && h.clock != nil {
		now = h.clock()
	}
	request, err := h.repo.CreatePurchaseRequest(c.Request.Context(), access.CreatePurchaseRequestParams{
		CourseID: body.CourseID, Email: body.Email, Now: now,
	})
	if err != nil {
		if errors.Is(err, access.ErrCourseNotPurchasable) {
			writeProblem(c, problem.NotFound())
			return
		}
		if errors.Is(err, access.ErrInvalidEmail) {
			writeProblem(c, problem.ValidationFailed())
			return
		}
		writeProblem(c, problem.Internal(""))
		return
	}
	handoff, err := access.WhatsAppHandoffURL(h.salesWhatsAppNumber, request, locale)
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}
	c.JSON(http.StatusCreated, createPurchaseRequestResponse{Reference: request.ReferenceCode, WhatsAppURL: handoff})
}

func (h *accessHandlers) listAdminPurchaseRequests(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit < 1 || limit > 100 {
		limit = 50
	}
	filter := access.ListPurchaseRequestsFilter{Query: c.Query("q"), Limit: limit, Offset: (page - 1) * limit}
	if state := access.PurchaseRequestState(c.Query("state")); state.Valid() {
		filter.State = &state
	}
	requests, total, err := h.repo.ListPurchaseRequests(c.Request.Context(), filter)
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}
	locale, _ := requestedLocale(c.GetHeader("Accept-Language"))
	for index := range requests {
		if locale == identity.LocaleArabic {
			requests[index].CourseTitle = requests[index].CourseTitleAr
		} else {
			requests[index].CourseTitle = requests[index].CourseTitleEn
		}
	}
	c.JSON(http.StatusOK, adminPurchaseRequestListResponse{PurchaseRequests: requests, Total: total, Page: page, Limit: limit})
}

func (h *accessHandlers) confirmPurchaseRequestPayment(c *gin.Context) {
	if _, err := uuid.Parse(c.Param("id")); err != nil {
		writeProblem(c, problem.NotFound())
		return
	}
	locale, ok := requestedLocale(c.GetHeader("Accept-Language"))
	if !ok {
		writeProblem(c, problem.ValidationFailed())
		return
	}
	now := time.Now()
	if h != nil && h.clock != nil {
		now = h.clock()
	}
	result, err := h.repo.ConfirmPurchaseRequest(c.Request.Context(), access.ConfirmPurchaseRequestParams{
		PurchaseRequestID: c.Param("id"), AdminAccountID: c.GetString(ctxUserIDKey), Locale: locale, Now: now,
	})
	if err != nil {
		switch {
		case errors.Is(err, access.ErrPurchaseRequestNotFound), errors.Is(err, access.ErrCourseNotPurchasable):
			writeProblem(c, problem.NotFound())
		case errors.Is(err, access.ErrExpiryRequired):
			// The request itself is still eligible; the Course simply has no valid
			// future default access expiry yet, so the Admin is told what to fix.
			writeProblem(c, problem.New(http.StatusConflict, "course-default-access-expiry-required", "Course access expiry is not configured", "Set the Course access expiry before confirming payment."))
		case errors.Is(err, access.ErrPurchaseRequestTransition), errors.Is(err, access.ErrDuplicateInvitation):
			writeProblem(c, problem.New(http.StatusConflict, "purchase-request-state-conflict", "Purchase request cannot be confirmed", "The purchase request is no longer eligible for this action."))
		case errors.Is(err, access.ErrIneligibleRecipient):
			writeProblem(c, problem.New(http.StatusConflict, "ineligible-recipient", "Ineligible recipient", "The recipient account does not satisfy recipient eligibility."))
		default:
			writeProblem(c, problem.Internal(""))
		}
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *accessHandlers) cancelPurchaseRequest(c *gin.Context) {
	if _, err := uuid.Parse(c.Param("id")); err != nil {
		writeProblem(c, problem.NotFound())
		return
	}
	now := time.Now()
	if h != nil && h.clock != nil {
		now = h.clock()
	}
	request, err := h.repo.CancelPurchaseRequest(c.Request.Context(), access.CancelPurchaseRequestParams{
		PurchaseRequestID: c.Param("id"), AdminAccountID: c.GetString(ctxUserIDKey), Now: now,
	})
	if err != nil {
		switch {
		case errors.Is(err, access.ErrPurchaseRequestNotFound):
			writeProblem(c, problem.NotFound())
		case errors.Is(err, access.ErrPurchaseRequestTransition):
			writeProblem(c, problem.New(http.StatusConflict, "purchase-request-state-conflict", "Purchase request cannot be cancelled", "The purchase request is no longer eligible for this action."))
		default:
			writeProblem(c, problem.Internal(""))
		}
		return
	}
	c.JSON(http.StatusOK, request)
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

	if _, err := identity.NormalizeEmail(body.Email); err != nil {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code:      "INVALID_EMAIL",
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
	locale, ok := requestedLocale(c.GetHeader("Accept-Language"))
	if !ok {
		writeProblem(c, problem.ValidationFailed())
		return
	}

	inv, _, err := h.repo.CreateInvitation(c.Request.Context(), access.CreateInvitationParams{
		CourseID:          body.CourseID,
		Email:             body.Email,
		AdminAccountID:    adminAccountID,
		AdminNote:         body.AdminNote,
		ExternalReference: body.ExternalReference,
		Locale:            locale,
		Now:               now,
	})
	if err != nil {
		if errors.Is(err, access.ErrCourseNotFound) {
			writeProblem(c, problem.NotFound())
			return
		}
		if errors.Is(err, access.ErrInvalidEmail) {
			writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
				Code:      "INVALID_EMAIL",
				Detail:    "A valid email address is required",
				Location:  problem.LocationBody,
				Parameter: "email",
			}))
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

func (h *accessHandlers) approveCourseAccessInvitation(c *gin.Context) {
	adminAccountID := c.GetString(ctxUserIDKey)
	id := c.Param("id")

	if _, err := uuid.Parse(id); err != nil {
		writeProblem(c, problem.NotFound())
		return
	}

	now := time.Now()
	if h != nil && h.clock != nil {
		now = h.clock()
	}

	var session identity.Session
	if val, ok := c.Get("authenticated_session"); ok {
		if s, ok := val.(identity.Session); ok {
			session = s
		}
	}

	if !session.AuthenticatedAt.IsZero() {
		if err := identity.CheckRecentAuthentication(session, 15*time.Minute, now); err != nil {
			writeProblem(c, problem.New(http.StatusForbidden, "recent-authentication-required",
				"Recent authentication required", "This operation requires recent authentication"))
			return
		}
	}

	res, err := h.repo.ApproveInvitation(c.Request.Context(), access.ApproveInvitationParams{
		InvitationID:   id,
		AdminAccountID: adminAccountID,
		Now:            now,
	})
	if err != nil {
		if errors.Is(err, access.ErrInvitationNotFound) || errors.Is(err, access.ErrCourseNotFound) {
			writeProblem(c, problem.NotFound())
			return
		}
		if errors.Is(err, access.ErrInvitationStateConflict) {
			writeProblem(c, problem.New(http.StatusConflict, "invitation-state-conflict",
				"Invitation state conflict", "Invitation is not in decision-ready state"))
			return
		}
		if errors.Is(err, access.ErrAlreadyHasActiveAccess) {
			writeProblem(c, problem.New(http.StatusConflict, "already-has-active-access",
				"Already has active access", "Student already holds active access entitlement for this course"))
			return
		}
		if errors.Is(err, access.ErrCourseNotGrantable) {
			writeProblem(c, problem.New(http.StatusConflict, "course-not-grantable",
				"Course not grantable", "Course is not in a grantable lifecycle state"))
			return
		}
		if errors.Is(err, access.ErrExpiryRequired) || errors.Is(err, access.ErrExpiryInPast) {
			writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
				Code:      "MISSING_DEFAULT_ACCESS_EXPIRY",
				Detail:    "Course has no valid future default access expiry instant configured",
				Location:  problem.LocationBody,
				Parameter: "default_access_ends_at",
			}))
			return
		}
		if errors.Is(err, access.ErrIneligibleRecipient) {
			writeProblem(c, problem.New(http.StatusConflict, "ineligible-recipient",
				"Ineligible recipient", "The recipient account does not satisfy recipient eligibility"))
			return
		}
		writeProblem(c, problem.Internal(""))
		return
	}

	c.JSON(http.StatusOK, res)
}

func (h *accessHandlers) rejectCourseAccessInvitation(c *gin.Context) {
	adminAccountID := c.GetString(ctxUserIDKey)
	id := c.Param("id")

	if _, err := uuid.Parse(id); err != nil {
		writeProblem(c, problem.NotFound())
		return
	}

	body := c.MustGet(strictJSONBodyContextKey).(*rejectInvitationBody)

	now := time.Now()
	if h != nil && h.clock != nil {
		now = h.clock()
	}

	inv, err := h.repo.RejectInvitation(c.Request.Context(), access.RejectInvitationParams{
		InvitationID:   id,
		AdminAccountID: adminAccountID,
		Reason:         body.Reason,
		Now:            now,
	})
	if err != nil {
		if errors.Is(err, access.ErrInvitationNotFound) {
			writeProblem(c, problem.NotFound())
			return
		}
		if errors.Is(err, access.ErrReasonRequired) {
			writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
				Code:      "REASON_REQUIRED",
				Detail:    "A non-empty decision reason is required for rejection",
				Location:  problem.LocationBody,
				Parameter: "reason",
			}))
			return
		}
		if errors.Is(err, access.ErrInvitationStateConflict) {
			writeProblem(c, problem.New(http.StatusConflict, "invitation-state-conflict",
				"Invitation state conflict", "Invitation is not in decision-ready state"))
			return
		}
		writeProblem(c, problem.Internal(""))
		return
	}

	c.JSON(http.StatusOK, inv)
}

func (h *accessHandlers) cancelCourseAccessInvitation(c *gin.Context) {
	adminAccountID := c.GetString(ctxUserIDKey)
	id := c.Param("id")

	if _, err := uuid.Parse(id); err != nil {
		writeProblem(c, problem.NotFound())
		return
	}

	now := time.Now()
	if h != nil && h.clock != nil {
		now = h.clock()
	}

	inv, err := h.repo.CancelInvitation(c.Request.Context(), access.CancelInvitationParams{
		InvitationID:   id,
		AdminAccountID: adminAccountID,
		Now:            now,
	})
	if err != nil {
		if errors.Is(err, access.ErrInvitationNotFound) {
			writeProblem(c, problem.NotFound())
			return
		}
		if errors.Is(err, access.ErrInvitationStateConflict) {
			writeProblem(c, problem.New(http.StatusConflict, "invitation-state-conflict",
				"Invitation state conflict", "Terminal invitation cannot be cancelled"))
			return
		}
		writeProblem(c, problem.Internal(""))
		return
	}

	c.JSON(http.StatusOK, inv)
}

func (h *accessHandlers) resendCourseAccessInvitation(c *gin.Context) {
	adminAccountID := c.GetString(ctxUserIDKey)
	id := c.Param("id")

	if _, err := uuid.Parse(id); err != nil {
		writeProblem(c, problem.NotFound())
		return
	}

	now := time.Now()
	if h != nil && h.clock != nil {
		now = h.clock()
	}

	inv, _, err := h.repo.ResendInvitation(c.Request.Context(), access.ResendInvitationParams{
		InvitationID:   id,
		AdminAccountID: adminAccountID,
		Now:            now,
	})
	if err != nil {
		if errors.Is(err, access.ErrInvitationNotFound) {
			writeProblem(c, problem.NotFound())
			return
		}
		if errors.Is(err, access.ErrInvitationStateConflict) {
			writeProblem(c, problem.New(http.StatusConflict, "invitation-state-conflict",
				"Invitation state conflict", "Accepted or terminal invitation cannot be reissued"))
			return
		}
		writeProblem(c, problem.Internal(""))
		return
	}

	c.JSON(http.StatusOK, inv)
}

func (h *accessHandlers) getAdminEntitlement(c *gin.Context) {
	id := c.Param("id")

	if _, err := uuid.Parse(id); err != nil {
		writeProblem(c, problem.NotFound())
		return
	}

	ent, err := h.repo.GetAdminEntitlementByID(c.Request.Context(), id)
	if err != nil {
		writeProblem(c, problem.NotFound())
		return
	}

	c.JSON(http.StatusOK, ent)
}

// requireRecentAdminAuthentication mirrors the approval guard: an elevated
// operation on existing access is refused, not degraded, when the session is
// no longer recently authenticated.
func requireRecentAdminAuthentication(c *gin.Context, now time.Time) bool {
	var session identity.Session
	if val, ok := c.Get("authenticated_session"); ok {
		if s, ok := val.(identity.Session); ok {
			session = s
		}
	}
	if session.AuthenticatedAt.IsZero() {
		return true
	}
	if err := identity.CheckRecentAuthentication(session, 15*time.Minute, now); err != nil {
		writeProblem(c, problem.New(http.StatusForbidden, "recent-authentication-required",
			"Recent authentication required", "This operation requires recent authentication"))
		return false
	}
	return true
}

// writeEntitlementMutationProblem maps the elevated-Admin entitlement errors
// onto the existing problem classes. No new error architecture is introduced.
func writeEntitlementMutationProblem(c *gin.Context, err error) {
	switch {
	case errors.Is(err, access.ErrEntitlementNotFound):
		writeProblem(c, problem.NotFound())
	case errors.Is(err, access.ErrEntitlementRevoked):
		writeProblem(c, problem.New(http.StatusConflict, "entitlement-revoked",
			"Entitlement already revoked", "This access grant is already revoked and cannot be changed"))
	case errors.Is(err, access.ErrEntitlementStale):
		writeProblem(c, problem.New(http.StatusConflict, "entitlement-stale",
			"Entitlement changed", "This access grant changed since it was loaded; reload it and try again"))
	case errors.Is(err, access.ErrReasonRequired):
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code:      "REASON_REQUIRED",
			Detail:    "Reason is required",
			Location:  problem.LocationBody,
			Parameter: "reason",
		}))
	case errors.Is(err, access.ErrExpiryRequired):
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code:      "DATE_REQUIRED",
			Detail:    "Expiry date (YYYY-MM-DD) is required",
			Location:  problem.LocationBody,
			Parameter: "date",
		}))
	default:
		writeProblem(c, problem.Internal(""))
	}
}

func optionalReference(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// adjustEntitlementExpiry extends or shortens one existing grant. Both
// directions are the same audited adjustment: a later instant extends access,
// an earlier one shortens it, and an instant already past ends it immediately
// (BR-026).
func (h *accessHandlers) adjustEntitlementExpiry(c *gin.Context) {
	adminAccountID := c.GetString(ctxUserIDKey)
	id := c.Param("id")

	if _, err := uuid.Parse(id); err != nil {
		writeProblem(c, problem.NotFound())
		return
	}

	body := c.MustGet(strictJSONBodyContextKey).(*adjustEntitlementExpiryBody)

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
	if !requireRecentAdminAuthentication(c, now) {
		return
	}

	newExpiry, err := access.ConvertKuwaitDateToUTCBoundary(body.Date)
	if err != nil {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code:      "INVALID_DATE_FORMAT",
			Detail:    "Date must be a valid Kuwait local date in YYYY-MM-DD format",
			Location:  problem.LocationBody,
			Parameter: "date",
		}))
		return
	}

	detail, err := h.repo.AdjustEntitlementExpiry(c.Request.Context(), access.AdjustEntitlementExpiryParams{
		EntitlementID:    id,
		AdminAccountID:   adminAccountID,
		ActorDescriptor:  adminAccountID,
		NewAccessEndsAt:  newExpiry,
		Reason:           body.Reason,
		SupportReference: optionalReference(body.SupportReference),
		ExpectedRevision: body.ExpectedRevision,
		Now:              now,
	})
	if err != nil {
		writeEntitlementMutationProblem(c, err)
		return
	}

	c.JSON(http.StatusOK, detail)
}

func (h *accessHandlers) revokeEntitlement(c *gin.Context) {
	adminAccountID := c.GetString(ctxUserIDKey)
	id := c.Param("id")

	if _, err := uuid.Parse(id); err != nil {
		writeProblem(c, problem.NotFound())
		return
	}

	body := c.MustGet(strictJSONBodyContextKey).(*revokeEntitlementBody)

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
	if !requireRecentAdminAuthentication(c, now) {
		return
	}

	detail, err := h.repo.RevokeEntitlement(c.Request.Context(), access.RevokeEntitlementParams{
		EntitlementID:    id,
		AdminAccountID:   adminAccountID,
		ActorDescriptor:  adminAccountID,
		Reason:           body.Reason,
		SupportReference: optionalReference(body.SupportReference),
		ExpectedRevision: body.ExpectedRevision,
		Now:              now,
	})
	if err != nil {
		writeEntitlementMutationProblem(c, err)
		return
	}

	c.JSON(http.StatusOK, detail)
}

func (h *accessHandlers) getStudentCourseAccessHistory(c *gin.Context) {
	studentAccountID := c.GetString(ctxUserIDKey)

	history, err := h.repo.GetStudentAccessHistory(c.Request.Context(), studentAccountID)
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}

	// The Course is named in the Student's language at the boundary, the same way the learning read
	// models resolve their titles. The authored pair never leaves the process.
	for i := range history.Items {
		history.Items[i].CourseTitle = localizedLearningTitle(
			c, history.Items[i].CourseTitleAr, history.Items[i].CourseTitleEn,
		)
	}
	if strings.TrimSpace(c.GetHeader("Accept-Language")) != "" {
		appendVary(c, "Accept-Language")
	}

	c.JSON(http.StatusOK, history)
}
