// Package ratelimit makes privacy-safe admission decisions against versioned,
// layered policies. Its state is disposable; callers never use it as business
// or authorization authority.
package ratelimit

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

type Dimension string

const (
	DimensionEndpoint   Dimension = "endpoint"
	DimensionIdentifier Dimension = "identifier"
	DimensionNetwork    Dimension = "network"
	DimensionAnonymous  Dimension = "anonymous"
	DimensionGlobal     Dimension = "global"
)

var policyIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*-v[1-9][0-9]*$`)

type Rule struct {
	Dimension  Dimension
	Limit      int64
	LocalLimit int64
}

// Policy is an immutable-by-convention quota definition assembled at startup.
// LocalLimit must never exceed its distributed counterpart.
type Policy struct {
	ID           string
	Category     string
	Endpoint     string
	Window       time.Duration
	Rules        []Rule
	LocalMaxKeys int
}

func (p Policy) Validate() error {
	if !policyIDPattern.MatchString(p.ID) {
		return errors.New("rate-limit policy ID must be explicitly versioned")
	}
	if p.Category == "" || p.Endpoint == "" {
		return errors.New("rate-limit policy category and endpoint are required")
	}
	if p.Window < time.Millisecond {
		return errors.New("rate-limit policy window must be at least one millisecond")
	}
	if len(p.Rules) == 0 {
		return errors.New("rate-limit policy must define at least one dimension")
	}
	if p.LocalMaxKeys <= 0 {
		return errors.New("strict local fallback must have a positive key bound")
	}
	seen := make(map[Dimension]struct{}, len(p.Rules))
	for _, rule := range p.Rules {
		switch rule.Dimension {
		case DimensionEndpoint, DimensionIdentifier, DimensionNetwork,
			DimensionAnonymous, DimensionGlobal:
		default:
			return fmt.Errorf("unsupported rate-limit dimension %q", rule.Dimension)
		}
		if _, duplicate := seen[rule.Dimension]; duplicate {
			return fmt.Errorf("duplicate rate-limit dimension %q", rule.Dimension)
		}
		seen[rule.Dimension] = struct{}{}
		if rule.Limit <= 0 || rule.LocalLimit <= 0 || rule.LocalLimit > rule.Limit {
			return fmt.Errorf("unsafe limits for rate-limit dimension %q", rule.Dimension)
		}
	}
	return nil
}

// DevelopmentAdmissionPolicy is a deterministic conservative fixture, not a
// production capacity claim. Production policy values remain an explicit
// deployment approval.
func DevelopmentAdmissionPolicy(endpoint string) Policy {
	return Policy{
		ID:       endpoint + "-v1",
		Category: "PUBLIC_IDENTITY",
		Endpoint: endpoint,
		Window:   time.Minute,
		Rules: []Rule{
			{Dimension: DimensionEndpoint, Limit: 60, LocalLimit: 6},
			{Dimension: DimensionIdentifier, Limit: 6, LocalLimit: 2},
			{Dimension: DimensionNetwork, Limit: 30, LocalLimit: 4},
			{Dimension: DimensionAnonymous, Limit: 10, LocalLimit: 2},
			{Dimension: DimensionGlobal, Limit: 300, LocalLimit: 20},
		},
		LocalMaxKeys: 4096,
	}
}

func DevelopmentPolicySetReadPolicy() Policy {
	return Policy{
		ID:       "registration-policy-set-v1",
		Category: "PUBLIC_IDENTITY_READ",
		Endpoint: "registration-policy-set",
		Window:   time.Minute,
		Rules: []Rule{
			{Dimension: DimensionEndpoint, Limit: 120, LocalLimit: 12},
			{Dimension: DimensionNetwork, Limit: 60, LocalLimit: 8},
			{Dimension: DimensionAnonymous, Limit: 30, LocalLimit: 4},
			{Dimension: DimensionGlobal, Limit: 600, LocalLimit: 30},
		},
		LocalMaxKeys: 4096,
	}
}
