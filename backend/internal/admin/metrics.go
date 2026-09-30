package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const metricsOverviewQuery = `
WITH
params AS (
    SELECT now() AS as_of, $1::text AS locale
),
student_counts AS (
    SELECT
        count(*) FILTER (WHERE a.role = 'STUDENT') AS total,
        count(*) FILTER (WHERE a.role = 'STUDENT' AND a.status = 'ACTIVE') AS active,
        count(*) FILTER (WHERE a.role = 'STUDENT' AND a.status = 'PENDING_VERIFICATION') AS pending_verification,
        count(*) FILTER (WHERE a.role = 'STUDENT' AND a.status = 'SUSPENDED') AS suspended
    FROM accounts a
),
registration_counts AS (
    SELECT
        count(*) FILTER (WHERE a.role = 'STUDENT' AND a.created_at >= p.as_of - interval '7 days') AS new_7d,
        count(*) FILTER (WHERE a.role = 'STUDENT' AND a.created_at >= p.as_of - interval '30 days') AS new_30d
    FROM accounts a
    CROSS JOIN params p
),
	sign_in_activity AS (
	    SELECT
	        count(DISTINCT a.id) FILTER (WHERE ise.occurred_at >= date_trunc('day', p.as_of, 'Asia/Kuwait')) AS today,
	        count(DISTINCT a.id) FILTER (WHERE ise.occurred_at >= p.as_of - interval '7 days') AS days_7,
	        count(DISTINCT a.id) FILTER (WHERE ise.occurred_at >= p.as_of - interval '30 days') AS days_30
	    FROM params p
	    LEFT JOIN identity_security_events ise
	      ON ise.event_type IN ('SESSION_CREATED', 'SESSION_RENEWED')
	     AND ise.occurred_at >= p.as_of - interval '30 days'
    LEFT JOIN accounts a
      ON a.id = ise.account_id AND a.role = 'STUDENT'
),
learning_activity AS (
    SELECT
        count(DISTINCT a.id) FILTER (WHERE progress.last_watched_at >= p.as_of - interval '7 days') AS days_7,
        count(DISTINCT a.id) FILTER (WHERE progress.last_watched_at >= p.as_of - interval '30 days') AS days_30,
        count(DISTINCT a.id) AS students_started
    FROM progress
    JOIN enrollments e ON e.id = progress.enrollment_id
    JOIN accounts a ON a.id = e.student_account_id AND a.role = 'STUDENT'
    CROSS JOIN params p
),
current_course_lessons AS (
    SELECT c.id AS course_id, cli.id AS lesson_identity_id
    FROM courses c
    JOIN course_revisions cr
      ON cr.id = c.live_revision_id
     AND cr.course_id = c.id
     AND cr.state = 'APPROVED'
    JOIN course_sections cs ON cs.revision_id = cr.id AND cs.course_id = c.id
    JOIN course_lessons cl ON cl.section_id = cs.id AND cl.course_id = c.id
    JOIN course_lesson_identities cli
      ON cli.id = cl.lesson_identity_id
     AND cli.course_id = c.id
     AND cli.section_identity_id = cl.section_identity_id
),
student_enrollment_progress AS (
    SELECT
        e.id AS enrollment_id,
        count(DISTINCT lesson.lesson_identity_id) AS total_lessons,
        count(DISTINCT progress.course_lesson_identity_id)
            FILTER (WHERE progress.completed_at IS NOT NULL) AS completed_lessons
    FROM enrollments e
    JOIN courses c ON c.id = e.course_id
    JOIN accounts student ON student.id = e.student_account_id AND student.role = 'STUDENT'
    LEFT JOIN current_course_lessons lesson ON lesson.course_id = e.course_id
    LEFT JOIN progress
      ON progress.enrollment_id = e.id
     AND progress.course_lesson_identity_id = lesson.lesson_identity_id
    GROUP BY e.id
),
progress_summary AS (
    SELECT
        COALESCE(
            avg(completed_lessons::numeric / NULLIF(total_lessons, 0)) * 100,
            0
        ) AS average_progress
    FROM student_enrollment_progress
),
durable_completion_summary AS (
    SELECT count(*) AS completions
    FROM course_completions completion
    JOIN enrollments e ON e.id = completion.enrollment_id
    JOIN accounts student ON student.id = e.student_account_id AND student.role = 'STUDENT'
),
course_lifecycle_counts AS (
    SELECT lifecycle::text AS lifecycle, count(*) AS total
    FROM courses
    GROUP BY lifecycle
),
instructor_counts AS (
    SELECT
        count(*) FILTER (WHERE a.role = 'INSTRUCTOR' AND a.status = 'ACTIVE') AS active,
        count(DISTINCT a.id) FILTER (
            WHERE a.role = 'INSTRUCTOR'
              AND a.status = 'ACTIVE'
              AND EXISTS (
                  SELECT 1 FROM courses c
                  WHERE c.owner_account_id = a.id AND c.lifecycle = 'PUBLISHED'
              )
        ) AS with_published_course
    FROM accounts a
),
top_subject_demand AS (
    SELECT COALESCE(
        jsonb_agg(
            jsonb_build_object(
                'subject_id', subject_id::text,
                'title', CASE WHEN (SELECT locale FROM params) = 'ar' THEN title_ar ELSE title_en END,
                'students', students,
                'served', served
            ) ORDER BY students DESC, subject_id
        ),
        '[]'::jsonb
    ) AS items
    FROM (
        SELECT
            s.id AS subject_id,
            s.title_ar,
            s.title_en,
            count(DISTINCT demand.account_id) AS students,
            EXISTS (
                SELECT 1 FROM courses c
                WHERE c.subject_id = s.id AND c.lifecycle = 'PUBLISHED'
            ) AS served
        FROM subject_demand_signals demand
        JOIN subjects s ON s.id = demand.subject_id
        WHERE demand.withdrawn_at IS NULL
        GROUP BY s.id, s.title_ar, s.title_en
        ORDER BY students DESC, s.id
        LIMIT 5
    ) demand
),
metric_rows AS (
    SELECT 10 AS ordinal, 'students.total' AS key, to_jsonb(student_counts.total) AS value, 'students.total' AS definition_key FROM student_counts
    UNION ALL SELECT 11, 'students.active', to_jsonb(student_counts.active), 'students.active' FROM student_counts
    UNION ALL SELECT 12, 'students.pending_verification', to_jsonb(student_counts.pending_verification), 'students.pending_verification' FROM student_counts
    UNION ALL SELECT 13, 'students.suspended', to_jsonb(student_counts.suspended), 'students.suspended' FROM student_counts
    UNION ALL SELECT 20, 'registrations.new_7d', to_jsonb(registration_counts.new_7d), 'registrations.new' FROM registration_counts
    UNION ALL SELECT 21, 'registrations.new_30d', to_jsonb(registration_counts.new_30d), 'registrations.new' FROM registration_counts
    UNION ALL SELECT 30, 'signed_in_activity.today', to_jsonb(sign_in_activity.today), 'signed_in_activity' FROM sign_in_activity
    UNION ALL SELECT 31, 'signed_in_activity.7d', to_jsonb(sign_in_activity.days_7), 'signed_in_activity' FROM sign_in_activity
    UNION ALL SELECT 32, 'signed_in_activity.30d', to_jsonb(sign_in_activity.days_30), 'signed_in_activity' FROM sign_in_activity
    UNION ALL SELECT 40, 'learning_activity.7d', to_jsonb(learning_activity.days_7), 'learning_activity' FROM learning_activity
    UNION ALL SELECT 41, 'learning_activity.30d', to_jsonb(learning_activity.days_30), 'learning_activity' FROM learning_activity
    UNION ALL SELECT 42, 'students.started_lessons', to_jsonb(learning_activity.students_started), 'students.started_lessons' FROM learning_activity
    UNION ALL SELECT 43, 'learning.average_course_progress', to_jsonb(progress_summary.average_progress), 'learning.average_course_progress' FROM progress_summary
    UNION ALL SELECT 44, 'learning.completions', to_jsonb(durable_completion_summary.completions), 'learning.completions' FROM durable_completion_summary
    UNION ALL SELECT 50, 'courses.draft', to_jsonb(COALESCE((SELECT total FROM course_lifecycle_counts WHERE lifecycle = 'DRAFT'), 0)), 'courses.lifecycle'
    UNION ALL SELECT 51, 'courses.pending_review', to_jsonb(COALESCE((SELECT total FROM course_lifecycle_counts WHERE lifecycle = 'PENDING_REVIEW'), 0)), 'courses.lifecycle'
	    UNION ALL SELECT 52, 'courses.revisions_pending_review', to_jsonb((SELECT count(*) FROM course_revisions WHERE state = 'PENDING_REVIEW')), 'courses.revisions_pending_review'
    UNION ALL SELECT 53, 'courses.changes_requested', to_jsonb(COALESCE((SELECT total FROM course_lifecycle_counts WHERE lifecycle = 'CHANGES_REQUESTED'), 0)), 'courses.lifecycle'
    UNION ALL SELECT 54, 'courses.published', to_jsonb(COALESCE((SELECT total FROM course_lifecycle_counts WHERE lifecycle = 'PUBLISHED'), 0)), 'courses.lifecycle'
    UNION ALL SELECT 55, 'courses.delisted', to_jsonb(COALESCE((SELECT total FROM course_lifecycle_counts WHERE lifecycle = 'DELISTED'), 0)), 'courses.lifecycle'
    UNION ALL SELECT 56, 'courses.archived', to_jsonb(COALESCE((SELECT total FROM course_lifecycle_counts WHERE lifecycle = 'ARCHIVED'), 0)), 'courses.lifecycle'
    UNION ALL SELECT 57, 'enrollments.total', to_jsonb((SELECT count(*) FROM enrollments)), 'enrollments.total'
    UNION ALL SELECT 58, 'enrollments.new_30d', to_jsonb((SELECT count(*) FROM enrollments e CROSS JOIN params p WHERE e.created_at >= p.as_of - interval '30 days')), 'enrollments.new'
    UNION ALL SELECT 60, 'instructors.active', to_jsonb(instructor_counts.active), 'instructors.active' FROM instructor_counts
    UNION ALL SELECT 61, 'instructors.with_published_course', to_jsonb(instructor_counts.with_published_course), 'instructors.with_published_course' FROM instructor_counts
    UNION ALL SELECT 70, 'purchase_requests.waiting_payment', to_jsonb((SELECT count(*) FROM purchase_requests WHERE state = 'WAITING_PAYMENT')), 'purchase_requests.state'
    UNION ALL SELECT 71, 'purchase_requests.invitation_created', to_jsonb((SELECT count(*) FROM purchase_requests WHERE state = 'INVITATION_CREATED')), 'purchase_requests.state'
    UNION ALL SELECT 72, 'purchase_requests.access_granted', to_jsonb((SELECT count(*) FROM purchase_requests WHERE state = 'ACCESS_GRANTED')), 'purchase_requests.state'
    UNION ALL SELECT 73, 'purchase_requests.cancelled', to_jsonb((SELECT count(*) FROM purchase_requests WHERE state = 'CANCELLED')), 'purchase_requests.state'
    UNION ALL SELECT 80, 'access_invitations.pending_student_acceptance', to_jsonb((SELECT count(*) FROM course_access_invitations WHERE state = 'PENDING_STUDENT_ACCEPTANCE')), 'access_invitations.state'
    UNION ALL SELECT 81, 'access_invitations.pending_admin_approval', to_jsonb((SELECT count(*) FROM course_access_invitations WHERE state = 'PENDING_ADMIN_APPROVAL')), 'access_invitations.state'
    UNION ALL SELECT 82, 'access_invitations.approved', to_jsonb((SELECT count(*) FROM course_access_invitations WHERE state = 'APPROVED')), 'access_invitations.state'
    UNION ALL SELECT 83, 'access_invitations.rejected', to_jsonb((SELECT count(*) FROM course_access_invitations WHERE state = 'REJECTED')), 'access_invitations.state'
    UNION ALL SELECT 84, 'access_invitations.cancelled', to_jsonb((SELECT count(*) FROM course_access_invitations WHERE state = 'CANCELLED')), 'access_invitations.state'
    UNION ALL SELECT 90, 'entitlements.manual_invitation.active', to_jsonb((SELECT count(*) FROM entitlements WHERE grant_source = 'MANUAL_INVITATION' AND state = 'ACTIVE')), 'entitlements.grant_source_and_state'
    UNION ALL SELECT 91, 'entitlements.manual_invitation.revoked', to_jsonb((SELECT count(*) FROM entitlements WHERE grant_source = 'MANUAL_INVITATION' AND state = 'REVOKED')), 'entitlements.grant_source_and_state'
	    UNION ALL SELECT 92, 'entitlements.purchase_request.active', to_jsonb((SELECT count(*) FROM entitlements WHERE grant_source = 'PURCHASE_REQUEST' AND state = 'ACTIVE')), 'entitlements.grant_source_and_state'
	    UNION ALL SELECT 93, 'entitlements.purchase_request.revoked', to_jsonb((SELECT count(*) FROM entitlements WHERE grant_source = 'PURCHASE_REQUEST' AND state = 'REVOKED')), 'entitlements.grant_source_and_state'
	    UNION ALL SELECT 94, 'entitlements.bundle_purchase.active', to_jsonb((SELECT count(*) FROM entitlements WHERE grant_source = 'BUNDLE_PURCHASE' AND state = 'ACTIVE')), 'entitlements.grant_source_and_state'
	    UNION ALL SELECT 95, 'entitlements.bundle_purchase.revoked', to_jsonb((SELECT count(*) FROM entitlements WHERE grant_source = 'BUNDLE_PURCHASE' AND state = 'REVOKED')), 'entitlements.grant_source_and_state'
    UNION ALL SELECT 100, 'subject_demand.top', top_subject_demand.items, 'subject_demand.top'
    FROM top_subject_demand
)
SELECT key, value, definition_key, (SELECT as_of FROM params) AS as_of
FROM metric_rows
ORDER BY ordinal
`

