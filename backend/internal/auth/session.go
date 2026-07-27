package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/requestid"
)

const (
	// SessionCookieName is host-prefixed so browsers enforce Secure, Path=/,
	// and the absence of a Domain attribute when the server sets it.
	SessionCookieName = "__Host-gradex_session"

	sessionCredentialBytes = 32
)

// SessionAuthenticator resolves the opaque first-party cookie through the
// server-side session authority while preserving the existing user-ID seam
// consumed by authorization middleware.
type SessionAuthenticator struct {
	repository SessionResolver
}

type SessionResolver interface {
	Resolve(
		context.Context,
		string,
		identity.CredentialUseKind,
		string,
	) (identity.SessionView, error)
}

// NewSessionAuthenticator creates the production cookie authenticator.
func NewSessionAuthenticator(repository SessionResolver) (*SessionAuthenticator, error) {
	if repository == nil {
		return nil, errors.New("session resolver is required")
	}
	return &SessionAuthenticator{repository: repository}, nil
}

// Authenticate returns the typed non-secret session facts for a request.
func (a *SessionAuthenticator) Authenticate(
	request *http.Request,
	useKind identity.CredentialUseKind,
) (identity.SessionView, error) {
	if a == nil || a.repository == nil {
		return identity.SessionView{}, identity.ErrAuthenticationRequired
	}
	digest, err := SessionCredentialDigest(request)
	if err != nil {
		return identity.SessionView{}, err
	}
	return a.repository.Resolve(
		request.Context(),
		digest,
		useKind,
		requestid.FromContext(request.Context()),
	)
}

func (a *SessionAuthenticator) UserFromRequest(c *gin.Context) (string, error) {
	view, err := a.Authenticate(c.Request, identity.UseReadOnly)
	if err != nil {
		return "", err
	}
	c.Set("authenticated_session", identity.Session{
		ID:                view.Session.SessionID,
		AccountID:         view.Session.AccountID,
		State:             identity.SessionActive,
		CurrentGeneration: view.Session.Generation,
		AuthenticatedAt:   view.Session.AuthenticatedAt,
		ReauthenticatedAt: view.Session.ReauthenticatedAt,
		IdleExpiresAt:     view.Session.IdleExpiresAt,
		AbsoluteExpiresAt: view.Session.AbsoluteExpiresAt,
	})
	return view.Session.AccountID, nil
}

// SessionCredentialDigest validates the cookie's canonical opaque format and
// returns only its one-way digest. Callers never retain or log the bearer.
func SessionCredentialDigest(request *http.Request) (string, error) {
	if request == nil {
		return "", identity.ErrAuthenticationRequired
	}
	var value string
	count := 0
	for _, cookie := range request.Cookies() {
		if cookie.Name == SessionCookieName {
			count++
			value = cookie.Value
		}
	}
	if count != 1 {
		return "", identity.ErrAuthenticationRequired
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != sessionCredentialBytes ||
		base64.RawURLEncoding.EncodeToString(decoded) != value {
		return "", identity.ErrAuthenticationRequired
	}
	return identity.DigestToken(value), nil
}
