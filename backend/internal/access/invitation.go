package access

import (
	"errors"
	"time"
)

type State string

const (
	StatePendingStudentAcceptance State = "PENDING_STUDENT_ACCEPTANCE"
	StatePendingAdminApproval     State = "PENDING_ADMIN_APPROVAL"
	StateApproved                 State = "APPROVED"
	StateRejected                 State = "REJECTED"
	StateCancelled                State = "CANCELLED"
)

func (s State) Valid() bool {
	switch s {
	case StatePendingStudentAcceptance, StatePendingAdminApproval, StateApproved, StateRejected, StateCancelled:
		return true
	default:
		return false
	}
}

func (s State) IsTerminal() bool {
	return s == StateApproved || s == StateRejected || s == StateCancelled
}

func (s State) CanAccept() bool {
	return s == StatePendingStudentAcceptance
}

var (
	ErrInvitationNotFound      = errors.New("invitation not found")
	ErrInvitationStateConflict = errors.New("invitation state conflict")
	ErrDuplicateInvitation     = errors.New("duplicate invitation")
	ErrIneligibleRecipient     = errors.New("ineligible recipient")
	ErrAcceptanceTokenExpired  = errors.New("acceptance token expired")
)

type Invitation struct {
	ID                  string     `json:"id"`
	NormalizedEmail     string     `json:"normalized_email"`
	Email               string     `json:"email"`
	CourseID            string     `json:"course_id"`
	CreatedByAccountID  string     `json:"created_by_account_id"`
	DecidedByAccountID  *string    `json:"decided_by_account_id,omitempty"`
	AcceptedByAccountID *string    `json:"accepted_by_account_id,omitempty"`
	State               State      `json:"state"`
	DecisionReason      *string    `json:"decision_reason,omitempty"`
	AdminNote           *string    `json:"admin_note,omitempty"`
	ExternalReference   *string    `json:"external_reference,omitempty"`
	ActionSecretID      *string    `json:"action_secret_id,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	AcceptedAt          *time.Time `json:"accepted_at,omitempty"`
	DecidedAt           *time.Time `json:"decided_at,omitempty"`
	CancelledAt         *time.Time `json:"cancelled_at,omitempty"`
}

// StudentInvitation is the recipient-facing projection for ST04 / ST10 (FR-036).
// It excludes admin_note, external_reference, decided_by_account_id, decision_reason.
type StudentInvitation struct {
	ID              string     `json:"id"`
	NormalizedEmail string     `json:"normalized_email"`
	Email           string     `json:"email"`
	CourseID        string     `json:"course_id"`
	State           State      `json:"state"`
	CreatedAt       time.Time  `json:"created_at"`
	AcceptedAt      *time.Time `json:"accepted_at,omitempty"`
}

func (i Invitation) ToStudentProjection() StudentInvitation {
	return StudentInvitation{
		ID:              i.ID,
		NormalizedEmail: i.NormalizedEmail,
		Email:           i.Email,
		CourseID:        i.CourseID,
		State:           i.State,
		CreatedAt:       i.CreatedAt,
		AcceptedAt:      i.AcceptedAt,
	}
}
