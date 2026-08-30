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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/media"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

type reviewDelivery struct{ refusingDelivery }

func (*reviewDelivery) IssueAdminReviewPlayback(_ context.Context, request media.AdminReviewPlaybackRequest) (media.PlaybackAuthorization, error) {
	return media.PlaybackAuthorization{
		AssetVersionID:  request.AssetVersionID,
		PlaybackSession: "review-test-session",
		ManifestURL:     "/api/v1/admin/review/playback-manifests/review-test-session/index.m3u8",
		ExpiresAt:       time.Now().Add(time.Minute),
	}, nil
}

func (*reviewDelivery) IssueAdminReviewPlaybackManifest(context.Context, string, string) (media.PlaybackManifest, error) {
	return media.PlaybackManifest{Contents: []byte("#EXTM3U\n#EXT-X-ENDLIST\n")}, nil
}

func (*reviewDelivery) IssueAdminReviewPlaybackRenditionManifest(context.Context, string, string, string) (media.PlaybackManifest, error) {
	return media.PlaybackManifest{Contents: []byte("#EXTM3U\n#EXT-X-ENDLIST\n")}, nil
}

func reviewMediaFoundation(t *testing.T, pool *pgxpool.Pool) *MediaFoundation {
	t.Helper()
	writer, err := outbox.NewWriter("review-test", []byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("creating review outbox writer: %v", err)
	}
	unavailable, err := media.NewUnavailableScanner("review route fixture")
	if err != nil {
		t.Fatalf("creating review scanner: %v", err)
	}
	scanner, err := media.NewScannerAdapter(unavailable)
	if err != nil {
		t.Fatalf("creating review scanner adapter: %v", err)
	}
	service, err := media.NewService(media.ServiceOptions{
		DB: pool, Store: &mediaRouterStore{}, Outbox: writer, Scanner: scanner,
		UploadURLExpiry: time.Minute, MaxUploadBytes: 1024,
	})
	if err != nil {
		t.Fatalf("creating review media service: %v", err)
	}
	foundation, err := NewMediaFoundation(MediaFoundationOptions{Service: service, Delivery: &reviewDelivery{}})
	if err != nil {
		t.Fatalf("creating review media foundation: %v", err)
	}
	return foundation
}

// T4-B academic fixture identifiers. Ordinary Instructor Course creation is now
// Academic Catalog based (D-093 §1), so a test that creates a Course through the
// product API needs a real Institution and a real canonical Subject.
const (
	reviewInstitutionID = "aaaa9999-0000-0000-0000-000000000001"
	reviewSubjectID     = "bbbb9999-0000-0000-0000-000000000001"
	reviewSubjectAltID  = "bbbb9999-0000-0000-0000-000000000002"
)

// academicCourseBody is the ordinary T4-B create payload: university, Subject,
// and the Course's own copy. There is no classification field — the server
// derives ACADEMIC_CATALOG from the academic context.
func academicCourseBody(titleAr, titleEn string) []byte {
	return []byte(`{"title_ar":"` + titleAr + `","title_en":"` + titleEn +
		`","institution_id":"` + reviewInstitutionID + `","subject_id":"` + reviewSubjectID + `"}`)
}

