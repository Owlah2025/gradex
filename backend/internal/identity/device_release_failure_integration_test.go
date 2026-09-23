//go:build integration

package identity

import (
	"context"
	"errors"
	"testing"

	"github.com/Owlah2025/gradex/backend/internal/db"
)

func TestSchema43MigrationFailureLeavesDirtySchemaUnservable(t *testing.T) {
	migrator, pool := schema41ReleasePathFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	if err := migrator.Migrate(uint(db.ActiveProcessingKindSchemaVersion)); err != nil {
		t.Fatalf("preparing clean schema 42: %v", err)
	}
	if _, err := pool.Exec(ctx,
		"ALTER TYPE session_revocation_reason RENAME TO session_revocation_reason_missing_for_failure_test",
	); err != nil {
		t.Fatalf("injecting local 0043 failure: %v", err)
	}
	if err := migrator.Steps(1); err == nil {
		t.Fatal("0043 unexpectedly succeeded after its required enum was removed")
	}
	state, err := db.ReadSchemaState(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != db.AutoDeviceReplacementSchemaVersion || !state.Dirty {
		t.Fatalf("failed 0043 marker = %+v, want version 43 dirty", state)
	}
	for _, floor := range []int64{db.ActiveProcessingKindSchemaVersion, db.AutoDeviceReplacementSchemaVersion} {
		if err := db.CheckSchemaAtLeast(ctx, pool, floor); !errors.Is(err, db.ErrSchemaDirty) {
			t.Fatalf("readiness floor %d on dirty 43 = %v, want ErrSchemaDirty", floor, err)
		}
	}
	_, _ = migrator.Close()
	pool.Close()
}
