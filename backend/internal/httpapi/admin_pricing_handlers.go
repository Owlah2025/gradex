package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

type adminPricingHandlers struct {
	repo *catalog.Repository
}

type setPriceBody struct {
	PriceMinorUnits *int64 `json:"price_minor_units"`
	Reason          string `json:"reason"`
}

func parsePricingMutationBody(c *gin.Context) (*setPriceBody, bool) {
	var body setPriceBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return nil, false
	}
	if body.PriceMinorUnits == nil || *body.PriceMinorUnits < 0 {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code:      "INVALID_PRICE",
			Detail:    "Price must be non-negative integer minor units",
			Location:  problem.LocationBody,
			Parameter: "price_minor_units",
		}))
		return nil, false
	}
	if strings.TrimSpace(body.Reason) == "" {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code:      "REASON_REQUIRED",
			Detail:    "Reason is required",
			Location:  problem.LocationBody,
			Parameter: "reason",
		}))
		return nil, false
	}
	return &body, true
}

func (h *adminPricingHandlers) handlePricingError(c *gin.Context, err error) {
	if errors.Is(err, catalog.ErrCourseNotFound) {
		writeProblem(c, problem.NotFound())
		return
	}
	if errors.Is(err, catalog.ErrInvalidPrice) {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code:      "INVALID_PRICE",
			Detail:    "Price must be non-negative integer minor units",
			Location:  problem.LocationBody,
			Parameter: "price_minor_units",
		}))
		return
	}
	if errors.Is(err, catalog.ErrReasonRequired) {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code:      "REASON_REQUIRED",
			Detail:    "Reason is required",
			Location:  problem.LocationBody,
			Parameter: "reason",
		}))
		return
	}
	writeProblem(c, problem.Internal(""))
}

func (h *adminPricingHandlers) setCoursePrice(c *gin.Context) {
	adminAccountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")

	body, ok := parsePricingMutationBody(c)
	if !ok {
		return
	}

	change, err := h.repo.SetCoursePrice(c.Request.Context(), catalog.SetCoursePriceRequest{
		CourseID:        courseID,
		AdminAccountID:  adminAccountID,
		ActorDescriptor: adminAccountID,
		PriceMinorUnits: *body.PriceMinorUnits,
		Reason:          body.Reason,
	})
	if err != nil {
		h.handlePricingError(c, err)
		return
	}

	c.JSON(http.StatusOK, change)
}

func (h *adminPricingHandlers) setSectionPrice(c *gin.Context) {
	adminAccountID := c.GetString(ctxUserIDKey)
	courseID := c.Param("id")
	sectionID := c.Param("sectionId")

	body, ok := parsePricingMutationBody(c)
	if !ok {
		return
	}

	change, err := h.repo.SetSectionPrice(c.Request.Context(), catalog.SetSectionPriceRequest{
		CourseID:          courseID,
		SectionIdentityID: sectionID,
		AdminAccountID:    adminAccountID,
		ActorDescriptor:   adminAccountID,
		PriceMinorUnits:   *body.PriceMinorUnits,
		Reason:            body.Reason,
	})
	if err != nil {
		h.handlePricingError(c, err)
		return
	}

	c.JSON(http.StatusOK, change)
}

func (h *adminPricingHandlers) getCoursePriceHistory(c *gin.Context) {
	courseID := c.Param("id")

	history, err := h.repo.GetCoursePriceHistory(c.Request.Context(), courseID)
	if err != nil {
		h.handlePricingError(c, err)
		return
	}

	c.JSON(http.StatusOK, history)
}
