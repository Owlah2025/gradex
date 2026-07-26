// Package logging emits Gradex's structured operational telemetry.
//
// The field set is a closed allowlist, not a convenience wrapper over slog.
// Design §10.2 forbids telemetry from carrying request or response bodies, raw
// queries, cookies, Authorization or CSRF headers, idempotency values, signed
// capabilities, personal data, provider payloads, or raw error text — so this
// package exposes typed events with fixed fields instead of a general
// key/value API that would let any of those in by accident.
//
// Telemetry is never authority. Losing a log line cannot change domain
// correctness, so every failure path here degrades quietly rather than
// disturbing the business request that produced it.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"unicode/utf8"
)

// maxFieldLength bounds any single string field. Oversized values are
// truncated rather than dropped, so a long route or error code still
// correlates while the output stays bounded.
const maxFieldLength = 256

// maxStackLength bounds a recorded stack trace. Stacks are code locations, not
// request data, but they are still unbounded input to the log sink.
const maxStackLength = 4096

// Logger writes structured events. The zero value is not usable; call New.
type Logger struct {
	slog *slog.Logger
}

// New builds a logger writing JSON to w, stamping every record with the
// service and environment that §10.2 lists among the safe request attributes.
func New(w io.Writer, service, environment string, level slog.Level) *Logger {
	handler := slog.NewJSONHandler(&safeWriter{dst: w}, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			// "time" is slog's builtin key; the agreed field name is
			// "timestamp".
			if a.Key == slog.TimeKey {
				a.Key = "timestamp"
			}
			// Defence in depth. Every field below is already sanitized at
			// construction, so this only catches a future caller that adds one
			// without going through the typed events.
			if a.Value.Kind() == slog.KindString {
				a.Value = slog.StringValue(Sanitize(a.Value.String()))
			}
			return a
		},
	})
	return &Logger{
		slog: slog.New(handler).With(
			slog.String("service", Sanitize(service)),
			slog.String("environment", Sanitize(environment)),
		),
	}
}

