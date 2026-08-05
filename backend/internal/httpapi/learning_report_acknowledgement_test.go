package httpapi

import (
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/learning"
)

// T065 focused acknowledgement evidence (FR-034, BR-146).
//
// FR-034 requires an acknowledgement that discloses no Admin queue state, no other report, and no
// moderation outcome. The route-level proof against real PostgreSQL lives in the integration file;
// what these pin is the shape itself — that the public response is a closed allowlist, and that it
// stays closed when the domain row it is built from grows.

// prohibitedAcknowledgementSubstrings is the disclosure set T065 forbids anywhere in a report-route
// response body: moderation workflow, queue mechanics, other reports, internal identity, and
// authority state.
var prohibitedAcknowledgementSubstrings = []string{
	// Moderation workflow and outcome.
	"status", "state", "moderat", "review", "resolv", "dismiss", "delist", "retire",
	"assign", "actor", "admin", "outcome", "verdict", "decision",
	// Queue mechanics.
	"queue", "position", "priority", "severity", "sla", "eta", "estimated",
	"pending", "backlog", "rank",
	// Other reports.
	"similar", "duplicate", "count", "total", "others", "reports",
	// Internal identity and authority.
	"revision", "asset_version", "target_revision_ref", "target_id", "target_kind",
	"course_id", "lesson_id", "entitlement", "enrollment", "reporter", "session",
	"report_context", "explanation", "object_key", "storage", "quota", "remaining",
}

// assertNoProhibitedDisclosure checks a raw response body against the forbidden set, allowing the
// approved field names through.
func assertNoProhibitedDisclosure(t *testing.T, label, body string, allowed ...string) {
	t.Helper()
	scan := strings.ToLower(body)
	for _, approved := range allowed {
		scan = strings.ReplaceAll(scan, strings.ToLower(approved), "")
	}
	for _, forbidden := range prohibitedAcknowledgementSubstrings {
		if strings.Contains(scan, forbidden) {
			t.Fatalf("%s disclosed %q: %s", label, forbidden, body)
		}
	}
}

// problemEnvelopeFields is the shared error contract every refusal on this route uses. A report
// response may add nothing to it: an extra member is where a report-specific disclosure would land.
var problemEnvelopeFields = map[string]bool{
	"type": true, "title": true, "status": true, "detail": true,
	"instance": true, "code": true, "request_id": true, "errors": true,
}

// problemViolationFields is the shared field-level violation contract. `pointer` names the offending
// request field; no member carries the value that was sent.
var problemViolationFields = map[string]bool{
	"code": true, "detail": true, "location": true, "pointer": true,
}

// assertProblemEnvelopeOnly proves a refusal body is exactly the shared envelope, with violations
// that name fields rather than describe reports.
func assertProblemEnvelopeOnly(t *testing.T, body []byte) {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding problem body: %v", err)
	}
	for name, value := range decoded {
		if !problemEnvelopeFields[name] {
			t.Fatalf("refusal body carried a non-envelope member %q: %s", name, body)
		}
		if name != "errors" {
			continue
		}
		violations, ok := value.([]any)
		if !ok {
			t.Fatalf("errors member is not a list: %s", body)
		}
		for _, entry := range violations {
			violation, ok := entry.(map[string]any)
			if !ok {
				t.Fatalf("violation entry is not an object: %s", body)
			}
			for member := range violation {
				if !problemViolationFields[member] {
					t.Fatalf("violation carried a non-contract member %q: %s", member, body)
				}
			}
			// A pointer may name only a field the request itself declares.
			pointer, _ := violation["pointer"].(string)
			switch pointer {
			case "#/report_context", "#/reason", "#/explanation":
			default:
				t.Fatalf("violation pointed at %q, which is not a public request field: %s", pointer, body)
			}
		}
	}
}

// TestAcknowledgementSchemaIsAClosedAllowlist pins the exact public property set.
func TestAcknowledgementSchemaIsAClosedAllowlist(t *testing.T) {
	encoded, err := json.Marshal(newLearningReportResponse(learning.Report{
		ID: "44444444-4444-4444-4444-444444444444", CreatedAt: reportTestIssuance,
	}))
	if err != nil {
		t.Fatalf("encoding acknowledgement: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decoding acknowledgement: %v", err)
	}
	names := make([]string, 0, len(decoded))
	for name := range decoded {
		names = append(names, name)
	}
	sort.Strings(names)
	want := append([]string(nil), learningReportResponseFields...)
	sort.Strings(want)
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("acknowledgement properties = %v, want exactly %v", names, want)
	}

	// No nested object can smuggle internal state through a leaf.
	for name, value := range decoded {
		switch value.(type) {
		case map[string]any, []any:
			t.Fatalf("acknowledgement property %q is a nested structure", name)
		}
	}
}

