//go:build !production

package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

// Test-runner-side session issuance.
//
// Repeated runs need dozens of authenticated Students, but the login endpoint is bounded at 30
// requests per minute per source network — a production limit this suite must respect rather
// than raise. Issuing sessions here keeps every one of those limits intact while still using the
// production authentication path: the real `SessionRepository`, loaded from the real
// configuration, verifying the real Argon2id credential and writing the real session family and
// generation rows. Nothing about the session is invented — the same code the HTTP handler calls
// produces it, so production middleware accepts it exactly as it accepts a browser login.
//
// The infrastructure smoke test continues to exercise the full HTTP login flow, so the login
// route itself remains covered.
type issuedSessionOutput struct {
	AccountID string        `json:"account_id"`
	Role      identity.Role `json:"role"`
	// CookieName is the production cookie name; the value is the opaque session credential.
	CookieName  string `json:"cookie_name"`
	CookieValue string `json:"cookie_value"`
	// CSRFToken is what the browser would have held in memory after logging in. It never reaches
	// disk: this is written to stdout for the test runner and nothing else.
	CSRFToken string `json:"csrf_token"`

	// The trusted-device credential this session is bound to.
	//
	// A Student session carries both its session cookie and the device cookie
	// admitted in the same login transaction.
	DeviceCookieName  string `json:"device_cookie_name"`
	DeviceCookieValue string `json:"device_cookie_value"`
	DeviceID          string `json:"device_id"`
}

// seededDeviceCredential derives a stable, obviously synthetic device
// credential. It is 32 bytes like every real one, and it exists only in this
// test-only binary.
func seededDeviceCredential(email string, slot int) (config.Secret, string) {
	sum := sha256.Sum256([]byte(fmt.Sprintf("gradex-e2e-device|%s|%d", strings.ToLower(email), slot)))
	plaintext := base64.RawURLEncoding.EncodeToString(sum[:])
	return config.NewSecret(plaintext), identity.DigestOpaqueCredential(plaintext)
}

func newFixtureDeviceService(pool *pgxpool.Pool, cfg *config.Config) (*identity.DeviceService, error) {
	admission := cfg.Admission()
	writer, err := outbox.NewWriter(
		admission.ProtectedPayloadKeyVersion(),
		[]byte(admission.ProtectedPayloadKey().Expose()),
	)
	if err != nil {
		return nil, err
	}
	settings := cfg.StudentDevices()
	return identity.NewDeviceService(identity.DeviceServiceOptions{
		Pool: pool, Outbox: writer,
		Policy: identity.DevicePolicy{
			TrustedDeviceLimit:  settings.TrustedDeviceLimit(),
			ReplacementCooldown: settings.ReplacementCooldown(),
		},
		Pepper: admission.EmailOTPPepper(), OTPTTL: admission.EmailOTPTTL(),
		Now: time.Now, Random: rand.Reader,
	})
}

