//go:build integration

package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/Owlah2025/gradex/backend/internal/learning"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

// T064 route evidence: the production router, real PostgreSQL, and the real
// limiter backends.
//
// The accepted fixture allows every request so T063's evidence is not bounded
// by a quota. These fixtures install an enforcing store instead, so the
// threshold, the isolation, and the outcome semantics are observed exactly
// where a Student would meet them.

// throttlingRedisClient reaches the local Redis the limiter integration tests
// already use. Keys derive from a per-run HMAC so nothing shared is touched.
func throttlingRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("Redis is required for the report throttle integration test: %v", err)
	}
	return client
}

// throttledFixture composes the accepted learning fixture with the production
// Redis-backed limiter store.
func throttledFixture(t *testing.T) learningIntegrationFixture {
	t.Helper()
	return newLearningIntegrationFixtureWith(t, learningFixtureOptions{
		rateStore: ratelimit.NewRedisStore(throttlingRedisClient(t)),
	})
}

// localThrottledFixture composes the same route with a failing distributed
// store, so the bounded local fallback makes every decision.
func localThrottledFixture(t *testing.T) learningIntegrationFixture {
	t.Helper()
	return newLearningIntegrationFixtureWith(t, learningFixtureOptions{rateStore: failingReportRateStore{}})
}

type failingReportRateStore struct{}

func (failingReportRateStore) Decide(context.Context, []ratelimit.Entry) (bool, error) {
	return false, fmt.Errorf("distributed rate-limit store is unavailable")
}

// distinctReportContext mints a valid context for a target the Student has not
// yet reported, so D-066's duplicate rule never masks a throttle decision.
func distinctReportContext(t *testing.T, f learningIntegrationFixture, index int) string {
	t.Helper()
	kinds := []learning.ReportTargetKind{
		learning.ReportTargetCourse, learning.ReportTargetLesson,
	}
	if index < len(kinds) {
		request := learning.ReportContextRequest{TargetKind: kinds[index], StableTargetID: f.lessonID}
		if kinds[index] == learning.ReportTargetCourse {
			request.StableTargetID = f.courseID
		}
		request.CourseID = f.courseID
		request.VisibleCourseRevisionID = liveRevisionOf(t, f)
		return fixtureContext(t, f, request)
	}
	// Beyond the two stable kinds the target is deliberately foreign: the
	// request is still an authenticated submission and still costs an attempt,
	// which is exactly the semantics under test.
	return fixtureContext(t, f, learning.ReportContextRequest{
		TargetKind: learning.ReportTargetLesson, CourseID: f.courseID,
		StableTargetID: uuid.NewString(), VisibleCourseRevisionID: liveRevisionOf(t, f),
	})
}

func assertThrottled(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("throttled Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
	seconds, err := strconv.Atoi(response.Header().Get("Retry-After"))
	if err != nil || seconds <= 0 || seconds > int(time.Hour.Seconds()) {
		t.Fatalf("Retry-After = %q, want a positive value inside the one-hour window", response.Header().Get("Retry-After"))
	}
}

// TestReportRouteAdmitsFivePerHourAndThenThrottles is the threshold through the
// real route: five authenticated submissions are admitted, the sixth is not,
// and the sixth writes nothing.
func TestReportRouteAdmitsFivePerHourAndThenThrottles(t *testing.T) {
	f := throttledFixture(t)
	admitted := int(ratelimit.ProtectedLearningReportsPerHour)

	for attempt := 0; attempt < admitted; attempt++ {
		response := submitReport(f, distinctReportContext(t, f, attempt), "inaccurate")
		if response.Code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d was throttled inside the quota", attempt+1)
		}
		// Each admitted attempt reaches its ordinary T063 outcome.
		switch attempt {
		case 0, 1:
			if response.Code != http.StatusCreated {
				t.Fatalf("attempt %d = %d %s, want a created report", attempt+1, response.Code, response.Body.String())
			}
		default:
			if response.Code != http.StatusNotFound {
				t.Fatalf("attempt %d = %d, want the uniform refusal for a foreign target", attempt+1, response.Code)
			}
		}
	}

	rowsBefore := reportCount(t, f)
	assertThrottled(t, submitReport(f, distinctReportContext(t, f, 0), "inaccurate"))
	if got := reportCount(t, f); got != rowsBefore {
		t.Fatalf("a throttled submission changed the report count from %d to %d", rowsBefore, got)
	}
}

