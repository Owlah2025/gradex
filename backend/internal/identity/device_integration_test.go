//go:build integration

package identity

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

// Student trusted-device policy, end to end against the real schema.
//
// These run against PostgreSQL rather than a fake repository because most of
// what is being asserted *is* the database: the two-device limit holds because
// of a row lock, a returning browser reuses its record because of a partial
// unique index, and revoking a device stops its sessions because of a foreign
// key. None of that is observable through a fake.

const (
	deviceTestPassword = "correct device login passphrase 9"
	deviceCodeSeed     = byte(0x31)
)

// deviceFixture is one Account with the two services that govern its devices.
type deviceFixture struct {
	pool     *pgxpool.Pool
	sessions *SessionRepository
	devices  *DeviceService
	account  string
	clock    *testClock
	playback *recordingPlaybackReleaser
}

// testClock lets a test move time forward across a 24-hour cooldown without
// waiting 24 hours, and keeps every service in the fixture on one clock.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// recordingPlaybackReleaser stands in for the Redis coordinator. The identity
// package must not need Redis to be tested; what matters here is *which*
// device's lease is released, which this records exactly.
type recordingPlaybackReleaser struct {
	mu       sync.Mutex
	released []string
	failure  error
}

func (r *recordingPlaybackReleaser) ReleaseDevice(_ context.Context, _, deviceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.released = append(r.released, deviceID)
	return r.failure
}

func (r *recordingPlaybackReleaser) Released() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.released...)
}

func newDeviceFixture(t *testing.T) *deviceFixture {
	t.Helper()
	return newDeviceFixtureWithPool(t, admissionPool(t), "device-student@example.test")
}

func newDeviceFixtureWithPool(t *testing.T, pool *pgxpool.Pool, email string) *deviceFixture {
	t.Helper()
	clock := &testClock{now: time.Date(2026, time.September, 10, 9, 0, 0, 0, time.UTC)}
	releaser := &recordingPlaybackReleaser{}

	writer, err := outbox.NewWriter("v1", bytes.Repeat([]byte{0x51}, 32))
	if err != nil {
		t.Fatalf("building the device outbox writer: %v", err)
	}
	devices, err := NewDeviceService(DeviceServiceOptions{
		Pool: pool, Outbox: writer,
		Policy:   DevicePolicy{TrustedDeviceLimit: 2, ReplacementCooldown: 24 * time.Hour},
		Pepper:   config.NewSecret(string(bytes.Repeat([]byte{0x71}, 32))),
		OTPTTL:   10 * time.Minute,
		Now:      clock.Now,
		Random:   bytes.NewReader(deterministicCodeSource(deviceCodeSeed)),
		Playback: releaser,
	})
	if err != nil {
		t.Fatalf("building the device service: %v", err)
	}

	sessions, err := NewSessionRepository(SessionRepositoryOptions{
		Pool: pool, Settings: sessionTestSettings(t),
		CSRFKey: bytes.Repeat([]byte{0x61}, 32),
		Now:     clock.Now, Devices: devices,
	})
	if err != nil {
		t.Fatalf("building the session repository: %v", err)
	}

	return &deviceFixture{
		pool: pool, sessions: sessions, devices: devices,
		account: insertDeviceAccount(t, pool, email),
		clock:   clock, playback: releaser,
	}
}

func insertDeviceAccount(t *testing.T, pool *pgxpool.Pool, email string) string {
	t.Helper()
	hash, err := HashPassword(deviceTestPassword)
	if err != nil {
		t.Fatalf("hashing the fixture password: %v", err)
	}
	verified := time.Now().UTC()
	var accountID string
	if err := pool.QueryRow(context.Background(),
		`WITH account AS (
		   INSERT INTO accounts
		     (normalized_email, email, role, status, display_name, email_verified_at)
		   VALUES ($1, $1, 'STUDENT', 'ACTIVE', 'Device Student', $2)
		   RETURNING id
		 )
		 INSERT INTO password_credentials (account_id, password_hash, state)
		 SELECT id, $3, 'ACTIVE' FROM account
		 RETURNING account_id::text`,
		email, verified, hash.Expose(),
	).Scan(&accountID); err != nil {
		t.Fatalf("inserting the fixture Account: %v", err)
	}
	return accountID
}

// browser is one simulated device: a credential that persists across its
// logins, exactly as a cookie does.
type browser struct {
	credential string
	digest     string
	userAgent  string
}