// seedLegacyCourseFixture builds a LEGACY_TAXONOMY Course directly, which the
// ordinary Instructor API deliberately can no longer do (D-093 §1, T4-B §18).
//
// This is a fixture path, not a product path. It exists so the legacy behaviour
// that must survive until T5 stays under test: an existing Course created before
// the redesign still validates, edits, submits, and reviews exactly as it did.
func seedLegacyCourseFixture(
	t *testing.T, p *pgxpool.Pool, ctx context.Context, instructorID, titleAr, titleEn string,
) (courseID, revisionID string) {
	t.Helper()
	if err := p.QueryRow(ctx, `
		INSERT INTO courses (owner_account_id, lifecycle, classification_model)
		VALUES ($1::uuid, 'DRAFT', 'LEGACY_TAXONOMY') RETURNING id::text`, instructorID).Scan(&courseID); err != nil {
		t.Fatalf("seeding legacy course: %v", err)
	}
	if err := p.QueryRow(ctx, `
		INSERT INTO course_revisions (course_id, state, revision_number, title_ar, title_en)
		VALUES ($1::uuid, 'DRAFT', 1, $2, $3) RETURNING id::text`,
		courseID, titleAr, titleEn).Scan(&revisionID); err != nil {
		t.Fatalf("seeding legacy revision: %v", err)
	}
	return courseID, revisionID
}

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

	// The Academic Catalog an ordinary T4-B Course is authored against.
	_, err = p.Exec(ctx, `
		INSERT INTO institutions (id, country_code, slug, name_ar, name_en)
		VALUES ($1, 'KW', 'review-university', 'جامعة', 'Review University')`, reviewInstitutionID)
	if err != nil {
		t.Fatalf("seeding institution: %v", err)
	}
	_, err = p.Exec(ctx, `
		INSERT INTO subjects (id, institution_id, official_code, title_ar, title_en) VALUES
		($1, $3, '0418-320', 'مبادئ نظم الحاسوب', 'Principles of Computer Systems'),
		($2, $3, '0418-321', 'نظم التشغيل', 'Operating Systems')`,
		reviewSubjectID, reviewSubjectAltID, reviewInstitutionID)
	if err != nil {
		t.Fatalf("seeding subjects: %v", err)
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

// hasSubmissionViolation reads the frozen `violations` array of a
// submission-incomplete problem response.
func hasSubmissionViolation(res map[string]any, code string) bool {
	violations, _ := res["violations"].([]any)
	for _, raw := range violations {
		violation, _ := raw.(map[string]any)
		if violation["code"] == code {
			return true
		}
	}
	return false
}

func TestSubmissionValidationReportsAllDefectsInOneResponse(t *testing.T) {
	freshSchema(t)
	p, ctx := pool(t)
	_, instructorID, videoAssetID, majorTermID, _ := seedReviewDatabase(t, p, ctx)

	ts := buildTestRouterWithAccount(t, p, instructorID, identity.RoleInstructor, identity.StatusActive)

	// Create course
	// LEGACY compatibility fixture (T4-B §48). Ordinary Instructor creation is
	// Academic Catalog based now, so this Course is constructed explicitly as
	// LEGACY_TAXONOMY to keep proving that an existing pre-redesign Course still
	// works end to end through review until T5 migrates it.
	courseID, revID := seedLegacyCourseFixture(t, p, ctx, instructorID, "دورة تجريبية", "Demo Course")

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
	// LEGACY compatibility fixture (T4-B §48). Ordinary Instructor creation is
	// Academic Catalog based now, so this Course is constructed explicitly as
	// LEGACY_TAXONOMY to keep proving that an existing pre-redesign Course still
	// works end to end through review until T5 migrates it.
	courseID, revID := seedLegacyCourseFixture(t, p, ctx, instructorID, "دورة متكاملة", "Complete Course")

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
	// LEGACY compatibility fixture (T4-B §48). Ordinary Instructor creation is
	// Academic Catalog based now, so this Course is constructed explicitly as
	// LEGACY_TAXONOMY to keep proving that an existing pre-redesign Course still
	// works end to end through review until T5 migrates it.
	courseID, revID := seedLegacyCourseFixture(t, p, ctx, instructorID, "دورة للمراجعة", "Review Course")

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

	mediaFoundation := reviewMediaFoundation(t, p)
	instructorTS := buildTestRouterWithAccount(t, p, instructorID, identity.RoleInstructor, identity.StatusActive, WithMediaFoundation(mediaFoundation))
	adminTS := buildTestRouterWithAccount(t, p, adminID, identity.RoleAdmin, identity.StatusActive, WithMediaFoundation(mediaFoundation))

	// 1. Create and submit course as Instructor
	// LEGACY compatibility fixture (T4-B §48). Ordinary Instructor creation is
	// Academic Catalog based now, so this Course is constructed explicitly as
	// LEGACY_TAXONOMY to keep proving that an existing pre-redesign Course still
	// works end to end through review until T5 migrates it.
	courseID, revID := seedLegacyCourseFixture(t, p, ctx, instructorID, "دورة مراجعة أدمن", "Admin Review Course")
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
	if previewURL, _ := prevRes["playback_url"].(string); previewURL != "/api/v1/admin/review/playback-manifests/review-test-session/index.m3u8" {
		t.Errorf("preview returned protected manifest %q", previewURL)
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

	// 5. Approval is refused over the wire until the Course carries a launch
	// price. The route is the only publication entry point, so this is the
	// direct-API proof that the invariant is not a frontend courtesy.
	resp, unpricedRes := doAuthReq(adminTS, "POST", "/api/v1/admin/review/courses/"+courseID+"/revisions/"+revID+"/approve", nil)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("unpriced approve returned status %d, want 422: %v", resp.StatusCode, unpricedRes)
	}
	if !hasSubmissionViolation(unpricedRes, "COURSE_PRICE_REQUIRED") {
		t.Fatalf("unpriced approve response lacks COURSE_PRICE_REQUIRED: %v", unpricedRes)
	}
	_ = p.QueryRow(ctx, `SELECT lifecycle FROM courses WHERE id = $1::uuid`, courseID).Scan(&lifecycle)
	if lifecycle == "PUBLISHED" {
		t.Fatalf("refused approve published the course anyway")
	}
	var refusedPublishAudits int
	_ = p.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'COURSE_PUBLISHED' AND target_id = $1`, courseID).Scan(&refusedPublishAudits)
	if refusedPublishAudits != 0 {
		t.Fatalf("refused approve wrote %d COURSE_PUBLISHED audit events", refusedPublishAudits)
	}

	// 6. Admin sets the Course launch price, then approves (T027)
	resp, priceRes := doAuthReq(adminTS, "PUT", "/api/v1/admin/courses/"+courseID+"/price", []byte(`{"price_minor_units":25000,"reason":"Launch price"}`))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setting course price returned status %d: %v", resp.StatusCode, priceRes)
	}

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
	// LEGACY compatibility fixture (T4-B §48). Ordinary Instructor creation is
	// Academic Catalog based now, so this Course is constructed explicitly as
	// LEGACY_TAXONOMY to keep proving that an existing pre-redesign Course still
	// works end to end through review until T5 migrates it.
	courseID, revID := seedLegacyCourseFixture(t, p, ctx, instructorID, "دورة", "Course")
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
		{"GET", "/api/v1/admin/review/playback-manifests/not-a-review-session/index.m3u8", nil},
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
	// LEGACY compatibility fixture (T4-B §48). Ordinary Instructor creation is
	// Academic Catalog based now, so this Course is constructed explicitly as
	// LEGACY_TAXONOMY to keep proving that an existing pre-redesign Course still
	// works end to end through review until T5 migrates it.
	courseID, revID := seedLegacyCourseFixture(t, p, ctx, instructorID, "دورة للمراجعة", "Review Course")
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
