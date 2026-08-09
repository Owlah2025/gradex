package identity

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	hibpRangeEndpoint   = "https://api.pwnedpasswords.com/range"
	hibpUserAgent       = "Gradex-Credential-Admission/1.0"
	hibpPrefixLength    = 5
	hibpSuffixLength    = 35
	hibpMaxResponseSize = 128 << 10

	// HIBPDefaultRequestTimeout is the approved total request bound for the
	// production Pwned Passwords range lookup.
	HIBPDefaultRequestTimeout = 3 * time.Second
)

// HIBPCompromisedSource implements the HIBP Pwned Passwords range protocol.
// Its input is already reduced to a five-character SHA-1 prefix by the shared
// credential boundary; it cannot access plaintext or a complete digest.
type HIBPCompromisedSource struct {
	endpoint *url.URL
	client   *http.Client
}

// NewHIBPCompromisedSource constructs the fixed production source with
// verified HTTPS and a bounded single request.
func NewHIBPCompromisedSource() (*HIBPCompromisedSource, error) {
	return newHIBPCompromisedSource(
		hibpRangeEndpoint,
		&http.Client{Timeout: HIBPDefaultRequestTimeout},
	)
}

func newHIBPCompromisedSource(endpoint string, client *http.Client) (*HIBPCompromisedSource, error) {
	if client == nil {
		return nil, errors.New("compromised-password HTTP client is required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, errors.New("compromised-password endpoint must be an absolute HTTPS URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("compromised-password endpoint must not contain credentials, a query, or a fragment")
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	if parsed.Path == "" {
		return nil, errors.New("compromised-password endpoint path is required")
	}
	clientCopy := *client
	clientCopy.CheckRedirect = rejectHIBPRedirect
	return &HIBPCompromisedSource{endpoint: parsed, client: &clientCopy}, nil
}

func rejectHIBPRedirect(*http.Request, []*http.Request) error {
	return errors.New("compromised-password redirects are not permitted")
}

func (*HIBPCompromisedSource) Scheme() CompromisedLookupScheme {
	return CompromisedSHA1V1
}

func (*HIBPCompromisedSource) PrefixLength() int { return hibpPrefixLength }

func (s *HIBPCompromisedSource) Lookup(
	ctx context.Context,
	request CompromisedRangeLookup,
) (CompromisedRangeResult, error) {
	httpRequest, err := s.rangeRequest(ctx, request)
	if err != nil {
		return CompromisedRangeResult{}, err
	}
	response, err := s.client.Do(httpRequest)
	if err != nil {
		return CompromisedRangeResult{}, errors.New("compromised-password service is unavailable")
	}
	defer response.Body.Close()
	return readHIBPResponse(response)
}

func (s *HIBPCompromisedSource) rangeRequest(
	ctx context.Context,
	lookup CompromisedRangeLookup,
) (*http.Request, error) {
	if lookup.Scheme != CompromisedSHA1V1 ||
		len(lookup.Prefix) != hibpPrefixLength ||
		!isUpperHex(lookup.Prefix) {
		return nil, errors.New("compromised-password lookup request is invalid")
	}
	requestURL := *s.endpoint
	requestURL.Path += "/" + lookup.Prefix
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, errors.New("constructing compromised-password request")
	}
	request.Header.Set("Accept", "text/plain")
	request.Header.Set("Add-Padding", "true")
	request.Header.Set("User-Agent", hibpUserAgent)
	return request, nil
}

func readHIBPResponse(response *http.Response) (CompromisedRangeResult, error) {
	if response.StatusCode != http.StatusOK {
		return CompromisedRangeResult{}, errors.New("compromised-password service returned an invalid response")
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, parseErr := mime.ParseMediaType(contentType)
		if parseErr != nil || mediaType != "text/plain" {
			return CompromisedRangeResult{}, errors.New("compromised-password service returned an invalid response")
		}
	}

	limited := io.LimitReader(response.Body, hibpMaxResponseSize+1)
	body, err := io.ReadAll(limited)
	if err != nil || len(body) > hibpMaxResponseSize {
		return CompromisedRangeResult{}, errors.New("compromised-password service returned an invalid response")
	}
	return parseHIBPRange(body)
}

func parseHIBPRange(body []byte) (CompromisedRangeResult, error) {
	candidates := make([]string, 0, 1_000)
	scanner := bufio.NewScanner(bytes.NewReader(body))
	records := 0
	for scanner.Scan() {
		records++
		suffix, count, err := parseHIBPRecord(strings.TrimSuffix(scanner.Text(), "\r"))
		if err != nil {
			return CompromisedRangeResult{}, errors.New("compromised-password service returned an invalid response")
		}
		if count > 0 {
			candidates = append(candidates, suffix)
		}
	}
	if err := scanner.Err(); err != nil {
		return CompromisedRangeResult{}, errors.New("compromised-password service returned an invalid response")
	}
	if records == 0 {
		return CompromisedRangeResult{}, errors.New("compromised-password service returned an invalid response")
	}
	return CompromisedRangeResult{CandidateSuffixes: candidates}, nil
}

func parseHIBPRecord(line string) (string, uint64, error) {
	suffix, countText, found := strings.Cut(line, ":")
	if !found || len(suffix) != hibpSuffixLength || !isUpperHex(suffix) {
		return "", 0, errors.New("invalid HIBP record")
	}
	count, err := strconv.ParseUint(countText, 10, 64)
	if err != nil {
		return "", 0, errors.New("invalid HIBP record")
	}
	return suffix, count, nil
}
