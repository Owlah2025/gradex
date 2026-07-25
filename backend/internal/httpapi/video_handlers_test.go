package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/requestid"
	"github.com/Owlah2025/gradex/backend/internal/video"
)

// internalDetails are strings that must never reach a client. Each stands for
// a class §2.3 forbids: object keys, filesystem paths, queue names, database
// constraint names, raw provider text, signed tokens, and internal lifecycle
// values.
var internalDetails = []string{
	"gradex-video/raw/lesson-99/source.mp4",
	"/var/lib/gradex/tmp",
	"video:transcode",
	"videos_lesson_id_key",
	"pq: duplicate key value violates unique constraint",
	"AccessDenied: signature mismatch",
	"eyJhbGciOiJIUzI1NiJ9.signed-playback-token",
	"PROCESSING",
	"lesson-99",
}

// fakeService returns a fixed error (or success) from every operation, so a
// test can drive one error class through the whole HTTP boundary.
type fakeService struct{ err error }

func (f fakeService) RequestUpload(context.Context, string, string, string) (video.UploadTicket, error) {
	if f.err != nil {
		return video.UploadTicket{}, f.err
	}
	return video.UploadTicket{UploadURL: "https://storage.example/put", RawKey: "raw/key"}, nil
}
func (f fakeService) CompleteUpload(context.Context, string) error { return f.err }
func (f fakeService) Retranscode(context.Context, string) error    { return f.err }
func (f fakeService) Publish(context.Context, string) error        { return f.err }
func (f fakeService) GetPlaybackURL(context.Context, string, string) (video.SignedURL, error) {
	if f.err != nil {
		return video.SignedURL{}, f.err
	}
	return video.SignedURL{URL: "https://cdn.example/master.m3u8"}, nil
}
func (f fakeService) UpdateProgress(context.Context, string, string, float64) (video.Progress, error) {
	if f.err != nil {
		return video.Progress{}, f.err
	}
	return video.Progress{MaxPositionSeconds: 12, Completed: false}, nil
}
func (f fakeService) ServeManifest(context.Context, string, string, string) ([]byte, string, error) {
	if f.err != nil {
		return nil, "", f.err
	}
	return []byte("#EXTM3U"), "application/vnd.apple.mpegurl", nil
}

type fakeAuth struct{ err error }

func (f fakeAuth) UserFromRequest(*gin.Context) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return "user-1", nil
}

type fakeEntitlements struct {
	allowed bool
	err     error
}

func (f fakeEntitlements) HasAccess(context.Context, string, string) (bool, error) {
	return f.allowed, f.err
}
func (f fakeEntitlements) IsInstructorForLesson(context.Context, string, string) (bool, error) {
	return f.allowed, f.err
}

func videoRouter(t *testing.T, svc video.Service, a fakeAuth, e fakeEntitlements) (*gin.Engine, *syncBuffer) {
	t.Helper()

	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV": "development", "REDIS_ADDR": "localhost:6379",
		"S3_ENDPOINT": "http://localhost:9000", "S3_BUCKET": "gradex-test",
	}), config.MapSecretResolver{
		"DATABASE_URL": "postgres://x", "S3_ACCESS_KEY": "a",
		"S3_SECRET_KEY": "b", "PLAYBACK_TOKEN_SECRET": "c",
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	buf := &syncBuffer{}
	logger := logging.New(buf, "gradex-api-test", "development", logging.LevelFromString("info"))

	reporter := health.New(time.Second)
	reporter.MarkStarted()

	r, err := NewRouter(cfg, logger, reporter, svc, a, e)
	if err != nil {
		t.Fatalf("router: %v", err)
	}
	return r, buf
}

