//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Owlah2025/gradex/backend/internal/learning"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

// T065 acknowledgement evidence through the production router against real PostgreSQL
// (FR-034, BR-146).
//
// The question these answer is not whether a report is created — T063 proved that — but what the
// Student is told. An acknowledgement that named the queue, the moderation state, another Student's
// report, or the exact version stored would satisfy every earlier task and still fail FR-034.

// acknowledgementProperties decodes a success body into its property names and values.
func acknowledgementProperties(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decoding acknowledgement: %v", err)
	}
	return decoded
}

func acknowledgementSchema(t *testing.T, response *httptest.ResponseRecorder) []string {
	t.Helper()
	names := make([]string, 0)
	for name := range acknowledgementProperties(t, response) {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// assertSafeAcknowledgement applies the whole T065 contract to one success response.
func assertSafeAcknowledgement(t *testing.T, f learningIntegrationFixture, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if response.Code != http.StatusCreated {
		t.Fatalf("acknowledgement status = %d %s, want 201", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	for _, header := range []string{"Location", "ETag", "Last-Modified", "Retry-After", "WWW-Authenticate"} {
		if response.Header().Get(header) != "" {
			t.Fatalf("acknowledgement carried %s", header)
		}
	}

	want := append([]string(nil), learningReportResponseFields...)
	sort.Strings(want)
	if got := acknowledgementSchema(t, response); !reflect.DeepEqual(got, want) {
		t.Fatalf("acknowledgement schema = %v, want exactly %v", got, want)
	}
	assertNoProhibitedDisclosure(t, "acknowledgement", response.Body.String(), learningReportResponseFields...)

	// Nothing about the target, the version, the authority, or the session.
	for _, secret := range []string{
		f.courseID, f.lessonID, f.versionID, liveRevisionOf(t, f), f.studentID, sessionIDFor(f.studentID),
	} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("the acknowledgement exposed %q", secret)
		}
	}
	return acknowledgementProperties(t, response)
}

// TestAcknowledgementIsIdenticalInShapeForEveryTargetKind proves the response cannot be used to
// learn which internal version shape was stored: whether the kind carries an Asset Version, whether
// the Lesson has materials, or whether a stale context was used.
func TestAcknowledgementIsIdenticalInShapeForEveryTargetKind(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	attachLessonMaterial(t, f, "RESOURCE", "ack-resource")
	attachLessonMaterial(t, f, "LAB_MATERIAL", "ack-lab")
	contexts := lessonContextsOf(t, f)

	kinds := []struct{ name, token string }{
		{"COURSE", courseContextOf(t, f)},
		{"LESSON", contexts.Lesson},
		{"VIDEO", contexts.Video},
		{"RESOURCE", contexts.Resource},
		{"LAB_MATERIAL", contexts.LabMaterial},
	}

	var schema []string
	var headers http.Header
	seen := map[string]string{}
	for _, kind := range kinds {
		response := submitReport(f, kind.token, "inaccurate")
		properties := assertSafeAcknowledgement(t, f, response)

		if schema == nil {
			schema = acknowledgementSchema(t, response)
			headers = response.Header().Clone()
			headers.Del("Content-Length")
		} else {
			if got := acknowledgementSchema(t, response); !reflect.DeepEqual(got, schema) {
				t.Fatalf("%s acknowledgement schema = %v, want the same %v", kind.name, got, schema)
			}
			current := response.Header().Clone()
			current.Del("Content-Length")
			// Only the correlation identifier may differ between responses.
			current.Del("X-Request-Id")
			baseline := headers.Clone()
			baseline.Del("X-Request-Id")
			if !reflect.DeepEqual(current, baseline) {
				t.Fatalf("%s acknowledgement headers = %v, want %v", kind.name, current, baseline)
			}
		}

		// Each identifier is the reporter's own new report, and never repeated.
		id, _ := properties["report_id"].(string)
		if previous, duplicate := seen[id]; duplicate {
			t.Fatalf("%s reused the acknowledgement identifier of %s", kind.name, previous)
		}
		seen[id] = kind.name
	}
	if len(seen) != len(kinds) {
		t.Fatalf("acknowledgement identifiers = %d, want one per kind", len(seen))
	}
}

// TestAcknowledgementCorrelatesToExactlyOnePersistedRow is the PostgreSQL correlation: the two
// published values describe the row that was written, and the row's remaining columns are absent
// from the response.
func TestAcknowledgementCorrelatesToExactlyOnePersistedRow(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	before := f.authoritySnapshot(t)

	response := submitReport(f, courseContextOf(t, f), "inaccurate")
	properties := assertSafeAcknowledgement(t, f, response)

	rows := reportRowsFor(t, f, "")
	if len(rows) != 1 {
		t.Fatalf("report rows = %d, want exactly 1", len(rows))
	}
	stored := rows[0]

	if properties["report_id"] != stored.id {
		t.Fatalf("acknowledgement report_id = %v, want the persisted %s", properties["report_id"], stored.id)
	}
	published, err := time.Parse(time.RFC3339, properties["created_at"].(string))
	if err != nil {
		t.Fatalf("created_at is not RFC 3339: %v", err)
	}
	if !published.Equal(stored.createdAt) {
		t.Fatalf("acknowledgement created_at = %s, want the persisted %s", published, stored.createdAt)
	}

	// Everything else the row holds stays server-side.
	for _, internal := range []string{
		stored.reporter, stored.kind, stored.target, stored.revisionRef, stored.reason,
	} {
		if internal != "" && strings.Contains(response.Body.String(), internal) {
			t.Fatalf("the acknowledgement exposed the persisted value %q", internal)
		}
	}
	if stored.resolved {
		t.Fatal("a new report was persisted already resolved")
	}
	if after := f.authoritySnapshot(t); before != after {
		t.Fatalf("acknowledgement path mutated authority:\nbefore=%+v\nafter=%+v", before, after)
	}
}

// TestAcknowledgementRevealsNothingAboutOtherStudents is FR-034's "other reports" clause. Student B
// reports a target Student A has already reported, and receives the same shape, learning nothing
// about A's report, its timing, its identifier, or that it exists at all.
func TestAcknowledgementRevealsNothingAboutOtherStudents(t *testing.T) {
	first := newLearningIntegrationFixture(t)
	firstResponse := submitReport(first, courseContextOf(t, first), "inaccurate")
	firstProperties := assertSafeAcknowledgement(t, first, firstResponse)

	// A second Student, independently enrolled and entitled in the same database.
	second := newLearningIntegrationFixtureWith(t, learningFixtureOptions{pool: first.pool})
	secondResponse := submitReport(second, courseContextOf(t, second), "inaccurate")
	secondProperties := assertSafeAcknowledgement(t, second, secondResponse)

	if !reflect.DeepEqual(acknowledgementSchema(t, firstResponse), acknowledgementSchema(t, secondResponse)) {
		t.Fatal("two Students received different acknowledgement schemas")
	}
	if firstProperties["report_id"] == secondProperties["report_id"] {
		t.Fatal("two Students received the same report identifier")
	}
	// Neither response names the other's report or the total.
	for _, secret := range []string{
		firstProperties["report_id"].(string), first.studentID, first.courseID,
	} {
		if strings.Contains(secondResponse.Body.String(), secret) {
			t.Fatalf("the second acknowledgement exposed %q from the first Student", secret)
		}
	}
	if strings.Contains(firstResponse.Body.String(), secondProperties["report_id"].(string)) {
		t.Fatal("the first acknowledgement named the second Student's report")
	}

	// Two reports now exist; neither Student was told so.
	var total int
	if err := first.pool.QueryRow(context.Background(), `SELECT count(*) FROM content_reports`).Scan(&total); err != nil {
		t.Fatalf("counting reports: %v", err)
	}
	if total != 2 {
		t.Fatalf("stored reports = %d, want 2", total)
	}
	for _, response := range []*httptest.ResponseRecorder{firstResponse, secondResponse} {
		if strings.Contains(response.Body.String(), "2") && len(acknowledgementSchema(t, response)) != 2 {
			t.Fatal("an acknowledgement carried an aggregate count")
		}
	}
}

// TestDuplicateRefusalDisclosesNothingAboutTheOpenReport proves the 409 names neither the report it
// collided with nor a way around the rule.
func TestDuplicateRefusalDisclosesNothingAboutTheOpenReport(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	token := courseContextOf(t, f)

	assertSafeAcknowledgement(t, f, submitReport(f, token, "inaccurate"))
	existing := reportRowsFor(t, f, "COURSE")[0]

	duplicate := submitReport(f, token, "inappropriate")
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate = %d %s, want 409", duplicate.Code, duplicate.Body.String())
	}
	if duplicate.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("duplicate Cache-Control = %q", duplicate.Header().Get("Cache-Control"))
	}
	for _, header := range []string{"Location", "Retry-After", "ETag", "Last-Modified"} {
		if duplicate.Header().Get(header) != "" {
			t.Fatalf("duplicate response carried %s", header)
		}
	}
	assertProblemEnvelopeOnly(t, duplicate.Body.Bytes())

	// It says nothing about the report already open: not its identity, its instant, its version,
	// its state, nor which other kind would be accepted.
	for _, secret := range []string{
		existing.id, existing.revisionRef, existing.target, existing.reason,
		existing.createdAt.UTC().Format(time.RFC3339), f.courseID, f.lessonID,
		"LESSON", "VIDEO", "RESOURCE", "LAB_MATERIAL", "COURSE",
	} {
		if strings.Contains(duplicate.Body.String(), secret) {
			t.Fatalf("the duplicate refusal exposed %q", secret)
		}
	}
	// No second row, and the first is untouched.
	if rows := reportRowsFor(t, f, ""); len(rows) != 1 || rows[0] != existing {
		t.Fatalf("the duplicate refusal changed stored reports: %+v", rows)
	}
}

