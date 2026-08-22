//go:build integration

package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// T4-A.1 §5 — the Admin HTTP path must refuse a normalized-code identity change
// with a semantic validation problem, never a raw 500 from a database
// constraint. This is the surface an Admin actually reaches, so proving the
// domain rule alone would not prove the product behaviour.
func TestAdminSubjectCodeIdentityOverHTTP(t *testing.T) {
	env := setupAcademicAPIServer(t)

	institution := env.mustCreate(t, "/api/v1/admin/academic/institutions", map[string]any{
		"country_code": "KW", "slug": "code-identity-university",
		"name_ar": "جامعة", "name_en": "Code Identity University",
		"max_academic_level": 4,
	})
	institutionID := idOf(t, institution)

	subject := env.mustCreate(t, "/api/v1/admin/academic/institutions/"+institutionID+"/subjects", map[string]any{
		"official_code": "0418-320",
		"title_ar":      "مبادئ نظم الحاسوب", "title_en": "Principles of Computer Systems",
	})
	subjectID := idOf(t, subject)

	patch := "/api/v1/admin/academic/subjects/" + subjectID

	// Formatting-only correction is accepted: same identity, different display.
	status, raw := env.call(t, http.MethodPatch, patch, env.adminToken, map[string]any{
		"official_code": "0418 320",
	})
	if status != http.StatusOK {
		t.Fatalf("formatting-only correction status = %d, want 200; body %s", status, raw)
	}

	// Renumbering is refused as a validation problem naming the reason.
	for _, renumber := range []string{"0418-321", "CS320"} {
		status, raw := env.call(t, http.MethodPatch, patch, env.adminToken, map[string]any{
			"official_code": renumber,
		})
		if status == http.StatusInternalServerError {
			t.Fatalf("renumber to %q returned a raw 500; body %s", renumber, raw)
		}
		if status != http.StatusUnprocessableEntity && status != http.StatusBadRequest {
			t.Fatalf("renumber to %q status = %d, want a validation refusal; body %s", renumber, status, raw)
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("parsing problem body: %v", err)
		}
		if !problemNamesViolation(body, "SUBJECT_CODE_IMMUTABLE") {
			t.Fatalf("refusal must name SUBJECT_CODE_IMMUTABLE; body %s", raw)
		}
	}

	// Withdrawing an established code is refused the same way.
	status, raw = env.call(t, http.MethodPatch, patch, env.adminToken, map[string]any{
		"clear_official_code": true,
	})
	if status == http.StatusInternalServerError {
		t.Fatalf("clearing the code returned a raw 500; body %s", raw)
	}
	if status != http.StatusUnprocessableEntity && status != http.StatusBadRequest {
		t.Fatalf("clearing the code status = %d, want a validation refusal; body %s", status, raw)
	}

	// The Subject still holds its original identity after every refusal, and the
	// display form from the accepted correction is the one that persisted.
	status, raw = env.call(t, http.MethodGet,
		"/api/v1/admin/academic/institutions/"+institutionID+"/subjects?q=0418320", env.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("listing subjects status = %d; body %s", status, raw)
	}
	var listed []map[string]any
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatalf("parsing subject list: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("search for the canonical code returned %d subjects, want 1; body %s", len(listed), raw)
	}
	if got, _ := listed[0]["official_code"].(string); got != "0418 320" {
		t.Fatalf("official_code = %q, want the reformatted display form", got)
	}

	// And the code was never released: a second Subject still cannot claim it.
	status, raw = env.call(t, http.MethodPost,
		"/api/v1/admin/academic/institutions/"+institutionID+"/subjects", env.adminToken, map[string]any{
			"official_code": "0418-320", "title_ar": "أخرى", "title_en": "Claimant",
		})
	if status != http.StatusConflict {
		t.Fatalf("claiming the reserved code status = %d, want 409; body %s", status, raw)
	}
}

// problemNamesViolation reports whether a problem body carries the given
// violation code, either as the top-level code or inside the per-field errors
// array the validation problem uses.
func problemNamesViolation(body map[string]any, code string) bool {
	if got, _ := body["code"].(string); got == code {
		return true
	}
	violations, _ := body["errors"].([]any)
	for _, entry := range violations {
		violation, _ := entry.(map[string]any)
		if got, _ := violation["code"].(string); got == code {
			return true
		}
	}
	return false
}
