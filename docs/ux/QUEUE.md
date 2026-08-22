# Prioritized Work Queue and Status Board

> Status: Queue drafted 2026-08-19. **Nothing approved. No implementation authorized.**
> Last Updated: 2026-08-19

Ordering is by journey dependency, shared-component leverage, and Student risk — **not** route order.
Foundations come first because polishing pages before the shell and token layer exist guarantees
rework and guarantees inconsistency.

## Status board

| # | Work unit | Persona | Status | Blocked by |
|---|---|---|---|---|
| **U0** | Phase authority + founder decisions | — | `FOUNDER_REVIEW` | founder |
| **U1** | Token + primitive foundation | Shared | `DESIGN_PROPOSED` | U0, DS-04 |
| **U2** | System states + route boundaries | Shared | `AUDITED` | U1 |
| **U3** | Student app shell + navigation | Student | `AUDITED` | U1, U2 |
| **U4** | Course access (ST03/ST04/ST10) | Student | `AUDITED` | U1, U2, U3 |
| **U5** | Student dashboard (ST05) | Student | `AUDITED` | U3 |
| **U6** | Course Home (ST06) | Student | `AUDITED` | U3, U5 |
| **U7** | Lesson Player surface (ST07) | Student | `AUDITED` | U3, U6 |
| **U8** | Catalog + Course Details (ST01/ST02) | Student/Public | `AUDITED` | U1, U3 |
| **U9** | Resources & Labs (ST08) | Student | `NOT_AUDITED` | UXD-03 |
| **U10** | Profile + Notifications (S08/S07) | Student | `NOT_AUDITED` | UXD-05, UXD-06 |
| **U11** | Instructor shell + dashboard | Instructor | `NOT_AUDITED` | U1, U2 |
| **U12** | Course Builder + Lesson editor + materials | Instructor | `NOT_AUDITED` | U11 |
| **U13** | Submit/review status + preview manager | Instructor | `NOT_AUDITED` | U12 |
| **U14** | Admin shell + Ops home | Admin | `NOT_AUDITED` | U1, U2 |
| **U15** | Course review + taxonomy + pricing split | Admin | `NOT_AUDITED` | U14 |
| **U16** | Course access invitations + entitlement detail | Admin | `NOT_AUDITED` | U14, U4 |
| **U17** | Users/staff + reported content | Admin | `NOT_AUDITED` | U14 |

---

## U0 — Phase authority and founder decisions

Not a code unit. The repository is in a frozen post-approval phase; this phase needs its own recorded
Decision in [DECISIONS.md](../DECISIONS.md) covering scope, seats, and the boundary that no product
behavior changes. Founder decisions required before U1:

1. **DS-04 — dark mode.** Adopt and tokenize (design-system extension), or remove the `.dark` block
   and `ThemeToggle`? The canonical export defines no dark theme and dark mode is already broken on
   several screens.
2. **UXD-07 — how-to-get-access copy.** Approve static bilingual guidance on Course Details as the
   cheap unblock for [JOURNEYS.md](JOURNEYS.md) Break 1, or hold for a payload change?
3. **Office hours / notifications / profile (UXD-04/05/06).** Still in MVP? Their absence leaves
   dangling references across all three personas.
4. **Seats.** Builder and reviewer for this phase.

## U1 — Token and primitive foundation

**Why first:** every page below consumes it. Building pages first means building each primitive
inline, three times, differently — which is exactly the state the product is in now.

- Port `--warning`/`--warning-soft`/`--info`/`--info-soft` from `docs/design-system/tokens/colors.css`
  into `globals.css` + `tailwind.config.ts` (DS-01). Add `Alert` `warning` tone, `Badge` `warning`
  variant.
- Build: `ProgressBar`, `StatusBadge`, `Toast` (+ provider), `Dialog`, `Skeleton`, `Breadcrumb`,
  `CourseCard`, `Select`.
- Fix `MobileNav` physical `side="right"` → logical side.
- Add a lint rule or CI grep rejecting Tailwind default-palette classes and hex literals in
  `src/components` and `src/app`.
- **Acceptance:** no visual change to any existing page; typecheck and unit tests green; the
  palette guard passes on the components it covers.

## U2 — System states and route boundaries

**Why second:** [INVENTORY.md](INVENTORY.md) X-04/X-05 make every later verification impossible —
you cannot verify a page's error handling when all errors render one string.

- `SystemState` component: 401, 403, 404, 5xx, offline. Distinct copy, distinct CTA per
  [NAVIGATION_RULES.md](../NAVIGATION_RULES.md) §8.
- `error.tsx`, `not-found.tsx`, `loading.tsx` for the `[locale]` tree and each role subtree.
- A shared server-side error classifier so `ProblemError` status maps to the right state instead of
  `catch { generic }`.
- **Acceptance:** an expired session, a 403, a missing Course, and a 5xx each produce a distinct,
  correct, bilingual screen with the correct CTA.

## U3 — Student app shell and navigation

**Why third:** it resolves [JOURNEYS.md](JOURNEYS.md) Break 3, and U4–U8 all render inside it.
Building it after the pages means retrofitting five screens.

- `src/app/[locale]/layout.tsx` resolving locale and direction **server-side** from the route segment
  (fixes X-02 SSR `lang`/`dir`, and X-03 locale split-brain for the `[locale]` tree).
- `StudentAppShell`: header (logo, browse, language, notifications slot, account slot) + bottom tabs
  `Home · Browse · Notifications · Profile` under 768px, promoted to top navigation above it, per
  [NAVIGATION_RULES.md](../NAVIGATION_RULES.md) §1.
- `Breadcrumb` wired for Course Details / Course Home / Lesson.
- Replace the dead anchor `navItems` with real routes (DEBT-03).
- Notifications and Profile tabs render an honest "not available yet" state until UXD-05/06 land —
  they must not be dead links.
- **Acceptance:** from any `/learn/*` route a Student can reach Catalog, switch language, and sign
  out, in both directions, at 360/768/1024/1440.

## U4 — Course access (ST03 / ST04 / ST10) — **recommended first implementation tranche**

See the tranche brief in the session response. Full rebuild of `app/[locale]/access/page.tsx`.

## U5 — Student dashboard (ST05)
Rebuild on `StudentAppShell` + `CourseCard` + `ProgressBar`. Add pending-invitation surface, expiry
and near-expiry states, a real `EmptyState` pointing to the catalog, and Access History entry.
Continue-learning is interim (two-hop via Course Home) until UXD-02.

## U6 — Course Home (ST06)
Breadcrumb, always-visible back, resume affordance derived from per-lesson progress (no backend
change needed), lesson completion markers that are not color-only, explained expired state.

## U7 — Lesson Player surface (ST07)
Video first; persistent lesson rail on desktop, sheet on mobile; Course identity and back affordance
always visible; styled player loading/failure with retry. **Player logic untouched.**

## U8 — Catalog and Course Details (ST01 / ST02)
Delete the legacy `Shell` (X-06); one chrome across the funnel. `CourseCard` everywhere. Result count
and pagination from data already returned. Course Details gains access guidance (UXD-07), access
term, `Go to course` when entitled, and a preview player. Taxonomy filters land when UXD-01 does.

## U9–U17
Expand each into a full brief when its predecessor reaches `VERIFIED`. Do not pre-plan in detail —
findings from U1–U8 will change them.

---

## Change log

| Date | Change |
|---|---|
| 2026-08-19 | Queue created from the initial repository audit. Nothing approved. |