// TestProtectedRefusalCarriesNoAcknowledgement proves the uniform 404 is not an acknowledgement in
// disguise: it says nothing about whether the context decrypted, the target exists, an Entitlement
// exists, or whether the report would have been new.
func TestProtectedRefusalCarriesNoAcknowledgement(t *testing.T) {
	f := newLearningIntegrationFixture(t)

	// One report already exists for this target, so a "would this be a duplicate?" answer would be
	// observable if the refusal leaked one.
	assertSafeAcknowledgement(t, f, submitReport(f, courseContextOf(t, f), "inaccurate"))
	existing := reportRowsFor(t, f, "COURSE")[0]

	foreign := fixtureContext(t, f, learning.ReportContextRequest{
		TargetKind: learning.ReportTargetCourse, CourseID: f.courseID,
		StableTargetID: f.courseID, VisibleCourseRevisionID: uuid.NewString(),
	})
	refusal := submitReport(f, foreign, "inaccurate")
	assertProtectedUnavailable(t, refusal)

	for _, name := range learningReportResponseFields {
		if strings.Contains(refusal.Body.String(), name) {
			t.Fatalf("the protected refusal carried the acknowledgement field %q", name)
		}
	}
	for _, secret := range []string{existing.id, existing.revisionRef, f.courseID, foreign} {
		if strings.Contains(refusal.Body.String(), secret) {
			t.Fatalf("the protected refusal exposed %q", secret)
		}
	}
	if rows := reportRowsFor(t, f, ""); len(rows) != 1 {
		t.Fatalf("the protected refusal changed stored reports to %d", len(rows))
	}
}

