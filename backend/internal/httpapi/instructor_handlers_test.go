package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

type denyAnnouncementRateStore struct{}

func (denyAnnouncementRateStore) Decide(context.Context, []ratelimit.Entry) (bool, error) {
	return false, nil
}

func TestCourseAnnouncementRateLimitDeniesBeforeHandler(t *testing.T) {
	limiter, err := ratelimit.New(denyAnnouncementRateStore{}, bytes.Repeat([]byte{0x71}, 32), time.Second)
	if err != nil {
		t.Fatalf("constructing limiter: %v", err)
	}
	policy := ratelimit.CourseAnnouncementPolicy()
	handler := (&authoringHandlers{limiter: limiter, announcementPolicy: policy}).requireAnnouncementRateDecision

	router := gin.New()
	router.POST("/announcements", func(c *gin.Context) {
		c.Set(ctxUserIDKey, "71111111-1111-1111-1111-111111111111")
		handler(c)
	})
	request := httptest.NewRequest(http.MethodPost, "/announcements", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("rate-limited announcement status = %d, want 429", response.Code)
	}
}
