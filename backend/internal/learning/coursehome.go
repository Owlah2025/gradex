package learning

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrCourseGraphNotFound = errors.New("learning course graph not found")
	ErrCourseGraphInvalid  = errors.New("learning course graph is incomplete")
)

// CourseGraph is the current Student-visible graph. Its identifiers are the
// stable identities that survive live-revision replacement; revision-owned row
// identifiers never cross this package boundary.
type CourseGraph struct {
	CourseID   string
	RevisionID string
	RevisionNo int
	TitleAr    string
	TitleEn    string
	Sections   []CourseGraphSection
}

type CourseGraphSection struct {
	ID       string
	TitleAr  string
	TitleEn  string
	Position int
	Lessons  []CourseGraphLesson
}

type CourseGraphLesson struct {
	ID                  string
	SectionID           string
	TitleAr             string
	TitleEn             string
	Position            int
	VideoAssetVersionID *string
}

// LessonIDs returns stable Lesson identities in authored graph order.
func (g CourseGraph) LessonIDs() []string {
	ids := make([]string, 0)
	for _, section := range g.Sections {
		for _, lesson := range section.Lessons {
			ids = append(ids, lesson.ID)
		}
	}
	return ids
}

// CourseProgressSummary is deliberately independent of duration. Completion
// is the count of completed Lessons over the current qualifying graph.
type CourseProgressSummary struct {
	CompletedLessons int
	TotalLessons     int
}

type StudentCourseSummary struct {
	CourseID            string
	TitleAr             string
	TitleEn             string
	EnrollmentCreatedAt time.Time
	Progress            CourseProgressSummary
}

// ReadCourseGraph reads only the current approved live revision. Two bounded
// queries load the revision metadata and the ordered graph; no per-row query
// is performed. Missing identities or an empty/incomplete graph fail closed.
func (r *Repository) ReadCourseGraph(ctx context.Context, courseID string) (CourseGraph, error) {
	if r == nil || r.pool == nil || courseID == "" {
		return CourseGraph{}, ErrCourseGraphNotFound
	}
	var graph CourseGraph
	r.observeQuery("learning.graph")
	err := r.pool.QueryRow(ctx, `
		SELECT cr.id::text, cr.course_id::text, cr.revision_number, cr.title_ar, cr.title_en
		FROM courses c
		JOIN course_revisions cr ON cr.id = c.live_revision_id
		WHERE c.id = $1::uuid AND cr.course_id = c.id AND cr.state = 'APPROVED'
	`, courseID).Scan(&graph.RevisionID, &graph.CourseID, &graph.RevisionNo, &graph.TitleAr, &graph.TitleEn)
	if errors.Is(err, pgx.ErrNoRows) {
		return CourseGraph{}, ErrCourseGraphNotFound
	}
	if err != nil {
		return CourseGraph{}, fmt.Errorf("reading live course revision: %w", err)
	}

	r.observeQuery("learning.graph")
	rows, err := r.pool.Query(ctx, `
		SELECT cs.section_identity_id::text, cs.title_ar, cs.title_en, cs.position,
		       cli.id::text, cl.section_identity_id::text,
		       cl.title_ar, cl.title_en, cl.position, cl.video_asset_version_id::text
		FROM course_sections cs
		JOIN course_section_identities csi
		  ON csi.id = cs.section_identity_id AND csi.course_id = cs.course_id
		LEFT JOIN course_lessons cl
		  ON cl.section_id = cs.id AND cl.course_id = cs.course_id
		LEFT JOIN course_lesson_identities cli
		  ON cli.id = cl.lesson_identity_id
		 AND cli.course_id = cl.course_id
		 AND cli.section_identity_id = cl.section_identity_id
		WHERE cs.revision_id = $1::uuid AND cs.course_id = $2::uuid
		ORDER BY cs.position ASC, cs.id ASC, cl.position ASC, cl.id ASC
	`, graph.RevisionID, graph.CourseID)
	if err != nil {
		return CourseGraph{}, fmt.Errorf("reading live course graph: %w", err)
	}
	defer rows.Close()

	seenSections := make(map[string]struct{})
	seenLessons := make(map[string]struct{})
	for rows.Next() {
		var section CourseGraphSection
		var lessonSectionID, lessonID, lessonTitleAr, lessonTitleEn, lessonVersionID *string
		var lessonPosition *int
		if err := rows.Scan(
			&section.ID, &section.TitleAr, &section.TitleEn, &section.Position,
			&lessonID, &lessonSectionID, &lessonTitleAr, &lessonTitleEn, &lessonPosition, &lessonVersionID,
		); err != nil {
			return CourseGraph{}, fmt.Errorf("scanning live course graph: %w", err)
		}
		if len(graph.Sections) == 0 || graph.Sections[len(graph.Sections)-1].ID != section.ID {
			if _, exists := seenSections[section.ID]; exists {
				return CourseGraph{}, ErrCourseGraphInvalid
			}
			seenSections[section.ID] = struct{}{}
			section.Lessons = []CourseGraphLesson{}
			graph.Sections = append(graph.Sections, section)
		}
		if lessonID == nil || lessonSectionID == nil || lessonTitleAr == nil || lessonTitleEn == nil || lessonPosition == nil || *lessonSectionID != section.ID {
			return CourseGraph{}, ErrCourseGraphInvalid
		}
		if _, exists := seenLessons[*lessonID]; exists {
			return CourseGraph{}, ErrCourseGraphInvalid
		}
		seenLessons[*lessonID] = struct{}{}
		graph.Sections[len(graph.Sections)-1].Lessons = append(graph.Sections[len(graph.Sections)-1].Lessons, CourseGraphLesson{
			ID: *lessonID, SectionID: section.ID, TitleAr: *lessonTitleAr, TitleEn: *lessonTitleEn,
			Position: *lessonPosition, VideoAssetVersionID: lessonVersionID,
		})
	}
	if err := rows.Err(); err != nil {
		return CourseGraph{}, fmt.Errorf("iterating live course graph: %w", err)
	}
	if len(graph.Sections) == 0 {
		return CourseGraph{}, ErrCourseGraphInvalid
	}
	for _, section := range graph.Sections {
		if len(section.Lessons) == 0 {
			return CourseGraph{}, ErrCourseGraphInvalid
		}
	}
	return graph, nil
}

