package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestOfflinePreflightDoesNotRequireUnrelatedRuntimeDependencies(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("SERVICE_ROLE", "worker")
	t.Setenv("PUBLIC_ORIGIN", "https://staging.gradex.network")

	t.Setenv("EMAIL_ENABLED", "true")
	t.Setenv("EMAIL_PROVIDER", "resend")
	t.Setenv("EMAIL_API_KEY", "re_preflight_test")
	t.Setenv("EMAIL_FROM_ADDRESS", "no-reply@updates.gradex.network")
	t.Setenv("EMAIL_FROM_NAME", "Gradex")
	t.Setenv("EMAIL_PROVIDER_TIMEOUT", "10s")

	t.Setenv("OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION", "test-v1")
	t.Setenv(
		"OUTBOX_PROTECTED_PAYLOAD_KEY",
		"12345678901234567890123456789012",
	)

	// Deliberately absent: an email-only launch gate must not depend on them.
	t.Setenv("DATABASE_URL", "")
	t.Setenv("REDIS_ADDR", "")
	t.Setenv("REDIS_PASSWORD", "")
	t.Setenv("S3_ENDPOINT", "")
	t.Setenv("S3_BUCKET", "")
	t.Setenv("S3_ACCESS_KEY", "")
	t.Setenv("S3_SECRET_KEY", "")
	t.Setenv("LEGAL_REGISTRATION_NUMBER", "")
	t.Setenv("LEGAL_REGISTERED_ADDRESS", "")

	var stdout bytes.Buffer
	err := run([]string{"-offline"}, &stdout)
	if err != nil {
		t.Fatalf(
			"offline email preflight was coupled to unrelated runtime dependencies: %v",
			err,
		)
	}

	if !strings.Contains(stdout.String(), "result: CONFIGURATION OK") {
		t.Fatalf("unexpected output:\n%s", stdout.String())
	}
}
