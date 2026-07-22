# WIREFRAMES

> Status: Draft
> Last Updated: 2026-07-21

Low-fidelity wireframes for every MVP screen. **Hierarchy and layout only** — no color, no styling, no real copy. Blocks are labeled by role in the layout, not designed.

**Source chain:** [SCREENS](SCREENS.md) · [NAVIGATION_MAP](NAVIGATION_MAP.md) · [NAVIGATION_RULES](NAVIGATION_RULES.md) → **Wireframes** → UI Mockups.

Each screen: read its contract in [SCREENS.md](SCREENS.md) (components/states/permissions) and its chrome/responsive row in [NAVIGATION_RULES.md](NAVIGATION_RULES.md). Frames below honor those. Student = **mobile frame** (single column + bottom tabs). Instructor/Admin = **desktop frame** (left sidebar + content).

## Legend

```
[ Label ]  button        [ input........ ]  field         ( ) radio   [ ] checkbox
====       section rule   ----  soft divider  [ IMG ]  image/thumb    « back
•Item      active nav      ▸ expand           >          chevron / go
{ State }  state note (layout variant, not drawn separately unless it changes hierarchy)
```

## Shells (drawn once, referenced by every screen)

```
STUDENT — mobile                     INSTRUCTOR / ADMIN — desktop
+---------------------------+        +----------+---------------------------------+
| Logo   [search]  Notif  = |        | Logo     | breadcrumb            Notif  ⋮  |
+---------------------------+        |          +---------------------------------+
|                           |        | •Nav     |                                 |
|         CONTENT           |        |  Nav     |            CONTENT              |
|                           |        |  Nav     |                                 |
+---------------------------+        |  Nav     |                                 |
| •Home  Browse  Notif  Prof|        |  ...     |                                 |
+---------------------------+        +----------+---------------------------------+
```

---

# Shared / System

## Landing
```
+---------------------------+
| Logo            [Sign in] |
+---------------------------+
|   ===== HERO =====        |
|   Value proposition       |
|   [ Browse ] [ Sign up ]  |
+---------------------------+
| Featured courses          |
| [IMG] [IMG] [IMG]  >      |
+---------------------------+
| How it works              |
| - Labs  - Community       |
| - Follow-up               |
+---------------------------+
| Footer: Legal | About     |
+---------------------------+
{ empty: thin launch strip }
```

## Login
```
+---------------------------+
|          Logo             |
+---------------------------+
|        Sign in            |
|  [ email............. ]   |
|  [ password.......... ]   |
|            Forgot? >      |
|      [   Sign in   ]      |
|  --------------------     |
|  New here? Register >     |
+---------------------------+
{ error: inline banner above form (401 / suspended) }
```

## Register
```
+---------------------------+
|          Logo             |
+---------------------------+
|      Create account       |
|  [ email............. ]   |
|  [ password.......... ]   |
|  [ confirm........... ]   |
|  [ ] Agree Terms/Privacy  |
|      [  Create  ]         |
|  Have an account? Sign in>|
+---------------------------+
{ error: duplicate email 409 inline }
```

## Forgot Password
```
+---------------------------+
|      Reset password       |
|  Enter your email         |
|  [ email............. ]   |
|      [ Send link ]        |
|  « Back to sign in        |
+---------------------------+
{ sent: generic confirmation replaces form }
```

## Reset Password
```
+---------------------------+
|     Set new password      |
|  [ new password...... ]   |
|  [ confirm........... ]   |
|      [   Save   ]         |
+---------------------------+
{ invalid/expired token: error + request-again link }
```

## Account / Settings
```
+---------------------------+  (role shell chrome)
| Account                   |
+---------------------------+
| Email                     |
|  [ ............. ] [Edit] |
| Password                  |
|  [ Change password ]      |
| Notifications             |
|  [x] Email  [x] In-app    |
| ----                      |
|  [ Log out ]              |
+---------------------------+
```

