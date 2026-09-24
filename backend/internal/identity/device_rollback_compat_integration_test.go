//go:build integration

package identity

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/golang-migrate/migrate/v4"

	"github.com/Owlah2025/gradex/backend/internal/db"
)

// stageIdentitySchema puts the identity test database at an exact schema.
//
// Tests that assert a specific schema boundary must stage that boundary
// explicitly rather than inherit whichever migration landed last; relative or
// head staging silently retargets them every time the head moves.
func stageIdentitySchema(t *testing.T, version int64) {
	t.Helper()
	m, err := migrate.New(sourceURL, testDSN)
	if err != nil {
		t.Fatalf("opening migrations: %v", err)
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Migrate(uint(version)); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("staging schema %d: %v", version, err)
	}
	current, dirty, err := m.Version()
	if err != nil || current != uint(version) || dirty {
		t.Fatalf("staged schema = version=%d dirty=%t err=%v, want clean %d", current, dirty, err, version)
	}
}

// Run through deploy/device43/verify-rollback-compat.sh. The probe is compiled
// from the frozen application plus the reviewed schema-43 compatibility patch.
func TestSchema43RollbackApplicationAfterAutomaticRotation(t *testing.T) {
	probe := os.Getenv("GRADEX_SCHEMA43_ROLLBACK_PROBE")
	if probe == "" {
		t.Skip("run deploy/device43/verify-rollback-compat.sh to build the rollback probe")
	}
	f := newDeviceFixture(t)
	// Staged at EXACTLY schema 43, which is the schema this rollback artifact
	// targets and the ceiling its build declares.
	//
	// newDeviceFixture migrates to the head, and the head is no longer 43. A
	// head-staged fixture would hand the schema-43 probe a database its own
	// supported range does not cover, and it would refuse to start — which is
	// correct behaviour for that artifact and tells us nothing about the rollback
	// it exists to prove. The forward artifact for schemas beyond 43 is
	// deploy/schema46, verified separately.
	stageIdentitySchema(t, db.AutoDeviceReplacementSchemaVersion)

	first := newBrowser(t, "Chrome/120.0 (Windows)")
	second := newBrowser(t, "Safari/604.1 (iPhone)")
	third := newBrowser(t, "Firefox/121.0 (Macintosh)")
	f.login(t, first, "compat-first-login")
	f.login(t, second, "compat-second-login")
	evictedID := f.deviceIDFor(t, second)
	thirdGrant := f.login(t, third, "compat-automatic-rotation")
	if thirdGrant.Device.EvictedDeviceID != evictedID {
		t.Fatalf("automatic rotation evicted %s, want %s", thirdGrant.Device.EvictedDeviceID, evictedID)
	}
	state, err := db.ReadSchemaState(context.Background(), f.pool)
	if err != nil || state.Version != db.AutoDeviceReplacementSchemaVersion || state.Dirty {
		t.Fatalf("schema state = %+v, err=%v", state, err)
	}

	command := exec.CommandContext(context.Background(), probe)
	command.Env = append(os.Environ(),
		"GRADEX_COMPAT_TEST_DSN="+testDSN,
		"GRADEX_COMPAT_ACCOUNT_ID="+f.account,
		"GRADEX_COMPAT_FIRST_DIGEST="+first.digest,
		"GRADEX_COMPAT_EVICTED_ID="+evictedID,
		"GRADEX_COMPAT_NEWER_ID="+thirdGrant.Device.DeviceID,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("rollback-compatible application failed: %v\n%s", err, output)
	}
	t.Logf("%s", output)
	state, err = db.ReadSchemaState(context.Background(), f.pool)
	if err != nil || state.Version != db.AutoDeviceReplacementSchemaVersion || state.Dirty {
		t.Fatalf("rollback application changed schema: %+v, err=%v", state, err)
	}
}
