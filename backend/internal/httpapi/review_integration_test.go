//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/identity"
)

func seedReviewDatabase(t *testing.T, pool interface{}, ctx context.Context) (adminID, instructorID, videoAssetID, majorTermID, subjectTermID string) {
	t.Helper()
	p := pool.(*pgxpool.Pool)

	adminID = "00000000-0000-0000-0000-000000000001"
	instructorID = "11111111-1111-1111-1111-111111111111"
	videoAssetID = "33333333-3333-3333-3333-333333333333"
	majorTermID = "44444444-4444-4444-4444-444444444444"
	subjectTermID = "55555555-5555-5555-5555-555555555555"

	_, err := p.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name) VALUES
		($1, 'admin@example.com', 'admin@example.com', 'ADMIN', 'ACTIVE', 'Admin User'),
		($2, 'instructor@example.com', 'instructor@example.com', 'INSTRUCTOR', 'ACTIVE', 'Instructor User')
	`, adminID, instructorID)
	if err != nil {
		t.Fatalf("seeding accounts: %v", err)
	}

	secID := "77777777-7777-7777-7777-777777777777"
	lesID := "88888888-8888-8888-8888-888888888888"
	dummyCourseID := "66666666-6666-6666-6666-666666666666"

	_, err = p.Exec(ctx, `INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1, $2, 'DRAFT')`, dummyCourseID, instructorID)
	if err != nil {
		t.Fatalf("seeding dummy course: %v", err)
	}
	_, err = p.Exec(ctx, `INSERT INTO sections (id, course_id, title, "order") VALUES ($1, $2, 'Sec', 1)`, secID, dummyCourseID)
	if err != nil {
		t.Fatalf("seeding dummy section: %v", err)
	}
	_, err = p.Exec(ctx, `INSERT INTO lessons (id, section_id, title, "order") VALUES ($1, $2, 'Les', 1)`, lesID, secID)
	if err != nil {
		t.Fatalf("seeding dummy lesson: %v", err)
	}
	_, err = p.Exec(ctx, `INSERT INTO videos (id, lesson_id, status) VALUES ($1, $2, 'READY')`, videoAssetID, lesID)
	if err != nil {
		t.Fatalf("seeding video: %v", err)
	}

	_, err = p.Exec(ctx, `
		INSERT INTO taxonomy_terms (id, kind, label_ar, label_en, academic_code) VALUES
		($1, 'MAJOR', 'تخصص', 'Major', NULL),
		($2, 'SUBJECT', 'مادة', 'Subject', 'SUBJ-01')
	`, majorTermID, subjectTermID)
	if err != nil {
		t.Fatalf("seeding taxonomy: %v", err)
	}

	return adminID, instructorID, videoAssetID, majorTermID, subjectTermID
}

func doAuthReq(ts *httptest.Server, method, path string, body []byte) (*http.Response, map[string]any) {
	req, _ := http.NewRequest(method, ts.URL+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://gradex.example")
	validCSRF := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
	validSession := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x41}, 32))
	req.Header.Set("X-CSRF-Token", validCSRF)
	req.AddCookie(&http.Cookie{
		Name:  "__Host-gradex_session",
		Value: validSession,
	})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()

	var res map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&res)
	return resp, res
}

func TestSubmissionValidationReportsAllDefectsInOneResponse(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)
	_, instructorID, videoAssetID, majorTermID, _ := seedReviewDatabase(t, p, ctx)

	ts := buildTestRouterWithAccount(t, p, instructorID, identity.RoleInstructor, identity.StatusActive)

	// Create course
	resp, body := doAuthReq(ts, "POST", "/api/v1/courses", []byte(`{"title_ar":"دورة تجريبية","title_en":"Demo Course"}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("creating course: status %d", resp.StatusCode)
	}
	courseID := body["id"].(string)
	revMap := body["editable_revision"].(map[string]any)
	revID := revMap["id"].(string)

	// Set Major taxonomy term only (leave Subject and StudyYear missing)
	_, _ = doAuthReq(ts, "PATCH", "/api/v1/courses/"+courseID+"/revisions/"+revID, []byte(`{"major_term_id":"`+majorTermID+`"}`))

	// Add section 1 with a lesson that has a video
	resp, secBody := doAuthReq(ts, "POST", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/sections", []byte(`{"title_ar":"فصل 1","title_en":"Section 1"}`))
	sec1ID := secBody["id"].(string)
	resp, lesBody := doAuthReq(ts, "POST", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/sections/"+sec1ID+"/lessons", []byte(`{"title_ar":"درس 1","title_en":"Lesson 1"}`))
	les1ID := lesBody["id"].(string)
	_, _ = doAuthReq(ts, "PUT", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/lessons/"+les1ID+"/video", []byte(`{"video_asset_version_id":"`+videoAssetID+`"}`))

	// Add section 2 with NO lessons (empty section)
	_, _ = doAuthReq(ts, "POST", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/sections", []byte(`{"title_ar":"فصل 2 فارغ","title_en":"Section 2 Empty"}`))

	// Add section 3 with lesson lacking video
	resp, sec3Body := doAuthReq(ts, "POST", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/sections", []byte(`{"title_ar":"فصل 3","title_en":"Section 3"}`))
	sec3ID := sec3Body["id"].(string)
	_, _ = doAuthReq(ts, "POST", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/sections/"+sec3ID+"/lessons", []byte(`{"title_ar":"درس بدون فيديو","title_en":"Lesson without video"}`))

	// Submit course -> expect 422 Unprocessable Entity with ALL failures in one response (FR-009, FR-010)
	resp, submitRes := doAuthReq(ts, "POST", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/submit", nil)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for incomplete submission, got %d: %v", resp.StatusCode, submitRes)
	}

	violationsRaw, ok := submitRes["violations"].([]any)
	if !ok {
		t.Fatalf("expected violations array in 422 response, got %v", submitRes)
	}

	// Must report ALL defects: empty section, lesson without video, missing Subject, missing StudyYear
	if len(violationsRaw) < 4 {
		t.Fatalf("expected at least 4 violations reported together, got %d: %v", len(violationsRaw), violationsRaw)
	}

	codes := make(map[string]bool)
	dimensions := make(map[string]bool)
	for _, v := range violationsRaw {
		vm, _ := v.(map[string]any)
		if code, ok := vm["code"].(string); ok {
			codes[code] = true
		}
		if dimension, ok := vm["dimension"].(string); ok {
			dimensions[dimension] = true
		}
	}

	if !codes["SECTION_EMPTY"] {
		t.Errorf("missing SECTION_EMPTY violation in response: %v", violationsRaw)
	}
	if !codes["LESSON_VIDEO_MISSING"] {
		t.Errorf("missing LESSON_VIDEO_MISSING violation in response: %v", violationsRaw)
	}
	if !codes["TAXONOMY_DIMENSION_MISSING"] {
		t.Errorf("missing TAXONOMY_DIMENSION_MISSING violation in response: %v", violationsRaw)
	}
	if !dimensions["SUBJECT"] {
		t.Errorf("missing SUBJECT taxonomy dimension violation: %v", violationsRaw)
	}
	if !dimensions["STUDY_YEAR"] {
		t.Errorf("missing STUDY_YEAR taxonomy dimension violation: %v", violationsRaw)
	}
}

func TestConcurrentSubmissionReturns409(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)
	_, instructorID, videoAssetID, majorTermID, subjectTermID := seedReviewDatabase(t, p, ctx)

	ts := buildTestRouterWithAccount(t, p, instructorID, identity.RoleInstructor, identity.StatusActive)

	// Create complete valid course
	resp, body := doAuthReq(ts, "POST", "/api/v1/courses", []byte(`{"title_ar":"دورة متكاملة","title_en":"Complete Course"}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create course status %d: %v", resp.StatusCode, body)
	}
	courseID := body["id"].(string)
	revMap := body["editable_revision"].(map[string]any)
	revID := revMap["id"].(string)

	studyYear := "YEAR_1"
	_, _ = doAuthReq(ts, "PATCH", "/api/v1/courses/"+courseID+"/revisions/"+revID, []byte(`{
		"major_term_id":"`+majorTermID+`",
		"subject_term_id":"`+subjectTermID+`",
		"study_year":"`+studyYear+`"
	}`))

	_, secBody := doAuthReq(ts, "POST", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/sections", []byte(`{"title_ar":"فصل 1","title_en":"Section 1"}`))
	secID := secBody["id"].(string)

	_, lesBody := doAuthReq(ts, "POST", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/sections/"+secID+"/lessons", []byte(`{"title_ar":"درس 1","title_en":"Lesson 1"}`))
	lesID := lesBody["id"].(string)

	_, _ = doAuthReq(ts, "PUT", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/lessons/"+lesID+"/video", []byte(`{"video_asset_version_id":"`+videoAssetID+`"}`))

	// Concurrent double submission: two parallel requests
	var wg sync.WaitGroup
	results := make([]int, 2)
	wg.Add(2)

	for i := 0; i < 2; i++ {
		idx := i
		go func() {
			defer wg.Done()
			r, _ := doAuthReq(ts, "POST", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/submit", nil)
			results[idx] = r.StatusCode
		}()
	}
	wg.Wait()

	// Exactly one succeeds (200) and the second gets 409 conflict
	successCount := 0
	conflictCount := 0
	for _, status := range results {
		if status == http.StatusOK {
			successCount++
		} else if status == http.StatusConflict {
			conflictCount++
		}
	}

	if successCount != 1 || conflictCount != 1 {
		t.Fatalf("expected 1 success (200) and 1 conflict (409) for concurrent submission, got statuses: %v", results)
	}
}

func TestEditingPendingReviewCourseIsRefused(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)
	_, instructorID, videoAssetID, majorTermID, subjectTermID := seedReviewDatabase(t, p, ctx)

	ts := buildTestRouterWithAccount(t, p, instructorID, identity.RoleInstructor, identity.StatusActive)

	// Create and submit complete course
	resp, body := doAuthReq(ts, "POST", "/api/v1/courses", []byte(`{"title_ar":"دورة للمراجعة","title_en":"Review Course"}`))
	courseID := body["id"].(string)
	revMap := body["editable_revision"].(map[string]any)
	revID := revMap["id"].(string)

	_, _ = doAuthReq(ts, "PATCH", "/api/v1/courses/"+courseID+"/revisions/"+revID, []byte(`{"major_term_id":"`+majorTermID+`","subject_term_id":"`+subjectTermID+`","study_year":"YEAR_1"}`))
	resp, secBody := doAuthReq(ts, "POST", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/sections", []byte(`{"title_ar":"فصل","title_en":"Section"}`))
	secID := secBody["id"].(string)
	resp, lesBody := doAuthReq(ts, "POST", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/sections/"+secID+"/lessons", []byte(`{"title_ar":"درس","title_en":"Lesson"}`))
	lesID := lesBody["id"].(string)
	_, _ = doAuthReq(ts, "PUT", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/lessons/"+lesID+"/video", []byte(`{"video_asset_version_id":"`+videoAssetID+`"}`))

	resp, _ = doAuthReq(ts, "POST", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/submit", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("submit failed: status %d", resp.StatusCode)
	}

	// Every mutation attempt on PENDING_REVIEW course must return 409 State Conflict
	mutations := []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{"update_course", "PATCH", "/api/v1/courses/" + courseID + "/revisions/" + revID, []byte(`{"title_en":"New Title"}`)},
		{"add_section", "POST", "/api/v1/courses/" + courseID + "/revisions/" + revID + "/sections", []byte(`{"title_ar":"فصل جديد","title_en":"New Section"}`)},
		{"add_lesson", "POST", "/api/v1/courses/" + courseID + "/revisions/" + revID + "/sections/" + secID + "/lessons", []byte(`{"title_ar":"درس جديد","title_en":"New Lesson"}`)},
		{"set_video", "PUT", "/api/v1/courses/" + courseID + "/revisions/" + revID + "/lessons/" + lesID + "/video", []byte(`{"video_asset_version_id":"` + videoAssetID + `"}`)},
		{"submit_again", "POST", "/api/v1/courses/" + courseID + "/revisions/" + revID + "/submit", nil},
	}

	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			r, _ := doAuthReq(ts, m.method, m.path, m.body)
			if r.StatusCode != http.StatusConflict {
				t.Fatalf("%s on PENDING_REVIEW course returned status %d, want 409 StateConflict", m.name, r.StatusCode)
			}
		})
	}
}

func TestAdminReviewFlowApproveRequestChangesAndPreview(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)
	adminID, instructorID, videoAssetID, majorTermID, subjectTermID := seedReviewDatabase(t, p, ctx)

	instructorTS := buildTestRouterWithAccount(t, p, instructorID, identity.RoleInstructor, identity.StatusActive)
	adminTS := buildTestRouterWithAccount(t, p, adminID, identity.RoleAdmin, identity.StatusActive)

	// 1. Create and submit course as Instructor
	resp, body := doAuthReq(instructorTS, "POST", "/api/v1/courses", []byte(`{"title_ar":"دورة مراجعة أدمن","title_en":"Admin Review Course"}`))
	courseID := body["id"].(string)
	revMap := body["editable_revision"].(map[string]any)
	revID := revMap["id"].(string)

	_, _ = doAuthReq(instructorTS, "PATCH", "/api/v1/courses/"+courseID+"/revisions/"+revID, []byte(`{"major_term_id":"`+majorTermID+`","subject_term_id":"`+subjectTermID+`","study_year":"YEAR_2"}`))
	resp, secBody := doAuthReq(instructorTS, "POST", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/sections", []byte(`{"title_ar":"فصل","title_en":"Section"}`))
	secID := secBody["id"].(string)
	resp, lesBody := doAuthReq(instructorTS, "POST", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/sections/"+secID+"/lessons", []byte(`{"title_ar":"درس","title_en":"Lesson"}`))
	lesID := lesBody["id"].(string)
	_, _ = doAuthReq(instructorTS, "PUT", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/lessons/"+lesID+"/video", []byte(`{"video_asset_version_id":"`+videoAssetID+`"}`))
	_, _ = doAuthReq(instructorTS, "POST", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/submit", nil)

	// 2. Admin lists queue
	resp, _ = doAuthReq(adminTS, "GET", "/api/v1/admin/review/queue", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin queue returned status %d", resp.StatusCode)
	}

	// 3. Admin previews lesson video (T028, BR-081)
	resp, prevRes := doAuthReq(adminTS, "POST", "/api/v1/admin/review/courses/"+courseID+"/revisions/"+revID+"/preview/"+lesID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin preview returned status %d: %v", resp.StatusCode, prevRes)
	}
	if prevRes["video_asset_version_id"] != videoAssetID {
		t.Errorf("preview returned video %v, want %s", prevRes["video_asset_version_id"], videoAssetID)
	}

	// Assert ADMIN_CONTENT_PREVIEWED audit row exists in DB
	var auditCount int
	err := p.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'ADMIN_CONTENT_PREVIEWED' AND target_id = $1`, lesID).Scan(&auditCount)
	if err != nil || auditCount == 0 {
		t.Fatalf("expected ADMIN_CONTENT_PREVIEWED audit event in database, got count %d, err %v", auditCount, err)
	}

	// 4. Request-changes requires reason (BR-072)
	resp, _ = doAuthReq(adminTS, "POST", "/api/v1/admin/review/courses/"+courseID+"/revisions/"+revID+"/request-changes", []byte(`{"reason":"  "}`))
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("request-changes with empty reason returned status %d, want 422", resp.StatusCode)
	}

	resp, _ = doAuthReq(adminTS, "POST", "/api/v1/admin/review/courses/"+courseID+"/revisions/"+revID+"/request-changes", []byte(`{"reason":"Need more detailed description in Arabic"}`))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request-changes returned status %d", resp.StatusCode)
	}

	// Verify course moved to CHANGES_REQUESTED
	var lifecycle string
	_ = p.QueryRow(ctx, `SELECT lifecycle FROM courses WHERE id = $1::uuid`, courseID).Scan(&lifecycle)
	if lifecycle != "CHANGES_REQUESTED" {
		t.Fatalf("expected course lifecycle CHANGES_REQUESTED, got %s", lifecycle)
	}

	// Resubmit course
	_, _ = doAuthReq(instructorTS, "POST", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/submit", nil)

	// 5. Admin approves course (T027)
	resp, approveRes := doAuthReq(adminTS, "POST", "/api/v1/admin/review/courses/"+courseID+"/revisions/"+revID+"/approve", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin approve returned status %d: %v", resp.StatusCode, approveRes)
	}

	_ = p.QueryRow(ctx, `SELECT lifecycle FROM courses WHERE id = $1::uuid`, courseID).Scan(&lifecycle)
	if lifecycle != "PUBLISHED" {
		t.Fatalf("expected course lifecycle PUBLISHED after approve, got %s", lifecycle)
	}

	// Verify COURSE_PUBLISHED audit row and notification intent in DB
	err = p.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'COURSE_PUBLISHED' AND target_id = $1`, courseID).Scan(&auditCount)
	if err != nil || auditCount == 0 {
		t.Fatalf("expected COURSE_PUBLISHED audit event in database, got count %d", auditCount)
	}

	var outboxCount int
	err = p.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE event_type = 'catalog.course_published'`).Scan(&outboxCount)
	if err != nil || outboxCount == 0 {
		t.Fatalf("expected catalog.course_published outbox event in database, got count %d", outboxCount)
	}
}

func TestInstructorCannotPublishThroughReviewRoutes(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)
	_, instructorID, videoAssetID, majorTermID, subjectTermID := seedReviewDatabase(t, p, ctx)

	instructorTS := buildTestRouterWithAccount(t, p, instructorID, identity.RoleInstructor, identity.StatusActive)

	// Create and submit course
	resp, body := doAuthReq(instructorTS, "POST", "/api/v1/courses", []byte(`{"title_ar":"دورة","title_en":"Course"}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create course status %d: %v", resp.StatusCode, body)
	}
	courseID := body["id"].(string)
	revMap := body["editable_revision"].(map[string]any)
	revID := revMap["id"].(string)

	_, _ = doAuthReq(instructorTS, "PATCH", "/api/v1/courses/"+courseID+"/revisions/"+revID, []byte(`{"major_term_id":"`+majorTermID+`","subject_term_id":"`+subjectTermID+`","study_year":"YEAR_1"}`))
	resp, secBody := doAuthReq(instructorTS, "POST", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/sections", []byte(`{"title_ar":"فصل","title_en":"Section"}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create section status %d: %v", resp.StatusCode, secBody)
	}
	secID := secBody["id"].(string)
	resp, lesBody := doAuthReq(instructorTS, "POST", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/sections/"+secID+"/lessons", []byte(`{"title_ar":"درس","title_en":"Lesson"}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create lesson status %d: %v", resp.StatusCode, lesBody)
	}
	lesID := lesBody["id"].(string)
	_, _ = doAuthReq(instructorTS, "PUT", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/lessons/"+lesID+"/video", []byte(`{"video_asset_version_id":"`+videoAssetID+`"}`))
	_, _ = doAuthReq(instructorTS, "POST", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/submit", nil)

	// Owning Instructor attempts direct API calls to all review routes -> MUST be refused 403 Forbidden (T030, FR-013)
	reviewRoutes := []struct {
		method string
		path   string
		body   []byte
	}{
		{"GET", "/api/v1/admin/review/queue", nil},
		{"GET", "/api/v1/admin/review/courses/" + courseID + "/revisions/" + revID, nil},
		{"POST", "/api/v1/admin/review/courses/" + courseID + "/revisions/" + revID + "/approve", nil},
		{"POST", "/api/v1/admin/review/courses/" + courseID + "/revisions/" + revID + "/request-changes", []byte(`{"reason":"unauthorized"}`)},
		{"POST", "/api/v1/admin/review/courses/" + courseID + "/revisions/" + revID + "/preview/" + lesID, nil},
	}

	for _, rt := range reviewRoutes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			r, res := doAuthReq(instructorTS, rt.method, rt.path, rt.body)
			if r.StatusCode != http.StatusForbidden {
				t.Fatalf("Instructor call to review route %s %s returned status %d, want 403 NOT_AUTHORIZED (got %v)", rt.method, rt.path, r.StatusCode, res)
			}
		})
	}
}

