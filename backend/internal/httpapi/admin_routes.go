package httpapi

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
)

func mountAdminRoutes(
	v1 *gin.RouterGroup,
	foundation *AdminFoundation,
	sessionFoundation *SessionFoundation,
	authenticator auth.Authenticator,
	principals identity.PrincipalResolver,
	logger *logging.Logger,
) error {
	if foundation == nil || foundation.service == nil {
		return fmt.Errorf("admin foundation is required to mount admin routes")
	}
	if authenticator == nil {
		return fmt.Errorf("authenticator is required to mount admin routes")
	}
	if principals == nil {
		return fmt.Errorf("principal resolver is required to mount admin routes")
	}
	h := &adminHandlers{service: foundation.service, userService: foundation.userService, recentAuthWindow: foundation.recentAuthWindow}
	readGroup := v1.Group("/admin")
	readGroup.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapUserDirectoryRead),
	)
	{
		readGroup.GET("/accounts", h.listAccounts)
		if foundation.userService != nil {
			readGroup.GET("/accounts/:accountId", h.getUser360)
			readGroup.GET("/accounts/:accountId/notes", h.listNotes)
			readGroup.GET("/accounts/:accountId/security-events", h.listSecurityEvents)
			readGroup.GET("/accounts/:accountId/course-options", h.listCourseOptions)
			readGroup.GET("/accounts/:accountId/access-diagnostics", h.diagnoseAccess)
		} else {
			readGroup.GET("/accounts/:accountId", h.getAccount)
		}
		readGroup.GET("/audit-events", h.listAuditEvents)
	}
	if foundation.userService != nil && sessionFoundation != nil {
		notesMutation := v1.Group("/admin/accounts/:accountId/notes")
		notesMutation.Use(
			sessionFoundation.requireSessionMutationSecurity(),
			requireAuth(authenticator),
			requireCapability(principals, logger, identity.CapUserAdministration),
			foundation.requireAdminRateDecision("admin-notes"),
		)
		notesMutation.POST("", strictJSONMiddleware(func() any { return &adminNoteBody{} }, adminNoteBodyLimit), h.addNote)

		sessionMutation := v1.Group("/admin/accounts/:accountId/session-revocations")
		sessionMutation.Use(
			sessionFoundation.requireSessionMutationSecurity(),
			requireAuth(authenticator),
			requireCapability(principals, logger, identity.CapSecurityOperations),
			foundation.requireAdminRateDecision("admin-session-revocations"),
		)
		sessionMutation.POST("", strictJSONMiddleware(func() any { return &sessionRevocationBody{} }, adminMutationBodyLimit), h.revokeSessions)
	}
	return nil
}
