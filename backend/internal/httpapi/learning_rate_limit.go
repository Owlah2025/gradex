package httpapi

import (
	"math"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

func (h *learningHandlers) requireRateDecision(c *gin.Context, endpoint string, input ratelimit.Input) bool {
	decision := h.foundation.limiter.Decide(c.Request.Context(), h.foundation.policies[endpoint], input)
	c.Set(limiterOutcomeContextKey, string(decision.Outcome))
	if decision.Allowed {
		return true
	}
	if decision.Outcome == ratelimit.OutcomeDenied || decision.Outcome == ratelimit.OutcomeFallbackDenied {
		c.Header("Cache-Control", "no-store")
		if seconds := int(math.Ceil(decision.RetryAfter.Seconds())); seconds > 0 {
			c.Header("Retry-After", strconv.Itoa(seconds))
		}
		writeProblem(c, problem.RateLimited())
		return false
	}
	h.logDenial(c, "RATE_LIMIT_UNAVAILABLE")
	writeProtectedUnavailable(c)
	return false
}

func progressRateIdentifier(studentID, lessonID string) string {
	return studentID + "\x00" + lessonID
}

// reportRateIdentifier keys the report throttle on the authenticated Student
// and nothing else (FR-032).
//
// It is a named function rather than the bare Account ID so the scope is
// stated where it is chosen: not the session, so signing in again does not
// reset the quota; not the Course or the report target, so a Student cannot
// open a fresh quota per Course; and never the request body or the encrypted
// context, which this decision runs before either is read.
func reportRateIdentifier(studentID string) string { return studentID }

// playbackRateIdentifier keys the playback issuance quota on the authenticated
// Student and nothing else (FR-017).
//
// Named for the same reason the report identifier is: the scope is stated where it
// is chosen. Not the Lesson, so Lesson-hopping cannot open a fresh quota per Lesson
// -- the extraction pattern R-04 sized this limit against. Not the session, so
// signing in again does not reset it. Never the request path or body.
func playbackRateIdentifier(studentID string) string { return studentID }
