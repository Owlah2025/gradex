# Screen Inventory and Audit

> Status: Initial audit complete for all implemented routes. No code changed.
> Last Updated: 2026-08-19
> Method: static read of `frontend/src/app/**` and `frontend/src/components/**` against
> [SCREENS.md](../SCREENS.md), [NAVIGATION_RULES.md](../NAVIGATION_RULES.md), and
> [`docs/design-system/`](../design-system/). Runtime/browser verification has **not** been done.

Verdicts: `REDESIGN` (rebuild the surface) · `REFINE` (structure is sound, fix drift) ·
`PRESERVE` (canonical, use as reference) · `BUILD` (screen contract exists, no implementation) ·
`DELETE` (duplicate/legacy route).

---

## 0. Cross-cutting findings

These affect many screens and are the reason the product reads as separately designed pages.

| ID | Finding | Evidence |
|---|---|---|
| X-01 | **No Student app shell.** `RoleWorkspaceShell` covers ADMIN and INSTRUCTOR only. Every `/[locale]/learn/*` route renders a bare `<main>` with no header, no navigation, no footer, no way to reach Catalog / Profile / sign-out. [NAVIGATION_RULES.md](../NAVIGATION_RULES.md) §1 requires bottom tabs on small screens and promoted top/side nav on wide screens. | `frontend/src/components/layout/role-workspace-shell.tsx:14`; no `src/app/[locale]/layout.tsx` exists |
| X-02 | **SSR emits the wrong language and direction.** The root layout hardcodes `lang="ar" dir="rtl"`. Locale/direction are corrected client-side in a `useEffect` after mount, so every English visitor gets an RTL first paint that flips, and server-rendered content carries `lang="ar"`. | `frontend/src/app/layout.tsx:78`; `frontend/src/lib/i18n/locale-provider.tsx:52` |
| X-03 | **Locale split-brain.** The `[locale]` path segment is authoritative only for `catalog` and `learn`. Everywhere else (auth, admin, access, staff) the locale is read from `localStorage`, so `/en/access` is not necessarily English. | `frontend/src/lib/i18n/locale-provider.tsx:33-42` |
| X-04 | **No route-level loading, error, or not-found boundaries.** Zero `loading.tsx`, `error.tsx`, or `not-found.tsx` in the entire app. Server-rendered Student pages are `force-dynamic`, so navigation blocks with no feedback at all. [SCREENS.md](../SCREENS.md) S10 requires shared system states. | `find frontend/src/app -name 'loading.tsx' -o -name 'error.tsx' -o -name 'not-found.tsx'` → empty |
| X-05 | **All failures collapse to one message.** Learning routes wrap the whole page in `try { … } catch { <LearningUnavailable/> }`. 401, 403, 404, entitlement-expired, and 5xx are indistinguishable, and none offers retry or a route to sign in. [NAVIGATION_RULES.md](../NAVIGATION_RULES.md) §8 requires distinct handling per class. | `[locale]/learn/dashboard/page.tsx:56`, `[locale]/learn/courses/[courseId]/page.tsx:65`, `.../lessons/[lessonId]/page.tsx:104` |
| X-06 | **Three competing page chromes.** (a) canonical `Navbar`+`Footer`+`Container`; (b) legacy `Shell` inside `public-catalogue.tsx` with its own header, own logo text, own language toggle; (c) no chrome at all on learning routes. Moving from Catalog list → Course detail visibly changes the entire chrome mid-journey. | `public-catalogue.tsx:99` (`Shell`) vs `public-catalogue.tsx:218` (`Navbar`) |
| X-07 | **Off-palette colors in shipped code.** `teal-700/800`, `slate-50/200/600/950`, `amber-50/300/950`, `emerald-600`, `red-*`, `blue-600`, `green-*`, `gray-*` appear in production components. `docs/design-system/tokens/colors.css` defines none of these. Teal is explicitly named legacy in the export readme. | `public-catalogue.tsx:93,103,127,146,155,331,344`; `[locale]/access/page.tsx` throughout; `sections/featured-courses.tsx:94` |
| X-08 | **Three duplicate language toggles.** `common/language-toggle.tsx` (canonical), `catalog` → `CatalogueLanguageToggle`, `learning` → `LearningLocaleToggle`. Different markup, different a11y, different switching behavior. | `public-catalogue.tsx:74`; `learning/learning-locale-toggle.tsx` |
| X-09 | **Three duplicate course-card implementations**, none of them the `CourseCard` the design system names. | `sections/featured-courses.tsx:42`, `public-catalogue.tsx:252`, `learn/dashboard/page.tsx:41` |
| X-10 | **Page-local copy dictionaries.** `public-catalogue.tsx` and `featured-courses.tsx` each carry a private `copy = { ar, en }` object instead of using `lib/i18n/dictionaries/`. `/[locale]/access` has no Arabic at all — it is 100% hardcoded English strings in an Arabic-first product. | `public-catalogue.tsx:27`; `featured-courses.tsx:24`; `[locale]/access/page.tsx` (no `t.` usage) |
| X-11 | **Primary nav is dead outside the landing page.** `navItems` are in-page anchors (`#courses`, `#why`, `#faq`). On Catalog, learn, admin, and instructor screens they scroll nowhere. | `frontend/src/components/layout/nav-items.ts:12-14` |
| X-12 | **Missing design-system primitives.** The canonical export lists `ProgressBar`, `Tabs`, `Select`, `Checkbox`, `Radio`, `Switch`, `Dialog`, `Toast`, `Tooltip`, `IconButton`, `CourseCard`. None exist in `frontend/src/components/ui/`. Consequences: student progress is text-only with no bar; there is no toast/confirmation pattern anywhere in the product; every form control beyond `Input` is a raw element. | `ls frontend/src/components/ui/` |
| X-13 | **Missing semantic tokens.** `--warning`/`--warning-soft` and `--info`/`--info-soft` exist in `docs/design-system/tokens/colors.css` but are absent from `globals.css` and `tailwind.config.ts`. This is *why* pending/awaiting states hardcode `amber-*`. | `tokens/colors.css:41-44` vs `frontend/src/app/globals.css:18-50` |
| X-14 | **RTL correctness gaps.** `MobileNav` hardcodes `side="right"` rather than a logical side. Physical-direction utilities appear in several components. | `layout/mobile-nav.tsx:34` |
| X-15 | **Dark mode is an unauthorized extension.** `globals.css` ships a full `.dark` palette and a `ThemeToggle`, but the canonical design system defines no dark theme. Several screens hardcode `bg-white`, so dark mode is already broken where it is exposed. Needs a founder decision: adopt-and-tokenize, or remove. | `globals.css:52-79`; `common/theme-toggle.tsx`; `[locale]/access/page.tsx` (`bg-white`) |
| X-16 | **No breadcrumbs anywhere**, though [NAVIGATION_RULES.md](../NAVIGATION_RULES.md) §2 specifies them for Course Details, Course Home, Lesson Player, and every Instructor/Admin detail screen. | no breadcrumb component exists |
| X-17 | **Instructor/Admin shell is a chip row, not a sidebar.** [NAVIGATION_RULES.md](../NAVIGATION_RULES.md) §1 and §3 specify a persistent sidebar on wide screens; the design system defines `--sidebar-width: 264px`. Implementation is a horizontal wrapped link row under the public navbar, with 3 of the 8 specified Admin destinations. | `layout/role-workspace-shell.tsx:33`; `role-workspace-navigation.ts:52` |

