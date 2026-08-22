# MVP-F20 — T4 Instructor Academic Context Completion

**Date:** 2026-08-22  
**Status:** `E2E_PROVEN_INSTRUCTOR_CONTEXT`

T4-A/A.1/B/C/D/E are now proven together. T4 used migrations 0025 and 0026 as designed; no later
migration was added. Subject remains Course-owned, audience overrides remain revision-owned, and
legacy compatibility remains in place for T5.

## Quality gates observed

```text
Backend
go build ./...                         PASS
go vet ./...                           PASS
go vet -tags=integration ./...         PASS
go test ./...                          PASS — 36 packages (27 ok, 9 no-test)

Relevant integration packages, sequential, uncached
internal/httpapi                        ok 294.114s
internal/catalog                        ok  65.257s
internal/academic                       ok  28.863s
internal/academic/importer              ok  20.083s
internal/academic/manifest              ok   0.133s
internal/db                             ok  36.385s
internal/catalogpublic                  ok   3.821s
cmd/api                                 ok  12.126s

Frontend
npm run typecheck                       PASS
npm test                                347 passed / 0 failed (2.518s)

New T4-C/D/E E2E                       5 passed (1.0m)
Relevant existing-feature E2E          71 passed (5.8m)
Media-authoring E2E                     2 passed (1.5m)
```

## Canonical gates

The pre-implementation T4-B closure was `137 passed / 6 failed / 3 did not run (9.3m)` over 146
tests. The final clean canonical command was run exactly once after implementation, with one worker
and no competing Playwright invocation:

```text
npx playwright test --workers=1
151 tests
142 passed
6 failed
3 did not run
duration: 10.2m
```

The six failures were exactly the accepted historical identities:

- `e2e/s5-expired-entitlement.spec.ts:712`
- `e2e/s5-playback-performance.spec.ts:157`
- `e2e/s5-viewport-evidence.spec.ts:223` at phone, tablet, laptop, and desktop

Both S15 dashboard-resume identities passed. All four T4-B and all five T4-C/D/E tests passed. No
new deterministic product failure or unclassified infrastructure identity remained.

## Scope and tracker

No T5 migration or T6 structured filter/personalization work started. MVP-F20 is `PROVEN`, but it is
an implementation tranche and promotes no canonical feature row. The score stays
**37 / 53 = 69.8%**; ST-03 remains `BACKEND_MISSING` for MVP-F22. Remaining Academic MVP work is T5
and T6 only.
