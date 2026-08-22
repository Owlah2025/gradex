package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/media"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

const mediaRequestBodyLimit int64 = 64 * 1024

type mediaHandlers struct {
	service *media.Service
}

type mediaUploadBody struct {
	CourseID       string          `json:"course_id" binding:"required"`
	RevisionID     string          `json:"revision_id"`
	LessonID       string          `json:"lesson_id"`
	LogicalAssetID string          `json:"logical_asset_id"`
	Kind           media.AssetKind `json:"kind" binding:"required"`
	ContentType    string          `json:"content_type" binding:"required"`
	SizeBytes      int64           `json:"size_bytes" binding:"required,gt=0"`
}

type mediaCompletionBody struct {
	ProviderEventID      string `json:"provider_event_id" binding:"required"`
	StorageObjectKey     string `json:"storage_object_key" binding:"required"`
	StorageObjectVersion string `json:"storage_object_version" binding:"required"`
	ContentType          string `json:"content_type" binding:"required"`
	SizeBytes            int64  `json:"size_bytes" binding:"required,gt=0"`
	SHA256Hex            string `json:"sha256_hex" binding:"required"`
}

type mediaOutOfBandScanBody struct {
	StorageObjectVersion string `json:"storage_object_version" binding:"required"`
	Method               string `json:"method" binding:"required"`
	Provider             string `json:"provider" binding:"required"`
	Reference            string `json:"reference" binding:"required"`
}

func mountMediaRoutes(v1 *gin.RouterGroup, foundation *MediaFoundation, authenticator auth.Authenticator, principals identity.PrincipalResolver, logger *logging.Logger) {
	h := &mediaHandlers{service: foundation.service}
	content := v1.Group("/media")
	mountMediaUploadRoutes(content, h, authenticator, principals, logger)
	mountMediaStatusRoute(content, h, authenticator, principals, logger)
	mountMediaRetryRoute(content, h, authenticator, principals, logger)
	mountMediaCatalogueRoutes(content, h, authenticator, principals, logger)
	if foundation.delivery != nil {
		mountMediaDeliveryRoutes(content, foundation, authenticator, principals, logger)
	}
}

func mountMediaCatalogueRoutes(content *gin.RouterGroup, h *mediaHandlers, authenticator auth.Authenticator, principals identity.PrincipalResolver, logger *logging.Logger) {
	catalogue := content.Group("/catalogue-loads")
	catalogue.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapAdminOperations),
		requireRole(identity.RoleAdmin),
	)
	catalogue.POST("", strictJSONMiddleware(func() any { return &mediaUploadBody{} }, mediaRequestBodyLimit), h.beginCatalogueLoad)
	catalogue.POST("/:id/completions", strictJSONMiddleware(func() any { return &mediaCompletionBody{} }, mediaRequestBodyLimit), h.completeCatalogueLoad)
	evidence := content.Group("/assets/:id/out-of-band-scan-evidence")
	evidence.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapAdminOperations),
		requireRole(identity.RoleAdmin),
	)
	evidence.POST("", strictJSONMiddleware(func() any { return &mediaOutOfBandScanBody{} }, mediaRequestBodyLimit), h.recordOutOfBandScanEvidence)
}

func mountMediaUploadRoutes(content *gin.RouterGroup, h *mediaHandlers, authenticator auth.Authenticator, principals identity.PrincipalResolver, logger *logging.Logger) {
	uploads := content.Group("/uploads")
	uploads.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapContentManagement),
		requireRole(identity.RoleInstructor),
	)
	uploads.POST("", strictJSONMiddleware(func() any { return &mediaUploadBody{} }, mediaRequestBodyLimit), h.beginUpload)
	uploads.POST("/:id/completions", strictJSONMiddleware(func() any { return &mediaCompletionBody{} }, mediaRequestBodyLimit), h.completeUpload)
}

func mountMediaStatusRoute(content *gin.RouterGroup, h *mediaHandlers, authenticator auth.Authenticator, principals identity.PrincipalResolver, logger *logging.Logger) {
	status := content.Group("/assets/:id")
	status.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapContentManagement),
	)
	status.GET("", h.status)
}

func mountMediaRetryRoute(content *gin.RouterGroup, h *mediaHandlers, authenticator auth.Authenticator, principals identity.PrincipalResolver, logger *logging.Logger) {
	retry := content.Group("/assets/:id/retries")
	retry.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapAdminOperations),
		requireRole(identity.RoleAdmin),
	)
	retry.POST("", h.retry)
}

