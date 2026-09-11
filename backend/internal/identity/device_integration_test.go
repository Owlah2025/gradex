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
}

func (r *recordingPlaybackReleaser) ReleaseDevice(_ context.Context, _, deviceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.released = append(r.released, deviceID)
	return nil
}

func (r *recordingPlaybackReleaser) Released() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.released...)
}

func newDeviceFixture(t *testing.T) *deviceFixture {
	t.Helper()
	pool := admissionPool(t)
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
		account: insertDeviceAccount(t, pool, "device-student@example.test"),
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
	t.Helper()
	grant, err := f.sessions.Login(context.Background(), LoginRequest{
		Email: "device-student@example.test", Password: config.NewSecret(deviceTestPassword),
		RequestID: requestID, DeviceCredentialDigest: b.digest,
		UserAgent: b.userAgent, SourceAddress: "203.0.113.7",
	})
	if err != nil {
		t.Fatalf("login from %s: %v", b.userAgent, err)
	}
	return grant
}

// completeTrust answers the live challenge with the code the deterministic
// source produced for it.
func (f *deviceFixture) completeTrust(
	t *testing.T, b *browser, grant SessionGrant, draw int, replace string,
) (DeviceTrustResult, error) {
	t.Helper()
	if grant.Device == nil || grant.Device.Challenge == nil {
		t.Fatal("no device challenge was issued")
	}
	return f.devices.CompleteTrust(context.Background(), DeviceTrustRequest{
		AccountID:       f.account,
		SessionID:       grant.Session.SessionID,
		PresentedDigest: b.digest,
		ChallengeID:     grant.Device.Challenge.ChallengeID,
		Code:            deterministicCode(deviceCodeSeed, draw),
		ReplaceDeviceID: replace,
		RequestID:       "request-trust",
	})
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

// Backend requirement 1 and 2: the first browser on an Account is not trusted
// by arriving. It authenticates, and is then challenged.
func TestFirstDeviceRequiresAnEmailedCodeBeforeItIsTrusted(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/120.0 Safari/537.36")

	grant := f.login(t, first, "request-login-1")
	if grant.Device == nil || grant.Device.Admission != AdmitNewDeviceWithSlot {
		t.Fatalf("admission = %+v, want OTP_REQUIRED", grant.Device)
	}
	if grant.Session.DeviceTrust != DeviceTrustPending {
		t.Fatalf("session trust = %s, want PENDING_DEVICE_TRUST", grant.Session.DeviceTrust)
	}
	if f.trustedCount(t) != 0 {
		t.Fatal("a device became trusted before its code was proven")
	}
	// The masked address tells the Student which mailbox to check without
	// disclosing the address itself.
	if masked := grant.Device.Challenge.MaskedEmail; masked == "" || masked == "device-student@example.test" {
		t.Fatalf("masked email = %q", masked)
	}

	if _, err := f.completeTrust(t, first, grant, 0, ""); err != nil {
		t.Fatalf("completing trust: %v", err)
	}
	if f.trustedCount(t) != 1 {
		t.Fatalf("trusted devices = %d, want 1", f.trustedCount(t))
	}
	if _, trust := f.sessionState(t, grant.Session.SessionID); trust != string(DeviceTrustEstablished) {
		t.Fatalf("session trust after completion = %s, want TRUSTED", trust)
	}
	if f.securityEvents(t, "DEVICE_TRUSTED") != 1 {
		t.Fatal("trusting a device left no security event")
	}
}

// Backend requirement 12 (added): an untrusted browser holds a session, but not
// an ordinary one. It cannot reach protected learning until trust completes.
func TestAnUntrustedBrowserCannotReachProtectedLearning(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 Safari/537.36 (Windows NT 10.0)")
	grant := f.login(t, first, "request-login-untrusted")

	principal := Principal{
		AccountID: f.account, Role: RoleStudent,
		Status: StatusActive, CredentialState: CredentialActive,
	}
	if AuthorizeSessionDevice(principal, grant.Session.DeviceTrust, CapLearningAccess).Allowed {
		t.Fatal("an untrusted browser reached protected learning")
	}
	// And it can still finish, or give up, which is the whole point of the
	// narrowed session rather than a refused login.
	for _, capability := range []Capability{CapDeviceManagement, CapSessionTerminate} {
		if !AuthorizeSessionDevice(principal, grant.Session.DeviceTrust, capability).Allowed {
			t.Fatalf("an untrusted browser could not use %s", capability)
		}
	}

	if _, err := f.completeTrust(t, first, grant, 0, ""); err != nil {
		t.Fatalf("completing trust: %v", err)
	}
	view, err := f.sessions.Resolve(
		context.Background(), SessionResolutionRequest{
			CredentialDigest: DigestToken(grant.Credential.Expose()), DeviceCredentialDigest: first.digest,
			UseKind: UseReadOnly, RequestID: "request-resolve",
		})
	if err != nil {
		t.Fatalf("resolving after trust: %v", err)
	}
	if !AuthorizeSessionDevice(principal, view.Session.DeviceTrust, CapLearningAccess).Allowed {
		t.Fatal("a trusted browser still could not reach protected learning")
	}
}

// Backend requirement 3: a second browser is challenged and then trusted, and
// requirement 11: both stay authenticated afterwards.
func TestSecondDeviceIsTrustedAndBothRemainAuthenticated(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 Safari/537.36 (Windows NT 10.0)")
	second := newBrowser(t, "Safari/604.1 (iPhone; CPU iPhone OS 17_0 like Mac OS X)")

	firstGrant := f.login(t, first, "request-login-1")
	if _, err := f.completeTrust(t, first, firstGrant, 0, ""); err != nil {
		t.Fatalf("trusting the first device: %v", err)
	}
	secondGrant := f.login(t, second, "request-login-2")
	if secondGrant.Device.Admission != AdmitNewDeviceWithSlot {
		t.Fatalf("second device admission = %s, want OTP_REQUIRED", secondGrant.Device.Admission)
	}
	if _, err := f.completeTrust(t, second, secondGrant, 1, ""); err != nil {
		t.Fatalf("trusting the second device: %v", err)
	}
	if f.trustedCount(t) != 2 {
		t.Fatalf("trusted devices = %d, want 2", f.trustedCount(t))
	}

	// Both sessions resolve, on their own devices, at the same time.
	for name, resolved := range map[string]struct {
		grant  SessionGrant
		digest string
	}{
		"first":  {firstGrant, first.digest},
		"second": {secondGrant, second.digest},
	} {
		view, err := f.sessions.Resolve(
			context.Background(), SessionResolutionRequest{
				CredentialDigest: DigestToken(resolved.grant.Credential.Expose()), DeviceCredentialDigest: resolved.digest,
				UseKind: UseReadOnly, RequestID: "request-resolve",
			})
		if err != nil {
			t.Fatalf("%s session did not resolve: %v", name, err)
		}
		if view.Session.DeviceTrust != DeviceTrustEstablished {
			t.Fatalf("%s session trust = %s, want TRUSTED", name, view.Session.DeviceTrust)
		}
	}
}

func TestTrustedStudentSessionRequiresItsMatchingDeviceCredential(t *testing.T) {
	f := newDeviceFixture(t)
	trustedBrowser := newBrowser(t, "Chrome/120.0 Safari/537.36 (Windows NT 10.0)")
	otherBrowser := newBrowser(t, "Safari/604.1 (iPhone; CPU iPhone OS 17_0 like Mac OS X)")
	grant := f.login(t, trustedBrowser, "request-login")
	if _, err := f.completeTrust(t, trustedBrowser, grant, 0, ""); err != nil {
		t.Fatal(err)
	}

	for name, digest := range map[string]string{
		"missing": "",
		"wrong":   otherBrowser.digest,
	} {
		t.Run(name, func(t *testing.T) {
			view, err := f.sessions.Resolve(context.Background(), SessionResolutionRequest{
				CredentialDigest: DigestToken(grant.Credential.Expose()), DeviceCredentialDigest: digest,
				UseKind: UseReadOnly, RequestID: "request-" + name,
			})
			if err != nil {
				t.Fatalf("session authentication failed instead of narrowing: %v", err)
			}
			if view.Session.DeviceTrust != DeviceTrustPending || view.Session.TrustedDeviceID != "" {
				t.Fatalf("trust = %s/%q, want pending with no device", view.Session.DeviceTrust, view.Session.TrustedDeviceID)
			}
		})
	}

	view, err := f.sessions.Resolve(context.Background(), SessionResolutionRequest{
		CredentialDigest: DigestToken(grant.Credential.Expose()), DeviceCredentialDigest: trustedBrowser.digest,
		UseKind: UseReadOnly, RequestID: "request-correct",
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Session.DeviceTrust != DeviceTrustEstablished || view.Session.TrustedDeviceID == "" {
		t.Fatalf("matching device trust = %s/%q", view.Session.DeviceTrust, view.Session.TrustedDeviceID)
	}
}

// Backend requirement 8: a returning trusted browser reuses its record rather
// than accumulating a new one on every login.
func TestAReturningTrustedBrowserReusesItsRecord(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 Safari/537.36 (Windows NT 10.0)")

	grant := f.login(t, first, "request-login-1")
	if _, err := f.completeTrust(t, first, grant, 0, ""); err != nil {
		t.Fatalf("trusting: %v", err)
	}
	before := f.deviceRows(t)

	for attempt := 0; attempt < 3; attempt++ {
		repeat := f.login(t, first, "request-login-repeat")
		if repeat.Device.Admission != AdmitTrustedDevice {
			t.Fatalf("returning login admission = %s, want TRUSTED", repeat.Device.Admission)
		}
		if repeat.Session.DeviceTrust != DeviceTrustEstablished {
			t.Fatalf("returning session trust = %s, want TRUSTED", repeat.Session.DeviceTrust)
		}
		if repeat.Device.Challenge != nil {
			t.Fatal("a returning trusted browser was challenged again")
		}
	}
	if after := f.deviceRows(t); after != before {
		t.Fatalf("device rows grew from %d to %d across repeat logins", before, after)
	}

	// A browser update rewrites the label and changes nothing else.
	first.userAgent = "Chrome/131.0 Safari/537.36 (Windows NT 10.0)"
	if repeat := f.login(t, first, "request-login-updated"); repeat.Device.Admission != AdmitTrustedDevice {
		t.Fatal("a browser update made a trusted device look new")
	}
	if after := f.deviceRows(t); after != before {
		t.Fatalf("a browser update created a device row: %d rows, want %d", after, before)
	}
}

// Backend requirement 4: a third browser is never silently trusted, and
// requirement 5: it may replace a device the Student names.
func TestThirdDeviceMustReplaceOneTheStudentChooses(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 Safari/537.36 (Windows NT 10.0)")
	second := newBrowser(t, "Safari/604.1 (iPhone; CPU iPhone OS 17_0 like Mac OS X)")
	third := newBrowser(t, "Firefox/121.0 (Macintosh; Intel Mac OS X 10_15_7)")

	firstGrant := f.login(t, first, "request-login-1")
	if _, err := f.completeTrust(t, first, firstGrant, 0, ""); err != nil {
		t.Fatalf("trusting the first device: %v", err)
	}
	firstDeviceID := f.deviceIDFor(t, first)

	secondGrant := f.login(t, second, "request-login-2")
	if _, err := f.completeTrust(t, second, secondGrant, 1, ""); err != nil {
		t.Fatalf("trusting the second device: %v", err)
	}

	thirdGrant := f.login(t, third, "request-login-3")
	if thirdGrant.Device.Admission != AdmitNewDeviceAtLimit {
		t.Fatalf("third device admission = %s, want LIMIT_REACHED", thirdGrant.Device.Admission)
	}
	if f.securityEvents(t, "DEVICE_LIMIT_REACHED") != 1 {
		t.Fatal("reaching the limit left no security event")
	}
	// The Student is still challenged first: the device list is only shown
	// after the mailbox is proven.
	if thirdGrant.Device.Challenge == nil {
		t.Fatal("the third device was refused without a challenge")
	}
	// Answering the code without naming a device to remove is refused, and
	// nothing is trusted.
	if _, err := f.completeTrust(t, third, thirdGrant, 2, ""); !errors.Is(err, ErrDeviceLimitReached) {
		t.Fatalf("third device without a replacement = %v, want ErrDeviceLimitReached", err)
	}
	if f.trustedCount(t) != 2 {
		t.Fatalf("trusted devices = %d after a refused third, want 2", f.trustedCount(t))
	}

	// Naming one of their own devices completes the swap atomically.
	retry := f.login(t, third, "request-login-3-retry")
	result, err := f.completeTrust(t, third, retry, 3, firstDeviceID)
	if err != nil {
		t.Fatalf("replacing the first device: %v", err)
	}
	if result.ReplacedDeviceID != firstDeviceID {
		t.Fatalf("replaced = %q, want %q", result.ReplacedDeviceID, firstDeviceID)
	}
	if f.trustedCount(t) != 2 {
		t.Fatalf("trusted devices = %d after the swap, want 2", f.trustedCount(t))
	}
	if f.securityEvents(t, "DEVICE_REVOKED") != 1 {
		t.Fatal("the replacement left no revocation event")
	}
	// The replaced device's lease is released; the other device's is not.
	released := f.playback.Released()
	if len(released) != 1 || released[0] != firstDeviceID {
		t.Fatalf("released leases = %v, want only the replaced device", released)
	}
}

func (f *deviceFixture) deviceIDFor(t *testing.T, b *browser) string {
	t.Helper()
	var id string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT id::text FROM identity_trusted_devices
		  WHERE account_id = $1::uuid AND credential_digest = $2 AND revoked_at IS NULL`,
		f.account, b.digest,
	).Scan(&id); err != nil {
		t.Fatalf("reading the device identity: %v", err)
	}
	return id
}

// Backend requirement 6: the replacement cooldown is enforced, and
// requirement 11 of the policy notes: it lifts on its own rather than locking
// the Account out.
func TestReplacementCooldownIsEnforcedAndThenLifts(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 Safari/537.36 (Windows NT 10.0)")
	second := newBrowser(t, "Safari/604.1 (iPhone; CPU iPhone OS 17_0 like Mac OS X)")
	third := newBrowser(t, "Firefox/121.0 (Macintosh; Intel Mac OS X 10_15_7)")
	fourth := newBrowser(t, "Edg/120.0 Chrome/120.0 (Windows NT 10.0)")

	firstGrant := f.login(t, first, "request-login-1")
	if _, err := f.completeTrust(t, first, firstGrant, 0, ""); err != nil {
		t.Fatalf("trusting the first device: %v", err)
	}
	firstID := f.deviceIDFor(t, first)
	secondGrant := f.login(t, second, "request-login-2")
	if _, err := f.completeTrust(t, second, secondGrant, 1, ""); err != nil {
		t.Fatalf("trusting the second device: %v", err)
	}
	secondID := f.deviceIDFor(t, second)

	thirdGrant := f.login(t, third, "request-login-3")
	if _, err := f.completeTrust(t, third, thirdGrant, 2, firstID); err != nil {
		t.Fatalf("the first replacement: %v", err)
	}

	// A second replacement, minutes later, is refused.
	f.clock.Advance(5 * time.Minute)
	fourthGrant := f.login(t, fourth, "request-login-4")
	_, err := f.completeTrust(t, fourth, fourthGrant, 3, secondID)
	if !errors.Is(err, ErrDeviceReplacementCooldown) {
		t.Fatalf("a second replacement inside the window = %v, want a cooldown refusal", err)
	}
	if f.securityEvents(t, "DEVICE_REPLACEMENT_BLOCKED") != 1 {
		t.Fatal("the blocked replacement left no security event")
	}
	if f.trustedCount(t) != 2 {
		t.Fatalf("trusted devices = %d after a blocked replacement, want 2", f.trustedCount(t))
	}

	// A day later it is allowed again. The cooldown is friction, not a lock.
	f.clock.Advance(24 * time.Hour)
	retry := f.login(t, fourth, "request-login-4-retry")
	if _, err := f.completeTrust(t, fourth, retry, 4, secondID); err != nil {
		t.Fatalf("replacing after the cooldown lifted: %v", err)
	}
}

// Backend requirement 7: an operator can release an Account that is stuck,
// without the Student having to reset their password.
func TestAdminOverrideReleasesTheCooldownAndRevokesDevices(t *testing.T) {
	f := newDeviceFixture(t)
	operator := insertDeviceAccount(t, f.pool, "device-operator@example.test")
	first := newBrowser(t, "Chrome/120.0 Safari/537.36 (Windows NT 10.0)")
	second := newBrowser(t, "Safari/604.1 (iPhone; CPU iPhone OS 17_0 like Mac OS X)")
	third := newBrowser(t, "Firefox/121.0 (Macintosh; Intel Mac OS X 10_15_7)")

	firstGrant := f.login(t, first, "request-login-1")
	if _, err := f.completeTrust(t, first, firstGrant, 0, ""); err != nil {
		t.Fatalf("trusting the first device: %v", err)
	}
	firstID := f.deviceIDFor(t, first)
	secondGrant := f.login(t, second, "request-login-2")
	if _, err := f.completeTrust(t, second, secondGrant, 1, ""); err != nil {
		t.Fatalf("trusting the second device: %v", err)
	}
	secondID := f.deviceIDFor(t, second)

	thirdGrant := f.login(t, third, "request-login-3")
	if _, err := f.completeTrust(t, third, thirdGrant, 2, firstID); err != nil {
		t.Fatalf("the first replacement: %v", err)
	}

	// The Student is now inside the cooldown. The operator releases it.
	command := AdminDeviceCommand{
		AccountID: f.account, DeviceID: secondID,
		ActorID: operator, RequestID: "request-admin-1",
	}
	if err := f.devices.AdminResetReplacementCooldown(context.Background(), command); err != nil {
		t.Fatalf("resetting the cooldown: %v", err)
	}
	if f.securityEvents(t, "ADMIN_DEVICE_COOLDOWN_RESET") != 1 {
		t.Fatal("the operator reset left no security event")
	}

	// An operator revocation of a single device is audited separately from the
	// Student's own removals.
	if err := f.devices.AdminRevokeDevice(context.Background(), command); err != nil {
		t.Fatalf("operator revocation: %v", err)
	}
	if f.securityEvents(t, "ADMIN_DEVICE_REVOKED") != 1 {
		t.Fatalf("operator revocation events = %d, want 1", f.securityEvents(t, "ADMIN_DEVICE_REVOKED"))
	}
	if f.trustedCount(t) != 1 {
		t.Fatalf("trusted devices after the operator revoke = %d, want 1", f.trustedCount(t))
	}

	// Revoke-all is broader and explicitly clears the cooldown too.
	revoked, err := f.devices.AdminRevokeAllDevices(context.Background(), AdminDeviceCommand{
		AccountID: f.account, ActorID: operator, RequestID: "request-admin-2",
	})
	if err != nil {
		t.Fatalf("operator revoke-all: %v", err)
	}
	if revoked == 0 || f.trustedCount(t) != 0 {
		t.Fatalf("revoke-all removed %d devices, %d remain trusted", revoked, f.trustedCount(t))
	}
	// Nothing in the audit trail carries a credential.
	assertNoSecretsInDeviceEvidence(t, f)
}

func assertNoSecretsInDeviceEvidence(t *testing.T, f *deviceFixture) {
	t.Helper()
	rows, err := f.pool.Query(context.Background(),
		`SELECT event_type, evidence::text FROM identity_security_events WHERE account_id = $1::uuid`,
		f.account,
	)
	if err != nil {
		t.Fatalf("reading security evidence: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var eventType, evidence string
		if err := rows.Scan(&eventType, &evidence); err != nil {
			t.Fatalf("scanning security evidence: %v", err)
		}
		for _, forbidden := range []string{"credential", "digest", "secret", "code"} {
			if containsFold(evidence, forbidden) {
				t.Fatalf("%s evidence mentions %q: %s", eventType, forbidden, evidence)
			}
		}
	}
}

func containsFold(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		bytes.Contains(bytes.ToLower([]byte(haystack)), bytes.ToLower([]byte(needle)))
}

// Backend requirements 9 and 10: revoking a device ends that device's sessions
// and leaves the other device signed in.
func TestRevokingADeviceEndsOnlyItsOwnSessions(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 Safari/537.36 (Windows NT 10.0)")
	second := newBrowser(t, "Safari/604.1 (iPhone; CPU iPhone OS 17_0 like Mac OS X)")

	firstGrant := f.login(t, first, "request-login-1")
	if _, err := f.completeTrust(t, first, firstGrant, 0, ""); err != nil {
		t.Fatalf("trusting the first device: %v", err)
	}
	firstID := f.deviceIDFor(t, first)
	secondGrant := f.login(t, second, "request-login-2")
	if _, err := f.completeTrust(t, second, secondGrant, 1, ""); err != nil {
		t.Fatalf("trusting the second device: %v", err)
	}

	// A second session family on the first device, to prove revocation is not
	// limited to the family that happened to trust it.
	extraGrant := f.login(t, first, "request-login-1b")
	if extraGrant.Session.DeviceTrust != DeviceTrustEstablished {
		t.Fatalf("a returning trusted browser got %s", extraGrant.Session.DeviceTrust)
	}

	if err := f.devices.Remove(context.Background(), RemoveRequest{
		AccountID: f.account, DeviceID: firstID, RequestID: "request-remove",
	}); err != nil {
		t.Fatalf("removing the first device: %v", err)
	}

	for _, grant := range []SessionGrant{firstGrant, extraGrant} {
		if _, err := f.sessions.Resolve(
			context.Background(), SessionResolutionRequest{
				CredentialDigest: DigestToken(grant.Credential.Expose()), DeviceCredentialDigest: first.digest,
				UseKind: UseReadOnly, RequestID: "request-resolve",
			},
		); err == nil {
			t.Fatal("a session on the revoked device still resolved")
		}
	}
	view, err := f.sessions.Resolve(
		context.Background(), SessionResolutionRequest{
			CredentialDigest: DigestToken(secondGrant.Credential.Expose()), DeviceCredentialDigest: second.digest,
			UseKind: UseReadOnly, RequestID: "request-resolve",
		})
	if err != nil {
		t.Fatalf("the other device was signed out by an unrelated revocation: %v", err)
	}
	if view.Session.DeviceTrust != DeviceTrustEstablished {
		t.Fatalf("the other device's trust = %s, want TRUSTED", view.Session.DeviceTrust)
	}

	// Removing one device must not spend the global session epoch, which is
	// what logout-all and suspension use.
	var epoch int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT session_epoch FROM accounts WHERE id = $1::uuid`, f.account,
	).Scan(&epoch); err != nil {
		t.Fatalf("reading the session epoch: %v", err)
	}
	if epoch != 1 {
		t.Fatalf("session epoch = %d, want it untouched at 1", epoch)
	}
}

// Requirement 2 of the corrections: a session that predates device policy is
// not logged out, cannot reach protected learning, and is bound by adopting a
// device.
func TestALegacySessionIsAdoptedRatherThanLoggedOut(t *testing.T) {
	f := newDeviceFixture(t)
	legacy := newBrowser(t, "Chrome/120.0 Safari/537.36 (Windows NT 10.0)")

	grant := f.login(t, legacy, "request-login-legacy")
	if _, err := f.completeTrust(t, legacy, grant, 0, ""); err != nil {
		t.Fatalf("trusting: %v", err)
	}
	// Rewind this family to exactly the shape the migration leaves behind: a
	// live session with no device binding at all.
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE sessions SET trusted_device_id = NULL, device_trust_state = 'LEGACY_UNBOUND'
		  WHERE id = $1::uuid`, grant.Session.SessionID,
	); err != nil {
		t.Fatalf("simulating a pre-deployment session: %v", err)
	}

	// It still authenticates — no mass logout.
	view, err := f.sessions.Resolve(
		context.Background(), SessionResolutionRequest{
			CredentialDigest: DigestToken(grant.Credential.Expose()), DeviceCredentialDigest: legacy.digest,
			UseKind: UseReadOnly, RequestID: "request-resolve",
		})
	if err != nil {
		t.Fatalf("a pre-deployment session was logged out: %v", err)
	}
	if view.Session.DeviceTrust != DeviceTrustLegacyUnbound {
		t.Fatalf("legacy trust = %s, want LEGACY_UNBOUND", view.Session.DeviceTrust)
	}

	// It is refused protected learning, and only protected learning.
	principal := Principal{
		AccountID: f.account, Role: RoleStudent,
		Status: StatusActive, CredentialState: CredentialActive,
	}
	decision := AuthorizeSessionDevice(principal, view.Session.DeviceTrust, CapLearningAccess)
	if decision.Allowed || decision.Reason != DenyDeviceAdoptionRequired {
		t.Fatalf("legacy learning decision = %+v, want DEVICE_ADOPTION_REQUIRED", decision)
	}
	if !AuthorizeSessionDevice(principal, view.Session.DeviceTrust, CapDeviceManagement).Allowed {
		t.Fatal("a legacy session could not reach its own device settings")
	}

	// Adopting binds it. The browser already holds a live trusted record for
	// this Account, so no second code is mailed for the same mailbox.
	result, err := f.devices.AdoptForSession(
		context.Background(), f.account, grant.Session.SessionID,
		DeviceContext{CredentialDigest: legacy.digest, UserAgent: legacy.userAgent, SourceAddress: "203.0.113.7"},
		"request-adopt",
	)
	if err != nil {
		t.Fatalf("adopting: %v", err)
	}
	if result.Admission != AdmitTrustedDevice {
		t.Fatalf("adoption admission = %s, want TRUSTED", result.Admission)
	}
	if f.securityEvents(t, "DEVICE_ADOPTED_LEGACY_SESSION") != 1 {
		t.Fatal("adoption left no security event")
	}
	adopted, err := f.sessions.Resolve(
		context.Background(), SessionResolutionRequest{
			CredentialDigest: DigestToken(grant.Credential.Expose()), DeviceCredentialDigest: legacy.digest,
			UseKind: UseReadOnly, RequestID: "request-resolve",
		})
	if err != nil {
		t.Fatalf("resolving after adoption: %v", err)
	}
	if adopted.Session.DeviceTrust != DeviceTrustEstablished {
		t.Fatalf("adopted trust = %s, want TRUSTED", adopted.Session.DeviceTrust)
	}
	if !AuthorizeSessionDevice(principal, adopted.Session.DeviceTrust, CapLearningAccess).Allowed {
		t.Fatal("an adopted session still could not reach protected learning")
	}
}

// A legacy session on a browser the Account has never trusted goes through the
// ordinary challenge, and keeps its non-protected authority while it waits.
func TestALegacySessionOnAnUnknownBrowserIsChallenged(t *testing.T) {
	f := newDeviceFixture(t)
	known := newBrowser(t, "Chrome/120.0 Safari/537.36 (Windows NT 10.0)")
	unknown := newBrowser(t, "Firefox/121.0 (Macintosh; Intel Mac OS X 10_15_7)")

	grant := f.login(t, known, "request-login-1")
	if _, err := f.completeTrust(t, known, grant, 0, ""); err != nil {
		t.Fatalf("trusting: %v", err)
	}
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE sessions SET trusted_device_id = NULL, device_trust_state = 'LEGACY_UNBOUND'
		  WHERE id = $1::uuid`, grant.Session.SessionID,
	); err != nil {
		t.Fatalf("simulating a pre-deployment session: %v", err)
	}

	result, err := f.devices.AdoptForSession(
		context.Background(), f.account, grant.Session.SessionID,
		DeviceContext{CredentialDigest: unknown.digest, UserAgent: unknown.userAgent},
		"request-adopt-unknown",
	)
	if err != nil {
		t.Fatalf("adopting an unknown browser: %v", err)
	}
	if result.Admission != AdmitNewDeviceWithSlot || result.Challenge == nil {
		t.Fatalf("adoption of an unknown browser = %+v, want a challenge", result)
	}
	// The waiting session keeps its ordinary authority rather than being
	// downgraded mid-visit.
	if _, trust := f.sessionState(t, grant.Session.SessionID); trust != string(DeviceTrustLegacyUnbound) {
		t.Fatalf("session trust while awaiting a code = %s, want it unchanged", trust)
	}
}

// Requirement 10 of the corrections: two simultaneous trust transactions cannot
// push an Account past the limit. The Account row lock is what serializes them.
func TestConcurrentTrustCannotExceedTheDeviceLimit(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 Safari/537.36 (Windows NT 10.0)")
	firstGrant := f.login(t, first, "request-login-1")
	if _, err := f.completeTrust(t, first, firstGrant, 0, ""); err != nil {
		t.Fatalf("trusting the first device: %v", err)
	}

	// Two more browsers race for the single remaining slot. Only one may take
	// it; the other must be told the Account is full rather than becoming a
	// third trusted device.
	second := newBrowser(t, "Safari/604.1 (iPhone; CPU iPhone OS 17_0 like Mac OS X)")
	third := newBrowser(t, "Firefox/121.0 (Macintosh; Intel Mac OS X 10_15_7)")
	secondGrant := f.login(t, second, "request-login-2")
	thirdGrant := f.login(t, third, "request-login-3")

	// Both hold a challenge id; the live-challenge rule means at most one is
	// still current, and the limit must hold regardless of which wins.
	var wg sync.WaitGroup
	results := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, results[0] = f.completeTrust(t, second, secondGrant, 1, "")
	}()
	go func() {
		defer wg.Done()
		_, results[1] = f.completeTrust(t, third, thirdGrant, 2, "")
	}()
	wg.Wait()

	if count := f.trustedCount(t); count > 2 {
		t.Fatalf("trusted devices = %d, want at most the configured limit of 2 (results: %v)", count, results)
	}
}

