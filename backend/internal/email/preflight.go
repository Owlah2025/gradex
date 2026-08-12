package email

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

const resendDomainsEndpoint = "https://api.resend.com/domains"

// SendingDomainStatus is the provider's own answer about one sending domain.
// `Verified` is the only status that may carry launch traffic: the provider
// reports it once the DNS records it generated are present and correct, which
// is the evidence LG-018 asks for and the one thing this repository cannot
// assert on its own.
type SendingDomainStatus struct {
	Domain   string
	Status   string
	Region   string
	Verified bool
	// Known reports whether the provider account contains this domain at all.
	// An unknown domain is a different operational failure from a known but
	// unverified one, and the two need different founder actions.
	Known bool
}

type SendingDomainInspectorOptions struct {
	APIKey     config.Secret
	Timeout    time.Duration
	HTTPClient *http.Client
	Endpoint   string
}

// SendingDomainInspector is a read-only provider client used by the launch
// preflight. It shares the sender's transport discipline — HTTPS only, no
// redirects, bounded body — but performs no mutation and sends no mail.
type SendingDomainInspector struct {
	apiKey   config.Secret
	client   *http.Client
	endpoint string
}

func NewSendingDomainInspector(options SendingDomainInspectorOptions) (*SendingDomainInspector, error) {
	if options.APIKey.IsEmpty() {
		return nil, errors.New("Resend API key is required")
	}
	if options.Timeout <= 0 || options.Timeout > 30*time.Second {
		return nil, errors.New("Resend timeout must be between 1ns and 30s")
	}
	endpoint := options.Endpoint
	if endpoint == "" {
		endpoint = resendDomainsEndpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("Resend endpoint must be an absolute credential-free HTTPS URL")
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	copyClient := *client
	copyClient.Timeout = options.Timeout
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &SendingDomainInspector{apiKey: options.APIKey, client: &copyClient, endpoint: endpoint}, nil
}

// SenderDomain is the domain part of a bare sender address, lowercased.
func SenderDomain(fromAddress string) (string, error) {
	at := strings.LastIndex(fromAddress, "@")
	if at <= 0 || at == len(fromAddress)-1 {
		return "", errors.New("sender address has no domain")
	}
	return strings.ToLower(fromAddress[at+1:]), nil
}

// Inspect reports the provider's status for the sending domain of fromAddress.
// It never returns provider response text, so a failure cannot leak account
// detail into operator output.
func (i *SendingDomainInspector) Inspect(ctx context.Context, fromAddress string) (SendingDomainStatus, error) {
	domain, err := SenderDomain(fromAddress)
	if err != nil {
		return SendingDomainStatus{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, i.endpoint, nil)
	if err != nil {
		return SendingDomainStatus{}, errors.New("building Resend domain request failed")
	}
	request.Header.Set("Authorization", "Bearer "+i.apiKey.Expose())
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "gradex-transactional-email/1")

	response, err := i.client.Do(request)
	if err != nil {
		return SendingDomainStatus{}, errors.New("Resend domain listing is unreachable")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 256*1024+1))
	if err != nil || len(body) > 256*1024 {
		return SendingDomainStatus{}, errors.New("Resend domain listing response could not be read")
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return SendingDomainStatus{}, errors.New("Resend rejected the API key for domain listing")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return SendingDomainStatus{}, fmt.Errorf("Resend domain listing failed with status %d", response.StatusCode)
	}

	var listing struct {
		Data []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Region string `json:"region"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &listing) != nil {
		return SendingDomainStatus{}, errors.New("Resend domain listing response is malformed")
	}
	for _, entry := range listing.Data {
		if !strings.EqualFold(strings.TrimSpace(entry.Name), domain) {
			continue
		}
		status := safeClass(entry.Status)
		return SendingDomainStatus{
			Domain:   domain,
			Status:   status,
			Region:   safeClass(entry.Region),
			Verified: status == "verified",
			Known:    true,
		}, nil
	}
	return SendingDomainStatus{Domain: domain, Status: "absent", Known: false}, nil
}
