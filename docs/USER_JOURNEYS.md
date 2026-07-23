# User Journeys

> Status: Aligned with approved MVP
> Last Updated: 2026-07-23

These task journeys apply the canonical scope in [PRD.md](PRD.md), rules in
[BUSINESS_RULES.md](BUSINESS_RULES.md), and terminology in [GLOSSARY.md](GLOSSARY.md).
They describe one responsive website. Student journeys must complete on phones, tablets/iPads,
laptops, and desktops; complex Instructor/Admin operations are responsive but desktop/tablet
optimized.

---

# 1. Student — Discover, Buy, and Learn

```text
Discover → Evaluate → Register/Verify or Sign In → Apply Coupon → Hosted Checkout
                                                                  ↓
Report/Refund ← Practise/Office Hours ← Watch/Resume ← Course Home/Receipt
                                                                  ↓
                                                         Expiry → Buy Again
```

## SJ-01 — Discover a Course

- **Goal:** Find a Course matching a university subject/level.
- **Actions:** Browse the published catalog, filter by Major/Subject/Study Year, search in Arabic or
  English, open a Course detail page.
- **Decisions:** Is this the right Course? Buy the complete Course or one Section?
- **Rules:** `Course → Section → Lesson`; “Chapter” may only label Section (BR-010/021). Only
  `PUBLISHED` Courses are discoverable; search matches both languages with Arabic normalization and
  ranks by relevance only (BR-161/162).
- **Edge cases:** Thin launch catalog; no Course for the selected filter combination; query typed
  with diacritics or a different hamza form; archived/unpublished Course absent from purchase
  results.
- **Failure behavior:** Catalog errors provide retry/empty states without exposing protected data.

## SJ-02 — Evaluate the Course

- **Goal:** Understand content, practical value, Instructor, price, and access term.
- **Actions:** Review outline, authored details, resources/lab inclusion, office-hours support,
  Course/Section prices, 150-day term, and optional public preview.
- **Rules:** Admin controls prices (BR-019); protected Labs/Resources are not previews
  (BR-143/144); active ownership changes CTA to “Go to Course” (BR-024).
- **Edge cases:** No preview asset; some Sections not owned; recently changed price affects future
  Orders only.
- **Failure behavior:** Preview/price/scope mismatch blocks checkout rather than charging stale data.

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

- **Goal:** Authenticate and continue the intended purchase/learning route.
- **Actions:** Enter credentials; refresh/rotate session as needed.
- **Rules:** Generic credential failure (BR-003), revoked refresh rejection (BR-005), immediate
  suspension enforcement (BR-007).
- **Edge cases:** Session expires during checkout; Account becomes suspended while active.
- **Failure behavior:** Re-authentication preserves safe return path; suspension never reaches
  protected content.

## SJ-05 — Apply a Coupon

- **Goal:** See a valid discount before entering hosted checkout.
- **Actions:** Enter code; view subtotal, integer-fils discount, total, and rejection reason.
- **Rules:** One coupon per Order; one consuming redemption per Student; global cap/target/window
  checked server-side (BR-124–129).
- **Edge cases:** Zero-value grant, expired/inactive/wrong-scope/already-used code, cap race.
- **Failure behavior:** Invalid coupon leaves catalog price unchanged; zero total never opens Tap.

## SJ-06 — Pay and Receive Access

- **Goal:** Complete card/KNET payment and know whether access is ready.
- **Actions:** Confirm one Course/Section Order; use Tap-hosted checkout; return to confirming/receipt.
- **Rules:** Verified webhook/API success—not redirect—grants one Entitlement (BR-020/021/031/033).
- **Edge cases:** Delayed callback, ambiguous timeout, duplicate callback, already-active Entitlement.
- **Failure behavior:** Declined/cancelled/timed-out attempt grants no access; ambiguous outcomes
  reconcile before retry (BR-022/034).
- **Notification:** Receipt is recorded after grant; delivery failure does not affect access
  (BR-120–123).

## SJ-07 — Orient in Course Home

- **Goal:** Understand purchased scope, locked Sections, progress, access expiry, materials, and
  upcoming office hours.
- **Actions:** Start/resume a Lesson; view Course outline and explicit locked state.
- **Rules:** Course purchase covers all Sections; Section purchase covers only that Section;
  Enrollment/progress can remain after Entitlement expiry.
- **Edge cases:** Mixed owned/locked Sections; expired access with retained progress.
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
- **Actions:** Download entitled Resource/Lab; open external community; view/join Course office hours.
- **Rules:** Each download is entitlement-checked (BR-023/063); Labs may carry buyer identification
  (BR-103); office-hours link requires active Course/Section Entitlement (BR-135/136).
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

