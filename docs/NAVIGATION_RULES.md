# NAVIGATION RULES

> Status: Draft
> Last Updated: 2026-07-21

Navigation **behavior** layer for the Gradex MVP — chrome, responsive targets, and the cross-cutting rules (guarded URLs, unsaved changes, browser-back semantics) that [SCREENS.md](SCREENS.md) and [NAVIGATION_MAP.md](NAVIGATION_MAP.md) don't cover. Locking these before wireframing because they're expensive to retrofit.

**Source chain:** [SCREENS](SCREENS.md) → [NAVIGATION_MAP](NAVIGATION_MAP.md) → **Navigation Rules** → Wireframes.

Two decisions here are genuine product-policy forks (marked **⟶ ratify in DECISIONS.md**): Lesson-Player back semantics, and the unsaved-changes intercept pattern.

---

## App shells

Chrome is not per-screen invention — it comes from one shell per role.

- **Student shell (mobile-first):** bottom tab bar = **`Home` · `Browse` · `Notifications` · `Profile`**. Top header carries context + search. Learning surfaces (player, checkout) are full-screen pushes that *suppress* the tab bar. On desktop the tab bar promotes to top-nav links; a left rail appears for filters (Catalog) and lesson list (Player).
- **Instructor shell (desktop-first):** persistent **left sidebar** = `Dashboard · Analytics · Payout Statements` (+ current-course context section). Top header. No bottom nav. Breadcrumbs on nested build screens.
- **Admin shell (desktop-first):** persistent **left sidebar** = `Ops · Moderation Queue · Users · Revenue · Refunds · Payouts · Reports`. Top header. No bottom nav.

---

## 1. Navigation chrome — per screen

`✓` present · `✗` absent · `~` conditional. "Back" = an explicit in-app back/close affordance (distinct from browser Back — see §5).

### Shared

| Screen | Header | Bottom Nav | Breadcrumb | Sidebar | Back |
|--------|:--:|:--:|:--:|:--:|:--:|
| Landing | ✓ | ✗ | ✗ | ✗ | ✗ |
| Login / Register | ~ logo only | ✗ | ✗ | ✗ | ✓ |
| Forgot / Reset Password | ~ logo only | ✗ | ✗ | ✗ | ✓ |
| Account / Settings | ✓ | ~ role shell | ✗ | ~ role shell | ✓ |
| Profile | ✓ | ~ role shell | ✗ | ~ role shell | ✓ |
| Notification Center | ✓ | ~ role shell | ✗ | ~ role shell | ✓ |
| Legal | ✓ | ✗ | ✗ | ✗ | ✓ |
| System Error & Empty States | ~ minimal | ✗ | ✗ | ✗ | ✓ home CTA |

### Student (mobile-first)

| Screen | Header | Bottom Nav | Breadcrumb | Sidebar | Back |
|--------|:--:|:--:|:--:|:--:|:--:|
| Catalog | ✓ search | ✓ (`Browse` root) | ✗ | ~ filter rail (desktop) | ✗ |
| Search Results | ✓ | ✓ | ✗ | ~ filter rail | ✓ |
| Course Details | ✓ | ✓ | ~ desktop | ✗ | ✓ |
| Checkout | ~ minimal | ✗ | ✗ | ✗ | ✓ cancel |
| Payment Success / Receipt | ✓ | ✗ | ✗ | ✗ | ✗ (suppressed, §5) |
| Student Dashboard | ✓ | ✓ (`Home` root) | ✗ | ✗ | ✗ |
| Course Home | ✓ | ✓ | ~ desktop | ✗ | ✓ → Dashboard |
| Lesson Player | ~ overlay | ✗ (immersive) | ~ desktop | ~ lesson rail (desktop) | ✓ → §5 |
| Lesson Resources & Labs | ✓ | ~ | ~ desktop | ✗ | ✓ → Course/Player |

### Instructor (desktop-first)

