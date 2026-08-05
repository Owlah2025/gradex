package learning

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrEnrollmentNotFound = errors.New("learning enrollment not found")
var ErrProgressNotFound = errors.New("learning progress not found")

// Enrollment is the durable learning relationship resolved for a request.  It
// deliberately contains no entitlement state: enrollment is history and is
// never an authorization input.
type Enrollment struct {
	ID        string
	StudentID string
	CourseID  string
}

type Progress struct {
	MaxPositionSeconds  float64
	LastPositionSeconds float64
	Completed           bool
}

// Repository resolves learning state. Enrollment rows are created only by S6.
type Repository struct {
	pool    *pgxpool.Pool
	observe func(string)
}

func NewRepository(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("learning database is required")
	}
	return &Repository{pool: pool}, nil
}

// NewRepositoryWithQueryObserver is a deterministic test seam for asserting
// bounded read-model query shapes. Production composition uses NewRepository.
func NewRepositoryWithQueryObserver(pool *pgxpool.Pool, observe func(string)) (*Repository, error) {
	repository, err := NewRepository(pool)
	if err != nil {
		return nil, err
	}
	repository.observe = observe
	return repository, nil
}

func (r *Repository) observeQuery(name string) {
	if r != nil && r.observe != nil {
		r.observe(name)
	}
}

func (r *Repository) EnrollmentID(ctx context.Context, studentID, courseID string) (string, error) {
	if r == nil || r.pool == nil || studentID == "" || courseID == "" {
		return "", ErrEnrollmentNotFound
	}
	var enrollmentID string
	r.observeQuery("learning.enrollment")
	err := r.pool.QueryRow(ctx, `
		SELECT id::text FROM enrollments
		WHERE student_account_id = $1::uuid AND course_id = $2::uuid`, studentID, courseID).Scan(&enrollmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrEnrollmentNotFound
	}
	if err != nil {
		return "", fmt.Errorf("resolving learning enrollment: %w", err)
	}
	return enrollmentID, nil
}

// EnrollmentForLesson resolves an existing enrollment only when the exact
// Student, Course and live approved Lesson graph agree. It is a read boundary:
// no missing relationship is repaired or synthesized here.
func (r *Repository) EnrollmentForLesson(ctx context.Context, studentID, lessonID string) (Enrollment, error) {
	if r == nil || r.pool == nil || studentID == "" || lessonID == "" {
		return Enrollment{}, ErrEnrollmentNotFound
	}
	var enrollment Enrollment
	r.observeQuery("learning.enrollment")
	err := r.pool.QueryRow(ctx, `
		SELECT e.id::text, e.student_account_id::text, e.course_id::text
		FROM enrollments e
		JOIN accounts a ON a.id = e.student_account_id
		JOIN courses c ON c.id = e.course_id
		JOIN course_revisions cr ON cr.id = c.live_revision_id
			AND cr.course_id = c.id AND cr.state = 'APPROVED'
		JOIN course_sections cs ON cs.revision_id = cr.id AND cs.course_id = c.id
		JOIN course_lessons cl ON cl.section_id = cs.id AND cl.course_id = c.id
		WHERE e.student_account_id = $1::uuid
		  AND cl.lesson_identity_id = $2::uuid
	`, studentID, lessonID).Scan(&enrollment.ID, &enrollment.StudentID, &enrollment.CourseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Enrollment{}, ErrEnrollmentNotFound
	}
	if err != nil {
		return Enrollment{}, fmt.Errorf("resolving learning enrollment for lesson: %w", err)
	}
	return enrollment, nil
}

// LessonVideoVersion returns the exact video selected by the live approved
// Course revision. It never falls back to a newer version of a logical Asset.
func (r *Repository) LessonVideoVersion(ctx context.Context, lessonID string) (string, error) {
	if r == nil || r.pool == nil || lessonID == "" {
		return "", ErrEnrollmentNotFound
	}
	var versionID string
	r.observeQuery("learning.media")
	err := r.pool.QueryRow(ctx, `
		SELECT cl.video_asset_version_id::text
		FROM course_lessons cl
		JOIN course_sections cs ON cs.id = cl.section_id AND cs.course_id = cl.course_id
		JOIN courses c ON c.id = cl.course_id
		JOIN course_revisions cr ON cr.id = c.live_revision_id
			AND cr.id = cs.revision_id AND cr.state = 'APPROVED'
		WHERE cl.lesson_identity_id = $1::uuid AND cl.video_asset_version_id IS NOT NULL
	`, lessonID).Scan(&versionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrEnrollmentNotFound
	}
	if err != nil {
		return "", fmt.Errorf("resolving lesson video version: %w", err)
	}
	return versionID, nil
}

// ProgressForLesson reads one durable Progress row by enrollment and stable
// Lesson identity. The caller resolves the live revision separately.
func (r *Repository) ProgressForLesson(ctx context.Context, enrollmentID, courseLessonIdentityID string) (Progress, error) {
	if r == nil || r.pool == nil || enrollmentID == "" || courseLessonIdentityID == "" {
		return Progress{}, ErrProgressNotFound
	}
	var progress Progress
	r.observeQuery("learning.lesson-progress")
	err := r.pool.QueryRow(ctx, `
		SELECT max_position_seconds, last_position_seconds, completed_at IS NOT NULL
		FROM progress
		WHERE enrollment_id = $1::uuid AND course_lesson_identity_id = $2::uuid
	`, enrollmentID, courseLessonIdentityID).Scan(
		&progress.MaxPositionSeconds, &progress.LastPositionSeconds, &progress.Completed,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Progress{}, ErrProgressNotFound
	}
	if err != nil {
		return Progress{}, fmt.Errorf("reading learning progress: %w", err)
	}
	return progress, nil
}
