package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
	"github.com/Owlah2025/gradex/backend/internal/requestid"
)

const (
	staffInvitationBodyLimit int64 = 1024
	staffPreviewBodyLimit    int64 = 512
	staffCompletionBodyLimit int64 = 2048
	staffSuspendBodyLimit    int64 = 512
	staffReinstateBodyLimit  int64 = 512
)

type staffService interface {
	CreateStaffInvitation(ctx context.Context, req identity.CreateStaffInvitationRequest) (identity.IssuedStaffInvitation, error)
	PreviewStaffInvitation(ctx context.Context, bearer string, now time.Time) (identity.StaffInvitationPreview, error)
	CompleteStaffInvitation(ctx context.Context, req identity.CompleteStaffInvitationRequest) (identity.CompleteStaffInvitationResult, error)
	RevokeStaffInvitation(ctx context.Context, req identity.RevokeStaffInvitationRequest) error
	SuspendAccount(ctx context.Context, req identity.SuspendAccountRequest) (identity.SuspendAccountResult, error)
	ReinstateAccount(ctx context.Context, req identity.ReinstateAccountRequest) (identity.ReinstateAccountResult, error)
	ListPendingInvitations(ctx context.Context, principal identity.Principal) ([]identity.StaffInvitation, error)
}

type StaffFoundation struct {
	service          staffService
	compromised      identity.CompromisedRangeSource
	limiter          *ratelimit.Limiter
	endpointPolicies map[string]ratelimit.Policy
	recentAuthWindow time.Duration
}

type StaffFoundationOptions struct {
	Service          staffService
	Compromised      identity.CompromisedRangeSource
	Limiter          *ratelimit.Limiter
	EndpointPolicies map[string]ratelimit.Policy
	RecentAuthWindow time.Duration
}

var requiredStaffPolicyEndpoints = [...]string{
	"staff-invitations-create",
	"staff-invitations-preview",
	"staff-invitations-complete",
}

func NewStaffFoundation(options StaffFoundationOptions) (*StaffFoundation, error) {
	if options.Service == nil {
		return nil, errors.New("staff service is required")
	}
	if options.Limiter == nil {
		return nil, errors.New("staff rate limiter is required")
	}
	if len(options.EndpointPolicies) == 0 {
		return nil, errors.New("staff endpoint policies are required")
	}
	endpointPolicies := make(map[string]ratelimit.Policy, len(options.EndpointPolicies))
	for endpoint, policy := range options.EndpointPolicies {
		if endpoint == "" || endpoint != policy.Endpoint {
			return nil, errors.New("staff endpoint policy key does not match its endpoint")
		}
		if err := policy.Validate(); err != nil {
			return nil, err
		}
		endpointPolicies[endpoint] = policy
	}
	for _, endpoint := range requiredStaffPolicyEndpoints {
		if _, configured := endpointPolicies[endpoint]; !configured {
			return nil, fmt.Errorf("required staff endpoint policy %q is missing", endpoint)
		}
	}
	if options.RecentAuthWindow <= 0 {
		return nil, errors.New("staff recent authentication window must be positive")
	}
	return &StaffFoundation{
		service:          options.Service,
		compromised:      options.Compromised,
		limiter:          options.Limiter,
		endpointPolicies: endpointPolicies,
		recentAuthWindow: options.RecentAuthWindow,
	}, nil
}

type staffHandlers struct {
	service          staffService
	compromised      identity.CompromisedRangeSource
	recentAuthWindow time.Duration
}

type createInvitationRequest struct {
	Email string        `json:"email" binding:"required"`
	Role  identity.Role `json:"role" binding:"required"`
	// Optional. Omitted means the request's Accept-Language, which itself falls
	// back to the platform default of Arabic.
	Locale identity.Locale `json:"locale"`
}

type invitationPreviewRequest struct {
	Bearer string `json:"bearer" binding:"required"`
}

type completeInvitationRequest struct {
	Bearer      string `json:"bearer" binding:"required"`
	DisplayName string `json:"display_name" binding:"required"`
	Password    string `json:"password" binding:"required"`
}

type suspendStaffRequest struct {
	Reason string `json:"reason" binding:"required"`
}

type reinstateStaffRequest struct {
	Reason string `json:"reason"`
}

