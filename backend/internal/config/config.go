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
	"log/slog"
	"net"
	"net/mail"
	"net/url"
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

// ServiceRole is which Gradex process this is. Readiness policy differs by
// role, so it is a typed value rather than a set of independent feature flags
// that could be combined into a state nobody designed.
type ServiceRole string

const (
	RoleAPI    ServiceRole = "api"
	RoleWorker ServiceRole = "worker"
)

func (r ServiceRole) Valid() bool {
	return r == RoleAPI || r == RoleWorker
}

// RequiresRedis reports whether this role cannot serve its responsibilities
// without Redis.
//
// Both roles require it today: the worker consumes from it, and the API
// enqueues synchronously during upload completion and retranscode, so losing
// Redis makes those commands fail rather than queue.
//
// This flips for the API after the PostgreSQL outbox cutover (domain §7.3,
// §§21-24), when command admission becomes durable in PostgreSQL alone and
// Redis becomes a dispatch detail. The decision lives here, attached to the
// role, so that change is one edit with one obvious meaning.
func (r ServiceRole) RequiresRedis() bool { return true }

// MediaOperatingMode is the explicit LG-014 operating switch. It is typed and
// startup-validated so an unknown mode cannot silently enable an unsafe path.
type MediaOperatingMode string

const (
	MediaOperatingModeScanner        MediaOperatingMode = "SCANNER"
	MediaOperatingModeAdminCatalogue MediaOperatingMode = "ADMIN_CATALOGUE"
)

func (m MediaOperatingMode) Valid() bool {
	return m == MediaOperatingModeScanner || m == MediaOperatingModeAdminCatalogue
}

// MediaScannerMode names which malware-scanning boundary this process builds.
//
// LG-014 has selected no production scanner, so `UNAVAILABLE` remains the
// default and every scan errors, leaving the Asset Version non-deliverable.
// `DEVELOPMENT_NO_OP` exists so a developer or an automated acceptance run can
// exercise the complete upload -> scan -> transcode -> READY path on a
// throwaway environment. It inspects nothing, so validate refuses it outside
// APP_ENV=development; it is not a scanner and never satisfies LG-014.
type MediaScannerMode string

const (
	MediaScannerModeUnavailable     MediaScannerMode = "UNAVAILABLE"
	MediaScannerModeDevelopmentNoOp MediaScannerMode = "DEVELOPMENT_NO_OP"
)

func (m MediaScannerMode) Valid() bool {
	return m == MediaScannerModeUnavailable || m == MediaScannerModeDevelopmentNoOp
}

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

// EmailProvider selects the narrow transactional delivery adapter. Domain
// modules never see this value; only worker composition does.
type EmailProvider string

const (
	EmailProviderFake    EmailProvider = "fake"
	EmailProviderMailpit EmailProvider = "mailpit"
	EmailProviderResend  EmailProvider = "resend"
)

func (p EmailProvider) Valid() bool {
	return p == EmailProviderFake || p == EmailProviderMailpit || p == EmailProviderResend
}

// EmailSettings is the immutable worker delivery configuration. API keys stay
// wrapped until the Resend adapter is constructed.
type EmailSettings struct {
	capability  Capability
	provider    EmailProvider
	apiKey      Secret
	smtpAddr    string
	fromAddress string
	fromName    string
	replyTo     string
	timeout     time.Duration
}

func (s EmailSettings) Enabled() bool           { return s.capability.Enabled() }
func (s EmailSettings) Reason() string          { return s.capability.Reason() }
func (s EmailSettings) Provider() EmailProvider { return s.provider }
func (s EmailSettings) APIKey() Secret          { return s.apiKey }
func (s EmailSettings) SMTPAddress() string     { return s.smtpAddr }
func (s EmailSettings) FromAddress() string     { return s.fromAddress }
func (s EmailSettings) FromName() string        { return s.fromName }
func (s EmailSettings) ReplyTo() string         { return s.replyTo }
func (s EmailSettings) Timeout() time.Duration  { return s.timeout }

// PasswordScreenMode selects only the implementation boundary. The
// deterministic source is a local test fixture; adapter mode selects the
// approved production source.
type PasswordScreenMode string

const (
	PasswordScreenUnavailable   PasswordScreenMode = "unavailable"
	PasswordScreenDeterministic PasswordScreenMode = "deterministic"
	PasswordScreenAdapter       PasswordScreenMode = "adapter"
)

func (m PasswordScreenMode) Valid() bool {
	switch m {
	case PasswordScreenUnavailable, PasswordScreenDeterministic, PasswordScreenAdapter:
		return true
	}
	return false
}

// LegalIdentityMode distinguishes an actual public operator identity from the
// exact non-public sentinel identity approved for the disposable S11 stack.
// It is deliberately an enum, not a permissive boolean bypass.
type LegalIdentityMode string

const (
	LegalIdentityPublic            LegalIdentityMode = "public"
	LegalIdentityControlledStaging LegalIdentityMode = "controlled-staging"

	StagingLegalRegistrationNumber = "STAGING-NOT-REGISTERED"
	StagingLegalRegisteredAddress  = "STAGING ONLY — LEGAL ENTITY DETAILS PENDING"
	ControlledStagingPublicOrigin  = "https://gradex.localhost:18443"
)

func (m LegalIdentityMode) Valid() bool {
	return m == LegalIdentityPublic || m == LegalIdentityControlledStaging
}

// LegalSettings is the immutable identity/contact data rendered by the
// approved bilingual policy set.
type LegalSettings struct {
	identityMode       LegalIdentityMode
	operatorName       string
	registrationNumber string
	registeredAddress  string
	privacyEmail       string
	supportEmail       string
	securityEmail      string
}

