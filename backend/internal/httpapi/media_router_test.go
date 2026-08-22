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

func (d *refusingDelivery) IssuePlaybackManifest(context.Context, string, string) (media.PlaybackManifest, error) {
	d.calls++
	return media.PlaybackManifest{}, media.ErrProtectedUnavailable
}

func (d *refusingDelivery) IssueDownload(context.Context, media.DownloadRequest) (media.DownloadAuthorization, error) {
	d.calls++
	return media.DownloadAuthorization{}, media.ErrProtectedUnavailable
}

func (d *refusingDelivery) IssueDownloadEntry(context.Context, media.DownloadEntryRequest) (media.DownloadAuthorization, error) {
	d.calls++
	return media.DownloadAuthorization{}, media.ErrProtectedUnavailable
}

func (d *refusingDelivery) IssueLessonFileDownload(context.Context, media.LessonFileDownloadRequest) (media.DownloadAuthorization, error) {
	d.calls++
	return media.DownloadAuthorization{}, media.ErrProtectedUnavailable
}

func (d *refusingDelivery) IssuePreview(context.Context, string) (media.PreviewAuthorization, error) {
	d.calls++
	return media.PreviewAuthorization{}, media.ErrProtectedUnavailable
}

func (d *refusingDelivery) IssueCoursePreview(context.Context, string) (media.PreviewAuthorization, error) {
	d.calls++
	return media.PreviewAuthorization{}, media.ErrProtectedUnavailable
}

type redirectingDelivery struct {
	refusingDelivery
	buyerTag string
}

func (*redirectingDelivery) IssueDownloadEntry(context.Context, media.DownloadEntryRequest) (media.DownloadAuthorization, error) {
	return media.DownloadAuthorization{URL: "https://storage.test/fresh-target"}, nil
}

func (d *redirectingDelivery) IssueLessonFileDownload(context.Context, media.LessonFileDownloadRequest) (media.DownloadAuthorization, error) {
	return media.DownloadAuthorization{URL: "https://storage.test/fresh-file-target", ExpiresAt: time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC), BuyerTag: d.buyerTag}, nil
}

type coursePreviewDelivery struct{ refusingDelivery }

func (*coursePreviewDelivery) IssueCoursePreview(context.Context, string) (media.PreviewAuthorization, error) {
	return media.PreviewAuthorization{
		URL:            "https://storage.test/preview?signature=bounded",
		AssetVersionID: "44444444-4444-4444-4444-444444444444",
		ExpiresAt:      time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC),
	}, nil
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

