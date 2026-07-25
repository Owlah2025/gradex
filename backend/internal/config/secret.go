package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// Secret wraps a resolved secret value so that the ordinary ways a value
// leaks — fmt verbs, structured log fields, JSON encoding of a config dump —
// all produce the redaction marker instead of the plaintext. Reading the real
// value requires calling Expose(), which is greppable in review.
//
// This is the enforcement point for the telemetry data boundary in
// docs/superpowers/specs/2026-07-27-api-security-integration-design.md §10.2:
// telemetry never contains password, token, signed capability, or provider
// signature material.
type Secret struct {
	value string
}

const redacted = "[REDACTED]"

// NewSecret is exported for tests and for callers that obtain a secret from a
// source other than the resolver.
func NewSecret(value string) Secret { return Secret{value: value} }

// Expose returns the plaintext. Every call site is a deliberate decision to
// hand the value to something that needs it — a driver, a signer, a provider
// client — and should hand it no further.
func (s Secret) Expose() string { return s.value }

func (s Secret) IsEmpty() bool { return s.value == "" }

// String, GoString, MarshalText, and MarshalJSON together cover %s, %v, %q,
// %#v, encoding/json, and any logger that reaches for an encoding.TextMarshaler.
// A Secret that grows a new leak path should fail one of the tests in
// secret_test.go rather than reach production.
func (s Secret) String() string               { return redacted }
func (s Secret) GoString() string             { return redacted }
func (s Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }

// LogValue satisfies slog.LogValuer, which is the interface slog consults
// first. Returning a plain string did not implement it: slog fell back to
// TextMarshaler and still redacted, but only incidentally, while the method
// name advertised a guarantee the type did not actually provide.
//
// The compile-time assertion below is the point — it fails if the signature
// drifts again.
func (s Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

var _ slog.LogValuer = Secret{}

// SecretRef names where a secret lives without containing it. Typed runtime
// configuration may hold a reference; it may never hold the value (§11.2).
type SecretRef struct {
	// Name is the key the resolver looks up — an environment variable today,
	// a secret-manager path once one is approved.
	Name string
	// Required marks a secret whose absence must be handled by the caller's
	// fail-closed rule rather than silently producing an empty value.
	Required bool
}

func (r SecretRef) String() string { return r.Name }

// SecretResolver turns references into values. The interface exists so the
// approved secret manager, workload identity, or secure injection can replace
// environment lookup without touching validation or any call site (§11.2).
type SecretResolver interface {
	Resolve(ref SecretRef) (Secret, error)
}

// EnvSecretResolver reads secrets from the process environment. This is the
// development and single-host deployment path. It is not the production
// target once a secret manager is approved, but it keeps the boundary honest
// in the meantime: nothing outside this type reads a secret from os.Getenv.
type EnvSecretResolver struct{}

func (EnvSecretResolver) Resolve(ref SecretRef) (Secret, error) {
	if strings.TrimSpace(ref.Name) == "" {
		return Secret{}, fmt.Errorf("secret reference has no name")
	}
	return Secret{value: os.Getenv(ref.Name)}, nil
}

// MapSecretResolver resolves from an in-memory map. Tests use it so that
// exercising a missing-secret path does not require mutating the environment.
type MapSecretResolver map[string]string

func (m MapSecretResolver) Resolve(ref SecretRef) (Secret, error) {
	if strings.TrimSpace(ref.Name) == "" {
		return Secret{}, fmt.Errorf("secret reference has no name")
	}
	return Secret{value: m[ref.Name]}, nil
}