func (s LegalSettings) IdentityMode() LegalIdentityMode { return s.identityMode }
func (s LegalSettings) OperatorName() string            { return s.operatorName }
func (s LegalSettings) RegistrationNumber() string      { return s.registrationNumber }
func (s LegalSettings) RegisteredAddress() string       { return s.registeredAddress }
func (s LegalSettings) PrivacyEmail() string            { return s.privacyEmail }
func (s LegalSettings) SupportEmail() string            { return s.supportEmail }
func (s LegalSettings) SecurityEmail() string           { return s.securityEmail }

// AdmissionSettings is the complete immutable configuration for public
// Student admission. Returning it by value prevents runtime retuning after
// startup validation.
type AdmissionSettings struct {
	capability Capability

	policySetID                string
	passwordScreenMode         PasswordScreenMode
	protectedPayloadKeyVersion string

	anonymousSessionTTL        time.Duration
	verificationTokenTTL       time.Duration
	passwordResetTokenTTL      time.Duration
	rateLimitTimeout           time.Duration
	compromisedPasswordTimeout time.Duration

	anonymousCookieSigningKey Secret
	anonymousCSRFKey          Secret
	limiterHMACKey            Secret
	protectedPayloadKey       Secret
}

func (a AdmissionSettings) Enabled() bool                          { return a.capability.Enabled() }
func (a AdmissionSettings) Reason() string                         { return a.capability.Reason() }
func (a AdmissionSettings) PolicySetID() string                    { return a.policySetID }
func (a AdmissionSettings) PasswordScreenMode() PasswordScreenMode { return a.passwordScreenMode }
func (a AdmissionSettings) ProtectedPayloadKeyVersion() string     { return a.protectedPayloadKeyVersion }
func (a AdmissionSettings) AnonymousSessionTTL() time.Duration     { return a.anonymousSessionTTL }
func (a AdmissionSettings) VerificationTokenTTL() time.Duration    { return a.verificationTokenTTL }
func (a AdmissionSettings) PasswordResetTokenTTL() time.Duration   { return a.passwordResetTokenTTL }
func (a AdmissionSettings) RateLimitTimeout() time.Duration        { return a.rateLimitTimeout }
func (a AdmissionSettings) CompromisedPasswordTimeout() time.Duration {
	return a.compromisedPasswordTimeout
}
func (a AdmissionSettings) AnonymousCookieSigningKey() Secret { return a.anonymousCookieSigningKey }
func (a AdmissionSettings) AnonymousCSRFKey() Secret          { return a.anonymousCSRFKey }
func (a AdmissionSettings) LimiterHMACKey() Secret            { return a.limiterHMACKey }
func (a AdmissionSettings) ProtectedPayloadKey() Secret       { return a.protectedPayloadKey }

// SessionWindow is one role's immutable server-authoritative lifetime.
type SessionWindow struct {
	idleExpiry     time.Duration
	absoluteExpiry time.Duration
}

func (w SessionWindow) IdleExpiry() time.Duration     { return w.idleExpiry }
func (w SessionWindow) AbsoluteExpiry() time.Duration { return w.absoluteExpiry }

// SessionSettings contains every security window used by authenticated
// sessions. Role windows are separate so a low-risk Student family cannot
// silently become the policy for privileged Accounts.
type SessionSettings struct {
	student    SessionWindow
	instructor SessionWindow
	admin      SessionWindow

	generalRecentAuthWindow     time.Duration
	highestRiskRecentAuthWindow time.Duration
	staleUseWindow              time.Duration
	csrfKey                     Secret
}

func (s SessionSettings) Student() SessionWindow    { return s.student }
func (s SessionSettings) Instructor() SessionWindow { return s.instructor }
func (s SessionSettings) Admin() SessionWindow      { return s.admin }
func (s SessionSettings) GeneralRecentAuthWindow() time.Duration {
	return s.generalRecentAuthWindow
}
func (s SessionSettings) HighestRiskRecentAuthWindow() time.Duration {
	return s.highestRiskRecentAuthWindow
}
func (s SessionSettings) StaleUseWindow() time.Duration { return s.staleUseWindow }
func (s SessionSettings) CSRFKey() Secret               { return s.csrfKey }

// Enabled reports whether the real authenticated-session boundary is
// configured. Development may omit it while retaining the fake auth seam;
// non-development validation requires it.
func (s SessionSettings) Enabled() bool { return !s.csrfKey.IsEmpty() }

// RedisSettings is the immutable connection contract shared by every API and
// worker Redis client. Credentials remain redacting Secrets until the queue
// adapter hands them directly to go-redis/asynq.
type RedisSettings struct {
	addr          string
	username      Secret
	password      Secret
	tlsEnabled    bool
	tlsServerName string
	tlsCACertFile string
}

func (s RedisSettings) Addr() string          { return s.addr }
func (s RedisSettings) Username() Secret      { return s.username }
func (s RedisSettings) Password() Secret      { return s.password }
func (s RedisSettings) TLSEnabled() bool      { return s.tlsEnabled }
func (s RedisSettings) TLSServerName() string { return s.tlsServerName }
func (s RedisSettings) TLSCACertFile() string { return s.tlsCACertFile }

func (s RedisSettings) String() string {
	return fmt.Sprintf("{addr:%q credentials:%s tls:%t}", s.addr, redacted, s.tlsEnabled)
}

func (s RedisSettings) GoString() string { return s.String() }

