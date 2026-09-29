package admin

import (
	"context"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/identity"
)

const (
	ActionAccessDiagnosed       = "ADMIN_ACCESS_DIAGNOSED"
	ActionNoteAdded             = "ADMIN_NOTE_ADDED"
	ActionSessionsRevoked       = "ADMIN_SESSIONS_REVOKED"
	User360TargetType           = "ACCOUNT"
	DiagnosticAllowed           = "ACTIVE"
	DiagnosticNoEntitlement     = "NO_ENTITLEMENT"
	DiagnosticRevoked           = "REVOKED"
	DiagnosticAccountSuspended  = "ACCOUNT_SUSPENDED"
	DiagnosticAccountUnverified = "ACCOUNT_UNVERIFIED"
	DiagnosticCourseSuspended   = "COURSE_ACCESS_SUSPENDED"
	DiagnosticCourseRetired     = "COURSE_RETIRED"
	DiagnosticScopeMismatch     = "SCOPE_MISMATCH"
	maxUser360Page              = 10_000
	maxUser360QueryLength       = 200
)

type User360Request struct {
	Principal     identity.Principal
	CorrelationID string
	Locale        identity.Locale
	AccountID     string
}

type User360Identity struct {
	ID                     string                 `json:"id"`
	DisplayName            string                 `json:"display_name"`
	Email                  string                 `json:"email"`
	Role                   identity.Role          `json:"role"`
	Status                 identity.AccountStatus `json:"status"`
	Locale                 identity.Locale        `json:"locale"`
	EmailVerified          bool                   `json:"email_verified"`
	CreatedAt              time.Time              `json:"created_at"`
	LastSignInActivityAt   *time.Time             `json:"last_sign_in_activity_at,omitempty"`
	LastLearningActivityAt *time.Time             `json:"last_learning_activity_at,omitempty"`
}

type AcademicProfile struct {
	SetupState        string `json:"setup_state"`
	EnrollmentStatus  string `json:"enrollment_status,omitempty"`
	InstitutionLabel  string `json:"institution_label,omitempty"`
	AcademicUnitLabel string `json:"academic_unit_label,omitempty"`
	ProgramLabel      string `json:"program_label,omitempty"`
	CurriculumLabel   string `json:"curriculum_label,omitempty"`
	CurrentLevel      *int16 `json:"current_level,omitempty"`
}

type UserCourse struct {
	ID                     string     `json:"id"`
	Title                  string     `json:"title"`
	Lifecycle              string     `json:"lifecycle"`
	CandidateRevisionState string     `json:"candidate_revision_state,omitempty"`
	EnrollmentsCount       int        `json:"enrollments_count,omitempty"`
	CompletedLessons       int        `json:"completed_lessons,omitempty"`
	TotalLessons           int        `json:"total_lessons,omitempty"`
	ProgressPercent        float64    `json:"progress_percent,omitempty"`
	LastWatchedAt          *time.Time `json:"last_watched_at,omitempty"`
}

type UserEntitlement struct {
	ID                   string                      `json:"id"`
	CourseID             string                      `json:"course_id"`
	CourseTitle          string                      `json:"course_title"`
	ScopeKind            string                      `json:"scope_kind"`
	ScopeID              string                      `json:"scope_id"`
	GrantSource          string                      `json:"grant_source"`
	SourceReferenceLabel string                      `json:"source_reference_label,omitempty"`
	GrantedByDisplayName string                      `json:"granted_by_display_name,omitempty"`
	GrantedAt            time.Time                   `json:"granted_at"`
	OriginalAccessEndsAt time.Time                   `json:"original_access_ends_at"`
	AccessEndsAt         time.Time                   `json:"access_ends_at"`
	RevokedAt            *time.Time                  `json:"revoked_at,omitempty"`
	State                string                      `json:"state"`
	Revision             int64                       `json:"revision"`
	Adjustments          []UserEntitlementAdjustment `json:"adjustments"`
}

type UserEntitlementAdjustment struct {
	ID               string    `json:"id"`
	EntitlementID    string    `json:"entitlement_id"`
	OldAccessEndsAt  time.Time `json:"old_access_ends_at"`
	NewAccessEndsAt  time.Time `json:"new_access_ends_at"`
	Reason           string    `json:"reason"`
	ActorAccountID   string    `json:"actor_account_id"`
	SupportReference *string   `json:"support_reference,omitempty"`
	AdjustedAt       time.Time `json:"adjusted_at"`
}

