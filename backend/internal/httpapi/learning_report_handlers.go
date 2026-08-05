package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/entitlement"
	"github.com/Owlah2025/gradex/backend/internal/learning"
	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

// Content report submission — `POST /api/v1/learn/reports` (FR-029, FR-030, FR-033, BR-145).
//
// The request names no target. It carries the opaque encrypted report context the read that
// rendered the page issued (D-065), and the exact Course, stable target, Revision, and Asset
// Version come only from decrypting it. A route that accepted a bare `target_id` would have to
// resolve current content at submission, which records an instance the Student never saw — the
// exact defect the context exists to prevent — and would let a client choose what it reports.
//
// The context is evidence, never capability. Possessing one authorises nothing: this handler still
// requires a current, authoritative Entitlement from S4 before a row is written (FR-033), and that
// decision runs inside the insert's own transaction so it cannot go stale in between.
//
// Scope: the 5/hour throttle is T064's and is deliberately absent here; resolution is S8's.

// learningReportBody is the whole public request. There is no field for a target, a Course, a
// Revision, an Asset Version, a reporter, or a session — each of those is server-held state, and
// accepting any of them from a client would make it selectable.
type learningReportBody struct {
	ReportContext string `json:"report_context" binding:"required"`
	Reason        string `json:"reason" binding:"required"`
	Explanation   string `json:"explanation"`
}

// learningReportResponse is the acknowledgement. It carries the reporter's own new identifier and
// the instant their own submission was recorded — nothing else (FR-034, D-063).
//
// FR-034 requires an acknowledgement that discloses no Admin queue state, no other report, and no
// moderation outcome. Neither field is any of those: both describe only the row this request just
// created, for the Student who created it. Everything the row also stores — the reporter, the
// stable target, the exact visible Revision or Asset Version, the reason, the explanation, and the
// resolution column S8 will own — stays server-side.
type learningReportResponse struct {
	ReportID  string    `json:"report_id"`
	CreatedAt time.Time `json:"created_at"`
}

// learningReportResponseFields is the complete public acknowledgement contract, stated once so a
// test can hold the serialized response to it. Adding a field here is a contract change and a
// documentation change, not an implementation detail.
var learningReportResponseFields = []string{"report_id", "created_at"}

// newLearningReportResponse is the only path from the stored report to the wire.
//
// It names both fields explicitly rather than embedding, copying, or re-tagging the domain value.
// That is what makes the allowlist hold under change: a new column on learning.Report arrives here
// as an unread field, so it cannot reach a client by being added upstream — it can only reach one
// by someone editing this function and its documented contract together.
func newLearningReportResponse(report learning.Report) learningReportResponse {
	return learningReportResponse{
		ReportID:  report.ID,
		CreatedAt: report.CreatedAt.UTC(),
	}
}

// createReport is the T063 route.
//
// Ordering is deliberate. Public input is admitted and validated first — it is the same answer for
// every caller and depends on no target, so it discloses nothing. Everything after it is
// target-authority and collapses into one refusal: context verification, the current Entitlement
// decision, and the relational binding check are indistinguishable on the wire.
func (h *learningHandlers) createReport(c *gin.Context) {
	// FR-032's throttle, before anything expensive and before anything the
	// request controls. The key needs only the Account the middleware already
	// authenticated, so a forged, malformed, or foreign submission costs the
	// sender an attempt exactly like a genuine one — a throttle that only
	// counted valid reports would leave abuse unbounded (R-15).
	if !h.requireRateDecision(c, "learning-report",
		ratelimit.Input{Identifier: reportRateIdentifier(c.GetString(ctxUserIDKey))}) {
		return
	}

	body := &learningReportBody{}
	if !bindStrictJSON(c, body, learningRequestBodyLimit) {
		return
	}
	content, ok := validReportContent(c, body)
	if !ok {
		return
	}

	binding, ok := h.verifiedReportBinding(c, body.ReportContext)
	if !ok {
		return
	}

	// The lesson the S4 decision is keyed to. For a COURSE report it also proves the grant covers
	// the whole Course rather than one Section, which is the same condition Course Home required to
	// issue the context in the first place.
	lessonID, ok := h.reportAuthorityLesson(c, binding)
	if !ok {
		return
	}

	studentID := c.GetString(ctxUserIDKey)
	deniedReason := entitlement.ReasonDependency
	report, err := h.foundation.repository.CreateReportGuarded(c.Request.Context(), binding, content,
		func() time.Time { return h.now().UTC() },
		func(ctx context.Context, tx pgx.Tx) error {
			decision := h.foundation.evaluator.EvaluateInTransaction(ctx, tx, studentID, lessonID, h.now().UTC())
			if decision.Allowed {
				return nil
			}
			deniedReason = decision.Reason
			return errReportAuthorizationDenied
		})
	if err != nil {
		h.writeReportFailure(c, err, deniedReason)
		return
	}

	c.Header("Cache-Control", "no-store")
	// No Location: S5 exposes no route that reads a report back, and pointing at one would imply a
	// retrieval surface that does not exist.
	c.JSON(http.StatusCreated, newLearningReportResponse(report))
}

// errReportAuthorizationDenied marks the guard's refusal so it is distinguishable from a database
// failure internally, while both leave the route through the one external refusal.
var errReportAuthorizationDenied = errors.New("learning report authorization denied before insertion")