func (s RedisSettings) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("addr", s.addr),
		slog.String("credentials", redacted),
		slog.Bool("tls", s.tlsEnabled),
	)
}

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
	serviceRole          ServiceRole
	readinessTimeout     time.Duration

	httpReadTimeout  time.Duration
	httpWriteTimeout time.Duration
	httpIdleTimeout  time.Duration
	shutdownTimeout  time.Duration

	sessions SessionSettings

	databaseURL Secret
	redis       RedisSettings

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

	ffmpegBinaryPath       string
	ffprobeBinaryPath      string
	mediaProcessingTimeout time.Duration
	mediaOperatingMode     MediaOperatingMode
	mediaScannerMode       MediaScannerMode

	authFakeMode bool

	payments  Capability
	email     EmailSettings
	admission AdmissionSettings
	legal     LegalSettings
}

func (c *Config) Environment() Environment { return c.environment }
func (c *Config) Port() string             { return c.port }
func (c *Config) PublicOrigin() string     { return c.publicOrigin }

// CanonicalPublicOrigin validates the exact browser-origin shape shared by
// startup configuration and HTTP admission.
func CanonicalPublicOrigin(raw string) (string, error) {
	origin, err := url.Parse(raw)
	if err != nil || origin.Scheme == "" || origin.Host == "" {
		return "", errors.New("public origin must be an absolute HTTP origin")
	}
	if origin.Scheme != "http" && origin.Scheme != "https" {
		return "", errors.New("public origin must use HTTP or HTTPS")
	}
	if origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return "", errors.New("public origin must not contain credentials, a path, query, or fragment")
	}
	return origin.Scheme + "://" + origin.Host, nil
}

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

func (c *Config) ServiceRole() ServiceRole        { return c.serviceRole }
func (c *Config) ReadinessTimeout() time.Duration { return c.readinessTimeout }

func (c *Config) HTTPReadTimeout() time.Duration  { return c.httpReadTimeout }
func (c *Config) HTTPWriteTimeout() time.Duration { return c.httpWriteTimeout }
func (c *Config) HTTPIdleTimeout() time.Duration  { return c.httpIdleTimeout }
func (c *Config) ShutdownTimeout() time.Duration  { return c.shutdownTimeout }

func (c *Config) Sessions() SessionSettings { return c.sessions }

func (c *Config) DatabaseURL() Secret  { return c.databaseURL }
func (c *Config) Redis() RedisSettings { return c.redis }

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

func (c *Config) FFmpegBinaryPath() string               { return c.ffmpegBinaryPath }
func (c *Config) FFprobeBinaryPath() string              { return c.ffprobeBinaryPath }
func (c *Config) MediaProcessingTimeout() time.Duration  { return c.mediaProcessingTimeout }
func (c *Config) MediaOperatingMode() MediaOperatingMode { return c.mediaOperatingMode }
func (c *Config) MediaScannerMode() MediaScannerMode     { return c.mediaScannerMode }

// AuthFakeMode reports the development-only identity seam. Validation refuses
// to let it be true in production; see validate.
func (c *Config) AuthFakeMode() bool { return c.authFakeMode }

