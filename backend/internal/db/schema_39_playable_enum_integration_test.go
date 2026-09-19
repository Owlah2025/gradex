//go:build integration

package db

import (
	"context"
	"errors"
	"testing"
)

func TestSchema39PlayableEnumCompatibilityBridge(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	pool := openPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	// 1. Migrate up to schema 38 (pre-migration state in production).
	if err := m.Migrate(uint(SubjectDemandSignalSchemaVersion)); err != nil {
		t.Fatalf("migrating to schema 38: %v", err)
	}

	state, err := ReadSchemaState(ctx, pool)
	if err != nil {
		t.Fatalf("reading schema state at 38: %v", err)
	}
	if state.Version != SubjectDemandSignalSchemaVersion || state.Dirty {
		t.Fatalf("schema at 38 = %+v, want clean version %d", state, SubjectDemandSignalSchemaVersion)
	}

	// 2. Compatibility bridge: binary with floor 38 must accept schema 38.
	if err := CheckSchemaAtLeast(ctx, pool, SubjectDemandSignalSchemaVersion); err != nil {
		t.Fatalf("bridge binary rejected schema 38 before migration: %v", err)
	}
	if err := CheckSchema(ctx, pool); err != nil {
		t.Fatalf("CheckSchema rejected schema 38: %v", err)
	}

	// PLAYABLE is not present in enum at schema 38.
	var hasPlayable bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_enum e
			JOIN pg_type t ON t.oid = e.enumtypid
			WHERE t.typname = 'media_asset_version_state' AND e.enumlabel = 'PLAYABLE'
		)
	`).Scan(&hasPlayable); err != nil {
		t.Fatalf("checking for PLAYABLE enum at schema 38: %v", err)
	}
	if hasPlayable {
		t.Fatal("PLAYABLE enum value already present at schema 38")
	}

	// 3. Migrate up to schema 39.
	if err := m.Steps(1); err != nil {
		t.Fatalf("applying migration 0039: %v", err)
	}

	state, err = ReadSchemaState(ctx, pool)
	if err != nil {
		t.Fatalf("reading schema state after 0039: %v", err)
	}
	if state.Version != MediaPlayableEnumSchemaVersion || state.Dirty {
		t.Fatalf("schema after 0039 = %+v, want clean version %d", state, MediaPlayableEnumSchemaVersion)
	}

	// 4. Compatibility bridge: binary with floor 38 must accept schema 39.
	if err := CheckSchemaAtLeast(ctx, pool, SubjectDemandSignalSchemaVersion); err != nil {
		t.Fatalf("bridge binary rejected schema 39 after migration: %v", err)
	}
	if err := CheckSchema(ctx, pool); err != nil {
		t.Fatalf("CheckSchema rejected schema 39: %v", err)
	}

	// PLAYABLE is present in enum at schema 39.
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_enum e
			JOIN pg_type t ON t.oid = e.enumtypid
			WHERE t.typname = 'media_asset_version_state' AND e.enumlabel = 'PLAYABLE'
		)
	`).Scan(&hasPlayable); err != nil {
		t.Fatalf("checking for PLAYABLE enum at schema 39: %v", err)
	}
	if !hasPlayable {
		t.Fatal("PLAYABLE enum value missing at schema 39")
	}

	// 5. Version bounds enforcement.
	// Version 40 (above MaxSchemaVersion) must be rejected.
	if _, err := pool.Exec(ctx,
		"UPDATE "+schemaMigrationsTable+" SET version = $1", MaxSchemaVersion+1); err != nil {
		t.Fatalf("setting version above ceiling: %v", err)
	}
	if err := CheckSchemaAtLeast(ctx, pool, SubjectDemandSignalSchemaVersion); !errors.Is(err, ErrSchemaIncompatible) {
		t.Fatalf("schema above ceiling accepted: %v, want ErrSchemaIncompatible", err)
	}

	// Version 37 (below floor 38) must be rejected.
	if _, err := pool.Exec(ctx,
		"UPDATE "+schemaMigrationsTable+" SET version = $1", StudentTrustedDeviceSchemaVersion); err != nil {
		t.Fatalf("setting version below floor: %v", err)
	}
	if err := CheckSchemaAtLeast(ctx, pool, SubjectDemandSignalSchemaVersion); !errors.Is(err, ErrSchemaIncompatible) {
		t.Fatalf("schema below floor accepted: %v, want ErrSchemaIncompatible", err)
	}

	// Dirty schema must be rejected.
	if _, err := pool.Exec(ctx,
		"UPDATE "+schemaMigrationsTable+" SET version = $1, dirty = true", MediaPlayableEnumSchemaVersion); err != nil {
		t.Fatalf("setting dirty marker: %v", err)
	}
	if err := CheckSchemaAtLeast(ctx, pool, SubjectDemandSignalSchemaVersion); !errors.Is(err, ErrSchemaDirty) {
		t.Fatalf("dirty schema accepted: %v, want ErrSchemaDirty", err)
	}

	// Restore clean 39 marker for down-marker test.
	if _, err := pool.Exec(ctx,
		"UPDATE "+schemaMigrationsTable+" SET version = $1, dirty = false", MediaPlayableEnumSchemaVersion); err != nil {
		t.Fatalf("restoring clean schema marker: %v", err)
	}

	// 6. No-op down marker: 0039 down is intentionally `SELECT 1;`.
	// It adjusts the migration version marker back to schema 38 without dirtying
	// the database, but it is NOT a true schema rollback: PostgreSQL cannot
	// drop enum values, so the 'PLAYABLE' label physically persists in
	// media_asset_version_state.
	if err := m.Steps(-1); err != nil {
		t.Fatalf("applying 0039 no-op down marker: %v", err)
	}
	state, err = ReadSchemaState(ctx, pool)
	if err != nil {
		t.Fatalf("reading schema state after 0039 down marker: %v", err)
	}
	if state.Version != SubjectDemandSignalSchemaVersion || state.Dirty {
		t.Fatalf("schema after 0039 down marker = %+v, want clean version %d", state, SubjectDemandSignalSchemaVersion)
	}
	if err := CheckSchemaAtLeast(ctx, pool, SubjectDemandSignalSchemaVersion); err != nil {
		t.Fatalf("bridge rejected schema 38 after 0039 down marker: %v", err)
	}

	// Explicitly assert that 'PLAYABLE' STILL EXISTS in media_asset_version_state,
	// proving that 0039 DOWN changes migration bookkeeping only and does not remove
	// the PostgreSQL enum label.
	var playableStillExists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_enum e
			JOIN pg_type t ON t.oid = e.enumtypid
			WHERE t.typname = 'media_asset_version_state' AND e.enumlabel = 'PLAYABLE'
		)
	`).Scan(&playableStillExists); err != nil {
		t.Fatalf("checking for PLAYABLE enum after 0039 down marker: %v", err)
	}
	if !playableStillExists {
		t.Fatal("PLAYABLE enum value was removed by 0039 down marker; expected it to persist")
	}
}
