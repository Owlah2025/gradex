package identity

import (
	"context"
	"errors"
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
	ID       string
	Locale   Locale
	Policies []RegistrationPolicy
}

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
	if first.ID != second.ID || len(first.Policies) != len(second.Policies) {
		return false
	}
	secondByKind := make(map[PolicyKind]RegistrationPolicy, len(second.Policies))
	for _, policy := range second.Policies {
		secondByKind[policy.Kind] = policy
	}
	for _, policy := range first.Policies {
		other, ok := secondByKind[policy.Kind]
		if !ok || policy.Version != other.Version || policy.URL != other.URL {
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
			!strings.HasPrefix(policy.URL, "/") ||
			strings.HasPrefix(policy.URL, "//") {
			return errors.New("registration policy metadata is incomplete")
		}
	}
	return nil
}