func (c *Config) Payments() Capability         { return c.payments }
func (c *Config) Email() EmailSettings         { return c.email }
func (c *Config) Admission() AdmissionSettings { return c.admission }
func (c *Config) Legal() LegalSettings         { return c.legal }

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
	environment := Environment(p.str("APP_ENV", string(EnvDevelopment)))

	cfg := &Config{
		environment: environment,

		port:                 p.str("PORT", "8080"),
		publicOrigin:         p.str("PUBLIC_ORIGIN", ""),
		corsAllowedOrigins:   p.list("CORS_ALLOWED_ORIGINS"),
		corsAllowCredentials: p.boolean("CORS_ALLOW_CREDENTIALS", false),
		trustedProxies:       p.list("TRUSTED_PROXIES"),
		logLevel:             p.str("LOG_LEVEL", "info"),
		serviceRole:          ServiceRole(p.str("SERVICE_ROLE", string(RoleAPI))),
		readinessTimeout:     p.duration("READINESS_TIMEOUT", 2*time.Second),

		httpReadTimeout:  p.duration("HTTP_READ_TIMEOUT", 15*time.Second),
		httpWriteTimeout: p.duration("HTTP_WRITE_TIMEOUT", 30*time.Second),
		httpIdleTimeout:  p.duration("HTTP_IDLE_TIMEOUT", 60*time.Second),
		shutdownTimeout:  p.duration("SHUTDOWN_TIMEOUT", 20*time.Second),

		sessions: SessionSettings{
			student: SessionWindow{
				idleExpiry:     p.duration("STUDENT_SESSION_IDLE_EXPIRY", 7*24*time.Hour),
				absoluteExpiry: p.duration("STUDENT_SESSION_ABSOLUTE_EXPIRY", 30*24*time.Hour),
			},
			instructor: SessionWindow{
				idleExpiry:     p.duration("INSTRUCTOR_SESSION_IDLE_EXPIRY", time.Hour),
				absoluteExpiry: p.duration("INSTRUCTOR_SESSION_ABSOLUTE_EXPIRY", 24*time.Hour),
			},
			admin: SessionWindow{
				idleExpiry:     p.duration("ADMIN_SESSION_IDLE_EXPIRY", 30*time.Minute),
				absoluteExpiry: p.duration("ADMIN_SESSION_ABSOLUTE_EXPIRY", 12*time.Hour),
			},
			generalRecentAuthWindow: p.duration(
				"GENERAL_RECENT_AUTH_WINDOW", 10*time.Minute,
			),
			highestRiskRecentAuthWindow: p.duration(
				"HIGHEST_RISK_RECENT_AUTH_WINDOW", 5*time.Minute,
			),
			staleUseWindow: p.duration("SESSION_STALE_USE_WINDOW", 5*time.Second),
		},

		redis: RedisSettings{
			addr:          p.str("REDIS_ADDR", ""),
			tlsEnabled:    p.boolean("REDIS_TLS_ENABLED", false),
			tlsServerName: p.str("REDIS_TLS_SERVER_NAME", ""),
			tlsCACertFile: p.str("REDIS_TLS_CA_CERT_FILE", ""),
		},

		s3Endpoint:     p.str("S3_ENDPOINT", ""),
		s3Bucket:       p.str("S3_BUCKET", ""),
		s3Region:       p.str("S3_REGION", "us-east-1"),
		s3UsePathStyle: p.boolean("S3_USE_PATH_STYLE", true),

		uploadURLExpiry:    p.duration("UPLOAD_URL_EXPIRY", 15*time.Minute),
		playbackURLExpiry:  p.duration("PLAYBACK_URL_EXPIRY", 5*time.Minute),
		maxUploadSizeBytes: p.integer("MAX_UPLOAD_SIZE_BYTES", 5*1024*1024*1024),

		ffmpegBinaryPath:       p.str("FFMPEG_BINARY_PATH", "ffmpeg"),
		ffprobeBinaryPath:      p.str("FFPROBE_BINARY_PATH", "ffprobe"),
		mediaProcessingTimeout: p.duration("MEDIA_PROCESSING_TIMEOUT", 15*time.Minute),
		mediaOperatingMode:     MediaOperatingMode(p.str("MEDIA_OPERATING_MODE", string(MediaOperatingModeScanner))),
		mediaScannerMode:       MediaScannerMode(p.str("MEDIA_SCANNER_MODE", string(MediaScannerModeUnavailable))),

		authFakeMode: p.boolean("AUTH_FAKE_MODE", false),

		legal: LegalSettings{
			identityMode:       LegalIdentityMode(p.str("LEGAL_IDENTITY_MODE", string(LegalIdentityPublic))),
			operatorName:       p.str("LEGAL_OPERATOR_NAME", ""),
			registrationNumber: p.str("LEGAL_REGISTRATION_NUMBER", ""),
			registeredAddress:  p.str("LEGAL_REGISTERED_ADDRESS", ""),
			privacyEmail:       p.str("PRIVACY_EMAIL", ""),
			supportEmail:       p.str("SUPPORT_EMAIL", ""),
			securityEmail:      p.str("SECURITY_EMAIL", ""),
		},
	}

	// Provider gates are read as settings here and turned into capabilities in
	// validate, where the fail-closed scope for each one is decided.
	tapEnabled := p.boolean("TAP_ENABLED", false)
	tapEnvironment := p.str("TAP_ENVIRONMENT", "test")
	tapAdapterApproved := p.boolean("TAP_ADAPTER_APPROVED", false)
	emailEnabled := p.boolean("EMAIL_ENABLED", false)
	emailProvider := EmailProvider(p.str("EMAIL_PROVIDER", string(EmailProviderResend)))
	registrationEnabled := p.boolean("STUDENT_REGISTRATION_ENABLED", false)
	registrationPolicyApproved := p.boolean("REGISTRATION_POLICY_APPROVED", false)
	passwordAdapterApproved := p.boolean("COMPROMISED_PASSWORD_ADAPTER_APPROVED", false)

	secrets := map[string]Secret{}
	for _, ref := range []SecretRef{
		{Name: "DATABASE_URL", Required: true},
		{Name: "REDIS_USERNAME"},
		{Name: "REDIS_PASSWORD"},
		{Name: "S3_ACCESS_KEY", Required: true},
		{Name: "S3_SECRET_KEY", Required: true},
		{Name: "PLAYBACK_TOKEN_SECRET", Required: true},
		{Name: "TAP_SECRET"},
		{Name: "EMAIL_API_KEY"},
		{Name: "ANONYMOUS_COOKIE_SIGNING_KEY"},
		{Name: "ANONYMOUS_CSRF_KEY"},
		{Name: "ADMISSION_LIMITER_HMAC_KEY"},
		{Name: "OUTBOX_PROTECTED_PAYLOAD_KEY"},
		{Name: "SESSION_CSRF_KEY"},
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
	cfg.redis.username = secrets["REDIS_USERNAME"]
	cfg.redis.password = secrets["REDIS_PASSWORD"]
	cfg.s3AccessKey = secrets["S3_ACCESS_KEY"]
	cfg.s3SecretKey = secrets["S3_SECRET_KEY"]
	cfg.playbackTokenSecret = secrets["PLAYBACK_TOKEN_SECRET"]
	cfg.sessions.csrfKey = secrets["SESSION_CSRF_KEY"]

	cfg.payments = tapCapability(cfg.environment, tapEnabled, tapEnvironment, tapAdapterApproved, secrets["TAP_SECRET"], p)
	cfg.email = emailSettings(emailSettingsInput{
		environment:                cfg.environment,
		serviceRole:                cfg.serviceRole,
		enabled:                    emailEnabled,
		provider:                   emailProvider,
		apiKey:                     secrets["EMAIL_API_KEY"],
		smtpAddr:                   p.str("EMAIL_SMTP_ADDR", ""),
		fromAddress:                p.str("EMAIL_FROM_ADDRESS", "no-reply@gradex.test"),
		fromName:                   p.str("EMAIL_FROM_NAME", "Gradex"),
		replyTo:                    p.str("EMAIL_REPLY_TO", ""),
		timeout:                    p.duration("EMAIL_PROVIDER_TIMEOUT", 10*time.Second),
		publicOrigin:               cfg.publicOrigin,
		protectedPayloadKeyVersion: p.str("OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION", ""),
		protectedPayloadKey:        secrets["OUTBOX_PROTECTED_PAYLOAD_KEY"],
	}, p)
	cfg.admission = admissionCapability(admissionCapabilityInput{
		environment:                cfg.environment,
		enabled:                    registrationEnabled,
		policySetID:                p.str("REGISTRATION_POLICY_SET_ID", ""),
		policyApproved:             registrationPolicyApproved,
		passwordScreenMode:         PasswordScreenMode(p.str("PASSWORD_SCREEN_MODE", string(PasswordScreenUnavailable))),
		passwordAdapterApproved:    passwordAdapterApproved,
		protectedPayloadKeyVersion: p.str("OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION", ""),
		anonymousSessionTTL:        p.duration("ANONYMOUS_SESSION_TTL", 30*time.Minute),
		verificationTokenTTL:       p.duration("VERIFICATION_TOKEN_TTL", 24*time.Hour),
		// Shorter than email verification on purpose: a reset secret replaces a
		// password, so its window of usefulness to an attacker who reaches the
		// mailbox is worth less time.
		passwordResetTokenTTL:      p.duration("PASSWORD_RESET_TOKEN_TTL", time.Hour),
		rateLimitTimeout:           p.duration("ADMISSION_RATE_LIMIT_TIMEOUT", 100*time.Millisecond),
		compromisedPasswordTimeout: p.duration("COMPROMISED_PASSWORD_TIMEOUT", 3*time.Second),
		anonymousCookieSigningKey:  secrets["ANONYMOUS_COOKIE_SIGNING_KEY"],
		anonymousCSRFKey:           secrets["ANONYMOUS_CSRF_KEY"],
		limiterHMACKey:             secrets["ADMISSION_LIMITER_HMAC_KEY"],
		protectedPayloadKey:        secrets["OUTBOX_PROTECTED_PAYLOAD_KEY"],
	}, p)

	p.rejectRetiredKeys()
	cfg.validate(p)

	if err := p.err(); err != nil {
		return nil, err
	}
	return cfg, nil
}