// The code is single use, budgeted, and cannot be aimed at another browser.
func TestDeviceCodesAreSingleUseAndBoundToTheirBrowser(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 Safari/537.36 (Windows NT 10.0)")
	other := newBrowser(t, "Firefox/121.0 (Macintosh; Intel Mac OS X 10_15_7)")

	grant := f.login(t, first, "request-login-1")

	// A second browser that somehow learned the code cannot spend it, because
	// it does not hold the device credential the challenge was raised for.
	if _, err := f.devices.CompleteTrust(context.Background(), DeviceTrustRequest{
		AccountID: f.account, SessionID: grant.Session.SessionID,
		PresentedDigest: other.digest,
		ChallengeID:     grant.Device.Challenge.ChallengeID,
		Code:            deterministicCode(deviceCodeSeed, 0),
		RequestID:       "request-wrong-browser",
	}); !errors.Is(err, ErrOTPInvalid) {
		t.Fatalf("another browser spending the code = %v, want ErrOTPInvalid", err)
	}

	if _, err := f.completeTrust(t, first, grant, 0, ""); err != nil {
		t.Fatalf("completing trust: %v", err)
	}
	// Replaying the same code is refused: the challenge is consumed.
	if _, err := f.completeTrust(t, first, grant, 0, ""); !errors.Is(err, ErrOTPInvalid) {
		t.Fatalf("replaying the code = %v, want ErrOTPInvalid", err)
	}
}

