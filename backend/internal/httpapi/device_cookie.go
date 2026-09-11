package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/identity"
)

// readDeviceCredential returns the digest of the credential this browser
// presented, or the empty string when it presented none or presented something
// that is not shaped like one.
//
// Shape is checked before the value can reach a database lookup, so a hostile
// client cannot turn the credential column into a search interface with
// arbitrary strings.
func readDeviceCredential(request *http.Request) string {
	return auth.DeviceCredentialDigest(request)
}

// pendingDeviceCredential resolves the browser's device credential, minting one
// when there is nothing usable to resolve.
//
// The minted credential is *not* written to the response here. That matters on
// the login route: an anonymous prober whose password is wrong must receive a
// response byte-identical to every other hidden failure, and a Set-Cookie
// carrying a fresh credential would both break that and hand out a device
// credential for an Account nobody proved they hold.
//
// So the caller commits the cookie only once the outcome is one that recorded
// the digest against a device row — a browser that never received the matching
// plaintext would leave a row nothing can ever match again. The work done here
// is identical either way, so the choice is not observable in the timing.
type pendingDeviceCredential struct {
	digest string
	minted *config.Secret
}

func pendingDeviceCredentialFor(c *gin.Context) (pendingDeviceCredential, error) {
	if digest := readDeviceCredential(c.Request); digest != "" {
		return pendingDeviceCredential{digest: digest}, nil
	}
	credential, digest, err := identity.NewOpaqueCredential()
	if err != nil {
		return pendingDeviceCredential{}, err
	}
	return pendingDeviceCredential{digest: digest, minted: &credential}, nil
}

// commit writes a freshly minted credential to the browser. It is a no-op when
// the browser already presented one, which is the common case.
func (p pendingDeviceCredential) commit(c *gin.Context) {
	if p.minted == nil {
		return
	}
	auth.WriteDeviceCookie(c.Writer, *p.minted)
}

// deviceContextFrom assembles the non-identity facts a device record stores for
// display and forensics.
//
// The User-Agent names the device for the Student and the address is kept for
// security review. Neither is ever compared to decide which record a browser
// matches, so a Student who updates their browser or changes network keeps the
// same device.
func deviceContextFrom(c *gin.Context, digest string) identity.DeviceContext {
	return identity.DeviceContext{
		CredentialDigest: digest,
		UserAgent:        c.GetHeader("User-Agent"),
		SourceAddress:    c.ClientIP(),
	}
}
