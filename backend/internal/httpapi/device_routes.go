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

const deviceRequestBodyLimit int64 = 1024

// deviceCommands is the device authority as the HTTP layer needs it.
type deviceCommands interface {
	Overview(ctx context.Context, accountID, currentDeviceID string, now time.Time) (identity.DeviceOverview, error)
	CompleteTrust(ctx context.Context, request identity.DeviceTrustRequest) (identity.DeviceTrustResult, error)
	ResendForAccount(ctx context.Context, accountID, userAgent, requestID string) (identity.DeviceChallenge, error)
	AdoptForSession(ctx context.Context, accountID, sessionID string, device identity.DeviceContext, requestID string) (identity.DeviceAdmissionResult, error)
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

type deviceTrustBody struct {
	Code string `json:"code" binding:"required"`
	// ReplaceDeviceID is supplied only on the third-device path, after the
	// Student has been shown their devices and picked one to remove.
	ReplaceDeviceID string `json:"replace_device_id"`
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
	meMutation.POST("/trust", strictJSONMiddleware(func() any { return &deviceTrustBody{} }, deviceRequestBodyLimit), h.trust)
	meMutation.POST("/trust/resend", h.resend)
	meMutation.POST("/adopt", h.adopt)
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

// trust completes the emailed challenge for this browser.
//
// The device being trusted is not in the body. It is read from the challenge
// and then proven by the device cookie, so a caller cannot aim somebody else's
// code at their own browser.
func (h *deviceHandlers) trust(c *gin.Context) {
	body := c.MustGet(strictJSONBodyContextKey).(*deviceTrustBody)
	digest := readDeviceCredential(c.Request)
	if digest == "" {
		// No usable device credential means there is nothing this code could
		// trust. Deliberately answered as an invalid code rather than as a
		// missing cookie, so the two cannot be told apart by probing.
		writeProblem(c, problem.ValidationFailed())
		return
	}
	result, err := h.foundation.devices.CompleteTrust(c.Request.Context(), identity.DeviceTrustRequest{
		AccountID:       c.GetString(ctxUserIDKey),
		SessionID:       sessionIDFrom(c),
		PresentedDigest: digest,
		ChallengeID:     challengeIDFrom(c),
		Code:            body.Code,
		ReplaceDeviceID: body.ReplaceDeviceID,
		RequestID:       requestid.FromContext(c.Request.Context()),
	})
	if err != nil {
		writeDeviceError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"state":             string(identity.DeviceTrustEstablished),
		"replaced_device":   result.ReplacedDeviceID != "",
		"revoked_sessions":  result.RevokedSessions,
		"device_registered": true,
	})
}

func (h *deviceHandlers) resend(c *gin.Context) {
	challenge, err := h.foundation.devices.ResendForAccount(
		c.Request.Context(), c.GetString(ctxUserIDKey),
		c.GetHeader("User-Agent"), requestid.FromContext(c.Request.Context()),
	)
	if err != nil {
		writeDeviceError(c, err)
		return
	}
	writeDeviceChallenge(c, http.StatusOK, challenge)
}

// adopt binds a session that predates device policy to a device.
func (h *deviceHandlers) adopt(c *gin.Context) {
	pendingDevice, err := pendingDeviceCredentialFor(c)
	if err != nil {
		writeProblem(c, problem.AuthenticationUnavailable())
		return
	}
	result, err := h.foundation.devices.AdoptForSession(
		c.Request.Context(), c.GetString(ctxUserIDKey), sessionIDFrom(c),
		deviceContextFrom(c, pendingDevice.digest), requestid.FromContext(c.Request.Context()),
	)
	if err != nil {
		writeDeviceError(c, err)
		return
	}
	pendingDevice.commit(c)
	c.Header("Cache-Control", "no-store")
	response := gin.H{
		"state":     string(result.TrustState),
		"admission": string(result.Admission),
	}
	if result.Challenge != nil {
		response["challenge"] = deviceChallengeBody(*result.Challenge)
	}
	c.JSON(http.StatusOK, response)
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

// challengeIDFrom reads the challenge this browser is answering.
//
// It is a header rather than a body field so the same value can be sent on the
// resend route, which has no body, and it is not a secret: holding it proves
// nothing without the mailed code, and the server still checks that it names
// the Account's one live challenge.
func challengeIDFrom(c *gin.Context) string {
	return c.GetHeader("X-Gradex-Device-Challenge")
}

func deviceChallengeBody(challenge identity.DeviceChallenge) gin.H {
	return gin.H{
		"challenge_id":        challenge.ChallengeID,
		"masked_email":        challenge.MaskedEmail,
		"expires_at":          challenge.ExpiresAt.UTC().Format(time.RFC3339),
		"resend_available_at": challenge.ResendAvailableAt.UTC().Format(time.RFC3339),
	}
}

func writeDeviceChallenge(c *gin.Context, status int, challenge identity.DeviceChallenge) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"challenge": deviceChallengeBody(challenge)})
}

// writeDeviceError maps the device authority's vocabulary onto the response
// classes. The mapping is deliberately explicit rather than defaulting to a
// specific answer: an unrecognized failure is an internal fault and must not be
// reported as a policy decision the Student can act on.
func writeDeviceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, identity.ErrOTPInvalid):
		writeProblem(c, problem.ValidationFailed())
	case errors.Is(err, identity.ErrOTPAttemptsExhausted):
		writeProblem(c, problem.RateLimited())
	case errors.Is(err, identity.ErrOTPResendTooSoon):
		writeProblem(c, problem.RateLimited())
	case errors.Is(err, identity.ErrDeviceLimitReached):
		writeProblem(c, problem.DeviceLimitReached())
	case errors.Is(err, identity.ErrDeviceReplacementCooldown):
		writeProblem(c, problem.DeviceReplacementCooldown())
	case errors.Is(err, identity.ErrDeviceUnknown):
		writeProblem(c, problem.DeviceNotFound())
	case errors.Is(err, identity.ErrDeliveryUnavailable):
		writeProblem(c, problem.TransactionalDeliveryUnavailable())
	case errors.Is(err, identity.ErrDeviceTrustUnavailable):
		writeProblem(c, problem.AuthenticationUnavailable())
	default:
		writeProblem(c, problem.Internal(""))
	}
}
