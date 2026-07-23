# Gradex Glossary

> Status: Canonical terminology
> Last Updated: 2026-07-23

The conceptual relationships and lifecycles behind these terms are defined in
[DOMAIN_MODEL.md](DOMAIN_MODEL.md).

## People and Access

| Term | Definition |
|---|---|
| Account | One person's identity, normalized email, role, status, language, display name, and authentication profile. |
| Display Name | Self-chosen, non-unique label shown for an Account; the only identity field an Instructor roster exposes. |
| Student | The only publicly self-registering role; browses, purchases, learns, reports content, and joins entitled office hours. |
| Instructor | Admin-invited role that owns and manages Course content but cannot control prices, refunds, coupons, or payouts. |
| Admin | Privileged Gradex operator responsible for provisioning, pricing, publishing, moderation, commerce, and payouts. |
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
| Taxonomy Term | Admin-managed bilingual vocabulary entry used to classify Courses; covers the Major and Subject dimensions. |
| Major | Course classification dimension naming the field of study, drawn from the Admin vocabulary. |
| Subject | Course classification dimension naming the university subject, with an optional academic code such as `CS 101`. |
| Study Year | Course classification dimension using the fixed enumeration `PREP`, `YEAR_1`–`YEAR_4`. |
| Section | Ordered purchasable grouping inside one Course. The canonical domain term. |
| Chapter | Optional localized/Student-facing label for Section; never a separate entity or purchase scope. |
| Lesson | Ordered learning unit inside one Section, with video and optional protected attachments. |
| Course Revision | Instructor-proposed change to a Published Course that is reviewed separately from the live version. |
| Lesson Resource | Protected reference material such as slides, notes, readings, or images. |
| Lab Material | Protected hands-on project file/guide that may carry a buyer identifier. |
| Public Preview | Optional separate public Course asset; never an exposed protected Lesson resource/lab. |
| Progress | Per-Student Lesson resume/completion state retained after Entitlement expiry. |
| Office Hours | One-off Course-scoped session using an external meeting link protected by authentication/Entitlement. |
| Content Report | Entitled Student report about a Course/Lesson/video/resource/lab, resolved by an Admin. |

## Commerce and Access

| Term | Definition |
|---|---|
| Catalog Price | Current Admin-controlled integer-fils amount for a Course or Section; affects future Orders only. |
| KWD | Kuwaiti dinar, displayed with three decimal places. |
| Fils | Integer monetary unit; 1 KWD = 1,000 fils. All Gradex money calculations use fils. |
| Order | One Student's commercial record for exactly one Course or Section, including immutable price/discount/policy snapshots. |
| Payment Attempt | One gateway attempt attached to an Order with its own idempotency and gateway references. |
| Enrollment | Durable Student-to-Course learning relationship used for roster/progress/history. |
| Entitlement | Time-bounded authorization for one Course or Section, created by a paid Order or zero-value grant. |
| Refund | One Admin-requested full/partial amount returned against a captured Order after gateway confirmation. |
| Remaining Refundable Balance | Captured Order amount minus all confirmed refunds; refund requests cannot exceed it. |
| Coupon | Admin-managed percentage/fixed discount, optionally Course/Section-scoped, applied before gateway checkout. |
| Coupon Redemption | Historical Coupon/Student/Order commit; it consumes per-Student eligibility until cumulative full refund releases it. |
| Zero-Value Grant | Successful coupon Order with total 0 KWD that creates normal access without calling a gateway. |
| Net Collected Revenue | Captured amount after coupons, confirmed refunds, and gateway/payment fees; basis for Instructor share. |

## Instructor Accounting

| Term | Definition |
|---|---|
| Revenue-Share Percentage | One platform-wide commercial configuration used for all MVP Instructor calculations; value is undecided until launch. |
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
| Published | Approved Course version is visible in the catalog and eligible for entitled access/purchase. |
| Delisted | Course removed from catalog discovery/new checkout without denying qualifying existing access. |
| Emergency Course Access Suspension | Elevated legal/security/malware/severe-moderation block on existing Student access without rewriting Entitlements. |
| Archived | Terminal catalog/new-purchase state for historical Course records; not a hard delete. |
| Active Entitlement | Current authorization within its access term and not revoked. |
| Expired Entitlement | Authorization whose current effective `access_ends_at` instant has passed. |
| Revoked Entitlement | Authorization ended before natural expiry, such as after cumulative full refund. |
| Audit Event | Immutable record of privileged actor, action, target, reason/context, and timestamp. |
| Launch Gate | Unresolved commercial/legal/provider/readiness item that blocks production (or a named fast-follow feature), not ordinary system design. |

## Release Terms

| Term | Definition |
|---|---|
| MVP | The approved launch scope in [PRD.md §4](PRD.md); not a synonym for every desired future feature. |
| Fast-Follow | Approved post-MVP work that must not block MVP, such as bundles, BNPL, or captions. |
| Out of Scope | Work not approved for MVP or immediate fast-follow. |
