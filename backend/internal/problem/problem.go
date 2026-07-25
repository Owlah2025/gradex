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

// The constructors below are the closed set of problem classes the API may
// return. Handlers choose among them; they never build a Problem from an
// internal error's text, because that text carries bucket and object keys,
// filesystem paths, queue names, database constraint names, and raw provider
// messages.

// Malformed reports a structurally invalid request — unparseable JSON, a wrong
// JSON type, a body that is not what the media type promised. It deliberately
// carries no parser detail: a decoder message quotes the offending input.
func Malformed() Problem {
	return New(http.StatusBadRequest, "malformed-request",
		"Malformed request",
		"The request body could not be parsed.")
}

// ValidationFailed reports a syntactically valid request whose field values are
// semantically unacceptable. Attach Violations to say which.
func ValidationFailed() Problem {
	return New(http.StatusUnprocessableEntity, "validation-failed",
		"Request validation failed",
		"One or more fields are invalid.")
}

// Unauthenticated reports missing or unusable authentication. Its companion
// WWW-Authenticate challenge is added by the writer.
func Unauthenticated() Problem {
	return New(http.StatusUnauthorized, "authentication-required",
		"Authentication required",
		"This resource requires an authenticated session.")
}

// NotAuthorized reports an authenticated principal without authority here.
//
// The detail is deliberately uniform: §6.1 keeps typed policy reasons —
// NOT_OWNER, ACCESS_NOT_COVERED, RESOURCE_SUSPENDED — for tests and security
// monitoring, and out of public errors, so the response cannot be used to
// probe ownership or coverage.
func NotAuthorized() Problem {
	return New(http.StatusForbidden, "not-authorized",
		"Not authorized",
		"This resource is not available to the current session.")
}

// NotFound reports an absent or concealed resource. §6.1 permits the same 404
// for absence and invisibility, so this body never distinguishes the two.
func NotFound() Problem {
	return New(http.StatusNotFound, "not-found",
		"Resource not found",
		"The requested resource does not exist.")
}

// MethodNotAllowed reports a known path carrying an unsupported method.
func MethodNotAllowed() Problem {
	return New(http.StatusMethodNotAllowed, "method-not-allowed",
		"Method not allowed",
		"The requested method is not supported for this resource.")
}

// StateConflict reports a well-formed, authorized command that conflicts with
// the resource's current state — a concurrent modification, or a resource that
// already exists.
func StateConflict() Problem {
	return New(http.StatusConflict, "state-conflict",
		"Conflicting resource state",
		"The resource changed while this request was in flight. Reload and try again.")
}

// UnsupportedStateTransition reports a command that is not legal from the
// resource's current state, as distinct from a race. The current state itself
// is not disclosed: internal lifecycle values are not part of the public
// contract.
func UnsupportedStateTransition() Problem {
	return New(http.StatusConflict, "unsupported-state-transition",
		"Unsupported state transition",
		"This operation is not available for the resource in its current state.")
}

// DependencyUnavailable reports a temporary failure of storage, the queue, or
// a provider. It is separated from Internal so a client can distinguish
// "retry later" from "this will not succeed".
func DependencyUnavailable() Problem {
	return New(http.StatusServiceUnavailable, "dependency-unavailable",
		"Service temporarily unavailable",
		"A dependency is temporarily unavailable. Try again shortly.")
}

// Internal is the generic unexpected-failure problem. Every unhandled fault
// converges here: diagnostics stay in protected logs, correlated only by the
// request ID (§2.3).
func Internal(requestID string) Problem {
	return New(http.StatusInternalServerError, "internal-error",
		"Internal server error",
		"The request could not be completed. If the problem persists, contact support with the request ID.").
		WithRequestID(requestID)
}

// WithRequestID returns a copy carrying the trusted correlation identifier,
// and derives the opaque `instance` URN from it.
//
// `instance` identifies this occurrence and must stay opaque: §2.3 forbids a
// problem from revealing internal structure, so it is not a resource path.
// Deriving it from the request ID keeps a support report, the response header,
// and the protected log correlated by a single value.
func (p Problem) WithRequestID(id string) Problem {
	p.RequestID = id
	if id != "" {
		p.Instance = "urn:gradex:problem:" + id
	}
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

	// §2.3 requires the fixed first-party challenge on every browser-facing
	// 401. GradexSession is an opaque-session challenge, not Basic or Bearer,
	// so it does not prompt the browser for native credentials. The challenge
	// never varies by hidden Account state.
	if p.Status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `GradexSession realm="gradex-web"`)
	}

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