## Profile
```
+---------------------------+
| « Profile          [Edit] |
+---------------------------+
|   ( avatar )              |
|   Name                    |
|   [ display name..... ]   |
|   Major / Year            |
|      [   Save   ]         |
+---------------------------+
{ view vs edit toggle }
```

## Notification Center
```
+---------------------------+
| « Notifications  [Read all]|
+---------------------------+
| • Payment receipt      >  |
| • Video ready          >  |
|   Review approved      >  |
|   ...                     |
+---------------------------+
{ empty: "No notifications yet" }
```

## Legal
```
+---------------------------+
| « Terms / Privacy / Refund|
+---------------------------+
| [Terms][Privacy][Refund]  | anchors
| ===                       |
| Section heading           |
| body text ..............  |
| ........................  |
+---------------------------+
```

## System Error & Empty States
```
+---------------------------+
|                           |
|        (  !  )            |
|   403 / 404 / 500 msg     |
|   Cause-appropriate line  |
|                           |
|  [ Go home ] [ Retry ]    |
|  (403 expired: [ Re-buy ])|
+---------------------------+
```

---

# Student  (mobile-first)

## Catalog
```
+---------------------------+
| Logo   [search...]  Notif |
+---------------------------+
| [ Filters v ]   Major/Year|
+---------------------------+
| Course grid               |
| +----------+ +----------+  |
| | [IMG]    | | [IMG]    |  |
| | Title    | | Title    |  |
| | Instr.   | | Instr.   |  |
| | Price·term| |Price·term| |
| +----------+ +----------+  |
| +----------+ +----------+  |
|      [ Load more ]        |
+---------------------------+
| •Home Browse Notif Prof   |
+---------------------------+
{ empty: "No courses for this filter" }
{ desktop: left filter sidebar, 3–4 col grid }
```

## Search Results
```
+---------------------------+
| « [ query........... ] X  |
+---------------------------+
| [ Filters v ]   N results |
+---------------------------+
| +----------+ +----------+  |
| | [IMG]    | | [IMG]    |  |
| | Title    | | Title    |  |
| +----------+ +----------+  |
+---------------------------+
| •Home Browse Notif Prof   |
+---------------------------+
{ empty: "Subject not covered" + suggestions }
```

## Course Details
```
+---------------------------+
| «                    Share|
+---------------------------+
|   [   IMG / preview   ]   |
|   [ ▷ Preview lesson ]    | -> modal
+---------------------------+
| Title                     |
| Instructor                |
| Price     Access until DATE|
+---------------------------+
| [ Buy course ][Buy chapter]|
+---------------------------+
| Outline                   |
| ▸ Section 1               |
|    - Lesson  (preview)    |
|    - Lesson               |
| ▸ Section 2               |
+---------------------------+
| Includes: labs·resources· |
| community                 |
| [ Sample lab download ]   |
+---------------------------+
{ owned: primary CTA -> "Go to course" }
{ price mid-change: last-approved shown }
```

## Checkout
```
+---------------------------+
| « Checkout                |
+---------------------------+
| Order summary             |
|  Item · scope             |
|  Price                    |
|  Access term              |
| ----                      |
| Payment method            |
|  ( ) Card                 |
|  ( ) KNET                 |
|  -> hosted gateway page   |
| ----                      |
| Refund policy >           |
|     [   Pay now   ]       |
+---------------------------+
{ processing: "Confirming payment..." spinner, no fail }
{ failed: error + [ Retry ] / [ Cancel ] }
{ already enrolled: blocked notice }
```

## Payment Success / Receipt
```
+---------------------------+
|        ( check )          |
|     Payment confirmed     |
+---------------------------+
| Receipt                   |
|  Item · amount · date     |
|  Txn reference            |
+---------------------------+
| [ Start first lesson ]    |
| [ Go to dashboard ]       |
+---------------------------+
{ no Back to checkout (history.replace) }
```

