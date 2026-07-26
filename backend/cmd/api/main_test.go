package main

import (
	"context"
	"strings"
	"testing"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

// S1B1 T017: every admission dependency composes at startup before the router
// can mount the public Student mutation boundary.
func TestBuildDevelopmentAdmissionFoundation(t *testing.T) {
	settings := config.MapLookup(map[string]string{
		"APP_ENV":                              "development",
		"PUBLIC_ORIGIN":                        "http://localhost:3000",
		"REDIS_ADDR":                           "localhost:6379",
		"S3_ENDPOINT":                          "http://localhost:9000",
		"S3_BUCKET":                            "gradex-test",
		"STUDENT_REGISTRATION_ENABLED":         "true",
		"REGISTRATION_POLICY_SET_ID":           "dev-registration-v1",
		"PASSWORD_SCREEN_MODE":                 "deterministic",
		"OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION": "dev-v1",
	})
	secrets := config.MapSecretResolver{
		"DATABASE_URL":                 "postgres://x",
		"S3_ACCESS_KEY":                "a",
		"S3_SECRET_KEY":                "b",
		"PLAYBACK_TOKEN_SECRET":        "playback",
		"ANONYMOUS_COOKIE_SIGNING_KEY": strings.Repeat("a", 32),
		"ANONYMOUS_CSRF_KEY":           strings.Repeat("b", 32),
		"ADMISSION_LIMITER_HMAC_KEY":   strings.Repeat("c", 32),
		"OUTBOX_PROTECTED_PAYLOAD_KEY": strings.Repeat("d", 32),
	}
	cfg, err := config.LoadFrom(settings, secrets)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), "postgres://test:test@localhost/test")
	if err != nil {
		t.Fatalf("constructing test pool: %v", err)
	}
	defer pool.Close()
	foundation, client, err := buildAdmissionFoundation(cfg, pool)
	if err != nil {
		t.Fatalf("building foundation: %v", err)
	}
	if foundation == nil || client == nil {
		t.Fatal("foundation composition omitted a required dependency")
	}
	_ = client.Close()
}

func TestBuildAdmissionFoundationRejectsNonDevelopmentFixtures(t *testing.T) {
	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV":                      "production",
		"PUBLIC_ORIGIN":                "https://gradex.example",
		"CORS_ALLOWED_ORIGINS":         "https://gradex.example",
		"REDIS_ADDR":                   "redis:6379",
		"S3_ENDPOINT":                  "https://storage.example",
		"S3_BUCKET":                    "gradex-test",
		"PASSWORD_SCREEN_MODE":         "unavailable",
		"STUDENT_REGISTRATION_ENABLED": "false",
	}), config.MapSecretResolver{
		"DATABASE_URL":          "postgres://x",
		"S3_ACCESS_KEY":         "a",
		"S3_SECRET_KEY":         "b",
		"PLAYBACK_TOKEN_SECRET": "playback",
	})
	if err != nil {
		t.Fatalf("loading production config: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), "postgres://test:test@localhost/test")
	if err != nil {
		t.Fatalf("constructing test pool: %v", err)
	}
	defer pool.Close()
	if _, _, err := buildAdmissionFoundation(cfg, pool); err == nil {
		t.Fatal("production API composed development admission fixtures")
	}
}

func TestRequiredSchemaVersionFollowsEnabledCapabilities(t *testing.T) {
	tests := map[string]struct {
		settings map[string]string
		secrets  config.MapSecretResolver
		want     int64
	}{
		"development fake auth": {
			settings: map[string]string{
				"APP_ENV": "development", "AUTH_FAKE_MODE": "true",
				"REDIS_ADDR": "localhost:6379", "S3_ENDPOINT": "http://localhost:9000",
				"S3_BUCKET": "gradex-test",
			},
			secrets: config.MapSecretResolver{
				"DATABASE_URL": "postgres://x", "S3_ACCESS_KEY": "a",
				"S3_SECRET_KEY": "b", "PLAYBACK_TOKEN_SECRET": "c",
			},
			want: db.MinSchemaVersion,
		},
		"real sessions": {
			settings: map[string]string{
				"APP_ENV": "development", "AUTH_FAKE_MODE": "false",
				"REDIS_ADDR": "localhost:6379", "S3_ENDPOINT": "http://localhost:9000",
				"S3_BUCKET": "gradex-test",
			},
			secrets: config.MapSecretResolver{
				"DATABASE_URL": "postgres://x", "S3_ACCESS_KEY": "a",
				"S3_SECRET_KEY": "b", "PLAYBACK_TOKEN_SECRET": "c",
			},
			want: db.SessionSchemaVersion,
		},
		"Student admission": {
			settings: map[string]string{
				"APP_ENV": "development", "AUTH_FAKE_MODE": "true",
				"PUBLIC_ORIGIN": "http://localhost:3000", "REDIS_ADDR": "localhost:6379",
				"S3_ENDPOINT": "http://localhost:9000", "S3_BUCKET": "gradex-test",
				"STUDENT_REGISTRATION_ENABLED":         "true",
				"REGISTRATION_POLICY_SET_ID":           "dev-registration-v1",
				"PASSWORD_SCREEN_MODE":                 "deterministic",
				"OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION": "dev-v1",
			},
			secrets: config.MapSecretResolver{
				"DATABASE_URL": "postgres://x", "S3_ACCESS_KEY": "a",
				"S3_SECRET_KEY": "b", "PLAYBACK_TOKEN_SECRET": "c",
				"ANONYMOUS_COOKIE_SIGNING_KEY": strings.Repeat("a", 32),
				"ANONYMOUS_CSRF_KEY":           strings.Repeat("b", 32),
				"ADMISSION_LIMITER_HMAC_KEY":   strings.Repeat("c", 32),
				"OUTBOX_PROTECTED_PAYLOAD_KEY": strings.Repeat("d", 32),
			},
			want: db.AdmissionSchemaVersion,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			cfg, err := config.LoadFrom(config.MapLookup(test.settings), test.secrets)
			if err != nil {
				t.Fatalf("loading config: %v", err)
			}
			if got := requiredSchemaVersion(cfg); got != test.want {
				t.Fatalf("required schema = %d, want %d", got, test.want)
			}
		})
	}
}
