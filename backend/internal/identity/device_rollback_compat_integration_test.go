//go:build integration

package identity

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/Owlah2025/gradex/backend/internal/db"
)

// Run through deploy/device43/verify-rollback-compat.sh. The probe is compiled
// from the frozen application plus the reviewed schema-43 compatibility patch.
func TestSchema43RollbackApplicationAfterAutomaticRotation(t *testing.T) {
	probe := os.Getenv("GRADEX_SCHEMA43_ROLLBACK_PROBE")
	if probe == "" {
		t.Skip("run deploy/device43/verify-rollback-compat.sh to build the rollback probe")
	}
	f := newDeviceFixture(t)
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