func newBrowser(t *testing.T, userAgent string) *browser {
	t.Helper()
	credential, digest, err := NewOpaqueCredential()
	if err != nil {
		t.Fatalf("minting a device credential: %v", err)
	}
	return &browser{credential: credential.Expose(), digest: digest, userAgent: userAgent}
}

func (f *deviceFixture) login(t *testing.T, b *browser, requestID string) SessionGrant {
	return f.loginEmail(t, "device-student@example.test", b, requestID)
}

func (f *deviceFixture) loginEmail(t *testing.T, email string, b *browser, requestID string) SessionGrant {
	t.Helper()
	grant, err := f.sessions.Login(context.Background(), LoginRequest{
		Email: email, Password: config.NewSecret(deviceTestPassword),
		RequestID: requestID, DeviceCredentialDigest: b.digest,
		UserAgent: b.userAgent, SourceAddress: "203.0.113.7",
	})
	if err != nil {
		t.Fatalf("login from %s: %v", b.userAgent, err)
	}
	return grant
}

func (f *deviceFixture) trustedCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM identity_trusted_devices
		  WHERE account_id = $1::uuid AND trusted_at IS NOT NULL AND revoked_at IS NULL`,
		f.account,
	).Scan(&count); err != nil {
		t.Fatalf("counting trusted devices: %v", err)
	}
	return count
}

func (f *deviceFixture) deviceRows(t *testing.T) int {
	t.Helper()
	var count int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM identity_trusted_devices WHERE account_id = $1::uuid`,
		f.account,
	).Scan(&count); err != nil {
		t.Fatalf("counting device rows: %v", err)
	}
	return count
}

func (f *deviceFixture) sessionState(t *testing.T, sessionID string) (string, string) {
	t.Helper()
	var state string
	var trust string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT state::text, device_trust_state::text FROM sessions WHERE id = $1::uuid`,
		sessionID,
	).Scan(&state, &trust); err != nil {
		t.Fatalf("reading session state: %v", err)
	}
	return state, trust
}

func (f *deviceFixture) securityEvents(t *testing.T, eventType string) int {
	t.Helper()
	var count int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM identity_security_events
		  WHERE account_id = $1::uuid AND event_type = $2`,
		f.account, eventType,
	).Scan(&count); err != nil {
		t.Fatalf("counting %s events: %v", eventType, err)
	}
	return count
}

