//go:build integration

package main

import (
	"context"
	"errors"
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
	"github.com/Owlah2025/gradex/backend/internal/video"
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

type fakeVideoService struct{}

func (f fakeVideoService) RequestUpload(context.Context, string, string, string) (video.UploadTicket, error) {
	return video.UploadTicket{}, nil
}
func (f fakeVideoService) CompleteUpload(context.Context, string) error { return nil }
func (f fakeVideoService) Retranscode(context.Context, string) error    { return nil }
func (f fakeVideoService) Publish(context.Context, string) error        { return nil }
func (f fakeVideoService) GetPlaybackURL(context.Context, string, string) (video.SignedURL, error) {
	return video.SignedURL{}, nil
}
func (f fakeVideoService) UpdateProgress(context.Context, string, string, float64) (video.Progress, error) {
	return video.Progress{}, nil
}
func (f fakeVideoService) ServeManifest(context.Context, string, string, string) ([]byte, string, error) {
	return nil, "", nil
}

func TestProductionRouterWiringHasNoMissingSurfaces(t *testing.T) {
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

	logger := logging.New(&syncBuffer{}, "gradex-api-test", "development", logging.LevelFromString("info"))
	reporter := health.New(time.Second)

	authenticator := auth.NewFakeAuthenticator()
	entitlements := auth.NewFakeEntitlementChecker(pool)
	principals := identity.NewDBPrincipalResolver(pool)

	r, err := httpapi.NewRouter(cfg, logger, reporter, fakeVideoService{}, authenticator, entitlements, principals, pf.Options...)
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
}

type syncBuffer struct{}

func (b *syncBuffer) Write(p []byte) (n int, err error) {
	return len(p), nil
}
