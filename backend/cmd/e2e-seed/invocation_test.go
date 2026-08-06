//go:build !production

package main

import (
	"flag"
	"strings"
	"testing"
)

// Whether this test binary was invoked as the E2E seeding tool, or merely compiled and run by an
// ordinary repository-wide `go test ./...`.
//
// WHY THIS EXISTS
//
//	This package is a tool, not a test suite: it is built with `go test -c` and driven by
//	`frontend/e2e/global-setup.ts`. Its TestMain therefore performed the seeding work directly and
//	ran the fail-closed safety validation first, which is correct for the tool and wrong for
//	`go test ./...` — that invocation supplies none of the E2E contract, so the safety gate
//	log.Fatal'd and failed the whole Backend job. Hosted run 31035395606 failed exactly there.
//
//	The fix is not to weaken the gate. It is to distinguish the two invocations, and to run the
//	gate on every invocation that asks for the tool. The decision is a pure function so it can be
//	proved directly rather than inferred from a CI result.
//
// THE RULE
//
//	Any E2E contract signal at all — one environment variable, or one tool flag — means the tool
//	was requested, and the full fail-closed validation runs. So a *partial* configuration is
//	refused rather than silently treated as "not the tool", which is the failure mode that would
//	matter: it is what could let a half-configured run reach a database it should never touch.
//
//	Absent every signal, nothing environment-dependent runs: no DSN is resolved, no connection is
//	opened, no database is created, dropped, or migrated, and no fixture is seeded.
type seedInvocation struct {
	// GRADEX_E2E_ALLOW_DATABASE_RESET
	AllowReset string
	// GRADEX_E2E_TARGET_DB_NAME
	TargetDBName string
	// GRADEX_E2E_TARGET_DB_URL
	TargetDSN string
	// GRADEX_E2E_ADMIN_DB_URL
	AdminDSN string
	// ADMIN_DATABASE_URL, the accepted legacy spelling
	LegacyAdminDSN string
	// Count of tool flags actually passed on the command line (flag.Visit, not flag.VisitAll:
	// only flags the caller set, never defaults).
	ToolFlagsSet int
}

// e2eToolInvocationRequested reports whether the caller asked for the seeding tool.
//
// Emptiness is checked on the raw value, deliberately not trimmed: a variable set to whitespace is
// a misconfiguration, and treating it as a request routes it into the validator, which refuses it.
// Trimming would instead route it into the inert path and silently skip the safety gate.
func e2eToolInvocationRequested(inv seedInvocation) bool {
	if inv.ToolFlagsSet > 0 {
		return true
	}
	for _, v := range []string{
		inv.AllowReset,
		inv.TargetDBName,
		inv.TargetDSN,
		inv.AdminDSN,
		inv.LegacyAdminDSN,
	} {
		if len(v) > 0 {
			return true
		}
	}
	return false
}

// isToolFlagName reports whether a set flag is one of this tool's own verbs.
//
// `go test` injects its own flags into the same FlagSet — it passes `-test.timeout` and
// `-test.paniconexit0` on every ordinary run — so counting every set flag made an ordinary
// `go test ./...` look like a deliberate tool invocation. That is precisely the bug this file
// exists to prevent, so the distinction is asserted rather than assumed.
func isToolFlagName(name string) bool {
	return !strings.HasPrefix(name, "test.")
}

// countToolFlagsSet counts only the tool's own flags that the caller actually set.
func countToolFlagsSet(fs *flag.FlagSet) int {
	n := 0
	fs.Visit(func(f *flag.Flag) {
		if isToolFlagName(f.Name) {
			n++
		}
	})
	return n
}

func TestGoTestOwnFlagsAreNotToolFlags(t *testing.T) {
	// The exact regression: `go test` sets these, and they must not be mistaken for tool verbs.
	for _, name := range []string{"test.timeout", "test.paniconexit0", "test.v", "test.run", "test.count"} {
		if isToolFlagName(name) {
			t.Fatalf("%q is a go test flag and must not count as a tool invocation", name)
		}
	}
	// The tool's real verbs must count.
	for _, name := range []string{"dbname", "drop", "issue-session", "query-progress", "access-mutation", "email"} {
		if !isToolFlagName(name) {
			t.Fatalf("%q is a tool verb and must count as a tool invocation", name)
		}
	}
}

