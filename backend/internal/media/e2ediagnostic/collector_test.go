package e2ediagnostic

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression guard for T035a: a failure artifact must remain useful without
// copying credentials or arbitrary JSON log fields into a retained file.
func TestFailureArtifactSanitizesRuntimeAndLogs(t *testing.T) {
	dir := t.TempDir()
	apiLog := filepath.Join(dir, "api.log")
	workerLog := filepath.Join(dir, "worker.log")
	if err := os.WriteFile(apiLog, []byte(`{"timestamp":"now","route_template":"/api/v1/media/assets/:id","method":"GET","status":200,"duration_ms":3,"cookie":"cookie-canary","csrf_token":"csrf-canary","upload_credential":"upload-canary","playback_credential":"playback-canary","presigned_url":"https://storage.invalid/object?signature=presigned-canary","object_key":"private-object-canary"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workerLog, []byte(`{"timestamp":"now","msg":"worker_failed","operation":"job_process","error_class":"timeout","password":"database-password-canary","redis_password":"redis-password-canary","resend_api_key":"resend-key-canary","storage_secret":"storage-secret-canary","session_secret":"session-secret-canary","encrypted_payload":"ciphertext-canary","invitation_token":"invitation-canary","reset_token":"reset-canary","verification_token":"verification-canary"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifact := Artifact{Runtime: safeRuntime(map[string]map[string]string{
		"api": {
			"APP_ENV":                      "development",
			"DATABASE_URL":                 "postgres://user:database-password-canary@localhost/db",
			"S3_ACCESS_KEY":                "storage-access-canary",
			"S3_SECRET_KEY":                "storage-secret-canary",
			"RESEND_API_KEY":               "resend-key-canary",
			"OUTBOX_PROTECTED_PAYLOAD_KEY": "outbox-key-canary",
		},
		"worker": {
			"REDIS_ADDR":                 "localhost:6379",
			"REDIS_PASSWORD":             "redis-password-canary",
			"SESSION_CSRF_KEY":           "session-secret-canary",
			"ANONYMOUS_CSRF_KEY":         "csrf-canary",
			"PLAYBACK_TOKEN_SECRET":      "playback-canary",
			"S3_SECRET_KEY":              "storage-secret-canary",
			"ADMISSION_LIMITER_HMAC_KEY": "limiter-key-canary",
		},
	}), Logs: Logs{API: apiLogs(apiLog), Worker: workerLogs(workerLog)}}
	output := filepath.Join(dir, "failure.json")
	if err := Write(output, artifact); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{
		"DATABASE_URL", "database-password-canary", "REDIS_PASSWORD", "redis-password-canary",
		"RESEND_API_KEY", "resend-key-canary", "S3_ACCESS_KEY", "storage-access-canary",
		"S3_SECRET_KEY", "storage-secret-canary", "SESSION_CSRF_KEY", "session-secret-canary",
		"ANONYMOUS_CSRF_KEY", "csrf-canary", "upload-canary", "playback-canary",
		"presigned-canary", "private-object-canary", "ciphertext-canary", "outbox-key-canary",
		"invitation-canary", "reset-canary", "verification-canary", "limiter-key-canary",
		"cookie", "password", "secret", "token", "credential", "presigned_url", "object_key",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("artifact retained %q: %s", forbidden, text)
		}
	}
	var retained Artifact
	if err := json.Unmarshal(encoded, &retained); err != nil {
		t.Fatal(err)
	}
	if len(retained.Logs.API) != 1 || retained.Logs.API[0].Route != "/api/v1/media/assets/:id" ||
		len(retained.Logs.Worker) != 1 || retained.Logs.Worker[0].Operation != "job_process" {
		t.Fatalf("artifact omitted safe correlation fields: %#v", retained.Logs)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("artifact permissions = %04o, want 0600", got)
	}
}