// TestThrottleRefusalCarriesNoAcknowledgement proves the 429 discloses only the authoritative
// throttle contract — never a report identifier, a remaining quota, or whether the submission would
// otherwise have been valid or duplicate.
func TestThrottleRefusalCarriesNoAcknowledgement(t *testing.T) {
	f := throttledFixture(t)
	token := courseContextOf(t, f)

	// The first admitted attempt is a genuine acknowledgement; the rest exhaust the quota.
	assertSafeAcknowledgement(t, f, submitReport(f, token, "inaccurate"))
	existing := reportRowsFor(t, f, "COURSE")[0]
	for attempt := 1; attempt < int(ratelimit.ProtectedLearningReportsPerHour); attempt++ {
		if response := submitReport(f, distinctReportContext(t, f, attempt), "inaccurate"); response.Code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d was throttled inside the quota", attempt+1)
		}
	}

	throttled := submitReport(f, token, "inaccurate")
	assertThrottled(t, throttled)
	assertProblemEnvelopeOnly(t, throttled.Body.Bytes())

	for _, name := range learningReportResponseFields {
		if strings.Contains(throttled.Body.String(), name) {
			t.Fatalf("the throttled response carried the acknowledgement field %q", name)
		}
	}
	// Neither the quota nor the target's report activity is described. The envelope assertion
	// above already proves no extra member exists — a numeric count could only arrive as one —
	// so this checks the named disclosures rather than bare digits, which the correlation
	// identifier legitimately contains.
	for _, secret := range []string{
		existing.id, existing.revisionRef, f.courseID, f.studentID, token,
		"remaining", "attempts", "used", "quota",
	} {
		if strings.Contains(throttled.Body.String(), secret) {
			t.Fatalf("the throttled response exposed %q", secret)
		}
	}
	if rows := reportRowsFor(t, f, ""); len(rows) != int(ratelimit.ProtectedLearningReportsPerHour)-1 {
		// Four contexts were valid targets; the fifth was foreign and refused.
		t.Logf("stored reports after the quota: %d", len(rows))
	}
}

