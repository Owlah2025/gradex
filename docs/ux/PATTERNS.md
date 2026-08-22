# Living Pattern Registry

> Status: Opened 2026-08-19. Append-only; supersede entries rather than deleting them.
> This file exists so page-by-page work does not produce another inconsistent product.

Every session **reads §1–§4 before writing UI code** and **appends to §5–§7 before ending**.

---

## 1. Component decision rules

Apply in order. Stop at the first rule that matches.

1. **A design-system primitive exists** (`components/ui/*`) → use it. Never restyle a raw `<a>`,
   `<button>`, or `<div>` into a lookalike. A link that acts as a button is
   `<Button asChild><Link …/></Button>`.
2. **A shared component exists** (`components/layout/*`, `components/common/*`) → use it. Do not
   write a second navbar, footer, language toggle, or empty state.
3. **The design system names the component but it is not built**
   (`ProgressBar`, `Toast`, `Dialog`, `Select`, `Tabs`, `Tooltip`, `IconButton`, `CourseCard`) →
   **build it in `components/ui/` as a primitive**, from tokens, before the page that needs it.
   Never inline it into a page.
4. **An existing primitive is close but lacks a variant** → extend it with a new `cva` variant.
   Adding `warning` to `Alert` is correct; forking a page-local warning box is not.
5. **The pattern is genuinely one-off and page-specific** → keep it in the page's own component
   directory (`components/<domain>/`). A page-specific component is acceptable only when no second
   screen will plausibly need it.
6. **The same page-specific component appears on a second screen** → promote it to
   `components/ui/` or `components/common/`, register it in §2, and delete both copies.

**Hard prohibitions**
- No hex literals in components. Tokens only.
- No Tailwind default palette (`slate-`, `gray-`, `teal-`, `amber-`, `emerald-`, `red-`, `green-`,
  `blue-`, `yellow-`). Only semantic tokens and the `gx.*` ramp.
- No page-local `copy = { ar, en }` objects. All copy from `lib/i18n/dictionaries/`.
- No physical-direction utilities (`ml-`, `mr-`, `pl-`, `pr-`, `left-`, `right-`, `text-left`).
  Use logical properties (`ms-`, `me-`, `ps-`, `pe-`, `start-`, `end-`, `text-start`).
- No new font family, type size outside the scale, radius outside 6/10/16/24/pill, or easing outside
  `ease-out-brand` at 120/200/320ms.
- Never more than one brand gradient and one accent (orange) CTA per view.

## 2. Shared component registry

Status: `✅` built and canonical · `🔨` to build · `⛔` legacy, do not use.

| Component | Location | Status | Notes |
|---|---|---|---|
| `Button` | `ui/button.tsx` | ✅ | variants: default, accent, secondary, outline, ghost, onDark, link, destructive |
| `Card` | `ui/card.tsx` | ✅ | `interactive` prop gives the lift-on-hover pattern |
| `Badge` | `ui/badge.tsx` | ✅ | needs a `warning` variant (see §5 DS-01) |
| `Alert` | `ui/alert.tsx` | ✅ | needs a `warning` tone (see §5 DS-01) |
| `Input`, `Field` | `ui/{input,field}.tsx` | ✅ | |
| `Accordion`, `Avatar`, `Sheet`, `Tag`, typography | `ui/*` | ✅ | |
| `EmptyState` | `common/empty-state.tsx` | ✅ | always pass an `action` |
| `SkipLink`, `LanguageToggle`, `ThemeToggle`, `Reveal` | `common/*` | ✅ | `LanguageToggle` is the **only** approved toggle |
| `Navbar`, `Footer`, `Container`, `Section`, `AuthActions`, `MobileNav` | `layout/*` | ✅ | `MobileNav` needs a logical sheet side |
| `AuthShell` | `auth/auth-shell.tsx` | ✅ | reference implementation for shells |
| `LessonPlayer`, `PlayerControls`, progress reporter | `learning/*` | ✅ | logic canonical; surface is being redesigned |
| `StudentAppShell` | — | 🔨 | header + bottom tabs (small) / top nav (wide); ST05–ST09 |
| `Breadcrumb` | — | 🔨 | RTL-aware chevrons |
| `ProgressBar` | — | 🔨 | with accessible text equivalent |
| `StatusBadge` | — | 🔨 | maps invitation / entitlement / lifecycle enums to localized labels + non-color markers |
| `Toast` | — | 🔨 | the product's confirmation pattern; none exists today |
| `Dialog` | — | 🔨 | Radix dialog is already a dependency |
| `Select`, `Checkbox`, `Radio`, `Switch` | — | 🔨 | needed by catalog filters and Instructor/Admin forms |
| `Skeleton` | — | 🔨 | pairs with route `loading.tsx` |
| `CourseCard` | — | 🔨 | one implementation to replace the three in `INVENTORY.md` X-09 |
| `SystemState` | — | 🔨 | 401 / 403 / 404 / 5xx / offline, per NAVIGATION_RULES §8 |
| `Sidebar` | — | 🔨 | Instructor/Admin, `--sidebar-width: 264px` |
| `Shell` in `catalog/public-catalogue.tsx` | | ⛔ | replaced by the canonical chrome |
| `CatalogueLanguageToggle`, `LearningLocaleToggle` | | ⛔ | replaced by `LanguageToggle` |

