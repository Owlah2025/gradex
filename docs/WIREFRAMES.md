# MVP Low-Fidelity Wireframes

> Status: Aligned with [SCREENS.md](SCREENS.md)
> Last Updated: 2026-07-23

These frames describe hierarchy and responsive behavior, not visual styling. Arabic layouts mirror
shell direction/reading order semantically; they are not separate screens. Every Student frame must
adapt across phone, tablet/iPad, laptop, and desktop.

## Legend

```text
[ Action ]    button/link                 [field........] input
▸ / ▾         collapsed / expanded       { state }       conditional state
🔒            locked by entitlement       [external]      leaves Gradex
```

## Responsive Shells

### Student — small screen

```text
+--------------------------------+
| Gradex      Search       Bell  |
|--------------------------------|
|                                |
|          screen body           |
|                                |
|--------------------------------|
| Home | Browse | Notice | Me    |
+--------------------------------+
```

### Student — wide screen

```text
+------------------------------------------------------------------+
| Gradex | Home | Browse | Notifications | Search | Language | Me |
|------------------------------------------------------------------|
| optional filter/lesson rail | main content                       |
+------------------------------------------------------------------+
```

### Instructor/Admin — responsive operations shell

```text
small:  +------------------------------+    wide: +-------------------------------+
        | ☰  Context      Bell | Me    |          | Sidebar | Header              |
        |------------------------------|          |         |---------------------|
        | stacked content / cards      |          |         | workspace / table   |
        +------------------------------+          +-------------------------------+
```

Instructor navigation has no earnings/payout entry. Admin navigation includes Users, Pricing,
Coupons, Review, Revenue, Refunds, Payouts, Reports.

---

# Shared and Authentication

## S01 — Landing

```text
+------------------------------------------------------------------+
| Gradex               Browse | Login | Register | العربية/English|
|------------------------------------------------------------------|
| University learning with real follow-up                          |
| [ Browse Courses ] [ How Gradex works ]                           |
|------------------------------------------------------------------|
| Featured published Courses                                       |
| [Course] [Course] [Course]                                       |
|------------------------------------------------------------------|
| Video + labs + community + office hours                          |
| Instructor value | FAQ | Terms | Privacy | Refund                |
+------------------------------------------------------------------+
```

No rating/testimonial/recommendation block appears unless separately approved with real data.

## S02–S06 — Auth Card Pattern

```text
+--------------------------------------+
| Gradex                          [←]  |
|--------------------------------------|
| Title                                |
| Explanation / assigned role          |
| [display name (register/invite)....] |
| [email.............................]  |
| [password (15–128).................] |
| [ Primary action ]                   |
| Secondary link / safe status         |
+--------------------------------------+
```

Variants:

- Registration → display name + email/password → generic accepted → Verify Email.
- Verify Email → verified / expired / reused / resend throttled.
- Staff invitation → assigned role + display name/password, no role picker.
- Reset → new password, no character-class checklist.

## S07–S09 — Notifications, Profile, Legal

```text
+-----------------------------------------------+
| Title                              Language   |
|-----------------------------------------------|
| Tabs/sections                                 |
| • transactional event / profile field / text |
| • timestamp / status / effective version     |
|-----------------------------------------------|
| [ contextual action ]                        |
+-----------------------------------------------+
```

Notification variant has read/unread actions but no preferences. Legal variant exposes Terms,
Privacy, and Refund Policy with language/version.

---

# Student

## ST01 — Catalog/Search

```text
+--------------------------------------------------+
| Search university Course...         [ Filters ] |
|--------------------------------------------------|
| {filter sheet/desktop rail}                      |
|   Major [v]  Subject [v]  Study Year [v]        |
|   {active chips} [ Clear all ]   N results      |
| [Course card]  [Course card]                     |
| Instructor · price · access-until date          |
| Labs/resources · office hours                    |
+--------------------------------------------------+
```

Filters are exact-match, one value per dimension. Search matches Arabic and English at once.

## ST02 — Course Details

```text
+--------------------------------------------------+
| Course title                      Instructor     |
| Authored language · summary                       |
| [ Public preview ] {hidden if none}              |
|--------------------------------------------------|
| Full Course                 40.000 KWD [ Buy ]   |
| ▾ Section 1                15.000 KWD [ Buy ]   |
|    Lesson A · Lesson B                           |
| ▸ Section 2                15.000 KWD [ Buy ]   |
|--------------------------------------------------|
| Included Resources · Labs · Community · Hours   |
| Access until: {date/time} | Refund Policy       |
+--------------------------------------------------+
```

There is no Sample Lab download. A “Chapter” label, if localized, replaces the visible word
Section only; it does not create a second object.

## ST03 — Checkout

