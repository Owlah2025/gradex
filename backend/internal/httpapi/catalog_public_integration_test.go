//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPublicCatalogRoutesExposeOnlyVisibleCourses(t *testing.T) {
	freshSchema(t)
	pool, ctx := pool(t)
	seedPublicCatalogOwner(t, pool, ctx)
	published := seedPublicCatalogCourse(t, pool, ctx, publicCourseVisibility{lifecycle: "PUBLISHED"})
	seedPricedPublicSection(t, pool, ctx, published)
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

func TestPublicCatalogSearchUsesPublishedOnlyLiveRevision(t *testing.T) {
	freshSchema(t)
	pool, ctx := pool(t)
	seedPublicCatalogOwner(t, pool, ctx)
	visibleCourseID := seedPublicCatalogCourse(t, pool, ctx, publicCourseVisibility{lifecycle: "PUBLISHED"})
	setSearchRevision(t, pool, ctx, visibleCourseID, "أحياء Mixed Biology ١٠١", "Live Biology 101", "وصف أحياء حي", "A live mixed-language description")
	seedSearchJoinedFields(t, pool, ctx, visibleCourseID)
	seedSearchRevision(t, pool, ctx, visibleCourseID, 2, "SUPERSEDED", "عنوان تاريخي فريد", "Historical Biology Only")
	r := buildPublicCatalogRouter(t, pool)

	for _, query := range []string{"احياء", "أَحْياء", "احــياء", "BIOLOGY", "١٠١", "101"} {
		assertSearchContains(t, publicSearchRequest(r, query), visibleCourseID, query)
	}
	for _, query := range []string{"search instructor", "علم الاحياء", "life science", "BIO-101"} {
		assertSearchContains(t, publicSearchRequest(r, query), visibleCourseID, query)
	}
	retiredTaxonomy := publicSearchRequest(r, "علم الاحياء")
	if !strings.Contains(retiredTaxonomy.Body.String(), "علم الأحياء") {
		t.Fatalf("search omitted retired assigned taxonomy: %s", retiredTaxonomy.Body.String())
	}
	assertSearchOmits(t, publicSearchRequest(r, "عنوان تاريخي فريد"), visibleCourseID, "historical non-live text")

	hidden := map[string]string{
		"draft":             seedPublicCatalogCourse(t, pool, ctx, publicCourseVisibility{lifecycle: "DRAFT"}),
		"pending review":    seedPublicCatalogCourse(t, pool, ctx, publicCourseVisibility{lifecycle: "PENDING_REVIEW"}),
		"changes requested": seedPublicCatalogCourse(t, pool, ctx, publicCourseVisibility{lifecycle: "CHANGES_REQUESTED"}),
		"delisted":          seedPublicCatalogCourse(t, pool, ctx, publicCourseVisibility{lifecycle: "DELISTED"}),
		"archived":          seedPublicCatalogCourse(t, pool, ctx, publicCourseVisibility{lifecycle: "ARCHIVED"}),
		"retired":           seedPublicCatalogCourse(t, pool, ctx, publicCourseVisibility{lifecycle: "PUBLISHED", retired: true}),
		"suspended":         seedPublicCatalogCourse(t, pool, ctx, publicCourseVisibility{lifecycle: "PUBLISHED", suspended: true}),
	}
	for state, courseID := range hidden {
		query := "مخفي " + state
		setSearchRevision(t, pool, ctx, courseID, query, "Hidden "+state, "", "")
		assertStoredSearchText(t, pool, ctx, courseID, query)
		assertSearchOmits(t, publicSearchRequest(r, query), courseID, state)
	}

	assertSearchOmits(t, publicSearchRequest(r, "lesson-private-title"), visibleCourseID, "lesson title")
	assertSearchOmits(t, publicSearchRequest(r, "resource-private-filename"), visibleCourseID, "resource filename")

	newLiveRevisionID := seedSearchRevision(t, pool, ctx, visibleCourseID, 3, "APPROVED", "نص حي جديد", "Replacement Live Text")
	if _, err := pool.Exec(ctx, `UPDATE courses SET live_revision_id = $1::uuid WHERE id = $2::uuid`, newLiveRevisionID, visibleCourseID); err != nil {
		t.Fatalf("repointing live revision: %v", err)
	}
	assertSearchContains(t, publicSearchRequest(r, "replacement live text"), visibleCourseID, "repointed live text")
	assertSearchOmits(t, publicSearchRequest(r, "live biology 101"), visibleCourseID, "former live text")
	detail := publicCatalogRequest(r, http.MethodGet, "/api/v1/catalog/courses/"+visibleCourseID)
	if !strings.Contains(detail.Body.String(), "نص حي جديد") {
		t.Fatalf("detail does not use repointed live revision: %s", detail.Body.String())
	}

	absent := publicCatalogRequest(r, http.MethodGet, "/api/v1/catalog/courses")
	for _, query := range []string{"", "   ", "ًُ", "ـــ", strings.Repeat("x", 10*1024), "%' OR 1=1 --"} {
		response := publicSearchRequest(r, query)
		if response.Code != http.StatusOK {
			t.Errorf("query %q status = %d, want 200: %s", query, response.Code, response.Body.String())
		}
		if (query == "" || query == "   " || query == "ًُ" || query == "ـــ") && response.Body.String() != absent.Body.String() {
			t.Errorf("empty-normalized query %q differs from absent query", query)
		}
	}
	clamped := publicCatalogRequest(r, http.MethodGet, "/api/v1/catalog/courses?page=0&page_size=1000&q=replacement")
	if clamped.Code != http.StatusOK || !strings.Contains(clamped.Body.String(), `"page":1`) || !strings.Contains(clamped.Body.String(), `"page_size":100`) {
		t.Errorf("clamped search pagination = %d %s", clamped.Code, clamped.Body.String())
	}
}

