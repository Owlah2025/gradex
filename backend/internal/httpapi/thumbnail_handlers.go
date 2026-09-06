package httpapi

import (
	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/media"
	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
)

type thumbnailBody struct {
	AssetVersionID         *string `json:"thumbnail_asset_version_id"`
	ExpectedAssetVersionID *string `json:"expected_asset_version_id"`
}

func (h *authoringHandlers) setThumbnail(c *gin.Context) {
	body := c.MustGet(strictJSONBodyContextKey).(*thumbnailBody)
	for _, id := range []*string{body.AssetVersionID, body.ExpectedAssetVersionID} {
		if id != nil {
			if _, err := uuid.Parse(*id); err != nil {
				writeProblem(c, problem.StateConflict())
				return
			}
		}
	}
	rev, err := h.repo.SetThumbnail(c.Request.Context(), catalog.SetThumbnailRequest{CourseID: c.Param("id"), RevisionID: c.Param("revisionId"), OwnerAccountID: c.GetString(ctxUserIDKey), AssetVersionID: body.AssetVersionID, ExpectedAssetVersionID: body.ExpectedAssetVersionID}, c.GetString(ctxUserIDKey))
	if err != nil {
		h.handleCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, rev)
}

func serveThumbnail(c *gin.Context, s *media.Service, public bool) {
	viewer := media.Viewer{}
	if !public {
		viewer = media.Viewer{AccountID: c.GetString(ctxUserIDKey)}
	}
	courseID := c.Param("id")
	if public {
		courseID = c.Param("idOrSlug")
	}
	body, err := s.ReadThumbnail(c.Request.Context(), media.ThumbnailReadRequest{CourseID: courseID, RevisionID: c.Param("revisionId"), AssetVersionID: c.Param("assetId"), Variant: c.Param("variant"), Viewer: viewer, Public: public})
	if err != nil {
		writeMediaProblem(c, err)
		return
	}
	c.Header("X-Content-Type-Options", "nosniff")
	if public {
		c.Header("Cache-Control", "public, max-age=86400, immutable")
	} else {
		c.Header("Cache-Control", "private, no-store")
	}
	c.Data(http.StatusOK, "image/jpeg", body)
}
