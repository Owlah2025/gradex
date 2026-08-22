//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// T3 Student academic profile HTTP surface.
//
// The two properties only the HTTP layer can prove: that the account always
// comes from the session so no Student can reach another's profile, and that
// the Student learning capability is the gate rather than a hidden route.

func TestStudentAcademicProfileAPI(t *testing.T) {
	env := setupAcademicAPIServer(t)
	ctx := context.Background()

	// The Student needs a catalog to point at, imported the way production does.
	status, raw := env.call(t, http.MethodPost,
		"/api/v1/admin/academic/institutions/00000000-0000-0000-0000-000000000000/import",
		env.adminToken, map[string]any{"manifest": "kuwait-university-launch-v1", "mode": "apply"})
	if status != http.StatusOK {
		t.Fatalf("seeding the launch catalog: %d %s", status, raw)
	}

	var institutionID, scienceCollege, csProgram string
	if err := env.pool.QueryRow(ctx,
		`SELECT id::text FROM institutions WHERE slug = 'kuwait-university'`).Scan(&institutionID); err != nil {
		t.Fatalf("reading institution: %v", err)
	}
	if err := env.pool.QueryRow(ctx,
		`SELECT id::text FROM academic_units WHERE institution_id = $1::uuid AND slug = 'science'`,
		institutionID).Scan(&scienceCollege); err != nil {
		t.Fatalf("reading college: %v", err)
	}
	if err := env.pool.QueryRow(ctx,
		`SELECT id::text FROM programs WHERE institution_id = $1::uuid AND slug = 'computer-science'`,
		institutionID).Scan(&csProgram); err != nil {
		t.Fatalf("reading program: %v", err)
	}

	t.Run("a Student with no profile is NOT_STARTED", func(t *testing.T) {
		status, raw := env.call(t, http.MethodGet, "/api/v1/me/academic-profile", env.studentToken, nil)
		if status != http.StatusOK {
			t.Fatalf("reading profile status = %d, want 200; body %s", status, raw)
		}
		var profile map[string]any
		if err := json.Unmarshal(raw, &profile); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if profile["setup_state"] != "NOT_STARTED" {
			t.Fatalf("setup_state = %v, want NOT_STARTED", profile["setup_state"])
		}
	})

	t.Run("the launch option projection exposes exactly the five KU Programs", func(t *testing.T) {
		status, raw := env.call(t, http.MethodGet,
			"/api/v1/me/academic-options/institutions", env.studentToken, nil)
		if status != http.StatusOK {
			t.Fatalf("institutions status = %d; body %s", status, raw)
		}
		var institutions []map[string]any
		if err := json.Unmarshal(raw, &institutions); err != nil {
			t.Fatalf("decoding institutions: %v", err)
		}
		if len(institutions) != 1 || institutions[0]["name_en"] != "Kuwait University" {
			t.Fatalf("institutions = %s", raw)
		}
		// The level bound travels with the institution, so no surface hardcodes it.
		if institutions[0]["max_academic_level"].(float64) != 5 {
			t.Fatalf("max_academic_level = %v, want the institution's own 5", institutions[0]["max_academic_level"])
		}

		status, raw = env.call(t, http.MethodGet,
			"/api/v1/me/academic-options/institutions/"+institutionID+"/colleges", env.studentToken, nil)
		if status != http.StatusOK {
			t.Fatalf("colleges status = %d; body %s", status, raw)
		}
		var colleges []map[string]any
		if err := json.Unmarshal(raw, &colleges); err != nil {
			t.Fatalf("decoding colleges: %v", err)
		}
		byName := map[string]string{}
		for _, college := range colleges {
			byName[college["name_en"].(string)] = college["id"].(string)
		}
		for _, expected := range []string{
			"College of Science", "College of Life Sciences", "College of Engineering and Petroleum",
		} {
			if byName[expected] == "" {
				t.Fatalf("college %q is missing from %s", expected, raw)
			}
		}
		// Departments are never offered as Colleges.
		for _, department := range []string{"Computer Science", "Information Science", "Mathematics"} {
			if byName[department] != "" {
				t.Fatalf("%q was offered as a College", department)
			}
		}

		// Programs resolve through the College subtree, which is what lets the
		// Student skip the Department step. The five launch Programs and nothing
		// else must appear.
		collected := map[string]bool{}
		for name, id := range byName {
			status, raw := env.call(t, http.MethodGet,
				"/api/v1/me/academic-options/institutions/"+institutionID+"/programs?college_id="+id,
				env.studentToken, nil)
			if status != http.StatusOK {
				t.Fatalf("programs for %s status = %d; body %s", name, status, raw)
			}
			var programs []map[string]any
			if err := json.Unmarshal(raw, &programs); err != nil {
				t.Fatalf("decoding programs: %v", err)
			}
			for _, program := range programs {
				collected[program["name_en"].(string)] = true
			}
		}
		want := []string{
			"Computer Science", "Cybersecurity", "Data Science and Artificial Intelligence",
			"Computer Engineering", "Electrical Engineering",
		}
		for _, name := range want {
			if !collected[name] {
				t.Errorf("launch Program %q is not offered", name)
			}
		}
		if len(collected) != len(want) {
			t.Fatalf("offered Programs = %v, want exactly the five launch Programs", collected)
		}
		// Founder scope: Mathematics majors and invented Programs never appear.
		for _, excluded := range []string{
			"Mathematics", "Financial Mathematics", "Software Engineering",
			"Cybersecurity Engineering", "Data Science", "Programming",
		} {
			if collected[excluded] {
				t.Errorf("%q was offered to a Student but is not a launch Program", excluded)
			}
		}
	})

	t.Run("a Student saves an enrolled profile and the server resolves the plan", func(t *testing.T) {
		status, raw := env.call(t, http.MethodPut, "/api/v1/me/academic-profile", env.studentToken,
			map[string]any{
				"institution_id": institutionID, "enrollment_status": "ENROLLED",
				"program_id": csProgram, "current_level": 2,
			})
		if status != http.StatusOK {
			t.Fatalf("saving status = %d, want 200; body %s", status, raw)
		}
		var profile map[string]any
		if err := json.Unmarshal(raw, &profile); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if profile["setup_state"] != "COMPLETED" || profile["enrollment_status"] != "ENROLLED" {
			t.Fatalf("profile = %s", raw)
		}
		if profile["curriculum_version_label"] != "2024" {
			t.Fatalf("curriculum = %v, want the server-resolved 2024 plan", profile["curriculum_version_label"])
		}
		// The College is derived from the Program, never stored twice.
		if profile["college_name"] != "College of Science" {
			t.Fatalf("derived college = %v", profile["college_name"])
		}
		if _, redundant := profile["academic_unit_id"]; redundant {
			t.Fatal("an enrolled profile carries a redundant academic unit")
		}
	})

	t.Run("a client cannot choose the curriculum", func(t *testing.T) {
		status, raw := env.call(t, http.MethodPut, "/api/v1/me/academic-profile", env.studentToken,
			map[string]any{
				"institution_id": institutionID, "enrollment_status": "ENROLLED",
				"program_id": csProgram, "curriculum_id": "00000000-0000-0000-0000-000000000001",
			})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("client-chosen curriculum status = %d, want 422; body %s", status, raw)
		}
		if !bytes.Contains(raw, []byte("CURRICULUM_NOT_SELECTABLE")) {
			t.Fatalf("refusal did not name the violation: %s", raw)
		}
	})

	t.Run("an undeclared Student keeps their College", func(t *testing.T) {
		status, raw := env.call(t, http.MethodPut, "/api/v1/me/academic-profile", env.studentToken,
			map[string]any{
				"institution_id": institutionID, "enrollment_status": "UNDECLARED",
				"academic_unit_id": scienceCollege,
			})
		if status != http.StatusOK {
			t.Fatalf("undeclared save status = %d; body %s", status, raw)
		}
		var profile map[string]any
		if err := json.Unmarshal(raw, &profile); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if profile["enrollment_status"] != "UNDECLARED" {
			t.Fatalf("status = %v", profile["enrollment_status"])
		}
		if profile["academic_unit_name"] != "College of Science" {
			t.Fatalf("the College context was lost: %s", raw)
		}
		if _, present := profile["program_id"]; present {
			t.Fatal("an undeclared profile carries a Program")
		}
	})

	t.Run("Kuwait University offers no foundation state", func(t *testing.T) {
		status, raw := env.call(t, http.MethodPut, "/api/v1/me/academic-profile", env.studentToken,
			map[string]any{"institution_id": institutionID, "enrollment_status": "FOUNDATION"})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("foundation status = %d, want 422; body %s", status, raw)
		}
		if !bytes.Contains(raw, []byte("FOUNDATION_NOT_SUPPORTED")) {
			t.Fatalf("refusal did not name the violation: %s", raw)
		}
	})

	t.Run("skip is its own command and clears the profile", func(t *testing.T) {
		status, raw := env.call(t, http.MethodPost, "/api/v1/me/academic-profile/skip", env.studentToken, nil)
		if status != http.StatusOK {
			t.Fatalf("skip status = %d; body %s", status, raw)
		}
		var profile map[string]any
		if err := json.Unmarshal(raw, &profile); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if profile["setup_state"] != "SKIPPED" {
			t.Fatalf("setup_state = %v, want SKIPPED", profile["setup_state"])
		}
		for _, leaked := range []string{"institution_id", "program_id", "curriculum_id", "current_level"} {
			if _, present := profile[leaked]; present {
				t.Fatalf("a SKIPPED profile still carries %s: %s", leaked, raw)
			}
		}
	})

	t.Run("the profile is private to its own Student", func(t *testing.T) {
		// Every route derives the account from the session, so there is no
		// parameter through which one Student could name another.
		status, _ := env.call(t, http.MethodGet, "/api/v1/me/academic-profile", env.instructorToken, nil)
		if status != http.StatusForbidden {
			t.Fatalf("Instructor read status = %d, want 403", status)
		}
		status, _ = env.call(t, http.MethodPut, "/api/v1/me/academic-profile", env.instructorToken,
			map[string]any{"institution_id": institutionID, "enrollment_status": "UNDECLARED"})
		if status != http.StatusForbidden {
			t.Fatalf("Instructor write status = %d, want 403", status)
		}
		status, _ = env.call(t, http.MethodPost, "/api/v1/me/academic-profile/skip", env.instructorToken, nil)
		if status != http.StatusForbidden {
			t.Fatalf("Instructor skip status = %d, want 403", status)
		}
		status, _ = env.call(t, http.MethodGet, "/api/v1/me/academic-profile", "", nil)
		if status != http.StatusUnauthorized && status != http.StatusForbidden {
			t.Fatalf("anonymous read status = %d, want 401 or 403", status)
		}
		status, _ = env.call(t, http.MethodGet, "/api/v1/me/academic-options/institutions", "", nil)
		if status != http.StatusUnauthorized && status != http.StatusForbidden {
			t.Fatalf("anonymous options status = %d, want 401 or 403", status)
		}

		// An Admin holds catalog authority but not Student profile authority:
		// T3 grants none, and there is no bulk listing to reach for either.
		status, _ = env.call(t, http.MethodGet, "/api/v1/me/academic-profile", env.adminToken, nil)
		if status != http.StatusForbidden {
			t.Fatalf("Admin profile read status = %d, want 403", status)
		}
		for _, enumeration := range []string{
			"/api/v1/me/academic-profile?account_id=" + institutionID,
			"/api/v1/admin/academic/student-profiles",
			"/api/v1/admin/students/" + institutionID + "/academic-profile",
		} {
			status, _ := env.call(t, http.MethodGet, enumeration, env.adminToken, nil)
			if status == http.StatusOK {
				t.Fatalf("%s returned another Student's profile data", enumeration)
			}
		}
	})
}