// A wrong code spends one attempt and the budget is terminal, so the emailed
// six digits cannot be guessed online.
func TestDeviceCodeGuessingIsBudgeted(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 Safari/537.36 (Windows NT 10.0)")
	grant := f.login(t, first, "request-login-1")

	wrong := DeviceTrustRequest{
		AccountID: f.account, SessionID: grant.Session.SessionID,
		PresentedDigest: first.digest, ChallengeID: grant.Device.Challenge.ChallengeID,
		Code: "000000", RequestID: "request-guess",
	}
	if deterministicCode(deviceCodeSeed, 0) == wrong.Code {
		t.Fatal("the fixture's wrong code is the right one")
	}
	for attempt := 0; attempt < EmailOTPMaxAttempts; attempt++ {
		if _, err := f.devices.CompleteTrust(context.Background(), wrong); err == nil {
			t.Fatalf("guess %d was accepted", attempt)
		}
	}
	// The budget is spent; even the correct code no longer works on this
	// challenge, so the Student must request a new one.
	if _, err := f.completeTrust(t, first, grant, 0, ""); !errors.Is(err, ErrOTPAttemptsExhausted) &&
		!errors.Is(err, ErrOTPInvalid) {
		t.Fatalf("after the budget = %v, want an exhausted or invalid refusal", err)
	}
	if f.trustedCount(t) != 0 {
		t.Fatal("a device was trusted by guessing")
	}
	if f.securityEvents(t, "DEVICE_TRUST_ATTEMPTS_EXHAUSTED") == 0 {
		t.Fatal("exhausting the budget left no security event")
	}
}

