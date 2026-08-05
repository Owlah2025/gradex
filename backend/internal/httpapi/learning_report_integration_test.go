//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Owlah2025/gradex/backend/internal/learning"
)

// T063 route evidence against real PostgreSQL through the production router.
//
// Every request below travels the whole chain: route match, authentication, the Student capability
// gate, the session, strict decoding, encrypted context verification, the current Entitlement
// decision, and the T062 domain insert. Nothing here calls the domain directly, because a report
// that only works when the handler is bypassed is not a working route.

const reportRoute = "/api/v1/learn/reports"

func reportBody(token, reason, explanation string) string {
	fields := map[string]string{"report_context": token, "reason": reason}
	if explanation != "" {
		fields["explanation"] = explanation
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func submitReport(f learningIntegrationFixture, token, reason string) *httptest.ResponseRecorder {
	return f.request(http.MethodPost, reportRoute, reportBody(token, reason, ""))
}

type storedReport struct {
	id          string
	reporter    string
	kind        string
	target      string
	revisionRef string
	reason      string
	explanation *string
	createdAt   time.Time
	resolved    bool
}

func reportRowsFor(t *testing.T, f learningIntegrationFixture, kind string) []storedReport {
	t.Helper()
	rows, err := f.pool.Query(context.Background(), `
		SELECT id::text, reporter_account_id::text, target_kind::text, target_id::text,
		       target_revision_ref::text, reason::text, explanation, created_at, resolved_at IS NOT NULL
		FROM content_reports
		WHERE ($1 = '' OR target_kind::text = $1)
		ORDER BY created_at, id
	`, kind)
	if err != nil {
		t.Fatalf("reading content reports: %v", err)
	}
	defer rows.Close()
	var reports []storedReport
	for rows.Next() {
		var report storedReport
		if err := rows.Scan(&report.id, &report.reporter, &report.kind, &report.target,
			&report.revisionRef, &report.reason, &report.explanation, &report.createdAt, &report.resolved); err != nil {
			t.Fatalf("scanning content report: %v", err)
		}
		reports = append(reports, report)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating content reports: %v", err)
	}
	return reports
}

func reportCount(t *testing.T, f learningIntegrationFixture) int {
	t.Helper()
	var count int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM content_reports`).Scan(&count); err != nil {
		t.Fatalf("counting content reports: %v", err)
	}
	return count
}

func decodeReportAcknowledgement(t *testing.T, response *httptest.ResponseRecorder) learningReportResponse {
	t.Helper()
	var body learningReportResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding report acknowledgement: %v", err)
	}
	return body
}

// TestReportRouteRecordsTheRenderedInstanceForEveryTargetKind is the T063 completion evidence.
//
// For all five kinds: the Student loads A through the real protected read, receives context A, B
// becomes current, and the submission through the real route succeeds and stores A. A fresh read
// then issues B, and once D-066's open report is resolved, B is reportable and stores B while the
// original stays bound to A.
func TestReportRouteRecordsTheRenderedInstanceForEveryTargetKind(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	ctx := context.Background()
	resourceA := attachLessonMaterial(t, f, "RESOURCE", "resource-a")
	labA := attachLessonMaterial(t, f, "LAB_MATERIAL", "lab-a")
	revisionA := liveRevisionOf(t, f)

	// 1–2. The Student's open page.
	staleCourse := courseContextOf(t, f)
	staleLesson := lessonContextsOf(t, f)

	// 3. B becomes current underneath it.
	f.replaceLiveRevision(t)
	revisionB := liveRevisionOf(t, f)
	if revisionB == revisionA {
		t.Fatal("revision B did not become live")
	}

	// 4–6. Each stale context is submitted to the real route while access remains active.
	stale := []struct {
		kind  string
		token string
		want  string
	}{
		{"COURSE", staleCourse, revisionA},
		{"LESSON", staleLesson.Lesson, revisionA},
		{"VIDEO", staleLesson.Video, f.versionID},
		{"RESOURCE", staleLesson.Resource, resourceA},
		{"LAB_MATERIAL", staleLesson.LabMaterial, labA},
	}
	for _, testCase := range stale {
		t.Run(testCase.kind, func(t *testing.T) {
			response := submitReport(f, testCase.token, "inaccurate")
			if response.Code != http.StatusCreated {
				t.Fatalf("submission = %d %s", response.Code, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("acknowledgement Cache-Control = %q", response.Header().Get("Cache-Control"))
			}
			if response.Header().Get("Location") != "" {
				t.Fatal("acknowledgement carried a Location")
			}
			acknowledgement := decodeReportAcknowledgement(t, response)

			rows := reportRowsFor(t, f, testCase.kind)
			if len(rows) != 1 {
				t.Fatalf("%s produced %d report rows, want exactly 1", testCase.kind, len(rows))
			}
			stored := rows[0]
			// 6. The stored instance is the one the Student saw, not the one live at submission.
			if stored.revisionRef != testCase.want {
				t.Fatalf("%s stored %s, want the rendered instance %s", testCase.kind, stored.revisionRef, testCase.want)
			}
			if testCase.want == revisionA && stored.revisionRef == revisionB {
				t.Fatalf("%s rebound to the instance current at submission", testCase.kind)
			}
			if stored.id != acknowledgement.ReportID {
				t.Fatalf("acknowledgement named %s, stored %s", acknowledgement.ReportID, stored.id)
			}
			if stored.reporter != f.studentID {
				t.Fatalf("%s attributed to %s, want the authenticated reporter", testCase.kind, stored.reporter)
			}
			if stored.reason != "inaccurate" || stored.explanation != nil {
				t.Fatalf("%s stored reason=%s explanation=%v", testCase.kind, stored.reason, stored.explanation)
			}
			if stored.resolved {
				t.Fatalf("%s was created already resolved", testCase.kind)
			}
			// The injected clock, not a wall-clock read.
			if !stored.createdAt.UTC().Equal(f.clock.Now().UTC()) {
				t.Fatalf("%s created_at = %s, want the injected clock %s", testCase.kind, stored.createdAt.UTC(), f.clock.Now().UTC())
			}
			// The acknowledgement exposes no internal identity.
			for _, secret := range []string{
				testCase.want, revisionA, revisionB, resourceA, labA, f.versionID,
				f.courseID, f.lessonID, sessionIDFor(f.studentID), testCase.token,
			} {
				if strings.Contains(response.Body.String(), secret) {
					t.Fatalf("the acknowledgement exposed %q", secret)
				}
			}
		})
	}

	// 7. A fresh read now issues a context bound to B.
	freshCourse := courseContextOf(t, f)
	verifier := verifierFor(t)
	freshBinding, err := verifier.Verify(freshCourse, f.studentID, sessionIDFor(f.studentID))
	if err != nil {
		t.Fatalf("verifying fresh context: %v", err)
	}
	if freshBinding.VisibleCourseRevisionID != revisionB {
		t.Fatalf("fresh context bound %s, want B", freshBinding.VisibleCourseRevisionID)
	}

	// 8. D-066 holds the second COURSE report closed until the first resolves.
	if response := submitReport(f, freshCourse, "inaccurate"); response.Code != http.StatusConflict {
		t.Fatalf("duplicate while the first COURSE report is open = %d %s", response.Code, response.Body.String())
	}
	if _, err := f.pool.Exec(ctx, `UPDATE content_reports SET resolved_at = now() WHERE target_kind = 'COURSE'`); err != nil {
		t.Fatalf("resolving the first report as fixture setup: %v", err)
	}
	if response := submitReport(f, freshCourse, "inaccurate"); response.Code != http.StatusCreated {
		t.Fatalf("reporting B after resolution = %d %s", response.Code, response.Body.String())
	}

	// 9. Context B stores B; the original report stays bound to A.
	// The injected clock makes both rows share created_at, so they are identified by the fixture
	// resolution rather than by ordering.
	courseReports := reportRowsFor(t, f, "COURSE")
	if len(courseReports) != 2 {
		t.Fatalf("COURSE reports = %d, want 2", len(courseReports))
	}
	for _, report := range courseReports {
		if report.resolved && report.revisionRef != revisionA {
			t.Fatalf("the original COURSE report changed to %s; it must stay bound to A", report.revisionRef)
		}
		if !report.resolved && report.revisionRef != revisionB {
			t.Fatalf("the second COURSE report stored %s, want B", report.revisionRef)
		}
	}
}

// TestReportRouteNormalizesExplanationWithoutTruncating pins the content contract: whitespace is
// trimmed, Unicode survives, and no field limit is invented beyond the body bound.
func TestReportRouteNormalizesExplanationWithoutTruncating(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	long := strings.Repeat("تفصيل ", 300)
	explanation := "  " + long + "\n"

	response := f.request(http.MethodPost, reportRoute, reportBody(courseContextOf(t, f), "other", explanation))
	if response.Code != http.StatusCreated {
		t.Fatalf("submission = %d %s", response.Code, response.Body.String())
	}
	rows := reportRowsFor(t, f, "COURSE")
	if len(rows) != 1 || rows[0].explanation == nil {
		t.Fatalf("stored rows = %+v", rows)
	}
	if *rows[0].explanation != strings.TrimSpace(long) {
		t.Fatalf("explanation was altered: %d stored bytes, want %d", len(*rows[0].explanation), len(strings.TrimSpace(long)))
	}
}

// reportRefusalCase is one way a submission can fail after the request itself is well formed.
type reportRefusalCase struct {
	name string
	// token builds the submitted context; mutate changes authoritative state first.
	token  func(t *testing.T, f learningIntegrationFixture) string
	mutate func(t *testing.T, f learningIntegrationFixture)
}

// foreignSignerContext mints a cryptographically valid context whose relational content is not.
// Authenticity only proves the server minted the values; it never proves they form a real target.
func fixtureContext(t *testing.T, f learningIntegrationFixture, request learning.ReportContextRequest) string {
	t.Helper()
	request.ReporterAccountID = f.studentID
	request.SessionID = sessionIDFor(f.studentID)
	token, err := verifierFor(t).Mint(request)
	if err != nil {
		t.Fatalf("minting fixture context: %v", err)
	}
	return token
}

func reportRefusalCases() []reportRefusalCase {
	liveCourseContext := func(t *testing.T, f learningIntegrationFixture) string { return courseContextOf(t, f) }

	return []reportRefusalCase{
		// Context security.
		{name: "malformed context", token: func(*testing.T, learningIntegrationFixture) string { return "not-a-context" }},
		{name: "tampered ciphertext", token: func(t *testing.T, f learningIntegrationFixture) string {
			token := courseContextOf(t, f)
			return token[:len(token)-2] + "AA"
		}},
		{name: "unknown version", token: func(t *testing.T, f learningIntegrationFixture) string {
			return "grc9." + strings.SplitN(courseContextOf(t, f), ".", 2)[1]
		}},
		{name: "another student's context", token: func(t *testing.T, f learningIntegrationFixture) string {
			return fixtureContextFor(t, uuid.NewString(), sessionIDFor(f.studentID), learning.ReportContextRequest{
				TargetKind: learning.ReportTargetCourse, CourseID: f.courseID,
				StableTargetID: f.courseID, VisibleCourseRevisionID: liveRevisionOf(t, f),
			})
		}},
		{name: "another session's context", token: func(t *testing.T, f learningIntegrationFixture) string {
			return fixtureContextFor(t, f.studentID, "a-different-session", learning.ReportContextRequest{
				TargetKind: learning.ReportTargetCourse, CourseID: f.courseID,
				StableTargetID: f.courseID, VisibleCourseRevisionID: liveRevisionOf(t, f),
			})
		}},

		// Current authority. The context is perfect; the access is not.
		{name: "no enrollment", token: liveCourseContext, mutate: func(t *testing.T, f learningIntegrationFixture) {
			if _, err := f.pool.Exec(context.Background(), `DELETE FROM enrollments WHERE student_account_id = $1::uuid`, f.studentID); err != nil {
				t.Fatalf("removing enrollment: %v", err)
			}
		}},
		{name: "no entitlement", token: liveCourseContext, mutate: func(t *testing.T, f learningIntegrationFixture) {
			if _, err := f.pool.Exec(context.Background(), `DELETE FROM entitlements WHERE student_account_id = $1::uuid`, f.studentID); err != nil {
				t.Fatalf("removing entitlement: %v", err)
			}
		}},
		{name: "expired entitlement", token: liveCourseContext, mutate: func(t *testing.T, f learningIntegrationFixture) {
			if _, err := f.pool.Exec(context.Background(),
				`UPDATE entitlements SET original_access_ends_at = $1, access_ends_at = $1 WHERE student_account_id = $2::uuid`,
				f.clock.Now(), f.studentID); err != nil {
				t.Fatalf("expiring entitlement: %v", err)
			}
		}},
		{name: "revoked entitlement", token: liveCourseContext, mutate: func(t *testing.T, f learningIntegrationFixture) {
			if _, err := f.pool.Exec(context.Background(),
				`UPDATE entitlements SET state = 'REVOKED', revoked_at = $1 WHERE student_account_id = $2::uuid`,
				f.clock.Now(), f.studentID); err != nil {
				t.Fatalf("revoking entitlement: %v", err)
			}
		}},
		{name: "account suspended", token: liveCourseContext, mutate: func(t *testing.T, f learningIntegrationFixture) {
			if _, err := f.pool.Exec(context.Background(),
				`UPDATE accounts SET status = 'SUSPENDED' WHERE id = $1::uuid`, f.studentID); err != nil {
				t.Fatalf("suspending account: %v", err)
			}
		}},
		{name: "emergency course suspension", token: liveCourseContext, mutate: func(t *testing.T, f learningIntegrationFixture) {
			if _, err := f.pool.Exec(context.Background(),
				`UPDATE courses SET access_suspended_at = $1, access_suspension_reason = 'report-test' WHERE id = $2::uuid`,
				f.clock.Now(), f.courseID); err != nil {
				t.Fatalf("suspending course access: %v", err)
			}
		}},
		{name: "section scoped entitlement", token: liveCourseContext, mutate: func(t *testing.T, f learningIntegrationFixture) {
			otherSection := uuid.NewString()
			if _, err := f.pool.Exec(context.Background(),
				`INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)`, otherSection, f.courseID); err != nil {
				t.Fatalf("seeding out-of-scope section: %v", err)
			}
			if _, err := f.pool.Exec(context.Background(),
				`UPDATE entitlements SET scope_kind = 'SECTION', scope_id = $1::uuid WHERE student_account_id = $2::uuid`,
				otherSection, f.studentID); err != nil {
				t.Fatalf("narrowing entitlement: %v", err)
			}
		}},

		// Content mismatch: valid tokens whose relational content cannot be true.
		{name: "foreign course", token: func(t *testing.T, f learningIntegrationFixture) string {
			foreign := uuid.NewString()
			return fixtureContext(t, f, learning.ReportContextRequest{
				TargetKind: learning.ReportTargetCourse, CourseID: foreign,
				StableTargetID: foreign, VisibleCourseRevisionID: liveRevisionOf(t, f),
			})
		}},
		{name: "foreign revision", token: func(t *testing.T, f learningIntegrationFixture) string {
			return fixtureContext(t, f, learning.ReportContextRequest{
				TargetKind: learning.ReportTargetCourse, CourseID: f.courseID,
				StableTargetID: f.courseID, VisibleCourseRevisionID: uuid.NewString(),
			})
		}},
		{name: "foreign lesson", token: func(t *testing.T, f learningIntegrationFixture) string {
			return fixtureContext(t, f, learning.ReportContextRequest{
				TargetKind: learning.ReportTargetLesson, CourseID: f.courseID,
				StableTargetID: uuid.NewString(), VisibleCourseRevisionID: liveRevisionOf(t, f),
			})
		}},
		{name: "course context represented as lesson", token: func(t *testing.T, f learningIntegrationFixture) string {
			return fixtureContext(t, f, learning.ReportContextRequest{
				TargetKind: learning.ReportTargetLesson, CourseID: f.courseID,
				StableTargetID: f.courseID, VisibleCourseRevisionID: liveRevisionOf(t, f),
			})
		}},
		{name: "foreign asset version", token: func(t *testing.T, f learningIntegrationFixture) string {
			return fixtureContext(t, f, learning.ReportContextRequest{
				TargetKind: learning.ReportTargetVideo, CourseID: f.courseID,
				StableTargetID: f.lessonID, VisibleCourseRevisionID: liveRevisionOf(t, f),
				VisibleAssetVersionID: uuid.NewString(),
			})
		}},
		{name: "resource represented as lab material", token: func(t *testing.T, f learningIntegrationFixture) string {
			version := attachLessonMaterial(t, f, "RESOURCE", "resource-mismatch")
			return fixtureContext(t, f, learning.ReportContextRequest{
				TargetKind: learning.ReportTargetLabMaterial, CourseID: f.courseID,
				StableTargetID: f.lessonID, VisibleCourseRevisionID: liveRevisionOf(t, f),
				VisibleAssetVersionID: version,
			})
		}},
		{name: "target removed after issuance", token: liveCourseContext, mutate: func(t *testing.T, f learningIntegrationFixture) {
			if _, err := f.pool.Exec(context.Background(),
				`UPDATE course_revisions SET state = 'DRAFT' WHERE course_id = $1::uuid`, f.courseID); err != nil {
				t.Fatalf("withdrawing the approved revision: %v", err)
			}
		}},
	}
}

// fixtureContextFor mints a context for an arbitrary reporter and session.
func fixtureContextFor(t *testing.T, reporter, session string, request learning.ReportContextRequest) string {
	t.Helper()
	request.ReporterAccountID = reporter
	request.SessionID = session
	token, err := verifierFor(t).Mint(request)
	if err != nil {
		t.Fatalf("minting fixture context: %v", err)
	}
	return token
}

// TestReportRefusalsAreOneByteIdenticalAnswer runs the whole refusal matrix against real
// PostgreSQL. Seventeen different causes — cryptographic, authority, and relational — produce one
// response, and none of them writes a row.
func TestReportRefusalsAreOneByteIdenticalAnswer(t *testing.T) {
	var baseline *learningWireResponse
	for _, testCase := range reportRefusalCases() {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			f := newLearningIntegrationFixture(t)
			token := testCase.token(t, f)
			if testCase.mutate != nil {
				testCase.mutate(t, f)
			}
			before := f.authoritySnapshot(t)

			response := submitReport(f, token, "inaccurate")
			denied := assertProtectedUnavailable(t, response)

			if got := reportCount(t, f); got != 0 {
				t.Fatalf("a refused submission wrote %d report rows", got)
			}
			if after := f.authoritySnapshot(t); before != after {
				t.Fatalf("a refused submission mutated authority:\nbefore=%+v\nafter=%+v", before, after)
			}
			// Raw bytes, uncompared and unnormalized, across every cause.
			if baseline == nil {
				baseline = &denied
				return
			}
			assertSameLearningWire(t, *baseline, denied)
		})
	}
}

// TestReportRouteRefusesRetainedExpiredReporting is FR-033 across the retained-expired boundary: the
// Course still reads, its Progress and Enrollment are retained, and reporting is still refused.
func TestReportRouteRefusesRetainedExpiredReporting(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	token := courseContextOf(t, f)
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE entitlements SET original_access_ends_at = $1, access_ends_at = $1 WHERE student_account_id = $2::uuid`,
		f.clock.Now().Add(-time.Hour), f.studentID); err != nil {
		t.Fatalf("expiring entitlement: %v", err)
	}

	home := f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID, "")
	if home.Code != http.StatusOK || !strings.Contains(home.Body.String(), `"learning_status":"expired"`) {
		t.Fatalf("retained-expired read = %d %s", home.Code, home.Body.String())
	}
	assertProtectedUnavailable(t, submitReport(f, token, "broken_unavailable"))
	if got := reportCount(t, f); got != 0 {
		t.Fatalf("a retained-expired submission wrote %d rows", got)
	}
}

