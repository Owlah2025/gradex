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

	// Device reports what this browser must do before it holds ordinary
	// Student authority. It is present only when device policy applies, and it
	// deliberately carries no device identifier, no credential, and no code —
	// only the state, and the masked mailbox the Student is waiting on.
	Device *SessionDeviceResponse `json:"device_trust,omitempty"`
}

// SessionDeviceResponse is the browser-facing device-trust state.
type SessionDeviceResponse struct {
	State     string                  `json:"state"`
	Admission string                  `json:"admission,omitempty"`
	Challenge *SessionDeviceChallenge `json:"challenge,omitempty"`
}

// SessionDeviceChallenge is what a browser awaiting trust needs to render the
// code screen: which mailbox, and how long it has.
type SessionDeviceChallenge struct {
	ChallengeID       string `json:"challenge_id"`
	MaskedEmail       string `json:"masked_email"`
	ExpiresAt         string `json:"expires_at"`
	ResendAvailableAt string `json:"resend_available_at"`
}

// DeviceResponseFor renders the device half of a session response.
func DeviceResponseFor(state identity.SessionDeviceTrust, admission *identity.DeviceAdmissionResult) *SessionDeviceResponse {
	if state == "" || state == identity.DeviceTrustNotApplicable {
		return nil
	}
	response := &SessionDeviceResponse{State: string(state)}
	if admission == nil {
		return response
	}
	response.Admission = string(admission.Admission)
	if admission.Challenge != nil {
		response.Challenge = &SessionDeviceChallenge{
			ChallengeID:       admission.Challenge.ChallengeID,
			MaskedEmail:       admission.Challenge.MaskedEmail,
			ExpiresAt:         admission.Challenge.ExpiresAt.UTC().Format(time.RFC3339),
			ResendAvailableAt: admission.Challenge.ResendAvailableAt.UTC().Format(time.RFC3339),
		}
	}
	return response
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
	return WriteSessionResponseWithDevice(w, status, session, credential, csrfToken, nil)
}

// WriteSessionResponseWithDevice is the same boundary with the device-trust
// state attached. Kept as a second entry point so every existing caller keeps
// its exact behavior and only the two device-aware routes opt in.
func WriteSessionResponseWithDevice(
	w http.ResponseWriter,
	status int,
	session identity.AuthenticatedSession,
	credential *config.Secret,
	csrfToken config.Secret,
	admission *identity.DeviceAdmissionResult,
) error {
	body, err := json.Marshal(authenticatedSessionResponse{
		Device:                 DeviceResponseFor(session.DeviceTrust, admission),
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

// The trusted-device cookie.
//
// It is written here, beside the session cookie, because this file is the
// reviewed browser-secret egress boundary: the one place a config.Secret is
// unwrapped on its way to a client. The device credential is a browser secret
// like the session credential — unguessable, long-lived, and useless to anyone
// who does not hold it — so it belongs to the same boundary rather than to a
// second Expose() site in an outward-facing package.
//
// Cookie semantics, and why each one:
//
//   - __Host- prefix. The browser refuses the cookie unless it is Secure,
//     Path=/, and carries no Domain attribute. That last part is the valuable
//     one: no subdomain, including one an attacker manages to stand up, can set
//     or overwrite this Account's device credential.
//   - HttpOnly. The frontend never reads this value — every decision that
//     depends on it is made server-side from the digest — so exposing it to
//     script would buy nothing and would hand any XSS a copy of a credential
//     that survives sign-out. This is the deliberate difference from the CSRF
//     token, which the browser must read and therefore is not a cookie.
//   - SameSite=Strict, matching the session cookie. Every request that consults
//     the device credential is a same-origin fetch from the application itself.
//   - A long Max-Age. Without one this would be a browser-session cookie, and
//     every browser restart would look like a brand-new device and burn one of
//     the Student's two slots. 400 days is the ceiling Chrome enforces, so
//     asking for more would silently become less.
//
// What it is not: authentication. Presenting it proves nothing about who is
// asking and grants no capability. Every protected request is authorized by the
// session cookie exactly as before; this value only answers "which of your
// browsers is this".
const deviceCookieMaxAge = int(400 * 24 * time.Hour / time.Second)

// WriteDeviceCookie hands a freshly minted device credential to the browser.
// Callers invoke it only once the digest has been recorded against a device
// row, so a browser never holds a credential that names nothing.
func WriteDeviceCookie(w http.ResponseWriter, credential config.Secret) {
	http.SetCookie(w, &http.Cookie{
		Name: DeviceCookieName, Value: credential.Expose(), Path: "/",
		MaxAge: deviceCookieMaxAge,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
}

// ClearDeviceCookie expires the credential in this browser.
//
// Used when the Student removes the device they are currently using: leaving a
// credential behind that names a revoked record would make the next login look
// like a returning device to the browser and like an unknown one to the server.
func ClearDeviceCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: DeviceCookieName, Value: "", Path: "/", MaxAge: -1,
		Expires: time.Unix(1, 0).UTC(),
		Secure:  true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
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
