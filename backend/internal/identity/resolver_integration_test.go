//go:build integration

package identity

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

func pool(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	t.Cleanup(cancel)

	p, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("opening pool: %v", err)
	}
	t.Cleanup(p.Close)
	return p, ctx
}

// The whole point of deriving the principal per request: the bootstrap Admin
// created by the real command resolves to a restricted principal that the real
// policy refuses. Stubs cannot prove this — the CHANGE_REQUIRED state has to
// come off an actual row written by the actual bootstrap.
func TestResolvedBootstrapAdminIsRestrictedByPolicy(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)

	result, err := Bootstrap(ctx, conn, request("op-resolve"))
	if err != nil {
		t.Fatalf("bootstrapping: %v", err)
	}

	p, pctx := pool(t)
	resolver := NewDBPrincipalResolver(p)

	principal, err := resolver.ResolvePrincipal(pctx, result.AccountID)
	if err != nil {
		t.Fatalf("resolving the bootstrap Admin: %v", err)
	}

	if principal.Role != RoleAdmin {
		t.Errorf("role = %q, want ADMIN", principal.Role)
	}
	if principal.Status != StatusActive {
		t.Errorf("status = %q, want ACTIVE", principal.Status)
	}
	if !principal.Restricted() {
		t.Fatalf("the bootstrap Admin resolved unrestricted (credential %q)", principal.CredentialState)
	}

	for _, c := range []Capability{
		CapAdminOperations, CapFinancialOperations, CapSecurityOperations,
		CapRetentionOperations, CapProviderOperations, CapContentManagement,
	} {
		if d := Authorize(principal, c); d.Allowed {
			t.Errorf("the freshly bootstrapped Admin was allowed %s", c)
		}
	}
	if !Authorize(principal, CapPasswordChange).Allowed {
		t.Error("the bootstrap Admin cannot change its password, which is the one thing it must be able to do")
	}
}

// Clearing the requirement is what unlocks Admin authority — and it takes
// effect on the next resolution, with nothing to invalidate.
func TestClearingTheRequirementUnlocksAuthorityImmediately(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)

	result, err := Bootstrap(ctx, conn, request("op-unlock"))
	if err != nil {
		t.Fatalf("bootstrapping: %v", err)
	}

	p, pctx := pool(t)
	resolver := NewDBPrincipalResolver(p)

	before, err := resolver.ResolvePrincipal(pctx, result.AccountID)
	if err != nil {
		t.Fatalf("resolving before: %v", err)
	}
	if Authorize(before, CapAdminOperations).Allowed {
		t.Fatal("Admin operations were allowed before the password change")
	}

	if _, err := conn.Exec(ctx,
		`UPDATE password_credentials SET state = 'ACTIVE' WHERE account_id = $1`, result.AccountID,
	); err != nil {
		t.Fatalf("clearing the requirement: %v", err)
	}

	after, err := resolver.ResolvePrincipal(pctx, result.AccountID)
	if err != nil {
		t.Fatalf("resolving after: %v", err)
	}
	if !Authorize(after, CapAdminOperations).Allowed {
		t.Fatal("Admin operations were still denied after the requirement was cleared")
	}
}

// Suspension takes effect on the next request too, for the same reason. S1C
// owns enforcing it across the surface; this proves the derivation already
// carries it.
func TestSuspensionIsVisibleOnTheNextResolution(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)

	result, err := Bootstrap(ctx, conn, request("op-suspend"))
	if err != nil {
		t.Fatalf("bootstrapping: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`UPDATE accounts SET status = 'SUSPENDED' WHERE id = $1`, result.AccountID,
	); err != nil {
		t.Fatalf("suspending: %v", err)
	}

	p, pctx := pool(t)
	principal, err := NewDBPrincipalResolver(p).ResolvePrincipal(pctx, result.AccountID)
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}

	for _, c := range AllCapabilities {
		if d := Authorize(principal, c); d.Allowed {
			t.Errorf("a suspended Account was allowed %s", c)
		}
	}
}

// An Account with no credential row resolves as restricted, not as
// unrestricted. The restrictive reading is deliberate: an Account that cannot
// prove a password should not hold authority.
func TestAccountWithoutCredentialResolvesRestricted(t *testing.T) {
	freshSchema(t)
	conn, ctx := connect(t)

	var accountID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO accounts (normalized_email, email, role, status, display_name)
		 VALUES ('nocred@gradex.example', 'nocred@gradex.example', 'INSTRUCTOR', 'ACTIVE', 'No Credential')
		 RETURNING id::text`,
	).Scan(&accountID); err != nil {
		t.Fatalf("inserting: %v", err)
	}

	p, pctx := pool(t)
	principal, err := NewDBPrincipalResolver(p).ResolvePrincipal(pctx, accountID)
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	if !principal.Restricted() {
		t.Fatalf("an Account with no credential resolved as %q", principal.CredentialState)
	}
	if Authorize(principal, CapContentManagement).Allowed {
		t.Error("an Account with no credential was allowed content management")
	}
}

func TestResolveUnknownAndMalformedIdentifiers(t *testing.T) {
	freshSchema(t)
	p, pctx := pool(t)
	resolver := NewDBPrincipalResolver(p)

	for name, id := range map[string]string{
		"unknown but well-formed": "99999999-9999-9999-9999-999999999999",
		"not a UUID":              "user-1",
		"empty":                   "",
		"SQL-ish":                 "' OR 1=1 --",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := resolver.ResolvePrincipal(pctx, id)
			if !errors.Is(err, ErrPrincipalNotFound) {
				t.Fatalf("err = %v, want ErrPrincipalNotFound", err)
			}
		})
	}
}

var _ = config.NewSecret
