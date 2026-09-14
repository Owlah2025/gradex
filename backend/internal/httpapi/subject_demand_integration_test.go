//go:build integration

package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// D-106: an unserved Subject is publicly discoverable, and a Student may
// register demand against it.
//
// The point of every assertion here is the boundary between what Gradex *knows*
// (academic identity) and what Gradex *sells* (a published Course). A Subject
// must be findable without being a product, and a demand signal must be
// attributable without being an entitlement.
//
// The demand journey deliberately never reads a Subject identifier out of the
// fixture. It discovers the Subject through the public API and raises demand
// with the identifier that response carried, because that is the only path a
// real client has: if discovery does not publish a usable identifier, the
// journey is impossible no matter what the database contains.

type subjectListingResponse struct {
	SubjectID       string `json:"subject_id"`
	Value           string `json:"value"`
	Code            string `json:"code"`
	TitleAr         string `json:"title_ar"`
	TitleEn         string `json:"title_en"`
	InstitutionSlug string `json:"institution_slug"`
	Served          bool   `json:"served"`
	Courses         []struct {
		Slug  string `json:"slug"`
		Title string `json:"title"`
	} `json:"courses"`
}

type subjectPageResponse struct {
	Items []subjectListingResponse `json:"items"`
	Total int                      `json:"total"`
}

// browseSubjects reads the public catalogue exactly as a client would.
func browseSubjects(t *testing.T, env *academicTestEnv, query string) subjectPageResponse {
	t.Helper()
	status, raw := env.call(t, http.MethodGet, "/api/v1/catalog/subjects?"+query, "", nil)
	if status != http.StatusOK {
		t.Fatalf("browsing subjects status = %d, want 200; body %s", status, raw)
	}
	var page subjectPageResponse
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("decoding subjects: %v", err)
	}
	return page
}

// seedDemandInstitution creates one Institution and its Subjects. It returns the
// Institution slug only: a Subject identifier is something the journey has to
// obtain from the public API, never from the fixture.
func seedDemandInstitution(t *testing.T, env *academicTestEnv, codes ...string) string {
	t.Helper()
	ctx := t.Context()
	const slug = "demand-university"

	var institutionID string
	if err := env.pool.QueryRow(ctx, `
		INSERT INTO institutions (country_code, slug, name_ar, name_en)
		VALUES ('KW', $1, 'جامعة الطلب', 'Demand University')
		RETURNING id::text`, slug).Scan(&institutionID); err != nil {
		t.Fatalf("seeding institution: %v", err)
	}
	for _, code := range codes {
		if _, err := env.pool.Exec(ctx, `
			INSERT INTO subjects (institution_id, official_code, title_ar, title_en)
			VALUES ($1::uuid, $2, 'مادة ' || $2, 'Subject ' || $2)`,
			institutionID, code); err != nil {
			t.Fatalf("seeding subject %s: %v", code, err)
		}
	}
	return slug
}

