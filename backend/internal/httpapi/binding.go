package httpapi

import (
	"errors"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"github.com/Owlah2025/gradex/backend/internal/problem"
)

// bindJSON decodes and validates a request body, writing the correct problem
// and returning false when it cannot.
//
// It separates two failures the framework reports through one error type,
// because §2.3 maps them to different statuses:
//
//   - the body is not parseable as the declared shape — structural, 400;
//   - the body parsed but a field is unacceptable — semantic, 422.
//
// Decoder text is never surfaced. A json.SyntaxError or UnmarshalTypeError
// message quotes the offending input, which on this API can be a filename, a
// token, or an email.
func bindJSON(c *gin.Context, dst any) bool {
	err := c.ShouldBindJSON(dst)
	if err == nil {
		return true
	}

	var validationErrs validator.ValidationErrors
	if errors.As(err, &validationErrs) {
		writeProblem(c, problem.ValidationFailed().WithViolations(violationsFrom(validationErrs, dst)...))
		return false
	}

	// Everything else is structural: syntax errors, type mismatches, an empty
	// body, a truncated stream.
	writeProblem(c, problem.Malformed())
	return false
}

// violationsFrom converts validator failures into the typed entries §2.3
// defines, using each field's JSON name so the pointer matches what the client
// actually sent rather than the Go struct field name.
//
// Only the field's location and a fixed code are reported. The rejected value
// is never echoed: on this API it can be a credential.
func violationsFrom(errs validator.ValidationErrors, dst any) []problem.Violation {
	out := make([]problem.Violation, 0, len(errs))
	for _, fe := range errs {
		name := jsonFieldName(dst, fe.Field())
		out = append(out, problem.Violation{
			Code:     violationCode(fe.Tag()),
			Detail:   violationDetail(fe.Tag()),
			Location: problem.LocationBody,
			Pointer:  "#/" + escapeJSONPointer(name),
		})
	}
	return out
}

func violationCode(tag string) string {
	switch tag {
	case "required":
		return "REQUIRED"
	default:
		return "INVALID_VALUE"
	}
}

func violationDetail(tag string) string {
	switch tag {
	case "required":
		return "This field is required."
	default:
		return "This field is invalid."
	}
}

// jsonFieldName maps a Go struct field name back to its JSON tag, falling back
// to the Go name when the struct carries no tag.
func jsonFieldName(dst any, goField string) string {
	t := reflect.TypeOf(dst)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return goField
	}
	field, ok := t.FieldByName(goField)
	if !ok {
		return goField
	}
	tag := field.Tag.Get("json")
	if tag == "" {
		return goField
	}
	if name, _, _ := strings.Cut(tag, ","); name != "" && name != "-" {
		return name
	}
	return goField
}

// escapeJSONPointer applies RFC 6901 escaping, which §2.3 requires for body
// pointers: "~" becomes "~0" and "/" becomes "~1".
func escapeJSONPointer(s string) string {
	s = strings.ReplaceAll(s, "~", "~0")
	return strings.ReplaceAll(s, "/", "~1")
}
