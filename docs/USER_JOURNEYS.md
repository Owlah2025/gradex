# User Journeys

> Status: Aligned with approved MVP
> Last Updated: 2026-07-28

These task journeys apply the canonical scope in [PRD.md](PRD.md), rules in
[BUSINESS_RULES.md](BUSINESS_RULES.md), and terminology in [GLOSSARY.md](GLOSSARY.md).
They describe one responsive website. Student journeys must complete on phones, tablets/iPads,
laptops, and desktops; complex Instructor/Admin operations are responsive but desktop/tablet
optimized.

---

# 1. Student — Discover, Gain Access, and Learn

```text
Discover → Evaluate → Pay externally (outside Gradex) → Register/Verify or Sign In
                                                                  ↓
                                          Receive Course Access Invitation
                                                                  ↓
                                        Accept  →  Await Admin Approval
                                                                  ↓
Report ← Practise/Office Hours ← Watch/Resume ← Course Home ← Access granted
                                                                  ↓
                                                 Expiry → Request access again
```

**Nothing in this journey charges money inside Gradex.** Payment is External Payment, confirmed by
an Admin out of band ([D-045](DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation)).

## SJ-01 — Discover a Course

- **Goal:** Find a Course matching a university subject/level.
- **Actions:** Browse the published catalog, filter by Major/Subject/Study Year, search in Arabic or
  English, open a Course detail page.
- **Decisions:** Is this the right Course? Access is granted for the complete Course only.
- **Rules:** `Course → Section → Lesson`; “Chapter” may only label Section (BR-010/021). Only
  `PUBLISHED` Courses are discoverable; search matches both languages with Arabic normalization and
  ranks by relevance only (BR-161/162).
- **Edge cases:** Thin launch catalog; no Course for the selected filter combination; query typed
  with diacritics or a different hamza form; archived/delisted Course absent from results.
- **Failure behavior:** Catalog errors provide retry/empty states without exposing protected data.

## SJ-02 — Evaluate the Course

- **Goal:** Understand content, practical value, Instructor, price, access term, and how to obtain
  access.
- **Actions:** Review outline, authored details, resources/lab inclusion, office-hours support,
  Course price, access term, optional public preview, and how-to-get-access guidance.
- **Rules:** Admin controls prices (BR-019); the displayed price tells the Student what to pay
  externally and Gradex charges nothing (BR-020); Section prices are not displayed because Section
  is not an acquirable scope (BR-021); protected Labs/Resources are not previews (BR-143/144);
  active access changes the CTA to “Go to Course” (BR-024).
- **Edge cases:** No preview asset; recently changed price; Student already holds active access.
- **Failure behavior:** The page never implies Gradex will take payment.

## SJ-03 — Register and Verify

- **Goal:** Create a Student account without losing the selected Course/Section.
- **Actions:** Submit display name/email/password; receive/consume verification link; return to
  original intent.
- **Rules:** Public registration is Student-only; verification precedes sign-in; responses do not
  expose existing accounts; the display name follows BR-105 and is not an identity key
  (BR-001/002/008/105).
- **Edge cases:** Existing email, expired/reused link, resend throttling, suspended account.
- **Failure behavior:** Generic safe response, clear verification status, preserved `returnTo`.

## SJ-04 — Sign In

- **Goal:** Authenticate and continue the intended access or learning route.
- **Actions:** Enter credentials; renew/rotate the opaque session credential as needed.
- **Rules:** Generic credential failure (BR-003), revoked refresh rejection (BR-005), immediate
  suspension enforcement (BR-007).
- **Edge cases:** Session expires mid-acceptance; Account becomes suspended while active.
- **Failure behavior:** Re-authentication preserves safe return path; suspension never reaches
  protected content.

## SJ-05 — Accept a Course Access Invitation

- **Goal:** Accept an Admin-issued invitation for one Course and understand that access is not yet
  active.
