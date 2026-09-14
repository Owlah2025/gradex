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

type subjectPageResponse struct {
	Items []struct {
		Value           string `json:"value"`
		Code            string `json:"code"`
		TitleAr         string `json:"title_ar"`
		TitleEn         string `json:"title_en"`
		InstitutionSlug string `json:"institution_slug"`
		Served          bool   `json:"served"`
		Courses         []struct {
			Slug string `json:"slug"`
		} `json:"courses"`
	} `json:"items"`
	Total int `json:"total"`
}

// seedDemandSubject creates one Institution and one Subject directly, because
// this test is about discovery and demand, not about the import path.
func seedDemandSubject(t *testing.T, env *academicTestEnv) (subjectID, institutionSlug string) {
	t.Helper()
	ctx := t.Context()
	institutionSlug = "demand-university"

	var institutionID string
	if err := env.pool.QueryRow(ctx, `
		INSERT INTO institutions (country_code, slug, name_ar, name_en)
		VALUES ('KW', $1, 'جامعة الطلب', 'Demand University')
		RETURNING id::text`, institutionSlug).Scan(&institutionID); err != nil {
		t.Fatalf("seeding institution: %v", err)
	}
	if err := env.pool.QueryRow(ctx, `
		INSERT INTO subjects (institution_id, official_code, title_ar, title_en)
		VALUES ($1::uuid, 'DMD 101', 'مادة غير مخدومة', 'Unserved Subject')
		RETURNING id::text`, institutionID).Scan(&subjectID); err != nil {
		t.Fatalf("seeding subject: %v", err)
	}
	return subjectID, institutionSlug
}

func TestUnservedSubjectIsDiscoverableAndCarriesDemand(t *testing.T) {
	env := setupAcademicAPIServer(t)
	subjectID, institutionSlug := seedDemandSubject(t, env)

	t.Run("an anonymous visitor finds the Subject and sees it is unserved", func(t *testing.T) {
		status, raw := env.call(t, http.MethodGet,
			"/api/v1/catalog/subjects?institution="+institutionSlug, "", nil)
		if status != http.StatusOK {
			t.Fatalf("browsing subjects status = %d, want 200; body %s", status, raw)
		}
		var page subjectPageResponse
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatalf("decoding subjects: %v", err)
		}
		if page.Total != 1 || len(page.Items) != 1 {
			t.Fatalf("subjects = %s; want exactly the seeded one", raw)
		}
		item := page.Items[0]
		// A Subject with no Course must still appear. This is the assertion
		// that separates a Subject list from a Course filter.
		if item.Served {
			t.Error("a Subject with no published Course must report served=false")
		}
		if len(item.Courses) != 0 {
			t.Errorf("an unserved Subject carried %d courses", len(item.Courses))
		}
		if item.Code != "DMD 101" || item.TitleEn != "Unserved Subject" {
			t.Errorf("subject = %+v; want the seeded code and title", item)
		}
		// Both languages reach the client so the caller renders either.
		if item.TitleAr == "" {
			t.Error("the Arabic title must be present")
		}
	})

	t.Run("a Subject search normalizes the code a Student would type", func(t *testing.T) {
		status, raw := env.call(t, http.MethodGet, "/api/v1/catalog/subjects?q=dmd101", "", nil)
		if status != http.StatusOK {
			t.Fatalf("searching subjects status = %d; body %s", status, raw)
		}
		var page subjectPageResponse
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatalf("decoding search: %v", err)
		}
		if page.Total != 1 {
			t.Fatalf("searching \"dmd101\" found %d subjects, want the one coded \"DMD 101\"", page.Total)
		}
	})

	t.Run("an anonymous visitor cannot register demand", func(t *testing.T) {
		status, _ := env.call(t, http.MethodPost, "/api/v1/me/subject-demand", "",
			map[string]any{"subject_id": subjectID})
		if status == http.StatusCreated {
			t.Fatal("demand was recorded without a session; D-106 §6 requires an authenticated Student")
		}
	})

	t.Run("a Student registers demand once", func(t *testing.T) {
		status, raw := env.call(t, http.MethodPost, "/api/v1/me/subject-demand", env.studentToken,
			map[string]any{"subject_id": subjectID, "note": "I take this in the fall"})
		if status != http.StatusCreated {
			t.Fatalf("raising demand status = %d, want 201; body %s", status, raw)
		}

		// Asking twice is a conflict, not a second signal: a demand count is a
		// count of Students, not of clicks.
		repeat, _ := env.call(t, http.MethodPost, "/api/v1/me/subject-demand", env.studentToken,
			map[string]any{"subject_id": subjectID})
		if repeat != http.StatusConflict {
			t.Fatalf("raising demand twice status = %d, want 409", repeat)
		}
	})

	t.Run("demand for an unknown Subject is not found", func(t *testing.T) {
		status, _ := env.call(t, http.MethodPost, "/api/v1/me/subject-demand", env.studentToken,
			map[string]any{"subject_id": "00000000-0000-0000-0000-0000000000ff"})
		if status != http.StatusNotFound {
			t.Fatalf("demand for an unknown Subject status = %d, want 404", status)
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
		if len(body.Items) != 1 || body.Items[0].SubjectID != subjectID {
			t.Fatalf("own demand = %s; want exactly the one raised", raw)
		}
		if body.Items[0].Note != "I take this in the fall" {
			t.Errorf("note = %q; the Student's own context must round-trip", body.Items[0].Note)
		}
	})

	t.Run("demand grants no access authority", func(t *testing.T) {
		// The Subject is still unserved after a signal: demand creates no
		// Course, no entitlement, and no storefront presence.
		status, raw := env.call(t, http.MethodGet,
			"/api/v1/catalog/subjects?institution="+institutionSlug, "", nil)
		if status != http.StatusOK {
			t.Fatalf("re-browsing status = %d; body %s", status, raw)
		}
		var page subjectPageResponse
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if len(page.Items) != 1 || page.Items[0].Served {
			t.Fatalf("a demand signal changed the Subject's served status: %s", raw)
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
				SubjectID string `json:"subject_id"`
				Students  int    `json:"students"`
				Served    bool   `json:"served"`
			} `json:"items"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decoding admin demand: %v", err)
		}
		if len(body.Items) != 1 || body.Items[0].SubjectID != subjectID {
			t.Fatalf("admin demand = %s; want the one Subject with demand", raw)
		}
		if body.Items[0].Students != 1 {
			t.Errorf("students = %d, want 1", body.Items[0].Students)
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
			"/api/v1/me/subject-demand/"+subjectID, env.studentToken, nil)
		if status != http.StatusNoContent {
			t.Fatalf("withdrawing status = %d, want 204; body %s", status, raw)
		}
		// Withdrawing again finds nothing live to withdraw.
		repeat, _ := env.call(t, http.MethodDelete,
			"/api/v1/me/subject-demand/"+subjectID, env.studentToken, nil)
		if repeat != http.StatusNotFound {
			t.Fatalf("withdrawing twice status = %d, want 404", repeat)
		}
		// A Student who changes their mind is not barred permanently.
		again, rawAgain := env.call(t, http.MethodPost, "/api/v1/me/subject-demand",
			env.studentToken, map[string]any{"subject_id": subjectID})
		if again != http.StatusCreated {
			t.Fatalf("re-raising after withdrawal status = %d, want 201; body %s", again, rawAgain)
		}
	})
}
