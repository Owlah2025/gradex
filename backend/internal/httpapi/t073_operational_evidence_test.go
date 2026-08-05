package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/entitlement"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/learning"
	"github.com/Owlah2025/gradex/backend/internal/logging"
)

// T073: the operational record for protected-learning denials and Progress-write failures is
// sufficient to reconstruct an incident without reproducing it (Principle IX).
//
// The external answer to every one of these is the same uniform 404 — that is the point of the
// refusal contract, and it means the log is the *only* place an operator can tell an expired
// Entitlement from a suspended Account from a store that stopped accepting writes. So the standard
// here is not "something was logged": it is that the typed cause survives to the log, that causes
// which need different responders are distinguishable, and that none of it leaks what the uniform
// refusal exists to hide.

// logRecords parses the structured lines one request produced.
func logRecords(t *testing.T, logs *syncBuffer) []map[string]any {
	t.Helper()
	records := make([]map[string]any, 0)
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("a log line is not structured JSON: %q", line)
		}
		records = append(records, record)
	}
	return records
}

func recordsNamed(records []map[string]any, name string) []map[string]any {
	matched := make([]map[string]any, 0)
	for _, record := range records {
		if record["msg"] == name {
			matched = append(matched, record)
		}
	}
	return matched
}

// TestEveryProtectedLearningDenialRecordsItsTypedReason walks the denial taxonomy a Student can
// actually reach and proves each one arrives in the log as itself, while the response stays
// byte-identical across all of them.
func TestEveryProtectedLearningDenialRecordsItsTypedReason(t *testing.T) {
	student := activeStudent()
	instructor := identity.Principal{AccountID: "user-1", Role: identity.RoleInstructor, Status: identity.StatusActive, CredentialState: identity.CredentialActive}
	suspended := identity.Principal{AccountID: "user-1", Role: identity.RoleStudent, Status: identity.StatusSuspended, CredentialState: identity.CredentialActive}

	tests := []struct {
		name       string
		principal  identity.Principal
		authErr    error
		decision   entitlement.Decision
		wantReason string
	}{
		{"unauthenticated", student, errors.New("no session"), entitlement.Decision{}, string(identity.DenyPrincipalNotFound)},
		{"missing capability", instructor, nil, entitlement.Decision{Allowed: true}, string(identity.DenyRoleLacksCapability)},
		{"account suspended", suspended, nil, entitlement.Decision{Allowed: true}, string(identity.DenyAccountSuspended)},
		{"no applicable grant", student, nil, entitlement.Decision{Reason: entitlement.ReasonNoApplicableGrant}, string(entitlement.ReasonNoApplicableGrant)},
		{"expired entitlement", student, nil, entitlement.Decision{Reason: entitlement.ReasonExpired}, string(entitlement.ReasonExpired)},
		{"emergency course suspension", student, nil, entitlement.Decision{Reason: entitlement.ReasonCourseSuspended}, string(entitlement.ReasonCourseSuspended)},
		{"retired beyond eligibility", student, nil, entitlement.Decision{Reason: entitlement.ReasonRetired}, string(entitlement.ReasonRetired)},
		{"dependency failure", student, nil, entitlement.Decision{Allowed: true}, string(entitlement.ReasonDependency)},
	}

	var baselineBody string
	var baselineHeaders http.Header
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router, logs := learningRouterUnderTest(t, tc.principal, tc.authErr, fixedLearningEvaluator{decision: tc.decision})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/learn/lessons/11111111-1111-1111-1111-111111111111/playback", nil))

			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want the uniform refusal", response.Code)
			}
			// Public refusal invariance: the internal reason changes, the answer does not.
			if baselineHeaders == nil {
				baselineBody, baselineHeaders = response.Body.String(), response.Header().Clone()
			} else {
				if response.Body.String() != baselineBody {
					t.Fatalf("the body varies with the internal reason: %q vs %q", response.Body.String(), baselineBody)
				}
				for _, header := range []string{"Cache-Control", "Content-Type", "Location", "X-Denial-Reason", "Retry-After"} {
					if response.Header().Get(header) != baselineHeaders.Get(header) {
						t.Fatalf("header %s varies with the internal reason", header)
					}
				}
			}

			// Exactly one authorization event, carrying the typed reason.
			denials := recordsNamed(logRecords(t, logs), "authorization_denied")
			if len(denials) != 1 {
				t.Fatalf("%s produced %d authorization_denied events, want exactly 1", tc.name, len(denials))
			}
			denial := denials[0]
			if denial["deny_reason"] != tc.wantReason {
				t.Fatalf("deny_reason = %v, want %q", denial["deny_reason"], tc.wantReason)
			}
			// Route template, never the raw URL: the Lesson id must not ride into the log through it.
			if template, _ := denial["route_template"].(string); !strings.Contains(template, ":lessonId") {
				t.Fatalf("route_template = %v, want the template rather than a resolved path", denial["route_template"])
			}
			if strings.Contains(logs.String(), "11111111-1111-1111-1111-111111111111") {
				t.Fatal("the resolved Lesson identifier reached the log")
			}
			if denial["level"] != "WARN" {
				t.Fatalf("level = %v, want WARN: an expected Student denial is not a server error", denial["level"])
			}
		})
	}
}

