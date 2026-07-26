package identity

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// CompromisedLookupScheme is the versioned one-way representation derived
// inside the password-plaintext boundary. Supporting both algorithms keeps the
// domain seam provider-neutral while LG-021 selects an approved source.
type CompromisedLookupScheme string

const (
	CompromisedSHA1V1   CompromisedLookupScheme = "sha1-v1"
	CompromisedSHA256V1 CompromisedLookupScheme = "sha256-v1"
)

const (
	minCompromisedPrefixLength = 4
	maxCompromisedPrefixLength = 16
	MaxCompromisedCandidates   = 10_000
)

// CompromisedRangeLookup is the only credential-derived value an adapter may
// receive. Prefix is uppercase hexadecimal and is always shorter than the full
// derived digest.
type CompromisedRangeLookup struct {
	Scheme CompromisedLookupScheme
	Prefix string
}

// CompromisedRangeResult contains only suffix candidates for the requested
// prefix. The credential boundary validates and compares them locally.
type CompromisedRangeResult struct {
	CandidateSuffixes []string
}

// CompromisedRangeSource is required for every credential creation/change.
// It never receives plaintext or the complete derived digest.
type CompromisedRangeSource interface {
	Scheme() CompromisedLookupScheme
	PrefixLength() int
	Lookup(context.Context, CompromisedRangeLookup) (CompromisedRangeResult, error)
}

type timeoutCompromisedSource struct {
	source  CompromisedRangeSource
	timeout time.Duration
}

// NewTimeoutCompromisedSource applies the configured whole-lookup deadline to
// an otherwise provider-neutral screening source.
func NewTimeoutCompromisedSource(
	source CompromisedRangeSource,
	timeout time.Duration,
) (CompromisedRangeSource, error) {
	if source == nil {
		return nil, errors.New("compromised-password source is required")
	}
	if timeout <= 0 {
		return nil, errors.New("compromised-password timeout must be positive")
	}
	return &timeoutCompromisedSource{source: source, timeout: timeout}, nil
}

func (s *timeoutCompromisedSource) Scheme() CompromisedLookupScheme {
	return s.source.Scheme()
}

func (s *timeoutCompromisedSource) PrefixLength() int {
	return s.source.PrefixLength()
}

func (s *timeoutCompromisedSource) Lookup(
	ctx context.Context,
	request CompromisedRangeLookup,
) (CompromisedRangeResult, error) {
	deadline, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	return s.source.Lookup(deadline, request)
}

func screenCompromised(
	ctx context.Context,
	password string,
	source CompromisedRangeSource,
) error {
	if source == nil {
		return errors.New("compromised-password screening is not configured")
	}

	full, err := deriveCompromisedLookup(source.Scheme(), password)
	if err != nil {
		return err
	}
	prefixLength := source.PrefixLength()
	if prefixLength < minCompromisedPrefixLength ||
		prefixLength > maxCompromisedPrefixLength ||
		prefixLength >= len(full) {
		return errors.New("compromised-password source has an unsafe prefix length")
	}

	result, err := source.Lookup(ctx, CompromisedRangeLookup{
		Scheme: source.Scheme(),
		Prefix: full[:prefixLength],
	})
	if err != nil {
		return fmt.Errorf("checking password against compromised set: %w", err)
	}
	if len(result.CandidateSuffixes) > MaxCompromisedCandidates {
		return errors.New("compromised-password source returned too many candidates")
	}

	wantSuffix := full[prefixLength:]
	for _, candidate := range result.CandidateSuffixes {
		candidate = strings.ToUpper(strings.TrimSpace(candidate))
		if len(candidate) != len(wantSuffix) || !isUpperHex(candidate) {
			return errors.New("compromised-password source returned a malformed candidate")
		}
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(wantSuffix)) == 1 {
			return fmt.Errorf("%w: appears in a known compromised set", ErrPasswordPolicy)
		}
	}
	return nil
}

func deriveCompromisedLookup(scheme CompromisedLookupScheme, password string) (string, error) {
	var encoded string
	switch scheme {
	case CompromisedSHA1V1:
		sum := sha1.Sum([]byte(password)) // #nosec G505 -- provider lookup representation, not credential storage
		encoded = hex.EncodeToString(sum[:])
	case CompromisedSHA256V1:
		sum := sha256.Sum256([]byte(password))
		encoded = hex.EncodeToString(sum[:])
	default:
		return "", fmt.Errorf("unsupported compromised-password lookup scheme %q", scheme)
	}
	return strings.ToUpper(encoded), nil
}

func isUpperHex(value string) bool {
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'A' || char > 'F') {
			return false
		}
	}
	return true
}

// DeterministicCompromisedSource is a network-free development/test fixture.
// Production configuration rejects it. It accepts full derived digests rather
// than plaintext values so even fixture construction does not widen the
// password boundary.
type DeterministicCompromisedSource struct {
	byPrefix map[string][]string
}

func NewDeterministicCompromisedSource(fullDigests ...string) (*DeterministicCompromisedSource, error) {
	source := &DeterministicCompromisedSource{byPrefix: make(map[string][]string)}
	for _, full := range fullDigests {
		full = strings.ToUpper(strings.TrimSpace(full))
		if len(full) != sha256.Size*2 || !isUpperHex(full) {
			return nil, errors.New("deterministic compromised-password fixture has an invalid digest")
		}
		source.byPrefix[full[:8]] = append(source.byPrefix[full[:8]], full[8:])
	}
	return source, nil
}

func (*DeterministicCompromisedSource) Scheme() CompromisedLookupScheme {
	return CompromisedSHA256V1
}

func (*DeterministicCompromisedSource) PrefixLength() int { return 8 }

func (s *DeterministicCompromisedSource) Lookup(
	_ context.Context,
	request CompromisedRangeLookup,
) (CompromisedRangeResult, error) {
	if request.Scheme != CompromisedSHA256V1 {
		return CompromisedRangeResult{}, errors.New("deterministic source received an unsupported scheme")
	}
	candidates := s.byPrefix[request.Prefix]
	out := make([]string, len(candidates))
	copy(out, candidates)
	return CompromisedRangeResult{CandidateSuffixes: out}, nil
}