func TestUnservedSubjectIsDiscoverableAndCarriesDemand(t *testing.T) {
	env := setupAcademicAPIServer(t)
	institutionSlug := seedDemandInstitution(t, env, "DMD 101")

	// Discovered once, through the public API, and reused by every subtest
	// below. This is the identifier a real client would hold.
	var discovered subjectListingResponse

	t.Run("an anonymous visitor finds the Subject and sees it is unserved", func(t *testing.T) {
		page := browseSubjects(t, env, "institution="+institutionSlug)
		if page.Total != 1 || len(page.Items) != 1 {
			t.Fatalf("subjects = %+v; want exactly the seeded one", page.Items)
		}
		discovered = page.Items[0]

		// A Subject with no Course must still appear. This is the assertion
		// that separates a Subject list from a Course filter.
		if discovered.Served {
			t.Error("a Subject with no published Course must report served=false")
		}
		if len(discovered.Courses) != 0 {
			t.Errorf("an unserved Subject carried %d courses", len(discovered.Courses))
		}
		if discovered.Code != "DMD 101" || discovered.TitleEn != "Subject DMD 101" {
			t.Errorf("subject = %+v; want the seeded code and title", discovered)
		}
		if discovered.TitleAr == "" {
			t.Error("the Arabic title must be present so the caller can render either language")
		}
		// Without this the demand endpoint is unreachable from discovery: the
		// public Value is the official code, which the write path does not take.
		if discovered.SubjectID == "" {
			t.Fatal("discovery published no subject_id; a client could not register demand")
		}
		if discovered.SubjectID == discovered.Value {
			t.Error("a coded Subject's Value must stay the shareable code, distinct from its identifier")
		}
	})

	t.Run("a Subject search normalizes the code a Student would type", func(t *testing.T) {
		if page := browseSubjects(t, env, "q=dmd101"); page.Total != 1 {
			t.Fatalf("searching \"dmd101\" found %d subjects, want the one coded \"DMD 101\"", page.Total)
		}
	})

	t.Run("an anonymous visitor cannot register demand", func(t *testing.T) {
		status, _ := env.call(t, http.MethodPost, "/api/v1/me/subject-demand", "",
			map[string]any{"subject_id": discovered.SubjectID})
		if status == http.StatusCreated {
			t.Fatal("demand was recorded without a session; D-106 §6 requires an authenticated Student")
		}
	})

	t.Run("a Student registers demand with the discovered identifier", func(t *testing.T) {
		status, raw := env.call(t, http.MethodPost, "/api/v1/me/subject-demand", env.studentToken,
			map[string]any{"subject_id": discovered.SubjectID, "note": "I take this in the fall"})
		if status != http.StatusCreated {
			t.Fatalf("raising demand status = %d, want 201; body %s", status, raw)
		}

		// Asking twice is a conflict, not a second signal: a demand count is a
		// count of Students, not of clicks.
		repeat, _ := env.call(t, http.MethodPost, "/api/v1/me/subject-demand", env.studentToken,
			map[string]any{"subject_id": discovered.SubjectID})
		if repeat != http.StatusConflict {
			t.Fatalf("raising demand twice status = %d, want 409", repeat)
		}
	})

	t.Run("a well-formed but unknown Subject is not found", func(t *testing.T) {
		status, _ := env.call(t, http.MethodPost, "/api/v1/me/subject-demand", env.studentToken,
			map[string]any{"subject_id": "00000000-0000-0000-0000-0000000000ff"})
		if status != http.StatusNotFound {
			t.Fatalf("demand for an unknown Subject status = %d, want 404", status)
		}
	})

	// A typo in a request body is a client error. Unguarded it reaches a ::uuid
	// cast and returns 500, which misreports whose fault it is and buries real
	// server faults in the same signal.
	t.Run("a malformed Subject identifier is a client error, never a 500", func(t *testing.T) {
		for _, malformed := range []string{"not-a-uuid", "DMD 101", "123", "'; DROP TABLE subjects;--"} {
			status, raw := env.call(t, http.MethodPost, "/api/v1/me/subject-demand",
				env.studentToken, map[string]any{"subject_id": malformed})
			// 422, not 400: the request parsed, a field value is invalid. That
			// is this API's convention for every other validation failure, and
			// the point of the assertion is that it is not 500.
			if status != http.StatusUnprocessableEntity {
				t.Errorf("raising demand for %q status = %d, want 422; body %s", malformed, status, raw)
			}
			// Asserted exactly, not merely "not 500": a 404 or a 204 here would
			// also pass a not-500 check while meaning the guard never ran and
			// the path silently accepted a value it cannot address.
			status, raw = env.call(t, http.MethodDelete,
				"/api/v1/me/subject-demand/"+malformed, env.studentToken, nil)
			if status != http.StatusUnprocessableEntity {
				t.Errorf("withdrawing %q status = %d, want 422; body %s", malformed, status, raw)
			}
		}
	})

	t.Run("the Student sees only their own signals", func(t *testing.T) {
		status, raw := env.call(t, http.MethodGet, "/api/v1/me/subject-demand", env.studentToken, nil)
		if status != http.StatusOK {
			t.Fatalf("listing own demand status = %d; body %s", status, raw)
		}
		var body struct {
			Items []struct {
				SubjectID string `json:"subject_id"`
				Note      string `json:"note"`
			} `json:"items"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decoding own demand: %v", err)
		}
		if len(body.Items) != 1 || body.Items[0].SubjectID != discovered.SubjectID {
			t.Fatalf("own demand = %s; want exactly the one raised", raw)
		}
		if body.Items[0].Note != "I take this in the fall" {
			t.Errorf("note = %q; the Student's own context must round-trip", body.Items[0].Note)
		}
	})

	t.Run("demand grants no access authority", func(t *testing.T) {
		// The Subject is still unserved after a signal: demand creates no
		// Course, no entitlement, and no storefront presence.
		page := browseSubjects(t, env, "institution="+institutionSlug)
		if len(page.Items) != 1 || page.Items[0].Served {
			t.Fatalf("a demand signal changed the Subject's served status: %+v", page.Items)
		}
	})

	t.Run("Admin reads aggregate demand; Student and Instructor cannot", func(t *testing.T) {
		status, raw := env.call(t, http.MethodGet, "/api/v1/admin/academic/subject-demand",
			env.adminToken, nil)
		if status != http.StatusOK {
			t.Fatalf("admin demand status = %d, want 200; body %s", status, raw)
		}
		var body struct {
			Items []struct {
				SubjectID         string `json:"subject_id"`
				InstitutionNameAr string `json:"institution_name_ar"`
				InstitutionNameEn string `json:"institution_name_en"`
				Students          int    `json:"students"`
				Served            bool   `json:"served"`
			} `json:"items"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decoding admin demand: %v", err)
		}
		if len(body.Items) != 1 || body.Items[0].SubjectID != discovered.SubjectID {
			t.Fatalf("admin demand = %s; want the one Subject with demand", raw)
		}
		if body.Items[0].Students != 1 {
			t.Errorf("students = %d, want 1", body.Items[0].Students)
		}
		// Both names ship so Admin can read the aggregate in either language
		// without re-requesting or keeping a client-side institution-name map.
		if body.Items[0].InstitutionNameAr != "جامعة الطلب" {
			t.Errorf("institution_name_ar = %q, want the seeded Arabic name",
				body.Items[0].InstitutionNameAr)
		}
		if body.Items[0].InstitutionNameEn != "Demand University" {
			t.Errorf("institution_name_en = %q, want the seeded English name",
				body.Items[0].InstitutionNameEn)
		}
		if body.Items[0].Served {
			t.Error("the Subject has no published Course and must report served=false")
		}

		for name, token := range map[string]string{
			"student": env.studentToken, "instructor": env.instructorToken, "anonymous": "",
		} {
			status, _ := env.call(t, http.MethodGet, "/api/v1/admin/academic/subject-demand", token, nil)
			if status == http.StatusOK {
				t.Errorf("%s read aggregate demand; it is Admin-only", name)
			}
		}
	})

	t.Run("withdrawing removes the signal and allows re-raising", func(t *testing.T) {
		status, raw := env.call(t, http.MethodDelete,
			"/api/v1/me/subject-demand/"+discovered.SubjectID, env.studentToken, nil)
		if status != http.StatusNoContent {
			t.Fatalf("withdrawing status = %d, want 204; body %s", status, raw)
		}
		// Withdrawing again finds nothing live to withdraw.
		repeat, _ := env.call(t, http.MethodDelete,
			"/api/v1/me/subject-demand/"+discovered.SubjectID, env.studentToken, nil)
		if repeat != http.StatusNotFound {
			t.Fatalf("withdrawing twice status = %d, want 404", repeat)
		}
		// A Student who changes their mind is not barred permanently.
		again, rawAgain := env.call(t, http.MethodPost, "/api/v1/me/subject-demand",
			env.studentToken, map[string]any{"subject_id": discovered.SubjectID})
		if again != http.StatusCreated {
			t.Fatalf("re-raising after withdrawal status = %d, want 201; body %s", again, rawAgain)
		}
	})
}

