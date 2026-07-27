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
	return New(http.StatusBadRequest, "malformed-json",
		"Malformed JSON",
		"The JSON request body could not be parsed.")
}

// ContentTooLarge reports that the request body crossed the endpoint's fixed
// byte limit. The configured limit is deliberately not reflected to callers.
func ContentTooLarge() Problem {
	return New(http.StatusRequestEntityTooLarge, "content-too-large",
		"Request content too large",
		"The request body is too large.")
}

// UnsupportedMediaType reports a request body that is not UTF-8 JSON.
func UnsupportedMediaType() Problem {
	return New(http.StatusUnsupportedMediaType, "unsupported-media-type",
		"Unsupported media type",
		"The request body must be UTF-8 JSON.")
}

func NotAcceptable() Problem {
	return New(http.StatusNotAcceptable, "not-acceptable",
		"Unsupported response representation",
		"The requested response language or representation is not available.")
}

// ValidationFailed reports a syntactically valid request whose field values are
// semantically unacceptable. Attach Violations to say which.
func ValidationFailed() Problem {
	return New(http.StatusUnprocessableEntity, "validation-failed",
		"Request validation failed",
		"One or more fields are invalid.")
}

// TokenInvalid deliberately collapses every unusable action-secret state.
// Unknown, malformed, wrong-purpose, expired, consumed, and superseded values
// must not become an oracle for Account or token lifecycle state (BR-008).
func TokenInvalid() Problem {
	return New(http.StatusBadRequest, "token-invalid",
		"Link unavailable",
		"This verification link cannot be used.")
}

// CSRFValidationFailed covers both an unusable anonymous synchronizer token
// and an untrusted Origin/Referer. The response does not say which check
// failed, so it cannot help a caller tune a cross-site request.
func CSRFValidationFailed() Problem {
	return New(http.StatusForbidden, "csrf-validation-failed",
		"Request validation failed",
		"The request could not be accepted from this browser context.")
}

// RateLimited is returned only after a configured policy was evaluated and
// found exhausted. Infrastructure failure uses RateLimitingUnavailable
// instead; calling both conditions "limited" would fabricate a quota decision.
func RateLimited() Problem {
	return New(http.StatusTooManyRequests, "rate-limited",
		"Too many requests",
		"Please wait before trying again.")
}

// RateLimitingUnavailable means neither Redis nor the policy's bounded local
// fallback could make a safe decision.
func RateLimitingUnavailable() Problem {
	return New(http.StatusServiceUnavailable, "rate-limiting-unavailable",
		"Service temporarily unavailable",
		"This request cannot be accepted right now. Try again shortly.")
}

// RegistrationUnavailable is intentionally generic across missing current
// policy and failed required credential screening. Those are distinct
// protected diagnostics, not public response classes.
func RegistrationUnavailable() Problem {
	return New(http.StatusServiceUnavailable, "registration-unavailable",
		"Registration temporarily unavailable",
		"Registration cannot be completed right now. Try again shortly.")
}

// TransactionalDeliveryUnavailable means the durable source/outbox admission
// boundary is unsafe. It says nothing about Account existence, a provider, or
// whether any message could have been delivered.
func TransactionalDeliveryUnavailable() Problem {
	return New(http.StatusServiceUnavailable, "transactional-delivery-unavailable",
		"Request temporarily unavailable",
		"This request cannot be accepted right now. Try again shortly.")
}

// Unauthenticated reports missing or unusable authentication. Its companion
// WWW-Authenticate challenge is added by the writer.
func Unauthenticated() Problem {
	return New(http.StatusUnauthorized, "authentication-required",
		"Authentication required",
		"This resource requires an authenticated session.")
}

// AuthenticationFailed deliberately collapses every syntactically admissible
// login denial. Unknown email, wrong password, unverified Account, inactive
// Account, and a candidate that changed during admission all share this body.
func AuthenticationFailed() Problem {
	return New(http.StatusUnauthorized, "authentication-failed",
		"Authentication failed",
		"The email or password is incorrect.")
}

// SessionReplaced reports the one tolerated immediate, non-sensitive
// presentation of a superseded session value. It does not disclose generation
// numbers or whether a replacement remains usable.
func SessionReplaced() Problem {
	return New(http.StatusUnauthorized, "session-replaced",
		"Session replaced",
		"This session has been replaced. Sign in again to continue.")
}

// SessionReuseDetected reports confirmed replay after the family has been
// revoked. The same response is used regardless of which generation or
// request class supplied the evidence.
func SessionReuseDetected() Problem {
	return New(http.StatusUnauthorized, "session-reuse-detected",
		"Session unavailable",
		"This session can no longer be used. Sign in again to continue.")
}

// SessionCSRFFailed reports a missing or mismatched authenticated-session CSRF
// value without reflecting the value or explaining how the check failed.
func SessionCSRFFailed() Problem {
	return New(http.StatusForbidden, "csrf-failed",
		"Request validation failed",
		"The request could not be validated for this session.")
}

// OriginNotAllowed is distinct from CSRF failure for clients while still
// withholding the configured trusted origin.
func OriginNotAllowed() Problem {
	return New(http.StatusForbidden, "origin-not-allowed",
		"Browser origin not allowed",
		"This request is not allowed from the current browser origin.")
}

// AuthenticationUnavailable is the fail-closed result when session admission
// or resolution cannot make an authoritative decision.
func AuthenticationUnavailable() Problem {
	return New(http.StatusServiceUnavailable, "authentication-unavailable",
		"Authentication temporarily unavailable",
		"Authentication cannot be completed right now. Try again shortly.")
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
