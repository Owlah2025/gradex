//go:build integration

package httpapi

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

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

type tokenSessionRepo struct {
	sessions map[string]identity.SessionView
}

func (r *tokenSessionRepo) Login(_ context.Context, _ identity.LoginRequest) (identity.SessionGrant, error) {
	return identity.SessionGrant{}, nil
}
func (r *tokenSessionRepo) CreateSession(_ context.Context, _ identity.AuthenticatedSession) error {
	return nil
}
func (r *tokenSessionRepo) RenewSession(_ context.Context, _, _ string, _, _, _ time.Time) (identity.SessionView, error) {
	return identity.SessionView{}, errors.New("not implemented")
}
func (r *tokenSessionRepo) Renew(_ context.Context, _ identity.SessionMutation) (identity.SessionGrant, error) {
	return identity.SessionGrant{}, nil
}
func (r *tokenSessionRepo) Logout(_ context.Context, _ identity.SessionMutation) error {
	return nil
}
func (r *tokenSessionRepo) RevokeSession(_ context.Context, _ string) error {
	return nil
}
func (r *tokenSessionRepo) RevokeFamily(_ context.Context, _ string) error {
	return nil
}
func (r *tokenSessionRepo) Resolve(_ context.Context, sessionToken string, _ identity.CredentialUseKind, _ string) (identity.SessionView, error) {
	v, ok := r.sessions[sessionToken]
	if !ok {
		return identity.SessionView{}, errors.New("invalid session")
	}
	return v, nil
}