func (r *Repository) GetMetricsOverview(ctx context.Context, request MetricsOverviewRequest) (MetricsOverviewResult, error) {
	if err := validateMetricsOverviewRequest(request); err != nil {
		return MetricsOverviewResult{}, err
	}
	if r == nil || r.pool == nil {
		return MetricsOverviewResult{}, ErrRepositoryNil
	}

	rows, err := r.pool.Query(ctx, metricsOverviewQuery, string(request.Locale))
	if err != nil {
		return MetricsOverviewResult{}, fmt.Errorf("querying admin metrics overview: %w", err)
	}
	defer rows.Close()

	metrics := make([]Metric, 0, 41)
	var asOf time.Time
	for rows.Next() {
		var key, definitionKey string
		var value []byte
		if err := rows.Scan(&key, &value, &definitionKey, &asOf); err != nil {
			return MetricsOverviewResult{}, fmt.Errorf("scanning admin metric: %w", err)
		}
		metrics = append(metrics, Metric{
			Key: key, Value: json.RawMessage(append([]byte(nil), value...)), DefinitionKey: definitionKey,
		})
	}
	if err := rows.Err(); err != nil {
		return MetricsOverviewResult{}, fmt.Errorf("iterating admin metrics overview: %w", err)
	}
	return MetricsOverviewResult{Metrics: metrics, AsOf: asOf}, nil
}