---

## 1. Shared / Public / Auth

| Screen | Route | Purpose | Entry | Primary CTA | Key components | States present | Verdict |
|---|---|---|---|---|---|---|---|
| **S01 Landing** | `/` | Explain value, move to discovery | Public root, logo | Browse courses | `Navbar`, `Hero`, `FeaturedCourses`, `WhyGradex`, `LearningExperience`, `Faq`, `FinalCta`, `Footer` | loading / empty / error / ready on featured courses | **PRESERVE** — reference implementation |
| **S02 Login** | `/login` | Authenticate, return safely | Header, guard redirect | Sign in | `AuthShell`, `login-form` | submitting, invalid, suspended | **PRESERVE** |
| **S03 Register** | `/register` | Create PENDING_VERIFICATION Student | Header, landing | Create account | `AuthShell`, `registration-form` | submitting, accepted, validation | **PRESERVE** |
| **S04 Verify email** | `/verify-email`, `/verify-email/result` | Activate or explain | Email deep link | Continue | `AuthShell`, `verification-*` | pending/verified/expired/used | **PRESERVE** |
| **S05 Recover** | `/recover`, `/recover/reset` | Non-enumerating reset | Login | Send / set password | `AuthShell`, `recovery-*` | generic accepted, expired token | **PRESERVE** |
| **S05b Password change** | `/password-change` | Forced credential rotation | `PasswordChangeGuard` | Change password | `AuthShell`, `password-change-form` | submitting, error | **PRESERVE** |
| **S06 Accept staff invitation** | `/staff/accept` | Activate Instructor/Admin | Email deep link | Activate | `staff-invitation-acceptance` | valid/expired/revoked/used | **REFINE** — audit against `AuthShell`; it should share the auth chrome |
| **S06b Onboard** | `/onboard` | (undocumented in SCREENS.md) | ? | ? | `onboarding-form` | ? | **AUDIT** — route has no SCREENS.md contract; confirm it is not legacy |
| **S09 Legal** | `/en/{terms,privacy}`, `/ar/{terms,privacy}` | Versioned bilingual policy | Footer, register | Return | `legal-policy-page` | versioned content | **REFINE** — uses hardcoded `/ar` `/en` prefixes while the rest of the app uses `[locale]`; no Refund Policy route despite S09 requiring three documents |
| **S07 Notification Center** | — | Durable transactional record | — | — | — | — | **BUILD** — no route, no component, **and no backend endpoint** (`/api/v1` has no notifications path). `UX_DEPENDENT`. |
| **S08 Profile and Account** | — | Profile, language, email/password, data requests, logout | — | — | — | — | **BUILD** — no route. Sign-out exists only as a header button. No `/api/v1/me/profile` endpoint. `UX_DEPENDENT`. |
| **S10 System States** | — | 401/403/404/expired/offline/5xx | — | — | — | — | **BUILD** — see X-04, X-05. This is the single highest-leverage shared gap. |

