package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/entitlement"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/learning"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/media"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

type fixedLearningEvaluator struct{ decision entitlement.Decision }

func (e fixedLearningEvaluator) Evaluate(context.Context, string, string, time.Time) entitlement.Decision {
	return e.decision
}

func (e fixedLearningEvaluator) EvaluateInTransaction(context.Context, pgx.Tx, string, string, time.Time) entitlement.Decision {
	return e.decision
}

func (e fixedLearningEvaluator) EvaluateRead(context.Context, string, string, time.Time) entitlement.ReadDecision {
	if e.decision.Reason == entitlement.ReasonExpired {
		return entitlement.ReadDecision{State: entitlement.ReadExpired, Reason: entitlement.ReasonExpired}
	}
	if e.decision.Allowed {
		return entitlement.ReadDecision{State: entitlement.ReadActive, Reason: entitlement.ReasonAllowed, CourseWide: true}
	}
	return entitlement.ReadDecision{State: entitlement.ReadDenied, Reason: e.decision.Reason}
}

func (e fixedLearningEvaluator) EvaluateCourseReads(context.Context, string, time.Time) (map[string]entitlement.ReadDecision, error) {
	return map[string]entitlement.ReadDecision{}, nil
}

type recordingLearningEvaluator struct {
	studentID string
	lessonID  string
	now       time.Time
	decision  entitlement.Decision
}

func (e *recordingLearningEvaluator) Evaluate(_ context.Context, studentID, lessonID string, now time.Time) entitlement.Decision {
	e.studentID, e.lessonID, e.now = studentID, lessonID, now
	return e.decision
}

func (e *recordingLearningEvaluator) EvaluateInTransaction(_ context.Context, _ pgx.Tx, studentID, lessonID string, now time.Time) entitlement.Decision {
	e.studentID, e.lessonID, e.now = studentID, lessonID, now
	return e.decision
}

func (e *recordingLearningEvaluator) EvaluateRead(_ context.Context, studentID, lessonID string, now time.Time) entitlement.ReadDecision {
	e.studentID, e.lessonID, e.now = studentID, lessonID, now
	if e.decision.Reason == entitlement.ReasonExpired {
		return entitlement.ReadDecision{State: entitlement.ReadExpired, Reason: entitlement.ReasonExpired}
	}
	if e.decision.Allowed {
		return entitlement.ReadDecision{State: entitlement.ReadActive, Reason: entitlement.ReasonAllowed, CourseWide: true}
	}
	return entitlement.ReadDecision{State: entitlement.ReadDenied, Reason: e.decision.Reason}
}

func (e *recordingLearningEvaluator) EvaluateCourseReads(context.Context, string, time.Time) (map[string]entitlement.ReadDecision, error) {
	return map[string]entitlement.ReadDecision{}, nil
}

type unavailableLearningMedia struct{}

func (unavailableLearningMedia) IssuePlayback(context.Context, media.PlaybackRequest) (media.PlaybackAuthorization, error) {
	return media.PlaybackAuthorization{}, media.ErrProtectedUnavailable
}

func (unavailableLearningMedia) TrustedVideoDuration(context.Context, string, string) (time.Duration, error) {
	return 0, media.ErrProtectedUnavailable
}

func (unavailableLearningMedia) MaterialKinds(context.Context, []string) (map[string][]media.Material, error) {
	return map[string][]media.Material{}, nil
}

type deniedLearningRateStore struct{}

func (deniedLearningRateStore) Decide(_ context.Context, entries []ratelimit.Entry) (bool, error) {
	return !strings.Contains(entries[0].Key, "learning-progress-v1"), nil
}

func deniedLearningLimiter(t *testing.T) *ratelimit.Limiter {
	t.Helper()
	limiter, err := ratelimit.New(deniedLearningRateStore{}, bytes.Repeat([]byte{0x52}, 32), time.Second)
	if err != nil {
		t.Fatalf("constructing denied learning limiter: %v", err)
	}
	return limiter
}

func learningRouterUnderTest(t *testing.T, principal identity.Principal, authErr error, evaluator learningEvaluator) (*gin.Engine, *syncBuffer) {
	return learningRouterWithLimiter(t, principal, authErr, evaluator, testLearningLimiter(t))
}

