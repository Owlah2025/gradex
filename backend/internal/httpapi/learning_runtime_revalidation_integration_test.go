//go:build integration

package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type learningWireResponse struct {
	status int
	header http.Header
	body   string
}

func protectedLearningRoutes(t *testing.T, fixture learningIntegrationFixture) []ginRoute {
	t.Helper()
	routes := make([]ginRoute, 0)
	for _, route := range fixture.router.Routes() {
		if strings.HasPrefix(route.Path, "/api/v1/learn/") {
			routes = append(routes, ginRoute{method: route.Method, path: route.Path})
		}
	}
	if len(routes) == 0 {
		t.Fatal("production router mounted no protected learning routes")
	}
	return routes
}

type ginRoute struct {
	method string
	path   string
}

func protectedLearningRequest(t *testing.T, fixture learningIntegrationFixture, route ginRoute) (method, path, body string) {
	t.Helper()
	path = strings.ReplaceAll(route.path, ":lessonId", fixture.lessonID)
	path = strings.ReplaceAll(path, ":courseId", fixture.courseID)
	if strings.Contains(path, ":") {
		t.Fatalf("protected learning route %s %s has an unsupported parameter", route.method, route.path)
	}
	switch {
	case route.method == http.MethodGet:
		return route.method, path, ""
	case route.method == http.MethodPost && strings.HasSuffix(route.path, "/playback"):
		return route.method, path, ""
	case route.method == http.MethodPut && strings.HasSuffix(route.path, "/progress"):
		return route.method, path, `{"position_seconds":30,"asset_version_id":"` + fixture.versionID + `"}`
	case route.method == http.MethodPost && strings.HasSuffix(route.path, "/reports"):
		// T063 names no target. The body carries the opaque context the active Course Home read
		// just issued, so this is exactly the request a rendered page would send.
		return route.method, path, `{"report_context":"` + courseContextOf(t, fixture) + `","reason":"inaccurate"}`
	default:
		t.Fatalf("protected learning route %s %s has no production request adapter; extend this test with its contract", route.method, route.path)
	}
	return "", "", ""
}

func learningWire(response *httptest.ResponseRecorder) learningWireResponse {
	return learningWireResponse{status: response.Code, header: response.Header().Clone(), body: response.Body.String()}
}

func assertLearningAllowed(t *testing.T, route ginRoute, response *httptest.ResponseRecorder) {
	t.Helper()
	want := http.StatusOK
	if route.method == http.MethodPut {
		want = http.StatusNoContent
	}
	if route.method == http.MethodPost && strings.HasSuffix(route.path, "/reports") {
		want = http.StatusCreated
	}
	if response.Code != want || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("initial %s %s = status %d headers=%v body=%s, want allowed %d no-store", route.method, route.path, response.Code, response.Header(), response.Body.String(), want)
	}
}

func assertProtectedUnavailable(t *testing.T, response *httptest.ResponseRecorder) learningWireResponse {
	t.Helper()
	got := learningWire(response)
	if got.status != http.StatusNotFound || got.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("protected denial = status %d headers=%v body=%q, want uniform no-store 404", got.status, got.header, got.body)
	}
	if got.header.Get("WWW-Authenticate") != "" || got.header.Get("Retry-After") != "" || got.header.Get("Location") != "" || got.header.Get("X-Request-ID") != "" {
		t.Fatalf("protected denial leaked a distinguishing header: %v", got.header)
	}
	return got
}

func assertSameLearningWire(t *testing.T, want, got learningWireResponse) {
	t.Helper()
	if got.status != want.status || got.body != want.body || !reflect.DeepEqual(got.header, want.header) {
		t.Fatalf("protected denial is distinguishable:\nstatus got/want: %d/%d\nheaders got/want: %#v/%#v\nbody got/want: %q/%q", got.status, want.status, got.header, want.header, got.body, want.body)
	}
}

func playbackRoute(t *testing.T, fixture learningIntegrationFixture) ginRoute {
	t.Helper()
	for _, route := range protectedLearningRoutes(t, fixture) {
		if route.method == http.MethodPost && strings.HasSuffix(route.path, "/playback") {
			return route
		}
	}
	t.Fatal("production router mounted no protected playback route")
	return ginRoute{}
}

func TestLearningRuntimeExpiryUsesInjectedUTCClock(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	route := playbackRoute(t, f)
	method, path, body := protectedLearningRequest(t, f, route)
	expiresAt := f.clock.Now()
	if _, err := f.pool.Exec(t.Context(), `UPDATE entitlements SET original_access_ends_at = $1, access_ends_at = $1 WHERE student_account_id = $2::uuid AND course_id = $3::uuid`, expiresAt, f.studentID, f.courseID); err != nil {
		t.Fatalf("setting deterministic expiry: %v", err)
	}

	f.clock.now = expiresAt.Add(-time.Nanosecond)
	assertLearningAllowed(t, route, f.request(method, path, body))

	f.clock.now = expiresAt
	assertProtectedUnavailable(t, f.request(method, path, body))

	f.clock.now = expiresAt.Add(time.Nanosecond)
	assertProtectedUnavailable(t, f.request(method, path, body))
}

