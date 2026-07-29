//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestAdminTaxonomyHTTPAPIRealPostgreSQL(t *testing.T) {
	ts, pool, _, instructorID, courseID, _, adminToken, instructorToken := setupAdminPricingAPIServer(t)
	ctx := context.Background()
	client := ts.Client()
	url := ts.URL + "/api/v1/admin/courses/" + courseID + "/taxonomy"

	var revisionID, majorID, subjectID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM course_revisions WHERE course_id = $1::uuid`, courseID).Scan(&revisionID); err != nil {
		t.Fatalf("loading candidate revision: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO taxonomy_terms (kind, label_ar, label_en) VALUES ('MAJOR', 'علوم', 'Science') RETURNING id::text`).Scan(&majorID); err != nil {
		t.Fatalf("creating major: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO taxonomy_terms (kind, label_ar, label_en) VALUES ('SUBJECT', 'فيزياء', 'Physics') RETURNING id::text`).Scan(&subjectID); err != nil {
		t.Fatalf("creating subject: %v", err)
	}
	body := []byte(`{"revision_id":"` + revisionID + `","major_term_id":"` + majorID + `","subject_term_id":"` + subjectID + `"}`)

	t.Run("Instructor has no taxonomy administration capability", func(t *testing.T) {
		resp := doPricingRequest(t, client, http.MethodPut, url, instructorToken, "https://gradex.example", instructorToken, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("Instructor taxonomy override status = %d, want 403", resp.StatusCode)
		}
	})

	t.Run("Admin request requires explicit revision ID", func(t *testing.T) {
		resp := doPricingRequest(t, client, http.MethodPut, url, adminToken, "https://gradex.example", adminToken, []byte(`{"major_term_id":"`+majorID+`","subject_term_id":"`+subjectID+`"}`))
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("missing revision_id status = %d, want 422", resp.StatusCode)
		}
	})

	t.Run("Admin assigns only named same-course candidate and audit commits", func(t *testing.T) {
		resp := doPricingRequest(t, client, http.MethodPut, url, adminToken, "https://gradex.example", adminToken, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Admin taxonomy override status = %d, want 200", resp.StatusCode)
		}
		var assignedMajor, assignedSubject string
		if err := pool.QueryRow(ctx, `SELECT major_term_id::text, subject_term_id::text FROM course_revisions WHERE id = $1::uuid`, revisionID).Scan(&assignedMajor, &assignedSubject); err != nil || assignedMajor != majorID || assignedSubject != subjectID {
			t.Fatalf("persisted assignment = %s/%s (err=%v), want %s/%s", assignedMajor, assignedSubject, err, majorID, subjectID)
		}
		var auditCount int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'COURSE_REVISION_UPDATED' AND target_id = $1`, courseID).Scan(&auditCount); err != nil || auditCount != 1 {
			t.Fatalf("Admin assignment audit count = %d (err=%v), want 1", auditCount, err)
		}
	})

	t.Run("Admin cannot substitute a revision from another Course", func(t *testing.T) {
		const otherCourseID = "72000000-0000-0000-0000-000000000001"
		const otherRevisionID = "72000000-0000-0000-0000-000000000002"
		if _, err := pool.Exec(ctx, `
			INSERT INTO courses (id, owner_account_id, lifecycle)
			VALUES ($1::uuid, $2::uuid, 'DRAFT')
		`, otherCourseID, instructorID); err != nil {
			t.Fatalf("creating cross-course fixture: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en, description_ar, description_en)
			VALUES ($1::uuid, $2::uuid, 'DRAFT', 1, 'دورة', 'Other Course', 'وصف', 'Description')
		`, otherRevisionID, otherCourseID); err != nil {
			t.Fatalf("creating cross-course revision: %v", err)
		}
		crossCourseBody := []byte(`{"revision_id":"` + otherRevisionID + `","major_term_id":"` + majorID + `","subject_term_id":"` + subjectID + `"}`)
		resp := doPricingRequest(t, client, http.MethodPut, url, adminToken, "https://gradex.example", adminToken, crossCourseBody)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("cross-course Admin taxonomy override status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("Instructor assignment remains explicit candidate mutation", func(t *testing.T) {
		instructorURL := ts.URL + "/api/v1/courses/" + courseID + "/revisions/" + revisionID
		resp := doPricingRequest(t, client, http.MethodPatch, instructorURL, instructorToken, "https://gradex.example", instructorToken, []byte(`{"major_term_id":"`+majorID+`","subject_term_id":"`+subjectID+`"}`))
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("explicit Instructor candidate assignment status = %d, want 200", resp.StatusCode)
		}
		implicitURL := ts.URL + "/api/v1/courses/" + courseID + "/revisions/71000000-0000-0000-0000-000000000099"
		resp = doPricingRequest(t, client, http.MethodPatch, implicitURL, instructorToken, "https://gradex.example", instructorToken, []byte(`{"major_term_id":"`+majorID+`","subject_term_id":"`+subjectID+`"}`))
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("unknown explicit revision status = %d, want 403", resp.StatusCode)
		}
	})
}

