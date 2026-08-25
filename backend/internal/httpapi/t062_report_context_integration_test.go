//go:build integration

package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Owlah2025/gradex/backend/internal/learning"
)

// T062 production issuance evidence (D-065).
//
// The domain tests prove a report stores whatever instance its binding names. These prove the
// running read models hand the browser a context bound to the instance *they just rendered*, so a
// report filed from a stale page names what the Student actually saw rather than whatever went
// live afterwards.

// verifierFor mirrors the deterministic issuer the test foundation composes, so a response's
// opaque token can be decrypted exactly as T063 will decrypt it.
func verifierFor(t *testing.T) *learning.ReportContextSigner {
	t.Helper()
	return testReportContextIssuer(t).(*learning.ReportContextSigner)
}

func sessionIDFor(studentID string) string { return "test-session-" + studentID }

func liveRevisionOf(t *testing.T, f learningIntegrationFixture) string {
	t.Helper()
	var revision string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT live_revision_id::text FROM courses WHERE id = $1::uuid`, f.courseID).Scan(&revision); err != nil {
		t.Fatalf("resolving live revision: %v", err)
	}
	return revision
}

// courseContextOf returns the opaque token from an active Course Home response.
func courseContextOf(t *testing.T, f learningIntegrationFixture) string {
	t.Helper()
	response := f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID, "")
	if response.Code != http.StatusOK {
		t.Fatalf("course home = %d", response.Code)
	}
	var body struct {
		ReportContext string `json:"report_context"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding course home: %v", err)
	}
	if body.ReportContext == "" {
		t.Fatal("active Course Home issued no report context")
	}
	return body.ReportContext
}

type lessonContexts struct {
	Lesson      string `json:"lesson"`
	Video       string `json:"video"`
	Resource    string `json:"resource"`
	LabMaterial string `json:"lab_material"`
}

func lessonContextsOf(t *testing.T, f learningIntegrationFixture) lessonContexts {
	t.Helper()
	response := f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID+"/lessons/"+f.lessonID, "")
	if response.Code != http.StatusOK {
		t.Fatalf("lesson read = %d %s", response.Code, response.Body.String())
	}
	var body struct {
		ReportContexts lessonContexts `json:"report_contexts"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding lesson: %v", err)
	}
	return body.ReportContexts
}

// attachLessonMaterial adds a READY Resource or Lab Material to the live revision's Lesson row.
func attachLessonMaterial(t *testing.T, f learningIntegrationFixture, kind, objectKey string) string {
	t.Helper()
	ctx := context.Background()
	var instructorID string
	if err := f.pool.QueryRow(ctx, `SELECT owner_account_id::text FROM courses WHERE id = $1::uuid`, f.courseID).Scan(&instructorID); err != nil {
		t.Fatalf("resolving instructor: %v", err)
	}
	assetID, versionID, scanID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO media_assets (id, kind, owner_account_id, course_id, lesson_id, visibility) VALUES ($1::uuid, $2::media_asset_kind, $3::uuid, $4::uuid, $5::uuid, 'PROTECTED')`,
			[]any{assetID, kind, instructorID, f.courseID, f.lessonID}},
		{`INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes) VALUES ($1::uuid, $2::uuid, $3::media_asset_kind, 'SCANNING', $4, $5, 'application/pdf', 1)`,
			[]any{versionID, assetID, kind, objectKey, objectKey}},
		{`INSERT INTO scan_attempts (id, asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity) VALUES ($1::uuid, $2::uuid, 1, $3, $4, 'PASSED', 'test-scanner')`,
			[]any{scanID, versionID, objectKey, objectKey}},
		{`UPDATE media_asset_versions SET state = 'SCAN_PASSED', successful_scan_attempt_id = $2::uuid WHERE id = $1::uuid`,
			[]any{versionID, scanID}},
		{`UPDATE media_asset_versions SET state = 'READY' WHERE id = $1::uuid`, []any{versionID}},
		{`INSERT INTO lesson_files (lesson_id, kind, asset_version_id, display_name_ar, display_name_en, position)
		  SELECT cl.id, $2::lesson_file_kind, $3::uuid, 'ملف', 'File', 0
		  FROM course_lessons cl
		  JOIN courses c ON c.id = cl.course_id AND c.live_revision_id = (SELECT revision_id FROM course_sections WHERE id = cl.section_id)
		  WHERE cl.lesson_identity_id = $1::uuid`,
			[]any{f.lessonID, kind, versionID}},
	}
	for _, statement := range statements {
		if _, err := f.pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("attaching %s: %v\n%s", kind, err, statement.query)
		}
	}
	return versionID
}

