# MVP-F23 / T7 — Student Learning Payload Contract Remediation

**Date:** 2026-08-23
**Tranche:** MVP-F23 (T7)
**Status:** `PROVEN`
**Authority:** Founder authorization 2026-08-23;
[D-065](../../DECISIONS.md#d-065--exact-visible-report-context-binding);
tracker gaps **GAP-03**, **GAP-04** and rows **ST-13**, **ST-16**, **ST-17**
**Governing rule:** D-089 §2 — scope limited to canonical gaps in the register

---

## 1. The measured root cause, which was not the recorded one

The gap register recorded GAP-03's cause as *"RSC serialises all client-component props; the context
is passed as a prop to `ReportTargetActions`"*. That is **wrong**, and acting on it would have
produced no fix. `ReportTargetActions` receives `targets: [{kind, context}]` — the key is `context`,
and the string `report_context` never appears in it.

The real payload was captured from a running page:

```
cd:{"name":"CourseOutline","key":null,"env":"Server","owner":"$46",
    "stack":[["CourseHomePage",".../learn/courses/[courseId]/page.tsx",135,92,31,1,false]],
    "props":{"course":{"course_id":"…","title":"…","learning_status":"active",
             …,"report_context":"grc1.gBkAquXUE7Niz0TIeoWNfeURXK3_VK6tFhQf5dxdxmNvWVhA…"}}}
```

`"env":"Server"`. This is React's **server-component owner stack**, emitted in development so
DevTools can show the server tree. It serialises every **server** component's props verbatim into
the page. `CourseOutline` was handed the whole `CourseHome`, so the whole `CourseHome` — including
the opaque report context — was published.

The same channel explains GAP-04: every learning view received the whole `dictionary.learning`, so
an expired Lesson's markup carried `"Active access"`, the copy it deliberately does not display.

**`report-labels.ts` had already written the rule** — and named this exact failure:

> "Handing it the whole learning dictionary would put unrelated strings — 'Active access' among them
> — into the markup of pages that must not contain them, which is how an expired Lesson would start
> carrying active-state copy it never displays."

It was applied to client components only, on the reasonable belief that only client props are
serialised. They are not the only ones. T7 applies the same rule one level up.

## 2. Production was never affected — and that is not the fix

Measured against a production build of the same commit, `report_context` is **absent from the
rendered HTML entirely** (`indexOf` = −1; payload 34,545 bytes versus 95,154 in development). Running
the three affected specs with `GRADEX_E2E_FRONTEND_MODE=production` produced **21 passed**.

That mattered as diagnosis and is recorded as a fact about the shipped artifact. It was explicitly
**not** adopted as the remediation. Switching the canonical suite to the mode where the assertion
happens to pass would have left the defect in place and relabelled the evidence — which is the
failure mode D-089 §4 and the tranche instructions both forbid. The code was fixed instead, and the
proof below is in **development mode**, the canonical mode, where the leak was real.

## 3. The contract

> **A component receives the narrowest data it renders. It is never handed a catalogue to choose
> from, nor a read model wider than it displays.**

This makes the leak structurally impossible rather than dependent on build mode. Three consequences:

1. **Resolved copy, not catalogues.** `LearningStatusBadge` took `status` plus the whole dictionary
   and chose between `active` and `expired`. It now takes the **resolved string**. An expired render
   cannot carry the active copy because the active copy never reaches it.
2. **Read-model slices, not read models.** `CourseOutline` renders sections, lesson links, and
   per-lesson materials, so it now takes `courseId`, `learningStatus`, and `sections` — never the
   whole `CourseHome`, which also carries the report context. `LessonNavigation` takes `courseId` and
   `navigation` rather than the whole `LessonReadModel`.
3. **No dictionary crosses a component boundary.** The Lesson page hands `LessonContent` no `labels`
   and no `playerLabels` at all. The status label cannot be resolved before the read model says which
   status it is, and passing both strings so the child could choose is precisely the defect — so the
   child selects its own dictionary from the locale and narrows after the fetch.

`learning-label-sets.ts` holds the narrow types and builders. They are `Pick`s so a renamed
dictionary key is a compile error, and **builders rather than casts** because a `Pick` alone
type-checks while the value still carries every key at runtime.

## 4. What did not change

No business rule, no authorization, no lifecycle, no entitlement, no access decision, no API, and no
backend line. D-065's contract is unchanged and is now honoured in both build modes: the token is
still minted at render time, still opaque, still carried as evidence and never as capability. No
assertion was weakened, no test skipped, no expected failure rewritten.

## 5. Database

**None.** No migration, column, or index. T7 is frontend prop shape only.

## 6. Files changed

**Frontend**
- `src/components/learning/learning-label-sets.ts` *(new)* — narrow label types and builders
- `src/components/learning/learning-views.tsx` — every component narrowed
- `src/app/[locale]/learn/courses/[courseId]/page.tsx`
- `src/app/[locale]/learn/courses/[courseId]/lessons/[lessonId]/page.tsx`
- `src/app/[locale]/learn/dashboard/page.tsx`

**Tests**
- `src/components/learning/learning-payload-contract.test.ts` *(new)*

## 7. Tests

```
npx tsc --noEmit     clean
npm test             378 passed / 0 failed      (was 371)
```

The seven new cases prove the narrowing at runtime — `Object.keys` on each built set, and a scan
asserting no set carries `en.learning.active` — and hold structurally over the shipped pages so a
later edit cannot widen a prop back: no page may pass an unnarrowed dictionary, the badge must
receive a resolved label, `CourseOutline` must not accept `CourseHome`, `LessonNavigation` must not
accept `LessonReadModel`, and `LessonContent` must receive no dictionary prop.

Backend was untouched; `go build ./...`, `go vet ./...`, `go vet -tags=integration ./...` and
`go test ./...` were re-run to confirm no incidental change.

## 8. E2E — development mode, where the defect was

```
npx playwright test s5-expired-entitlement s5-viewport-evidence --workers=1
  17 passed
```

Both previously-failing identities now pass against the real frontend, real API, and real
PostgreSQL, with the assertions exactly as they were written:

- `s5-viewport-evidence.spec.ts:223` ×4 (phone, tablet, laptop, desktop) — **GAP-03 / RC-3 closed**
- `s5-expired-entitlement.spec.ts:712` — **GAP-04 / RC-4 closed**

## 9. Known unrelated bugs

- `courses.default_access_ends_at IS NULL` → `ConfirmPurchaseRequest` scans NULL into `time.Time` and
  returns 500 instead of `ErrExpiryRequired`/409. Untouched; not in the gap register.
- `s5-playback-performance.spec.ts:157` remains the one accepted failure in development. It is not a
  gap-register item and not a defect: the spec asserts its own precondition —
  *"T076 must measure the built frontend: run with `GRADEX_E2E_FRONTEND_MODE=production` after
  `npm run build`"* — and it **passes in production mode** (measured this tranche: 3382/3374/3338/3344 ms
  against a 5000 ms threshold, 4/4). Making it pass in the canonical development run would require a
  governance decision about which mode the canonical suite measures, which is out of T7's authority.
- `internal/media` cross-suite interference: untouched.

## 10. Repository safety

No reset, clean, stash, restore, or broad checkout. No database or volume touched. No package-wide
formatting. The protected dirty baseline including `deploy/scripts/environment.sh` is preserved. A
temporary diagnostic spec used to capture the payload was deleted after use.
