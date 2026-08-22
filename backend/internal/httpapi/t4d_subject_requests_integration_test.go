//go:build integration

package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func containsT4D(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}

func createSubjectlessCourse(t *testing.T, e *t4bEnv, title string) map[string]any {
	t.Helper()
	status, raw := e.createCourse(t, map[string]any{
		"title_ar": "مسودة مادة مفقودة", "title_en": title,
		"description_ar": "وصف", "description_en": "Description",
		"institution_id": e.institutionID,
	})
	if status != http.StatusCreated {
		t.Fatalf("subject-less Course status = %d; body %s", status, raw)
	}
	var course map[string]any
	if err := json.Unmarshal(raw, &course); err != nil {
		t.Fatal(err)
	}
	if course["classification_model"] != "ACADEMIC_CATALOG" || course["subject_id"] != nil {
		t.Fatalf("subject-less Course = %#v", course)
	}
	return course
}

func createRequestAPI(t *testing.T, e *t4bEnv, courseID, code string) map[string]any {
	t.Helper()
	status, raw := e.env.call(t, http.MethodPost, "/api/v1/authoring/academic/subject-requests",
		e.env.instructorToken, map[string]any{
			"institution_id": e.institutionID, "course_id": courseID,
			"proposed_official_code": code,
			"proposed_title_ar":      "مادة مطلوبة", "proposed_title_en": "Requested Subject",
			"note": "Needed for this Course",
		})
	if status != http.StatusCreated {
		t.Fatalf("request create status = %d; body %s", status, raw)
	}
	var request map[string]any
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	return request
}

