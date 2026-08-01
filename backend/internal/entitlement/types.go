package entitlement

import "time"

type ScopeKind string

const (
	ScopeCourse  ScopeKind = "COURSE"
	ScopeSection ScopeKind = "SECTION"
)

func (s ScopeKind) Valid() bool { return s == ScopeCourse || s == ScopeSection }

type GrantSource string

const GrantSourceManualInvitation GrantSource = "MANUAL_INVITATION"

func (s GrantSource) Valid() bool { return s == GrantSourceManualInvitation }

type State string

const (
	StateActive  State = "ACTIVE"
	StateRevoked State = "REVOKED"
)

func (s State) Valid() bool { return s == StateActive || s == StateRevoked }

// Record is the immutable/provenance-bearing grant record S4 evaluates. S6
// creates it through its audited Admin-Approval transaction.
type Record struct {
	ID                      string
	StudentAccountID        string
	ScopeKind               ScopeKind
	ScopeID                 string
	CourseID                string
	GrantSource             GrantSource
	SourceInvitationID      *string
	OriginalAccessEndsAt    time.Time
	AccessEndsAt            time.Time
	RevokedAt               *time.Time
	RetirementEligibilityAt time.Time
	State                   State
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

func (r Record) Revoked() bool { return r.State == StateRevoked || r.RevokedAt != nil }

// Lesson is the authoritative, server-read Course graph node used for scope.
// It intentionally contains no client-provided Course or Section membership.
type Lesson struct {
	ID               string
	CourseID         string
	SectionID        string
	AccountStatus    string
	CourseSuspended  bool
	RetiredAt        *time.Time
	SectionRetiredAt *time.Time
	LessonRetiredAt  *time.Time
}

// Snapshot contains every required runtime input. A missing field is denied;
// an evaluator never reconstructs course membership from a request body.
type Snapshot struct {
	Lesson       Lesson
	Entitlements []Record
}

type Reason string

const (
	ReasonAllowed           Reason = "ALLOWED"
	ReasonNoApplicableGrant Reason = "NO_APPLICABLE_GRANT"
	ReasonExpired           Reason = "EXPIRED"
	ReasonAccountSuspended  Reason = "ACCOUNT_SUSPENDED"
	ReasonCourseSuspended   Reason = "COURSE_ACCESS_SUSPENDED"
	ReasonRetired           Reason = "RETIRED_WITHOUT_ELIGIBILITY"
	ReasonDependency        Reason = "DEPENDENCY_FAILURE"
)

// Decision is internal-only evidence. Delivery translates every denied reason
// to its single external refusal and records the typed reason in audit/logging.
type Decision struct {
	Allowed       bool
	Reason        Reason
	EntitlementID string
}
