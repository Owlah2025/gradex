//go:build integration

package db

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestDeviceCutoverCommandWorksOnCleanSchema38(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	pool := openPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	if err := m.Migrate(uint(SubjectDemandSignalSchemaVersion)); err != nil {
		t.Fatal(err)
	}
	var accountID, sessionID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts
		  (normalized_email, email, role, status, display_name, email_verified_at)
		 VALUES ('cutover38@example.test', 'cutover38@example.test',
		         'STUDENT', 'ACTIVE', 'Cutover 38', now())
		 RETURNING id::text`).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO sessions
		  (account_id, admitted_epoch, idle_expires_at, absolute_expires_at)
		 VALUES ($1::uuid, 1, now() + interval '1 day', now() + interval '7 days')
		 RETURNING id::text`, accountID).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	binary := t.TempDir() + "/gradex-device-cutover"
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../cmd/device-cutover")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building cutover command: %v\n%s", err, output)
	}
	run := func(args ...string) string {
		t.Helper()
		command := exec.CommandContext(ctx, binary, args...)
		command.Env = append(os.Environ(), "DATABASE_URL="+testDSN)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("cutover command: %v\n%s", err, output)
		}
		return string(output)
	}
	before := run("-mode=check")
	if !strings.Contains(before, `"schema_version":38`) ||
		!strings.Contains(before, `"legacy_sessions":1`) {
		t.Fatalf("schema-38 preflight = %s", before)
	}
	run("-mode=apply", "-expected-schema=38", "-expected-legacy=1",
		"-expected-pending=0", "-expected-otp=0", "-operator=test-operator",
		"-request-id=cutover-schema38", "-confirm=REVOKE_HISTORICAL_DEVICE_SESSIONS")
	after := run("-mode=check")
	if !strings.Contains(after, `"legacy_sessions":0`) {
		t.Fatalf("schema-38 postflight = %s", after)
	}
	var state, reason string
	if err := pool.QueryRow(ctx,
		`SELECT state::text, revocation_reason::text FROM sessions WHERE id=$1::uuid`,
		sessionID).Scan(&state, &reason); err != nil {
		t.Fatal(err)
	}
	if state != "REVOKED" || reason != "ADMIN_REVOKED" {
		t.Fatalf("schema-38 family = %s/%s", state, reason)
	}
}
