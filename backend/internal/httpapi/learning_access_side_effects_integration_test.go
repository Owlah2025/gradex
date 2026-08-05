//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Owlah2025/gradex/backend/internal/media"
)

type learningAuthoritySnapshot struct {
	entitlements string
	enrollments  string
	progress     string
}

func (f learningIntegrationFixture) authoritySnapshot(t *testing.T) learningAuthoritySnapshot {
	t.Helper()
	var snapshot learningAuthoritySnapshot
	err := f.pool.QueryRow(context.Background(), `
		SELECT
		  COALESCE((SELECT jsonb_agg(to_jsonb(e) ORDER BY e.id)
		              FROM entitlements e
		             WHERE e.student_account_id = $1::uuid AND e.course_id = $2::uuid), '[]'::jsonb)::text,
		  COALESCE((SELECT jsonb_agg(to_jsonb(e) ORDER BY e.id)
		              FROM enrollments e
		             WHERE e.student_account_id = $1::uuid AND e.course_id = $2::uuid), '[]'::jsonb)::text,
		  COALESCE((SELECT jsonb_agg(to_jsonb(p) ORDER BY p.enrollment_id, p.course_lesson_identity_id)
		              FROM progress p
		              JOIN enrollments e ON e.id = p.enrollment_id
		             WHERE e.student_account_id = $1::uuid AND e.course_id = $2::uuid), '[]'::jsonb)::text
	`, f.studentID, f.courseID).Scan(&snapshot.entitlements, &snapshot.enrollments, &snapshot.progress)
	if err != nil {
		t.Fatalf("reading authority snapshot: %v", err)
	}
	return snapshot
}

type denialTransition struct {
	name          string
	mutate        func(*testing.T, learningIntegrationFixture)
	neverAuthored bool
}

func runtimeDenialTransitions() []denialTransition {
	return []denialTransition{
		{
			name: "expired",
			mutate: func(t *testing.T, f learningIntegrationFixture) {
				if _, err := f.pool.Exec(context.Background(), `UPDATE entitlements SET original_access_ends_at = $1, access_ends_at = $1 WHERE student_account_id = $2::uuid AND course_id = $3::uuid`, f.clock.Now(), f.studentID, f.courseID); err != nil {
					t.Fatalf("expiring entitlement: %v", err)
				}
			},
		},
		{
			name: "revoked",
			mutate: func(t *testing.T, f learningIntegrationFixture) {
				if _, err := f.pool.Exec(context.Background(), `UPDATE entitlements SET state = 'REVOKED', revoked_at = $1 WHERE student_account_id = $2::uuid AND course_id = $3::uuid`, f.clock.Now(), f.studentID, f.courseID); err != nil {
					t.Fatalf("revoking entitlement: %v", err)
				}
			},
		},
		{
			name: "out of scope",
			mutate: func(t *testing.T, f learningIntegrationFixture) {
				otherSection := uuid.NewString()
				if _, err := f.pool.Exec(context.Background(), `INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)`, otherSection, f.courseID); err != nil {
					t.Fatalf("seeding alternate section: %v", err)
				}
				if _, err := f.pool.Exec(context.Background(), `UPDATE entitlements SET scope_kind = 'SECTION', scope_id = $1::uuid WHERE student_account_id = $2::uuid AND course_id = $3::uuid`, otherSection, f.studentID, f.courseID); err != nil {
					t.Fatalf("moving entitlement out of lesson scope: %v", err)
				}
			},
		},
		{
			name: "account suspended",
			mutate: func(t *testing.T, f learningIntegrationFixture) {
				if _, err := f.pool.Exec(context.Background(), `UPDATE accounts SET status = 'SUSPENDED' WHERE id = $1::uuid`, f.studentID); err != nil {
					t.Fatalf("suspending account: %v", err)
				}
			},
		},
		{
			name: "emergency suspended",
			mutate: func(t *testing.T, f learningIntegrationFixture) {
				if _, err := f.pool.Exec(context.Background(), `UPDATE courses SET access_suspended_at = $1, access_suspension_reason = 'side-effect-test' WHERE id = $2::uuid`, f.clock.Now(), f.courseID); err != nil {
					t.Fatalf("suspending Course access: %v", err)
				}
			},
		},
		{
			name: "retired ineligible",
			mutate: func(t *testing.T, f learningIntegrationFixture) {
				if _, err := f.pool.Exec(context.Background(), `UPDATE courses SET retired_at = $1 WHERE id = $2::uuid`, f.clock.Now(), f.courseID); err != nil {
					t.Fatalf("retiring Course: %v", err)
				}
				if _, err := f.pool.Exec(context.Background(), `UPDATE entitlements SET retirement_eligibility_at = $1 WHERE student_account_id = $2::uuid AND course_id = $3::uuid`, f.clock.Now(), f.studentID, f.courseID); err != nil {
					t.Fatalf("removing retirement eligibility: %v", err)
				}
			},
		},
		{name: "never-authored Lesson id", neverAuthored: true},
	}
}

