package admin

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/identity"
)

func TestQueryKindClassifiesSearchKinds(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{name: "empty", query: "  ", want: "none"},
		{name: "email", query: "alice@example.com", want: "email"},
		{name: "name", query: "Alice Student", want: "name"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := queryKind(test.query); got != test.want {
				t.Fatalf("query kind = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAccountWhereEscapesLikeMetacharacters(t *testing.T) {
	where, args, _ := accountWhere(AccountDirectoryRequest{Query: `a_%\`})
	if !strings.Contains(where, "ESCAPE chr(92)") {
		t.Fatalf("account search does not declare its escape character: %s", where)
	}
	if got, want := args[0], `a\_\%\\`; got != want {
		t.Fatalf("escaped search pattern = %q, want %q", got, want)
	}
}

func TestAdminRequestValidatorsRejectInvalidFilters(t *testing.T) {
	principal := identity.Principal{
		AccountID:       "admin-id",
		Role:            identity.RoleAdmin,
		Status:          identity.StatusActive,
		CredentialState: identity.CredentialActive,
	}
	from := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	to := from.Add(-time.Hour)

	accountCases := []struct {
		name string
		edit func(*AccountDirectoryRequest)
	}{
		{name: "page zero", edit: func(req *AccountDirectoryRequest) { req.Page = 0 }},
		{name: "limit over maximum", edit: func(req *AccountDirectoryRequest) { req.Limit = 51 }},
		{name: "unknown role", edit: func(req *AccountDirectoryRequest) { req.Role = "UNKNOWN" }},
		{name: "unknown status", edit: func(req *AccountDirectoryRequest) { req.Status = "UNKNOWN" }},
		{name: "reversed joined range", edit: func(req *AccountDirectoryRequest) { req.JoinedFrom, req.JoinedTo = &from, &to }},
	}
	for _, test := range accountCases {
		t.Run("account/"+test.name, func(t *testing.T) {
			req := AccountDirectoryRequest{Principal: principal, Locale: identity.LocaleEnglish, Page: 1, Limit: 50}
			test.edit(&req)
			if !errors.Is(validateAccountDirectoryRequest(req), ErrInvalidInput) {
				t.Fatal("account validator accepted invalid input")
			}
		})
	}

	auditCases := []struct {
		name string
		edit func(*AuditEventRequest)
	}{
		{name: "page zero", edit: func(req *AuditEventRequest) { req.Page = 0 }},
		{name: "limit over maximum", edit: func(req *AuditEventRequest) { req.Limit = 51 }},
		{name: "unknown module", edit: func(req *AuditEventRequest) { req.Module = "UNKNOWN" }},
		{name: "reversed occurred range", edit: func(req *AuditEventRequest) { req.OccurredFrom, req.OccurredTo = &from, &to }},
	}
	for _, test := range auditCases {
		t.Run("audit/"+test.name, func(t *testing.T) {
			req := AuditEventRequest{Principal: principal, Page: 1, Limit: 50}
			test.edit(&req)
			if !errors.Is(validateAuditEventRequest(req), ErrInvalidInput) {
				t.Fatal("audit validator accepted invalid input")
			}
		})
	}
}

func TestLocalizedInstitutionUsesRequestedLanguageAndFallback(t *testing.T) {
	arabic := "جامعة الكويت"
	english := "Kuwait University"
	empty := "   "
	tests := []struct {
		name   string
		locale identity.Locale
		ar     *string
		en     *string
		want   string
	}{
		{name: "arabic preferred", locale: identity.LocaleArabic, ar: &arabic, en: &english, want: arabic},
		{name: "arabic falls back to english", locale: identity.LocaleArabic, ar: &empty, en: &english, want: english},
		{name: "english preferred", locale: identity.LocaleEnglish, ar: &arabic, en: &english, want: english},
		{name: "english falls back to arabic", locale: identity.LocaleEnglish, ar: &arabic, en: &empty, want: arabic},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := localizedInstitution(test.locale, test.ar, test.en); got != test.want {
				t.Fatalf("institution label = %q, want %q", got, test.want)
			}
		})
	}
}
