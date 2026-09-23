//go:build integration

package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

func TestDeviceCutoverRefusesNonStudentHistoricalFamily(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 (Windows)")
	trusted := f.login(t, first, "cutover-staff-control")
	var staffAccount, staffSession string
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO accounts
		  (normalized_email, email, role, status, display_name, email_verified_at)
		 VALUES ('cutover-staff@example.test', 'cutover-staff@example.test',
		         'INSTRUCTOR', 'ACTIVE', 'Cutover Staff', now())
		 RETURNING id::text`).Scan(&staffAccount); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO sessions
		  (account_id, admitted_epoch, idle_expires_at, absolute_expires_at)
		 VALUES ($1::uuid, 1, now() + interval '1 day', now() + interval '7 days')
		 RETURNING id::text`, staffAccount).Scan(&staffSession); err != nil {
		t.Fatal(err)
	}
	counts, err := ReadDeviceCutoverCounts(context.Background(), f.pool)
	if err != nil || counts.LegacySessions != 1 {
		t.Fatalf("preflight = %+v, %v", counts, err)
	}
	_, err = ApplyDeviceCutover(context.Background(), f.pool, DeviceCutoverRequest{
		Expected: counts, Operator: "test-release-operator", RequestID: "staff-refusal",
	})
	if err == nil {
		t.Fatal("cutover silently revoked a staff legacy family")
	}
	if state, _ := f.sessionState(t, staffSession); state != "ACTIVE" {
		t.Fatal("staff family changed")
	}
	if state, _ := f.sessionState(t, trusted.Session.SessionID); state != "ACTIVE" {
		t.Fatal("Student trusted family changed")
	}
}

