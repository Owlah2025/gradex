//go:build integration

package db

import (
	"context"
	"testing"
)

func TestSchema43UpDownUpFromCurrentPredecessor(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	pool := openPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	if err := m.Migrate(uint(ActiveProcessingKindSchemaVersion)); err != nil {
		t.Fatal(err)
	}
	if err := m.Steps(1); err != nil {
		t.Fatalf("42 -> 43: %v", err)
	}
	if err := m.Steps(-1); err != nil {
		t.Fatalf("43 -> 42 with no automatic replacements: %v", err)
	}
	state, err := ReadSchemaState(ctx, pool)
	if err != nil || state.Version != ActiveProcessingKindSchemaVersion || state.Dirty {
		t.Fatalf("after downgrade: %+v, %v", state, err)
	}
	if err := m.Steps(1); err != nil {
		t.Fatalf("42 -> 43 after downgrade: %v", err)
	}
}

func TestSchema43DownRefusesAutomaticReplacementEvidence(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	pool := openPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	if err := m.Migrate(uint(AutoDeviceReplacementSchemaVersion)); err != nil {
		t.Fatal(err)
	}
	var accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (normalized_email, email, role, status, display_name, email_verified_at)
		 VALUES ('schema43@example.test', 'schema43@example.test', 'STUDENT', 'ACTIVE', 'Schema 43', now())
		 RETURNING id::text`).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO identity_trusted_devices
		 (account_id, credential_digest, label, browser_family, platform_family,
		  first_seen_at, last_seen_at, trusted_at, revoked_at, revocation_reason)
		 VALUES ($1::uuid, 'schema43-digest', 'Browser', 'Browser', 'Platform',
		  now(), now(), now(), now(), 'AUTO_REPLACED')`,
		accountID); err != nil {
		t.Fatal(err)
	}
	if err := m.Steps(-1); err == nil {
		t.Fatal("downgrade discarded automatic-replacement evidence")
	}
}
