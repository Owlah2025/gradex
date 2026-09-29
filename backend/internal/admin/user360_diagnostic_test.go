package admin

import (
	"testing"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/entitlement"
)

func TestDiagnosticPrimaryMapsEvaluatorAndAccountFacts(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)
	cases := []struct {
		name      string
		decision  entitlement.Decision
		status    string
		verified  bool
		lifecycle string
		suspended bool
		retiredAt *time.Time
		rows      []UserEntitlement
		want      string
	}{
		{name: "active", decision: entitlement.Decision{Allowed: true, Reason: entitlement.ReasonAllowed}, status: "ACTIVE", verified: true, lifecycle: "PUBLISHED", rows: []UserEntitlement{{State: "ACTIVE", AccessEndsAt: future, ScopeKind: "COURSE"}}, want: DiagnosticAllowed},
		{name: "no entitlement", decision: entitlement.Decision{Reason: entitlement.ReasonNoApplicableGrant}, status: "ACTIVE", verified: true, lifecycle: "PUBLISHED", want: DiagnosticNoEntitlement},
		{name: "expired", decision: entitlement.Decision{Reason: entitlement.ReasonExpired}, status: "ACTIVE", verified: true, lifecycle: "PUBLISHED", rows: []UserEntitlement{{State: "EXPIRED", AccessEndsAt: past, ScopeKind: "COURSE"}}, want: "EXPIRED"},
		{name: "revoked", decision: entitlement.Decision{Reason: entitlement.ReasonNoApplicableGrant}, status: "ACTIVE", verified: true, lifecycle: "PUBLISHED", rows: []UserEntitlement{{State: "REVOKED", RevokedAt: &past, AccessEndsAt: future, ScopeKind: "COURSE"}}, want: DiagnosticRevoked},
		{name: "account suspended", decision: entitlement.Decision{Reason: entitlement.ReasonNoApplicableGrant}, status: "SUSPENDED", verified: true, lifecycle: "PUBLISHED", want: DiagnosticAccountSuspended},
		{name: "unverified", decision: entitlement.Decision{Reason: entitlement.ReasonAllowed, Allowed: true}, status: "ACTIVE", verified: false, lifecycle: "PUBLISHED", want: DiagnosticAccountUnverified},
		{name: "course suspended", decision: entitlement.Decision{Reason: entitlement.ReasonCourseSuspended}, status: "ACTIVE", verified: true, lifecycle: "PUBLISHED", suspended: true, want: DiagnosticCourseSuspended},
		{name: "course retired", decision: entitlement.Decision{Reason: entitlement.ReasonRetired}, status: "ACTIVE", verified: true, lifecycle: "PUBLISHED", retiredAt: &past, want: DiagnosticCourseRetired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := diagnosticPrimary(tc.decision, tc.status, tc.verified, tc.lifecycle, tc.suspended, tc.retiredAt, "lesson", tc.rows, now)
			if got != tc.want {
				t.Fatalf("diagnostic code = %q, want %q", got, tc.want)
			}
		})
	}
}
