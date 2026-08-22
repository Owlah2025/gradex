//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// T4-B — Instructor Subject-first authoring.
//
// The product change these prove: an Instructor names a university and a
// canonical Subject, and Gradex derives everything else. They never create a
// Subject, never choose a legacy Major term, and never handle an identifier.
//
// Scope. Audience customization (T4-C) and Subject requests (T4-D) are not
// implemented and are not exercised. The inferred audience proven here is a
// read-only derivation that writes nothing.

type t4bEnv struct {
	env           *academicTestEnv
	ctx           context.Context
	institutionID string
	// Subject mapped to two Programs, one Subject mapped to none.
	sharedSubjectID   string
	unmappedSubjectID string
	altSubjectID      string
	otherInstitution  string
	foreignSubjectID  string
}

// setupT4B builds a small but real Academic Catalog: one university with a
// College and Department, two Programs on active Curricula, a Subject both
// Programs require, an unmapped elective, and a second university to prove
// cross-Institution refusals.
func setupT4B(t *testing.T) *t4bEnv {
	t.Helper()
	env := setupAcademicAPIServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	e := &t4bEnv{env: env, ctx: ctx}

	institution := env.mustCreate(t, "/api/v1/admin/academic/institutions", map[string]any{
		"country_code": "KW", "slug": "t4b-university",
		"name_ar": "جامعة الكويت", "name_en": "Kuwait University",
		"max_academic_level": 5,
	})
	e.institutionID = idOf(t, institution)

	college := env.mustCreate(t, "/api/v1/admin/academic/institutions/"+e.institutionID+"/units", map[string]any{
		"kind": "COLLEGE", "slug": "science", "name_ar": "كلية العلوم", "name_en": "College of Science",
	})
	collegeID := idOf(t, college)
	department := env.mustCreate(t, "/api/v1/admin/academic/institutions/"+e.institutionID+"/units", map[string]any{
		"kind": "DEPARTMENT", "slug": "computer-science",
		"name_ar": "قسم علوم الحاسوب", "name_en": "Computer Science Department",
		"parent_unit_id": collegeID,
	})
	departmentID := idOf(t, department)

	programIDs := map[string]string{}
	for _, spec := range []struct{ slug, ar, en string }{
		{"computer-science", "علوم الحاسوب", "Computer Science"},
		{"cybersecurity", "الأمن السيبراني", "Cybersecurity"},
	} {
		program := env.mustCreate(t, "/api/v1/admin/academic/institutions/"+e.institutionID+"/programs", map[string]any{
			"slug": spec.slug, "name_ar": spec.ar, "name_en": spec.en,
			"degree_kind": "BSC", "owning_unit_id": departmentID,
		})
		programIDs[spec.slug] = idOf(t, program)
	}

	shared := env.mustCreate(t, "/api/v1/admin/academic/institutions/"+e.institutionID+"/subjects", map[string]any{
		"official_code": "0418-320",
		"title_ar":      "مبادئ نظم الحاسوب", "title_en": "Principles of Computer Systems",
		"owning_unit_id": departmentID,
	})
	e.sharedSubjectID = idOf(t, shared)

	alt := env.mustCreate(t, "/api/v1/admin/academic/institutions/"+e.institutionID+"/subjects", map[string]any{
		"official_code": "0418-321",
		"title_ar":      "نظم التشغيل", "title_en": "Operating Systems",
		"owning_unit_id": departmentID,
	})
	e.altSubjectID = idOf(t, alt)

	// A legitimate canonical Subject with no Curriculum mapping. T2's launch data
	// contains six of these, so this is a real state and not a contrived one.
	unmapped := env.mustCreate(t, "/api/v1/admin/academic/institutions/"+e.institutionID+"/subjects", map[string]any{
		"official_code": "0418-466",
		"title_ar":      "مواضيع مختارة", "title_en": "Selected Topics",
		"owning_unit_id": departmentID,
	})
	e.unmappedSubjectID = idOf(t, unmapped)

	// Both Programs require the shared Subject; only Computer Science publishes
	// authoritative placement for it.
	for slug, programID := range programIDs {
		curriculum := env.mustCreate(t, "/api/v1/admin/academic/programs/"+programID+"/curricula", map[string]any{
			"version_label": "2024",
		})
		curriculumID := idOf(t, curriculum)
		mapping := map[string]any{
			"subject_id": e.sharedSubjectID, "requirement_kind": "MAJOR_CORE", "credits": 3,
		}
		if slug == "computer-science" {
			mapping["recommended_level"] = 2
			mapping["recommended_semester"] = 1
		}
		env.mustCreate(t, "/api/v1/admin/academic/curricula/"+curriculumID+"/subjects", mapping)
	}

	other := env.mustCreate(t, "/api/v1/admin/academic/institutions", map[string]any{
		"country_code": "KW", "slug": "t4b-other-university",
		"name_ar": "جامعة أخرى", "name_en": "Other University", "max_academic_level": 4,
	})
	e.otherInstitution = idOf(t, other)
	foreign := env.mustCreate(t, "/api/v1/admin/academic/institutions/"+e.otherInstitution+"/subjects", map[string]any{
		"official_code": "0418-320", "title_ar": "مادة أخرى", "title_en": "Foreign Subject",
	})
	e.foreignSubjectID = idOf(t, foreign)

	return e
}

