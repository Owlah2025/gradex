package instructorprofile

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestValidSlugAcceptsOnlyCanonicalPublicSlugs(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "minimum length", value: "abc", want: true},
		{name: "hyphenated", value: "ahmed-al-salem", want: true},
		{name: "uppercase", value: "Ahmed", want: false},
		{name: "too short", value: "ab", want: false},
		{name: "too long", value: strings.Repeat("a", 61), want: false},
		{name: "leading hyphen", value: "-ahmed", want: false},
		{name: "repeated hyphen", value: "ahmed--salem", want: false},
		{name: "underscore", value: "ahmed_salem", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validSlug(test.value); got != test.want {
				t.Fatalf("validSlug(%q) = %t, want %t", test.value, got, test.want)
			}
		})
	}
}

func TestNormalizeDraftTrimsAndCanonicalizesInput(t *testing.T) {
	request := SaveDraftRequest{
		AccountID:        "account-id",
		ExpectedRevision: 3,
		PublicSlug:       "  ahmed-salem  ",
		HeadlineAr:       "  عنوان  ",
		HeadlineEn:       "  Headline  ",
		BioAr:            "  نبذة  ",
		BioEn:            "  Biography  ",
		ExpertiseIDs: []string{
			"550E8400-E29B-41D4-A716-446655440000",
			"550e8400-e29b-41d4-a716-446655440000",
		},
	}

	normalized, err := normalizeDraft(request)
	if err != nil {
		t.Fatalf("normalizeDraft returned error: %v", err)
	}
	if normalized.PublicSlug != "ahmed-salem" || normalized.HeadlineAr != "عنوان" || normalized.HeadlineEn != "Headline" {
		t.Fatalf("normalized text = %#v", normalized)
	}
	if normalized.BioAr != "نبذة" || normalized.BioEn != "Biography" {
		t.Fatalf("normalized biographies = %#v", normalized)
	}
	if len(normalized.ExpertiseIDs) != 1 || normalized.ExpertiseIDs[0] != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("normalized expertise IDs = %#v", normalized.ExpertiseIDs)
	}
}

func TestPrepareDecisionEnforcesModerationTransitions(t *testing.T) {
	base := &Profile{
		DisplayName:      "Instructor",
		PublicSlug:       stringPtr("instructor"),
		HeadlineEn:       "Clear teaching",
		BioEn:            "A useful biography.",
		PublicationState: StatePendingReview,
	}

	tests := []struct {
		name         string
		state        PublicationState
		kind         decisionKind
		wantState    PublicationState
		wantAction   string
		wantSnapshot bool
		wantError    error
	}{
		{
			name:  "approve pending review",
			state: StatePendingReview, kind: decisionApprove,
			wantState: StatePublished, wantAction: "INSTRUCTOR_PROFILE_APPROVED", wantSnapshot: true,
		},
		{
			name:  "approve draft is rejected",
			state: StateDraft, kind: decisionApprove, wantError: ErrInvalidTransition,
		},
		{
			name:  "request changes pending review",
			state: StatePendingReview, kind: decisionRequestChange,
			wantState: StateChangesRequested, wantAction: "INSTRUCTOR_PROFILE_CHANGES_REQUESTED",
		},
		{
			name:  "request changes published is rejected",
			state: StatePublished, kind: decisionRequestChange, wantError: ErrInvalidTransition,
		},
		{
			name:  "hide published",
			state: StatePublished, kind: decisionHide,
			wantState: StateHidden, wantAction: "INSTRUCTOR_PROFILE_HIDDEN",
		},
		{
			name:  "hide hidden is rejected",
			state: StateHidden, kind: decisionHide, wantError: ErrInvalidTransition,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := *base
			profile.PublicationState = test.state
			action, state, snapshot, err := prepareDecision(&profile, "review reason", test.kind)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("prepareDecision error = %v, want %v", err, test.wantError)
			}
			if test.wantError != nil {
				return
			}
			if action != test.wantAction || state != test.wantState {
				t.Fatalf("decision = (%q, %q), want (%q, %q)", action, state, test.wantAction, test.wantState)
			}
			if (snapshot != nil) != test.wantSnapshot {
				t.Fatalf("snapshot present = %t, want %t", snapshot != nil, test.wantSnapshot)
			}
		})
	}
}

func TestMapWriteErrorMapsProfileUniquenessRaces(t *testing.T) {
	tests := []struct {
		name       string
		constraint string
		want       error
	}{
		{name: "first profile insert race", constraint: "instructor_profiles_pkey", want: ErrRevisionConflict},
		{name: "published slug collision", constraint: "instructor_profiles_published_slug_key", want: ErrSlugTaken},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := mapWriteError(&pgconn.PgError{Code: "23505", ConstraintName: test.constraint})
			if !errors.Is(err, test.want) {
				t.Fatalf("mapWriteError(%s) = %v, want %v", test.constraint, err, test.want)
			}
		})
	}
}

func stringPtr(value string) *string { return &value }
