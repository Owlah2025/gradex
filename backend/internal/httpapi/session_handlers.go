package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/problem"
	"github.com/Owlah2025/gradex/backend/internal/requestid"
)

const sessionLoginBodyLimit int64 = 1024

type sessionLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type sessionHandlers struct {
	repository    sessionCommands
	authenticator *auth.SessionAuthenticator
}

func (h *sessionHandlers) login(c *gin.Context) {
	request := c.MustGet(strictJSONBodyContextKey).(*sessionLoginRequest)
	grant, err := h.repository.Login(c.Request.Context(), identity.LoginRequest{
		Email: request.Email, Password: config.NewSecret(request.Password),
		RequestID: requestid.FromContext(c.Request.Context()),
	})
	if err != nil {
		writeSessionError(c, err)
		return
	}

	clearAnonymousCookie(c)
	_ = auth.WriteSessionResponse(
		c.Writer, http.StatusCreated, grant.Session, &grant.Credential, grant.CSRFToken,
	)
}

func (h *sessionHandlers) resolve(c *gin.Context) {
	view, err := h.authenticator.Authenticate(c.Request, identity.UseReadOnly)
	if err != nil {
		writeSessionError(c, err)
		return
	}
	_ = auth.WriteSessionResponse(
		c.Writer, http.StatusOK, view.Session, nil, view.CSRFToken,
	)
}

func (h *sessionHandlers) renew(c *gin.Context) {
	grant, err := h.repository.Renew(c.Request.Context(), sessionMutationFrom(c))
	if err != nil {
		writeSessionError(c, err)
		return
	}
	_ = auth.WriteSessionResponse(
		c.Writer, http.StatusOK, grant.Session, &grant.Credential, grant.CSRFToken,
	)
}

func (h *sessionHandlers) logout(c *gin.Context) {
	if err := h.repository.Logout(c.Request.Context(), sessionMutationFrom(c)); err != nil {
		writeSessionError(c, err)
		return
	}
	auth.ClearSessionCookie(c.Writer)
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusNoContent)
}

func sessionMutationFrom(c *gin.Context) identity.SessionMutation {
	return identity.SessionMutation{
		CredentialDigest: c.GetString(sessionCredentialDigestContextKey),
		CSRFDigest:       c.GetString(sessionCSRFDigestContextKey),
		RequestID:        requestid.FromContext(c.Request.Context()),
	}
}

func writeSessionError(c *gin.Context, err error) {
	c.Header("Cache-Control", "no-store")
	switch {
	case errors.Is(err, identity.ErrAuthenticationFailed):
		writeProblem(c, problem.AuthenticationFailed())
	case errors.Is(err, identity.ErrSessionReplaced):
		writeProblem(c, problem.SessionReplaced())
	case errors.Is(err, identity.ErrSessionReuseDetected):
		writeProblem(c, problem.SessionReuseDetected())
	case errors.Is(err, identity.ErrSessionCSRFFailed):
		writeProblem(c, problem.SessionCSRFFailed())
	case errors.Is(err, identity.ErrAuthenticationRequired):
		writeProblem(c, problem.Unauthenticated())
	default:
		writeProblem(c, problem.AuthenticationUnavailable())
	}
}

func clearAnonymousCookie(c *gin.Context) {
	expireHostCookie(c, anonymousCookieName)
}

func expireHostCookie(c *gin.Context, name string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: name, Value: "", Path: "/", MaxAge: -1,
		Expires: time.Unix(1, 0).UTC(),
		Secure:  true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
}