---

## 2. Student

### ST01 — Catalog and search
- **Route** `/[locale]/catalog` · **Upstream** Landing, header, dashboard · **Downstream** Course Details
- **Goal** find a published Course · **Primary CTA** open Course Details · **Secondary** search, clear
- **Data** `GET /api/v1/catalog/courses?q=` → `{items, page, page_size, total}`
- **States present** loading, searching, error+retry, empty, no-results, ready
- **Findings**
  - **Taxonomy filters are entirely missing.** [SCREENS.md](../SCREENS.md) ST01 requires Major /
    Subject / Study Year filters, active-filter chips, and a result count. Only free-text search
    exists. The `TaxonomyTermSelect` component exists but is unused here.
  - `total` / `page` / `page_size` are returned and ignored — no result count, no pagination. A thin
    catalog hides this; a real one will not.
  - Off-palette: search toggle uses `outline-teal-700`, `border-slate-300` (X-07).
  - `formatFils` is imported and used in the list card, but `CatalogueDetail` re-implements the same
    formatting inline. Two price formatters, one screen file.
  - No sort control despite ST01 listing "sort".
- **Responsive** two-column grid only; no filter sheet on small screens (ST01 requires one).
- **Verdict** **REFINE** (shell/tokens/copy) + **BUILD** (filters, count, pagination — filters are
  `UX_DEPENDENT`, see [PATTERNS.md](PATTERNS.md) §5 UXD-01).