## Student Dashboard
```
+---------------------------+
| Logo            Notif   = |
+---------------------------+
| Continue learning         |
| +-----------------------+ |
| | [IMG] Lesson · 12:04  | |
| |        [ Resume ]     | |
| +-----------------------+ |
+---------------------------+
| My courses                |
| +--------+ Progress ▓▓░ 40%|
| | [IMG]  | Access to DATE |
| +--------+                |
| +--------+ Progress ▓░░ 15%|
+---------------------------+
| •Home Browse Notif Prof   |
+---------------------------+
{ empty: "No courses yet" -> [ Browse ] }
{ near expiry: badge on card }
```

## Course Home
```
+---------------------------+
| « Course title            |
+---------------------------+
| Progress ▓▓▓░░ 55%        |
| Access until DATE         |
| Est. time to complete     |
+---------------------------+
| [ Start here / Resume ]   |
+---------------------------+
| ▸ Section 1               |
|   ✓ Lesson (done)         |
|   ▷ Lesson (current)      |
|   ⌷ Lesson (locked)       | <- chapter-only
| ▸ Section 2               |
+---------------------------+
| [ Resources & labs ]      |
| [ Community ]  (external) |
+---------------------------+
| •Home Browse Notif Prof   |
+---------------------------+
{ locked lessons visible when chapter-only }
```

## Lesson Player
```
+---------------------------+
| « Course       (overlay)  |
+---------------------------+
|                           |
|      [   VIDEO   ]        |
|   ▷ ▮▮  --o------  ⚙ ⛶     | controls
+---------------------------+
| Resume at 12:04?  [ Go ]  |
+---------------------------+
| Lesson title              |
| [ Resources ] [ Next > ]  |
+---------------------------+
| Up next: Lesson N+1       |
+---------------------------+
{ tab bar hidden (immersive) }
{ access denied mid-watch -> 403 screen }
{ desktop: right lesson-list rail + notes }
```

## Lesson Resources & Labs
```
+---------------------------+
| « Lesson materials        |
+---------------------------+
| Resources (slides/notes)  |
|  - file.pdf   [Download]  |
|  - file.pptx  [Download]  |
+---------------------------+
| Lab materials             |
|  - project.zip [Download] |
|  - guide.pdf   [Download] |
|  [ ] Mark lab done        |
|  Setup checklist ▸        |
+---------------------------+
| [ Community ] (external)  |
+---------------------------+
{ empty: "No materials for this lesson" }
{ link expired -> re-issue inline }
```

---

# Instructor  (desktop-first — left sidebar shell)

## Instructor Dashboard
```
+----------+---------------------------------+
| Logo     | My courses            Notif  ⋮  |
| •Dash    +---------------------------------+
|  Analyt. | [ + New course ]                |
|  Payouts | ------------------------------- |
|          | Title      Status        Actions|
|          | Course A   Published    [Edit]  |
|          | Course B   Pending      (lock)  |
|          | Course C   Rejected !   [Edit]  |
|          |   reason shown on Course C      |
+----------+---------------------------------+
{ empty: "No courses yet" -> New course }
{ pending rows read-only }
```

## Course Builder
```
+----------+---------------------------------+
| Logo     | Dash / Course        autosaved  |
| •Dash    +---------------------------------+
|          | [ Course title........ ]        |
|          | [ Description......... ]        |
|          | Price [ KWD ]                   |
|          | ============ Structure ======== |
|          | ▸ Section 1        [+][^][v][x] |
|          |    - Lesson        [edit][x]    |
|          |    - Lesson                     |
|          | ▸ Section 2                     |
|          | [ + Add section ]               |
|          | ------------------------------- |
|          | [ Preview ] [ Submit for review]|
+----------+---------------------------------+
{ draft (invisible) · pending (read-only) · pending-revision }
```