// TestDenialEmitsOneAuthorizationEventAndOneRequestEvent pins the cardinality. Middleware, handler,
// and repository each have a denial path; a request that walked two of them would double-count on
// every dashboard built from this log.
func TestDenialEmitsOneAuthorizationEventAndOneRequestEvent(t *testing.T) {
	router, logs := learningRouterUnderTest(t, activeStudent(), nil,
		fixedLearningEvaluator{decision: entitlement.Decision{Reason: entitlement.ReasonExpired}})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/learn/lessons/11111111-1111-1111-1111-111111111111/playback", nil))

	records := logRecords(t, logs)
	if got := len(recordsNamed(records, "authorization_denied")); got != 1 {
		t.Fatalf("authorization_denied events = %d, want 1", got)
	}
	// The request-completion event is separate telemetry and must survive alongside it.
	if got := len(recordsNamed(records, "http_request")); got != 1 {
		t.Fatalf("http_request events = %d, want 1", got)
	}
	if got := len(recordsNamed(records, "protected_write_failed")); got != 0 {
		t.Fatalf("a read denial emitted %d write-failure events", got)
	}
}

// progressReachesPersistenceRepository resolves the Enrollment so the request gets past admission,
// then fails the guarded write — a store that accepted the transaction and refused the row.
type progressReachesPersistenceRepository struct{ countingLearningRepository }

func (r *progressReachesPersistenceRepository) EnrollmentForLesson(context.Context, string, string) (learning.Enrollment, error) {
	return learning.Enrollment{ID: "44444444-4444-4444-4444-444444444444"}, nil
}

// durationBearingMedia returns a trusted duration so the handler reaches the write rather than
// stopping at the media boundary.
type durationBearingMedia struct{ unavailableLearningMedia }

func (durationBearingMedia) TrustedVideoDuration(context.Context, string, string) (time.Duration, error) {
	return time.Minute, nil
}

// progressPersistenceRouter composes a router whose Progress write reaches persistence and fails
// there. Without it a Progress test stops at the media boundary and never exercises the branch.
func progressPersistenceRouter(t *testing.T) (*gin.Engine, *syncBuffer) {
	t.Helper()
	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV": "development", "REDIS_ADDR": "localhost:6379",
		"S3_ENDPOINT": "http://localhost:9000", "S3_BUCKET": "gradex-test",
	}), config.MapSecretResolver{
		"DATABASE_URL": "postgres://x", "S3_ACCESS_KEY": "a", "S3_SECRET_KEY": "b", "PLAYBACK_TOKEN_SECRET": "c",
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	foundation, err := NewLearningFoundation(LearningFoundationOptions{
		Repository: &progressReachesPersistenceRepository{}, Evaluator: allowingReportEvaluator(),
		Media: durationBearingMedia{}, ReportContexts: testReportContextIssuer(t),
		Limiter: testLearningLimiter(t), Policies: testLearningPolicies(),
	})
	if err != nil {
		t.Fatalf("creating learning foundation: %v", err)
	}
	logs := &syncBuffer{}
	router, err := NewRouter(cfg, logging.New(logs, "gradex-api-test", "development", logging.LevelFromString("info")),
		health.New(time.Second), fakeAuth{}, fixedPrincipals{principal: activeStudent()}, WithLearningFoundation(foundation))
	if err != nil {
		t.Fatalf("router: %v", err)
	}
	return router, logs
}