| Screen | Header | Bottom Nav | Breadcrumb | Sidebar | Back |
|--------|:--:|:--:|:--:|:--:|:--:|
| Instructor Dashboard | ✓ | ✗ | ✗ | ✓ (root) | ✗ |
| Course Builder | ✓ | ✗ | ✓ Dash / Course | ✓ | ✓ |
| Lesson Editor | ✓ | ✗ | ✓ Dash / Course / Lesson | ✓ | ✓ ⚠ unsaved §4 |
| Resources & Labs Manager | ✓ | ✗ | ✓ …/ Lesson / Materials | ✓ | ✓ ⚠ unsaved §4 |
| Submit for Review | ✓ | ✗ | ✓ Dash / Course | ✓ | ✓ |
| Course Analytics | ✓ | ✗ | ✓ Dash / Course / Analytics | ✓ | ✓ |
| Payout Statements | ✓ | ✗ | ✗ | ✓ (root) | ✗ |

### Admin (desktop-first)

| Screen | Header | Bottom Nav | Breadcrumb | Sidebar | Back |
|--------|:--:|:--:|:--:|:--:|:--:|
| Admin Ops Landing | ✓ | ✗ | ✗ | ✓ (root) | ✗ |
| Moderation Queue | ✓ | ✗ | ✗ | ✓ | ✗ |
| Content Review | ✓ | ✗ | ✓ Queue / Course | ✓ | ✓ |
| User Management | ✓ | ✗ | ✗ (+ detail drawer) | ✓ | ~ drawer close |
| Revenue Dashboard | ✓ | ✗ | ✗ | ✓ | ✗ |
| Refunds | ✓ | ✗ | ✗ (+ order lookup) | ✓ | ~ |
| Payouts | ✓ | ✗ | ✓ Payouts / Run | ✓ | ✓ into run |
| Reported Content | ✓ | ✗ | ✗ | ✓ | ✗ |

---

## 2. Responsive behavior — per role, with screen exceptions

| Role | Primary target | Mobile nav | Tablet nav | Desktop nav |
|------|----------------|-----------|-----------|-------------|
| Student | **Mobile** | Bottom tabs + full-screen pushes; filters/lists as bottom sheet | Bottom tabs + 2-col grid + drawer | Top nav (tabs promote to links) + left rail; no bottom tabs |
| Instructor | **Desktop** | Header + hamburger drawer, single column, stacked (usable, not optimized) | Collapsible icon-rail sidebar + drawer | Persistent left sidebar + header |
| Admin | **Desktop** | Header + drawer; triage/read realistic, heavy ops degraded | Collapsible sidebar | Persistent left sidebar + header |

**Rule:** default a screen to its **role's primary target**. Never emit a desktop-first layout for a student screen or a mobile-first layout for instructor/admin.

**Screen exceptions worth pinning now:**

```
Lesson Player          Primary: Mobile
  Mobile   fullscreen video · controls overlay · lesson list = bottom sheet · tab bar hidden
  Tablet   video + collapsible lesson drawer
  Desktop  video + persistent right lesson rail + notes column

Catalog filters        Primary: Mobile
  Mobile   "Filters" button → bottom sheet
  Desktop  left filter sidebar (always visible)

Course Builder         Primary: Desktop (reorder + upload are desktop-recommended)
  Mobile   view/light-edit only; drag-reorder + bulk upload degrade — warn, don't block
```

---

## 3. Direct URL / guarded-route access — global rule

Every guarded route resolves through one gate. Applies to typed URLs, shared links, refresh, and bookmarks.

```
Request a route
├── Public route (Landing, Catalog, Search, Course Details, Legal)
│     └── render (shareable deep links allowed here)
└── Guarded route
      ├── Not authenticated ──► Login  [capture returnTo]
      │        └── on success ──► original route (works for Register too)
      ├── Authenticated, wrong role ──► 403 (no role/existence leak)
      ├── Authenticated, right role, no entitlement (e.g. /course/123/lesson/5 not owned)
      │        └── ──► Course Details (upsell), not a raw 403 oracle
      └── Entitlement expired ──► 403 Access Denied ──► Checkout (re-buy)   (BR-023/024/025)
```

