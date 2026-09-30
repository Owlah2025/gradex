package learning

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrProgressUnavailable = errors.New("learning progress is unavailable")

// ProgressWrite is deliberately limited to server-derived facts. Callers pass
// a bounded position and the exact played version; no client percentage,
// duration, or timestamp crosses this boundary.
type ProgressWrite struct {
	EnrollmentID             string
	CourseLessonIdentityID   string
	PositionSeconds          float64
	CompletingAssetVersionID string
	Completed                bool
}

func BoundPosition(position, duration float64) float64 {
	if position < 0 {
		return 0
	}
	if position > duration {
		return duration
	}
	return position
}

// SaveProgress is one atomic PostgreSQL upsert. GREATEST preserves the
// monotonic maximum under concurrent writers while COALESCE makes completion
// and its exact asset version write-once.
func (r *Repository) SaveProgress(ctx context.Context, write ProgressWrite) error {
	if r == nil || r.pool == nil || write.EnrollmentID == "" || write.CourseLessonIdentityID == "" ||
		write.PositionSeconds < 0 || math.IsNaN(write.PositionSeconds) || math.IsInf(write.PositionSeconds, 0) ||
		(write.Completed && write.CompletingAssetVersionID == "") {
		return ErrProgressUnavailable
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("beginning learning progress transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := saveProgress(ctx, tx, write); err != nil {
		return err
	}
	if err := recordCourseCompletion(ctx, tx, write.EnrollmentID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing learning progress transaction: %w", err)
	}
	return nil
}

// ProgressMutationGuard runs inside the transaction immediately before the
// atomic upsert. HTTP composition supplies the authoritative access decision;
// learning remains independent of the entitlement model and performs no
// authorization writes.
type ProgressMutationGuard func(context.Context, pgx.Tx) error

// SaveProgressGuarded couples final authorization and the stable-key upsert in
// one PostgreSQL transaction. Read committed preserves the upsert's normal
// concurrent-writer convergence; the evaluator's authority-row locks prevent
// a committed access-state change from landing between the final decision and
// mutation.
func (r *Repository) SaveProgressGuarded(ctx context.Context, write ProgressWrite, guard ProgressMutationGuard) error {
	if r == nil || r.pool == nil || guard == nil || write.EnrollmentID == "" || write.CourseLessonIdentityID == "" ||
		write.PositionSeconds < 0 || math.IsNaN(write.PositionSeconds) || math.IsInf(write.PositionSeconds, 0) ||
		(write.Completed && write.CompletingAssetVersionID == "") {
		return ErrProgressUnavailable
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("beginning guarded learning progress transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := guard(ctx, tx); err != nil {
		return err
	}
	if err := saveProgress(ctx, tx, write); err != nil {
		return err
	}
	if err := recordCourseCompletion(ctx, tx, write.EnrollmentID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing guarded learning progress transaction: %w", err)
	}
	return nil
}

type progressExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func saveProgress(ctx context.Context, executor progressExecutor, write ProgressWrite) error {
	var completedAt *time.Time
	var versionID *string
	if write.Completed {
		now := time.Now().UTC()
		completedAt = &now
		versionID = &write.CompletingAssetVersionID
	}
	_, err := executor.Exec(ctx, `
		INSERT INTO progress (
			enrollment_id, course_lesson_identity_id, max_position_seconds, last_position_seconds,
			completed_at, completing_asset_version_id, last_watched_at, updated_at
		) VALUES ($1::uuid, $2::uuid, $3, $3, $4, $5::uuid, now(), now())
		ON CONFLICT (enrollment_id, course_lesson_identity_id) DO UPDATE SET
			max_position_seconds = GREATEST(progress.max_position_seconds, EXCLUDED.max_position_seconds),
			last_position_seconds = EXCLUDED.last_position_seconds,
			completed_at = COALESCE(progress.completed_at, EXCLUDED.completed_at),
			completing_asset_version_id = COALESCE(progress.completing_asset_version_id, EXCLUDED.completing_asset_version_id),
			last_watched_at = EXCLUDED.last_watched_at,
			updated_at = now()
	`, write.EnrollmentID, write.CourseLessonIdentityID, write.PositionSeconds, completedAt, versionID)
	if err != nil {
		return fmt.Errorf("saving learning progress: %w", err)
	}
	return nil
}

// recordCourseCompletion locks the Enrollment before checking the live graph.
// Two concurrent final Lesson writes therefore serialize: the writer that sees
// the other writer's committed Progress row records the one durable fact, and
// the unique enrollment constraint makes retries harmless.
func recordCourseCompletion(ctx context.Context, tx pgx.Tx, enrollmentID string) error {
	var lockedEnrollmentID string
	if err := tx.QueryRow(ctx, `
		SELECT id::text
		FROM enrollments
		WHERE id = $1::uuid
		FOR UPDATE
	`, enrollmentID).Scan(&lockedEnrollmentID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("locking learning enrollment: %w", ErrProgressUnavailable)
		}
		return fmt.Errorf("locking learning enrollment: %w", err)
	}

	_, err := tx.Exec(ctx, `
		WITH current_course_lessons AS (
			SELECT e.id AS enrollment_id,
			       e.student_account_id,
			       e.course_id,
			       cr.id AS course_revision_id,
			       cr.revision_number,
			       cli.id AS lesson_identity_id
			FROM enrollments e
			JOIN courses c ON c.id = e.course_id
			JOIN course_revisions cr
			  ON cr.id = c.live_revision_id
			 AND cr.course_id = c.id
			 AND cr.state = 'APPROVED'
			JOIN course_sections cs
			  ON cs.revision_id = cr.id
			 AND cs.course_id = c.id
			JOIN course_lessons cl
			  ON cl.section_id = cs.id
			 AND cl.course_id = c.id
			JOIN course_lesson_identities cli
			  ON cli.id = cl.lesson_identity_id
			 AND cli.course_id = c.id
			 AND cli.section_identity_id = cl.section_identity_id
			WHERE e.id = $1::uuid
		), completion_candidate AS (
			SELECT current_course_lessons.enrollment_id,
			       current_course_lessons.student_account_id,
			       current_course_lessons.course_id,
			       current_course_lessons.course_revision_id,
			       current_course_lessons.revision_number,
			       count(DISTINCT current_course_lessons.lesson_identity_id)::INTEGER AS required_lesson_count,
			       count(DISTINCT progress.course_lesson_identity_id)
			           FILTER (WHERE progress.completed_at IS NOT NULL)::INTEGER AS completed_lesson_count,
			       max(progress.completed_at) AS completed_at
			FROM current_course_lessons
			LEFT JOIN progress
			  ON progress.enrollment_id = current_course_lessons.enrollment_id
			 AND progress.course_lesson_identity_id = current_course_lessons.lesson_identity_id
			GROUP BY current_course_lessons.enrollment_id,
			         current_course_lessons.student_account_id,
			         current_course_lessons.course_id,
			         current_course_lessons.course_revision_id,
			         current_course_lessons.revision_number
		)
		INSERT INTO course_completions (
			enrollment_id,
			student_account_id,
			course_id,
			completed_at,
			course_revision_id,
			course_revision_number,
			required_lesson_count,
			completed_lesson_count,
			source
		)
		SELECT enrollment_id,
		       student_account_id,
		       course_id,
		       completed_at,
		       course_revision_id,
		       revision_number,
		       required_lesson_count,
		       completed_lesson_count,
		       'PROGRESS'::course_completion_source
		FROM completion_candidate
		WHERE required_lesson_count > 0
		  AND completed_lesson_count = required_lesson_count
		  AND completed_at IS NOT NULL
		ON CONFLICT (enrollment_id) DO NOTHING
	`, lockedEnrollmentID)
	if err != nil {
		return fmt.Errorf("recording course completion: %w", err)
	}
	return nil
}