func (f *deviceFixture) deviceIDFor(t *testing.T, b *browser) string {
	t.Helper()
	var id string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT id::text FROM identity_trusted_devices
		 WHERE account_id = $1::uuid AND credential_digest = $2 AND revoked_at IS NULL`,
		f.account, b.digest).Scan(&id); err != nil {
		t.Fatalf("reading device id: %v", err)
	}
	return id
}

// A password login immediately creates a trusted device and session.
func TestFirstDeviceIsTrustedWithoutEmail(t *testing.T) {
	f := newDeviceFixture(t)
	f.devices.outbox = nil // Login must not touch the device-email writer.
	first := newBrowser(t, "Chrome/120.0 Safari/537.36 (Windows NT 10.0)")
	grant := f.login(t, first, "login-1")
	if grant.Device == nil || grant.Device.Admission != AdmitTrustedDevice ||
		grant.Device.Challenge != nil || grant.Session.DeviceTrust != DeviceTrustEstablished {
		t.Fatalf("first admission = %+v, session = %+v", grant.Device, grant.Session)
	}
	if f.trustedCount(t) != 1 {
		t.Fatalf("trusted count = %d", f.trustedCount(t))
	}
	assertNoDeviceChallenge(t, f)
}

func assertNoDeviceChallenge(t *testing.T, f *deviceFixture) {
	t.Helper()
	var codes, emails int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM identity_action_secrets WHERE account_id = $1::uuid AND purpose = 'DEVICE_TRUST_OTP'`,
		f.account).Scan(&codes); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox_events WHERE aggregate_id = $1::uuid AND event_type = 'identity.device_trust_code_requested'`,
		f.account).Scan(&emails); err != nil {
		t.Fatal(err)
	}
	if codes != 0 || emails != 0 || f.securityEvents(t, "DEVICE_TRUST_CHALLENGED") != 0 {
		t.Fatalf("device challenges = %d, emails = %d", codes, emails)
	}
}

func TestSecondDeviceAndReturningBrowser(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 (Windows)")
	second := newBrowser(t, "Safari/604.1 (iPhone)")
	firstGrant := f.login(t, first, "login-1")
	secondGrant := f.login(t, second, "login-2")
	if f.trustedCount(t) != 2 {
		t.Fatalf("trusted count = %d", f.trustedCount(t))
	}
	for _, entry := range []struct {
		grant   SessionGrant
		browser *browser
	}{{firstGrant, first}, {secondGrant, second}} {
		if entry.grant.Device.Challenge != nil || entry.grant.Session.DeviceTrust != DeviceTrustEstablished {
			t.Fatalf("device was challenged: %+v", entry.grant.Device)
		}
		resolveGrant(t, f, entry.grant, entry.browser)
	}
	before := f.deviceRows(t)
	for range 3 {
		repeat := f.login(t, first, "repeat-login")
		if repeat.Device.DeviceID != firstGrant.Device.DeviceID ||
			repeat.Session.DeviceTrust != DeviceTrustEstablished {
			t.Fatalf("returning device = %+v", repeat.Device)
		}
	}
	if f.deviceRows(t) != before || f.trustedCount(t) != 2 {
		t.Fatal("returning browser changed device slots")
	}
	assertNoDeviceChallenge(t, f)
}

func resolveGrant(t *testing.T, f *deviceFixture, grant SessionGrant, browser *browser) {
	t.Helper()
	if _, err := f.sessions.Resolve(context.Background(), SessionResolutionRequest{
		CredentialDigest:       DigestToken(grant.Credential.Expose()),
		DeviceCredentialDigest: browser.digest, UseKind: UseReadOnly,
	}); err != nil {
		t.Fatalf("session did not resolve: %v", err)
	}
}

func TestThirdAndFourthDevicesRotateNewestSlotDespiteActivity(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 (Windows)")
	second := newBrowser(t, "Safari/604.1 (iPhone)")
	third := newBrowser(t, "Firefox/121.0 (Macintosh)")
	fourth := newBrowser(t, "Edg/120.0 (Windows)")
	firstGrant := f.login(t, first, "login-1")
	firstID := f.deviceIDFor(t, first)
	secondGrant := f.login(t, second, "login-2")
	secondExtra := f.login(t, second, "login-2b")
	secondID := f.deviceIDFor(t, second)
	f.clock.Advance(time.Second)
	f.login(t, first, "touch-first")
	thirdGrant := f.login(t, third, "login-3")
	if thirdGrant.Device.EvictedDeviceID != secondID ||
		thirdGrant.Device.Challenge != nil || thirdGrant.Session.DeviceTrust != DeviceTrustEstablished {
		t.Fatalf("third admission = %+v", thirdGrant.Device)
	}
	if f.deviceIDFor(t, first) != firstID || f.trustedCount(t) != 2 {
		t.Fatal("oldest device did not survive")
	}
	for _, grant := range []SessionGrant{secondGrant, secondExtra} {
		if state, _ := f.sessionState(t, grant.Session.SessionID); state != "REVOKED" {
			t.Fatalf("evicted session state = %s", state)
		}
	}
	if state, _ := f.sessionState(t, firstGrant.Session.SessionID); state != "ACTIVE" {
		t.Fatalf("surviving session state = %s", state)
	}
	var deviceReason, sessionReason, replacementMode string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT revocation_reason::text FROM identity_trusted_devices WHERE id = $1::uuid`,
		secondID).Scan(&deviceReason); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(context.Background(),
		`SELECT revocation_reason::text FROM sessions WHERE id = $1::uuid`,
		secondGrant.Session.SessionID).Scan(&sessionReason); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(context.Background(),
		`SELECT evidence->>'replacement_mode' FROM identity_security_events
		 WHERE account_id = $1::uuid AND event_type = 'DEVICE_TRUSTED'
		 AND evidence->>'device_id' = $2`, f.account, thirdGrant.Device.DeviceID).Scan(&replacementMode); err != nil {
		t.Fatal(err)
	}
	if deviceReason != "AUTO_REPLACED" || sessionReason != "AUTO_REPLACED" || replacementMode != "AUTOMATIC" {
		t.Fatalf("rotation reasons = %s, %s, %s", deviceReason, sessionReason, replacementMode)
	}
	if _, err := f.sessions.Resolve(context.Background(), SessionResolutionRequest{
		CredentialDigest:       DigestToken(secondGrant.Credential.Expose()),
		DeviceCredentialDigest: second.digest, UseKind: UseReadOnly,
	}); err == nil {
		t.Fatal("evicted device session still resolves")
	}
	thirdID := f.deviceIDFor(t, third)
	fourthGrant := f.login(t, fourth, "login-4")
	if fourthGrant.Device.EvictedDeviceID != thirdID || f.trustedCount(t) != 2 {
		t.Fatalf("fourth admission = %+v", fourthGrant.Device)
	}
	released := f.playback.Released()
	if len(released) != 2 || released[0] != secondID || released[1] != thirdID {
		t.Fatalf("released leases = %v", released)
	}
	resolveGrant(t, f, firstGrant, first)
	resolveGrant(t, f, fourthGrant, fourth)
	assertNoDeviceChallenge(t, f)
}