// validReportContent checks the parts of the request a Student can see and correct.
//
// These are public input failures, identical for every caller and independent of any target, so
// they get the ordinary validation problem rather than the protected refusal. Nothing here consults
// the context, the Course, or the Entitlement — the moment a check depends on what exists, its
// answer belongs in the uniform refusal instead.
func validReportContent(c *gin.Context, body *learningReportBody) (learning.ReportContent, bool) {
	content := learning.ReportContent{
		Reason: learning.ReportReason(body.Reason),
		// Trimmed here so a whitespace-only explanation fails the `other` requirement at the
		// boundary rather than at the database constraint, matching the domain's normalization.
		Explanation: strings.TrimSpace(body.Explanation),
	}
	if !learning.ValidReportReason(content.Reason) {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "INVALID_VALUE", Detail: "This field is invalid.",
			Location: problem.LocationBody, Pointer: "#/reason",
		}))
		return learning.ReportContent{}, false
	}
	if content.Reason == learning.ReasonOther && content.Explanation == "" {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "REQUIRED", Detail: "This field is required.",
			Location: problem.LocationBody, Pointer: "#/explanation",
		}))
		return learning.ReportContent{}, false
	}
	return content, true
}

// verifiedReportBinding decrypts and authenticates the context against this request's own
// principal and session.
//
// Every failure — malformed, tampered, foreign key, wrong version, wrong purpose, expired, issued
// in the future, another Student's, another session's — is one refusal. An expired context is
// refused rather than renewed: the Student reloads, which legitimately re-renders and re-reports
// the newer content (D-065). The token and its plaintext are never logged.
func (h *learningHandlers) verifiedReportBinding(c *gin.Context, token string) (learning.VerifiedReportBinding, bool) {
	session, ok := sessionFromContext(c)
	if !ok || session.ID == "" {
		h.logDenial(c, entitlement.ReasonDependency)
		writeProtectedUnavailable(c)
		return learning.VerifiedReportBinding{}, false
	}
	reporter := c.GetString(ctxUserIDKey)
	if reporter == "" {
		h.logDenial(c, entitlement.ReasonDependency)
		writeProtectedUnavailable(c)
		return learning.VerifiedReportBinding{}, false
	}
	binding, err := h.foundation.reportContexts.Verify(token, reporter, session.ID)
	if err != nil {
		h.logDenial(c, entitlement.ReasonNoApplicableGrant)
		writeProtectedUnavailable(c)
		return learning.VerifiedReportBinding{}, false
	}
	return binding, true
}

// reportAuthorityLesson resolves the Lesson the S4 decision is keyed to.
//
// Every Entitlement decision in this codebase is lesson-keyed and lives once, in S4 (Principle IV),
// so a Course-level report is authorised against a Lesson of that Course exactly as Course Home is.
// The Course-wide scope check mirrors Course Home's: a Section-scoped grant can never render — and
// therefore never report — the Course as a whole. Scope is read outside the transaction because it
// only tightens the decision; the authority-timing check runs inside it.
func (h *learningHandlers) reportAuthorityLesson(c *gin.Context, binding learning.VerifiedReportBinding) (string, bool) {
	if binding.TargetKind != learning.ReportTargetCourse {
		return binding.StableTargetID, true
	}

	graph, err := h.foundation.readRepository.ReadCourseGraph(c.Request.Context(), binding.CourseID)
	if err != nil || len(graph.LessonIDs()) == 0 {
		h.logDenial(c, entitlement.ReasonNoApplicableGrant)
		writeProtectedUnavailable(c)
		return "", false
	}
	lessonID := graph.LessonIDs()[0]
	decision := h.foundation.readEvaluator.EvaluateRead(c.Request.Context(), c.GetString(ctxUserIDKey), lessonID, h.now().UTC())
	if decision.State != entitlement.ReadActive || !decision.CourseWide {
		h.logDenial(c, decision.Reason)
		writeProtectedUnavailable(c)
		return "", false
	}
	return lessonID, true
}

// writeReportFailure maps every creation outcome to its external answer.
//
// A duplicate is the reporter's own still-open report for the same stable target (D-066). It is
// answered distinctly because it is reached only after current access is proven, so it discloses
// nothing about what exists — and because FR-032's two controls fail differently and must remain
// distinguishable from T064's throttle (R-11). The body carries no reference to the earlier report:
// not its identifier, its Revision, its Asset Version, nor any moderation state.
//
// Everything else is target authority — an unavailable target, an incoherent binding, a withdrawn
// Entitlement, a dependency failure — and leaves as the one protected refusal. An invalid binding
// is deliberately *not* mapped to a descriptive 400: the public request was already validated, so
// what remains describes server-held state.
func (h *learningHandlers) writeReportFailure(c *gin.Context, err error, deniedReason entitlement.Reason) {
	switch {
	case errors.Is(err, learning.ErrReportDuplicate):
		c.Header("Cache-Control", "no-store")
		writeProblem(c, problem.StateConflict())
	case errors.Is(err, errReportAuthorizationDenied):
		h.logDenial(c, deniedReason)
		writeProtectedUnavailable(c)
	case errors.Is(err, learning.ErrReportTargetUnavailable), errors.Is(err, learning.ErrReportInvalid):
		h.logDenial(c, entitlement.ReasonNoApplicableGrant)
		writeProtectedUnavailable(c)
	default:
		h.logDenial(c, entitlement.ReasonDependency)
		writeProtectedUnavailable(c)
	}
}
