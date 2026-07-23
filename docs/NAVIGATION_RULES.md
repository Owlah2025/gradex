# Navigation Rules

> Status: Aligned with approved MVP
> Last Updated: 2026-07-23

Navigation behavior for the responsive Gradex website. Screen definitions live in
[SCREENS.md](SCREENS.md); route relationships in [NAVIGATION_MAP.md](NAVIGATION_MAP.md).

## 1. App Shells

- **Public/Student:** public top navigation. Authenticated small screens use bottom tabs
  `Home · Browse · Notifications · Profile`; tablet/desktop layouts promote these destinations to
  wider top/side navigation. Checkout and Lesson Player may use focused/immersive chrome.
- **Instructor:** responsive shell with `Dashboard · Courses · Analytics · Office Hours ·
  Notifications · Profile`. Persistent sidebar on wide screens; drawer/collapsed rail on smaller
  screens. No payout/earnings destination.
- **Admin:** responsive operations shell with `Ops · Course Review · Users · Pricing · Coupons ·
  Revenue · Refunds · Payouts · Reports`. Persistent sidebar on wide screens and drawer/collapsed
  rail on smaller screens.

Student functionality is complete across phones, tablets/iPads, laptops, and desktops. Instructor
and Admin shells remain responsive, while complex operational screens are optimized for
tablet/laptop/desktop (BR-147/148).

## 2. Chrome by Screen

`✓` present · `✗` absent · `~` conditional.

### Shared and Student

| Screen | Header | Student Tabs | Breadcrumb/Context | Back/Close |
|---|:---:|:---:|:---:|:---:|
| Landing / Catalog | ✓ | ~ authenticated | ~ desktop | ~ |
| Course Details | ✓ | ~ | ~ desktop | ✓ |
| Login / Register / Verify / Accept Invitation | logo | ✗ | ✗ | ✓ |
| Forgot / Reset Password | logo | ✗ | ✗ | ✓ |
| Checkout | minimal | ✗ | ✗ | cancel |
| Payment Confirming / Receipt | minimal | ✗ | ✗ | controlled (§6) |
| Student Dashboard | ✓ | ✓ | ✗ | ✗ root |
| Course Home | ✓ | ✓ | ~ desktop | Dashboard |
| Lesson Player | overlay/minimal | ✗ | ~ desktop | Course/history (§6) |
| Resources & Labs | ✓ | ~ | ~ desktop | Course/Player |
| Office Hours | ✓ | ✓ | ~ | Course/Dashboard |
| Orders & Refunds | ✓ | ✓ | ~ | Profile/Dashboard |
| Notification Center | ✓ | ✓ | ✗ | prior/root |
| Profile / Language / Account | ✓ | ✓ | ✗ | prior/root |
| Legal | ✓ | ✗ | ✗ | prior/home |

### Instructor

| Screen | Sidebar | Breadcrumb | Back/Close |
|---|:---:|:---:|:---:|
| Dashboard / Course List | ✓ | ✗ | ✗ root |
| Course Builder | ✓ | Dashboard / Course | ✓ |
| Lesson Editor / Materials | ✓ | Dashboard / Course / Lesson | ✓ + dirty guard |
| Public Preview Manager | ✓ | Dashboard / Course / Preview | ✓ + dirty/upload guard |
| Submit / Review Status | ✓ | Dashboard / Course / Review | ✓ |
| Course Analytics | ✓ | Dashboard / Course / Analytics | ✓ |
| Office Hours | ✓ | Dashboard / Office Hours | ~ |

### Admin

| Screen | Sidebar | Breadcrumb | Back/Close |
|---|:---:|:---:|:---:|
| Ops / User / Pricing / Coupon / Revenue roots | ✓ | ✗ | ✗ root |
| Invitation/User detail | ✓ | Users / Account | drawer or ✓ |
| Moderation Queue / Content Review | ✓ | Queue / Course | ~ / ✓ |
| Refund detail | ✓ | Refunds / Order | ✓ |
| Payout Run / Statement | ✓ | Payouts / Period / Instructor | ✓ |
| Reported Content / Resolution | ✓ | Reports / Item | ✓ |
| Office-Hours moderation | ✓ | Ops / Office Hours | ✓ |

## 3. Responsive Behavior