### ST02 — Course details
- **Route** `/[locale]/catalog/[idOrSlug]` · **Upstream** Catalog, Landing featured, search
- **Goal** evaluate a Course and learn how to get access · **Primary CTA** how to get access / Go to Course
- **Data** `GET /api/v1/catalog/courses/{idOrSlug}` → adds `description`, `sections[]`
- **Findings**
  - **Uses the legacy `Shell`** while its own list page uses `Navbar`/`Footer`. Chrome changes
    mid-journey (X-06). Most visible inconsistency in the Student funnel.
  - Hardcoded `text-slate-600`, `text-teal-800`, `bg-white`, `border-amber-300 bg-amber-50` (X-07).
  - **No access term, no how-to-get-access guidance, no `Go to Course` state for an entitled
    Student, no public-preview player.** All four are required by ST02. `has_preview` is rendered as
    a sentence of text with no way to play it.
  - No back affordance to the catalog; no breadcrumb (X-16).
  - Section list shows title + lesson count only — no Resources/Labs summary, no office-hours signal.
  - Loading is a bare unstyled `<p>`; 404 and 5xx both render the same amber block.
- **Verdict** **REDESIGN**.

### ST03/ST04/ST10 — Course access invitation, access status, access history
- **Route** `/[locale]/access` (all three screen contracts collapsed into one page)
- **Goal** accept an invitation; know where access stands · **This is the only path to access in the product.**
- **Data** `GET /api/v1/me/course-access-invitations/{id}`, `POST …/accept`, `GET /api/v1/me/course-access`
- **Findings — worst-implemented screen in the product**
  - **Zero Arabic.** Every string is hardcoded English (X-10). Arabic is the default locale.
  - **Zero design system.** `text-gray-900`, `bg-white`, `bg-red-50`, `bg-blue-600`, `bg-emerald-600`,
    `bg-amber-50`, `border-blue-500` — none are Gradex tokens. Breaks dark mode outright.
  - **Leaks raw identifiers to the Student:** renders `Course ID: {course_id}` (a UUID) and
    `Status: PENDING_ADMIN_APPROVAL` (a raw wire enum) as user-facing text. The Course *title* is
    never shown. ST04/ST10 require the Course identity, not its key.
  - `new Date(...).toLocaleString()` with no locale or timezone argument — wrong dates in Arabic.
  - Title "Student Course Access Portal" is not product language; [GLOSSARY.md](../GLOSSARY.md) has
    no "portal".
  - Three of ST03's required content items are absent: the invited email address, the access term
    that would apply, and the accepted policy versions.
  - Missing states: `CANCELLED`, expired link + resend, wrong-identity-signed-in refusal.
  - `disabled={submitting || !token}` tests the ref object, not `token.current` — the guard never
    fires. (Correctness issue found during UX audit; belongs to the owning slice, not this phase.)
  - No shell, no back path, no route to the dashboard.
- **Verdict** **REDESIGN**. Highest Student risk in the product.

### ST05 — Student dashboard
- **Route** `/[locale]/learn/dashboard` · **Upstream** login redirect, header · **Downstream** Course Home
- **Goal** resume learning; see owned/expired Courses · **Primary CTA** should be *Continue learning*
- **Data** `GET /api/v1/learn/dashboard` → `{courses:[{course_id,title,learning_status,expires_at,progress}]}`
- **Findings**
  - **No "Continue Learning".** ST05 names it first. The payload carries no resume pointer, so this
    is `UX_DEPENDENT` (UXD-02) unless implemented as a two-hop dashboard → Course Home → resume.
  - Missing from ST05: upcoming office hours, recent notifications, pending invitations awaiting
    acceptance or approval, and any link to Access History. A Student with a pending invitation sees
    nothing here about it.
  - No shell (X-01) — no header, no nav, no sign-out, no route to Catalog.
  - Progress is a text fraction; no `ProgressBar` exists (X-12).
  - Empty state is a bare bordered box with no CTA — it does not point to the catalog. Violates the
    `EmptyState` contract, and `EmptyState` is not used.
  - Card CTA is a raw `<Link className="rounded-md border …">` styled to look like a button rather
    than `Button asChild` — so it misses the design system's pill radius, focus ring, and press state.
  - Near-expiry state (ST05) is not distinguished from active.
  - All failures → one message (X-05).
- **Verdict** **REDESIGN**.

