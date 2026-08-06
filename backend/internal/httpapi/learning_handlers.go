package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/entitlement"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/learning"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/media"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

const learningRequestBodyLimit = 16 << 10

var errProgressAuthorizationChanged = errors.New("learning progress authorization changed before mutation")

type learningHandlers struct {
	foundation *LearningFoundation
	logger     *logging.Logger
	now        func() time.Time
}

type learningStatus string

const (
	learningStatusActive  learningStatus = "active"
	learningStatusExpired learningStatus = "expired"
)

type learningProgressResponse struct {
	PositionSeconds float64 `json:"position_seconds"`
	Completed       bool    `json:"completed"`
}

type learningMaterialResponse struct {
	Kind media.MaterialKind `json:"kind"`
}

type learningCourseProgressResponse struct {
	CompletedLessons int `json:"completed_lessons"`
	TotalLessons     int `json:"total_lessons"`
	Percent          int `json:"percent"`
}

type dashboardCourseResponse struct {
	CourseID       string                         `json:"course_id"`
	Title          string                         `json:"title"`
	LearningStatus learningStatus                 `json:"learning_status"`
	ExpiresAt      *time.Time                     `json:"expires_at"`
	Progress       learningCourseProgressResponse `json:"progress"`
}

type dashboardResponse struct {
	Courses []dashboardCourseResponse `json:"courses"`
}

type courseHomeLessonResponse struct {
	LessonID  string                     `json:"lesson_id"`
	Title     string                     `json:"title"`
	Progress  learningProgressResponse   `json:"progress"`
	Materials []learningMaterialResponse `json:"materials"`
}

type courseHomeSectionResponse struct {
	SectionID string                     `json:"section_id"`
	Title     string                     `json:"title"`
	Lessons   []courseHomeLessonResponse `json:"lessons"`
}

type courseHomeResponse struct {
	CourseID       string                         `json:"course_id"`
	Title          string                         `json:"title"`
	LearningStatus learningStatus                 `json:"learning_status"`
	ExpiresAt      *time.Time                     `json:"expires_at"`
	Progress       learningCourseProgressResponse `json:"progress"`
	Sections       []courseHomeSectionResponse    `json:"sections"`
	// ReportContext is an opaque encrypted token binding a COURSE report to the exact Revision
	// this response rendered (D-065). Issued only for an active read; absent otherwise. It is
	// evidence, not a capability — possessing one authorises nothing.
	ReportContext string `json:"report_context,omitempty"`
}

// lessonReportContexts carries one opaque token per reportable target present in the visible
// Lesson graph. A kind that is not present has no context at all.
type lessonReportContexts struct {
	Lesson      string `json:"lesson"`
	Video       string `json:"video,omitempty"`
	Resource    string `json:"resource,omitempty"`
	LabMaterial string `json:"lab_material,omitempty"`
}

type lessonSectionResponse struct {
	SectionID string `json:"section_id"`
	Title     string `json:"title"`
}

type lessonNavigationResponse struct {
	PreviousLessonID *string `json:"previous_lesson_id"`
	NextLessonID     *string `json:"next_lesson_id"`
}

type lessonResponse struct {
	CourseID       string                     `json:"course_id"`
	LessonID       string                     `json:"lesson_id"`
	Section        lessonSectionResponse      `json:"section"`
	Title          string                     `json:"title"`
	LearningStatus learningStatus             `json:"learning_status"`
	ExpiresAt      *time.Time                 `json:"expires_at"`
	Progress       learningProgressResponse   `json:"progress"`
	Navigation     lessonNavigationResponse   `json:"navigation"`
	Materials      []learningMaterialResponse `json:"materials"`
	ReportContexts *lessonReportContexts      `json:"report_contexts,omitempty"`
}

type learningProgressBody struct {
	PositionSeconds float64 `json:"position_seconds"`
	AssetVersionID  string  `json:"asset_version_id" binding:"required"`
}