func issueSession(ctx context.Context, targetDSN, email, password string, deviceSlot int) (issuedSessionOutput, error) {
	if deviceSlot < 0 || deviceSlot > 1 {
		return issuedSessionOutput{}, fmt.Errorf("device slot must be 0 or 1")
	}
	// The production loader, so session windows and the CSRF key are resolved exactly as the API
	// resolves them. The same SESSION_CSRF_KEY the API runs with must be in this process's
	// environment, or the issued CSRF token would not validate.
	cfg, err := config.Load()
	if err != nil {
		return issuedSessionOutput{}, fmt.Errorf("loading production configuration: %w", err)
	}
	if !cfg.Sessions().Enabled() {
		return issuedSessionOutput{}, fmt.Errorf("session settings are disabled; SESSION_CSRF_KEY is required")
	}

	pool, err := pgxpool.New(ctx, targetDSN)
	if err != nil {
		return issuedSessionOutput{}, fmt.Errorf("connecting to target db for session issuance: %w", err)
	}
	defer pool.Close()

	devices, err := newFixtureDeviceService(pool, cfg)
	if err != nil {
		return issuedSessionOutput{}, err
	}
	repository, err := identity.NewSessionRepository(identity.SessionRepositoryOptions{
		Pool:     pool,
		Settings: cfg.Sessions(),
		CSRFKey:  []byte(cfg.Sessions().CSRFKey().Expose()),
		Now:      time.Now,
		Devices:  devices,
	})
	if err != nil {
		return issuedSessionOutput{}, fmt.Errorf("building session repository: %w", err)
	}

	// The device credential is derived from the Account and a slot rather than
	// minted fresh, because a fixture that minted every time would model
	// something false: one Student signing in again on the same browser would
	// look like a *different* device, burn a device slot, and — since one
	// account may hold one protected playback at a time — be refused by its own
	// previous test. Deterministic slots make "sign this Student in again" mean
	// the same browser, and asking for slot 1 mean genuinely a second one.
	deviceCredential, deviceDigest := seededDeviceCredential(email, deviceSlot)

	grant, err := repository.Login(ctx, identity.LoginRequest{
		Email:                  email,
		Password:               config.NewSecret(password),
		RequestID:              "e2e-session-issuance",
		DeviceCredentialDigest: deviceDigest,
		UserAgent:              "Mozilla/5.0 (X11; Linux x86_64) Chrome/120.0 Safari/537.36",
	})
	if err != nil {
		return issuedSessionOutput{}, fmt.Errorf("issuing session for %s: %w", email, err)
	}

	deviceID := ""
	if grant.Session.Role == identity.RoleStudent {
		deviceID = grant.Device.DeviceID
	}

	issued := issuedSessionOutput{
		AccountID:   grant.Session.AccountID,
		Role:        grant.Session.Role,
		CookieName:  auth.SessionCookieName,
		CookieValue: grant.Credential.Expose(),
		CSRFToken:   grant.CSRFToken.Expose(),
	}
	if grant.Session.Role == identity.RoleStudent {
		issued.DeviceCookieName = auth.DeviceCookieName
		issued.DeviceCookieValue = deviceCredential.Expose()
		issued.DeviceID = deviceID
	}
	return issued, nil
}

// TrustPendingDevicesFor confirms every device an Account has started
// confirming, and binds its narrowed sessions to one of them.
//
// This is the fixture equivalent of a Student typing the emailed code. It
// exists because most suites authenticate a Student in order to test something
// else entirely — Course Home, Progress, the catalogue — and making each of
// them drive a mailbox would make the fixture the test and would couple every
// one of them to a mail server they otherwise do not need.
//
// The real confirmation flow is not skipped anywhere it is the subject: it is
// covered by the identity integration suite and driven end to end, through the
// product's own screens and a real emailed code, by the device-security browser
// journey.
//
// This lives in the seeder — a test-only binary — and there is deliberately no
// equivalent path in the product.
func trustPendingDevicesFor(ctx context.Context, pool *pgxpool.Pool, email string) error {
	var accountID string
	if err := pool.QueryRow(ctx,
		`SELECT id::text FROM accounts WHERE normalized_email = lower($1)`, email,
	).Scan(&accountID); err != nil {
		return fmt.Errorf("resolving %s for device confirmation: %w", email, err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE identity_trusted_devices
		   SET trusted_at = COALESCE(trusted_at, now()), updated_at = now()
		 WHERE account_id = $1::uuid AND revoked_at IS NULL
	`, accountID); err != nil {
		return fmt.Errorf("confirming pending devices for %s: %w", email, err)
	}
	// Bind every narrowed session to the device it raised its challenge for, so
	// each browser keeps its own device rather than borrowing another's.
	if _, err := pool.Exec(ctx, `
		UPDATE sessions s
		   SET trusted_device_id = challenge.trusted_device_id,
		       device_trust_state = 'TRUSTED'
		  FROM (
		    SELECT DISTINCT ON (account_id) account_id, trusted_device_id
		      FROM identity_action_secrets
		     WHERE account_id = $1::uuid AND purpose = 'DEVICE_TRUST_OTP'
		     ORDER BY account_id, issued_at DESC
		  ) AS challenge
		 WHERE s.account_id = challenge.account_id
		   AND s.state = 'ACTIVE'
		   AND s.device_trust_state = 'PENDING_DEVICE_TRUST'
		   AND challenge.trusted_device_id IS NOT NULL
	`, accountID); err != nil {
		return fmt.Errorf("binding narrowed sessions for %s: %w", email, err)
	}
	return nil
}

func encodeIssuedSession(session issuedSessionOutput) ([]byte, error) {
	encoded, err := json.Marshal(session)
	if err != nil {
		return nil, fmt.Errorf("encoding issued session: %w", err)
	}
	return encoded, nil
}
