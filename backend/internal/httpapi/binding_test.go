package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/problem"
)

type strictRequest struct {
	Email string `json:"email" binding:"required"`
}

func strictBindingRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/strict", func(c *gin.Context) {
		var request strictRequest
		if !bindStrictJSON(c, &request, 64) {
			return
		}
		c.JSON(http.StatusOK, request)
	})
	return router
}

func TestStrictJSONRejectsAmbiguousOrUnsupportedBodies(t *testing.T) {
	tests := map[string]struct {
		contentType string
		body        string
		wantStatus  int
		wantCode    string
	}{
		"valid": {
			contentType: "application/json",
			body:        `{"email":"student@example.com"}`,
			wantStatus:  http.StatusOK,
		},
		"duplicate member": {
			contentType: "application/json",
			body:        `{"email":"one@example.com","email":"two@example.com"}`,
			wantStatus:  http.StatusBadRequest,
			wantCode:    "MALFORMED_JSON",
		},
		"unknown member": {
			contentType: "application/json",
			body:        `{"email":"student@example.com","role":"ADMIN"}`,
			wantStatus:  http.StatusBadRequest,
			wantCode:    "MALFORMED_JSON",
		},
		"trailing document": {
			contentType: "application/json",
			body:        `{"email":"student@example.com"} {}`,
			wantStatus:  http.StatusBadRequest,
			wantCode:    "MALFORMED_JSON",
		},
		"wrong field type": {
			contentType: "application/json",
			body:        `{"email":42}`,
			wantStatus:  http.StatusBadRequest,
			wantCode:    "MALFORMED_JSON",
		},
		"missing required field": {
			contentType: "application/json",
			body:        `{}`,
			wantStatus:  http.StatusUnprocessableEntity,
			wantCode:    "VALIDATION_FAILED",
		},
		"unsupported media type": {
			contentType: "text/plain",
			body:        `{"email":"student@example.com"}`,
			wantStatus:  http.StatusUnsupportedMediaType,
			wantCode:    "UNSUPPORTED_MEDIA_TYPE",
		},
		"unsupported charset": {
			contentType: "application/json; charset=iso-8859-1",
			body:        `{"email":"student@example.com"}`,
			wantStatus:  http.StatusUnsupportedMediaType,
			wantCode:    "UNSUPPORTED_MEDIA_TYPE",
		},
		"oversized": {
			contentType: "application/json",
			body:        `{"email":"` + strings.Repeat("a", 65) + `"}`,
			wantStatus:  http.StatusRequestEntityTooLarge,
			wantCode:    "CONTENT_TOO_LARGE",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/strict", strings.NewReader(tt.body))
			request.Header.Set("Content-Type", tt.contentType)
			recorder := httptest.NewRecorder()
			strictBindingRouter().ServeHTTP(recorder, request)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
			if tt.wantCode == "" {
				return
			}
			if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store before strict binding can fail", got)
			}
			if recorder.Header().Get("Content-Type") != problem.ContentType {
				t.Errorf("Content-Type = %q", recorder.Header().Get("Content-Type"))
			}
			var got problem.Problem
			if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
				t.Fatalf("decoding Problem Details: %v", err)
			}
			if got.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", got.Code, tt.wantCode)
			}
		})
	}
}