```text
+----------------------------------------+
| Checkout                         [ X ] |
|----------------------------------------|
| Course / Section snapshot              |
| Subtotal                   15.000 KWD  |
| Coupon [............] [ Apply ]        |
| Discount                  - 3.000 KWD  |
| Total                      12.000 KWD  |
| Access until: {exact disclosed date}   |
| [✓] Accept Refund Policy v...          |
| [ Continue to Tap ]                    |
+----------------------------------------+

{ total 0.000 → Grant access; do not open Tap }
```

## ST04 — Confirmation / Receipt States

```text
+----------------------------------------+
| { Confirming payment… }                |
| { Paid / Free grant / Failed }         |
|----------------------------------------|
| Order · item · amounts · reference     |
| Access until DATE · policy version     |
| [ Start / Go to Course ] [ Orders ]   |
+----------------------------------------+
```

## ST05–ST06 — Dashboard / Course Home

```text
+--------------------------------------------------+
| Continue learning                                |
| [ Course · Lesson · progress · Resume ]          |
|--------------------------------------------------|
| Course Home · access until DATE                  |
| ▾ Section 1 (owned)                              |
|    ✓ Lesson A    ▷ Lesson B                      |
| ▸ Section 2 🔒                                   |
|--------------------------------------------------|
| Resources | Labs | Office Hours | Community     |
| [ Report Course ]                                |
+--------------------------------------------------+
```

## ST07–ST08 — Lesson Player / Materials

```text
+--------------------------------------------------+
| ← Course | Lesson title                         |
|--------------------------------------------------|
|                RESPONSIVE VIDEO                  |
|      play · seek · volume · quality · fullscreen|
|--------------------------------------------------|
| [Previous] progress [Next] [Report]             |
|--------------------------------------------------|
| Resources                   Labs                 |
| [File · type · size · Download]                 |
+--------------------------------------------------+
```

On small screens the Lesson list/materials use sheets/stacked sections. On desktop a persistent
Lesson rail may appear. Captions are not shown as an MVP control.

## ST09 — Office Hours

```text
+----------------------------------------+
| Upcoming Office Hours                  |
|----------------------------------------|
| Course · Session title                 |
| Localized DATE/TIME · Scheduled        |
| [ View Course ] [ Join ]               |
|----------------------------------------|
| {rescheduled} {cancelled} {empty}      |
+----------------------------------------+
```

`Join` is rendered only after authorization; the URL is not embedded in unauthorized data.

## ST10 — Orders and Refunds

```text
+--------------------------------------------------+
| Orders & Refunds                                 |
|--------------------------------------------------|
| Order # · Course/Section · Paid/Free/Failed      |
| paid · discount · access term            [Open] |
|--------------------------------------------------|
| Detail: payment reference · policy version       |
| Refunds: amount · Pending/Succeeded/Failed       |
| Remaining refundable balance                     |
+--------------------------------------------------+
```

## Report Content Modal

```text
+----------------------------------------+
| Report Course / Lesson / File     [X] |
| Reason [select......................]  |
| Details [...........................]  |
| [ Submit Report ]                     |
+----------------------------------------+
```

---

# Instructor

## IN01 — Dashboard

```text
+----------+------------------------------------------+
| Dashboard| My Courses                               |
| Courses  | [Course · Draft · Edit]                  |
| Analytics| [Course · Changes Requested · Review]    |
| Hours    | [Course · Published · Analytics]         |
| Notice   | Upcoming sessions · processing failures  |
+----------+------------------------------------------+
```

No earnings or payout-statement destination.

## IN02–IN05 — Builder, Lesson, Materials, Preview

```text
+----------+-----------------------------------------------+
| Courses  | Course Builder · autosaved/sync state         |
|          | Title [.................................]      |
|          | Course price 40.000 KWD  🔒 Admin-controlled  |
|          | ▾ Section 1 · 15.000 KWD 🔒                   |
|          |    Lesson A [Video: READY] [Materials]        |
|          |    Lesson B [Video: PROCESSING]               |
|          | [ + Section ] [ Public Preview ]              |
|          | [ Submit for Review ]                         |
+----------+-----------------------------------------------+

Materials: Resources | Labs → upload/scan/available states
Preview: one separate asset → permission ✓ → scan → public
```

## IN06 — Submit / Review

```text
+-----------------------------------------------+
| Review readiness                              |
| ✓ Section/Lesson structure                    |
| ✓ Required READY video content                |
| ! Missing item → [ Fix ]                      |
|-----------------------------------------------|
| {Ready [Submit]} {Pending Review read-only}  |
| {Changes Requested: reason [Revise]}          |
+-----------------------------------------------+
```

## IN07–IN08 — Analytics / Office Hours

```text
Analytics                       Office Hours
+--------------------------+    +-------------------------------+
| Enrollments · completion |    | Course [owned Published....] |
| Lesson progress funnel   |    | Title / description          |
| Student roster (minimal) |    | Start / End / external link  |
| no revenue/earnings      |    | [Schedule] [Reschedule][Cancel]|
+--------------------------+    +-------------------------------+
```

