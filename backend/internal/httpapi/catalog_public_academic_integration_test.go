//go:build integration

package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// T6 — the public academic discovery HTTP contract.
//
// The repository tests own the query semantics. What is proven here is the
// wiring a browser actually depends on: that the query parameters reach the
// filters, that the option endpoints are anonymous and leak nothing, that no
// identifier reaches the response where a name belongs, and that a personalised
// ordering is not stored in the shared public cache.

type academicOptionsResponse struct {
	Items []struct {
		Slug          string `json:"slug"`
		Value         string `json:"value"`
		Code          string `json:"code"`
		NameAr        string `json:"name_ar"`
		NameEn        string `json:"name_en"`
		TitleAr       string `json:"title_ar"`
		TitleEn       string `json:"title_en"`
		CollegeNameEn string `json:"college_name_en"`
	} `json:"items"`
}

type publicCourseListResponse struct {
	Items []struct {
		ID    string `json:"id"`
		Slug  string `json:"slug"`
		Title string `json:"title"`
	} `json:"items"`
	Total int `json:"total"`
}

func TestT6PublicAcademicFiltersAndOptionsOverHTTP(t *testing.T) {
	freshSchema(t)
	pool, ctx := pool(t)
	seedPublicCatalogOwner(t, pool, ctx)

	scan := func(dest *string, sql string, args ...any) {
		t.Helper()
		if err := pool.QueryRow(ctx, sql, args...).Scan(dest); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}

	var institution, college, engineering, science, subject, engCurriculum, sciCurriculum string
	scan(&institution, `INSERT INTO institutions (country_code, slug, name_ar, name_en)
		VALUES ('KW', 'kuwait-university', 'جامعة الكويت', 'Kuwait University') RETURNING id::text`)
	scan(&college, `INSERT INTO academic_units (institution_id, kind, slug, name_ar, name_en)
		VALUES ($1::uuid, 'COLLEGE', 'engineering', 'كلية الهندسة', 'College of Engineering')
		RETURNING id::text`, institution)
	scan(&engineering, `INSERT INTO programs (institution_id, owning_unit_id, slug, name_ar, name_en, degree_kind)
		VALUES ($1::uuid, $2::uuid, 'computer-engineering', 'هندسة الحاسوب', 'Computer Engineering', 'BSC')
		RETURNING id::text`, institution, college)
	scan(&science, `INSERT INTO programs (institution_id, slug, name_ar, name_en, degree_kind)
		VALUES ($1::uuid, 'computer-science', 'علوم الحاسوب', 'Computer Science', 'BSC')
		RETURNING id::text`, institution)
	scan(&subject, `INSERT INTO subjects (institution_id, official_code, title_ar, title_en)
		VALUES ($1::uuid, '0418-201', 'هياكل البيانات', 'Data Structures & Algorithms')
		RETURNING id::text`, institution)
	scan(&engCurriculum, `INSERT INTO curricula (program_id, institution_id, version_label, status)
		VALUES ($1::uuid, $2::uuid, '2024', 'ACTIVE') RETURNING id::text`, engineering, institution)
	scan(&sciCurriculum, `INSERT INTO curricula (program_id, institution_id, version_label, status)
		VALUES ($1::uuid, $2::uuid, '2024', 'ACTIVE') RETURNING id::text`, science, institution)
	for _, curriculum := range []string{engCurriculum, sciCurriculum} {
		exec(`INSERT INTO curriculum_subjects (curriculum_id, subject_id, institution_id, requirement_kind)
			VALUES ($1::uuid, $2::uuid, $3::uuid, 'MAJOR_CORE')`, curriculum, subject, institution)
	}

	published := seedPublicCatalogCourse(t, pool, ctx, publicCourseVisibility{lifecycle: "PUBLISHED"})
	exec(`UPDATE courses SET classification_model = 'ACADEMIC_CATALOG',
	        institution_id = $1::uuid, subject_id = $2::uuid WHERE id = $3::uuid`,
		institution, subject, published)

	r := buildPublicCatalogRouter(t, pool)

	// --- Option endpoints are anonymous and name things, not identify them ---
	institutions := publicCatalogRequest(r, http.MethodGet, "/api/v1/catalog/academic-options/institutions")
	if institutions.Code != http.StatusOK {
		t.Fatalf("institution options status = %d: %s", institutions.Code, institutions.Body.String())
	}
	var institutionOptions academicOptionsResponse
	if err := json.Unmarshal(institutions.Body.Bytes(), &institutionOptions); err != nil {
		t.Fatalf("decoding institution options: %v", err)
	}
	if len(institutionOptions.Items) != 1 || institutionOptions.Items[0].Slug != "kuwait-university" {
		t.Fatalf("institution options = %+v", institutionOptions.Items)
	}
	if institutionOptions.Items[0].NameAr == "" || institutionOptions.Items[0].NameEn == "" {
		t.Fatal("an option must carry both languages so the client never renders a raw value")
	}
	if strings.Contains(institutions.Body.String(), institution) {
		t.Fatal("the public institution option leaked its identifier")
	}

	programs := publicCatalogRequest(r, http.MethodGet,
		"/api/v1/catalog/academic-options/institutions/kuwait-university/programs")
	var programOptions academicOptionsResponse
	if err := json.Unmarshal(programs.Body.Bytes(), &programOptions); err != nil {
		t.Fatalf("decoding program options: %v", err)
	}
	if len(programOptions.Items) != 2 {
		t.Fatalf("program options = %+v, want both Programs", programOptions.Items)
	}
	if strings.Contains(programs.Body.String(), engineering) {
		t.Fatal("the public program option leaked its identifier")
	}

	subjects := publicCatalogRequest(r, http.MethodGet,
		"/api/v1/catalog/academic-options/institutions/kuwait-university/subjects?program=computer-engineering")
	var subjectOptions academicOptionsResponse
	if err := json.Unmarshal(subjects.Body.Bytes(), &subjectOptions); err != nil {
		t.Fatalf("decoding subject options: %v", err)
	}
	if len(subjectOptions.Items) != 1 || subjectOptions.Items[0].Value != "0418-201" {
		t.Fatalf("subject options = %+v; the filter value must be the shareable official code", subjectOptions.Items)
	}
	if subjectOptions.Items[0].TitleAr == "" || subjectOptions.Items[0].TitleEn == "" {
		t.Fatal("a Subject option must carry both languages")
	}

	// An unknown University is an empty list, not a failure: a stale shared link
	// must degrade to an ordinary empty state.
	unknown := publicCatalogRequest(r, http.MethodGet,
		"/api/v1/catalog/academic-options/institutions/no-such-university/programs")
	if unknown.Code != http.StatusOK || !strings.Contains(unknown.Body.String(), `"items":[]`) {
		t.Fatalf("unknown institution options = %d %s", unknown.Code, unknown.Body.String())
	}

	// --- The filters reach the query ---
	for name, path := range map[string]string{
		"institution": "/api/v1/catalog/courses?institution=kuwait-university",
		"program":     "/api/v1/catalog/courses?program=computer-engineering",
		"subject":     "/api/v1/catalog/courses?subject=0418-201",
		"combined": "/api/v1/catalog/courses?institution=kuwait-university" +
			"&program=computer-engineering&subject=0418-201",
	} {
		t.Run(name, func(t *testing.T) {
			response := publicCatalogRequest(r, http.MethodGet, path)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", response.Code, response.Body.String())
			}
			var list publicCourseListResponse
			if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
				t.Fatalf("decoding: %v", err)
			}
			if list.Total != 1 || len(list.Items) != 1 || list.Items[0].ID != published {
				t.Fatalf("%s filter returned %+v", name, list)
			}
			// A filtered response is still shared and cacheable.
			if response.Header().Get("Cache-Control") != publicCatalogCacheControl {
				t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
			}
		})
	}

	// A filter that matches nothing is an empty catalogue, never an error.
	for _, path := range []string{
		"/api/v1/catalog/courses?institution=no-such-university",
		"/api/v1/catalog/courses?program=no-such-program",
		"/api/v1/catalog/courses?subject=9999-999",
		"/api/v1/catalog/courses?institution=" + strings.Repeat("x", 500),
	} {
		response := publicCatalogRequest(r, http.MethodGet, path)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want an ordinary empty catalogue: %s",
				path, response.Code, response.Body.String())
		}
	}

	// --- A personalised ordering must not enter the shared cache ---
	ranked := publicCatalogRequest(r, http.MethodGet,
		"/api/v1/catalog/courses?relevant_to_program=computer-engineering")
	if ranked.Code != http.StatusOK {
		t.Fatalf("ranked status = %d: %s", ranked.Code, ranked.Body.String())
	}
	if got := ranked.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("ranked Cache-Control = %q; a personalised ordering must not be publicly cached", got)
	}
	var rankedList publicCourseListResponse
	if err := json.Unmarshal(ranked.Body.Bytes(), &rankedList); err != nil {
		t.Fatalf("decoding ranked: %v", err)
	}
	if rankedList.Total != 1 {
		t.Fatalf("ranking removed Courses: %+v", rankedList)
	}

	// --- Course detail names the audience, in the reader's language ---
	english := publicCatalogRequestWithLanguage(r, http.MethodGet, "/api/v1/catalog/courses/"+published, "en")
	if english.Code != http.StatusOK {
		t.Fatalf("english detail status = %d: %s", english.Code, english.Body.String())
	}
	if !strings.Contains(english.Body.String(), "Computer Engineering") ||
		!strings.Contains(english.Body.String(), "Computer Science") {
		t.Fatalf("english detail omitted the inferred Program audience: %s", english.Body.String())
	}
	detail := publicCatalogRequestWithLanguage(r, http.MethodGet, "/api/v1/catalog/courses/"+published, "ar")
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status = %d: %s", detail.Code, detail.Body.String())
	}
	body := detail.Body.String()
	if !strings.Contains(body, "هندسة الحاسوب") || !strings.Contains(body, "علوم الحاسوب") {
		t.Fatalf("arabic detail omitted the localized Program audience: %s", body)
	}
	if strings.Contains(body, "Computer Engineering") {
		t.Fatalf("arabic detail rendered an English academic name: %s", body)
	}
	for _, identifier := range []string{institution, subject, engineering, science, engCurriculum} {
		if strings.Contains(body, identifier) {
			t.Fatalf("public detail leaked an internal identifier %s: %s", identifier, body)
		}
	}
}
