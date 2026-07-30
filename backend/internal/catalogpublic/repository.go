package catalogpublic

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrRepositoryNil = errors.New("database pool is required")
	ErrVisibilityNil = errors.New("published-only visibility predicate is required")
)

// Repository is the public catalogue read boundary. Its later list and detail
// consumers must obtain rows through visibility; construction refuses a
// missing policy so a query cannot silently fall back to unfiltered data.
type Repository struct {
	pool       *pgxpool.Pool
	visibility VisibilityPredicate
}

func NewRepository(pool *pgxpool.Pool, visibility VisibilityPredicate) (*Repository, error) {
	if pool == nil {
		return nil, ErrRepositoryNil
	}
	if visibility == nil {
		return nil, ErrVisibilityNil
	}
	return &Repository{pool: pool, visibility: visibility}, nil
}

// List validates the public visibility boundary for the list route. T010 owns
// the list projection, so this foundation deliberately returns no catalogue
// fields yet.
func (r *Repository) List(ctx context.Context) error {
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS (`+r.visibleCourseQuery()+`)`).Scan(&exists); err != nil {
		return fmt.Errorf("checking public catalogue visibility: %w", err)
	}
	return nil
}

// Detail reports whether an exact Course identifier is public. The identifier
// and every visibility exclusion share one SQL boundary, so callers cannot
// fetch a Course and make a separate visibility decision in application code.
func (r *Repository) Detail(ctx context.Context, identifier string) (bool, error) {
	var visible bool
	query := `SELECT EXISTS (` + r.visibleCourseQuery() + ` AND c.id::text = $1)`
	if err := r.pool.QueryRow(ctx, query, identifier).Scan(&visible); err != nil {
		return false, fmt.Errorf("checking public course visibility: %w", err)
	}
	return visible, nil
}

func (r *Repository) visibleCourseQuery() string {
	return `SELECT 1
		FROM courses c
		JOIN course_revisions cr ON cr.course_id = c.id
		WHERE ` + r.visibility("c", "cr")
}
