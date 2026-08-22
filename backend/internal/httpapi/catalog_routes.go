package httpapi

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
)

func mountCatalogRoutes(
	v1 *gin.RouterGroup,
	foundation *CatalogFoundation,
	mediaFoundation *MediaFoundation,
	sessionFoundation *SessionFoundation,
	authenticator auth.Authenticator,
	principals identity.PrincipalResolver,
	logger *logging.Logger,
) error {
	if foundation == nil || foundation.repository == nil || foundation.ownership == nil || foundation.assetValidator == nil {
		return fmt.Errorf("complete catalog foundation is required")
	}
	if sessionFoundation == nil {
		return fmt.Errorf("session foundation is required for catalog routes")
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
	if mediaFoundation != nil {
		reviewH.playback = mediaFoundation.AdminReviewMedia()
	}

	ownershipMw, err := RequireCourseOwnership(foundation.ownership, logger)
	if err != nil {
		return fmt.Errorf("building course ownership middleware: %w", err)
	}

	// Unowned GET routes (Course listing, taxonomy terms listing)
	unownedGetGroup := v1.Group("")
	unownedGetGroup.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapContentManagement),
	)
	{
		unownedGetGroup.GET("/courses", h.listOwnedCourses)
		unownedGetGroup.GET("/taxonomy/terms", h.listTaxonomyTerms)
	}

	// Unowned mutation routes (Course creation)
	unownedMutationGroup := v1.Group("")
	unownedMutationGroup.Use(
		sessionFoundation.requireSessionMutationSecurity(),
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapContentManagement),
	)
	{
		unownedMutationGroup.POST("/courses", h.createCourse)
	}

	// Owned GET routes under /courses/:id
	ownedGetGroup := v1.Group("/courses/:id")
	ownedGetGroup.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapContentManagement),
		ownershipMw,
	)
	{
		ownedGetGroup.GET("", h.getOwnedCourse)
	}

	// Owned candidate mutation routes under /courses/:id
	ownedMutationGroup := v1.Group("/courses/:id")
	ownedMutationGroup.Use(
		sessionFoundation.requireSessionMutationSecurity(),
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapContentManagement),
		ownershipMw,
	)
	{
		ownedMutationGroup.PUT("/candidate", h.createCandidate)
		// D-093 5. Pre-publication Subject correction. The route exists for every
		// Course; the domain refuses it for a legacy Course, for one under
		// review, and for one that has published.
		ownedMutationGroup.PUT("/subject", h.setCourseSubject)
		ownedMutationGroup.PATCH("/revisions/:revisionId", h.updateCourseRevision)
		ownedMutationGroup.PUT("/revisions/:revisionId/audience", h.setRevisionAudience)
		ownedMutationGroup.DELETE("/revisions/:revisionId/audience", h.resetRevisionAudience)
		ownedMutationGroup.POST("/revisions/:revisionId/sections", h.addSection)
		ownedMutationGroup.PATCH("/revisions/:revisionId/sections/:sectionId", h.updateSection)
		ownedMutationGroup.DELETE("/revisions/:revisionId/sections/:sectionId", h.deleteSection)
		ownedMutationGroup.POST("/revisions/:revisionId/sections/:sectionId/lessons", h.addLesson)
		ownedMutationGroup.PATCH("/revisions/:revisionId/lessons/:lessonId", h.updateLesson)
		ownedMutationGroup.DELETE("/revisions/:revisionId/lessons/:lessonId", h.deleteLesson)
		ownedMutationGroup.PUT("/revisions/:revisionId/lessons/:lessonId/video", h.setLessonVideo)
		ownedMutationGroup.PUT("/revisions/:revisionId/lessons/:lessonId/files", h.addLessonFile)
		ownedMutationGroup.DELETE("/revisions/:revisionId/lessons/:lessonId/files", h.deleteLessonFile)
		ownedMutationGroup.PUT("/revisions/:revisionId/preview", h.setPreviewAsset)
		ownedMutationGroup.DELETE("/revisions/:revisionId/preview", h.clearPreviewAsset)
		ownedMutationGroup.POST("/revisions/:revisionId/submit", h.submitCourse)
	}

	// Admin review GET routes under /admin/review
	adminReviewGetGroup := v1.Group("/admin/review")
	adminReviewGetGroup.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapCatalogPublish),
	)
	{
		adminReviewGetGroup.GET("/queue", reviewH.listQueue)
		adminReviewGetGroup.GET("/courses/:id/revisions/:revisionId", reviewH.getCourseRevisionGraph)
		adminReviewGetGroup.GET("/playback-manifests/:playbackSession/index.m3u8", reviewH.playbackManifest)
	}

	// Admin review mutation routes under /admin/review
	adminReviewMutationGroup := v1.Group("/admin/review")
	adminReviewMutationGroup.Use(
		sessionFoundation.requireSessionMutationSecurity(),
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapCatalogPublish),
	)
	{
		adminReviewMutationGroup.POST("/courses/:id/revisions/:revisionId/approve", reviewH.approveCourse)
		adminReviewMutationGroup.POST("/courses/:id/revisions/:revisionId/request-changes", reviewH.requestChanges)
		adminReviewMutationGroup.POST("/courses/:id/revisions/:revisionId/preview/:lessonId", reviewH.previewLesson)
	}

	pricingH := &adminPricingHandlers{
		repo: foundation.repository,
	}
	lifecycleH := &adminLifecycleHandlers{repo: foundation.repository}
	taxonomyH := &adminTaxonomyHandlers{repo: foundation.repository}

	adminPricingGetGroup := v1.Group("/admin/courses/:id")
	adminPricingGetGroup.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapCatalogPricing),
	)
	{
		adminPricingGetGroup.GET("/price-history", pricingH.getCoursePriceHistory)
	}

	adminPricingMutationGroup := v1.Group("/admin/courses/:id")
	adminPricingMutationGroup.Use(
		sessionFoundation.requireSessionMutationSecurity(),
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapCatalogPricing),
	)
	{
		adminPricingMutationGroup.PUT("/price", pricingH.setCoursePrice)
		adminPricingMutationGroup.PUT("/sections/:sectionId/price", pricingH.setSectionPrice)
	}

	adminLifecycleMutationGroup := v1.Group("/admin/courses/:id")
	adminLifecycleMutationGroup.Use(
		sessionFoundation.requireSessionMutationSecurity(),
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapCatalogPublish),
	)
	{
		adminLifecycleMutationGroup.POST("/delist", lifecycleH.transition(catalog.LifecycleDelisted))
		adminLifecycleMutationGroup.POST("/relist", lifecycleH.transition(catalog.LifecyclePublished))
		adminLifecycleMutationGroup.POST("/retire", lifecycleH.retire)
		adminLifecycleMutationGroup.POST("/archive", lifecycleH.transition(catalog.LifecycleArchived))
		adminLifecycleMutationGroup.DELETE("", lifecycleH.delete)
		adminLifecycleMutationGroup.POST("/owner", lifecycleH.reassignOwner)
		adminLifecycleMutationGroup.POST("/access-suspension", lifecycleH.suspend)
		adminLifecycleMutationGroup.DELETE("/access-suspension", lifecycleH.restoreAccess)
	}

	adminTaxonomyMutationGroup := v1.Group("/admin/courses/:id")
	adminTaxonomyMutationGroup.Use(
		sessionFoundation.requireSessionMutationSecurity(),
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapCatalogTaxonomy),
	)
	{
		adminTaxonomyMutationGroup.PUT("/taxonomy", taxonomyH.assignTaxonomy)
	}

	adminTaxonomyTermMutationGroup := v1.Group("/admin/taxonomy/terms")
	adminTaxonomyTermMutationGroup.Use(
		sessionFoundation.requireSessionMutationSecurity(),
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapCatalogTaxonomy),
	)
	{
		adminTaxonomyTermMutationGroup.POST("", taxonomyH.createTerm)
		adminTaxonomyTermMutationGroup.PATCH("/:id", taxonomyH.renameTerm)
		adminTaxonomyTermMutationGroup.POST("/:id/retire", taxonomyH.retireTerm)
		adminTaxonomyTermMutationGroup.DELETE("/:id", taxonomyH.deleteTerm)
	}

	return nil
}