type UserInvitation struct {
	ID                string     `json:"id"`
	CourseID          string     `json:"course_id"`
	CourseTitle       string     `json:"course_title"`
	State             string     `json:"state"`
	CreatedByName     string     `json:"created_by_name,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	DecidedAt         *time.Time `json:"decided_at,omitempty"`
	AcceptedAt        *time.Time `json:"accepted_at,omitempty"`
	ExternalReference string     `json:"external_reference,omitempty"`
}

type UserPurchaseRequest struct {
	ID              string     `json:"id"`
	Reference       string     `json:"reference"`
	CourseTitle     string     `json:"course_title,omitempty"`
	BundleTitle     string     `json:"bundle_title,omitempty"`
	State           string     `json:"state"`
	RequestedAt     time.Time  `json:"requested_at"`
	AccessGrantedAt *time.Time `json:"access_granted_at,omitempty"`
}

type SecurityEvent struct {
	ID         string         `json:"id"`
	OccurredAt time.Time      `json:"occurred_at"`
	EventType  string         `json:"event_type"`
	RequestID  string         `json:"request_id"`
	Evidence   map[string]any `json:"evidence"`
}

type AdminNote struct {
	ID         string    `json:"id"`
	AuthorName string    `json:"author_name"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
}

type StudentUser360 struct {
	AcademicProfile  *AcademicProfile             `json:"academic_profile,omitempty"`
	Courses          []UserCourse                 `json:"courses"`
	Entitlements     []UserEntitlement            `json:"entitlements"`
	Invitations      []UserInvitation             `json:"invitations"`
	PurchaseRequests []UserPurchaseRequest        `json:"purchase_requests"`
	Devices          identity.AdminDeviceOverview `json:"devices"`
	SecurityEvents   []SecurityEvent              `json:"security_events"`
	AuditEvents      []AuditEvent                 `json:"audit_events"`
	Notes            []AdminNote                  `json:"notes"`
}

type InstructorCourse struct {
	ID                     string `json:"id"`
	Title                  string `json:"title"`
	Lifecycle              string `json:"lifecycle"`
	CandidateRevisionState string `json:"candidate_revision_state,omitempty"`
	EnrollmentsCount       int    `json:"enrollments_count"`
}

type InstructorUser360 struct {
	StaffStatus  string             `json:"staff_status"`
	OwnedCourses []InstructorCourse `json:"owned_courses"`
	AuditEvents  []AuditEvent       `json:"audit_events"`
	Notes        []AdminNote        `json:"notes"`
}

type User360 struct {
	Identity   User360Identity    `json:"identity"`
	Student    *StudentUser360    `json:"student,omitempty"`
	Instructor *InstructorUser360 `json:"instructor,omitempty"`
}

type NoteListRequest struct {
	Principal identity.Principal
	AccountID string
	Limit     int
}

type AddNoteRequest struct {
	Principal     identity.Principal
	CorrelationID string
	AccountID     string
	Body          string
}

type SessionRevocationRequest struct {
	Principal        identity.Principal
	ActorSession     identity.Session
	RecentAuthWindow time.Duration
	CorrelationID    string
	AccountID        string
	Reason           string
	Now              time.Time
}

type SessionRevocationResult struct {
	Epoch               int `json:"epoch"`
	RevokedSessionCount int `json:"revoked_session_count"`
}

type CourseOption struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Lifecycle string `json:"lifecycle"`
}

type CourseOptionsRequest struct {
	Principal identity.Principal
	Locale    identity.Locale
	Query     string
	Limit     int
}

type AccessDiagnosticRequest struct {
	Principal     identity.Principal
	CorrelationID string
	Locale        identity.Locale
	AccountID     string
	CourseID      string
	Now           time.Time
}

type DiagnosticFact struct {
	Code  string `json:"code"`
	Value string `json:"value,omitempty"`
}

type AccessDiagnostic struct {
	AccountID         string            `json:"account_id"`
	CourseID          string            `json:"course_id"`
	CourseTitle       string            `json:"course_title"`
	PrimaryReasonCode string            `json:"primary_reason_code"`
	ReasonCode        string            `json:"reason_code"`
	EvaluatorReason   string            `json:"evaluator_reason"`
	Allowed           bool              `json:"allowed"`
	Facts             []DiagnosticFact  `json:"facts"`
	Entitlements      []UserEntitlement `json:"entitlements"`
}

type SecurityEventsRequest struct {
	Principal identity.Principal
	AccountID string
	Page      int
	Limit     int
}

type SecurityEventsResult struct {
	Events []SecurityEvent `json:"events"`
	Total  int             `json:"total"`
	Page   int             `json:"page"`
	Limit  int             `json:"limit"`
}

type NoteListResult struct {
	Notes []AdminNote `json:"notes"`
}

type deviceReader interface {
	AdminOverview(context.Context, string, time.Time) (identity.AdminDeviceOverview, error)
}
