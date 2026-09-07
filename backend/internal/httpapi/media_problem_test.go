package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Owlah2025/gradex/backend/internal/media"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

func TestMediaContentTypeMismatchProblemIsActionableAndSafe(t *testing.T) {
	response := httptest.NewRecorder()
	if err := problem.Write(response, mediaValidationProblem(&media.ContentTypeMismatchError{
		DeclaredContentType: "video/mp4",
		ActualContentType:   "video/webm",
	})); err != nil {
		t.Fatalf("writing problem: %v", err)
	}
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d, want 422", response.Code)
	}
	var got problem.Problem
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding problem: %v", err)
	}
	if got.Code != "VALIDATION_FAILED" || len(got.Violations) != 1 {
		t.Fatalf("problem=%+v, want one validation violation", got)
	}
	violation := got.Violations[0]
	if violation.Code != "CONTENT_TYPE_MISMATCH" || violation.Pointer != "/content_type" || violation.Parameter != "content_type" {
		t.Fatalf("violation=%+v, want safe content-type violation", violation)
	}
	if violation.Detail != "The selected file does not match the required MP4 format. Choose a valid MP4 video." {
		t.Fatalf("detail=%q, want actionable MP4 guidance", violation.Detail)
	}
	for _, forbidden := range []string{"webm", "ebml", "quarantine", "storage"} {
		if strings.Contains(strings.ToLower(got.Detail+violation.Detail), forbidden) {
			t.Fatalf("problem leaked %q: %+v", forbidden, got)
		}
	}
}

func TestUnknownMediaValidationKeepsGenericProblem(t *testing.T) {
	got := mediaValidationProblem(media.ErrValidation)
	if got.Code != "VALIDATION_FAILED" || len(got.Violations) != 0 || got.Detail != "One or more fields are invalid." {
		t.Fatalf("problem=%+v, want generic validation fallback", got)
	}
}
