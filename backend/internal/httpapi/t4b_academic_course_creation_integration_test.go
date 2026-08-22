//go:build integration

package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// T4-B creation and editing.
//
// The central transition: ordinary Instructor Course creation is now Academic
// Catalog based, and there is no ordinary path back to the legacy model.

func (e *t4bEnv) createCourse(t *testing.T, body map[string]any) (int, []byte) {
	t.Helper()
	return e.env.call(t, http.MethodPost, "/api/v1/courses", e.env.instructorToken, body)
}

func (e *t4bEnv) academicCourseBody(subjectID string) map[string]any {
	return map[string]any{
		"title_ar": "كورس أكاديمي", "title_en": "Academic Course",
		"description_ar": "وصف", "description_en": "Description",
		"institution_id": e.institutionID, "subject_id": subjectID,
	}
}

func (e *t4bEnv) mustCreateAcademicCourse(t *testing.T, subjectID string) map[string]any {
	t.Helper()
	status, raw := e.createCourse(t, e.academicCourseBody(subjectID))
	if status != http.StatusCreated {
		t.Fatalf("creating academic course status = %d; body %s", status, raw)
	}
	var course map[string]any
	if err := json.Unmarshal(raw, &course); err != nil {
		t.Fatalf("parsing created course: %v", err)
	}
	return course
}

// --- 11..22. Creation ----------------------------------------------------

func TestT4BOrdinaryCourseCreationIsAcademicCatalog(t *testing.T) {
	e := setupT4B(t)

	// 12..16. A successful create is Academic, carries the canonical identity,
	// makes its first revision atomically, and populates no legacy taxonomy.
	course := e.mustCreateAcademicCourse(t, e.sharedSubjectID)
	if course["classification_model"] != "ACADEMIC_CATALOG" {
		t.Fatalf("ordinary creation produced %v, want ACADEMIC_CATALOG", course["classification_model"])
	}
	if course["institution_id"] != e.institutionID {
		t.Fatalf("Course Institution = %v, want the selected university", course["institution_id"])
	}
	if course["subject_id"] != e.sharedSubjectID {
		t.Fatalf("Course Subject = %v, want the selected canonical Subject", course["subject_id"])
	}
	revision, _ := course["editable_revision"].(map[string]any)
	if revision == nil || revision["revision_number"].(float64) != 1 {
		t.Fatalf("initial revision was not created with the Course: %v", course["editable_revision"])
	}

	courseID := course["id"].(string)
	var major, subjectTerm, studyYear *string
	if err := e.env.pool.QueryRow(e.ctx, `
		SELECT major_term_id::text, subject_term_id::text, study_year::text
		FROM course_revisions WHERE course_id = $1::uuid`, courseID).Scan(&major, &subjectTerm, &studyYear); err != nil {
		t.Fatalf("reading revision taxonomy: %v", err)
	}
	if major != nil || subjectTerm != nil || studyYear != nil {
		t.Fatalf("an Academic Course was given legacy taxonomy: major=%v subject=%v year=%v",
			major, subjectTerm, studyYear)
	}

	// No audience row is written at creation: zero rows remains the automatic
	// state until T4-C implements an override.
	var targets int
	if err := e.env.pool.QueryRow(e.ctx,
		`SELECT count(*) FROM course_program_targets`).Scan(&targets); err != nil {
		t.Fatalf("counting program targets: %v", err)
	}
	if targets != 0 {
		t.Fatalf("creation wrote %d course_program_targets rows", targets)
	}
}

