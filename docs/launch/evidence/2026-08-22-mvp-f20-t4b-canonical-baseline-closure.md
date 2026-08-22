# MVP-F20 / T4-B — Canonical Baseline Closure

**Date:** 2026-08-22  
**Status:** `BASELINE_ACCEPTED`

Before T4-C/D/E implementation, one clean canonical Playwright invocation ran with no competing
Playwright process and the established single-worker harness:

```text
npx playwright test --workers=1
146 tests
137 passed
6 failed
3 did not run
duration: 9.3m
```

The failures were exactly the six accepted historical identities:

- `e2e/s5-expired-entitlement.spec.ts:712`
- `e2e/s5-playback-performance.spec.ts:157`
- `e2e/s5-viewport-evidence.spec.ts:223` at phone, tablet, laptop, and desktop

Both `s15-dashboard-resume.spec.ts:135` and `:213` passed in this clean run. No unrelated new
failure identity appeared, so the T4 first gate permitted implementation to continue.
