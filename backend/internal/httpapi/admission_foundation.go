package httpapi

import (
	"errors"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

var requiredStudentAdmissionPolicyEndpoints = [...]string{
	"session-bootstrap",
	"registration-policy-set",
	"student-registrations",
	"email-verification-requests",
	"email-verifications",
	"purchase-requests",
}

var requiredRecoveryPolicyEndpoints = [...]string{
	"password-reset-requests",
	"password-resets",
}

// AdmissionFoundation owns the canonical anonymous browser boundary and the
// Student-registration dependencies. Recovery deliberately composes through a
// separate RecoveryFoundation so closing registration cannot close recovery.
type AdmissionFoundation struct {
	security         *anonymousSecurity
	service          admissionCommands
	policies         identity.PolicySetResolver
	limiter          *ratelimit.Limiter
	endpointPolicies map[string]ratelimit.Policy
}

// RecoveryFoundation mounts credential recovery through the same anonymous
// security, CSRF, and rate-limit boundary as other public Identity commands.
// It has no registration dependency or authority.
type RecoveryFoundation struct {
	admission *AdmissionFoundation
	recovery  recoveryCommands
}

type AdmissionFoundationOptions struct {
	PublicOrigin        string
	CookieSigningKey    []byte
	CSRFKey             []byte
	AnonymousSessionTTL time.Duration
	Policies            identity.PolicySetResolver
	Service             admissionCommands
	Limiter             *ratelimit.Limiter
	EndpointPolicies    map[string]ratelimit.Policy
}

// PurchaseAdmissionFoundationOptions is the deliberately smaller anonymous
// boundary needed when public purchase requests are available but Student
// registration is disabled. It shares the canonical admission cookie, CSRF,
// and rate-decision primitives without composing unrelated registration
// services or mounting their routes.
type PurchaseAdmissionFoundationOptions struct {
	PublicOrigin        string
	CookieSigningKey    []byte
	CSRFKey             []byte
	AnonymousSessionTTL time.Duration
	Limiter             *ratelimit.Limiter
	EndpointPolicies    map[string]ratelimit.Policy
}

type RecoveryFoundationOptions struct {
	Admission *AdmissionFoundation
	Recovery  recoveryCommands
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
	if options.Policies == nil || options.Service == nil || options.Limiter == nil {
		return nil, errors.New("admission foundation dependencies are required")
	}
	endpointPolicies, err := validatedEndpointPolicies(options.EndpointPolicies, requiredStudentAdmissionPolicyEndpoints[:])
	if err != nil {
		return nil, err
	}
	return &AdmissionFoundation{
		security:         security,
		service:          options.Service,
		policies:         options.Policies,
		limiter:          options.Limiter,
		endpointPolicies: endpointPolicies,
	}, nil
}

// NewPurchaseAdmissionFoundation creates the canonical anonymous boundary for
// public purchase requests and any separately composed anonymous capability.
// It never carries Student-registration dependencies or mounts their routes.
func NewPurchaseAdmissionFoundation(options PurchaseAdmissionFoundationOptions) (*AdmissionFoundation, error) {
	security, err := newAnonymousSecurity(
		options.PublicOrigin,
		options.CookieSigningKey,
		options.CSRFKey,
		options.AnonymousSessionTTL,
	)
	if err != nil {
		return nil, err
	}
	if options.Limiter == nil {
		return nil, errors.New("purchase admission limiter is required")
	}
	endpointPolicies, err := validatedEndpointPolicies(options.EndpointPolicies, []string{"purchase-requests"})
	if err != nil {
		return nil, err
	}
	return &AdmissionFoundation{
		security:         security,
		limiter:          options.Limiter,
		endpointPolicies: endpointPolicies,
	}, nil
}

// NewRecoveryFoundation validates that recovery can only be mounted through a
// complete anonymous boundary with both recovery endpoint policies. A missing
// dependency is a construction error, never a silent route omission.
func NewRecoveryFoundation(options RecoveryFoundationOptions) (*RecoveryFoundation, error) {
	if options.Admission == nil || options.Recovery == nil {
		return nil, errors.New("recovery foundation dependencies are required")
	}
	for _, endpoint := range requiredRecoveryPolicyEndpoints {
		if _, configured := options.Admission.endpointPolicies[endpoint]; !configured {
			return nil, errors.New("required recovery endpoint policy is missing")
		}
	}
	return &RecoveryFoundation{admission: options.Admission, recovery: options.Recovery}, nil
}

func validatedEndpointPolicies(
	policies map[string]ratelimit.Policy,
	required []string,
) (map[string]ratelimit.Policy, error) {
	if len(policies) == 0 {
		return nil, errors.New("admission endpoint policies are required")
	}
	endpointPolicies := make(map[string]ratelimit.Policy, len(policies))
	for endpoint, policy := range policies {
		if endpoint == "" || endpoint != policy.Endpoint {
			return nil, errors.New("admission endpoint policy key does not match its endpoint")
		}
		if err := policy.Validate(); err != nil {
			return nil, err
		}
		endpointPolicies[endpoint] = policy
	}
	for _, endpoint := range required {
		if _, configured := endpointPolicies[endpoint]; !configured {
			return nil, errors.New("required admission endpoint policy is missing")
		}
	}
	return endpointPolicies, nil
}

// RateLimiter exposes the already composed public-rate boundary to another
// public surface. It does not grant authority; callers still supply and
// validate their own versioned endpoint policy.
func (f *AdmissionFoundation) RateLimiter() *ratelimit.Limiter {
	if f == nil {
		return nil
	}
	return f.limiter
}
