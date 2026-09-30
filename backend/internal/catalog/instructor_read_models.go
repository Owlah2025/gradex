package catalog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type InstructorRevisionSummary struct {
	ID             string        `json:"id"`
	RevisionNumber int           `json:"revision_number"`
	State          RevisionState `json:"state"`
	ReviewReason   *string       `json:"review_reason,omitempty"`
}

type InstructorDashboardCourse struct {
	CourseID                  string                     `json:"course_id"`
	TitleAr                   string                     `json:"title_ar"`
	TitleEn                   string                     `json:"title_en"`
	Lifecycle                 CourseLifecycle            `json:"lifecycle"`
	PublishedRevision         *InstructorRevisionSummary `json:"published_revision,omitempty"`
	CandidateRevision         *InstructorRevisionSummary `json:"candidate_revision,omitempty"`
	LatestChangeRequestReason *string                    `json:"latest_change_request_reason,omitempty"`
	Enrollments               int                        `json:"enrollments"`
	LearningActiveStudents7d  int                        `json:"learning_active_students_7d"`
	AverageProgress           float64                    `json:"average_progress"`
	Completions               int                        `json:"completions"`
	MediaProcessingFailures   int                        `json:"media_processing_failures"`
}

type InstructorAlert struct {
	Kind     string  `json:"kind"`
	CourseID *string `json:"course_id,omitempty"`
	TitleAr  string  `json:"title_ar"`
	TitleEn  string  `json:"title_en"`
	Reason   *string `json:"reason,omitempty"`
}

type InstructorDashboard struct {
	Courses []InstructorDashboardCourse `json:"courses"`
	Alerts  []InstructorAlert           `json:"alerts"`
}

type CourseAnalyticsRequest struct {
	CourseID       string
	OwnerAccountID string
	Now            time.Time
}

type CourseLessonReach struct {
	LessonID          string  `json:"lesson_id"`
	SectionTitleAr    string  `json:"section_title_ar"`
	SectionTitleEn    string  `json:"section_title_en"`
	LessonTitleAr     string  `json:"lesson_title_ar"`
	LessonTitleEn     string  `json:"lesson_title_en"`
	SectionPosition   int     `json:"section_position"`
	LessonPosition    int     `json:"lesson_position"`
	StudentsReached   int     `json:"students_reached"`
	StudentsCompleted int     `json:"students_completed"`
	ReachPercent      float64 `json:"reach_percent"`
	DropOffPercent    float64 `json:"drop_off_percent"`
}

type CourseAnalyticsDefinition struct {
	Ar string `json:"ar"`
	En string `json:"en"`
}

type CourseAnalytics struct {
	CourseID                 string                               `json:"course_id"`
	TitleAr                  string                               `json:"title_ar"`
	TitleEn                  string                               `json:"title_en"`
	Lifecycle                CourseLifecycle                      `json:"lifecycle"`
	Enrolled                 int                                  `json:"enrolled"`
	Started                  int                                  `json:"started"`
	LearningActiveStudents7d int                                  `json:"learning_active_students_7d"`
	AverageProgress          float64                              `json:"average_progress"`
	Completed                int                                  `json:"completed"`
	WatchTimeCollected       bool                                 `json:"watch_time_collected"`
	Definitions              map[string]CourseAnalyticsDefinition `json:"definitions"`
	LessonReach              []CourseLessonReach                  `json:"lesson_reach"`
}

