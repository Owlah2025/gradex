package catalog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

var (
	ErrRepositoryNil     = errors.New("database pool is required")
	ErrCourseNotFound    = errors.New("course not found")
	ErrLifecycleConflict = errors.New("course lifecycle conflict")
)

type LifecycleConflictError struct {
	CourseID string
	Actual   string
	Expected []string
}

func (e *LifecycleConflictError) Error() string {
	return fmt.Sprintf("course %s has lifecycle %q, expected one of %v", e.CourseID, e.Actual, e.Expected)
}

type CourseRow struct {
	ID                     string
	OwnerAccountID         string
	Lifecycle              string
	LiveRevisionID         *string
	ClassificationModel    string
	InstitutionID          *string
	SubjectID              *string
	AccessSuspendedAt      *time.Time
	AccessSuspensionReason *string
	RetiredAt              *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type Repository struct {
	pool         *pgxpool.Pool
	outboxWriter *outbox.Writer
}

// NewRepository constructs a new Repository.
// Standing clause: both pool and outbox writer are required at construction.
// A component refuses to build without a security-relevant dependency;
// no mutation-after-construction path may restore it.
func NewRepository(pool *pgxpool.Pool, writer *outbox.Writer) (*Repository, error) {
	if pool == nil {
		return nil, ErrRepositoryNil
	}
	if writer == nil {
		return nil, errors.New("outbox writer is required")
	}
	return &Repository{pool: pool, outboxWriter: writer}, nil
}

func (r *Repository) Pool() *pgxpool.Pool {
	return r.pool
}

// IsCourseOwner checks if accountID is the owner of courseID.
func (r *Repository) IsCourseOwner(ctx context.Context, courseID string, accountID string) (bool, error) {
	var isOwner bool
	query := `SELECT EXISTS (SELECT 1 FROM courses WHERE id = $1 AND owner_account_id = $2)`
	err := r.pool.QueryRow(ctx, query, courseID, accountID).Scan(&isOwner)
	if err != nil {
		return false, fmt.Errorf("checking course ownership: %w", err)
	}
	return isOwner, nil
}

// ExecTx executes a function within a database transaction.
func (r *Repository) ExecTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	return nil
}

// LockCourse takes SELECT ... FOR UPDATE on the course row inside tx,
// and re-asserts that the course lifecycle matches expectedLifecycles (if specified).
// If state does not match, returns a LifecycleConflictError (409 conflict).
func (r *Repository) LockCourse(
	ctx context.Context,
	tx pgx.Tx,
	courseID string,
	expectedLifecycles ...string,
) (*CourseRow, error) {
	if tx == nil {
		return nil, errors.New("transaction is required for LockCourse")
	}
	if courseID == "" {
		return nil, errors.New("courseID is required")
	}

	query := `
		SELECT id, owner_account_id, lifecycle, live_revision_id,
		       classification_model, institution_id, subject_id,
		       access_suspended_at, access_suspension_reason, retired_at,
		       created_at, updated_at
		FROM courses
		WHERE id = $1
		FOR UPDATE
	`

	var row CourseRow
	err := tx.QueryRow(ctx, query, courseID).Scan(
		&row.ID, &row.OwnerAccountID, &row.Lifecycle, &row.LiveRevisionID,
		&row.ClassificationModel, &row.InstitutionID, &row.SubjectID,
		&row.AccessSuspendedAt, &row.AccessSuspensionReason, &row.RetiredAt,
		&row.CreatedAt, &row.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCourseNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("locking course %s: %w", courseID, err)
	}

	// Re-assert expected state INSIDE transaction after taking lock.
	if len(expectedLifecycles) > 0 {
		matched := false
		for _, exp := range expectedLifecycles {
			if row.Lifecycle == exp {
				matched = true
				break
			}
		}
		if !matched {
			return nil, &LifecycleConflictError{
				CourseID: courseID,
				Actual:   row.Lifecycle,
				Expected: expectedLifecycles,
			}
		}
	}

	return &row, nil
}
