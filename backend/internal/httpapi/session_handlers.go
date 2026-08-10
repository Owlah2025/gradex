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

// passwordChangeBodyLimit bounds the one route that carries two password
// plaintexts. Passwords have an enforced maximum length, so nothing legitimate
// approaches this and a larger body is refused before it is parsed.
const passwordChangeBodyLimit int64 = 2048

type sessionLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// passwordChangeRequest is the mandatory and voluntary change body.
//
// Both fields are required at the boundary. The confirmation field the form
// shows is deliberately not accepted here: it is a typing check the browser
// owns, and sending a third copy of the new password would widen the plaintext
// surface for no server-side decision.
type passwordChangeRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required"`
}

type sessionHandlers struct {
	repository    sessionCommands
	authenticator *auth.SessionAuthenticator
	compromised   identity.CompromisedRangeSource
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

// changePassword replaces the caller's own password and returns the rotated
// session.
//
// This is the one authenticated route a CHANGE_REQUIRED principal may reach.
// That is not a bypass: the policy already grants CapPasswordChange to a
// restricted principal and refuses it everything else, so mounting the route
// behind that capability is what makes the restricted state escapable instead
// of terminal. Without it the bootstrap Administrator authenticates and then
// has no path — browser or API — to the state the bootstrap command tells it to
// reach.
//
// The request still carries every ordinary protection: same-origin admission, a
// valid session cookie, the session CSRF token, a bounded body, and the rate
// limiter. The password plaintexts travel no further than the domain's
// credential boundary, and no error path echoes them.
func (h *sessionHandlers) changePassword(c *gin.Context) {
	request := c.MustGet(strictJSONBodyContextKey).(*passwordChangeRequest)

	grant, err := h.repository.ChangePassword(c.Request.Context(), identity.PasswordChangeCommand{
		CredentialDigest: c.GetString(sessionCredentialDigestContextKey),
		CSRFDigest:       c.GetString(sessionCSRFDigestContextKey),
		CurrentPassword:  config.NewSecret(request.CurrentPassword),
		NewPassword:      config.NewSecret(request.NewPassword),
		Compromised:      h.compromised,
		RequestID:        requestid.FromContext(c.Request.Context()),
	})
	if err != nil {
		writePasswordChangeError(c, err)
		return
	}

	// The replacement cookie and CSRF token are written only here, after the
	// domain transaction committed. A rolled-back change can never leave the
	// browser holding credentials for a generation the database does not have.
	_ = auth.WriteSessionResponse(
		c.Writer, http.StatusOK, grant.Session, &grant.Credential, grant.CSRFToken,
	)
}

// writePasswordChangeError maps the domain's refusals onto the problem set.
//
// Unlike login, this caller has already proven who it is, so the two failures
// it can actually fix are reported apart: a wrong current password and a new
// password the policy or the compromised set refuses. Neither response contains
// the rejected value, the stored hash, or which rule matched.
func writePasswordChangeError(c *gin.Context, err error) {
	c.Header("Cache-Control", "no-store")
	switch {
	case errors.Is(err, identity.ErrCurrentPasswordIncorrect),
		errors.Is(err, identity.ErrCurrentPasswordRequired):
		writeProblem(c, problem.AuthenticationFailed())
	case errors.Is(err, identity.ErrPasswordPolicy):
		writeProblem(c, problem.ValidationFailed())
	case errors.Is(err, identity.ErrRecentAuthRequired):
		// The session is genuine but authenticated too long ago for a
		// credential change. Signing in again is the recovery.
		writeProblem(c, problem.NotAuthorized())
	case errors.Is(err, identity.ErrSessionCSRFFailed):
		writeProblem(c, problem.SessionCSRFFailed())
	case errors.Is(err, identity.ErrSessionReplaced):
		writeProblem(c, problem.SessionReplaced())
	case errors.Is(err, identity.ErrSessionReuseDetected):
		writeProblem(c, problem.SessionReuseDetected())
	case errors.Is(err, identity.ErrSessionNotUsable),
		errors.Is(err, identity.ErrStaleGeneration),
		errors.Is(err, identity.ErrAuthenticationRequired),
		errors.Is(err, identity.ErrPrincipalNotFound):
		writeProblem(c, problem.Unauthenticated())
	default:
		// Anything else is an operational fault. It carries a request ID and no
		// detail, because the wrapped text can name database objects.
		writeProblem(c, problem.Internal(requestid.FromContext(c.Request.Context())))
	}
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
