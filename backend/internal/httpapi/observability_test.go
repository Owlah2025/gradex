package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/requestid"
)

func init() { gin.SetMode(gin.TestMode) }

// syncBuffer collects log output safely under the concurrency test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// records parses each JSON log line the test produced.
func (b *syncBuffer) records(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line is not JSON: %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}

func (b *syncBuffer) requestRecord(t *testing.T) map[string]any {
	t.Helper()
	for _, rec := range b.records(t) {
		if rec["msg"] == "http_request" {
			return rec
		}
	}
	t.Fatal("no http_request log record was emitted")
	return nil
}

// testEngine builds the real middleware chain plus a handful of routes that
// stand in for handler behaviour: success, a validation problem, and a panic.
func testEngine(t *testing.T) (*gin.Engine, *syncBuffer) {
	t.Helper()

	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV":     "development",
		"REDIS_ADDR":  "localhost:6379",
		"S3_ENDPOINT": "http://localhost:9000",
		"S3_BUCKET":   "gradex-test",
	}), config.MapSecretResolver{
		"DATABASE_URL":          "postgres://x",
		"S3_ACCESS_KEY":         "a",
		"S3_SECRET_KEY":         "b",
		"PLAYBACK_TOKEN_SECRET": "c",
	})
	if err != nil {
		t.Fatalf("loading test config: %v", err)
	}

	buf := &syncBuffer{}
	logger := logging.New(buf, "gradex-api-test", string(cfg.Environment()), logging.LevelFromString("info"))

	r, err := newEngine(cfg, logger)
	if err != nil {
		t.Fatalf("building engine: %v", err)
	}

	r.GET("/api/v1/courses/:courseID", func(c *gin.Context) {
		// Echo the trusted ID the context carries so the test can compare the
		// context value against the header and the log.
		c.JSON(http.StatusOK, gin.H{"seen_request_id": requestid.FromContext(c.Request.Context())})
	})
	r.POST("/api/v1/courses/:courseID", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	r.GET("/api/v1/validate", func(c *gin.Context) {
		writeProblem(c, problem.New(http.StatusUnprocessableEntity, "validation-failed",
			"Request validation failed", "One or more fields are invalid.").
			WithViolations(problem.Violation{
				Code: "REQUIRED", Detail: "Email is required.",
				Location: problem.LocationBody, Pointer: "#/email",
			}))
	})
	r.GET("/api/v1/boom", func(c *gin.Context) {
		panic("handler exploded with secret-token-abc123 and student@example.com")
	})
	r.GET("/api/v1/boom-after-write", func(c *gin.Context) {
		c.String(http.StatusOK, "partial")
		panic("exploded after committing the response")
	})

	return r, buf
}

func do(r *gin.Engine, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) problem.Problem {
	t.Helper()
	var p problem.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("response body is not a Problem: %q: %v", rec.Body.String(), err)
	}
	return p
}

// A client-supplied X-Request-ID must never become the identifier Gradex
// reports. It is kept beside the trusted one as a parent hint.
func TestClientRequestIDNeverReplacesTrustedID(t *testing.T) {
	r, buf := testEngine(t)

	const clientValue = "client-supplied-abc123"
	req := httptest.NewRequest(http.MethodGet, "/api/v1/courses/course-42", nil)
	req.Header.Set(requestid.HeaderName, clientValue)

	rec := do(r, req)

	trusted := rec.Header().Get(requestid.HeaderName)
	if trusted == "" {
		t.Fatal("no trusted request ID was returned")
	}
	if trusted == clientValue {
		t.Fatal("the client-supplied value was adopted as the trusted ID")
	}

	rc := buf.requestRecord(t)
	if rc["request_id"] != trusted {
		t.Errorf("log request_id %v does not match header %q", rc["request_id"], trusted)
	}
	if rc["parent_request_id"] != clientValue {
		t.Errorf("parent_request_id = %v, want the sanitized client value %q", rc["parent_request_id"], clientValue)
	}
}