// mountLearningRoutes composes learning only behind the same live
// authentication/principal boundary as protected media. Every handler below
// re-evaluates entitlement itself at request time before protected learning
// reads or writes.
func mountLearningRoutes(v1 *gin.RouterGroup, foundation *LearningFoundation, authenticator auth.Authenticator, principals identity.PrincipalResolver, logger *logging.Logger) error {
	if err := foundation.validate(); err != nil {
		return fmt.Errorf("complete learning foundation is required: %w", err)
	}
	h := &learningHandlers{foundation: foundation, logger: logger, now: foundation.now}
	learn := v1.Group("/learn")
	learn.Use(requireProtectedLearningAccess(authenticator, principals, logger))
	learn.GET("/dashboard", h.dashboard)
	learn.GET("/courses/:courseId", h.courseHome)
	learn.GET("/courses/:courseId/lessons/:lessonId", h.lesson)
	learn.POST("/lessons/:lessonId/playback", h.issuePlayback)
	learn.PUT("/lessons/:lessonId/progress", h.saveProgress)
	// T063. The report-specific 5/hour throttle is T064's and is not mounted here; the general
	// authentication and source protections above still apply.
	learn.POST("/reports", h.createReport)
	return nil
}

func (h *learningHandlers) authorizeRead(c *gin.Context, lessonID string) (entitlement.ReadDecision, bool) {
	decision := h.foundation.readEvaluator.EvaluateRead(c.Request.Context(), c.GetString(ctxUserIDKey), lessonID, h.now().UTC())
	if decision.State == entitlement.ReadDenied {
		h.logDenial(c, decision.Reason)
		writeProtectedUnavailable(c)
		return decision, false
	}
	return decision, true
}

func (h *learningHandlers) dashboard(c *gin.Context) {
	studentID := c.GetString(ctxUserIDKey)
	summaries, err := h.foundation.readRepository.ListStudentCourseSummaries(c.Request.Context(), studentID)
	if err != nil {
		h.logDenial(c, entitlement.ReasonDependency)
		writeProtectedUnavailable(c)
		return
	}
	decisions, err := h.foundation.readEvaluator.EvaluateCourseReads(c.Request.Context(), studentID, h.now().UTC())
	if err != nil {
		h.logDenial(c, entitlement.ReasonDependency)
		writeProtectedUnavailable(c)
		return
	}
	writeLearningJSON(c, h.dashboardReadModel(c, summaries, decisions))
}

func (h *learningHandlers) dashboardReadModel(c *gin.Context, summaries []learning.StudentCourseSummary, decisions map[string]entitlement.ReadDecision) dashboardResponse {
	response := dashboardResponse{Courses: make([]dashboardCourseResponse, 0, len(summaries))}
	for _, summary := range summaries {
		decision, ok := decisions[summary.CourseID]
		if !ok || !decision.CourseWide || (decision.State != entitlement.ReadActive && decision.State != entitlement.ReadExpired) {
			continue
		}
		response.Courses = append(response.Courses, dashboardCourseResponse{
			CourseID: summary.CourseID, Title: localizedLearningTitle(c, summary.TitleAr, summary.TitleEn),
			LearningStatus: presentationLearningStatus(decision), ExpiresAt: utcTimePtr(decision.ExpiresAt),
			Progress: courseProgressResponse(summary.Progress),
		})
	}
	return response
}

func (h *learningHandlers) courseHome(c *gin.Context) {
	courseID := c.Param("courseId")
	graph, err := h.foundation.readRepository.ReadCourseGraph(c.Request.Context(), courseID)
	if err != nil || len(graph.LessonIDs()) == 0 {
		h.logDenial(c, entitlement.ReasonDependency)
		writeProtectedUnavailable(c)
		return
	}
	decision, ok := h.authorizeRead(c, graph.LessonIDs()[0])
	if !ok || !decision.CourseWide {
		if ok {
			h.logDenial(c, entitlement.ReasonNoApplicableGrant)
			writeProtectedUnavailable(c)
		}
		return
	}
	enrollmentID, err := h.foundation.readRepository.EnrollmentID(c.Request.Context(), c.GetString(ctxUserIDKey), courseID)
	if err != nil {
		h.logDenial(c, entitlement.ReasonNoApplicableGrant)
		writeProtectedUnavailable(c)
		return
	}
	progressByLesson, summary, err := h.foundation.readRepository.ReadCourseProgress(c.Request.Context(), enrollmentID, courseID, graph)
	if err != nil {
		h.logDenial(c, entitlement.ReasonDependency)
		writeProtectedUnavailable(c)
		return
	}
	materialKinds := map[string][]media.Material{}
	if decision.State == entitlement.ReadActive {
		materialKinds, err = h.foundation.media.MaterialKinds(c.Request.Context(), graph.LessonIDs())
		if err != nil {
			h.logDenial(c, entitlement.ReasonDependency)
			writeProtectedUnavailable(c)
			return
		}
	}
	model := courseHomeReadModel(c, graph, decision, progressByLesson, summary, materialKinds)
	if decision.State == entitlement.ReadActive {
		// One COURSE context, bound to the Revision this very response rendered.
		tokens, ok := h.mintReportContexts(c, graph.CourseID, graph.RevisionID, []learning.ReportContextRequest{
			{TargetKind: learning.ReportTargetCourse, StableTargetID: graph.CourseID},
		})
		if !ok {
			return
		}
		model.ReportContext = tokens[0]
	}
	writeLearningJSON(c, model)
}

