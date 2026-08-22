package httpapi

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
)

// mountAcademicRoutes mounts the D-091 Academic Catalog administration surface.
//
// Every route below — read and write alike — sits behind authentication plus
// identity.CapAcademicCatalog, which only an Admin holds. There is deliberately
// no anonymous, Student, or Instructor entry point in T1: the Admin surface is
// fully served by these reads, so no public discovery API is opened early.
//
// Mutations additionally carry the session mutation-security middleware, so the
// CSRF and origin contract is identical to every other privileged catalog
// command.
func mountAcademicRoutes(
	v1 *gin.RouterGroup,
	foundation *AcademicFoundation,
	sessionFoundation *SessionFoundation,
	authenticator auth.Authenticator,
	principals identity.PrincipalResolver,
	logger *logging.Logger,
) error {
	if foundation == nil || foundation.repository == nil {
		return fmt.Errorf("academic foundation is required to mount academic routes")
	}
	if sessionFoundation == nil {
		return fmt.Errorf("session foundation is required to mount academic routes")
	}
	if authenticator == nil {
		return fmt.Errorf("authenticator is required to mount academic routes")
	}
	if principals == nil {
		return fmt.Errorf("principal resolver is required to mount academic routes")
	}

	h := &academicHandlers{repo: foundation.repository}
	requestH := &subjectRequestHandlers{repo: foundation.repository}
	importH := &academicImportHandlers{repo: foundation.repository}
	profileH := &academicProfileHandlers{repo: foundation.repository}

	readGroup := v1.Group("/admin/academic")
	readGroup.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapAcademicCatalog),
	)
	{
		readGroup.GET("/institutions", h.listInstitutions)
		readGroup.GET("/institutions/:institutionId/units", h.listUnits)
		readGroup.GET("/institutions/:institutionId/programs", h.listPrograms)
		readGroup.GET("/institutions/:institutionId/subjects", h.listSubjects)
		readGroup.GET("/programs/:programId/curricula", h.listCurricula)
		readGroup.GET("/curricula/:curriculumId/subjects", h.listCurriculumSubjects)
		readGroup.GET("/subject-requests", requestH.listAdmin)
		// Only the identifiers an Admin may select. No path, no URL, no upload.
		readGroup.GET("/manifests", importH.listManifests)
	}

	mutationGroup := v1.Group("/admin/academic")
	mutationGroup.Use(
		sessionFoundation.requireSessionMutationSecurity(),
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapAcademicCatalog),
	)
	{
		mutationGroup.POST("/institutions", h.createInstitution)
		mutationGroup.PATCH("/institutions/:institutionId", h.updateInstitution)
		mutationGroup.POST("/institutions/:institutionId/retire", h.retireInstitution)

		mutationGroup.POST("/institutions/:institutionId/units", h.createUnit)
		mutationGroup.PATCH("/units/:unitId", h.updateUnit)
		mutationGroup.POST("/units/:unitId/retire", h.retireUnit)

		mutationGroup.POST("/institutions/:institutionId/programs", h.createProgram)
		mutationGroup.PATCH("/programs/:programId", h.updateProgram)
		mutationGroup.POST("/programs/:programId/retire", h.retireProgram)

		mutationGroup.POST("/programs/:programId/curricula", h.createCurriculum)
		mutationGroup.PATCH("/curricula/:curriculumId", h.updateCurriculum)
		mutationGroup.POST("/curricula/:curriculumId/retire", h.retireCurriculum)

		mutationGroup.POST("/curricula/:curriculumId/subjects", h.mapSubject)
		mutationGroup.DELETE("/curricula/:curriculumId/subjects/:subjectId", h.unmapSubject)

		mutationGroup.POST("/institutions/:institutionId/subjects", h.createSubject)
		mutationGroup.PATCH("/subjects/:subjectId", h.updateSubject)
		mutationGroup.POST("/subjects/:subjectId/retire", h.retireSubject)
		mutationGroup.POST("/subject-requests/:requestId/link", requestH.link)
		mutationGroup.POST("/subject-requests/:requestId/approve-new", requestH.approveNew)
		mutationGroup.POST("/subject-requests/:requestId/reject", requestH.reject)

		// Both dry run and apply sit behind mutation security. A dry run writes
		// nothing, but it reads the whole catalog and reports intended changes,
		// so it carries the same authorization as the apply it previews.
		mutationGroup.POST("/institutions/:institutionId/import", importH.runImport)
	}

	// Student academic profile and onboarding options (D-092, T3).
	//
	// Behind the Student learning capability, exactly like the rest of /me. The
	// account always comes from the session, never from the request, so no shape
	// of call reaches another Student's profile and there is no bulk listing.
	//
	// This is discovery data: mounting it changes no access decision, and a
	// Student with no profile is unaffected everywhere else in the product.
	// Instructor authoring reads (D-091 9, T4-B).
	//
	// Behind the Instructor's own content-management capability, never the
	// Academic Catalog capability. An Instructor selects from the Admin-owned
	// catalog and can reach no mutation: this group mounts read routes only, so
	// there is no Subject create, amend, retire, or curriculum-mapping handler
	// behind it to call.
	authoringAcademicGroup := v1.Group("/authoring/academic")
	authoringAcademicGroup.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapContentManagement),
	)
	{
		authoringH := &authoringAcademicHandlers{repo: foundation.repository}
		authoringAcademicGroup.GET("/institutions", authoringH.listInstitutions)
		authoringAcademicGroup.GET("/institutions/:institutionId/subjects", authoringH.searchSubjects)
		authoringAcademicGroup.GET("/institutions/:institutionId/subjects/:subjectId", authoringH.getSubject)
		authoringAcademicGroup.GET("/subject-requests", requestH.listOwn)
	}

	authoringAcademicMutationGroup := v1.Group("/authoring/academic")
	authoringAcademicMutationGroup.Use(
		sessionFoundation.requireSessionMutationSecurity(),
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapContentManagement),
	)
	{
		authoringAcademicMutationGroup.POST("/subject-requests", requestH.create)
	}

	meAcademicReadGroup := v1.Group("/me")
	meAcademicReadGroup.Use(
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapLearningAccess),
	)
	{
		meAcademicReadGroup.GET("/academic-profile", profileH.getProfile)
		meAcademicReadGroup.GET("/academic-options/institutions", profileH.listInstitutions)
		meAcademicReadGroup.GET("/academic-options/institutions/:institutionId/colleges", profileH.listColleges)
		meAcademicReadGroup.GET("/academic-options/institutions/:institutionId/programs", profileH.listPrograms)
	}

	meAcademicMutationGroup := v1.Group("/me")
	meAcademicMutationGroup.Use(
		sessionFoundation.requireSessionMutationSecurity(),
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapLearningAccess),
	)
	{
		meAcademicMutationGroup.PUT("/academic-profile", profileH.saveProfile)
		meAcademicMutationGroup.POST("/academic-profile/skip", profileH.skipOnboarding)
	}

	return nil
}