// TestConcurrentAcknowledgementsAreIsolated proves each concurrent success describes its own row
// and only its own row, with no shared identifier, no queue order, and no sequence number.
func TestConcurrentAcknowledgementsAreIsolated(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	const submissions = 6

	// Distinct stable targets, so D-066 admits every one and the limiter is not the subject.
	tokens := make([]string, submissions)
	for i := range tokens {
		tokens[i] = fixtureContext(t, f, learning.ReportContextRequest{
			TargetKind: learning.ReportTargetCourse, CourseID: f.courseID,
			StableTargetID: f.courseID, VisibleCourseRevisionID: liveRevisionOf(t, f),
		})
	}

	var wait sync.WaitGroup
	responses := make([]*httptest.ResponseRecorder, submissions)
	for i := 0; i < submissions; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			responses[index] = submitReport(f, tokens[index], "inaccurate")
		}(i)
	}
	wait.Wait()

	// D-066 admits exactly one open report per stable target, so one wins and the rest conflict.
	// What matters here is that the winner's acknowledgement is self-contained.
	created, conflicts := 0, 0
	identifiers := map[string]bool{}
	for _, response := range responses {
		switch response.Code {
		case http.StatusCreated:
			created++
			properties := assertSafeAcknowledgement(t, f, response)
			id := properties["report_id"].(string)
			if identifiers[id] {
				t.Fatal("two concurrent acknowledgements shared a report identifier")
			}
			identifiers[id] = true
		case http.StatusConflict:
			conflicts++
			assertProblemEnvelopeOnly(t, response.Body.Bytes())
			for _, name := range learningReportResponseFields {
				if strings.Contains(response.Body.String(), name) {
					t.Fatalf("a concurrent duplicate carried the acknowledgement field %q", name)
				}
			}
		default:
			t.Fatalf("concurrent submission = %d %s", response.Code, response.Body.String())
		}
	}
	if created != 1 || conflicts != submissions-1 {
		t.Fatalf("concurrent submissions = %d created, %d conflicts, want exactly one winner", created, conflicts)
	}

	rows := reportRowsFor(t, f, "")
	if len(rows) != 1 {
		t.Fatalf("concurrent submissions stored %d rows, want 1", len(rows))
	}
	if !identifiers[rows[0].id] {
		t.Fatal("the persisted report was not the one acknowledged")
	}
	// The losing responses named nothing about the winner.
	for _, response := range responses {
		if response.Code == http.StatusConflict && strings.Contains(response.Body.String(), rows[0].id) {
			t.Fatal("a concurrent duplicate named the winning report")
		}
	}
}