// assertProblemEnvelope checks every invariant the error contract promises,
// independent of which problem was returned.
func assertProblemEnvelope(t *testing.T, rec *httptest.ResponseRecorder) problem.Problem {
	t.Helper()

	if ct := rec.Header().Get("Content-Type"); ct != problem.ContentType {
		t.Errorf("Content-Type = %q, want %q", ct, problem.ContentType)
	}

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("body is not JSON: %q", rec.Body.String())
	}
	if _, legacy := raw["error"]; legacy {
		t.Errorf("legacy {\"error\": ...} shape survived: %s", rec.Body.String())
	}

	var p problem.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("body is not a Problem: %v", err)
	}

	if p.Status != rec.Code {
		t.Errorf("body status %d disagrees with HTTP status %d", p.Status, rec.Code)
	}
	if p.Type == "" || p.Code == "" || p.Title == "" {
		t.Errorf("problem is missing type/code/title: %+v", p)
	}
	// type and code are generated from one another, so they cannot disagree.
	wantCode := strings.ToUpper(strings.ReplaceAll(
		strings.TrimPrefix(p.Type, "https://api.gradex.com/problems/"), "-", "_"))
	if p.Code != wantCode {
		t.Errorf("code %q contradicts type %q", p.Code, p.Type)
	}
	if header := rec.Header().Get(requestid.HeaderName); p.RequestID != header {
		t.Errorf("body request_id %q != X-Request-ID %q", p.RequestID, header)
	}
	if p.Instance == "" || !strings.HasPrefix(p.Instance, "urn:gradex:problem:") {
		t.Errorf("instance %q is not an opaque URN", p.Instance)
	}
	if strings.Contains(p.Instance, "/api/") {
		t.Errorf("instance %q leaks a resource path", p.Instance)
	}

	body := rec.Body.String()
	for _, secret := range internalDetails {
		if strings.Contains(body, secret) {
			t.Errorf("response leaked internal detail %q: %s", secret, body)
		}
	}
	return p
}

func authorized() (fakeAuth, fakeEntitlements) {
	return fakeAuth{}, fakeEntitlements{allowed: true}
}

// Every video error class must arrive as the right public problem, with no
// internal text attached, on every route that can produce it.
func TestVideoErrorClassesMapToPublicProblems(t *testing.T) {
	// Each internal error wraps details that must not escape.
	cases := []struct {
		name       string
		serviceErr error
		wantStatus int
		wantCode   string
	}{
		{
			"not found",
			fmt.Errorf("%w: no object found at %s", video.ErrNotFound, internalDetails[0]),
			http.StatusNotFound, "NOT_FOUND",
		},
		{
			"state conflict",
			fmt.Errorf("%w: lesson %s video is not PUBLISHED (currently %s)", video.ErrConflict, "lesson-99", "PROCESSING"),
			http.StatusConflict, "UNSUPPORTED_STATE_TRANSITION",
		},
		{
			// A lost compare-and-swap is retryable, so it must not arrive as
			// the same problem as an illegal transition.
			"concurrent modification",
			fmt.Errorf("%w: video for lesson %s changed state concurrently", video.ErrConcurrentModification, "lesson-99"),
			http.StatusConflict, "STATE_CONFLICT",
		},
		{
			"validation",
			fmt.Errorf("%w: file type %q not allowed", video.ErrValidation, ".exe"),
			http.StatusUnprocessableEntity, "VALIDATION_FAILED",
		},
		{
			"dependency unavailable",
			fmt.Errorf("%w: enqueueing to %s: %s", video.ErrUnavailable, "video:transcode", "AccessDenied: signature mismatch"),
			http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE",
		},
		{
			"unrecognised internal error fails closed",
			errors.New("pq: duplicate key value violates unique constraint \"videos_lesson_id_key\""),
			http.StatusInternalServerError, "INTERNAL_ERROR",
		},
	}

	routes := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/lessons/lesson-99/video/complete"},
		{http.MethodPost, "/api/v1/lessons/lesson-99/video/retry"},
		{http.MethodPost, "/api/v1/lessons/lesson-99/video/publish"},
		{http.MethodGet, "/api/v1/lessons/lesson-99/video/playback-url"},
	}

	for _, tc := range cases {
		for _, route := range routes {
			t.Run(tc.name+" "+route.path, func(t *testing.T) {
				a, e := authorized()
				r, _ := videoRouter(t, fakeService{err: tc.serviceErr}, a, e)

				rec := do(r, httptest.NewRequest(route.method, route.path, nil))

				if rec.Code != tc.wantStatus {
					t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.wantStatus, rec.Body.String())
				}
				if p := assertProblemEnvelope(t, rec); p.Code != tc.wantCode {
					t.Errorf("code = %q, want %q", p.Code, tc.wantCode)
				}
			})
		}
	}
}