func TestT4BCreationRefusesIncompleteOrInvalidAcademicContext(t *testing.T) {
	e := setupT4B(t)

	// 11/20/21. Academic context is required, so the ordinary route can no longer
	// produce a legacy Course by omission.
	cases := []struct {
		name string
		body map[string]any
	}{
		{"no academic context at all", map[string]any{"title_ar": "ك", "title_en": "Course"}},
		{"subject without institution", map[string]any{
			"title_ar": "ك", "title_en": "Course", "subject_id": e.sharedSubjectID}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, raw := e.createCourse(t, tc.body)
			if status == http.StatusCreated {
				t.Fatalf("ordinary creation succeeded without academic context; body %s", raw)
			}
			if status == http.StatusInternalServerError {
				t.Fatalf("refusal was a raw 500; body %s", raw)
			}
		})
	}

	// And nothing was created by any of them.
	var courses int
	if err := e.env.pool.QueryRow(e.ctx, `SELECT count(*) FROM courses`).Scan(&courses); err != nil {
		t.Fatalf("counting courses: %v", err)
	}
	if courses != 0 {
		t.Fatalf("%d Courses exist after refused creations", courses)
	}

	// 18. A Subject from another Institution is refused.
	status, raw := e.createCourse(t, map[string]any{
		"title_ar": "ك", "title_en": "Course",
		"institution_id": e.institutionID, "subject_id": e.foreignSubjectID,
	})
	if status == http.StatusCreated {
		t.Fatalf("a cross-Institution Subject was accepted; body %s", raw)
	}

	// 19. A retired Subject is refused server-side, not merely hidden from search.
	if status, raw := e.env.call(t, http.MethodPost,
		"/api/v1/admin/academic/subjects/"+e.altSubjectID+"/retire", e.env.adminToken, nil); status != http.StatusOK {
		t.Fatalf("retiring subject status = %d; body %s", status, raw)
	}
	status, raw = e.createCourse(t, e.academicCourseBody(e.altSubjectID))
	if status == http.StatusCreated {
		t.Fatalf("a retired Subject was accepted for a new Course; body %s", raw)
	}
}

// 17/29/30. Classification cannot be forged, and the two models cannot be mixed.
func TestT4BClassificationCannotBeForgedFromThePayload(t *testing.T) {
	e := setupT4B(t)

	// Naming a classification in the payload changes nothing: the field does not
	// exist in the contract, so it is ignored and the server derives the model.
	status, raw := e.createCourse(t, map[string]any{
		"title_ar": "ك", "title_en": "Course",
		"institution_id": e.institutionID, "subject_id": e.sharedSubjectID,
		"classification_model": "LEGACY_TAXONOMY",
	})
	if status != http.StatusCreated {
		t.Fatalf("creation status = %d; body %s", status, raw)
	}
	var course map[string]any
	if err := json.Unmarshal(raw, &course); err != nil {
		t.Fatalf("parsing course: %v", err)
	}
	if course["classification_model"] != "ACADEMIC_CATALOG" {
		t.Fatalf("a payload flipped the classification to %v", course["classification_model"])
	}
	courseID := course["id"].(string)
	revisionID := course["editable_revision"].(map[string]any)["id"].(string)

	// 29. Legacy taxonomy is refused on an Academic Course through the ordinary
	// revision editor, which is the route the legacy panel uses.
	var majorTermID string
	if err := e.env.pool.QueryRow(e.ctx, `
		INSERT INTO taxonomy_terms (kind, label_ar, label_en)
		VALUES ('MAJOR', 'تخصص', 'Major') RETURNING id::text`).Scan(&majorTermID); err != nil {
		t.Fatalf("seeding legacy term: %v", err)
	}
	status, raw = e.env.call(t, http.MethodPatch,
		"/api/v1/courses/"+courseID+"/revisions/"+revisionID, e.env.instructorToken,
		map[string]any{"major_term_id": majorTermID})
	if status == http.StatusOK {
		t.Fatalf("an Academic Course accepted legacy taxonomy; body %s", raw)
	}
	if status == http.StatusInternalServerError {
		t.Fatalf("the refusal was a raw 500; body %s", raw)
	}

	// An ordinary title edit on the same shared route still works, so the refusal
	// is scoped to the legacy fields rather than to the whole editor.
	if status, raw := e.env.call(t, http.MethodPatch,
		"/api/v1/courses/"+courseID+"/revisions/"+revisionID, e.env.instructorToken,
		map[string]any{"title_en": "Renamed"}); status != http.StatusOK {
		t.Fatalf("ordinary revision edit status = %d; body %s", status, raw)
	}

	// 30. And a legacy Course cannot acquire academic identity through the
	// Instructor Subject route.
	legacyCourseID, _ := seedLegacyCourseFixture(t, e.env.pool, e.ctx, instructorAccountID(t, e), "قديم", "Legacy")
	status, raw = e.env.call(t, http.MethodPut,
		"/api/v1/courses/"+legacyCourseID+"/subject", e.env.instructorToken,
		map[string]any{"subject_id": e.sharedSubjectID})
	if status == http.StatusOK {
		t.Fatalf("a legacy Course was converted through the academic route; body %s", raw)
	}
	var model string
	if err := e.env.pool.QueryRow(e.ctx,
		`SELECT classification_model::text FROM courses WHERE id = $1::uuid`, legacyCourseID).Scan(&model); err != nil {
		t.Fatalf("re-reading legacy course: %v", err)
	}
	if model != "LEGACY_TAXONOMY" {
		t.Fatalf("legacy Course classification drifted to %s", model)
	}
}

