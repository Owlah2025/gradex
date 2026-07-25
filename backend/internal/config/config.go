// Package config loads and validates Gradex's runtime configuration.
//
// Two layers stay deliberately separate, per
// docs/superpowers/specs/2026-07-27-api-security-integration-design.md §11.2:
// typed non-secret settings parsed from the deployment environment, and secret
// values resolved through a SecretResolver from references the settings carry
// but never contain.
//
// Load validates everything before returning, so a *Config that exists is one
// that passed. It is immutable after construction: fields are unexported and
// read through value-returning getters, so nothing can retune the runtime
// after startup validation has already accepted it.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environment separates development, staging, and production so that
// validation can refuse in production what is merely inconvenient in
// development. Secrets for these environments are separated too (§11.2).
type Environment string

const (
	EnvDevelopment Environment = "development"
	EnvStaging     Environment = "staging"
	EnvProduction  Environment = "production"
)

func (e Environment) Valid() bool {
	switch e {
	case EnvDevelopment, EnvStaging, EnvProduction:
		return true
	}
	return false
}

func (e Environment) IsProduction() bool { return e == EnvProduction }

// Capability records whether a gated provider surface is available, and when
// it is not, a safe reason suitable for logs and readiness output.
//
// This implements "fail closed at the smallest safe scope" (§11.2): an absent
// Tap secret disables payment initiation and webhooks without preventing
// unrelated learning traffic, so the process still starts and still serves what
// it can legitimately serve.
type Capability struct {
	enabled bool
	reason  string
}

func (c Capability) Enabled() bool { return c.enabled }
func (c Capability) Reason() string {
	if c.enabled {
		return ""
	}
	return c.reason
}

func enabled() Capability               { return Capability{enabled: true} }
func disabled(reason string) Capability { return Capability{reason: reason} }

// Config is the immutable runtime configuration. Construct it only through
// Load or LoadFrom.
type Config struct {
	environment Environment

	port                 string
	publicOrigin         string
	corsAllowedOrigins   []string
	corsAllowCredentials bool
	trustedProxies       []string
	logLevel             string

	httpReadTimeout  time.Duration
	httpWriteTimeout time.Duration
	httpIdleTimeout  time.Duration
	shutdownTimeout  time.Duration

	sessionIdleExpiry     time.Duration
	sessionAbsoluteExpiry time.Duration

	databaseURL Secret
	redisAddr   string

	s3Endpoint     string
	s3Bucket       string
	s3Region       string
	s3AccessKey    Secret
	s3SecretKey    Secret
	s3UsePathStyle bool

	uploadURLExpiry     time.Duration
	playbackURLExpiry   time.Duration
	maxUploadSizeBytes  int64
	playbackTokenSecret Secret

	ffmpegBinaryPath  string
	ffprobeBinaryPath string

	authFakeMode bool

	payments Capability
	email    Capability
}

func (c *Config) Environment() Environment { return c.environment }
func (c *Config) Port() string             { return c.port }
func (c *Config) PublicOrigin() string     { return c.publicOrigin }

// CORSAllowedOrigins returns a copy so a caller cannot mutate the accepted
// origin set through the slice it was handed.
func (c *Config) CORSAllowedOrigins() []string {
	out := make([]string, len(c.corsAllowedOrigins))
	copy(out, c.corsAllowedOrigins)
	return out
}

func (c *Config) CORSAllowCredentials() bool { return c.corsAllowCredentials }

// TrustedProxies returns a copy of the proxy CIDRs whose forwarding headers may
// be believed. An empty result means trust none, which is the default: the
// framework's own default of trusting every proxy would let any client forge
// its apparent address.
func (c *Config) TrustedProxies() []string {
	out := make([]string, len(c.trustedProxies))
	copy(out, c.trustedProxies)
	return out
}

func (c *Config) LogLevel() string { return c.logLevel }

func (c *Config) HTTPReadTimeout() time.Duration  { return c.httpReadTimeout }
func (c *Config) HTTPWriteTimeout() time.Duration { return c.httpWriteTimeout }
func (c *Config) HTTPIdleTimeout() time.Duration  { return c.httpIdleTimeout }
func (c *Config) ShutdownTimeout() time.Duration  { return c.shutdownTimeout }

func (c *Config) SessionIdleExpiry() time.Duration     { return c.sessionIdleExpiry }
func (c *Config) SessionAbsoluteExpiry() time.Duration { return c.sessionAbsoluteExpiry }

func (c *Config) DatabaseURL() Secret { return c.databaseURL }
func (c *Config) RedisAddr() string   { return c.redisAddr }

