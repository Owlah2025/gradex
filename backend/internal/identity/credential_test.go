package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

type recordingRangeSource struct {
	scheme       CompromisedLookupScheme
	prefixLength int
	result       CompromisedRangeResult
	err          error
	request      CompromisedRangeLookup
	deadline     time.Time
}

func (s *recordingRangeSource) Scheme() CompromisedLookupScheme { return s.scheme }
func (s *recordingRangeSource) PrefixLength() int               { return s.prefixLength }
func (s *recordingRangeSource) Lookup(ctx context.Context, request CompromisedRangeLookup) (CompromisedRangeResult, error) {
	s.request = request
	s.deadline, _ = ctx.Deadline()
	return s.result, s.err
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func sourceForPassword(password string) *recordingRangeSource {
	full := sha256Hex(password)
	return &recordingRangeSource{
		scheme:       CompromisedSHA256V1,
		prefixLength: 8,
		result: CompromisedRangeResult{
			CandidateSuffixes: []string{full[8:]},
		},
	}
}

func newCredentialPreparation(password string) credentialPreparation {
	return credentialPreparation{
		next: config.NewSecret(password),
		mode: credentialPrepareNew,
	}
}

func TestPrepareCredentialKeepsPlaintextAndFullDigestInsideBoundary(t *testing.T) {
	source := sourceForPassword("another-long-passphrase")
	_, err := prepareCredential(
		context.Background(),
		newCredentialPreparation(goodPassword),
		source,
	)
	if err != nil {
		t.Fatalf("preparing credential: %v", err)
	}

	full := sha256Hex(goodPassword)
	if source.request.Prefix == "" {
		t.Fatal("range source did not receive a lookup")
	}
	if strings.Contains(source.request.Prefix, goodPassword) {
		t.Fatal("range source received password plaintext")
	}
	if source.request.Prefix == full {
		t.Fatal("range source received the complete derived digest")
	}
	if source.request.Prefix != full[:source.prefixLength] {
		t.Errorf("prefix = %q, want derived prefix", source.request.Prefix)
	}
}

func TestPrepareCredentialRejectsCompromisedPassword(t *testing.T) {
	_, err := prepareCredential(
		context.Background(),
		newCredentialPreparation(goodPassword),
		sourceForPassword(goodPassword),
	)
	if !errors.Is(err, ErrPasswordPolicy) {
		t.Fatalf("expected ErrPasswordPolicy, got %v", err)
	}
}

func TestPrepareCredentialFailsClosedWithoutUsableSource(t *testing.T) {
	tests := map[string]CompromisedRangeSource{
		"nil": nil,
		"source error": &recordingRangeSource{
			scheme:       CompromisedSHA256V1,
			prefixLength: 8,
			err:          errors.New("source unavailable"),
		},
		"unknown scheme": &recordingRangeSource{
			scheme:       "unknown-v1",
			prefixLength: 8,
		},
		"full digest prefix": &recordingRangeSource{
			scheme:       CompromisedSHA256V1,
			prefixLength: 64,
		},
		"malformed suffix": &recordingRangeSource{
			scheme:       CompromisedSHA256V1,
			prefixLength: 8,
			result:       CompromisedRangeResult{CandidateSuffixes: []string{"not-hex"}},
		},
	}

	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := prepareCredential(
				context.Background(),
				newCredentialPreparation(goodPassword),
				source,
			)
			if err == nil {
				t.Fatal("expected fail-closed error")
			}
			if strings.Contains(err.Error(), goodPassword) {
				t.Fatalf("error leaked password: %v", err)
			}
		})
	}
}

func TestPrepareCredentialRejectsOversizedCandidateSet(t *testing.T) {
	source := &recordingRangeSource{
		scheme:       CompromisedSHA256V1,
		prefixLength: 8,
		result: CompromisedRangeResult{
			CandidateSuffixes: make([]string, MaxCompromisedCandidates+1),
		},
	}
	_, err := prepareCredential(
		context.Background(),
		newCredentialPreparation(goodPassword),
		source,
	)
	if err == nil {
		t.Fatal("expected oversized source response to fail closed")
	}
}

func TestTimeoutCompromisedSourceBoundsLookup(t *testing.T) {
	source := sourceForPassword("another-long-passphrase")
	const timeout = 75 * time.Millisecond
	bounded, err := NewTimeoutCompromisedSource(source, timeout)
	if err != nil {
		t.Fatalf("constructing timeout source: %v", err)
	}
	started := time.Now()
	if _, err := prepareCredential(
		context.Background(), newCredentialPreparation(goodPassword), bounded,
	); err != nil {
		t.Fatalf("preparing credential: %v", err)
	}
	if source.deadline.IsZero() {
		t.Fatal("compromised-password lookup received no deadline")
	}
	remaining := source.deadline.Sub(started)
	if remaining <= 0 || remaining > timeout+25*time.Millisecond {
		t.Fatalf("lookup deadline = %s after start, want at most %s", remaining, timeout)
	}
}