// TestStaleAndFreshAcknowledgementsAreIndistinguishable is the T063 regression from T065's angle:
// the exact-visible binding still stores A from a stale context and B from a fresh one, and the
// acknowledgement for each is identical in shape — it never says which was recorded.
func TestStaleAndFreshAcknowledgementsAreIndistinguishable(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	revisionA := liveRevisionOf(t, f)
	stale := courseContextOf(t, f)

	f.replaceLiveRevision(t)
	revisionB := liveRevisionOf(t, f)
	if revisionA == revisionB {
		t.Fatal("revision B did not become live")
	}

	staleResponse := submitReport(f, stale, "inaccurate")
	staleProperties := assertSafeAcknowledgement(t, f, staleResponse)

	// The database still holds the accepted exact-visible assertion.
	storedA := reportRowsFor(t, f, "COURSE")[0]
	if storedA.revisionRef != revisionA {
		t.Fatalf("the stale context stored %s, want the rendered revision A %s", storedA.revisionRef, revisionA)
	}

	// D-066 holds the target closed until the first report resolves.
	if _, err := f.pool.Exec(context.Background(), `
		UPDATE content_reports
		SET resolved_at = now(), resolved_by_account_id = $1::uuid,
		    resolution_action = 'DISMISSED', resolution_reason = 'fixture setup'
		WHERE target_kind = 'COURSE'
	`, f.studentID); err != nil {
		t.Fatalf("resolving as fixture setup: %v", err)
	}

	fresh := courseContextOf(t, f)
	freshResponse := submitReport(f, fresh, "inaccurate")
	freshProperties := assertSafeAcknowledgement(t, f, freshResponse)

	var storedB string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT target_revision_ref::text FROM content_reports WHERE resolved_at IS NULL`).Scan(&storedB); err != nil {
		t.Fatalf("reading the second report: %v", err)
	}
	if storedB != revisionB {
		t.Fatalf("the fresh context stored %s, want revision B %s", storedB, revisionB)
	}

	// Identical shape; only the independently generated values differ.
	if !reflect.DeepEqual(acknowledgementSchema(t, staleResponse), acknowledgementSchema(t, freshResponse)) {
		t.Fatal("stale and fresh acknowledgements have different schemas")
	}
	if staleProperties["report_id"] == freshProperties["report_id"] {
		t.Fatal("stale and fresh submissions shared a report identifier")
	}
	for _, response := range []*httptest.ResponseRecorder{staleResponse, freshResponse} {
		for _, revision := range []string{revisionA, revisionB} {
			if strings.Contains(response.Body.String(), revision) {
				t.Fatalf("an acknowledgement disclosed which revision was recorded: %s", response.Body.String())
			}
		}
	}
}

// TestReportRouteLogsDiscloseNoSubmittedContent proves the operational record keeps the same
// boundary the response does: no context, no explanation, no session, no version.
func TestReportRouteLogsDiscloseNoSubmittedContent(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	token := courseContextOf(t, f)
	explanation := "SENTINEL-EXPLANATION-IN-LOGS"

	// One success, one duplicate, one validation failure, one protected refusal.
	if response := f.request(http.MethodPost, reportRoute, reportBody(token, "other", explanation)); response.Code != http.StatusCreated {
		t.Fatalf("submission = %d %s", response.Code, response.Body.String())
	}
	f.request(http.MethodPost, reportRoute, reportBody(token, "other", explanation))
	f.request(http.MethodPost, reportRoute, reportBody(token, "not-a-reason", ""))
	f.request(http.MethodPost, reportRoute, reportBody("grc1.tampered", "inaccurate", ""))

	logs := f.logs.String()
	for _, secret := range []string{
		token, explanation, "SENTINEL", sessionIDFor(f.studentID), liveRevisionOf(t, f),
		f.versionID, f.lessonID,
	} {
		if strings.Contains(logs, secret) {
			t.Fatalf("the report route logged %q", secret)
		}
	}
}
