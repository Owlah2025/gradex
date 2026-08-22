# Gradex Glossary

> Status: Canonical terminology
> Last Updated: 2026-07-28

The conceptual relationships and lifecycles behind these terms are defined in
[DOMAIN_MODEL.md](DOMAIN_MODEL.md).

## People and Access

| Term | Definition |
|---|---|
| Account | One person's identity, normalized email, role, status, language, display name, and authentication profile. |
| Display Name | Self-chosen, non-unique label shown for an Account; the only identity field an Instructor roster exposes. |
| Student | The only publicly self-registering role; browses, accepts Course Access Invitations, learns, reports content, and joins entitled office hours. |
| Instructor | Admin-invited role that owns and manages Course content and sees its Student roster, but cannot control prices or who is granted access. |
| Admin | Privileged Gradex operator responsible for provisioning, pricing, publishing, moderation, and granting Course access after confirming External Payment. |
| Bootstrap Admin | The first Admin, created once through secure deployment rather than public UI or a repository credential. |
| Verification | Proof that a Student controls the registered email before the Account becomes Active. |
| Invitation | Expiring Admin-issued activation path for an Instructor or additional Admin. |
| Session | Refreshable authenticated relationship that can be revoked independently from the Account. |
| Suspension | Immediate Account restriction blocking protected actions without deleting commercial/content history. |
| Deactivation | Account closure state subject to approved retention and anonymization rules. |

## Course and Learning

