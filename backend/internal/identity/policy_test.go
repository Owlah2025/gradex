package identity

import "testing"

func principal(role Role, status AccountStatus, cred CredentialState) Principal {
	return Principal{
		AccountID:       "11111111-1111-1111-1111-111111111111",
		Role:            role,
		Status:          status,
		CredentialState: cred,
	}
}

// The restricted bootstrap principal is the reason this policy exists. It
// carries the ADMIN role and must still be refused every Admin, financial,
// security, retention, provider, and content-management capability until its
// password is changed.
func TestRestrictedBootstrapAdminIsDeniedEverythingButPasswordChange(t *testing.T) {
	p := principal(RoleAdmin, StatusActive, CredentialChangeRequired)

	allowed := map[Capability]bool{
		CapPasswordChange:   true,
		CapSessionTerminate: true,
	}

	for _, c := range AllCapabilities {
		decision := Authorize(p, c)
		want := allowed[c]

		if decision.Allowed != want {
			t.Errorf("Authorize(restricted admin, %s).Allowed = %v, want %v", c, decision.Allowed, want)
		}
		if !want && decision.Reason != DenyPasswordChangeRequired {
			t.Errorf("Authorize(restricted admin, %s).Reason = %q, want %q",
				c, decision.Reason, DenyPasswordChangeRequired)
		}
	}
}

// Suspension outranks the restricted principal's own password-change
// permission. A suspended Account must not be able to act at all — including
// to rehabilitate itself.
func TestSuspensionOverridesEveryCapability(t *testing.T) {
	for _, role := range []Role{RoleStudent, RoleInstructor, RoleAdmin} {
		for _, cred := range []CredentialState{CredentialActive, CredentialChangeRequired} {
			p := principal(role, StatusSuspended, cred)
			for _, c := range AllCapabilities {
				decision := Authorize(p, c)
				if decision.Allowed {
					t.Errorf("suspended %s with credential %s was allowed %s", role, cred, c)
				}
				if decision.Reason != DenyAccountSuspended {
					t.Errorf("suspended %s/%s/%s reason = %q, want %q",
						role, cred, c, decision.Reason, DenyAccountSuspended)
				}
			}
		}
	}
}

// The ordering that matters: an ordinary Admin gets Admin authority, and the
// same Account one state-change earlier does not.
func TestPasswordChangeUnlocksNormalAdminAuthority(t *testing.T) {
	restricted := principal(RoleAdmin, StatusActive, CredentialChangeRequired)
	unlocked := principal(RoleAdmin, StatusActive, CredentialActive)

	if Authorize(restricted, CapAdminOperations).Allowed {
		t.Fatal("a restricted Admin was allowed Admin operations")
	}
	if !Authorize(unlocked, CapAdminOperations).Allowed {
		t.Fatal("an ordinary Admin was denied Admin operations")
	}
}

func TestRoleCapabilityMatrix(t *testing.T) {
	// The complete expected matrix for Accounts in good standing. Written out
	// rather than derived, so a policy change has to be restated here
	// deliberately instead of silently agreeing with itself.
	matrix := map[Role]map[Capability]bool{
		RoleAdmin: {
			CapPasswordChange: true, CapSessionTerminate: true,
			CapAdminOperations: true, CapFinancialOperations: true,
			CapSecurityOperations: true, CapRetentionOperations: true,
			CapProviderOperations: true, CapContentManagement: true,
			CapLearningAccess: false,
			CapCatalogPublish: true, CapCatalogPricing: true, CapCatalogTaxonomy: true,
		},
		RoleInstructor: {
			CapPasswordChange: true, CapSessionTerminate: true,
			CapContentManagement: true,
			CapAdminOperations:   false, CapFinancialOperations: false,
			CapSecurityOperations: false, CapRetentionOperations: false,
			CapProviderOperations: false, CapLearningAccess: false,
			CapCatalogPublish: false, CapCatalogPricing: false, CapCatalogTaxonomy: false,
		},
		RoleStudent: {
			CapPasswordChange: true, CapSessionTerminate: true,
			CapLearningAccess:  true,
			CapAdminOperations: false, CapFinancialOperations: false,
			CapSecurityOperations: false, CapRetentionOperations: false,
			CapProviderOperations: false, CapContentManagement: false,
			CapCatalogPublish: false, CapCatalogPricing: false, CapCatalogTaxonomy: false,
		},
	}

	for role, expected := range matrix {
		if len(expected) != len(AllCapabilities) {
			t.Fatalf("the %s row covers %d capabilities but there are %d; the matrix is not total",
				role, len(expected), len(AllCapabilities))
		}
		p := principal(role, StatusActive, CredentialActive)
		for _, c := range AllCapabilities {
			want, stated := expected[c]
			if !stated {
				t.Fatalf("the %s row does not state a decision for %s", role, c)
			}
			if got := Authorize(p, c).Allowed; got != want {
				t.Errorf("Authorize(%s, %s).Allowed = %v, want %v", role, c, got, want)
			}
		}
	}
}