Rules:
- **`returnTo` is preserved across both Login and Register**, and survives the auth round-trip (generalizes the deep-link-return already noted in T3/T4).
- **No existence oracle:** a not-owned or non-existent course resolves to the same public Course Details / not-found path — don't let 403-vs-404 leak what exists (consistent with BR-003 no-email-leak stance).
- **Only public screens are shareable.** Authed deep links always pass through the gate above; they never render for a logged-out viewer.
- Hosted gateway checkout is `[external]` — return is by gateway redirect **plus** webhook, never client-side trust (BR-020).

---

## 4. Unsaved changes — global rule  ⟶ ratify in DECISIONS.md

**Dirty-state screens:** Course Builder, Lesson Editor, Resources & Labs Manager, Profile, Account/Settings, and admin free-text actions (reject reason, refund note).

```
User leaves a dirty screen  (in-app Back · nav click · tab close · browser Back)
└── Unsaved changes?
      ├── No  ──► navigate
      └── Yes ──► intercept
            ├── Save draft  ──► persist ──► navigate
            ├── Discard     ──► drop changes ──► navigate
            └── Cancel      ──► stay
```

Nuances:
- **Course Builder autosaves** (SCREENS.md) → intercept fires only for an in-flight/unsynced change, not routine edits.
- **Uploads in progress** (Lesson Editor, Resources & Labs Manager) → intercept warns "leaving cancels this upload."
- Guard **tab/browser close** too (`beforeunload`), not just in-app nav.
- Read-only states (course Pending Approval, BR-016) are never dirty → no intercept.

---

## 5. Browser history & back semantics — global rule + Lesson Player decision

**General:** in-app Back and browser Back **agree** — no divergent history. Shell roots (Student Dashboard, Catalog, Instructor Dashboard, Admin Ops Landing) are history roots (Back doesn't wander mid-app). Flow terminals control their own history:

- **Checkout** Back = cancel → Course Details.
- **Payment Success / Receipt** = `history.replace` on success → **Back does not return to a completed payment** (prevents re-pay confusion). Forward is via explicit CTAs only.

### Lesson Player — the decision  ⟶ ratify in DECISIONS.md

Question raised: watch Lesson 1 → 2 → 3, press Back — go to **Lesson 2** or **Course Home**?

**Decision:**
1. **Each lesson is its own route** (`/course/:id/lesson/:n`).
2. **Lesson→lesson navigation PUSHES history** — manual *Next/Prev* and **auto-advance** at ≥90% (BR-051) both push. So **Back from Lesson 3 → Lesson 2 → Lesson 1** — Back retraces what the student actually moved through. Predictable, matches expectation.
3. **Entering a lesson from Course Home pushes over it** → from the *first* lesson opened, Back = Course Home.
4. **Deep link straight into a lesson** (empty in-app history) → Back falls back to **Course Home** (synthesized), never out of the app.
5. **Course Home is always one tap** via a dedicated chrome affordance (breadcrumb on desktop, back-to-course chevron in the mobile overlay header) — **independent of history depth**, so a binge-watcher never has to Back through every lesson to reach the overview.

```
Course Home
   └─(open L1, push)─► Lesson 1 ─(Next, push)─► Lesson 2 ─(Next, push)─► Lesson 3
                                                                            │
   Browser/in-app Back retraces: L3 ◄─ L2 ◄─ L1 ◄─ Course Home
   Chrome "◄ Course" affordance: jumps to Course Home from any lesson, any depth
```

Rejected alternative: `history.replace` on advance (Back from any lesson → Course Home). Cleaner stack but violates the "Back undoes my last move" expectation and makes rewatching the previous lesson harder. Chosen push+affordance gives both.

---

## Open items to ratify

- **D-candidate:** Lesson-Player back semantics (§5) — push-per-lesson + always-available Course-Home affordance.
- **D-candidate:** unsaved-changes intercept (§4) — Save draft / Discard / Cancel, + autosave & upload nuances.
- **D-candidate:** receipt back-suppression via `history.replace` (§5).

Fold these into [DECISIONS.md](DECISIONS.md) when the design phase's decisions are batched.
