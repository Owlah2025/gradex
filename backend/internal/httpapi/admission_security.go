package httpapi

import (
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/problem"
)

func (s *anonymousSecurity) requireAdmission() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !s.hasTrustedBrowserOrigin(c.Request) {
			c.Header("Cache-Control", "no-store")
			writeProblem(c, problem.CSRFValidationFailed())
			return
		}
		payload, ok := s.validCookiePayload(c.Request)
		if !ok || !secureTokenEqual(c.GetHeader(csrfHeaderName), s.csrfToken(payload)) {
			c.Header("Cache-Control", "no-store")
			writeProblem(c, problem.CSRFValidationFailed())
			return
		}
		c.Set(anonymousIDContextKey, base64.RawURLEncoding.EncodeToString(payload[9:]))
		c.Next()
	}
}

func (s *anonymousSecurity) hasTrustedBrowserOrigin(request *http.Request) bool {
	if origin := request.Header.Get("Origin"); origin != "" {
		return origin == s.publicOrigin
	}
	referer := request.Header.Get("Referer")
	parsed, err := url.Parse(referer)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return false
	}
	return parsed.Scheme+"://"+parsed.Host == s.publicOrigin
}

func secureTokenEqual(got, want string) bool {
	gotBytes, err := base64.RawURLEncoding.DecodeString(got)
	if err != nil {
		return false
	}
	wantBytes, err := base64.RawURLEncoding.DecodeString(want)
	return err == nil && subtle.ConstantTimeCompare(gotBytes, wantBytes) == 1
}