### ST06 — Course home
- **Route** `/[locale]/learn/courses/[courseId]` · **Upstream** dashboard, access page · **Downstream** Lesson
- **Data** `GET /api/v1/learn/courses/{id}` → sections → lessons with per-lesson `progress` + `materials`
- **Findings**
  - No shell (X-01), no breadcrumb (X-16).
  - **Back-to-dashboard link is at the bottom of the page**, after the whole outline, and its label is
    the dashboard's *title* rather than a back affordance. [NAVIGATION_RULES.md](../NAVIGATION_RULES.md)
    §6 requires an always-visible Course/root affordance.
  - **No resume affordance.** Per-lesson `position_seconds` and `completed` are present in the
    payload, so "resume where you left off" is derivable client-side here — no backend change needed.
    Not implemented.
  - Lesson rows show a raw seconds string (`"127 seconds · not completed"`) rather than a duration or
    a completion indicator. No completion checkmark, no non-color state marker.
  - Material links are dumped inline under every lesson row as anonymous "Resource" / "Lab" chips —
    ST08's Resources & Labs screen does not exist (see below).
  - Expired-access state renders the outline but silently drops material links, with no explanation.
- **Verdict** **REDESIGN**.

### ST07 — Lesson player
- **Route** `/[locale]/learn/courses/[courseId]/lessons/[lessonId]`
- **Data** `GET /api/v1/learn/…/lessons/{id}`, `POST` playback authorization, progress reporter
- **Findings**
  - Player *logic* is strong: HLS, quality state machine, resume seek, progress reporting, transient-
    vs-fatal failure separation, keyboard-labelled controls. **Preserve the logic.**
  - **Layout is inverted.** Order is: header → materials chips → video. Materials sit above the video.
  - **No lesson rail or outline**, at any breakpoint. ST07 and [NAVIGATION_RULES.md](../NAVIGATION_RULES.md)
    §3 both require a persistent rail on desktop and a sheet on mobile. The Student cannot see where
    they are in the Course while watching.
  - No "back to Course" affordance except Previous/Next at the bottom.
  - Player loading and failure states are unstyled bare `<p>` elements with no retry.
  - `<h1>` is the lesson title with the section title as a `<p>` above it — Course identity never
    appears on the page.
  - Not immersive/focused chrome as [NAVIGATION_RULES.md](../NAVIGATION_RULES.md) §2 specifies.
- **Verdict** **REDESIGN** the surface, **PRESERVE** `lesson-player.tsx` / `player-controls.tsx`
  logic and the `progress-reporter` contract.

### ST08 — Lesson resources and labs
- **Route** — none. Material links navigate the browser directly to
  `/api/v1/media/lessons/{id}/materials/{resource|lab-material}`.
- **Findings** No screen exists. ST08 requires separate Resource and Lab lists with type, size, and
  description; generating-link, expired/retry, denied, and unavailable states. Today a click is a
  raw navigation with no feedback, no error surface, and no return path.
- **Verdict** **BUILD**. Type/size/description are not in the current payload → `UX_DEPENDENT` (UXD-03).

### ST09 — Office hours
- **Route** — none. No component, no API path.
- **Verdict** **BUILD** / `UX_DEPENDENT` (UXD-04). Referenced by ST05, ST06, and the Instructor and
  Admin shells, so its absence leaves dangling promises across three personas.

---

## 3. Instructor

| Screen | Route | Implementation | Findings | Verdict |
|---|---|---|---|---|
| **IN01 Dashboard / Courses** | `/[locale]/instructor/courses` **and** `/instructor/courses` | `course-builder.tsx` | **Duplicate routes render the identical component.** One must go. There is no dashboard — the route lands straight in the builder, so lifecycle state, video failures, and change-request response have no home. | **DELETE** the un-localed twin; **BUILD** the dashboard |
| **IN02 Course Builder** | same | `course-builder.tsx`, `taxonomy-assignment-panel.tsx`, `server-pricing-panel.tsx` | Audit not yet run in depth. Confirm: autosave/sync indicator, read-only `PENDING_REVIEW`, missing-classification blocking, reorder on small screens. | `NOT_AUDITED` |
| **IN03 Lesson editor** | — | `lesson-video-upload.tsx` | No dedicated route; upload lives inside the builder. Dirty/upload-leave guard not verified. | `NOT_AUDITED` |
| **IN04 Resources and Labs manager** | — | `lesson-resource-upload.tsx` | Same. | `NOT_AUDITED` |
| **IN05 Public preview manager** | — | none found | **BUILD** |
| **IN06 Submit / review status** | — | inside builder | Checklist, missing-item links, Admin reason/history not confirmed present. | `NOT_AUDITED` |
| **IN07 Course analytics** | — | none found | **BUILD** |
| **IN08 Instructor office hours** | — | none found | **BUILD** / `UX_DEPENDENT` |
| **Shell** | `instructor/layout.tsx` | `RoleWorkspaceShell` | Chip row, not sidebar (X-17). Nav has 2 entries vs the 6 in [NAVIGATION_RULES.md](../NAVIGATION_RULES.md) §1. One entry is a `#course-builder` fragment. | **REDESIGN** |