// instructorAccountID resolves the Instructor the academic test env authenticates
// as, so a fixture Course can be owned by them.
func instructorAccountID(t *testing.T, e *t4bEnv) string {
	t.Helper()
	var id string
	if err := e.env.pool.QueryRow(e.ctx,
		`SELECT id::text FROM accounts WHERE role = 'INSTRUCTOR' ORDER BY created_at ASC LIMIT 1`).Scan(&id); err != nil {
		t.Fatalf("resolving instructor account: %v", err)
	}
	return id
}

// --- 23..28. Subject editing across the lifecycle -------------------------

func TestT4BSubjectEditingAcrossTheLifecycle(t *testing.T) {
	e := setupT4B(t)
	course := e.mustCreateAcademicCourse(t, e.sharedSubjectID)
	courseID := course["id"].(string)

	setSubject := func(subjectID string) (int, []byte) {
		return e.env.call(t, http.MethodPut, "/api/v1/courses/"+courseID+"/subject",
			e.env.instructorToken, map[string]any{"subject_id": subjectID})
	}

	// 23/24. A pre-publication correction within the Course's own Institution.
	status, raw := setSubject(e.altSubjectID)
	if status != http.StatusOK {
		t.Fatalf("pre-publication Subject correction status = %d; body %s", status, raw)
	}
	var updated map[string]any
	if err := json.Unmarshal(raw, &updated); err != nil {
		t.Fatalf("parsing updated course: %v", err)
	}
	if updated["subject_id"] != e.altSubjectID {
		t.Fatalf("Subject was not corrected: %v", updated["subject_id"])
	}
	if updated["classification_model"] != "ACADEMIC_CATALOG" {
		t.Fatalf("correcting the Subject changed the classification to %v", updated["classification_model"])
	}
	if updated["institution_id"] != e.institutionID {
		t.Fatalf("correcting the Subject moved the Course's Institution to %v", updated["institution_id"])
	}

	// 25. A Subject from another Institution is refused, so a Course never
	// migrates university through a Subject edit.
	if status, raw := setSubject(e.foreignSubjectID); status == http.StatusOK {
		t.Fatalf("a cross-Institution Subject edit succeeded; body %s", raw)
	}

	// 26. Under review the Subject is frozen.
	if _, err := e.env.pool.Exec(e.ctx, `
		UPDATE course_revisions SET state = 'PENDING_REVIEW', submitted_at = now()
		WHERE course_id = $1::uuid`, courseID); err != nil {
		t.Fatalf("moving revision to PENDING_REVIEW: %v", err)
	}
	if status, raw := setSubject(e.sharedSubjectID); status == http.StatusOK {
		t.Fatalf("the Subject changed while the Course was under review; body %s", raw)
	}

	// 27. After the Admin requests changes, and while the Course has still never
	// published, correction is possible again.
	if _, err := e.env.pool.Exec(e.ctx, `
		UPDATE course_revisions SET state = 'CHANGES_REQUESTED', review_reason = 'wrong subject'
		WHERE course_id = $1::uuid`, courseID); err != nil {
		t.Fatalf("requesting changes: %v", err)
	}
	if status, raw := setSubject(e.sharedSubjectID); status != http.StatusOK {
		t.Fatalf("Subject correction after Request Changes status = %d; body %s", status, raw)
	}

	// 28. First publication makes it identity, permanently.
	var revisionID string
	if err := e.env.pool.QueryRow(e.ctx, `
		UPDATE course_revisions SET state = 'APPROVED' WHERE course_id = $1::uuid
		RETURNING id::text`, courseID).Scan(&revisionID); err != nil {
		t.Fatalf("approving revision: %v", err)
	}
	if _, err := e.env.pool.Exec(e.ctx, `
		UPDATE courses SET live_revision_id = $1::uuid, lifecycle = 'PUBLISHED' WHERE id = $2::uuid`,
		revisionID, courseID); err != nil {
		t.Fatalf("publishing course: %v", err)
	}
	if status, raw := setSubject(e.altSubjectID); status == http.StatusOK {
		t.Fatalf("a published Course's Subject was changed; body %s", raw)
	}
	var finalSubject string
	if err := e.env.pool.QueryRow(e.ctx,
		`SELECT subject_id::text FROM courses WHERE id = $1::uuid`, courseID).Scan(&finalSubject); err != nil {
		t.Fatalf("re-reading subject: %v", err)
	}
	if finalSubject != e.sharedSubjectID {
		t.Fatalf("published Subject drifted to %s", finalSubject)
	}
}