func TestAdminTaxonomyTermHTTPAPIRealPostgreSQL(t *testing.T) {
	ts, pool, _, _, courseID, _, adminToken, instructorToken := setupAdminPricingAPIServer(t)
	ctx := context.Background()
	client := ts.Client()
	termsURL := ts.URL + "/api/v1/admin/taxonomy/terms"

	var revisionID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM course_revisions WHERE course_id = $1::uuid`, courseID).Scan(&revisionID); err != nil {
		t.Fatalf("loading candidate revision: %v", err)
	}

	createTerm := func(t *testing.T, token string, body []byte, wantStatus int) string {
		t.Helper()
		resp := doPricingRequest(t, client, http.MethodPost, termsURL, token, "https://gradex.example", token, body)
		defer resp.Body.Close()
		if resp.StatusCode != wantStatus {
			t.Fatalf("taxonomy term create status = %d, want %d", resp.StatusCode, wantStatus)
		}
		if wantStatus != http.StatusCreated {
			return ""
		}
		var term struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&term); err != nil || term.ID == "" {
			t.Fatalf("decoding created taxonomy term: term=%+v err=%v", term, err)
		}
		return term.ID
	}

	t.Run("Instructor is denied every taxonomy term mutation", func(t *testing.T) {
		createTerm(t, instructorToken, []byte(`{"kind":"MAJOR","label_ar":"علوم","label_en":"Science"}`), http.StatusForbidden)
		for _, request := range []struct {
			method string
			path   string
			body   []byte
		}{
			{method: http.MethodPatch, path: "/missing", body: []byte(`{"label_ar":"علوم","label_en":"Science"}`)},
			{method: http.MethodPost, path: "/missing/retire"},
			{method: http.MethodDelete, path: "/missing"},
		} {
			resp := doPricingRequest(t, client, request.method, termsURL+request.path, instructorToken, "https://gradex.example", instructorToken, request.body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("Instructor %s %s status = %d, want 403", request.method, request.path, resp.StatusCode)
			}
		}
	})

	t.Run("Admin validates kind and subject academic-code contract", func(t *testing.T) {
		createTerm(t, adminToken, []byte(`{"kind":"MAJOR","label_ar":"علوم","label_en":"Science","academic_code":"SCI"}`), http.StatusUnprocessableEntity)
		createTerm(t, adminToken, []byte(`{"kind":"UNKNOWN","label_ar":"علوم","label_en":"Science"}`), http.StatusUnprocessableEntity)
	})

	majorID := createTerm(t, adminToken, []byte(`{"kind":"MAJOR","label_ar":"علوم","label_en":"Science"}`), http.StatusCreated)
	subjectID := createTerm(t, adminToken, []byte(`{"kind":"SUBJECT","label_ar":"فيزياء","label_en":"Physics","academic_code":"PHY101"}`), http.StatusCreated)

	t.Run("Admin rename preserves the term identity and audit", func(t *testing.T) {
		resp := doPricingRequest(t, client, http.MethodPatch, termsURL+"/"+majorID, adminToken, "https://gradex.example", adminToken, []byte(`{"label_ar":"علوم محدثة","label_en":"Updated Science"}`))
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("taxonomy rename status = %d, want 200", resp.StatusCode)
		}
		var label string
		var auditCount int
		if err := pool.QueryRow(ctx, `SELECT label_en FROM taxonomy_terms WHERE id = $1::uuid`, majorID).Scan(&label); err != nil || label != "Updated Science" {
			t.Fatalf("renamed label = %q (err=%v), want Updated Science", label, err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'TAXONOMY_TERM_RENAMED' AND target_id = $1`, majorID).Scan(&auditCount); err != nil || auditCount != 1 {
			t.Fatalf("rename audit count = %d (err=%v), want 1", auditCount, err)
		}
	})

	t.Run("referenced term cannot be deleted and retirement is audited", func(t *testing.T) {
		assignmentURL := ts.URL + "/api/v1/admin/courses/" + courseID + "/taxonomy"
		assignmentBody := []byte(`{"revision_id":"` + revisionID + `","major_term_id":"` + majorID + `","subject_term_id":"` + subjectID + `"}`)
		resp := doPricingRequest(t, client, http.MethodPut, assignmentURL, adminToken, "https://gradex.example", adminToken, assignmentBody)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("taxonomy assignment status = %d, want 200", resp.StatusCode)
		}

		resp = doPricingRequest(t, client, http.MethodDelete, termsURL+"/"+majorID, adminToken, "https://gradex.example", adminToken, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("referenced taxonomy delete status = %d, want 409", resp.StatusCode)
		}

		resp = doPricingRequest(t, client, http.MethodPost, termsURL+"/"+majorID+"/retire", adminToken, "https://gradex.example", adminToken, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("taxonomy retire status = %d, want 200", resp.StatusCode)
		}
		var retiredAuditCount int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'TAXONOMY_TERM_RETIRED' AND target_id = $1`, majorID).Scan(&retiredAuditCount); err != nil || retiredAuditCount != 1 {
			t.Fatalf("retirement audit count = %d (err=%v), want 1", retiredAuditCount, err)
		}

		resp = doPricingRequest(t, client, http.MethodPost, termsURL+"/"+majorID+"/retire", adminToken, "https://gradex.example", adminToken, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("second taxonomy retire status = %d, want 409", resp.StatusCode)
		}
	})

	t.Run("unreferenced term deletion and missing-term refusal use the contract statuses", func(t *testing.T) {
		unusedID := createTerm(t, adminToken, []byte(`{"kind":"MAJOR","label_ar":"رياضيات","label_en":"Mathematics"}`), http.StatusCreated)
		resp := doPricingRequest(t, client, http.MethodDelete, termsURL+"/"+unusedID, adminToken, "https://gradex.example", adminToken, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("unreferenced taxonomy delete status = %d, want 204", resp.StatusCode)
		}
		var deletionAuditCount int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'TAXONOMY_TERM_DELETED' AND target_id = $1`, unusedID).Scan(&deletionAuditCount); err != nil || deletionAuditCount != 1 {
			t.Fatalf("deletion audit count = %d (err=%v), want 1", deletionAuditCount, err)
		}

		resp = doPricingRequest(t, client, http.MethodPatch, termsURL+"/71000000-0000-0000-0000-000000000099", adminToken, "https://gradex.example", adminToken, []byte(`{"label_ar":"علوم","label_en":"Science"}`))
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("missing taxonomy rename status = %d, want 404", resp.StatusCode)
		}
	})
}
