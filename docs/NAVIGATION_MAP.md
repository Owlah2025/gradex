# NAVIGATION MAP

> Status: Draft
> Last Updated: 2026-07-21

Per-role navigation trees for the Gradex **MVP**, derived from the Entry/Exit edges in [SCREENS.md](SCREENS.md). Shows the primary paths a user takes through the product — the skeleton wireframes hang off. If a path isn't here, it isn't in the MVP.

**Source chain:** [USER_JOURNEYS](USER_JOURNEYS.md) → [SCREENS](SCREENS.md) → **Navigation Map** → Wireframes → UI Mockups.

## Legend

```
├── └── │      parent → child navigation edge
[modal]        opens over parent, not a route (see SCREENS.md demotions)
[state]        a state of the parent screen, not a route
[external]     leaves the app (e.g. Discord, hosted gateway page)
◄─ back        notable return path
(loop)         repeats / cycles back
► goes to      jump to a node documented elsewhere in this map
```

Cross-cutting screens (Notification Center, Account/Settings, Profile, Legal, Error states) are reachable from **any** authenticated screen via global chrome — mapped once under **Global**, not repeated in each tree.

---

## Shared — entry & auth spine

Every role enters here. Auth routes by role on success.

```
Landing
├── Catalog                          ► STUDENT tree
├── Course Details                   ► STUDENT tree (public, shareable deep link)
├── Login
│     ├── [role redirect] ──► Student Dashboard / Instructor Dashboard / Admin Ops Landing
│     ├── [deep-link return] ──► Checkout        (buy-while-logged-out, T3)
│     ├── Forgot Password
│     │     └── Reset Password        [external: email link]
│     └── Register
├── Register
│     ├── [role redirect / deep-link return]
│     └── Login
└── Legal (Terms / Privacy / Refund)
```

---

## Global — reachable from any authenticated screen

```
(top nav / account menu / footer, any role)
├── Notification Center
│     └── [deep link] ──► Receipt / Course Home / Lesson Player / Moderation item
├── Account / Settings
├── Profile
├── Legal
└── System Error & Empty States
      ├── 403 Access Denied / Enrollment Expired ──► Checkout (re-buy, BR-024/025)
      ├── 404 Not Found ──► Landing / Dashboard
      └── 500 / Offline ──► retry
```

---

## Student

Mobile-first. Public discovery flows into the authed learning loop.

```
Landing
└── Catalog
      ├── Search Results
      │     └── Course Details
      └── Course Details
            ├── Lesson Preview                     [modal]
            ├── [owned] "Go to course" ──►  Course Home        (BR-024)
            └── Checkout                            (auth-gated; login/register then return)
                  ├── Processing "confirming…"      [state]     (webhook lag, BR-020)
                  ├── Failed / declined ──► retry   [state]     (BR-022)
                  └── Payment Success / Receipt
                        ├── "Start first lesson" ──► Lesson Player
                        └── Student Dashboard
                              │
   ┌──────────────────────────┘
   │
Student Dashboard
├── "Continue learning" ──► Lesson Player            (resume exact position, T8)
├── Catalog                                          (browse more)
├── Profile
└── Course Home
      ├── Lesson Player
      │     ├── next / prev lesson                   (auto-advance, loop)
      │     ├── Lesson Resources & Labs
      │     └── ◄─ back to Course Home
      ├── Lesson Resources & Labs
      │     ├── download resources / lab materials
      │     └── Community link-out                   [external: Discord, T7]
      └── Community link-out                          [external]
```

**Loop:** Dashboard ⇄ Course Home ⇄ Lesson Player ⇄ Resources is the repeat learning cycle (watch → practice → return) until course done or access expires (silent expiry, D-009).

---

## Instructor

Desktop-first. Provisioned manually → lands on dashboard post-login. No earnings figures anywhere (BR-064/074).

```
Instructor Dashboard
├── Course Builder                     (new / edit course)
│     ├── Lesson Editor
│     │     ├── video upload ──► transcode status (Uploading→Processing→Ready / Failed)
│     │     └── Resources & Labs Manager
│     ├── Resources & Labs Manager     (2 buckets: resources / labs)
│     └── Submit for Review
│           ├── [blocked] pre-submit checklist ──► fix-jump ◄─ Course Builder
│           └── [submitted] ──► Instructor Dashboard   (course = Pending Approval)
├── Course Analytics                   (enrollments, completion funnel, roster)
├── Payout Statements                  (download PDF/CSV; no earnings figures)
└── Review Outcome                     [notification + Dashboard status]
      ├── Approved ──► course Published
      └── Rejected + reason ──► Course Builder   (back to Draft, editable, BR-072)
```

**Cycle:** Build → Submit → (rejected → revise → resubmit) → Published → edit live (pending-revision, live stays up) → re-review (BR-016/017/090).

---

## Admin

Desktop-first. Privileged, audited. Only admin sees PII (BR-101).

```
Admin Ops Landing                       (queue depth · pending refunds · failed transcodes)
├── Moderation Queue
│     └── Content Review
│           ├── audited lesson preview           (no enrollment, BR-081)
│           ├── Approve ──► Published + notify    (BR-071) ◄─ back to Queue
│           └── Reject + required reason ──► Draft (BR-072) ◄─ back to Queue
├── User Management
│     └── Suspend / reinstate            (reason + audit; student vs instructor differ, BR-007/065)
├── Revenue Dashboard
│     └── Refunds                        (drill-in)
├── Refunds
│     ├── policy check ──► gateway refund ──► revoke on confirm (BR-041)
│     └── payout already paid ──► clawback flag ► Payouts        (BR-043)
├── Payouts
│     ├── itemized run ──► Approve ──► Paid + reference (BR-073)
│     └── generates statement ► Instructor · Payout Statements
└── Reported Content
      └── ► Content Review / take-down action
```

---

## Cross-role handoffs

Edges that cross a role boundary (async, via state + notification — no shared screen):

```
Instructor · Submit for Review ─────► Admin · Moderation Queue           (course enters queue)
Admin · Content Review (approve/reject) ─► Instructor · Review Outcome    [notification]
Admin · Payouts (statement) ─────────► Instructor · Payout Statements
Student · Checkout (paid) ───────────► Admin · Revenue Dashboard          (revenue recorded)
Admin · Refunds ─────────────────────► Student · 403 Access Denied        (access revoked on confirm)
```
