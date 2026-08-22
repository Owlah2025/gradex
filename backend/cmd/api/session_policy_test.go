package main

import (
	"testing"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/ratelimit"
)

func TestProductionCompositionUsesProductionSessionPolicies(t *testing.T) {
	policies := sessionPolicies(config.EnvProduction)
	if policies["session-bootstrap"].ID != ratelimit.ProductionAnonymousBootstrapPolicy().ID {
		t.Fatal("production composition did not select production bootstrap policy")
	}
	if policies["sessions"].ID != ratelimit.ProductionLoginPolicy().ID {
		t.Fatal("production composition did not select production login policy")
	}
}

func TestDevelopmentCompositionRetainsDevelopmentSessionPolicies(t *testing.T) {
	policies := sessionPolicies(config.EnvDevelopment)
	if policies["session-bootstrap"].ID != ratelimit.DevelopmentAnonymousBootstrapPolicy().ID ||
		policies["sessions"].ID != ratelimit.DevelopmentLoginPolicy().ID {
		t.Fatal("development composition did not retain development policies")
	}
}
