package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/requestid"
)

// fakeAuth is a shared test seam for pre-existing router tests. It does not
// mount routes or emulate a production handler.
type fakeAuth struct{ err error }

func (f fakeAuth) UserFromRequest(c *gin.Context) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	// Both production authenticators publish the authenticated session; a double that does not is
	// not modelling an authenticated request.
	c.Set("authenticated_session", identity.Session{ID: "test-session-user-1", AccountID: "user-1", State: identity.SessionActive})
	return "user-1", nil
}

// assertProblemEnvelope checks the repository-wide public error contract
// without reproducing any retired handler behavior.
func assertProblemEnvelope(t *testing.T, rec *httptest.ResponseRecorder) problem.Problem {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != problem.ContentType {
		t.Errorf("Content-Type = %q, want %q", ct, problem.ContentType)
	}

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("body is not JSON: %q", rec.Body.String())
	}
	if _, legacy := raw["error"]; legacy {
		t.Errorf("legacy {\"error\": ...} shape survived: %s", rec.Body.String())
	}

	var p problem.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("body is not a Problem: %v", err)
	}
	if p.Status != rec.Code {
		t.Errorf("body status %d disagrees with HTTP status %d", p.Status, rec.Code)
	}
	if p.Type == "" || p.Code == "" || p.Title == "" {
		t.Errorf("problem is missing type/code/title: %+v", p)
	}
	wantCode := strings.ToUpper(strings.ReplaceAll(
		strings.TrimPrefix(p.Type, "https://api.gradex.com/problems/"), "-", "_"))
	if p.Code != wantCode {
		t.Errorf("code %q contradicts type %q", p.Code, p.Type)
	}
	if header := rec.Header().Get(requestid.HeaderName); p.RequestID != header {
		t.Errorf("body request_id %q != X-Request-ID %q", p.RequestID, header)
	}
	if p.Instance == "" || !strings.HasPrefix(p.Instance, "urn:gradex:problem:") {
		t.Errorf("instance %q is not an opaque URN", p.Instance)
	}
	if strings.Contains(p.Instance, "/api/") {
		t.Errorf("instance %q leaks a resource path", p.Instance)
	}
	for _, forbidden := range []string{
		"gradex-video/raw", "/var/lib/gradex/tmp", "video:transcode",
		"videos_lesson_id_key", "AccessDenied: signature mismatch",
		"eyJhbGciOiJIUzI1NiJ9.signed-playback-token",
	} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Errorf("response leaked internal detail %q: %s", forbidden, rec.Body.String())
		}
	}
	return p
}
