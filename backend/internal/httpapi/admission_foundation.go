package httpapi

import (
	"errors"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

// AdmissionFoundation is the fail-closed dependency set shared by every
// public Identity command. Construction validates the complete set before the
// bootstrap route is mounted; Student mutation routes are added separately.
type AdmissionFoundation struct {
	security         *anonymousSecurity
	service          admissionCommands
	policies         identity.PolicySetResolver
	limiter          *ratelimit.Limiter
	endpointPolicies map[string]ratelimit.Policy
}

type AdmissionFoundationOptions struct {
	PublicOrigin        string
	CookieSigningKey    string
	CSRFKey             string
	AnonymousSessionTTL time.Duration
	Policies            identity.PolicySetResolver
	Service             admissionCommands
	Limiter             *ratelimit.Limiter
	EndpointPolicies    map[string]ratelimit.Policy
}

func NewAdmissionFoundation(options AdmissionFoundationOptions) (*AdmissionFoundation, error) {
	security, err := newAnonymousSecurity(
		options.PublicOrigin,
		[]byte(options.CookieSigningKey),
		[]byte(options.CSRFKey),
		options.AnonymousSessionTTL,
	)
	if err != nil {
		return nil, err
	}
	if options.Policies == nil || options.Service == nil || options.Limiter == nil {
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
	return &AdmissionFoundation{
		security:         security,
		service:          options.Service,
		policies:         options.Policies,
		limiter:          options.Limiter,
		endpointPolicies: endpointPolicies,
	}, nil
}