func TestDenialsAreByteIdentical(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*testing.T, learningIntegrationFixture)
		requestFor func(learningIntegrationFixture, string) string
	}{
		{
			name: "expired",
			mutate: func(t *testing.T, f learningIntegrationFixture) {
				if _, err := f.pool.Exec(t.Context(), `UPDATE entitlements SET original_access_ends_at = $1, access_ends_at = $1 WHERE student_account_id = $2::uuid AND course_id = $3::uuid`, f.clock.Now(), f.studentID, f.courseID); err != nil {
					t.Fatalf("expiring entitlement: %v", err)
				}
			},
		},
		{
			name: "revoked",
			mutate: func(t *testing.T, f learningIntegrationFixture) {
				if _, err := f.pool.Exec(t.Context(), `UPDATE entitlements SET state = 'REVOKED', revoked_at = $1 WHERE student_account_id = $2::uuid AND course_id = $3::uuid`, f.clock.Now(), f.studentID, f.courseID); err != nil {
					t.Fatalf("revoking entitlement: %v", err)
				}
			},
		},
		{
			name: "out of scope",
			mutate: func(t *testing.T, f learningIntegrationFixture) {
				otherSection := uuid.NewString()
				if _, err := f.pool.Exec(t.Context(), `INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)`, otherSection, f.courseID); err != nil {
					t.Fatalf("seeding out-of-scope section: %v", err)
				}
				if _, err := f.pool.Exec(t.Context(), `UPDATE entitlements SET scope_kind = 'SECTION', scope_id = $1::uuid WHERE student_account_id = $2::uuid AND course_id = $3::uuid`, otherSection, f.studentID, f.courseID); err != nil {
					t.Fatalf("narrowing entitlement outside the lesson scope: %v", err)
				}
			},
		},
		{
			name: "account suspended",
			mutate: func(t *testing.T, f learningIntegrationFixture) {
				if _, err := f.pool.Exec(t.Context(), `UPDATE accounts SET status = 'SUSPENDED' WHERE id = $1::uuid`, f.studentID); err != nil {
					t.Fatalf("suspending account: %v", err)
				}
			},
		},
		{
			name: "emergency suspended",
			mutate: func(t *testing.T, f learningIntegrationFixture) {
				if _, err := f.pool.Exec(t.Context(), `UPDATE courses SET access_suspended_at = $1, access_suspension_reason = 'runtime-test' WHERE id = $2::uuid`, f.clock.Now(), f.courseID); err != nil {
					t.Fatalf("suspending course access: %v", err)
				}
			},
		},
		{
			name: "retired ineligible",
			mutate: func(t *testing.T, f learningIntegrationFixture) {
				if _, err := f.pool.Exec(t.Context(), `UPDATE courses SET retired_at = $1 WHERE id = $2::uuid`, f.clock.Now(), f.courseID); err != nil {
					t.Fatalf("retiring course: %v", err)
				}
				if _, err := f.pool.Exec(t.Context(), `UPDATE entitlements SET retirement_eligibility_at = $1 WHERE student_account_id = $2::uuid AND course_id = $3::uuid`, f.clock.Now(), f.studentID, f.courseID); err != nil {
					t.Fatalf("making entitlement retirement-ineligible: %v", err)
				}
			},
		},
		{
			name:   "never authored lesson",
			mutate: func(*testing.T, learningIntegrationFixture) {},
			requestFor: func(_ learningIntegrationFixture, _ string) string {
				return "/api/v1/learn/lessons/" + uuid.NewString() + "/playback"
			},
		},
	}

	var baseline *learningWireResponse
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newLearningIntegrationFixture(t)
			route := playbackRoute(t, f)
			method, path, body := protectedLearningRequest(t, f, route)
			assertLearningAllowed(t, route, f.request(method, path, body))
			if f.store.callCount() != 1 {
				t.Fatalf("initial allowed playback signed %d URLs, want 1", f.store.callCount())
			}
			tc.mutate(t, f)
			if tc.requestFor != nil {
				path = tc.requestFor(f, path)
			}
			denied := assertProtectedUnavailable(t, f.request(method, path, body))
			if f.store.callCount() != 1 {
				t.Fatalf("denied %s request issued another playback URL", tc.name)
			}
			if baseline == nil {
				baseline = &denied
			} else {
				assertSameLearningWire(t, *baseline, denied)
			}
		})
	}
}