func (h *mediaHandlers) beginUpload(c *gin.Context) {
	body := c.MustGet(strictJSONBodyContextKey).(*mediaUploadBody)
	ticket, err := h.service.BeginUpload(c.Request.Context(), media.UploadRequest{
		OwnerAccountID: c.GetString(ctxUserIDKey), CourseID: body.CourseID, RevisionID: body.RevisionID, LessonID: body.LessonID,
		LogicalAssetID: body.LogicalAssetID, Kind: body.Kind,
		ContentType: body.ContentType, SizeBytes: body.SizeBytes,
	})
	if err != nil {
		writeMediaProblem(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, gin.H{
		"asset_version_id":   ticket.AssetVersionID,
		"upload_url":         ticket.UploadURL,
		"storage_object_key": ticket.StorageObjectKey,
		"expires_at":         ticket.ExpiresAt,
	})
}

func (h *mediaHandlers) beginCatalogueLoad(c *gin.Context) {
	body := c.MustGet(strictJSONBodyContextKey).(*mediaUploadBody)
	ticket, err := h.service.BeginCatalogueLoad(c.Request.Context(), media.CatalogueLoadRequest{
		AdminAccountID: c.GetString(ctxUserIDKey), CourseID: body.CourseID, LessonID: body.LessonID,
		LogicalAssetID: body.LogicalAssetID, Kind: body.Kind, ContentType: body.ContentType, SizeBytes: body.SizeBytes,
	})
	if err != nil {
		writeMediaProblem(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, gin.H{"asset_version_id": ticket.AssetVersionID, "upload_url": ticket.UploadURL, "storage_object_key": ticket.StorageObjectKey, "expires_at": ticket.ExpiresAt})
}

func (h *mediaHandlers) completeUpload(c *gin.Context) {
	body := c.MustGet(strictJSONBodyContextKey).(*mediaCompletionBody)
	result, err := h.service.CompleteUpload(c.Request.Context(), media.CompleteUploadRequest{
		OwnerAccountID: c.GetString(ctxUserIDKey), AssetVersionID: c.Param("id"),
		ProviderEventID: body.ProviderEventID, StorageObjectKey: body.StorageObjectKey,
		StorageObjectVersion: body.StorageObjectVersion, ContentType: body.ContentType,
		SizeBytes: body.SizeBytes, SHA256Hex: body.SHA256Hex,
	})
	if err != nil {
		writeMediaProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"asset_version_id": result.AssetVersionID, "state": result.State, "duplicate": result.Duplicate})
}

func (h *mediaHandlers) completeCatalogueLoad(c *gin.Context) {
	body := c.MustGet(strictJSONBodyContextKey).(*mediaCompletionBody)
	result, err := h.service.CompleteCatalogueLoad(c.Request.Context(), media.CatalogueCompletionRequest{
		AdminAccountID: c.GetString(ctxUserIDKey), AssetVersionID: c.Param("id"), ProviderEventID: body.ProviderEventID,
		StorageObjectKey: body.StorageObjectKey, StorageObjectVersion: body.StorageObjectVersion,
		ContentType: body.ContentType, SizeBytes: body.SizeBytes, SHA256Hex: body.SHA256Hex,
	})
	if err != nil {
		writeMediaProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"asset_version_id": result.AssetVersionID, "state": result.State, "duplicate": result.Duplicate})
}

func (h *mediaHandlers) recordOutOfBandScanEvidence(c *gin.Context) {
	body := c.MustGet(strictJSONBodyContextKey).(*mediaOutOfBandScanBody)
	if err := h.service.RecordOutOfBandScanEvidence(c.Request.Context(), media.OutOfBandScanEvidence{
		AdminAccountID: c.GetString(ctxUserIDKey), AssetVersionID: c.Param("id"), StorageObjectVersion: body.StorageObjectVersion,
		Method: body.Method, Provider: body.Provider, Reference: body.Reference,
	}); err != nil {
		writeMediaProblem(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *mediaHandlers) status(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	status, err := h.service.GetStatus(c.Request.Context(), c.Param("id"), media.Viewer{AccountID: principal.AccountID, Role: string(principal.Role)})
	if err != nil {
		writeMediaProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"asset_version_id":    status.AssetVersionID,
		"logical_asset_id":    status.LogicalAssetID,
		"kind":                status.Kind,
		"state":               status.State,
		"size_bytes":          status.SizeBytes,
		"trusted_duration_ms": status.TrustedDurationMS,
		"created_at":          status.CreatedAt,
		"deliverable":         status.Deliverable(),
	})
}

func (h *mediaHandlers) retry(c *gin.Context) {
	if err := h.service.Retry(c.Request.Context(), media.RetryRequest{
		AssetVersionID: c.Param("id"), AdminAccountID: c.GetString(ctxUserIDKey), ActorDescriptor: c.GetString(ctxUserIDKey),
	}); err != nil {
		writeMediaProblem(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"asset_version_id": c.Param("id"), "state": media.StateQuarantined})
}

func requireRole(role identity.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal, ok := principalFrom(c)
		if !ok || principal.Role != role {
			writeProblem(c, problem.NotAuthorized())
			return
		}
		c.Next()
	}
}

func writeMediaProblem(c *gin.Context, err error) {
	switch {
	case errors.Is(err, media.ErrNotFound):
		writeProblem(c, problem.NotFound())
	case errors.Is(err, media.ErrValidation):
		writeProblem(c, problem.ValidationFailed())
	case errors.Is(err, media.ErrUnavailable):
		writeProblem(c, problem.DependencyUnavailable())
	case errors.Is(err, media.ErrConcurrentModification):
		writeProblem(c, problem.StateConflict())
	case errors.Is(err, media.ErrNotAuthorized):
		writeProblem(c, problem.NotAuthorized())
	case errors.Is(err, media.ErrConflict):
		writeProblem(c, problem.UnsupportedStateTransition())
	default:
		writeProblem(c, problem.Internal(""))
	}
}
