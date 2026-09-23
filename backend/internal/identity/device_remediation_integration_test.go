//go:build integration

package identity

import (
	"context"
	"errors"
	"testing"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

func TestOverLimitLoginFailsClosedAndAuditsCorruption(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 (Windows)")
	second := newBrowser(t, "Safari/604.1 (iPhone)")
	third := newBrowser(t, "Firefox/121.0 (Macintosh)")
	fourth := newBrowser(t, "Edg/120.0 (Windows)")
	firstGrant := f.login(t, first, "overlimit-first")
	secondGrant := f.login(t, second, "overlimit-second")
	if _, err := f.pool.Exec(context.Background(),
		`INSERT INTO identity_trusted_devices
		  (account_id, credential_digest, label, browser_family, platform_family,
		   first_seen_at, last_seen_at, trusted_at, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'Firefox on Mac', 'Firefox', 'Mac',
		         $3, $3, $3, $3, $3)`,
		f.account, third.digest, f.clock.Now()); err != nil {
		t.Fatal(err)
	}
	_, err := f.sessions.Login(context.Background(), LoginRequest{
		Email: "device-student@example.test", Password: config.NewSecret(deviceTestPassword),
		RequestID: "overlimit-fourth", DeviceCredentialDigest: fourth.digest,
		UserAgent: fourth.userAgent,
	})
	if !errors.Is(err, ErrDeviceLimitInvariantViolation) {
		t.Fatalf("over-limit login = %v", err)
	}
	if f.trustedCount(t) != 3 || f.deviceRows(t) != 3 {
		t.Fatalf("over-limit login changed devices: trusted=%d rows=%d", f.trustedCount(t), f.deviceRows(t))
	}
	for _, grant := range []SessionGrant{firstGrant, secondGrant} {
		if state, _ := f.sessionState(t, grant.Session.SessionID); state != "ACTIVE" {
			t.Fatalf("over-limit login revoked existing family: %s", state)
		}
	}
	if len(f.playback.Released()) != 0 {
		t.Fatal("over-limit refusal released playback")
	}
	var trustedCount, limit, audits int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT (metadata->>'trusted_count')::int, (metadata->>'device_limit')::int
		  FROM audit_events
		  WHERE action='DEVICE_LIMIT_INVARIANT_VIOLATION'
		    AND correlation_id='overlimit-fourth'`).Scan(&trustedCount, &limit); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events WHERE action='DEVICE_LIMIT_INVARIANT_VIOLATION'`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if trustedCount != 3 || limit != 2 || audits != 1 {
		t.Fatalf("over-limit audit = count=%d limit=%d rows=%d", trustedCount, limit, audits)
	}
}

func TestAutomaticRevocationIsAccountScopedAndExplicitlyAudited(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 (Windows)")
	second := newBrowser(t, "Safari/604.1 (iPhone)")
	third := newBrowser(t, "Firefox/121.0 (Macintosh)")
	f.login(t, first, "scope-first")
	f.login(t, second, "scope-second")
	victimID := f.deviceIDFor(t, second)
	otherAccount := insertDeviceAccount(t, f.pool, "scope-other@example.test")
	var otherSessionID string
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO sessions
		  (account_id, admitted_epoch, idle_expires_at, absolute_expires_at,
		   trusted_device_id, device_trust_state)
		 VALUES ($1::uuid, 1, now() + interval '1 day', now() + interval '7 days',
		         $2::uuid, 'TRUSTED')
		 RETURNING id::text`, otherAccount, victimID).Scan(&otherSessionID); err != nil {
		t.Fatal(err)
	}
	grant := f.login(t, third, "scope-third")
	if grant.Device.EvictedDeviceID != victimID {
		t.Fatal("wrong automatic victim")
	}
	if state, _ := f.sessionState(t, otherSessionID); state != "ACTIVE" {
		t.Fatalf("cross-account family was revoked: %s", state)
	}
	var admittedID, evictedID, mode string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT metadata->>'admitted_device_id', metadata->>'evicted_device_id',
		        metadata->>'replacement_mode'
		  FROM audit_events
		  WHERE action='AUTO_DEVICE_REPLACED' AND correlation_id='scope-third'`,
	).Scan(&admittedID, &evictedID, &mode); err != nil {
		t.Fatal(err)
	}
	if admittedID != grant.Device.DeviceID || evictedID != victimID || mode != "AUTOMATIC" {
		t.Fatalf("automatic audit = admitted=%s evicted=%s mode=%s", admittedID, evictedID, mode)
	}
}

