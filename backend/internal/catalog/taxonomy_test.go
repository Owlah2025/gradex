package catalog

import (
	"context"
	"errors"
	"testing"
)

func TestTaxonomyInputValidationRejectsInvalidCombinations(t *testing.T) {
	code := "  CS 101  "
	blank := " "

	tests := []struct {
		name       string
		kind       TaxonomyKind
		labelAr    string
		labelEn    string
		code       *string
		wantCode   string
		shouldFail bool
	}{
		{name: "major without academic code", kind: TaxonomyMajor, labelAr: "علوم", labelEn: "Science"},
		{name: "subject normalizes academic code", kind: TaxonomySubject, labelAr: "حاسب", labelEn: "Computing", code: &code, wantCode: "CS 101"},
		{name: "invalid kind refused", kind: "OTHER", labelAr: "علوم", labelEn: "Science", shouldFail: true},
		{name: "blank Arabic label refused", kind: TaxonomyMajor, labelAr: blank, labelEn: "Science", shouldFail: true},
		{name: "blank English label refused", kind: TaxonomyMajor, labelAr: "علوم", labelEn: blank, shouldFail: true},
		{name: "major academic code refused", kind: TaxonomyMajor, labelAr: "علوم", labelEn: "Science", code: &code, shouldFail: true},
		{name: "blank subject academic code refused", kind: TaxonomySubject, labelAr: "حاسب", labelEn: "Computing", code: &blank, shouldFail: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateTaxonomyTermInput(tt.kind, tt.labelAr, tt.labelEn, tt.code)
			if tt.shouldFail {
				if !errors.Is(err, ErrInvalidTaxonomyTerm) {
					t.Fatalf("got error %v, want %v", err, ErrInvalidTaxonomyTerm)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateTaxonomyTermInput returned %v", err)
			}
			if tt.wantCode == "" && got != nil {
				t.Fatalf("got academic code %q, want nil", *got)
			}
			if tt.wantCode != "" && (got == nil || *got != tt.wantCode) {
				t.Fatalf("got academic code %v, want %q", got, tt.wantCode)
			}
		})
	}
}

func TestTaxonomyMutationValidationRejectsMissingIdentifiers(t *testing.T) {
	if err := validateTaxonomyMutation("", "admin-id"); !errors.Is(err, ErrTaxonomyTermNotFound) {
		t.Fatalf("missing term ID error = %v, want %v", err, ErrTaxonomyTermNotFound)
	}
	if err := validateTaxonomyMutation("term-id", ""); err == nil || err.Error() != "admin account ID is required" {
		t.Fatalf("missing admin error = %v", err)
	}
}

func TestAdminTaxonomyAssignmentRejectsEmptyTermIdentifier(t *testing.T) {
	repo := &Repository{}
	_, err := repo.AssignTaxonomyToRevision(context.Background(), AssignTaxonomyRequest{
		CourseID: "course-id", RevisionID: "revision-id", AdminAccountID: "admin-id", SubjectTermID: "subject-id",
	})
	if !errors.Is(err, ErrInvalidTaxonomyTerm) {
		t.Fatalf("empty term ID error = %v, want %v", err, ErrInvalidTaxonomyTerm)
	}
}