func TestApprovalRevalidatesOwnerSuspension(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)
	adminID, instructorID, videoAssetID, majorTermID, subjectTermID := seedReviewDatabase(t, p, ctx)

	instructorTS := buildTestRouterWithAccount(t, p, instructorID, identity.RoleInstructor, identity.StatusActive)
	adminTS := buildTestRouterWithAccount(t, p, adminID, identity.RoleAdmin, identity.StatusActive)

	// Create and submit course as Instructor
	resp, body := doAuthReq(instructorTS, "POST", "/api/v1/courses", []byte(`{"title_ar":"دورة للمراجعة","title_en":"Review Course"}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("creating course: status %d", resp.StatusCode)
	}
	courseID := body["id"].(string)
	revMap := body["editable_revision"].(map[string]any)
	revID := revMap["id"].(string)

	_, _ = doAuthReq(instructorTS, "PATCH", "/api/v1/courses/"+courseID+"/revisions/"+revID, []byte(`{"major_term_id":"`+majorTermID+`","subject_term_id":"`+subjectTermID+`","study_year":"YEAR_1"}`))
	resp, secBody := doAuthReq(instructorTS, "POST", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/sections", []byte(`{"title_ar":"فصل","title_en":"Section"}`))
	secID := secBody["id"].(string)
	resp, lesBody := doAuthReq(instructorTS, "POST", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/sections/"+secID+"/lessons", []byte(`{"title_ar":"درس","title_en":"Lesson"}`))
	lesID := lesBody["id"].(string)
	_, _ = doAuthReq(instructorTS, "PUT", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/lessons/"+lesID+"/video", []byte(`{"video_asset_version_id":"`+videoAssetID+`"}`))
	_, _ = doAuthReq(instructorTS, "POST", "/api/v1/courses/"+courseID+"/revisions/"+revID+"/submit", nil)

	// Suspend owner account in DB before Admin approves course (Case 3 revalidation)
	_, err := p.Exec(ctx, `UPDATE accounts SET status = 'SUSPENDED' WHERE id = $1::uuid`, instructorID)
	if err != nil {
		t.Fatalf("suspending owner account: %v", err)
	}

	// Admin approves course -> MUST fail due to owner account status revalidation inside approving transaction
	resp, approveRes := doAuthReq(adminTS, "POST", "/api/v1/admin/review/courses/"+courseID+"/revisions/"+revID+"/approve", nil)
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("Admin approve succeeded despite owner account being SUSPENDED, want failure: %v", approveRes)
	}
}