func (c *Config) S3Endpoint() string   { return c.s3Endpoint }
func (c *Config) S3Bucket() string     { return c.s3Bucket }
func (c *Config) S3Region() string     { return c.s3Region }
func (c *Config) S3AccessKey() Secret  { return c.s3AccessKey }
func (c *Config) S3SecretKey() Secret  { return c.s3SecretKey }
func (c *Config) S3UsePathStyle() bool { return c.s3UsePathStyle }

func (c *Config) UploadURLExpiry() time.Duration   { return c.uploadURLExpiry }
func (c *Config) PlaybackURLExpiry() time.Duration { return c.playbackURLExpiry }
func (c *Config) MaxUploadSizeBytes() int64        { return c.maxUploadSizeBytes }
func (c *Config) PlaybackTokenSecret() Secret      { return c.playbackTokenSecret }

func (c *Config) FFmpegBinaryPath() string  { return c.ffmpegBinaryPath }
func (c *Config) FFprobeBinaryPath() string { return c.ffprobeBinaryPath }

// AuthFakeMode reports the development-only identity seam. Validation refuses
// to let it be true in production; see validate.
func (c *Config) AuthFakeMode() bool { return c.authFakeMode }

func (c *Config) Payments() Capability { return c.payments }
func (c *Config) Email() Capability    { return c.email }

// Lookup reads one setting. os.LookupEnv satisfies it; tests supply a map so
// they never mutate the process environment.
type Lookup func(key string) (string, bool)

// OSLookup is the process-environment source.
func OSLookup(key string) (string, bool) { return os.LookupEnv(key) }

// MapLookup builds a Lookup over a fixed map, for tests.
func MapLookup(m map[string]string) Lookup {
	return func(key string) (string, bool) {
		v, ok := m[key]
		return v, ok
	}
}

// Load reads configuration from the process environment and resolves secrets
// from it as well. This is the development and single-host path; the resolver
// is the seam an approved secret manager replaces.
func Load() (*Config, error) {
	return LoadFrom(OSLookup, EnvSecretResolver{})
}

