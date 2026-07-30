package httpapi

import (
	"context"
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

func TestPublicCatalogRoutesUseTheSharedVisibilityBoundary(t *testing.T) {
	r := publicCatalogRouter(t, fakeAuth{}, fixedPrincipals{})
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

func TestPublicCatalogRouteExposureGuard(t *testing.T) {
	r := publicCatalogRouter(t, fakeAuth{}, fixedPrincipals{})
	for _, route := range r.Routes() {
		if !strings.HasPrefix(route.Path, "/api/v1/catalog") {
			continue
		}
		if route.Method != http.MethodGet && route.Method != http.MethodHead {
			t.Errorf("public route %s %s is not read-only", route.Method, route.Path)
		}
		for _, prohibited := range []string{
			"session", "credential", "capability", "order", "checkout", "cart", "coupon", "payment",
			"callback", "webhook", "refund", "invoice", "entitlement", "enrollment", "invitation",
			"progress", "upload", "process",
		} {
			if strings.Contains(strings.ToLower(route.Path+" "+route.Handler), prohibited) {
				t.Errorf("public route %s %s names prohibited concept %q", route.Method, route.Path, prohibited)
			}
		}
	}
}

func TestPublicCatalogRoutesDoNotReadAuthenticationOrCapabilities(t *testing.T) {
	authCalled := false
	principalCalled := false
	r := publicCatalogRouter(t,
		publicCatalogAuthTripwire{called: &authCalled},
		publicCatalogPrincipalTripwire{called: &principalCalled},
	)
	do(r, httptest.NewRequest(http.MethodGet, "/api/v1/catalog/courses", nil))
	if authCalled {
		t.Error("public catalogue route read authentication")
	}
	if principalCalled {
		t.Error("public catalogue route read a capability principal")
	}
}

type publicCatalogAuthTripwire struct{ called *bool }

func (t publicCatalogAuthTripwire) UserFromRequest(*gin.Context) (string, error) {
	*t.called = true
	return "", nil
}

type publicCatalogPrincipalTripwire struct{ called *bool }

func (t publicCatalogPrincipalTripwire) ResolvePrincipal(context.Context, string) (identity.Principal, error) {
	*t.called = true
	return identity.Principal{}, nil
}

func publicCatalogRouter(t *testing.T, authenticator auth.Authenticator, principals identity.PrincipalResolver) *gin.Engine {
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
		fakeEntitlements{allowed: true},
		principals,
		WithPublicCatalogFoundation(foundation),
	)
	if err != nil {
		t.Fatalf("constructing production router: %v", err)
	}
	return r
}
