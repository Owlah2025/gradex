package httpapi

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/media"
	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

type mediaDeliveryHandlers struct {
	delivery          mediaDeliveryIssuer
	logger            *logging.Logger
	rateLimiter       *ratelimit.Limiter
	previewRatePolicy ratelimit.Policy
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

// lessonFileDownloadAuthorizationResponse contains only the temporary object
// capability the browser needs. BuyerTag is present only for a Lab Material
// and is the existing opaque per-Entitlement BR-103 marker; it is never an
// entitlement or Student identifier. Attachment, asset-version, storage,
// scan, and entitlement internals are deliberately absent.
type lessonFileDownloadAuthorizationResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
	BuyerTag  string    `json:"buyer_tag,omitempty"`
}

// publicPreviewAuthorizationBody deliberately excludes the internal Asset
// Version identifier. Course Details needs an expiring media URL only; the
// Course-scoped route already resolved the exact revision-owned preview.
type publicPreviewAuthorizationBody struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

func mountMediaDeliveryRoutes(content *gin.RouterGroup, foundation *MediaFoundation, authenticator auth.Authenticator, principals identity.PrincipalResolver, logger *logging.Logger) {
	h := &mediaDeliveryHandlers{
		delivery: foundation.delivery, logger: logger,
		rateLimiter: foundation.rateLimiter, previewRatePolicy: foundation.previewRatePolicy,
	}
	protected := content.Group("")
	protected.Use(requireProtectedLearningAccess(authenticator, principals, logger))
	protected.POST("/playback-authorizations", strictJSONMiddleware(func() any { return &playbackAuthorizationBody{} }, mediaRequestBodyLimit), h.playbackAuthorization)
	protected.GET("/playback-manifests/:playbackSession/index.m3u8", h.playbackManifest)
	protected.GET("/playback-manifests/:playbackSession/renditions/:rendition/index.m3u8", h.playbackRenditionManifest)
	protected.POST("/download-authorizations", strictJSONMiddleware(func() any { return &downloadAuthorizationBody{} }, mediaRequestBodyLimit), h.downloadAuthorization)
	protected.POST("/courses/:courseId/lessons/:lessonId/materials/:materialId/download-authorizations", h.lessonFileDownloadAuthorization)
	protected.GET("/lessons/:lessonId/materials/resource", func(c *gin.Context) { h.materialEntry(c, media.KindResource) })
	protected.GET("/lessons/:lessonId/materials/lab-material", func(c *gin.Context) { h.materialEntry(c, media.KindLabMaterial) })
	content.GET("/courses/:courseID/preview", h.coursePreview)
	content.GET("/previews/:id", h.preview)
}

func (h *mediaDeliveryHandlers) playbackManifest(c *gin.Context) {
	manifest, err := h.delivery.IssuePlaybackManifest(
		c.Request.Context(), c.GetString(ctxUserIDKey), c.Param("playbackSession"),
	)
	if err != nil {
		logProtectedDeliveryDenial(c, h.logger, err)
		writeProtectedUnavailable(c)
		return
	}
	writePlaybackManifest(c, manifest)
}

func (h *mediaDeliveryHandlers) playbackRenditionManifest(c *gin.Context) {
	manifest, err := h.delivery.IssuePlaybackRenditionManifest(
		c.Request.Context(), c.GetString(ctxUserIDKey), c.Param("playbackSession"), c.Param("rendition"),
	)
	if err != nil {
		logProtectedDeliveryDenial(c, h.logger, err)
		writeProtectedUnavailable(c)
		return
	}
	writePlaybackManifest(c, manifest)
}

func writePlaybackManifest(c *gin.Context, manifest media.PlaybackManifest) {
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "application/vnd.apple.mpegurl")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "application/vnd.apple.mpegurl", manifest.Contents)
}

