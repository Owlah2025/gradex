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

	pf, err := buildProductionFoundations(cfg, pool)
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

	logger := logging.New(&syncBuffer{}, "gradex-api-test", "development", logging.LevelFromString("info"))
	reporter := health.New(time.Second)

	authenticator := auth.NewFakeAuthenticator()
	principals := identity.NewDBPrincipalResolver(pool)

	routerOptions := append([]httpapi.RouterOption(nil), pf.Options...)
	routerOptions = append(routerOptions, httpapi.WithMediaFoundation(mediaFoundation))
	r, err := httpapi.NewRouter(cfg, logger, reporter, authenticator, principals, routerOptions...)
	if err != nil {
		t.Fatalf("httpapi.NewRouter: %v", err)
	}

	// Enumerate all mounted routes
	routes := r.Routes()
	t.Logf("Enumerated %d mounted production routes:", len(routes))

	requiredSurfaces := map[string]string{
		"Sessions":         "/api/v1/sessions",
		"StudentAdmission": "/api/v1/student-registrations",
		"Staff":            "/api/v1/staff",
		"CatalogAuthoring": "/api/v1/courses",
		"AdminReview":      "/api/v1/admin/review",
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