// TestProgressPersistenceFailureIsNotRecordedAsAnAuthorizationDenial is the distinction T073 turns
// on for Progress writes.
//
// A store that refuses the write and a Student whose access ended produce the same uniform refusal.
// If both were logged as `authorization_denied` with the shared DEPENDENCY_FAILURE reason — which
// this route also uses for the enrollment and trusted-duration lookups — an operator could not tell
// a database outage from an entitlement problem, which is exactly the incident Principle IX says
// the log must let them reconstruct.
func TestProgressPersistenceFailureIsNotRecordedAsAnAuthorizationDenial(t *testing.T) {
	router, logs := progressPersistenceRouter(t)

	request := httptest.NewRequest(http.MethodPut,
		"/api/v1/learn/lessons/11111111-1111-1111-1111-111111111111/progress",
		strings.NewReader(`{"position_seconds":30,"asset_version_id":"22222222-2222-2222-2222-222222222222"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	// The Student still gets the uniform refusal — the correction is internal only.
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want the uniform refusal", response.Code)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
	if response.Header().Get("X-Denial-Reason") != "" {
		t.Fatal("the response carried a denial reason")
	}

	records := logRecords(t, logs)
	failures := recordsNamed(records, "protected_write_failed")
	if len(failures) != 1 {
		t.Fatalf("protected_write_failed events = %d, want exactly 1; a store that refused the write "+
			"must be distinguishable from an authorization decision", len(failures))
	}
	failure := failures[0]
	if failure["operation"] != operationProgressWrite {
		t.Fatalf("operation = %v, want %q", failure["operation"], operationProgressWrite)
	}
	if failure["stage"] != writeStagePersistence {
		t.Fatalf("stage = %v, want %q", failure["stage"], writeStagePersistence)
	}
	if failure["failure_class"] != writeFailureStoreRejected {
		t.Fatalf("failure_class = %v, want %q", failure["failure_class"], writeFailureStoreRejected)
	}
	if failure["level"] != "ERROR" {
		t.Fatalf("level = %v, want ERROR: a store refusing writes is an infrastructure incident, not a refusal", failure["level"])
	}

	// And it is not counted as a refusal: an outage must not read as a spike in denials.
	if got := len(recordsNamed(records, "authorization_denied")); got != 0 {
		t.Fatalf("a store rejection produced %d authorization_denied events; a database outage would "+
			"read as an entitlement problem", got)
	}

	// Nothing the Student sent reaches the log.
	for _, sentinel := range []string{"position_seconds", "22222222-2222-2222-2222-222222222222", "11111111-1111-1111-1111-111111111111", "44444444-4444-4444-4444-444444444444"} {
		if strings.Contains(logs.String(), sentinel) {
			t.Fatalf("the write-failure log carried %q", sentinel)
		}
	}
}

// TestOperationalEventsCarryNoSensitiveValue plants distinctive sentinels in everything a request
// controls and proves none of them reaches the log.
func TestOperationalEventsCarryNoSensitiveValue(t *testing.T) {
	const (
		sentinelContext     = "grc1.SENTINELREPORTCONTEXTVALUE"
		sentinelExplanation = "SENTINEL-EXPLANATION-TEXT"
		sentinelVersion     = "55555555-5555-5555-5555-555555555555"
	)

	router, logs := learningRouterUnderTest(t, activeStudent(), nil,
		fixedLearningEvaluator{decision: entitlement.Decision{Reason: entitlement.ReasonNoApplicableGrant}})

	// A report submission carries the context and the explanation; a Progress write carries the
	// Asset Version. Both are refused, and both refusals are logged.
	report := httptest.NewRequest(http.MethodPost, "/api/v1/learn/reports",
		strings.NewReader(`{"report_context":"`+sentinelContext+`","reason":"other","explanation":"`+sentinelExplanation+`"}`))
	report.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(httptest.NewRecorder(), report)

	progress := httptest.NewRequest(http.MethodPut,
		"/api/v1/learn/lessons/11111111-1111-1111-1111-111111111111/progress",
		strings.NewReader(`{"position_seconds":30,"asset_version_id":"`+sentinelVersion+`"}`))
	progress.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(httptest.NewRecorder(), progress)

	output := logs.String()
	if strings.TrimSpace(output) == "" {
		t.Fatal("no log output was produced; the exclusion assertion would pass vacuously")
	}
	for _, sentinel := range []string{
		sentinelContext, "SENTINELREPORTCONTEXTVALUE", sentinelExplanation, sentinelVersion,
		"11111111-1111-1111-1111-111111111111", "test-session-user-1",
		"position_seconds", "asset_version_id", "report_context", "explanation",
	} {
		if strings.Contains(output, sentinel) {
			t.Fatalf("the operational log carried %q:\n%s", sentinel, output)
		}
	}
}

// TestOperationalEventsUseClosedFieldSets keeps the events from quietly growing a field that would
// carry the values the refusal contract hides.
func TestOperationalEventsUseClosedFieldSets(t *testing.T) {
	router, logs := learningRouterUnderTest(t, activeStudent(), nil,
		fixedLearningEvaluator{decision: entitlement.Decision{Reason: entitlement.ReasonExpired}})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/learn/lessons/11111111-1111-1111-1111-111111111111/playback", nil))

	allowed := map[string]map[string]bool{
		"authorization_denied": {
			"time": true, "timestamp": true, "level": true, "msg": true, "service": true, "environment": true,
			"request_id": true, "method": true, "route_template": true, "capability": true, "deny_reason": true,
		},
		"protected_write_failed": {
			"time": true, "timestamp": true, "level": true, "msg": true, "service": true, "environment": true,
			"request_id": true, "method": true, "route_template": true,
			"operation": true, "stage": true, "failure_class": true,
		},
	}

	seen := 0
	for _, record := range logRecords(t, logs) {
		name, _ := record["msg"].(string)
		permitted, tracked := allowed[name]
		if !tracked {
			continue
		}
		seen++
		for field := range record {
			if !permitted[field] {
				t.Fatalf("%s gained the field %q; every field on an operational event is a decision "+
					"about what a protected refusal reveals", name, field)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no tracked operational event was produced; the allowlist would pass vacuously")
	}
}

// TestEveryProtectedLearningRouteLogsItsDenials is the stale guard: route coverage is derived from
// the mounted production router, so a new protected route cannot silently ship without denial
// observability.
func TestEveryProtectedLearningRouteLogsItsDenials(t *testing.T) {
	router, _ := learningRouterUnderTest(t, activeStudent(), nil, fixedLearningEvaluator{})

	protected := make([]string, 0)
	for _, route := range router.Routes() {
		if strings.HasPrefix(route.Path, "/api/v1/learn/") {
			protected = append(protected, route.Method+" "+route.Path)
		}
	}
	sort.Strings(protected)
	if len(protected) == 0 {
		t.Fatal("no protected learning routes were mounted; this guard would pass vacuously")
	}

	for _, mounted := range protected {
		method, path, _ := strings.Cut(mounted, " ")
		// Resolve the template to a concrete request without leaking anything meaningful.
		request := strings.NewReplacer(
			":courseId", "33333333-3333-3333-3333-333333333333",
			":lessonId", "11111111-1111-1111-1111-111111111111",
		).Replace(path)

		var body string
		if method == http.MethodPut || (method == http.MethodPost && strings.HasSuffix(path, "/reports")) {
			body = `{}`
		}
		// A principal that fails the capability gate denies every route uniformly, which is the one
		// denial every protected route must be able to produce.
		instructor := identity.Principal{AccountID: "user-1", Role: identity.RoleInstructor, Status: identity.StatusActive, CredentialState: identity.CredentialActive}
		routeRouter, logs := learningRouterUnderTest(t, instructor, nil, fixedLearningEvaluator{decision: entitlement.Decision{Allowed: true}})

		httpRequest := httptest.NewRequest(method, request, strings.NewReader(body))
		if body != "" {
			httpRequest.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		routeRouter.ServeHTTP(response, httpRequest)

		if response.Code != http.StatusNotFound {
			t.Fatalf("%s = %d, want the uniform refusal", mounted, response.Code)
		}
		denials := recordsNamed(logRecords(t, logs), "authorization_denied")
		if len(denials) != 1 {
			t.Fatalf("%s produced %d authorization_denied events, want exactly 1; a protected route "+
				"without denial observability cannot be reconstructed from the log", mounted, len(denials))
		}
		if denials[0]["deny_reason"] != string(identity.DenyRoleLacksCapability) {
			t.Fatalf("%s deny_reason = %v", mounted, denials[0]["deny_reason"])
		}
	}
	t.Logf("denial observability proven for %d protected learning routes: %v", len(protected), protected)
}

// TestProtectedLearningNeverLogsBodiesContextsOrStoreErrors is the source-level companion: the
// production files that answer these routes must not reach for a raw error or a request field.
func TestProtectedLearningNeverLogsBodiesContextsOrStoreErrors(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolving internal root: %v", err)
	}
	files := []string{
		filepath.Join(root, "httpapi", "learning_handlers.go"),
		filepath.Join(root, "httpapi", "learning_report_handlers.go"),
		filepath.Join(root, "httpapi", "learning_rate_limit.go"),
	}

	// A logging call that renders an error or a body field is the shape that leaks SQL, constraint
	// names, and Student text into telemetry.
	forbidden := regexp.MustCompile(`(?i)(slog\.|logger\.|log\.)[A-Za-z]*\(.*(err\.Error\(\)|body\.|\.Explanation|ReportContext|report_context)`)
	for _, file := range files {
		content, readErr := os.ReadFile(file)
		if readErr != nil {
			t.Fatalf("reading %s: %v", file, readErr)
		}
		// Comments are prose; a doc comment naming a forbidden sink is not a use of one.
		source := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(string(content), "")
		source = regexp.MustCompile(`(?m)//.*$`).ReplaceAllString(source, "")
		if forbidden.MatchString(source) {
			t.Fatalf("%s logs an error message or a request field; protected-learning telemetry carries "+
				"typed causes only", filepath.Base(file))
		}
		if strings.Contains(source, "fmt.Print") || strings.Contains(source, "println(") {
			t.Fatalf("%s uses free-form printing instead of the structured logger", filepath.Base(file))
		}
	}
}