// Header, request context, log record, and Problem Details body must all carry
// the same trusted value, or correlation is useless.
func TestTrustedIDIsConsistentAcrossHeaderContextAndLog(t *testing.T) {
	r, buf := testEngine(t)

	rec := do(r, httptest.NewRequest(http.MethodGet, "/api/v1/courses/course-42", nil))

	header := rec.Header().Get(requestid.HeaderName)
	var body struct {
		SeenRequestID string `json:"seen_request_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}

	if body.SeenRequestID != header {
		t.Errorf("context ID %q != header ID %q", body.SeenRequestID, header)
	}
	if got := buf.requestRecord(t)["request_id"]; got != header {
		t.Errorf("log ID %v != header ID %q", got, header)
	}
}

func TestProblemDetailsCarryTheTrustedID(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		path   string
		status int
		code   string
	}{
		{"validation", http.MethodGet, "/api/v1/validate", http.StatusUnprocessableEntity, "VALIDATION_FAILED"},
		{"not found", http.MethodGet, "/api/v1/nothing-here", http.StatusNotFound, "NOT_FOUND"},
		{"method not allowed", http.MethodDelete, "/api/v1/courses/course-42", http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED"},
		{"panic", http.MethodGet, "/api/v1/boom", http.StatusInternalServerError, "INTERNAL_ERROR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, buf := testEngine(t)
			rec := do(r, httptest.NewRequest(tc.method, tc.path, nil))

			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			if ct := rec.Header().Get("Content-Type"); ct != problem.ContentType {
				t.Errorf("Content-Type = %q, want %q", ct, problem.ContentType)
			}

			p := decodeProblem(t, rec)
			if p.Code != tc.code {
				t.Errorf("code = %q, want %q", p.Code, tc.code)
			}
			if p.Status != tc.status {
				t.Errorf("body status = %d, want %d matching the status line", p.Status, tc.status)
			}
			header := rec.Header().Get(requestid.HeaderName)
			if p.RequestID != header {
				t.Errorf("problem request_id %q != header %q", p.RequestID, header)
			}
			if got := buf.requestRecord(t)["request_id"]; got != header {
				t.Errorf("log request_id %v != header %q", got, header)
			}
			if got := buf.requestRecord(t)["safe_error_code"]; got != tc.code {
				t.Errorf("log safe_error_code = %v, want %q", got, tc.code)
			}
		})
	}
}

func TestRequestLogCarriesOnlyTypedAdmissionFailureStage(t *testing.T) {
	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV":     "development",
		"REDIS_ADDR":  "localhost:6379",
		"S3_ENDPOINT": "http://localhost:9000",
		"S3_BUCKET":   "gradex-test",
	}), config.MapSecretResolver{
		"DATABASE_URL":          "postgres://x",
		"S3_ACCESS_KEY":         "a",
		"S3_SECRET_KEY":         "b",
		"PLAYBACK_TOKEN_SECRET": "c",
	})
	if err != nil {
		t.Fatalf("loading test config: %v", err)
	}

	buf := &syncBuffer{}
	logger := logging.New(buf, "gradex-api-test", "development", logging.LevelFromString("info"))
	router, err := newEngine(cfg, logger)
	if err != nil {
		t.Fatalf("building engine: %v", err)
	}
	router.POST("/api/v1/admission", func(c *gin.Context) {
		setAdmissionFailureStage(c, admissionFailureStageDomain)
		writeProblem(c, problem.RegistrationUnavailable())
	})

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admission",
		strings.NewReader(`{"email":"student@example.com","password":"secret-token"}`),
	)
	response := do(router, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}

	record := buf.requestRecord(t)
	if got := record["admission_failure_stage"]; got != string(admissionFailureStageDomain) {
		t.Fatalf("admission_failure_stage = %v", got)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("encoding log record: %v", err)
	}
	for _, forbidden := range []string{"student@example.com", "secret-token", "email", "password"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("request log leaked %q: %s", forbidden, encoded)
		}
	}
}

// A panic must produce the generic envelope: no panic text, no stack, nothing
// the handler happened to be holding.
func TestPanicReturnsGenericSafeProblem(t *testing.T) {
	r, buf := testEngine(t)
	rec := do(r, httptest.NewRequest(http.MethodGet, "/api/v1/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}

	body := rec.Body.String()
	for _, leaked := range []string{"secret-token-abc123", "student@example.com", "exploded", "goroutine", ".go:"} {
		if strings.Contains(body, leaked) {
			t.Errorf("response body leaked %q: %s", leaked, body)
		}
	}

	// The panic value must not reach the log either — only its type and a
	// bounded stack.
	logs := buf.String()
	for _, leaked := range []string{"secret-token-abc123", "student@example.com"} {
		if strings.Contains(logs, leaked) {
			t.Errorf("logs leaked the panic value %q", leaked)
		}
	}
	var panicRec map[string]any
	for _, rec := range buf.records(t) {
		if rec["msg"] == "panic_recovered" {
			panicRec = rec
		}
	}
	if panicRec == nil {
		t.Fatal("no panic_recovered record was emitted")
	}
	if panicRec["error_class"] != "string" {
		t.Errorf("error_class = %v, want the panic value's type", panicRec["error_class"])
	}
	if _, ok := panicRec["stack"]; !ok {
		t.Error("a sanitized stack should be preserved in the protected log")
	}
}

// Once bytes are on the wire a second response cannot be appended. The panic
// must still be recovered and logged.
func TestPanicAfterResponseCommittedDoesNotAppendSecondResponse(t *testing.T) {
	r, buf := testEngine(t)
	rec := do(r, httptest.NewRequest(http.MethodGet, "/api/v1/boom-after-write", nil))

	if body := rec.Body.String(); body != "partial" {
		t.Errorf("body = %q, want the already-committed %q with nothing appended", body, "partial")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want the already-committed 200", rec.Code)
	}

	var committed any
	for _, r := range buf.records(t) {
		if r["msg"] == "panic_recovered" {
			committed = r["response_committed"]
		}
	}
	if committed != true {
		t.Errorf("response_committed = %v, want true", committed)
	}
}

// Route templates, never literal paths: the literal path carries identifiers
// and can carry a token.
func TestLogsUseRouteTemplatesNotLiteralPaths(t *testing.T) {
	r, buf := testEngine(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/courses/course-42?access_token=super-secret&email=student@example.com", nil)
	do(r, req)

	rc := buf.requestRecord(t)
	if rc["route_template"] != "/api/v1/courses/:courseID" {
		t.Errorf("route_template = %v, want the template", rc["route_template"])
	}

	logs := buf.String()
	for _, leaked := range []string{"course-42", "super-secret", "student@example.com", "access_token"} {
		if strings.Contains(logs, leaked) {
			t.Errorf("logs contain %q, which came from the literal path or query", leaked)
		}
	}
}

// Credential-bearing headers and cookies must never appear in telemetry.
func TestSensitiveHeadersAndCookiesAreNeverLogged(t *testing.T) {
	r, buf := testEngine(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/courses/course-42", nil)
	req.Header.Set("Authorization", "Bearer bearer-token-value")
	req.Header.Set("X-CSRF-Token", "csrf-token-value")
	req.Header.Set("Idempotency-Key", "idempotency-key-value")
	req.Header.Set("Cookie", "gradex_session=session-cookie-value")
	do(r, req)

	logs := buf.String()
	for _, leaked := range []string{
		"bearer-token-value", "csrf-token-value", "idempotency-key-value",
		"session-cookie-value", "Bearer", "gradex_session",
	} {
		if strings.Contains(logs, leaked) {
			t.Errorf("logs contain sensitive value %q", leaked)
		}
	}
}

// The allowlist is closed: only the agreed fields are emitted.
func TestRequestLogFieldsAreTheAgreedAllowlist(t *testing.T) {
	r, buf := testEngine(t)
	do(r, httptest.NewRequest(http.MethodGet, "/api/v1/courses/course-42", nil))

	allowed := map[string]bool{
		"timestamp": true, "level": true, "msg": true, "service": true, "environment": true,
		"request_id": true, "parent_request_id": true, "method": true, "route_template": true,
		"status": true, "duration_ms": true, "response_size": true, "safe_error_code": true,
	}
	for field := range buf.requestRecord(t) {
		if !allowed[field] {
			t.Errorf("unexpected telemetry field %q", field)
		}
	}
}

// A malformed or oversized client correlation value is dropped, not truncated
// and not passed through into the log.
func TestClientCorrelationValueIsRejectedWhenUnsafe(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"newline injection", "abc\ninjected=\"evil\""},
		{"control characters", "abc\x00def"},
		{"too long", strings.Repeat("a", requestid.MaxParentLength+1)},
		{"spaces", "not a valid id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, buf := testEngine(t)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/courses/course-42", nil)
			req.Header.Set(requestid.HeaderName, tc.value)
			do(r, req)

			rc := buf.requestRecord(t)
			if _, present := rc["parent_request_id"]; present {
				t.Errorf("unsafe client value was retained as %v", rc["parent_request_id"])
			}
			if strings.Contains(buf.String(), "injected") {
				t.Error("log injection reached the output")
			}
		})
	}
}

// Unmatched routes and unsupported methods still run the whole chain.
func TestFallbackHandlersAreCorrelatedAndLogged(t *testing.T) {
	t.Run("unmatched route", func(t *testing.T) {
		r, buf := testEngine(t)
		rec := do(r, httptest.NewRequest(http.MethodGet, "/api/v1/does-not-exist", nil))

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
		if rec.Header().Get(requestid.HeaderName) == "" {
			t.Error("unmatched route returned no request ID")
		}
		rc := buf.requestRecord(t)
		if rc["route_template"] != unmatchedRoute {
			t.Errorf("route_template = %v, want %q", rc["route_template"], unmatchedRoute)
		}
		if strings.Contains(buf.String(), "does-not-exist") {
			t.Error("the literal unmatched path was logged")
		}
	})

	t.Run("method not allowed", func(t *testing.T) {
		r, buf := testEngine(t)
		rec := do(r, httptest.NewRequest(http.MethodDelete, "/api/v1/courses/course-42", nil))

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
		allow := rec.Header().Get("Allow")
		for _, want := range []string{http.MethodGet, http.MethodPost} {
			if !strings.Contains(allow, want) {
				t.Errorf("Allow = %q, want it to include %s", allow, want)
			}
		}
		if buf.requestRecord(t)["status"] != float64(http.StatusMethodNotAllowed) {
			t.Error("the 405 was not logged with its status")
		}
	})
}

// Concurrent requests must never observe the same identifier.
func TestConcurrentRequestsGetDistinctIDs(t *testing.T) {
	r, _ := testEngine(t)

	const n = 64
	ids := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := do(r, httptest.NewRequest(http.MethodGet, "/api/v1/courses/course-42", nil))
			ids[i] = rec.Header().Get(requestid.HeaderName)
		}()
	}
	wg.Wait()

	seen := make(map[string]bool, n)
	for _, id := range ids {
		if id == "" {
			t.Fatal("a concurrent request produced no ID")
		}
		if seen[id] {
			t.Fatalf("duplicate request ID %q across concurrent requests", id)
		}
		seen[id] = true
	}
}

func TestSuccessfulRequestIsLoggedWithoutErrorCode(t *testing.T) {
	r, buf := testEngine(t)
	do(r, httptest.NewRequest(http.MethodGet, "/api/v1/courses/course-42", nil))

	rc := buf.requestRecord(t)
	if rc["level"] != "INFO" {
		t.Errorf("level = %v, want INFO for a successful request", rc["level"])
	}
	if _, present := rc["safe_error_code"]; present {
		t.Error("a successful request should carry no safe_error_code")
	}
	if rc["status"] != float64(http.StatusOK) {
		t.Errorf("status = %v, want 200", rc["status"])
	}
}
