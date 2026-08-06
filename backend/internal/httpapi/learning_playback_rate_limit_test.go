//go:build !production

package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/logging"

	"github.com/Owlah2025/gradex/backend/internal/entitlement"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

// H-1 remediation evidence: FR-017 and BR-102 require playback issuance to be
// rate-limited per Student *and* per source address. Tier 3 review found the
// playback half unenforced — issuePlayback went straight to authorization — while
// the Progress and report mutations were already throttled. These tests are the
// executable evidence the finding asked for; they assert the control, its ordering,
// its fail-closed behaviour, and that it did not weaken authorization.
//
// The route is POST /api/v1/learn/lessons/:lessonId/playback.

const playbackRateLesson = "30000000-0000-0000-0000-000000000001"

func playbackRequest() *http.Request {
	return httptest.NewRequest(http.MethodPost, "/api/v1/learn/lessons/"+playbackRateLesson+"/playback", nil)
}

// TestPlaybackIssuanceAsksForBothCeilingsBeforeAuthorizing pins the ordering the
// review required: source address first, then Student, and only then authorization.
// Ordering is the security property — deciding after authorization would let a
// throttled caller learn entitlement state, and deciding after issuance would hand
// out a signed target the ceiling was meant to withhold.
func TestPlaybackIssuanceAsksForBothCeilingsBeforeAuthorizing(t *testing.T) {
	store := &spyReportRateStore{allow: true}
	parts := learningThrottleRouter(t, activeStudent(), nil, store)

	parts.router.ServeHTTP(httptest.NewRecorder(), playbackRequest())

	if got := store.decisions(); got != 2 {
		t.Fatalf("playback asked for %d rate decisions, want 2 (source then Student)", got)
	}
	keys := store.observedKeys()
	if len(keys) != 2 {
		t.Fatalf("observed %d limiter keys, want 2", len(keys))
	}
	// Distinct keys prove the two buckets cannot collide for one request.
	if keys[0] == keys[1] {
		t.Fatalf("source and Student ceilings shared one key %q; the buckets must be independent", keys[0])
	}
}

// TestPlaybackSourceCeilingDeniesBeforeAuthorizationOrIssuance proves a source
// denial is the exact contract — 429, no-store, a usable Retry-After — and that
// nothing downstream ran.
func TestPlaybackSourceCeilingDeniesBeforeAuthorizationOrIssuance(t *testing.T) {
	store := &spyReportRateStore{allow: false}
	parts := learningThrottleRouter(t, activeStudent(), nil, store)

	response := httptest.NewRecorder()
	parts.router.ServeHTTP(response, playbackRequest())

	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("exhausted source ceiling = %d, want 429", response.Code)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	retryAfter := response.Header().Get("Retry-After")
	seconds, err := strconv.Atoi(retryAfter)
	if err != nil || seconds <= 0 {
		t.Fatalf("Retry-After = %q, want a positive integer number of seconds", retryAfter)
	}
	// The source ceiling is decided first, so exactly one decision is spent and the
	// Student bucket is never touched.
	if got := store.decisions(); got != 1 {
		t.Fatalf("a source denial spent %d decisions, want 1 — the Student quota must not be charged", got)
	}
	if parts.evaluator.studentID != "" {
		t.Fatal("a throttled request reached authorization; the ceiling must decide first")
	}
	if parts.repository.videoCalls != 0 {
		t.Fatal("a throttled request resolved a media version")
	}
}

// TestPlaybackStudentCeilingDeniesAfterSourceAllows covers the second dimension:
// the source address is fine, the Student's own quota is spent.
func TestPlaybackStudentCeilingDeniesAfterSourceAllows(t *testing.T) {
	store := &scriptedRateStore{answers: []bool{true, false}}
	parts := learningPlaybackRouter(t, store)

	response := httptest.NewRecorder()
	parts.router.ServeHTTP(response, playbackRequest())

	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("exhausted Student quota = %d, want 429", response.Code)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if seconds, err := strconv.Atoi(response.Header().Get("Retry-After")); err != nil || seconds <= 0 {
		t.Fatal("a Student-quota denial must carry a positive Retry-After")
	}
	if store.calls != 2 {
		t.Fatalf("expected both ceilings consulted, got %d decisions", store.calls)
	}
	if parts.evaluator.studentID != "" {
		t.Fatal("a Student-throttled request reached authorization")
	}
	if parts.repository.videoCalls != 0 {
		t.Fatal("a Student-throttled request resolved a media version")
	}
}

