// Package problem implements the RFC 9457 Problem Details envelope frozen in
// design §2.3.
//
// It is the only error shape /api/v1 returns. The package deliberately does
// not depend on the web framework so that handlers, middleware, and recovery
// all write the identical representation.
package problem

import (
	"encoding/json"
	"net/http"
)

// ContentType is the media type every error response carries.
const ContentType = "application/problem+json"

// typeBase prefixes the Gradex-controlled absolute `type` URIs that are the
// canonical machine identifier for a problem.
const typeBase = "https://api.gradex.com/problems/"

// Location values for a validation violation that is not in the body.
const (
	LocationBody   = "body"
	LocationQuery  = "query"
	LocationPath   = "path"
	LocationHeader = "header"
	LocationCookie = "cookie"
)

// Violation is one field-level validation failure. It carries only a safe
// code, safe guidance, and the location of the offending input — never the
// value the client sent, which may be a credential.
type Violation struct {
	Code      string `json:"code"`
	Detail    string `json:"detail,omitempty"`
	Location  string `json:"location,omitempty"`
	Pointer   string `json:"pointer,omitempty"`
	Parameter string `json:"parameter,omitempty"`
}

// Problem is the response body. Field order follows §2.3's example.
//
// Nothing here may carry a stack trace, SQL or constraint text, storage object
// key, provider payload, internal address, or another Account's state. Callers
// construct Detail from fixed safe strings, not from a wrapped error.
type Problem struct {
	Type       string      `json:"type"`
	Title      string      `json:"title"`
	Status     int         `json:"status"`
	Detail     string      `json:"detail,omitempty"`
	Instance   string      `json:"instance,omitempty"`
	Code       string      `json:"code"`
	RequestID  string      `json:"request_id,omitempty"`
	Violations []Violation `json:"errors,omitempty"`
}

// New builds a problem whose `type` URI and uppercase `code` are generated
// from each other, so the two can never contradict as §2.3 requires.
//
// slug is the kebab-case identifier, e.g. "validation-failed", which yields
// type ".../problems/validation-failed" and code "VALIDATION_FAILED".
func New(status int, slug, title, detail string) Problem {
	return Problem{
		Type:   typeBase + slug,
		Title:  title,
		Status: status,
		Detail: detail,
		Code:   codeFromSlug(slug),
	}
}

// Internal is the generic unexpected-failure problem. Every unhandled fault
// converges here: diagnostics stay in protected logs, correlated only by the
// request ID (§2.3).
func Internal(requestID string) Problem {
	p := New(http.StatusInternalServerError, "internal-error",
		"Internal server error",
		"The request could not be completed. If the problem persists, contact support with the request ID.")
	p.RequestID = requestID
	return p
}

// WithRequestID returns a copy carrying the trusted correlation identifier.
func (p Problem) WithRequestID(id string) Problem {
	p.RequestID = id
	return p
}

// WithViolations returns a copy carrying field-level validation detail.
func (p Problem) WithViolations(v ...Violation) Problem {
	p.Violations = v
	return p
}

// Write emits the problem. It sets the status from Problem.Status so the HTTP
// status line and the body can never disagree — §2.3 makes the status line
// authoritative and requires the body to match it.
//
// Write reports whether it wrote anything. A caller that has already committed
// headers or bytes must not call it; see the recovery middleware, which checks
// before writing rather than appending an invalid second response.
func Write(w http.ResponseWriter, p Problem) error {
	body, err := json.Marshal(p)
	if err != nil {
		// Marshalling a Problem cannot fail for the fixed field set above, but
		// falling through to a bare status is still safer than emitting a
		// half-encoded body.
		w.WriteHeader(http.StatusInternalServerError)
		return err
	}
	w.Header().Set("Content-Type", ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(p.Status)
	_, err = w.Write(body)
	return err
}

// codeFromSlug converts "validation-failed" to "VALIDATION_FAILED".
func codeFromSlug(slug string) string {
	out := make([]byte, 0, len(slug))
	for i := 0; i < len(slug); i++ {
		c := slug[i]
		switch {
		case c == '-':
			out = append(out, '_')
		case c >= 'a' && c <= 'z':
			out = append(out, c-('a'-'A'))
		default:
			out = append(out, c)
		}
	}
	return string(out)
}