func (e *t4bEnv) instructorGet(t *testing.T, path string) (int, []byte) {
	t.Helper()
	return e.env.call(t, http.MethodGet, path, e.env.instructorToken, nil)
}

func (e *t4bEnv) subjectSearch(t *testing.T, query string) []map[string]any {
	t.Helper()
	status, raw := e.instructorGet(t,
		"/api/v1/authoring/academic/institutions/"+e.institutionID+"/subjects?q="+query)
	if status != http.StatusOK {
		t.Fatalf("subject search %q status = %d; body %s", query, status, raw)
	}
	var found []map[string]any
	if err := json.Unmarshal(raw, &found); err != nil {
		t.Fatalf("parsing subject search: %v", err)
	}
	return found
}

// --- 1..10. Instructor reads --------------------------------------------

func TestT4BInstructorAcademicReads(t *testing.T) {
	e := setupT4B(t)

	t.Run("1/2 Instructor lists active Institutions and never a retired one", func(t *testing.T) {
		status, raw := e.instructorGet(t, "/api/v1/authoring/academic/institutions")
		if status != http.StatusOK {
			t.Fatalf("listing institutions status = %d; body %s", status, raw)
		}
		var institutions []map[string]any
		if err := json.Unmarshal(raw, &institutions); err != nil {
			t.Fatalf("parsing institutions: %v", err)
		}
		if len(institutions) != 2 {
			t.Fatalf("got %d institutions, want the two seeded ones", len(institutions))
		}

		// Retire one and it leaves the Instructor's choices.
		status, raw = e.env.call(t, http.MethodPost,
			"/api/v1/admin/academic/institutions/"+e.otherInstitution+"/retire", e.env.adminToken, nil)
		if status != http.StatusOK {
			t.Fatalf("retiring institution status = %d; body %s", status, raw)
		}
		status, raw = e.instructorGet(t, "/api/v1/authoring/academic/institutions")
		if status != http.StatusOK {
			t.Fatalf("re-listing institutions status = %d; body %s", status, raw)
		}
		if err := json.Unmarshal(raw, &institutions); err != nil {
			t.Fatalf("parsing institutions: %v", err)
		}
		if len(institutions) != 1 {
			t.Fatalf("a retired Institution is still offered: %s", raw)
		}
	})

	t.Run("3/4/5/6 one Subject is reachable by code, normalized code, and either title", func(t *testing.T) {
		for _, query := range []string{"0418-320", "0418320", "Principles%20of%20Computer%20Systems", "%D9%85%D8%A8%D8%A7%D8%AF%D8%A6"} {
			found := e.subjectSearch(t, query)
			if len(found) == 0 {
				t.Fatalf("query %q found nothing", query)
			}
			matched := false
			for _, subject := range found {
				if subject["id"] == e.sharedSubjectID {
					matched = true
				}
			}
			if !matched {
				t.Fatalf("query %q did not resolve the canonical Subject; got %v", query, found)
			}
		}
	})

	t.Run("7 a retired Subject is not offered for a new Course", func(t *testing.T) {
		status, raw := e.env.call(t, http.MethodPost,
			"/api/v1/admin/academic/subjects/"+e.altSubjectID+"/retire", e.env.adminToken, nil)
		if status != http.StatusOK {
			t.Fatalf("retiring subject status = %d; body %s", status, raw)
		}
		for _, subject := range e.subjectSearch(t, "0418") {
			if subject["id"] == e.altSubjectID {
				t.Fatalf("a retired Subject is offered as selectable: %v", subject)
			}
		}
	})

	t.Run("8/9 search is scoped to the named Institution", func(t *testing.T) {
		for _, subject := range e.subjectSearch(t, "0418-320") {
			if subject["id"] == e.foreignSubjectID {
				t.Fatalf("search leaked a Subject from another Institution")
			}
		}
		// The same code in the other Institution resolves there, and only there.
		status, raw := e.instructorGet(t,
			"/api/v1/authoring/academic/institutions/"+e.otherInstitution+"/subjects?q=0418-320")
		if status != http.StatusOK {
			t.Fatalf("scoped search status = %d; body %s", status, raw)
		}
		var found []map[string]any
		if err := json.Unmarshal(raw, &found); err != nil {
			t.Fatalf("parsing scoped search: %v", err)
		}
		if len(found) != 1 || found[0]["id"] != e.foreignSubjectID {
			t.Fatalf("other-Institution search returned %s", raw)
		}
	})

	t.Run("10 an Instructor holds no Subject mutation authority", func(t *testing.T) {
		// Every Admin Academic Catalog mutation an Instructor might reach for.
		attempts := []struct {
			method, path string
			body         any
		}{
			{http.MethodPost, "/api/v1/admin/academic/institutions/" + e.institutionID + "/subjects",
				map[string]any{"official_code": "9999-999", "title_ar": "م", "title_en": "Invented"}},
			{http.MethodPatch, "/api/v1/admin/academic/subjects/" + e.sharedSubjectID,
				map[string]any{"title_en": "Renamed By Instructor"}},
			{http.MethodPost, "/api/v1/admin/academic/subjects/" + e.sharedSubjectID + "/retire", nil},
		}
		for _, attempt := range attempts {
			status, raw := e.env.call(t, attempt.method, attempt.path, e.env.instructorToken, attempt.body)
			if status != http.StatusForbidden && status != http.StatusUnauthorized {
				t.Fatalf("%s %s status = %d, want a refusal; body %s",
					attempt.method, attempt.path, status, raw)
			}
		}
	})
}

