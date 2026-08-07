package access

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrCourseNotFound         = errors.New("course not found")
	ErrReasonRequired         = errors.New("reason is required")
	ErrExpiryRequired         = errors.New("default access expiry is required")
	ErrExpiryInPast           = errors.New("default access expiry must be in the future")
	ErrAlreadyHasActiveAccess = errors.New("already has active access")
	ErrCourseNotGrantable     = errors.New("course is not grantable")
	ErrInvalidDateFormat      = errors.New("date must be in YYYY-MM-DD format")
)

var KuwaitLocation = time.FixedZone("Asia/Kuwait", 3*3600)

type Entitlement struct {
	ID                      string     `json:"id"`
	StudentAccountID        string     `json:"student_account_id"`
	ScopeKind               string     `json:"scope_kind"`
	ScopeID                 string     `json:"scope_id"`
	CourseID                string     `json:"course_id"`
	GrantSource             string     `json:"grant_source"`
	SourceInvitationID      *string    `json:"source_invitation_id,omitempty"`
	OriginalAccessEndsAt    time.Time  `json:"original_access_ends_at"`
	AccessEndsAt            time.Time  `json:"access_ends_at"`
	RevokedAt               *time.Time `json:"revoked_at,omitempty"`
	RetirementEligibilityAt time.Time  `json:"retirement_eligibility_at"`
	State                   string     `json:"state"`
	Revision                int64      `json:"revision"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

type ApproveInvitationParams struct {
	InvitationID   string
	AdminAccountID string
	Now            time.Time
}

type ApproveInvitationResult struct {
	Invitation  Invitation  `json:"invitation"`
	Entitlement Entitlement `json:"entitlement"`
}

type RejectInvitationParams struct {
	InvitationID   string
	AdminAccountID string
	Reason         string
	Now            time.Time
}

type CancelInvitationParams struct {
	InvitationID   string
	AdminAccountID string
	Now            time.Time
}

type ResendInvitationParams struct {
	InvitationID   string
	AdminAccountID string
	Now            time.Time
	TTL            time.Duration
}

type StudentCourseAccessHistoryItem struct {
	CourseID        string             `json:"course_id"`
	Invitation      *StudentInvitation `json:"invitation,omitempty"`
	AccessEndsAt    *time.Time         `json:"access_ends_at,omitempty"`
	HasActiveAccess bool               `json:"has_active_access"`
}

type StudentCourseAccessHistoryResponse struct {
	Items []StudentCourseAccessHistoryItem `json:"items"`
}

type EntitlementAdjustment struct {
	ID               string    `json:"id"`
	EntitlementID    string    `json:"entitlement_id"`
	OldAccessEndsAt  time.Time `json:"old_access_ends_at"`
	NewAccessEndsAt  time.Time `json:"new_access_ends_at"`
	Reason           string    `json:"reason"`
	ActorAccountID   string    `json:"actor_account_id"`
	SupportReference *string   `json:"support_reference,omitempty"`
	AdjustedAt       time.Time `json:"adjusted_at"`
}

type AdminEntitlementDetail struct {
	Entitlement
	Adjustments []EntitlementAdjustment `json:"adjustments"`
}

// ConvertKuwaitDateToUTCExpiry converts a Kuwait-local calendar date string (YYYY-MM-DD)
// to an exclusive UTC expiry instant representing the first instant of the following local day in UTC,
// and validates that the resulting instant is strictly after now.
func ConvertKuwaitDateToUTCExpiry(dateStr string, now time.Time) (time.Time, error) {
	if dateStr == "" {
		return time.Time{}, ErrExpiryRequired
	}
	t, err := time.ParseInLocation("2006-01-02", dateStr, KuwaitLocation)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %v", ErrInvalidDateFormat, err)
	}
	followingDay := time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, KuwaitLocation).UTC()
	if !followingDay.After(now.UTC()) {
		return time.Time{}, ErrExpiryInPast
	}
	return followingDay, nil
}
