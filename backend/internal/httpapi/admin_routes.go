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
	h := &adminHandlers{service: foundation.service}
	readGroup := v1.Group("/admin")
	readGroup.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapUserDirectoryRead),
	)
	{
		readGroup.GET("/accounts", h.listAccounts)
		readGroup.GET("/accounts/:accountId", h.getAccount)
		readGroup.GET("/audit-events", h.listAuditEvents)
	}
	return nil
}