func TestPlaybackReleaseFailureDoesNotUndoAutomaticRotation(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 (Windows)")
	second := newBrowser(t, "Safari/604.1 (iPhone)")
	third := newBrowser(t, "Firefox/121.0 (Macintosh)")
	firstGrant := f.login(t, first, "release-failure-first")
	secondGrant := f.login(t, second, "release-failure-second")
	victimID := f.deviceIDFor(t, second)
	f.playback.failure = errors.New("temporary playback coordinator failure")
	thirdGrant := f.login(t, third, "release-failure-third")
	if thirdGrant.Session.DeviceTrust != DeviceTrustEstablished ||
		thirdGrant.Device.EvictedDeviceID != victimID || f.trustedCount(t) != 2 {
		t.Fatalf("rotation did not commit: %+v", thirdGrant.Device)
	}
	if state, _ := f.sessionState(t, secondGrant.Session.SessionID); state != "REVOKED" {
		t.Fatalf("victim session = %s", state)
	}
	if state, _ := f.sessionState(t, firstGrant.Session.SessionID); state != "ACTIVE" {
		t.Fatalf("survivor session = %s", state)
	}
	released := f.playback.Released()
	if len(released) != 1 || released[0] != victimID {
		t.Fatalf("release attempts = %v", released)
	}
}

func TestSecurityRecoveryRetiresOutstandingDeviceOTP(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 (Windows)")
	pending := newBrowser(t, "Firefox/121.0 (Macintosh)")
	f.login(t, first, "recovery-first")
	otpID := seedOutstandingDeviceOTP(t, f, pending)
	tx, err := f.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	revision, err := lockAccountForDevicePolicy(context.Background(), tx, f.account)
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := f.devices.RevokeAllForSecurityRecovery(
		context.Background(), tx, f.account, revision, DeviceRevokedByReset,
		"recovery-clears-otp", f.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(revoked) != 2 || f.trustedCount(t) != 0 {
		t.Fatalf("recovery revoked=%d trusted=%d", len(revoked), f.trustedCount(t))
	}
	var supersededID string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT superseded_by_id::text FROM identity_action_secrets WHERE id=$1::uuid`,
		otpID).Scan(&supersededID); err != nil {
		t.Fatal(err)
	}
	if supersededID != otpID {
		t.Fatal("security recovery left a live device challenge")
	}
}

func seedOutstandingDeviceOTP(t *testing.T, f *deviceFixture, browser *browser) string {
	t.Helper()
	var deviceID string
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO identity_trusted_devices
		  (account_id, credential_digest, label, browser_family, platform_family)
		 VALUES ($1::uuid, $2, 'Firefox on Mac', 'Firefox', 'Mac')
		 RETURNING id::text`, f.account, browser.digest).Scan(&deviceID); err != nil {
		t.Fatal(err)
	}
	var otpID string
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO identity_action_secrets
		  (account_id, purpose, secret_digest, issued_at, expires_at, created_at, trusted_device_id)
		 VALUES ($1::uuid, 'DEVICE_TRUST_OTP', decode(repeat('ab', 32), 'hex'),
		         now(), now() + interval '10 minutes', now(), $2::uuid)
		 RETURNING id::text`, f.account, deviceID).Scan(&otpID); err != nil {
		t.Fatal(err)
	}
	return otpID
}
