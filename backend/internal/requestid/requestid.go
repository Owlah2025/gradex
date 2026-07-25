// Package requestid creates and carries the correlation identifier for one
// HTTP attempt.
//
// Design §2.1 is specific about the trust model: Gradex creates a fresh opaque
// value for every attempt, it carries no Account, email, token, or business
// meaning, and a client-supplied value is untrusted. A client value is
// therefore never adopted as the identifier — at most it is sanitized and kept
// beside it as a parent correlation hint.
package requestid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

// HeaderName is both the response header Gradex sets and the request header a
// client may use to offer a parent correlation value.
const HeaderName = "X-Request-ID"

// MaxParentLength bounds an accepted client value. Anything longer is dropped
// rather than truncated: a truncated correlation ID correlates to nothing, so
// keeping a prefix would only manufacture a misleading value.
const MaxParentLength = 64

type contextKey int

const (
	trustedKey contextKey = iota
	parentKey
)

// New returns a fresh 128-bit opaque identifier. crypto/rand.Read is
// documented never to fail; it panics internally rather than returning a
// predictable value, which is the correct failure mode for a security-relevant
// identifier.
func New() string {
	var b [16]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// WithTrusted attaches the server-generated identifier.
func WithTrusted(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, trustedKey, id)
}

// FromContext returns the trusted identifier, or "" when none was attached.
func FromContext(ctx context.Context) string {
	id, _ := ctx.Value(trustedKey).(string)
	return id
}

// WithParent attaches the sanitized client-supplied correlation value.
func WithParent(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, parentKey, id)
}

// ParentFromContext returns the sanitized client value, or "" when the client
// supplied none or supplied one that was rejected.
func ParentFromContext(ctx context.Context) string {
	id, _ := ctx.Value(parentKey).(string)
	return id
}

// SanitizeParent accepts a client correlation value only when it is safe to
// place in a structured log field: printable ASCII drawn from an identifier
// alphabet, non-empty, and within MaxParentLength.
//
// Returning "" for anything else keeps log injection, control characters, and
// unbounded fields out of the logging path at the boundary rather than relying
// on every downstream sink to re-check.
func SanitizeParent(raw string) string {
	if raw == "" || len(raw) > MaxParentLength {
		return ""
	}
	for i := 0; i < len(raw); i++ {
		if !isIdentifierByte(raw[i]) {
			return ""
		}
	}
	return raw
}

func isIdentifierByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z',
		c >= 'A' && c <= 'Z',
		c >= '0' && c <= '9',
		c == '-', c == '_', c == '.':
		return true
	}
	return false
}
