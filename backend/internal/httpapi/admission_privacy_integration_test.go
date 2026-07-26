//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

const (
	httpAdmissionAdminDSN = "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable"
	httpAdmissionDB       = "gradex_http_admission_test"
	httpAdmissionDSN      = "postgres://gradex:gradex@localhost:5432/" + httpAdmissionDB + "?sslmode=disable"
)

func freshHTTPAdmissionPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	admin, err := pgxpool.New(ctx, httpAdmissionAdminDSN)
	if err != nil {
		t.Fatalf("opening admin pool: %v", err)
	}
	defer admin.Close()
	_, _ = admin.Exec(ctx,
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`,
		httpAdmissionDB,
	)
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+httpAdmissionDB); err != nil {
		t.Fatalf("dropping admission database: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+httpAdmissionDB); err != nil {
		t.Fatalf("creating admission database: %v", err)
	}
	migrator, err := migrate.New("file://../db/migrations", httpAdmissionDSN)
	if err != nil {
		t.Fatalf("opening migrations: %v", err)
	}
	t.Cleanup(func() { _, _ = migrator.Close() })
	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrating admission database: %v", err)
	}
	pool, err := pgxpool.New(ctx, httpAdmissionDSN)
	if err != nil {
		t.Fatalf("opening admission pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func realAdmissionRouter(
	t *testing.T,
	pool *pgxpool.Pool,
	rateStore admissionRateStore,
) *gin.Engine {
	t.Helper()
	english, arabic := identityPolicySets()
	policies, err := identity.NewStaticPolicySetResolver(english, arabic)
	if err != nil {
		t.Fatalf("constructing policies: %v", err)
	}
	compromised, err := identity.NewDeterministicCompromisedSource()
	if err != nil {
		t.Fatalf("constructing password source: %v", err)
	}
	writer, err := outbox.NewWriter("test-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("constructing outbox writer: %v", err)
	}
	randomness := make([]byte, 0, 32*16)
	for offset := byte(0); offset < 16; offset++ {
		randomness = append(randomness, bytes.Repeat([]byte{0x71 + offset}, 32)...)
	}
	service, err := identity.NewAdmissionService(identity.AdmissionServiceOptions{
		Pool: pool, Policies: policies, Compromised: compromised, Outbox: writer,
		VerificationTTL: time.Hour, Now: time.Now,
		Random: bytes.NewReader(randomness),
	})
	if err != nil {
		t.Fatalf("constructing admission service: %v", err)
	}
	limiter, err := ratelimit.New(
		rateStore, bytes.Repeat([]byte{0x31}, 32), time.Second,
	)
	if err != nil {
		t.Fatalf("constructing limiter: %v", err)
	}
	endpointPolicies := make(map[string]ratelimit.Policy)
	for _, endpoint := range []string{
		"student-registrations", "email-verification-requests", "email-verifications",
	} {
		endpointPolicies[endpoint] = ratelimit.DevelopmentAdmissionPolicy(endpoint)
	}
	readPolicy := ratelimit.DevelopmentPolicySetReadPolicy()
	endpointPolicies[readPolicy.Endpoint] = readPolicy
	bootstrapPolicy := ratelimit.DevelopmentAnonymousBootstrapPolicy()
	endpointPolicies[bootstrapPolicy.Endpoint] = bootstrapPolicy
	foundation, err := NewAdmissionFoundation(AdmissionFoundationOptions{
		PublicOrigin: "https://gradex.example", CookieSigningKey: bytes.Repeat([]byte("a"), 32),
		CSRFKey: bytes.Repeat([]byte("b"), 32), AnonymousSessionTTL: time.Hour,
		Policies: policies, Service: service,
		Limiter: limiter, EndpointPolicies: endpointPolicies,
	})
	if err != nil {
		t.Fatalf("constructing foundation: %v", err)
	}
	router := gin.New()
	router.Use(requestIDMiddleware())
	mountAdmissionRoutes(router.Group("/api/v1"), foundation)
	return router
}

type privacyResponse struct {
	status       int
	body         string
	contentType  string
	cacheControl string
	setCookie    string
	location     string
	duration     time.Duration
}

func postAdmission(
	router *gin.Engine,
	path string,
	body string,
	cookie *http.Cookie,
	csrf string,
) privacyResponse {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://gradex.example")
	request.Header.Set(csrfHeaderName, csrf)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	started := time.Now()
	router.ServeHTTP(response, request)
	return privacyResponse{
		status: response.Code, body: response.Body.String(),
		contentType:  response.Header().Get("Content-Type"),
		cacheControl: response.Header().Get("Cache-Control"),
		setCookie:    response.Header().Get("Set-Cookie"),
		location:     response.Header().Get("Location"),
		duration:     time.Since(started),
	}
}

func assertPrivacyEquivalent(t *testing.T, first, second privacyResponse) {
	t.Helper()
	first.duration = 0
	second.duration = 0
	if first != second {
		t.Fatalf("hidden outcomes differ:\nfirst  %+v\nsecond %+v", first, second)
	}
}

func assertSameTimingClass(t *testing.T, first, second time.Duration) {
	t.Helper()
	faster, slower := first, second
	if faster > slower {
		faster, slower = slower, faster
	}
	difference := slower - faster
	if difference > 150*time.Millisecond ||
		slower > 3*faster+20*time.Millisecond {
		t.Fatalf("hidden outcomes left timing class: %s versus %s", first, second)
	}
}

// BR-001/003: real PostgreSQL eligible and hidden no-op paths expose identical
// HTTP status, bytes, headers, and cookie behavior.
func TestAdmissionHiddenOutcomesAreHTTPEquivalent(t *testing.T) {
	pool := freshHTTPAdmissionPool(t)
	router := realAdmissionRouter(t, pool, admissionRateStore{allowed: true})
	cookie, csrf := bootstrapAdmissionBrowser(t, router)
	registration := `{"display_name":"Nora Ahmed","email":"Student@Example.com",` +
		`"password":"correct horse battery staple","locale":"en",` +
		`"policy_set_id":"registration-v1"}`
	first := postAdmission(
		router, "/api/v1/student-registrations", registration, cookie, csrf,
	)
	duplicate := postAdmission(
		router,
		"/api/v1/student-registrations",
		strings.Replace(registration, "Student@Example.com", "student@example.com", 1),
		cookie,
		csrf,
	)
	assertPrivacyEquivalent(t, first, duplicate)
	assertSameTimingClass(t, first.duration, duplicate.duration)
	if first.status != http.StatusAccepted || first.cacheControl != "no-store" {
		t.Fatalf("registration acknowledgment = %+v", first)
	}

	eligible := postAdmission(
		router,
		"/api/v1/email-verification-requests",
		`{"email":"student@example.com"}`,
		cookie,
		csrf,
	)
	unknown := postAdmission(
		router,
		"/api/v1/email-verification-requests",
		`{"email":"unknown@example.com"}`,
		cookie,
		csrf,
	)
	assertPrivacyEquivalent(t, eligible, unknown)
	assertSameTimingClass(t, eligible.duration, unknown.duration)
}

func assertResponseExcludesCanaries(
	t *testing.T,
	response privacyResponse,
	canaries ...string,
) {
	t.Helper()
	for _, canary := range canaries {
		if strings.Contains(response.body, canary) {
			t.Errorf("response body exposed request canary %q", canary)
		}
	}
}

func TestAdmissionResponsesExcludeRequestCanaries(t *testing.T) {
	pool := freshHTTPAdmissionPool(t)
	router := realAdmissionRouter(t, pool, admissionRateStore{allowed: true})
	cookie, csrf := bootstrapAdmissionBrowser(t, router)
	const (
		email    = "transport-canary@example.com"
		password = "Transport canary password 908172"
	)
	registration := postAdmission(
		router,
		"/api/v1/student-registrations",
		`{"display_name":"Transport Canary","email":"`+email+`",`+
			`"password":"`+password+`","locale":"en",`+
			`"policy_set_id":"registration-v1"}`,
		cookie,
		csrf,
	)
	assertResponseExcludesCanaries(t, registration, email, password)

	duplicate := postAdmission(
		router,
		"/api/v1/student-registrations",
		`{"display_name":"Duplicate Canary","email":"`+email+`",`+
			`"password":"`+password+`","locale":"en",`+
			`"policy_set_id":"registration-v1"}`,
		cookie,
		csrf,
	)
	assertResponseExcludesCanaries(t, duplicate, email, password)

	resend := postAdmission(
		router,
		"/api/v1/email-verification-requests",
		`{"email":"`+email+`"}`,
		cookie,
		csrf,
	)
	assertResponseExcludesCanaries(t, resend, email)

	const invalidBearer = "INVALID_BEARER_CANARY_012345678901234567890"
	invalid := postAdmission(
		router,
		"/api/v1/email-verifications",
		`{"token":"`+invalidBearer+`"}`,
		cookie,
		csrf,
	)
	assertResponseExcludesCanaries(t, invalid, invalidBearer)

	deniedRouter := realAdmissionRouter(t, pool, admissionRateStore{allowed: false})
	denied := postAdmission(
		deniedRouter,
		"/api/v1/email-verification-requests",
		`{"email":"limiter-canary@example.com"}`,
		cookie,
		csrf,
	)
	if denied.status != http.StatusTooManyRequests {
		t.Fatalf("limiter canary status = %d, want 429", denied.status)
	}
	assertResponseExcludesCanaries(t, denied, "limiter-canary@example.com")
}