// TestPlaybackCeilingDependencyFailureFailsClosed is the property the review named
// explicitly: an undecidable limiter must never yield a successful issuance. It
// returns the uniform protected refusal, carries no Retry-After (nothing is known
// about when to retry), and discloses no internal limiter error.
func TestPlaybackCeilingDependencyFailureFailsClosed(t *testing.T) {
	for name, answers := range map[string][]bool{
		"source ceiling undecidable":  nil,    // first call fails
		"Student ceiling undecidable": {true}, // source allows, second fails
	} {
		t.Run(name, func(t *testing.T) {
			store := &scriptedRateStore{answers: answers, failAfterScript: true}
			parts := learningPlaybackRouter(t, store)

			response := httptest.NewRecorder()
			parts.router.ServeHTTP(response, playbackRequest())

			if response.Code == http.StatusOK {
				t.Fatal("an undecidable playback ceiling issued playback; it must fail closed")
			}
			if response.Code == http.StatusTooManyRequests {
				t.Fatalf("dependency failure answered 429; a quota denial and an outage are different states")
			}
			if got := response.Header().Get("Retry-After"); got != "" {
				t.Fatalf("an undecidable ceiling advertised Retry-After %q", got)
			}
			if parts.repository.videoCalls != 0 || parts.evaluator.studentID != "" {
				t.Fatal("an undecidable ceiling still reached authorization or media resolution")
			}
			body := response.Body.String()
			for _, leak := range []string{"redis", "limiter", "rate", "quota", "entitlement", "asset", "version"} {
				if strings.Contains(strings.ToLower(body), leak) {
					t.Fatalf("protected refusal leaked %q: %s", leak, body)
				}
			}
		})
	}
}

// TestPlaybackBucketsAreKeyedIndependently proves the two ceilings cannot merge and
// that the Student bucket is scoped to the Student alone.
//
// Cross-Student isolation is asserted at the key function rather than through two
// routers: the authentication double in this package resolves every request to one
// account, so two routers would compare identical identities and prove nothing. The
// key is what decides isolation, so that is what is asserted.
func TestPlaybackBucketsAreKeyedIndependently(t *testing.T) {
	store := &spyReportRateStore{allow: true}
	parts := learningThrottleRouter(t, activeStudent(), nil, store)
	parts.router.ServeHTTP(httptest.NewRecorder(), playbackRequest())

	keys := store.observedKeys()
	if len(keys) != 2 {
		t.Fatalf("observed %d keys, want 2 (source then Student)", len(keys))
	}
	if keys[0] == keys[1] {
		t.Fatal("the source and Student playback buckets shared one key")
	}

	// Distinct Students get distinct buckets...
	if playbackRateIdentifier("student-a") == playbackRateIdentifier("student-b") {
		t.Fatal("two Students resolved to one playback bucket")
	}
	// ...and the key is the Student alone, so Lesson-hopping cannot open a fresh quota
	// per Lesson, which is the extraction pattern R-04 sized this limit against.
	if got := playbackRateIdentifier("student-a"); got != "student-a" {
		t.Fatalf("playback identifier = %q, want the bare Student id", got)
	}
	// The progress identifier mixes in the Lesson; the playback one must not, or the
	// two controls would answer different questions under the same name.
	if playbackRateIdentifier("student-a") == progressRateIdentifier("student-a", playbackRateLesson) {
		t.Fatal("the playback bucket collided with a Progress bucket")
	}
}

// TestPlaybackPoliciesCarryTheAuthoritativeQuotas checks the numbers themselves, so
// a later edit that silently loosens FR-017 fails here rather than in production.
func TestPlaybackPoliciesCarryTheAuthoritativeQuotas(t *testing.T) {
	student := ratelimit.ProtectedLearningPlaybackPolicy()
	if student.Window.Minutes() != 10 {
		t.Fatalf("Student playback window = %v, want 10 minutes (R-04)", student.Window)
	}
	if len(student.Rules) != 1 || student.Rules[0].Dimension != ratelimit.DimensionIdentifier {
		t.Fatal("the Student playback policy must be keyed on the authenticated identifier alone")
	}
	if student.Rules[0].Limit != 30 || student.Rules[0].LocalLimit != 30 {
		t.Fatalf("Student playback limit = %d/%d, want 30/30 (FR-017, R-04)",
			student.Rules[0].Limit, student.Rules[0].LocalLimit)
	}

	source := ratelimit.ProtectedLearningPlaybackSourcePolicy()
	if source.Window.Minutes() != 10 {
		t.Fatalf("source playback window = %v, want 10 minutes", source.Window)
	}
	if len(source.Rules) != 1 || source.Rules[0].Dimension != ratelimit.DimensionSourceAddr {
		t.Fatal("the source playback policy must be keyed on the source address alone")
	}
	if source.Rules[0].Limit != 600 || source.Rules[0].LocalLimit != 600 {
		t.Fatalf("source playback limit = %d/%d, want 600/600 (D-071)",
			source.Rules[0].Limit, source.Rules[0].LocalLimit)
	}
	// The source ceiling must be strictly larger than a single Student's quota, or one
	// ordinary Student behind a shared campus NAT could exhaust the whole address.
	if source.Rules[0].Limit <= student.Rules[0].Limit {
		t.Fatal("the source ceiling must exceed one Student's quota")
	}
	if student.Endpoint == source.Endpoint || student.ID == source.ID {
		t.Fatal("the two playback policies must be distinct endpoints so their buckets cannot merge")
	}
	if err := student.Validate(); err != nil {
		t.Fatalf("Student playback policy invalid: %v", err)
	}
	if err := source.Validate(); err != nil {
		t.Fatalf("source playback policy invalid: %v", err)
	}
}

