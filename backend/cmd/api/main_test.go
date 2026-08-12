//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/httpapi"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/queue"
	"github.com/Owlah2025/gradex/backend/internal/storage"
)

const (
	apiAdminDSN   = "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable"
	apiTestDBName = "gradex_api_wiring_test"
	apiTestDSN    = "postgres://gradex:gradex@localhost:5432/" + apiTestDBName + "?sslmode=disable"
	apiSourceURL  = "file://../../internal/db/migrations"
	apiOpTimeout  = 30 * time.Second
)

func freshAPISchema(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), apiOpTimeout)
	defer cancel()

	admin, err := pgxpool.New(ctx, apiAdminDSN)
	if err != nil {
		t.Fatalf("connecting to admin db: %v", err)
	}
	defer admin.Close()

	_, _ = admin.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, apiTestDBName)
	_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+apiTestDBName)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+apiTestDBName); err != nil {
		t.Fatalf("creating test db: %v", err)
	}

	m, err := migrate.New(apiSourceURL, apiTestDSN)
	if err != nil {
		t.Fatalf("creating migrator: %v", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrating up: %v", err)
	}
}

func apiPool(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), apiOpTimeout)
	t.Cleanup(cancel)

	p, err := pgxpool.New(ctx, apiTestDSN)
	if err != nil {
		t.Fatalf("opening pool: %v", err)
	}
	t.Cleanup(p.Close)
	return p, ctx
}

func TestBuildLearningFoundationRejectsMissingMedia(t *testing.T) {
	if _, _, err := buildLearningFoundation(nil, nil, nil, nil); err == nil {
		t.Fatal("production learning composition accepted a missing media foundation")
	}
}

func TestProductionRegistrationFoundationsStartWithHIBPAndApprovedPolicySet(t *testing.T) {
	freshAPISchema(t)
	pool, _ := apiPool(t)
	cfg := productionAdmissionConfig(t)
	redisConnection, err := queue.NewConnection(cfg.Redis())
	if err != nil {
		t.Fatalf("building production Redis configuration: %v", err)
	}

	foundations, err := buildProductionFoundations(cfg, pool, redisConnection)
	if err != nil {
		t.Fatalf("production registration composition did not start: %v", err)
	}
	defer foundations.Close()
	if foundations.AdmissionRedis == nil {
		t.Fatal("production Student admission foundation was not composed")
	}
	if foundations.StaffRedis == nil {
		t.Fatal("production staff invitation foundation was not composed")
	}
}