// AggregateCourseProgress counts only completed stable Lesson identities in
// the supplied current graph and only for the authenticated Student's
// Enrollment. It never reads or mutates Entitlement state.
func (r *Repository) AggregateCourseProgress(ctx context.Context, studentID, courseID string, graph CourseGraph) (CourseProgressSummary, error) {
	ids := graph.LessonIDs()
	if r == nil || r.pool == nil || studentID == "" || courseID == "" || graph.CourseID != courseID || len(ids) == 0 {
		return CourseProgressSummary{}, ErrEnrollmentNotFound
	}
	var enrollmentCount, completedLessons int64
	r.observeQuery("learning.course-progress")
	err := r.pool.QueryRow(ctx, `
		SELECT count(DISTINCT e.id),
		       count(DISTINCT p.course_lesson_identity_id) FILTER (WHERE p.completed_at IS NOT NULL)
		FROM enrollments e
		JOIN course_lesson_identities cli
		  ON cli.course_id = e.course_id AND cli.id = ANY($3::uuid[])
		LEFT JOIN progress p
		  ON p.enrollment_id = e.id AND p.course_lesson_identity_id = cli.id
		WHERE e.student_account_id = $1::uuid AND e.course_id = $2::uuid
	`, studentID, courseID, ids).Scan(&enrollmentCount, &completedLessons)
	if err != nil {
		return CourseProgressSummary{}, fmt.Errorf("aggregating course progress: %w", err)
	}
	if enrollmentCount != 1 {
		return CourseProgressSummary{}, ErrEnrollmentNotFound
	}
	return CourseProgressSummary{CompletedLessons: int(completedLessons), TotalLessons: len(ids)}, nil
}

