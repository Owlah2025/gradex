package httpapi

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
)

func mountCatalogRoutes(
	v1 *gin.RouterGroup,
	foundation *CatalogFoundation,
	sessionFoundation *SessionFoundation,
	authenticator auth.Authenticator,
	principals identity.PrincipalResolver,
	logger *logging.Logger,
) error {
	if foundation == nil || foundation.ownership == nil {
		return fmt.Errorf("catalog ownership checker is required")
	}
	if logger == nil {
		return fmt.Errorf("logger is required")
	}

	h := &authoringHandlers{
		repo:           foundation.repository,
		assetValidator: foundation.assetValidator,
		logger:         logger,
	}

	reviewH := &reviewHandlers{
		repo:           foundation.repository,
		assetValidator: foundation.assetValidator,
		logger:         logger,
	}

	ownershipMw, err := RequireCourseOwnership(foundation.ownership, logger)
	if err != nil {
		return fmt.Errorf("building course ownership middleware: %w", err)
	}

	// Unowned authoring routes (Course creation, listing owned courses, taxonomy listing)
	unownedGroup := v1.Group("")
	unownedGroup.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapContentManagement),
	)
	{
		unownedGroup.POST("/courses", h.createCourse)
		unownedGroup.GET("/courses", h.listOwnedCourses)
		unownedGroup.GET("/taxonomy/terms", h.listTaxonomyTerms)
	}

	// Owned course routes under /courses/:id - EVERY route carries RequireCourseOwnership
	ownedGroup := v1.Group("/courses/:id")
	ownedGroup.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapContentManagement),
		ownershipMw,
	)
	{
		ownedGroup.GET("", h.getOwnedCourse)
		ownedGroup.PATCH("", h.updateCourse)
		ownedGroup.POST("/sections", h.addSection)
		ownedGroup.PATCH("/sections/:sectionId", h.updateSection)
		ownedGroup.DELETE("/sections/:sectionId", h.deleteSection)
		ownedGroup.POST("/sections/:sectionId/lessons", h.addLesson)
		ownedGroup.PATCH("/lessons/:lessonId", h.updateLesson)
		ownedGroup.DELETE("/lessons/:lessonId", h.deleteLesson)
		ownedGroup.PUT("/lessons/:lessonId/video", h.setLessonVideo)
		ownedGroup.PUT("/lessons/:lessonId/files", h.addLessonFile)
		ownedGroup.DELETE("/lessons/:lessonId/files", h.deleteLessonFile)
		ownedGroup.PUT("/preview", h.setPreviewAsset)
		ownedGroup.DELETE("/preview", h.clearPreviewAsset)
		ownedGroup.POST("/submit", h.submitCourse)
	}

	// Admin review queue and review actions under /admin/review
	// Require authenticated Admin session and CATALOG_PUBLISH through identity.Authorize (contracts/review-api.md)
	adminReviewGroup := v1.Group("/admin/review")
	adminReviewGroup.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapCatalogPublish),
	)
	{
		adminReviewGroup.GET("/queue", reviewH.listQueue)
		adminReviewGroup.GET("/courses/:id", reviewH.getCourseGraph)
		adminReviewGroup.POST("/courses/:id/approve", reviewH.approveCourse)
		adminReviewGroup.POST("/courses/:id/request-changes", reviewH.requestChanges)
		adminReviewGroup.POST("/courses/:id/preview/:lessonId", reviewH.previewLesson)
	}

	return nil
}