func learningRouterWithLimiter(t *testing.T, principal identity.Principal, authErr error, evaluator learningEvaluator, limiter *ratelimit.Limiter) (*gin.Engine, *syncBuffer) {
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
	pool, err := pgxpool.New(context.Background(), "postgres://gradex:gradex@127.0.0.1:1/gradex?sslmode=disable")
	if err != nil {
		t.Fatalf("creating lazy learning pool: %v", err)
	}
	t.Cleanup(pool.Close)
	repository, err := learning.NewRepository(pool)
	if err != nil {
		t.Fatalf("creating learning repository: %v", err)
	}
	foundation, err := NewLearningFoundation(LearningFoundationOptions{
		ReportContexts: testReportContextIssuer(t),
		Repository:     repository, Evaluator: evaluator, Media: unavailableLearningMedia{},
		Limiter: limiter, Policies: testLearningPolicies(),
	})
	if err != nil {
		t.Fatalf("creating learning foundation: %v", err)
	}
	buf := &syncBuffer{}
	logger := logging.New(buf, "gradex-api-test", "development", logging.LevelFromString("info"))
	router, err := NewRouter(cfg, logger, health.New(time.Second), fakeAuth{err: authErr}, fixedPrincipals{principal: principal}, WithLearningFoundation(foundation))
	if err != nil {
		t.Fatalf("building learning router: %v", err)
	}
	return router, buf
}