// TestActiveReadsIssueContextsBoundToTheRenderedInstance covers every issuance surface at once.
func TestActiveReadsIssueContextsBoundToTheRenderedInstance(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	resourceVersion := attachLessonMaterial(t, f, "RESOURCE", "resource-a")
	labVersion := attachLessonMaterial(t, f, "LAB_MATERIAL", "lab-a")
	verifier := verifierFor(t)
	revision := liveRevisionOf(t, f)
	session := sessionIDFor(f.studentID)

	// Course Home: exactly one COURSE context, bound to the Revision this response rendered.
	courseToken := courseContextOf(t, f)
	courseBinding, err := verifier.Verify(courseToken, f.studentID, session)
	if err != nil {
		t.Fatalf("verifying course context: %v", err)
	}
	if courseBinding.TargetKind != learning.ReportTargetCourse || courseBinding.StableTargetID != f.courseID {
		t.Fatalf("course context bound %+v", courseBinding)
	}
	if courseBinding.VisibleCourseRevisionID != revision {
		t.Fatalf("course context revision %s, want response revision %s", courseBinding.VisibleCourseRevisionID, revision)
	}
	if courseBinding.VisibleAssetVersionID != "" {
		t.Fatal("a COURSE context must carry no Asset Version")
	}

	// Lesson: one context per target actually present in the visible graph.
	contexts := lessonContextsOf(t, f)
	for name, token := range map[string]string{
		"lesson": contexts.Lesson, "video": contexts.Video,
		"resource": contexts.Resource, "lab_material": contexts.LabMaterial,
	} {
		if token == "" {
			t.Fatalf("%s context missing from an active Lesson read", name)
		}
	}

	expected := map[string]struct {
		kind    learning.ReportTargetKind
		version string
	}{
		contexts.Lesson:      {learning.ReportTargetLesson, ""},
		contexts.Video:       {learning.ReportTargetVideo, f.versionID},
		contexts.Resource:    {learning.ReportTargetResource, resourceVersion},
		contexts.LabMaterial: {learning.ReportTargetLabMaterial, labVersion},
	}
	for token, want := range expected {
		binding, err := verifier.Verify(token, f.studentID, session)
		if err != nil {
			t.Fatalf("verifying %s context: %v", want.kind, err)
		}
		if binding.TargetKind != want.kind {
			t.Fatalf("kind %s, want %s", binding.TargetKind, want.kind)
		}
		if binding.StableTargetID != f.lessonID {
			t.Fatalf("%s bound target %s, want stable Lesson %s", want.kind, binding.StableTargetID, f.lessonID)
		}
		if binding.VisibleCourseRevisionID != revision {
			t.Fatalf("%s bound revision %s, want %s", want.kind, binding.VisibleCourseRevisionID, revision)
		}
		if binding.VisibleAssetVersionID != want.version {
			t.Fatalf("%s bound version %s, want exact visible %s", want.kind, binding.VisibleAssetVersionID, want.version)
		}
		// Every context comes from the same rendered snapshot and the same principal.
		if binding.CourseID != f.courseID || binding.ReporterAccountID != f.studentID || binding.SessionID != session {
			t.Fatalf("%s context bound a different principal or Course: %+v", want.kind, binding)
		}
	}

	// Opacity: no internal identity is recoverable from the public token.
	for _, token := range []string{courseToken, contexts.Lesson, contexts.Video, contexts.Resource, contexts.LabMaterial} {
		envelope, err := base64.RawURLEncoding.DecodeString(strings.SplitN(token, ".", 2)[1])
		if err != nil {
			t.Fatalf("decoding envelope: %v", err)
		}
		for _, secret := range []string{f.courseID, f.lessonID, f.versionID, revision, resourceVersion, labVersion, f.studentID} {
			if strings.Contains(token, secret) || strings.Contains(string(envelope), secret) {
				t.Fatal("a report context exposed an internal identifier")
			}
		}
		var probe map[string]any
		if json.Unmarshal(envelope, &probe) == nil {
			t.Fatal("a report context envelope decoded as JSON; it is not encrypted")
		}
	}
}

// TestOnlyPresentTargetKindsReceiveContexts proves absent kinds get no context at all.
func TestOnlyPresentTargetKindsReceiveContexts(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	contexts := lessonContextsOf(t, f)

	if contexts.Lesson == "" || contexts.Video == "" {
		t.Fatal("a Lesson with a video must receive LESSON and VIDEO contexts")
	}
	// No Resource or Lab Material is attached, so neither may be offered.
	if contexts.Resource != "" || contexts.LabMaterial != "" {
		t.Fatalf("absent material kinds received contexts: resource=%t lab=%t",
			contexts.Resource != "", contexts.LabMaterial != "")
	}
}

