package catalog

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	defaultRosterPageSize = 20
	maxRosterPageSize     = 100
)

const courseRosterQuery = `
	WITH ranked_entitlements AS (
		SELECT e.*,
		       row_number() OVER (
			   PARTITION BY e.student_account_id, e.course_id
			   ORDER BY e.updated_at DESC, e.created_at DESC, e.id DESC
		       ) AS entitlement_rank
		FROM entitlements e
		WHERE e.course_id = $1::uuid AND e.scope_kind = 'COURSE'
	),
	current_course_lessons AS (
		SELECT cli.id AS lesson_identity_id
		FROM courses c
		JOIN course_revisions live
		  ON live.id = c.live_revision_id
		 AND live.course_id = c.id
		 AND live.state = 'APPROVED'
		JOIN course_sections cs ON cs.revision_id = live.id AND cs.course_id = c.id
		JOIN course_lessons cl ON cl.section_id = cs.id AND cl.course_id = c.id
		JOIN course_lesson_identities cli
		  ON cli.id = cl.lesson_identity_id
		 AND cli.course_id = c.id
		 AND cli.section_identity_id = cl.section_identity_id
		WHERE c.id = $1::uuid AND c.owner_account_id = $2::uuid
	),
	current_progress AS (
		SELECT enrollment.id AS enrollment_id,
		       count(DISTINCT lesson.lesson_identity_id) AS total_lessons,
		       count(DISTINCT progress.course_lesson_identity_id)
		           FILTER (WHERE progress.completed_at IS NOT NULL) AS completed_lessons
		FROM enrollments enrollment
		LEFT JOIN current_course_lessons lesson ON TRUE
		LEFT JOIN progress
		  ON progress.enrollment_id = enrollment.id
		 AND progress.course_lesson_identity_id = lesson.lesson_identity_id
		WHERE enrollment.course_id = $1::uuid
		GROUP BY enrollment.id
	),
	learning_activity AS (
		SELECT enrollment_id, max(last_watched_at) AS last_learning_activity
		FROM progress
		GROUP BY enrollment_id
	)
	SELECT a.display_name,
	       enrollment.created_at,
	       entitlement.created_at,
	       entitlement.access_ends_at,
	       CASE
	           WHEN entitlement.id IS NULL THEN 'DENIED'
	           WHEN entitlement.state = 'REVOKED' THEN 'REVOKED'
	           WHEN entitlement.access_ends_at <= $3::timestamptz THEN 'EXPIRED'
	           WHEN c.retired_at IS NOT NULL
	                AND entitlement.retirement_eligibility_at >= c.retired_at THEN 'DENIED'
	           WHEN a.status::text <> 'ACTIVE' OR c.access_suspended_at IS NOT NULL THEN 'SUSPENDED'
	           ELSE 'ACTIVE'
	       END AS access_status,
	       CASE
	           WHEN COALESCE(current_progress.total_lessons, 0) = 0 THEN 0
	           ELSE ROUND(current_progress.completed_lessons::numeric * 100 / current_progress.total_lessons)::integer
	       END AS progress_percent,
	       COALESCE(completion.id IS NOT NULL, false) AS completed,
	       completion.completed_at,
	       learning_activity.last_learning_activity
	FROM enrollments enrollment
	JOIN courses c
	  ON c.id = enrollment.course_id
	 AND c.id = $1::uuid
	 AND c.owner_account_id = $2::uuid
	JOIN accounts a ON a.id = enrollment.student_account_id
	LEFT JOIN ranked_entitlements entitlement
	  ON entitlement.student_account_id = enrollment.student_account_id
	 AND entitlement.course_id = enrollment.course_id
	 AND entitlement.entitlement_rank = 1
	LEFT JOIN current_progress
	  ON current_progress.enrollment_id = enrollment.id
	LEFT JOIN course_completions completion
	  ON completion.enrollment_id = enrollment.id
	LEFT JOIN learning_activity
	  ON learning_activity.enrollment_id = enrollment.id
	ORDER BY enrollment.created_at ASC, enrollment.student_account_id ASC
	LIMIT $4 OFFSET $5
`

type CourseRosterAccessStatus string

