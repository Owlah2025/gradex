package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/entitlement"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/learning"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

// T064 focused route evidence: where the report throttle runs, what it costs,
// and what it refuses.
//
// The threshold itself is the policy's, proven in internal/ratelimit and again
// through real PostgreSQL and Redis in the integration file. What these pin is
// the wiring: the limiter sees only the authenticated Account, it runs before
// anything the request controls, and a refusal reaches no verifier, evaluator,
// or repository.

// spyReportRateStore records every decision the route asks for and answers from
// a script, so a test can assert both the count and the effect.
type spyReportRateStore struct {
	mu       sync.Mutex
	calls    int
	keys     []string
	allow    bool
	failWith error
}

func (s *spyReportRateStore) Decide(_ context.Context, entries []ratelimit.Entry) (bool, error) {
	s.mu.Lock()
	s.calls++
	for _, entry := range entries {
		s.keys = append(s.keys, entry.Key)
	}
	s.mu.Unlock()
	if s.failWith != nil {
		return false, s.failWith
	}
	return s.allow, nil
}

func (s *spyReportRateStore) decisions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *spyReportRateStore) observedKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.keys...)
}

// countingReportContexts records whether the encrypted context was ever touched.
type countingReportContexts struct {
	mu       sync.Mutex
	mints    int
	verifies int
	delegate LearningReportContextIssuer
}

func (c *countingReportContexts) Mint(request learning.ReportContextRequest) (string, error) {
	c.mu.Lock()
	c.mints++
	c.mu.Unlock()
	return c.delegate.Mint(request)
}

func (c *countingReportContexts) Verify(token, reporter, session string) (learning.VerifiedReportBinding, error) {
	c.mu.Lock()
	c.verifies++
	c.mu.Unlock()
	return c.delegate.Verify(token, reporter, session)
}

func (c *countingReportContexts) verifyCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.verifies
}

type throttleRouterParts struct {
	router     *gin.Engine
	store      *spyReportRateStore
	contexts   *countingReportContexts
	repository *countingLearningRepository
	evaluator  *recordingLearningEvaluator
	logs       *syncBuffer
}

func learningThrottleRouter(t *testing.T, principal identity.Principal, authErr error, store *spyReportRateStore) throttleRouterParts {
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
	limiter, err := ratelimit.New(store, []byte(strings.Repeat("t", 32)), time.Second)
	if err != nil {
		t.Fatalf("constructing throttle limiter: %v", err)
	}
	repository := &countingLearningRepository{}
	contexts := &countingReportContexts{delegate: testReportContextIssuer(t)}
	evaluator := &recordingLearningEvaluator{decision: entitlement.Decision{Allowed: true, Reason: entitlement.ReasonAllowed}}
	foundation, err := NewLearningFoundation(LearningFoundationOptions{
		Repository: repository, Evaluator: evaluator, Media: unavailableLearningMedia{},
		ReportContexts: contexts, Limiter: limiter, Policies: testLearningPolicies(),
		Now: func() time.Time { return reportTestIssuance },
	})
	if err != nil {
		t.Fatalf("creating learning foundation: %v", err)
	}
	logs := &syncBuffer{}
	router, err := NewRouter(cfg, logging.New(logs, "gradex-api-test", "development", logging.LevelFromString("info")),
		health.New(time.Second), fakeAuth{err: authErr}, fixedPrincipals{principal: principal}, WithLearningFoundation(foundation))
	if err != nil {
		t.Fatalf("building throttle router: %v", err)
	}
	return throttleRouterParts{router: router, store: store, contexts: contexts, repository: repository, evaluator: evaluator, logs: logs}
}

// TestReportThrottleRunsAfterAuthenticationAndTheStudentGate proves an
// unauthenticated or non-Student caller never reaches — and never spends — a
// Student-keyed quota, so the throttle cannot become an identity oracle.
func TestReportThrottleRunsAfterAuthenticationAndTheStudentGate(t *testing.T) {
	valid := defaultLessonContext(t)
	tests := []struct {
		name      string
		principal identity.Principal
		authErr   error
	}{
		{"anonymous", activeStudent(), errors.New("no session")},
		{"instructor", identity.Principal{AccountID: "user-1", Role: identity.RoleInstructor, Status: identity.StatusActive, CredentialState: identity.CredentialActive}, nil},
		{"admin", identity.Principal{AccountID: "user-1", Role: identity.RoleAdmin, Status: identity.StatusActive, CredentialState: identity.CredentialActive}, nil},
		{"suspended student", identity.Principal{AccountID: "user-1", Role: identity.RoleStudent, Status: identity.StatusSuspended, CredentialState: identity.CredentialActive}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parts := learningThrottleRouter(t, tc.principal, tc.authErr, &spyReportRateStore{allow: true})
			response := reportRequest(t, parts.router, "application/json", `{"report_context":"`+valid+`","reason":"inaccurate"}`)
			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want the uniform refusal", response.Code)
			}
			if parts.store.decisions() != 0 {
				t.Fatalf("%s spent %d report quota decisions before the Student gate", tc.name, parts.store.decisions())
			}
		})
	}
}