// TestExpiredAndUnavailableReadsIssueNoContext covers every omission surface.
func TestExpiredAndUnavailableReadsIssueNoContext(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	ctx := context.Background()

	// Dashboard never issues contexts.
	dashboard := f.request(http.MethodGet, "/api/v1/learn/dashboard", "")
	if dashboard.Code != http.StatusOK {
		t.Fatalf("dashboard = %d", dashboard.Code)
	}
	if strings.Contains(dashboard.Body.String(), "report_context") {
		t.Fatal("Dashboard issued a report context")
	}

	// Retained-expired reads keep their content but offer no reporting context.
	if _, err := f.pool.Exec(ctx,
		`UPDATE entitlements SET access_ends_at = $1, original_access_ends_at = $1 WHERE student_account_id = $2::uuid`,
		f.clock.Now().Add(-time.Hour), f.studentID); err != nil {
		t.Fatalf("expiring entitlement: %v", err)
	}

	home := f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID, "")
	if home.Code != http.StatusOK {
		t.Fatalf("retained-expired course home = %d", home.Code)
	}
	if strings.Contains(home.Body.String(), "report_context") {
		t.Fatal("retained-expired Course Home issued a report context")
	}
	lesson := f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID+"/lessons/"+f.lessonID, "")
	if lesson.Code != http.StatusOK {
		t.Fatalf("retained-expired lesson = %d", lesson.Code)
	}
	if strings.Contains(lesson.Body.String(), "report_context") {
		t.Fatal("retained-expired Lesson issued report contexts")
	}

	// A generically unavailable read reveals nothing, contexts included.
	unavailable := f.request(http.MethodGet, "/api/v1/learn/courses/"+uuid.NewString(), "")
	if unavailable.Code != http.StatusNotFound {
		t.Fatalf("unavailable course = %d", unavailable.Code)
	}
	if strings.Contains(unavailable.Body.String(), "report_context") {
		t.Fatal("an unavailable read issued a report context")
	}
}

// TestIssuanceAddsNoDatabaseQueries proves minting is in-memory: the accepted D-063 counts are
// unchanged by report contexts.
func TestIssuanceAddsNoDatabaseQueries(t *testing.T) {
	f := newLearningIntegrationFixture(t)

	f.queries.reset()
	if response := f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID, ""); response.Code != http.StatusOK {
		t.Fatalf("course home = %d", response.Code)
	}
	if got := f.queries.get("learning.graph"); got != 2 {
		t.Fatalf("Course Home graph queries = %d, want the accepted 2", got)
	}
	if got := f.queries.get("learning.enrollment"); got != 1 {
		t.Fatalf("Course Home enrollment queries = %d, want 1", got)
	}
	for _, name := range []string{"learning.report.binding", "learning.report.insert"} {
		if f.queries.get(name) != 0 {
			t.Fatalf("Course Home performed report-domain query %q", name)
		}
	}

	f.queries.reset()
	if response := f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID+"/lessons/"+f.lessonID, ""); response.Code != http.StatusOK {
		t.Fatalf("lesson = %d", response.Code)
	}
	// The accepted D-063 count, unchanged by issuance: contexts add no query of their own.
	if got := f.queries.get("learning.graph"); got != 2 {
		t.Fatalf("Lesson graph queries = %d, want the accepted 2", got)
	}
	for _, name := range []string{"learning.report.binding", "learning.report.insert"} {
		if f.queries.get(name) != 0 {
			t.Fatalf("Lesson performed report-domain query %q", name)
		}
	}
}