| Surface | Small screen | Tablet | Laptop/Desktop |
|---|---|---|---|
| Student shell | Bottom tabs; sheets for filters/lists | Tabs or compact rail; two-column where useful | Top navigation; persistent filter/lesson rail where useful |
| Lesson Player | Responsive/fullscreen video; Lesson list sheet | Video + collapsible Lesson drawer | Video + persistent Lesson rail |
| Instructor/Admin shell | Header + drawer; stacked forms/tables | Collapsible rail; responsive table/card choice | Persistent sidebar; full table/workspace |
| Course Builder | Usable single-column edits; reorder/upload may advise larger screen | Full builder with collapsible outline | Full multi-pane builder |
| Financial/moderation tables | Essential fields/cards; drill-in | Scroll/column prioritization | Full table + filters/detail pane |

No route may be desktop-only. A complex screen may recommend a larger display but must provide a
safe responsive view and must not pretend unsupported edits succeeded.

## 4. Locale and Direction

- Arabic is the first default when no saved preference exists; English is always selectable.
- Language choice persists across public/authenticated routes and does not change Course-authored
  content.
- Direction switches at the document/shell level. Navigation order, chevrons, breadcrumbs, drawers,
  tables, form alignment, and animation direction follow RTL/LTR semantics—not visual mirroring by
  exception.
- Route identifiers remain locale-neutral; localized slugs may be added only with a canonical URL
  and redirect policy.
- Dates/times use the selected locale and user timezone; office hours default to Kuwait time when
  no timezone is known.

## 5. Guarded Routes and Direct URLs

```text
Request route
├── Public route → render
└── Guarded route
    ├── unauthenticated → Login (capture safe returnTo)
    ├── pending verification → Verify Email
    ├── suspended/deactivated → blocked account state
    ├── wrong role/ownership → 403 without existence leak
    ├── missing/expired entitlement → Course Details / expired access state
    └── authorized → render
```

Rules:

- `returnTo` survives Login/Register/Verification but is validated as an internal allowed route.
- Public shareable routes are Landing, Catalog/Search, Course Details/Public Preview, and Legal.
- Protected Lesson, file, office-hours, report, Order/refund, Instructor, and Admin routes always
  pass the server-side role/ownership/status/entitlement gate.
- A Section entitlement grants only its Lessons but grants Course-scoped office-hours access.
- Admin Course preview is an audited role permission, not a fake Student Entitlement.
- Hosted Tap checkout is external; redirect controls navigation only, never payment truth.

## 6. History and Back Semantics

- In-app Back and browser Back should agree unless a documented terminal flow protects users.
- Shell roots (Student Dashboard/Catalog, Instructor Dashboard, Admin Ops) are stable roots.
- Checkout Back cancels/returns to Course Details without creating access.
- Successful Receipt replaces the transient checkout-return entry so Back cannot reopen a completed
  payment form; explicit CTAs go to Course Home, first Lesson, or Orders.
- Each Lesson has its own route. Next/Previous/auto-advance pushes history; Back retraces Lessons.
- An always-visible Course affordance returns directly to Course Home without traversing all Lesson
  history. A direct Lesson deep link falls back to Course Home when no in-app history exists.

## 7. Unsaved Changes and Uploads

Dirty screens include Course/Section/Lesson edits, materials/preview uploads, profile changes,
Admin price reason, change-request/report resolution, refund reason/amount, and payout reference.

```text
Leave dirty screen
├── no unsaved work → navigate
└── unsaved work → Save draft | Discard | Stay
```

- Course Builder autosave shows sync state; intercept only when change is unsynced/in-flight.
- Leaving during an upload explains whether it cancels or can resume; it never silently abandons.
- Browser/tab close receives the appropriate native warning when reliable recovery is impossible.
- `PENDING_REVIEW` content is read-only and therefore cannot become dirty for the Instructor.
- Submitted refunds, price changes, and payout records are immutable events/state transitions, not
  unsaved form state rewritten after success.

## 8. Error and Empty-State Navigation

- 401: authenticate/re-authenticate and return safely.
- 403 wrong role/ownership: neutral Access Denied with role-appropriate root CTA.
- Missing/expired Student Entitlement: Course Details/expired state with allowed purchase path.
- 404: no entity/existence leak; return to role root/catalog.
- Payment/Refund pending: retain stable status page and poll/reconcile; do not infer failure.
- Offline/5xx: preserve safe local form state where possible and offer retry.
- Empty states point only to authorized MVP actions—never to ratings, payout dashboards,
  notification preferences, platform-wide office hours, bundles, or BNPL.