// LevelFromString maps the validated LOG_LEVEL setting onto a slog level.
// Config validation already rejects anything outside this set, so the default
// arm is unreachable through normal startup; it stays conservative rather than
// permissive in case a future caller bypasses validation.
func LevelFromString(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// RequestEvent is one completed HTTP attempt.
//
// RouteTemplate is the matched route pattern such as
// /api/v1/lessons/:lessonID/video/publish — never the literal path, which
// carries identifiers and can carry tokens.
type RequestEvent struct {
	RequestID             string
	ParentRequestID       string
	Method                string
	RouteTemplate         string
	Status                int
	DurationMillis        int64
	ResponseSize          int
	SafeErrorCode         string
	LimiterOutcome        string
	AdmissionFailureStage AdmissionFailureStage
	// Routine marks a high-frequency endpoint whose successful attempts are
	// not worth an info line. Failures ignore it.
	Routine bool
}

// AdmissionFailureStage is a closed diagnostic classification for public
// Identity admission. It identifies only which fail-closed boundary stopped a
// request; it never carries the underlying error, Account state, identifier,
// credential, or request data.
type AdmissionFailureStage string

const (
	AdmissionFailureStructure       AdmissionFailureStage = "STRUCTURE"
	AdmissionFailureBrowserSecurity AdmissionFailureStage = "BROWSER_SECURITY"
	AdmissionFailureRateDecision    AdmissionFailureStage = "RATE_DECISION"
	AdmissionFailureDomain          AdmissionFailureStage = "DOMAIN"
)

func (s AdmissionFailureStage) valid() bool {
	switch s {
	case AdmissionFailureStructure,
		AdmissionFailureBrowserSecurity,
		AdmissionFailureRateDecision,
		AdmissionFailureDomain:
		return true
	default:
		return false
	}
}

// RequestCompleted logs one finished request at a level chosen from its
// status: server faults are errors, client faults are warnings, the rest is
// informational.
func (l *Logger) RequestCompleted(ev RequestEvent) {
	attrs := []any{
		slog.String("request_id", Sanitize(ev.RequestID)),
		slog.String("method", Sanitize(ev.Method)),
		slog.String("route_template", Sanitize(ev.RouteTemplate)),
		slog.Int("status", ev.Status),
		slog.Int64("duration_ms", ev.DurationMillis),
		slog.Int("response_size", ev.ResponseSize),
	}
	if ev.ParentRequestID != "" {
		attrs = append(attrs, slog.String("parent_request_id", Sanitize(ev.ParentRequestID)))
	}
	if ev.SafeErrorCode != "" {
		attrs = append(attrs, slog.String("safe_error_code", Sanitize(ev.SafeErrorCode)))
	}
	if ev.LimiterOutcome != "" {
		attrs = append(attrs, slog.String("limiter_outcome", Sanitize(ev.LimiterOutcome)))
	}
	if ev.AdmissionFailureStage.valid() {
		attrs = append(attrs,
			slog.String("admission_failure_stage", string(ev.AdmissionFailureStage)),
		)
	}

	switch {
	case ev.Status >= 500:
		l.slog.Error("http_request", attrs...)
	case ev.Status >= 400:
		l.slog.Warn("http_request", attrs...)
	case ev.Routine:
		// Probes run every few seconds for the life of the process; logging
		// each success at info level would bury real traffic.
		l.slog.Debug("http_request", attrs...)
	default:
		l.slog.Info("http_request", attrs...)
	}
}

// DependencyEvent is one failed readiness check.
//
// The error is recorded here and nowhere else: readiness responses are
// unauthenticated, so the DSN, host, database name, SQL text, or migration
// state a check failure carries belongs only in the protected log.
type DependencyEvent struct {
	RequestID string
	Check     string
	Err       error
}

func (l *Logger) DependencyUnready(ev DependencyEvent) {
	attrs := []any{
		slog.String("request_id", Sanitize(ev.RequestID)),
		slog.String("check", Sanitize(ev.Check)),
	}
	if ev.Err != nil {
		attrs = append(attrs, slog.String("error", Sanitize(ev.Err.Error())))
	}
	l.slog.Warn("dependency_unready", attrs...)
}

// PanicEvent is a recovered panic.
//
// It carries the panic's Go type and a bounded stack, never the panic value:
// a panic value routinely contains whatever the handler was holding, which may
// be request data, a credential, or a provider payload.
type PanicEvent struct {
	RequestID         string
	ParentRequestID   string
	Method            string
	RouteTemplate     string
	ErrorClass        string
	Stack             string
	ResponseCommitted bool
}

func (l *Logger) PanicRecovered(ev PanicEvent) {
	attrs := []any{
		slog.String("request_id", Sanitize(ev.RequestID)),
		slog.String("method", Sanitize(ev.Method)),
		slog.String("route_template", Sanitize(ev.RouteTemplate)),
		slog.String("error_class", Sanitize(ev.ErrorClass)),
		slog.String("stack", truncate(sanitizeControl(ev.Stack), maxStackLength)),
		slog.Bool("response_committed", ev.ResponseCommitted),
	}
	if ev.ParentRequestID != "" {
		attrs = append(attrs, slog.String("parent_request_id", Sanitize(ev.ParentRequestID)))
	}
	l.slog.Error("panic_recovered", attrs...)
}

// AuthorizationEvent is one authorization decision that was not an allow.
//
// It carries the typed policy reason, which design §6.1 places in security
// monitoring rather than in the response: a caller that could read "suspended"
// versus "wrong role" versus "no such Account" would be reading the hidden
// Account state §5 protects. The response stays uniform; this is where the
// distinction lives.
//
// It carries no Account identifier. Correlating a denial to an Account is done
// through the request ID and Audit, so a log shipped to a monitoring provider
// does not become a directory of who was refused what.
type AuthorizationEvent struct {
	RequestID     string
	Method        string
	RouteTemplate string
	Capability    string
	DenyReason    string
}

func (l *Logger) AuthorizationDenied(ev AuthorizationEvent) {
	l.slog.Warn("authorization_denied",
		slog.String("request_id", Sanitize(ev.RequestID)),
		slog.String("method", Sanitize(ev.Method)),
		slog.String("route_template", Sanitize(ev.RouteTemplate)),
		slog.String("capability", Sanitize(ev.Capability)),
		slog.String("deny_reason", Sanitize(ev.DenyReason)),
	)
}

// AuthorizationFaultEvent is a failure to *reach* an authorization decision —
// the principal could not be resolved because a dependency failed.
//
// Kept separate from a denial on purpose: counting resolution faults as
// refusals would make a database outage look like a spike in authorization
// decisions on every dashboard.
type AuthorizationFaultEvent struct {
	RequestID     string
	Method        string
	RouteTemplate string
	Capability    string
	Err           error
}

func (l *Logger) AuthorizationFault(ev AuthorizationFaultEvent) {
	attrs := []any{
		slog.String("request_id", Sanitize(ev.RequestID)),
		slog.String("method", Sanitize(ev.Method)),
		slog.String("route_template", Sanitize(ev.RouteTemplate)),
		slog.String("capability", Sanitize(ev.Capability)),
	}
	if ev.Err != nil {
		attrs = append(attrs, slog.String("error", Sanitize(ev.Err.Error())))
	}
	l.slog.Error("authorization_fault", attrs...)
}

// ErrorClassOf names a panic value's type without rendering the value.
func ErrorClassOf(v any) string { return fmt.Sprintf("%T", v) }

// Sanitize makes a string safe to place in a structured field: control
// characters — including the newlines that enable log injection — become '?',
// and the result is bounded.
func Sanitize(s string) string {
	return truncate(sanitizeControl(s), maxFieldLength)
}

func sanitizeControl(s string) string {
	if !strings.ContainsFunc(s, isControl) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isControl(r) {
			b.WriteByte('?')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

// truncate bounds a field by bytes, then steps back to a rune boundary.
//
// Cutting mid-rune emits invalid UTF-8, which the JSON encoder replaces with
// U+FFFD — so a truncated Arabic route, error code, or stack frame would end in
// replacement characters rather than readable text. Gradex is Arabic-default,
// so multi-byte content in a log field is the normal case, not an edge case.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…truncated"
}

// safeWriter isolates the logging destination from the request path.
//
// A failing or panicking log sink must not take down a business request that
// already succeeded, so writes are guarded and failures are reported once
// through a minimal stderr fallback rather than retried, buffered, or
// propagated. The fallback prints a fixed notice, never the record it failed
// to write, because that record is what could not be safely handled.
type safeWriter struct {
	dst io.Writer

	mu       sync.Mutex
	notified bool
}

func (w *safeWriter) Write(p []byte) (n int, err error) {
	defer func() {
		if r := recover(); r != nil {
			w.notifyOnce()
			// Report the write as accepted. The caller is slog, which would
			// otherwise surface a telemetry fault into the request path.
			n, err = len(p), nil
		}
	}()

	n, err = w.dst.Write(p)
	if err != nil {
		w.notifyOnce()
		return len(p), nil
	}
	return n, nil
}

func (w *safeWriter) notifyOnce() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.notified {
		return
	}
	w.notified = true
	fmt.Fprintln(os.Stderr, "gradex: structured log sink is failing; telemetry for this process is degraded")
}
