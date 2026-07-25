package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const plaintext = "super-secret-value-do-not-leak"

// TestSecretNeverRendersPlaintext covers the formatting and encoding paths a
// secret realistically escapes through: a log line, a struct dump, a JSON
// diagnostic endpoint. Each must produce the redaction marker.
func TestSecretNeverRendersPlaintext(t *testing.T) {
	s := NewSecret(plaintext)

	rendered := map[string]string{
		"%s":           fmt.Sprintf("%s", s),
		"%v":           fmt.Sprintf("%v", s),
		"%q":           fmt.Sprintf("%q", s),
		"%#v":          fmt.Sprintf("%#v", s),
		"String()":     s.String(),
		"LogValue()":   s.LogValue(),
		"struct %v":    fmt.Sprintf("%v", struct{ Token Secret }{s}),
		"pointer %v":   fmt.Sprintf("%v", &s),
		"slice %v":     fmt.Sprintf("%v", []Secret{s}),
		"map value %v": fmt.Sprintf("%v", map[string]Secret{"k": s}),
	}

	text, err := s.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText: %v", err)
	}
	rendered["MarshalText"] = string(text)

	encoded, err := json.Marshal(struct {
		Token Secret `json:"token"`
	}{s})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	rendered["json.Marshal"] = string(encoded)

	for path, out := range rendered {
		if strings.Contains(out, plaintext) {
			t.Errorf("%s leaked the plaintext: %s", path, out)
		}
		if !strings.Contains(out, redacted) {
			t.Errorf("%s did not redact, got %q", path, out)
		}
	}
}

// Expose is the single sanctioned way out. If this ever stops working the
// drivers and signers that need the real value break loudly.
func TestSecretExposeReturnsPlaintext(t *testing.T) {
	if got := NewSecret(plaintext).Expose(); got != plaintext {
		t.Errorf("Expose() = %q, want %q", got, plaintext)
	}
}

func TestSecretIsEmpty(t *testing.T) {
	if !NewSecret("").IsEmpty() {
		t.Error("empty secret should report IsEmpty")
	}
	if NewSecret("x").IsEmpty() {
		t.Error("non-empty secret should not report IsEmpty")
	}
}

func TestResolverRejectsUnnamedRef(t *testing.T) {
	for name, r := range map[string]SecretResolver{
		"env": EnvSecretResolver{},
		"map": MapSecretResolver{},
	} {
		if _, err := r.Resolve(SecretRef{Name: "  "}); err == nil {
			t.Errorf("%s resolver accepted a reference with no name", name)
		}
	}
}
