//go:build integration

package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestT4CAudienceHTTPSubsetResetLockAndAuthorization(t *testing.T) {
	e := setupT4B(t)
	status, raw := e.createCourse(t, e.academicCourseBody(e.sharedSubjectID))
	if status != http.StatusCreated {
		t.Fatalf("Course create status = %d; body %s", status, raw)
	}
	var course map[string]any
	if err := json.Unmarshal(raw, &course); err != nil {
		t.Fatal(err)
	}
	courseID := course["id"].(string)
	revision := course["editable_revision"].(map[string]any)
	revisionID := revision["id"].(string)

	var validProgramID string
	if err := e.env.pool.QueryRow(e.ctx, `
		SELECT id::text FROM programs
		WHERE institution_id = $1 AND name_en = 'Computer Science'`, e.institutionID).Scan(&validProgramID); err != nil {
		t.Fatal(err)
	}
	status, raw = e.env.call(t, http.MethodPut,
		"/api/v1/courses/"+courseID+"/revisions/"+revisionID+"/audience",
		e.env.instructorToken, map[string]any{"program_ids": []string{validProgramID}})
	if status != http.StatusOK || !containsT4D(string(raw), "CUSTOMIZED", "Computer Science") {
		t.Fatalf("custom audience status = %d; body %s", status, raw)
	}
	var targets int
	if err := e.env.pool.QueryRow(e.ctx,
		`SELECT count(*) FROM course_program_targets WHERE revision_id = $1`, revisionID).Scan(&targets); err != nil {
		t.Fatal(err)
	}
	if targets != 1 {
		t.Fatalf("custom audience stored %d targets", targets)
	}

	var foreignProgramID string
	if err := e.env.pool.QueryRow(e.ctx, `
		INSERT INTO programs (institution_id, slug, name_ar, name_en, degree_kind)
		VALUES ($1, 'foreign-program', 'تخصص أجنبي', 'Foreign Program', 'BSC') RETURNING id::text`,
		e.otherInstitution).Scan(&foreignProgramID); err != nil {
		t.Fatal(err)
	}
	for name, ids := range map[string][]string{
		"cross Institution": {foreignProgramID},
		"duplicate":         {validProgramID, validProgramID},
	} {
		t.Run(name, func(t *testing.T) {
			status, raw := e.env.call(t, http.MethodPut,
				"/api/v1/courses/"+courseID+"/revisions/"+revisionID+"/audience",
				e.env.instructorToken, map[string]any{"program_ids": ids})
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d; body %s", status, raw)
			}
		})
	}

	status, raw = e.env.call(t, http.MethodDelete,
		"/api/v1/courses/"+courseID+"/revisions/"+revisionID+"/audience",
		e.env.instructorToken, nil)
	if status != http.StatusOK || !containsT4D(string(raw), "AUTOMATIC", "Computer Science", "Cybersecurity") {
		t.Fatalf("reset audience status = %d; body %s", status, raw)
	}
	if err := e.env.pool.QueryRow(e.ctx,
		`SELECT count(*) FROM course_program_targets WHERE revision_id = $1`, revisionID).Scan(&targets); err != nil {
		t.Fatal(err)
	}
	if targets != 0 {
		t.Fatalf("automatic reset left %d rows", targets)
	}

	status, _ = e.env.call(t, http.MethodPut,
		"/api/v1/courses/"+courseID+"/revisions/"+revisionID+"/audience",
		e.env.studentToken, map[string]any{"program_ids": []string{validProgramID}})
	if status != http.StatusForbidden {
		t.Fatalf("Student audience mutation status = %d", status)
	}
	if _, err := e.env.pool.Exec(e.ctx,
		`UPDATE course_revisions SET state = 'PENDING_REVIEW' WHERE id = $1`, revisionID); err != nil {
		t.Fatal(err)
	}
	status, _ = e.env.call(t, http.MethodPut,
		"/api/v1/courses/"+courseID+"/revisions/"+revisionID+"/audience",
		e.env.instructorToken, map[string]any{"program_ids": []string{validProgramID}})
	if status != http.StatusConflict {
		t.Fatalf("pending-review audience mutation status = %d, want 409", status)
	}
}
