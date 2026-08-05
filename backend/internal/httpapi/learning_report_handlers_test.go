package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
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

// T063 focused route evidence.
//
// These pin the parts of `POST /api/v1/learn/reports` that need no database: what the request may
// contain, which failures are public and which are the uniform refusal, and that nothing reaches
// the report repository until the context and current access have been established. The A→B
// fidelity, duplicate, and side-effect properties are proven against real PostgreSQL through the
// production router in learning_report_integration_test.go.

const (
	reportRoutePath   = "/api/v1/learn/reports"
	reportTestSession = "test-session-user-1"
	reportTestRoot    = "gradex-test-report-context-root-secret"
)

var reportTestIssuance = time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)

// reportSignerAt builds a signer sharing the accepted test key, with its own clock. Minting at one
// instant and verifying at another is how the expiry boundary is exercised without sleeping.
func reportSignerAt(t *testing.T, root string, at time.Time) *learning.ReportContextSigner {
	t.Helper()
	signer, err := learning.NewReportContextSigner(
		learning.DeriveReportContextKey([]byte(root)),
		learning.DefaultReportContextLifetime,
		func() time.Time { return at },
		func(b []byte) error {
			for i := range b {
				b[i] = byte(i + 7)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("constructing report context signer: %v", err)
	}
	return signer
}

// mintTestContext produces a context for the authenticated focused-test principal.
func mintTestContext(t *testing.T, signer *learning.ReportContextSigner, request learning.ReportContextRequest) string {
	t.Helper()
	if request.ReporterAccountID == "" {
		request.ReporterAccountID = "user-1"
		request.SessionID = reportTestSession
	}
	if request.CourseID == "" {
		request.CourseID = "33333333-3333-3333-3333-333333333333"
	}
	if request.StableTargetID == "" {
		request.StableTargetID = "11111111-1111-1111-1111-111111111111"
	}
	if request.VisibleCourseRevisionID == "" {
		request.VisibleCourseRevisionID = "44444444-4444-4444-4444-444444444444"
	}
	token, err := signer.Mint(request)
	if err != nil {
		t.Fatalf("minting test report context: %v", err)
	}
	return token
}

func defaultLessonContext(t *testing.T) string {
	t.Helper()
	return mintTestContext(t, reportSignerAt(t, reportTestRoot, reportTestIssuance),
		learning.ReportContextRequest{TargetKind: learning.ReportTargetLesson})
}

// learningReportRouter composes the production router with a counting repository, so a test can
// prove the report domain was never reached.
func learningReportRouter(t *testing.T, principal identity.Principal, authErr error, evaluator learningEvaluator, issuer LearningReportContextIssuer) (*gin.Engine, *countingLearningRepository, *syncBuffer) {
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
	repository := &countingLearningRepository{}
	foundation, err := NewLearningFoundation(LearningFoundationOptions{
		Repository: repository, Evaluator: evaluator, Media: unavailableLearningMedia{},
		ReportContexts: issuer, Limiter: testLearningLimiter(t), Policies: testLearningPolicies(),
		Now: func() time.Time { return reportTestIssuance },
	})
	if err != nil {
		t.Fatalf("creating learning foundation: %v", err)
	}
	buf := &syncBuffer{}
	router, err := NewRouter(cfg, logging.New(buf, "gradex-api-test", "development", logging.LevelFromString("info")),
		health.New(time.Second), fakeAuth{err: authErr}, fixedPrincipals{principal: principal}, WithLearningFoundation(foundation))
	if err != nil {
		t.Fatalf("building learning report router: %v", err)
	}
	return router, repository, buf
}

func reportRequest(t *testing.T, router *gin.Engine, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, reportRoutePath, strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func activeStudent() identity.Principal {
	return identity.Principal{AccountID: "user-1", Role: identity.RoleStudent, Status: identity.StatusActive, CredentialState: identity.CredentialActive}
}

func allowingReportEvaluator() learningEvaluator {
	return fixedLearningEvaluator{decision: entitlement.Decision{Allowed: true, Reason: entitlement.ReasonAllowed}}
}

// TestReportRouteIsMountedForStudentPost proves the exact route and method exist under the
// protected learning group, and that no other method is served there.
func TestReportRouteIsMountedForStudentPost(t *testing.T) {
	router, _, _ := learningReportRouter(t, activeStudent(), nil, allowingReportEvaluator(), testReportContextIssuer(t))

	var methods []string
	for _, route := range router.Routes() {
		if route.Path == reportRoutePath {
			methods = append(methods, route.Method)
		}
	}
	if !reflect.DeepEqual(methods, []string{http.MethodPost}) {
		t.Fatalf("report route methods = %v, want exactly POST", methods)
	}
}

// TestReportRequestValidationIsPublicAndTargetIndependent covers the admission boundary. Each of
// these answers is the same for every caller and depends on no target, so it is an ordinary
// validation problem rather than the protected refusal — and none of them reaches the domain.
func TestReportRequestValidationIsPublicAndTargetIndependent(t *testing.T) {
	valid := defaultLessonContext(t)
	oversized := strings.Repeat("x", learningRequestBodyLimit+1)

	tests := []struct {
		name        string
		contentType string
		body        string
		want        int
	}{
		{"wrong content type", "text/plain", `{"report_context":"x","reason":"inaccurate"}`, http.StatusUnsupportedMediaType},
		{"body over the limit", "application/json", `{"report_context":"` + oversized + `","reason":"inaccurate"}`, http.StatusRequestEntityTooLarge},
		{"empty body", "application/json", ``, http.StatusBadRequest},
		{"malformed JSON", "application/json", `{"reason":`, http.StatusBadRequest},
		{"trailing JSON", "application/json", `{"report_context":"` + valid + `","reason":"inaccurate"}{}`, http.StatusBadRequest},
		{"duplicate member", "application/json", `{"report_context":"` + valid + `","reason":"inaccurate","reason":"other"}`, http.StatusBadRequest},
		{"unknown field", "application/json", `{"report_context":"` + valid + `","reason":"inaccurate","queue_position":1}`, http.StatusBadRequest},
		{"type mismatch", "application/json", `{"report_context":"` + valid + `","reason":5}`, http.StatusBadRequest},
		{"missing context", "application/json", `{"reason":"inaccurate"}`, http.StatusUnprocessableEntity},
		{"missing reason", "application/json", `{"report_context":"` + valid + `"}`, http.StatusUnprocessableEntity},
		{"reason outside the fixed set", "application/json", `{"report_context":"` + valid + `","reason":"spam"}`, http.StatusUnprocessableEntity},
		{"other without explanation", "application/json", `{"report_context":"` + valid + `","reason":"other"}`, http.StatusUnprocessableEntity},
		{"other with blank explanation", "application/json", `{"report_context":"` + valid + `","reason":"other","explanation":"   "}`, http.StatusUnprocessableEntity},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router, repository, _ := learningReportRouter(t, activeStudent(), nil, allowingReportEvaluator(), testReportContextIssuer(t))
			response := reportRequest(t, router, tc.contentType, tc.body)
			if response.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", response.Code, tc.want, response.Body.String())
			}
			if repository.reportCalls != 0 {
				t.Fatal("an invalid request reached report creation")
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
			}
		})
	}
}

