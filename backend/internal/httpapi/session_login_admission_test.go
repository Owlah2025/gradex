package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestLoginAdmissionAppliesLoginSpecificRequestDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	foundation := &SessionFoundation{loginRequestTimeout: time.Minute}
	router.POST("/sessions", foundation.loginAdmission(), func(c *gin.Context) {
		deadline, ok := c.Request.Context().Deadline()
		if !ok {
			t.Error("login request has no deadline")
			return
		}
		remaining := time.Until(deadline)
		if remaining < 59*time.Second || remaining > time.Minute {
			t.Errorf("remaining login lifetime = %s, want approximately one minute", remaining)
		}
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodPost, "/sessions", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", response.Code)
	}
}
