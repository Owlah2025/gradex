package identity

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

type Locale string

const (
	LocaleArabic  Locale = "ar"
	LocaleEnglish Locale = "en"
)

func (l Locale) Valid() bool { return l == LocaleArabic || l == LocaleEnglish }

type PolicyKind string

const (
	PolicyPrivacyNotice  PolicyKind = "PRIVACY_NOTICE"
	PolicyTermsOfService PolicyKind = "TERMS_OF_SERVICE"
)

type RegistrationPolicy struct {
	Kind    PolicyKind
	Version string
	Label   string
	URL     string
}

type RegistrationPolicySet struct {
	ID            string
	Version       string
	EffectiveDate string
	MinimumAge    int
	PrimaryLocale Locale
	Locale        Locale
	Policies      []RegistrationPolicy
}

const (
	ApprovedPolicySetID         = "gradex-legal-2026-08-09-v1"
	ApprovedPolicySetVersion    = "2026-08-09-v1"
	ApprovedPrivacyVersion      = "2026-08-09-v1"
	ApprovedTermsVersion        = "2026-08-09-v1"
	ApprovedPolicyEffectiveDate = "2026-08-09"
	ApprovedPolicyMinimumAge    = 18
)

// PolicySetResolver supplies the exact currently required acceptance set. A
// missing resolver is an unavailable admission dependency, never permission to
// invent or retain a stale default.
type PolicySetResolver interface {
	Current(context.Context, Locale) (RegistrationPolicySet, error)
	Resolve(context.Context, string, Locale) (RegistrationPolicySet, error)
}

var (
	ErrPolicySetUnavailable = errors.New("registration policy set is unavailable")
	ErrPolicySetStale       = errors.New("registration policy set is not current")
)

type StaticPolicySetResolver struct {
	sets map[Locale]RegistrationPolicySet
}

// ApprovedPolicySetResolver is the production implementation authorized by
// LG-011. It is a distinct composition type so the development fixture cannot
// be selected accidentally in a non-development dependency graph.
type ApprovedPolicySetResolver struct {
	sets *StaticPolicySetResolver
}

// NewApprovedPolicySetResolver resolves the approved bilingual set and derives
// every canonical URL from the validated public origin.
func NewApprovedPolicySetResolver(
	publicOrigin string,
	configuredID string,
) (*ApprovedPolicySetResolver, error) {
	if configuredID != ApprovedPolicySetID {
		return nil, errors.New("configured registration policy set is not the approved LG-011 set")
	}
	origin, err := url.Parse(publicOrigin)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil ||
		origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return nil, errors.New("approved registration policy set requires an exact HTTPS public origin")
	}
	base := origin.Scheme + "://" + origin.Host
	sets, err := NewStaticPolicySetResolver(
		approvedPolicySet(base, approvedPolicyLocalization{
			locale: LocaleEnglish, privacyPath: "/en/privacy", termsPath: "/en/terms",
			privacyLabel: "Privacy Policy", termsLabel: "Terms of Use",
		}),
		approvedPolicySet(base, approvedPolicyLocalization{
			locale: LocaleArabic, privacyPath: "/ar/privacy", termsPath: "/ar/terms",
			privacyLabel: "سياسة الخصوصية", termsLabel: "شروط الاستخدام",
		}),
	)
	if err != nil {
		return nil, err
	}
	return &ApprovedPolicySetResolver{sets: sets}, nil
}

type approvedPolicyLocalization struct {
	locale                                           Locale
	privacyPath, termsPath, privacyLabel, termsLabel string
}

func approvedPolicySet(base string, localized approvedPolicyLocalization) RegistrationPolicySet {
	return RegistrationPolicySet{
		ID: ApprovedPolicySetID, Version: ApprovedPolicySetVersion,
		EffectiveDate: ApprovedPolicyEffectiveDate, MinimumAge: ApprovedPolicyMinimumAge,
		PrimaryLocale: LocaleArabic, Locale: Locale(localized.locale),
		Policies: []RegistrationPolicy{
			{Kind: PolicyPrivacyNotice, Version: ApprovedPrivacyVersion, Label: localized.privacyLabel, URL: base + localized.privacyPath},
			{Kind: PolicyTermsOfService, Version: ApprovedTermsVersion, Label: localized.termsLabel, URL: base + localized.termsPath},
		},
	}
}

