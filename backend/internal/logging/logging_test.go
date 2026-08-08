package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"unicode/utf8"
)

func decode(t *testing.T, line string) map[string]any {
	t.Helper()
	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &rec); err != nil {
		t.Fatalf("not JSON: %q: %v", line, err)
	}
	return rec
}

func TestSanitizeReplacesControlCharacters(t *testing.T) {
	// A newline in a log field lets an attacker forge an adjacent record.
	got := Sanitize("route\ninjected=\"evil\"\r\tend\x00")
	for _, bad := range []string{"\n", "\r", "\t", "\x00"} {
		if strings.Contains(got, bad) {
			t.Errorf("sanitized value still contains %q: %q", bad, got)
		}
	}
	if !strings.Contains(got, "injected") {
		t.Error("sanitizing should neutralize control characters, not drop content")
	}
}

func TestSanitizeTruncatesOversizedFields(t *testing.T) {
	got := Sanitize(strings.Repeat("a", maxFieldLength*3))
	if len(got) > maxFieldLength+len("…truncated") {
		t.Errorf("field was not bounded: %d bytes", len(got))
	}
	if !strings.HasSuffix(got, "truncated") {
		t.Error("truncation should be visible in the output")
	}
}

// Truncation must not cut a multi-byte character in half. Gradex is
// Arabic-default, so a long route or error string in a log field is routinely
// multi-byte; a byte-boundary cut would end the field in U+FFFD replacement
// characters. Reported as a LOW finding in the Day 6 review.
func TestSanitizeTruncatesOnRuneBoundaries(t *testing.T) {
	for name, s := range map[string]string{
		"arabic":   strings.Repeat("مرحبا بالعالم ", 200),
		"japanese": strings.Repeat("日本語テキスト", 200),
		"emoji":    strings.Repeat("🎓📚", 400),
		"mixed":    strings.Repeat("course-مساق-", 200),
	} {
		t.Run(name, func(t *testing.T) {
			got := Sanitize(s)
			if !utf8.ValidString(got) {
				t.Errorf("truncated value is not valid UTF-8: %q", got)
			}
			if strings.ContainsRune(got, utf8.RuneError) {
				t.Errorf("truncated value contains the replacement character: %q", got)
			}
			if !strings.HasSuffix(got, "truncated") {
				t.Errorf("value was not truncated: %q", got)
			}
		})
	}
}

// The same guarantee for stacks, which take a different bound.
func TestStackTruncationIsRuneSafe(t *testing.T) {
	var buf bytes.Buffer
	New(&buf, "s", "e", slog.LevelInfo).PanicRecovered(PanicEvent{
		Stack: strings.Repeat("日本語のスタックトレース", 1000),
	})
	stack, _ := decode(t, buf.String())["stack"].(string)
	if strings.ContainsRune(stack, utf8.RuneError) {
		t.Errorf("truncated stack contains the replacement character: %q", stack)
	}
}

func TestRequestCompletedEmitsAgreedFields(t *testing.T) {
	var buf bytes.Buffer
	New(&buf, "gradex-api", "production", slog.LevelInfo).RequestCompleted(RequestEvent{
		RequestID:       "trusted-id",
		ParentRequestID: "parent-id",
		Method:          "GET",
		RouteTemplate:   "/api/v1/courses/:courseID",
		Status:          200,
		DurationMillis:  12,
		ResponseSize:    345,
		LimiterOutcome:  "ALLOW",
	})

	rec := decode(t, buf.String())
	for field, want := range map[string]any{
		"service":           "gradex-api",
		"environment":       "production",
		"request_id":        "trusted-id",
		"parent_request_id": "parent-id",
		"method":            "GET",
		"route_template":    "/api/v1/courses/:courseID",
		"status":            float64(200),
		"duration_ms":       float64(12),
		"response_size":     float64(345),
		"limiter_outcome":   "ALLOW",
	} {
		if rec[field] != want {
			t.Errorf("%s = %v, want %v", field, rec[field], want)
		}
	}
	if _, ok := rec["timestamp"]; !ok {
		t.Error("record has no timestamp field")
	}
	if _, ok := rec["time"]; ok {
		t.Error("slog's default time key should have been renamed")
	}
}