// TestAcknowledgementCannotLeakANewDomainField is the serialization guard: every field the stored
// report carries is given a distinctive value, and the acknowledgement built from it still exposes
// exactly two. A column added to the domain row arrives here unread.
func TestAcknowledgementCannotLeakANewDomainField(t *testing.T) {
	// Every field of the domain row, filled with a value that would be obvious on the wire.
	stored := learning.Report{
		ID:                "44444444-4444-4444-4444-444444444444",
		ReporterAccountID: "SENTINEL-REPORTER",
		TargetKind:        learning.ReportTargetVideo,
		TargetID:          "SENTINEL-TARGET",
		TargetRevisionRef: "SENTINEL-VERSION",
		Reason:            learning.ReasonSuspectedCopyrightViolatio,
		Explanation:       "SENTINEL-EXPLANATION",
		CreatedAt:         reportTestIssuance,
	}

	// The guard only means something if the fixture actually populated every field.
	value := reflect.ValueOf(stored)
	for i := 0; i < value.NumField(); i++ {
		if value.Field(i).IsZero() {
			t.Fatalf("domain field %q is unset; this guard must exercise every field",
				value.Type().Field(i).Name)
		}
	}

	encoded, err := json.Marshal(newLearningReportResponse(stored))
	if err != nil {
		t.Fatalf("encoding acknowledgement: %v", err)
	}
	body := string(encoded)
	for _, sentinel := range []string{
		"SENTINEL-REPORTER", "SENTINEL-TARGET", "SENTINEL-VERSION", "SENTINEL-EXPLANATION",
		string(learning.ReportTargetVideo), string(learning.ReasonSuspectedCopyrightViolatio),
	} {
		if strings.Contains(body, sentinel) {
			t.Fatalf("the acknowledgement exposed domain field value %q: %s", sentinel, body)
		}
	}
	assertNoProhibitedDisclosure(t, "acknowledgement", body, learningReportResponseFields...)
}

// TestAcknowledgementDTOCannotCarryOpenTypes proves the response struct has no escape hatch: no
// embedded struct, no map, no interface, and no field beyond the allowlist. Those are the shapes
// through which an upstream change leaks without anyone editing this file.
func TestAcknowledgementDTOCannotCarryOpenTypes(t *testing.T) {
	responseType := reflect.TypeOf(learningReportResponse{})
	if responseType.NumField() != len(learningReportResponseFields) {
		t.Fatalf("acknowledgement has %d fields, want exactly %d", responseType.NumField(), len(learningReportResponseFields))
	}
	for i := 0; i < responseType.NumField(); i++ {
		field := responseType.Field(i)
		if field.Anonymous {
			t.Fatalf("acknowledgement field %q is embedded; embedding promotes fields silently", field.Name)
		}
		switch field.Type.Kind() {
		case reflect.Map, reflect.Interface, reflect.Slice, reflect.Struct, reflect.Pointer:
			// time.Time is a struct but serializes as a single RFC 3339 scalar.
			if field.Type != reflect.TypeOf(time.Time{}) {
				t.Fatalf("acknowledgement field %q has open type %s", field.Name, field.Type)
			}
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" {
			t.Fatalf("acknowledgement field %q has no explicit JSON name", field.Name)
		}
	}

	// The domain row carries fields the acknowledgement must never gain a home for.
	acknowledged := map[string]bool{}
	for i := 0; i < responseType.NumField(); i++ {
		acknowledged[responseType.Field(i).Name] = true
	}
	for _, internal := range []string{
		"ReporterAccountID", "TargetKind", "TargetID", "TargetRevisionRef", "Reason", "Explanation",
	} {
		if _, exists := reflect.TypeOf(learning.Report{}).FieldByName(internal); !exists {
			t.Fatalf("domain field %q no longer exists; this guard is stale", internal)
		}
		if acknowledged[internal] {
			t.Fatalf("the acknowledgement gained internal field %q", internal)
		}
	}
}

