package identity

import (
	"errors"
	"testing"
)

func TestNormalizeEmailAccepts(t *testing.T) {
	tests := map[string]string{
		"lowercases":                   "Admin@Gradex.Example",
		"trims surrounding space":      "  admin@gradex.example  ",
		"trims a trailing newline":     "admin@gradex.example\n",
		"keeps a plus tag":             "admin+launch@gradex.example",
		"keeps dots in the local part": "first.last@gradex.example",
	}

	want := map[string]string{
		"lowercases":                   "admin@gradex.example",
		"trims surrounding space":      "admin@gradex.example",
		"trims a trailing newline":     "admin@gradex.example",
		"keeps a plus tag":             "admin+launch@gradex.example",
		"keeps dots in the local part": "first.last@gradex.example",
	}

	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := NormalizeEmail(input)
			if err != nil {
				t.Fatalf("normalizing %q: %v", input, err)
			}
			if got != want[name] {
				t.Fatalf("normalized to %q, want %q", got, want[name])
			}
		})
	}
}

// Surrounding whitespace is trimmed; whitespace *inside* the address is not a
// formatting artifact and must be rejected rather than quietly accepted.
func TestNormalizeEmailRejects(t *testing.T) {
	for name, input := range map[string]string{
		"empty":              "",
		"only whitespace":    "   ",
		"no at sign":         "not-an-address",
		"nothing before at":  "@gradex.example",
		"nothing after at":   "admin@",
		"domain without dot": "admin@localhost",
		"inner space":        "ad min@gradex.example",
		"inner newline":      "admin\n@gradex.example",
		"inner tab":          "admin\t@gradex.example",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NormalizeEmail(input); !errors.Is(err, ErrInvalidEmail) {
				t.Fatalf("expected ErrInvalidEmail for %q, got %v", input, err)
			}
		})
	}
}

// Two addresses differing only by case must normalize to the same value, which
// is what makes the accounts_normalized_email_key unique constraint meaningful.
func TestNormalizeEmailCollapsesCaseVariants(t *testing.T) {
	first, err := NormalizeEmail("Admin@Gradex.Example")
	if err != nil {
		t.Fatalf("normalizing: %v", err)
	}
	second, err := NormalizeEmail("aDmIn@gRaDeX.eXaMpLe")
	if err != nil {
		t.Fatalf("normalizing: %v", err)
	}
	if first != second {
		t.Fatalf("case variants normalized differently: %q vs %q", first, second)
	}
}
