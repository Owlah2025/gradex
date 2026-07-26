package httpapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

const (
	anonymousCookieName = "__Host-gradex_anon"
	csrfHeaderName      = "X-CSRF-Token"

	anonymousStateVersion byte = 1
	anonymousIDSize            = 32
	anonymousPayloadSize       = 1 + 8 + anonymousIDSize
	anonymousMACSize           = sha256.Size
)

const anonymousIDContextKey = "anonymousID"

type anonymousSecurity struct {
	publicOrigin string
	cookieKey    []byte
	csrfKey      []byte
	ttl          time.Duration
	now          func() time.Time
	random       io.Reader
}

func newAnonymousSecurity(
	publicOrigin string,
	cookieKey []byte,
	csrfKey []byte,
	ttl time.Duration,
) (*anonymousSecurity, error) {
	origin, err := config.CanonicalPublicOrigin(publicOrigin)
	if err != nil {
		return nil, err
	}
	if len(cookieKey) < 32 || len(csrfKey) < 32 {
		return nil, errors.New("anonymous security keys must contain at least 32 bytes")
	}
	if hmac.Equal(cookieKey, csrfKey) {
		return nil, errors.New("anonymous cookie and CSRF keys must be distinct")
	}
	if ttl < time.Second {
		return nil, errors.New("anonymous session TTL must be at least one second")
	}
	return &anonymousSecurity{
		publicOrigin: origin,
		cookieKey:    append([]byte(nil), cookieKey...),
		csrfKey:      append([]byte(nil), csrfKey...),
		ttl:          ttl,
		now:          time.Now,
		random:       rand.Reader,
	}, nil
}

func (s *anonymousSecurity) bootstrapHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		payload, ok := s.validCookiePayload(c.Request)
		if !ok {
			var err error
			payload, err = s.newPayload()
			if err != nil {
				writeProblem(c, problem.Internal(""))
				return
			}
			http.SetCookie(c.Writer, &http.Cookie{
				Name:     anonymousCookieName,
				Value:    s.encodeCookie(payload),
				Path:     "/",
				MaxAge:   int(s.ttl.Seconds()),
				Secure:   true,
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
		}

		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{"csrf_token": s.csrfToken(payload)})
	}
}

func (s *anonymousSecurity) newPayload() ([]byte, error) {
	payload := make([]byte, anonymousPayloadSize)
	payload[0] = anonymousStateVersion
	expiresAt := s.now().Add(s.ttl).Unix()
	binary.BigEndian.PutUint64(payload[1:9], uint64(expiresAt))
	if _, err := io.ReadFull(s.random, payload[9:]); err != nil {
		return nil, err
	}
	return payload, nil
}

func (s *anonymousSecurity) validCookiePayload(request *http.Request) ([]byte, bool) {
	cookie, err := request.Cookie(anonymousCookieName)
	if err != nil {
		return nil, false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(payload) != anonymousPayloadSize ||
		payload[0] != anonymousStateVersion {
		return nil, false
	}
	mac, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(mac) != anonymousMACSize {
		return nil, false
	}
	expectedMAC := s.cookieMAC(payload)
	if subtle.ConstantTimeCompare(mac, expectedMAC) != 1 {
		return nil, false
	}
	expiresAt := int64(binary.BigEndian.Uint64(payload[1:9]))
	if !s.now().Before(time.Unix(expiresAt, 0)) {
		return nil, false
	}
	return payload, true
}

func (s *anonymousSecurity) encodeCookie(payload []byte) string {
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(s.cookieMAC(payload))
}

func (s *anonymousSecurity) cookieMAC(payload []byte) []byte {
	mac := hmac.New(sha256.New, s.cookieKey)
	_, _ = mac.Write([]byte("gradex-anonymous-cookie-v1\x00"))
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func (s *anonymousSecurity) csrfToken(payload []byte) string {
	mac := hmac.New(sha256.New, s.csrfKey)
	_, _ = mac.Write([]byte("gradex-anonymous-csrf-v1\x00"))
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
