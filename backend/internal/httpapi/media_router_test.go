package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/media"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

type mediaRouterStore struct {
	mu           sync.Mutex
	presignCalls int
}

func (s *mediaRouterStore) PresignPutURL(context.Context, string, string, time.Duration) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.presignCalls++
	return "", errors.New("handler reached test storage")
}

func (*mediaRouterStore) HeadObjectVersion(context.Context, string, string) (int64, bool, error) {
	return 0, false, errors.New("handler reached test storage")
}

func (*mediaRouterStore) DownloadPrefixVersion(context.Context, string, string, int64) ([]byte, error) {
	return nil, errors.New("handler reached test storage")
}

func (*mediaRouterStore) HashObjectVersion(context.Context, string, string) (string, error) {
	return "", errors.New("handler reached test storage")
}

func (s *mediaRouterStore) presignCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.presignCalls
}

func mediaRouterUnderTest(t *testing.T, principal identity.Principal) (*gin.Engine, *mediaRouterStore) {
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
		t.Fatalf("creating lazy media pool: %v", err)
	}
	t.Cleanup(pool.Close)
	writer, err := outbox.NewWriter("media-router-test", []byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("creating media outbox writer: %v", err)
	}
	unavailable, err := media.NewUnavailableScanner("test scanner is intentionally unavailable")
	if err != nil {
		t.Fatalf("creating unavailable scanner: %v", err)
	}
	scanner, err := media.NewScannerAdapter(unavailable)
	if err != nil {
		t.Fatalf("creating scanner adapter: %v", err)
	}
	store := &mediaRouterStore{}
	service, err := media.NewService(media.ServiceOptions{
		DB: pool, Store: store, Outbox: writer, Scanner: scanner,
		UploadURLExpiry: time.Minute, MaxUploadBytes: 1024,
	})
	if err != nil {
		t.Fatalf("creating media service: %v", err)
	}
	foundation, err := NewMediaFoundation(MediaFoundationOptions{Service: service})
	if err != nil {
		t.Fatalf("creating media foundation: %v", err)
	}
	logger := logging.New(&syncBuffer{}, "gradex-api-test", "development", logging.LevelFromString("info"))
	reporter := health.New(time.Second)
	reporter.MarkStarted()
	router, err := NewRouter(
		cfg, logger, reporter, fakeAuth{}, fixedPrincipals{principal: principal},
		WithMediaFoundation(foundation),
	)
	if err != nil {
		t.Fatalf("building production media router: %v", err)
	}
	return router, store
}

func TestD7ProductionMediaRoutesRequireCapabilitiesBeforeHandlers(t *testing.T) {
	restrictedInstructor := identity.Principal{
		AccountID: "11111111-1111-1111-1111-111111111111", Role: identity.RoleInstructor,
		Status: identity.StatusActive, CredentialState: identity.CredentialChangeRequired,
	}
	router, store := mediaRouterUnderTest(t, restrictedInstructor)

	mounted := make(map[string]bool)
	for _, route := range router.Routes() {
		mounted[route.Method+" "+route.Path] = true
		if route.Path == "/api/v1/lessons/:lessonID/video/upload-url" || route.Path == "/api/v1/lessons/:lessonID/video/complete" ||
			route.Path == "/api/v1/lessons/:lessonID/video/retry" || route.Path == "/api/v1/lessons/:lessonID/video/publish" ||
			route.Path == "/api/v1/lessons/:lessonID/video/playback-url" || route.Path == "/api/v1/videos/:videoID/manifest/*filepath" {
			t.Fatalf("retired legacy media route is mounted in production router: %s %s", route.Method, route.Path)
		}
	}
	for _, route := range []string{
		"POST /api/v1/media/uploads",
		"POST /api/v1/media/uploads/:id/completions",
		"GET /api/v1/media/assets/:id",
		"POST /api/v1/media/assets/:id/retries",
	} {
		if !mounted[route] {
			t.Fatalf("production router is missing D7 media route %q", route)
		}
	}

	upload := httptest.NewRequest(http.MethodPost, "/api/v1/media/uploads", strings.NewReader(`{"course_id":"11111111-1111-1111-1111-111111111111","kind":"VIDEO","content_type":"video/mp4","size_bytes":1}`))
	upload.Header.Set("Content-Type", "application/json")
	uploadResponse := httptest.NewRecorder()
	router.ServeHTTP(uploadResponse, upload)
	if uploadResponse.Code != http.StatusForbidden {
		t.Fatalf("restricted Instructor upload status=%d, want capability denial 403: %s", uploadResponse.Code, uploadResponse.Body.String())
	}
	if store.presignCallCount() != 0 {
		t.Fatal("upload handler reached storage before the content-management capability decision")
	}

	restrictedAdmin := identity.Principal{
		AccountID: "22222222-2222-2222-2222-222222222222", Role: identity.RoleAdmin,
		Status: identity.StatusActive, CredentialState: identity.CredentialChangeRequired,
	}
	retryRouter, retryStore := mediaRouterUnderTest(t, restrictedAdmin)
	retry := httptest.NewRequest(http.MethodPost, "/api/v1/media/assets/33333333-3333-3333-3333-333333333333/retries", nil)
	retryResponse := httptest.NewRecorder()
	retryRouter.ServeHTTP(retryResponse, retry)
	if retryResponse.Code != http.StatusForbidden {
		t.Fatalf("restricted Admin retry status=%d, want capability denial 403: %s", retryResponse.Code, retryResponse.Body.String())
	}
	if retryStore.presignCallCount() != 0 {
		t.Fatal("retry handler reached media storage before the admin capability decision")
	}
}
