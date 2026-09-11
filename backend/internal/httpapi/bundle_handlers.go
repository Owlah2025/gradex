package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

type adminBundleHandlers struct{ repo *catalog.Repository }

type bundleMutationBody struct {
	TitleAr                string   `json:"title_ar"`
	TitleEn                string   `json:"title_en"`
	DescriptionAr          string   `json:"description_ar"`
	DescriptionEn          string   `json:"description_en"`
	CourseIDs              []string `json:"course_ids"`
	RegularPriceMinorUnits *int64   `json:"regular_price_minor_units"`
	OfferPriceMinorUnits   *int64   `json:"offer_price_minor_units"`
	PriceReason            string   `json:"price_reason"`
	ExpectedRevision       int64    `json:"expected_revision"`
}

type bundleTransitionBody struct {
	ExpectedRevision int64 `json:"expected_revision"`
}

func bundlePriceInput(body *bundleMutationBody) *catalog.BundlePriceInput {
	if body.RegularPriceMinorUnits == nil {
		return nil
	}
	return &catalog.BundlePriceInput{
		RegularMinorUnits: *body.RegularPriceMinorUnits,
		OfferMinorUnits:   body.OfferPriceMinorUnits,
		Reason:            body.PriceReason,
	}
}

func (h *adminBundleHandlers) create(c *gin.Context) {
	body := c.MustGet(strictJSONBodyContextKey).(*bundleMutationBody)
	adminID := c.GetString(ctxUserIDKey)
	bundle, err := h.repo.CreateBundle(c.Request.Context(), catalog.CreateBundleRequest{
		TitleAr: body.TitleAr, TitleEn: body.TitleEn,
		DescriptionAr: body.DescriptionAr, DescriptionEn: body.DescriptionEn,
		CourseIDs: body.CourseIDs, Price: bundlePriceInput(body),
		AdminAccountID: adminID, ActorDescriptor: adminID,
	})
	if err != nil {
		writeBundleProblem(c, err)
		return
	}
	c.JSON(http.StatusCreated, bundle)
}

func (h *adminBundleHandlers) update(c *gin.Context) {
	body := c.MustGet(strictJSONBodyContextKey).(*bundleMutationBody)
	adminID := c.GetString(ctxUserIDKey)
	bundle, err := h.repo.UpdateBundle(c.Request.Context(), catalog.UpdateBundleRequest{
		BundleID: c.Param("id"), ExpectedRevision: body.ExpectedRevision,
		TitleAr: body.TitleAr, TitleEn: body.TitleEn,
		DescriptionAr: body.DescriptionAr, DescriptionEn: body.DescriptionEn,
		CourseIDs: body.CourseIDs, Price: bundlePriceInput(body),
		AdminAccountID: adminID, ActorDescriptor: adminID,
	})
	if err != nil {
		writeBundleProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, bundle)
}

func (h *adminBundleHandlers) list(c *gin.Context) {
	items, err := h.repo.ListBundles(c.Request.Context())
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *adminBundleHandlers) listEligibleCourses(c *gin.Context) {
	search := strings.TrimSpace(c.Query("q"))
	if len(search) > 200 {
		writeProblem(c, problem.ValidationFailed())
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	result, err := h.repo.ListEligibleBundleCourses(c.Request.Context(), page, search)
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *adminBundleHandlers) get(c *gin.Context) {
	bundle, err := h.repo.GetBundle(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeBundleProblem(c, err)
		return
	}
	c.JSON(http.StatusOK, bundle)
}

func (h *adminBundleHandlers) transition(target catalog.BundleLifecycle) gin.HandlerFunc {
	return func(c *gin.Context) {
		body := c.MustGet(strictJSONBodyContextKey).(*bundleTransitionBody)
		adminID := c.GetString(ctxUserIDKey)
		bundle, err := h.repo.TransitionBundle(c.Request.Context(), catalog.TransitionBundleRequest{
			BundleID: c.Param("id"), ExpectedRevision: body.ExpectedRevision, Target: target,
			AdminAccountID: adminID, ActorDescriptor: adminID,
		})
		if err != nil {
			writeBundleProblem(c, err)
			return
		}
		c.JSON(http.StatusOK, bundle)
	}
}

func writeBundleProblem(c *gin.Context, err error) {
	switch {
	case errors.Is(err, catalog.ErrBundleNotFound):
		writeProblem(c, problem.NotFound())
	case errors.Is(err, catalog.ErrBundleVersionConflict), errors.Is(err, catalog.ErrBundleLifecycle):
		writeProblem(c, problem.New(http.StatusConflict, "bundle-state-conflict", "Bundle changed", "Refresh the Bundle and try again."))
	case errors.Is(err, catalog.ErrBundleMemberCount), errors.Is(err, catalog.ErrBundleMemberInvalid),
		errors.Is(err, catalog.ErrBundleDescription), errors.Is(err, catalog.ErrBundlePriceRequired), errors.Is(err, catalog.ErrInvalidPrice),
		errors.Is(err, catalog.ErrInvalidOfferPrice), errors.Is(err, catalog.ErrReasonRequired):
		writeProblem(c, problem.ValidationFailed())
	default:
		writeProblem(c, problem.Internal(""))
	}
}