// mintReportContexts builds the encrypted report contexts for one active protected read.
//
// Every value comes from the graph and material result that produced *this* response — nothing is
// re-resolved. That is the whole guarantee: a page rendered from Revision A carries contexts
// bound to A, so a report filed from it names A even after B goes live (D-065).
//
// Issuance is all-or-nothing. A read that can render active content but cannot mint its contexts
// would leave the Student unable to report accurately, so the failure surfaces as the ordinary
// protected-unavailable refusal rather than a partially reportable page.
func (h *learningHandlers) mintReportContexts(c *gin.Context, courseID, revisionID string, requests []learning.ReportContextRequest) ([]string, bool) {
	session, ok := sessionFromContext(c)
	if !ok || session.ID == "" {
		h.logDenial(c, entitlement.ReasonDependency)
		writeProtectedUnavailable(c)
		return nil, false
	}
	reporter := c.GetString(ctxUserIDKey)
	if reporter == "" || courseID == "" || revisionID == "" {
		h.logDenial(c, entitlement.ReasonDependency)
		writeProtectedUnavailable(c)
		return nil, false
	}

	tokens := make([]string, 0, len(requests))
	for _, request := range requests {
		request.ReporterAccountID = reporter
		request.SessionID = session.ID
		request.CourseID = courseID
		request.VisibleCourseRevisionID = revisionID
		token, err := h.foundation.reportContexts.Mint(request)
		if err != nil || token == "" {
			// The cause is cryptographic or configuration detail and never reaches the client.
			h.logDenial(c, entitlement.ReasonDependency)
			writeProtectedUnavailable(c)
			return nil, false
		}
		tokens = append(tokens, token)
	}
	return tokens, true
}

func courseHomeReadModel(c *gin.Context, graph learning.CourseGraph, decision entitlement.ReadDecision, progressByLesson map[string]learning.Progress, summary learning.CourseProgressSummary, materialKinds map[string][]media.Material) courseHomeResponse {
	response := courseHomeResponse{
		CourseID: graph.CourseID, Title: localizedLearningTitle(c, graph.TitleAr, graph.TitleEn),
		LearningStatus: presentationLearningStatus(decision), ExpiresAt: utcTimePtr(decision.ExpiresAt),
		Progress: courseProgressResponse(summary), Sections: make([]courseHomeSectionResponse, 0, len(graph.Sections)),
	}
	for _, section := range graph.Sections {
		response.Sections = append(response.Sections, courseHomeSectionReadModel(c, section, progressByLesson, materialKinds))
	}
	return response
}

func courseHomeSectionReadModel(c *gin.Context, section learning.CourseGraphSection, progressByLesson map[string]learning.Progress, materialKinds map[string][]media.Material) courseHomeSectionResponse {
	response := courseHomeSectionResponse{SectionID: section.ID, Title: localizedLearningTitle(c, section.TitleAr, section.TitleEn), Lessons: make([]courseHomeLessonResponse, 0, len(section.Lessons))}
	for _, lesson := range section.Lessons {
		progress := progressByLesson[lesson.ID]
		response.Lessons = append(response.Lessons, courseHomeLessonResponse{
			LessonID: lesson.ID, Title: localizedLearningTitle(c, lesson.TitleAr, lesson.TitleEn),
			Progress:  learningProgressResponse{PositionSeconds: progress.LastPositionSeconds, Completed: progress.Completed},
			Materials: materialResponses(materialKinds[lesson.ID]),
		})
	}
	return response
}

