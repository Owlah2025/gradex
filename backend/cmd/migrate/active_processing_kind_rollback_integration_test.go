//go:build integration

package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedActiveProcessingKind(t *testing.T, ctx context.Context, pool *pgxpool.Pool, kind string) string {
	t.Helper()
	versionID := seedMediaVersion(t, ctx, pool)
	if _, err := pool.Exec(ctx, `
		INSERT INTO scan_attempts (asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity, reason)
		VALUES ($1::uuid, 1, 'schema42-scan', 'v', 'PASSED', 'fixture', 'verified')
	`, versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE media_asset_versions SET state='SCAN_PASSED',
		  successful_scan_attempt_id=(SELECT id FROM scan_attempts WHERE asset_version_id=$1::uuid)
		WHERE id=$1::uuid
	`, versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE media_asset_versions SET state='PROCESSING',
		  processing_stage='TRANSCODING', processing_progress_percent=0,
		  processing_updated_at=now(), processing_attempt_token='full-op',
		  work_claim_token='full-op', work_claimed_at=now(),
		  work_lease_expires_at=now()+interval '1 hour', active_processing_attempt_kind='FULL'
		WHERE id=$1::uuid
	`, versionID); err != nil {
		t.Fatal(err)
	}
	if kind == "FULL" {
		return versionID
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO video_renditions (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms, processing_operation_id)
		VALUES ($1::uuid, '1080p', 'media/schema42/full/1080p/playlist.m3u8', 1920, 1080, 5000, 90000, 'full-op')
	`, versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE media_asset_versions SET state='PLAYABLE', work_claim_token=NULL,
		  work_claimed_at=NULL, work_lease_expires_at=NULL, active_processing_attempt_kind=NULL
		WHERE id=$1::uuid
	`, versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE media_asset_versions SET processing_attempt_token='enh-op',
		  work_claim_token='enh-op', work_claimed_at=now(),
		  work_lease_expires_at=now()+interval '1 hour', active_processing_attempt_kind='ENHANCEMENT'
		WHERE id=$1::uuid
	`, versionID); err != nil {
		t.Fatal(err)
	}
	return versionID
}

func TestSchema42DownRefusesActiveProcessingBeforeDirtyMarker(t *testing.T) {
	for _, kind := range []string{"FULL", "ENHANCEMENT"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			defer cancel()
			m, cfg, pool := migrateCommandHarness(t, ctx)
			stageSchema42(t, m)
			versionID := seedActiveProcessingKind(t, ctx, pool, kind)
			if _, err := pool.Exec(ctx, `UPDATE media_asset_versions SET active_processing_attempt_kind=NULL WHERE id=$1::uuid`, versionID); err == nil {
				t.Fatal("schema 42 accepted an untyped active processing claim")
			}
			if _, err := pool.Exec(ctx, `UPDATE media_asset_versions SET processing_attempt_token='wrong-op' WHERE id=$1::uuid`, versionID); err == nil {
				t.Fatal("schema 42 accepted a processing token different from its claim")
			}
			if kind == "FULL" {
				if _, err := pool.Exec(ctx, `UPDATE media_asset_versions SET active_processing_attempt_kind='FINALIZATION' WHERE id=$1::uuid`, versionID); err == nil {
					t.Fatal("schema 42 accepted FINALIZATION in PROCESSING")
				}
			}
			if err := down(m, cfg, []string{"1"}); err == nil || !strings.Contains(err.Error(), "active processing operation") {
				t.Fatalf("42 -> 41 with active %s error=%v", kind, err)
			}
			version, dirty, err := m.Version()
			if err != nil || version != uint(db.ActiveProcessingKindSchemaVersion) || dirty {
				t.Fatalf("refused rollback marker version=%d dirty=%t err=%v", version, dirty, err)
			}
			var activeKind string
			if err := pool.QueryRow(ctx, `SELECT active_processing_attempt_kind::text FROM media_asset_versions WHERE id=$1::uuid`, versionID).Scan(&activeKind); err != nil || activeKind != kind {
				t.Fatalf("active kind after refusal=%q err=%v", activeKind, err)
			}
		})
	}
}

func TestSchema42DownAllowsTerminalEnhancementHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	m, cfg, pool := migrateCommandHarness(t, ctx)
	stageSchema42(t, m)
	versionID := seedMediaVersion(t, ctx, pool)
	if _, err := pool.Exec(ctx, `UPDATE media_asset_versions SET state='SCAN_ERROR' WHERE id=$1::uuid`, versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO processing_attempts (asset_version_id, operation_id, state, attempt_kind, rendition_count, error_reason)
		VALUES ($1::uuid, 'enh-terminal', 'FAILED', 'ENHANCEMENT', 0, 'prior enhancement failed')
	`, versionID); err != nil {
		t.Fatal(err)
	}
	if err := down(m, cfg, []string{"1"}); err != nil {
		t.Fatalf("42 -> 41 with terminal history: %v", err)
	}
	version, dirty, err := m.Version()
	if err != nil || version != uint(db.EnhancementRecoveryFoundationSchemaVersion) || dirty {
		t.Fatalf("schema after down version=%d dirty=%t err=%v", version, dirty, err)
	}
	if err := db.CheckEnhancementRecoveryRollbackSafety(ctx, pool); err == nil {
		t.Fatal("41 -> 40 rollback reopened despite historical ENHANCEMENT")
	}
}