// TestReportRequestRejectsClientChosenTargets is the D-065 boundary at the wire: a client may not
// name what it reports. Every internal identity is an unknown field, so a request carrying one is
// refused outright rather than silently ignored.
func TestReportRequestRejectsClientChosenTargets(t *testing.T) {
	valid := defaultLessonContext(t)
	for _, field := range []string{
		`"target_id":"11111111-1111-1111-1111-111111111111"`,
		`"target_kind":"LESSON"`,
		`"course_id":"33333333-3333-3333-3333-333333333333"`,
		`"revision_id":"44444444-4444-4444-4444-444444444444"`,
		`"asset_version_id":"55555555-5555-5555-5555-555555555555"`,
		`"target_revision_ref":"44444444-4444-4444-4444-444444444444"`,
		`"reporter_account_id":"user-1"`,
		`"session_id":"` + reportTestSession + `"`,
	} {
		t.Run(field, func(t *testing.T) {
			router, repository, _ := learningReportRouter(t, activeStudent(), nil, allowingReportEvaluator(), testReportContextIssuer(t))
			body := `{"report_context":"` + valid + `","reason":"inaccurate",` + field + `}`
			response := reportRequest(t, router, "application/json", body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("client-chosen target accepted: status = %d %s", response.Code, response.Body.String())
			}
			if repository.reportCalls != 0 {
				t.Fatal("a client-chosen target reached report creation")
			}
		})
	}
}