func TestDeviceCutoverRevokesOnlyHistoricalFamiliesAndChallenges(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 (Windows)")
	second := newBrowser(t, "Safari/604.1 (iPhone)")
	fresh := newBrowser(t, "Firefox/121.0 (Macintosh)")
	legacy := f.login(t, first, "cutover-legacy-login")
	pending := f.login(t, second, "cutover-pending-login")
	trusted := f.login(t, first, "cutover-trusted-login")
	otherAccount := insertDeviceAccount(t, f.pool, "cutover-other@example.test")
	otherBrowser := newBrowser(t, "Chrome/120.0 (Linux)")
	other, err := f.sessions.Login(context.Background(), LoginRequest{
		Email: "cutover-other@example.test", Password: config.NewSecret(deviceTestPassword),
		RequestID: "cutover-other-login", DeviceCredentialDigest: otherBrowser.digest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE sessions SET device_trust_state = 'LEGACY_UNBOUND', trusted_device_id = NULL
		  WHERE id = $1::uuid`, legacy.Session.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE sessions SET device_trust_state = 'PENDING_DEVICE_TRUST', trusted_device_id = NULL
		  WHERE id = $1::uuid`, pending.Session.SessionID); err != nil {
		t.Fatal(err)
	}
	var pendingDeviceID string
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO identity_trusted_devices
		  (account_id, credential_digest, label, browser_family, platform_family)
		 VALUES ($1::uuid, $2, 'Firefox on Mac', 'Firefox', 'Mac')
		 RETURNING id::text`, f.account, fresh.digest).Scan(&pendingDeviceID); err != nil {
		t.Fatal(err)
	}
	var otpID string
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO identity_action_secrets
		  (account_id, purpose, secret_digest, issued_at, expires_at, created_at, trusted_device_id)
		 VALUES ($1::uuid, 'DEVICE_TRUST_OTP', decode(repeat('ab', 32), 'hex'),
		         now(), now() + interval '10 minutes', now(), $2::uuid)
		 RETURNING id::text`, f.account, pendingDeviceID).Scan(&otpID); err != nil {
		t.Fatal(err)
	}

	counts, err := ReadDeviceCutoverCounts(context.Background(), f.pool)
	if err != nil || counts.LegacySessions != 1 || counts.PendingSessions != 1 ||
		counts.OutstandingOTP != 1 || counts.UnexpiredOTP != 1 {
		t.Fatalf("preflight = %+v, err=%v", counts, err)
	}
	_, err = ApplyDeviceCutover(context.Background(), f.pool, DeviceCutoverRequest{
		Expected: DeviceCutoverCounts{}, Operator: "test-release-operator",
		RequestID: "cutover-wrong-counts",
	})
	if err == nil {
		t.Fatal("count mismatch was accepted")
	}
	if state, _ := f.sessionState(t, legacy.Session.SessionID); state != "ACTIVE" {
		t.Fatal("refused cutover changed a session")
	}
	applied, err := ApplyDeviceCutover(context.Background(), f.pool, DeviceCutoverRequest{
		Expected: counts, Operator: "test-release-operator", RequestID: "cutover-approved",
	})
	if err != nil || applied.LegacySessions != 1 || applied.PendingSessions != 1 {
		t.Fatalf("cutover = %+v, err=%v", applied, err)
	}
	for _, grant := range []SessionGrant{legacy, pending} {
		if state, _ := f.sessionState(t, grant.Session.SessionID); state != "REVOKED" {
			t.Fatalf("historical family state = %s", state)
		}
	}
	if state, _ := f.sessionState(t, trusted.Session.SessionID); state != "ACTIVE" {
		t.Fatal("trusted family was revoked")
	}
	if state, _ := f.sessionState(t, other.Session.SessionID); state != "ACTIVE" {
		t.Fatal("other Account's trusted family was revoked")
	}
	if otherAccount == f.account {
		t.Fatal("fixture Accounts collided")
	}
	var supersededAt *time.Time
	var supersededBy string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT superseded_at, superseded_by_id::text FROM identity_action_secrets WHERE id=$1::uuid`,
		otpID).Scan(&supersededAt, &supersededBy); err != nil {
		t.Fatal(err)
	}
	if supersededAt == nil || supersededBy != otpID {
		t.Fatal("device OTP survived cutover")
	}
	after, err := ReadDeviceCutoverCounts(context.Background(), f.pool)
	if err != nil || after.LegacySessions != 0 || after.PendingSessions != 0 || after.OutstandingOTP != 0 {
		t.Fatalf("cutover residue = %+v, err=%v", after, err)
	}
	var audits int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events WHERE action='STUDENT_DEVICE_CUTOVER' AND correlation_id='cutover-approved'`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("cutover audits = %d", audits)
	}
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events WHERE action='STUDENT_DEVICE_CUTOVER_SESSION_REVOKED' AND correlation_id='cutover-approved'`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 2 {
		t.Fatalf("per-family cutover audits = %d", audits)
	}
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events WHERE action='DEVICE_TRUST_OTP_INVALIDATED' AND target_id=$1 AND correlation_id='cutover-approved'`, otpID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("device OTP invalidation audits = %d", audits)
	}
	grant := f.login(t, fresh, "cutover-fresh-password-login")
	if grant.Session.DeviceTrust != DeviceTrustEstablished || grant.Device.Challenge != nil {
		t.Fatalf("fresh login after cutover = %+v", grant.Device)
	}
	if _, err := f.sessions.Resolve(context.Background(), SessionResolutionRequest{
		CredentialDigest:       DigestToken(legacy.Credential.Expose()),
		DeviceCredentialDigest: first.digest, UseKind: UseReadOnly,
	}); !errors.Is(err, ErrAuthenticationRequired) {
		t.Fatalf("revoked legacy family resolved: %v", err)
	}
}