// TestAcknowledgementTimestampIsThePersistedInstant proves the response reports the stored creation
// time in the accepted UTC RFC 3339 form, not a second handler clock that could disagree with the
// row, and not a review deadline or estimated resolution time.
func TestAcknowledgementTimestampIsThePersistedInstant(t *testing.T) {
	stored := time.Date(2026, 8, 4, 9, 30, 0, 0, time.FixedZone("somewhere", 3*60*60))
	encoded, err := json.Marshal(newLearningReportResponse(learning.Report{
		ID: "44444444-4444-4444-4444-444444444444", CreatedAt: stored,
	}))
	if err != nil {
		t.Fatalf("encoding acknowledgement: %v", err)
	}

	var decoded struct {
		CreatedAt string `json:"created_at"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decoding acknowledgement: %v", err)
	}
	parsed, err := time.Parse(time.RFC3339, decoded.CreatedAt)
	if err != nil {
		t.Fatalf("created_at = %q, want RFC 3339: %v", decoded.CreatedAt, err)
	}
	if !parsed.Equal(stored) {
		t.Fatalf("created_at = %s, want the persisted instant %s", parsed, stored)
	}
	if !strings.HasSuffix(decoded.CreatedAt, "Z") {
		t.Fatalf("created_at = %q, want UTC as the accepted read models use", decoded.CreatedAt)
	}
}

// TestValidationResponsesRedactSubmittedContent proves a public validation failure names the field
// at fault and never echoes what was sent — not the encrypted context, not the explanation, and no
// decoder text quoting the input.
func TestValidationResponsesRedactSubmittedContent(t *testing.T) {
	context := defaultLessonContext(t)
	explanation := "SENTINEL-EXPLANATION-TEXT"

	tests := []struct {
		name string
		body string
		want int
	}{
		{"invalid reason", `{"report_context":"` + context + `","reason":"SENTINEL-REASON","explanation":"` + explanation + `"}`, http.StatusUnprocessableEntity},
		{"other without explanation", `{"report_context":"` + context + `","reason":"other"}`, http.StatusUnprocessableEntity},
		{"missing context", `{"reason":"inaccurate","explanation":"` + explanation + `"}`, http.StatusUnprocessableEntity},
		{"unknown field", `{"report_context":"` + context + `","reason":"inaccurate","queue_position":3}`, http.StatusBadRequest},
		{"malformed JSON", `{"report_context":"` + context + `","reason":`, http.StatusBadRequest},
		{"type mismatch", `{"report_context":"` + context + `","reason":{"SENTINEL":"OBJECT"}}`, http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router, _, logs := learningReportRouter(t, activeStudent(), nil, allowingReportEvaluator(), testReportContextIssuer(t))
			response := reportRequest(t, router, "application/json", tc.body)
			if response.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", response.Code, tc.want, response.Body.String())
			}
			surfaces := response.Body.String() + logs.String()
			for _, sentinel := range []string{
				context, explanation, "SENTINEL-REASON", "SENTINEL-EXPLANATION-TEXT", "SENTINEL", "OBJECT",
			} {
				if strings.Contains(surfaces, sentinel) {
					t.Fatalf("a validation response or its log echoed %q: %s", sentinel, surfaces)
				}
			}
			// The body is the shared problem envelope and nothing else. Checking the property
			// set rather than keywords keeps RFC 7807's own `status` member — the HTTP status,
			// not a moderation status — from being mistaken for a disclosure.
			assertProblemEnvelopeOnly(t, response.Body.Bytes())
		})
	}
}

// TestAcknowledgementHasNoRetrievalOrCachingHeaders proves the success response points at no
// read-back route and cannot be stored.
func TestAcknowledgementHasNoRetrievalOrCachingHeaders(t *testing.T) {
	parts := learningThrottleRouter(t, activeStudent(), nil, &spyReportRateStore{allow: true})
	response := reportRequest(t, parts.router, "application/json",
		`{"report_context":"`+defaultLessonContext(t)+`","reason":"inaccurate"}`)

	// The counting repository refuses, so this exercises the header contract of the route rather
	// than a created row; the created case is proven against PostgreSQL in the integration file.
	for _, header := range []string{"Location", "ETag", "Last-Modified", "Retry-After", "WWW-Authenticate"} {
		if response.Header().Get(header) != "" {
			t.Fatalf("report response carried %s: %q", header, response.Header().Get(header))
		}
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
	}
}
