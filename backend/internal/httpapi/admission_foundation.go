package httpapi

import (
	"errors"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

var requiredAdmissionPolicyEndpoints = [...]string{
	"session-bootstrap",
	"registration-policy-set",
	"student-registrations",
	"email-verification-requests",
	"email-verifications",
	"password-reset-requests",
	"password-resets",
}

// AdmissionFoundation is the fail-closed dependency set shared by every
// public Identity command. Construction validates the complete set before the
// bootstrap route is mounted; Student mutation routes are added separately.
type AdmissionFoundation struct {
	security         *anonymousSecurity
	service          admissionCommands
	recovery         recoveryCommands
	policies         identity.PolicySetResolver
	limiter          *ratelimit.Limiter
	endpointPolicies map[string]ratelimit.Policy
}

type AdmissionFoundationOptions struct {
	PublicOrigin        string
	CookieSigningKey    []byte
	CSRFKey             []byte
	AnonymousSessionTTL time.Duration
	Policies            identity.PolicySetResolver
	Service             admissionCommands
	Recovery            recoveryCommands
	Limiter             *ratelimit.Limiter
	EndpointPolicies    map[string]ratelimit.Policy
}

func NewAdmissionFoundation(options AdmissionFoundationOptions) (*AdmissionFoundation, error) {
	security, err := newAnonymousSecurity(
		options.PublicOrigin,
		options.CookieSigningKey,
		options.CSRFKey,
		options.AnonymousSessionTTL,
	)
	if err != nil {
		return nil, err
	}
	if options.Policies == nil || options.Service == nil ||
		options.Recovery == nil || options.Limiter == nil {
		return nil, errors.New("admission foundation dependencies are required")
	}
	if len(options.EndpointPolicies) == 0 {
		return nil, errors.New("admission endpoint policies are required")
	}
	endpointPolicies := make(map[string]ratelimit.Policy, len(options.EndpointPolicies))
	for endpoint, policy := range options.EndpointPolicies {
		if endpoint == "" || endpoint != policy.Endpoint {
			return nil, errors.New("admission endpoint policy key does not match its endpoint")
		}
		if err := policy.Validate(); err != nil {
			return nil, err
		}
		endpointPolicies[endpoint] = policy
	}
	for _, endpoint := range requiredAdmissionPolicyEndpoints {
		if _, configured := endpointPolicies[endpoint]; !configured {
			return nil, errors.New("required admission endpoint policy is missing")
		}
	}
	return &AdmissionFoundation{
		security:         security,
		service:          options.Service,
		recovery:         options.Recovery,
		policies:         options.Policies,
		limiter:          options.Limiter,
		endpointPolicies: endpointPolicies,
	}, nil
}