// 31. A legacy Course keeps its own editor working throughout.
func TestT4BLegacyCourseEditingRemainsAvailable(t *testing.T) {
	e := setupT4B(t)
	instructorID := instructorAccountID(t, e)
	courseID, revisionID := seedLegacyCourseFixture(t, e.env.pool, e.ctx, instructorID, "قديم", "Legacy Course")

	var majorTermID, subjectTermID string
	if err := e.env.pool.QueryRow(e.ctx, `
		INSERT INTO taxonomy_terms (kind, label_ar, label_en)
		VALUES ('MAJOR', 'تخصص', 'Major') RETURNING id::text`).Scan(&majorTermID); err != nil {
		t.Fatalf("seeding major term: %v", err)
	}
	if err := e.env.pool.QueryRow(e.ctx, `
		INSERT INTO taxonomy_terms (kind, label_ar, label_en)
		VALUES ('SUBJECT', 'مادة', 'Subject') RETURNING id::text`).Scan(&subjectTermID); err != nil {
		t.Fatalf("seeding subject term: %v", err)
	}

	// The legacy classification vocabulary still writes, through the same route
	// the compatibility panel uses.
	status, raw := e.env.call(t, http.MethodPatch,
		"/api/v1/courses/"+courseID+"/revisions/"+revisionID, e.env.instructorToken,
		map[string]any{"major_term_id": majorTermID, "subject_term_id": subjectTermID, "study_year": "YEAR_1"})
	if status != http.StatusOK {
		t.Fatalf("legacy taxonomy edit status = %d; body %s", status, raw)
	}

	var storedMajor, storedYear string
	if err := e.env.pool.QueryRow(e.ctx, `
		SELECT major_term_id::text, study_year::text FROM course_revisions WHERE id = $1::uuid`,
		revisionID).Scan(&storedMajor, &storedYear); err != nil {
		t.Fatalf("re-reading legacy taxonomy: %v", err)
	}
	if storedMajor != majorTermID || storedYear != "YEAR_1" {
		t.Fatalf("legacy taxonomy did not persist: major=%s year=%s", storedMajor, storedYear)
	}

	// And the legacy Course acquired no academic identity along the way.
	var institution, subject *string
	if err := e.env.pool.QueryRow(e.ctx,
		`SELECT institution_id::text, subject_id::text FROM courses WHERE id = $1::uuid`,
		courseID).Scan(&institution, &subject); err != nil {
		t.Fatalf("re-reading legacy course: %v", err)
	}
	if institution != nil || subject != nil {
		t.Fatalf("a legacy Course gained academic identity: institution=%v subject=%v", institution, subject)
	}
}