## 3. State patterns

| State | Pattern |
|---|---|
| Loading (route) | `loading.tsx` with `Skeleton` matching the loaded layout's shape. Never a bare `<p>`. |
| Loading (in-place) | `Skeleton` or a disabled control with a pending label. Announce with `aria-live="polite"`. |
| Empty | `EmptyState` with title, description, and **an action that leads somewhere authorized**. |
| Error (retryable) | `Alert tone="error"` + a retry control that re-runs the request. |
| 401 | Redirect to `/login?returnTo=<validated internal route>`. Never a generic error. |
| 403 | Neutral access-denied. No existence leak. CTA to the role root. |
| 404 | Not-found. No existence leak. CTA to role root or catalog. |
| Entitlement expired | Named expired state with the Course identity and a route to Course Details. |
| Pending / awaiting | `warning` tone + `StatusBadge`. Must state plainly that nothing is granted yet. |
| Mutation pending | Disable the control, swap the label, keep the control's size stable. |
| Mutation success | `Toast` + the surface reflects the new truth. Immutable results render as state, not as an editable form. |
| Mutation failure | `Alert tone="error"` near the control; preserve the user's input. |
| Destructive / irreversible | `Dialog` confirmation naming the exact scope and consequence. |

## 4. Layout and navigation rules

- Container max 1200px (`max-w-container`). Cards pad 24px. Marketing sections 64–96px block.
- Grids collapse on content, not device names. Course grids: 1 col → 2 at ~768px → 3 at ~1024px.
- One `<h1>` per page, naming the page. Heading order unbroken.
- Every non-root screen has a visible way back that does not depend on browser history.
- Breadcrumbs on Course Details, Course Home, Lesson Player, and all Instructor/Admin detail screens.
- The Lesson Player keeps an always-visible Course affordance ([NAVIGATION_RULES.md](../NAVIGATION_RULES.md) §6).
- Locale lives in the URL for `[locale]` routes and is the authority there; the saved preference is
  the fallback elsewhere. Switching language preserves the current route.
- Verification order: RTL/Arabic first, then LTR/English.

## 5. Design decisions

| ID | Date | Decision | Rationale |
|---|---|---|---|
| DS-01 | 2026-08-19 | Add `warning` and `info` semantic tokens to `globals.css` + `tailwind.config.ts`, sourced verbatim from `docs/design-system/tokens/colors.css` (`--warning:#b45309`, `--warning-soft:#fcf4e6`, `--info:#4f7cff`, `--info-soft:#eef2ff`). Add matching `Alert` tone and `Badge` variant. | The canonical export defines them; their absence is the direct cause of the hardcoded `amber-*` in `/access` and `featured-courses`. This is a token *port*, not an invention. |
| DS-02 | 2026-08-19 | `docs/design-system/` is canonical and is verified byte-identical to the Claude Design export. Existing frontend code is an implementation subject to audit, never a second source of truth. | Prevents drift being ratified by precedent. |
| DS-03 | 2026-08-19 | One language toggle: `common/language-toggle.tsx`. The catalog and learning variants are deleted as their screens are reworked. | Three toggles behave differently today. |
| DS-04 | *open* | Dark mode: adopt-and-tokenize, or remove? The canonical export defines no dark theme, yet `globals.css` ships a full `.dark` palette and a visible `ThemeToggle`, and several screens hardcode `bg-white` so dark mode is already broken where exposed. | **Founder decision required.** Blocks the token pass. |