## Lesson Editor
```
+----------+---------------------------------+
| Logo     | Dash / Course / Lesson          |
| •Dash    +---------------------------------+
|          | [ Lesson title........ ]        |
|          | ===== Video =====               |
|          | +-----------------------------+ |
|          | |  Drag & drop / [ Upload ]   | |
|          | +-----------------------------+ |
|          | Status: Uploading ▓▓▓░ 60%      |
|          |         Processing... / Ready ✓ |
|          | [ Replace ]                     |
|          | ------------------------------- |
|          | [ Resources & labs ]            |
+----------+---------------------------------+
{ FAILED: retry (auto 3x then manual) }
{ over max size: reject msg }
{ « Back while unsaved -> intercept }
```

## Resources & Labs Manager
```
+----------+---------------------------------+
| Logo     | Dash / Course / Lesson / Mat.   |
| •Dash    +---------------------------------+
|          | Resources (<=50MB/file,200MB)   |
|          | +-------------------+ [Upload]  |
|          | | drop files        |           |
|          | +-------------------+           |
|          |  - slides.pdf          [x]      |
|          | ------------------------------- |
|          | Lab materials (<=250MB,1GB)     |
|          | +-------------------+ [Upload]  |
|          |  - project.zip         [x]      |
|          |  - guide.pdf           [x]      |
|          | Materials complete: ✓           |
+----------+---------------------------------+
{ wrong type / over cap -> reject inline }
```

## Submit for Review
```
+----------+---------------------------------+
| Logo     | Dash / Course                   |
| •Dash    +---------------------------------+
|          | Pre-submit checklist            |
|          |  ✓ >=1 section / lesson         |
|          |  ✓ Every lesson has READY video |
|          |  ! Lesson 3 video processing    | -> [Fix]
|          |  ✓ Price set                    |
|          | ------------------------------- |
|          | [ Submit for review ] (disabled |
|          |   until blockers clear)         |
+----------+---------------------------------+
{ blocked: list names what's missing + jump }
```

## Course Analytics
```
+----------+---------------------------------+
| Logo     | Dash / Course / Analytics       |
|  Dash    +---------------------------------+
| •Analyt. | Enrollments: N   Completion: %  |
|          | ============ Funnel =========== |
|          | L1 ▓▓▓▓▓▓▓▓  100%               |
|          | L2 ▓▓▓▓▓▓░░   72%               |
|          | L3 ▓▓▓▓░░░░   48%  <- drop       |
|          | ------------------------------- |
|          | Student roster                  |
|          |  name · progress                |
+----------+---------------------------------+
| NO earnings figures anywhere               |
{ empty: "No enrollments yet" }
```

## Payout Statements
```
+----------+---------------------------------+
| Logo     | Payout statements               |
|  Dash    +---------------------------------+
| •Payouts | Cadence: explained up front     |
|          | ------------------------------- |
|          | Cycle          Statement        |
|          | 2026-06     [ PDF ] [ CSV ]     |
|          | 2026-05     [ PDF ] [ CSV ]     |
|          | (statements only, no live $)    |
+----------+---------------------------------+
{ empty: "No statements yet" }
```

---

# Admin  (desktop-first — left sidebar shell)

## Admin Ops Landing
```
+----------+---------------------------------+
| Logo     | Ops                   Notif  ⋮  |
| •Ops     +---------------------------------+
| Queue    | +----------+ +----------+       |
| Users    | | Queue    | | Pending  |       |
| Revenue  | | depth: N | | refunds:N|       |
| Refunds  | +----------+ +----------+       |
| Payouts  | +----------+                    |
| Reports  | | Failed   |                    |
|          | | transc: N|  -> quick links    |
+----------+---------------------------------+
{ all-clear: empty queues }
```

## Moderation Queue
```
+----------+---------------------------------+
| Logo     | Moderation queue                |
| •Queue   +---------------------------------+
|          | [ ] Course · Instr · Age/SLA    |
|          | [ ] Course A · X · 2d  [Review]  |
|          | [ ] Course B · Y · 4h  [Review]  |
|          | ------------------------------- |
|          | [ Bulk triage ]                 |
+----------+---------------------------------+
{ empty: queue clear }
{ launch-week: long batch list }
```