// --- 32..37. Inferred audience, read-only --------------------------------

func TestT4BInferredAudienceIsDerivedAndNeverPersisted(t *testing.T) {
	e := setupT4B(t)

	found := e.subjectSearch(t, "0418-320")
	var shared map[string]any
	for _, subject := range found {
		if subject["id"] == e.sharedSubjectID {
			shared = subject
		}
	}
	if shared == nil {
		t.Fatalf("shared Subject not found: %v", found)
	}

	// 32/33. A Subject required by two Programs reports both.
	programs, _ := shared["programs"].([]any)
	if len(programs) != 2 {
		t.Fatalf("shared Subject reports %d Programs, want 2: %v", len(programs), shared)
	}
	names := map[string]map[string]any{}
	for _, entry := range programs {
		program, _ := entry.(map[string]any)
		names[program["name_en"].(string)] = program
	}
	if _, ok := names["Computer Science"]; !ok {
		t.Fatalf("Computer Science missing from the inferred audience: %v", programs)
	}
	if _, ok := names["Cybersecurity"]; !ok {
		t.Fatalf("Cybersecurity missing from the inferred audience: %v", programs)
	}

	// 36/37. Placement appears only where the Curriculum carries it, and is never
	// invented for the Program that has none.
	if level, ok := names["Computer Science"]["recommended_level"]; !ok || level.(float64) != 2 {
		t.Fatalf("authoritative placement was dropped: %v", names["Computer Science"])
	}
	if _, present := names["Cybersecurity"]["recommended_level"]; present {
		t.Fatalf("placement was fabricated for a Program with no authoritative data: %v", names["Cybersecurity"])
	}

	// 34. An unmapped Subject reports zero Programs and is not an error.
	unmapped := e.subjectSearch(t, "0418-466")
	if len(unmapped) != 1 {
		t.Fatalf("unmapped Subject search returned %d results", len(unmapped))
	}
	unmappedPrograms, _ := unmapped[0]["programs"].([]any)
	if len(unmappedPrograms) != 0 {
		t.Fatalf("an unmapped Subject reported an audience: %v", unmappedPrograms)
	}

	// Nothing was persisted to produce any of that.
	var targets int
	if err := e.env.pool.QueryRow(e.ctx, `SELECT count(*) FROM course_program_targets`).Scan(&targets); err != nil {
		t.Fatalf("counting program targets: %v", err)
	}
	if targets != 0 {
		t.Fatalf("reading the inferred audience wrote %d course_program_targets rows", targets)
	}
}