const metricsCoursesQuery = `
WITH current_course_lessons AS (
    SELECT c.id AS course_id, cli.id AS lesson_identity_id
    FROM courses c
    JOIN course_revisions cr ON cr.id = c.live_revision_id AND cr.course_id = c.id AND cr.state = 'APPROVED'
    JOIN course_sections cs ON cs.revision_id = cr.id AND cs.course_id = c.id
    JOIN course_lessons cl ON cl.section_id = cs.id AND cl.course_id = c.id
    JOIN course_lesson_identities cli
      ON cli.id = cl.lesson_identity_id AND cli.course_id = c.id AND cli.section_identity_id = cl.section_identity_id
),
enrollment_progress AS (
    SELECT
        e.id AS enrollment_id,
        e.course_id,
        e.student_account_id,
        count(DISTINCT lesson.lesson_identity_id) AS total_lessons,
        count(DISTINCT progress.course_lesson_identity_id) FILTER (WHERE progress.completed_at IS NOT NULL) AS completed_lessons,
        count(DISTINCT progress.id) AS progress_rows,
        max(progress.last_watched_at) AS last_watched_at,
        completion.id IS NOT NULL AS completed_durably
    FROM enrollments e
    JOIN accounts student ON student.id = e.student_account_id AND student.role = 'STUDENT'
    LEFT JOIN current_course_lessons lesson ON lesson.course_id = e.course_id
    LEFT JOIN progress
      ON progress.enrollment_id = e.id
     AND progress.course_lesson_identity_id = lesson.lesson_identity_id
    LEFT JOIN course_completions completion ON completion.enrollment_id = e.id
    GROUP BY e.id, e.course_id, e.student_account_id, completion.id
),
course_rollup AS (
    SELECT
        course_id,
        count(*) AS enrolled,
        count(*) FILTER (WHERE progress_rows > 0) AS started,
        count(DISTINCT student_account_id) FILTER (WHERE last_watched_at >= now() - interval '7 days') AS learning_active_7d,
        COALESCE(avg(completed_lessons::numeric / NULLIF(total_lessons, 0)) * 100, 0) AS average_progress,
        count(*) FILTER (WHERE completed_durably) AS completed
    FROM enrollment_progress
    GROUP BY course_id
)
SELECT
	    c.id::text AS id,
	    CASE WHEN $1::text = 'ar' THEN COALESCE(live.title_ar, latest.title_ar, '') ELSE COALESCE(live.title_en, latest.title_en, '') END AS title,
	    COALESCE(owner.display_name, '') AS instructor,
	    c.lifecycle::text AS lifecycle,
	    COALESCE(rollup.enrolled, 0) AS enrolled,
	    COALESCE(rollup.started, 0) AS started,
	    COALESCE(rollup.learning_active_7d, 0) AS learning_active_7d,
	    COALESCE(rollup.average_progress, 0) AS average_progress,
	    COALESCE(rollup.completed, 0) AS completed
FROM courses c
LEFT JOIN course_revisions live ON live.id = c.live_revision_id AND live.course_id = c.id
LEFT JOIN LATERAL (
    SELECT cr.title_ar, cr.title_en
    FROM course_revisions cr
    WHERE cr.course_id = c.id
    ORDER BY cr.revision_number DESC
    LIMIT 1
) latest ON TRUE
JOIN accounts owner ON owner.id = c.owner_account_id
LEFT JOIN course_rollup rollup ON rollup.course_id = c.id
ORDER BY %s
LIMIT $2 OFFSET $3
`