const instructorDashboardQuery = `
WITH owned_courses AS (
    SELECT c.*
    FROM courses c
    WHERE c.owner_account_id = $1::uuid
),
current_course_lessons AS (
    SELECT c.id AS course_id, cli.id AS lesson_identity_id
    FROM owned_courses c
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
),
current_lesson_counts AS (
    SELECT course_id, count(*) AS total_lessons
    FROM current_course_lessons
    GROUP BY course_id
),
enrollment_progress AS (
    SELECT e.id AS enrollment_id,
           e.course_id,
           e.student_account_id,
           COALESCE(lesson_counts.total_lessons, 0) AS total_lessons,
           count(DISTINCT progress.course_lesson_identity_id)
               FILTER (WHERE progress.completed_at IS NOT NULL) AS completed_lessons,
           count(DISTINCT progress.id) AS progress_rows,
           max(progress.last_watched_at) AS last_watched_at
    FROM enrollments e
    JOIN owned_courses c ON c.id = e.course_id
    JOIN accounts student ON student.id = e.student_account_id AND student.role = 'STUDENT'
    LEFT JOIN current_lesson_counts lesson_counts ON lesson_counts.course_id = e.course_id
    LEFT JOIN progress
       ON progress.enrollment_id = e.id
      AND EXISTS (
          SELECT 1
          FROM current_course_lessons lesson
          WHERE lesson.course_id = e.course_id
            AND lesson.lesson_identity_id = progress.course_lesson_identity_id
      )
    GROUP BY e.id, e.course_id, e.student_account_id, lesson_counts.total_lessons
),
course_rollup AS (
    SELECT course_id,
           count(*) AS enrollments,
           count(*) FILTER (WHERE progress_rows > 0) AS started,
           count(DISTINCT student_account_id)
               FILTER (WHERE last_watched_at >= $2::timestamptz - interval '7 days') AS learning_active_7d,
           COALESCE(avg(completed_lessons::numeric / NULLIF(total_lessons, 0)) * 100, 0) AS average_progress,
           count(*) FILTER (WHERE EXISTS (
               SELECT 1 FROM course_completions completion
               WHERE completion.enrollment_id = enrollment_progress.enrollment_id
           )) AS completions
    FROM enrollment_progress
    GROUP BY course_id
)
SELECT c.id::text,
       COALESCE(live.title_ar, candidate.title_ar, latest.title_ar, ''),
       COALESCE(live.title_en, candidate.title_en, latest.title_en, ''),
       c.lifecycle::text,
       live.id::text,
       live.revision_number,
       candidate.id::text,
       candidate.revision_number,
       candidate.state::text,
       candidate.review_reason,
       latest_change.review_reason,
       COALESCE(rollup.enrollments, 0),
       COALESCE(rollup.learning_active_7d, 0),
       COALESCE(rollup.average_progress, 0),
       COALESCE(rollup.completions, 0),
       COALESCE(media_failures.total, 0)
FROM owned_courses c
LEFT JOIN course_revisions live
  ON live.id = c.live_revision_id
 AND live.course_id = c.id
LEFT JOIN LATERAL (
    SELECT cr.id, cr.revision_number, cr.state, cr.title_ar, cr.title_en, cr.review_reason
    FROM course_revisions cr
    WHERE cr.course_id = c.id
      AND cr.state IN ('DRAFT', 'CHANGES_REQUESTED', 'PENDING_REVIEW')
    ORDER BY cr.revision_number DESC
    LIMIT 1
) candidate ON TRUE
LEFT JOIN LATERAL (
    SELECT cr.title_ar, cr.title_en
    FROM course_revisions cr
    WHERE cr.course_id = c.id
    ORDER BY cr.revision_number DESC
    LIMIT 1
) latest ON TRUE
LEFT JOIN LATERAL (
    SELECT cr.review_reason
    FROM course_revisions cr
    WHERE cr.course_id = c.id
      AND cr.state = 'CHANGES_REQUESTED'
      AND cr.review_reason IS NOT NULL
    ORDER BY cr.reviewed_at DESC NULLS LAST, cr.revision_number DESC
    LIMIT 1
) latest_change ON TRUE
LEFT JOIN course_rollup rollup ON rollup.course_id = c.id
LEFT JOIN LATERAL (
    SELECT count(DISTINCT mav.id) AS total
    FROM (
        SELECT cr.preview_asset_version_id AS asset_version_id
        FROM course_revisions cr
        WHERE cr.id IN (c.live_revision_id, candidate.id)
        UNION ALL
        SELECT cr.thumbnail_asset_version_id
        FROM course_revisions cr
        WHERE cr.id IN (c.live_revision_id, candidate.id)
        UNION ALL
        SELECT cl.video_asset_version_id
        FROM course_revisions cr
        JOIN course_sections cs ON cs.revision_id = cr.id
        JOIN course_lessons cl ON cl.section_id = cs.id
        WHERE cr.id IN (c.live_revision_id, candidate.id)
        UNION ALL
        SELECT lf.asset_version_id
        FROM course_revisions cr
        JOIN course_sections cs ON cs.revision_id = cr.id
        JOIN course_lessons cl ON cl.section_id = cs.id
        JOIN lesson_files lf ON lf.lesson_id = cl.id
        WHERE cr.id IN (c.live_revision_id, candidate.id)
    ) referenced
    JOIN media_asset_versions mav ON mav.id = referenced.asset_version_id
    WHERE mav.state IN ('PROCESS_FAILED', 'SCAN_FAILED', 'SCAN_ERROR')
) media_failures ON TRUE
ORDER BY c.updated_at DESC, c.id ASC
`

