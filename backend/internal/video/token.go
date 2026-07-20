package video

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// signPlaybackToken produces a short-lived HMAC token scoped to one video ID,
// used by the manifest-proxy endpoint (see playback.go's ServeManifest) since
// the HLS player fetching master/child playlists won't carry the student's
// normal auth header — this is the capability token that stands in for it,
// analogous to a real CDN's signed-URL/cookie mechanism.
func signPlaybackToken(secret, videoID string, expiresAt time.Time) string {
	exp := strconv.FormatInt(expiresAt.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(videoID + "." + exp))
	sig := hex.EncodeToString(mac.Sum(nil))
	return exp + "." + sig
}

func verifyPlaybackToken(secret, videoID, token string) error {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return fmt.Errorf("%w: malformed token", ErrConflict)
	}
	exp, sig := parts[0], parts[1]

	expUnix, err := strconv.ParseInt(exp, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: malformed token expiry", ErrConflict)
	}
	if time.Now().Unix() > expUnix {
		return fmt.Errorf("%w: token expired", ErrConflict)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(videoID + "." + exp))
	want := hex.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(sig), []byte(want)) != 1 {
		return fmt.Errorf("%w: invalid token signature", ErrConflict)
	}
	return nil
}
