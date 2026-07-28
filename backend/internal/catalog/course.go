package catalog

import (
	"errors"
	"fmt"
	"time"
)

type CourseLifecycle string

const (
	LifecycleDraft            CourseLifecycle = "DRAFT"
	LifecyclePendingReview    CourseLifecycle = "PENDING_REVIEW"
	LifecycleChangesRequested CourseLifecycle = "CHANGES_REQUESTED"
	LifecyclePublished        CourseLifecycle = "PUBLISHED"
	LifecycleDelisted         CourseLifecycle = "DELISTED"
	LifecycleArchived         CourseLifecycle = "ARCHIVED"
)

func (l CourseLifecycle) Valid() bool {
	switch l {
	case LifecycleDraft, LifecyclePendingReview, LifecycleChangesRequested, LifecyclePublished, LifecycleDelisted, LifecycleArchived:
		return true
	default:
		return false
	}
}

var (
	ErrAccountSuspended = errors.New("instructor account is suspended")
	ErrInvalidLifecycle = errors.New("invalid course lifecycle transition")
)

type Course struct {
	ID                     string          `json:"id"`
	OwnerAccountID         string          `json:"owner_account_id"`
	Lifecycle              CourseLifecycle `json:"lifecycle"`
	LiveRevisionID         *string         `json:"live_revision_id,omitempty"`
	AccessSuspendedAt      *time.Time      `json:"access_suspended_at,omitempty"`
	AccessSuspensionReason *string         `json:"access_suspension_reason,omitempty"`
	RetiredAt              *time.Time      `json:"retired_at,omitempty"`
	CreatedAt              time.Time       `json:"created_at"`
	UpdatedAt              time.Time       `json:"updated_at"`
	EditableRevision       *CourseRevision `json:"editable_revision,omitempty"`
	LiveRevision           *CourseRevision `json:"live_revision,omitempty"`
}

func (c *Course) ValidateInvariants() error {
	if c.ID == "" {
		return errors.New("course ID is required")
	}
	if c.OwnerAccountID == "" {
		return errors.New("course owner account ID is required")
	}
	if !c.Lifecycle.Valid() {
		return fmt.Errorf("invalid course lifecycle: %s", c.Lifecycle)
	}
	if c.Lifecycle == LifecyclePublished && (c.LiveRevisionID == nil || *c.LiveRevisionID == "") {
		return errors.New("published course must have live_revision_id set")
	}
	return nil
}
