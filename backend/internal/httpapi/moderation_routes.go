package httpapi

import (
	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
)

func mountAdminReportRoutes(
	v1 *gin.RouterGroup,
	foundation *ModerationFoundation,
	sessionFoundation *SessionFoundation,
	authenticator auth.Authenticator,
	principals identity.PrincipalResolver,
	logger *logging.Logger,
) {
	handlers := &adminReportHandlers{reports: foundation.reports, catalog: foundation.catalog}

	readGroup := v1.Group("/admin/reports")
	readGroup.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapAdminOperations),
	)
	{
		readGroup.GET("", handlers.list)
		readGroup.GET("/:id", handlers.detail)
	}

	mutationGroup := v1.Group("/admin/reports/:id")
	mutationGroup.Use(
		sessionFoundation.requireSessionMutationSecurity(),
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapAdminOperations),
	)
	{
		mutationGroup.POST("/resolve",
			strictJSONMiddleware(func() any { return &adminReportResolutionBody{} }, moderationResolutionBodyLimit),
			handlers.resolve,
		)
	}
}