func (r *Repository) ReadInstructorDashboard(ctx context.Context, ownerAccountID string, now time.Time) (InstructorDashboard, error) {
	if r == nil || r.pool == nil {
		return InstructorDashboard{}, ErrRepositoryNil
	}
	if ownerAccountID == "" || now.IsZero() {
		return InstructorDashboard{}, ErrCourseNotFound
	}
	rows, err := r.pool.Query(ctx, instructorDashboardQuery, ownerAccountID, now.UTC())
	if err != nil {
		return InstructorDashboard{}, fmt.Errorf("reading instructor dashboard: %w", err)
	}
	defer rows.Close()

	response := InstructorDashboard{Courses: make([]InstructorDashboardCourse, 0), Alerts: make([]InstructorAlert, 0)}
	for rows.Next() {
		var item InstructorDashboardCourse
		var lifecycle string
		var liveID, candidateID, candidateState, candidateReason, latestChangeReason *string
		var liveRevisionNumber, candidateRevisionNumber *int
		var mediaFailures int64
		if err := rows.Scan(
			&item.CourseID, &item.TitleAr, &item.TitleEn, &lifecycle,
			&liveID, &liveRevisionNumber,
			&candidateID, &candidateRevisionNumber, &candidateState, &candidateReason,
			&latestChangeReason,
			&item.Enrollments, &item.LearningActiveStudents7d, &item.AverageProgress, &item.Completions,
			&mediaFailures,
		); err != nil {
			return InstructorDashboard{}, fmt.Errorf("scanning instructor dashboard course: %w", err)
		}
		item.Lifecycle = CourseLifecycle(lifecycle)
		item.MediaProcessingFailures = int(mediaFailures)
		if liveID != nil && liveRevisionNumber != nil {
			item.PublishedRevision = &InstructorRevisionSummary{ID: *liveID, RevisionNumber: *liveRevisionNumber, State: RevisionApproved}
		}
		if candidateID != nil && candidateRevisionNumber != nil && candidateState != nil {
			item.CandidateRevision = &InstructorRevisionSummary{
				ID: *candidateID, RevisionNumber: *candidateRevisionNumber, State: RevisionState(*candidateState), ReviewReason: candidateReason,
			}
		}
		if latestChangeReason != nil && *latestChangeReason != "" {
			item.LatestChangeRequestReason = latestChangeReason
		}
		response.Courses = append(response.Courses, item)

		courseID := item.CourseID
		if item.CandidateRevision != nil {
			switch item.CandidateRevision.State {
			case RevisionChangesRequested:
				response.Alerts = append(response.Alerts, InstructorAlert{Kind: "CHANGES_REQUESTED", CourseID: &courseID, TitleAr: item.TitleAr, TitleEn: item.TitleEn, Reason: item.CandidateRevision.ReviewReason})
			case RevisionPendingReview:
				response.Alerts = append(response.Alerts, InstructorAlert{Kind: "AWAITING_REVIEW", CourseID: &courseID, TitleAr: item.TitleAr, TitleEn: item.TitleEn})
			}
		}
		if item.MediaProcessingFailures > 0 {
			response.Alerts = append(response.Alerts, InstructorAlert{Kind: "MEDIA_PROCESSING_FAILED", CourseID: &courseID, TitleAr: item.TitleAr, TitleEn: item.TitleEn})
		}
	}
	if err := rows.Err(); err != nil {
		return InstructorDashboard{}, fmt.Errorf("iterating instructor dashboard: %w", err)
	}

	var profileReason *string
	var profileChangesRequested bool
	if err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM instructor_profiles
			WHERE account_id = $1::uuid AND publication_state = 'CHANGES_REQUESTED'
		), (
			SELECT decision_note
			FROM instructor_profiles
			WHERE account_id = $1::uuid AND publication_state = 'CHANGES_REQUESTED'
		)
	`, ownerAccountID).Scan(&profileChangesRequested, &profileReason); err != nil {
		return InstructorDashboard{}, fmt.Errorf("reading instructor profile alert: %w", err)
	}
	if profileChangesRequested {
		response.Alerts = append(response.Alerts, InstructorAlert{Kind: "PROFILE_CHANGES_REQUESTED", TitleAr: "", TitleEn: "", Reason: profileReason})
	}
	return response, nil
}

const courseAnalyticsTotalsQuery = `
WITH current_course_lessons AS (
    SELECT cli.id AS lesson_identity_id
    FROM courses c
    JOIN course_revisions live
      ON live.id = c.live_revision_id AND live.course_id = c.id AND live.state = 'APPROVED'
    JOIN course_sections cs ON cs.revision_id = live.id AND cs.course_id = c.id
    JOIN course_lessons cl ON cl.section_id = cs.id AND cl.course_id = c.id
    JOIN course_lesson_identities cli
      ON cli.id = cl.lesson_identity_id
     AND cli.course_id = c.id
     AND cli.section_identity_id = cl.section_identity_id
    WHERE c.id = $1::uuid AND c.owner_account_id = $2::uuid
),
current_lesson_counts AS (
    SELECT count(*) AS total_lessons
    FROM current_course_lessons
),
enrollment_progress AS (
    SELECT e.id AS enrollment_id,
           e.student_account_id,
           COALESCE(lesson_counts.total_lessons, 0) AS total_lessons,
           count(DISTINCT progress.course_lesson_identity_id)
               FILTER (WHERE progress.completed_at IS NOT NULL) AS completed_lessons,
           count(DISTINCT progress.id) AS progress_rows,
           max(progress.last_watched_at) AS last_watched_at
    FROM enrollments e
    JOIN accounts student ON student.id = e.student_account_id AND student.role = 'STUDENT'
    CROSS JOIN current_lesson_counts lesson_counts
    LEFT JOIN progress
       ON progress.enrollment_id = e.id
      AND EXISTS (
          SELECT 1
          FROM current_course_lessons lesson
          WHERE lesson.lesson_identity_id = progress.course_lesson_identity_id
      )
    WHERE e.course_id = $1::uuid
    GROUP BY e.id, e.student_account_id, lesson_counts.total_lessons
),
rollup AS (
    SELECT count(*) AS enrolled,
           count(*) FILTER (WHERE progress_rows > 0) AS started,
           count(DISTINCT student_account_id)
               FILTER (WHERE last_watched_at >= $3::timestamptz - interval '7 days') AS learning_active_7d,
           COALESCE(avg(completed_lessons::numeric / NULLIF(total_lessons, 0)) * 100, 0) AS average_progress,
           count(*) FILTER (WHERE EXISTS (
               SELECT 1 FROM course_completions completion
               WHERE completion.enrollment_id = enrollment_progress.enrollment_id
           )) AS completed
    FROM enrollment_progress
)
SELECT c.id::text,
       COALESCE(live.title_ar, candidate.title_ar, latest.title_ar, ''),
       COALESCE(live.title_en, candidate.title_en, latest.title_en, ''),
       c.lifecycle::text,
       COALESCE(rollup.enrolled, 0),
       COALESCE(rollup.started, 0),
       COALESCE(rollup.learning_active_7d, 0),
       COALESCE(rollup.average_progress, 0),
       COALESCE(rollup.completed, 0)