func (h *staffHandlers) createInvitation(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok {
		writeProblem(c, problem.NotAuthorized())
		return
	}

	session, ok := sessionFromContext(c)
	if !ok {
		writeProblem(c, problem.Unauthenticated())
		return
	}
	req := c.MustGet(strictJSONBodyContextKey).(*createInvitationRequest)

	now := time.Now().UTC()
	reqID := requestid.FromContext(c.Request.Context())

	// The invitee has no Account yet, so the notification locale comes from the
	// inviting request: an explicit body field, else Accept-Language, else the
	// platform default. Same resolution the admission surface already uses.
	locale := req.Locale
	if locale == "" {
		resolved, ok := requestedLocale(c.GetHeader("Accept-Language"))
		if !ok {
			writeProblem(c, problem.ValidationFailed())
			return
		}
		locale = resolved
	}

	issued, err := h.service.CreateStaffInvitation(c.Request.Context(), identity.CreateStaffInvitationRequest{
		ActorPrincipal:   principal,
		ActorSession:     session,
		RecentAuthWindow: h.recentAuthWindow,
		Email:            req.Email,
		Role:             req.Role,
		Locale:           locale,
		Now:              now,
		RequestID:        reqID,
	})
	if err != nil {
		if errors.Is(err, identity.ErrInvalidLocale) {
			writeProblem(c, problem.ValidationFailed())
			return
		}
		if errors.Is(err, identity.ErrAccountAlreadyExists) {
			writeProblem(c, problem.StateConflict())
			return
		}
		if errors.Is(err, identity.ErrRecentAuthRequired) {
			writeProblem(c, problem.NotAuthorized())
			return
		}
		if errors.Is(err, identity.ErrUnauthorized) {
			writeProblem(c, problem.NotAuthorized())
			return
		}
		writeProblem(c, problem.Internal(reqID))
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":           issued.Invitation.ID,
		"email":        issued.Invitation.Email,
		"invited_role": issued.Invitation.InvitedRole,
		"bearer":       issued.BearerToken,
		"created_at":   issued.Invitation.CreatedAt,
	})
}