// ReadCourseProgress returns every current-graph Lesson's Progress in one
// Student-scoped query together with the same completion aggregation used by
// Course Home. Missing rows are represented as zero/unfinished values.
func (r *Repository) ReadCourseProgress(ctx context.Context, enrollmentID, courseID string, graph CourseGraph) (map[string]Progress, CourseProgressSummary, error) {
	ids := graph.LessonIDs()
	if r == nil || r.pool == nil || enrollmentID == "" || courseID == "" || graph.CourseID != courseID || len(ids) == 0 {
		return nil, CourseProgressSummary{}, ErrProgressNotFound
	}
	r.observeQuery("learning.course-progress")
	rows, err := r.pool.Query(ctx, `
		SELECT cli.id::text,
		       COALESCE(p.last_position_seconds, 0),
		       COALESCE(p.completed_at IS NOT NULL, false)
		FROM course_lesson_identities cli
		LEFT JOIN progress p
		  ON p.enrollment_id = $1::uuid AND p.course_lesson_identity_id = cli.id
		WHERE cli.course_id = $2::uuid AND cli.id = ANY($3::uuid[])
		ORDER BY array_position($3::uuid[], cli.id)
	`, enrollmentID, courseID, ids)
	if err != nil {
		return nil, CourseProgressSummary{}, fmt.Errorf("reading course progress: %w", err)
	}
	defer rows.Close()
	progressByLesson := make(map[string]Progress, len(ids))
	completed := 0
	for rows.Next() {
		var lessonID string
		var progress Progress
		if err := rows.Scan(&lessonID, &progress.LastPositionSeconds, &progress.Completed); err != nil {
			return nil, CourseProgressSummary{}, fmt.Errorf("scanning course progress: %w", err)
		}
		progress.MaxPositionSeconds = progress.LastPositionSeconds
		progressByLesson[lessonID] = progress
		if progress.Completed {
			completed++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, CourseProgressSummary{}, fmt.Errorf("iterating course progress: %w", err)
	}
	if len(progressByLesson) != len(ids) {
		return nil, CourseProgressSummary{}, ErrCourseGraphInvalid
	}
	return progressByLesson, CourseProgressSummary{CompletedLessons: completed, TotalLessons: len(ids)}, nil
}

// ReadLessonProgress performs the single bounded lookup used by the Lesson
// read model. Missing Progress is a normal zero/unfinished presentation.
func (r *Repository) ReadLessonProgress(ctx context.Context, enrollmentID, lessonID string) (Progress, error) {
	progress, err := r.ProgressForLesson(ctx, enrollmentID, lessonID)
	if errors.Is(err, ErrProgressNotFound) {
		return Progress{}, nil
	}
	return progress, err
}

// ListStudentCourseSummaries is the Dashboard read query. It groups current
// live graph Lessons and the authenticated Student's Progress in one query,
// ordered by Enrollment creation then stable Course ID, with no N+1 loops.
func (r *Repository) ListStudentCourseSummaries(ctx context.Context, studentID string) ([]StudentCourseSummary, error) {
	if r == nil || r.pool == nil || studentID == "" {
		return nil, ErrEnrollmentNotFound
	}
	r.observeQuery("learning.dashboard")
	rows, err := r.pool.Query(ctx, `
		SELECT e.course_id::text, e.created_at, cr.title_ar, cr.title_en,
		       count(DISTINCT cli.id),
		       count(DISTINCT p.course_lesson_identity_id) FILTER (WHERE p.completed_at IS NOT NULL)
		FROM enrollments e
		JOIN courses c ON c.id = e.course_id
		JOIN course_revisions cr ON cr.id = c.live_revision_id AND cr.course_id = c.id AND cr.state = 'APPROVED'
		JOIN course_sections cs ON cs.revision_id = cr.id AND cs.course_id = c.id
		JOIN course_lessons cl ON cl.section_id = cs.id AND cl.course_id = c.id
		JOIN course_lesson_identities cli
		  ON cli.id = cl.lesson_identity_id AND cli.course_id = c.id AND cli.section_identity_id = cl.section_identity_id
		LEFT JOIN progress p
		  ON p.enrollment_id = e.id AND p.course_lesson_identity_id = cli.id
		WHERE e.student_account_id = $1::uuid
		GROUP BY e.course_id, e.created_at, cr.title_ar, cr.title_en
		ORDER BY e.created_at DESC, e.course_id ASC
	`, studentID)
	if err != nil {
		return nil, fmt.Errorf("listing student course summaries: %w", err)
	}
	defer rows.Close()
	summaries := make([]StudentCourseSummary, 0)
	for rows.Next() {
		var summary StudentCourseSummary
		var total, completed int64
		if err := rows.Scan(&summary.CourseID, &summary.EnrollmentCreatedAt, &summary.TitleAr, &summary.TitleEn, &total, &completed); err != nil {
			return nil, fmt.Errorf("scanning student course summary: %w", err)
		}
		summary.Progress = CourseProgressSummary{CompletedLessons: int(completed), TotalLessons: int(total)}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating student course summaries: %w", err)
	}
	return summaries, nil
}

// ResumeCandidate is the best Lesson to continue in one Course: the Lesson the Student was last
// watching and has not finished, or — when nothing in that Course is part-finished — its first
// unfinished Lesson in authored order.
type ResumeCandidate struct {
	CourseID      string
	LessonID      string
	TitleAr       string
	TitleEn       string
	LastWatchedAt *time.Time
	// The Course's most recent learning activity, used to rank Courses against each other when no
	// single Lesson is part-finished.
	CourseLastActiveAt *time.Time
}

// ListStudentResumeCandidates answers "what should this Student continue?" from Progress alone.
//
// No new persistence was needed: `progress.last_watched_at` and `completed_at` already carry it.
// Only the current approved live revision is joined, so an Instructor's in-flight candidate revision
// can never redirect a Student, and a Lesson dropped from the live graph simply stops being a
// candidate rather than producing a broken pointer.
//
// One bounded query with a per-Course window; there is no per-Course or per-Lesson round trip.
// Entitlement is deliberately NOT filtered here — enrollment and Progress outlive access by design,
// so the caller applies the authoritative evaluator decision and drops Courses that are no longer
// readable. Ordering is total and deterministic: a part-finished Lesson outranks an unstarted one,
// then most recent activity, then the Course's own recency, then stable Course id.
func (r *Repository) ListStudentResumeCandidates(ctx context.Context, studentID string) ([]ResumeCandidate, error) {
	if r == nil || r.pool == nil || studentID == "" {
		return nil, ErrEnrollmentNotFound
	}
	r.observeQuery("learning.resume")
	rows, err := r.pool.Query(ctx, `
		WITH student_lessons AS (
			-- The pointer must carry the stable Lesson identity, not the revision-scoped
			-- course_lessons row id. Progress is keyed on the identity, the Lesson routes
			-- resolve the identity, and a row id changes every time a revision is replaced,
			-- so selecting cl.id here produced a link the Student could not follow.
			SELECT e.course_id, cli.id AS lesson_id, cl.title_ar, cl.title_en,
			       cs.position AS section_position, cl.position AS lesson_position,
			       p.completed_at, p.last_watched_at
			FROM enrollments e
			JOIN courses c ON c.id = e.course_id
			JOIN course_revisions cr ON cr.id = c.live_revision_id AND cr.course_id = c.id AND cr.state = 'APPROVED'
			JOIN course_sections cs ON cs.revision_id = cr.id AND cs.course_id = c.id
			JOIN course_lessons cl ON cl.section_id = cs.id AND cl.course_id = c.id
			JOIN course_lesson_identities cli
			  ON cli.id = cl.lesson_identity_id AND cli.course_id = c.id AND cli.section_identity_id = cl.section_identity_id
			LEFT JOIN progress p
			  ON p.enrollment_id = e.id AND p.course_lesson_identity_id = cli.id
			WHERE e.student_account_id = $1::uuid
		),
		course_activity AS (
			SELECT course_id, max(last_watched_at) AS last_active
			FROM student_lessons GROUP BY course_id
		),
		ranked AS (
			SELECT sl.course_id, sl.lesson_id, sl.title_ar, sl.title_en, sl.last_watched_at,
			       ca.last_active,
			       row_number() OVER (
			         PARTITION BY sl.course_id
			         ORDER BY (sl.last_watched_at IS NOT NULL) DESC,
			                  sl.last_watched_at DESC NULLS LAST,
			                  sl.section_position ASC, sl.lesson_position ASC
			       ) AS rank_in_course
			FROM student_lessons sl
			JOIN course_activity ca ON ca.course_id = sl.course_id
			WHERE sl.completed_at IS NULL
		)
		SELECT course_id::text, lesson_id::text, title_ar, title_en, last_watched_at, last_active
		FROM ranked
		WHERE rank_in_course = 1
		ORDER BY (last_watched_at IS NOT NULL) DESC, last_watched_at DESC NULLS LAST,
		         last_active DESC NULLS LAST, course_id ASC
	`, studentID)
	if err != nil {
		return nil, fmt.Errorf("listing student resume candidates: %w", err)
	}
	defer rows.Close()

	candidates := make([]ResumeCandidate, 0)
	for rows.Next() {
		var candidate ResumeCandidate
		if err := rows.Scan(
			&candidate.CourseID, &candidate.LessonID, &candidate.TitleAr, &candidate.TitleEn,
			&candidate.LastWatchedAt, &candidate.CourseLastActiveAt,
		); err != nil {
			return nil, fmt.Errorf("scanning student resume candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating student resume candidates: %w", err)
	}
	return candidates, nil
}
