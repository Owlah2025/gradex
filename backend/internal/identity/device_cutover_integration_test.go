//go:build integration

package identity

import (
	"context"
	"errors"
	"testing"
	"time"

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
