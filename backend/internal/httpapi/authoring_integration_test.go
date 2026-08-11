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
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

const (
	authzAdminDSN   = "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable"
	authzTestDBName = "gradex_authoring_test"
	authzTestDSN    = "postgres://gradex:gradex@localhost:5432/" + authzTestDBName + "?sslmode=disable"
	authzSourceURL  = "file://../db/migrations"
	authzOpTimeout  = 30 * time.Second
)

func testWriterForAuthoring(t *testing.T) *outbox.Writer {
	t.Helper()
	writer, err := outbox.NewWriter("test-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("outbox.NewWriter: %v", err)
	}
	return writer
}

func freshSchema(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), authzOpTimeout)
	defer cancel()

	admin, err := pgxpool.New(ctx, authzAdminDSN)
	if err != nil {
		t.Fatalf("connecting to admin db: %v", err)
	}
	defer admin.Close()

	_, _ = admin.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, authzTestDBName)
	_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+authzTestDBName)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+authzTestDBName); err != nil {
		t.Fatalf("creating test db: %v", err)
	}

	m, err := migrate.New(authzSourceURL, authzTestDSN)
	if err != nil {
		t.Fatalf("creating migrator: %v", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrating up: %v", err)
	}
}

func pool(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), authzOpTimeout)
	t.Cleanup(cancel)

	p, err := pgxpool.New(ctx, authzTestDSN)
	if err != nil {
		t.Fatalf("opening pool: %v", err)
	}
	t.Cleanup(p.Close)
	return p, ctx
}

type dbPrincipalResolver struct {
	pool *pgxpool.Pool
}

func (r dbPrincipalResolver) ResolvePrincipal(ctx context.Context, accountID string) (identity.Principal, error) {
	var p identity.Principal
	var roleStr, statusStr string
	query := `SELECT id, role, status FROM accounts WHERE id = $1::uuid`
	err := r.pool.QueryRow(ctx, query, accountID).Scan(&p.AccountID, &roleStr, &statusStr)
	if err != nil {
		return identity.Principal{}, identity.ErrPrincipalNotFound
	}
	p.Role = identity.Role(roleStr)
	p.Status = identity.AccountStatus(statusStr)
	p.CredentialState = identity.CredentialActive
	return p, nil
}

type dbSessionRepo struct {
	pool *pgxpool.Pool
}

func (r dbSessionRepo) Resolve(ctx context.Context, sessionToken string, kind identity.CredentialUseKind, rawIP string) (identity.SessionView, error) {
	return identity.SessionView{}, errors.New("not implemented")
}

func buildTestRouterWithAccount(t *testing.T, pool *pgxpool.Pool, accountID string, role identity.Role, status identity.AccountStatus, options ...RouterOption) *httptest.Server {
	t.Helper()

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
	reporter := health.New(time.Second)
	reporter.MarkStarted()

	limiter, _ := ratelimit.New(fakeRateStore{}, bytes.Repeat([]byte{0x31}, 32), time.Second)
	sessionPolicies := testSessionEndpointPolicies()

	now := time.Now().UTC()
	sessionRepo := &fakeSessionRepository{
		view: identity.SessionView{
			Session: identity.AuthenticatedSession{
				AccountID:         accountID,
				SessionID:         "test-session-id",
				Role:              role,
				CredentialState:   identity.CredentialActive,
				AuthenticatedAt:   now,
				IdleExpiresAt:     now.Add(24 * time.Hour),
				AbsoluteExpiresAt: now.Add(24 * time.Hour),
			},
		},
	}

	sessionFoundation, err := NewSessionFoundation(SessionFoundationOptions{
		PublicOrigin:        "https://gradex.example",
		CookieSigningKey:    bytes.Repeat([]byte{0x31}, 32),
		AnonymousCSRFKey:    bytes.Repeat([]byte{0x32}, 32),
		AnonymousSessionTTL: 24 * time.Hour,
		Repository:          sessionRepo,
		Compromised:         testCompromisedSource(t),
		Limiter:             limiter,
		EndpointPolicies:    sessionPolicies,
	})
	if err != nil {
		t.Fatalf("NewSessionFoundation: %v", err)
	}

	outboxWriter, err := outbox.NewWriter("key-v1", bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("outbox.NewWriter: %v", err)
	}
	catalogRepo, err := catalog.NewRepository(pool, outboxWriter)
	if err != nil {
		t.Fatalf("catalog.NewRepository: %v", err)
	}
	catalogFoundation, err := NewCatalogFoundation(CatalogFoundationOptions{
		Repository:     catalogRepo,
		AssetValidator: catalog.NewDBAssetVersionValidator(pool),
	})
	if err != nil {
		t.Fatalf("NewCatalogFoundation: %v", err)
	}

	principals := dbPrincipalResolver{pool: pool}

	routerOptions := []RouterOption{
		WithSessionFoundation(sessionFoundation),
		WithCatalogFoundation(catalogFoundation),
	}
	routerOptions = append(routerOptions, options...)
	r, err := NewRouter(cfg, logger, reporter, sessionFoundation.authenticator, principals, routerOptions...)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)
	return ts
}