// Security recovery clears every device and the cooldown with it, so a Student
// who has just proven control of their mailbox is not made to wait a day.
func TestSecurityRecoveryClearsDevicesAndTheCooldown(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 Safari/537.36 (Windows NT 10.0)")
	second := newBrowser(t, "Safari/604.1 (iPhone; CPU iPhone OS 17_0 like Mac OS X)")

	firstGrant := f.login(t, first, "request-login-1")
	if _, err := f.completeTrust(t, first, firstGrant, 0, ""); err != nil {
		t.Fatalf("trusting the first device: %v", err)
	}
	secondGrant := f.login(t, second, "request-login-2")
	if _, err := f.completeTrust(t, second, secondGrant, 1, ""); err != nil {
		t.Fatalf("trusting the second device: %v", err)
	}

	tx, err := f.pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("beginning the recovery transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	revoked, err := f.devices.RevokeAllForSecurityRecovery(
		context.Background(), tx, f.account, 1, DeviceRevokedByReset, "request-reset", f.clock.Now())
	if err != nil {
		t.Fatalf("revoking for recovery: %v", err)
	}
	if len(revoked) != 2 {
		t.Fatalf("revoked %d devices, want 2", len(revoked))
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("committing the recovery: %v", err)
	}
	if f.trustedCount(t) != 0 {
		t.Fatalf("trusted devices after recovery = %d, want 0", f.trustedCount(t))
	}

	// And the Student can immediately trust a device again.
	fresh := newBrowser(t, "Edg/120.0 Chrome/120.0 (Windows NT 10.0)")
	grant := f.login(t, fresh, "request-login-after-reset")
	if _, err := f.completeTrust(t, fresh, grant, 2, ""); err != nil {
		t.Fatalf("trusting a device straight after recovery: %v", err)
	}
}

