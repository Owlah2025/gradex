//go:build integration

package identity

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/db"
)

func schema41ReleasePathFixture(t *testing.T) (*migrate.Migrate, *pgxpool.Pool) {
	t.Helper()
	freshSchema(t)
	migrator, err := migrate.New(sourceURL, testDSN)
	if err != nil {
		t.Fatalf("opening release-path migrator: %v", err)
	}
	t.Cleanup(func() { _, _ = migrator.Close() })
	if err := migrator.Migrate(uint(db.EnhancementRecoveryFoundationSchemaVersion)); err != nil {
		t.Fatalf("preparing the verified schema-41 start: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), testDSN)
	if err != nil {
		t.Fatalf("opening schema-41 release-path database: %v", err)
	}
	t.Cleanup(pool.Close)
	return migrator, pool
}

func seedReleasePathQuarantinedVideo(t *testing.T, ctx context.Context, pool *pgxpool.Pool, accountID string) string {
	t.Helper()
	var courseID, assetID, versionID string
	if err := pool.QueryRow(ctx,
		"INSERT INTO courses (owner_account_id) VALUES ($1::uuid) RETURNING id::text", accountID,
	).Scan(&courseID); err != nil {
		t.Fatalf("seeding media owner Course: %v", err)
	}
	if err := pool.QueryRow(ctx,
		"INSERT INTO media_assets (kind, owner_account_id, course_id, visibility) "+
			"VALUES ('VIDEO', $1::uuid, $2::uuid, 'PROTECTED') RETURNING id::text",
		accountID, courseID,
	).Scan(&assetID); err != nil {
		t.Fatalf("seeding logical media asset: %v", err)
	}
	if err := pool.QueryRow(ctx,
		"INSERT INTO media_asset_versions "+
			"(logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes) "+
			"VALUES ($1::uuid, 'VIDEO', 'QUARANTINED', 'release-path/source.mp4', 'v1', 'video/mp4', 100) "+
			"RETURNING id::text", assetID,
	).Scan(&versionID); err != nil {
		t.Fatalf("seeding quiescent media version: %v", err)
	}
	return versionID
}

func insertHistoricalSession(t *testing.T, ctx context.Context, pool *pgxpool.Pool, accountID string, trust SessionDeviceTrust, suffix string) string {
	t.Helper()
	var sessionID string
	if err := pool.QueryRow(ctx,
		"INSERT INTO sessions "+
			"(account_id, admitted_epoch, idle_expires_at, absolute_expires_at, device_trust_state) "+
			"VALUES ($1::uuid, 1, now() + interval '1 day', now() + interval '7 days', $2) "+
			"RETURNING id::text", accountID, trust,
	).Scan(&sessionID); err != nil {
		t.Fatalf("seeding historical %s session family: %v", trust, err)
	}
	if _, err := pool.Exec(ctx,
		"INSERT INTO session_credentials (session_id, generation, credential_digest, csrf_digest) "+
			"VALUES ($1::uuid, 1, $2, $3)", sessionID, "release-path-"+suffix, "csrf-"+suffix,
	); err != nil {
		t.Fatalf("seeding historical session credential generation: %v", err)
	}
	return sessionID
}

func verifySchema42RecoveryReadiness(t *testing.T, ctx context.Context) {
	t.Helper()
	probe := os.Getenv("GRADEX_SCHEMA42_RECOVERY_PROBE")
	if probe == "" {
		return
	}
	command := exec.CommandContext(ctx, probe, "-test.run=^TestSchema42RecoveryReadinessPredicates$", "-test.v")
	command.Env = append(os.Environ(), "GRADEX_SCHEMA42_RECOVERY_DSN="+testDSN)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("frozen schema-42 recovery readiness probe: %v\n%s", err, output)
	}
	t.Logf("schema-42 recovery probe: %s", output)
}

func TestStudentDeviceReleasePathFromClean41Through43(t *testing.T) {
	migrator, pool := schema41ReleasePathFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	state, err := db.ReadSchemaState(ctx, pool)
	if err != nil || state.Version != db.EnhancementRecoveryFoundationSchemaVersion || state.Dirty {
		t.Fatalf("starting marker = %+v, err=%v; want clean 41", state, err)
	}

	f := newDeviceFixtureWithPool(t, pool, "device-release-path@example.test")
	trustedBrowser := newBrowser(t, "Chrome/120.0 (Windows)")
	trustedSession := f.loginEmail(t, "device-release-path@example.test", trustedBrowser, "release-path-trusted-login")
	legacyAccount := insertDeviceAccount(t, pool, "device-release-legacy@example.test")
	pendingAccount := insertDeviceAccount(t, pool, "device-release-pending@example.test")
	legacySessionID := insertHistoricalSession(t, ctx, pool, legacyAccount, DeviceTrustLegacyUnbound, "legacy")
	pendingSessionID := insertHistoricalSession(t, ctx, pool, pendingAccount, DeviceTrustPending, "pending")
	pendingBrowser := newBrowser(t, "Firefox/121.0 (Macintosh)")
	var pendingDeviceID string
	if err := pool.QueryRow(ctx,
		"INSERT INTO identity_trusted_devices "+
			"(account_id, credential_digest, label, browser_family, platform_family) "+
			"VALUES ($1::uuid, $2, 'Firefox on Mac', 'Firefox', 'Mac') RETURNING id::text",
		pendingAccount, pendingBrowser.digest,
	).Scan(&pendingDeviceID); err != nil {
		t.Fatalf("seeding historical pending device: %v", err)
	}
	if _, err := pool.Exec(ctx,
		"INSERT INTO identity_action_secrets "+
			"(account_id, purpose, secret_digest, issued_at, expires_at, created_at, trusted_device_id) "+
			"VALUES ($1::uuid, 'DEVICE_TRUST_OTP', decode(repeat('ac', 32), 'hex'), "+
			"now(), now() + interval '10 minutes', now(), $2::uuid)",
		pendingAccount, pendingDeviceID,
	); err != nil {
		t.Fatalf("seeding outstanding historical device OTP: %v", err)
	}
	mediaVersionID := seedReleasePathQuarantinedVideo(t, ctx, pool, f.account)

	var scannings, processings, claims int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FILTER (WHERE state='SCANNING'), "+
			"count(*) FILTER (WHERE state='PROCESSING'), "+
			"count(*) FILTER (WHERE work_claim_token IS NOT NULL) FROM media_asset_versions",
	).Scan(&scannings, &processings, &claims); err != nil {
		t.Fatal(err)
	}
	if scannings != 0 || processings != 0 || claims != 0 {
		t.Fatalf("schema-42 media gate: scanning=%d processing=%d claims=%d", scannings, processings, claims)
	}

	if err := migrator.Migrate(uint(db.ActiveProcessingKindSchemaVersion)); err != nil {
		t.Fatalf("frozen 0042 migration from clean 41: %v", err)
	}
	state, err = db.ReadSchemaState(ctx, pool)
	if err != nil || state.Version != 42 || state.Dirty {
		t.Fatalf("schema-42 marker = %+v, err=%v", state, err)
	}
	var activeKind *string
	if err := pool.QueryRow(ctx,
		"SELECT active_processing_attempt_kind::text FROM media_asset_versions WHERE id=$1::uuid",
		mediaVersionID,
	).Scan(&activeKind); err != nil || activeKind != nil {
		t.Fatalf("quiescent media evidence changed across 0042: active kind=%v, err=%v", activeKind, err)
	}

	verifySchema42RecoveryReadiness(t, ctx)

	counts, err := ReadDeviceCutoverCounts(ctx, pool)
	if err != nil || counts.LegacySessions != 1 || counts.PendingSessions != 1 ||
		counts.OutstandingOTP != 1 || counts.UnexpiredOTP != 1 {
		t.Fatalf("historical state before cutover = %+v, err=%v", counts, err)
	}
	_, err = ApplyDeviceCutover(ctx, pool, DeviceCutoverRequest{
		Expected: DeviceCutoverCounts{}, Operator: "release-path-test", RequestID: "release-path-count-mismatch",
	})
	if err == nil {
		t.Fatal("device cutover accepted changed historical counts")
	}
	if sessionState, _ := f.sessionState(t, trustedSession.Session.SessionID); sessionState != "ACTIVE" {
		t.Fatal("refused count mismatch changed the trusted Student family")
	}
	for _, sessionID := range []string{legacySessionID, pendingSessionID} {
		var status string
		if err := pool.QueryRow(ctx, "SELECT state::text FROM sessions WHERE id=$1::uuid", sessionID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "ACTIVE" {
			t.Fatalf("refused count mismatch changed historical family %s to %s", sessionID, status)
		}
	}

	if _, err := ApplyDeviceCutover(ctx, pool, DeviceCutoverRequest{
		Expected: counts, Operator: "release-path-test", RequestID: "release-path-cutover",
	}); err != nil {
		t.Fatalf("device cutover at clean 42: %v", err)
	}
	for _, sessionID := range []string{legacySessionID, pendingSessionID} {
		var status string
		if err := pool.QueryRow(ctx, "SELECT state::text FROM sessions WHERE id=$1::uuid", sessionID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "REVOKED" {
			t.Fatalf("cutover left historical family %s in state %s", sessionID, status)
		}
	}
	if sessionState, trustState := f.sessionState(t, trustedSession.Session.SessionID); sessionState != "ACTIVE" || trustState != string(DeviceTrustEstablished) {
		t.Fatalf("trusted Student family changed during cutover: %s/%s", sessionState, trustState)
	}
	counts, err = ReadDeviceCutoverCounts(ctx, pool)
	if err != nil || counts.LegacySessions != 0 || counts.PendingSessions != 0 || counts.OutstandingOTP != 0 {
		t.Fatalf("historical residue after cutover = %+v, err=%v", counts, err)
	}
	verifySchema42RecoveryReadiness(t, ctx)

	if err := migrator.Steps(1); err != nil {
		t.Fatalf("schema 42 -> 43: %v", err)
	}
	state, err = db.ReadSchemaState(ctx, pool)
	if err != nil || state.Version != db.AutoDeviceReplacementSchemaVersion || state.Dirty {
		t.Fatalf("schema-43 marker = %+v, err=%v", state, err)
	}
	if err := db.CheckSchemaAtLeast(ctx, pool, db.AutoDeviceReplacementSchemaVersion); err != nil {
		t.Fatalf("f41f9c28 API schema readiness predicate: %v", err)
	}
	newBrowser := newBrowser(t, "Safari/605.1 (iPhone)")
	grant, err := f.sessions.Login(ctx, LoginRequest{
		Email: "device-release-path@example.test", Password: config.NewSecret(deviceTestPassword),
		RequestID: "release-path-post-43-login", DeviceCredentialDigest: newBrowser.digest,
	})
	if err != nil {
		t.Fatalf("f41f9c28 login after clean schema 43: %v", err)
	}
	if grant.Device == nil || grant.Device.Admission != AdmitTrustedDevice ||
		grant.Session.DeviceTrust != DeviceTrustEstablished || grant.Device.Challenge != nil {
		t.Fatalf("post-43 admission = %+v, session = %+v", grant.Device, grant.Session)
	}
	if f.trustedCount(t) != 2 {
		t.Fatalf("post-43 live trusted-device count = %d, want 2", f.trustedCount(t))
	}
}