// TestReportThrottleCountsEveryAuthenticatedOutcome is the attempt-count
// matrix: a created report, a duplicate, a malformed body, an invalid reason,
// and a protected refusal each cost one attempt, so no failure mode is a free
// way to keep submitting.
func TestReportThrottleCountsEveryAuthenticatedOutcome(t *testing.T) {
	f := throttledFixture(t)
	contexts := lessonContextsOf(t, f)
	valid := contexts.Lesson

	outcomes := []struct {
		name string
		body string
		want int
	}{
		{"created", reportBody(valid, "inaccurate", ""), http.StatusCreated},
		{"duplicate", reportBody(valid, "inappropriate", ""), http.StatusConflict},
		{"malformed JSON", `{ not json`, http.StatusBadRequest},
		{"invalid reason", reportBody(valid, "spam", ""), http.StatusUnprocessableEntity},
		{"protected refusal", reportBody("grc1.tampered", "inaccurate", ""), http.StatusNotFound},
	}
	if len(outcomes) != int(ratelimit.ProtectedLearningReportsPerHour) {
		t.Fatalf("the matrix must consume the whole quota; it has %d of %d", len(outcomes), ratelimit.ProtectedLearningReportsPerHour)
	}

	for _, outcome := range outcomes {
		response := f.request(http.MethodPost, reportRoute, outcome.body)
		if response.Code != outcome.want {
			t.Fatalf("%s = %d %s, want %d", outcome.name, response.Code, response.Body.String(), outcome.want)
		}
	}

	// Five attempts of five different kinds exhausted the quota exactly.
	assertThrottled(t, f.request(http.MethodPost, reportRoute, reportBody(valid, "inaccurate", "")))
}

// TestReportThrottleIsolatesStudents proves one Student's exhausted quota does
// not reach another's.
func TestReportThrottleIsolatesStudents(t *testing.T) {
	store := ratelimit.NewRedisStore(throttlingRedisClient(t))
	first := newLearningIntegrationFixtureWith(t, learningFixtureOptions{rateStore: store})

	for attempt := 0; attempt < int(ratelimit.ProtectedLearningReportsPerHour); attempt++ {
		if response := submitReport(first, distinctReportContext(t, first, attempt), "inaccurate"); response.Code == http.StatusTooManyRequests {
			t.Fatalf("Student A attempt %d was throttled inside the quota", attempt+1)
		}
	}
	assertThrottled(t, submitReport(first, distinctReportContext(t, first, 0), "inaccurate"))

	// A second Student in the same database and the same limiter is untouched.
	second := newLearningIntegrationFixtureWith(t, learningFixtureOptions{rateStore: store, pool: first.pool})
	if response := submitReport(second, courseContextOf(t, second), "inaccurate"); response.Code != http.StatusCreated {
		t.Fatalf("Student B = %d %s, want an independent quota", response.Code, response.Body.String())
	}
}