// TestReportRouteEnforcesAuthorityAtInsertion proves the consistency boundary: access that ends
// after the context is issued still refuses, and no row is written.
func TestReportRouteEnforcesAuthorityAtInsertion(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	token := courseContextOf(t, f)

	// The same context succeeds while access holds.
	if response := submitReport(f, token, "inaccurate"); response.Code != http.StatusCreated {
		t.Fatalf("submission while entitled = %d %s", response.Code, response.Body.String())
	}
	if _, err := f.pool.Exec(context.Background(), `DELETE FROM content_reports`); err != nil {
		t.Fatalf("clearing reports: %v", err)
	}

	// Access ends. The context is unchanged and still cryptographically valid.
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE entitlements SET state = 'REVOKED', revoked_at = $1 WHERE student_account_id = $2::uuid`,
		f.clock.Now(), f.studentID); err != nil {
		t.Fatalf("revoking entitlement: %v", err)
	}
	assertProtectedUnavailable(t, submitReport(f, token, "inaccurate"))
	if got := reportCount(t, f); got != 0 {
		t.Fatalf("a submission after revocation wrote %d rows", got)
	}
}

// TestReportDuplicateGranularityFollowsD066 covers the whole duplicate contract at the route:
// same Student, same kind, same stable target while unresolved is refused; a different kind is
// not; and resolution reopens the target to a new report.
func TestReportDuplicateGranularityFollowsD066(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	contexts := lessonContextsOf(t, f)

	if response := submitReport(f, contexts.Lesson, "inaccurate"); response.Code != http.StatusCreated {
		t.Fatalf("first LESSON report = %d %s", response.Code, response.Body.String())
	}

	duplicate := submitReport(f, contexts.Lesson, "broken_unavailable")
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate = %d %s", duplicate.Code, duplicate.Body.String())
	}
	if duplicate.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("duplicate Cache-Control = %q", duplicate.Header().Get("Cache-Control"))
	}
	// The conflict must not describe the report it collided with.
	existing := reportRowsFor(t, f, "LESSON")
	for _, secret := range []string{existing[0].id, existing[0].revisionRef, f.lessonID, f.courseID, "resolved", "moderat"} {
		if strings.Contains(duplicate.Body.String(), secret) {
			t.Fatalf("the duplicate response exposed %q", secret)
		}
	}

	// A different target kind on the same stable Lesson is a distinct report.
	if response := submitReport(f, contexts.Video, "inaccurate"); response.Code != http.StatusCreated {
		t.Fatalf("VIDEO report on the same Lesson = %d %s", response.Code, response.Body.String())
	}

	// Another Student is unaffected by this Student's open report. The route authenticates one
	// Student per fixture, so the second reporter is exercised at the domain boundary it shares.
	otherStudent := seedSecondStudent(t, f)
	otherBinding, err := verifierFor(t).Verify(
		fixtureContextFor(t, otherStudent, "test-session-"+otherStudent, learning.ReportContextRequest{
			TargetKind: learning.ReportTargetLesson, CourseID: f.courseID,
			StableTargetID: f.lessonID, VisibleCourseRevisionID: liveRevisionOf(t, f),
		}), otherStudent, "test-session-"+otherStudent)
	if err != nil {
		t.Fatalf("verifying second reporter context: %v", err)
	}
	if _, err := f.repository.CreateReport(context.Background(), otherBinding,
		learning.ReportContent{Reason: learning.ReasonInaccurate}, f.clock.Now); err != nil {
		t.Fatalf("a second Student must be able to report the same target: %v", err)
	}

	// Once the first report is resolved, the Student may report the target again.
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE content_reports SET resolved_at = now() WHERE reporter_account_id = $1::uuid AND target_kind = 'LESSON'`,
		f.studentID); err != nil {
		t.Fatalf("resolving as fixture setup: %v", err)
	}
	if response := submitReport(f, contexts.Lesson, "inaccurate"); response.Code != http.StatusCreated {
		t.Fatalf("report after resolution = %d %s", response.Code, response.Body.String())
	}
}