// TestLearningFoundationRequiresBothPlaybackPolicies proves the wiring cannot be
// forgotten again: the omission Tier 3 found is now a startup failure.
func TestLearningFoundationRequiresBothPlaybackPolicies(t *testing.T) {
	for _, endpoint := range []string{"learning-playback", "learning-playback-source"} {
		policies := testLearningPolicies()
		delete(policies, endpoint)
		if _, err := NewLearningFoundation(LearningFoundationOptions{
			Repository: &countingLearningRepository{}, Evaluator: allowingReportEvaluator(),
			Media: unavailableLearningMedia{}, ReportContexts: testReportContextIssuer(t),
			Limiter: testLearningLimiter(t), Policies: policies,
		}); err == nil {
			t.Fatalf("a foundation missing %q was accepted; playback must never serve unthrottled", endpoint)
		}

		// An invalid policy is refused as firmly as a missing one.
		invalid := testLearningPolicies()
		broken := invalid[endpoint]
		broken.Rules = nil
		invalid[endpoint] = broken
		if _, err := NewLearningFoundation(LearningFoundationOptions{
			Repository: &countingLearningRepository{}, Evaluator: allowingReportEvaluator(),
			Media: unavailableLearningMedia{}, ReportContexts: testReportContextIssuer(t),
			Limiter: testLearningLimiter(t), Policies: invalid,
		}); err == nil {
			t.Fatalf("a foundation with an invalid %q policy was accepted", endpoint)
		}
	}
}

// scriptedRateStore answers a fixed sequence of allow/deny decisions and then, when
// failAfterScript is set, reports the backend as undecidable. It exists so a test can
// place a denial or an outage on the *second* ceiling specifically, which the shared
// spy store cannot express.
type scriptedRateStore struct {
	answers         []bool
	failAfterScript bool
	calls           int
	keys            []string
}

func (s *scriptedRateStore) Decide(_ context.Context, entries []ratelimit.Entry) (bool, error) {
	index := s.calls
	s.calls++
	for _, entry := range entries {
		s.keys = append(s.keys, entry.Key)
	}
	if index < len(s.answers) {
		return s.answers[index], nil
	}
	if s.failAfterScript {
		return false, errors.New("rate-limit backend unavailable")
	}
	return true, nil
}

// learningPlaybackRouter builds the learning router around a scripted store, mirroring
// learningThrottleRouter but with a limiter whose answers a test controls per call.
func learningPlaybackRouter(t *testing.T, store *scriptedRateStore) throttleRouterParts {
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
	limiter, err := ratelimit.New(store, []byte(strings.Repeat("p", 32)), time.Second)
	if err != nil {
		t.Fatalf("constructing playback limiter: %v", err)
	}
	repository := &countingLearningRepository{}
	evaluator := &recordingLearningEvaluator{decision: entitlement.Decision{Allowed: true, Reason: entitlement.ReasonAllowed}}
	foundation, err := NewLearningFoundation(LearningFoundationOptions{
		Repository: repository, Evaluator: evaluator, Media: unavailableLearningMedia{},
		ReportContexts: testReportContextIssuer(t), Limiter: limiter, Policies: testLearningPolicies(),
		Now: func() time.Time { return reportTestIssuance },
	})
	if err != nil {
		t.Fatalf("creating learning foundation: %v", err)
	}
	logs := &syncBuffer{}
	router, err := NewRouter(cfg, logging.New(logs, "gradex-api-test", "development", logging.LevelFromString("info")),
		health.New(time.Second), fakeAuth{}, fixedPrincipals{principal: activeStudent()},
		WithLearningFoundation(foundation))
	if err != nil {
		t.Fatalf("building playback router: %v", err)
	}
	return throttleRouterParts{router: router, repository: repository, evaluator: evaluator, logs: logs}
}
