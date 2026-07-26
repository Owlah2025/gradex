package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/requestid"
)

const (
	registrationBodyLimit            int64 = 2048
	verificationRequestBodyLimit     int64 = 512
	verificationConsumptionBodyLimit int64 = 512
)

type admissionCommands interface {
	RegisterStudent(context.Context, identity.StudentRegistration) error
	RequestEmailVerification(context.Context, identity.VerificationRequest) error
	VerifyEmail(context.Context, string, string) error
}

type identityHandlers struct {
	service  admissionCommands
	policies identity.PolicySetResolver
}

type studentRegistrationRequest struct {
	DisplayName string `json:"display_name" binding:"required"`
	Email       string `json:"email" binding:"required"`
	Password    string `json:"password" binding:"required"`
	Locale      string `json:"locale" binding:"required"`
	PolicySetID string `json:"policy_set_id" binding:"required"`
}

type verificationRequestBody struct {
	Email string `json:"email" binding:"required"`
}

type verificationConsumptionBody struct {
	Token string `json:"token" binding:"required"`
}

func (h *identityHandlers) currentPolicySet(c *gin.Context) {
	locale, ok := requestedLocale(c.GetHeader("Accept-Language"))
	if !ok {
		writeProblem(c, problem.NotAcceptable())
		return
	}
	set, err := h.policies.Current(c.Request.Context(), locale)
	if err != nil {
		c.Header("Cache-Control", "no-store")
		setAdmissionFailureStage(c, admissionFailureStageDomain)
		writeProblem(c, problem.RegistrationUnavailable())
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Language", string(locale))
	c.Header("Vary", "Accept-Language")
	c.JSON(http.StatusOK, policySetResponse(set))
}

func policySetResponse(set identity.RegistrationPolicySet) gin.H {
	policies := make([]gin.H, 0, len(set.Policies))
	for _, policy := range set.Policies {
		policies = append(policies, gin.H{
			"kind": policy.Kind, "version": policy.Version,
			"label": policy.Label, "url": policy.URL,
		})
	}
	return gin.H{"id": set.ID, "policies": policies}
}

func requestedLocale(header string) (identity.Locale, bool) {
	if strings.TrimSpace(header) == "" {
		return identity.LocaleArabic, true
	}
	for _, preference := range strings.Split(header, ",") {
		tag := strings.ToLower(strings.TrimSpace(strings.SplitN(preference, ";", 2)[0]))
		switch {
		case tag == "ar" || strings.HasPrefix(tag, "ar-"):
			return identity.LocaleArabic, true
		case tag == "en" || strings.HasPrefix(tag, "en-"):
			return identity.LocaleEnglish, true
		}
	}
	return "", false
}

func (h *identityHandlers) registerStudent(
	c *gin.Context,
	request *studentRegistrationRequest,
) {
	err := h.service.RegisterStudent(c.Request.Context(), identity.StudentRegistration{
		DisplayName: request.DisplayName,
		Email:       request.Email,
		Password:    config.NewSecret(request.Password),
		Locale:      identity.Locale(request.Locale),
		PolicySetID: request.PolicySetID,
		RequestID:   requestid.FromContext(c.Request.Context()),
	})
	if err != nil {
		h.writeAdmissionError(c, err)
		return
	}
	writeAdmissionSuccess(c, http.StatusAccepted, gin.H{
		"code": "REGISTRATION_REQUEST_ACCEPTED",
	})
}

func (h *identityHandlers) registerBoundStudent(c *gin.Context) {
	request := c.MustGet(strictJSONBodyContextKey).(*studentRegistrationRequest)
	h.registerStudent(c, request)
}

func (h *identityHandlers) requestVerification(
	c *gin.Context,
	request *verificationRequestBody,
) {
	err := h.service.RequestEmailVerification(
		c.Request.Context(),
		identity.VerificationRequest{
			Email: request.Email, RequestID: requestid.FromContext(c.Request.Context()),
		},
	)
	if err != nil {
		h.writeAdmissionError(c, err)
		return
	}
	writeAdmissionSuccess(c, http.StatusAccepted, gin.H{
		"code": "VERIFICATION_REQUEST_ACCEPTED",
	})
}

func (h *identityHandlers) requestBoundVerification(c *gin.Context) {
	request := c.MustGet(strictJSONBodyContextKey).(*verificationRequestBody)
	h.requestVerification(c, request)
}

func (h *identityHandlers) consumeVerification(
	c *gin.Context,
	request *verificationConsumptionBody,
) {
	err := h.service.VerifyEmail(
		c.Request.Context(),
		request.Token,
		requestid.FromContext(c.Request.Context()),
	)
	if err != nil {
		h.writeAdmissionError(c, err)
		return
	}
	writeAdmissionSuccess(c, http.StatusOK, gin.H{"status": "VERIFIED"})
}

func (h *identityHandlers) consumeBoundVerification(c *gin.Context) {
	request := c.MustGet(strictJSONBodyContextKey).(*verificationConsumptionBody)
	h.consumeVerification(c, request)
}

func writeAdmissionSuccess(c *gin.Context, status int, body gin.H) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, body)
}

func (h *identityHandlers) writeAdmissionError(c *gin.Context, err error) {
	c.Header("Cache-Control", "no-store")
	setAdmissionFailureStage(c, admissionFailureStageDomain)
	switch {
	case errors.Is(err, identity.ErrTokenInvalid):
		writeProblem(c, problem.TokenInvalid())
	case errors.Is(err, identity.ErrDeliveryUnavailable):
		writeProblem(c, problem.TransactionalDeliveryUnavailable())
	case errors.Is(err, identity.ErrAdmissionUnavailable):
		writeProblem(c, problem.RegistrationUnavailable())
	case isAdmissionValidation(err):
		writeProblem(c, admissionValidationProblem(err))
	default:
		writeProblem(c, problem.Internal(requestid.FromContext(c.Request.Context())))
	}
}

func admissionValidationProblem(err error) problem.Problem {
	field := "email"
	switch {
	case errors.Is(err, identity.ErrInvalidDisplayName):
		field = "display_name"
	case errors.Is(err, identity.ErrInvalidLocale):
		field = "locale"
	case errors.Is(err, identity.ErrPasswordPolicy):
		field = "password"
	case errors.Is(err, identity.ErrPolicySetStale):
		field = "policy_set_id"
	}
	return problem.ValidationFailed().WithViolations(problem.Violation{
		Code: "INVALID_VALUE", Detail: "This field is invalid.",
		Location: problem.LocationBody, Pointer: "#/" + field,
	})
}

func isAdmissionValidation(err error) bool {
	return errors.Is(err, identity.ErrInvalidDisplayName) ||
		errors.Is(err, identity.ErrInvalidEmail) ||
		errors.Is(err, identity.ErrInvalidLocale) ||
		errors.Is(err, identity.ErrPasswordPolicy) ||
		errors.Is(err, identity.ErrPolicySetStale)
}
