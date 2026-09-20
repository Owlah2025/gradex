//go:build integration

package db

import (
	"context"
	"testing"
)

func TestSchema40PlayableFoundationCompatibility(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	pool := openPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	// 1. Migrate up to schema 39
	if err := m.Migrate(uint(MediaPlayableEnumSchemaVersion)); err != nil {
		t.Fatalf("migrating to schema 39: %v", err)
	}

	state, err := ReadSchemaState(ctx, pool)
	if err != nil {
		t.Fatalf("reading schema state at 39: %v", err)
	}
	if state.Version != MediaPlayableEnumSchemaVersion || state.Dirty {
		t.Fatalf("schema at 39 = %+v", state)
	}

	// 2. Migrate up to schema 40
	if err := m.Steps(1); err != nil {
		t.Fatalf("applying migration 0040: %v", err)
	}

	state, err = ReadSchemaState(ctx, pool)
	if err != nil {
		t.Fatalf("reading schema state after 0040: %v", err)
	}
	if state.Version != MediaPlayableFoundationSchemaVersion || state.Dirty {
		t.Fatalf("schema after 0040 = %+v", state)
	}

	// 3. Test Constraints and Triggers
	// Create required fixtures: Account, Course, Section, Lesson, Asset, AssetVersion (PROCESSING)
	var accountID, courseID, assetID, versionID string
	err = pool.QueryRow(ctx, "INSERT INTO accounts (email, normalized_email, display_name, role, status) VALUES ('test40@example.com', 'test40@example.com', 'Test', 'STUDENT', 'ACTIVE') RETURNING id::text").Scan(&accountID)
	if err != nil { t.Fatalf("insert account: %v", err) }
	err = pool.QueryRow(ctx, "INSERT INTO courses (owner_account_id) VALUES ($1::uuid) RETURNING id::text", accountID).Scan(&courseID)
	if err != nil { t.Fatalf("insert course: %v", err) }
	err = pool.QueryRow(ctx, "INSERT INTO media_assets (kind, owner_account_id, course_id, visibility) VALUES ('VIDEO', $1::uuid, $2::uuid, 'PROTECTED') RETURNING id::text", accountID, courseID).Scan(&assetID)
	if err != nil { t.Fatalf("insert media_asset: %v", err) }
	
	// Create Scan attempt and Upload intent
	err = pool.QueryRow(ctx, "INSERT INTO media_asset_versions (logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes) VALUES ($1::uuid, 'VIDEO', 'SCANNING', 'k', 'v', 'video/mp4', 100) RETURNING id::text", assetID).Scan(&versionID)
	if err != nil { t.Fatalf("insert version: %v", err) }
	

	var scanAttemptID string
	err = pool.QueryRow(ctx, "INSERT INTO scan_attempts (asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity, reason) VALUES ($1::uuid, 1, 'w', 'v', 'PASSED', 's', 'r') RETURNING id::text", versionID).Scan(&scanAttemptID)
	if err != nil { t.Fatalf("insert scan attempt: %v", err) }

	// Advance to PROCESSING
	_, err = pool.Exec(ctx, "UPDATE media_asset_versions SET state = 'SCAN_PASSED', successful_scan_attempt_id = $2::uuid WHERE id = $1::uuid", versionID, scanAttemptID)
	if err != nil { t.Fatalf("update to SCAN_PASSED: %v", err) }
	
	_, err = pool.Exec(ctx, "UPDATE media_asset_versions SET state = 'PROCESSING', work_claim_token='token1', work_claimed_at=now(), work_lease_expires_at=now() + interval '1 hour' WHERE id = $1::uuid", versionID)
	if err != nil { t.Fatalf("update to PROCESSING: %v", err) }

	// C. permits: PROCESSING -> PLAYABLE (and A. accepts active claim with state = PLAYABLE)
	_, err = pool.Exec(ctx, "UPDATE media_asset_versions SET state = 'PLAYABLE' WHERE id = $1::uuid", versionID)
	if err != nil { t.Fatalf("update to PLAYABLE failed: %v", err) }

	// B. rejects incoherent claim tuples
	_, err = pool.Exec(ctx, "UPDATE media_asset_versions SET work_claim_token = '' WHERE id = $1::uuid", versionID)
	if err == nil { t.Fatal("expected failure on incoherent claim (missing claimed_at update)") }

	// E. rejects: PLAYABLE -> PROCESS_FAILED
	_, err = pool.Exec(ctx, "UPDATE media_asset_versions SET state = 'PROCESS_FAILED' WHERE id = $1::uuid", versionID)
	if err == nil { t.Fatal("expected failure on PLAYABLE -> PROCESS_FAILED transition") }

	// F. preserves existing legal transitions
	// Allow clearing claim in PLAYABLE (same state transition)
	_, err = pool.Exec(ctx, "UPDATE media_asset_versions SET work_claim_token = NULL, work_claimed_at = NULL, work_lease_expires_at = NULL WHERE id = $1::uuid", versionID)
	if err != nil { t.Fatalf("failed clearing claim in PLAYABLE: %v", err) }

	// Add transcode attempt to allow transition to READY
	var processAttemptID string
	err = pool.QueryRow(ctx, "INSERT INTO processing_attempts (asset_version_id, operation_id, state, rendition_count, trusted_duration_ms, output_prefix) VALUES ($1::uuid, 'op1', 'SUCCEEDED', 1, 1000, 'prefix') RETURNING id::text", versionID).Scan(&processAttemptID)
	if err != nil { t.Fatalf("insert processing attempt: %v", err) }

	// D. permits: PLAYABLE -> READY
	_, err = pool.Exec(ctx, "UPDATE media_asset_versions SET state = 'READY', trusted_duration_ms = 1000, successful_processing_attempt_id = $2::uuid WHERE id = $1::uuid", versionID, processAttemptID)
	if err != nil { t.Fatalf("update to READY failed: %v", err) }

	// G. expired-lease index predicate contains PLAYABLE
	// Verify index exists and uses PLAYABLE
	var indexDef string
	err = pool.QueryRow(ctx, "SELECT indexdef FROM pg_indexes WHERE tablename = 'media_asset_versions' AND indexname = 'media_asset_versions_expired_work_lease_idx'").Scan(&indexDef)
	if err != nil { t.Fatalf("reading index def: %v", err) }
	if !contains(indexDef, "PLAYABLE") {
		t.Fatalf("index missing PLAYABLE: %s", indexDef)
	}

	// 4. Down Migration
	if err := m.Steps(-1); err != nil {
		t.Fatalf("applying 0040 down: %v", err)
	}
	
	// Ensure PLAYABLE enum still exists
	var playableStillExists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_enum e
			JOIN pg_type t ON t.oid = e.enumtypid
			WHERE t.typname = 'media_asset_version_state' AND e.enumlabel = 'PLAYABLE'
		)
	`).Scan(&playableStillExists); err != nil {
		t.Fatalf("checking for PLAYABLE enum after 0040 down marker: %v", err)
	}
	if !playableStillExists {
		t.Fatal("PLAYABLE enum value was removed by 0040 down marker; expected it to persist")
	}
}

func contains(s, substr string) bool {
	for i := 0; i < len(s)-len(substr)+1; i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
