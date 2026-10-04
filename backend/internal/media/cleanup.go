package media

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryRower captures the QueryRow method shared by *pgxpool.Pool, pgx.Tx,
// and unit test doubles.
type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// HasRenditionReferences reports whether any durable video_renditions row
// references storage_object_key beneath or equal to prefix.
// It is conservative and fails safe: any validation or query error is returned
// with false so callers retain objects when safety cannot be conclusively proven.
func HasRenditionReferences(ctx context.Context, q queryRower, assetVersionID, prefix string) (bool, error) {
	if q == nil {
		return false, errors.New("database queryer is required to check rendition references")
	}
	cleanPrefix := strings.TrimRight(strings.TrimSpace(prefix), "/")
	if cleanPrefix == "" || cleanPrefix == "media" {
		return false, fmt.Errorf("%w: attempt prefix %q is invalid or too broad", ErrValidation, prefix)
	}
	if strings.Contains(cleanPrefix, "..") {
		return false, fmt.Errorf("%w: attempt prefix %q contains path traversal", ErrValidation, prefix)
	}

	prefixWithSlash := cleanPrefix + "/"

	cleanAssetID := strings.TrimSpace(assetVersionID)
	if cleanAssetID != "" {
		if _, err := uuid.Parse(cleanAssetID); err != nil {
			return false, fmt.Errorf("%w: invalid asset version UUID %q", ErrValidation, assetVersionID)
		}
		var referenced bool
		err := q.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM video_renditions
				WHERE asset_version_id = $1::uuid
				  AND (storage_object_key = $2 OR starts_with(storage_object_key, $3))
			)
		`, cleanAssetID, cleanPrefix, prefixWithSlash).Scan(&referenced)
		if err != nil {
			return false, fmt.Errorf("checking rendition references for version %s prefix %q: %w", cleanAssetID, cleanPrefix, err)
		}
		return referenced, nil
	}

	var referenced bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM video_renditions
			WHERE storage_object_key = $1 OR starts_with(storage_object_key, $2)
		)
	`, cleanPrefix, prefixWithSlash).Scan(&referenced)
	if err != nil {
		return false, fmt.Errorf("checking rendition references for prefix %q: %w", cleanPrefix, err)
	}
	return referenced, nil
}

// HasAttemptRenditionReferences reports whether any durable video_renditions row
// references storage_object_key beneath or equal to the attempt's canonical prefix.
func HasAttemptRenditionReferences(ctx context.Context, q queryRower, assetVersionID, operationID string) (bool, error) {
	if strings.TrimSpace(operationID) == "" {
		return false, fmt.Errorf("%w: operation ID is required", ErrValidation)
	}
	prefix := ProcessingOutputPrefix(assetVersionID, operationID)
	return HasRenditionReferences(ctx, q, assetVersionID, prefix)
}

// CleanupAttemptSafe evaluates whether an attempt's prefix is safe to delete
// by checking PostgreSQL for durable video_renditions references.
// If references exist or safety cannot be proven (e.g. DB error), deletion is skipped.
// If proven unreferenced, the registered cleaner removes the prefix.
func (w *Worker) CleanupAttemptSafe(ctx context.Context, assetVersionID, operationID string) (bool, error) {
	return cleanupAttemptSafeWithDB(ctx, w.db, w.process, assetVersionID, operationID)
}

func cleanupAttemptSafeWithDB(ctx context.Context, q queryRower, process any, assetVersionID, operationID string) (bool, error) {
	cleaner, ok := process.(interface {
		CleanupAttempt(context.Context, string, string) error
	})
	if !ok {
		return false, nil
	}
	if strings.TrimSpace(assetVersionID) == "" || strings.TrimSpace(operationID) == "" {
		return false, fmt.Errorf("%w: asset version ID and operation ID are required for cleanup", ErrValidation)
	}

	checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	referenced, err := HasAttemptRenditionReferences(checkCtx, q, assetVersionID, operationID)
	if err != nil {
		return false, fmt.Errorf("evaluating cleanup safety for attempt %s: %w", operationID, err)
	}
	if referenced {
		// Durable renditions reference this attempt prefix: deletion is refused.
		return false, nil
	}

	cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cleanupCancel()
	if err := cleaner.CleanupAttempt(cleanupCtx, assetVersionID, operationID); err != nil {
		return false, fmt.Errorf("cleaning attempt %s: %w", operationID, err)
	}
	return true, nil
}
func CleanupAbandonedMultipartUploads(ctx context.Context, db *pgxpool.Pool, store ObjectStore, limit int) (int, error) {
	rows, err := db.Query(ctx, `
		SELECT id, expected_object_key, provider_upload_id
		FROM upload_intents
		WHERE is_multipart = true AND completed_at IS NULL AND expires_at < now()
		LIMIT $1
	`, limit)
	if err != nil {
		return 0, fmt.Errorf("querying abandoned multipart uploads: %w", err)
	}
	defer rows.Close()

	type intent struct {
		id       string
		key      string
		uploadID string
	}
	var intents []intent
	for rows.Next() {
		var i intent
		if err := rows.Scan(&i.id, &i.key, &i.uploadID); err != nil {
			return 0, fmt.Errorf("scanning abandoned multipart upload: %w", err)
		}
		intents = append(intents, i)
	}
	rows.Close()

	cleaned := 0
	for _, i := range intents {
		if i.uploadID != "" {
			_ = store.AbortMultipartUpload(ctx, i.key, i.uploadID)
		}
		// Mark it as failed so it drops out of the index
		_, err := db.Exec(ctx, `
			UPDATE upload_intents SET completed_at = now(), completion_fingerprint = 'ABORTED' WHERE id = $1::uuid
		`, i.id)
		if err != nil {
			return cleaned, fmt.Errorf("marking multipart upload as aborted: %w", err)
		}
		cleaned++
	}
	return cleaned, nil
}