func (h *mediaDeliveryHandlers) playbackAuthorization(c *gin.Context) {
	if !h.allowProtectedPlaybackIssuance(c) {
		return
	}
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
	if !h.allowProtectedMaterialDownload(c) {
		return
	}
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

func (h *mediaDeliveryHandlers) lessonFileDownloadAuthorization(c *gin.Context) {
	if !h.allowProtectedMaterialDownload(c) {
		return
	}
	locale := "ar"
	if strings.HasPrefix(strings.ToLower(c.GetHeader("Accept-Language")), "en") {
		locale = "en"
	}
	issued, err := h.delivery.IssueLessonFileDownload(c.Request.Context(), media.LessonFileDownloadRequest{
		StudentID: c.GetString(ctxUserIDKey), CourseID: c.Param("courseId"), LessonID: c.Param("lessonId"),
		FileID: c.Param("materialId"), Locale: locale,
	})
	if err != nil {
		logProtectedDeliveryDenial(c, h.logger, err)
		writeProtectedUnavailable(c)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.JSON(http.StatusOK, lessonFileDownloadAuthorizationResponse{
		URL: issued.URL, ExpiresAt: issued.ExpiresAt, BuyerTag: issued.BuyerTag,
	})
}

func (h *mediaDeliveryHandlers) materialEntry(c *gin.Context, kind media.AssetKind) {
	if !h.allowProtectedMaterialDownload(c) {
		return
	}
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

// allowProtectedMaterialDownload applies the existing protected-learning
// signing ceilings before resolving a Course, Lesson, or attachment. The
// quota cannot become a material-inventory oracle, and an unavailable limiter
// never leaves a route that can mint a private object URL.
func (h *mediaDeliveryHandlers) allowProtectedMaterialDownload(c *gin.Context) bool {
	return h.allowProtectedSigning(c,
		ratelimit.ProtectedLearningMaterialDownloadSourcePolicy(),
		ratelimit.ProtectedLearningMaterialDownloadPolicy(),
	)
}

// allowProtectedPlaybackIssuance applies the same FR-017 / BR-102 ceilings to
// this route that the learning Lesson route already applies to its own.
//
// This endpoint mints playback sessions too, so leaving it unbounded would have
// left the bounded route beside an unbounded one reaching the same signer --
// exactly the "open a fresh quota by using the other door" shape the Student and
// source policies exist to close. The policies are the established ones, keyed
// the same way, so the two doors share one quota rather than each having their
// own: a Student who has spent their issuances on the Lesson route has spent
// them here as well.
//
// The ceilings are sized for issuance abuse, not for study: reloading a page,
// moving between Lessons, and recovering from a dropped network all stay far
// inside them.
func (h *mediaDeliveryHandlers) allowProtectedPlaybackIssuance(c *gin.Context) bool {
	return h.allowProtectedSigning(c,
		ratelimit.ProtectedLearningPlaybackSourcePolicy(),
		ratelimit.ProtectedLearningPlaybackPolicy(),
	)
}

// allowProtectedSigning decides the source ceiling and then the Student ceiling
// before a route may sign anything, in that fixed order.
//
// Deciding before authorization is deliberate: a throttled caller learns nothing
// about entitlement, Course inventory, or media identity, and quota state never
// becomes an authorization input. A limiter that cannot decide refuses with the
// uniform protected response, so no signed capability is ever issued on an
// undecided ceiling.
func (h *mediaDeliveryHandlers) allowProtectedSigning(c *gin.Context, sourcePolicy, studentPolicy ratelimit.Policy) bool {
	// The production composition always supplies this established limiter for
	// media signing. Test-only router seams may omit it when their subject is
	// route authorization rather than rate limiting.
	if h.rateLimiter == nil {
		return true
	}
	for _, check := range []struct {
		policy ratelimit.Policy
		input  ratelimit.Input
	}{
		{sourcePolicy, ratelimit.Input{ClientIP: c.ClientIP()}},
		{studentPolicy, ratelimit.Input{Identifier: c.GetString(ctxUserIDKey)}},
	} {
		decision := h.rateLimiter.Decide(c.Request.Context(), check.policy, check.input)
		c.Set(limiterOutcomeContextKey, string(decision.Outcome))
		if decision.Allowed {
			continue
		}
		c.Header("Cache-Control", "no-store")
		if decision.Outcome == ratelimit.OutcomeDenied || decision.Outcome == ratelimit.OutcomeFallbackDenied {
			if seconds := int(math.Ceil(decision.RetryAfter.Seconds())); seconds > 0 {
				c.Header("Retry-After", strconv.Itoa(seconds))
			}
			writeProblem(c, problem.RateLimited())
			return false
		}
		writeProtectedUnavailable(c)
		return false
	}
	return true
}

func (h *mediaDeliveryHandlers) preview(c *gin.Context) {
	if !h.allowPublicPreview(c) {
		return
	}
	issued, err := h.delivery.IssuePreview(c.Request.Context(), c.Param("id"))
	if err != nil {
		logProtectedDeliveryDenial(c, h.logger, err)
		writeProtectedUnavailable(c)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, issued)
}

func (h *mediaDeliveryHandlers) coursePreview(c *gin.Context) {
	if !h.allowPublicPreview(c) {
		return
	}
	issued, err := h.delivery.IssueCoursePreview(c.Request.Context(), c.Param("courseID"))
	if err != nil {
		logProtectedDeliveryDenial(c, h.logger, err)
		writeProtectedUnavailable(c)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, publicPreviewAuthorizationBody{URL: issued.URL, ExpiresAt: issued.ExpiresAt})
}

func (h *mediaDeliveryHandlers) allowPublicPreview(c *gin.Context) bool {
	// Test-only router seams can omit the limiter. Production composition always
	// supplies it; an empty policy therefore never silently disables a live
	// public issuance boundary.
	if h.rateLimiter == nil {
		return true
	}
	decision := h.rateLimiter.Decide(c.Request.Context(), h.previewRatePolicy, ratelimit.Input{ClientIP: c.ClientIP()})
	c.Set(limiterOutcomeContextKey, string(decision.Outcome))
	if decision.Allowed {
		return true
	}
	c.Header("Cache-Control", "no-store")
	if decision.Outcome == ratelimit.OutcomeDenied || decision.Outcome == ratelimit.OutcomeFallbackDenied {
		if seconds := int(math.Ceil(decision.RetryAfter.Seconds())); seconds > 0 {
			c.Header("Retry-After", strconv.Itoa(seconds))
		}
		writeProblem(c, problem.RateLimited())
		return false
	}
	// Issuance must never proceed when a fail-closed source quota cannot be
	// decided. Keep the same inventory-safe response used by absent media.
	writeProtectedUnavailable(c)
	return false
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