// TestReportThrottleFollowsTheStudentAcrossSessionsAndCourses proves the quota
// is Account-scoped: signing in again does not reset it, and a second Course
// does not open a second allowance.
func TestReportThrottleFollowsTheStudentAcrossSessionsAndCourses(t *testing.T) {
	store := ratelimit.NewRedisStore(throttlingRedisClient(t))
	session1 := newLearningIntegrationFixtureWith(t, learningFixtureOptions{rateStore: store})

	// Three attempts in the first session and first Course.
	for attempt := 0; attempt < 3; attempt++ {
		if response := submitReport(session1, distinctReportContext(t, session1, attempt), "inaccurate"); response.Code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d was throttled inside the quota", attempt+1)
		}
	}

	// The same Student, a different authenticated session, and a different
	// Course: the remaining allowance is the same allowance.
	otherCourse := newLearningIntegrationFixtureWith(t, learningFixtureOptions{
		rateStore: store, pool: session1.pool,
		studentID: session1.studentID, sessionID: "second-session-" + session1.studentID,
	})
	if otherCourse.courseID == session1.courseID {
		t.Fatal("the second fixture reused the first Course")
	}
	for attempt := 0; attempt < 2; attempt++ {
		if response := submitReport(otherCourse, distinctReportContext(t, otherCourse, attempt), "inaccurate"); response.Code == http.StatusTooManyRequests {
			t.Fatalf("cross-Course attempt %d was throttled inside the shared quota", attempt+1)
		}
	}

	// Five in total across two sessions and two Courses: the sixth is refused
	// in either session.
	assertThrottled(t, submitReport(otherCourse, distinctReportContext(t, otherCourse, 0), "inaccurate"))
	assertThrottled(t, submitReport(session1, distinctReportContext(t, session1, 0), "inaccurate"))
}

// TestReportThrottleLocalFallbackEnforcesTheSameThresholdThroughTheRoute is the
// local-parity evidence at the route: with the distributed store unavailable,
// the bounded fallback admits the same five and refuses the sixth.
func TestReportThrottleLocalFallbackEnforcesTheSameThresholdThroughTheRoute(t *testing.T) {
	f := localThrottledFixture(t)

	for attempt := 0; attempt < int(ratelimit.ProtectedLearningReportsPerHour); attempt++ {
		if response := submitReport(f, distinctReportContext(t, f, attempt), "inaccurate"); response.Code == http.StatusTooManyRequests {
			t.Fatalf("local fallback throttled attempt %d inside the quota", attempt+1)
		}
	}
	assertThrottled(t, submitReport(f, distinctReportContext(t, f, 0), "inaccurate"))
}

// TestReportThrottleDoesNotOvershootUnderConcurrency proves concurrent
// submissions cannot claim more than the quota. Every attempt names a distinct
// target, so the duplicate index cannot be mistaken for the limiter.
func TestReportThrottleDoesNotOvershootUnderConcurrency(t *testing.T) {
	f := throttledFixture(t)
	const attempts = 16
	tokens := make([]string, attempts)
	for i := range tokens {
		tokens[i] = fixtureContext(t, f, learning.ReportContextRequest{
			TargetKind: learning.ReportTargetLesson, CourseID: f.courseID,
			StableTargetID: uuid.NewString(), VisibleCourseRevisionID: liveRevisionOf(t, f),
		})
	}

	var wait sync.WaitGroup
	codes := make([]int, attempts)
	for i := 0; i < attempts; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			codes[index] = submitReport(f, tokens[index], "inaccurate").Code
		}(i)
	}
	wait.Wait()

	admitted, throttled := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusTooManyRequests:
			throttled++
		case http.StatusNotFound:
			admitted++
		default:
			t.Fatalf("concurrent submission produced an unexpected status %d", code)
		}
	}
	if admitted != int(ratelimit.ProtectedLearningReportsPerHour) {
		t.Fatalf("concurrent submissions admitted %d, want exactly %d", admitted, ratelimit.ProtectedLearningReportsPerHour)
	}
	if throttled != attempts-admitted {
		t.Fatalf("concurrent submissions throttled %d, want %d", throttled, attempts-admitted)
	}
	// Both backends survive the race.
	if response := f.request(http.MethodGet, "/api/v1/learn/courses/"+f.courseID, ""); response.Code != http.StatusOK {
		t.Fatalf("read after concurrent throttling = %d", response.Code)
	}
}