// Structural and semantic body failures are different statuses, and neither
// may echo the decoder's view of the input.
func TestRequestBodyFailures(t *testing.T) {
	t.Run("malformed JSON is 400 and leaks no parser internals", func(t *testing.T) {
		a, e := authorized()
		r, _ := videoRouter(t, fakeService{}, a, e)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/lessons/lesson-99/video/upload-url",
			strings.NewReader(`{"filename": "secret-file.mp4", `))
		req.Header.Set("Content-Type", "application/json")
		rec := do(r, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
		p := assertProblemEnvelope(t, rec)
		if p.Code != "MALFORMED_REQUEST" {
			t.Errorf("code = %q, want MALFORMED_REQUEST", p.Code)
		}
		for _, leaked := range []string{"secret-file.mp4", "unexpected end", "offset", "json:"} {
			if strings.Contains(rec.Body.String(), leaked) {
				t.Errorf("parser internals leaked %q: %s", leaked, rec.Body.String())
			}
		}
	})

	t.Run("missing required fields are 422 with body pointers", func(t *testing.T) {
		a, e := authorized()
		r, _ := videoRouter(t, fakeService{}, a, e)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/lessons/lesson-99/video/upload-url",
			strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		rec := do(r, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422", rec.Code)
		}
		p := assertProblemEnvelope(t, rec)
		if len(p.Violations) != 2 {
			t.Fatalf("expected 2 violations, got %+v", p.Violations)
		}

		pointers := map[string]bool{}
		for _, v := range p.Violations {
			pointers[v.Pointer] = true
			if v.Location != problem.LocationBody {
				t.Errorf("location = %q, want %q", v.Location, problem.LocationBody)
			}
			if v.Code != "REQUIRED" {
				t.Errorf("code = %q, want REQUIRED", v.Code)
			}
		}
		// The pointer uses the JSON name the client sent, not the Go field.
		for _, want := range []string{"#/filename", "#/content_type"} {
			if !pointers[want] {
				t.Errorf("missing pointer %q, got %v", want, pointers)
			}
		}
		if pointers["#/ContentType"] {
			t.Error("pointer used the Go field name instead of the JSON name")
		}
	})

	t.Run("field-level service validation names the field", func(t *testing.T) {
		a, e := authorized()
		r, _ := videoRouter(t, fakeService{
			err: fmt.Errorf("%w: position_seconds must be >= 0", video.ErrValidation),
		}, a, e)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/lessons/lesson-99/progress",
			strings.NewReader(`{"position_seconds": -5}`))
		req.Header.Set("Content-Type", "application/json")
		rec := do(r, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422", rec.Code)
		}
		p := assertProblemEnvelope(t, rec)
		if len(p.Violations) != 1 || p.Violations[0].Pointer != "#/position_seconds" {
			t.Errorf("expected a position_seconds violation, got %+v", p.Violations)
		}
	})
}

