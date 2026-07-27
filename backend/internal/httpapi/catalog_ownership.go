package httpapi

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

// CourseOwnershipChecker resolves course ownership for authorization middleware.
type CourseOwnershipChecker interface {
	IsCourseOwner(ctx context.Context, courseID string, accountID string) (bool, error)
}

// RequireCourseOwnership returns middleware enforcing ownership over a course.
//
// Standing Clause: required dependencies (checker and logger) are validated at construction,
// refusing to build without them.
//
// Denial semantics (FR-002, spec US1 scenario 2):
// A non-owner, Student, or anonymous caller requesting a Course they do not own
// receives a uniform NotAuthorized refusal that does NOT distinguish "does not exist"
// from "not yours".
func RequireCourseOwnership(checker CourseOwnershipChecker, logger *logging.Logger) (gin.HandlerFunc, error) {
	if checker == nil {
		return nil, fmt.Errorf("course ownership checker is required")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	return func(c *gin.Context) {
		accountID := c.GetString(ctxUserIDKey)
		courseID := c.Param("id")
		if courseID == "" {
			courseID = c.Param("courseID")
		}

		if accountID == "" || courseID == "" {
			writeProblem(c, problem.NotAuthorized())
			return
		}

		isOwner, err := checker.IsCourseOwner(c.Request.Context(), courseID, accountID)
		if err != nil {
			logger.AuthorizationFault(logging.AuthorizationFaultEvent{
				Method:        c.Request.Method,
				RouteTemplate: c.FullPath(),
				Capability:    "COURSE_OWNERSHIP",
				Err:           err,
			})
			writeProblem(c, problem.Internal(""))
			return
		}

		if !isOwner {
			writeProblem(c, problem.NotAuthorized())
			return
		}

		c.Next()
	}, nil
}