type admissionCapabilityInput struct {
	environment                Environment
	enabled                    bool
	policySetID                string
	policyApproved             bool
	passwordScreenMode         PasswordScreenMode
	passwordAdapterApproved    bool
	protectedPayloadKeyVersion string
	anonymousSessionTTL        time.Duration
	verificationTokenTTL       time.Duration
	passwordResetTokenTTL      time.Duration
	rateLimitTimeout           time.Duration
	compromisedPasswordTimeout time.Duration
	anonymousCookieSigningKey  Secret
	anonymousCSRFKey           Secret
	limiterHMACKey             Secret
	protectedPayloadKey        Secret
}

func admissionCapability(in admissionCapabilityInput, p *parser) AdmissionSettings {
	settings := AdmissionSettings{
		capability:                 disabled("STUDENT_REGISTRATION_ENABLED is false"),
		policySetID:                in.policySetID,
		passwordScreenMode:         in.passwordScreenMode,
		protectedPayloadKeyVersion: in.protectedPayloadKeyVersion,
		anonymousSessionTTL:        in.anonymousSessionTTL,
		verificationTokenTTL:       in.verificationTokenTTL,
		passwordResetTokenTTL:      in.passwordResetTokenTTL,
		rateLimitTimeout:           in.rateLimitTimeout,
		compromisedPasswordTimeout: in.compromisedPasswordTimeout,
		anonymousCookieSigningKey:  in.anonymousCookieSigningKey,
		anonymousCSRFKey:           in.anonymousCSRFKey,
		limiterHMACKey:             in.limiterHMACKey,
		protectedPayloadKey:        in.protectedPayloadKey,
	}

	if !in.passwordScreenMode.Valid() {
		p.errf("PASSWORD_SCREEN_MODE must be unavailable, deterministic, or adapter; got %q", in.passwordScreenMode)
	}
	if in.environment != EnvDevelopment &&
		in.passwordScreenMode == PasswordScreenDeterministic {
		p.errf("deterministic PASSWORD_SCREEN_MODE is permitted only in development")
	}
	if in.environment.IsProduction() {
		if in.passwordScreenMode == PasswordScreenAdapter && !in.passwordAdapterApproved {
			p.errf("production PASSWORD_SCREEN_MODE=adapter requires COMPROMISED_PASSWORD_ADAPTER_APPROVED=true (LG-021)")
		}
	}
	if !in.enabled {
		return settings
	}

	if in.policySetID == "" {
		p.errf("REGISTRATION_POLICY_SET_ID is required when STUDENT_REGISTRATION_ENABLED=true")
	}
	if in.protectedPayloadKeyVersion == "" {
		p.errf("OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION is required when STUDENT_REGISTRATION_ENABLED=true")
	}
	if in.passwordScreenMode == PasswordScreenUnavailable {
		p.errf("PASSWORD_SCREEN_MODE must be deterministic or adapter when STUDENT_REGISTRATION_ENABLED=true")
	}
	for _, required := range []struct {
		name  string
		value Secret
	}{
		{"ANONYMOUS_COOKIE_SIGNING_KEY", in.anonymousCookieSigningKey},
		{"ANONYMOUS_CSRF_KEY", in.anonymousCSRFKey},
		{"ADMISSION_LIMITER_HMAC_KEY", in.limiterHMACKey},
		{"OUTBOX_PROTECTED_PAYLOAD_KEY", in.protectedPayloadKey},
	} {
		if required.value.IsEmpty() {
			p.errf("%s is required when STUDENT_REGISTRATION_ENABLED=true", required.name)
		}
	}

	if in.environment.IsProduction() {
		if !in.policyApproved {
			p.errf("STUDENT_REGISTRATION_ENABLED=true in production requires REGISTRATION_POLICY_APPROVED=true (LG-011)")
		}
		if in.passwordScreenMode != PasswordScreenAdapter || !in.passwordAdapterApproved {
			p.errf("production registration requires PASSWORD_SCREEN_MODE=adapter and COMPROMISED_PASSWORD_ADAPTER_APPROVED=true (LG-021)")
		}
	}

	settings.capability = enabled()
	return settings
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

type emailSettingsInput struct {
	environment                Environment
	serviceRole                ServiceRole
	enabled                    bool
	provider                   EmailProvider
	apiKey                     Secret
	smtpAddr                   string
	fromAddress                string
	fromName                   string
	replyTo                    string
	timeout                    time.Duration
	publicOrigin               string
	protectedPayloadKeyVersion string
	protectedPayloadKey        Secret
}

func emailSettings(in emailSettingsInput, p *parser) EmailSettings {
	settings := EmailSettings{
		capability: disabled("EMAIL_ENABLED is false"), provider: in.provider,
		apiKey: in.apiKey, smtpAddr: strings.TrimSpace(in.smtpAddr), fromAddress: strings.TrimSpace(in.fromAddress),
		fromName: strings.TrimSpace(in.fromName), replyTo: strings.TrimSpace(in.replyTo), timeout: in.timeout,
	}
	if in.timeout < time.Second || in.timeout > 30*time.Second {
		p.errf("EMAIL_PROVIDER_TIMEOUT must be between 1s and 30s")
	}
	if in.provider == EmailProviderMailpit && in.environment != EnvDevelopment {
		p.errf("EMAIL_PROVIDER=mailpit is only allowed when APP_ENV=development")
		return settings
	}
	if !in.enabled {
		if in.environment.IsProduction() && in.serviceRole == RoleWorker {
			p.errf("EMAIL_ENABLED must be true for the production worker")
		}
		return settings
	}
	if err := validateEnabledEmail(in, settings); err != nil {
		p.errf("%s", err)
		return settings
	}
	settings.capability = enabled()
	return settings
}

func validateEnabledEmail(in emailSettingsInput, settings EmailSettings) error {
	if !in.provider.Valid() {
		return fmt.Errorf("EMAIL_PROVIDER must be fake, mailpit, or resend; got %q", in.provider)
	}
	if in.provider == EmailProviderMailpit {
		if err := validateMailpitSMTPAddress(settings.smtpAddr); err != nil {
			return err
		}
	}
	if in.environment.IsProduction() && in.provider != EmailProviderResend {
		return errors.New("production transactional email requires EMAIL_PROVIDER=resend")
	}
	if in.provider == EmailProviderResend && in.apiKey.IsEmpty() {
		return errors.New("EMAIL_API_KEY is required for Resend delivery")
	}
	if err := validateEmailAddresses(settings); err != nil {
		return err
	}
	if strings.TrimSpace(in.protectedPayloadKeyVersion) == "" || in.protectedPayloadKey.IsEmpty() {
		return errors.New("transactional email requires the protected outbox key and version")
	}
	return validateProductionEmail(in, settings)
}

func validateMailpitSMTPAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" {
		return errors.New("EMAIL_SMTP_ADDR must be a loopback host:port for Mailpit delivery")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("EMAIL_SMTP_ADDR must use a loopback IP address for Mailpit delivery")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return errors.New("EMAIL_SMTP_ADDR must contain a valid port for Mailpit delivery")
	}
	return nil
}

