//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/access"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

func setupAdminAccessAPIServer(t *testing.T) (*httptest.Server, *pgxpool.Pool, string, string, string) {
	t.Helper()
	freshSchema(t)
	p, ctx := pool(t)

	adminID := "10000000-0000-0000-0000-000000000001"
	instID := "10000000-0000-0000-0000-000000000002"

	_, err := p.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name) VALUES
		($1, 'admin-access@example.com', 'admin-access@example.com', 'ADMIN', 'ACTIVE', 'Admin Access User'),
		($2, 'instructor-access@example.com', 'instructor-access@example.com', 'INSTRUCTOR', 'ACTIVE', 'Instructor Access User')
	`, adminID, instID)
	if err != nil {
		t.Fatalf("seeding accounts: %v", err)
	}

	courseID := "20000000-0000-0000-0000-000000000001"
	_, err = p.Exec(ctx, `
		INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1, $2, 'DRAFT')
	`, courseID, instID)
	if err != nil {
		t.Fatalf("seeding course: %v", err)
	}

	accessRepo, err := access.NewRepository(p)
	if err != nil {
		t.Fatalf("access.NewRepository: %v", err)
	}

	accessFoundation, err := NewAccessFoundation(AccessFoundationOptions{
		Repository: accessRepo,
	})
	if err != nil {
		t.Fatalf("NewAccessFoundation: %v", err)
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
	logger := logging.New(buf, "gradex-access-test", "development", logging.LevelFromString("info"))
	reporter := health.New(1 * time.Second)
	reporter.MarkStarted()

	adminToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x51}, 32))
	instToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x52}, 32))
	now := time.Now().UTC()

	adminView := identity.SessionView{
		Session: identity.AuthenticatedSession{
			AccountID:         adminID,
			SessionID:         "admin-access-session-id",
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
			SessionID:         "inst-access-session-id",
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
		WithAccessFoundation(accessFoundation),
	)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	ts := httptest.NewTLSServer(r)
	t.Cleanup(ts.Close)

	return ts, p, adminID, courseID, adminToken
}

func TestAdminAccessExpiryHTTPAPI_RealPostgreSQL(t *testing.T) {
	ts, pool, adminID, courseID, adminToken := setupAdminAccessAPIServer(t)
	ctx := context.Background()

	client := ts.Client()
	validOrigin := "https://gradex.example"
	expiryURL := ts.URL + "/api/v1/admin/courses/" + courseID + "/default-access-expiry"

	t.Run("Admin PUT default access expiry succeeds with Kuwait date conversion and audit record", func(t *testing.T) {
		body := []byte(`{"date": "2026-12-31", "reason": "Semester end 2026"}`)
		resp := doPricingRequest(t, client, "PUT", expiryURL, adminToken, validOrigin, adminToken, body)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("PUT default access expiry status = %d, want 200", resp.StatusCode)
		}

		var res struct {
			CourseID            string    `json:"course_id"`
			DefaultAccessEndsAt time.Time `json:"default_access_ends_at"`
			Reason              string    `json:"reason"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			t.Fatalf("decoding response: %v", err)
		}

		wantUTC := "2026-12-31T21:00:00Z"
		if res.DefaultAccessEndsAt.UTC().Format(time.RFC3339) != wantUTC {
			t.Errorf("got DefaultAccessEndsAt = %s, want %s", res.DefaultAccessEndsAt.UTC().Format(time.RFC3339), wantUTC)
		}

		var dbEndsAt time.Time
		err := pool.QueryRow(ctx, `SELECT default_access_ends_at FROM courses WHERE id = $1::uuid`, courseID).Scan(&dbEndsAt)
		if err != nil {
			t.Fatalf("reading default_access_ends_at from DB: %v", err)
		}
		if dbEndsAt.UTC().Format(time.RFC3339) != wantUTC {
			t.Errorf("DB default_access_ends_at = %s, want %s", dbEndsAt.UTC().Format(time.RFC3339), wantUTC)
		}

		var auditCount int
		err = pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM audit_events
			WHERE action = 'COURSE_DEFAULT_ACCESS_EXPIRY_SET' AND target_type = 'COURSE' AND target_id = $1 AND actor_account_id = $2::uuid
		`, courseID, adminID).Scan(&auditCount)
		if err != nil || auditCount != 1 {
			t.Errorf("audit_events count = %d (err=%v), want 1", auditCount, err)
		}
	})

	t.Run("Invalid date format returns 422", func(t *testing.T) {
		body := []byte(`{"date": "2026/12/31", "reason": "Bad date"}`)
		resp := doPricingRequest(t, client, "PUT", expiryURL, adminToken, validOrigin, adminToken, body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("Invalid date status = %d, want 422", resp.StatusCode)
		}
	})

	t.Run("Missing reason returns 422", func(t *testing.T) {
		body := []byte(`{"date": "2026-12-31", "reason": ""}`)
		resp := doPricingRequest(t, client, "PUT", expiryURL, adminToken, validOrigin, adminToken, body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("Missing reason status = %d, want 422", resp.StatusCode)
		}
	})

	t.Run("Non-existent course returns 404", func(t *testing.T) {
		nonExistentURL := ts.URL + "/api/v1/admin/courses/90000000-0000-0000-0000-000000000001/default-access-expiry"
		body := []byte(`{"date": "2026-12-31", "reason": "Valid reason"}`)
		resp := doPricingRequest(t, client, "PUT", nonExistentURL, adminToken, validOrigin, adminToken, body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Non-existent course status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("Missing CSRF or Origin returns 403", func(t *testing.T) {
		body := []byte(`{"date": "2026-12-31", "reason": "Valid reason"}`)

		resp := doPricingRequest(t, client, "PUT", expiryURL, adminToken, "", adminToken, body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Missing Origin status = %d, want 403", resp.StatusCode)
		}

		resp = doPricingRequest(t, client, "PUT", expiryURL, adminToken, validOrigin, "bad-csrf", body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Bad CSRF status = %d, want 403", resp.StatusCode)
		}
	})
}