// TestReportContextFailuresAreOneRefusal covers every way a context can fail to establish the
// binding. All of them are byte-identical, and none of them reaches the report domain.
func TestReportContextFailuresAreOneRefusal(t *testing.T) {
	live := reportSignerAt(t, reportTestRoot, reportTestIssuance)
	// The foundation verifies at issuance + 1h, so a context minted at issuance is expired.
	lateVerifier := reportSignerAt(t, reportTestRoot, reportTestIssuance.Add(time.Hour))
	foreignKey := reportSignerAt(t, "a-different-application-root-secret-entirely", reportTestIssuance)

	valid := mintTestContext(t, live, learning.ReportContextRequest{TargetKind: learning.ReportTargetLesson})
	tampered := valid[:len(valid)-2] + "AA"
	foreignReporter := mintTestContext(t, live, learning.ReportContextRequest{
		TargetKind: learning.ReportTargetLesson, ReporterAccountID: "user-2", SessionID: reportTestSession,
	})
	foreignSession := mintTestContext(t, live, learning.ReportContextRequest{
		TargetKind: learning.ReportTargetLesson, ReporterAccountID: "user-1", SessionID: "another-session",
	})
	future := mintTestContext(t, reportSignerAt(t, reportTestRoot, reportTestIssuance.Add(24*time.Hour)),
		learning.ReportContextRequest{TargetKind: learning.ReportTargetLesson})
	wrongKey := mintTestContext(t, foreignKey, learning.ReportContextRequest{TargetKind: learning.ReportTargetLesson})

	tests := []struct {
		name     string
		issuer   LearningReportContextIssuer
		token    string
		student  identity.Principal
		authErr  error
		decision entitlement.Decision
	}{
		{name: "malformed context", issuer: live, token: "not-a-context"},
		{name: "unknown version prefix", issuer: live, token: "grc9." + strings.SplitN(valid, ".", 2)[1]},
		{name: "undecodable envelope", issuer: live, token: "grc1.!!!!"},
		{name: "tampered ciphertext", issuer: live, token: tampered},
		{name: "wrong encryption key", issuer: live, token: wrongKey},
		{name: "expired context", issuer: lateVerifier, token: valid},
		{name: "future issued context", issuer: live, token: future},
		{name: "another student's context", issuer: live, token: foreignReporter},
		{name: "another session's context", issuer: live, token: foreignSession},
		{name: "oversized context", issuer: live, token: strings.Repeat("a", 5000)},
	}

	var baseline *httptest.ResponseRecorder
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			student := tc.student
			if student.AccountID == "" {
				student = activeStudent()
			}
			decision := tc.decision
			if decision.Reason == "" {
				decision = entitlement.Decision{Allowed: true, Reason: entitlement.ReasonAllowed}
			}
			router, repository, _ := learningReportRouter(t, student, tc.authErr, fixedLearningEvaluator{decision: decision}, tc.issuer)
			body, err := json.Marshal(map[string]string{"report_context": tc.token, "reason": "inaccurate"})
			if err != nil {
				t.Fatalf("encoding request: %v", err)
			}
			response := reportRequest(t, router, "application/json", string(body))
			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want the uniform refusal: %s", response.Code, response.Body.String())
			}
			if repository.reportCalls != 0 {
				t.Fatal("an unverified context reached report creation")
			}
			if baseline == nil {
				baseline = response
				return
			}
			if response.Body.String() != baseline.Body.String() || !reflect.DeepEqual(response.Header(), baseline.Header()) {
				t.Fatalf("refusal differs from baseline:\nheaders %#v/%#v\nbody %q/%q",
					response.Header(), baseline.Header(), response.Body.String(), baseline.Body.String())
			}
		})
	}
}