// publishCourseForSubject attaches a published Course to one Subject and returns
// its generated slug.
//
// Publication is the last step, not the first: courses_published_has_live_revision
// refuses a PUBLISHED Course that has no live revision.
func publishCourseForSubject(t *testing.T, env *academicTestEnv, subjectID string) string {
	t.Helper()
	ctx := t.Context()

	var instructorID string
	if err := env.pool.QueryRow(ctx, `
		INSERT INTO accounts (normalized_email, email, role, status, display_name)
		VALUES ('served-inst@example.com', 'served-inst@example.com', 'INSTRUCTOR', 'ACTIVE', 'Served Instructor')
		RETURNING id::text`).Scan(&instructorID); err != nil {
		t.Fatalf("seeding instructor: %v", err)
	}

	var courseID, slug string
	if err := env.pool.QueryRow(ctx, `
		INSERT INTO courses (owner_account_id, lifecycle, classification_model, institution_id, subject_id)
		SELECT $1::uuid, 'DRAFT', 'ACADEMIC_CATALOG', s.institution_id, s.id
		FROM subjects s WHERE s.id = $2::uuid
		RETURNING id::text, slug`, instructorID, subjectID).Scan(&courseID, &slug); err != nil {
		t.Fatalf("seeding course: %v", err)
	}

	var revisionID string
	if err := env.pool.QueryRow(ctx, `
		INSERT INTO course_revisions (course_id, state, revision_number, title_ar, title_en, description_ar, description_en)
		VALUES ($1::uuid, 'APPROVED', 1, 'كورس مخدوم', 'Served Course', 'وصف', 'Description')
		RETURNING id::text`, courseID).Scan(&revisionID); err != nil {
		t.Fatalf("seeding revision: %v", err)
	}
	if _, err := env.pool.Exec(ctx, `
		UPDATE courses SET lifecycle = 'PUBLISHED', live_revision_id = $2::uuid
		WHERE id = $1::uuid`, courseID, revisionID); err != nil {
		t.Fatalf("publishing course: %v", err)
	}
	return slug
}