func (h *learningHandlers) lesson(c *gin.Context) {
	courseID, lessonID := c.Param("courseId"), c.Param("lessonId")
	graph, err := h.foundation.readRepository.ReadCourseGraph(c.Request.Context(), courseID)
	if err != nil {
		h.logDenial(c, entitlement.ReasonDependency)
		writeProtectedUnavailable(c)
		return
	}
	selected, section, found := findCourseLesson(graph, lessonID)
	if !found {
		h.logDenial(c, entitlement.ReasonNoApplicableGrant)
		writeProtectedUnavailable(c)
		return
	}
	decision, ok := h.authorizeRead(c, lessonID)
	if !ok {
		return
	}
	enrollmentID, err := h.foundation.readRepository.EnrollmentID(c.Request.Context(), c.GetString(ctxUserIDKey), courseID)
	if err != nil {
		h.logDenial(c, entitlement.ReasonNoApplicableGrant)
		writeProtectedUnavailable(c)
		return
	}
	progress, err := h.foundation.readRepository.ReadLessonProgress(c.Request.Context(), enrollmentID, lessonID)
	if err != nil {
		h.logDenial(c, entitlement.ReasonDependency)
		writeProtectedUnavailable(c)
		return
	}
	previousID, nextID := lessonNavigation(graph.LessonIDs(), lessonID)
	materialKinds := map[string][]media.Material{}
	if decision.State == entitlement.ReadActive {
		materialKinds, err = h.foundation.media.MaterialKinds(c.Request.Context(), []string{lessonID})
		if err != nil {
			h.logDenial(c, entitlement.ReasonDependency)
			writeProtectedUnavailable(c)
			return
		}
	}
	response := lessonResponse{
		CourseID: graph.CourseID, LessonID: selected.ID,
		Section: lessonSectionResponse{SectionID: section.ID, Title: localizedLearningTitle(c, section.TitleAr, section.TitleEn)},
		Title:   localizedLearningTitle(c, selected.TitleAr, selected.TitleEn), LearningStatus: presentationLearningStatus(decision),
		ExpiresAt: utcTimePtr(decision.ExpiresAt), Progress: learningProgressResponse{PositionSeconds: progress.LastPositionSeconds, Completed: progress.Completed},
		Navigation: lessonNavigationResponse{PreviousLessonID: previousID, NextLessonID: nextID},
		Materials:  materialResponses(materialKinds[lessonID]),
	}

	if decision.State == entitlement.ReadActive {
		// A context per target actually present in this visible Lesson graph — no more.
		requests := []learning.ReportContextRequest{
			{TargetKind: learning.ReportTargetLesson, StableTargetID: selected.ID},
		}
		kinds := []learning.ReportTargetKind{learning.ReportTargetLesson}

		if selected.VideoAssetVersionID != nil && *selected.VideoAssetVersionID != "" {
			requests = append(requests, learning.ReportContextRequest{
				TargetKind: learning.ReportTargetVideo, StableTargetID: selected.ID,
				VisibleAssetVersionID: *selected.VideoAssetVersionID,
			})
			kinds = append(kinds, learning.ReportTargetVideo)
		}
		if version := materialVersion(materialKinds[lessonID], media.MaterialResource); version != "" {
			requests = append(requests, learning.ReportContextRequest{
				TargetKind: learning.ReportTargetResource, StableTargetID: selected.ID,
				VisibleAssetVersionID: version,
			})
			kinds = append(kinds, learning.ReportTargetResource)
		}
		if version := materialVersion(materialKinds[lessonID], media.MaterialLabMaterial); version != "" {
			requests = append(requests, learning.ReportContextRequest{
				TargetKind: learning.ReportTargetLabMaterial, StableTargetID: selected.ID,
				VisibleAssetVersionID: version,
			})
			kinds = append(kinds, learning.ReportTargetLabMaterial)
		}

		tokens, ok := h.mintReportContexts(c, graph.CourseID, graph.RevisionID, requests)
		if !ok {
			return
		}
		contexts := &lessonReportContexts{}
		for i, kind := range kinds {
			switch kind {
			case learning.ReportTargetLesson:
				contexts.Lesson = tokens[i]
			case learning.ReportTargetVideo:
				contexts.Video = tokens[i]
			case learning.ReportTargetResource:
				contexts.Resource = tokens[i]
			case learning.ReportTargetLabMaterial:
				contexts.LabMaterial = tokens[i]
			}
		}
		response.ReportContexts = contexts
	}

	writeLearningJSON(c, response)
}

// materialResponses exposes only the kind. The exact Asset Version each material resolved to stays
// internal: it feeds report-context minting and never the public contract.
func materialResponses(materials []media.Material) []learningMaterialResponse {
	response := make([]learningMaterialResponse, 0, len(materials))
	for _, kind := range []media.MaterialKind{media.MaterialResource, media.MaterialLabMaterial} {
		for _, available := range materials {
			if available.Kind == kind {
				response = append(response, learningMaterialResponse{Kind: kind})
				break
			}
		}
	}
	return response
}

