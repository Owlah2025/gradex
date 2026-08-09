//go:build provider

package identity

import (
	"context"
	"testing"
)

// This opt-in compatibility check uses a fixed digest prefix, not a password.
// Deterministic unit and integration tests remain independent of live HIBP.
func TestHIBPProviderCompatibility(t *testing.T) {
	source, err := NewHIBPCompromisedSource()
	if err != nil {
		t.Fatalf("constructing production HIBP source: %v", err)
	}
	result, err := source.Lookup(context.Background(), CompromisedRangeLookup{
		Scheme: CompromisedSHA1V1,
		Prefix: "00000",
	})
	if err != nil {
		t.Fatalf("live HIBP range lookup: %v", err)
	}
	if len(result.CandidateSuffixes) == 0 {
		t.Fatal("live HIBP response contained no positive-count suffixes")
	}
}
