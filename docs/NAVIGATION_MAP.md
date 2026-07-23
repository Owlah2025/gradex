# Navigation Map

> Status: Aligned with approved MVP
> Last Updated: 2026-07-23

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
    ├── Entitlement Expired → Course Details / Checkout
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
    └── Checkout
        ├── Choose Course or Section
        ├── Apply Coupon                           [state]
        ├── Tap Hosted Checkout                    [external]
        ├── Confirming / Failed                    [state]
        └── Receipt
            ├── Start / Resume → Lesson Player
            ├── Course Home
            └── Orders & Refunds

Student Dashboard
├── Continue Learning → Lesson Player
├── My Courses → Course Home
├── Browse → Catalog
├── Upcoming Office Hours
└── Orders & Refunds

Course Home
├── Section / Lesson outline
│   ├── Lesson Player
│   │   ├── Previous / Next Lesson
│   │   ├── Resources & Labs
│   │   └── Report Content                         [modal]
│   └── Locked Section / Lesson                    [state]
├── Resources & Labs
├── Upcoming Office Hours
│   └── Join External Meeting                      [external, authorized]
├── Community                                      [external]
└── Report Course                                  [modal]

Orders & Refunds
└── Order / Payment / Refund detail
```

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

There is no Instructor earnings, payout-statement, withdrawal, pricing-edit, coupon, or refund route.
The monthly payout statement is sent by email outside the authenticated UI.

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
├── Catalog Taxonomy
│   ├── Majors / Subjects vocabulary
│   └── Term Detail → Edit / Retire / Delete (unreferenced only)
├── Pricing
│   └── Course / Section Price + Audit History
├── Coupons
│   ├── Create / Edit / Deactivate
│   └── Redemption History
├── Revenue
│   └── Order / Payment Attempt Detail
├── Refunds
│   └── Order Lookup → Full / Partial Refund → Gateway Status
├── Payouts
│   └── Monthly Run → Instructor Statement → Approve → Record Paid Reference
├── Reported Content
│   └── Report Detail → Dismiss / Request Changes / Unpublish / Suspend
└── Office-Hours Moderation
    └── Session Detail → Cancel with reason
```

Admins do not create platform-wide office-hours sessions in MVP.

## Cross-Role Handoffs

```text
Admin Invitation                  → Instructor/Admin activation
Instructor Submit                → Admin Course Review queue
Admin Publish / Request Changes  → Instructor status + notification
Student Payment Success          → Admin Revenue + Instructor earning line
Student Content Report           → Admin Reported Content queue
Admin Refund Success             → Student refund state + entitlement/payout adjustment
Instructor Office-Hours change   → Entitled Student list + notifications
Admin Monthly Payout             → Instructor emailed statement
```

All handoffs are state/event transitions. Email delivery is never the source of truth.