func TestEveryDenialLeavesEntitlementEnrollmentAndProgressUnchanged(t *testing.T) {
	baseline := newLearningIntegrationFixture(t)
	routes := protectedLearningRoutes(t, baseline)
	baseline.pool.Close()
	for _, transition := range runtimeDenialTransitions() {
		for _, route := range routes {
			transition, route := transition, route
			t.Run(transition.name+"/"+route.method, func(t *testing.T) {
				if transition.neverAuthored && route.method == http.MethodGet {
					t.Skip("never-authored transition is exercised on mutation routes; read routes use current graph identifiers")
				}
				if transition.neverAuthored && !strings.Contains(route.path, ":lessonId") {
					// The transition substitutes a never-authored Lesson into the path. A route
					// that carries no path target cannot express it; T063's equivalent case — a
					// context bound to a target that is no longer relationally valid — is covered
					// in its own refusal matrix.
					t.Skip("never-authored transition needs a Lesson path parameter")
				}
				f := newLearningIntegrationFixture(t)
				method, path, body := protectedLearningRequest(t, f, route)
				assertLearningAllowed(t, route, f.request(method, path, body))
				if transition.mutate != nil {
					transition.mutate(t, f)
				}
				if transition.neverAuthored {
					path = "/api/v1/learn/lessons/" + uuid.NewString() + "/" + route.path[strings.LastIndex(route.path, "/")+1:]
				}
				before := f.authoritySnapshot(t)
				response := f.request(method, path, body)
				if route.method == http.MethodGet {
					if transition.name == "account suspended" {
						assertProtectedUnavailable(t, response)
					} else if transition.name == "expired" {
						assertReadSuccess(t, response)
						if !strings.Contains(response.Body.String(), `"learning_status":"expired"`) {
							t.Fatalf("expired read response = %s", response.Body.String())
						}
					} else if strings.HasSuffix(route.path, "/dashboard") {
						assertReadSuccess(t, response)
						if response.Body.String() != `{"courses":[]}` {
							t.Fatalf("omitted dashboard response = %s", response.Body.String())
						}
					} else {
						assertProtectedUnavailable(t, response)
					}
				} else {
					assertProtectedUnavailable(t, response)
				}
				after := f.authoritySnapshot(t)
				if before != after {
					t.Fatalf("%s denial mutated authority:\nbefore=%+v\nafter=%+v", transition.name, before, after)
				}
			})
		}
	}
}

func TestProtectedPlaybackDoesNotExtendS4TokenBoundary(t *testing.T) {
	f := newLearningIntegrationFixture(t)
	route := playbackRoute(t, f)
	method, path, body := protectedLearningRequest(t, f, route)
	accessEnds := f.clock.Now().Add(time.Minute)
	if _, err := f.pool.Exec(context.Background(), `UPDATE entitlements SET original_access_ends_at = $1, access_ends_at = $1 WHERE student_account_id = $2::uuid AND course_id = $3::uuid`, accessEnds, f.studentID, f.courseID); err != nil {
		t.Fatalf("setting access boundary: %v", err)
	}

	f.clock.now = accessEnds.Add(-time.Second)
	issuedResponse := f.request(method, path, body)
	assertLearningAllowed(t, route, issuedResponse)
	var issued media.PlaybackAuthorization
	if err := json.Unmarshal(issuedResponse.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decoding issued playback authorization: %v", err)
	}
	if !issued.ExpiresAt.After(accessEnds) {
		t.Fatalf("issued URL expires at %s, want after access boundary %s", issued.ExpiresAt, accessEnds)
	}
	if f.store.callCount() != 1 {
		t.Fatalf("initial playback signed %d URLs, want 1", f.store.callCount())
	}

	f.clock.now = accessEnds.Add(time.Second)
	assertProtectedUnavailable(t, f.request(method, path, body))
	if f.store.callCount() != 1 {
		t.Fatalf("post-expiry request created a new signed URL; calls=%d", f.store.callCount())
	}
}
