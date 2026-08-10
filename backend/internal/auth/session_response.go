package auth

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/identity"
)

type authenticatedSessionResponse struct {
	Status      string        `json:"status"`
	Role        identity.Role `json:"role"`
	DisplayName string        `json:"display_name"`
	// PasswordChangeRequired reports that this principal is authenticated but
	// restricted: the policy refuses it every capability except changing its
	// password and ending its session.
	//
	// It is a derived boolean, never the credential state itself. The browser
	// needs exactly one fact — whether to send the visitor to the mandatory
	// change screen — and publishing the enum instead would put credential
	// internals on a public response for no additional client capability.
	// Without it a restricted Administrator authenticates successfully and then
	// collects 403s on every screen with nothing telling it why.
	PasswordChangeRequired bool   `json:"password_change_required"`
	CSRFToken              string `json:"csrf_token"`
	IdleExpiresAt          string `json:"idle_expires_at"`
	AbsoluteExpiresAt      string `json:"absolute_expires_at"`
}

// WriteSessionResponse is the reviewed browser-secret egress boundary. It
// unwraps the CSRF value only into the no-store JSON body and, when supplied,
// unwraps the opaque credential only into the hardened host cookie.
func WriteSessionResponse(
	w http.ResponseWriter,
	status int,
	session identity.AuthenticatedSession,
	credential *config.Secret,
	csrfToken config.Secret,
) error {
	body, err := json.Marshal(authenticatedSessionResponse{
		Status:                 "AUTHENTICATED",
		Role:                   session.Role,
		DisplayName:            session.DisplayName,
		PasswordChangeRequired: session.CredentialState == identity.CredentialChangeRequired,
		CSRFToken:              csrfToken.Expose(),
		IdleExpiresAt:          session.IdleExpiresAt.UTC().Format(time.RFC3339),
		AbsoluteExpiresAt:      session.AbsoluteExpiresAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if credential != nil {
		http.SetCookie(w, &http.Cookie{
			Name: SessionCookieName, Value: credential.Expose(), Path: "/",
			Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
		})
	}
	w.WriteHeader(status)
	_, err = w.Write(body)
	return err
}

// ClearSessionCookie expires the host cookie. Callers invoke this only after
// authoritative server-side revocation has committed.
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookieName, Value: "", Path: "/", MaxAge: -1,
		Expires: time.Unix(1, 0).UTC(),
		Secure:  true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
}