func TestPublicCatalogSearchOwnershipJoinUsesTrigramIndex(t *testing.T) {
	freshSchema(t)
	pool, ctx := pool(t)
	seedPublicCatalogOwner(t, pool, ctx)
	courseID := seedPublicCatalogCourse(t, pool, ctx, publicCourseVisibility{lifecycle: "PUBLISHED"})
	setSearchRevision(t, pool, ctx, courseID, "عنوان", "Indexed Ownership Needle", "", "")

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquiring explain connection: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SET enable_seqscan = off`); err != nil {
		t.Fatalf("preferring trigram index: %v", err)
	}
	if _, err := conn.Exec(ctx, `SET enable_indexscan = off`); err != nil {
		t.Fatalf("preferring trigram bitmap scan: %v", err)
	}
	rows, err := conn.Query(ctx, `EXPLAIN (COSTS OFF)
		SELECT c.id
		FROM courses c
		JOIN course_revisions cr ON cr.course_id = c.id
		WHERE `+catalogpublic.PublishedOnly("c", "cr")+`
			AND cr.search_text LIKE '%' || catalog_normalize_ar($1) || '%'`, "ownership needle")
	if err != nil {
		t.Fatalf("explaining ownership search query: %v", err)
	}
	defer rows.Close()
	plan := make([]string, 0)
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scanning explain line: %v", err)
		}
		plan = append(plan, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading explain plan: %v", err)
	}
	if !strings.Contains(strings.Join(plan, "\n"), "course_revisions_search_text_trgm_idx") {
		t.Fatalf("ownership search query omitted trigram index:\n%s", strings.Join(plan, "\n"))
	}
}

func TestPublicCatalogSearchDoesNotExposeHiddenCourses(t *testing.T) {
	freshSchema(t)
	pool, ctx := pool(t)
	seedPublicCatalogOwner(t, pool, ctx)
	hiddenCourseID := seedPublicCatalogCourse(t, pool, ctx, publicCourseVisibility{lifecycle: "DRAFT"})
	setSearchRevision(t, pool, ctx, hiddenCourseID, "عنوان مسودة مخفي", "Hidden Draft Search Title", "", "")
	assertStoredSearchText(t, pool, ctx, hiddenCourseID, "عنوان مسودة مخفي")
	assertSearchOmits(t, publicSearchRequest(buildPublicCatalogRouter(t, pool), "عنوان مسودة مخفي"), hiddenCourseID, "draft course")
}

func publicSearchRequest(r *gin.Engine, query string) *httptest.ResponseRecorder {
	return publicCatalogRequest(r, http.MethodGet, "/api/v1/catalog/courses?q="+url.QueryEscape(query))
}

func assertSearchContains(t *testing.T, response *httptest.ResponseRecorder, courseID, query string) {
	t.Helper()
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), courseID) {
		t.Fatalf("search %q = %d %s, want public course %s", query, response.Code, response.Body.String(), courseID)
	}
	for _, prohibited := range []string{"section_price", "price_minor_units", `"email"`, "owner_account", "reviewed_by", "resources", "lab_materials"} {
		if strings.Contains(strings.ToLower(response.Body.String()), prohibited) {
			t.Fatalf("search %q exposes prohibited field %q: %s", query, prohibited, response.Body.String())
		}
	}
}

func assertSearchOmits(t *testing.T, response *httptest.ResponseRecorder, courseID, reason string) {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("search %s status = %d: %s", reason, response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), courseID) {
		t.Fatalf("search %s exposed course %s: %s", reason, courseID, response.Body.String())
	}
}

func setSearchRevision(t *testing.T, pool *pgxpool.Pool, ctx context.Context, courseID, titleAr, titleEn, descriptionAr, descriptionEn string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE course_revisions SET title_ar = $1, title_en = $2, description_ar = $3, description_en = $4 WHERE course_id = $5::uuid`, titleAr, titleEn, descriptionAr, descriptionEn, courseID); err != nil {
		t.Fatalf("setting search revision content: %v", err)
	}
}

