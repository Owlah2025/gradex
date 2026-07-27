package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/requestid"
)

type staffService interface {
	CreateStaffInvitation(ctx context.Context, req identity.CreateStaffInvitationRequest) (identity.IssuedStaffInvitation, error)
	PreviewStaffInvitation(ctx context.Context, bearer string, now time.Time) (identity.StaffInvitationPreview, error)
	CompleteStaffInvitation(ctx context.Context, req identity.CompleteStaffInvitationRequest) (identity.CompleteStaffInvitationResult, error)
	RevokeStaffInvitation(ctx context.Context, req identity.RevokeStaffInvitationRequest) error
	SuspendAccount(ctx context.Context, req identity.SuspendAccountRequest) (identity.SuspendAccountResult, error)
	ReinstateAccount(ctx context.Context, req identity.ReinstateAccountRequest) (identity.ReinstateAccountResult, error)
}

type StaffFoundation struct {
	service     staffService
	compromised identity.CompromisedRangeSource
}

func NewStaffFoundation(service staffService, compromised identity.CompromisedRangeSource) *StaffFoundation {
	return &StaffFoundation{service: service, compromised: compromised}
}

type staffHandlers struct {
	service     staffService
	compromised identity.CompromisedRangeSource
}

type createInvitationRequest struct {
	Email string        `json:"email" binding:"required"`
	Role  identity.Role `json:"role" binding:"required"`
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

	var req createInvitationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeProblem(c, problem.ValidationFailed())
		return
	}

	now := time.Now().UTC()
	reqID := requestid.FromContext(c.Request.Context())

	issued, err := h.service.CreateStaffInvitation(c.Request.Context(), identity.CreateStaffInvitationRequest{
		ActorPrincipal:   principal,
		ActorSession:     session,
		RecentAuthWindow: 10 * time.Minute,
		Email:            req.Email,
		Role:             req.Role,
		Now:              now,
		RequestID:        reqID,
	})
	if err != nil {
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
	_, ok := principalFrom(c)
	if !ok {
		writeProblem(c, problem.NotAuthorized())
		return
	}
	// Stub/response for listing pending invitations
	c.JSON(http.StatusOK, gin.H{
		"invitations": []gin.H{},
	})
}

func (h *staffHandlers) previewInvitation(c *gin.Context) {
	var req invitationPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeProblem(c, problem.ValidationFailed())
		return
	}

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
	var req completeInvitationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeProblem(c, problem.ValidationFailed())
		return
	}

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
		RecentAuthWindow: 10 * time.Minute,
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
		RecentAuthWindow: 10 * time.Minute,
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
	_ = c.ShouldBindJSON(&req)

	now := time.Now().UTC()
	reqID := requestid.FromContext(c.Request.Context())

	res, err := h.service.ReinstateAccount(c.Request.Context(), identity.ReinstateAccountRequest{
		ActorPrincipal:   principal,
		ActorSession:     session,
		RecentAuthWindow: 10 * time.Minute,
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
