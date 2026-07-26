package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"

	"github.com/Owlah2025/gradex/backend/internal/problem"
)

const strictJSONBodyContextKey = "strictJSONBody"

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

// bindStrictJSON enforces the public admission request boundary before any
// security or domain work runs. It accepts exactly one bounded UTF-8 JSON
// document whose declared object shape is unambiguous.
func bindStrictJSON(c *gin.Context, dst any, maxBytes int64) bool {
	// Strict admission responses are credential/privacy sensitive even when
	// decoding fails before a handler runs. Set this before inspecting the
	// body so every exit path is non-cacheable.
	c.Header("Cache-Control", "no-store")
	valid := false
	defer func() {
		if !valid {
			setAdmissionFailureStage(c, admissionFailureStageStructure)
		}
	}()

	mediaType, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") ||
		hasUnsupportedMediaParameters(params) {
		writeProblem(c, problem.UnsupportedMediaType())
		return false
	}

	body, tooLarge, err := readBounded(c.Request.Body, maxBytes)
	if err != nil {
		writeProblem(c, problem.Malformed())
		return false
	}
	if tooLarge {
		writeProblem(c, problem.ContentTooLarge())
		return false
	}
	if err := rejectDuplicateMembers(body); err != nil {
		writeProblem(c, problem.Malformed())
		return false
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeProblem(c, problem.Malformed())
		return false
	}
	if err := requireJSONEOF(decoder); err != nil {
		writeProblem(c, problem.Malformed())
		return false
	}

	if err := binding.Validator.ValidateStruct(dst); err != nil {
		var validationErrs validator.ValidationErrors
		if errors.As(err, &validationErrs) {
			writeProblem(c, problem.ValidationFailed().
				WithViolations(violationsFrom(validationErrs, dst)...))
			return false
		}
		writeProblem(c, problem.Malformed())
		return false
	}
	valid = true
	return true
}

// strictJSONMiddleware places structural admission before Origin/CSRF and rate
// decisions in a route's handler chain. The final handler receives the exact
// validated pointer through strictJSONBodyContextKey.
func strictJSONMiddleware(factory func() any, maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		dst := factory()
		if dst == nil || !bindStrictJSON(c, dst, maxBytes) {
			return
		}
		c.Set(strictJSONBodyContextKey, dst)
		c.Next()
	}
}

func hasUnsupportedMediaParameters(params map[string]string) bool {
	for name, value := range params {
		if !strings.EqualFold(name, "charset") ||
			(value != "" && !strings.EqualFold(value, "utf-8")) {
			return true
		}
	}
	return false
}

func readBounded(body io.Reader, maxBytes int64) ([]byte, bool, error) {
	if maxBytes < 0 {
		return nil, false, errors.New("negative body limit")
	}
	limited := io.LimitReader(body, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, err
	}
	return data, int64(len(data)) > maxBytes, nil
}

func rejectDuplicateMembers(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			nameToken, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := nameToken.(string)
			if !ok {
				return errors.New("JSON object member name is not a string")
			}
			if _, duplicate := seen[name]; duplicate {
				return errors.New("duplicate JSON object member")
			}
			seen[name] = struct{}{}
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim('}') {
			return errors.New("unterminated JSON object")
		}
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim(']') {
			return errors.New("unterminated JSON array")
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	return nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON documents")
	}
	return err
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