// materialVersion returns the exact Asset Version the visible graph resolved for one kind, or "".
func materialVersion(materials []media.Material, kind media.MaterialKind) string {
	for _, available := range materials {
		if available.Kind == kind {
			return available.AssetVersionID
		}
	}
	return ""
}

func findCourseLesson(graph learning.CourseGraph, lessonID string) (learning.CourseGraphLesson, learning.CourseGraphSection, bool) {
	for _, section := range graph.Sections {
		for _, lesson := range section.Lessons {
			if lesson.ID == lessonID {
				return lesson, section, true
			}
		}
	}
	return learning.CourseGraphLesson{}, learning.CourseGraphSection{}, false
}

func lessonNavigation(ids []string, lessonID string) (*string, *string) {
	for index, id := range ids {
		if id != lessonID {
			continue
		}
		var previousID, nextID *string
		if index > 0 {
			previous := ids[index-1]
			previousID = &previous
		}
		if index+1 < len(ids) {
			next := ids[index+1]
			nextID = &next
		}
		return previousID, nextID
	}
	return nil, nil
}

func courseProgressResponse(summary learning.CourseProgressSummary) learningCourseProgressResponse {
	percent := 0
	if summary.TotalLessons > 0 {
		percent = summary.CompletedLessons * 100 / summary.TotalLessons
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	return learningCourseProgressResponse{CompletedLessons: summary.CompletedLessons, TotalLessons: summary.TotalLessons, Percent: percent}
}

func presentationLearningStatus(decision entitlement.ReadDecision) learningStatus {
	if decision.State == entitlement.ReadExpired {
		return learningStatusExpired
	}
	return learningStatusActive
}

func localizedLearningTitle(c *gin.Context, titleAr, titleEn string) string {
	if strings.HasPrefix(strings.ToLower(c.GetHeader("Accept-Language")), "en") {
		return titleEn
	}
	return titleAr
}

func utcTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := value.UTC()
	return &copy
}

func writeLearningJSON(c *gin.Context, value any) {
	c.Header("Cache-Control", "no-store")
	if strings.TrimSpace(c.GetHeader("Accept-Language")) != "" {
		appendVary(c, "Accept-Language")
	}
	c.JSON(http.StatusOK, value)
}

func (h *learningHandlers) authorize(c *gin.Context, lessonID string) bool {
	decision := h.foundation.evaluator.Evaluate(c.Request.Context(), c.GetString(ctxUserIDKey), lessonID, h.now().UTC())
	if decision.Allowed {
		return true
	}
	h.logDenial(c, decision.Reason)
	writeProtectedUnavailable(c)
	return false
}

func (h *learningHandlers) issuePlayback(c *gin.Context) {
	lessonID := c.Param("lessonId")
	// FR-017 / BR-102: both playback ceilings are decided before authorization and
	// before any issuance, in the same order the Progress mutation uses -- source
	// address first, then Student. Deciding first means a throttled caller learns
	// nothing about entitlement, Course inventory, or media identity, and quota state
	// never becomes an authorization input. A limiter dependency failure fails closed
	// inside requireRateDecision with the uniform protected refusal, so no signed
	// target is ever issued when a ceiling cannot be evaluated.
	if !h.requireRateDecision(c, "learning-playback-source", ratelimit.Input{ClientIP: c.ClientIP()}) {
		return
	}
	if !h.requireRateDecision(c, "learning-playback", ratelimit.Input{Identifier: playbackRateIdentifier(c.GetString(ctxUserIDKey))}) {
		return
	}
	if !h.authorize(c, lessonID) {
		return
	}
	// The exact version is selected from the approved lesson graph by S4. A
	// client cannot choose a version or turn this endpoint into a signer.
	versionID, err := h.foundation.repository.LessonVideoVersion(c.Request.Context(), lessonID)
	if err != nil {
		h.logDenial(c, entitlement.ReasonDependency)
		writeProtectedUnavailable(c)
		return
	}
	issued, err := h.foundation.media.IssuePlayback(c.Request.Context(), media.PlaybackRequest{
		StudentID: c.GetString(ctxUserIDKey), LessonID: lessonID, AssetVersionID: versionID,
	})
	if err != nil {
		h.logDenial(c, protectedReason(err))
		writeProtectedUnavailable(c)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, issued)
}

