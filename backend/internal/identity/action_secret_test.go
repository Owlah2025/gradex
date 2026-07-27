package identity

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewActionSecretGeneratesDigestOnlyStorageMaterial(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	issued, err := newActionSecret(actionSecretOptions{
		Purpose: ActionEmailVerification,
		Now:     now, TTL: time.Hour, Random: bytes.NewReader(bytes.Repeat([]byte{0x42}, 32)),
	})
	if err != nil {
		t.Fatalf("issuing action secret: %v", err)
	}
	bearer := issued.Bearer.Expose()
	if len(bearer) != 43 || strings.Contains(bearer, "=") {
		t.Fatalf("bearer format = %q, want 32-byte raw URL encoding", bearer)
	}
	if len(issued.Digest) != 32 || bytes.Contains(issued.Digest, []byte(bearer)) {
		t.Fatal("stored material is not a fixed one-way digest")
	}
	if issued.ExpiresAt != now.Add(time.Hour) || issued.Purpose != ActionEmailVerification {
		t.Fatalf("issued lifecycle = %+v", issued)
	}
}

func TestDigestActionSecretCollapsesMalformedBearerValues(t *testing.T) {
	for _, bearer := range []string{"", "not base64!", "c2hvcnQ"} {
		t.Run(bearer, func(t *testing.T) {
			if _, err := DigestActionSecret(bearer); !errors.Is(err, ErrTokenInvalid) {
				t.Fatalf("error = %v, want ErrTokenInvalid", err)
			}
		})
	}
}

// TestNewActionSecretCarriesRequestedPurpose guards the property that made
// password recovery possible without a second secret table: the issued purpose
// is the one the caller asked for. The constructor previously hardcoded
// EMAIL_VERIFICATION, so a reset secret would have been written under the wrong
// purpose and silently collided with the one-live-per-purpose index.
func TestNewActionSecretCarriesRequestedPurpose(t *testing.T) {
	for _, purpose := range []ActionSecretPurpose{ActionEmailVerification, ActionPasswordReset} {
		t.Run(string(purpose), func(t *testing.T) {
			issued, err := newActionSecret(actionSecretOptions{
				Purpose: purpose,
				Now:     time.Now(), TTL: time.Hour,
				Random: bytes.NewReader(bytes.Repeat([]byte{0x11}, 32)),
			})
			if err != nil {
				t.Fatalf("issuing action secret: %v", err)
			}
			if issued.Purpose != purpose {
				t.Fatalf("purpose = %q, want %q", issued.Purpose, purpose)
			}
		})
	}
}

func TestNewActionSecretRejectsUnsafeClockRandomAndTTL(t *testing.T) {
	tests := map[string]actionSecretOptions{
		"zero TTL": {
			Purpose: ActionEmailVerification,
			Now:     time.Now(), Random: bytes.NewReader(make([]byte, 32)),
		},
		"short random": {
			Purpose: ActionEmailVerification,
			Now:     time.Now(), TTL: time.Hour, Random: bytes.NewReader(make([]byte, 31)),
		},
		"missing purpose": {
			Now: time.Now(), TTL: time.Hour, Random: bytes.NewReader(make([]byte, 32)),
		},
		"unknown purpose": {
			Purpose: ActionSecretPurpose("ACCOUNT_DELETION"),
			Now:     time.Now(), TTL: time.Hour, Random: bytes.NewReader(make([]byte, 32)),
		},
	}
	for scenario, options := range tests {
		t.Run(scenario, func(t *testing.T) {
			if _, err := newActionSecret(options); err == nil {
				t.Fatal("unsafe action-secret configuration was accepted")
			}
		})
	}
}
