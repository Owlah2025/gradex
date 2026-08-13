package config

import "time"

// EmailPreflightConfig is the narrow configuration boundary used by the
// transactional-email launch gate.
//
// It deliberately excludes unrelated runtime dependencies such as PostgreSQL,
// Redis, object storage, media, sessions, and legal identity, while reusing
// the same email validation used by the API and worker.
type EmailPreflightConfig struct {
	environment  Environment
	publicOrigin string
	email        EmailSettings
}

func (c *EmailPreflightConfig) Environment() Environment { return c.environment }
func (c *EmailPreflightConfig) PublicOrigin() string     { return c.publicOrigin }
func (c *EmailPreflightConfig) Email() EmailSettings     { return c.email }

// LoadEmailPreflight reads only the configuration required to prove
// transactional-email launch readiness.
func LoadEmailPreflight() (*EmailPreflightConfig, error) {
	return LoadEmailPreflightFrom(OSLookup, EnvSecretResolver{})
}

// LoadEmailPreflightFrom is the injectable form used by tests.
func LoadEmailPreflightFrom(
	lookup Lookup,
	resolver SecretResolver,
) (*EmailPreflightConfig, error) {
	p := &parser{lookup: lookup}

	environment := Environment(p.str("APP_ENV", string(EnvDevelopment)))
	serviceRole := ServiceRole(p.str("SERVICE_ROLE", string(RoleAPI)))
	publicOrigin := p.str("PUBLIC_ORIGIN", "")

	if !environment.Valid() {
		p.errf(
			"APP_ENV must be one of development, staging, production; got %q",
			environment,
		)
	}
	if !serviceRole.Valid() {
		p.errf("SERVICE_ROLE must be api or worker; got %q", serviceRole)
	}

	resolve := func(name string) Secret {
		secret, err := resolver.Resolve(SecretRef{Name: name})
		if err != nil {
			p.errf("resolving secret %s: %v", name, err)
			return Secret{}
		}
		return secret
	}

	settings := emailSettings(emailSettingsInput{
		environment:                environment,
		serviceRole:                serviceRole,
		enabled:                    p.boolean("EMAIL_ENABLED", false),
		provider:                   EmailProvider(p.str("EMAIL_PROVIDER", string(EmailProviderResend))),
		apiKey:                     resolve("EMAIL_API_KEY"),
		smtpAddr:                   p.str("EMAIL_SMTP_ADDR", ""),
		fromAddress:                p.str("EMAIL_FROM_ADDRESS", "no-reply@gradex.test"),
		fromName:                   p.str("EMAIL_FROM_NAME", "Gradex"),
		replyTo:                    p.str("EMAIL_REPLY_TO", ""),
		timeout:                    p.duration("EMAIL_PROVIDER_TIMEOUT", 10*time.Second),
		publicOrigin:               publicOrigin,
		protectedPayloadKeyVersion: p.str("OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION", ""),
		protectedPayloadKey:        resolve("OUTBOX_PROTECTED_PAYLOAD_KEY"),
	}, p)

	if err := p.err(); err != nil {
		return nil, err
	}

	return &EmailPreflightConfig{
		environment:  environment,
		publicOrigin: publicOrigin,
		email:        settings,
	}, nil
}
