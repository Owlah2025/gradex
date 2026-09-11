package httpapi

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/media"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

// writePlaybackProblem answers the three playback-concurrency outcomes
// specifically and reports whether it handled the error.
//
// It returns false for everything else, so every existing protected-media
// failure keeps the uniform refusal it has always had and this function can
// never become a general-purpose error leak on that surface.
//
// None of the three responses says anything about the other device: not its
// label, not its platform, not what it is watching, not when it started. The
// Student is told their account is busy and what to do about it, which is the
// entire useful content of the answer.
func writePlaybackProblem(c *gin.Context, err error) bool {
	switch {
	case errors.Is(err, media.ErrPlaybackConflict):
		writeProblem(c, problem.PlaybackActiveOnAnotherDevice())
	case errors.Is(err, media.ErrPlaybackLeaseLost):
		writeProblem(c, problem.PlaybackLeaseLost())
	case errors.Is(err, media.ErrPlaybackCoordinationUnavailable):
		// Fail closed, and say so as a temporary condition rather than a
		// denial: nothing about this Student was decided.
		writeProblem(c, problem.PlaybackCoordinationUnavailable())
	default:
		return false
	}
	return true
}

// sessionIDFrom reads the login family behind this request. It is recorded on
// the playback lease for incident review and is never an authorization input.
func sessionIDFrom(c *gin.Context) string {
	value, ok := c.Get("authenticated_session")
	if !ok {
		return ""
	}
	session, ok := value.(identity.Session)
	if !ok {
		return ""
	}
	return session.ID
}