- **Actions:** Open the invitation link, sign in or register with the invited email, review the
  Course and access term, accept.
- **Rules:** Only an Account whose normalized email matches may accept, and any other identity is
  refused server-side (BR-166). Acceptance moves the invitation to pending Admin approval and
  **grants no access** (BR-029). An Account alone never grants access.
- **Edge cases:** Signed in as a different Account; no Account yet; acceptance link expired and needs
  reissuing (BR-169); invitation already cancelled.
- **Failure behavior:** A refused acceptance never partially grants access and never reveals whether
  another Account exists.

## SJ-06 — Await Admin Approval and Receive Access

- **Goal:** Know whether access has been granted, and reach the Course once it has.
- **Actions:** View access status; receive the access-granted notification; open the Course.
- **Rules:** **Admin Approval is the sole grant trigger** (BR-167). It creates or reuses the
  Enrollment and creates exactly one Entitlement, idempotently, with the Course's configured expiry
  snapshotted (BR-024/025). A rejection carries a reason (BR-168).
- **Edge cases:** Approval repeated or concurrent — exactly one Entitlement results; Course lacking a
  future access-expiry instant cannot be approved; Account suspended before approval.
- **Failure behavior:** Until approval lands there is no access, and the status screen says so
  plainly rather than implying a pending purchase.
- **Notification:** The access-granted notice is recorded after the Entitlement exists; delivery
  failure does not affect access (BR-120–123).

## SJ-07 — Orient in Course Home

- **Goal:** Understand entitled scope, progress, access expiry, materials, and upcoming office
  hours.
- **Actions:** Start/resume a Lesson; view the Course outline.
- **Rules:** A Course Entitlement covers every Section and Lesson in that Course (BR-024);
  Enrollment/progress can remain after Entitlement expiry.
- **Edge cases:** Expired access with retained progress; emergency Course access suspension.
- **Failure behavior:** Locked content never receives signed playback/download URLs.

## SJ-08 — Watch and Resume a Lesson

- **Goal:** Watch smoothly and never lose progress.
- **Actions:** Play/pause/seek/quality/fullscreen; resume position; continue between Lessons.
- **Rules:** Active scope checked before signed playback (BR-023/050); completion at ≥90% never
  regresses (BR-051); resume uses last position (BR-052).
- **Device behavior:** Responsive video, landscape, keyboard controls, and browser fullscreen where
  available (BR-147/151).
- **Failure behavior:** Transient token/progress failure recovers without interrupting playback;
  authorization/expiry fails explicitly (BR-053).

## SJ-09 — Use Resources, Labs, Community, and Office Hours

- **Goal:** Practise and receive follow-up.
- **Actions:** Download entitled Resource/Lab; view/join Course office hours.
- **Rules:** Each download is entitlement-checked (BR-023/063); Labs may carry buyer identification
  (BR-103); office-hours link requires an active Course Entitlement (BR-135/136).
