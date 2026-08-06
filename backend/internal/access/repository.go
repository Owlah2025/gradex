package access

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("pgxpool is required")
	}
	return &Repository{pool: pool}, nil
}

type SetCourseDefaultAccessExpiryParams struct {
	CourseID            string
	AdminAccountID      string
	ActorDescriptor     string
	DefaultAccessEndsAt time.Time
	Reason              string
}

func (r *Repository) SetCourseDefaultAccessExpiry(ctx context.Context, params SetCourseDefaultAccessExpiryParams) error {
	if r == nil || r.pool == nil {
		return errors.New("repository is not initialized")
	}
	if strings.TrimSpace(params.CourseID) == "" {
		return ErrCourseNotFound
	}
	if params.DefaultAccessEndsAt.IsZero() {
		return ErrExpiryRequired
	}
	if strings.TrimSpace(params.Reason) == "" {
		return ErrReasonRequired
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var existingCourseID string
	err = tx.QueryRow(ctx, "SELECT id::text FROM courses WHERE id = $1 FOR UPDATE", params.CourseID).Scan(&existingCourseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCourseNotFound
	}
	if err != nil {
		return fmt.Errorf("locking course: %w", err)
	}

	tag, err := tx.Exec(ctx, "UPDATE courses SET default_access_ends_at = $1 WHERE id = $2", params.DefaultAccessEndsAt, params.CourseID)
	if err != nil {
		return fmt.Errorf("updating course default_access_ends_at: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrCourseNotFound
	}

	metadata, err := json.Marshal(map[string]any{
		"default_access_ends_at": params.DefaultAccessEndsAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("marshaling audit metadata: %w", err)
	}

	actorID := params.AdminAccountID
	if actorID == "" {
		actorID = params.ActorDescriptor
	}

	auditQuery := `
		INSERT INTO audit_events (
			actor_account_id, actor_role, actor_descriptor,
			action, module, target_type, target_id,
			reason, metadata
		) VALUES (
			$1, 'ADMIN', $2,
			'COURSE_DEFAULT_ACCESS_EXPIRY_SET', 'IDENTITY_AND_ACCESS', 'COURSE', $3,
			$4, $5
		)
	`
	_, err = tx.Exec(ctx, auditQuery,
		params.AdminAccountID, params.ActorDescriptor, params.CourseID,
		params.Reason, metadata,
	)
	if err != nil {
		return fmt.Errorf("writing audit event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}
