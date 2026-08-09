package main

import (
	"bytes"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

func workerConfig(t *testing.T, emailEnabled bool) *config.Config {
	t.Helper()
	values := map[string]string{
		"APP_ENV": "development", "SERVICE_ROLE": "worker", "REDIS_ADDR": "localhost:6379",
		"PUBLIC_ORIGIN": "http://localhost:3000",
		"S3_ENDPOINT":   "http://localhost:9000", "S3_BUCKET": "gradex-test",
		"EMAIL_PROVIDER": "fake", "OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION": "test-v1",
	}
	if emailEnabled {
		values["EMAIL_ENABLED"] = "true"
	}
	settings, err := config.LoadFrom(config.MapLookup(values), config.MapSecretResolver{
		"DATABASE_URL": "postgres://x", "S3_ACCESS_KEY": "a", "S3_SECRET_KEY": "b",
		"PLAYBACK_TOKEN_SECRET": "c", "OUTBOX_PROTECTED_PAYLOAD_KEY": "12345678901234567890123456789012",
	})
	if err != nil {
		t.Fatalf("loading worker configuration: %v", err)
	}
	return settings
}

func TestWorkerBuildsConfiguredFakeTransactionalEmailDispatcher(t *testing.T) {
	settings := workerConfig(t, true)
	writer, err := outbox.NewWriter("test-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := buildTransactionalEmailDispatcher(transactionalEmailDependencies{
		pool: &pgxpool.Pool{}, config: settings, outbox: writer,
	})
	if err != nil {
		t.Fatalf("building dispatcher: %v", err)
	}
	if dispatcher == nil {
		t.Fatal("enabled development email produced no dispatcher")
	}
}

func TestWorkerOmitsTransactionalEmailDispatcherWhenDisabledOutsideProduction(t *testing.T) {
	dispatcher, err := buildTransactionalEmailDispatcher(transactionalEmailDependencies{config: workerConfig(t, false)})
	if err != nil {
		t.Fatal(err)
	}
	if dispatcher != nil {
		t.Fatal("disabled development email produced a dispatcher")
	}
}