## Content Review
```
+----------+---------------------------------+
| Logo     | Queue / Course                  |
| •Queue   +---------------------------------+
|          | Outline | [  PREVIEW VIDEO  ]   |
|          | ▸ Sec 1 |  (audited, logged)    |
|          |  L1     |  ▷ ▮ --o-- ⚙          |
|          |  L2     | ----------------------|
|          |         | Reviewer checklist    |
|          | ------------------------------- |
|          | [ Approve ]  [ Reject ]         |
|          |   Reject -> reason (required)   |
|          |   [ reason templates v ]        |
+----------+---------------------------------+
{ approve = atomic publish; revision applies to live }
```

## User Management
```
+----------+---------------------------------+
| Logo     | Users            [search.....]  |
| •Users   +---------------------------------+
|          | Role: [All v]                   |
|          | Name · Email(PII) · Role · State|
|          | Fahd · ...· Student · Active [⋮]|
|          |   [⋮] -> Suspend / Reinstate     |
|          | ------------------------------- |
|          | (detail drawer opens right)     |
+----------+---------------------------------+
{ suspend: reason required + audit }
{ student suspend kills access; instructor no }
```

## Revenue Dashboard
```
+----------+---------------------------------+
| Logo     | Revenue                         |
| •Revenue +---------------------------------+
|          | Period [ v ]   Total: KWD ...   |
|          | ===== trend chart (bars) =====  |
|          | ▓ ▓ ▓ ▓ ▓ ▓                     |
|          | ------------------------------- |
|          | Per-course revenue   table      |
|          | Refund / chargeback trend       |
|          | ! Reconciliation flags   >      |
+----------+---------------------------------+
{ desync warning surfaced }
```

## Refunds
```
+----------+---------------------------------+
| Logo     | Refunds                         |
| •Refunds +---------------------------------+
|          | [ Order lookup......... ][Find] |
|          | ------------------------------- |
|          | Order · student · amount        |
|          | Policy check:                   |
|          |   streamed? file opened? (14d)  |
|          |   -> Eligible / Ineligible      |
|          | ( ) Full   ( ) Partial [ amt ]  |
|          | [ Issue refund ]                |
|          | { pending-refund until gateway }|
|          | ! Payout paid -> clawback flag  |
+----------+---------------------------------+
{ gateway fail -> do not revoke; reconcile }
```

## Payouts
```
+----------+---------------------------------+
| Logo     | Payouts / Run                   |
| •Payouts +---------------------------------+
|          | Cycle [ v ]                     |
|          | Instructor · course · gross     |
|          |   - fees  - refunds  = net      |
|          | ! reconciliation flags          |
|          | ------------------------------- |
|          | [ Approve ]  then  [ Mark paid ]|
|          |   ref: [ ........ ]             |
|          | -> generates statement PDF/CSV  |
+----------+---------------------------------+
| earnings live HERE only, never instructor  |
{ refund after paid -> clawback next cycle }
```

## Reported Content
```
+----------+---------------------------------+
| Logo     | Reported content                |
| •Reports +---------------------------------+
|          | Target · reporter · reason · date|
|          | Course A · ... · spam  [Open]   |
|          | Material X · ... · IP  [Open]   |
|          |   [Open] -> Content Review/action|
|          | action: Dismiss / Take down/Warn|
+----------+---------------------------------+
{ empty: no reports }
```

---

## Coverage

34 / 34 screens. States shown as `{ ... }` notes where they change hierarchy; full state/permission contract in [SCREENS.md](SCREENS.md), chrome/responsive in [NAVIGATION_RULES.md](NAVIGATION_RULES.md). Demoted nodes (Lesson Preview modal, Payment Processing/Failed states, Community link-out, Review Outcome) appear as annotations on their parent frames, not as separate screens.
