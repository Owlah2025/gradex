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

func TestApprovedPolicyResolverReturnsStableBilingualCanonicalMetadata(t *testing.T) {
	resolver, err := NewApprovedPolicySetResolver("https://gradex.example", ApprovedPolicySetID)
	if err != nil {
		t.Fatalf("constructing approved resolver: %v", err)
	}
	for _, tc := range []struct {
		locale     Locale
		privacyURL string
		termsURL   string
	}{
		{LocaleArabic, "https://gradex.example/ar/privacy", "https://gradex.example/ar/terms"},
		{LocaleEnglish, "https://gradex.example/en/privacy", "https://gradex.example/en/terms"},
	} {
		set, currentErr := resolver.Current(context.Background(), tc.locale)
		if currentErr != nil {
			t.Fatalf("resolving %s policy set: %v", tc.locale, currentErr)
		}
		if set.ID != ApprovedPolicySetID || set.Version != ApprovedPolicySetVersion ||
			set.EffectiveDate != ApprovedPolicyEffectiveDate || set.MinimumAge != ApprovedPolicyMinimumAge ||
			set.PrimaryLocale != LocaleArabic || set.Locale != tc.locale {
			t.Fatalf("unexpected approved metadata for %s: %+v", tc.locale, set)
		}
		byKind := map[PolicyKind]RegistrationPolicy{}
		for _, policy := range set.Policies {
			byKind[policy.Kind] = policy
		}
		if byKind[PolicyPrivacyNotice].Version != ApprovedPrivacyVersion ||
			byKind[PolicyPrivacyNotice].URL != tc.privacyURL ||
			byKind[PolicyTermsOfService].Version != ApprovedTermsVersion ||
			byKind[PolicyTermsOfService].URL != tc.termsURL {
			t.Fatalf("unexpected localized policy URLs/versions for %s: %+v", tc.locale, byKind)
		}
	}
}

func TestApprovedPolicyResolverFailsClosedOnUnapprovedConfiguration(t *testing.T) {
	for name, values := range map[string][2]string{
		"stale set":        {"https://gradex.example", "old-set"},
		"plain HTTP":       {"http://gradex.example", ApprovedPolicySetID},
		"origin with path": {"https://gradex.example/path", ApprovedPolicySetID},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewApprovedPolicySetResolver(values[0], values[1]); err == nil {
				t.Fatal("unsafe or stale production policy configuration was accepted")
			}
		})
	}
	resolver, err := NewApprovedPolicySetResolver("https://gradex.example", ApprovedPolicySetID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background(), "old-set", LocaleArabic); !errors.Is(err, ErrPolicySetStale) {
		t.Fatalf("stale registration set error = %v", err)
	}
	if _, err := resolver.Current(context.Background(), Locale("fr")); !errors.Is(err, ErrPolicySetUnavailable) {
		t.Fatalf("unsupported locale error = %v", err)
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