func setupAdminPricingAPIServer(t *testing.T) (*httptest.Server, *pgxpool.Pool, string, string, string, string, string, string) {
	t.Helper()
	freshSchema(t)
	p, ctx := pool(t)

	adminID := "10000000-0000-0000-0000-000000000001"
	instID := "10000000-0000-0000-0000-000000000002"

	_, err := p.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name) VALUES
		($1, 'admin@example.com', 'admin@example.com', 'ADMIN', 'ACTIVE', 'Admin User'),
		($2, 'instructor@example.com', 'instructor@example.com', 'INSTRUCTOR', 'ACTIVE', 'Instructor User')
	`, adminID, instID)
	if err != nil {
		t.Fatalf("seeding accounts: %v", err)
	}

	obWriter, err := outbox.NewWriter("key-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("outbox.NewWriter: %v", err)
	}
	catalogRepo, err := catalog.NewRepository(p, obWriter)
	if err != nil {
		t.Fatalf("catalog.NewRepository: %v", err)
	}

	course, err := catalogRepo.CreateCourse(ctx, catalog.CreateCourseRequest{
		OwnerAccountID: instID,
		TitleAr:        "دورة اختبار التسعير",
		TitleEn:        "Pricing Test Course",
		DescriptionAr:  "وصف",
		DescriptionEn:  "Description",
	}, instID)
	if err != nil {
		t.Fatalf("creating course: %v", err)
	}

	ownedCourse, err := catalogRepo.GetOwnedCourse(ctx, course.ID, instID)
	if err != nil {
		t.Fatalf("getting owned course: %v", err)
	}

	sec, err := catalogRepo.AddSection(ctx, catalog.AddSectionRequest{
		CourseID:       course.ID,
		RevisionID:     ownedCourse.EditableRevision.ID,
		OwnerAccountID: instID,
		TitleAr:        "قسم 1",
		TitleEn:        "Section 1",
	}, instID)
	if err != nil {
		t.Fatalf("adding section: %v", err)
	}

	catalogFoundation, err := NewCatalogFoundation(CatalogFoundationOptions{
		Repository:     catalogRepo,
		AssetValidator: catalog.NewDBAssetVersionValidator(p),
	})
	if err != nil {
		t.Fatalf("NewCatalogFoundation: %v", err)
	}

	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV": "development", "REDIS_ADDR": "localhost:6379",
		"S3_ENDPOINT": "http://localhost:9000", "S3_BUCKET": "gradex-test",
	}), config.MapSecretResolver{
		"DATABASE_URL": authzTestDSN, "S3_ACCESS_KEY": "a",
		"S3_SECRET_KEY": "b", "PLAYBACK_TOKEN_SECRET": "c",
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	buf := &syncBuffer{}
	logger := logging.New(buf, "gradex-api-test", "development", logging.LevelFromString("info"))
	reporter := health.New(1 * time.Second)
	reporter.MarkStarted()

	adminToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x41}, 32))
	instToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
	now := time.Now().UTC()

	adminView := identity.SessionView{
		Session: identity.AuthenticatedSession{
			AccountID:         adminID,
			SessionID:         "admin-session-id",
			Role:              identity.RoleAdmin,
			CredentialState:   identity.CredentialActive,
			AuthenticatedAt:   now,
			IdleExpiresAt:     now.Add(24 * time.Hour),
			AbsoluteExpiresAt: now.Add(24 * time.Hour),
		},
	}
	instView := identity.SessionView{
		Session: identity.AuthenticatedSession{
			AccountID:         instID,
			SessionID:         "inst-session-id",
			Role:              identity.RoleInstructor,
			CredentialState:   identity.CredentialActive,
			AuthenticatedAt:   now,
			IdleExpiresAt:     now.Add(24 * time.Hour),
			AbsoluteExpiresAt: now.Add(24 * time.Hour),
		},
	}

	sessionRepo := &tokenSessionRepo{
		sessions: map[string]identity.SessionView{
			adminToken:                       adminView,
			identity.DigestToken(adminToken): adminView,
			instToken:                        instView,
			identity.DigestToken(instToken):  instView,
		},
	}

	limiter, _ := ratelimit.New(fakeRateStore{}, bytes.Repeat([]byte{0x31}, 32), time.Second)
	sessionPolicies := map[string]ratelimit.Policy{
		"session-bootstrap":  ratelimit.DevelopmentAnonymousBootstrapPolicy(),
		"sessions":           ratelimit.DevelopmentLoginPolicy(),
		"session-resolution": ratelimit.DevelopmentSessionPolicy("session-resolution"),
		"session-renewals":   ratelimit.DevelopmentSessionPolicy("session-renewals"),
		"session-logout":     ratelimit.DevelopmentSessionPolicy("session-logout"),
	}

	sessionFoundation, err := NewSessionFoundation(SessionFoundationOptions{
		PublicOrigin:        "https://gradex.example",
		CookieSigningKey:    bytes.Repeat([]byte{0x31}, 32),
		AnonymousCSRFKey:    bytes.Repeat([]byte{0x32}, 32),
		AnonymousSessionTTL: 24 * time.Hour,
		Repository:          sessionRepo,
		Limiter:             limiter,
		EndpointPolicies:    sessionPolicies,
	})
	if err != nil {
		t.Fatalf("NewSessionFoundation: %v", err)
	}

	principals := dbPrincipalResolver{pool: p}

	r, err := NewRouter(cfg, logger, reporter, sessionFoundation.authenticator, principals,
		WithSessionFoundation(sessionFoundation),
		WithCatalogFoundation(catalogFoundation),
	)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	ts := httptest.NewTLSServer(r)
	t.Cleanup(ts.Close)

	return ts, p, adminID, instID, course.ID, sec.SectionIdentityID, adminToken, instToken
}

func doPricingRequest(
	t *testing.T,
	client *http.Client,
	method, url string,
	token string,
	origin string,
	csrf string,
	body []byte,
) *http.Response {
	t.Helper()
	var req *http.Request
	var err error
	if len(body) > 0 {
		req, err = http.NewRequest(method, url, bytes.NewReader(body))
	} else {
		req, err = http.NewRequest(method, url, nil)
	}
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	if token != "" {
		req.AddCookie(&http.Cookie{
			Name:   auth.SessionCookieName,
			Value:  token,
			Secure: true,
		})
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	return resp
}

// TestCatalogMutationRouteTableRequiresSessionMutationSecurity derives the S2
// mutation surface from the production-composed Gin router.  It deliberately
// sends no valid body: Origin/CSRF must reject before request binding or any
// domain mutation, so malformed fixture data cannot hide a missing boundary.
func TestCatalogMutationRouteTableRequiresSessionMutationSecurity(t *testing.T) {
	ts, _, _, _, courseID, sectionID, adminToken, _ := setupAdminPricingAPIServer(t)
	engine, ok := ts.Config.Handler.(*gin.Engine)
	if !ok {
		t.Fatal("test server does not expose the production Gin router")
	}

	routes := catalogMutationRoutes(engine)
	if len(routes) == 0 {
		t.Fatal("no catalog mutation routes were derived from the live router")
	}

	for _, route := range routes {
		path := materializeCatalogMutationRoute(route.Path, courseID, sectionID)
		t.Run(route.Method+" "+route.Path+" rejects missing origin", func(t *testing.T) {
			response := doPricingRequest(t, ts.Client(), route.Method, ts.URL+path, adminToken, "", adminToken, nil)
			defer response.Body.Close()
			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("missing Origin status = %d, want 403", response.StatusCode)
			}
		})

		t.Run(route.Method+" "+route.Path+" rejects invalid csrf", func(t *testing.T) {
			response := doPricingRequest(t, ts.Client(), route.Method, ts.URL+path, adminToken, "https://gradex.example", "not-the-session-token", nil)
			defer response.Body.Close()
			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("invalid CSRF status = %d, want 403", response.StatusCode)
			}
		})
	}
}

func catalogMutationRoutes(engine *gin.Engine) []gin.RouteInfo {
	var routes []gin.RouteInfo
	for _, route := range engine.Routes() {
		if route.Method == http.MethodGet || !isCatalogMutationPath(route.Path) {
			continue
		}
		routes = append(routes, route)
	}
	return routes
}

func isCatalogMutationPath(path string) bool {
	return strings.HasPrefix(path, "/api/v1/courses") ||
		strings.HasPrefix(path, "/api/v1/admin/courses") ||
		strings.HasPrefix(path, "/api/v1/admin/review") ||
		strings.HasPrefix(path, "/api/v1/admin/taxonomy/terms")
}

func materializeCatalogMutationRoute(path, courseID, sectionID string) string {
	path = strings.ReplaceAll(path, ":revisionId", "30000000-0000-0000-0000-000000000001")
	path = strings.ReplaceAll(path, ":sectionId", sectionID)
	path = strings.ReplaceAll(path, ":lessonId", "30000000-0000-0000-0000-000000000002")
	path = strings.ReplaceAll(path, ":id", courseID)
	return path
}

func TestAdminPricingHTTPAPI_RealPostgreSQL(t *testing.T) {
	ts, pool, _, _, courseID, sectionID, adminToken, instToken := setupAdminPricingAPIServer(t)
	ctx := context.Background()

	client := ts.Client()
	validOrigin := "https://gradex.example"

	coursePriceURL := ts.URL + "/api/v1/admin/courses/" + courseID + "/price"
	sectionPriceURL := ts.URL + "/api/v1/admin/courses/" + courseID + "/sections/" + sectionID + "/price"
	historyURL := ts.URL + "/api/v1/admin/courses/" + courseID + "/price-history"

	t.Run("Missing Origin or CSRF rejected before domain work for Course and Section", func(t *testing.T) {
		body := []byte(`{"price_minor_units": 25000, "reason": "Test"}`)

		// Course route - Missing Origin
		resp := doPricingRequest(t, client, "PUT", coursePriceURL, adminToken, "", adminToken, body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Course PUT missing Origin got status %d, want 403", resp.StatusCode)
		}

		// Course route - Wrong CSRF
		resp = doPricingRequest(t, client, "PUT", coursePriceURL, adminToken, validOrigin, "wrong-csrf", body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Course PUT wrong CSRF got status %d, want 403", resp.StatusCode)
		}

		// Section route - Missing Origin
		resp = doPricingRequest(t, client, "PUT", sectionPriceURL, adminToken, "", adminToken, body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Section PUT missing Origin got status %d, want 403", resp.StatusCode)
		}

		// Section route - Wrong CSRF
		resp = doPricingRequest(t, client, "PUT", sectionPriceURL, adminToken, validOrigin, "wrong-csrf", body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Section PUT wrong CSRF got status %d, want 403", resp.StatusCode)
		}

		// Assert zero price change records were written by rejected requests
		var rejectedCount int
		err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM course_price_changes WHERE course_id = $1::uuid`, courseID).Scan(&rejectedCount)
		if err != nil || rejectedCount != 0 {
			t.Fatalf("expected 0 course_price_changes rows after security rejections, got %d (err: %v)", rejectedCount, err)
		}
	})

	t.Run("Instructor direct write refused for Course and Section", func(t *testing.T) {
		body := []byte(`{"price_minor_units": 25000, "reason": "Instructor attempt"}`)

		resp := doPricingRequest(t, client, "PUT", coursePriceURL, instToken, validOrigin, instToken, body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Instructor Course PUT got status %d, want 403", resp.StatusCode)
		}

		resp = doPricingRequest(t, client, "PUT", sectionPriceURL, instToken, validOrigin, instToken, body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Instructor Section PUT got status %d, want 403", resp.StatusCode)
		}
	})

	t.Run("Admin Course Price PUT succeeds and persists price and audit rows", func(t *testing.T) {
		body := []byte(`{"price_minor_units": 25000, "reason": "Initial course price"}`)
		resp := doPricingRequest(t, client, "PUT", coursePriceURL, adminToken, validOrigin, adminToken, body)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Course price PUT status = %d, want 200", resp.StatusCode)
		}

		var pc catalog.PriceChange
		if err := json.NewDecoder(resp.Body).Decode(&pc); err != nil {
			t.Fatalf("decoding response: %v", err)
		}
		if pc.NewValueMinorUnits != 25000 || pc.Reason != "Initial course price" {
			t.Errorf("got PriceChange %+v, want 25000 fils", pc)
		}

		var persistedPrice int64
		err := pool.QueryRow(ctx, `
			SELECT new_value_minor_units FROM course_price_changes
			WHERE course_id = $1::uuid AND section_id IS NULL
		`, courseID).Scan(&persistedPrice)
		if err != nil || persistedPrice != 25000 {
			t.Errorf("persisted course_price_changes err=%v, price=%d, want 25000", err, persistedPrice)
		}

		var auditCount int
		err = pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM audit_events
			WHERE action = 'COURSE_PRICE_CHANGED' AND target_type = 'COURSE' AND target_id = $1
		`, courseID).Scan(&auditCount)
		if err != nil || auditCount != 1 {
			t.Errorf("persisted audit_events count = %d (err=%v), want 1", auditCount, err)
		}
	})

	t.Run("Admin Section Price PUT succeeds and persists price and audit rows", func(t *testing.T) {
		body := []byte(`{"price_minor_units": 10000, "reason": "Section standalone price"}`)
		resp := doPricingRequest(t, client, "PUT", sectionPriceURL, adminToken, validOrigin, adminToken, body)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Section price PUT status = %d, want 200", resp.StatusCode)
		}

		var pc catalog.PriceChange
		if err := json.NewDecoder(resp.Body).Decode(&pc); err != nil {
			t.Fatalf("decoding response: %v", err)
		}
		if pc.NewValueMinorUnits != 10000 || pc.SectionID == nil || *pc.SectionID != sectionID {
			t.Errorf("got PriceChange %+v, want section %s 10000 fils", pc, sectionID)
		}

		var persistedPrice int64
		err := pool.QueryRow(ctx, `
			SELECT new_value_minor_units FROM course_price_changes
			WHERE course_id = $1::uuid AND section_id = $2::uuid
		`, courseID, sectionID).Scan(&persistedPrice)
		if err != nil || persistedPrice != 10000 {
			t.Errorf("persisted section course_price_changes err=%v, price=%d, want 10000", err, persistedPrice)
		}

		var auditCount int
		err = pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM audit_events
			WHERE action = 'COURSE_PRICE_CHANGED' AND target_type = 'SECTION' AND target_id = $1
		`, sectionID).Scan(&auditCount)
		if err != nil || auditCount != 1 {
			t.Errorf("persisted audit_events section count = %d (err=%v), want 1", auditCount, err)
		}
	})

	t.Run("Admin GET price history succeeds", func(t *testing.T) {
		resp := doPricingRequest(t, client, "GET", historyURL, adminToken, "", "", nil)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET price history status = %d, want 200", resp.StatusCode)
		}

		var history []catalog.PriceChange
		if err := json.NewDecoder(resp.Body).Decode(&history); err != nil {
			t.Fatalf("decoding response: %v", err)
		}
		if len(history) != 2 {
			t.Fatalf("history length = %d, want 2", len(history))
		}
	})
}