func (h *staffHandlers) listInvitations(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok {
		writeProblem(c, problem.NotAuthorized())
		return
	}

	invitations, err := h.service.ListPendingInvitations(c.Request.Context(), principal)
	if err != nil {
		if errors.Is(err, identity.ErrUnauthorized) {
			writeProblem(c, problem.NotAuthorized())
			return
		}
		writeProblem(c, problem.Internal(requestid.FromContext(c.Request.Context())))
		return
	}

	items := make([]gin.H, 0, len(invitations))
	for _, inv := range invitations {
		items = append(items, gin.H{
			"id":           inv.ID,
			"email":        inv.Email,
			"invited_role": inv.InvitedRole,
			"state":        inv.State,
			"created_at":   inv.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{"invitations": items})
}

func (h *staffHandlers) previewInvitation(c *gin.Context) {
	req := c.MustGet(strictJSONBodyContextKey).(*invitationPreviewRequest)

	now := time.Now().UTC()
	prev, err := h.service.PreviewStaffInvitation(c.Request.Context(), req.Bearer, now)
	if err != nil {
		if errors.Is(err, identity.ErrInvitationInvalid) {
			writeProblem(c, problem.TokenInvalid())
			return
		}
		writeProblem(c, problem.Internal(requestid.FromContext(c.Request.Context())))
		return
	}

	// Non-enumerating preview: returns invited_role and state ONLY.
	c.JSON(http.StatusOK, gin.H{
		"invited_role": prev.InvitedRole,
		"state":        prev.State,
	})
}

func (h *staffHandlers) completeInvitation(c *gin.Context) {
	req := c.MustGet(strictJSONBodyContextKey).(*completeInvitationRequest)

	now := time.Now().UTC()
	reqID := requestid.FromContext(c.Request.Context())

	// No role field accepted from request! Authoritative from stored invitation.
	res, err := h.service.CompleteStaffInvitation(c.Request.Context(), identity.CompleteStaffInvitationRequest{
		Bearer:      req.Bearer,
		DisplayName: req.DisplayName,
		Password:    config.NewSecret(req.Password),
		Compromised: h.compromised,
		Now:         now,
		RequestID:   reqID,
	})

	if err != nil {
		if errors.Is(err, identity.ErrInvitationAlreadyUsed) || errors.Is(err, identity.ErrInvitationExpired) || errors.Is(err, identity.ErrInvitationSuperseded) || errors.Is(err, identity.ErrInvitationRevoked) || errors.Is(err, identity.ErrInvitationInvalid) {
			writeProblem(c, problem.TokenInvalid())
			return
		}
		if errors.Is(err, identity.ErrPasswordPolicy) {
			writeProblem(c, problem.ValidationFailed())
			return
		}
		if errors.Is(err, identity.ErrAccountAlreadyExists) {
			writeProblem(c, problem.StateConflict())
			return
		}
		writeProblem(c, problem.Internal(reqID))
		return
	}

	// Completion issues NO session (I8). Onboarding complete, invitee logs in normally.
	c.JSON(http.StatusOK, gin.H{
		"account_id":   res.AccountID,
		"invited_role": res.InvitedRole,
	})
}

func (h *staffHandlers) revokeInvitation(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	session, ok := sessionFromContext(c)
	if !ok {
		writeProblem(c, problem.Unauthenticated())
		return
	}

	invitationID := c.Param("id")
	if invitationID == "" {
		writeProblem(c, problem.ValidationFailed())
		return
	}

	now := time.Now().UTC()
	reqID := requestid.FromContext(c.Request.Context())

	err := h.service.RevokeStaffInvitation(c.Request.Context(), identity.RevokeStaffInvitationRequest{
		ActorPrincipal:   principal,
		ActorSession:     session,
		RecentAuthWindow: h.recentAuthWindow,
		InvitationID:     invitationID,
		Now:              now,
		RequestID:        reqID,
	})

	if err != nil {
		if errors.Is(err, identity.ErrInvitationNotFound) {
			writeProblem(c, problem.NotFound())
			return
		}
		if errors.Is(err, identity.ErrRecentAuthRequired) || errors.Is(err, identity.ErrUnauthorized) {
			writeProblem(c, problem.NotAuthorized())
			return
		}
		writeProblem(c, problem.Internal(reqID))
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *staffHandlers) suspendStaff(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	session, ok := sessionFromContext(c)
	if !ok {
		writeProblem(c, problem.Unauthenticated())
		return
	}

	subjectID := c.Param("id")
	var req suspendStaffRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeProblem(c, problem.ValidationFailed())
		return
	}

	now := time.Now().UTC()
	reqID := requestid.FromContext(c.Request.Context())

	res, err := h.service.SuspendAccount(c.Request.Context(), identity.SuspendAccountRequest{
		ActorPrincipal:   principal,
		ActorSession:     session,
		RecentAuthWindow: h.recentAuthWindow,
		SubjectAccountID: subjectID,
		Reason:           req.Reason,
		Now:              now,
		RequestID:        reqID,
	})

	if err != nil {
		if errors.Is(err, identity.ErrAccountNotFound) {
			writeProblem(c, problem.NotFound())
			return
		}
		if errors.Is(err, identity.ErrRecentAuthRequired) || errors.Is(err, identity.ErrUnauthorized) {
			writeProblem(c, problem.NotAuthorized())
			return
		}
		writeProblem(c, problem.Internal(reqID))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"already_suspended": res.AlreadySuspended,
		"revision":          res.Revision,
		"epoch":             res.Epoch,
	})
}

func (h *staffHandlers) reinstateStaff(c *gin.Context) {
	principal, ok := principalFrom(c)
	if !ok {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	session, ok := sessionFromContext(c)
	if !ok {
		writeProblem(c, problem.Unauthenticated())
		return
	}

	subjectID := c.Param("id")
	var req reinstateStaffRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			writeProblem(c, problem.ValidationFailed())
			return
		}
	}

	now := time.Now().UTC()
	reqID := requestid.FromContext(c.Request.Context())

	res, err := h.service.ReinstateAccount(c.Request.Context(), identity.ReinstateAccountRequest{
		ActorPrincipal:   principal,
		ActorSession:     session,
		RecentAuthWindow: h.recentAuthWindow,
		SubjectAccountID: subjectID,
		Reason:           req.Reason,
		Now:              now,
		RequestID:        reqID,
	})

	if err != nil {
		if errors.Is(err, identity.ErrAccountNotFound) {
			writeProblem(c, problem.NotFound())
			return
		}
		if errors.Is(err, identity.ErrRecentAuthRequired) || errors.Is(err, identity.ErrUnauthorized) {
			writeProblem(c, problem.NotAuthorized())
			return
		}
		writeProblem(c, problem.Internal(reqID))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"already_active": res.AlreadyActive,
	})
}

func sessionFromContext(c *gin.Context) (identity.Session, bool) {
	// Utility to retrieve authenticated session from context if attached by auth middleware
	if v, ok := c.Get("authenticated_session"); ok {
		if sess, ok := v.(identity.Session); ok {
			return sess, true
		}
	}
	return identity.Session{}, false
}
