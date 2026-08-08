package queue

import (
	"crypto/tls"
	"encoding/pem"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

func loadRedisSettings(
	t *testing.T,
	mutate func(map[string]string, config.MapSecretResolver),
) config.RedisSettings {
	t.Helper()
	settings := map[string]string{
		"APP_ENV": "production", "PUBLIC_ORIGIN": "https://gradex.example",
		"CORS_ALLOWED_ORIGINS": "https://gradex.example", "CORS_ALLOW_CREDENTIALS": "true",
		"REDIS_ADDR": "redis.internal:6379", "REDIS_TLS_ENABLED": "true",
		"S3_ENDPOINT": "https://storage.example", "S3_BUCKET": "gradex-media",
	}
	secrets := config.MapSecretResolver{
		"DATABASE_URL": "postgres://gradex:pw@db:5432/gradex", "REDIS_PASSWORD": "redis-password-canary",
		"S3_ACCESS_KEY": "access", "S3_SECRET_KEY": "secret", "PLAYBACK_TOKEN_SECRET": strings.Repeat("p", 32),
		"SESSION_CSRF_KEY": strings.Repeat("s", 32), "ANONYMOUS_COOKIE_SIGNING_KEY": strings.Repeat("a", 32),
		"ANONYMOUS_CSRF_KEY": strings.Repeat("b", 32), "ADMISSION_LIMITER_HMAC_KEY": strings.Repeat("c", 32),
	}
	if mutate != nil {
		mutate(settings, secrets)
	}
	cfg, err := config.LoadFrom(config.MapLookup(settings), secrets)
	if err != nil {
		t.Fatalf("loading Redis test settings: %v", err)
	}
	return cfg.Redis()
}

func TestDevelopmentConnectionPermitsPlaintextWithoutAuthentication(t *testing.T) {
	settings := loadRedisSettings(t, func(values map[string]string, secrets config.MapSecretResolver) {
		values["APP_ENV"] = "development"
		values["REDIS_TLS_ENABLED"] = "false"
		delete(secrets, "REDIS_PASSWORD")
	})
	connection, err := NewConnection(settings)
	if err != nil {
		t.Fatalf("building local Redis connection: %v", err)
	}
	options := connection.redisOptions()
	if options.TLSConfig != nil || options.Username != "" || options.Password != "" {
		t.Fatal("local Redis options unexpectedly enabled TLS or authentication")
	}
}

func TestPasswordOnlyAuthenticationReachesBothRedisLibraries(t *testing.T) {
	connection, err := NewConnection(loadRedisSettings(t, nil))
	if err != nil {
		t.Fatalf("building password-authenticated Redis connection: %v", err)
	}
	assertCompatibleOptions(t, connection, "", "redis-password-canary")
}

func TestACLAuthenticationAndVerifiedTLSReachBothRedisLibraries(t *testing.T) {
	caFile := trustedTestCertificate(t)
	settings := loadRedisSettings(t, func(values map[string]string, secrets config.MapSecretResolver) {
		values["REDIS_TLS_SERVER_NAME"] = "redis.provider.example"
		values["REDIS_TLS_CA_CERT_FILE"] = caFile
		secrets["REDIS_USERNAME"] = "gradex-acl"
	})
	connection, err := NewConnection(settings)
	if err != nil {
		t.Fatalf("building ACL Redis connection: %v", err)
	}
	assertCompatibleOptions(t, connection, "gradex-acl", "redis-password-canary")
	options := connection.redisOptions()
	if options.TLSConfig == nil {
		t.Fatal("Redis TLS configuration is absent")
	}
	if options.TLSConfig.ServerName != "redis.provider.example" {
		t.Fatalf("Redis TLS server name = %q", options.TLSConfig.ServerName)
	}
	if options.TLSConfig.MinVersion != tls.VersionTLS12 || options.TLSConfig.InsecureSkipVerify {
		t.Fatal("Redis TLS does not enforce TLS 1.2+ with certificate verification")
	}
}

func TestInvalidCACertificateFailsWithoutCredentialDisclosure(t *testing.T) {
	caFile := filepath.Join(t.TempDir(), "invalid-ca.pem")
	if err := os.WriteFile(caFile, []byte("not a certificate"), 0o600); err != nil {
		t.Fatalf("writing invalid CA fixture: %v", err)
	}
	settings := loadRedisSettings(t, func(values map[string]string, _ config.MapSecretResolver) {
		values["REDIS_TLS_CA_CERT_FILE"] = caFile
	})
	_, err := NewConnection(settings)
	if err == nil {
		t.Fatal("invalid Redis CA certificate was accepted")
	}
	if strings.Contains(fmt.Sprint(err), "redis-password-canary") {
		t.Fatal("Redis credential appeared in TLS configuration error")
	}
}

func assertCompatibleOptions(t *testing.T, connection *Connection, username, password string) {
	t.Helper()
	driverOptions := connection.redisOptions()
	queueOptions := connection.asynqOptions()
	if driverOptions.Addr != queueOptions.Addr || driverOptions.Username != queueOptions.Username ||
		driverOptions.Password != queueOptions.Password {
		t.Fatal("go-redis and asynq authentication options diverged")
	}
	if driverOptions.Username != username || driverOptions.Password != password {
		t.Fatal("Redis authentication options did not preserve the validated credentials")
	}
	if (driverOptions.TLSConfig == nil) != (queueOptions.TLSConfig == nil) {
		t.Fatal("go-redis and asynq TLS options diverged")
	}
}

func trustedTestCertificate(t *testing.T) string {
	t.Helper()
	server := httptest.NewTLSServer(nil)
	defer server.Close()
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	path := filepath.Join(t.TempDir(), "redis-ca.pem")
	if err := os.WriteFile(path, certificate, 0o600); err != nil {
		t.Fatalf("writing CA fixture: %v", err)
	}
	return path
}