// TestReportThrottleRunsBeforeTheBodyAndTheContext proves the decision needs
// nothing the request controls: a refused attempt never parses the body and
// never decrypts the context, so an attacker cannot make the server do the
// expensive work first.
func TestReportThrottleRunsBeforeTheBodyAndTheContext(t *testing.T) {
	for _, body := range []string{
		`{"report_context":"` + defaultLessonContext(t) + `","reason":"inaccurate"}`,
		`{"report_context":"tampered","reason":"not-a-reason"}`,
		`{ this is not JSON`,
		``,
	} {
		parts := learningThrottleRouter(t, activeStudent(), nil, &spyReportRateStore{allow: false})
		response := reportRequest(t, parts.router, "application/json", body)

		if response.Code != http.StatusTooManyRequests {
			t.Fatalf("throttled status = %d, want 429 regardless of the body: %s", response.Code, response.Body.String())
		}
		if parts.store.decisions() != 1 {
			t.Fatalf("one request produced %d throttle decisions, want exactly 1", parts.store.decisions())
		}
		if parts.contexts.verifyCount() != 0 {
			t.Fatal("a throttled request decrypted the report context")
		}
		if parts.evaluator.studentID != "" {
			t.Fatal("a throttled request evaluated Entitlement")
		}
		if parts.repository.reportCalls != 0 {
			t.Fatal("a throttled request reached report creation")
		}
	}
}

// TestThrottledReportResponseIsTheExactContract pins the wire contract of a
// quota refusal, including what it must not carry.
func TestThrottledReportResponseIsTheExactContract(t *testing.T) {
	parts := learningThrottleRouter(t, activeStudent(), nil, &spyReportRateStore{allow: false})
	response := reportRequest(t, parts.router, "application/json",
		`{"report_context":"`+defaultLessonContext(t)+`","reason":"inaccurate"}`)

	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", response.Code)
	}
	if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/problem+json") {
		t.Fatalf("Content-Type = %q, want application/problem+json", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	retryAfter := response.Header().Get("Retry-After")
	seconds, err := strconv.Atoi(retryAfter)
	if err != nil {
		t.Fatalf("Retry-After = %q, want integer delta-seconds", retryAfter)
	}
	if seconds <= 0 || seconds > int(time.Hour.Seconds()) {
		t.Fatalf("Retry-After = %d, want a positive value inside the one-hour window", seconds)
	}
	if response.Header().Get("Location") != "" {
		t.Fatal("a throttled response carried a Location")
	}

	// The refusal describes only that the caller must wait.
	body := response.Body.String()
	if !strings.Contains(body, `"status":429`) {
		t.Fatalf("throttled body = %s", body)
	}
	for _, secret := range append(parts.store.observedKeys(),
		"user-1", "learning-report-v1", "identifier", "redis", "Redis", "local",
		"remaining", "quota", "course", "lesson", "report_id", "entitlement",
	) {
		if strings.Contains(body, secret) {
			t.Fatalf("the throttled response exposed %q: %s", secret, body)
		}
	}
}

// correlationPattern matches the per-request identifier the ordinary problem
// writer attaches. A 429 is not inventory-sensitive — unlike the protected 404,
// which deliberately strips it — so it keeps the normal correlation value and
// only that value may differ between two throttled responses.
var correlationPattern = regexp.MustCompile(`[0-9a-f]{32}`)