// LoadFrom parses settings from lookup, resolves secret references through
// resolver, validates the result, and only then builds the immutable Config.
// It reports every problem it finds rather than the first, so an operator
// fixes one deployment instead of discovering faults one restart at a time.
func LoadFrom(lookup Lookup, resolver SecretResolver) (*Config, error) {
	p := &parser{lookup: lookup}

	cfg := &Config{
		environment: Environment(p.str("APP_ENV", string(EnvDevelopment))),

		port:                 p.str("PORT", "8080"),
		publicOrigin:         p.str("PUBLIC_ORIGIN", ""),
		corsAllowedOrigins:   p.list("CORS_ALLOWED_ORIGINS"),
		corsAllowCredentials: p.boolean("CORS_ALLOW_CREDENTIALS", false),
		trustedProxies:       p.list("TRUSTED_PROXIES"),
		logLevel:             p.str("LOG_LEVEL", "info"),

		httpReadTimeout:  p.duration("HTTP_READ_TIMEOUT", 15*time.Second),
		httpWriteTimeout: p.duration("HTTP_WRITE_TIMEOUT", 30*time.Second),
		httpIdleTimeout:  p.duration("HTTP_IDLE_TIMEOUT", 60*time.Second),
		shutdownTimeout:  p.duration("SHUTDOWN_TIMEOUT", 20*time.Second),

		sessionIdleExpiry:     p.duration("SESSION_IDLE_EXPIRY", 12*time.Hour),
		sessionAbsoluteExpiry: p.duration("SESSION_ABSOLUTE_EXPIRY", 720*time.Hour),

		redisAddr: p.str("REDIS_ADDR", ""),

		s3Endpoint:     p.str("S3_ENDPOINT", ""),
		s3Bucket:       p.str("S3_BUCKET", ""),
		s3Region:       p.str("S3_REGION", "us-east-1"),
		s3UsePathStyle: p.boolean("S3_USE_PATH_STYLE", true),

		uploadURLExpiry:    p.duration("UPLOAD_URL_EXPIRY", 15*time.Minute),
		playbackURLExpiry:  p.duration("PLAYBACK_URL_EXPIRY", 5*time.Minute),
		maxUploadSizeBytes: p.integer("MAX_UPLOAD_SIZE_BYTES", 5*1024*1024*1024),

		ffmpegBinaryPath:  p.str("FFMPEG_BINARY_PATH", "ffmpeg"),
		ffprobeBinaryPath: p.str("FFPROBE_BINARY_PATH", "ffprobe"),

		authFakeMode: p.boolean("AUTH_FAKE_MODE", false),
	}

	// Provider gates are read as settings here and turned into capabilities in
	// validate, where the fail-closed scope for each one is decided.
	tapEnabled := p.boolean("TAP_ENABLED", false)
	tapEnvironment := p.str("TAP_ENVIRONMENT", "test")
	tapAdapterApproved := p.boolean("TAP_ADAPTER_APPROVED", false)
	emailEnabled := p.boolean("EMAIL_ENABLED", false)

	secrets := map[string]Secret{}
	for _, ref := range []SecretRef{
		{Name: "DATABASE_URL", Required: true},
		{Name: "S3_ACCESS_KEY", Required: true},
		{Name: "S3_SECRET_KEY", Required: true},
		{Name: "PLAYBACK_TOKEN_SECRET", Required: true},
		{Name: "TAP_SECRET"},
		{Name: "EMAIL_API_KEY"},
	} {
		s, err := resolver.Resolve(ref)
		if err != nil {
			p.errf("resolving secret %s: %v", ref.Name, err)
			continue
		}
		if ref.Required && s.IsEmpty() {
			p.errf("%s is required and was not resolved", ref.Name)
		}
		secrets[ref.Name] = s
	}

	cfg.databaseURL = secrets["DATABASE_URL"]
	cfg.s3AccessKey = secrets["S3_ACCESS_KEY"]
	cfg.s3SecretKey = secrets["S3_SECRET_KEY"]
	cfg.playbackTokenSecret = secrets["PLAYBACK_TOKEN_SECRET"]

	cfg.payments = tapCapability(cfg.environment, tapEnabled, tapEnvironment, tapAdapterApproved, secrets["TAP_SECRET"], p)
	cfg.email = emailCapability(emailEnabled, secrets["EMAIL_API_KEY"])

	p.rejectRetiredKeys()
	cfg.validate(p)

	if err := p.err(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// tapCapability applies the §11.2 rules for the payment provider. Live Tap
// without the approved LG-010 adapter contract is listed there as an invalid
// configuration, so it blocks startup rather than merely disabling payments —
// a deployment that believes it is taking live money must not come up quietly
// unable to.
func tapCapability(env Environment, enabledFlag bool, tapEnv string, adapterApproved bool, secret Secret, p *parser) Capability {
	if !enabledFlag {
		return disabled("TAP_ENABLED is false")
	}
	switch tapEnv {
	case "test", "live":
	default:
		p.errf("TAP_ENVIRONMENT must be \"test\" or \"live\", got %q", tapEnv)
		return disabled("TAP_ENVIRONMENT is invalid")
	}
	if tapEnv == "live" && !adapterApproved {
		p.errf("TAP_ENVIRONMENT=live requires TAP_ADAPTER_APPROVED=true; the LG-010 authenticity contract is not approved")
		return disabled("LG-010 Tap adapter contract is not approved")
	}
	if env.IsProduction() && tapEnv == "test" {
		p.errf("TAP_ENVIRONMENT=test is not permitted when APP_ENV=production")
		return disabled("Tap test environment in production")
	}
	if secret.IsEmpty() {
		// Smallest safe scope: payment initiation and webhooks are unavailable,
		// unrelated learning traffic continues.
		return disabled("TAP_SECRET is absent")
	}
	return enabled()
}

func emailCapability(enabledFlag bool, secret Secret) Capability {
	if !enabledFlag {
		return disabled("EMAIL_ENABLED is false")
	}
	if secret.IsEmpty() {
		return disabled("EMAIL_API_KEY is absent")
	}
	return enabled()
}

func (c *Config) validate(p *parser) {
	if !c.environment.Valid() {
		p.errf("APP_ENV must be one of development, staging, production; got %q", c.environment)
	}

	if c.redisAddr == "" {
		p.errf("REDIS_ADDR is required")
	}
	if c.s3Endpoint == "" {
		p.errf("S3_ENDPOINT is required")
	}
	if c.s3Bucket == "" {
		p.errf("S3_BUCKET is required")
	}

	for _, d := range []struct {
		name string
		v    time.Duration
	}{
		{"HTTP_READ_TIMEOUT", c.httpReadTimeout},
		{"HTTP_WRITE_TIMEOUT", c.httpWriteTimeout},
		{"HTTP_IDLE_TIMEOUT", c.httpIdleTimeout},
		{"SHUTDOWN_TIMEOUT", c.shutdownTimeout},
		{"SESSION_IDLE_EXPIRY", c.sessionIdleExpiry},
		{"SESSION_ABSOLUTE_EXPIRY", c.sessionAbsoluteExpiry},
		{"UPLOAD_URL_EXPIRY", c.uploadURLExpiry},
		{"PLAYBACK_URL_EXPIRY", c.playbackURLExpiry},
	} {
		if d.v <= 0 {
			p.errf("%s must be positive, got %s", d.name, d.v)
		}
	}

	// Named explicitly in §11.2 as an invalid configuration: an idle expiry
	// above the absolute expiry makes the absolute bound unreachable, so the
	// session would never actually be capped.
	if c.sessionIdleExpiry > 0 && c.sessionAbsoluteExpiry > 0 && c.sessionIdleExpiry >= c.sessionAbsoluteExpiry {
		p.errf("SESSION_IDLE_EXPIRY (%s) must be less than SESSION_ABSOLUTE_EXPIRY (%s)",
			c.sessionIdleExpiry, c.sessionAbsoluteExpiry)
	}

	if c.maxUploadSizeBytes <= 0 {
		p.errf("MAX_UPLOAD_SIZE_BYTES must be positive, got %d", c.maxUploadSizeBytes)
	}

	switch c.logLevel {
	case "debug", "info", "warn", "error":
	default:
		p.errf("LOG_LEVEL must be one of debug, info, warn, error; got %q", c.logLevel)
	}

	// Credentialed CORS with a wildcard origin is the other named invalid
	// example. It is refused in every environment because there is no
	// environment in which it is correct.
	for _, o := range c.corsAllowedOrigins {
		if o == "*" && c.corsAllowCredentials {
			p.errf("CORS_ALLOWED_ORIGINS may not contain \"*\" when CORS_ALLOW_CREDENTIALS is true")
		}
	}

	if !c.environment.IsProduction() {
		return
	}

	// Production-only rules. None of these have a permissive default: the
	// setting is either explicitly correct or startup is blocked.
	if c.publicOrigin == "" {
		p.errf("PUBLIC_ORIGIN is required when APP_ENV=production")
	} else if !strings.HasPrefix(c.publicOrigin, "https://") {
		p.errf("PUBLIC_ORIGIN must be an https origin in production, got %q", c.publicOrigin)
	}
	for _, o := range c.corsAllowedOrigins {
		if o == "*" {
			p.errf("CORS_ALLOWED_ORIGINS may not contain \"*\" when APP_ENV=production")
			continue
		}
		if !strings.HasPrefix(o, "https://") {
			p.errf("CORS origin %q must be https in production", o)
		}
	}
	if c.authFakeMode {
		p.errf("AUTH_FAKE_MODE must be false when APP_ENV=production")
	}
	// The .env.example placeholder is a real deployment hazard: it is a valid
	// non-empty string, so an emptiness check alone would accept it.
	if strings.HasPrefix(c.playbackTokenSecret.Expose(), "changeme") {
		p.errf("PLAYBACK_TOKEN_SECRET is still the example placeholder")
	}
}

// retiredKeys maps settings this package used to read to their replacements.
// Renaming a key without this check is a silent failure: the old value stays
// in the deployment, the new key falls back to its default, and nothing
// reports that the operator's intent was dropped.
var retiredKeys = map[string]string{
	"UPLOAD_URL_EXPIRY_MINUTES":   "UPLOAD_URL_EXPIRY (a duration, e.g. \"15m\")",
	"PLAYBACK_URL_EXPIRY_MINUTES": "PLAYBACK_URL_EXPIRY (a duration, e.g. \"5m\")",
}

func (p *parser) rejectRetiredKeys() {
	for old, replacement := range retiredKeys {
		if _, ok := p.raw(old); ok {
			p.errf("%s is no longer read; use %s", old, replacement)
		}
	}
}

// parser accumulates settings faults alongside parsing so that one Load
// reports the whole picture.
type parser struct {
	lookup Lookup
	errs   []error
}

func (p *parser) errf(format string, args ...any) {
	p.errs = append(p.errs, fmt.Errorf(format, args...))
}

func (p *parser) err() error {
	if len(p.errs) == 0 {
		return nil
	}
	return fmt.Errorf("invalid configuration: %w", errors.Join(p.errs...))
}

func (p *parser) raw(key string) (string, bool) {
	v, ok := p.lookup(key)
	if !ok {
		return "", false
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	return v, true
}

func (p *parser) str(key, fallback string) string {
	if v, ok := p.raw(key); ok {
		return v
	}
	return fallback
}

func (p *parser) list(key string) []string {
	v, ok := p.raw(key)
	if !ok {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func (p *parser) boolean(key string, fallback bool) bool {
	v, ok := p.raw(key)
	if !ok {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		p.errf("%s must be a boolean, got %q", key, v)
		return fallback
	}
	return b
}

func (p *parser) integer(key string, fallback int64) int64 {
	v, ok := p.raw(key)
	if !ok {
		return fallback
	}
	i, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		p.errf("%s must be an integer, got %q", key, v)
		return fallback
	}
	return i
}

func (p *parser) duration(key string, fallback time.Duration) time.Duration {
	v, ok := p.raw(key)
	if !ok {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		p.errf("%s must be a duration such as \"30s\" or \"15m\", got %q", key, v)
		return fallback
	}
	return d
}