func (r *ApprovedPolicySetResolver) Current(ctx context.Context, locale Locale) (RegistrationPolicySet, error) {
	if r == nil || r.sets == nil {
		return RegistrationPolicySet{}, ErrPolicySetUnavailable
	}
	return r.sets.Current(ctx, locale)
}

func (r *ApprovedPolicySetResolver) Resolve(
	ctx context.Context,
	id string,
	locale Locale,
) (RegistrationPolicySet, error) {
	if r == nil || r.sets == nil {
		return RegistrationPolicySet{}, ErrPolicySetUnavailable
	}
	return r.sets.Resolve(ctx, id, locale)
}

// NewStaticPolicySetResolver is the explicit development/test fixture seam.
// Production must inject approved bilingual content after LG-011 closes.
func NewStaticPolicySetResolver(sets ...RegistrationPolicySet) (*StaticPolicySetResolver, error) {
	resolver := &StaticPolicySetResolver{sets: make(map[Locale]RegistrationPolicySet)}
	var canonical RegistrationPolicySet
	for _, set := range sets {
		if err := validatePolicySet(set); err != nil {
			return nil, err
		}
		if _, duplicate := resolver.sets[set.Locale]; duplicate {
			return nil, errors.New("duplicate registration policy-set locale")
		}
		if len(resolver.sets) == 0 {
			canonical = set
		} else if !policyVersionsMatch(canonical, set) {
			return nil, errors.New("localized registration policy sets do not match")
		}
		set.Policies = append([]RegistrationPolicy(nil), set.Policies...)
		resolver.sets[set.Locale] = set
	}
	if len(resolver.sets) != 2 {
		return nil, errors.New("registration policy set requires Arabic and English variants")
	}
	return resolver, nil
}

func (r *StaticPolicySetResolver) Current(
	_ context.Context,
	locale Locale,
) (RegistrationPolicySet, error) {
	set, ok := r.sets[locale]
	if !ok {
		return RegistrationPolicySet{}, ErrPolicySetUnavailable
	}
	set.Policies = append([]RegistrationPolicy(nil), set.Policies...)
	return set, nil
}

func (r *StaticPolicySetResolver) Resolve(
	ctx context.Context,
	id string,
	locale Locale,
) (RegistrationPolicySet, error) {
	set, err := r.Current(ctx, locale)
	if err != nil {
		return RegistrationPolicySet{}, ErrPolicySetUnavailable
	}
	if id != set.ID {
		return RegistrationPolicySet{}, ErrPolicySetStale
	}
	return set, nil
}

func policyVersionsMatch(first, second RegistrationPolicySet) bool {
	if first.ID != second.ID || first.Version != second.Version ||
		first.EffectiveDate != second.EffectiveDate || first.MinimumAge != second.MinimumAge ||
		first.PrimaryLocale != second.PrimaryLocale || len(first.Policies) != len(second.Policies) {
		return false
	}
	secondByKind := make(map[PolicyKind]RegistrationPolicy, len(second.Policies))
	for _, policy := range second.Policies {
		secondByKind[policy.Kind] = policy
	}
	for _, policy := range first.Policies {
		other, ok := secondByKind[policy.Kind]
		if !ok || policy.Version != other.Version {
			return false
		}
	}
	return true
}

func validatePolicySet(set RegistrationPolicySet) error {
	if strings.TrimSpace(set.ID) == "" || !set.Locale.Valid() {
		return errors.New("registration policy set ID and locale are required")
	}
	if len(set.Policies) != 2 {
		return errors.New("registration policy set must contain exactly two required policies")
	}
	seen := make(map[PolicyKind]struct{}, 2)
	for _, policy := range set.Policies {
		if policy.Kind != PolicyPrivacyNotice && policy.Kind != PolicyTermsOfService {
			return errors.New("registration policy kind is unsupported")
		}
		if _, duplicate := seen[policy.Kind]; duplicate {
			return errors.New("registration policy kind is duplicated")
		}
		seen[policy.Kind] = struct{}{}
		if strings.TrimSpace(policy.Version) == "" ||
			strings.TrimSpace(policy.Label) == "" ||
			!validPolicyURL(policy.URL) {
			return errors.New("registration policy metadata is incomplete")
		}
	}
	return nil
}

func validPolicyURL(raw string) bool {
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") {
		return true
	}
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil &&
		parsed.RawQuery == "" && parsed.Fragment == "" && strings.HasPrefix(parsed.Path, "/")
}