- **Community — deferred to S18 on 2026-07-29 by
  [D-046](DECISIONS.md#d-046--the-external-course-community-link-is-deferred-to-post-launch).** No MVP
  screen shows a Course community link and no MVP journey step opens one. The external Discord
  community still exists; its link is shared out of band until S18. The journey title is retained
  unchanged so approved references to this anchor keep resolving.
- **Edge cases:** Expired signed URL, cancelled/rescheduled session, external link failure.
- **Failure behavior:** Reissue authorized download; cancelled session is not joinable; notification
  failure does not alter schedule.

## SJ-10 — Report Content

- **Goal:** Tell Gradex about broken, inaccurate, inappropriate, or rights-infringing content.
- **Actions:** Choose a report target/reason; explain “other”; submit.
- **Rules:** Student must be entitled; reports are rate-limited and never auto-hide content
  (BR-145/146).
- **Failure behavior:** Duplicate/spam attempt is throttled; successful submission receives a safe
  acknowledgement without revealing Admin operations.

## SJ-11 — Raise a Refund or Billing Question

- **Goal:** Resolve a payment question when Gradex holds no payment record.
- **Actions:** Contact the support route; the founder handles the refund entirely outside Gradex.
- **Rules:** Terms §8 is the approved no-commerce payment/consumer-rights disclosure
(BR-153/`LG-011`); the MVP requires no standalone Refund Policy while Gradex processes no refunds
(BR-040–047 deferred). If access must
  end as a result, an Admin uses the audited Entitlement adjustment or revocation (BR-026) — never
  an unrecorded deletion.
- **Edge cases:** Student paid but was never invited; Student invited but never paid; access granted
  in error.
- **Failure behavior:** No Gradex screen implies a refund is in progress, because Gradex has no
  payment state to report.
- **MVP boundary:** Reconciliation between External Payment records and granted access is a manual
  founder process. `LG-016` remains open on how those records must be kept.

## SJ-12 — Return After Expiry

- **Goal:** Continue learning after the access period ends.
- **Actions:** Sign in, see retained progress and expired access, pay externally again and receive a
  new Course Access Invitation.
- **Rules:** Expiry ends access but preserves Enrollment/progress; at most one active Entitlement
  exists per Student and Course (BR-024/025).
- **MVP boundary:** No expiry reminder and no self-service renewal; regaining access repeats the
  invitation and approval workflow.

---

# 2. Instructor — Join, Publish, Support, and Receive a Statement

```text
Admin Invitation → Activate → Build Content → Upload/Preview → Submit
                                                       ↓
Emailed Statement ← Analytics/Office Hours ← Published ← Review/Revise
```

## IJ-01 — Accept Invitation

- **Goal:** Activate a verified Instructor account securely.
- **Actions:** Consume Admin invitation; set display name and initial password; sign in.
- **Rules:** No public Instructor signup (BR-009); approved password policy (BR-002); display name
  follows BR-105 and is not an identity key.
- **Edge cases:** Expired/reused invitation, suspended Account.
- **Failure behavior:** Resend is Admin/rate controlled; no role self-selection.

## IJ-02 — Build Course Structure

- **Goal:** Create an owned `Course → Section → Lesson` outline and classify it for the catalog.
- **Actions:** Create/reorder/edit Course content with autosave/draft behavior; select one Major,
  Subject, and Study Year from the Admin-managed vocabulary.
- **Rules:** Own Courses only (BR-060); ordering persists (BR-010); price is visible read-only and
  only Admin can change it (BR-019); taxonomy terms may be selected but never created, renamed, or
  retired by an Instructor (BR-158).
- **Edge cases:** Empty/duplicate titles, pending-review read-only state, needed subject missing from
  the vocabulary, a previously assigned term later retired by an Admin.
- **Failure behavior:** Cross-owner, price, and vocabulary mutation are denied server-side.

## IJ-03 — Upload Videos, Resources, and Labs

- **Goal:** Attach ready learning content to each Lesson.
- **Actions:** Upload video through processing; add separate Resources/Lab Materials.
- **Rules:** Existing video pipeline (BR-062/091); allowed category/type/size rules
  (BR-063/067/068); protected downloads require scan/entitlement (BR-104).
- **Edge cases:** Processing failure/retry, over-cap/wrong-type file, replacement preserving progress.
- **Failure behavior:** Clear status; rejected/failed upload never becomes available.

## IJ-04 — Add an Optional Public Preview

- **Goal:** Help public Students evaluate the Course without exposing protected materials.
- **Actions:** Upload one preview; confirm permission; wait for validation/scan.
- **Rules:** Preview is separate from Lesson assets and at most one per Course (BR-143/144).
- **Failure behavior:** Failed scan/validation keeps it private without affecting protected Course.

## IJ-05 — Submit and Handle Review

- **Goal:** Publish complete, approved content.
- **Actions:** Run readiness checklist; submit; view Pending Review; receive approval/change request;
  revise and resubmit.
- **Rules:** Readiness (BR-012/013), locked review state (BR-016), required Admin reason
  (BR-070–072), explicit Course lifecycle (BR-090).
- **Failure behavior:** Missing items block submission with exact fixes; notification failure does
  not replace dashboard state.

## IJ-06 — Maintain a Published Course

- **Goal:** Improve content without silently altering the approved live version.
- **Actions:** Create a Course Revision; preview; resubmit.
- **Rules:** Live version remains unchanged until revision approval (BR-017/090); price is not in
  Instructor revision.
- **Failure behavior:** Change request leaves live version intact.

## IJ-07 — View Analytics and Run Office Hours

- **Goal:** Understand learning engagement and support Students.
- **Actions:** View own enrollments/completion/roster; schedule/reschedule/cancel one-off Course
  office hours.
- **Rules:** Analytics limited to owned Course; price read-only/no earnings dashboard (BR-064);
  own Published Course office hours only (BR-134).
- **Failure behavior:** Suspended Instructor cannot edit/schedule, while approved Student content
  remains governed by BR-065.

## IJ-08 — Receive Compensation — outside Gradex

- **Goal:** Understand the amount transferred.
- **Actions:** Compensation is arranged and paid entirely out of band by the founder. Gradex produces
  no statement, ledger, or earnings view.
- **Rules:** Payout processing is deferred with in-platform payments (BR-073/074 deferred). Revenue
  share remains a required term of the Instructor agreement under `LG-020`.
- **MVP boundary:** No in-app earnings, statement, or withdrawal exists, and none is calculated.

---

# 3. Admin — Provision, Price, Publish, and Grant Access

```text
Bootstrap/Sign In → Invite Staff → Price Courses → Review/Publish
        ├→ Suspend/Reactivate
        ├→ Confirm External Payment → Invite to Course → Approve → Access granted
        ├→ Adjust Entitlement expiry
        └→ Content Reports/Delist
```

## AJ-01 — Bootstrap and Sign In

- **Goal:** Establish and use privileged access safely.
- **Actions:** One-time secure deployment creates first Admin; force password change; subsequent
  sessions use normal auth.
- **Rules:** No repository credential (BR-009); privileged actions audited.
- **Failure behavior:** Suspension blocks Admin protected actions immediately.

## AJ-02 — Invite and Manage Accounts

- **Goal:** Provision Instructors/Admins and keep access safe.
- **Actions:** Send invitation, view status, resend/revoke as allowed, suspend/reactivate with reason.
- **Rules:** Public role assignment prohibited; existing Account emails cannot be invited/converted;
  suspension immediate (BR-007/009).
- **Failure behavior:** Conflicting-address/expired/reused invitations fail safely; active sessions
  cannot bypass suspend.

## AJ-03 — Set Course Prices

- **Goal:** Publish the amount a Student must pay externally.
- **Actions:** Set/change price with required reason; review audit history.
- **Rules:** Admin-only, integer fils (BR-019). The price is displayed to Students as guidance for
  External Payment; Gradex charges nothing (BR-020). Section prices are maintained but not displayed
  because Section is not an acquirable scope (BR-021).
- **Failure behavior:** A price change never alters an existing Entitlement or its expiry.

## AJ-04 — Maintain the Catalog Taxonomy

- **Goal:** Keep Major/Subject vocabularies usable so catalog filtering stays meaningful.
- **Actions:** Create terms with Arabic/English labels and optional Subject code; rename, retire,
  delete unreferenced terms; override a Course's classification.
- **Rules:** Admin-only and audited (BR-158); renaming never rewrites assigned Courses; a retired
  term stays on existing Courses until reassigned; a referenced term cannot be deleted (BR-159/160).
- **Edge cases:** Instructor requests a missing subject; near-duplicate labels; retiring a term still
  used by Published Courses.
- **Failure behavior:** Delete is blocked while referenced rather than cascading; classification
  stays valid for every Published Course.

## AJ-05 — Review and Publish Course Content

- **Goal:** Publish quality content without half-applied state.
- **Actions:** Triage Pending Review; audited media preview; publish or request changes with reason.
- **Rules:** Admin preview is separate from Student Entitlement (BR-081); status transitions follow
  BR-070–072/090.
- **Failure behavior:** Approval/revision application is atomic; queue/dashboard state is source of
  truth if notification fails.

## AJ-06 — Grant Course Access

- **Goal:** Turn a confirmed External Payment into active course access, safely and auditably.
- **Actions:** Confirm the payment out of band; create a Course Access Invitation for one Student
  email and one Course, optionally recording a note and an opaque external reference; watch the queue
  for acceptance; approve, reject with a reason, or cancel.
- **Rules:** Creation grants nothing and is never evidence that payment occurred inside Gradex
  (BR-020/165). **Approval is the sole grant trigger** and requires the course-access capability plus
  valid recent authentication, or it is refused (BR-167). Approval is idempotent and creates exactly
  one Entitlement (BR-024). A Course without a future access-expiry instant cannot be approved
  (BR-025). Every transition is audited (BR-168).
- **Edge cases:** Student never accepts; wrong email entered; duplicate invitation refused; Student
  already holds active access; Account suspended between acceptance and approval.
- **Failure behavior:** A failed approval grants nothing and leaves the invitation in its prior state.

## AJ-07 — Adjust or End Course Access

- **Goal:** Correct an access term or end access after an out-of-band refund.
- **Actions:** Extend or shorten an Entitlement's effective expiry with a required reason, or revoke
  it.
- **Rules:** Elevated Admin only; the adjustment atomically records old/new instants, reason, actor,
  and timestamp with immutable audit evidence and a Student notification (BR-026).
  `original_access_ends_at` never changes, and moving expiry into the past never deletes Enrollment,
  Progress, or invitation history.
- **Failure behavior:** No unrecorded deletion path exists — ending access is always an audited
  transition.

## AJ-08 — Resolve Content Reports

- **Goal:** Correct problems while avoiding automatic or unaudited removal.
- **Actions:** Review target/evidence; dismiss, request changes, delist/retire, invoke emergency
  Course access suspension, or suspend Account as warranted.
- **Rules:** No auto-hide; reason/action/actor/timestamp audited (BR-145/146).
- **Failure behavior:** Unpublish is reversible and does not erase Entitlements/history.

## AJ-09 — Instructor Payouts — deferred out of MVP

Payout processing, statements, and the earnings ledger are deferred with in-platform payments
(BR-073/074 deferred). Gradex holds no revenue record to calculate a share from, so compensation is
arranged and paid entirely out of band. Revenue-share terms remain a required part of the Instructor
agreement under `LG-020`.

## AJ-10 — Moderate Office Hours

- **Goal:** Stop an inappropriate/invalid session without becoming its scheduler.
- **Actions:** View session details; cancel with reason.
- **Rules:** Admin cannot create platform-wide sessions; cancellation retained/audited and notifies
  entitled Students best-effort (BR-134/137/139/140).

---

# Cross-Cutting Boundaries

- No Instructor price editing, access granting, or in-app payout dashboard.
- No protected sample Lab; public preview is separate.
- No separate Chapter entity.
- No built-in live video, recurrence, RSVP, attendance, recording, or calendar integration.
- No notification preference, marketing, SMS/WhatsApp, or push journey.
- No public review/rating/recommendation journey.
- **No checkout, cart, coupon, order, receipt, or refund journey.** Payment is External Payment and
  access is granted by Admin-approved Course Access Invitation
  ([D-045](DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation)).
- Remaining legal/provider/commercial work is in [LAUNCH_GATES.md](LAUNCH_GATES.md), not hidden in
  journey assumptions.
