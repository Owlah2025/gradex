//go:build integration

package db

import (
	"context"
	"strings"
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
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	err = pool.QueryRow(ctx, "INSERT INTO courses (owner_account_id) VALUES ($1::uuid) RETURNING id::text", accountID).Scan(&courseID)
	if err != nil {
		t.Fatalf("insert course: %v", err)
	}
	err = pool.QueryRow(ctx, "INSERT INTO media_assets (kind, owner_account_id, course_id, visibility) VALUES ('VIDEO', $1::uuid, $2::uuid, 'PROTECTED') RETURNING id::text", accountID, courseID).Scan(&assetID)
	if err != nil {
		t.Fatalf("insert media_asset: %v", err)
	}

	// Create Scan attempt and Upload intent
	err = pool.QueryRow(ctx, "INSERT INTO media_asset_versions (logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes) VALUES ($1::uuid, 'VIDEO', 'SCANNING', 'k', 'v', 'video/mp4', 100) RETURNING id::text", assetID).Scan(&versionID)
	if err != nil {
		t.Fatalf("insert version: %v", err)
	}

	var scanAttemptID string
	err = pool.QueryRow(ctx, "INSERT INTO scan_attempts (asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity, reason) VALUES ($1::uuid, 1, 'w', 'v', 'PASSED', 's', 'r') RETURNING id::text", versionID).Scan(&scanAttemptID)
	if err != nil {
		t.Fatalf("insert scan attempt: %v", err)
	}

	// Advance to PROCESSING
	_, err = pool.Exec(ctx, "UPDATE media_asset_versions SET state = 'SCAN_PASSED', successful_scan_attempt_id = $2::uuid WHERE id = $1::uuid", versionID, scanAttemptID)
	if err != nil {
		t.Fatalf("update to SCAN_PASSED: %v", err)
	}

	_, err = pool.Exec(ctx, "UPDATE media_asset_versions SET state = 'PROCESSING', work_claim_token='token1', work_claimed_at=now(), work_lease_expires_at=now() + interval '1 hour' WHERE id = $1::uuid", versionID)
	if err != nil {
		t.Fatalf("update to PROCESSING: %v", err)
	}

	// C. permits: PROCESSING -> PLAYABLE (and A. accepts active claim with state = PLAYABLE)
	_, err = pool.Exec(ctx, "UPDATE media_asset_versions SET state = 'PLAYABLE' WHERE id = $1::uuid", versionID)
	if err != nil {
		t.Fatalf("update to PLAYABLE failed: %v", err)
	}

	// B. rejects incoherent claim tuples
	_, err = pool.Exec(ctx, "UPDATE media_asset_versions SET work_claim_token = '' WHERE id = $1::uuid", versionID)
	if err == nil {
		t.Fatal("expected failure on incoherent claim (missing claimed_at update)")
	}

	// E. rejects: PLAYABLE -> PROCESS_FAILED
	_, err = pool.Exec(ctx, "UPDATE media_asset_versions SET state = 'PROCESS_FAILED' WHERE id = $1::uuid", versionID)
	if err == nil {
		t.Fatal("expected failure on PLAYABLE -> PROCESS_FAILED transition")
	}

	// E2. rejects: PLAYABLE -> QUARANTINED
	_, err = pool.Exec(ctx, "UPDATE media_asset_versions SET state = 'QUARANTINED' WHERE id = $1::uuid", versionID)
	if err == nil {
		t.Fatal("expected failure on PLAYABLE -> QUARANTINED transition")
	}

	// F. preserves existing legal transitions
	// Allow clearing claim in PLAYABLE (same state transition)
	_, err = pool.Exec(ctx, "UPDATE media_asset_versions SET work_claim_token = NULL, work_claimed_at = NULL, work_lease_expires_at = NULL WHERE id = $1::uuid", versionID)
	if err != nil {
		t.Fatalf("failed clearing claim in PLAYABLE: %v", err)
	}

	// Add transcode attempt to allow transition to READY
	var processAttemptID string
	err = pool.QueryRow(ctx, "INSERT INTO processing_attempts (asset_version_id, operation_id, state, rendition_count, trusted_duration_ms, output_prefix) VALUES ($1::uuid, 'op1', 'SUCCEEDED', 1, 1000, 'prefix') RETURNING id::text", versionID).Scan(&processAttemptID)
	if err != nil {
		t.Fatalf("insert processing attempt: %v", err)
	}

	// D. permits: PLAYABLE -> READY
	_, err = pool.Exec(ctx, "UPDATE media_asset_versions SET state = 'READY', trusted_duration_ms = 1000, successful_processing_attempt_id = $2::uuid WHERE id = $1::uuid", versionID, processAttemptID)
	if err != nil {
		t.Fatalf("update to READY failed: %v", err)
	}

	// G. expired-lease index predicate contains PLAYABLE
	// Verify index exists and uses PLAYABLE
	var indexDef string
	err = pool.QueryRow(ctx, "SELECT indexdef FROM pg_indexes WHERE tablename = 'media_asset_versions' AND indexname = 'media_asset_versions_expired_work_lease_idx'").Scan(&indexDef)
	if err != nil {
		t.Fatalf("reading index def: %v", err)
	}
	if !strings.Contains(indexDef, "PLAYABLE") {
		t.Fatalf("index missing PLAYABLE: %s", indexDef)
	}

	// H. Regression tests for preserved trigger guards under schema 40:
	// H1: storage_object_version immutability
	_, err = pool.Exec(ctx, "UPDATE media_asset_versions SET storage_object_version = 'v2' WHERE id = $1::uuid", versionID)
	if err == nil {
		t.Fatal("expected failure modifying storage_object_version after upload")
	}

	// H2: sha256_hex immutability
	_, err = pool.Exec(ctx, "UPDATE media_asset_versions SET sha256_hex = '0000000000000000000000000000000000000000000000000000000000000000' WHERE id = $1::uuid", versionID)
	if err == nil {
		t.Fatal("expected failure modifying sha256_hex after upload")
	}

	// H3: D-088 / D-096 validation allowlist check is preserved
	var previewAssetID, previewVersionID string
	err = pool.QueryRow(ctx, "INSERT INTO media_assets (kind, owner_account_id, course_id, visibility) VALUES ('PREVIEW', $1::uuid, $2::uuid, 'PUBLIC_PREVIEW') RETURNING id::text", accountID, courseID).Scan(&previewAssetID)
	if err != nil {
		t.Fatalf("insert preview asset: %v", err)
	}
	err = pool.QueryRow(ctx, "INSERT INTO media_asset_versions (logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes, sha256_hex) VALUES ($1::uuid, 'PREVIEW', 'QUARANTINED', 'kp', 'vp', 'application/pdf', 200, 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad') RETURNING id::text", previewAssetID).Scan(&previewVersionID)
	if err != nil {
		t.Fatalf("insert preview version: %v", err)
	}
	_, err = pool.Exec(ctx, "INSERT INTO upload_intents (asset_version_id, expected_object_key, expected_content_type, expected_size_bytes, max_size_bytes, expires_at) VALUES ($1::uuid, 'kp', 'application/pdf', 200, 1000, now() + interval '1 hour')", previewVersionID)
	if err != nil {
		t.Fatalf("insert upload intent: %v", err)
	}
	var valAttemptID string
	err = pool.QueryRow(ctx, `
		INSERT INTO validation_attempts (asset_version_id, attempt_number, work_id, storage_object_version, outcome, validator_identity, profile, declared_content_type, verified_size_bytes, max_size_bytes, sha256_hex)
		VALUES ($1::uuid, 1, 'val-work-1', 'vp', 'PASSED', 'test-validator', 'D-088-TRUSTED-INSTRUCTOR', 'application/pdf', 200, 1000, 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad')
		RETURNING id::text
	`, previewVersionID).Scan(&valAttemptID)
	if err != nil {
		t.Fatalf("insert validation attempt: %v", err)
	}
	_, err = pool.Exec(ctx, "UPDATE media_asset_versions SET state = 'VALIDATED', successful_validation_attempt_id = $2::uuid WHERE id = $1::uuid", previewVersionID, valAttemptID)
	if err == nil || !strings.Contains(err.Error(), "outside the D-088 trusted-validation profile") {
		t.Fatalf("expected D-088 profile refusal for non-MP4 preview, got %v", err)
	}

	// H4: Thumbnail raster evidence guard is preserved
	var revID string
	err = pool.QueryRow(ctx, "INSERT INTO course_revisions (course_id, revision_number, title_ar, title_en) VALUES ($1::uuid, 1, 'العنوان', 'Title') RETURNING id::text", courseID).Scan(&revID)
	if err != nil {
		t.Fatalf("insert course revision: %v", err)
	}
	var thumbAssetID, thumbVersionID string
	err = pool.QueryRow(ctx, "INSERT INTO media_assets (kind, owner_account_id, course_id, preview_origin_revision_id, visibility) VALUES ('THUMBNAIL', $1::uuid, $2::uuid, $3::uuid, 'PROTECTED') RETURNING id::text", accountID, courseID, revID).Scan(&thumbAssetID)
	if err != nil {
		t.Fatalf("insert thumbnail asset: %v", err)
	}
	err = pool.QueryRow(ctx, "INSERT INTO media_asset_versions (logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes, sha256_hex) VALUES ($1::uuid, 'THUMBNAIL', 'QUARANTINED', 'kt', 'vt', 'image/png', 300, 'cb8379ac2098aa165029e3938a51da0bcecfc008fd6795f401178647f96c5b34') RETURNING id::text", thumbAssetID).Scan(&thumbVersionID)
	if err != nil {
		t.Fatalf("insert thumbnail version: %v", err)
	}
	_, err = pool.Exec(ctx, "UPDATE media_asset_versions SET state = 'READY' WHERE id = $1::uuid", thumbVersionID)
	if err == nil || !strings.Contains(err.Error(), "thumbnail requires exact-source raster processing evidence") {
		t.Fatalf("expected thumbnail raster evidence refusal, got %v", err)
	}

	// 4. Down Migration
	if err := m.Steps(-1); err != nil {
		t.Fatalf("applying 0040 down: %v", err)
	}

	state, err = ReadSchemaState(ctx, pool)
	if err != nil {
		t.Fatalf("reading schema state after down: %v", err)
	}
	if state.Version != MediaPlayableEnumSchemaVersion || state.Dirty {
		t.Fatalf("schema after down = %+v", state)
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

	// Assert schema 39 restored behavior
	err = pool.QueryRow(ctx, "SELECT indexdef FROM pg_indexes WHERE tablename = 'media_asset_versions' AND indexname = 'media_asset_versions_expired_work_lease_idx'").Scan(&indexDef)
	if err != nil {
		t.Fatalf("reading index def after down: %v", err)
	}
	if strings.Contains(indexDef, "PLAYABLE") {
		t.Fatalf("index predicate after down still contains PLAYABLE: %s", indexDef)
	}

	// 5. Migrate up to schema 40 again (repeat cycle check)
	if err := m.Steps(1); err != nil {
		t.Fatalf("re-applying migration 0040: %v", err)
	}
	state, err = ReadSchemaState(ctx, pool)
	if err != nil {
		t.Fatalf("reading schema state after 2nd 0040 up: %v", err)
	}
	if state.Version != MediaPlayableFoundationSchemaVersion || state.Dirty {
		t.Fatalf("schema after 2nd 0040 up = %+v", state)
	}
}