// TestThrottledReportMutatesNothing captures before/after state across every
// table a report could plausibly touch.
func TestThrottledReportMutatesNothing(t *testing.T) {
	f := throttledFixture(t)
	attachLessonMaterial(t, f, "RESOURCE", "throttle-resource")
	for attempt := 0; attempt < int(ratelimit.ProtectedLearningReportsPerHour); attempt++ {
		submitReport(f, distinctReportContext(t, f, attempt), "inaccurate")
	}

	beforeAuthority := f.authoritySnapshot(t)
	beforeContent := contentSnapshot(t, f)
	beforeReports := reportRowsFor(t, f, "")

	assertThrottled(t, submitReport(f, distinctReportContext(t, f, 0), "inaccurate"))

	if after := f.authoritySnapshot(t); beforeAuthority != after {
		t.Fatalf("a throttled submission mutated authority:\nbefore=%+v\nafter=%+v", beforeAuthority, after)
	}
	afterContent := contentSnapshot(t, f)
	for name, value := range beforeContent {
		if afterContent[name] != value {
			t.Fatalf("a throttled submission mutated %s", name)
		}
	}
	afterReports := reportRowsFor(t, f, "")
	if len(afterReports) != len(beforeReports) {
		t.Fatalf("a throttled submission changed report rows from %d to %d", len(beforeReports), len(afterReports))
	}
	for i := range beforeReports {
		if beforeReports[i] != afterReports[i] {
			t.Fatalf("a throttled submission altered an existing report:\nbefore=%+v\nafter=%+v", beforeReports[i], afterReports[i])
		}
	}
}

// TestThrottleDoesNotDisturbExactVisibleBinding reruns T063's A→B evidence with
// the throttle in place: an admitted submission from a stale page still stores
// the instance it rendered, for every kind. The limiter must not become a
// reason to re-resolve current content.
func TestThrottleDoesNotDisturbExactVisibleBinding(t *testing.T) {
	f := throttledFixture(t)
	resourceA := attachLessonMaterial(t, f, "RESOURCE", "throttle-resource-a")
	labA := attachLessonMaterial(t, f, "LAB_MATERIAL", "throttle-lab-a")
	revisionA := liveRevisionOf(t, f)

	staleCourse := courseContextOf(t, f)
	staleLesson := lessonContextsOf(t, f)

	f.replaceLiveRevision(t)
	if liveRevisionOf(t, f) == revisionA {
		t.Fatal("revision B did not become live")
	}

	// Exactly the quota: five kinds, five attempts.
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
		response := submitReport(f, testCase.token, "inaccurate")
		if response.Code != http.StatusCreated {
			t.Fatalf("%s admitted submission = %d %s", testCase.kind, response.Code, response.Body.String())
		}
		rows := reportRowsFor(t, f, testCase.kind)
		if len(rows) != 1 {
			t.Fatalf("%s produced %d rows", testCase.kind, len(rows))
		}
		if rows[0].revisionRef != testCase.want {
			t.Fatalf("%s stored %s under the throttle, want the rendered instance %s",
				testCase.kind, rows[0].revisionRef, testCase.want)
		}
	}

	// The quota is now spent, and the refusal is the throttle's, not T063's.
	assertThrottled(t, submitReport(f, staleCourse, "inaccurate"))
}

// TestThrottledResponseRevealsNothing pins the information boundary of a quota
// refusal at the route.
func TestThrottledResponseRevealsNothing(t *testing.T) {
	f := throttledFixture(t)
	token := courseContextOf(t, f)
	for attempt := 0; attempt < int(ratelimit.ProtectedLearningReportsPerHour); attempt++ {
		submitReport(f, distinctReportContext(t, f, attempt), "inaccurate")
	}

	response := submitReport(f, token, "inaccurate")
	assertThrottled(t, response)
	for _, secret := range []string{
		f.studentID, f.courseID, f.lessonID, f.versionID, liveRevisionOf(t, f), token,
		sessionIDFor(f.studentID), "learning-report-v1", "gradex:rl:",
	} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("the throttled response exposed %q", secret)
		}
	}
}
