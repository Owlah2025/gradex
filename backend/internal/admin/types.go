package admin

import (
	"errors"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/identity"
)

var (
	ErrRepositoryNil   = errors.New("admin repository requires a database pool")
	ErrInvalidInput    = errors.New("admin read input is invalid")
	ErrUnauthorized    = errors.New("admin read requires the user directory capability")
	ErrAccountNotFound = errors.New("admin account was not found")
	ErrCourseNotFound  = errors.New("admin course was not found")
)

const (
	ActionUserSearched = "ADMIN_USER_SEARCHED"
	ActionUserViewed   = "ADMIN_USER_VIEWED"
	ActionAuditViewed  = "ADMIN_AUDIT_VIEWED"

	DirectoryTargetType = "ACCOUNT_DIRECTORY"
	DirectoryTargetID   = "DIRECTORY"
	AuditTargetType     = "AUDIT_LOG"
	AuditTargetID       = "AUDIT_LOG"
)

type AccountDirectoryRequest struct {
	Principal     identity.Principal
	CorrelationID string
	Locale        identity.Locale
	Query         string
	Role          identity.Role
	Status        identity.AccountStatus
	InstitutionID string
	JoinedFrom    *time.Time
	JoinedTo      *time.Time
	Page          int
	Limit         int
}

type AccountDirectoryEntry struct {
	ID                   string                 `json:"id"`
	DisplayName          string                 `json:"display_name"`
	Email                string                 `json:"email"`
	Role                 identity.Role          `json:"role"`
	Status               identity.AccountStatus `json:"status"`
	Locale               identity.Locale        `json:"locale"`
	EmailVerified        bool                   `json:"email_verified"`
	CreatedAt            time.Time              `json:"created_at"`
	InstitutionLabel     string                 `json:"institution_label"`
	LastSignInActivityAt *time.Time             `json:"last_sign_in_activity_at,omitempty"`
}

type AccountDirectoryResult struct {
	Accounts []AccountDirectoryEntry `json:"accounts"`
	Total    int                     `json:"total"`
	Page     int                     `json:"page"`
	Limit    int                     `json:"limit"`
}

type AccountIdentityRequest struct {
	Principal     identity.Principal
	CorrelationID string
	Locale        identity.Locale
	AccountID     string
}

type AuditEventRequest struct {
	Principal      identity.Principal
	CorrelationID  string
	ActorAccountID string
	ActorQuery     string
	TargetType     string
	TargetID       string
	Action         string
	Module         string
	OccurredFrom   *time.Time
	OccurredTo     *time.Time
	Page           int
	Limit          int
}

type AuditEvent struct {
	ID               string         `json:"id"`
	OccurredAt       time.Time      `json:"occurred_at"`
	ActorAccountID   *string        `json:"actor_account_id,omitempty"`
	ActorDisplayName string         `json:"actor_display_name"`
	ActorRole        string         `json:"actor_role"`
	Action           string         `json:"action"`
	Module           string         `json:"module"`
	TargetType       string         `json:"target_type"`
	TargetID         string         `json:"target_id"`
	TargetLabel      string         `json:"target_label,omitempty"`
	Reason           string         `json:"reason"`
	Metadata         map[string]any `json:"metadata"`
}

type AuditEventResult struct {
	Events []AuditEvent `json:"audit_events"`
	Total  int          `json:"total"`
	Page   int          `json:"page"`
	Limit  int          `json:"limit"`
}
