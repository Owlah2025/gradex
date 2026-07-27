package httpapi

import (
	"encoding/base64"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

const (
	sessionCredentialDigestContextKey = "sessionCredentialDigest"
	sessionCSRFDigestContextKey       = "sessionCSRFDigest"
)

func (f *SessionFoundation) requireSessionMutationSecurity() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if !f.security.hasTrustedBrowserOrigin(c.Request) {
			writeProblem(c, problem.OriginNotAllowed())
			return
		}
		credentialDigest, err := auth.SessionCredentialDigest(c.Request)
		if err != nil {
			writeProblem(c, problem.Unauthenticated())
			return
		}
		csrfToken := c.GetHeader(csrfHeaderName)
		decoded, err := base64.RawURLEncoding.DecodeString(csrfToken)
		if err != nil || len(decoded) != 32 ||
			base64.RawURLEncoding.EncodeToString(decoded) != csrfToken {
			writeProblem(c, problem.SessionCSRFFailed())
			return
		}
		c.Set(sessionCredentialDigestContextKey, credentialDigest)
		c.Set(sessionCSRFDigestContextKey, identity.DigestToken(csrfToken))
		c.Next()
	}
}