## 4. Admin

| Screen | Route | Implementation | Findings | Verdict |
|---|---|---|---|---|
| **AD01 Admin Ops** | — | none | No operations home. Admin lands on the review queue. | **BUILD** |
| **AD02 Users and invitations** | `/staff` | `staff-management.tsx` | Outside the `[locale]` tree, so it never gets a locale segment (X-03). | `NOT_AUDITED` |
| **AD03 Pricing** | `/[locale]/admin/catalog` (embedded) | `pricing-panel.tsx`, `pricing-form.tsx`, `pricing-history-table.tsx` | Merged into the catalog workspace rather than its own destination. | `NOT_AUDITED` |
| **AD04 Course review queue** | `/[locale]/admin/catalog` | `review-queue.tsx` | `NOT_AUDITED` | `NOT_AUDITED` |
| **AD05 Content review** | same | `review-lesson-preview.tsx`, `submitted-revision-inspector.tsx`, `lifecycle-controls.tsx` | Recently unified (`5f3a2b3`). | `NOT_AUDITED` |
| **AD06 Course access invitations** | `/[locale]/admin/course-access` | `published-course-selector.tsx` | The Admin half of the Student's only access path — audit alongside ST03. | `NOT_AUDITED` |
| **AD07 Entitlement detail** | — | none found | **BUILD** |
| **AD10 Reported content** | — | none found | **BUILD** — Students can file reports today with no Admin surface to resolve them |
| **AD11 Office-hours moderation** | — | none | **BUILD** / `UX_DEPENDENT` |
| **AD12 Catalog taxonomy** | `/[locale]/admin/catalog` | `taxonomy-vocabulary-panel.tsx`, `taxonomy-term-management.tsx`, `taxonomy-override-form.tsx` | `NOT_AUDITED` | `NOT_AUDITED` |
| **Shell** | `[locale]/admin/layout.tsx` | `RoleWorkspaceShell` | 3 of 8 specified destinations; one points outside the locale tree (X-17). | **REDESIGN** |

---

## 5. What is canonical vs legacy

**Canonical — reuse and extend.**
`components/ui/*` primitives · `components/layout/{navbar,footer,container,section,auth-actions}` ·
`components/brand/*` · `components/common/{empty-state,skip-link,language-toggle,reveal}` ·
`components/auth/auth-shell.tsx` · `components/sections/*` · `lib/i18n/dictionaries/*` ·
`lib/formatters/*` · `components/learning/{lesson-player,player-controls,progress-reporter}` logic.

**Legacy — replace, do not extend.**
`Shell` / `CatalogueLanguageToggle` / `Taxonomy` / `Price` / `Failure` inside
`components/catalog/public-catalogue.tsx` · `components/learning/learning-locale-toggle.tsx` ·
page-local `copy` objects · every raw `<Link className="rounded-md border …">` button lookalike ·
the whole of `app/[locale]/access/page.tsx` · `app/instructor/*` (duplicate of `app/[locale]/instructor/*`).

**Missing — build once, in the design system, before the pages that need it.**
`AppShell` (Student) · `Sidebar` (Instructor/Admin) · `Breadcrumb` · `ProgressBar` · `Toast` ·
`Dialog` · `Select` · `Skeleton` · `StatusBadge` (invitation/entitlement/lifecycle states) ·
`CourseCard` · `Pagination` · `FilterChips` · system-state pages (401/403/404/5xx/offline).