// 35. Only a Program's own ACTIVE Curriculum contributes, and a retired Program
// never does.
func TestT4BInferredAudienceUsesOnlyActiveCurriculaAndLivePrograms(t *testing.T) {
	e := setupT4B(t)

	audienceSize := func() int {
		t.Helper()
		for _, subject := range e.subjectSearch(t, "0418-320") {
			if subject["id"] == e.sharedSubjectID {
				programs, _ := subject["programs"].([]any)
				return len(programs)
			}
		}
		t.Fatalf("shared Subject disappeared from search")
		return -1
	}
	if got := audienceSize(); got != 2 {
		t.Fatalf("baseline audience = %d, want 2", got)
	}

	// Superseding a Curriculum removes its Program from the inferred audience,
	// because only the ACTIVE plan of a Program counts.
	var curriculumID string
	if err := e.env.pool.QueryRow(e.ctx, `
		SELECT c.id::text FROM curricula c
		JOIN programs p ON p.id = c.program_id
		WHERE p.name_en = 'Cybersecurity'`).Scan(&curriculumID); err != nil {
		t.Fatalf("loading curriculum: %v", err)
	}
	if _, err := e.env.pool.Exec(e.ctx,
		`UPDATE curricula SET status = 'SUPERSEDED' WHERE id = $1::uuid`, curriculumID); err != nil {
		t.Fatalf("superseding curriculum: %v", err)
	}
	if got := audienceSize(); got != 1 {
		t.Fatalf("a superseded Curriculum still contributes: audience = %d", got)
	}

	// And a retired Program drops out even while its Curriculum is ACTIVE.
	if _, err := e.env.pool.Exec(e.ctx,
		`UPDATE curricula SET status = 'ACTIVE' WHERE id = $1::uuid`, curriculumID); err != nil {
		t.Fatalf("restoring curriculum: %v", err)
	}
	if _, err := e.env.pool.Exec(e.ctx,
		`UPDATE programs SET retired_at = now() WHERE name_en = 'Cybersecurity'`); err != nil {
		t.Fatalf("retiring program: %v", err)
	}
	if got := audienceSize(); got != 1 {
		t.Fatalf("a retired Program still contributes: audience = %d", got)
	}
}
