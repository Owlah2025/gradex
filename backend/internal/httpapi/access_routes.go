package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/access"
	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

const accessMutationBodyLimit = 16 * 1024

type accessHandlers struct {
	repo *access.Repository
}

type setDefaultAccessExpiryBody struct {
	Date   string `json:"date"`
	Reason string `json:"reason"`
}

type defaultAccessExpiryResponse struct {
	CourseID            string    `json:"course_id"`
	DefaultAccessEndsAt time.Time `json:"default_access_ends_at"`
	Reason              string    `json:"reason"`
}

func mountAccessRoutes(
	v1 *gin.RouterGroup,
	foundation *AccessFoundation,
	sessionFoundation *SessionFoundation,
	authenticator auth.Authenticator,
	principals identity.PrincipalResolver,
	logger *logging.Logger,
) error {
	if foundation == nil || foundation.repository == nil {
		return errors.New("access foundation and repository are required")
	}

	h := &accessHandlers{repo: foundation.repository}

	adminAccessGroup := v1.Group("/admin/courses")
	adminAccessGroup.Use(
		sessionFoundation.requireSessionMutationSecurity(),
		requireAuth(authenticator),
		requireCapability(principals, logger, identity.CapCourseAccessGrant),
	)
	{
		adminAccessGroup.PUT("/:id/default-access-expiry",
			strictJSONMiddleware(func() any { return &setDefaultAccessExpiryBody{} }, accessMutationBodyLimit),
			h.setCourseDefaultAccessExpiry,
		)
	}

	return nil
}

func (h *accessHandlers) setCourseDefaultAccessExpiry(c *gin.Context) {
	adminAccountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")

	body := c.MustGet(strictJSONBodyContextKey).(*setDefaultAccessExpiryBody)

	if strings.TrimSpace(body.Date) == "" {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code:      "DATE_REQUIRED",
			Detail:    "Expiry date (YYYY-MM-DD) is required",
			Location:  problem.LocationBody,
			Parameter: "date",
		}))
		return
	}

	if strings.TrimSpace(body.Reason) == "" {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code:      "REASON_REQUIRED",
			Detail:    "Reason is required",
			Location:  problem.LocationBody,
			Parameter: "reason",
		}))
		return
	}

	utcExpiry, err := access.ConvertKuwaitDateToUTCExpiry(body.Date)
	if err != nil {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code:      "INVALID_DATE_FORMAT",
			Detail:    "Date must be a valid Kuwait local date in YYYY-MM-DD format",
			Location:  problem.LocationBody,
			Parameter: "date",
		}))
		return
	}

	err = h.repo.SetCourseDefaultAccessExpiry(c.Request.Context(), access.SetCourseDefaultAccessExpiryParams{
		CourseID:            courseID,
		AdminAccountID:      adminAccountID,
		ActorDescriptor:     adminAccountID,
		DefaultAccessEndsAt: utcExpiry,
		Reason:              body.Reason,
	})
	if err != nil {
		if errors.Is(err, access.ErrCourseNotFound) {
			writeProblem(c, problem.NotFound())
			return
		}
		if errors.Is(err, access.ErrReasonRequired) || errors.Is(err, access.ErrExpiryRequired) {
			writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
				Code:      "VALIDATION_FAILED",
				Detail:    err.Error(),
				Location:  problem.LocationBody,
				Parameter: "date",
			}))
			return
		}
		writeProblem(c, problem.Internal(""))
		return
	}

	c.JSON(http.StatusOK, defaultAccessExpiryResponse{
		CourseID:            courseID,
		DefaultAccessEndsAt: utcExpiry,
		Reason:              body.Reason,
	})
}