func TestPasswordResetRevokesDevicesClearsCooldownAndReleasesPlayback(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 Safari/537.36 (Windows NT 10.0)")
	second := newBrowser(t, "Safari/604.1 (iPhone; CPU iPhone OS 17_0 like Mac OS X)")
	firstGrant := f.login(t, first, "request-login-first")
	if _, err := f.completeTrust(t, first, firstGrant, 0, ""); err != nil {
		t.Fatal(err)
	}
	secondGrant := f.login(t, second, "request-login-second")
	if _, err := f.completeTrust(t, second, secondGrant, 1, ""); err != nil {
		t.Fatal(err)
	}
	firstID, secondID := f.deviceIDFor(t, first), f.deviceIDFor(t, second)
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO identity_device_replacement_state
		  (account_id, last_replacement_at, created_at, updated_at)
		VALUES ($1::uuid, $2, $2, $2)
	`, f.account, f.clock.Now()); err != nil {
		t.Fatal(err)
	}

	recovery := recoveryService(t, f.pool, f.clock.Now(), 0x91)
	recovery.AttachDevices(f.devices)
	if err := recovery.RequestPasswordReset(context.Background(), PasswordResetRequest{
		Email: "device-student@example.test", RequestID: "request-reset",
	}); err != nil {
		t.Fatal(err)
	}
	if err := recovery.CompletePasswordReset(context.Background(), PasswordResetCompletion{
		Token: deterministicBearer(0x91), Password: config.NewSecret("quiet lantern beside seventeen rivers"),
		RequestID: "request-reset-complete",
	}); err != nil {
		t.Fatal(err)
	}

	if f.trustedCount(t) != 0 {
		t.Fatalf("trusted devices after recovery = %d, want 0", f.trustedCount(t))
	}
	state, err := f.devices.replacementState(context.Background(), f.account)
	if err != nil {
		t.Fatal(err)
	}
	if !state.CooldownUntil(f.devices.policy).IsZero() {
		t.Fatal("password recovery left the replacement cooldown active")
	}
	released := f.playback.Released()
	if len(released) != 2 ||
		!((released[0] == firstID && released[1] == secondID) || (released[0] == secondID && released[1] == firstID)) {
		t.Fatalf("released playback devices = %v, want %s and %s", released, firstID, secondID)
	}
}

// The Devices screen shows what the Student needs and nothing more.
func TestDeviceOverviewExposesNoSecrets(t *testing.T) {
	f := newDeviceFixture(t)
	first := newBrowser(t, "Chrome/120.0 Safari/537.36 (Windows NT 10.0)")
	grant := f.login(t, first, "request-login-1")
	if _, err := f.completeTrust(t, first, grant, 0, ""); err != nil {
		t.Fatalf("trusting: %v", err)
	}
	deviceID := f.deviceIDFor(t, first)

	overview, err := f.devices.Overview(context.Background(), f.account, deviceID, f.clock.Now())
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if len(overview.Devices) != 1 {
		t.Fatalf("devices = %d, want 1", len(overview.Devices))
	}
	summary := overview.Devices[0]
	if !summary.CurrentDevice {
		t.Fatal("the calling browser was not marked as the current device")
	}
	if summary.Label != "Chrome on Windows" {
		t.Fatalf("label = %q", summary.Label)
	}
	if overview.DeviceLimit != 2 {
		t.Fatalf("published limit = %d, want 2", overview.DeviceLimit)
	}
	if !overview.ReplacementReady || overview.CooldownUntil != nil {
		t.Fatalf("a fresh Account reports a cooldown: %+v", overview)
	}
}