func TestAutomaticRotationIgnoresManualCooldown(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 (Windows)")
	second := newBrowser(t, "Safari/604.1 (iPhone)")
	third := newBrowser(t, "Firefox/121.0 (Macintosh)")
	f.login(t, first, "login-1")
	f.login(t, second, "login-2")
	if _, err := f.pool.Exec(context.Background(),
		`INSERT INTO identity_device_replacement_state (account_id, last_replacement_at)
		 VALUES ($1::uuid, $2)`, f.account, f.clock.Now()); err != nil {
		t.Fatal(err)
	}
	f.login(t, third, "login-3")
	state, err := f.devices.replacementState(context.Background(), f.account)
	if err != nil || state.LastReplacementAt == nil || !state.LastReplacementAt.Equal(f.clock.Now()) {
		t.Fatalf("automatic rotation changed cooldown: %+v, %v", state, err)
	}
}

func TestConcurrentNewLoginsKeepTwoDeviceLimit(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 (Windows)")
	f.login(t, first, "login-1")
	browsers := []*browser{
		newBrowser(t, "Safari/604.1 (iPhone)"),
		newBrowser(t, "Firefox/121.0 (Macintosh)"),
	}
	var wg sync.WaitGroup
	failures := make(chan error, len(browsers))
	for _, b := range browsers {
		wg.Add(1)
		go func(b *browser) {
			defer wg.Done()
			_, err := f.sessions.Login(context.Background(), LoginRequest{
				Email: "device-student@example.test", Password: config.NewSecret(deviceTestPassword),
				RequestID:              "concurrent-" + b.digest[:12],
				DeviceCredentialDigest: b.digest, UserAgent: b.userAgent,
			})
			failures <- err
		}(b)
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatalf("concurrent login: %v", err)
		}
	}
	if f.trustedCount(t) != 2 || f.deviceIDFor(t, first) == "" {
		t.Fatalf("concurrent trusted count = %d", f.trustedCount(t))
	}
	var newerCount int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM identity_trusted_devices WHERE account_id = $1::uuid
		 AND revoked_at IS NULL AND credential_digest = ANY($2)`,
		f.account, []string{browsers[0].digest, browsers[1].digest}).Scan(&newerCount); err != nil {
		t.Fatal(err)
	}
	if newerCount != 1 {
		t.Fatalf("newer live devices = %d, want 1", newerCount)
	}
}

func TestAutoRotationRollbackKeepsDevicesAndPlayback(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 (Windows)")
	second := newBrowser(t, "Safari/604.1 (iPhone)")
	third := newBrowser(t, "Firefox/121.0 (Macintosh)")
	f.login(t, first, "login-1")
	f.login(t, second, "login-2")
	secondID := f.deviceIDFor(t, second)
	tx, err := f.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	revision, err := lockAccountForDevicePolicy(context.Background(), tx, f.account)
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.devices.admitInTransaction(context.Background(), tx, DeviceAdmissionRequest{
		AccountID: f.account, Revision: revision, Role: RoleStudent,
		PresentedDigest: third.digest, UserAgent: third.userAgent, RequestID: "rollback-admission",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.EvictedDeviceID != secondID {
		t.Fatalf("evicted = %s", result.EvictedDeviceID)
	}
	if _, err := tx.Exec(context.Background(), `SELECT 1/0`); err == nil {
		t.Fatal("expected a write-transaction failure before commit")
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.trustedCount(t) != 2 || f.deviceRows(t) != 2 ||
		f.deviceIDFor(t, second) != secondID || len(f.playback.Released()) != 0 {
		t.Fatal("rollback changed durable devices or released playback")
	}
	var audits int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events WHERE action = 'AUTO_DEVICE_REPLACED' AND correlation_id = 'rollback-admission'`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 0 {
		t.Fatal("rollback left automatic replacement audit evidence")
	}
}