// TestReportRouteRefusesNonStudentPrincipalsIdentically keeps the route on the same authentication
// and capability gate as every other protected learning route: an Instructor, an Admin, a suspended
// Account, and an anonymous caller are indistinguishable from each other and from a bad context.
func TestReportRouteRefusesNonStudentPrincipalsIdentically(t *testing.T) {
	valid := defaultLessonContext(t)
	tests := []struct {
		name       string
		principal  identity.Principal
		authErr    error
		wantReason string
	}{
		{"anonymous", activeStudent(), errors.New("no session"), string(identity.DenyPrincipalNotFound)},
		{"instructor", identity.Principal{AccountID: "user-1", Role: identity.RoleInstructor, Status: identity.StatusActive, CredentialState: identity.CredentialActive}, nil, string(identity.DenyRoleLacksCapability)},
		{"admin", identity.Principal{AccountID: "user-1", Role: identity.RoleAdmin, Status: identity.StatusActive, CredentialState: identity.CredentialActive}, nil, string(identity.DenyRoleLacksCapability)},
		{"suspended student", identity.Principal{AccountID: "user-1", Role: identity.RoleStudent, Status: identity.StatusSuspended, CredentialState: identity.CredentialActive}, nil, string(identity.DenyAccountSuspended)},
	}

	var baseline *httptest.ResponseRecorder
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router, repository, logs := learningReportRouter(t, tc.principal, tc.authErr, allowingReportEvaluator(), testReportContextIssuer(t))
			response := reportRequest(t, router, "application/json", `{"report_context":"`+valid+`","reason":"inaccurate"}`)
			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want the uniform refusal", response.Code)
			}
			if repository.reportCalls != 0 {
				t.Fatal("an unauthorized principal reached report creation")
			}
			if baseline == nil {
				baseline = response
			} else if response.Body.String() != baseline.Body.String() || !reflect.DeepEqual(response.Header(), baseline.Header()) {
				t.Fatalf("principal refusal differs from baseline: %q/%q", response.Body.String(), baseline.Body.String())
			}
			assertDenyLogged(t, logs, tc.wantReason)
		})
	}
}

// TestReportRouteRequiresCurrentEntitlement is FR-033 at the route: the context is evidence, never
// capability, so every S4 denial refuses the submission even though the token itself is perfect.
func TestReportRouteRequiresCurrentEntitlement(t *testing.T) {
	valid := defaultLessonContext(t)
	for _, reason := range []entitlement.Reason{
		entitlement.ReasonNoApplicableGrant, entitlement.ReasonExpired,
		entitlement.ReasonAccountSuspended, entitlement.ReasonCourseSuspended, entitlement.ReasonRetired,
	} {
		t.Run(string(reason), func(t *testing.T) {
			router, _, logs := learningReportRouter(t, activeStudent(), nil,
				fixedLearningEvaluator{decision: entitlement.Decision{Reason: reason}}, testReportContextIssuer(t))
			response := reportRequest(t, router, "application/json", `{"report_context":"`+valid+`","reason":"inaccurate"}`)
			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want the uniform refusal for %s", response.Code, reason)
			}
			assertDenyLogged(t, logs, string(reason))
		})
	}
}

// TestReportRefusalsRevealNothing pins what a refusal may not contain. The evidence a report stores
// is exactly the evidence a denial must never leak.
func TestReportRefusalsRevealNothing(t *testing.T) {
	signer := reportSignerAt(t, reportTestRoot, reportTestIssuance)
	token := mintTestContext(t, signer, learning.ReportContextRequest{
		TargetKind:            learning.ReportTargetVideo,
		VisibleAssetVersionID: "55555555-5555-5555-5555-555555555555",
	})
	router, _, logs := learningReportRouter(t, activeStudent(), nil,
		fixedLearningEvaluator{decision: entitlement.Decision{Reason: entitlement.ReasonNoApplicableGrant}}, testReportContextIssuer(t))
	response := reportRequest(t, router, "application/json", `{"report_context":"`+token+`","reason":"inaccurate"}`)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d", response.Code)
	}
	for _, header := range []string{"Location", "Retry-After", "WWW-Authenticate"} {
		if response.Header().Get(header) != "" {
			t.Fatalf("refusal carried %s", header)
		}
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
	surfaces := response.Body.String() + logs.String()
	for _, secret := range []string{
		token, "55555555-5555-5555-5555-555555555555", "44444444-4444-4444-4444-444444444444",
		"33333333-3333-3333-3333-333333333333", "revision", "asset_version", "target_revision_ref",
		"entitlement_id", "enrollment_id", reportTestSession,
	} {
		if strings.Contains(surfaces, secret) {
			t.Fatalf("a refusal or its log exposed %q", secret)
		}
	}
}

