package main

import (
	"strings"
	"testing"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

// S1B1 T017: every admission dependency composes at startup while Student
// mutation routes remain a later, explicit router step.
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
	foundation, client, err := buildAdmissionFoundation(cfg)
	if err != nil {
		t.Fatalf("building foundation: %v", err)
	}
	if foundation == nil || client == nil {
		t.Fatal("foundation composition omitted a required dependency")
	}
	_ = client.Close()
}
