// Package auth defines the seam video (and later, other) packages use to
// check who's calling and what they're allowed to do — without depending on
// a concrete auth implementation. Real JWT auth is a separate, later task;
// see fake.go for the dev-only implementation used until then.
package auth

import "github.com/gin-gonic/gin"

// Authenticator resolves the calling user's ID from an incoming request.
type Authenticator interface {
	UserFromRequest(c *gin.Context) (userID string, err error)
}