func TestProgressRateLimitRunsAfterAccessAndBeforeRepositoryMutation(t *testing.T) {
	student := identity.Principal{AccountID: "user-1", Role: identity.RoleStudent, Status: identity.StatusActive, CredentialState: identity.CredentialActive}
	evaluator := &recordingLearningEvaluator{decision: entitlement.Decision{Allowed: true, Reason: entitlement.ReasonAllowed}}
	router, _ := learningRouterWithLimiter(t, student, nil, evaluator, deniedLearningLimiter(t))
	lessonID := "11111111-1111-1111-1111-111111111111"
	request := httptest.NewRequest(http.MethodPut, "/api/v1/learn/lessons/"+lessonID+"/progress", strings.NewReader(`{"position_seconds": 5, "asset_version_id": "22222222-2222-2222-2222-222222222222"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Retry-After") != "60" {
		t.Fatalf("rate-limited progress response = %d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	if evaluator.studentID != "" || evaluator.lessonID != "" {
		t.Fatalf("student/lesson rate limit evaluated entitlement before denying: evaluator=%+v", evaluator)
	}
}

type sourceDeniedLearningRateStore struct{ calls int }

func (s *sourceDeniedLearningRateStore) Decide(_ context.Context, entries []ratelimit.Entry) (bool, error) {
	s.calls++
	return !strings.Contains(entries[0].Key, "learning-progress-source-v1"), nil
}

func sourceDeniedLearningLimiter(t *testing.T) (*ratelimit.Limiter, *sourceDeniedLearningRateStore) {
	t.Helper()
	store := &sourceDeniedLearningRateStore{}
	limiter, err := ratelimit.New(store, bytes.Repeat([]byte{0x56}, 32), time.Second)
	if err != nil {
		t.Fatalf("constructing source-denied learning limiter: %v", err)
	}
	return limiter, store
}

func TestProgressSourceLimitRunsBeforeAllProtectedLearningAccess(t *testing.T) {
	student := identity.Principal{AccountID: "user-1", Role: identity.RoleStudent, Status: identity.StatusActive, CredentialState: identity.CredentialActive}
	evaluator := &recordingLearningEvaluator{decision: entitlement.Decision{Allowed: true, Reason: entitlement.ReasonAllowed}}
	limiter, store := sourceDeniedLearningLimiter(t)
	router, _ := learningRouterWithLimiter(t, student, nil, evaluator, limiter)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/learn/lessons/11111111-1111-1111-1111-111111111111/progress", strings.NewReader(`{"position_seconds": 5, "asset_version_id": "22222222-2222-2222-2222-222222222222"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Retry-After") != "60" {
		t.Fatalf("source-limited progress response = %d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	if evaluator.studentID != "" || evaluator.lessonID != "" {
		t.Fatalf("source limiter evaluated entitlement before denying: evaluator=%+v", evaluator)
	}
	if store.calls != 1 {
		t.Fatalf("source denial reached %d limiter policies, want only the source policy", store.calls)
	}
}

type unavailableLearningRateStore struct{}

func (unavailableLearningRateStore) Decide(context.Context, []ratelimit.Entry) (bool, error) {
	return false, errors.New("limiter unavailable")
}

func unavailableLearningLimiter(t *testing.T) *ratelimit.Limiter {
	t.Helper()
	limiter, err := ratelimit.New(unavailableLearningRateStore{}, bytes.Repeat([]byte{0x53}, 32), time.Second)
	if err != nil {
		t.Fatalf("constructing unavailable learning limiter: %v", err)
	}
	return limiter
}

func TestProgressSourceLimiterFailsClosedBeforeRepositoryMutation(t *testing.T) {
	student := identity.Principal{AccountID: "user-1", Role: identity.RoleStudent, Status: identity.StatusActive, CredentialState: identity.CredentialActive}
	evaluator := &recordingLearningEvaluator{decision: entitlement.Decision{Allowed: true, Reason: entitlement.ReasonAllowed}}
	router, _ := learningRouterWithLimiter(t, student, nil, evaluator, unavailableLearningLimiter(t))
	request := httptest.NewRequest(http.MethodPut, "/api/v1/learn/lessons/11111111-1111-1111-1111-111111111111/progress", strings.NewReader(`{"position_seconds": 5, "asset_version_id": "22222222-2222-2222-2222-222222222222"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || response.Header().Get("Retry-After") != "" {
		t.Fatalf("unavailable source limiter response = %d headers=%v", response.Code, response.Header())
	}
	if evaluator.studentID != "" || evaluator.lessonID != "" {
		t.Fatalf("unavailable source limiter evaluated entitlement before denying: evaluator=%+v", evaluator)
	}
}

type countingLearningRepository struct {
	enrollmentCalls int
	videoCalls      int
	progressCalls   int
	reportCalls     int
}

func (r *countingLearningRepository) ReadCourseGraph(context.Context, string) (learning.CourseGraph, error) {
	return learning.CourseGraph{}, learning.ErrCourseGraphNotFound
}

func (r *countingLearningRepository) ReadCourseProgress(context.Context, string, string, learning.CourseGraph) (map[string]learning.Progress, learning.CourseProgressSummary, error) {
	return nil, learning.CourseProgressSummary{}, learning.ErrProgressNotFound
}

func (r *countingLearningRepository) ReadLessonProgress(context.Context, string, string) (learning.Progress, error) {
	return learning.Progress{}, learning.ErrProgressNotFound
}

func (r *countingLearningRepository) ListStudentCourseSummaries(context.Context, string) ([]learning.StudentCourseSummary, error) {
	return nil, nil
}

func (r *countingLearningRepository) ListStudentResumeCandidates(context.Context, string) ([]learning.ResumeCandidate, error) {
	return nil, nil
}

func (r *countingLearningRepository) EnrollmentID(context.Context, string, string) (string, error) {
	return "", learning.ErrEnrollmentNotFound
}

func (r *countingLearningRepository) EnrollmentForLesson(context.Context, string, string) (learning.Enrollment, error) {
	r.enrollmentCalls++
	return learning.Enrollment{}, learning.ErrEnrollmentNotFound
}

func (r *countingLearningRepository) LessonVideoVersion(context.Context, string) (string, error) {
	r.videoCalls++
	return "", learning.ErrEnrollmentNotFound
}

func (r *countingLearningRepository) SaveProgressGuarded(context.Context, learning.ProgressWrite, learning.ProgressMutationGuard) error {
	r.progressCalls++
	return learning.ErrProgressUnavailable
}

// CreateReportGuarded runs the guard the way the real repository does — first, inside the write —
// so a route test observes the authorization decision rather than a double that skips it.
func (r *countingLearningRepository) CreateReportGuarded(ctx context.Context, _ learning.VerifiedReportBinding, _ learning.ReportContent, _ learning.ReportClock, guard learning.ReportMutationGuard) (learning.Report, error) {
	r.reportCalls++
	if guard != nil {
		if err := guard(ctx, nil); err != nil {
			return learning.Report{}, err
		}
	}
	return learning.Report{}, learning.ErrReportTargetUnavailable
}

type countingLearningMedia struct{ durationCalls, playbackCalls int }

func (m *countingLearningMedia) IssuePlayback(context.Context, media.PlaybackRequest) (media.PlaybackAuthorization, error) {
	m.playbackCalls++
	return media.PlaybackAuthorization{}, media.ErrProtectedUnavailable
}

func (m *countingLearningMedia) TrustedVideoDuration(context.Context, string, string) (time.Duration, error) {
	m.durationCalls++
	return 0, media.ErrProtectedUnavailable
}

func (m *countingLearningMedia) MaterialKinds(context.Context, []string) (map[string][]media.Material, error) {
	return map[string][]media.Material{}, nil
}

type recordingLearningAuthenticator struct {
	calls int
	err   error
}

func (a *recordingLearningAuthenticator) UserFromRequest(c *gin.Context) (string, error) {
	a.calls++
	if a.err != nil {
		return "", a.err
	}
	c.Set("authenticated_session", identity.Session{ID: "test-session-user-1", AccountID: "user-1", State: identity.SessionActive})
	return "user-1", nil
}

type recordingLearningPrincipals struct {
	calls     int
	principal identity.Principal
}

func (r *recordingLearningPrincipals) ResolvePrincipal(context.Context, string) (identity.Principal, error) {
	r.calls++
	return r.principal, nil
}

func TestProgressSourceDenialRunsAfterBasicGateAndBeforeProtectedLearningBoundaries(t *testing.T) {
	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV": "development", "REDIS_ADDR": "localhost:6379", "S3_ENDPOINT": "http://localhost:9000", "S3_BUCKET": "gradex-test",
	}), config.MapSecretResolver{"DATABASE_URL": "postgres://x", "S3_ACCESS_KEY": "a", "S3_SECRET_KEY": "b", "PLAYBACK_TOKEN_SECRET": "c"})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	repository := &countingLearningRepository{}
	mediaBoundary := &countingLearningMedia{}
	evaluator := &recordingLearningEvaluator{decision: entitlement.Decision{Allowed: true, Reason: entitlement.ReasonAllowed}}
	limiter, _ := sourceDeniedLearningLimiter(t)
	foundation, err := NewLearningFoundation(LearningFoundationOptions{Repository: repository, Evaluator: evaluator, Media: mediaBoundary, ReportContexts: testReportContextIssuer(t), Limiter: limiter, Policies: testLearningPolicies()})
	if err != nil {
		t.Fatalf("learning foundation: %v", err)
	}
	authenticator := &recordingLearningAuthenticator{}
	principals := &recordingLearningPrincipals{principal: identity.Principal{AccountID: "user-1", Role: identity.RoleStudent, Status: identity.StatusActive, CredentialState: identity.CredentialActive}}
	router, err := NewRouter(cfg, logging.New(&syncBuffer{}, "gradex-api-test", "development", logging.LevelFromString("info")), health.New(time.Second), authenticator, principals, WithLearningFoundation(foundation))
	if err != nil {
		t.Fatalf("router: %v", err)
	}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/learn/lessons/11111111-1111-1111-1111-111111111111/progress", strings.NewReader(`{"position_seconds":5,"asset_version_id":"22222222-2222-2222-2222-222222222222"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests || authenticator.calls != 1 || principals.calls != 1 {
		t.Fatalf("source denial = status %d auth=%d principals=%d", response.Code, authenticator.calls, principals.calls)
	}
	if evaluator.studentID != "" || repository.enrollmentCalls != 0 || repository.videoCalls != 0 || repository.progressCalls != 0 || mediaBoundary.durationCalls != 0 || mediaBoundary.playbackCalls != 0 {
		t.Fatalf("source denial reached protected boundary: evaluator=%+v repository=%+v media=%+v", evaluator, repository, mediaBoundary)
	}
}

func TestUnauthenticatedProgressRequestDoesNotSpendSourceCapacity(t *testing.T) {
	student := identity.Principal{AccountID: "user-1", Role: identity.RoleStudent, Status: identity.StatusActive, CredentialState: identity.CredentialActive}
	limiter, store := sourceDeniedLearningLimiter(t)
	router, _ := learningRouterWithLimiter(t, student, errors.New("no session"), fixedLearningEvaluator{}, limiter)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/v1/learn/lessons/11111111-1111-1111-1111-111111111111/progress", nil))
	if response.Code != http.StatusNotFound || store.calls != 0 {
		t.Fatalf("unauthenticated progress = status %d limiter calls=%d", response.Code, store.calls)
	}
}

// All externally distinguishable protected-learning failures must travel
// through writeProtectedUnavailable. This includes failures before the
// evaluator, evaluator denials, and a missing authoritative graph row.
func TestLearningDenialsAreByteIdenticalAndTypedInternally(t *testing.T) {
	student := identity.Principal{AccountID: "user-1", Role: identity.RoleStudent, Status: identity.StatusActive, CredentialState: identity.CredentialActive}
	instructor := identity.Principal{AccountID: "user-1", Role: identity.RoleInstructor, Status: identity.StatusActive, CredentialState: identity.CredentialActive}
	suspended := identity.Principal{AccountID: "user-1", Role: identity.RoleStudent, Status: identity.StatusSuspended, CredentialState: identity.CredentialActive}
	tests := []struct {
		name       string
		principal  identity.Principal
		authErr    error
		decision   entitlement.Decision
		wantReason string
	}{
		{"authentication absent", student, errors.New("no session"), entitlement.Decision{}, string(identity.DenyPrincipalNotFound)},
		{"instructor", instructor, nil, entitlement.Decision{Allowed: true, Reason: entitlement.ReasonAllowed}, string(identity.DenyRoleLacksCapability)},
		{"suspended student", suspended, nil, entitlement.Decision{Allowed: true, Reason: entitlement.ReasonAllowed}, string(identity.DenyAccountSuspended)},
		{"expired entitlement", student, nil, entitlement.Decision{Reason: entitlement.ReasonExpired}, string(entitlement.ReasonExpired)},
		{"revoked or out of scope entitlement", student, nil, entitlement.Decision{Reason: entitlement.ReasonNoApplicableGrant}, string(entitlement.ReasonNoApplicableGrant)},
		{"emergency suspended course", student, nil, entitlement.Decision{Reason: entitlement.ReasonCourseSuspended}, string(entitlement.ReasonCourseSuspended)},
		{"retired ineligible entitlement", student, nil, entitlement.Decision{Reason: entitlement.ReasonRetired}, string(entitlement.ReasonRetired)},
		{"missing enrollment or lesson graph", student, nil, entitlement.Decision{Allowed: true, Reason: entitlement.ReasonAllowed}, string(entitlement.ReasonDependency)},
	}

	var baseline *httptest.ResponseRecorder
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router, logs := learningRouterUnderTest(t, tc.principal, tc.authErr, fixedLearningEvaluator{decision: tc.decision})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/learn/lessons/11111111-1111-1111-1111-111111111111/playback", nil))
			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want uniform 404: %s", response.Code, response.Body.String())
			}
			if baseline == nil {
				baseline = response
			} else if response.Code != baseline.Code || response.Body.String() != baseline.Body.String() || !reflect.DeepEqual(response.Header(), baseline.Header()) {
				t.Fatalf("protected denial differs from baseline:\nstatus %d/%d\nheaders %#v/%#v\nbody %q/%q", response.Code, baseline.Code, response.Header(), baseline.Header(), response.Body.String(), baseline.Body.String())
			}
			assertDenyLogged(t, logs, tc.wantReason)
		})
	}
}

func TestEveryLearningRouteRefusesNonStudentAccounts(t *testing.T) {
	instructor := identity.Principal{AccountID: "user-1", Role: identity.RoleInstructor, Status: identity.StatusActive, CredentialState: identity.CredentialActive}
	router, logs := learningRouterUnderTest(t, instructor, nil, fixedLearningEvaluator{decision: entitlement.Decision{Allowed: true, Reason: entitlement.ReasonAllowed}})
	var baseline *httptest.ResponseRecorder
	for _, route := range router.Routes() {
		if !strings.HasPrefix(route.Path, "/api/v1/learn/lessons/:lessonId") {
			continue
		}
		path := "/api/v1/learn/lessons/11111111-1111-1111-1111-111111111111" + route.Path[len("/api/v1/learn/lessons/:lessonId"):]
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(route.Method, path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s %s status = %d, want uniform 404", route.Method, route.Path, response.Code)
		}
		if baseline == nil {
			baseline = response
		} else if response.Body.String() != baseline.Body.String() || !reflect.DeepEqual(response.Header(), baseline.Header()) {
			t.Fatalf("%s %s has a distinguishable role denial", route.Method, route.Path)
		}
	}
	assertDenyLogged(t, logs, string(identity.DenyRoleLacksCapability))
}

func TestLearningAuthorizationPassesAuthoritativeEvaluatorInputs(t *testing.T) {
	student := identity.Principal{AccountID: "user-1", Role: identity.RoleStudent, Status: identity.StatusActive, CredentialState: identity.CredentialActive}
	evaluator := &recordingLearningEvaluator{decision: entitlement.Decision{Reason: entitlement.ReasonExpired}}
	router, _ := learningRouterUnderTest(t, student, nil, evaluator)
	lessonID := "11111111-1111-1111-1111-111111111111"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/learn/lessons/"+lessonID+"/playback", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("evaluator denial status = %d, want 404", response.Code)
	}
	if evaluator.studentID != "user-1" || evaluator.lessonID != lessonID || evaluator.now.IsZero() || evaluator.now.Location() != time.UTC {
		t.Fatalf("evaluator inputs = student=%q lesson=%q now=%v", evaluator.studentID, evaluator.lessonID, evaluator.now)
	}
}