// TestReportRouteRefusesWithoutAnAuthenticatedSession proves the session binding cannot be skipped:
// an authenticator that publishes no session leaves nothing to bind the context to, so the route
// refuses rather than accepting a context on the Account alone.
func TestReportRouteRefusesWithoutAnAuthenticatedSession(t *testing.T) {
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
	repository := &countingLearningRepository{}
	foundation, err := NewLearningFoundation(LearningFoundationOptions{
		Repository: repository, Evaluator: allowingReportEvaluator(), Media: unavailableLearningMedia{},
		ReportContexts: testReportContextIssuer(t), Limiter: testLearningLimiter(t), Policies: testLearningPolicies(),
		Now: func() time.Time { return reportTestIssuance },
	})
	if err != nil {
		t.Fatalf("creating learning foundation: %v", err)
	}
	router, err := NewRouter(cfg, logging.New(&syncBuffer{}, "gradex-api-test", "development", logging.LevelFromString("info")),
		health.New(time.Second), sessionlessLearningAuth{}, fixedPrincipals{principal: activeStudent()}, WithLearningFoundation(foundation))
	if err != nil {
		t.Fatalf("router: %v", err)
	}

	response := reportRequest(t, router, "application/json", `{"report_context":"`+defaultLessonContext(t)+`","reason":"inaccurate"}`)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want the uniform refusal", response.Code)
	}
	if repository.reportCalls != 0 {
		t.Fatal("a sessionless request reached report creation")
	}
}

// sessionlessLearningAuth authenticates an Account but publishes no session.
type sessionlessLearningAuth struct{}

func (sessionlessLearningAuth) UserFromRequest(*gin.Context) (string, error) { return "user-1", nil }

// TestReportFoundationRequiresAVerifyingContextDependency proves minting and verification are one
// dependency: a foundation cannot be composed with an issuer that cannot verify what it minted.
func TestReportFoundationRequiresAVerifyingContextDependency(t *testing.T) {
	// The compile-time half: nothing may satisfy the dependency by minting alone.
	var _ reportContextIssuer = mintAndVerifyIssuer{}

	if _, err := NewLearningFoundation(LearningFoundationOptions{
		Repository: &countingLearningRepository{}, Evaluator: allowingReportEvaluator(),
		Media: unavailableLearningMedia{}, Limiter: testLearningLimiter(t), Policies: testLearningPolicies(),
	}); err == nil {
		t.Fatal("a foundation without a report context dependency must not construct")
	}
}

// mintAndVerifyIssuer makes the compile-time contract explicit: the dependency is satisfiable only
// by something that verifies as well as mints.
type mintAndVerifyIssuer struct{}

func (mintAndVerifyIssuer) Mint(learning.ReportContextRequest) (string, error) { return "", nil }

func (mintAndVerifyIssuer) Verify(string, string, string) (learning.VerifiedReportBinding, error) {
	return learning.VerifiedReportBinding{}, errors.New("unavailable")
}

// TestReportGuardIsMandatory proves the domain refuses an unguarded route write: a report may never
// be created without the current-access decision in the same transaction.
func TestReportGuardIsMandatory(t *testing.T) {
	repository := &learning.Repository{}
	if _, err := repository.CreateReportGuarded(context.Background(), learning.VerifiedReportBinding{},
		learning.ReportContent{Reason: learning.ReasonInaccurate}, func() time.Time { return reportTestIssuance }, nil); err == nil {
		t.Fatal("report creation accepted a nil authorization guard")
	}
}
