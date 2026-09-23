package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/requestid"
)

// deviceCommands is the device authority as the HTTP layer needs it.
type deviceCommands interface {
	Overview(ctx context.Context, accountID, currentDeviceID string, now time.Time) (identity.DeviceOverview, error)
	Remove(ctx context.Context, request identity.RemoveRequest) error

	AdminOverview(ctx context.Context, accountID string, now time.Time) (identity.AdminDeviceOverview, error)
	AdminRevokeDevice(ctx context.Context, command identity.AdminDeviceCommand) error
	AdminRevokeAllDevices(ctx context.Context, command identity.AdminDeviceCommand) (int, error)
	AdminResetReplacementCooldown(ctx context.Context, command identity.AdminDeviceCommand) error
}

// DeviceFoundation is the validated dependency set for the device routes.
type DeviceFoundation struct {
	devices deviceCommands
	now     func() time.Time
}

func NewDeviceFoundation(devices deviceCommands, now func() time.Time) (*DeviceFoundation, error) {
	if devices == nil {
		return nil, errors.New("device foundation requires the device authority")
	}
	if now == nil {
		now = time.Now
	}
	return &DeviceFoundation{devices: devices, now: now}, nil
}

type deviceHandlers struct {
	foundation *DeviceFoundation
	logger     *logging.Logger
}

func mountDeviceRoutes(
	v1 *gin.RouterGroup,
	foundation *DeviceFoundation,
	sessionFoundation *SessionFoundation,
	authenticator auth.Authenticator,
	principals identity.PrincipalResolver,
	logger *logging.Logger,
) error {
	if foundation == nil || sessionFoundation == nil {
		return errors.New("device and session foundations are required")
	}
	h := &deviceHandlers{foundation: foundation, logger: logger}

	// Every route here is gated on CapDeviceManagement, which the policy grants
	// to a Student and refuses to every operator. An Admin cannot reach another
	// Account's devices through this surface at all; that authority lives under
	// CapSecurityOperations on the routes below.
	meRead := v1.Group("/me/devices",
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapDeviceManagement),
	)
	meRead.GET("", h.overview)
	meMutation := v1.Group("/me/devices",
		sessionFoundation.requireSessionMutationSecurity(),
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapDeviceManagement),
	)
	meMutation.DELETE("/:deviceId", h.remove)

	adminRead := v1.Group("/admin/students/:accountId/devices",
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapSecurityOperations),
	)
	adminRead.GET("", h.adminOverview)
	adminMutation := v1.Group("/admin/students/:accountId/devices",
		sessionFoundation.requireSessionMutationSecurity(),
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapSecurityOperations),
	)
	adminMutation.POST("/:deviceId/revocations", h.adminRevoke)
	adminMutation.POST("/revocations", h.adminRevokeAll)
	adminMutation.POST("/cooldown-resets", h.adminResetCooldown)
	return nil
}

func (h *deviceHandlers) overview(c *gin.Context) {
	overview, err := h.foundation.devices.Overview(
		c.Request.Context(), c.GetString(ctxUserIDKey),
		auth.TrustedDeviceFromContext(c), h.foundation.now().UTC(),
	)
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, overview)
}

// remove ends one of the Student's own devices.
//
// Removing the browser making the request is allowed. It ends this browser's
// sessions and clears its device credential, and the interface says so before
// the Student confirms — a Student whose only other device was lost or stolen
// needs exactly this to work, and refusing it would leave them with no route
// that does not involve a password reset.
func (h *deviceHandlers) remove(c *gin.Context) {
	if auth.DeviceTrustFromContext(c) != identity.DeviceTrustEstablished {
		// A password-authenticated pending browser may inspect the coarse list so
		// it can nominate a replacement inside OTP completion, but it may not
		// churn devices through the standalone removal command.
		writeProblem(c, problem.NotAuthorized())
		return
	}
	deviceID := c.Param("deviceId")
	removingCurrent := deviceID != "" && deviceID == auth.TrustedDeviceFromContext(c)
	if err := h.foundation.devices.Remove(c.Request.Context(), identity.RemoveRequest{
		AccountID: c.GetString(ctxUserIDKey),
		DeviceID:  deviceID,
		RequestID: requestid.FromContext(c.Request.Context()),
	}); err != nil {
		writeDeviceError(c, err)
		return
	}
	if removingCurrent {
		// The session families bound to this device are already revoked
		// server-side. Clearing both cookies stops the browser presenting
		// credentials that name records which no longer exist.
		auth.ClearSessionCookie(c.Writer)
		auth.ClearDeviceCookie(c.Writer)
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"removed": true, "signed_out": removingCurrent})
}

// Operator routes.

func (h *deviceHandlers) adminOverview(c *gin.Context) {
	overview, err := h.foundation.devices.AdminOverview(
		c.Request.Context(), c.Param("accountId"), h.foundation.now().UTC(),
	)
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, overview)
}

func (h *deviceHandlers) adminRevoke(c *gin.Context) {
	if err := h.foundation.devices.AdminRevokeDevice(c.Request.Context(), h.adminCommand(c)); err != nil {
		writeDeviceError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"revoked": 1})
}

func (h *deviceHandlers) adminRevokeAll(c *gin.Context) {
	revoked, err := h.foundation.devices.AdminRevokeAllDevices(c.Request.Context(), h.adminCommand(c))
	if err != nil {
		writeDeviceError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"revoked": revoked})
}

func (h *deviceHandlers) adminResetCooldown(c *gin.Context) {
	if err := h.foundation.devices.AdminResetReplacementCooldown(c.Request.Context(), h.adminCommand(c)); err != nil {
		writeDeviceError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"cooldown_cleared": true})
}

// adminCommand names the operator on every audited action. The actor is the
// authenticated Admin from the session, never a field in the request.
func (h *deviceHandlers) adminCommand(c *gin.Context) identity.AdminDeviceCommand {
	return identity.AdminDeviceCommand{
		AccountID: c.Param("accountId"),
		DeviceID:  c.Param("deviceId"),
		ActorID:   c.GetString(ctxUserIDKey),
		RequestID: requestid.FromContext(c.Request.Context()),
	}
}

// writeDeviceError maps the device authority's vocabulary onto the response
// classes. The mapping is deliberately explicit rather than defaulting to a
// specific answer: an unrecognized failure is an internal fault and must not be
// reported as a policy decision the Student can act on.
func writeDeviceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, identity.ErrDeviceLimitReached):
		writeProblem(c, problem.DeviceLimitReached())
	case errors.Is(err, identity.ErrDeviceReplacementCooldown):
		writeProblem(c, problem.DeviceReplacementCooldown())
	case errors.Is(err, identity.ErrDeviceUnknown):
		writeProblem(c, problem.DeviceNotFound())
	case errors.Is(err, identity.ErrDeviceTrustUnavailable):
		writeProblem(c, problem.AuthenticationUnavailable())
	default:
		writeProblem(c, problem.Internal(""))
	}
}
