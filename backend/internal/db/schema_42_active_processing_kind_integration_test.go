//go:build integration

package db

import (
	"context"
	"strings"
	"testing"
)

func TestSchema42PreservesHistoricalEvidenceAndDowngradesCleanly(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	pool := openPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	if err := m.Migrate(uint(EnhancementRecoveryFoundationSchemaVersion)); err != nil {
		t.Fatal(err)
	}
	fixture := seedSchema40MediaEvidence(t, ctx, pool, "schema42-history")
	if _, err := pool.Exec(ctx, `UPDATE media_asset_versions SET state='SCAN_ERROR' WHERE id=$1::uuid`, fixture.versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO processing_attempts (asset_version_id, operation_id, state, attempt_kind, rendition_count, error_reason)
		VALUES ($1::uuid, 'historical-enhancement', 'FAILED', 'ENHANCEMENT', 0, 'prior failure')
	`, fixture.versionID); err != nil {
		t.Fatal(err)
	}
	if err := m.Steps(1); err != nil {
		t.Fatalf("41 -> 42: %v", err)
	}
	state, err := ReadSchemaState(ctx, pool)
	if err != nil || state.Version != ActiveProcessingKindSchemaVersion || state.Dirty {
		t.Fatalf("schema after up=%+v err=%v", state, err)
	}
	var columnType string
	if err := pool.QueryRow(ctx, `
		SELECT udt_name FROM information_schema.columns
		WHERE table_name='media_asset_versions' AND column_name='active_processing_attempt_kind'
	`).Scan(&columnType); err != nil || columnType != "media_processing_attempt_kind" {
		t.Fatalf("active kind type=%q err=%v", columnType, err)
	}
	var activeKind *string
	if err := pool.QueryRow(ctx, `SELECT active_processing_attempt_kind::text FROM media_asset_versions WHERE id=$1::uuid`, fixture.versionID).Scan(&activeKind); err != nil || activeKind != nil {
		t.Fatalf("historical asset active kind=%v err=%v", activeKind, err)
	}
	if err := CheckEnhancementRecoveryRollbackSafety(ctx, pool); err == nil {
		t.Fatal("historical ENHANCEMENT did not close schema 40 rollback floor")
	}
	if err := m.Steps(-1); err != nil {
		t.Fatalf("42 -> 41 with terminal evidence: %v", err)
	}
	state, err = ReadSchemaState(ctx, pool)
	if err != nil || state.Version != EnhancementRecoveryFoundationSchemaVersion || state.Dirty {
		t.Fatalf("schema after down=%+v err=%v", state, err)
	}
	var historicalKind string
	if err := pool.QueryRow(ctx, `SELECT attempt_kind::text FROM processing_attempts WHERE asset_version_id=$1::uuid AND operation_id='historical-enhancement'`, fixture.versionID).Scan(&historicalKind); err != nil || historicalKind != "ENHANCEMENT" {
		t.Fatalf("historical attempt kind=%q err=%v", historicalKind, err)
	}
	if err := CheckEnhancementRecoveryRollbackSafety(ctx, pool); err == nil {
		t.Fatal("41 -> 40 rollback reopened after 42 -> 41")
	}
}

func TestSchema42RefusesUnquiescedMediaWork(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	pool := openPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	if err := m.Migrate(uint(EnhancementRecoveryFoundationSchemaVersion)); err != nil {
		t.Fatal(err)
	}
	fixture := seedSchema40MediaEvidence(t, ctx, pool, "schema42-active")
	if _, err := pool.Exec(ctx, `
		INSERT INTO scan_attempts (asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity, reason)
		VALUES ($1::uuid, 1, 'schema42-active-scan', 'v', 'PASSED', 'fixture', 'verified')
	`, fixture.versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE media_asset_versions SET state='SCAN_PASSED',
		  successful_scan_attempt_id=(SELECT id FROM scan_attempts WHERE asset_version_id=$1::uuid)
		WHERE id=$1::uuid
	`, fixture.versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE media_asset_versions SET state='PROCESSING',
		  processing_stage='TRANSCODING', processing_progress_percent=0,
		  processing_updated_at=now(), processing_attempt_token='active-full',
		  work_claim_token='active-full', work_claimed_at=now(),
		  work_lease_expires_at=now()+interval '1 hour'
		WHERE id=$1::uuid
	`, fixture.versionID); err != nil {
		t.Fatal(err)
	}
	if err := m.Steps(1); err == nil || !strings.Contains(err.Error(), "quiesce media producers") {
		t.Fatalf("unquiesced migration error=%v", err)
	}
	var hasColumn bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_name='media_asset_versions' AND column_name='active_processing_attempt_kind'
	)`).Scan(&hasColumn); err != nil || hasColumn {
		t.Fatalf("column after refused migration=%t err=%v", hasColumn, err)
	}
	var historicalKind string
	if err := pool.QueryRow(ctx, `SELECT attempt_kind::text FROM processing_attempts WHERE id=$1::uuid`, fixture.attemptID).Scan(&historicalKind); err != nil || historicalKind != "FULL" {
		t.Fatalf("history after refusal=%q err=%v", historicalKind, err)
	}
}