func TestCountToolFlagsSetIgnoresGoTestFlags(t *testing.T) {
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	fs.Bool("test.paniconexit0", false, "")
	fs.String("test.timeout", "", "")
	fs.Bool("drop", false, "")
	if err := fs.Parse([]string{"-test.paniconexit0", "-test.timeout=10m"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := countToolFlagsSet(fs); got != 0 {
		t.Fatalf("only go test flags were set; want 0 tool flags, got %d", got)
	}

	fs2 := flag.NewFlagSet("probe2", flag.ContinueOnError)
	fs2.Bool("test.paniconexit0", false, "")
	fs2.Bool("drop", false, "")
	if err := fs2.Parse([]string{"-test.paniconexit0", "-drop"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := countToolFlagsSet(fs2); got != 1 {
		t.Fatalf("one tool verb was set; want 1, got %d", got)
	}
}

func TestOrdinaryGoTestIsInert(t *testing.T) {
	// `go test ./...` with none of the contract: the tool must not be requested, so TestMain runs
	// the ordinary cases and exits without resolving a DSN or touching a database.
	if e2eToolInvocationRequested(seedInvocation{}) {
		t.Fatal("an invocation carrying no E2E signal must not request the seeding tool")
	}
}

func TestCompleteEnvironmentRequestsTheTool(t *testing.T) {
	inv := seedInvocation{
		AllowReset:   "1",
		TargetDBName: "gradex_playwright_e2e_msgc8u9g13ash9rq",
		TargetDSN:    "postgres://gradex:gradex@127.0.0.1:5432/gradex_playwright_e2e_msgc8u9g13ash9rq?sslmode=disable",
		AdminDSN:     "postgres://gradex:gradex@127.0.0.1:5432/postgres?sslmode=disable",
	}
	if !e2eToolInvocationRequested(inv) {
		t.Fatal("a complete E2E environment must request the seeding tool")
	}
}

func TestPartialEnvironmentStillRequestsTheTool(t *testing.T) {
	// The property that matters. Each of these is incomplete and must reach the fail-closed
	// validator rather than be mistaken for an ordinary `go test` run. Going inert here is the
	// dangerous outcome, because it would skip the safety gate entirely.
	for name, inv := range map[string]seedInvocation{
		"only the reset acknowledgement": {AllowReset: "1"},
		"only a target database name":    {TargetDBName: "gradex_playwright_e2e_msgc8u9g13ash9rq"},
		"only a target DSN":              {TargetDSN: "postgres://gradex:gradex@127.0.0.1:5432/x?sslmode=disable"},
		"only an admin DSN":              {AdminDSN: "postgres://gradex:gradex@127.0.0.1:5432/postgres?sslmode=disable"},
		"only the legacy admin DSN":      {LegacyAdminDSN: "postgres://gradex:gradex@127.0.0.1:5432/postgres?sslmode=disable"},
		"reset set without a target":     {AllowReset: "1", AdminDSN: "postgres://gradex:gradex@127.0.0.1:5432/postgres?sslmode=disable"},
		"whitespace is not absence":      {AllowReset: " "},
	} {
		if !e2eToolInvocationRequested(inv) {
			t.Fatalf("%s: a partial E2E environment must still request the tool so the fail-closed validator refuses it", name)
		}
	}
}

func TestToolFlagAloneRequestsTheTool(t *testing.T) {
	// `-issue-session`, `-query-progress`, `-drop`, and the rest are tool verbs. One of them with
	// no environment at all is still a tool invocation, and must be validated, not ignored.
	if !e2eToolInvocationRequested(seedInvocation{ToolFlagsSet: 1}) {
		t.Fatal("a tool flag must request the seeding tool even with an empty environment")
	}
}

func TestUnsafeTargetsAreRefusedByTheValidator(t *testing.T) {
	// The invocation decision only routes; refusal remains the validator's job, and it must still
	// refuse a shared or production-shaped target. Asserted here so the routing change cannot be
	// read as having moved that responsibility.
	for name, cfg := range map[string]struct {
		allowReset, adminDSN, targetDSN, appDSN string
	}{
		"the application database itself": {
			"1",
			"postgres://gradex:gradex@127.0.0.1:5432/postgres?sslmode=disable",
			"postgres://gradex:gradex@127.0.0.1:5432/gradex?sslmode=disable",
			"postgres://gradex:gradex@127.0.0.1:5432/gradex?sslmode=disable",
		},
		"a database outside the E2E prefix": {
			"1",
			"postgres://gradex:gradex@127.0.0.1:5432/postgres?sslmode=disable",
			"postgres://gradex:gradex@127.0.0.1:5432/staging?sslmode=disable",
			"postgres://gradex:gradex@127.0.0.1:5432/gradex?sslmode=disable",
		},
		"a remote host": {
			"1",
			"postgres://gradex:gradex@db.production.example.com:5432/postgres?sslmode=disable",
			"postgres://gradex:gradex@db.production.example.com:5432/gradex_playwright_e2e_x?sslmode=disable",
			"postgres://gradex:gradex@127.0.0.1:5432/gradex?sslmode=disable",
		},
		"a missing reset acknowledgement": {
			"",
			"postgres://gradex:gradex@127.0.0.1:5432/postgres?sslmode=disable",
			"postgres://gradex:gradex@127.0.0.1:5432/gradex_playwright_e2e_msgc8u9g13ash9rq?sslmode=disable",
			"postgres://gradex:gradex@127.0.0.1:5432/gradex?sslmode=disable",
		},
	} {
		err := validateSeedSafety(cfg.allowReset, cfg.adminDSN, cfg.targetDSN, cfg.appDSN)
		if err == nil {
			t.Fatalf("%s: the safety validator must refuse this target", name)
		}
	}

	// And it still accepts a correctly configured isolated target, so the checks above are not
	// passing because everything is refused.
	if err := validateSeedSafety(
		"1",
		"postgres://gradex:gradex@127.0.0.1:5432/postgres?sslmode=disable",
		"postgres://gradex:gradex@127.0.0.1:5432/gradex_playwright_e2e_msgc8u9g13ash9rq?sslmode=disable",
		"postgres://gradex:gradex@127.0.0.1:5432/gradex?sslmode=disable",
	); err != nil {
		t.Fatalf("a correctly isolated E2E target must be accepted: %v", err)
	}
}
