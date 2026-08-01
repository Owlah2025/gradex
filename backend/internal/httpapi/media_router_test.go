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
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

type mediaRouterStore struct {
	mu           sync.Mutex
	presignCalls int
}

// refusingDelivery is a test double for the delivery service boundary, not a
// replacement router or handler. The real production router remains under
// test; each protected delivery failure must travel through its one refusal
// constructor.
type refusingDelivery struct{ calls int }

func (d *refusingDelivery) IssuePlayback(context.Context, media.PlaybackRequest) (media.PlaybackAuthorization, error) {
	d.calls++
	return media.PlaybackAuthorization{}, media.ErrProtectedUnavailable
}

func (d *refusingDelivery) IssueDownload(context.Context, media.DownloadRequest) (media.DownloadAuthorization, error) {
	d.calls++
	return media.DownloadAuthorization{}, media.ErrProtectedUnavailable
}

func (d *refusingDelivery) IssuePreview(context.Context, string) (media.PreviewAuthorization, error) {
	d.calls++
	return media.PreviewAuthorization{}, media.ErrProtectedUnavailable
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
	return mediaRouterWithDeliveryUnderTest(t, principal, nil)
}

func mediaRouterWithDeliveryUnderTest(t *testing.T, principal identity.Principal, delivery mediaDeliveryIssuer) (*gin.Engine, *mediaRouterStore) {
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
	foundation, err := NewMediaFoundation(MediaFoundationOptions{Service: service, Delivery: delivery})
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

func TestD8ProtectedDeliveryDenialsAreByteIdenticalOnTheProductionRouter(t *testing.T) {
	student := identity.Principal{
		AccountID: "user-1", Role: identity.RoleStudent, Status: identity.StatusActive, CredentialState: identity.CredentialActive,
	}
	delivery := &refusingDelivery{}
	router, _ := mediaRouterWithDeliveryUnderTest(t, student, delivery)

	for _, route := range []string{
		"POST /api/v1/media/playback-authorizations",
		"POST /api/v1/media/download-authorizations",
		"GET /api/v1/media/previews/:id",
	} {
		mounted := false
		for _, actual := range router.Routes() {
			if actual.Method+" "+actual.Path == route {
				mounted = true
				break
			}
		}
		if !mounted {
			t.Fatalf("production router is missing D8 delivery route %q", route)
		}
	}

	type requestCase struct {
		name   string
		method string
		path   string
		body   string
	}
	// The production delivery service turns every typed evaluator denial into
	// ErrProtectedUnavailable. These requests prove the live router maps the
	// resulting cases — including the absent target case — to fixed wire bytes.
	cases := []requestCase{
		{"non-existent", http.MethodPost, "/api/v1/media/playback-authorizations", `{"lesson_id":"00000000-0000-0000-0000-000000000001","asset_version_id":"00000000-0000-0000-0000-000000000001"}`},
		{"expired", http.MethodPost, "/api/v1/media/playback-authorizations", `{"lesson_id":"00000000-0000-0000-0000-000000000002","asset_version_id":"00000000-0000-0000-0000-000000000002"}`},
		{"revoked", http.MethodPost, "/api/v1/media/playback-authorizations", `{"lesson_id":"00000000-0000-0000-0000-000000000003","asset_version_id":"00000000-0000-0000-0000-000000000003"}`},
		{"out-of-scope", http.MethodPost, "/api/v1/media/playback-authorizations", `{"lesson_id":"00000000-0000-0000-0000-000000000004","asset_version_id":"00000000-0000-0000-0000-000000000004"}`},
		{"account-suspended", http.MethodPost, "/api/v1/media/playback-authorizations", `{"lesson_id":"00000000-0000-0000-0000-000000000005","asset_version_id":"00000000-0000-0000-0000-000000000005"}`},
		{"course-emergency-suspended", http.MethodPost, "/api/v1/media/playback-authorizations", `{"lesson_id":"00000000-0000-0000-0000-000000000006","asset_version_id":"00000000-0000-0000-0000-000000000006"}`},
		{"retired-ineligible", http.MethodPost, "/api/v1/media/playback-authorizations", `{"lesson_id":"00000000-0000-0000-0000-000000000007","asset_version_id":"00000000-0000-0000-0000-000000000007"}`},
	}
	var wantBody []byte
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status=%d body=%s, want uniform 404", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != problem.ContentType {
				t.Fatalf("content type=%q, want %q", got, problem.ContentType)
			}
			if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Location") != "" || rec.Header().Get("X-Request-ID") != "" {
				t.Fatalf("uniform denial headers=%v", rec.Header())
			}
			if wantBody == nil {
				wantBody = append([]byte(nil), rec.Body.Bytes()...)
			} else if string(wantBody) != rec.Body.String() {
				t.Fatalf("response body differs from non-existent denial\nwant=%s\n got=%s", wantBody, rec.Body.String())
			}
		})
	}
	if delivery.calls != len(cases) {
		t.Fatalf("delivery issuer calls=%d, want %d", delivery.calls, len(cases))
	}
}
