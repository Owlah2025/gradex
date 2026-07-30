package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPublicCatalogFoundationRefusesMissingRepository(t *testing.T) {
	if _, err := NewPublicCatalogFoundation(PublicCatalogFoundationOptions{}); err == nil {
		t.Fatal("NewPublicCatalogFoundation accepted a missing repository")
	}
	if err := WithPublicCatalogFoundation(nil)(&routerOptions{}); err == nil {
		t.Fatal("WithPublicCatalogFoundation accepted a missing foundation")
	}
}

func TestAnonymousProblemWriterOmitsRequestSpecificIdentifiers(t *testing.T) {
	r := gin.New()
	r.Use(requestIDMiddleware())
	r.GET("/anonymous", func(c *gin.Context) {
		writeAnonymousProblem(c, catalogpublic.NotFound().WithRequestID("must-not-reach-the-client"))
	})

	first := httptest.NewRecorder()
	second := httptest.NewRecorder()
	r.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/anonymous", nil))
	r.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/anonymous", nil))
	if !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
		t.Fatalf("anonymous problem bodies differ: %q != %q", first.Body.String(), second.Body.String())
	}
	if first.Header().Get("X-Request-ID") != "" || second.Header().Get("X-Request-ID") != "" {
		t.Fatal("anonymous problem exposes a request identifier header")
	}
	var body problem.Problem
	if err := json.Unmarshal(first.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding anonymous problem: %v", err)
	}
	if body.Status != http.StatusNotFound || body.Code != "NOT_FOUND" || body.RequestID != "" || body.Instance != "" {
		t.Errorf("anonymous problem shape = %#v", body)
	}
}

func TestPublicCatalogRoutesUseTheSharedVisibilityBoundary(t *testing.T) {
	r := publicCatalogRouter(t, fakeAuth{}, fakeEntitlements{allowed: true}, fixedPrincipals{})
	count := 0
	for _, route := range r.Routes() {
		if !strings.HasPrefix(route.Path, "/api/v1/catalog") {
			continue
		}
		count++
		if !strings.Contains(route.Handler, "(*publicCatalogHandlers).") {
			t.Errorf("public route %s %s bypasses public catalogue handlers (%s)", route.Method, route.Path, route.Handler)
		}
	}
	if count == 0 {
		t.Fatal("production router registered no public catalogue routes")
	}
}

func TestPublicCatalogCacheComposesVaryHeaders(t *testing.T) {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Header("Vary", "Origin")
	})
	router.Use(publicCatalogCache())
	router.GET("/catalog", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/catalog", nil))
	if got := response.Header().Get("Cache-Control"); got != publicCatalogCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, publicCatalogCacheControl)
	}
	if got := response.Header().Get("Vary"); got != "Origin, Accept-Language" {
		t.Fatalf("Vary = %q, want composed Origin and Accept-Language values", got)
	}
}

func TestPublicCatalogRouteExposureGuard(t *testing.T) {
	r := publicCatalogRouter(t, fakeAuth{}, fakeEntitlements{allowed: true}, fixedPrincipals{})
	for _, route := range r.Routes() {
		for _, prohibited := range []string{
			"order", "checkout", "cart", "coupon", "payment", "callback", "webhook", "refund", "invoice", "entitlement",
		} {
			if strings.Contains(strings.ToLower(route.Path+" "+route.Handler), prohibited) {
				t.Errorf("application route %s %s names prohibited commerce concept %q", route.Method, route.Path, prohibited)
			}
		}
		if !strings.HasPrefix(route.Path, "/api/v1/catalog") {
			continue
		}
		if route.Method != http.MethodGet && route.Method != http.MethodHead {
			t.Errorf("public route %s %s is not read-only", route.Method, route.Path)
		}
		for _, prohibited := range []string{
			"session", "credential", "capability", "enrollment", "invitation", "progress", "upload", "process",
		} {
			if strings.Contains(strings.ToLower(route.Path+" "+route.Handler), prohibited) {
				t.Errorf("public route %s %s names prohibited public-route concept %q", route.Method, route.Path, prohibited)
			}
		}
	}
}

func TestPublicCatalogRoutesDoNotReadAuthenticationBoundaries(t *testing.T) {
	tripwires := &publicCatalogRouteTripwires{}
	authenticator, err := auth.NewSessionAuthenticator(publicCatalogSessionTripwire{tripwires: tripwires})
	if err != nil {
		t.Fatalf("constructing session authenticator tripwire: %v", err)
	}
	r := publicCatalogRouter(t, authenticator, publicCatalogEntitlementTripwire{tripwires: tripwires}, publicCatalogPrincipalTripwire{tripwires: tripwires})

	for _, route := range r.Routes() {
		if !strings.HasPrefix(route.Path, "/api/v1/catalog") {
			continue
		}
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			req := httptest.NewRequest(route.Method, publicCatalogRequestPath(route.Path), nil)
			req.AddCookie(&http.Cookie{
				Name:  auth.SessionCookieName,
				Value: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x61}, 32)),
			})
			do(r, req)
			tripwires.assertUntouched(t)
		})
	}
}

