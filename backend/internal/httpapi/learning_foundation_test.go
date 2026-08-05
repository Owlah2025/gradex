package httpapi

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/entitlement"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/learning"
	"github.com/Owlah2025/gradex/backend/internal/media"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

type allowingLearningRateStore struct{}

func (allowingLearningRateStore) Decide(context.Context, []ratelimit.Entry) (bool, error) {
	return true, nil
}

func testLearningLimiter(t *testing.T) *ratelimit.Limiter {
	t.Helper()
	limiter, err := ratelimit.New(allowingLearningRateStore{}, bytes.Repeat([]byte{0x51}, 32), time.Second)
	if err != nil {
		t.Fatalf("constructing learning limiter: %v", err)
	}
	return limiter
}

func testLearningPolicies() map[string]ratelimit.Policy {
	return map[string]ratelimit.Policy{
		"learning-progress-source": ratelimit.ProtectedLearningProgressSourcePolicy(),
		"learning-progress":        ratelimit.ProtectedLearningProgressPolicy(),
		"learning-report":          ratelimit.ProtectedLearningReportPolicy(),
	}
}

type learningFoundationEvaluator struct{}

func (learningFoundationEvaluator) Evaluate(context.Context, string, string, time.Time) entitlement.Decision {
	return entitlement.Decision{}
}

func (learningFoundationEvaluator) EvaluateInTransaction(context.Context, pgx.Tx, string, string, time.Time) entitlement.Decision {
	return entitlement.Decision{}
}

func (learningFoundationEvaluator) EvaluateRead(context.Context, string, string, time.Time) entitlement.ReadDecision {
	return entitlement.ReadDecision{State: entitlement.ReadDenied, Reason: entitlement.ReasonDependency}
}

func (learningFoundationEvaluator) EvaluateCourseReads(context.Context, string, time.Time) (map[string]entitlement.ReadDecision, error) {
	return map[string]entitlement.ReadDecision{}, nil
}

type learningFoundationMedia struct{}

func (learningFoundationMedia) IssuePlayback(context.Context, media.PlaybackRequest) (media.PlaybackAuthorization, error) {
	return media.PlaybackAuthorization{}, nil
}

func (learningFoundationMedia) TrustedVideoDuration(context.Context, string, string) (time.Duration, error) {
	return time.Second, nil
}

func (learningFoundationMedia) MaterialKinds(context.Context, []string) (map[string][]media.Material, error) {
	return map[string][]media.Material{}, nil
}

func TestLearningFoundationRequiresCompleteDependencies(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://gradex:gradex@127.0.0.1:1/gradex?sslmode=disable")
	if err != nil {
		t.Fatalf("constructing lazy pool: %v", err)
	}
	t.Cleanup(pool.Close)
	repository, err := learning.NewRepository(pool)
	if err != nil {
		t.Fatalf("constructing repository: %v", err)
	}
	complete := LearningFoundationOptions{
		Repository: repository, Evaluator: learningFoundationEvaluator{}, Media: learningFoundationMedia{},
		ReportContexts: testReportContextIssuer(t),
		Limiter:        testLearningLimiter(t), Policies: testLearningPolicies(),
	}
	var nilRepository *learning.Repository
	for _, options := range []LearningFoundationOptions{
		{},
		{Evaluator: complete.Evaluator, Media: complete.Media},
		{Repository: complete.Repository, Media: complete.Media},
		{Repository: complete.Repository, Evaluator: complete.Evaluator},
		{Repository: complete.Repository, Evaluator: complete.Evaluator, Media: complete.Media, Limiter: complete.Limiter},
		// A complete foundation except for the report-context issuer: protected learning must not
		// compose without one, because a read that cannot mint a context renders content the
		// Student is unable to report accurately (D-065).
		{Repository: complete.Repository, Evaluator: complete.Evaluator, Media: complete.Media,
			Limiter: complete.Limiter, Policies: testLearningPolicies()},
		{Repository: complete.Repository, Evaluator: complete.Evaluator, Media: complete.Media, Limiter: complete.Limiter,
			ReportContexts: complete.ReportContexts,
			Policies:       map[string]ratelimit.Policy{"learning-progress": ratelimit.ProtectedLearningProgressPolicy()}},
		{Repository: nilRepository, Evaluator: complete.Evaluator, Media: complete.Media, ReportContexts: complete.ReportContexts, Limiter: complete.Limiter, Policies: testLearningPolicies()},
	} {
		if _, err := NewLearningFoundation(options); err == nil {
			t.Fatal("learning foundation accepted a missing required dependency")
		}
	}
	if _, err := NewLearningFoundation(complete); err != nil {
		t.Fatalf("learning foundation rejected complete dependencies: %v", err)
	}
}