// TestThrottledReportIsIdenticalAcrossTargetsAndStudents proves the refusal
// carries nothing that varies with who is asking or what they meant to report,
// apart from the ordinary request correlation identifier.
func TestThrottledReportIsIdenticalAcrossTargetsAndStudents(t *testing.T) {
	signer := reportSignerAt(t, reportTestRoot, reportTestIssuance)
	bodies := []string{
		`{"report_context":"` + defaultLessonContext(t) + `","reason":"inaccurate"}`,
		`{"report_context":"` + mintTestContext(t, signer, learning.ReportContextRequest{
			TargetKind: learning.ReportTargetVideo, VisibleAssetVersionID: "55555555-5555-5555-5555-555555555555",
		}) + `","reason":"inappropriate","explanation":"something"}`,
		`{"report_context":"` + mintTestContext(t, signer, learning.ReportContextRequest{
			TargetKind: learning.ReportTargetCourse, StableTargetID: "33333333-3333-3333-3333-333333333333",
		}) + `","reason":"other","explanation":"detail"}`,
	}

	var baseline string
	for _, body := range bodies {
		parts := learningThrottleRouter(t, activeStudent(), nil, &spyReportRateStore{allow: false})
		response := reportRequest(t, parts.router, "application/json", body)
		if response.Code != http.StatusTooManyRequests {
			t.Fatalf("status = %d, want 429", response.Code)
		}
		got := correlationPattern.ReplaceAllString(response.Body.String(), "<correlation>")
		if !strings.Contains(got, "<correlation>") {
			t.Fatalf("throttled body carried no correlation identifier to elide: %s", response.Body.String())
		}
		if baseline == "" {
			baseline = got
			continue
		}
		if got != baseline {
			t.Fatalf("throttled body varies with the request: %q vs %q", got, baseline)
		}
	}
}

// TestAdmittedReportRequestProceedsToTheAcceptedRoute proves the throttle is
// transparent when it admits: T063 runs exactly as before.
func TestAdmittedReportRequestProceedsToTheAcceptedRoute(t *testing.T) {
	parts := learningThrottleRouter(t, activeStudent(), nil, &spyReportRateStore{allow: true})
	response := reportRequest(t, parts.router, "application/json",
		`{"report_context":"`+defaultLessonContext(t)+`","reason":"inaccurate"}`)

	// The counting repository refuses, so the accepted uniform refusal is the
	// proof the request travelled the whole T063 path.
	if response.Code != http.StatusNotFound {
		t.Fatalf("admitted request = %d %s", response.Code, response.Body.String())
	}
	if parts.store.decisions() != 1 {
		t.Fatalf("admitted request made %d throttle decisions, want 1", parts.store.decisions())
	}
	if parts.contexts.verifyCount() != 1 {
		t.Fatalf("admitted request verified the context %d times, want 1", parts.contexts.verifyCount())
	}
	if parts.repository.reportCalls != 1 {
		t.Fatalf("admitted request reached report creation %d times, want 1", parts.repository.reportCalls)
	}
}

// TestReportThrottleBackendFailureFailsClosedAsProtectedUnavailable follows
// D-061's established boundary: a quota denial is 429, but a limiter dependency
// failure is the protected-unavailable response — and it still admits nothing.
func TestReportThrottleBackendFailureFailsClosedAsProtectedUnavailable(t *testing.T) {
	parts := learningThrottleRouter(t, activeStudent(), nil,
		&spyReportRateStore{failWith: errors.New("redis unavailable")})
	// The bounded local fallback is exhausted, so no backend can decide.
	parts.repository.reportCalls = 0

	var lastResponse *httptest.ResponseRecorder
	for attempt := 0; attempt < int(ratelimit.ProtectedLearningReportsPerHour)+1; attempt++ {
		lastResponse = reportRequest(t, parts.router, "application/json",
			`{"report_context":"`+defaultLessonContext(t)+`","reason":"inaccurate"}`)
	}
	// Beyond the local quota the fallback refuses; it never admits without limit.
	if lastResponse.Code != http.StatusTooManyRequests {
		t.Fatalf("exhausted local fallback = %d, want a refusal", lastResponse.Code)
	}
	if parts.repository.reportCalls > int(ratelimit.ProtectedLearningReportsPerHour) {
		t.Fatalf("a Redis outage admitted %d submissions, want at most the local quota", parts.repository.reportCalls)
	}

	// An undecidable policy is the protected refusal, not a throttle answer.
	undecidable := learningThrottleRouter(t, activeStudent(), nil,
		&spyReportRateStore{failWith: errors.New("redis unavailable")})
	undecidable.repository.reportCalls = 0
	strict := testLearningPolicies()
	strictPolicy := ratelimit.ProtectedLearningReportPolicy()
	strictPolicy.FailClosed = true
	strict["learning-report"] = strictPolicy
	response := throttleRouterWithPolicies(t, strict).request(t,
		`{"report_context":"`+defaultLessonContext(t)+`","reason":"inaccurate"}`)
	if response.Code != http.StatusNotFound {
		t.Fatalf("undecidable limiter = %d, want the protected-unavailable refusal", response.Code)
	}
	if response.Header().Get("Retry-After") != "" {
		t.Fatal("an undecidable limiter answered with a Retry-After")
	}
}

// throttleRouterWithPolicies composes a router around an explicit policy map.
type policyThrottleRouter struct {
	router     *gin.Engine
	repository *countingLearningRepository
}

