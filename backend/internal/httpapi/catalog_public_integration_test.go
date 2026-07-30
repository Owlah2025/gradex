//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPublicCatalogRoutesExposeOnlyVisibleCourses(t *testing.T) {
	freshSchema(t)
	pool, ctx := pool(t)
	seedPublicCatalogOwner(t, pool, ctx)
	published := seedPublicCatalogCourse(t, pool, ctx, publicCourseVisibility{lifecycle: "PUBLISHED"})
	seedRetiredPublicTaxonomy(t, pool, ctx, published)
	hidden := map[string]string{
		"draft":             seedPublicCatalogCourse(t, pool, ctx, publicCourseVisibility{lifecycle: "DRAFT"}),
		"pending review":    seedPublicCatalogCourse(t, pool, ctx, publicCourseVisibility{lifecycle: "PENDING_REVIEW"}),
		"changes requested": seedPublicCatalogCourse(t, pool, ctx, publicCourseVisibility{lifecycle: "CHANGES_REQUESTED"}),
		"delisted":          seedPublicCatalogCourse(t, pool, ctx, publicCourseVisibility{lifecycle: "DELISTED"}),
		"archived":          seedPublicCatalogCourse(t, pool, ctx, publicCourseVisibility{lifecycle: "ARCHIVED"}),
		"suspended":         seedPublicCatalogCourse(t, pool, ctx, publicCourseVisibility{lifecycle: "PUBLISHED", suspended: true}),
		"retired":           seedPublicCatalogCourse(t, pool, ctx, publicCourseVisibility{lifecycle: "PUBLISHED", retired: true}),
	}
	r := buildPublicCatalogRouter(t, pool)

	list := publicCatalogRequest(r, http.MethodGet, "/api/v1/catalog/courses")
	if list.Code != http.StatusOK {
		t.Fatalf("public list status = %d, want 200: %s", list.Code, list.Body.String())
	}
	if list.Header().Get("Cache-Control") != publicCatalogCacheControl {
		t.Errorf("public list Cache-Control = %q, want %q", list.Header().Get("Cache-Control"), publicCatalogCacheControl)
	}
	if list.Header().Get("Set-Cookie") != "" {
		t.Errorf("public list set a cookie: %q", list.Header().Get("Set-Cookie"))
	}
	if !strings.Contains(list.Body.String(), "علوم متقاعده") {
		t.Fatalf("list omitted retired assigned taxonomy: %s", list.Body.String())
	}

	visible := publicCatalogRequest(r, http.MethodGet, "/api/v1/catalog/courses/"+published)
	if visible.Code != http.StatusOK {
		t.Fatalf("published detail status = %d, want 200: %s", visible.Code, visible.Body.String())
	}
	if !strings.Contains(visible.Body.String(), "علوم متقاعده") {
		t.Fatalf("detail omitted retired assigned taxonomy: %s", visible.Body.String())
	}
	if _, err := pool.Exec(ctx, `UPDATE accounts SET status = 'SUSPENDED' WHERE id = '11111111-1111-1111-1111-111111111111'`); err != nil {
		t.Fatalf("suspending public course owner: %v", err)
	}
	if suspendedOwner := publicCatalogRequest(r, http.MethodGet, "/api/v1/catalog/courses/"+published); suspendedOwner.Code != http.StatusOK {
		t.Fatalf("published course disappeared with suspended owner: %d %s", suspendedOwner.Code, suspendedOwner.Body.String())
	}
	var slug string
	if err := pool.QueryRow(ctx, `SELECT slug FROM courses WHERE id = $1::uuid`, published).Scan(&slug); err != nil {
		t.Fatalf("reading generated course slug: %v", err)
	}
	bySlug := publicCatalogRequest(r, http.MethodGet, "/api/v1/catalog/courses/"+slug)
	if bySlug.Code != http.StatusOK {
		t.Fatalf("slug detail status = %d, want 200: %s", bySlug.Code, bySlug.Body.String())
	}
	for _, body := range []string{list.Body.String(), visible.Body.String()} {
		for _, prohibited := range []string{"price_minor_units", "section_price", "\"email\"", "\"resources\"", "\"lab_materials\"", "owner_account", "reviewed_by"} {
			if strings.Contains(strings.ToLower(body), prohibited) {
				t.Errorf("public response exposes prohibited field %q: %s", prohibited, body)
			}
		}
	}
	missing := publicCatalogRequest(r, http.MethodGet, "/api/v1/catalog/courses/00000000-0000-0000-0000-000000000000")
	for name, identifier := range hidden {
		t.Run(name, func(t *testing.T) {
			got := publicCatalogRequest(r, http.MethodGet, "/api/v1/catalog/courses/"+identifier)
			assertSamePublicCatalogNotFound(t, missing, got)
		})
	}
	malformed := publicCatalogRequest(r, http.MethodGet, "/api/v1/catalog/courses/not-a-canonical-slug")
	assertSamePublicCatalogNotFound(t, missing, malformed)
}