func publicCatalogRequestPath(routePath string) string {
	for start := strings.IndexByte(routePath, ':'); start >= 0; start = strings.IndexByte(routePath, ':') {
		end := strings.IndexByte(routePath[start:], '/')
		if end < 0 {
			return routePath[:start] + "00000000-0000-0000-0000-000000000000"
		}
		routePath = routePath[:start] + "00000000-0000-0000-0000-000000000000" + routePath[start+end:]
	}
	return strings.ReplaceAll(routePath, "*filepath", "asset")
}

type publicCatalogRouteTripwires struct {
	authentication bool
	session        bool
	credential     bool
	principal      bool
	capability     bool
	entitlement    bool
}

// Resolve runs only after SessionAuthenticator accepted the valid test cookie,
// so one callback witnesses authentication, session resolution, and credential parsing.
type publicCatalogSessionTripwire struct{ tripwires *publicCatalogRouteTripwires }

func (t publicCatalogSessionTripwire) Resolve(context.Context, string, identity.CredentialUseKind, string) (identity.SessionView, error) {
	t.tripwires.authentication = true
	t.tripwires.session = true
	t.tripwires.credential = true
	return identity.SessionView{}, nil
}

type publicCatalogPrincipalTripwire struct{ tripwires *publicCatalogRouteTripwires }

func (t publicCatalogPrincipalTripwire) ResolvePrincipal(context.Context, string) (identity.Principal, error) {
	t.tripwires.principal = true
	t.tripwires.capability = true
	return identity.Principal{}, nil
}

type publicCatalogEntitlementTripwire struct{ tripwires *publicCatalogRouteTripwires }

func (t publicCatalogEntitlementTripwire) HasAccess(context.Context, string, string) (bool, error) {
	t.tripwires.entitlement = true
	return true, nil
}

func (t publicCatalogEntitlementTripwire) IsInstructorForLesson(context.Context, string, string) (bool, error) {
	t.tripwires.entitlement = true
	return true, nil
}

func (t *publicCatalogRouteTripwires) assertUntouched(tb testing.TB) {
	tb.Helper()
	if t.authentication || t.session || t.credential || t.principal || t.capability || t.entitlement {
		tb.Fatalf("public catalogue route invoked authentication=%t session=%t credential=%t principal=%t capability=%t entitlement=%t",
			t.authentication, t.session, t.credential, t.principal, t.capability, t.entitlement)
	}
}

func publicCatalogRouter(t *testing.T, authenticator auth.Authenticator, entitlements auth.EntitlementChecker, principals identity.PrincipalResolver) *gin.Engine {
	t.Helper()
	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV": "development", "REDIS_ADDR": "localhost:6379",
		"S3_ENDPOINT": "http://localhost:9000", "S3_BUCKET": "gradex-test",
	}), config.MapSecretResolver{
		"DATABASE_URL": "postgres://x", "S3_ACCESS_KEY": "a",
		"S3_SECRET_KEY": "b", "PLAYBACK_TOKEN_SECRET": "c",
	})
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}

	pool, err := pgxpool.New(context.Background(), "postgres://gradex:gradex@127.0.0.1:1/gradex?sslmode=disable")
	if err != nil {
		t.Fatalf("constructing lazy database pool: %v", err)
	}
	t.Cleanup(pool.Close)
	repository, err := catalogpublic.NewRepository(pool, catalogpublic.PublishedOnly)
	if err != nil {
		t.Fatalf("constructing public catalogue repository: %v", err)
	}
	foundation, err := NewPublicCatalogFoundation(PublicCatalogFoundationOptions{Repository: repository})
	if err != nil {
		t.Fatalf("constructing public catalogue foundation: %v", err)
	}

	reporter := health.New(time.Second)
	reporter.MarkStarted()
	r, err := NewRouter(
		cfg,
		logging.New(&syncBuffer{}, "gradex-api-test", "development", logging.LevelFromString("info")),
		reporter,
		fakeService{},
		authenticator,
		entitlements,
		principals,
		WithPublicCatalogFoundation(foundation),
	)
	if err != nil {
		t.Fatalf("constructing production router: %v", err)
	}
	return r
}
