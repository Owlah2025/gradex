//go:build integration

package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/db"
)

// TestSchema42RecoveryReadinessPredicates is copied into the frozen 3C-B source
// worktree by deploy/device43/verify-schema41-to43.sh. It proves the actual
// schema-42 API and worker floors against the disposable schema-42 release-path
// database without changing that database.
func TestSchema42RecoveryReadinessPredicates(t *testing.T) {
	dsn := os.Getenv("GRADEX_SCHEMA42_RECOVERY_DSN")
	if dsn == "" {
		t.Skip("run deploy/device43/verify-schema41-to43.sh to test the frozen schema-42 build")
	}
	if db.MaxSchemaVersion != 42 {
		t.Fatalf("schema-42 recovery build ceiling = %d, want 42", db.MaxSchemaVersion)
	}
	apiFloor := requiredSchemaVersion(nil)
	if apiFloor != db.SubjectDemandSignalSchemaVersion {
		t.Fatalf("schema-42 recovery API floor = %d, want %d", apiFloor, db.SubjectDemandSignalSchemaVersion)
	}
	if db.ActiveProcessingKindSchemaVersion != 42 {
		t.Fatalf("schema-42 recovery worker floor = %d, want 42", db.ActiveProcessingKindSchemaVersion)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connecting to schema-42 release-path database: %v", err)
	}
	defer pool.Close()
	if err := db.Ping(ctx, pool); err != nil {
		t.Fatalf("API readiness database probe: %v", err)
	}
	state, err := db.ReadSchemaState(ctx, pool)
	if err != nil || state.Version != 42 || state.Dirty {
		t.Fatalf("schema-42 recovery state = %+v, err=%v", state, err)
	}
	if err := db.CheckSchemaAtLeast(ctx, pool, apiFloor); err != nil {
		t.Fatalf("schema-42 recovery API readiness check: %v", err)
	}
	if err := db.CheckSchemaAtLeast(ctx, pool, db.ActiveProcessingKindSchemaVersion); err != nil {
		t.Fatalf("schema-42 recovery worker readiness check: %v", err)
	}
}
