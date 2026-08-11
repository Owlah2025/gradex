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
	if err := os.WriteFile(apiLog, []byte(`{"timestamp":"now","route_template":"/api/v1/media/assets/:id","method":"GET","status":200,"duration_ms":3,"cookie":"must-not-retain"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workerLog, []byte(`{"timestamp":"now","msg":"worker_failed","operation":"job_process","error_class":"timeout","password":"must-not-retain"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifact := Artifact{Runtime: safeRuntime(map[string]map[string]string{"api": {"APP_ENV": "development", "S3_SECRET_KEY": "must-not-retain"}, "worker": {"REDIS_ADDR": "localhost:6379", "SESSION_CSRF_KEY": "must-not-retain"}}), Logs: Logs{API: apiLogs(apiLog), Worker: workerLogs(workerLog)}}
	encoded, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"S3_SECRET_KEY", "SESSION_CSRF_KEY", "must-not-retain", "cookie", "password"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("artifact retained %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, `"route":"/api/v1/media/assets/:id"`) || !strings.Contains(text, `"operation":"job_process"`) {
		t.Fatalf("artifact omitted safe correlation fields: %s", text)
	}
}