// An unverified Account can only manage its own credential and session. It is
// not yet a participant in anything else.
func TestUnverifiedAccountIsLimited(t *testing.T) {
	p := principal(RoleStudent, StatusPendingVerification, CredentialActive)

	if !Authorize(p, CapPasswordChange).Allowed {
		t.Error("an unverified Account could not change its password")
	}
	if !Authorize(p, CapSessionTerminate).Allowed {
		t.Error("an unverified Account could not end its session")
	}
	if d := Authorize(p, CapLearningAccess); d.Allowed || d.Reason != DenyAccountUnverified {
		t.Errorf("unverified learning access = %+v, want denial with %q", d, DenyAccountUnverified)
	}
}

// Deny-by-default, checked structurally: an incoherent principal — one the
// schema should never have produced — is refused rather than guessed at.
func TestIncoherentPrincipalIsDenied(t *testing.T) {
	broken := []struct {
		name string
		p    Principal
	}{
		{"no account ID", Principal{Role: RoleAdmin, Status: StatusActive, CredentialState: CredentialActive}},
		{"unknown role", principal("SUPERUSER", StatusActive, CredentialActive)},
		{"unknown status", principal(RoleAdmin, "DELETED", CredentialActive)},
		{"unknown credential state", principal(RoleAdmin, StatusActive, "EXPIRED")},
		{"zero value", Principal{}},
	}

	for _, tc := range broken {
		t.Run(tc.name, func(t *testing.T) {
			for _, c := range AllCapabilities {
				d := Authorize(tc.p, c)
				if d.Allowed {
					t.Errorf("an incoherent principal was allowed %s", c)
				}
				if d.Reason != DenyIncoherentPrincipal {
					t.Errorf("reason = %q, want %q", d.Reason, DenyIncoherentPrincipal)
				}
			}
		})
	}
}

// A capability the policy has never heard of is denied, not allowed. This is
// the property that makes adding a route safe: forgetting to extend the policy
// fails closed.
func TestUnknownCapabilityIsDenied(t *testing.T) {
	p := principal(RoleAdmin, StatusActive, CredentialActive)
	d := Authorize(p, Capability("DELETE_EVERYTHING"))
	if d.Allowed {
		t.Fatal("an unknown capability was allowed")
	}
	if d.Reason != DenyUnknownCapability {
		t.Errorf("reason = %q, want %q", d.Reason, DenyUnknownCapability)
	}
}

// Totality: every (role, status, credential, capability) combination produces a
// decision with a reason, and no allow ever carries a deny reason. Without
// this, a policy could pass every case above and still fall through to an
// unlabelled default on a combination nobody wrote a test for.
func TestPolicyIsTotal(t *testing.T) {
	roles := []Role{RoleStudent, RoleInstructor, RoleAdmin}
	statuses := []AccountStatus{StatusPendingVerification, StatusActive, StatusSuspended}
	creds := []CredentialState{CredentialActive, CredentialChangeRequired}

	combinations := 0
	for _, role := range roles {
		for _, status := range statuses {
			for _, cred := range creds {
				for _, c := range AllCapabilities {
					combinations++
					d := Authorize(principal(role, status, cred), c)
					if d.Allowed && d.Reason != DenyNone {
						t.Errorf("%s/%s/%s/%s: allowed but carries reason %q", role, status, cred, c, d.Reason)
					}
					if !d.Allowed && d.Reason == DenyNone {
						t.Errorf("%s/%s/%s/%s: denied with no reason", role, status, cred, c)
					}
				}
			}
		}
	}

	if want := len(roles) * len(statuses) * len(creds) * len(AllCapabilities); combinations != want {
		t.Fatalf("covered %d combinations, expected %d", combinations, want)
	}
}