---

# Admin

## AD01–AD02 — Ops / Users and Invitations

```text
+----------+------------------------------------------------+
| Ops      | Pending Reviews · Reports · Refunds · Payouts  |
| Users    |------------------------------------------------|
| Pricing  | Users [search/filter] [Invite Staff]           |
| Review   | Account · Role · Status · Invitation           |
| ...      | [Suspend/Reactivate] [Resend/Revoke Invite]    |
+----------+------------------------------------------------+
```

## AD03 — Pricing

```text
+--------------------------------------------------+
| Course / Section Pricing                         |
|--------------------------------------------------|
| Course price [40000 fils]                        |
| Section 1   [15000 fils]                         |
| Reason      [.................................]  |
| [ Save audited price change ]                    |
|--------------------------------------------------|
| History: old → new · Admin · reason · time       |
+--------------------------------------------------+
```

## AD04–AD05 — Course Queue / Review

```text
+-----------+------------------------------------------------+
| Review    | Pending Review queue                           |
|           | Course · Instructor · first/revision · age     |
|-----------+------------------------------------------------|
| Content Review                                             |
| Outline | audited video/material/preview | revision diff  |
| [ Publish ] [ Request Changes ] [ Unpublish ] [ Archive ] |
| Reason [...............................................]   |
+------------------------------------------------------------+
```

Only valid state actions are enabled; change request/unpublish/archive require reason where
specified.

## AD06 — Coupons

```text
+--------------------------------------------------+
| Coupons                         [ Create ]        |
| Code · type/value · targets · window · cap/state|
|--------------------------------------------------|
| Edit: Course/Section targets · global cap        |
| No per-user-limit field                          |
| [ Save ] [ Deactivate ]                          |
| Redemption/refund history                        |
+--------------------------------------------------+
```

## AD07–AD08 — Revenue / Refund

```text
Revenue / Order detail              Refund drawer/page
+------------------------------+    +-------------------------------+
| Order/Attempt/Entitlement     |    | Captured / refunded / remain |
| subtotal/discount/paid        |    | Method: partial supported?   |
| coupon / gateway reference   |    | Amount [fils...............] |
| refund + earning lines       |    | Reason [...................] |
| {reconciliation warning}     |    | [ Request Refund ]           |
+------------------------------+    | {Pending/Success/Failed}     |
                                    +-------------------------------+
```

## AD09 — Payouts

```text
+--------------------------------------------------+
| Monthly Payout Run · share: {configured/block}  |
|--------------------------------------------------|
| Instructor · Orders · fees · refunds · adjust   |
| Net collected · share · payable                 |
| [ Generate ] [ Approve ]                        |
| Bank reference [................] [ Mark Paid ] |
| [ Email Statement ]                             |
+--------------------------------------------------+
```

## AD10–AD11 — Reports / Office-Hours Moderation

```text
Reported Content                     Office-Hours Moderation
+-------------------------------+    +-------------------------------+
| Target · reporter · reason    |    | Course · Instructor · time   |
| State/history/evidence        |    | external link · state        |
| [Dismiss] [Request Changes]   |    | Reason [...................] |
| [Unpublish] [Suspend]         |    | [ Cancel Session ]           |
+-------------------------------+    +-------------------------------+
```

Admins have no create/platform-wide office-hours control.

## AD12 — Catalog Taxonomy

```text
+--------------------------------------------------+
| Catalog Taxonomy    [ Majors ] [ Subjects ]     |
|--------------------------------------------------|
| ar label · en label · code · state · #courses   |
| ---------------------------------------------   |
| علوم حاسوب · Computer Science · — · active · 7  |
| برمجة ١   · CS 101          · CS 101 · act · 3  |
| [ Edit ] [ Retire ] [ Delete: blocked if used ] |
|--------------------------------------------------|
| [ + New term ]  ar [......] en [......] code[..]|
+--------------------------------------------------+
Study Year is a fixed enumeration and is not edited here.
```

---

# Coverage

| Screen IDs | Wireframe coverage |
|---|---|
| S01–S10 | Landing, Auth pattern, Notifications/Profile/Legal pattern, shared state behavior |
| ST01–ST10 | Catalog, Course, Checkout, Receipt, Dashboard/Course, Player/Materials, Hours, Orders |
| IN01–IN08 | Dashboard, Builder/Lesson/Materials/Preview, Review, Analytics, Hours |
| AD01–AD12 | Ops/Users, Pricing, Review, Coupons, Revenue/Refund, Payouts, Reports/Hours moderation, Taxonomy |

Detailed field/state/permission contracts remain in [SCREENS.md](SCREENS.md). These frames do not
add routes or features beyond that source.
