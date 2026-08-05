// Package e2equery provides read-only PostgreSQL snapshots for browser end-to-end
// evidence.
//
// Every query resolves through the authoritative schema — the `progress` table keyed
// by Enrollment and stable Course Lesson Identity — rather than through convenience
// columns that do not exist. A helper that silently reports "no row" for a schema it
// cannot address lets an assertion pass without proving persistence, so absence is
// always reported explicitly and never as a zero value.
//
// This package duplicates no authorization logic. It reads state; it decides nothing.
package e2equery

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProgressSnapshot is the durable Progress state for one (Student, Course, stable Lesson).
// Found is always meaningful: false means no row exists, which is distinct from a row at
// position zero.
type ProgressSnapshot struct {
	Found bool `json:"found"`
	// MaxPositionSeconds is the monotonic maximum — the value the upsert advances with GREATEST.
	MaxPositionSeconds float64 `json:"max_position_seconds"`
	// PositionSeconds is `last_position_seconds`, the resume point, which is deliberately
	// not monotonic.
	PositionSeconds float64 `json:"position_seconds"`
	Completed       bool    `json:"completed"`
	CompletedAt     string  `json:"completed_at"`
	// AssetVersionID is `completing_asset_version_id`, present only once completion is written.
	AssetVersionID string `json:"asset_version_id"`
	UpdatedAt      string `json:"updated_at"`
}

// ReadProgress resolves the Student's Enrollment for the Course, the stable Course Lesson
// Identity, and the matching Progress row.
//
// The Course is required on both sides of the join: an Enrollment belongs to a Course and a
// Lesson Identity belongs to a Course, so a Lesson Identity from a different Course cannot
// match even when the Student is enrolled in both. That makes Student, Course, and Lesson
// isolation structural rather than something the caller must remember to check.
func ReadProgress(ctx context.Context, pool *pgxpool.Pool, studentAccountID, courseID, lessonIdentityID string) (ProgressSnapshot, error) {
	var snapshot ProgressSnapshot
	var completedAt *time.Time
	var assetVersionID *string
	var updatedAt time.Time

	err := pool.QueryRow(ctx, `
		SELECT
			progress.max_position_seconds::float8,
			progress.last_position_seconds::float8,
			progress.completed_at,
			progress.completing_asset_version_id,
			progress.updated_at
		FROM progress
		JOIN enrollments
			ON enrollments.id = progress.enrollment_id
		JOIN course_lesson_identities
			ON course_lesson_identities.id = progress.course_lesson_identity_id
		WHERE enrollments.student_account_id = $1
		  AND enrollments.course_id = $2
		  AND course_lesson_identities.id = $3
		  AND course_lesson_identities.course_id = $2
	`, studentAccountID, courseID, lessonIdentityID).Scan(
		&snapshot.MaxPositionSeconds,
		&snapshot.PositionSeconds,
		&completedAt,
		&assetVersionID,
		&updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProgressSnapshot{Found: false}, nil
	}
	if err != nil {
		return ProgressSnapshot{}, fmt.Errorf("reading progress snapshot: %w", err)
	}

	snapshot.Found = true
	snapshot.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	if completedAt != nil {
		snapshot.Completed = true
		snapshot.CompletedAt = completedAt.UTC().Format(time.RFC3339Nano)
	}
	if assetVersionID != nil {
		snapshot.AssetVersionID = *assetVersionID
	}
	return snapshot, nil
}
