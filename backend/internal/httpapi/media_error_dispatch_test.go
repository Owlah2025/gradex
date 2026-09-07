package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/media"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

// TestMediaErrorDispatchMapsContentTypeMismatchTo422 exercises the media error
// dispatch every media route funnels through, rather than the problem
// constructor on its own. writeMediaProblem selects its branch with
// errors.Is(err, media.ErrValidation), so a ContentTypeMismatchError that lost
// its Unwrap chain would fall through to the default branch and answer 500
// with no violation — a regression the constructor-level test cannot see.
func TestMediaErrorDispatchMapsContentTypeMismatchTo422(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for name, err := range map[string]error{
		"typed error":   &media.ContentTypeMismatchError{DeclaredContentType: "video/mp4", ActualContentType: "video/webm"},
		"wrapped error": fmt.Errorf("completing upload: %w", &media.ContentTypeMismatchError{DeclaredContentType: "video/mp4", ActualContentType: "video/webm"}),
	} {
		t.Run(name, func(t *testing.T) {
			router := gin.New()
			router.POST("/media/assets/:id/completions", func(c *gin.Context) {
				writeMediaProblem(c, err)
			})

			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/media/assets/asset-1/completions", nil))

			if response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status=%d body=%s, want 422", response.Code, response.Body.String())
			}
			var got problem.Problem
			if decodeErr := json.Unmarshal(response.Body.Bytes(), &got); decodeErr != nil {
				t.Fatalf("decoding problem: %v", decodeErr)
			}
			if got.Code != "VALIDATION_FAILED" {
				t.Fatalf("code=%q, want VALIDATION_FAILED", got.Code)
			}
			if len(got.Violations) != 1 || got.Violations[0].Pointer != "/content_type" {
				t.Fatalf("violations=%+v, want one /content_type violation", got.Violations)
			}
			if got.Violations[0].Code != "CONTENT_TYPE_MISMATCH" {
				t.Fatalf("violation code=%q, want CONTENT_TYPE_MISMATCH", got.Violations[0].Code)
			}
		})
	}
}