func TestTrustedSessionRequiresMatchingDeviceCredential(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 (Windows)")
	other := newBrowser(t, "Safari/604.1 (iPhone)")
	grant := f.login(t, first, "login")
	for _, digest := range []string{"", other.digest} {
		view, err := f.sessions.Resolve(context.Background(), SessionResolutionRequest{
			CredentialDigest:       DigestToken(grant.Credential.Expose()),
			DeviceCredentialDigest: digest, UseKind: UseReadOnly,
		})
		if err != nil {
			t.Fatal(err)
		}
		if view.Session.DeviceTrust != DeviceTrustPending {
			t.Fatalf("wrong-device trust = %s", view.Session.DeviceTrust)
		}
	}
	resolveGrant(t, f, grant, first)
}

func TestManualRemovalAndAdminRevocationStillScopeSessions(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 (Windows)")
	second := newBrowser(t, "Safari/604.1 (iPhone)")
	firstGrant := f.login(t, first, "login-1")
	secondGrant := f.login(t, second, "login-2")
	firstID := f.deviceIDFor(t, first)
	secondID := f.deviceIDFor(t, second)
	if err := f.devices.Remove(context.Background(), RemoveRequest{
		AccountID: f.account, DeviceID: secondID, RequestID: "remove-second",
	}); err != nil {
		t.Fatal(err)
	}
	if state, _ := f.sessionState(t, secondGrant.Session.SessionID); state != "REVOKED" {
		t.Fatalf("manually removed session = %s", state)
	}
	resolveGrant(t, f, firstGrant, first)
	if err := f.devices.Remove(context.Background(), RemoveRequest{
		AccountID: f.account, DeviceID: firstID, RequestID: "remove-first-too-soon",
	}); !errors.Is(err, ErrDeviceReplacementCooldown) {
		t.Fatalf("manual cooldown = %v", err)
	}
	operator := insertDeviceAccount(t, f.pool, "device-operator@example.test")
	if err := f.devices.AdminRevokeDevice(context.Background(), AdminDeviceCommand{
		AccountID: f.account, DeviceID: firstID, ActorID: operator, RequestID: "admin-revoke",
	}); err != nil {
		t.Fatal(err)
	}
	if state, _ := f.sessionState(t, firstGrant.Session.SessionID); state != "REVOKED" {
		t.Fatalf("admin-revoked session = %s", state)
	}
	if f.securityEvents(t, "ADMIN_DEVICE_REVOKED") != 1 {
		t.Fatal("missing admin audit event")
	}
}

func TestAdminRevokeAllStillClearsDevices(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 (Windows)")
	second := newBrowser(t, "Safari/604.1 (iPhone)")
	firstGrant := f.login(t, first, "login-1")
	secondGrant := f.login(t, second, "login-2")
	operator := insertDeviceAccount(t, f.pool, "device-operator-all@example.test")
	revoked, err := f.devices.AdminRevokeAllDevices(context.Background(), AdminDeviceCommand{
		AccountID: f.account, ActorID: operator, RequestID: "admin-revoke-all",
	})
	if err != nil || revoked != 2 || f.trustedCount(t) != 0 {
		t.Fatalf("revoke all: %d, %v", revoked, err)
	}
	for _, grant := range []SessionGrant{firstGrant, secondGrant} {
		if state, _ := f.sessionState(t, grant.Session.SessionID); state != "REVOKED" {
			t.Fatalf("revoke-all session state = %s", state)
		}
	}
}

func TestPasswordResetStillRevokesDevices(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 (Windows)")
	second := newBrowser(t, "Safari/604.1 (iPhone)")
	f.login(t, first, "login-1")
	f.login(t, second, "login-2")
	recovery := recoveryService(t, f.pool, f.clock.Now(), 0x91)
	recovery.AttachDevices(f.devices)
	if err := recovery.RequestPasswordReset(context.Background(), PasswordResetRequest{
		Email: "device-student@example.test", RequestID: "reset",
	}); err != nil {
		t.Fatal(err)
	}
	if err := recovery.CompletePasswordReset(context.Background(), PasswordResetCompletion{
		Token: deterministicBearer(0x91), Password: config.NewSecret("quiet lantern beside seventeen rivers"),
		RequestID: "complete-reset",
	}); err != nil {
		t.Fatal(err)
	}
	if f.trustedCount(t) != 0 || len(f.playback.Released()) != 2 {
		t.Fatal("password reset did not revoke devices and release playback")
	}
}
