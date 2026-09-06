package media

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type ThumbnailCleanupStore interface {
	DeleteThumbnailObjects(context.Context, string, string) error
}

// CleanupThumbnails retains all revision references, including superseded and
// rejected history. Abandoned drafts remain authored data; only assets no longer
// referenced by any revision are eligible after seven days.
func CleanupThumbnails(ctx context.Context, pool *pgxpool.Pool, store ThumbnailCleanupStore, now time.Time) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT ma.id::text,v.id::text FROM media_assets ma JOIN media_asset_versions v ON v.logical_asset_id=ma.id
 WHERE ma.kind='THUMBNAIL' AND ma.retired_at IS NULL AND v.created_at<$1
 AND NOT EXISTS(SELECT 1 FROM course_revisions r WHERE r.thumbnail_asset_version_id=v.id)
 ORDER BY ma.id LIMIT 100 FOR UPDATE OF ma SKIP LOCKED`, now.Add(-7*24*time.Hour))
	if err != nil {
		return err
	}
	type candidate struct{ asset, version string }
	candidates, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (candidate, error) {
		var c candidate
		err := row.Scan(&c.asset, &c.version)
		return c, err
	})
	if err != nil {
		return err
	}
	for _, c := range candidates {
		// Recheck on a fresh statement snapshot after acquiring the same asset lock
		// used by attachment. A concurrent attach must either win or be refused.
		tag, err := tx.Exec(ctx, `UPDATE media_assets SET retired_at=now() WHERE id=$1::uuid
   AND NOT EXISTS(SELECT 1 FROM course_revisions WHERE thumbnail_asset_version_id=$2::uuid)`, c.asset, c.version)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		if _, err = tx.Exec(ctx, `INSERT INTO media_thumbnail_cleanup(asset_version_id) VALUES($1::uuid) ON CONFLICT DO NOTHING`, c.version); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	rows, err = pool.Query(ctx, `SELECT c.asset_version_id::text,ma.course_id::text FROM media_thumbnail_cleanup c
 JOIN media_asset_versions v ON v.id=c.asset_version_id JOIN media_assets ma ON ma.id=v.logical_asset_id
 WHERE c.deleted_at IS NULL ORDER BY c.claimed_at LIMIT 100`)
	if err != nil {
		return err
	}
	type deletion struct{ version, course string }
	deletions, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (deletion, error) {
		var d deletion
		err := row.Scan(&d.version, &d.course)
		return d, err
	})
	if err != nil {
		return err
	}
	for _, d := range deletions {
		if err = store.DeleteThumbnailObjects(ctx, d.course, d.version); err != nil {
			return fmt.Errorf("thumbnail cleanup storage: %w", err)
		}
		if _, err = pool.Exec(ctx, `UPDATE media_thumbnail_cleanup SET deleted_at=now() WHERE asset_version_id=$1::uuid`, d.version); err != nil {
			return err
		}
	}
	return nil
}