func buildPublicCatalogRouter(t *testing.T, pool *pgxpool.Pool) *gin.Engine {
	t.Helper()
	cfg, err := config.LoadFrom(config.MapLookup(map[string]string{
		"APP_ENV": "development", "REDIS_ADDR": "localhost:6379",
		"S3_ENDPOINT": "http://localhost:9000", "S3_BUCKET": "gradex-test",
	}), config.MapSecretResolver{
		"DATABASE_URL": authzTestDSN, "S3_ACCESS_KEY": "a",
		"S3_SECRET_KEY": "b", "PLAYBACK_TOKEN_SECRET": "c",
	})
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}

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
		fakeAuth{},
		fixedPrincipals{},
		WithPublicCatalogFoundation(foundation),
	)
	if err != nil {
		t.Fatalf("constructing public catalogue router: %v", err)
	}
	return r
}

func TestPrivateDraftProtectionAcrossReadRoutes(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)

	ownerID := "11111111-1111-1111-1111-111111111111"
	nonOwnerID := "22222222-2222-2222-2222-222222222222"
	studentID := "33333333-3333-3333-3333-333333333333"
	suspendedID := "44444444-4444-4444-4444-444444444444"

	// Seed accounts in DB
	_, err := p.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name) VALUES
		($1, 'owner@example.com', 'owner@example.com', 'INSTRUCTOR', 'ACTIVE', 'Owner Instructor'),
		($2, 'nonowner@example.com', 'nonowner@example.com', 'INSTRUCTOR', 'ACTIVE', 'Non-Owner Instructor'),
		($3, 'student@example.com', 'student@example.com', 'STUDENT', 'ACTIVE', 'Student User'),
		($4, 'suspended@example.com', 'suspended@example.com', 'INSTRUCTOR', 'SUSPENDED', 'Suspended Instructor')
	`, ownerID, nonOwnerID, studentID, suspendedID)
	if err != nil {
		t.Fatalf("seeding accounts: %v", err)
	}

	repo, err := catalog.NewRepository(p, testWriterForAuthoring(t))
	if err != nil {
		t.Fatalf("catalog.NewRepository: %v", err)
	}

	// Create course as Owner
	course, err := repo.CreateCourse(ctx, catalog.CreateCourseRequest{
		OwnerAccountID: ownerID,
		TitleAr:        "دورة تجريبية",
		TitleEn:        "Test Course",
		DescriptionAr:  "وصف الدورة",
		DescriptionEn:  "Test Description",
	}, "owner@example.com")
	if err != nil {
		t.Fatalf("CreateCourse: %v", err)
	}

	if course.Lifecycle != catalog.LifecycleDraft {
		t.Fatalf("course lifecycle = %s, want DRAFT", course.Lifecycle)
	}

	nonOwnerServer := buildTestRouterWithAccount(t, p, nonOwnerID, identity.RoleInstructor, identity.StatusActive)
	studentServer := buildTestRouterWithAccount(t, p, studentID, identity.RoleStudent, identity.StatusActive)
	suspendedServer := buildTestRouterWithAccount(t, p, suspendedID, identity.RoleInstructor, identity.StatusSuspended)

	// Test 1: Non-owner Instructor reading existing course vs non-existent course
	recYours := makeAuthRequest(t, nonOwnerServer.URL+"/api/v1/courses/"+course.ID)
	recNonExistent := makeAuthRequest(t, nonOwnerServer.URL+"/api/v1/courses/00000000-0000-0000-0000-000000000000")

	pYours := parseProblemFromHTTP(t, recYours)
	pNonExistent := parseProblemFromHTTP(t, recNonExistent)

	if recYours.StatusCode != http.StatusForbidden || recNonExistent.StatusCode != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d and %d", recYours.StatusCode, recNonExistent.StatusCode)
	}
	if pYours.Status != pNonExistent.Status || pYours.Code != pNonExistent.Code || pYours.Type != pNonExistent.Type || pYours.Detail != pNonExistent.Detail {
		t.Fatalf("Non-enumeration failed: 'not yours' (%+v) != 'does not exist' (%+v)", pYours, pNonExistent)
	}

	// Test 2: Student reading draft course
	recStudent := makeAuthRequest(t, studentServer.URL+"/api/v1/courses/"+course.ID)
	if recStudent.StatusCode != http.StatusForbidden {
		t.Fatalf("student got status %d, want 403", recStudent.StatusCode)
	}

	// Test 3: Suspended instructor reading/editing
	recSuspended := makeAuthRequest(t, suspendedServer.URL+"/api/v1/courses/"+course.ID)
	if recSuspended.StatusCode != http.StatusForbidden {
		t.Fatalf("suspended instructor got status %d, want 403", recSuspended.StatusCode)
	}

	// Test 4: Anonymous caller reading draft course
	recAnon, err := http.Get(nonOwnerServer.URL + "/api/v1/courses/" + course.ID)
	if err != nil {
		t.Fatalf("anonymous request: %v", err)
	}
	if recAnon.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous got status %d, want 401", recAnon.StatusCode)
	}
}

func TestSuspendedInstructorRefusedEditing(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)

	suspendedID := "44444444-4444-4444-4444-444444444444"
	courseID := "55555555-5555-5555-5555-555555555555"

	_, err := p.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name) VALUES
		($1, 'suspended@example.com', 'suspended@example.com', 'INSTRUCTOR', 'SUSPENDED', 'Suspended Instructor')
	`, suspendedID)
	if err != nil {
		t.Fatalf("seeding account: %v", err)
	}

	_, err = p.Exec(ctx, `
		INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1, $2, 'DRAFT')
	`, courseID, suspendedID)
	if err != nil {
		t.Fatalf("seeding course: %v", err)
	}

	revID := "66666666-6666-6666-6666-666666666666"
	_, err = p.Exec(ctx, `
		INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en, description_ar, description_en)
		VALUES ($1, $2, 'DRAFT', 1, 'Title AR', 'Title EN', 'Desc AR', 'Desc EN')
	`, revID, courseID)
	if err != nil {
		t.Fatalf("seeding revision: %v", err)
	}

	repo, err := catalog.NewRepository(p, testWriterForAuthoring(t))
	if err != nil {
		t.Fatalf("catalog.NewRepository: %v", err)
	}

	// Attempt edit by suspended instructor
	_, err = repo.AddSection(ctx, catalog.AddSectionRequest{
		CourseID:       courseID,
		RevisionID:     revID,
		OwnerAccountID: suspendedID,
		TitleAr:        "قسم جديد",
		TitleEn:        "New Section",
	}, "suspended@example.com")

	if err == nil {
		t.Fatal("expected error on editing by suspended instructor, got nil")
	}
	if !errors.Is(err, catalog.ErrAccountSuspended) {
		t.Fatalf("got error %v, want ErrAccountSuspended", err)
	}
}

func makeAuthRequest(t *testing.T, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Origin", "https://gradex.example")
	validToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x41}, 32))
	req.AddCookie(&http.Cookie{
		Name:  "__Host-gradex_session",
		Value: validToken,
	})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	return resp
}

func parseProblemFromHTTP(t *testing.T, resp *http.Response) problem.Problem {
	t.Helper()
	defer resp.Body.Close()
	var p problem.Problem
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatalf("decoding problem response: %v", err)
	}
	return p
}