// seedSecondStudent adds an independently enrolled and entitled Student.
func seedSecondStudent(t *testing.T, f learningIntegrationFixture) string {
	t.Helper()
	ctx := context.Background()
	student := uuid.NewString()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO accounts (id, normalized_email, email, role, status, display_name) VALUES ($1::uuid, $2, $2, 'STUDENT', 'ACTIVE', 'Second student')`,
			[]any{student, "s5-student-2@example.test"}},
		{`INSERT INTO password_credentials (account_id, password_hash, state) VALUES ($1::uuid, '$argon2id$fixture', 'ACTIVE')`, []any{student}},
		{`INSERT INTO enrollments (student_account_id, course_id) VALUES ($1::uuid, $2::uuid)`, []any{student, f.courseID}},
		{`INSERT INTO entitlements (id, student_account_id, scope_kind, scope_id, course_id, grant_source, original_access_ends_at, access_ends_at, retirement_eligibility_at, state)
		  VALUES ($1::uuid, $2::uuid, 'COURSE', $3::uuid, $3::uuid, 'MANUAL_INVITATION', $4, $4, $5, 'ACTIVE')`,
			[]any{uuid.NewString(), student, f.courseID, f.clock.Now().Add(time.Hour), f.clock.Now().Add(-time.Hour)}},
	}
	for _, statement := range statements {
		if _, err := f.pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seeding second student: %v", err)
		}
	}
	return student
}

// TestConcurrentDuplicateSubmissionsCreateOneReport proves the database is the concurrency
// authority (R-11): no application pre-check, one winner, and a healthy pool afterwards.
func TestConcurrentDuplicateSubmissionsCreateOneReport(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	token := courseContextOf(t, f)

	const submissions = 8
	var wait sync.WaitGroup
	codes := make([]int, submissions)
	for i := 0; i < submissions; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			codes[index] = submitReport(f, token, "inaccurate").Code
		}(i)
	}
	wait.Wait()

	created, conflicts := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("concurrent submission produced an unexpected status %d", code)
		}
	}
	if created != 1 || conflicts != submissions-1 {
		t.Fatalf("concurrent submissions = %d created, %d conflicts, want exactly one winner", created, conflicts)
	}
	if got := reportCount(t, f); got != 1 {
		t.Fatalf("concurrent submissions produced %d rows, want 1", got)
	}
	// The pool survives the losing transactions.
	if response := f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID, ""); response.Code != http.StatusOK {
		t.Fatalf("read after concurrent submissions = %d", response.Code)
	}
}

// TestSuccessfulReportMutatesNothingElse is FR-031 and BR-146 at the row level: a report changes the
// Course, its revision, the Lesson identity, the media, Progress, Enrollment, and Entitlement not at
// all. It adds exactly one row, to one table.
func TestSuccessfulReportMutatesNothingElse(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	attachLessonMaterial(t, f, "RESOURCE", "resource-side-effect")
	token := lessonContextsOf(t, f).Resource

	before := f.authoritySnapshot(t)
	beforeContent := contentSnapshot(t, f)

	if response := submitReport(f, token, "inappropriate"); response.Code != http.StatusCreated {
		t.Fatalf("submission = %d %s", response.Code, response.Body.String())
	}

	if after := f.authoritySnapshot(t); before != after {
		t.Fatalf("a report mutated authority:\nbefore=%+v\nafter=%+v", before, after)
	}
	if after := contentSnapshot(t, f); !reflect.DeepEqual(beforeContent, after) {
		t.Fatalf("a report mutated content:\nbefore=%+v\nafter=%+v", beforeContent, after)
	}
	if got := reportCount(t, f); got != 1 {
		t.Fatalf("report rows = %d, want exactly 1", got)
	}
	// The read the Student came from is unchanged: nothing is hidden, retired, or marked.
	lesson := f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID+"/lessons/"+f.lessonID, "")
	if lesson.Code != http.StatusOK {
		t.Fatalf("post-report lesson read = %d", lesson.Code)
	}
	for _, marker := range []string{"report_id", "reported", "flagged", "under_review"} {
		if strings.Contains(lesson.Body.String(), marker) {
			t.Fatalf("the read model exposed %q after a report", marker)
		}
	}
}

// contentSnapshot captures everything a report must leave alone.
func contentSnapshot(t *testing.T, f learningIntegrationFixture) map[string]string {
	t.Helper()
	snapshot := make(map[string]string)
	queries := map[string]string{
		"courses":         `SELECT COALESCE(jsonb_agg(to_jsonb(c) ORDER BY c.id), '[]'::jsonb)::text FROM courses c WHERE c.id = $1::uuid`,
		"revisions":       `SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY r.id), '[]'::jsonb)::text FROM course_revisions r WHERE r.course_id = $1::uuid`,
		"lessons":         `SELECT COALESCE(jsonb_agg(to_jsonb(l) ORDER BY l.id), '[]'::jsonb)::text FROM course_lessons l WHERE l.course_id = $1::uuid`,
		"lesson_identity": `SELECT COALESCE(jsonb_agg(to_jsonb(i) ORDER BY i.id), '[]'::jsonb)::text FROM course_lesson_identities i WHERE i.course_id = $1::uuid`,
		"files":           `SELECT COALESCE(jsonb_agg(to_jsonb(f) ORDER BY f.lesson_id, f.kind), '[]'::jsonb)::text FROM lesson_files f JOIN course_lessons cl ON cl.id = f.lesson_id WHERE cl.course_id = $1::uuid`,
		"versions":        `SELECT COALESCE(jsonb_agg(to_jsonb(v) ORDER BY v.id), '[]'::jsonb)::text FROM media_asset_versions v JOIN media_assets a ON a.id = v.logical_asset_id WHERE a.course_id = $1::uuid`,
	}
	for name, query := range queries {
		var value string
		if err := f.pool.QueryRow(context.Background(), query, f.courseID).Scan(&value); err != nil {
			t.Fatalf("snapshotting %s: %v", name, err)
		}
		snapshot[name] = value
	}
	return snapshot
}