// Authentication and authorization failures must be uniform: nothing about
// who owns the lesson, or whether it exists at all.
func TestAuthFailuresRevealNothing(t *testing.T) {
	t.Run("unauthenticated returns the fixed challenge", func(t *testing.T) {
		r, _ := videoRouter(t, fakeService{},
			fakeAuth{err: errors.New("missing X-Debug-User-ID header (fake auth mode)")},
			fakeEntitlements{allowed: true})

		rec := do(r, httptest.NewRequest(http.MethodGet, "/api/v1/lessons/lesson-99/video/playback-url", nil))

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		p := assertProblemEnvelope(t, rec)
		if p.Code != "AUTHENTICATION_REQUIRED" {
			t.Errorf("code = %q", p.Code)
		}
		if got := rec.Header().Get("WWW-Authenticate"); got != `GradexSession realm="gradex-web"` {
			t.Errorf("WWW-Authenticate = %q, want the fixed GradexSession challenge", got)
		}
		if strings.Contains(rec.Body.String(), "X-Debug-User-ID") {
			t.Error("the authenticator's internal reason reached the client")
		}
	})

	t.Run("denial does not reveal ownership or existence", func(t *testing.T) {
		for _, route := range []string{
			"/api/v1/lessons/lesson-99/video/playback-url",
			"/api/v1/lessons/lesson-99/video/publish",
		} {
			r, _ := videoRouter(t, fakeService{}, fakeAuth{}, fakeEntitlements{allowed: false})

			method := http.MethodGet
			if strings.HasSuffix(route, "publish") {
				method = http.MethodPost
			}
			rec := do(r, httptest.NewRequest(method, route, nil))

			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s: status = %d, want 403", route, rec.Code)
			}
			p := assertProblemEnvelope(t, rec)
			if p.Code != "NOT_AUTHORIZED" {
				t.Errorf("%s: code = %q", route, p.Code)
			}
			for _, leaked := range []string{"instructor", "owner", "not the instructor", "no access", "lesson-99"} {
				if strings.Contains(strings.ToLower(rec.Body.String()), strings.ToLower(leaked)) {
					t.Errorf("%s: denial revealed %q: %s", route, leaked, rec.Body.String())
				}
			}
		}
	})

	// Both denials must be indistinguishable, or the pair discloses which
	// check failed.
	t.Run("student and instructor denials are identical", func(t *testing.T) {
		bodies := map[string]string{}
		for name, route := range map[string]string{
			"student":    "/api/v1/lessons/lesson-99/video/playback-url",
			"instructor": "/api/v1/lessons/lesson-99/video/publish",
		} {
			r, _ := videoRouter(t, fakeService{}, fakeAuth{}, fakeEntitlements{allowed: false})
			method := http.MethodGet
			if name == "instructor" {
				method = http.MethodPost
			}
			rec := do(r, httptest.NewRequest(method, route, nil))

			var p problem.Problem
			_ = json.Unmarshal(rec.Body.Bytes(), &p)
			// The request ID differs per attempt by design; blank it out.
			p.RequestID, p.Instance = "", ""
			out, _ := json.Marshal(p)
			bodies[name] = string(out)
		}
		if bodies["student"] != bodies["instructor"] {
			t.Errorf("denials differ:\n student: %s\n instructor: %s", bodies["student"], bodies["instructor"])
		}
	})

	// An entitlement lookup fault is a fault, not a denial, and still says
	// nothing about the resource.
	t.Run("entitlement lookup failure is a generic 500", func(t *testing.T) {
		r, _ := videoRouter(t, fakeService{}, fakeAuth{},
			fakeEntitlements{err: errors.New("pq: duplicate key value violates unique constraint \"videos_lesson_id_key\"")})

		rec := do(r, httptest.NewRequest(http.MethodGet, "/api/v1/lessons/lesson-99/video/playback-url", nil))

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
		if p := assertProblemEnvelope(t, rec); p.Code != "INTERNAL_ERROR" {
			t.Errorf("code = %q", p.Code)
		}
	})
}

// The success contract is unchanged by this retrofit.
func TestSuccessfulResponsesAreUnchanged(t *testing.T) {
	a, e := authorized()
	r, _ := videoRouter(t, fakeService{}, a, e)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/lessons/lesson-99/progress",
		strings.NewReader(`{"position_seconds": 12}`))
	req.Header.Set("Content-Type", "application/json")
	rec := do(r, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("success Content-Type = %q, want application/json", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("success body is not JSON: %v", err)
	}
	for _, field := range []string{"max_position_seconds", "completed"} {
		if _, ok := body[field]; !ok {
			t.Errorf("success body lost field %q: %s", field, rec.Body.String())
		}
	}
}

// The request log records the public code, correlated with the response.
func TestErrorsAreLoggedWithTheirPublicCode(t *testing.T) {
	a, e := authorized()
	r, buf := videoRouter(t, fakeService{err: fmt.Errorf("%w: gone", video.ErrNotFound)}, a, e)

	rec := do(r, httptest.NewRequest(http.MethodPost, "/api/v1/lessons/lesson-99/video/publish", nil))

	logRec := buf.requestRecord(t)
	if logRec["safe_error_code"] != "NOT_FOUND" {
		t.Errorf("safe_error_code = %v, want NOT_FOUND", logRec["safe_error_code"])
	}
	if logRec["request_id"] != rec.Header().Get(requestid.HeaderName) {
		t.Error("log and response are not correlated")
	}
	if logRec["route_template"] != "/api/v1/lessons/:lessonID/video/publish" {
		t.Errorf("route_template = %v", logRec["route_template"])
	}
}
