package identity

import "testing"

func student(status AccountStatus, credential CredentialState) Principal {
	return Principal{
		AccountID: "11111111-1111-4111-8111-111111111111",
		Role:      RoleStudent, Status: status, CredentialState: credential,
	}
}

// Requirement: a browser that authenticated but has not completed device trust
// holds only enough authority to finish, recover, or give up. In particular it
// does not hold protected learning.
func TestAPendingDeviceHoldsOnlyTheSelfServiceCapabilities(t *testing.T) {
	principal := student(StatusActive, CredentialActive)
	allowed := map[Capability]bool{
		CapDeviceManagement: true,
		CapPasswordChange:   true,
		CapSessionTerminate: true,
	}
	for _, capability := range AllCapabilities {
		decision := AuthorizeSessionDevice(principal, DeviceTrustPending, capability)
		want := allowed[capability]
		if decision.Allowed != want {
			t.Fatalf("pending device, %s: allowed = %v, want %v", capability, decision.Allowed, want)
		}
		if !want && Authorize(principal, capability).Allowed &&
			decision.Reason != DenyDeviceTrustRequired {
			t.Fatalf("pending device, %s: reason = %s, want DEVICE_TRUST_REQUIRED", capability, decision.Reason)
		}
	}
}

// A trusted browser behaves exactly as it did before this feature existed.
func TestATrustedDeviceChangesNothing(t *testing.T) {
	principal := student(StatusActive, CredentialActive)
	for _, capability := range AllCapabilities {
		base := Authorize(principal, capability)
		narrowed := AuthorizeSessionDevice(principal, DeviceTrustEstablished, capability)
		if base != narrowed {
			t.Fatalf("%s: trusted decision %+v differs from the account decision %+v",
				capability, narrowed, base)
		}
	}
}

// The rollout rule: a session that predates device policy keeps ordinary
// authority and loses exactly protected learning, so deployment is not a mass
// logout and is also not a permanent exemption.
func TestALegacySessionLosesOnlyProtectedLearning(t *testing.T) {
	principal := student(StatusActive, CredentialActive)
	for _, capability := range AllCapabilities {
		base := Authorize(principal, capability)
		narrowed := AuthorizeSessionDevice(principal, DeviceTrustLegacyUnbound, capability)
		if capability == CapLearningAccess {
			if narrowed.Allowed {
				t.Fatal("a legacy session reached protected learning without adopting a device")
			}
			if narrowed.Reason != DenyDeviceAdoptionRequired {
				t.Fatalf("legacy learning reason = %s, want DEVICE_ADOPTION_REQUIRED", narrowed.Reason)
			}
			continue
		}
		if base != narrowed {
			t.Fatalf("%s: legacy decision %+v differs from the account decision %+v",
				capability, narrowed, base)
		}
	}
}

// Device policy is a Student control. Staff authentication must be untouched,
// including for the LEGACY_UNBOUND rows the migration leaves on their sessions.
func TestStaffAuthenticationIsUnaffectedByDevicePolicy(t *testing.T) {
	for _, role := range []Role{RoleInstructor, RoleAdmin} {
		principal := Principal{
			AccountID: "22222222-2222-4222-8222-222222222222",
			Role:      role, Status: StatusActive, CredentialState: CredentialActive,
		}
		for _, trust := range []SessionDeviceTrust{
			DeviceTrustNotApplicable, DeviceTrustPending,
			DeviceTrustEstablished, DeviceTrustLegacyUnbound,
		} {
			for _, capability := range AllCapabilities {
				base := Authorize(principal, capability)
				narrowed := AuthorizeSessionDevice(principal, trust, capability)
				if base != narrowed {
					t.Fatalf("%s/%s/%s: %+v differs from the account decision %+v",
						role, trust, capability, narrowed, base)
				}
			}
		}
	}
}

// Account-level refusals still win. A suspended Student on a perfectly trusted
// device is refused for suspension, which is what monitoring has to see.
func TestAccountRefusalsOutrankDeviceRefusals(t *testing.T) {
	suspended := student(StatusSuspended, CredentialActive)
	decision := AuthorizeSessionDevice(suspended, DeviceTrustPending, CapDeviceManagement)
	if decision.Allowed {
		t.Fatal("a suspended Account reached its device settings")
	}
	if decision.Reason != DenyAccountSuspended {
		t.Fatalf("reason = %s, want ACCOUNT_SUSPENDED", decision.Reason)
	}

	restricted := student(StatusActive, CredentialChangeRequired)
	if got := AuthorizeSessionDevice(restricted, DeviceTrustEstablished, CapLearningAccess); got.Reason != DenyPasswordChangeRequired {
		t.Fatalf("restricted reason = %s, want PASSWORD_CHANGE_REQUIRED", got.Reason)
	}
	// And a mandatory password change stays reachable from an untrusted device,
	// or a Student in that state on a new browser would have no route at all.
	if !AuthorizeSessionDevice(restricted, DeviceTrustPending, CapPasswordChange).Allowed {
		t.Fatal("a restricted Student could not change their password from a new device")
	}
}

// An unknown or absent trust state narrows rather than widens.
func TestUnknownTrustStatesFailClosed(t *testing.T) {
	principal := student(StatusActive, CredentialActive)
	for _, trust := range []SessionDeviceTrust{"", "SOMETHING_ELSE", DeviceTrustNotApplicable} {
		if AuthorizeSessionDevice(principal, trust, CapLearningAccess).Allowed {
			t.Fatalf("trust state %q reached protected learning", trust)
		}
		if !AuthorizeSessionDevice(principal, trust, CapSessionTerminate).Allowed {
			t.Fatalf("trust state %q could not log out", trust)
		}
	}
}
