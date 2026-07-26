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
		Now: now, TTL: time.Hour, Random: bytes.NewReader(bytes.Repeat([]byte{0x42}, 32)),
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

func TestNewActionSecretRejectsUnsafeClockRandomAndTTL(t *testing.T) {
	tests := map[string]actionSecretOptions{
		"zero TTL":     {Now: time.Now(), Random: bytes.NewReader(make([]byte, 32))},
		"short random": {Now: time.Now(), TTL: time.Hour, Random: bytes.NewReader(make([]byte, 31))},
	}
	for scenario, options := range tests {
		t.Run(scenario, func(t *testing.T) {
			if _, err := newActionSecret(options); err == nil {
				t.Fatal("unsafe action-secret configuration was accepted")
			}
		})
	}
}
