package ratelimit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net"
	"strings"
	"sync"
	"time"
)

type Outcome string

const (
	OutcomeAllowed         Outcome = "ALLOW"
	OutcomeDenied          Outcome = "DENY"
	OutcomeFallbackAllowed Outcome = "FALLBACK_ALLOW"
	OutcomeFallbackDenied  Outcome = "FALLBACK_DENY"
	OutcomeUnavailable     Outcome = "UNAVAILABLE"
)

type Decision struct {
	Allowed    bool
	Outcome    Outcome
	RetryAfter time.Duration
}

type Input struct {
	Identifier  string
	ClientIP    string
	AnonymousID string
}

type Entry struct {
	Key    string
	Limit  int64
	Window time.Duration
}

type Limiter struct {
	store   Store
	hmacKey []byte
	timeout time.Duration
	local   *localFallback
	now     func() time.Time

	circuitMu       sync.Mutex
	failures        int
	circuitOpenTill time.Time
}

func New(store Store, hmacKey []byte, timeout time.Duration) (*Limiter, error) {
	if store == nil {
		return nil, errors.New("distributed rate-limit store is required")
	}
	if len(hmacKey) < 32 {
		return nil, errors.New("rate-limit HMAC key must contain at least 32 bytes")
	}
	if timeout <= 0 {
		return nil, errors.New("rate-limit timeout must be positive")
	}
	return &Limiter{
		store:   store,
		hmacKey: append([]byte(nil), hmacKey...),
		timeout: timeout,
		local:   newLocalFallback(),
		now:     time.Now,
	}, nil
}

// Decide returns only a typed safe outcome. Dependency errors and derived keys
// stay inside the limiter and cannot reach routine telemetry or public errors.
func (l *Limiter) Decide(ctx context.Context, policy Policy, input Input) Decision {
	if err := policy.Validate(); err != nil {
		return Decision{Outcome: OutcomeUnavailable}
	}
	distributed, local, ok := l.entries(policy, input)
	if !ok {
		return Decision{Outcome: OutcomeUnavailable}
	}

	if l.circuitReady() {
		deadline, cancel := context.WithTimeout(ctx, l.timeout)
		allowed, err := l.store.Decide(deadline, distributed)
		cancel()
		if err == nil {
			l.recordDistributedSuccess()
			if allowed {
				return Decision{Allowed: true, Outcome: OutcomeAllowed}
			}
			return Decision{Outcome: OutcomeDenied, RetryAfter: policy.Window}
		}
		l.recordDistributedFailure()
	}

	allowed, available := l.local.decide(local, policy.LocalMaxKeys)
	if !available {
		return Decision{Outcome: OutcomeUnavailable}
	}
	if allowed {
		return Decision{Allowed: true, Outcome: OutcomeFallbackAllowed}
	}
	return Decision{Outcome: OutcomeFallbackDenied, RetryAfter: policy.Window}
}

func (l *Limiter) entries(policy Policy, input Input) ([]Entry, []Entry, bool) {
	distributed := make([]Entry, 0, len(policy.Rules))
	local := make([]Entry, 0, len(policy.Rules))
	for _, rule := range policy.Rules {
		value, ok := dimensionValue(rule.Dimension, policy, input)
		if !ok {
			return nil, nil, false
		}
		key := l.opaqueKey(policy.ID, rule.Dimension, value)
		distributed = append(distributed, Entry{Key: key, Limit: rule.Limit, Window: policy.Window})
		local = append(local, Entry{Key: key, Limit: rule.LocalLimit, Window: policy.Window})
	}
	return distributed, local, true
}

func dimensionValue(dimension Dimension, policy Policy, input Input) (string, bool) {
	switch dimension {
	case DimensionEndpoint:
		return policy.Endpoint, policy.Endpoint != ""
	case DimensionIdentifier:
		return bounded(input.Identifier, 512)
	case DimensionNetwork:
		return networkPrefix(input.ClientIP)
	case DimensionAnonymous:
		return bounded(input.AnonymousID, 256)
	case DimensionGlobal:
		return "global", true
	default:
		return "", false
	}
}

func bounded(value string, max int) (string, bool) {
	value = strings.TrimSpace(value)
	return value, value != "" && len(value) <= max
}

func networkPrefix(raw string) (string, bool) {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil {
		return "", false
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		return net.IP(ipv4).Mask(net.CIDRMask(24, 32)).String() + "/24", true
	}
	return ip.Mask(net.CIDRMask(56, 128)).String() + "/56", true
}

func (l *Limiter) opaqueKey(policyID string, dimension Dimension, value string) string {
	mac := hmac.New(sha256.New, l.hmacKey)
	_, _ = mac.Write([]byte("gradex-rate-limit-v1\x00"))
	_, _ = mac.Write([]byte(policyID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(dimension))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(value))
	digest := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return "gradex:rl:" + policyID + ":" + string(dimension) + ":" + digest
}

func (l *Limiter) circuitReady() bool {
	l.circuitMu.Lock()
	defer l.circuitMu.Unlock()
	return !l.now().Before(l.circuitOpenTill)
}

func (l *Limiter) recordDistributedSuccess() {
	l.circuitMu.Lock()
	defer l.circuitMu.Unlock()
	l.failures = 0
	l.circuitOpenTill = time.Time{}
}

func (l *Limiter) recordDistributedFailure() {
	l.circuitMu.Lock()
	defer l.circuitMu.Unlock()
	l.failures++
	if l.failures >= 3 {
		l.circuitOpenTill = l.now().Add(10 * l.timeout)
		l.failures = 0
	}
}