func (r *Repository) ListMetricsCourses(ctx context.Context, request MetricsCoursesRequest) (MetricsCoursesResult, error) {
	if err := validateMetricsCoursesRequest(request); err != nil {
		return MetricsCoursesResult{}, err
	}
	if r == nil || r.pool == nil {
		return MetricsCoursesResult{}, ErrRepositoryNil
	}

	var total int
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM courses").Scan(&total); err != nil {
		return MetricsCoursesResult{}, fmt.Errorf("counting admin course metrics: %w", err)
	}
	rows, err := r.pool.Query(ctx, fmt.Sprintf(metricsCoursesQuery, metricsCourseOrder(request.Sort, request.Direction)),
		string(request.Locale), request.Limit, (request.Page-1)*request.Limit)
	if err != nil {
		return MetricsCoursesResult{}, fmt.Errorf("querying admin course metrics: %w", err)
	}
	defer rows.Close()

	items := make([]CourseMetric, 0, request.Limit)
	for rows.Next() {
		var item CourseMetric
		if err := rows.Scan(&item.ID, &item.Title, &item.Instructor, &item.Lifecycle, &item.Enrolled,
			&item.Started, &item.LearningActive7d, &item.AverageProgress, &item.Completed); err != nil {
			return MetricsCoursesResult{}, fmt.Errorf("scanning admin course metric: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return MetricsCoursesResult{}, fmt.Errorf("iterating admin course metrics: %w", err)
	}
	return MetricsCoursesResult{Items: items, Total: total, Page: request.Page, Limit: request.Limit,
		HasMore: request.Page*request.Limit < total}, nil
}

const metricsInstructorsQuery = `
WITH current_course_lessons AS (
    SELECT c.id AS course_id, cli.id AS lesson_identity_id
    FROM courses c
    JOIN course_revisions cr ON cr.id = c.live_revision_id AND cr.course_id = c.id AND cr.state = 'APPROVED'
    JOIN course_sections cs ON cs.revision_id = cr.id AND cs.course_id = c.id
    JOIN course_lessons cl ON cl.section_id = cs.id AND cl.course_id = c.id
    JOIN course_lesson_identities cli
      ON cli.id = cl.lesson_identity_id AND cli.course_id = c.id AND cli.section_identity_id = cl.section_identity_id
),
published AS (
    SELECT owner_account_id, count(*) AS published_courses
    FROM courses
    WHERE lifecycle = 'PUBLISHED'
    GROUP BY owner_account_id
),
learning_active AS (
    SELECT c.owner_account_id, count(DISTINCT e.student_account_id) AS active_students
    FROM courses c
    JOIN current_course_lessons lesson ON lesson.course_id = c.id
    JOIN enrollments e ON e.course_id = c.id
    JOIN accounts student ON student.id = e.student_account_id AND student.role = 'STUDENT'
    JOIN progress p ON p.enrollment_id = e.id AND p.course_lesson_identity_id = lesson.lesson_identity_id
    WHERE p.last_watched_at >= now() - interval '7 days'
    GROUP BY c.owner_account_id
),
enrollment_totals AS (
    SELECT c.owner_account_id, count(e.id) AS total_enrollments
    FROM courses c
    LEFT JOIN enrollments e ON e.course_id = c.id
    GROUP BY c.owner_account_id
)
SELECT
	    a.id::text AS id,
	    COALESCE(a.display_name, '') AS name,
	    COALESCE(published.published_courses, 0) AS published_courses,
	    COALESCE(enrollment_totals.total_enrollments, 0) AS total_enrollments,
	    COALESCE(learning_active.active_students, 0) AS learning_active_students_7d
FROM accounts a
LEFT JOIN published ON published.owner_account_id = a.id
LEFT JOIN enrollment_totals ON enrollment_totals.owner_account_id = a.id
LEFT JOIN learning_active ON learning_active.owner_account_id = a.id
WHERE a.role = 'INSTRUCTOR'
ORDER BY %s
LIMIT $1 OFFSET $2
`

func (r *Repository) ListMetricsInstructors(ctx context.Context, request MetricsInstructorsRequest) (MetricsInstructorsResult, error) {
	if err := validateMetricsInstructorsRequest(request); err != nil {
		return MetricsInstructorsResult{}, err
	}
	if r == nil || r.pool == nil {
		return MetricsInstructorsResult{}, ErrRepositoryNil
	}

	var total int
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM accounts WHERE role = 'INSTRUCTOR'").Scan(&total); err != nil {
		return MetricsInstructorsResult{}, fmt.Errorf("counting admin instructor metrics: %w", err)
	}
	rows, err := r.pool.Query(ctx, fmt.Sprintf(metricsInstructorsQuery, metricsInstructorOrder(request.Sort, request.Direction)),
		request.Limit, (request.Page-1)*request.Limit)
	if err != nil {
		return MetricsInstructorsResult{}, fmt.Errorf("querying admin instructor metrics: %w", err)
	}
	defer rows.Close()

	items := make([]InstructorMetric, 0, request.Limit)
	for rows.Next() {
		var item InstructorMetric
		if err := rows.Scan(&item.ID, &item.Name, &item.PublishedCourses, &item.TotalEnrollments,
			&item.LearningActiveStudents7d); err != nil {
			return MetricsInstructorsResult{}, fmt.Errorf("scanning admin instructor metric: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return MetricsInstructorsResult{}, fmt.Errorf("iterating admin instructor metrics: %w", err)
	}
	return MetricsInstructorsResult{Items: items, Total: total, Page: request.Page, Limit: request.Limit,
		HasMore: request.Page*request.Limit < total}, nil
}

func validateMetricsOverviewRequest(request MetricsOverviewRequest) error {
	if err := validateIdentityRead(request.Principal); err != nil {
		return err
	}
	if !request.Locale.Valid() {
		return ErrInvalidInput
	}
	return nil
}

func validateMetricsCoursesRequest(request MetricsCoursesRequest) error {
	if err := validateIdentityRead(request.Principal); err != nil {
		return err
	}
	if !request.Locale.Valid() || !validMetricsPage(request.Page, request.Limit) ||
		!validMetricsDirection(request.Direction) || !validMetricsCourseSort(request.Sort) {
		return ErrInvalidInput
	}
	return nil
}

func validateMetricsInstructorsRequest(request MetricsInstructorsRequest) error {
	if err := validateIdentityRead(request.Principal); err != nil {
		return err
	}
	if !validMetricsPage(request.Page, request.Limit) || !validMetricsDirection(request.Direction) || !validMetricsInstructorSort(request.Sort) {
		return ErrInvalidInput
	}
	return nil
}

func validMetricsPage(page, limit int) bool {
	return page >= 1 && page <= 10_000 && limit >= 1 && limit <= 50
}

func validMetricsDirection(direction string) bool {
	return direction == "asc" || direction == "desc"
}

func validMetricsCourseSort(sort string) bool {
	switch sort {
	case "title", "instructor", "lifecycle", "enrolled", "started", "learning_active_7d", "average_progress", "completed":
		return true
	default:
		return false
	}
}

func validMetricsInstructorSort(sort string) bool {
	switch sort {
	case "name", "published_courses", "total_enrollments", "learning_active_students_7d":
		return true
	default:
		return false
	}
}

func metricsCourseOrder(sort, direction string) string {
	column := map[string]string{
		"title":              "title",
		"instructor":         "instructor",
		"lifecycle":          "lifecycle",
		"enrolled":           "enrolled",
		"started":            "started",
		"learning_active_7d": "learning_active_7d",
		"average_progress":   "average_progress",
		"completed":          "completed",
	}[sort]
	return fmt.Sprintf("%s %s NULLS LAST, c.id %s", column, strings.ToUpper(direction), strings.ToUpper(direction))
}

func metricsInstructorOrder(sort, direction string) string {
	column := map[string]string{
		"name":                        "name",
		"published_courses":           "published_courses",
		"total_enrollments":           "total_enrollments",
		"learning_active_students_7d": "learning_active_students_7d",
	}[sort]
	return fmt.Sprintf("%s %s NULLS LAST, a.id %s", column, strings.ToUpper(direction), strings.ToUpper(direction))
}

var _ interface {
	GetMetricsOverview(context.Context, MetricsOverviewRequest) (MetricsOverviewResult, error)
	ListMetricsCourses(context.Context, MetricsCoursesRequest) (MetricsCoursesResult, error)
	ListMetricsInstructors(context.Context, MetricsInstructorsRequest) (MetricsInstructorsResult, error)
} = (*Repository)(nil)