func TestExpiredStaffCleanupRevokes26ExpiredAdminAndInstructorSessions(t *testing.T) {
	f := newDeviceFixture(t)
	admin := insertExpiredStaffCleanupAccount(t, f.pool, "ADMIN", "cleanup-26-admin@example.test")
	instructor := insertExpiredStaffCleanupAccount(t, f.pool, "INSTRUCTOR", "cleanup-26-instructor@example.test")
	var targetIDs []string
	for index := 0; index < 20; index++ {
		targetIDs = append(targetIDs, insertCleanupSessionFixture(t, f.pool, admin, cleanupSessionFixture{
			absoluteExpiry: time.Now().UTC().Add(-time.Duration(index+1) * time.Hour),
		}))
	}
	for index := 0; index < 6; index++ {
		targetIDs = append(targetIDs, insertCleanupSessionFixture(t, f.pool, instructor, cleanupSessionFixture{
			absoluteExpiry: time.Now().UTC().Add(-time.Duration(index+1) * time.Hour),
		}))
	}

	counts, err := ReadExpiredStaffLegacyCleanupCounts(context.Background(), f.pool)
	if err != nil || counts != (ExpiredStaffLegacyCleanupCounts{EligibleExpiredLegacy: 26}) {
		t.Fatalf("preflight counts = %+v, err=%v", counts, err)
	}
	applied, err := ApplyExpiredStaffLegacyCleanup(context.Background(), f.pool, ExpiredStaffLegacyCleanupRequest{
		ExpectedEligible: 26, Operator: "release-operator", RequestID: "cleanup-26",
	})
	if err != nil || applied != counts {
		t.Fatalf("cleanup = %+v, err=%v", applied, err)
	}
	for _, sessionID := range targetIDs {
		var state, reason string
		if err := f.pool.QueryRow(context.Background(),
			`SELECT state::text, revocation_reason::text FROM sessions WHERE id=$1::uuid`, sessionID).Scan(&state, &reason); err != nil {
			t.Fatal(err)
		}
		if state != "REVOKED" || reason != "ADMIN_REVOKED" {
			t.Fatalf("target %s = %s/%s", sessionID, state, reason)
		}
	}
	var audited int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events WHERE actor_role='RELEASE_OPERATOR' AND action='EXPIRED_LEGACY_SESSION_CLEANUP' AND correlation_id='cleanup-26'`).Scan(&audited); err != nil {
		t.Fatal(err)
	}
	if audited != 26 {
		t.Fatalf("per-session cleanup audits = %d, want 26", audited)
	}
}

func TestExpiredStaffCleanupRefusesNonexpiredAndPendingStaffSessions(t *testing.T) {
	for _, role := range []string{"INSTRUCTOR", "ADMIN"} {
		t.Run(role, func(t *testing.T) {
			f := newDeviceFixture(t)
			accountID := insertExpiredStaffCleanupAccount(t, f.pool, role, "cleanup-block-"+strings.ToLower(role)+"@example.test")
			legacyID := insertCleanupSessionFixture(t, f.pool, accountID, cleanupSessionFixture{
				absoluteExpiry: time.Now().UTC().Add(24 * time.Hour),
			})
			counts, err := ReadExpiredStaffLegacyCleanupCounts(context.Background(), f.pool)
			if err != nil || counts.NonExpiredLegacy != 1 || counts.EligibleExpiredLegacy != 0 {
				t.Fatalf("preflight counts = %+v, err=%v", counts, err)
			}
			if _, err := ApplyExpiredStaffLegacyCleanup(context.Background(), f.pool, ExpiredStaffLegacyCleanupRequest{
				ExpectedEligible: 0, Operator: "release-operator", RequestID: "cleanup-block-" + role,
			}); err == nil {
				t.Fatal("cleanup accepted non-permitted Staff history")
			}
			if state, _ := f.sessionState(t, legacyID); state != "ACTIVE" {
				t.Fatalf("refused cleanup changed %s to %s", legacyID, state)
			}
		})
	}
	t.Run("pending non-Student family", func(t *testing.T) {
		f := newDeviceFixture(t)
		accountID := insertExpiredStaffCleanupAccount(t, f.pool, "ADMIN", "cleanup-pending-admin@example.test")
		pendingID := insertCleanupSessionFixture(t, f.pool, accountID, cleanupSessionFixture{
			trustState: "PENDING_DEVICE_TRUST", absoluteExpiry: time.Now().UTC().Add(-time.Hour),
		})
		counts, err := ReadExpiredStaffLegacyCleanupCounts(context.Background(), f.pool)
		if err != nil || counts.PendingDeviceTrust != 1 {
			t.Fatalf("preflight counts = %+v, err=%v", counts, err)
		}
		if _, err := ApplyExpiredStaffLegacyCleanup(context.Background(), f.pool, ExpiredStaffLegacyCleanupRequest{
			ExpectedEligible: 0, Operator: "release-operator", RequestID: "cleanup-pending-admin",
		}); err == nil {
			t.Fatal("cleanup accepted a non-Student pending family")
		}
		if state, _ := f.sessionState(t, pendingID); state != "ACTIVE" {
			t.Fatalf("refused cleanup changed pending session to %s", state)
		}
	})
}

func TestExpiredStaffCleanupLeavesStudentAndTrustedFamiliesUntouched(t *testing.T) {
	f := newDeviceFixture(t)
	studentLegacyID := insertCleanupSessionFixture(t, f.pool, f.account, cleanupSessionFixture{
		absoluteExpiry: time.Now().UTC().Add(24 * time.Hour),
	})
	studentBrowser := newBrowser(t, "Chrome/120.0 (Windows)")
	studentTrusted := f.login(t, studentBrowser, "staff-cleanup-student-trusted")
	staffAccount := insertExpiredStaffCleanupAccount(t, f.pool, "INSTRUCTOR", "cleanup-trusted-staff@example.test")
	staffDeviceID := insertCleanupTrustedDevice(t, f.pool, staffAccount)
	staffTrustedID := insertCleanupSessionFixture(t, f.pool, staffAccount, cleanupSessionFixture{
		trustState: "TRUSTED", absoluteExpiry: time.Now().UTC().Add(24 * time.Hour), deviceID: staffDeviceID,
	})

	if _, err := ApplyExpiredStaffLegacyCleanup(context.Background(), f.pool, ExpiredStaffLegacyCleanupRequest{
		ExpectedEligible: 0, Operator: "release-operator", RequestID: "cleanup-student-and-trusted",
	}); err != nil {
		t.Fatal(err)
	}
	for _, sessionID := range []string{studentLegacyID, studentTrusted.Session.SessionID, staffTrustedID} {
		if state, _ := f.sessionState(t, sessionID); state != "ACTIVE" {
			t.Fatalf("cleanup changed non-target session %s to %s", sessionID, state)
		}
	}
	for _, deviceID := range []string{studentTrusted.Device.DeviceID, staffDeviceID} {
		var revokedAt *time.Time
		if err := f.pool.QueryRow(context.Background(),
			`SELECT revoked_at FROM identity_trusted_devices WHERE id=$1::uuid`, deviceID).Scan(&revokedAt); err != nil {
			t.Fatal(err)
		}
		if revokedAt != nil {
			t.Fatalf("trusted device %s was touched", deviceID)
		}
	}
	studentCounts, err := ReadDeviceCutoverCounts(context.Background(), f.pool)
	if err != nil || studentCounts.LegacySessions != 1 {
		t.Fatalf("Student cutover state = %+v, err=%v", studentCounts, err)
	}
}

func TestExpiredStaffCleanupCountMismatchAndConcurrentChangeRollback(t *testing.T) {
	t.Run("wrong expected count", func(t *testing.T) {
		f := newDeviceFixture(t)
		accountID := insertExpiredStaffCleanupAccount(t, f.pool, "ADMIN", "cleanup-wrong-count@example.test")
		firstID := insertCleanupSessionFixture(t, f.pool, accountID, cleanupSessionFixture{
			absoluteExpiry: time.Now().UTC().Add(-time.Hour),
		})
		secondID := insertCleanupSessionFixture(t, f.pool, accountID, cleanupSessionFixture{
			absoluteExpiry: time.Now().UTC().Add(-2 * time.Hour),
		})
		if _, err := ApplyExpiredStaffLegacyCleanup(context.Background(), f.pool, ExpiredStaffLegacyCleanupRequest{
			ExpectedEligible: 1, Operator: "release-operator", RequestID: "cleanup-wrong-count",
		}); err == nil {
			t.Fatal("cleanup accepted a mismatched expected count")
		}
		for _, sessionID := range []string{firstID, secondID} {
			if state, _ := f.sessionState(t, sessionID); state != "ACTIVE" {
				t.Fatalf("count mismatch changed %s to %s", sessionID, state)
			}
		}
	})

	t.Run("target changes while cleanup waits", func(t *testing.T) {
		f := newDeviceFixture(t)
		accountID := insertExpiredStaffCleanupAccount(t, f.pool, "INSTRUCTOR", "cleanup-race@example.test")
		sessionID := insertCleanupSessionFixture(t, f.pool, accountID, cleanupSessionFixture{
			absoluteExpiry: time.Now().UTC().Add(-time.Hour),
		})
		blocker, err := f.pool.Begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := blocker.Exec(context.Background(),
			`UPDATE sessions SET absolute_expires_at=now()+interval '1 day', idle_expires_at=now()+interval '1 hour' WHERE id=$1::uuid`, sessionID); err != nil {
			_ = blocker.Rollback(context.Background())
			t.Fatal(err)
		}
		type cleanupResult struct {
			err error
		}
		finished := make(chan cleanupResult, 1)
		go func() {
			_, err := ApplyExpiredStaffLegacyCleanup(context.Background(), f.pool, ExpiredStaffLegacyCleanupRequest{
				ExpectedEligible: 1, Operator: "release-operator", RequestID: "cleanup-race",
			})
			finished <- cleanupResult{err: err}
		}()
		lockWaitSeen := false
		for attempts := 0; attempts < 100; attempts++ {
			var waiting int
			if err := f.pool.QueryRow(context.Background(), `
				SELECT count(*) FROM pg_stat_activity
				 WHERE datname = current_database() AND wait_event_type = 'Lock'
				   AND query LIKE 'LOCK TABLE sessions IN SHARE ROW EXCLUSIVE MODE%'`).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			if waiting > 0 {
				lockWaitSeen = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !lockWaitSeen {
			t.Fatal("cleanup did not wait for the session write lock")
		}
		if err := blocker.Commit(context.Background()); err != nil {
			t.Fatal(err)
		}
		select {
		case result := <-finished:
			if result.err == nil {
				t.Fatal("cleanup accepted a target changed during its gate")
			}
		case <-time.After(10 * time.Second):
			t.Fatal("cleanup did not finish after the concurrent target change")
		}
		if state, _ := f.sessionState(t, sessionID); state != "ACTIVE" {
			t.Fatalf("concurrent count mismatch changed target to %s", state)
		}
	})
}

func TestExpiredStaffCleanupAuditEpochOtherSessionAndRetry(t *testing.T) {
	f := newDeviceFixture(t)
	accountID := insertExpiredStaffCleanupAccount(t, f.pool, "ADMIN", "cleanup-audit-admin@example.test")
	otherAccountID := insertExpiredStaffCleanupAccount(t, f.pool, "INSTRUCTOR", "cleanup-audit-other@example.test")
	otherDeviceID := insertCleanupTrustedDevice(t, f.pool, otherAccountID)
	targetID := insertCleanupSessionFixture(t, f.pool, accountID, cleanupSessionFixture{
		absoluteExpiry: time.Now().UTC().Add(-time.Hour),
	})
	otherID := insertCleanupSessionFixture(t, f.pool, otherAccountID, cleanupSessionFixture{
		trustState: "TRUSTED", absoluteExpiry: time.Now().UTC().Add(24 * time.Hour), deviceID: otherDeviceID,
	})
	var epochBefore, epochAfter int
	if err := f.pool.QueryRow(context.Background(), `SELECT session_epoch FROM accounts WHERE id=$1::uuid`, accountID).Scan(&epochBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyExpiredStaffLegacyCleanup(context.Background(), f.pool, ExpiredStaffLegacyCleanupRequest{
		ExpectedEligible: 1, Operator: "release-operator", RequestID: "cleanup-audit-retry",
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(context.Background(), `SELECT session_epoch FROM accounts WHERE id=$1::uuid`, accountID).Scan(&epochAfter); err != nil {
		t.Fatal(err)
	}
	if epochAfter != epochBefore {
		t.Fatalf("session epoch changed from %d to %d", epochBefore, epochAfter)
	}
	if state, _ := f.sessionState(t, otherID); state != "ACTIVE" {
		t.Fatalf("other account session changed to %s", state)
	}
	var otherDeviceRevokedAt *time.Time
	if err := f.pool.QueryRow(context.Background(),
		`SELECT revoked_at FROM identity_trusted_devices WHERE id=$1::uuid`, otherDeviceID).Scan(&otherDeviceRevokedAt); err != nil {
		t.Fatal(err)
	}
	if otherDeviceRevokedAt != nil {
		t.Fatal("other account trusted device was touched")
	}
	var actorRole, actor, action, module, targetType, auditedTarget, reason, correlation, accountRole, trustState string
	var expiry time.Time
	if err := f.pool.QueryRow(context.Background(), `
		SELECT actor_role, actor_descriptor, action, module::text, target_type, target_id,
		       reason, correlation_id, metadata->>'account_role',
		       metadata->>'device_trust_state', (metadata->>'absolute_expires_at')::timestamptz
		  FROM audit_events WHERE correlation_id='cleanup-audit-retry'`).Scan(
		&actorRole, &actor, &action, &module, &targetType, &auditedTarget,
		&reason, &correlation, &accountRole, &trustState, &expiry); err != nil {
		t.Fatal(err)
	}
	if actorRole != "RELEASE_OPERATOR" || actor != "release-operator" || action != "EXPIRED_LEGACY_SESSION_CLEANUP" ||
		module != "IDENTITY_AND_ACCESS" || targetType != "SESSION" || auditedTarget != targetID ||
		accountRole != "ADMIN" || trustState != "LEGACY_UNBOUND" || correlation != "cleanup-audit-retry" ||
		!strings.Contains(reason, "expired non-Student") || expiry.IsZero() {
		t.Fatalf("cleanup audit semantics were incomplete: role=%s actor=%s action=%s module=%s target=%s/%s role=%s state=%s correlation=%s reason=%q expiry=%s",
			actorRole, actor, action, module, targetType, auditedTarget, accountRole, trustState, correlation, reason, expiry)
	}
	if _, err := ApplyExpiredStaffLegacyCleanup(context.Background(), f.pool, ExpiredStaffLegacyCleanupRequest{
		ExpectedEligible: 1, Operator: "release-operator", RequestID: "cleanup-audit-retry",
	}); err == nil {
		t.Fatal("retry with stale positive expected count succeeded")
	}
	if _, err := ApplyExpiredStaffLegacyCleanup(context.Background(), f.pool, ExpiredStaffLegacyCleanupRequest{
		ExpectedEligible: 0, Operator: "release-operator", RequestID: "cleanup-audit-retry-zero",
	}); err != nil {
		t.Fatalf("zero-count idempotent retry failed: %v", err)
	}
	var auditCount int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events WHERE action='EXPIRED_LEGACY_SESSION_CLEANUP' AND target_id=$1`, targetID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("cleanup audit count after retry = %d", auditCount)
	}
}

