package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/media"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

type mediaDeliveryHandlers struct {
	delivery mediaDeliveryIssuer
	logger   *logging.Logger
}

type playbackAuthorizationBody struct {
	LessonID       string `json:"lesson_id" binding:"required"`
	AssetVersionID string `json:"asset_version_id" binding:"required"`
}

type downloadAuthorizationBody struct {
	LessonID       string          `json:"lesson_id" binding:"required"`
	AssetVersionID string          `json:"asset_version_id" binding:"required"`
	Kind           media.AssetKind `json:"kind" binding:"required"`
}

func mountMediaDeliveryRoutes(content *gin.RouterGroup, delivery mediaDeliveryIssuer, authenticator auth.Authenticator, principals identity.PrincipalResolver, logger *logging.Logger) {
	h := &mediaDeliveryHandlers{delivery: delivery, logger: logger}
	protected := content.Group("")
	protected.Use(requireProtectedLearningAccess(authenticator, principals, logger))
	protected.POST("/playback-authorizations", strictJSONMiddleware(func() any { return &playbackAuthorizationBody{} }, mediaRequestBodyLimit), h.playbackAuthorization)
	protected.POST("/download-authorizations", strictJSONMiddleware(func() any { return &downloadAuthorizationBody{} }, mediaRequestBodyLimit), h.downloadAuthorization)
	protected.GET("/lessons/:lessonId/materials/resource", func(c *gin.Context) { h.materialEntry(c, media.KindResource) })
	protected.GET("/lessons/:lessonId/materials/lab-material", func(c *gin.Context) { h.materialEntry(c, media.KindLabMaterial) })
	content.GET("/previews/:id", h.preview)
}

func (h *mediaDeliveryHandlers) playbackAuthorization(c *gin.Context) {
	body := c.MustGet(strictJSONBodyContextKey).(*playbackAuthorizationBody)
	issued, err := h.delivery.IssuePlayback(c.Request.Context(), media.PlaybackRequest{
		StudentID: c.GetString(ctxUserIDKey), LessonID: body.LessonID, AssetVersionID: body.AssetVersionID,
	})
	if err != nil {
		logProtectedDeliveryDenial(c, h.logger, err)
		writeProtectedUnavailable(c)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, issued)
}

func (h *mediaDeliveryHandlers) downloadAuthorization(c *gin.Context) {
	body := c.MustGet(strictJSONBodyContextKey).(*downloadAuthorizationBody)
	issued, err := h.delivery.IssueDownload(c.Request.Context(), media.DownloadRequest{
		StudentID: c.GetString(ctxUserIDKey), LessonID: body.LessonID, AssetVersionID: body.AssetVersionID, Kind: body.Kind,
	})
	if err != nil {
		logProtectedDeliveryDenial(c, h.logger, err)
		writeProtectedUnavailable(c)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, issued)
}

func (h *mediaDeliveryHandlers) materialEntry(c *gin.Context, kind media.AssetKind) {
	issued, err := h.delivery.IssueDownloadEntry(c.Request.Context(), media.DownloadEntryRequest{
		StudentID: c.GetString(ctxUserIDKey), LessonID: c.Param("lessonId"), Kind: kind,
	})
	if err != nil {
		logProtectedDeliveryDenial(c, h.logger, err)
		writeProtectedUnavailable(c)
		return
	}
	// The signed target is intentionally carried only in the redirect header.
	// It is never serialized into an S5 response body or persisted by the app.
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("Location", issued.URL)
	c.Status(http.StatusFound)
}

func (h *mediaDeliveryHandlers) preview(c *gin.Context) {
	issued, err := h.delivery.IssuePreview(c.Request.Context(), c.Param("id"))
	if err != nil {
		logProtectedDeliveryDenial(c, h.logger, err)
		writeProtectedUnavailable(c)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, issued)
}

func logProtectedDeliveryDenial(c *gin.Context, logger *logging.Logger, err error) {
	reason, ok := media.ProtectedDenialReason(err)
	if !ok || logger == nil {
		return
	}
	logger.AuthorizationDenied(logging.AuthorizationEvent{
		Method: c.Request.Method, RouteTemplate: c.FullPath(), Capability: string(identity.CapLearningAccess), DenyReason: string(reason),
	})
}

// requireProtectedLearningAccess keeps the Account/capability gate server-side
// but maps failures on this inventory-sensitive surface to the same refusal as
// a missing Asset. The evaluator still re-reads Account status immediately
// before signing, so no login-time decision is reused.
func requireProtectedLearningAccess(authenticator auth.Authenticator, principals identity.PrincipalResolver, logger *logging.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, err := authenticator.UserFromRequest(c)
		if err != nil {
			logProtectedLearningDenial(c, logger, identity.DenyPrincipalNotFound)
			writeProtectedUnavailable(c)
			return
		}
		principal, err := principals.ResolvePrincipal(c.Request.Context(), accountID)
		if err != nil {
			logProtectedLearningDenial(c, logger, identity.DenyPrincipalNotFound)
			writeProtectedUnavailable(c)
			return
		}
		decision := identity.Authorize(principal, identity.CapLearningAccess)
		if !decision.Allowed {
			logProtectedLearningDenial(c, logger, decision.Reason)
			writeProtectedUnavailable(c)
			return
		}
		c.Set(ctxUserIDKey, accountID)
		c.Set(ctxPrincipalKey, principal)
		c.Next()
	}
}

func logProtectedLearningDenial(c *gin.Context, logger *logging.Logger, reason identity.DenyReason) {
	if logger == nil {
		return
	}
	logger.AuthorizationDenied(logging.AuthorizationEvent{
		Method: c.Request.Method, RouteTemplate: c.FullPath(), Capability: string(identity.CapLearningAccess), DenyReason: string(reason),
	})
}

// writeProtectedUnavailable is the only external denial constructor for all
// protected media failures, including an absent exact Asset Version. Its fixed
// bytes, headers, and redirect absence prevent a content inventory oracle.
func writeProtectedUnavailable(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	// A fresh request identifier would make otherwise identical denials differ
	// on the wire. The request logger retains its trusted correlation value,
	// while this inventory-sensitive response deliberately has no identifier,
	// redirect, or cause-specific field.
	writeAnonymousProblem(c, problem.NotFound())
}
