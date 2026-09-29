package httpapi

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
)

func mountProfileRoutes(
	v1 *gin.RouterGroup,
	foundation *ProfileFoundation,
	sessionFoundation *SessionFoundation,
	authenticator auth.Authenticator,
	principals identity.PrincipalResolver,
	logger *logging.Logger,
) error {
	if foundation == nil || foundation.instructor == nil || foundation.account == nil || foundation.catalog == nil {
		return fmt.Errorf("profile foundation is incomplete")
	}
	if sessionFoundation == nil {
		return fmt.Errorf("session foundation is required for profile mutations")
	}
	if authenticator == nil || principals == nil {
		return fmt.Errorf("authentication dependencies are required for profile routes")
	}

	h := &profileHandlers{foundation: foundation}

	instructorRead := v1.Group("/me")
	instructorRead.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapContentManagement),
	)
	instructorRead.GET("/instructor-profile", h.getInstructorProfile)

	instructorMutation := v1.Group("/me")
	instructorMutation.Use(
		sessionFoundation.requireSessionMutationSecurity(),
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapContentManagement),
	)
	instructorMutation.PUT(
		"/instructor-profile",
		strictJSONMiddleware(func() any { return &instructorProfileDraftBody{} }, instructorProfileBodyLimit),
		h.saveInstructorProfile,
	)
	instructorMutation.POST(
		"/instructor-profile/submission",
		strictJSONMiddleware(func() any { return &instructorProfileSubmissionBody{} }, instructorProfileBodyLimit),
		h.submitInstructorProfile,
	)

	studentRead := v1.Group("/me")
	studentRead.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapLearningAccess),
	)
	studentRead.GET("/profile", h.getAccountProfile)

	studentMutation := v1.Group("/me")
	studentMutation.Use(
		sessionFoundation.requireSessionMutationSecurity(),
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapLearningAccess),
	)
	studentMutation.PUT(
		"/profile",
		strictJSONMiddleware(func() any { return &accountProfileBody{} }, instructorProfileBodyLimit),
		h.updateAccountProfile,
	)

	adminRead := v1.Group("/admin/instructor-profiles")
	adminRead.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapCatalogPublish),
	)
	adminRead.GET("", h.listInstructorProfiles)
	adminRead.GET("/:accountId", h.getAdminInstructorProfile)

	adminMutation := v1.Group("/admin/instructor-profiles")
	adminMutation.Use(
		sessionFoundation.requireSessionMutationSecurity(),
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapCatalogPublish),
	)
	adminMutation.POST(
		"/:accountId/approve",
		strictJSONMiddleware(func() any { return &moderationReasonBody{} }, instructorProfileBodyLimit),
		h.approveInstructorProfile,
	)
	adminMutation.POST(
		"/:accountId/request-changes",
		strictJSONMiddleware(func() any { return &moderationReasonBody{} }, instructorProfileBodyLimit),
		h.requestInstructorChanges,
	)
	adminMutation.POST(
		"/:accountId/hide",
		strictJSONMiddleware(func() any { return &moderationReasonBody{} }, instructorProfileBodyLimit),
		h.hideInstructorProfile,
	)

	publicCatalog := v1.Group("/catalog")
	publicCatalog.Use(publicCatalogCache())
	publicCatalog.GET("/instructors/:slug", h.publicInstructorProfile)
	return nil
}