func insertExpiredStaffCleanupAccount(t *testing.T, pool *pgxpool.Pool, role, email string) string {
	t.Helper()
	var accountID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO accounts (normalized_email, email, role, status, display_name, email_verified_at)
		VALUES ($1, $1, $2, 'ACTIVE', 'Cleanup Staff', now())
		RETURNING id::text`, email, role).Scan(&accountID); err != nil {
		t.Fatalf("inserting %s cleanup account: %v", role, err)
	}
	return accountID
}

type cleanupSessionFixture struct {
	trustState     string
	absoluteExpiry time.Time
	state          string
	deviceID       string
}

func insertCleanupSessionFixture(
	t *testing.T, pool *pgxpool.Pool, accountID string, fixture cleanupSessionFixture,
) string {
	t.Helper()
	if fixture.trustState == "" {
		fixture.trustState = "LEGACY_UNBOUND"
	}
	if fixture.state == "" {
		fixture.state = "ACTIVE"
	}
	var sessionID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO sessions
		  (account_id, admitted_epoch, idle_expires_at, absolute_expires_at,
		   state, device_trust_state, trusted_device_id)
		VALUES ($1::uuid, 1, $2::timestamptz - interval '1 minute', $2::timestamptz,
		        $3::session_state, $4::session_device_trust_state, nullif($5, '')::uuid)
		RETURNING id::text`, accountID, fixture.absoluteExpiry, fixture.state, fixture.trustState, fixture.deviceID).Scan(&sessionID); err != nil {
		t.Fatalf("inserting cleanup session: %v", err)
	}
	return sessionID
}

func insertCleanupTrustedDevice(t *testing.T, pool *pgxpool.Pool, accountID string) string {
	t.Helper()
	_, digest, err := NewOpaqueCredential()
	if err != nil {
		t.Fatal(err)
	}
	var deviceID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO identity_trusted_devices
		  (account_id, credential_digest, label, browser_family, platform_family, trusted_at)
		VALUES ($1::uuid, $2, 'Instructor browser', 'Chrome', 'Linux', now())
		RETURNING id::text`, accountID, digest).Scan(&deviceID); err != nil {
		t.Fatalf("inserting trusted cleanup fixture: %v", err)
	}
	return deviceID
}