func TestQualifyingRetirementRemainsDelegatedToS4Evaluator(t *testing.T) {
	for _, lifecycle := range []string{"PUBLISHED", "DELISTED", "ARCHIVED"} {
		t.Run(strings.ToLower(lifecycle), func(t *testing.T) {
			f := newLearningIntegrationFixture(t)
			route := playbackRoute(t, f)
			method, path, body := protectedLearningRequest(t, f, route)
			assertLearningAllowed(t, route, f.request(method, path, body))

			retiredAt := f.clock.Now()
			if _, err := f.pool.Exec(t.Context(), `UPDATE courses SET lifecycle = $1::course_lifecycle, retired_at = $2 WHERE id = $3::uuid`, lifecycle, retiredAt, f.courseID); err != nil {
				t.Fatalf("applying %s retirement state: %v", lifecycle, err)
			}
			if _, err := f.pool.Exec(t.Context(), `UPDATE entitlements SET retirement_eligibility_at = $1 WHERE student_account_id = $2::uuid AND course_id = $3::uuid`, retiredAt.Add(-time.Nanosecond), f.studentID, f.courseID); err != nil {
				t.Fatalf("setting qualifying retirement eligibility: %v", err)
			}
			assertLearningAllowed(t, route, f.request(method, path, body))
		})
	}
}

func TestEveryProtectedLearningRouteRevalidates(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	routes := protectedLearningRoutes(t, f)
	var deniedBaseline *learningWireResponse

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			f := newLearningIntegrationFixture(t)
			method, path, body := protectedLearningRequest(t, f, route)
			assertLearningAllowed(t, route, f.request(method, path, body))
			beforeEvaluate, beforeTx, beforeTarget := f.evaluator.counts()
			beforeSigns := f.store.callCount()

			if _, err := f.pool.Exec(t.Context(), `UPDATE entitlements SET state = 'REVOKED', revoked_at = $1 WHERE student_account_id = $2::uuid AND course_id = $3::uuid`, f.clock.Now(), f.studentID, f.courseID); err != nil {
				t.Fatalf("revoking live access after successful %s: %v", route.path, err)
			}
			response := f.request(method, path, body)
			afterEvaluate, afterTx, afterTarget := f.evaluator.counts()

			if afterEvaluate != beforeEvaluate+1 {
				t.Fatalf("%s %s did not perform its own fresh handler evaluation after revocation: evaluate calls %d -> %d", route.method, route.path, beforeEvaluate, afterEvaluate)
			}
			var denied *learningWireResponse
			if route.method == http.MethodGet && strings.HasSuffix(route.path, "/dashboard") {
				if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || response.Body.String() != `{"courses":[]}` {
					t.Fatalf("revoked dashboard = status %d headers=%v body=%q, want empty no-store dashboard", response.Code, response.Header(), response.Body.String())
				}
			} else {
				wire := assertProtectedUnavailable(t, response)
				denied = &wire
			}
			switch route.method {
			case http.MethodPost:
				if afterTarget != beforeTarget || f.store.callCount() != beforeSigns {
					t.Fatalf("revoked playback reached S4 issuance: target evaluations %d -> %d, signatures %d -> %d", beforeTarget, afterTarget, beforeSigns, f.store.callCount())
				}
			case http.MethodPut:
				if afterTx != beforeTx {
					t.Fatalf("revoked progress reached final mutation guard: transaction evaluations %d -> %d", beforeTx, afterTx)
				}
				var position float64
				if err := f.pool.QueryRow(t.Context(), `SELECT max_position_seconds FROM progress`).Scan(&position); err != nil || position != 30 {
					t.Fatalf("revoked Progress changed durable state: position=%v err=%v", position, err)
				}
			}
			if denied != nil {
				if deniedBaseline == nil {
					deniedBaseline = denied
				} else {
					assertSameLearningWire(t, *deniedBaseline, *denied)
				}
			}
		})
	}
}

func TestLearningPayloadsExposeNoS5AuthorizationDecision(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	route := playbackRoute(t, f)
	method, path, body := protectedLearningRequest(t, f, route)
	response := f.request(method, path, body)
	assertLearningAllowed(t, route, response)

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decoding S4 playback issuance: %v", err)
	}
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	want := []string{"asset_version_id", "expires_at", "manifest_url", "playback_session"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("learning playback exposed an S5 authorization decision: keys=%v want S4 issuance=%v", keys, want)
	}
	for _, forbidden := range []string{"allowed", "capability", "decision", "entitlement", "entitlement_id", "scope"} {
		if _, ok := payload[forbidden]; ok {
			t.Fatalf("learning payload leaked client-representable S5 authorization field %q", forbidden)
		}
	}
}
