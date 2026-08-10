package email

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

const resendEndpoint = "https://api.resend.com/emails"

var safeProviderValue = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,200}$`)

type ResendOptions struct {
	APIKey     config.Secret
	Timeout    time.Duration
	HTTPClient *http.Client
	Endpoint   string
}

type ResendSender struct {
	apiKey   config.Secret
	client   *http.Client
	endpoint string
}

func NewResendSender(options ResendOptions) (*ResendSender, error) {
	if options.APIKey.IsEmpty() {
		return nil, errors.New("Resend API key is required")
	}
	if options.Timeout <= 0 || options.Timeout > 30*time.Second {
		return nil, errors.New("Resend timeout must be between 1ns and 30s")
	}
	endpoint := options.Endpoint
	if endpoint == "" {
		endpoint = resendEndpoint
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
	// The Resend submission endpoint is fixed, so a redirect is never
	// legitimate here. Following one would replay the Authorization header and
	// the recipient body to whatever host the response named.
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &ResendSender{apiKey: options.APIKey, client: &copyClient, endpoint: endpoint}, nil
}

func (*ResendSender) Provider() string { return "resend" }

func (s *ResendSender) Send(ctx context.Context, message Message, idempotencyKey string) (SendResult, error) {
	if err := validateMessage(message); err != nil {
		return SendResult{}, permanent("invalid_message", "invalid_message")
	}
	if len(idempotencyKey) < 1 || len(idempotencyKey) > 256 || strings.ContainsAny(idempotencyKey, "\r\n\x00") {
		return SendResult{}, permanent("invalid_idempotency_key", "invalid_idempotency_key")
	}
	request, err := s.request(ctx, message, idempotencyKey)
	if err != nil {
		return SendResult{}, err
	}
	response, err := s.client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return SendResult{}, transient("timeout", "timeout", 0)
		}
		return SendResult{}, transient("network", "network", 0)
	}
	return readResendResponse(response)
}

func (s *ResendSender) request(ctx context.Context, message Message, idempotencyKey string) (*http.Request, error) {
	payload := struct {
		From    string   `json:"from"`
		To      []string `json:"to"`
		Subject string   `json:"subject"`
		HTML    string   `json:"html"`
		Text    string   `json:"text"`
		ReplyTo string   `json:"reply_to,omitempty"`
	}{message.From, []string{message.Recipient}, message.Subject, message.HTML, message.Text, message.ReplyTo}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, permanent("invalid_message", "encoding_failed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, permanent("request_build", "request_build")
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey.Expose())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "gradex-transactional-email/1")
	req.Header.Set("Idempotency-Key", idempotencyKey)
	return req, nil
}

func readResendResponse(resp *http.Response) (SendResult, error) {
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024+1))
	if readErr != nil || len(responseBody) > 64*1024 {
		return SendResult{}, transient("response_read", "response_read", 0)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var accepted struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(responseBody, &accepted) != nil || !safeProviderValue.MatchString(accepted.ID) {
			return SendResult{}, permanent("malformed_response", "malformed_response")
		}
		return SendResult{ProviderMessageID: accepted.ID}, nil
	}
	// The client refuses to follow redirects, so a 3xx arrives here intact.
	// Named explicitly rather than left to the generic rejection path so the
	// ledger records why it was refused instead of a bare status code.
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return SendResult{}, permanent("redirect_refused", "redirect_refused")
	}
	return SendResult{}, classifyResendRejection(resp, responseBody)
}

func classifyResendRejection(resp *http.Response, responseBody []byte) error {
	var providerError struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(responseBody, &providerError)
	code := safeCode(providerError.Name, resp.StatusCode)
	retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
	if resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 ||
		(resp.StatusCode == http.StatusConflict && code == "concurrent_idempotent_requests") {
		return transient(http.StatusText(resp.StatusCode), code, retryAfter)
	}
	return permanent("provider_rejected", code)
}

func safeCode(value string, status int) string {
	value = strings.TrimSpace(value)
	if safeProviderValue.MatchString(value) && len(value) <= 80 {
		return value
	}
	return "http_" + strconv.Itoa(status)
}

func parseRetryAfter(value string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds <= 0 || seconds > 24*60*60 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func transient(class, code string, after time.Duration) error {
	return &SendFailure{Kind: FailureTransient, Class: safeClass(class), Code: safeClass(code), RetryAfter: after}
}

func permanent(class, code string) error {
	return &SendFailure{Kind: FailurePermanent, Class: safeClass(class), Code: safeClass(code)}
}

func safeClass(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "_")
	if value == "" || len(value) > 80 || !safeProviderValue.MatchString(value) {
		return "unknown"
	}
	return value
}
