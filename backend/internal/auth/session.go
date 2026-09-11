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

	// DeviceCookieName carries the opaque trusted-device credential.
	//
	// It lives beside the session cookie name because they are set by the same
	// server on the same origin and are read together by anything that has to
	// reason about "which browser is this login on" — but they are emphatically
	// not the same kind of value. The session cookie authenticates; this one
	// only tells the server which of the Student's browsers is asking, grants
	// no capability by itself, and is refused as authentication everywhere.
	DeviceCookieName = "__Host-gradex_device"

	sessionCredentialBytes = 32
)

// SessionAuthenticator resolves the opaque first-party cookie through the
// server-side session authority while preserving the existing user-ID seam
// consumed by authorization middleware.
type SessionAuthenticator struct {
	repository SessionResolver
}

type SessionResolver interface {
	Resolve(context.Context, identity.SessionResolutionRequest) (identity.SessionView, error)
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
	return a.repository.Resolve(request.Context(), identity.SessionResolutionRequest{
		CredentialDigest:       digest,
		DeviceCredentialDigest: DeviceCredentialDigest(request),
		UseKind:                useKind,
		RequestID:              requestid.FromContext(request.Context()),
	})
}

// DeviceCredentialDigest returns the digest of a canonical device credential,
// or the empty string when the browser did not present one. Missing or malformed
// device state narrows a Student session; it does not authenticate or reject an
// otherwise valid staff session.
func DeviceCredentialDigest(request *http.Request) string {
	if request == nil {
		return ""
	}
	var value string
	count := 0
	for _, cookie := range request.Cookies() {
		if cookie.Name == DeviceCookieName {
			count++
			value = cookie.Value
		}
	}
	if count != 1 || !identity.ValidOpaqueCredential(value) {
		return ""
	}
	return identity.DigestOpaqueCredential(value)
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
	// Device trust is published as its own context values rather than being
	// folded into identity.Session, which describes the login family and says
	// nothing about which browser holds it. Authorization middleware reads
	// these; nothing else does.
	c.Set(SessionDeviceTrustKey, view.Session.DeviceTrust)
	c.Set(SessionTrustedDeviceKey, view.Session.TrustedDeviceID)
	return view.Session.AccountID, nil
}

// Context keys for the device facts resolved with the session.
const (
	SessionDeviceTrustKey   = "sessionDeviceTrust"
	SessionTrustedDeviceKey = "sessionTrustedDeviceID"
)

// DeviceTrustFromContext reads the resolved trust state.
//
// A request that somehow reached authorization without one resolves to pending
// rather than trusted, so a missing value can never widen authority.
func DeviceTrustFromContext(c *gin.Context) identity.SessionDeviceTrust {
	value, ok := c.Get(SessionDeviceTrustKey)
	if !ok {
		return identity.DeviceTrustPending
	}
	trust, ok := value.(identity.SessionDeviceTrust)
	if !ok || !trust.Valid() {
		return identity.DeviceTrustPending
	}
	return trust
}

// TrustedDeviceFromContext reads the device this session is bound to, or the
// empty string when it is bound to none.
func TrustedDeviceFromContext(c *gin.Context) string {
	value, ok := c.Get(SessionTrustedDeviceKey)
	if !ok {
		return ""
	}
	deviceID, _ := value.(string)
	return deviceID
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