## 6. UX-dependent product/backend changes

Raised, not actioned. Each needs its own authority before any backend work.

| ID | Screen | Current behavior | Problem | Desired behavior | Backend implication |
|---|---|---|---|---|---|
| UXD-01 | ST01 Catalog | `GET /api/v1/catalog/courses` accepts `q` and pagination only | [SCREENS.md](../SCREENS.md) ST01 requires exact-match filters on Major / Subject / Study Year; the UI cannot filter | Filter by one value per taxonomy dimension, with active-filter chips and a result count | Query params + repository predicates in `backend/internal/catalogpublic`. `GET /api/v1/taxonomy/terms` already exists to populate the controls. |
| UXD-02 | ST05 Dashboard | `GET /api/v1/learn/dashboard` returns aggregate course progress only | ST05 names "Continue Learning" first; there is no resume pointer | Dashboard offers one-click resume into the exact lesson and position | Add a resume pointer (last-viewed or next-incomplete lesson id + position) to the dashboard read model. **Interim without backend change:** derive resume on Course Home, where per-lesson progress is already present. |
| UXD-03 | ST08 Resources & Labs | Material links are raw navigations to the media API; payload carries `kind` only | ST08 requires type, size, and description, plus generating/expired/denied states | A real materials screen with per-file metadata and a designed download lifecycle | Extend the learning read model's `materials[]` with filename, mime/type, size, description. |
| UXD-04 | ST09 / IN08 / AD11 Office hours | No route and no API path | Referenced by ST05, ST06, and both operations shells | Course-scoped sessions: view/join (Student), create/reschedule/cancel (Instructor), cancel (Admin) | Whole feature. Confirm it is still in MVP before designing. |
| UXD-05 | S07 Notifications | No route, no API path | ST05 and every cross-role handoff in [NAVIGATION_MAP.md](../NAVIGATION_MAP.md) assume a notification record. `AuthActions` already renders a non-functional bell. | Durable per-recipient record with read state and safe deep links | Whole feature. |
| UXD-06 | S08 Profile | No route, no `/api/v1/me/profile` | Sign-out exists only as a header button; display name, email change, language, and data requests have no home | Profile screen per S08 | Profile read/update endpoints. |
| UXD-07 | ST02 Course Details | Payload has no access term and no how-to-get-access text | The discovery→access journey dead-ends (see [JOURNEYS.md](JOURNEYS.md) Break 1) | Course Details explains how access is obtained and shows the access term | Either add fields to the public course payload, or approve static bilingual guidance copy. **Cheapest unblock: approve static copy.** |
| UXD-08 | ST03/ST04 Access | Invitation payload exposes `course_id` but the UI must show identity | Student sees a UUID | Show Course title, invited email, access term, accepted policy versions | Confirm whether the invitation payload already carries the Course title; if not, add it. |

## 7. Discovered UX debt

Real problems found during audit that are **not** presentation work and belong to their owning slice,
not this phase. Recorded so they are not lost.

| ID | Location | Issue |
|---|---|---|
| DEBT-01 | `app/[locale]/access/page.tsx` | `disabled={submitting \|\| !token}` tests the ref object rather than `token.current`, so the missing-token guard never fires. |
| DEBT-02 | `app/instructor/**` vs `app/[locale]/instructor/**` | Duplicate route trees rendering the identical component. |
| DEBT-03 | `components/layout/nav-items.ts` | Primary nav items are landing-page anchors and are dead on every other route. |
| DEBT-04 | `app/{ar,en}/{terms,privacy}` | Legal routes use hardcoded locale prefixes instead of `[locale]`; no Refund Policy route exists though S09 requires three documents. |
| DEBT-05 | `app/(auth)/onboard` | Route has no contract in [SCREENS.md](../SCREENS.md). Confirm live or legacy. |
| DEBT-06 | Product-wide | Student content reports can be filed; no Admin surface (AD10) exists to resolve them. |