func TestCourseScopedPreviewResponseDoesNotSerializeTheAssetVersion(t *testing.T) {
	publicPrincipal := identity.Principal{Status: identity.StatusActive, CredentialState: identity.CredentialActive}
	router, _ := mediaRouterWithDeliveryUnderTest(t, publicPrincipal, &coursePreviewDelivery{})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet,
		"/api/v1/media/courses/11111111-1111-1111-1111-111111111111/preview", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("course preview status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"url":"https://storage.test/preview?signature=bounded"`) {
		t.Fatalf("course preview response omitted signed URL: %s", body)
	}
	if strings.Contains(body, "44444444-4444-4444-4444-444444444444") || strings.Contains(body, "asset_version_id") {
		t.Fatalf("course preview response leaked an internal asset version: %s", body)
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
		"GET /api/v1/media/playback-manifests/:playbackSession/index.m3u8",
		"POST /api/v1/media/download-authorizations",
		"POST /api/v1/media/courses/:courseId/lessons/:lessonId/materials/:materialId/download-authorizations",
		"GET /api/v1/media/lessons/:lessonId/materials/resource",
		"GET /api/v1/media/lessons/:lessonId/materials/lab-material",
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
		{"invalid-playback-session", http.MethodGet, "/api/v1/media/playback-manifests/invalid/index.m3u8", ""},
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

func TestLessonFileDownloadAuthorizationUsesAPrivateJSONContract(t *testing.T) {
	student := identity.Principal{AccountID: "user-1", Role: identity.RoleStudent, Status: identity.StatusActive, CredentialState: identity.CredentialActive}
	router, _ := mediaRouterWithDeliveryUnderTest(t, student, &redirectingDelivery{})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/media/courses/course-1/lessons/lesson-1/materials/file-1/download-authorizations", nil)
	request.Header.Set("Accept-Language", "en")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("response=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	for _, prohibited := range []string{"asset_version_id", "buyer_tag", "storage_object", "file-1", "course-1", "lesson-1"} {
		if strings.Contains(response.Body.String(), prohibited) {
			t.Fatalf("download authorization leaked %q: %s", prohibited, response.Body.String())
		}
	}
	if !strings.Contains(response.Body.String(), `"url":"https://storage.test/fresh-file-target"`) {
		t.Fatalf("download authorization omitted URL: %s", response.Body.String())
	}
}

func TestLessonFileDownloadAuthorizationSerializesOnlyAnOpaqueIssuedBuyerTag(t *testing.T) {
	student := identity.Principal{AccountID: "user-1", Role: identity.RoleStudent, Status: identity.StatusActive, CredentialState: identity.CredentialActive}
	router, _ := mediaRouterWithDeliveryUnderTest(t, student, &redirectingDelivery{buyerTag: "opaque-lab-marker"})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/media/courses/course-1/lessons/lesson-1/materials/file-1/download-authorizations", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("response=%d body=%s", response.Code, response.Body.String())
	}
	for _, prohibited := range []string{"user-1", "entitlement", "asset_version_id", "storage_object", "file-1", "course-1", "lesson-1"} {
		if strings.Contains(response.Body.String(), prohibited) {
			t.Fatalf("download authorization leaked %q: %s", prohibited, response.Body.String())
		}
	}
	if !strings.Contains(response.Body.String(), `"buyer_tag":"opaque-lab-marker"`) {
		t.Fatalf("download authorization omitted issued opaque buyer tag: %s", response.Body.String())
	}
}

func TestPublicCoursePreviewRouteIsAnonymousAndKeepsUnknownCoursesInventorySafe(t *testing.T) {
	delivery := &refusingDelivery{}
	router, _ := mediaRouterWithDeliveryUnderTest(t, identity.Principal{}, delivery)
	const path = "/api/v1/media/courses/00000000-0000-0000-0000-000000000001/preview"
	mounted := false
	for _, route := range router.Routes() {
		if route.Method+" "+route.Path == "GET /api/v1/media/courses/:courseID/preview" {
			mounted = true
			break
		}
	}
	if !mounted {
		t.Fatal("public Course preview route is not mounted")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != http.StatusNotFound || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Location") != "" {
		t.Fatalf("anonymous unknown Course preview response=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	if delivery.calls != 1 {
		t.Fatalf("delivery calls=%d, want one Course-scoped lookup", delivery.calls)
	}
}

func TestD064StableMaterialEntryRedirectIsNoStoreAndContainsNoBody(t *testing.T) {
	student := identity.Principal{
		AccountID: "user-1", Role: identity.RoleStudent, Status: identity.StatusActive, CredentialState: identity.CredentialActive,
	}
	router, _ := mediaRouterWithDeliveryUnderTest(t, student, &redirectingDelivery{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/media/lessons/lesson-1/materials/resource", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusFound {
		t.Fatalf("status=%d body=%q, want 302", response.Code, response.Body.String())
	}
	if response.Header().Get("Location") != "https://storage.test/fresh-target" {
		t.Fatalf("location=%q", response.Header().Get("Location"))
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("redirect headers=%v", response.Header())
	}
	if response.Body.Len() != 0 {
		t.Fatalf("redirect body leaked signed target: %q", response.Body.String())
	}
}

func TestD064StableMaterialDenialsRemainUniformWithoutRedirect(t *testing.T) {
	student := identity.Principal{
		AccountID: "user-1", Role: identity.RoleStudent, Status: identity.StatusActive, CredentialState: identity.CredentialActive,
	}
	delivery := &refusingDelivery{}
	router, _ := mediaRouterWithDeliveryUnderTest(t, student, delivery)
	paths := []string{
		"/api/v1/media/lessons/lesson-1/materials/resource",
		"/api/v1/media/lessons/lesson-1/materials/lab-material",
	}
	var baseline *httptest.ResponseRecorder
	for _, path := range paths {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound || response.Header().Get("Location") != "" || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("denial %s = status %d headers=%v", path, response.Code, response.Header())
		}
		if baseline == nil {
			baseline = response
		} else if response.Body.String() != baseline.Body.String() {
			t.Fatalf("denial bodies differ: %q vs %q", baseline.Body.String(), response.Body.String())
		}
	}
	if delivery.calls != len(paths) {
		t.Fatalf("delivery calls=%d, want %d", delivery.calls, len(paths))
	}
}
