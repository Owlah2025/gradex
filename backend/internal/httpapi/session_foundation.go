package httpapi

import (
	"context"
	"errors"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

var requiredSessionPolicyEndpoints = [...]string{
	"session-bootstrap",
	"sessions",
	"session-resolution",
	"session-renewals",
	"session-logout",
}

type sessionCommands interface {
	Login(context.Context, identity.LoginRequest) (identity.SessionGrant, error)
	Resolve(
		context.Context,
		string,
		identity.CredentialUseKind,
		string,
	) (identity.SessionView, error)
	Renew(context.Context, identity.SessionMutation) (identity.SessionGrant, error)
	Logout(context.Context, identity.SessionMutation) error
}

// SessionFoundation is the validated dependency set for the four
// authenticated-session routes and their anonymous login bootstrap.
type SessionFoundation struct {
	security         *anonymousSecurity
	repository       sessionCommands
	authenticator    *auth.SessionAuthenticator
	limiter          *ratelimit.Limiter
	endpointPolicies map[string]ratelimit.Policy
}

type SessionFoundationOptions struct {
	PublicOrigin        string
	CookieSigningKey    []byte
	AnonymousCSRFKey    []byte
	AnonymousSessionTTL time.Duration
	Repository          sessionCommands
	Limiter             *ratelimit.Limiter
	EndpointPolicies    map[string]ratelimit.Policy
}

func NewSessionFoundation(options SessionFoundationOptions) (*SessionFoundation, error) {
	security, err := newAnonymousSecurity(
		options.PublicOrigin,
		options.CookieSigningKey,
		options.AnonymousCSRFKey,
		options.AnonymousSessionTTL,
	)
	if err != nil {
		return nil, err
	}
	if options.Repository == nil || options.Limiter == nil {
		return nil, errors.New("session foundation dependencies are required")
	}
	endpointPolicies := make(map[string]ratelimit.Policy, len(options.EndpointPolicies))
	for endpoint, policy := range options.EndpointPolicies {
		if endpoint == "" || endpoint != policy.Endpoint {
			return nil, errors.New("session endpoint policy key does not match its endpoint")
		}
		if err := policy.Validate(); err != nil {
			return nil, err
		}
		endpointPolicies[endpoint] = policy
	}
	for _, endpoint := range requiredSessionPolicyEndpoints {
		if _, configured := endpointPolicies[endpoint]; !configured {
			return nil, errors.New("required session endpoint policy is missing")
		}
	}
	authenticator, err := auth.NewSessionAuthenticator(options.Repository)
	if err != nil {
		return nil, err
	}
	return &SessionFoundation{
		security:         security,
		repository:       options.Repository,
		authenticator:    authenticator,
		limiter:          options.Limiter,
		endpointPolicies: endpointPolicies,
	}, nil
}