func TestRequestCompletedUsesClosedFieldAllowlist(t *testing.T) {
	var buf bytes.Buffer
	New(&buf, "gradex-api", "production", slog.LevelInfo).RequestCompleted(RequestEvent{
		RequestID:      "request-id",
		Method:         "POST",
		RouteTemplate:  "/api/v1/student-registrations",
		Status:         202,
		LimiterOutcome: "ALLOW",
	})

	rec := decode(t, buf.String())
	allowed := map[string]bool{
		"timestamp": true, "level": true, "msg": true,
		"service": true, "environment": true,
		"request_id": true, "method": true, "route_template": true,
		"status": true, "duration_ms": true, "response_size": true,
		"limiter_outcome": true,
	}
	for field := range rec {
		if !allowed[field] {
			t.Errorf("request log emitted non-allowlisted field %q", field)
		}
	}
	for _, forbidden := range []string{
		"email", "identifier", "identifier_hmac", "password", "password_hash",
		"token", "digest", "query", "cookie", "csrf", "body", "provider_payload",
	} {
		if _, present := rec[forbidden]; present {
			t.Errorf("request log emitted forbidden field %q", forbidden)
		}
	}
}

func TestRequestLevelFollowsStatus(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{{200, "INFO"}, {404, "WARN"}, {500, "ERROR"}} {
		var buf bytes.Buffer
		New(&buf, "s", "e", slog.LevelInfo).RequestCompleted(RequestEvent{Status: tc.status})
		if got := decode(t, buf.String())["level"]; got != tc.want {
			t.Errorf("status %d logged at %v, want %v", tc.status, got, tc.want)
		}
	}
}

func TestWorkerEventsAreStructuredCorrelatableAndExcludeRawErrors(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, "gradex-worker", "production", slog.LevelInfo)
	logger.WorkerLifecycle(WorkerReady)
	logger.WorkerFailed(WorkerFailureEvent{
		Operation: "job_process", ErrorClass: "*errors.errorString", JobType: "media:transcode",
		TaskID: "task-correlation-id", RetryCount: 2, MaxRetry: 5,
	})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("worker log lines=%d, want 2", len(lines))
	}
	lifecycle := decode(t, lines[0])
	if lifecycle["msg"] != "worker_lifecycle" || lifecycle["phase"] != "READY" {
		t.Fatalf("lifecycle record=%v", lifecycle)
	}
	failure := decode(t, lines[1])
	for field, want := range map[string]any{
		"msg": "worker_failure", "operation": "job_process", "error_class": "*errors.errorString",
		"job_type": "media:transcode", "task_id": "task-correlation-id",
		"retry_count": float64(2), "max_retry": float64(5),
	} {
		if failure[field] != want {
			t.Errorf("%s=%v, want %v", field, failure[field], want)
		}
	}
	for _, forbidden := range []string{"error", "payload", "asset_version_id", "token", "cookie", "password"} {
		if _, present := failure[forbidden]; present {
			t.Errorf("worker failure emitted forbidden field %q", forbidden)
		}
	}
}

// The panic value is excluded on purpose: it routinely holds whatever the
// handler was working with.
func TestPanicRecoveredExcludesPanicValue(t *testing.T) {
	var buf bytes.Buffer
	New(&buf, "s", "e", slog.LevelInfo).PanicRecovered(PanicEvent{
		RequestID:  "id",
		ErrorClass: ErrorClassOf(errors.New("boom")),
		Stack:      "goroutine 1 [running]:\nmain.main()\n\t/app/main.go:10",
	})

	rec := decode(t, buf.String())
	if rec["error_class"] != "*errors.errorString" {
		t.Errorf("error_class = %v, want the Go type", rec["error_class"])
	}
	stack, _ := rec["stack"].(string)
	if strings.Contains(stack, "\n") {
		t.Error("stack newlines should be neutralized before logging")
	}
	if !strings.Contains(stack, "main.go") {
		t.Error("a sanitized stack should still be usable for diagnosis")
	}
}

func TestStackIsBounded(t *testing.T) {
	var buf bytes.Buffer
	New(&buf, "s", "e", slog.LevelInfo).PanicRecovered(PanicEvent{
		Stack: strings.Repeat("x", maxStackLength*2),
	})
	stack, _ := decode(t, buf.String())["stack"].(string)
	if len(stack) > maxStackLength+len("…truncated") {
		t.Errorf("stack was not bounded: %d bytes", len(stack))
	}
}

// failingWriter models a log sink that has gone bad mid-process.
type failingWriter struct{ mode string }

func (w failingWriter) Write([]byte) (int, error) {
	if w.mode == "panic" {
		panic("sink exploded")
	}
	return 0, errors.New("sink unavailable")
}

// Telemetry is never authority: a broken sink must not propagate into the
// request that produced the line.
func TestLoggingFailureDoesNotPropagate(t *testing.T) {
	for _, mode := range []string{"error", "panic"} {
		t.Run(mode, func(t *testing.T) {
			logger := New(failingWriter{mode: mode}, "s", "e", slog.LevelInfo)
			// The assertion is simply that this returns rather than panicking
			// or surfacing an error to the caller.
			logger.RequestCompleted(RequestEvent{RequestID: "id", Status: 200})
		})
	}
}