FROM courses c
LEFT JOIN course_revisions live ON live.id = c.live_revision_id AND live.course_id = c.id
LEFT JOIN LATERAL (
    SELECT cr.title_ar, cr.title_en
    FROM course_revisions cr
    WHERE cr.course_id = c.id AND cr.state IN ('DRAFT', 'CHANGES_REQUESTED', 'PENDING_REVIEW')
    ORDER BY cr.revision_number DESC LIMIT 1
) candidate ON TRUE
LEFT JOIN LATERAL (
    SELECT cr.title_ar, cr.title_en
    FROM course_revisions cr
    WHERE cr.course_id = c.id
    ORDER BY cr.revision_number DESC LIMIT 1
) latest ON TRUE
CROSS JOIN rollup
WHERE c.id = $1::uuid AND c.owner_account_id = $2::uuid
`

const courseAnalyticsLessonReachQuery = `
WITH current_lessons AS (
    SELECT cli.id AS lesson_identity_id,
           cli.id::text AS lesson_id,
           cs.title_ar AS section_title_ar,
           cs.title_en AS section_title_en,
           cl.title_ar AS lesson_title_ar,
           cl.title_en AS lesson_title_en,
           cs.position AS section_position,
           cl.position AS lesson_position,
           cs.id AS section_id,
           cl.id AS lesson_row_id
    FROM courses c
    JOIN course_revisions live
      ON live.id = c.live_revision_id AND live.course_id = c.id AND live.state = 'APPROVED'
    JOIN course_sections cs ON cs.revision_id = live.id AND cs.course_id = c.id
    JOIN course_lessons cl ON cl.section_id = cs.id AND cl.course_id = c.id
    JOIN course_lesson_identities cli
      ON cli.id = cl.lesson_identity_id
     AND cli.course_id = cl.course_id
     AND cli.section_identity_id = cl.section_identity_id
    WHERE c.id = $1::uuid AND c.owner_account_id = $2::uuid
),
lesson_progress AS (
    SELECT progress.course_lesson_identity_id AS lesson_identity_id,
           count(DISTINCT progress.enrollment_id) AS students_reached,
           count(DISTINCT progress.enrollment_id) FILTER (WHERE progress.completed_at IS NOT NULL) AS students_completed
    FROM progress
    JOIN enrollments enrollment ON enrollment.id = progress.enrollment_id AND enrollment.course_id = $1::uuid
    JOIN accounts student ON student.id = enrollment.student_account_id AND student.role = 'STUDENT'
    JOIN current_lessons lesson ON lesson.lesson_identity_id = progress.course_lesson_identity_id
    GROUP BY progress.course_lesson_identity_id
)
SELECT lesson.lesson_id,
       lesson.section_title_ar, lesson.section_title_en,
       lesson.lesson_title_ar, lesson.lesson_title_en,
       lesson.section_position, lesson.lesson_position,
       COALESCE(lesson_progress.students_reached, 0),
       COALESCE(lesson_progress.students_completed, 0)