func seedSearchJoinedFields(t *testing.T, pool *pgxpool.Pool, ctx context.Context, courseID string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE accounts SET display_name = 'Search Instructor' WHERE id = '11111111-1111-1111-1111-111111111111'`); err != nil {
		t.Fatalf("setting searchable instructor: %v", err)
	}
	var majorID, subjectID string
	if err := pool.QueryRow(ctx, `INSERT INTO taxonomy_terms (kind, label_ar, label_en) VALUES ('MAJOR', 'علم الأحياء', 'Life Science') RETURNING id::text`).Scan(&majorID); err != nil {
		t.Fatalf("creating searchable major: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO taxonomy_terms (kind, label_ar, label_en, academic_code) VALUES ('SUBJECT', 'أحياء', 'Biology', 'BIO-101') RETURNING id::text`).Scan(&subjectID); err != nil {
		t.Fatalf("creating searchable subject: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE course_revisions SET major_term_id = $1::uuid, subject_term_id = $2::uuid WHERE course_id = $3::uuid`, majorID, subjectID, courseID); err != nil {
		t.Fatalf("assigning searchable taxonomy: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE taxonomy_terms SET retired_at = now() WHERE id IN ($1::uuid, $2::uuid)`, majorID, subjectID); err != nil {
		t.Fatalf("retiring searchable taxonomy: %v", err)
	}
}

func seedSearchRevision(t *testing.T, pool *pgxpool.Pool, ctx context.Context, courseID string, number int, state, titleAr, titleEn string) string {
	t.Helper()
	var revisionID string
	if err := pool.QueryRow(ctx, `INSERT INTO course_revisions (course_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::revision_state, $3, $4, $5) RETURNING id::text`, courseID, state, number, titleAr, titleEn).Scan(&revisionID); err != nil {
		t.Fatalf("creating %s search revision: %v", state, err)
	}
	return revisionID
}

func assertStoredSearchText(t *testing.T, pool *pgxpool.Pool, ctx context.Context, courseID, query string) {
	t.Helper()
	var searchable bool
	if err := pool.QueryRow(ctx, `SELECT search_text LIKE '%' || catalog_normalize_ar($1) || '%' FROM course_revisions WHERE course_id = $2::uuid`, query, courseID).Scan(&searchable); err != nil {
		t.Fatalf("checking stored hidden search text: %v", err)
	}
	if !searchable {
		t.Fatalf("hidden course %s lacks searchable text for %q", courseID, query)
	}
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

func seedPricedPublicSection(t *testing.T, pool *pgxpool.Pool, ctx context.Context, courseID string) {
	t.Helper()
	var identityID string
	if err := pool.QueryRow(ctx, `INSERT INTO course_section_identities (course_id) VALUES ($1::uuid) RETURNING id::text`, courseID).Scan(&identityID); err != nil {
		t.Fatalf("creating section identity: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO course_sections (revision_id, course_id, section_identity_id, title_ar, title_en, position, price_minor_units) VALUES ((SELECT live_revision_id FROM courses WHERE id = $1::uuid), $1::uuid, $2::uuid, 'قسم مدفوع', 'Priced section', 0, 10000)`, courseID, identityID); err != nil {
		t.Fatalf("creating priced public section: %v", err)
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
