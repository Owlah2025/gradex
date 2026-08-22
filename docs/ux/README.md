# Gradex UI/UX Improvement Phase — Charter

> **PAUSED 2026-08-20.** The founder redirected work to proving the canonical MVP is functionally
> complete end to end first. Resume only after
> [`docs/mvp/FUNCTIONAL_COMPLETION.md`](../mvp/FUNCTIONAL_COMPLETION.md) reports no remaining MVP
> blockers. Functional UI changes required to make an MVP feature work are owned by the `MVP-Fxx`
> queue in that document, not by this phase. The audit findings below remain valid input.

> Status: Phase opened 2026-08-19. **Awaiting a recorded Decision granting this phase authority.**
> Owner seat: unassigned until the developer assigns builder/reviewer per [CLAUDE.md](../../CLAUDE.md#seats).
> Last Updated: 2026-08-19

This directory is the source of truth for the Gradex UI/UX improvement phase. Any Claude session
resuming this work reads this file first, then [QUEUE.md](QUEUE.md) to find the next work unit.

## 1. Authority and boundaries

This phase changes presentation. It does not change product behavior.

| Concern | Authority | This phase may |
|---|---|---|
| Product scope, screens, permissions | [PRD.md](../PRD.md), [SCREENS.md](../SCREENS.md), [BUSINESS_RULES.md](../BUSINESS_RULES.md), [DECISIONS.md](../DECISIONS.md) | **Read only.** Never widen or narrow. |
| Route hierarchy and shell behavior | [NAVIGATION_MAP.md](../NAVIGATION_MAP.md), [NAVIGATION_RULES.md](../NAVIGATION_RULES.md) | Implement what they already specify. |
| Layout hierarchy per screen | [WIREFRAMES.md](../WIREFRAMES.md) | Implement and refine within the specified hierarchy. |
| Visual/token/interaction language | [`docs/design-system/`](../design-system/) | **Apply.** Extend only via [PATTERNS.md](PATTERNS.md). |
| Journeys | [USER_JOURNEYS.md](../USER_JOURNEYS.md) | Implement; record friction in [JOURNEYS.md](JOURNEYS.md). |
| Authorization, entitlement, lifecycle, API contracts, data models | Backend + specs | **Never change.** Raise a `UX-DEPENDENT` item instead. |

The repository is in a frozen post-approval phase (see [CLAUDE.md](../../CLAUDE.md)). Production
behavior at head `2c43b90` is closed to change. **This phase must not start implementation until a
Decision records its scope and seats.** Until then, only analysis and documentation are authorized.

### Design-system authority — verified

`docs/design-system/tokens/*.css` and `docs/design-system/styles.css` are byte-identical to the
Claude Design export bundled in `Wireframe review_ student and instructor flows.zip`
(`_ds/gradex-design-system-f4d3887e-1532-4325-9466-e254aefbbec9/`). Verified 2026-08-19 by file diff.

**`docs/design-system/` is canonical.** Everything in `frontend/` is an *implementation* of it and is
subject to audit, including code that already exists. Existing components are not evidence of
compliance.

## 2. Goals

Gradex must read as one product, not a set of separately designed pages. Concretely, at every point
in every journey a user can answer:

1. Where am I?
2. What can I do here?
3. What should I do next?
4. What happened after I acted?
5. How do I get back to what I was doing?

## 3. UX principles

1. **Journey first, page second.** A page is only finished when its journey still works end to end.
2. **The design system is the vocabulary.** No new colors, radii, shadows, type sizes, or motion
   curves outside the token set. No component reinvented that already exists.
3. **Every state is a designed state.** Loading, empty, one item, many items, long text, error,
   retry, denied, expired, pending, success. A screen with only a happy path is unfinished.
4. **Arabic first, RTL first.** Arabic is the default locale (BR-149). Layouts are designed in RTL
   and verified in LTR, not the reverse.
5. **Truthful surfaces.** No fabricated testimonials, ratings, counts, or "recommended" labels. No
   screen implies Gradex takes payment. No pending state is shown as granted access.
6. **Never leak identifiers.** A Student never sees a UUID, an enum wire value, an Admin note, or an
   external payment reference.
7. **Clarity before decoration.** One brand gradient per view, one accent CTA per view.
8. **Accessibility and responsiveness are Definition of Done**, not cleanup.

## 4. Persona order

**Student → Instructor → Admin.** Shared foundations required by Student are treated as Student work.

## 5. Work-unit lifecycle

Every unit in [QUEUE.md](QUEUE.md) moves through these statuses:

| Status | Meaning | Exit requires |
|---|---|---|
| `NOT_AUDITED` | Not yet examined this phase | — |
| `AUDITED` | Audit findings recorded in [INVENTORY.md](INVENTORY.md) | Findings written with file:line evidence |
| `DESIGN_PROPOSED` | IA/layout/state proposal written | Proposal recorded in the unit's entry |
| `FOUNDER_REVIEW` | Presented, awaiting founder judgement | Founder response recorded |
| `APPROVED` | Founder approved the direction | Approval dated in the unit's entry |
| `IMPLEMENTING` | Code in progress | — |
| `IMPLEMENTED` | Code complete, self-checked | Typecheck + unit tests pass |
| `VERIFIED` | Verification matrix complete | Evidence recorded (see §6) |
| `BLOCKED` | Cannot proceed | Blocker recorded with owner |
| `UX_DEPENDENT` | Needs a product/backend change first | Item raised in [PATTERNS.md](PATTERNS.md) §5 |

`FOUNDER_REVIEW` and `UX_DEPENDENT` are additions to the states in the original brief. They exist
because this repository requires an explicit human decision gate and because presentation work here
regularly hits real backend contract limits that must not be silently designed around.

A unit never skips `AUDITED` or `VERIFIED`.

## 6. Definition of Done for a work unit

A unit is `VERIFIED` only when **all** of the following hold and are recorded in its queue entry.

**Functional**
- [ ] Every state in the screen's [SCREENS.md](../SCREENS.md) contract renders correctly.
- [ ] Loading, empty, populated (1 item and many items), error+retry, and denied/expired are each
      exercised against the real API — not mock data.
- [ ] Failure modes are distinguished: 401 routes to login with a validated `returnTo`; 403 shows a
      neutral denial; 404 shows not-found without existence leak; 5xx offers retry. A single generic
      "unavailable" for all of these is a defect.
- [ ] Mutations expose pending, success, and failure. Success is confirmed visibly.
- [ ] No behavior, permission, or API contract changed.

**Design system**
- [ ] No hardcoded hex, no Tailwind palette colors outside the `gx.*` ramp and semantic tokens
      (`slate-`, `teal-`, `amber-`, `emerald-`, `gray-`, `red-`, `blue-`, `green-` are all defects).
- [ ] Uses shared primitives (`Button`, `Card`, `Badge`, `Alert`, `Input`, `Field`, `EmptyState`,
      typography) rather than re-styled raw elements.
- [ ] Copy comes from `frontend/src/lib/i18n/dictionaries/`, not a page-local `copy` object.
- [ ] Sentence case; Western Arabic numerals; KWD at three decimals in an LTR island.

**Responsive**
- [ ] 360px, 768px, 1024px, 1440px all usable. No horizontal page scroll.
- [ ] No route is desktop-only. Complex screens may recommend a larger display but must stay safe.

**Bilingual**
- [ ] Full Arabic and English copy. No untranslated string.
- [ ] RTL verified: nav order, chevrons, breadcrumbs, drawer side, table alignment, form alignment.
      Uses logical properties (`ms-`/`me-`/`start-`/`end-`), never `ml-`/`mr-`/`left-`/`right-`.
- [ ] Long Arabic strings do not clip or overflow.

**Accessibility**
- [ ] One `<h1>`; heading order unbroken; landmarks present.
- [ ] Complete keyboard path; visible focus everywhere; focus trapped and restored in overlays.
- [ ] All controls labelled; errors announced; state never conveyed by color alone.
- [ ] Touch targets ≥ 44px; AA contrast; `prefers-reduced-motion` respected.
- [ ] `axe-core` clean via the existing `@axe-core/playwright` harness.

**Journey regression**
- [ ] The containing journey still completes end to end after the change.
- [ ] Back navigation (in-app and browser) behaves per [NAVIGATION_RULES.md](../NAVIGATION_RULES.md) §6.

## 7. Per-unit process

`A` Understand → `B` Audit → `C` Redesign → `D` Founder review → `E` Implement → `F` Verify →
`G` Journey regression. Steps `A`–`C` produce written output in [INVENTORY.md](INVENTORY.md) and the
unit's [QUEUE.md](QUEUE.md) entry before any code is written. Step `D` is a hard stop.

## 8. Session protocol

**Starting a session**
1. Read this file.
2. Read [QUEUE.md](QUEUE.md) — the status board is the current state of the world.
3. Take the highest-priority unit not in a terminal state; if one is `IMPLEMENTING`, resume it.
4. Check open `UX_DEPENDENT` and `BLOCKED` items before starting anything new.
5. Verify claims against the repository. Conversation memory is not evidence.

**Ending a session**
1. Update the unit's status and evidence in [QUEUE.md](QUEUE.md).
2. Record any new shared pattern, component, or decision in [PATTERNS.md](PATTERNS.md).
3. Record newly discovered UX debt in [INVENTORY.md](INVENTORY.md).
4. Never mark `VERIFIED` without the §6 evidence.

## 9. Files

| File | Contents |
|---|---|
| `README.md` | This charter — authority, principles, statuses, Definition of Done, session protocol |
| [INVENTORY.md](INVENTORY.md) | Every screen: route, purpose, state, audit findings, verdict |
| [JOURNEYS.md](JOURNEYS.md) | Journey maps and where they break |
| [PATTERNS.md](PATTERNS.md) | Living registry: components, patterns, rules, decisions, UX-dependent items |
| [QUEUE.md](QUEUE.md) | Prioritized work units and the status board |