## SJ-11 — Request and Track a Refund

- **Goal:** Request a policy-eligible full/partial refund and see its status.
- **Actions:** Contact/support flow supplies Order and reason; Admin makes the gateway request;
  Student sees pending/succeeded/failed status.
- **Rules:** Accepted bilingual policy version governs eligibility (BR-044/153); gateway success is
  authoritative; partial keeps access, cumulative full revokes (BR-041/046/047).
- **Edge cases:** Multiple partial refunds, unsupported method, amount above remaining balance.
- **Failure behavior:** Pending/failed request does not revoke access; confirmed full refund does.

## SJ-12 — Return After Expiry

- **Goal:** Continue learning after the 150-day term.
- **Actions:** Sign in, see retained progress and expired access, purchase again through normal flow.
- **Rules:** Expiry ends access but preserves Enrollment/progress; active duplicate purchase is
  blocked, expired scope may be repurchased (BR-024/025).
- **MVP boundary:** No expiry reminder or dedicated renewal flow.

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

## IJ-08 — Receive Monthly Payout Statement

- **Goal:** Understand the monthly amount transferred.
- **Actions:** Receive emailed statement listing eligible Orders and adjustments; raise an ops query
  outside the platform if needed.
- **Rules:** One configured global share of net collected revenue; manual bank transfer; no in-app
  earnings/withdrawal (BR-073/074).
- **Edge cases:** Late refund/chargeback appears on next statement.

---

# 3. Admin — Provision, Price, Moderate, Refund, and Reconcile

```text
Bootstrap/Sign In → Invite Staff → Price Courses/Sections → Review/Publish
        ├→ Suspend/Reactivate
        ├→ Coupons/Revenue/Refunds
        ├→ Content Reports/Unpublish
        └→ Monthly Statements/Payouts
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

## AJ-03 — Set Course and Section Prices

- **Goal:** Control catalog commercial terms.
- **Actions:** Set/change price with required reason; review audit history.
- **Rules:** Admin-only, integer fils, future Orders only (BR-019).
- **Failure behavior:** Historical Orders/refunds/payouts remain unchanged.

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

## AJ-06 — Manage Coupons and Revenue

- **Goal:** Run promotions and reconcile money/access.
- **Actions:** Create/edit/deactivate Coupon; view redemption history; inspect Orders/Attempts.
- **Rules:** Admin-only, frozen redeemed value fields, one coupon/order, one consuming redemption
  per Student (BR-124–133).
- **Failure behavior:** Reconciliation flags gateway/Order disagreement; history is not deleted.

## AJ-07 — Process Full or Partial Refunds

- **Goal:** Apply the approved policy safely.
- **Actions:** Check policy/version, remaining balance, and method support; submit amount/reason;
  wait for gateway result.
- **Rules:** Admin-only; idempotent/audited; no access change before confirmation; partial/full
  semantics (BR-040–047).
- **Failure behavior:** Failure leaves access/revenue unchanged; late success applies exactly once.

## AJ-08 — Resolve Content Reports

- **Goal:** Correct problems while avoiding automatic or unaudited removal.
- **Actions:** Review target/evidence; dismiss, request changes, unpublish, or suspend as warranted.
- **Rules:** No auto-hide; reason/action/actor/timestamp audited (BR-145/146).
- **Failure behavior:** Unpublish is reversible and does not erase Entitlements/history.

## AJ-09 — Run Monthly Instructor Payouts

- **Goal:** Transfer the correct amount and produce transparent records.
- **Actions:** Generate/review statement; apply adjustments; approve; transfer by bank; record
  reference; email statement.
- **Rules:** One configured global percentage; net collected basis; late changes go to future
  statement; no automated settlement (BR-073/074).
- **Failure behavior:** Idempotent run/reference checks prevent duplicate payment; corrections are
  adjustments, not silent edits to Paid statements.

## AJ-10 — Moderate Office Hours

- **Goal:** Stop an inappropriate/invalid session without becoming its scheduler.
- **Actions:** View session details; cancel with reason.
- **Rules:** Admin cannot create platform-wide sessions; cancellation retained/audited and notifies
  entitled Students best-effort (BR-134/137/139/140).

---

# Cross-Cutting Boundaries

- No Instructor price editing or in-app payout dashboard.
- No protected sample Lab; public preview is separate.
- No separate Chapter entity.
- No built-in live video, recurrence, RSVP, attendance, recording, or calendar integration.
- No notification preference, marketing, SMS/WhatsApp, or push journey.
- No public review/rating/recommendation journey.
- Remaining legal/provider/commercial work is in [LAUNCH_GATES.md](LAUNCH_GATES.md), not hidden in
  journey assumptions.
