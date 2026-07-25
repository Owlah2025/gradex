package requestid

import (
	"context"
	"strings"
	"testing"
)

func TestNewProducesDistinctOpaqueIDs(t *testing.T) {
	const n = 1000
	seen := make(map[string]bool, n)
	for range n {
		id := New()
		if len(id) != 32 {
			t.Fatalf("id %q is not 128 bits of hex", id)
		}
		if seen[id] {
			t.Fatalf("New() repeated %q", id)
		}
		seen[id] = true
	}
}

func TestContextRoundTrip(t *testing.T) {
	ctx := WithParent(WithTrusted(context.Background(), "trusted"), "parent")

	if got := FromContext(ctx); got != "trusted" {
		t.Errorf("FromContext = %q, want %q", got, "trusted")
	}
	if got := ParentFromContext(ctx); got != "parent" {
		t.Errorf("ParentFromContext = %q, want %q", got, "parent")
	}
}

func TestEmptyContextReturnsEmptyStrings(t *testing.T) {
	if FromContext(context.Background()) != "" || ParentFromContext(context.Background()) != "" {
		t.Error("a context with no IDs should yield empty strings, not panic")
	}
}

func TestSanitizeParentAcceptsIdentifierValues(t *testing.T) {
	for _, ok := range []string{"abc123", "req-1.2_3", strings.Repeat("a", MaxParentLength)} {
		if got := SanitizeParent(ok); got != ok {
			t.Errorf("SanitizeParent(%q) = %q, want it accepted", ok, got)
		}
	}
}

// Anything that could break a log line, or that is unbounded, is dropped
// rather than repaired: a truncated correlation ID correlates to nothing, so
// keeping a prefix would only manufacture a misleading value.
func TestSanitizeParentRejectsUnsafeValues(t *testing.T) {
	for name, value := range map[string]string{
		"empty":           "",
		"newline":         "abc\ndef",
		"carriage return": "abc\rdef",
		"tab":             "abc\tdef",
		"null byte":       "abc\x00def",
		"space":           "abc def",
		"quote":           `abc"def`,
		"too long":        strings.Repeat("a", MaxParentLength+1),
		"non-ascii":       "abcé",
		"json injection":  `","evil":"`,
	} {
		t.Run(name, func(t *testing.T) {
			if got := SanitizeParent(value); got != "" {
				t.Errorf("SanitizeParent(%q) = %q, want it rejected", value, got)
			}
		})
	}
}
