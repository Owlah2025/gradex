package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Run before golang-migrate marks the schema dirty. Thumbnail history is never
// destroyed as a side effect of a development rollback command.
func CheckThumbnailRollbackSafety(ctx context.Context, pool *pgxpool.Pool) error {
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM media_assets WHERE kind='THUMBNAIL')`).Scan(&exists); err != nil {
		return fmt.Errorf("checking thumbnail rollback safety: %w", err)
	}
	if exists {
		return errors.New("thumbnail assets exist; retain schema 33 and use an application build compatible with that schema")
	}
	return nil
}
