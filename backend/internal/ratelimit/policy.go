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
	DimensionSourceAddr Dimension = "source_address"
	DimensionAnonymous  Dimension = "anonymous"
	DimensionGlobal     Dimension = "global"
)

var policyIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*-v[1-9][0-9]*$`)

type Rule struct {
	Dimension  Dimension
	Limit      int64
	LocalLimit int64
	Burst      int64
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
	FailClosed   bool
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
		case DimensionEndpoint, DimensionIdentifier, DimensionNetwork, DimensionSourceAddr,
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
		if rule.Burst < 0 {
			return fmt.Errorf("invalid burst for rate-limit dimension %q", rule.Dimension)
		}
	}
	return nil
}

const (
	ProtectedLearningProgressSourceRequestsPerMinute int64 = 1200
	ProtectedLearningProgressSourceBurst             int64 = 120
)

func ProtectedLearningProgressSourcePolicy() Policy {
	return Policy{
		ID:       "learning-progress-source-v1",
		Category: "PROTECTED_LEARNING",
		Endpoint: "learning-progress-source",
		Window:   time.Minute,
		Rules: []Rule{{
			Dimension: DimensionSourceAddr, Limit: ProtectedLearningProgressSourceRequestsPerMinute,
			LocalLimit: ProtectedLearningProgressSourceRequestsPerMinute, Burst: ProtectedLearningProgressSourceBurst,
		}},
		LocalMaxKeys: 8192,
		FailClosed:   true,
	}
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

func DevelopmentAnonymousBootstrapPolicy() Policy {
	return Policy{
		ID:       "anonymous-session-bootstrap-v1",
		Category: "PUBLIC_IDENTITY_CAPABILITY",
		Endpoint: "session-bootstrap",
		Window:   time.Minute,
		Rules: []Rule{
			{Dimension: DimensionEndpoint, Limit: 120, LocalLimit: 12},
			{Dimension: DimensionNetwork, Limit: 60, LocalLimit: 8},
			{Dimension: DimensionGlobal, Limit: 600, LocalLimit: 30},
		},
		LocalMaxKeys: 4096,
	}
}

// DevelopmentSessionPolicy is a conservative fixture for authenticated
// session traffic. The identifier input is already a one-way digest of the
// presented opaque authority and is HMAC-keyed again by the limiter.
// DevelopmentPasswordResetCompletionPolicy bounds the unauthenticated reset
// completion endpoint far more tightly than the generic admission policy.
//
// Completion is the only anonymous route that can reach Argon2id, so it is the
// cheapest request an attacker can send for the most server CPU. The generic
// policy is a poor fit for it: its identifier dimension keys on the presented
// token, and an attacker varies that freely, so that dimension contributes no
// protection at all against random-token flooding. The limits that actually
// bind here are network, endpoint, and global, and they are set well below the
// admission defaults.
//
// A service-side preflight rejects unknown tokens before any hashing, so these
// limits are the second line rather than the only one.
func DevelopmentPasswordResetCompletionPolicy() Policy {
	return Policy{
		ID:       "password-resets-v1",
		Category: "PUBLIC_IDENTITY_CREDENTIAL",
		Endpoint: "password-resets",
		Window:   time.Minute,
		Rules: []Rule{
			{Dimension: DimensionEndpoint, Limit: 20, LocalLimit: 3},
			{Dimension: DimensionIdentifier, Limit: 5, LocalLimit: 2},
			{Dimension: DimensionNetwork, Limit: 10, LocalLimit: 2},
			{Dimension: DimensionAnonymous, Limit: 5, LocalLimit: 2},
			{Dimension: DimensionGlobal, Limit: 60, LocalLimit: 8},
		},
		LocalMaxKeys: 4096,
	}
}

func DevelopmentSessionPolicy(endpoint string) Policy {
	return Policy{
		ID:       endpoint + "-v1",
		Category: "AUTHENTICATED_SESSION",
		Endpoint: endpoint,
		Window:   time.Minute,
		Rules: []Rule{
			{Dimension: DimensionEndpoint, Limit: 120, LocalLimit: 12},
			{Dimension: DimensionIdentifier, Limit: 30, LocalLimit: 4},
			{Dimension: DimensionNetwork, Limit: 60, LocalLimit: 8},
			{Dimension: DimensionGlobal, Limit: 600, LocalLimit: 30},
		},
		LocalMaxKeys: 4096,
	}
}

// DevelopmentLoginPolicy adds the anonymous-browser layer to the keyed
// normalized-email, network, endpoint, and global layers.
func DevelopmentLoginPolicy() Policy {
	return Policy{
		ID:       "sessions-v1",
		Category: "PUBLIC_AUTHENTICATION",
		Endpoint: "sessions",
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

// DevelopmentStaffInvitationPolicy is a conservative fixture for staff
// invitation endpoints. Creation is admin-only so the limit is generous;
// preview and completion are anonymous and must be tighter to prevent
// bearer brute-forcing.
func DevelopmentStaffInvitationPolicy(endpoint string) Policy {
	return Policy{
		ID:       endpoint + "-v1",
		Category: "STAFF_LIFECYCLE",
		Endpoint: endpoint,
		Window:   time.Minute,
		Rules: []Rule{
			{Dimension: DimensionEndpoint, Limit: 20, LocalLimit: 3},
			{Dimension: DimensionIdentifier, Limit: 5, LocalLimit: 2},
			{Dimension: DimensionNetwork, Limit: 10, LocalLimit: 2},
			{Dimension: DimensionGlobal, Limit: 60, LocalLimit: 8},
		},
		LocalMaxKeys: 4096,
	}
}

// ProtectedLearningReportsPerHour is FR-032's report-submission ceiling. It is
// deliberately separate from the duplicate constraint, which is a database
// index: a limit alone permits five identical reports about one Lesson, and a
// constraint alone permits five hundred about five hundred Lessons (R-11).
const ProtectedLearningReportsPerHour int64 = 5

// ProtectedLearningReportPolicy bounds one Student's report submissions to
// five per hour, keyed on the authenticated Account alone.
//
// The Account is the whole key on purpose. Adding the Course or the report
// target would let one Student open a fresh quota per Course, which is the
// abuse this limit exists to bound; adding the session would let the same
// Student reset the quota by logging in again. Neither the request body nor
// the encrypted report context contributes anything to the key — the context
// is not even decrypted before this decision runs.
//
// The local fallback carries the identical limit, so a Redis outage narrows
// the quota's scope to one process but never widens it.
func ProtectedLearningReportPolicy() Policy {
	return Policy{
		ID:       "learning-report-v1",
		Category: "PROTECTED_LEARNING",
		Endpoint: "learning-report",
		Window:   time.Hour,
		Rules: []Rule{
			{Dimension: DimensionIdentifier, Limit: ProtectedLearningReportsPerHour, LocalLimit: ProtectedLearningReportsPerHour},
		},
		LocalMaxKeys: 8192,
	}
}

// ProtectedLearningProgressPolicy limits one Student's writes for one stable
// Lesson. The key is assembled only from authenticated server state, never
// from a request body.
func ProtectedLearningProgressPolicy() Policy {
	return Policy{
		ID:       "learning-progress-v1",
		Category: "PROTECTED_LEARNING",
		Endpoint: "learning-progress",
		Window:   time.Minute,
		Rules: []Rule{
			{Dimension: DimensionIdentifier, Limit: 12, LocalLimit: 12},
		},
		LocalMaxKeys: 8192,
	}
}
