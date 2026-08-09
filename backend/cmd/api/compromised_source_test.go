package main

import (
	"strings"
	"testing"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/identity"
)

func productionAdmissionConfig(t *testing.T) *config.Config {
	t.Helper()
	settings := map[string]string{
		"APP_ENV":                               "production",
		"PUBLIC_ORIGIN":                         "https://gradex.example",
		"CORS_ALLOWED_ORIGINS":                  "https://gradex.example",
		"CORS_ALLOW_CREDENTIALS":                "true",
		"REDIS_ADDR":                            "redis:6379",
		"REDIS_TLS_ENABLED":                     "true",
		"S3_ENDPOINT":                           "https://storage.example",
		"S3_BUCKET":                             "gradex-media",
		"AUTH_FAKE_MODE":                        "false",
		"STUDENT_REGISTRATION_ENABLED":          "true",
		"REGISTRATION_POLICY_SET_ID":            "approved-policy-set",
		"REGISTRATION_POLICY_APPROVED":          "true",
		"PASSWORD_SCREEN_MODE":                  "adapter",
		"COMPROMISED_PASSWORD_ADAPTER_APPROVED": "true",
		"OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION":  "prod-v1",
	}
	secrets := config.MapSecretResolver{
		"DATABASE_URL":                 "postgres://gradex:pw@db:5432/gradex",
		"REDIS_PASSWORD":               "redis-password",
		"S3_ACCESS_KEY":                "access",
		"S3_SECRET_KEY":                "secret",
		"PLAYBACK_TOKEN_SECRET":        strings.Repeat("p", 32),
		"SESSION_CSRF_KEY":             strings.Repeat("s", 32),
		"ANONYMOUS_COOKIE_SIGNING_KEY": strings.Repeat("a", 32),
		"ANONYMOUS_CSRF_KEY":           strings.Repeat("b", 32),
		"ADMISSION_LIMITER_HMAC_KEY":   strings.Repeat("c", 32),
		"OUTBOX_PROTECTED_PAYLOAD_KEY": strings.Repeat("d", 32),
	}
	cfg, err := config.LoadFrom(config.MapLookup(settings), secrets)
	if err != nil {
		t.Fatalf("loading production admission config: %v", err)
	}
	return cfg
}

func TestProductionCompositionSelectsHIBPAndStopsAtLG011(t *testing.T) {
	cfg := productionAdmissionConfig(t)
	source, err := buildCompromisedPasswordSource(cfg)
	if err != nil {
		t.Fatalf("building production compromised-password source: %v", err)
	}
	if source.Scheme() != identity.CompromisedSHA1V1 || source.PrefixLength() != 5 {
		t.Fatalf("production source = %s/%d, want HIBP SHA-1/5", source.Scheme(), source.PrefixLength())
	}

	_, _, err = buildAdmissionFoundation(cfg, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "LG-011") {
		t.Fatalf("production admission error = %v, want remaining LG-011 fail-closed boundary", err)
	}
}