// setCourseSuspension toggles the suspension that PublishedOnly reads. A
// suspended Course must carry a reason, which courses_suspension_check enforces.
func setCourseSuspension(t *testing.T, env *academicTestEnv, slug string, suspended bool) {
	t.Helper()
	statement := `UPDATE courses SET access_suspended_at = NULL, access_suspension_reason = NULL WHERE slug = $1`
	if suspended {
		statement = `UPDATE courses SET access_suspended_at = now(), access_suspension_reason = 'integration-test' WHERE slug = $1`
	}
	if _, err := env.pool.Exec(t.Context(), statement, slug); err != nil {
		t.Fatalf("setting suspension on %s: %v", slug, err)
	}
}

// A Subject's served status is derived from Courses at read time, never stored.
// This walks the whole lifecycle so a future change that caches the flag fails
// here rather than in production, where the catalogue would keep advertising a
// Course that publication state had already withdrawn.
func TestSubjectServedStatusFollowsCoursePublication(t *testing.T) {
	env := setupAcademicAPIServer(t)
	institutionSlug := seedDemandInstitution(t, env, "SRV 100", "UNS 200")

	find := func(t *testing.T, code string) subjectListingResponse {
		t.Helper()
		for _, item := range browseSubjects(t, env, "institution="+institutionSlug).Items {
			if item.Code == code {
				return item
			}
		}
		t.Fatalf("subject %s not found in the public catalogue", code)
		return subjectListingResponse{}
	}

	served := find(t, "SRV 100")
	if served.Served {
		t.Fatal("the Subject has no Course yet and must start unserved")
	}

	var courseSlug string
	t.Run("publishing a Course makes its Subject served and links the Course", func(t *testing.T) {
		courseSlug = publishCourseForSubject(t, env, served.SubjectID)

		item := find(t, "SRV 100")
		if !item.Served {
			t.Fatal("a Subject taught by a published Course must report served=true")
		}
		if len(item.Courses) != 1 {
			t.Fatalf("served Subject carried %d courses, want 1", len(item.Courses))
		}
		// The slug is what the caller links to, so a wrong one sends the
		// Student to the wrong Course or to nothing at all.
		if item.Courses[0].Slug != courseSlug {
			t.Errorf("course slug = %q, want %q", item.Courses[0].Slug, courseSlug)
		}
		// The public catalogue answers in Arabic unless Accept-Language starts
		// with "en", and the test client sends no such header.
		if item.Courses[0].Title != "كورس مخدوم" {
			t.Errorf("course title = %q, want the revision's Arabic title", item.Courses[0].Title)
		}
	})

	t.Run("served Subjects sort before unserved ones", func(t *testing.T) {
		items := browseSubjects(t, env, "institution="+institutionSlug).Items
		if len(items) != 2 {
			t.Fatalf("got %d subjects, want 2", len(items))
		}
		// The contract: within an Institution, a visitor meets what Gradex
		// actually teaches before what it does not. SRV 100 sorts first only
		// because it is served -- by code alone, SRV would follow UNS.
		if !items[0].Served || items[1].Served {
			t.Fatalf("ordering = [%s served=%v, %s served=%v]; served must come first",
				items[0].Code, items[0].Served, items[1].Code, items[1].Served)
		}
		if items[0].Code != "SRV 100" || items[1].Code != "UNS 200" {
			t.Fatalf("ordering = [%s, %s]; want the served SRV 100 ahead of UNS 200",
				items[0].Code, items[1].Code)
		}
	})

	t.Run("the availability filter splits served from unserved", func(t *testing.T) {
		servedPage := browseSubjects(t, env, "institution="+institutionSlug+"&availability=served")
		if servedPage.Total != 1 || servedPage.Items[0].Code != "SRV 100" {
			t.Fatalf("availability=served returned %+v; want only SRV 100", servedPage.Items)
		}
		unservedPage := browseSubjects(t, env, "institution="+institutionSlug+"&availability=unserved")
		if unservedPage.Total != 1 || unservedPage.Items[0].Code != "UNS 200" {
			t.Fatalf("availability=unserved returned %+v; want only UNS 200", unservedPage.Items)
		}
	})

	t.Run("suspending the Course returns the Subject to unserved", func(t *testing.T) {
		setCourseSuspension(t, env, courseSlug, true)
		item := find(t, "SRV 100")
		if item.Served {
			t.Fatal("a suspended Course must not keep its Subject served")
		}
		if len(item.Courses) != 0 {
			t.Errorf("a suspended Course was still linked: %+v", item.Courses)
		}
	})

	t.Run("unsuspending restores it", func(t *testing.T) {
		setCourseSuspension(t, env, courseSlug, false)
		item := find(t, "SRV 100")
		if !item.Served || len(item.Courses) != 1 {
			t.Fatalf("unsuspending did not restore served status: %+v", item)
		}
	})

	t.Run("retiring the Course returns the Subject to unserved", func(t *testing.T) {
		if _, err := env.pool.Exec(t.Context(),
			`UPDATE courses SET retired_at = now() WHERE slug = $1`, courseSlug); err != nil {
			t.Fatalf("retiring course: %v", err)
		}
		if item := find(t, "SRV 100"); item.Served {
			t.Fatal("a retired Course must not keep its Subject served")
		}
	})

	t.Run("Admin demand aggregation agrees with the catalogue on served", func(t *testing.T) {
		// The aggregation composes the same PublishedOnly predicate, so a
		// retired Course must read as unserved there too.
		if _, err := env.pool.Exec(t.Context(),
			`UPDATE courses SET retired_at = NULL WHERE slug = $1`, courseSlug); err != nil {
			t.Fatalf("restoring course: %v", err)
		}
		status, raw := env.call(t, http.MethodPost, "/api/v1/me/subject-demand", env.studentToken,
			map[string]any{"subject_id": served.SubjectID})
		if status != http.StatusCreated {
			t.Fatalf("raising demand status = %d; body %s", status, raw)
		}
		status, raw = env.call(t, http.MethodGet, "/api/v1/admin/academic/subject-demand",
			env.adminToken, nil)
		if status != http.StatusOK {
			t.Fatalf("admin demand status = %d; body %s", status, raw)
		}
		var body struct {
			Items []struct {
				SubjectID string `json:"subject_id"`
				Served    bool   `json:"served"`
			} `json:"items"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decoding admin demand: %v", err)
		}
		if len(body.Items) != 1 || body.Items[0].SubjectID != served.SubjectID {
			t.Fatalf("admin demand = %s; want the Subject just asked for", raw)
		}
		if !body.Items[0].Served {
			t.Error("the Subject has a live published Course; Admin must see served=true")
		}
	})
}