const (
	RosterAccessActive    CourseRosterAccessStatus = "ACTIVE"
	RosterAccessExpired   CourseRosterAccessStatus = "EXPIRED"
	RosterAccessRevoked   CourseRosterAccessStatus = "REVOKED"
	RosterAccessSuspended CourseRosterAccessStatus = "SUSPENDED"
	RosterAccessDenied    CourseRosterAccessStatus = "DENIED"
)

type CourseRosterEntry struct {
	DisplayName          string                   `json:"display_name"`
	AccessStatus         CourseRosterAccessStatus `json:"access_status"`
	EnrolledAt           time.Time                `json:"enrolled_at"`
	AccessStartedAt      *time.Time               `json:"access_started_at,omitempty"`
	AccessEndsAt         *time.Time               `json:"access_ends_at,omitempty"`
	ProgressPercent      int                      `json:"progress_percent"`
	Completed            bool                     `json:"completed"`
	CompletedAt          *time.Time               `json:"completed_at,omitempty"`
	LastLearningActivity *time.Time               `json:"last_learning_activity,omitempty"`
}

type CourseRosterPage struct {
	Items    []CourseRosterEntry `json:"items"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
	HasNext  bool                `json:"has_next"`
}

type CourseRosterRequest struct {
	CourseID       string
	OwnerAccountID string
	Page           int
	PageSize       int
	Now            time.Time
}

func (r *Repository) ListOwnedCourseRoster(ctx context.Context, request CourseRosterRequest) (CourseRosterPage, error) {
	if r == nil || r.pool == nil {
		return CourseRosterPage{}, ErrRepositoryNil
	}
	if request.CourseID == "" || request.OwnerAccountID == "" {
		return CourseRosterPage{}, ErrCourseNotFound
	}
	if request.Now.IsZero() {
		return CourseRosterPage{}, fmt.Errorf("roster clock is required")
	}

	request = normalizeRosterRequest(request)
	entries, err := r.queryCourseRosterEntries(ctx, request)
	if err != nil {
		return CourseRosterPage{}, err
	}

	hasNext := len(entries) > request.PageSize
	if hasNext {
		entries = entries[:request.PageSize]
	}
	return CourseRosterPage{Items: entries, Page: request.Page, PageSize: request.PageSize, HasNext: hasNext}, nil
}

func (r *Repository) queryCourseRosterEntries(ctx context.Context, request CourseRosterRequest) ([]CourseRosterEntry, error) {
	offset := (request.Page - 1) * request.PageSize
	rows, err := r.pool.Query(ctx, courseRosterQuery, request.CourseID, request.OwnerAccountID, request.Now.UTC(), request.PageSize+1, offset)
	if err != nil {
		return nil, fmt.Errorf("querying course roster: %w", err)
	}
	defer rows.Close()
	return scanCourseRosterEntries(rows, request.PageSize)
}

func scanCourseRosterEntries(rows pgx.Rows, pageSize int) ([]CourseRosterEntry, error) {
	entries := make([]CourseRosterEntry, 0, pageSize)
	for rows.Next() {
		entry, err := scanCourseRosterEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading course roster: %w", err)
	}
	return entries, nil
}

func scanCourseRosterEntry(row pgx.Rows) (CourseRosterEntry, error) {
	var entry CourseRosterEntry
	var accessStartedAt, accessEndsAt, completedAt, lastLearningActivity *time.Time
	if err := row.Scan(
		&entry.DisplayName,
		&entry.EnrolledAt,
		&accessStartedAt,
		&accessEndsAt,
		&entry.AccessStatus,
		&entry.ProgressPercent,
		&entry.Completed,
		&completedAt,
		&lastLearningActivity,
	); err != nil {
		return CourseRosterEntry{}, fmt.Errorf("scanning course roster: %w", err)
	}
	entry.EnrolledAt = entry.EnrolledAt.UTC()
	entry.AccessStartedAt = utcTime(accessStartedAt)
	entry.AccessEndsAt = utcTime(accessEndsAt)
	entry.CompletedAt = utcTime(completedAt)
	entry.LastLearningActivity = utcTime(lastLearningActivity)
	return entry, nil
}

func normalizeRosterRequest(request CourseRosterRequest) CourseRosterRequest {
	if request.Page < 1 {
		request.Page = 1
	}
	if request.PageSize < 1 || request.PageSize > maxRosterPageSize {
		request.PageSize = defaultRosterPageSize
	}
	return request
}

func utcTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}