func validateEmailAddresses(settings EmailSettings) error {
	if parsed, err := mail.ParseAddress(settings.fromAddress); err != nil || parsed.Address != settings.fromAddress {
		return errors.New("EMAIL_FROM_ADDRESS must be a valid bare address")
	}
	if settings.fromName == "" || len(settings.fromName) > 100 || strings.ContainsAny(settings.fromName, "\r\n\x00") {
		return errors.New("EMAIL_FROM_NAME must be present and safe")
	}
	if settings.replyTo != "" {
		parsed, err := mail.ParseAddress(settings.replyTo)
		if err != nil || parsed.Address != settings.replyTo {
			return errors.New("EMAIL_REPLY_TO must be a valid bare address")
		}
	}
	return nil
}

// providerSandboxSenderDomains are provider-owned domains that exist so an
// integration can send before it owns a verified domain. They deliver, which
// is exactly why they must never reach production: mail would leave under the
// provider's identity rather than a Gradex-controlled, SPF/DKIM/DMARC-aligned
// sending domain, and the address is not one a recipient can reply to or
// report against Gradex.
var providerSandboxSenderDomains = []string{"resend.dev"}

func validateProductionEmail(in emailSettingsInput, settings EmailSettings) error {
	if !in.environment.IsProduction() {
		return nil
	}
	address, _ := mail.ParseAddress(settings.fromAddress)
	domain := strings.ToLower(strings.SplitN(address.Address, "@", 2)[1])
	if domain == "localhost" || strings.HasSuffix(domain, ".test") || strings.HasSuffix(domain, ".example") {
		return errors.New("EMAIL_FROM_ADDRESS must use a real sender domain in production")
	}
	for _, sandbox := range providerSandboxSenderDomains {
		if domain == sandbox || strings.HasSuffix(domain, "."+sandbox) {
			return fmt.Errorf(
				"EMAIL_FROM_ADDRESS must use a Gradex-verified sending domain in production; %q is a provider sandbox domain",
				sandbox,
			)
		}
	}
	if !strings.HasPrefix(in.publicOrigin, "https://") {
		return errors.New("transactional email requires an HTTPS PUBLIC_ORIGIN in production")
	}
	return nil
}