func (h *learningHandlers) saveProgress(c *gin.Context) {
	lessonID := c.Param("lessonId")
	studentID := c.GetString(ctxUserIDKey)
	if !h.requireRateDecision(c, "learning-progress-source", ratelimit.Input{ClientIP: c.ClientIP()}) {
		return
	}
	if !h.requireRateDecision(c, "learning-progress", ratelimit.Input{Identifier: progressRateIdentifier(studentID, lessonID)}) {
		return
	}
	if !h.authorize(c, lessonID) {
		return
	}
	body := &learningProgressBody{}
	if !bindStrictJSON(c, body, learningRequestBodyLimit) {
		return
	}
	enrollment, err := h.foundation.repository.EnrollmentForLesson(c.Request.Context(), studentID, lessonID)
	if err != nil {
		h.logDenial(c, entitlement.ReasonDependency)
		writeProtectedUnavailable(c)
		return
	}
	duration, err := h.foundation.media.TrustedVideoDuration(c.Request.Context(), lessonID, body.AssetVersionID)
	if err != nil || duration <= 0 {
		h.logDenial(c, entitlement.ReasonDependency)
		writeProtectedUnavailable(c)
		return
	}
	seconds := learning.BoundPosition(body.PositionSeconds, duration.Seconds())
	if h.foundation.beforeProgressMutation != nil {
		h.foundation.beforeProgressMutation(c.Request.Context())
	}
	write := learning.ProgressWrite{
		EnrollmentID: enrollment.ID, CourseLessonIdentityID: lessonID, PositionSeconds: seconds,
		Completed: learning.Completes(seconds, duration.Seconds()), CompletingAssetVersionID: body.AssetVersionID,
	}
	finalReason := entitlement.ReasonDependency
	err = h.foundation.repository.SaveProgressGuarded(c.Request.Context(), write, func(ctx context.Context, tx pgx.Tx) error {
		decision := h.foundation.evaluator.EvaluateInTransaction(ctx, tx, studentID, lessonID, h.now().UTC())
		if decision.Allowed {
			return nil
		}
		finalReason = decision.Reason
		return errProgressAuthorizationChanged
	})
	if err != nil {
		// Both outcomes answer the Student identically, so the log is the only place they can be
		// told apart. Access that ended between admission and the write is an authorization
		// decision and is recorded as a denial with its typed reason; a store that refused the
		// write is not a decision at all, and recording it as one would make a database outage
		// read as a spike in refusals on the Progress route.
		if errors.Is(err, errProgressAuthorizationChanged) {
			h.logDenial(c, finalReason)
		} else {
			h.logWriteFailure(c, operationProgressWrite, writeStagePersistence, writeFailureStoreRejected)
		}
		writeProtectedUnavailable(c)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusNoContent)
}

func protectedReason(err error) entitlement.Reason {
	if reason, ok := media.ProtectedDenialReason(err); ok {
		return reason
	}
	return entitlement.ReasonDependency
}

// The closed vocabularies the protected-write failure event carries. They are constants rather
// than literals so a new stage or class is a deliberate addition to this list.
const (
	operationProgressWrite = "progress_write"

	writeStagePersistence = "persistence"

	// writeFailureStoreRejected is the one class S5 can currently distinguish: the guarded
	// transaction reached the store and did not complete. It deliberately says nothing about why —
	// the store's own message is where SQL and constraint names would leak.
	writeFailureStoreRejected = "store_rejected"
)

// logWriteFailure records a protected write that failed for a reason that is not an authorization
// decision. It is Error level, unlike a denial, because nobody needs paging about a Student whose
// access expired and everybody does about a store that stopped accepting writes.
func (h *learningHandlers) logWriteFailure(c *gin.Context, operation, stage, failureClass string) {
	if h.logger == nil {
		return
	}
	h.logger.ProtectedWriteFailed(logging.ProtectedWriteFailureEvent{
		Method: c.Request.Method, RouteTemplate: c.FullPath(),
		Operation: operation, Stage: stage, FailureClass: failureClass,
	})
}

func (h *learningHandlers) logDenial(c *gin.Context, reason entitlement.Reason) {
	if h.logger == nil {
		return
	}
	h.logger.AuthorizationDenied(logging.AuthorizationEvent{
		Method: c.Request.Method, RouteTemplate: c.FullPath(), Capability: string(identity.CapLearningAccess), DenyReason: string(reason),
	})
}
