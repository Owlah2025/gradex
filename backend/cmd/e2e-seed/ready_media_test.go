//go:build !production

package main

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedReadyVideoForCourse creates only disposable media evidence for a course that the supplied
// owner already owns. Browser journeys create courses through the public API, so a static seeded
// Asset Version cannot be reused: the media ownership invariant intentionally rejects attaching an
// Asset Version from another Course. This helper keeps the E2E fixture on the same invariant as
// production while avoiding a large-byte upload for tests whose concern is authoring lifecycle.
func seedReadyVideoForCourse(ctx context.Context, pool *pgxpool.Pool, courseID, ownerID string) (string, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("beginning ready video fixture transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var actualOwnerID string
	if err := tx.QueryRow(ctx, `SELECT owner_account_id::text FROM courses WHERE id = $1::uuid FOR SHARE`, courseID).Scan(&actualOwnerID); err != nil {
		if err == pgx.ErrNoRows {
			return "", fmt.Errorf("course %s does not exist", courseID)
		}
		return "", fmt.Errorf("reading course owner: %w", err)
	}
	if actualOwnerID != ownerID {
		return "", fmt.Errorf("course %s is owned by %s, not %s", courseID, actualOwnerID, ownerID)
	}

	assetID := uuid.New().String()
	versionID := uuid.New().String()
	scanID := uuid.New().String()
	processingID := uuid.New().String()
	if err := seedReadyVideoEvidence(ctx, tx, ownerID, courseID, assetID, versionID, scanID, processingID); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("committing ready video fixture: %w", err)
	}
	return versionID, nil
}

func seedReadyVideoEvidence(ctx context.Context, tx pgx.Tx, ownerID, courseID, assetID, versionID, scanID, processingID string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO media_assets (id, kind, owner_account_id, course_id, visibility)
		VALUES ($1::uuid, 'VIDEO', $2::uuid, $3::uuid, 'PROTECTED')`, assetID, ownerID, courseID); err != nil {
		return fmt.Errorf("insert ready video asset: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes)
		VALUES ($1::uuid, $2::uuid, 'VIDEO', 'SCANNING', $3, 'v1', 'application/vnd.apple.mpegurl', 1048576)`,
		versionID, assetID, "e2e/"+versionID+"/master.m3u8"); err != nil {
		return fmt.Errorf("insert ready video version: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO scan_attempts (id, asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity)
		VALUES ($1::uuid, $2::uuid, 1, $3, 'v1', 'PASSED', 'e2e-fixture-scanner')`,
		scanID, versionID, "e2e-scan-"+versionID); err != nil {
		return fmt.Errorf("insert ready video scan evidence: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO processing_attempts (id, asset_version_id, operation_id, state, output_prefix, rendition_count, trusted_duration_ms)
		VALUES ($1::uuid, $2::uuid, $3, 'SUCCEEDED', $4, 1, 30000)`,
		processingID, versionID, "e2e-process-"+versionID, "e2e/"+versionID+"/renditions/"); err != nil {
		return fmt.Errorf("insert ready video processing evidence: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO video_renditions (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms)
		VALUES ($1::uuid, '720p', $2, 1280, 720, 2800, 30000)`,
		versionID, "e2e/"+versionID+"/renditions/720p.m3u8"); err != nil {
		return fmt.Errorf("insert ready video rendition: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE media_asset_versions
		SET state = 'SCAN_PASSED', successful_scan_attempt_id = $2::uuid
		WHERE id = $1::uuid`, versionID, scanID); err != nil {
		return fmt.Errorf("mark ready video scan passed: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE media_asset_versions SET state = 'PROCESSING' WHERE id = $1::uuid`, versionID); err != nil {
		return fmt.Errorf("mark ready video processing: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE media_asset_versions
		SET state = 'READY', successful_processing_attempt_id = $2::uuid, trusted_duration_ms = 30000
		WHERE id = $1::uuid`, versionID, processingID); err != nil {
		return fmt.Errorf("mark ready video ready: %w", err)
	}
	return nil
}
