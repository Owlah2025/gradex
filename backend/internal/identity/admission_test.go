package identity

import (
	"context"
	"errors"
	"testing"
)

func TestStaticPolicyResolverDistinguishesUnavailableFromStaleClientSet(t *testing.T) {
	resolver := testPolicyResolver(t)
	if _, err := resolver.Resolve(
		context.Background(), "old-set", LocaleEnglish,
	); !errors.Is(err, ErrPolicySetStale) {
		t.Fatalf("stale set error = %v, want ErrPolicySetStale", err)
	}
	if _, err := resolver.Current(
		context.Background(), "fr",
	); !errors.Is(err, ErrPolicySetUnavailable) {
		t.Fatalf("unsupported locale error = %v, want ErrPolicySetUnavailable", err)
	}
}

func TestStaticPolicyResolverRejectsLocalizedVersionDrift(t *testing.T) {
	english, arabic := testPolicySets()
	arabic.Policies[0].Version = "different-v1"
	if _, err := NewStaticPolicySetResolver(english, arabic); err == nil {
		t.Fatal("localized policy versions drifted without failing startup")
	}
}

func testPolicyResolver(t *testing.T) *StaticPolicySetResolver {
	t.Helper()
	english, arabic := testPolicySets()
	resolver, err := NewStaticPolicySetResolver(english, arabic)
	if err != nil {
		t.Fatalf("constructing policy resolver: %v", err)
	}
	return resolver
}

func testPolicySets() (RegistrationPolicySet, RegistrationPolicySet) {
	policies := []RegistrationPolicy{
		{
			Kind: PolicyPrivacyNotice, Version: "privacy-v1",
			Label: "Privacy notice", URL: "/legal/privacy",
		},
		{
			Kind: PolicyTermsOfService, Version: "terms-v1",
			Label: "Terms of service", URL: "/legal/terms",
		},
	}
	english := RegistrationPolicySet{
		ID: "registration-v1", Locale: LocaleEnglish,
		Policies: append([]RegistrationPolicy(nil), policies...),
	}
	arabic := RegistrationPolicySet{
		ID: "registration-v1", Locale: LocaleArabic,
		Policies: append([]RegistrationPolicy(nil), policies...),
	}
	arabic.Policies[0].Label = "إشعار الخصوصية"
	arabic.Policies[1].Label = "شروط الخدمة"
	return english, arabic
}
