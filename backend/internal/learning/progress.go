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
	return saveProgress(ctx, r.pool, write)
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
