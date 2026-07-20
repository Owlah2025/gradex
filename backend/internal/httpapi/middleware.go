package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
)

const ctxUserIDKey = "userID"

var (
	errNotInstructor = errors.New("not the instructor for this lesson")
	errNoAccess      = errors.New("no access to this lesson")
)

func errorResponse(c *gin.Context, status int, err error) {
	c.AbortWithStatusJSON(status, gin.H{"error": err.Error()})
}

// requireAuth resolves the caller's identity but does not check any
// entitlement — used as the shared first step by both the instructor and
// student middleware groups.
func requireAuth(authenticator auth.Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, err := authenticator.UserFromRequest(c)
		if err != nil {
			errorResponse(c, http.StatusUnauthorized, err)
			return
		}
		c.Set(ctxUserIDKey, userID)
		c.Next()
	}
}

// requireInstructor checks the authenticated user owns the :lessonID lesson
// as its instructor. Must run after requireAuth.
func requireInstructor(entitlements auth.EntitlementChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString(ctxUserIDKey)
		lessonID := c.Param("lessonID")
		ok, err := entitlements.IsInstructorForLesson(c.Request.Context(), userID, lessonID)
		if err != nil {
			errorResponse(c, http.StatusInternalServerError, err)
			return
		}
		if !ok {
			errorResponse(c, http.StatusForbidden, errNotInstructor)
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
			errorResponse(c, http.StatusInternalServerError, err)
			return
		}
		if !ok {
			errorResponse(c, http.StatusForbidden, errNoAccess)
			return
		}
		c.Next()
	}
}
