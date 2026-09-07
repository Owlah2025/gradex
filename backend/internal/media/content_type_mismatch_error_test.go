package media

import (
	"errors"
	"fmt"
	"testing"
)

// TestContentTypeMismatchErrorSatisfiesBothSentinels pins the two identities
// callers depend on. The HTTP layer dispatches on ErrValidation to reach the
// 422 branch and only then inspects the typed error, so losing either identity
// silently downgrades a content-type mismatch into a 500.
func TestContentTypeMismatchErrorSatisfiesBothSentinels(t *testing.T) {
	err := error(&ContentTypeMismatchError{DeclaredContentType: "video/mp4", ActualContentType: "video/webm"})

	if !errors.Is(err, ErrContentTypeMismatch) {
		t.Fatal("ContentTypeMismatchError is not ErrContentTypeMismatch")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatal("ContentTypeMismatchError is not ErrValidation; the Unwrap chain is broken")
	}

	// The identities must survive the wrapping the service layer applies.
	wrapped := fmt.Errorf("completing upload: %w", err)
	if !errors.Is(wrapped, ErrContentTypeMismatch) || !errors.Is(wrapped, ErrValidation) {
		t.Fatal("wrapping lost a content-type mismatch identity")
	}

	var typed *ContentTypeMismatchError
	if !errors.As(wrapped, &typed) || typed.ActualContentType != "video/webm" {
		t.Fatalf("errors.As did not recover the typed error: %+v", typed)
	}

	// It must not be mistaken for unrelated sentinels.
	for name, other := range map[string]error{
		"not found":  ErrNotFound,
		"conflict":   ErrConflict,
		"authorized": ErrNotAuthorized,
	} {
		if errors.Is(err, other) {
			t.Fatalf("ContentTypeMismatchError matched the unrelated %s sentinel", name)
		}
	}
}