| Term | Definition |
|---|---|
| Course | Top-level catalog and learning product owned by one Instructor and priced/published by Admins. |
| Taxonomy Term | *(Legacy, D-022 — superseded by D-091.)* Admin-managed bilingual vocabulary entry used to classify Courses; covers the Major and Subject dimensions. Operational until the Academic Catalog cutover. |
| Institution | A degree-granting university. Owns every academic identity beneath it. Arabic: الجامعة. *(D-091)* |
| Academic Unit | A named organisational unit inside an Institution — `COLLEGE` (الكلية), `DEPARTMENT` (القسم), or `SERVICE_UNIT`. Self-nesting; a unit may attach directly to the Institution. *(D-091)* |
| Program | The degree specialisation a Student follows, owned by an Academic Unit. Distinct from a Department. Arabic: التخصص. *(D-091)* |
| Curriculum | A versioned academic plan (major sheet) for one Program; exactly one is `ACTIVE`. Arabic: الخطة الدراسية. *(D-091)* |
| Curriculum Subject | The mapping between a Curriculum and a Subject, carrying requirement kind, recommended level, recommended semester, and credits as metadata only. *(D-091)* |
| Academic Level | A Student's current standing at their Institution, and separately a CurriculumSubject *recommendation*. Never a property of a Subject or a Gradex Course. Bounds are per-Institution. Arabic: المستوى الدراسي. *(D-091)* |
| Student Academic Profile | A Student's `(Institution, Program, Curriculum, level, enrollment status)`. **Discovery-only — never influences entitlement or access.** *(D-091)* |
| Major | *(Legacy, D-022 — superseded by D-091's Program.)* Course classification dimension naming the field of study, drawn from the Admin vocabulary. |
| Subject | The Institution's canonical academic course identity, with Arabic and English titles and an optional official code such as `0410-101`. Belongs to the Institution, never to one Program; a Gradex Course teaches exactly one. Arabic: المادة (official alternative: المقرر). *(D-091; supersedes the D-022 classification dimension of the same name.)* |
| Study Year | *(Legacy, D-022 — superseded by D-091's Academic Level.)* Course classification dimension using the fixed enumeration `PREP`, `YEAR_1`–`YEAR_4`. Operational until the Academic Catalog cutover. |
| Section | Ordered grouping inside one Course. The canonical domain term. Not an acquirable access scope in MVP. |
| Chapter | Optional localized/Student-facing label for Section; never a separate entity or access scope. |
| Lesson | Ordered learning unit inside one Section, with video and optional protected attachments. |
| Course Revision | Instructor-proposed change to a Published Course that is reviewed separately from the live version. |
| Lesson Resource | Protected reference material such as slides, notes, readings, or images. |
| Lab Material | Protected hands-on project file/guide that may carry a buyer identifier. |
| Public Preview | Optional separate public Course asset; never an exposed protected Lesson resource/lab. |
| Progress | Per-Student Lesson resume/completion state retained after Entitlement expiry. |
| Office Hours | One-off Course-scoped session using an external meeting link protected by authentication/Entitlement. |
| Content Report | Entitled Student report about a Course/Lesson/video/resource/lab, resolved by an Admin. |

## Course Access

| Term | Definition |
|---|---|
| Catalog Price | Current Admin-controlled integer-fils amount for a Course or Section. In MVP the Course price is displayed so a Student knows what to pay externally; Gradex charges nothing. Section prices are retained but not displayed. |
| KWD | Kuwaiti dinar, displayed with three decimal places. |
| Fils | Integer monetary unit; 1 KWD = 1,000 fils. All Gradex money display uses fils. |
| External Payment | Payment performed and verified entirely outside Gradex by the admin team. Gradex stores no payment transaction, amount, currency, or status. |
| Course Access Invitation | Admin-created workflow record binding one Student email to one Course, allowing that Student to request access. It is never the authoritative access record. |
| Admin Approval | The final Admin action on an accepted Course Access Invitation, and the sole authoritative trigger that activates access. |
| Enrollment | Durable Student-to-Course learning relationship used for roster/progress/history. |
| Entitlement | **The authoritative access record.** Time-bounded authorization for one complete Course, carrying a typed grant source. |
| Grant Source | Typed discriminator recording how an Entitlement was granted. MVP implements `MANUAL_INVITATION` only; `PAID_ORDER`, `PROMOTIONAL`, and `DIRECT_ADMIN_GRANT` are reserved and not implemented. |

### Deferred with in-platform payments

Retained as the design of record; none of these entities exists in MVP
([D-045](DECISIONS.md#d-045--mvp-launches-without-in-platform-payments-course-access-is-granted-by-admin-approved-course-access-invitation)).

| Term | Definition |
|---|---|
| Order | One Student's commercial record for exactly one Course or Section, including immutable price/discount/policy snapshots. |
| Payment Attempt | One gateway attempt attached to an Order with its own idempotency and gateway references. |
| Refund | One Admin-requested full/partial amount returned against a captured Order after gateway confirmation. |
| Remaining Refundable Balance | Captured Order amount minus all confirmed refunds; refund requests cannot exceed it. |
| Coupon | Admin-managed percentage/fixed discount, optionally Course/Section-scoped, applied before gateway checkout. |
| Coupon Redemption | Historical Coupon/Student/Order commit; it consumes per-Student eligibility until cumulative full refund releases it. |
| Zero-Value Grant | Successful coupon Order with total 0 KWD that creates normal access without calling a gateway. |
| Net Collected Revenue | Captured amount after coupons, confirmed refunds, and gateway/payment fees; basis for Instructor share. |

## Instructor Accounting — deferred out of MVP

Instructors are paid entirely out of band at launch. Revenue-share terms remain a required part of
the Instructor agreement under `LG-020`.

| Term | Definition |
|---|---|
| Revenue-Share Percentage | One platform-wide commercial configuration; value is undecided and is a required Instructor-agreement term. |
| Earning | Instructor accounting line derived from net collected revenue for an eligible Order. |
| Adjustment | Later correction, such as a refund/chargeback after a prior payout, applied to a future statement. |
| Payout Statement | Monthly itemization of Orders, fees, refunds/adjustments, share, and amount owed to one Instructor. |
| Payout | Manual bank transfer recorded by an Admin with status and reference; not an Instructor withdrawal. |

## Content and System States

| Term | Definition |
|---|---|
| Draft | Course is editable by its Instructor and not public. |
| Pending Review | Course/revision is read-only to the Instructor while an Admin reviews it. |
| Changes Requested | Admin returned a Course/revision with a required reason for Instructor revision. |
| Published | Approved Course version is visible in the catalog and eligible for entitled access. |
| Delisted | Course removed from catalog discovery and new access grants without denying qualifying existing access. |
| Emergency Course Access Suspension | Elevated legal/security/malware/severe-moderation block on existing Student access without rewriting Entitlements. |
| Archived | Terminal state for catalog discovery and new access grants on historical Course records; not a hard delete. |
| Active Entitlement | Current authorization within its access term and not revoked. |
| Expired Entitlement | Authorization whose current effective `access_ends_at` instant has passed. |
| Revoked Entitlement | Authorization ended before natural expiry through an audited Admin action. |
| Audit Event | Immutable record of privileged actor, action, target, reason/context, and timestamp. |
| Launch Gate | Unresolved commercial/legal/provider/readiness item that blocks production (or a named fast-follow feature), not ordinary system design. |

## Release Terms

| Term | Definition |
|---|---|
| MVP | The approved launch scope in [PRD.md §4](PRD.md); not a synonym for every desired future feature. |
| Fast-Follow | Approved post-MVP work that must not block MVP, such as bundles, BNPL, or captions. |
| Out of Scope | Work not approved for MVP or immediate fast-follow. |