func (p policyThrottleRouter) request(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	return reportRequest(t, p.router, "application/json", body)
}

func throttleRouterWithPolicies(t *testing.T, policies map[string]ratelimit.Policy) policyThrottleRouter {
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
	limiter, err := ratelimit.New(&spyReportRateStore{failWith: errors.New("redis unavailable")},
		[]byte(strings.Repeat("u", 32)), time.Second)
	if err != nil {
		t.Fatalf("constructing limiter: %v", err)
	}
	repository := &countingLearningRepository{}
	foundation, err := NewLearningFoundation(LearningFoundationOptions{
		Repository: repository, Evaluator: allowingReportEvaluator(), Media: unavailableLearningMedia{},
		ReportContexts: testReportContextIssuer(t), Limiter: limiter, Policies: policies,
		Now: func() time.Time { return reportTestIssuance },
	})
	if err != nil {
		t.Fatalf("creating learning foundation: %v", err)
	}
	router, err := NewRouter(cfg, logging.New(&syncBuffer{}, "gradex-api-test", "development", logging.LevelFromString("info")),
		health.New(time.Second), fakeAuth{}, fixedPrincipals{principal: activeStudent()}, WithLearningFoundation(foundation))
	if err != nil {
		t.Fatalf("router: %v", err)
	}
	return policyThrottleRouter{router: router, repository: repository}
}

// TestLearningFoundationRequiresTheReportPolicy proves the throttle cannot be
// left unconfigured: a foundation without it does not construct.
func TestLearningFoundationRequiresTheReportPolicy(t *testing.T) {
	policies := testLearningPolicies()
	delete(policies, "learning-report")

	if _, err := NewLearningFoundation(LearningFoundationOptions{
		Repository: &countingLearningRepository{}, Evaluator: allowingReportEvaluator(),
		Media: unavailableLearningMedia{}, ReportContexts: testReportContextIssuer(t),
		Limiter: testLearningLimiter(t), Policies: policies,
	}); err == nil {
		t.Fatal("a learning foundation constructed without the report throttle policy")
	}

	// An invalid report policy is refused just as firmly as a missing one.
	invalid := testLearningPolicies()
	broken := ratelimit.ProtectedLearningReportPolicy()
	broken.Rules = nil
	invalid["learning-report"] = broken
	if _, err := NewLearningFoundation(LearningFoundationOptions{
		Repository: &countingLearningRepository{}, Evaluator: allowingReportEvaluator(),
		Media: unavailableLearningMedia{}, ReportContexts: testReportContextIssuer(t),
		Limiter: testLearningLimiter(t), Policies: invalid,
	}); err == nil {
		t.Fatal("a learning foundation constructed with an invalid report throttle policy")
	}
}

// TestReportThrottleIsNotAppliedToOtherLearningRoutes proves the report policy
// belongs to one route: the reads and the Progress write never consult it.
func TestReportThrottleIsNotAppliedToOtherLearningRoutes(t *testing.T) {
	parts := learningThrottleRouter(t, activeStudent(), nil, &spyReportRateStore{allow: true})
	reportPolicyKeys := func() int {
		matched := 0
		for _, key := range parts.store.observedKeys() {
			if strings.Contains(key, "learning-report-v1") {
				matched++
			}
		}
		return matched
	}

	lesson := "11111111-1111-1111-1111-111111111111"
	course := "33333333-3333-3333-3333-333333333333"
	for _, request := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/learn/dashboard", ""},
		{http.MethodGet, "/api/v1/learn/courses/" + course, ""},
		{http.MethodGet, "/api/v1/learn/courses/" + course + "/lessons/" + lesson, ""},
		{http.MethodPost, "/api/v1/learn/lessons/" + lesson + "/playback", ""},
		{http.MethodPut, "/api/v1/learn/lessons/" + lesson + "/progress", `{"position_seconds":5,"asset_version_id":"22222222-2222-2222-2222-222222222222"}`},
	} {
		httpRequest := httptest.NewRequest(request.method, request.path, strings.NewReader(request.body))
		if request.body != "" {
			httpRequest.Header.Set("Content-Type", "application/json")
		}
		parts.router.ServeHTTP(httptest.NewRecorder(), httpRequest)
	}
	if reportPolicyKeys() != 0 {
		t.Fatalf("unrelated learning routes consulted the report throttle %d times", reportPolicyKeys())
	}

	// The report route does, exactly once.
	reportRequest(t, parts.router, "application/json", `{"report_context":"`+defaultLessonContext(t)+`","reason":"inaccurate"}`)
	if reportPolicyKeys() != 1 {
		t.Fatalf("the report route consulted its throttle %d times, want exactly 1", reportPolicyKeys())
	}
}
