//go:build integration

package db

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// schema41Fixture seeds one Asset Version at schema 40, along with one
// SUCCEEDED whole-ladder processing attempt and one canonical rendition, so the
// migration is exercised over data that predates it rather than over an empty
// table.
type schema41Fixture struct {
	versionID string
	attemptID string
}

func seedSchema40MediaEvidence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string) schema41Fixture {
	t.Helper()
	var accountID, courseID, assetID string
	var fixture schema41Fixture
	if err := pool.QueryRow(ctx, `
		INSERT INTO accounts (email, normalized_email, display_name, role, status)
		VALUES ($1, $1, 'Test', 'STUDENT', 'ACTIVE') RETURNING id::text
	`, "schema41-"+suffix+"@example.com").Scan(&accountID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	if err := pool.QueryRow(ctx,
		"INSERT INTO courses (owner_account_id) VALUES ($1::uuid) RETURNING id::text",
		accountID).Scan(&courseID); err != nil {
		t.Fatalf("insert course: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_assets (kind, owner_account_id, course_id, visibility)
		VALUES ('VIDEO', $1::uuid, $2::uuid, 'PROTECTED') RETURNING id::text
	`, accountID, courseID).Scan(&assetID); err != nil {
		t.Fatalf("insert media_asset: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_asset_versions (logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes)
		VALUES ($1::uuid, 'VIDEO', 'SCANNING', 'k', 'v', 'video/mp4', 100) RETURNING id::text
	`, assetID).Scan(&fixture.versionID); err != nil {
		t.Fatalf("insert version: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO processing_attempts (asset_version_id, operation_id, state, rendition_count, trusted_duration_ms, output_prefix)
		VALUES ($1::uuid, 'legacy-op', 'SUCCEEDED', 1, 1000, 'media/legacy/hls/abc') RETURNING id::text
	`, fixture.versionID).Scan(&fixture.attemptID); err != nil {
		t.Fatalf("insert legacy processing attempt: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO video_renditions (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms)
		VALUES ($1::uuid, '720p', 'media/legacy/hls/abc/720p/playlist.m3u8', 1280, 720, 2800, 1000)
	`, fixture.versionID); err != nil {
		t.Fatalf("insert legacy rendition: %v", err)
	}
	return fixture
}

// TestSchema41EnhancementRecoveryFoundation proves 0041 applies over real
// schema-40 evidence, resolves history to FULL without a rewrite, and
// discriminates the result-coherence rule by attempt kind.
func TestSchema41EnhancementRecoveryFoundation(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	pool := openPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	if err := m.Migrate(uint(MediaPlayableFoundationSchemaVersion)); err != nil {
		t.Fatalf("migrating to schema 40: %v", err)
	}
	state, err := ReadSchemaState(ctx, pool)
	if err != nil {
		t.Fatalf("reading schema state at 40: %v", err)
	}
	if state.Version != MediaPlayableFoundationSchemaVersion || state.Dirty {
		t.Fatalf("schema at 40 = %+v", state)
	}

	fixture := seedSchema40MediaEvidence(t, ctx, pool, "up")

	// 1. Schema 41 applies cleanly over schema-40 fixtures.
	if err := m.Steps(1); err != nil {
		t.Fatalf("applying migration 0041: %v", err)
	}
	state, err = ReadSchemaState(ctx, pool)
	if err != nil {
		t.Fatalf("reading schema state after 0041: %v", err)
	}
	if state.Version != EnhancementRecoveryFoundationSchemaVersion || state.Dirty {
		t.Fatalf("schema after 0041 = %+v", state)
	}

	// 2. Existing processing attempts read as FULL.
	var kind string
	if err := pool.QueryRow(ctx,
		"SELECT attempt_kind::text FROM processing_attempts WHERE id = $1::uuid",
		fixture.attemptID).Scan(&kind); err != nil {
		t.Fatalf("reading migrated attempt kind: %v", err)
	}
	if kind != "FULL" {
		t.Fatalf("pre-0041 attempt kind = %q, want FULL", kind)
	}

	// 3. Existing renditions survive with NULL provenance and are not rewritten.
	var provenance *string
	var storageKey string
	if err := pool.QueryRow(ctx, `
		SELECT processing_operation_id, storage_object_key FROM video_renditions WHERE asset_version_id = $1::uuid
	`, fixture.versionID).Scan(&provenance, &storageKey); err != nil {
		t.Fatalf("reading migrated rendition: %v", err)
	}
	if provenance != nil {
		t.Fatalf("legacy rendition provenance = %q, want NULL (no backfill of append-only evidence)", *provenance)
	}
	if storageKey != "media/legacy/hls/abc/720p/playlist.m3u8" {
		t.Fatalf("legacy rendition storage key changed to %q", storageKey)
	}

	// 4. An ordinary successful FULL attempt remains valid, by default and explicitly.
	if _, err := pool.Exec(ctx, `
		INSERT INTO processing_attempts (asset_version_id, operation_id, state, rendition_count, trusted_duration_ms, output_prefix)
		VALUES ($1::uuid, 'full-default', 'SUCCEEDED', 4, 1000, 'media/x/hls/abc')
	`, fixture.versionID); err != nil {
		t.Fatalf("successful FULL attempt via default kind rejected: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO processing_attempts (asset_version_id, operation_id, state, attempt_kind, rendition_count, trusted_duration_ms, output_prefix)
		VALUES ($1::uuid, 'full-explicit', 'SUCCEEDED', 'FULL', 4, 1000, 'media/x/hls/def')
	`, fixture.versionID); err != nil {
		t.Fatalf("explicit successful FULL attempt rejected: %v", err)
	}

	// 5. An ordinary FAILED attempt remains valid, unchanged.
	if _, err := pool.Exec(ctx, `
		INSERT INTO processing_attempts (asset_version_id, operation_id, state, rendition_count, error_reason)
		VALUES ($1::uuid, 'failed-op', 'FAILED', 0, 'encoder died')
	`, fixture.versionID); err != nil {
		t.Fatalf("FAILED attempt rejected: %v", err)
	}

	// 6. SUCCEEDED FINALIZATION with no output is schema-valid.
	if _, err := pool.Exec(ctx, `
		INSERT INTO processing_attempts (asset_version_id, operation_id, state, attempt_kind, rendition_count, trusted_duration_ms, output_prefix)
		VALUES ($1::uuid, 'finalize-op', 'SUCCEEDED', 'FINALIZATION', 0, 1000, NULL)
	`, fixture.versionID); err != nil {
		t.Fatalf("SUCCEEDED FINALIZATION with zero renditions rejected: %v", err)
	}

	// 7. SUCCEEDED FULL with rendition_count = 0 is still rejected.
	if _, err := pool.Exec(ctx, `
		INSERT INTO processing_attempts (asset_version_id, operation_id, state, attempt_kind, rendition_count, trusted_duration_ms, output_prefix)
		VALUES ($1::uuid, 'full-zero', 'SUCCEEDED', 'FULL', 0, 1000, 'media/x/hls/ghi')
	`, fixture.versionID); err == nil {
		t.Fatal("expected a zero-rendition successful FULL attempt to be refused")
	}

	// 8. SUCCEEDED ENHANCEMENT with invalid result evidence is rejected.
	if _, err := pool.Exec(ctx, `
		INSERT INTO processing_attempts (asset_version_id, operation_id, state, attempt_kind, rendition_count, trusted_duration_ms, output_prefix)
		VALUES ($1::uuid, 'enh-zero', 'SUCCEEDED', 'ENHANCEMENT', 0, 1000, 'media/x/hls/jkl')
	`, fixture.versionID); err == nil {
		t.Fatal("expected a zero-rendition successful ENHANCEMENT attempt to be refused")
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO processing_attempts (asset_version_id, operation_id, state, attempt_kind, rendition_count, trusted_duration_ms, output_prefix)
		VALUES ($1::uuid, 'enh-noduration', 'SUCCEEDED', 'ENHANCEMENT', 2, NULL, 'media/x/hls/mno')
	`, fixture.versionID); err == nil {
		t.Fatal("expected a successful ENHANCEMENT attempt with no trusted duration to be refused")
	}

	// 9. A successful attempt that produced output may not have a NULL prefix
	//    unless it is FINALIZATION, which must have produced nothing.
	if _, err := pool.Exec(ctx, `
		INSERT INTO processing_attempts (asset_version_id, operation_id, state, attempt_kind, rendition_count, trusted_duration_ms, output_prefix)
		VALUES ($1::uuid, 'full-noprefix', 'SUCCEEDED', 'FULL', 4, 1000, NULL)
	`, fixture.versionID); err == nil {
		t.Fatal("expected a successful FULL attempt with NULL output_prefix to be refused")
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO processing_attempts (asset_version_id, operation_id, state, attempt_kind, rendition_count, trusted_duration_ms, output_prefix)
		VALUES ($1::uuid, 'finalize-withprefix', 'SUCCEEDED', 'FINALIZATION', 0, 1000, 'media/x/hls/pqr')
	`, fixture.versionID); err == nil {
		t.Fatal("expected a FINALIZATION attempt claiming storage output to be refused")
	}

	// media_processing_state is untouched: no scheduling/reservation value.
	var stateValues []string
	rows, err := pool.Query(ctx, `
		SELECT enumlabel FROM pg_enum e JOIN pg_type t ON t.oid = e.enumtypid
		WHERE t.typname = 'media_processing_state' ORDER BY e.enumsortorder
	`)
	if err != nil {
		t.Fatalf("reading media_processing_state values: %v", err)
	}
	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			t.Fatalf("scanning enum label: %v", err)
		}
		stateValues = append(stateValues, label)
	}
	rows.Close()
	if len(stateValues) != 2 || stateValues[0] != "SUCCEEDED" || stateValues[1] != "FAILED" {
		t.Fatalf("media_processing_state = %v, want exactly [SUCCEEDED FAILED]", stateValues)
	}

	// 10. The new column is covered by the existing append-only trigger.
	if _, err := pool.Exec(ctx, `
		INSERT INTO video_renditions (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms, processing_operation_id)
		VALUES ($1::uuid, '1080p', 'media/x/hls/abc/1080p/playlist.m3u8', 1920, 1080, 5000, 1000, 'op-new')
	`, fixture.versionID); err != nil {
		t.Fatalf("inserting a rendition with provenance: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE video_renditions SET processing_operation_id = 'op-forged' WHERE asset_version_id = $1::uuid AND name = '1080p'
	`, fixture.versionID); err == nil {
		t.Fatal("expected UPDATE of rendition provenance to be refused by the append-only trigger")
	}
	if _, err := pool.Exec(ctx, `
		UPDATE video_renditions SET processing_operation_id = 'op-backfill' WHERE asset_version_id = $1::uuid AND name = '720p'
	`, fixture.versionID); err == nil {
		t.Fatal("expected backfill of a legacy rendition's provenance to be refused by the append-only trigger")
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO video_renditions (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms, processing_operation_id)
		VALUES ($1::uuid, '480p', 'media/x/hls/abc/480p/playlist.m3u8', 854, 480, 1400, 1000, '   ')
	`, fixture.versionID); err == nil {
		t.Fatal("expected a blank processing_operation_id to be refused")
	}
}

// TestSchema41DownMigration proves the rollback floor in both directions: it
// reverses cleanly while every attempt is FULL, and refuses without mutating
// anything once finalization evidence exists.
func TestSchema41DownMigration(t *testing.T) {
	t.Run("reverses when no schema-41-only evidence exists", func(t *testing.T) {
		freshDatabase(t)
		m := openMigrator(t)
		pool := openPool(t)
		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()

		if err := m.Migrate(uint(EnhancementRecoveryFoundationSchemaVersion)); err != nil {
			t.Fatalf("migrating to schema 41: %v", err)
		}
		fixture := seedSchema40MediaEvidence(t, ctx, pool, "down-clean")
		// A rendition written after 0041 carries provenance; that alone must not
		// block rollback, or the floor would close the moment 0041 deploys.
		if _, err := pool.Exec(ctx, `
			INSERT INTO video_renditions (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms, processing_operation_id)
			VALUES ($1::uuid, '1080p', 'media/x/hls/abc/1080p/playlist.m3u8', 1920, 1080, 5000, 1000, 'op-new')
		`, fixture.versionID); err != nil {
			t.Fatalf("inserting a provenance-carrying rendition: %v", err)
		}

		if err := m.Steps(-1); err != nil {
			t.Fatalf("rolling back 0041: %v", err)
		}
		state, err := ReadSchemaState(ctx, pool)
		if err != nil {
			t.Fatalf("reading schema state after rollback: %v", err)
		}
		if state.Version != MediaPlayableFoundationSchemaVersion || state.Dirty {
			t.Fatalf("schema after rollback = %+v, want clean 40", state)
		}

		// The restored schema-40 rule must be the authoritative one again.
		if _, err := pool.Exec(ctx, `
			INSERT INTO processing_attempts (asset_version_id, operation_id, state, rendition_count, trusted_duration_ms, output_prefix)
			VALUES ($1::uuid, 'post-down-zero', 'SUCCEEDED', 0, 1000, NULL)
		`, fixture.versionID); err == nil {
			t.Fatal("expected restored schema 40 to refuse a zero-rendition successful attempt")
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO processing_attempts (asset_version_id, operation_id, state, rendition_count, trusted_duration_ms, output_prefix)
			VALUES ($1::uuid, 'post-down-ok', 'SUCCEEDED', 1, 1000, 'media/x/hls/abc')
		`, fixture.versionID); err != nil {
			t.Fatalf("restored schema 40 refused an ordinary successful attempt: %v", err)
		}
		// Attempt rows survive rollback; only the added column is gone.
		var attempts int
		if err := pool.QueryRow(ctx,
			"SELECT count(*) FROM processing_attempts WHERE asset_version_id = $1::uuid",
			fixture.versionID).Scan(&attempts); err != nil {
			t.Fatalf("counting attempts after rollback: %v", err)
		}
		if attempts != 2 {
			t.Fatalf("attempts after rollback = %d, want 2 (the seeded one plus the post-rollback insert)", attempts)
		}
	})

	t.Run("refuses without mutation when finalization evidence exists", func(t *testing.T) {
		freshDatabase(t)
		m := openMigrator(t)
		pool := openPool(t)
		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()

		if err := m.Migrate(uint(EnhancementRecoveryFoundationSchemaVersion)); err != nil {
			t.Fatalf("migrating to schema 41: %v", err)
		}
		fixture := seedSchema40MediaEvidence(t, ctx, pool, "down-blocked")
		if _, err := pool.Exec(ctx, `
			INSERT INTO processing_attempts (asset_version_id, operation_id, state, attempt_kind, rendition_count, trusted_duration_ms, output_prefix)
			VALUES ($1::uuid, 'finalize-op', 'SUCCEEDED', 'FINALIZATION', 0, 1000, NULL)
		`, fixture.versionID); err != nil {
			t.Fatalf("inserting finalization evidence: %v", err)
		}

		if err := m.Steps(-1); err == nil {
			t.Fatal("expected rollback to refuse while unrepresentable finalization evidence exists")
		}

		// Nothing may have been destroyed or rewritten by the refused attempt.
		var kind string
		var renditionCount int
		var prefix *string
		if err := pool.QueryRow(ctx, `
			SELECT attempt_kind::text, rendition_count, output_prefix FROM processing_attempts
			WHERE asset_version_id = $1::uuid AND operation_id = 'finalize-op'
		`, fixture.versionID).Scan(&kind, &renditionCount, &prefix); err != nil {
			t.Fatalf("finalization evidence did not survive the refused rollback: %v", err)
		}
		if kind != "FINALIZATION" || renditionCount != 0 || prefix != nil {
			t.Fatalf("finalization evidence was rewritten: kind=%s rendition_count=%d output_prefix=%v", kind, renditionCount, prefix)
		}
		var renditions int
		if err := pool.QueryRow(ctx,
			"SELECT count(*) FROM video_renditions WHERE asset_version_id = $1::uuid",
			fixture.versionID).Scan(&renditions); err != nil {
			t.Fatalf("counting renditions after refused rollback: %v", err)
		}
		if renditions != 1 {
			t.Fatalf("renditions after refused rollback = %d, want 1", renditions)
		}
	})
}
