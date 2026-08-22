# Navigation Map

> Status: Aligned with approved MVP
> Last Updated: 2026-07-28

Per-role route hierarchy for the responsive Gradex website. Behavioral rules live in
[NAVIGATION_RULES.md](NAVIGATION_RULES.md); detailed screen contracts in [SCREENS.md](SCREENS.md).

## Legend

```text
[modal]      overlay, not a route
[state]      state within the parent route
[external]   leaves Gradex
►            cross-tree navigation
```

## Public and Authentication

```text
Landing
├── Catalog / Search
│   └── Course Details
│       └── Public Preview                         [modal or embedded]
├── Login
│   ├── Forgot Password → Reset Password           [email deep link]
│   └── Register → Verify Email                    [email deep link]
├── Accept Staff Invitation                        [email deep link]
└── Legal
    ├── Terms
    ├── Privacy
    └── Refund Policy
```

Successful auth routes by role or returns to a validated internal `returnTo` destination.

## Authenticated Global

```text
Role shell
├── Notifications
├── Profile
│   ├── Language / Locale
│   ├── Change Email → Verify New Email
│   ├── Change Password
│   └── Account / Data Request
├── Legal
└── Error/Status
    ├── Access Denied / Suspended
    ├── Entitlement Expired → Course Details
    ├── Not Found
    └── Offline / Retry
```

## Student

```text
Catalog / Search
├── Filter by Major / Subject / Study Year        [state]
└── Course Details
    ├── Public Preview
    ├── [actively entitled] Go to Course → Course Home
    ├── I want to buy this Course → email form → WhatsApp [external]
    └── How to Get Access                          [informational]

Course Access Invitation                          [entry: emailed link]
├── Sign In / Register with the invited email
├── Accept standard invitation                     → Awaiting Admin Approval  [state]
├── Accept purchase-backed invitation              → Course Home
├── Wrong identity signed in                      [state, refused]
└── Link expired → Request a new link             [state]

Access Status
├── Awaiting your acceptance                      [state]
├── Awaiting Admin approval                       [state]
├── Access active                                 → Course Home
├── Rejected (with reason)                        [state]
└── Cancelled / Expired                           [state]

Student Dashboard
├── Continue Learning → Lesson Player
├── My Courses → Course Home
├── Pending Invitation → Course Access Invitation
├── Browse → Catalog
├── Upcoming Office Hours
└── Access History

Course Home
├── Section / Lesson outline
│   └── Lesson Player
│       ├── Previous / Next Lesson
│       ├── Resources & Labs
│       └── Report Content                         [modal]
├── Resources & Labs
├── Upcoming Office Hours
│   └── Join External Meeting                      [external, authorized]
├── Community                                      [external]
└── Report Course                                  [modal]

Access History
└── Per-Course invitation state + Entitlement term
```

There is no checkout, cart, coupon, order, receipt, or refund route. A Course Entitlement covers
every Section in its Course, so there is no locked-Section state inside an entitled Course.

## Instructor

```text
Instructor Dashboard
├── Courses
│   └── Course Builder
│       ├── Section / Lesson Editor
│       │   ├── Video Upload / Processing          [state]
│       │   └── Resources & Labs
│       ├── Public Preview Manager
│       ├── Price                                  [read-only]
│       ├── Submit / Review Status
│       │   ├── Pending Review                     [read-only state]
│       │   ├── Changes Requested → Builder
│       │   └── Published
│       └── Published Revision → Submit / Review
├── Analytics
│   └── Course Analytics / Roster
├── Office Hours
│   └── Create / Reschedule / Cancel owned Course session
└── Notifications
```

There is no Instructor earnings, payout-statement, withdrawal, pricing-edit, or access-granting
route. Instructor compensation is arranged entirely outside the platform in MVP.

## Admin

```text
Admin Ops
├── Users
│   ├── Invite Instructor / Admin
│   ├── Invitation Status
│   └── User Detail → Suspend / Reactivate
├── Course Review
│   ├── Moderation Queue
│   └── Content Review
│       ├── Audited Video / Content Preview
│       ├── Publish
│       ├── Request Changes
│       ├── Unpublish / Republish
│       └── Archive (when allowed)
├── Catalog Taxonomy                      [legacy; D-022, superseded by D-091]
│   ├── Majors / Subjects vocabulary
│   └── Term Detail → Edit / Retire / Delete (unreferenced only)
├── Academic Catalog / الكتالوج الأكاديمي   [D-091, AD13]
│   └── Institution
│       ├── Academic Units (College / Department tree) → Create / Edit / Re-parent / Retire
│       ├── Programs → Create / Edit / Retire
│       │   └── Curriculum → Subject mappings (requirement kind, recommended level/semester)
│       └── Subjects → Create / Edit / Retire (duplicate refused)
├── Pricing
│   └── Course / Section Price + Audit History
├── Course Access Invitations
│   ├── Create (Student email + one Course)
│   ├── Awaiting Acceptance                        [queue]
│   ├── Awaiting Approval                          [queue]
│   └── Invitation Detail → Approve / Reject with reason / Cancel / Resend link
├── Purchase Requests
│   ├── Search by request reference, email, Course title, or state
│   ├── Waiting for external payment
│   └── Confirm payment & send invitation          [idempotent]
├── Entitlements
│   └── Entitlement Detail → Extend / Shorten expiry / Revoke  [audited]
├── Reported Content
│   └── Report Detail → Dismiss / Request Changes / Unpublish / Suspend
└── Office-Hours Moderation
    └── Session Detail → Cancel with reason
```

Admins do not create platform-wide office-hours sessions in MVP. There is no coupon, revenue,
refund, or payout route: those features are deferred with in-platform payments
([D-045](DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation)).
**Standard Invitation approval and purchase payment confirmation plus matching Student acceptance are the only product routes that create Course access.**

## Cross-Role Handoffs

```text
Admin Invitation                  → Instructor/Admin activation
Instructor Submit                 → Admin Course Review queue
Admin Publish / Request Changes   → Instructor status + notification
Admin Creates Course Invitation   → Student invitation notice
Student Accepts Invitation        → Admin Awaiting-Approval queue
Admin Approval                    → Entitlement + Enrollment + Student access-granted notice
Student Purchase Request           → persisted request → Student WhatsApp handoff
Admin Payment Confirmation         → purchase-backed invitation email
Student Accepts Purchase Invitation → Entitlement + Enrollment + Course Home
Admin Rejection                   → Student notice with reason
Student Content Report            → Admin Reported Content queue
Admin Entitlement Adjustment      → Student expiry-change notice
Instructor Office-Hours change    → Entitled Student list + notifications
```

All handoffs are state/event transitions. Email delivery is never the source of truth.
