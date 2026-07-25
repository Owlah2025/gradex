package httpapi

import (
	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

const ctxUserIDKey = "userID"

// requireAuth resolves the caller's identity but does not check any
// entitlement — used as the shared first step by both the instructor and
// student middleware groups.
//
// The authenticator's error is not reported: it describes why authentication
// failed, and §5 keeps hidden Account state out of public responses. The
// response is the uniform challenge for every cause.
func requireAuth(authenticator auth.Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, err := authenticator.UserFromRequest(c)
		if err != nil {
			writeProblem(c, problem.Unauthenticated())
			return
		}
		c.Set(ctxUserIDKey, userID)
		c.Next()
	}
}

// requireInstructor checks the authenticated user owns the :lessonID lesson
// as its instructor. Must run after requireAuth.
//
// A lookup failure and a denial are reported differently — one is a fault, the
// other a decision — but neither says which lesson, who owns it, or whether it
// exists. §6.1 keeps typed policy reasons in security monitoring, not in the
// response.
func requireInstructor(entitlements auth.EntitlementChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString(ctxUserIDKey)
		lessonID := c.Param("lessonID")
		ok, err := entitlements.IsInstructorForLesson(c.Request.Context(), userID, lessonID)
		if err != nil {
			writeProblem(c, problem.Internal(""))
			return
		}
		if !ok {
			writeProblem(c, problem.NotAuthorized())
			return
		}
		c.Next()
	}
}

// requireStudentAccess checks the authenticated user has purchased/enrolled
// in the course containing :lessonID. Must run after requireAuth.
func requireStudentAccess(entitlements auth.EntitlementChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString(ctxUserIDKey)
		lessonID := c.Param("lessonID")
		ok, err := entitlements.HasAccess(c.Request.Context(), userID, lessonID)
		if err != nil {
			writeProblem(c, problem.Internal(""))
			return
		}
		if !ok {
			writeProblem(c, problem.NotAuthorized())
			return
		}
		c.Next()
	}
}