FROM current_lessons lesson
LEFT JOIN lesson_progress ON lesson_progress.lesson_identity_id = lesson.lesson_identity_id
ORDER BY lesson.section_position ASC, lesson.section_id ASC, lesson.lesson_position ASC, lesson.lesson_row_id ASC
`

func (r *Repository) ReadCourseAnalytics(ctx context.Context, request CourseAnalyticsRequest) (CourseAnalytics, error) {
	if r == nil || r.pool == nil {
		return CourseAnalytics{}, ErrRepositoryNil
	}
	if request.CourseID == "" || request.OwnerAccountID == "" || request.Now.IsZero() {
		return CourseAnalytics{}, ErrCourseNotFound
	}

	var analytics CourseAnalytics
	var lifecycle string
	if err := r.pool.QueryRow(ctx, courseAnalyticsTotalsQuery, request.CourseID, request.OwnerAccountID, request.Now.UTC()).Scan(
		&analytics.CourseID, &analytics.TitleAr, &analytics.TitleEn, &lifecycle,
		&analytics.Enrolled, &analytics.Started, &analytics.LearningActiveStudents7d,
		&analytics.AverageProgress, &analytics.Completed,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CourseAnalytics{}, ErrCourseNotFound
		}
		return CourseAnalytics{}, fmt.Errorf("reading course analytics totals: %w", err)
	}
	analytics.Lifecycle = CourseLifecycle(lifecycle)
	analytics.WatchTimeCollected = false
	analytics.Definitions = map[string]CourseAnalyticsDefinition{
		"learning_active_7d": {Ar: "الطلبة الذين لديهم نشاط تعلم خلال آخر 7 أيام، محسوباً من آخر مشاهدة مسجلة.", En: "Distinct enrolled students with progress activity in the last 7 days, based on last_watched_at."},
		"average_progress":   {Ar: "متوسط الدروس المكتملة مقسوماً على عدد الدروس المنشورة حالياً؛ لا يمثل وقت المشاهدة.", En: "Average completed lessons divided by the current published lesson count; this is not watch time."},
		"lesson_reach":       {Ar: "عدد الطلاب المسجلين الذين لديهم أي تقدم في الدرس، مرتباً حسب المنهج المنشور.", En: "Distinct enrolled students with any progress row for the lesson, ordered by the published curriculum."},
		"drop_off":           {Ar: "تقدير: نسبة الوصول إلى الدرس السابق ناقص نسبة الوصول إلى هذا الدرس.", En: "Approximation: the previous lesson's reach percentage minus this lesson's reach percentage."},
		"watch_time":         {Ar: "لا تجمع GradeX وقت المشاهدة.", En: "Watch time is not collected by GradeX."},
	}

	rows, err := r.pool.Query(ctx, courseAnalyticsLessonReachQuery, request.CourseID, request.OwnerAccountID)
	if err != nil {
		return CourseAnalytics{}, fmt.Errorf("reading course lesson analytics: %w", err)
	}
	defer rows.Close()
	analytics.LessonReach = make([]CourseLessonReach, 0)
	previousReach := 0.0
	for rows.Next() {
		var item CourseLessonReach
		if err := rows.Scan(
			&item.LessonID, &item.SectionTitleAr, &item.SectionTitleEn, &item.LessonTitleAr, &item.LessonTitleEn,
			&item.SectionPosition, &item.LessonPosition, &item.StudentsReached, &item.StudentsCompleted,
		); err != nil {
			return CourseAnalytics{}, fmt.Errorf("scanning course lesson analytics: %w", err)
		}
		if analytics.Enrolled > 0 {
			item.ReachPercent = float64(item.StudentsReached) * 100 / float64(analytics.Enrolled)
		}
		if len(analytics.LessonReach) > 0 && previousReach > item.ReachPercent {
			item.DropOffPercent = previousReach - item.ReachPercent
		}
		previousReach = item.ReachPercent
		analytics.LessonReach = append(analytics.LessonReach, item)
	}
	if err := rows.Err(); err != nil {
		return CourseAnalytics{}, fmt.Errorf("iterating course lesson analytics: %w", err)
	}
	return analytics, nil
}

var _ AnnouncementReader = (*Repository)(nil)