func TestT4DSubjectRequestHTTPWorkflowsAndAuthorization(t *testing.T) {
	e := setupT4B(t)

	// D1 — link existing.
	course := createSubjectlessCourse(t, e, "T4D Link Existing")
	courseID := course["id"].(string)
	request := createRequestAPI(t, e, courseID, "0418-320")
	requestID := request["id"].(string)
	status, raw := e.env.call(t, http.MethodGet,
		"/api/v1/authoring/academic/subject-requests?course_id="+courseID,
		e.env.instructorToken, nil)
	if status != http.StatusOK {
		t.Fatalf("Instructor own request read status = %d; body %s", status, raw)
	}
	status, raw = e.env.call(t, http.MethodPost,
		"/api/v1/admin/academic/subject-requests/"+requestID+"/link",
		e.env.adminToken, map[string]any{"subject_id": e.sharedSubjectID})
	if status != http.StatusOK {
		t.Fatalf("link existing status = %d; body %s", status, raw)
	}
	var assigned string
	if err := e.env.pool.QueryRow(e.ctx, `SELECT subject_id::text FROM courses WHERE id = $1`, courseID).Scan(&assigned); err != nil {
		t.Fatal(err)
	}
	if assigned != e.sharedSubjectID {
		t.Fatalf("linked Course Subject = %s", assigned)
	}

	// D2 — approve as new creates exactly one canonical Subject and attaches it.
	newCourse := createSubjectlessCourse(t, e, "T4D Approve New")
	newCourseID := newCourse["id"].(string)
	newRequest := createRequestAPI(t, e, newCourseID, "T4D-NEW-777")
	status, raw = e.env.call(t, http.MethodPost,
		"/api/v1/admin/academic/subject-requests/"+newRequest["id"].(string)+"/approve-new",
		e.env.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("approve new status = %d; body %s", status, raw)
	}
	var createdSubjects int
	if err := e.env.pool.QueryRow(e.ctx, `
		SELECT count(*) FROM subjects
		WHERE institution_id = $1 AND code_normalized = academic_normalize_code('T4D-NEW-777')`,
		e.institutionID).Scan(&createdSubjects); err != nil {
		t.Fatal(err)
	}
	if createdSubjects != 1 {
		t.Fatalf("approve new created %d Subjects", createdSubjects)
	}

	// D3 — reject returns the reason to the Instructor and leaves the draft subject-less.
	rejectedCourse := createSubjectlessCourse(t, e, "T4D Reject")
	rejectedCourseID := rejectedCourse["id"].(string)
	rejectedRequest := createRequestAPI(t, e, rejectedCourseID, "T4D-REJECT")
	status, raw = e.env.call(t, http.MethodPost,
		"/api/v1/admin/academic/subject-requests/"+rejectedRequest["id"].(string)+"/reject",
		e.env.adminToken, map[string]any{"reason": "Use the official university title."})
	if status != http.StatusOK || !containsT4D(string(raw), "REJECTED", "Use the official university title.") {
		t.Fatalf("reject status = %d; body %s", status, raw)
	}
	var rejectedSubject *string
	if err := e.env.pool.QueryRow(e.ctx, `SELECT subject_id::text FROM courses WHERE id = $1`, rejectedCourseID).Scan(&rejectedSubject); err != nil {
		t.Fatal(err)
	}
	if rejectedSubject != nil {
		t.Fatalf("rejected Course gained Subject %s", *rejectedSubject)
	}

	// D4 — Instructor chooses A before Admin resolves B. The request resolves with
	// a semantic conflict, but compare-and-set leaves A untouched.
	raceCourse := createSubjectlessCourse(t, e, "T4D Race")
	raceCourseID := raceCourse["id"].(string)
	raceRequest := createRequestAPI(t, e, raceCourseID, "T4D-RACE")
	status, raw = e.env.call(t, http.MethodPut, "/api/v1/courses/"+raceCourseID+"/subject",
		e.env.instructorToken, map[string]any{"subject_id": e.sharedSubjectID})
	if status != http.StatusOK {
		t.Fatalf("manual Subject choice status = %d; body %s", status, raw)
	}
	status, raw = e.env.call(t, http.MethodPost,
		"/api/v1/admin/academic/subject-requests/"+raceRequest["id"].(string)+"/link",
		e.env.adminToken, map[string]any{"subject_id": e.altSubjectID})
	if status != http.StatusConflict || !containsT4D(string(raw), "COURSE_SUBJECT_ALREADY_SELECTED", "not reassigned") {
		t.Fatalf("race resolution status = %d; body %s", status, raw)
	}
	if err := e.env.pool.QueryRow(e.ctx, `SELECT subject_id::text FROM courses WHERE id = $1`, raceCourseID).Scan(&assigned); err != nil {
		t.Fatal(err)
	}
	if assigned != e.sharedSubjectID {
		t.Fatalf("race overwrote Course Subject with %s", assigned)
	}

	// Student and anonymous callers have no request authority; Instructor cannot
	// resolve, and the Admin queue is semantic rather than an ID-only workflow.
	for _, authCase := range []struct {
		name, token string
		want        int
	}{{"Student", e.env.studentToken, http.StatusForbidden}, {"anonymous", "", http.StatusUnauthorized}} {
		t.Run(authCase.name, func(t *testing.T) {
			status, _ := e.env.call(t, http.MethodGet, "/api/v1/authoring/academic/subject-requests", authCase.token, nil)
			if status != authCase.want {
				t.Fatalf("status = %d, want %d", status, authCase.want)
			}
		})
	}
	status, _ = e.env.call(t, http.MethodPost,
		"/api/v1/admin/academic/subject-requests/"+requestID+"/reject",
		e.env.instructorToken, map[string]any{"reason": "tamper"})
	if status != http.StatusForbidden {
		t.Fatalf("Instructor resolution status = %d, want 403", status)
	}
	status, raw = e.env.call(t, http.MethodGet,
		"/api/v1/admin/academic/subject-requests?status=REJECTED", e.env.adminToken, nil)
	if status != http.StatusOK || !containsT4D(string(raw), "Catalog Instructor", "T4D Reject", "Kuwait University") {
		t.Fatalf("Admin semantic queue status = %d; body %s", status, raw)
	}
}
