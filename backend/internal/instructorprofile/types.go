// Package instructorprofile owns the instructor's moderated public identity.
//
// Draft fields are private until an Admin approves a submission. The public
// read model is a JSON snapshot so later draft edits cannot change a published
// page accidentally.
package instructorprofile

import (
	"errors"
	"time"
)

type PublicationState string

const (
	StateDraft            PublicationState = "DRAFT"
	StatePendingReview    PublicationState = "PENDING_REVIEW"
	StatePublished        PublicationState = "PUBLISHED"
	StateChangesRequested PublicationState = "CHANGES_REQUESTED"
	StateHidden           PublicationState = "HIDDEN"
)

func (s PublicationState) Valid() bool {
	switch s {
	case StateDraft, StatePendingReview, StatePublished, StateChangesRequested, StateHidden:
		return true
	default:
		return false
	}
}

type ExpertiseItem struct {
	ID           string  `json:"id"`
	OfficialCode *string `json:"official_code,omitempty"`
	TitleAr      string  `json:"title_ar"`
	TitleEn      string  `json:"title_en"`
}

type PublicSnapshot struct {
	PublicSlug  string          `json:"public_slug"`
	DisplayName string          `json:"display_name"`
	HeadlineAr  string          `json:"headline_ar"`
	HeadlineEn  string          `json:"headline_en"`
	BioAr       string          `json:"bio_ar"`
	BioEn       string          `json:"bio_en"`
	Expertise   []ExpertiseItem `json:"expertise"`
	AvatarURL   *string         `json:"avatar_url,omitempty"`
}

type Profile struct {
	AccountID         string           `json:"account_id"`
	DisplayName       string           `json:"display_name"`
	PublicSlug        *string          `json:"public_slug,omitempty"`
	HeadlineAr        string           `json:"headline_ar"`
	HeadlineEn        string           `json:"headline_en"`
	BioAr             string           `json:"bio_ar"`
	BioEn             string           `json:"bio_en"`
	AvatarURL         *string          `json:"avatar_url,omitempty"`
	PublicationState  PublicationState `json:"publication_state"`
	PublishedSnapshot *PublicSnapshot  `json:"published_snapshot,omitempty"`
	Expertise         []ExpertiseItem  `json:"expertise"`
	SubmittedAt       *time.Time       `json:"submitted_at,omitempty"`
	DecidedAt         *time.Time       `json:"decided_at,omitempty"`
	DecidedBy         *string          `json:"-"`
	DecisionNote      *string          `json:"decision_note,omitempty"`
	Revision          int              `json:"revision"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
}

type AdminListItem struct {
	AccountID        string           `json:"account_id"`
	DisplayName      string           `json:"display_name"`
	PublicSlug       *string          `json:"public_slug,omitempty"`
	PublicationState PublicationState `json:"publication_state"`
	SubmittedAt      *time.Time       `json:"submitted_at,omitempty"`
	UpdatedAt        time.Time        `json:"updated_at"`
	Revision         int              `json:"revision"`
}

type SaveDraftRequest struct {
	AccountID        string
	ExpectedRevision int
	PublicSlug       string
	HeadlineAr       string
	HeadlineEn       string
	BioAr            string
	BioEn            string
	ExpertiseIDs     []string
}

type SubmitRequest struct {
	AccountID        string
	ExpectedRevision int
}

type DecisionRequest struct {
	AccountID        string
	AdminAccountID   string
	ExpectedRevision int
	Reason           string
}

type ListRequest struct {
	State PublicationState
	Page  int
	Limit int
}

type ListResult struct {
	Items   []AdminListItem `json:"items"`
	Page    int             `json:"page"`
	Limit   int             `json:"limit"`
	HasMore bool            `json:"has_more"`
}

var (
	ErrRepositoryNil     = errors.New("instructor profile database pool is required")
	ErrNotInstructor     = errors.New("account is not an instructor")
	ErrProfileNotFound   = errors.New("instructor profile not found")
	ErrInvalidInput      = errors.New("instructor profile input is invalid")
	ErrSlugTaken         = errors.New("instructor profile slug is already used")
	ErrRevisionConflict  = errors.New("instructor profile revision is stale")
	ErrInvalidTransition = errors.New("instructor profile state transition is invalid")
	ErrReasonRequired    = errors.New("moderation reason is required")
	ErrSubjectNotFound   = errors.New("instructor profile expertise subject not found")
)

type SubmissionIncompleteError struct {
	Violations []string
}

func (e *SubmissionIncompleteError) Error() string {
	return "instructor profile submission is incomplete"
}