// TestRunningReadModelStalePageReportsWhatItRendered is T062's completion evidence: the real read
// model issues context A, B goes live, and a report filed with A stores A.
func TestRunningReadModelStalePageReportsWhatItRendered(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	ctx := context.Background()
	resourceA := attachLessonMaterial(t, f, "RESOURCE", "resource-a")
	labA := attachLessonMaterial(t, f, "LAB_MATERIAL", "lab-a")
	verifier := verifierFor(t)
	session := sessionIDFor(f.studentID)
	revisionA := liveRevisionOf(t, f)

	// 1–2. The Student's open page: contexts minted from revision A with versions A.
	staleCourse := courseContextOf(t, f)
	staleLesson := lessonContextsOf(t, f)

	// 3. B becomes current — a new approved revision carrying the same stable Lesson identity.
	f.replaceLiveRevision(t)
	revisionB := liveRevisionOf(t, f)
	if revisionB == revisionA {
		t.Fatal("revision B did not become live")
	}

	reportRepository, err := learning.NewRepository(f.pool)
	if err != nil {
		t.Fatalf("constructing report repository: %v", err)
	}
	clock := func() time.Time { return f.clock.Now() }

	// 4–6. Every stale context still reports the instance the Student saw.
	stale := []struct {
		name    string
		token   string
		want    string
		notWant string
	}{
		{"COURSE", staleCourse, revisionA, revisionB},
		{"LESSON", staleLesson.Lesson, revisionA, revisionB},
		{"VIDEO", staleLesson.Video, f.versionID, ""},
		{"RESOURCE", staleLesson.Resource, resourceA, ""},
		{"LAB_MATERIAL", staleLesson.LabMaterial, labA, ""},
	}
	for _, testCase := range stale {
		t.Run(testCase.name, func(t *testing.T) {
			binding, err := verifier.Verify(testCase.token, f.studentID, session)
			if err != nil {
				t.Fatalf("verifying stale context: %v", err)
			}
			if binding.VisibleCourseRevisionID != revisionA && binding.TargetKind != learning.ReportTargetVideo &&
				binding.TargetKind != learning.ReportTargetResource && binding.TargetKind != learning.ReportTargetLabMaterial {
				t.Fatalf("stale context rebound to %s", binding.VisibleCourseRevisionID)
			}
			report, err := reportRepository.CreateReport(ctx, binding,
				learning.ReportContent{Reason: learning.ReasonInaccurate}, clock)
			if err != nil {
				t.Fatalf("creating report from the stale page: %v", err)
			}
			var stored string
			if err := f.pool.QueryRow(ctx,
				`SELECT target_revision_ref::text FROM content_reports WHERE id = $1::uuid`, report.ID).Scan(&stored); err != nil {
				t.Fatalf("reading stored report: %v", err)
			}
			if testCase.notWant != "" && stored == testCase.notWant {
				t.Fatalf("%s recorded the instance current at submission (%s), not the one rendered (%s)",
					testCase.name, testCase.notWant, testCase.want)
			}
			if stored != testCase.want {
				t.Fatalf("%s stored %s, want the rendered instance %s", testCase.name, stored, testCase.want)
			}
		})
	}

	// 7–8. A fresh authoritative read now issues a context bound to B.
	freshCourse := courseContextOf(t, f)
	freshBinding, err := verifier.Verify(freshCourse, f.studentID, session)
	if err != nil {
		t.Fatalf("verifying fresh context: %v", err)
	}
	if freshBinding.VisibleCourseRevisionID != revisionB {
		t.Fatalf("fresh context bound %s, want revision B %s", freshBinding.VisibleCourseRevisionID, revisionB)
	}

	// 9–12. D-066 keeps the Student's second COURSE report closed until the first resolves; once it
	// does, the new context stores B and the original stays bound to A.
	if _, err := reportRepository.CreateReport(ctx, freshBinding,
		learning.ReportContent{Reason: learning.ReasonInaccurate}, clock); !errors.Is(err, learning.ErrReportDuplicate) {
		t.Fatalf("expected a duplicate refusal while the first COURSE report is open, got %v", err)
	}
	if _, err := f.pool.Exec(ctx, `
		UPDATE content_reports
		SET resolved_at = now(), resolved_by_account_id = $1::uuid,
		    resolution_action = 'DISMISSED', resolution_reason = 'fixture setup'
		WHERE target_kind = 'COURSE'
	`, f.studentID); err != nil {
		t.Fatalf("resolving the first report: %v", err)
	}
	second, err := reportRepository.CreateReport(ctx, freshBinding,
		learning.ReportContent{Reason: learning.ReasonInaccurate}, clock)
	if err != nil {
		t.Fatalf("reporting B after resolution: %v", err)
	}
	var storedB, storedA string
	if err := f.pool.QueryRow(ctx,
		`SELECT target_revision_ref::text FROM content_reports WHERE id = $1::uuid`, second.ID).Scan(&storedB); err != nil {
		t.Fatalf("reading second report: %v", err)
	}
	if storedB != revisionB {
		t.Fatalf("the second report stored %s, want B %s", storedB, revisionB)
	}
	if err := f.pool.QueryRow(ctx,
		`SELECT target_revision_ref::text FROM content_reports WHERE target_kind = 'COURSE' AND resolved_at IS NOT NULL`).Scan(&storedA); err != nil {
		t.Fatalf("reading original report: %v", err)
	}
	if storedA != revisionA {
		t.Fatalf("the original report changed to %s; it must stay bound to A %s", storedA, revisionA)
	}
}
