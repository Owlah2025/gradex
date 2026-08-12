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
	ErrEntitlementNotFound    = errors.New("entitlement not found")
	ErrEntitlementRevoked     = errors.New("entitlement is already revoked")
	ErrEntitlementStale       = errors.New("entitlement was changed by another operation")
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

// AdminEntitlementDetail is the AD07 read model. `Entitlement` is a named
// field rather than an embedded one: embedding flattened the entitlement into
// the response object, which is not the shape the Admin surface reads.
type AdminEntitlementDetail struct {
	Entitlement Entitlement             `json:"entitlement"`
	Adjustments []EntitlementAdjustment `json:"adjustments"`
}

// AdjustEntitlementExpiryParams carries one audited elevated-Admin expiry
// adjustment under BR-026. `original_access_ends_at` is never an input: it is
// the snapshot taken at Admin Approval and is not editable by any actor.
type AdjustEntitlementExpiryParams struct {
	EntitlementID    string
	AdminAccountID   string
	ActorDescriptor  string
	NewAccessEndsAt  time.Time
	Reason           string
	SupportReference *string
	// ExpectedRevision, when non-zero, requires the stored entitlement revision
	// to match. It is how a stale Admin view is refused rather than applied.
	ExpectedRevision int64
	Now              time.Time
}

// RevokeEntitlementParams carries one audited elevated-Admin revocation.
// Revocation ends access; it deletes no Enrollment, Progress, Invitation, or
// adjustment history (BR-026).
type RevokeEntitlementParams struct {
	EntitlementID    string
	AdminAccountID   string
	ActorDescriptor  string
	Reason           string
	SupportReference *string
	ExpectedRevision int64
	Now              time.Time
}

// ConvertKuwaitDateToUTCExpiry converts a Kuwait-local calendar date string (YYYY-MM-DD)
// to an exclusive UTC expiry instant representing the first instant of the following local day in UTC,
// and validates that the resulting instant is strictly after now.
func ConvertKuwaitDateToUTCExpiry(dateStr string, now time.Time) (time.Time, error) {
	boundary, err := ConvertKuwaitDateToUTCBoundary(dateStr)
	if err != nil {
		return time.Time{}, err
	}
	if !boundary.After(now.UTC()) {
		return time.Time{}, ErrExpiryInPast
	}
	return boundary, nil
}

// ConvertKuwaitDateToUTCBoundary is the same Kuwait-local boundary conversion
// without the future-instant requirement.
//
// A Course default expiry must be in the future before an Invitation can be
// approved (BR-025), but an entitlement adjustment may deliberately move the
// effective instant into the past: "moving expiry into the past ends access
// immediately" (BR-026). The two callers therefore need the same calendar
// arithmetic under different preconditions, not two different conversions.
func ConvertKuwaitDateToUTCBoundary(dateStr string) (time.Time, error) {
	if dateStr == "" {
		return time.Time{}, ErrExpiryRequired
	}
	t, err := time.ParseInLocation("2006-01-02", dateStr, KuwaitLocation)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %v", ErrInvalidDateFormat, err)
	}
	return time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, KuwaitLocation).UTC(), nil
}