func seedRetiredPublicTaxonomy(t *testing.T, pool *pgxpool.Pool, ctx context.Context, courseID string) {
	t.Helper()
	var majorID, subjectID string
	if err := pool.QueryRow(ctx, `INSERT INTO taxonomy_terms (kind, label_ar, label_en) VALUES ('MAJOR', 'علوم متقاعده', 'Retired Science') RETURNING id::text`).Scan(&majorID); err != nil {
		t.Fatalf("creating retired major: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO taxonomy_terms (kind, label_ar, label_en, academic_code) VALUES ('SUBJECT', 'احياء متقاعده', 'Retired Biology', 'BIO') RETURNING id::text`).Scan(&subjectID); err != nil {
		t.Fatalf("creating retired subject: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE course_revisions SET major_term_id = $1::uuid, subject_term_id = $2::uuid WHERE id = (SELECT live_revision_id FROM courses WHERE id = $3::uuid)`, majorID, subjectID, courseID); err != nil {
		t.Fatalf("assigning public taxonomy: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE taxonomy_terms SET retired_at = now() WHERE id IN ($1::uuid, $2::uuid)`, majorID, subjectID); err != nil {
		t.Fatalf("retiring assigned taxonomy: %v", err)
	}
}

func publicCatalogRequest(r *gin.Engine, method, path string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	return recorder
}

func assertSamePublicCatalogNotFound(t *testing.T, want, got *httptest.ResponseRecorder) {
	t.Helper()
	if want.Code != http.StatusNotFound || got.Code != http.StatusNotFound {
		t.Fatalf("not-found statuses = (%d, %d), want both 404", want.Code, got.Code)
	}
	if !reflect.DeepEqual(want.Header(), got.Header()) {
		t.Fatalf("not-found headers differ: missing=%v hidden=%v", want.Header(), got.Header())
	}
	if !bytes.Equal(want.Body.Bytes(), got.Body.Bytes()) {
		t.Fatalf("not-found bodies differ: missing=%q hidden=%q", want.Body.String(), got.Body.String())
	}
}

func seedPublicCatalogOwner(t *testing.T, pool *pgxpool.Pool, ctx context.Context) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
		VALUES ('11111111-1111-1111-1111-111111111111', 'public-owner@example.test', 'public-owner@example.test', 'INSTRUCTOR', 'ACTIVE', 'Public Owner')
	`); err != nil {
		t.Fatalf("seeding public catalogue owner: %v", err)
	}
}

type publicCourseVisibility struct {
	lifecycle string
	suspended bool
	retired   bool
}

func seedPublicCatalogCourse(t *testing.T, pool *pgxpool.Pool, ctx context.Context, visibility publicCourseVisibility) string {
	t.Helper()
	var courseID, revisionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO courses (owner_account_id, lifecycle)
		VALUES ('11111111-1111-1111-1111-111111111111', 'DRAFT')
		RETURNING id::text
	`).Scan(&courseID); err != nil {
		t.Fatalf("creating %s course: %v", visibility.lifecycle, err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO course_revisions (course_id, state, revision_number, title_ar, title_en)
		VALUES ($1::uuid, 'APPROVED', 1, 'عنوان عام', 'Public title')
		RETURNING id::text
	`, courseID).Scan(&revisionID); err != nil {
		t.Fatalf("creating %s revision: %v", visibility.lifecycle, err)
	}
	if visibility.lifecycle == "PUBLISHED" {
		if _, err := pool.Exec(ctx, `
			UPDATE courses SET lifecycle = 'PUBLISHED', live_revision_id = $1::uuid
			WHERE id = $2::uuid
		`, revisionID, courseID); err != nil {
			t.Fatalf("publishing course: %v", err)
		}
	} else if _, err := pool.Exec(ctx, `UPDATE courses SET lifecycle = $1::course_lifecycle WHERE id = $2::uuid`, visibility.lifecycle, courseID); err != nil {
		t.Fatalf("setting course lifecycle %s: %v", visibility.lifecycle, err)
	}
	if visibility.suspended {
		if _, err := pool.Exec(ctx, `
			UPDATE courses
			SET access_suspended_at = now(), access_suspension_reason = 'emergency access suspension'
			WHERE id = $1::uuid
		`, courseID); err != nil {
			t.Fatalf("suspending course: %v", err)
		}
	}
	if visibility.retired {
		if _, err := pool.Exec(ctx, `UPDATE courses SET retired_at = now() WHERE id = $1::uuid`, courseID); err != nil {
			t.Fatalf("retiring course: %v", err)
		}
	}
	return courseID
}
