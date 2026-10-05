package media

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func cleanupMultipartSession(ctx context.Context, db *pgxpool.Pool, store multipartStore, id string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var key, uploadID, status string
	var completed bool
	err = tx.QueryRow(ctx, `SELECT expected_object_key, COALESCE(provider_upload_id,''), multipart_status, completed_at IS NOT NULL FROM upload_intents WHERE asset_version_id=$1::uuid AND is_multipart FOR UPDATE`, id).Scan(&key, &uploadID, &status, &completed)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status == "ABORTED" {
		return nil
	}
	if completed {
		return ErrConflict
	}
	if uploadID != "" {
		if err := store.AbortMultipartUpload(ctx, key, uploadID); err != nil {
			return fmt.Errorf("%w: aborting multipart: %v", ErrUnavailable, err)
		}
	} else if err := store.AbortMultipartKey(ctx, key); err != nil {
		return fmt.Errorf("%w: reconciling unbound multipart: %v", ErrUnavailable, err)
	}
	version, err := store.MultipartObjectIdentity(ctx, key)
	if err != nil {
		return fmt.Errorf("%w: inspecting abandoned object: %v", ErrUnavailable, err)
	}
	if version != "" {
		if err := store.DeleteMultipartObject(ctx, key, version); err != nil {
			return fmt.Errorf("%w: deleting abandoned object: %v", ErrUnavailable, err)
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE upload_intents SET multipart_status='ABORTED' WHERE asset_version_id=$1::uuid`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func CleanupAbandonedMultipartUploads(ctx context.Context, db *pgxpool.Pool, objects ObjectStore, limit int) (int, error) {
	store, ok := objects.(multipartStore)
	if !ok {
		return 0, ErrUnavailable
	}
	if limit <= 0 || limit > 100 {
		return 0, ErrValidation
	}
	rows, err := db.Query(ctx, `SELECT asset_version_id::text FROM upload_intents WHERE is_multipart AND completed_at IS NULL AND multipart_status <> 'ABORTED' AND (expires_at <= now() OR multipart_status='ABORTING') ORDER BY expires_at,id LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	cleaned := 0
	var failures []error
	for _, id := range ids {
		// Committing the refusal first prevents completions from racing a failed provider cleanup.
		tag, err := db.Exec(ctx, `UPDATE upload_intents SET multipart_status='ABORTING' WHERE asset_version_id=$1::uuid AND completed_at IS NULL AND multipart_status <> 'ABORTED' AND (expires_at <= now() OR multipart_status='ABORTING')`, id)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		if err := cleanupMultipartSession(ctx, db, store, id); err != nil {
			failures = append(failures, fmt.Errorf("upload %s: %w", id, err))
			continue
		}
		cleaned++
	}
	return cleaned, errors.Join(failures...)
}