func (c *Config) validate(p *parser) {
	if !c.environment.Valid() {
		p.errf("APP_ENV must be one of development, staging, production; got %q", c.environment)
	}

	if c.redis.addr == "" {
		p.errf("REDIS_ADDR is required")
	}
	if strings.Contains(c.redis.addr, "://") || strings.Contains(c.redis.addr, "@") {
		p.errf("REDIS_ADDR must be a credential-free host:port")
	}
	if !c.redis.username.IsEmpty() && c.redis.password.IsEmpty() {
		p.errf("REDIS_PASSWORD is required when REDIS_USERNAME is configured")
	}
	if !c.redis.tlsEnabled && (c.redis.tlsServerName != "" || c.redis.tlsCACertFile != "") {
		p.errf("REDIS_TLS_SERVER_NAME and REDIS_TLS_CA_CERT_FILE require REDIS_TLS_ENABLED=true")
	}
	if c.environment != EnvDevelopment {
		if c.redis.password.IsEmpty() {
			p.errf("REDIS_PASSWORD is required outside development")
		}
		if !c.redis.tlsEnabled {
			p.errf("REDIS_TLS_ENABLED must be true outside development")
		}
	}
	if c.environment != EnvDevelopment && c.sessions.csrfKey.IsEmpty() {
		p.errf("SESSION_CSRF_KEY is required outside development")
	}
	if c.sessions.Enabled() {
		if _, err := CanonicalPublicOrigin(c.publicOrigin); err != nil {
			p.errf("PUBLIC_ORIGIN must be an exact HTTP origin when authenticated sessions are enabled")
		}
		for _, required := range []struct {
			name  string
			value Secret
		}{
			{"ANONYMOUS_COOKIE_SIGNING_KEY", c.admission.anonymousCookieSigningKey},
			{"ANONYMOUS_CSRF_KEY", c.admission.anonymousCSRFKey},
			{"ADMISSION_LIMITER_HMAC_KEY", c.admission.limiterHMACKey},
		} {
			if required.value.IsEmpty() {
				p.errf("%s is required when authenticated sessions are enabled", required.name)
			}
		}
	}
	if c.admission.Enabled() {
		if _, err := CanonicalPublicOrigin(c.publicOrigin); err != nil {
			p.errf("PUBLIC_ORIGIN must be an exact HTTP origin when STUDENT_REGISTRATION_ENABLED=true")
		}
	}
	c.validateLegalSettings(p)
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
		{"STUDENT_SESSION_IDLE_EXPIRY", c.sessions.student.idleExpiry},
		{"STUDENT_SESSION_ABSOLUTE_EXPIRY", c.sessions.student.absoluteExpiry},
		{"INSTRUCTOR_SESSION_IDLE_EXPIRY", c.sessions.instructor.idleExpiry},
		{"INSTRUCTOR_SESSION_ABSOLUTE_EXPIRY", c.sessions.instructor.absoluteExpiry},
		{"ADMIN_SESSION_IDLE_EXPIRY", c.sessions.admin.idleExpiry},
		{"ADMIN_SESSION_ABSOLUTE_EXPIRY", c.sessions.admin.absoluteExpiry},
		{"GENERAL_RECENT_AUTH_WINDOW", c.sessions.generalRecentAuthWindow},
		{"HIGHEST_RISK_RECENT_AUTH_WINDOW", c.sessions.highestRiskRecentAuthWindow},
		{"SESSION_STALE_USE_WINDOW", c.sessions.staleUseWindow},
		{"READINESS_TIMEOUT", c.readinessTimeout},
		{"UPLOAD_URL_EXPIRY", c.uploadURLExpiry},
		{"PLAYBACK_URL_EXPIRY", c.playbackURLExpiry},
		{"MEDIA_PROCESSING_TIMEOUT", c.mediaProcessingTimeout},
		{"ANONYMOUS_SESSION_TTL", c.admission.anonymousSessionTTL},
		{"VERIFICATION_TOKEN_TTL", c.admission.verificationTokenTTL},
		{"PASSWORD_RESET_TOKEN_TTL", c.admission.passwordResetTokenTTL},
		{"ADMISSION_RATE_LIMIT_TIMEOUT", c.admission.rateLimitTimeout},
		{"COMPROMISED_PASSWORD_TIMEOUT", c.admission.compromisedPasswordTimeout},
	} {
		if d.v <= 0 {
			p.errf("%s must be positive, got %s", d.name, d.v)
		}
	}

	for _, role := range []struct {
		name   string
		window SessionWindow
	}{
		{"STUDENT", c.sessions.student},
		{"INSTRUCTOR", c.sessions.instructor},
		{"ADMIN", c.sessions.admin},
	} {
		if role.window.idleExpiry > 0 && role.window.absoluteExpiry > 0 &&
			role.window.idleExpiry >= role.window.absoluteExpiry {
			p.errf("%s_SESSION_IDLE_EXPIRY (%s) must be less than %s_SESSION_ABSOLUTE_EXPIRY (%s)",
				role.name, role.window.idleExpiry, role.name, role.window.absoluteExpiry)
		}
	}
	if c.sessions.highestRiskRecentAuthWindow > c.sessions.generalRecentAuthWindow {
		p.errf("HIGHEST_RISK_RECENT_AUTH_WINDOW (%s) must not exceed GENERAL_RECENT_AUTH_WINDOW (%s)",
			c.sessions.highestRiskRecentAuthWindow, c.sessions.generalRecentAuthWindow)
	}
	if c.sessions.generalRecentAuthWindow >= c.sessions.admin.idleExpiry {
		p.errf("GENERAL_RECENT_AUTH_WINDOW (%s) must be less than ADMIN_SESSION_IDLE_EXPIRY (%s)",
			c.sessions.generalRecentAuthWindow, c.sessions.admin.idleExpiry)
	}

	if c.maxUploadSizeBytes <= 0 {
		p.errf("MAX_UPLOAD_SIZE_BYTES must be positive, got %d", c.maxUploadSizeBytes)
	}
	if !c.mediaOperatingMode.Valid() {
		p.errf("MEDIA_OPERATING_MODE must be SCANNER or ADMIN_CATALOGUE, got %q", c.mediaOperatingMode)
	}
	if !c.mediaScannerMode.Valid() {
		p.errf("MEDIA_SCANNER_MODE must be UNAVAILABLE or DEVELOPMENT_NO_OP, got %q", c.mediaScannerMode)
	}
	// The no-op scanner inspects nothing, so it is refused anywhere real content
	// could exist. Only a development environment may build it.
	if c.mediaScannerMode == MediaScannerModeDevelopmentNoOp && c.environment != EnvDevelopment {
		p.errf("MEDIA_SCANNER_MODE=DEVELOPMENT_NO_OP is refused when APP_ENV=%s", c.environment)
	}

	switch c.logLevel {
	case "debug", "info", "warn", "error":
	default:
		p.errf("LOG_LEVEL must be one of debug, info, warn, error; got %q", c.logLevel)
	}

	if !c.serviceRole.Valid() {
		p.errf("SERVICE_ROLE must be one of api, worker; got %q", c.serviceRole)
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

func (c *Config) validateLegalSettings(p *parser) {
	if c.environment == EnvDevelopment {
		return
	}
	if !c.legal.identityMode.Valid() {
		p.errf("LEGAL_IDENTITY_MODE must be public or controlled-staging; got %q", c.legal.identityMode)
	}
	validateRequiredLegalSettings(c.legal, p)
	validateLegalEmails(c.legal, p)
	origin, err := CanonicalPublicOrigin(c.publicOrigin)
	if err != nil || !strings.HasPrefix(origin, "https://") {
		p.errf("PUBLIC_ORIGIN must be an exact HTTPS origin for legal policies")
	}
	c.validateLegalIdentityMode(origin, p)
}

func validateRequiredLegalSettings(legal LegalSettings, p *parser) {
	for _, required := range []struct {
		name  string
		value string
	}{
		{"LEGAL_OPERATOR_NAME", legal.operatorName},
		{"LEGAL_REGISTRATION_NUMBER", legal.registrationNumber},
		{"LEGAL_REGISTERED_ADDRESS", legal.registeredAddress},
		{"PRIVACY_EMAIL", legal.privacyEmail},
		{"SUPPORT_EMAIL", legal.supportEmail},
		{"SECURITY_EMAIL", legal.securityEmail},
	} {
		if strings.TrimSpace(required.value) == "" {
			p.errf("%s is required outside development", required.name)
		}
	}
}

func validateLegalEmails(legal LegalSettings, p *parser) {
	for _, contact := range []struct {
		name  string
		value string
	}{
		{"PRIVACY_EMAIL", legal.privacyEmail},
		{"SUPPORT_EMAIL", legal.supportEmail},
		{"SECURITY_EMAIL", legal.securityEmail},
	} {
		address, err := mail.ParseAddress(contact.value)
		if err != nil || address.Address != contact.value {
			p.errf("%s must be a valid email address", contact.name)
		}
	}
}

func (c *Config) validateLegalIdentityMode(origin string, p *parser) {
	hasSentinel := c.legal.registrationNumber == StagingLegalRegistrationNumber ||
		c.legal.registeredAddress == StagingLegalRegisteredAddress
	switch c.legal.identityMode {
	case LegalIdentityPublic:
		if hasSentinel {
			p.errf("public legal identity rejects controlled-staging sentinel values")
		}
	case LegalIdentityControlledStaging:
		if c.environment != EnvProduction || origin != ControlledStagingPublicOrigin ||
			c.legal.registrationNumber != StagingLegalRegistrationNumber ||
			c.legal.registeredAddress != StagingLegalRegisteredAddress {
			p.errf("controlled-staging legal identity requires the exact disposable S11 origin and sentinel values")
		}
	}
}

// retiredKeys maps settings this package used to read to their replacements.
// Renaming a key without this check is a silent failure: the old value stays
// in the deployment, the new key falls back to its default, and nothing
// reports that the operator's intent was dropped.
var retiredKeys = map[string]string{
	"UPLOAD_URL_EXPIRY_MINUTES":   "UPLOAD_URL_EXPIRY (a duration, e.g. \"15m\")",
	"PLAYBACK_URL_EXPIRY_MINUTES": "PLAYBACK_URL_EXPIRY (a duration, e.g. \"5m\")",
	"SESSION_IDLE_EXPIRY":         "the role-specific *_SESSION_IDLE_EXPIRY settings",
	"SESSION_ABSOLUTE_EXPIRY":     "the role-specific *_SESSION_ABSOLUTE_EXPIRY settings",
	"RECENT_AUTH_WINDOW":          "GENERAL_RECENT_AUTH_WINDOW and HIGHEST_RISK_RECENT_AUTH_WINDOW",
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