// TestDevelopmentStaffLifecycleMountsWithoutStudentRegistration is the
// regression proof for the founder-acceptance defect: staff composition used to
// be gated on cfg.Admission().Enabled(), so the intended posture — sessions on,
// STUDENT_REGISTRATION_ENABLED=false — dropped the whole Admin staff surface and
// POST /api/v1/staff-invitations answered 404/unmatched. The two admissions are
// independent, and this test holds them apart in both directions: staff routes
// mounted, Student admission routes still absent.
func TestDevelopmentStaffLifecycleMountsWithoutStudentRegistration(t *testing.T) {
	freshAPISchema(t)
	pool, _ := apiPool(t)

	// STUDENT_REGISTRATION_ENABLED is deliberately absent: it defaults to false,
	// which is exactly the founder environment that produced the 404.
	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV": "development", "REDIS_ADDR": "localhost:6379",
		"PUBLIC_ORIGIN": "https://gradex.example",
		"S3_ENDPOINT":   "http://localhost:9000", "S3_BUCKET": "gradex-test",
		"PASSWORD_SCREEN_MODE":                 "deterministic",
		"OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION": "key-v1",
	}), config.MapSecretResolver{
		"DATABASE_URL":                 apiTestDSN,
		"S3_ACCESS_KEY":                "a",
		"S3_SECRET_KEY":                "b",
		"PLAYBACK_TOKEN_SECRET":        "c",
		"OUTBOX_PROTECTED_PAYLOAD_KEY": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"SESSION_CSRF_KEY":             "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB",
		"ANONYMOUS_COOKIE_SIGNING_KEY": "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC",
		"ANONYMOUS_CSRF_KEY":           "DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD",
		"ADMISSION_LIMITER_HMAC_KEY":   "EEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEE",
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.Admission().Enabled() {
		t.Fatalf("Student admission is enabled; this test must run with it disabled: %s", cfg.Admission().Reason())
	}
	if !cfg.Sessions().Enabled() {
		t.Fatal("sessions are disabled; staff mutations require the session boundary")
	}

	redisConnection, err := queue.NewConnection(cfg.Redis())
	if err != nil {
		t.Fatalf("Redis connection configuration: %v", err)
	}

	pf, err := buildProductionFoundations(cfg, pool, redisConnection)
	if err != nil {
		t.Fatalf("buildProductionFoundations: %v", err)
	}
	defer pf.Close()
	if pf.StaffRedis == nil {
		t.Fatal("staff lifecycle foundation was not composed while Student registration is disabled")
	}
	if pf.AdmissionRedis != nil {
		t.Fatal("Student admission foundation was composed while STUDENT_REGISTRATION_ENABLED is false")
	}

	logger := logging.New(&syncBuffer{}, "gradex-api-test", "development", logging.LevelFromString("info"))
	r, err := httpapi.NewRouter(
		cfg, logger, health.New(time.Second), auth.NewFakeAuthenticator(),
		identity.NewDBPrincipalResolver(pool), pf.Options...,
	)
	if err != nil {
		t.Fatalf("httpapi.NewRouter: %v", err)
	}

	mounted := make(map[string]bool)
	for _, route := range r.Routes() {
		mounted[route.Method+" "+route.Path] = true
	}

	requiredStaffRoutes := []string{
		"GET /api/v1/staff-invitations",
		"GET /api/v1/staff-invitations/instructors",
		"POST /api/v1/staff-invitations",
		"DELETE /api/v1/staff-invitations/:id",
		"GET /api/v1/staff-invitations/preview",
		"POST /api/v1/staff-invitation-completions",
	}
	for _, route := range requiredStaffRoutes {
		if !mounted[route] {
			t.Fatalf("staff route %q is not mounted while Student registration is disabled", route)
		}
	}

	// The fix must not re-open Student admission as a side effect.
	forbiddenAdmissionRoutes := []string{
		"POST /api/v1/student-registrations",
		"POST /api/v1/email-verification-requests",
		"POST /api/v1/email-verifications",
		"POST /api/v1/password-reset-requests",
		"POST /api/v1/password-resets",
		"GET /api/v1/registration-policy-set",
	}
	for _, route := range forbiddenAdmissionRoutes {
		if mounted[route] {
			t.Fatalf("Student admission route %q was mounted while registration is disabled", route)
		}
	}

	// The Admin surface is composed, not exposed: the staff mutation boundary
	// still refuses a foreign origin, a bad CSRF token, and an anonymous caller.
	validToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x41}, 32))
	securityCases := []struct {
		name       string
		origin     string
		csrf       string
		wantStatus int
		wantCode   string
	}{
		{name: "missing origin", csrf: validToken, wantStatus: http.StatusForbidden, wantCode: "ORIGIN_NOT_ALLOWED"},
		{
			name: "foreign origin", origin: "https://attacker.example", csrf: validToken,
			wantStatus: http.StatusForbidden, wantCode: "ORIGIN_NOT_ALLOWED",
		},
		{
			name: "missing CSRF", origin: "https://gradex.example",
			wantStatus: http.StatusForbidden, wantCode: "CSRF_FAILED",
		},
		{
			name: "invalid CSRF", origin: "https://gradex.example", csrf: "malformed",
			wantStatus: http.StatusForbidden, wantCode: "CSRF_FAILED",
		},
		{
			name: "anonymous", origin: "https://gradex.example", csrf: validToken,
			wantStatus: http.StatusUnauthorized, wantCode: "AUTHENTICATION_REQUIRED",
		},
	}

	for _, mutation := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/staff-invitations"},
		{http.MethodDelete, "/api/v1/staff-invitations/inv-99"},
	} {
		for _, securityCase := range securityCases {
			t.Run(mutation.method+" "+mutation.path+"/"+securityCase.name, func(t *testing.T) {
				request := httptest.NewRequest(
					mutation.method, mutation.path,
					strings.NewReader(`{"email":"staff@example.com","role":"INSTRUCTOR"}`),
				)
				request.Header.Set("Content-Type", "application/json")
				request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: validToken})
				if securityCase.origin != "" {
					request.Header.Set("Origin", securityCase.origin)
				}
				if securityCase.csrf != "" {
					request.Header.Set("X-CSRF-Token", securityCase.csrf)
				}

				response := httptest.NewRecorder()
				r.ServeHTTP(response, request)

				var body struct {
					Code string `json:"code"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatalf("decoding Problem Details response: %v", err)
				}
				if response.Code == http.StatusNotFound {
					t.Fatalf("staff mutation route is unmatched: %s", response.Body.String())
				}
				if response.Code != securityCase.wantStatus || body.Code != securityCase.wantCode {
					t.Fatalf(
						"staff mutation security response = %d/%s, want %d/%s: %s",
						response.Code, body.Code, securityCase.wantStatus,
						securityCase.wantCode, response.Body.String(),
					)
				}
			})
		}
	}
}

func TestProductionStaffCompositionFailsClosedOnNamedPrerequisites(t *testing.T) {
	cfg := productionAdmissionConfig(t)
	for _, testCase := range []struct {
		name  string
		pool  *pgxpool.Pool
		redis *queue.Connection
		want  string
	}{
		{name: "postgres", want: "PostgreSQL staff invitation storage"},
		{name: "redis", pool: &pgxpool.Pool{}, want: "Redis rate limiting"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validateStaffComposition(cfg, testCase.pool, testCase.redis); err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("staff prerequisite error = %v, want %q", err, testCase.want)
			}
		})
	}
}

// TestDevelopmentStaffCompositionFailsClosedOnScreeningMode keeps the widened
// gate fail closed. Enabled sessions already force ADMISSION_LIMITER_HMAC_KEY
// and the anonymous keys, and every composition needs the protected outbox key,
// so PASSWORD_SCREEN_MODE is the one staff dependency a
// STUDENT_REGISTRATION_ENABLED=false environment can still be missing. It must
// stop startup — staff completion sets a password and screening it is not
// optional — rather than dropping the Admin surface back to 404.
func TestDevelopmentStaffCompositionFailsClosedOnScreeningMode(t *testing.T) {
	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV": "development", "REDIS_ADDR": "localhost:6379",
		"PUBLIC_ORIGIN": "https://gradex.example",
		"S3_ENDPOINT":   "http://localhost:9000", "S3_BUCKET": "gradex-test",
		"OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION": "key-v1",
	}), config.MapSecretResolver{
		"DATABASE_URL":                 apiTestDSN,
		"S3_ACCESS_KEY":                "a",
		"S3_SECRET_KEY":                "b",
		"PLAYBACK_TOKEN_SECRET":        "c",
		"OUTBOX_PROTECTED_PAYLOAD_KEY": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"SESSION_CSRF_KEY":             "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB",
		"ANONYMOUS_COOKIE_SIGNING_KEY": "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC",
		"ANONYMOUS_CSRF_KEY":           "DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD",
		"ADMISSION_LIMITER_HMAC_KEY":   "EEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEE",
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	if _, _, err := buildStaffFoundation(cfg, nil, nil); err == nil {
		t.Fatal("staff composition accepted an unavailable PASSWORD_SCREEN_MODE")
	} else if !strings.Contains(err.Error(), "compromised-password") {
		t.Fatalf("staff composition error = %q, want a compromised-password screening error", err)
	}
}

// TestDevelopmentStaffCompositionRequiresSessions proves the one gate that was
// kept. Staff mutations carry the session and CSRF boundary, and
// httpapi.NewRouter refuses to build a staff surface without a session
// foundation, so composing one with sessions off would stop the API from
// starting at all.
func TestDevelopmentStaffCompositionRequiresSessions(t *testing.T) {
	freshAPISchema(t)
	pool, _ := apiPool(t)

	// No SESSION_CSRF_KEY: development permits it, and sessions are then off.
	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV": "development", "REDIS_ADDR": "localhost:6379",
		"PUBLIC_ORIGIN": "https://gradex.example",
		"S3_ENDPOINT":   "http://localhost:9000", "S3_BUCKET": "gradex-test",
		"PASSWORD_SCREEN_MODE":                 "deterministic",
		"OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION": "key-v1",
	}), config.MapSecretResolver{
		"DATABASE_URL":                 apiTestDSN,
		"S3_ACCESS_KEY":                "a",
		"S3_SECRET_KEY":                "b",
		"PLAYBACK_TOKEN_SECRET":        "c",
		"OUTBOX_PROTECTED_PAYLOAD_KEY": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.Sessions().Enabled() {
		t.Fatal("sessions are enabled; this test must run with them disabled")
	}

	redisConnection, err := queue.NewConnection(cfg.Redis())
	if err != nil {
		t.Fatalf("Redis connection configuration: %v", err)
	}
	pf, err := buildProductionFoundations(cfg, pool, redisConnection)
	if err != nil {
		t.Fatalf("buildProductionFoundations: %v", err)
	}
	defer pf.Close()
	if pf.StaffRedis != nil {
		t.Fatal("staff foundation was composed without the session boundary it requires")
	}
}

func TestProductionRouterWiringAndMutationSecurity(t *testing.T) {
	freshAPISchema(t)
	pool, _ := apiPool(t)

	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV": "development", "REDIS_ADDR": "localhost:6379",
		"PUBLIC_ORIGIN": "https://gradex.example",
		"S3_ENDPOINT":   "http://localhost:9000", "S3_BUCKET": "gradex-test",
		"STUDENT_REGISTRATION_ENABLED":         "true",
		"REGISTRATION_POLICY_SET_ID":           "dev-set-1",
		"PASSWORD_SCREEN_MODE":                 "deterministic",
		"OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION": "key-v1",
	}), config.MapSecretResolver{
		"DATABASE_URL":                 apiTestDSN,
		"S3_ACCESS_KEY":                "a",
		"S3_SECRET_KEY":                "b",
		"PLAYBACK_TOKEN_SECRET":        "c",
		"OUTBOX_PROTECTED_PAYLOAD_KEY": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"SESSION_CSRF_KEY":             "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB",
		"ANONYMOUS_COOKIE_SIGNING_KEY": "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC",
		"ANONYMOUS_CSRF_KEY":           "DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD",
		"ADMISSION_LIMITER_HMAC_KEY":   "EEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEE",
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	redisConnection, err := queue.NewConnection(cfg.Redis())
	if err != nil {
		t.Fatalf("Redis connection configuration: %v", err)
	}

	pf, err := buildProductionFoundations(cfg, pool, redisConnection)
	if err != nil {
		t.Fatalf("buildProductionFoundations: %v", err)
	}
	defer pf.Close()
	storageClient, err := storage.New(context.Background(), storage.Options{
		Endpoint: cfg.S3Endpoint(), AccessKey: cfg.S3AccessKey().Expose(), SecretKey: cfg.S3SecretKey().Expose(),
		Bucket: cfg.S3Bucket(), Region: cfg.S3Region(), UsePathStyle: cfg.S3UsePathStyle(),
	})
	if err != nil {
		t.Fatalf("building test storage client: %v", err)
	}
	mediaFoundation, err := buildMediaFoundation(cfg, pool, storageClient)
	if err != nil {
		t.Fatalf("building test media foundation: %v", err)
	}
	learningFoundation, learningRedis, err := buildLearningFoundation(cfg, pool, mediaFoundation, redisConnection)
	if err != nil {
		t.Fatalf("building real learning foundation: %v", err)
	}
	defer learningRedis.Close()

	logger := logging.New(&syncBuffer{}, "gradex-api-test", "development", logging.LevelFromString("info"))
	reporter := health.New(time.Second)

	authenticator := auth.NewFakeAuthenticator()
	principals := identity.NewDBPrincipalResolver(pool)

	routerOptions := append([]httpapi.RouterOption(nil), pf.Options...)
	routerOptions = append(routerOptions, httpapi.WithMediaFoundation(mediaFoundation))
	routerOptions = append(routerOptions, httpapi.WithLearningFoundation(learningFoundation))
	r, err := httpapi.NewRouter(cfg, logger, reporter, authenticator, principals, routerOptions...)
	if err != nil {
		t.Fatalf("httpapi.NewRouter: %v", err)
	}

	// Enumerate all mounted routes
	routes := r.Routes()
	t.Logf("Enumerated %d mounted production routes:", len(routes))

	requiredSurfaces := map[string]string{
		"Sessions":          "/api/v1/sessions",
		"StudentAdmission":  "/api/v1/student-registrations",
		"Staff":             "/api/v1/staff",
		"CatalogAuthoring":  "/api/v1/courses",
		"ProtectedLearning": "/api/v1/learn/lessons/:lessonId/progress",
		"AdminReview":       "/api/v1/admin/review",
	}
	requiredLearningRoutes := []string{
		"POST /api/v1/learn/lessons/:lessonId/playback",
		"PUT /api/v1/learn/lessons/:lessonId/progress",
	}

	requiredD5Routes := []struct {
		method string
		path   string
	}{
		{method: "GET", path: "/api/v1/courses/:id"},
		{method: "PUT", path: "/api/v1/courses/:id/candidate"},
		{method: "PATCH", path: "/api/v1/courses/:id/revisions/:revisionId"},
		{method: "POST", path: "/api/v1/courses/:id/revisions/:revisionId/sections"},
		{method: "PATCH", path: "/api/v1/courses/:id/revisions/:revisionId/sections/:sectionId"},
		{method: "DELETE", path: "/api/v1/courses/:id/revisions/:revisionId/sections/:sectionId"},
		{method: "POST", path: "/api/v1/courses/:id/revisions/:revisionId/sections/:sectionId/lessons"},
		{method: "PATCH", path: "/api/v1/courses/:id/revisions/:revisionId/lessons/:lessonId"},
		{method: "DELETE", path: "/api/v1/courses/:id/revisions/:revisionId/lessons/:lessonId"},
		{method: "PUT", path: "/api/v1/courses/:id/revisions/:revisionId/lessons/:lessonId/video"},
		{method: "PUT", path: "/api/v1/courses/:id/revisions/:revisionId/lessons/:lessonId/files"},
		{method: "DELETE", path: "/api/v1/courses/:id/revisions/:revisionId/lessons/:lessonId/files"},
		{method: "PUT", path: "/api/v1/courses/:id/revisions/:revisionId/preview"},
		{method: "DELETE", path: "/api/v1/courses/:id/revisions/:revisionId/preview"},
		{method: "POST", path: "/api/v1/courses/:id/revisions/:revisionId/submit"},
		{method: "GET", path: "/api/v1/admin/review/queue"},
		{method: "GET", path: "/api/v1/admin/review/courses/:id/revisions/:revisionId"},
		{method: "POST", path: "/api/v1/admin/review/courses/:id/revisions/:revisionId/approve"},
		{method: "POST", path: "/api/v1/admin/review/courses/:id/revisions/:revisionId/request-changes"},
		{method: "POST", path: "/api/v1/admin/review/courses/:id/revisions/:revisionId/preview/:lessonId"},
	}

	surfaceMounted := make(map[string]bool)
	d5Mounted := make(map[string]bool)

	for _, route := range routes {
		t.Logf("  %-6s %s", route.Method, route.Path)
		for surfaceName, pathPrefix := range requiredSurfaces {
			if route.Path == pathPrefix || (len(route.Path) > len(pathPrefix) && route.Path[:len(pathPrefix)] == pathPrefix) {
				surfaceMounted[surfaceName] = true
			}
		}
		for _, req := range requiredD5Routes {
			if route.Method == req.method && route.Path == req.path {
				d5Mounted[req.method+" "+req.path] = true
			}
		}
	}

	for surfaceName, pathPrefix := range requiredSurfaces {
		if !surfaceMounted[surfaceName] {
			t.Fatalf("CRITICAL MISCONFIGURATION: Production router built by cmd/api is missing surface '%s' (%s)", surfaceName, pathPrefix)
		}
	}
	for _, required := range requiredLearningRoutes {
		methodPath := strings.SplitN(required, " ", 2)
		var found bool
		for _, route := range routes {
			if route.Method == methodPath[0] && route.Path == methodPath[1] {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("production router built by cmd/api is missing learning route %q", required)
		}
	}

	for _, req := range requiredD5Routes {
		key := req.method + " " + req.path
		if !d5Mounted[key] {
			t.Fatalf("CRITICAL MISCONFIGURATION: Production router built by cmd/api is missing D5 route '%s'", key)
		}
	}

	requiredD7Routes := []string{
		"POST /api/v1/media/uploads",
		"POST /api/v1/media/uploads/:id/completions",
		"GET /api/v1/media/assets/:id",
		"POST /api/v1/media/assets/:id/retries",
	}
	requiredD8Routes := []string{
		"POST /api/v1/media/playback-authorizations",
		"POST /api/v1/media/download-authorizations",
		"GET /api/v1/media/previews/:id",
	}
	mounted := make(map[string]bool)
	for _, route := range routes {
		mounted[route.Method+" "+route.Path] = true
		if strings.HasPrefix(route.Path, "/api/v1/lessons/") || strings.HasPrefix(route.Path, "/api/v1/videos/") {
			t.Fatalf("legacy video route remains in production composition: %s %s", route.Method, route.Path)
		}
	}
	for _, route := range requiredD7Routes {
		if !mounted[route] {
			t.Fatalf("production router is missing D7 route %q", route)
		}
	}
	for _, route := range requiredD8Routes {
		if !mounted[route] {
			t.Fatalf("production router is missing D8 route %q", route)
		}
	}

	validToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x41}, 32))
	securityCases := []struct {
		name       string
		origin     string
		csrf       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "missing origin",
			csrf:       validToken,
			wantStatus: http.StatusForbidden,
			wantCode:   "ORIGIN_NOT_ALLOWED",
		},
		{
			name:       "foreign origin",
			origin:     "https://attacker.example",
			csrf:       validToken,
			wantStatus: http.StatusForbidden,
			wantCode:   "ORIGIN_NOT_ALLOWED",
		},
		{
			name:       "missing CSRF",
			origin:     "https://gradex.example",
			wantStatus: http.StatusForbidden,
			wantCode:   "CSRF_FAILED",
		},
		{
			name:       "invalid CSRF",
			origin:     "https://gradex.example",
			csrf:       "malformed",
			wantStatus: http.StatusForbidden,
			wantCode:   "CSRF_FAILED",
		},
		{
			name:       "anonymous",
			origin:     "https://gradex.example",
			csrf:       validToken,
			wantStatus: http.StatusUnauthorized,
			wantCode:   "AUTHENTICATION_REQUIRED",
		},
	}

	for _, route := range requiredD5Routes {
		if route.method == http.MethodGet {
			continue
		}
		for _, securityCase := range securityCases {
			t.Run(route.method+" "+route.path+"/"+securityCase.name, func(t *testing.T) {
				request := httptest.NewRequest(route.method, route.path, nil)
				request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: validToken})
				if securityCase.origin != "" {
					request.Header.Set("Origin", securityCase.origin)
				}
				if securityCase.csrf != "" {
					request.Header.Set("X-CSRF-Token", securityCase.csrf)
				}

				response := httptest.NewRecorder()
				r.ServeHTTP(response, request)

				var body struct {
					Code string `json:"code"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatalf("decoding Problem Details response: %v", err)
				}
				if response.Code != securityCase.wantStatus || body.Code != securityCase.wantCode {
					t.Fatalf(
						"mutation security response = %d/%s, want %d/%s: %s",
						response.Code, body.Code, securityCase.wantStatus,
						securityCase.wantCode, response.Body.String(),
					)
				}
			})
		}
	}
}

type syncBuffer struct{}

func (b *syncBuffer) Write(p []byte) (n int, err error) {
	return len(p), nil
}