func TestLearningFoundationRouterOptionRejectsIncompleteFoundation(t *testing.T) {
	for _, foundation := range []*LearningFoundation{
		nil,
		{},
		{repository: &learning.Repository{}},
	} {
		if err := WithLearningFoundation(foundation)(&routerOptions{}); err == nil {
			t.Fatal("router option accepted an incomplete learning foundation")
		}
	}
}

// testReportContextIssuer is the deterministic encrypted issuer used wherever a test composes a
// learning foundation. Production derives its key from a configured secret; this one is fixed so
// tokens are reproducible, and it is never reachable from production composition.
func testReportContextIssuer(t *testing.T) LearningReportContextIssuer {
	t.Helper()
	issuer, err := learning.NewReportContextSigner(
		learning.DeriveReportContextKey([]byte("gradex-test-report-context-root-secret")),
		learning.DefaultReportContextLifetime,
		func() time.Time { return time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC) },
		func(b []byte) error {
			for i := range b {
				b[i] = byte(i + 7)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("constructing test report context issuer: %v", err)
	}
	return issuer
}

// The report context binds to the production session row, so the handler must refuse rather than
// invent one. These pin the extraction contract used by sessionFromContext.
func TestAuthenticatedSessionExtractionRefusesMissingOrMalformedValues(t *testing.T) {
	gin.SetMode(gin.TestMode)

	missing, _ := gin.CreateTestContext(httptest.NewRecorder())
	if _, ok := sessionFromContext(missing); ok {
		t.Fatal("no authenticated session must not resolve")
	}

	malformed, _ := gin.CreateTestContext(httptest.NewRecorder())
	malformed.Set("authenticated_session", "not-a-session")
	if _, ok := sessionFromContext(malformed); ok {
		t.Fatal("a malformed authenticated session must not resolve")
	}

	// An Account ID alone is not a session identity and must never be substituted for one.
	accountOnly, _ := gin.CreateTestContext(httptest.NewRecorder())
	accountOnly.Set(ctxUserIDKey, "account-1")
	if _, ok := sessionFromContext(accountOnly); ok {
		t.Fatal("an Account identity must not be read as a session identity")
	}

	present, _ := gin.CreateTestContext(httptest.NewRecorder())
	present.Set("authenticated_session", identity.Session{ID: "session-1", AccountID: "account-1", State: identity.SessionActive})
	session, ok := sessionFromContext(present)
	if !ok || session.ID != "session-1" {
		t.Fatalf("authenticated session = %+v ok=%t", session, ok)
	}
}

func TestReportContextIssuerConstructionFailsClosed(t *testing.T) {
	clock := func() time.Time { return time.Unix(1_800_000_000, 0).UTC() }
	nonce := func(b []byte) error { return nil }
	root := []byte("a-sufficiently-long-application-root-secret")

	if _, err := learning.NewReportContextSigner(learning.DeriveReportContextKey(root),
		learning.DefaultReportContextLifetime, clock, nonce); err != nil {
		t.Fatalf("valid configuration must construct an issuer: %v", err)
	}
	// A weak or absent root secret cannot produce a usable key length, and an invalid lifetime or
	// missing clock is refused outright — production composition cannot proceed without any of them.
	if _, err := learning.NewReportContextSigner(nil, learning.DefaultReportContextLifetime, clock, nonce); err == nil {
		t.Fatal("a missing key must fail")
	}
	if _, err := learning.NewReportContextSigner(learning.DeriveReportContextKey(root), 0, clock, nonce); err == nil {
		t.Fatal("an invalid lifetime must fail")
	}
	if _, err := learning.NewReportContextSigner(learning.DeriveReportContextKey(root),
		learning.DefaultReportContextLifetime, nil, nonce); err == nil {
		t.Fatal("a missing clock must fail")
	}
}
