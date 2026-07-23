# Gradex Domain, Data, and State Design

> Status: In progress — conversational Sections 1–3 approved and locked
> Date: 2026-07-26
> Scope: Complete MVP authoritative domain state and PostgreSQL model
> Change boundary: Design only; this record does not implement migrations or application behavior

## 1. Purpose and Authority

This record translates the approved
[platform architecture](2026-07-25-platform-architecture-design.md), canonical
[Domain Model](../../DOMAIN_MODEL.md), [Business Rules](../../BUSINESS_RULES.md),
[PRD](../../PRD.md), [Decisions](../../DECISIONS.md), and
[launch gates](../../LAUNCH_GATES.md) into an implementable domain/data/state design.

PostgreSQL is authoritative. A module owns its tables and invariants; another module may change that
state only through an explicit application contract. One application coordinator may compose
module commands in one database transaction when a cross-module invariant requires atomicity.

Open legal, accounting, provider, retention, and operating questions remain configurable boundaries.
This design must not supply an invented answer to `LG-001`–`LG-004`, `LG-008`–`LG-010`, or
`LG-014`–`LG-020`.

The conversational approval sections map to this evolving record as follows:

- Section 1: §§2–3.
- Section 2: §§4–7.
- Section 3: §§8–11.
- Later sections will add the remaining operational modules, retention treatment, and migration
  sequence.

## 2. Shared Relational Conventions

### 2.1 Selected approach

Gradex uses a strongly relational core with stable logical content identities and immutable
submitted/published versions. JSONB is not an escape hatch for business structure.

| Concern | Convention |
|---|---|
| Primary keys | UUID; existing UUID identities remain valid during migration |
| Instants | `timestamptz`, persisted in UTC |
| Admin calendar dates | Interpreted in Kuwait time; the exclusive end-of-day boundary is the first instant of the following local day converted to UTC |
| Money | `bigint` integer fils plus explicit three-letter currency; no floating point |
| State values | Constrained text owned by migrations; no unconstrained status strings |
| Mutable aggregates | `revision bigint NOT NULL`; every update compares the expected revision and increments it exactly once |
| JSONB | Provider payload mirrors, immutable external/document evidence, and Audit metadata only |
| Deletion | Hard delete only history-free drafts/unreferenced vocabulary; financial, access, learning, moderation, and Audit history remains |
| External work | Authoritative state plus stable-ID outbox intent commit together |

Every optimistic mutation is equivalent to:

```sql
UPDATE aggregate
SET ..., revision = revision + 1
WHERE id = $id AND revision = $expected_revision;
```

Zero affected rows is a concurrency conflict, not silent success.

### 2.2 Idempotency and asynchronous delivery

`idempotency_records` is shared infrastructure, not a substitute for domain uniqueness:

- scope includes authenticated actor or external merchant/provider account, command/endpoint, and
  caller-supplied key;
- the request fingerprint covers the material canonical request;
- reusing a scope/key with a different fingerprint is rejected;
- processing/result state and the authoritative result reference are retained for the applicable
  policy period;
- a unique constraint covers `(principal_scope, principal_id, command, key)`.

Every `outbox_events.id` is a stable event ID created in the originating transaction. Consumers
record `(consumer, event_id)` or enforce an equivalent domain idempotency key. Redis/asynq delivery
may duplicate or disappear without losing PostgreSQL intent.

Required Audit evidence participates in the state-changing transaction. Optional notification,
email, reporting, and projection work follows the outbox.

## 3. Access Expiry, Content History, and Authority

### 3.1 Exact commercial and runtime values

A Course stores `default_access_ends_at` for future purchases. A Section stores no independent
access period.

| Value | Authority |
|---|---|
| `courses.default_access_ends_at` | Admin-configured input for future Course/Section checkout |
| `order_items.access_ends_at` | Immutable expiry disclosed and commercially accepted for that Order |
| `entitlements.original_access_ends_at` | Immutable initial purchased/granted expiry |
| `entitlements.access_ends_at` | Current authoritative runtime expiry after Admin adjustments |

Access is allowed only while:

```text
current_timestamp < entitlement.access_ends_at
```

Changing the Course default affects future Orders only. Checkout is unavailable without a future
Course default, but existing entitled Students may continue using a Published Course.

An elevated Admin may extend or shorten an individual Entitlement. The same transaction updates the
effective expiry and inserts an immutable adjustment containing old/new expiry, actor, reason,
timestamp, and optional support/refund reference, plus Audit and notification outbox evidence.
Moving expiry into the past ends access immediately without deleting Enrollment, Progress, Order,
or adjustment history.

### 3.2 Retirement and historical access

Omission, supersession, retirement, and archival are different:

- **omitted:** not selected into one revision; does not change the logical identity;
- **superseded:** a newer authored version or approved Course revision replaced an older version;
- **retired:** an explicit privileged command blocks future acquisition/inclusion;
- **archived:** hidden from normal operational views according to lifecycle, without erasing history.

Retirement never occurs merely because an Instructor omitted content from a draft.
Catalog delisting removes discovery/new checkout but preserves qualifying existing access. Emergency
Course access suspension is the distinct elevated legal/security/malware/severe-moderation command
that blocks existing runtime access; it records constrained reason, Audit, and notification/outbox
evidence without mutating Entitlements.

A retired Course, Section, Lesson, or authored version remains accessible only through an
otherwise-active Entitlement whose `retirement_eligibility_at`, copied from Order `accepted_at`,
predates `retired_at`. A grandfathered paid Order must remain within its payment deadline and have a
verified capture occurrence within that deadline. Acquisition/payment/webhook times remain separate.

Each Order Item and Entitlement preserves the approved Course revision used at acquisition. A Course
Entitlement authorizes the current approved graph plus qualifying retired content from its acquired
graph. A Section Entitlement authorizes its stable Section and the qualifying Lesson graph acquired
with it. New Entitlements never silently receive retired content.

## 4. Identity and Access Data

### 4.1 Explicit single-role boundary

Every Account has exactly one role assigned at creation and immutable during MVP:

- Student Accounts alone place Orders, receive ordinary Entitlements, create Enrollments, and
  record Progress.
- Instructor Accounts author assigned content but cannot consume Student content.
- Admin Accounts use the separate authorized and audited preview path; preview creates no ordinary
  Entitlement or Progress.
- A person needing separate capabilities uses a separate Account with another normalized email.

There is no `account_roles` table and no role-update command. `accounts.role` is constrained
`NOT NULL` and excluded from ordinary Account update statements.

Future role conversion is a separate deferred feature. It must validate the target role and
incompatible active state/obligations, prevent Course orphaning, update role, increment
`session_epoch`, revoke all Sessions/token families, preserve ownership, purchases, Progress,
authored content, Audit, and all historical foreign keys, and atomically record old/new role, actor,
reason, timestamp, Audit, and outbox. None of that behavior is partially implemented in MVP.

If multi-role membership is later approved, migration creates `account_roles`, backfills one
membership from each immutable `accounts.role`, changes authorization to read memberships, and only
then retires the old column. Role grant/revocation history is preserved separately. This migration
path is not an MVP table or capability.

### 4.2 Owned tables

| Table | Purpose and important constraints |
|---|---|
| `accounts` | Normalized unique email, immutable constrained role, status, display name, locale, verification/security timestamps, `session_epoch`, revision |
| `password_credentials` | Restricted one-to-one password hash and change timestamp; no profile data |
| `sessions` | Account, refresh family, hashed token material, rotation ancestry, expiry, revocation/reuse state |
| `account_tokens` | Hashed verification/reset/email-change token, purpose, subject, expiry, consumed time; bearer secret never stored |
| `staff_invitations` | Normalized unregistered email, Instructor/Admin role, inviter, token digest, lifecycle, expiry, optional resulting Account |
| `instructor_profiles` | Optional role-specific Instructor metadata without duplicating Account role or identity |
| `policy_acceptances` | Immutable Account, policy kind/version, accepted time, locale, and evidence |
| `instructor_agreement_acceptances` | Immutable agreement/version evidence; terms remain blocked on `LG-020` |

Indexes/constraints include:

- unique normalized Account email;
- one credential per Account;
- token-digest uniqueness and purpose/status expiry indexes;
- partial uniqueness for one active staff invitation per normalized email;
- role/status checks and Student-only ownership checks at the relevant module command boundaries;
- Account/session indexes that support immediate suspension and family revocation.

July 27 freezes token hashing, rotation-family, immediate-suspension, and rate-limit mechanics.

### 4.3 Staff invitation transactions

Invitation creation atomically writes:

1. the Invitation and hashed token;
2. required Audit evidence;
3. a stable notification outbox event.

It creates no placeholder Account.

Invitation acceptance atomically:

1. locks and validates the Invitation/token;
2. rechecks that its normalized email belongs to no Account;
3. marks the token/Invitation consumed;
4. creates the Account with its immutable assigned role and verified email;
5. creates its password credential and applicable profile;
6. links `resulting_account_id`;
7. records required policy/agreement acceptance;
8. writes Audit and outbox evidence.

Expired, revoked, replayed, or conflicting acceptance creates no Account.

## 5. Catalog Revision Graph

### 5.1 Revision lifecycle

A Course may have at most one mutable `DRAFT` revision. Submission validates and atomically freezes
the complete graph:

```text
DRAFT → IN_REVIEW → APPROVED → SUPERSEDED
                    └→ REJECTED
```

`IN_REVIEW`, `APPROVED`, `REJECTED`, and `SUPERSEDED` revisions are immutable. A change request
records an immutable rejection/review decision and clones the reviewed graph into a new `DRAFT`;
it never reopens the submitted revision. An approved revision is never edited.

The Course presentation lifecycle remains
`DRAFT/PENDING_REVIEW/CHANGES_REQUESTED/PUBLISHED/DELISTED/ARCHIVED`. Delisting controls discovery
and new checkout only. A Published Course remains Published while another revision is in review.
Retirement and emergency access suspension remain orthogonal.

### 5.2 Stable identities and immutable graphs

| Table | Ownership |
|---|---|
| `courses` | Stable aggregate, owner, presentation status, current approved revision pointer, future-purchase expiry, community link, retirement/archive fields, revision |
| `course_revisions` | Course, revision state, frozen time, submitter, review/evidence references |
| `course_versions` | Immutable Course-authored fields selected by one revision |
| `sections` / `lessons` | Stable logical identities and ancestry with explicit retirement/archive fields |
| `section_versions` / `lesson_versions` | Immutable authored values |
| revision membership tables | Exact Course/Section/Lesson versions and positions comprising the complete graph |
| `course_review_decisions` | Immutable reviewer, outcome, reason, evidence, and timestamp |
| `course_access_suspensions` | Elevated legal/security/malware/severe-moderation block, actor/reason/evidence, start/end, resolution |

Submission and approval validate the entire graph: ownership, exact ancestry, unique ordering,
classification, nonempty structure, required ready/scanned assets, and absence of retired content.
Approval revalidates under locks and atomically moves every required approved-current pointer. No
Course can expose a partially approved graph.

`course_revisions` has `UNIQUE(course_id, id)`. The Course pointer uses a composite foreign key:

```text
(courses.id, courses.current_approved_revision_id)
    → course_revisions(course_id, id)
```

The referenced revision must be frozen and `APPROVED` at pointer-swap time. PostgreSQL retains many
historically approved/superseded revisions; only `courses.current_approved_revision_id` determines
the public graph. Immutable review/approval evidence is never overwritten on supersession.

A partial unique index permits at most one `DRAFT` revision per Course. Position constraints enforce
one Section position per Course revision and one Lesson position per Section membership.

## 6. Taxonomy, Price, and Retirement

### 6.1 Taxonomy

`taxonomy_terms` contains a constrained `kind` of `MAJOR` or `SUBJECT`, bilingual labels, optional
Subject code, lifecycle, and revision. `UNIQUE(id, kind)` supports composite foreign keys from
classification rows carrying constrained `major_kind = MAJOR` and `subject_kind = SUBJECT`.

Major and Subject remain independent controlled dimensions in MVP. No approved product rule says a
Subject is compatible only with selected Majors, so the schema does not invent that restriction.
If product later approves compatibility, a `major_subject_compatibility` join can be introduced
without changing stable term IDs.

### 6.2 Catalog prices and access-period configuration

`catalog_prices` has exactly one target—Course or Section—enforced by foreign keys plus an exclusive
target check. It stores positive integer fils and explicit currency (`KWD` in MVP). Zero-value access
uses a real Coupon Order, not a zero catalog price.

`price_changes` immutably records target, old/new amount and currency, Admin, reason, and time.
`course_access_period_changes` does the same for old/new future-purchase expiry.

A price record never makes content sellable by itself. Checkout also requires Published/current
graph eligibility, a future access expiry, and no retirement block. Retired targets reject new price
activation. Completed Orders rely only on their immutable Order Item snapshots.

Indexes support one current price per target, Admin price queues, and Course/Section checkout lookup.

### 6.3 Explicit retirement command

Retirement is an elevated Admin command with reason, compare-and-swap, Audit, and outbox evidence.

- Retiring a Course blocks Course and contained Section checkout immediately.
- Retiring a Section blocks direct Section checkout.
- If the current approved graph contains a retired Section or Lesson, any new purchase whose scope
  would include it is blocked.
- Checkout may resume after a newly approved complete graph excludes the retired content.
- Existing qualifying Entitlements continue through their historical acquired graph under §3.2.

Omitting content from a draft, superseding a version, and archiving an operational record do not
invoke retirement.

## 7. Media and Asset Versions

### 7.1 Owned records and exact references

| Table | Purpose |
|---|---|
| `media_assets` | Stable exact kind, owning Course/Lesson scope, visibility/security classification, retirement |
| `media_asset_versions` | Immutable source object identity, content metadata/hash, permission evidence, state, successful attempt references |
| `upload_intents` | Expected object/version/limits, signer, lifecycle, expiry, completion evidence |
| `scan_attempts` | Provider/reference, state, reason code, timestamps, immutable result evidence |
| `processing_attempts` | Operation ID, transformation profile, state, retry relation, output identity, evidence |
| `video_renditions` | Exact successful video-version outputs and technical metadata |

Asset kind and owning scope use relational checks/foreign keys: Lesson Video, Lesson Resource, Lesson
Lab Material, and Course Public Preview cannot point to an invalid owner. A Course revision
references the exact `media_asset_version`, not merely a mutable Asset.

Visibility distinguishes protected content from public preview. A public preview is its own Asset
and version with explicit public-use permission evidence; a protected Resource is never converted
into a preview by toggling visibility.

Accepted object identity—provider, bucket, key, provider version ID, size, checksum algorithm, and
checksum—is immutable and unique where appropriate. A multipart ETag is not accepted as a
cryptographic checksum. Gradex stores a provider-supported checksum or independently calculated
content hash.

### 7.2 Type-specific lifecycle

Shared states have type-specific branches:

```text
AWAITING_UPLOAD
    → QUARANTINED
    → SCANNING
        → READY
        → PROCESSING → READY
```

`UPLOADING` belongs to `upload_intents` unless the storage integration supplies a reliable upload-
started signal. The Asset Version remains `AWAITING_UPLOAD` until completion verification.

- files needing no transformation may move from successful scanning directly to `READY`;
- video/transformed assets move from successful scanning through `PROCESSING`;
- `FAILED` is retryable and records the failed stage/reason;
- a retry creates a new immutable attempt and may re-enter that stage;
- `REJECTED` is terminal for the source version;
- scan rejection, unsupported format, permission rejection, and processing failure use distinct
  reason codes/evidence;
- a rejected source requires a new `media_asset_version`;
- `READY` references the successful scan and, where applicable, processing attempt.

`READY` means technically valid only. Student availability comes from the approved Course revision,
Course state, Account status, and Entitlement—not from a Media `PUBLISHED` flag.

### 7.3 Upload-completion boundary

Remote verification and a PostgreSQL transaction are not described as one atomic operation:

1. Gradex verifies the immutable provider object identity/version, size, allowed type, and
   cryptographic checksum.
2. In one PostgreSQL transaction it locks/rechecks the intent and Asset Version, records verification
   evidence, marks the intent completed, moves the Asset Version into quarantine/scan state, writes
   required Audit evidence, and creates the stable-ID processing outbox event.
3. The successfully verified provider object is non-overwritable; replacement creates a new Asset
   Version.

Duplicate completion uses the scoped idempotency record and returns the original result. Conflicting
object identity or request fingerprint is rejected.

The current `videos` table and direct-asynq handoff are migration inputs, not a second production
source of truth. July 28 sequencing preserves the working slice while moving it into Asset Versions
and the PostgreSQL outbox.

## 8. Commerce and Payment State

### 8.1 Owned tables

| Table | Purpose and important constraints |
|---|---|
| `orders` | One Student, commercial lifecycle, accepted policy/version, currency/totals, revision, distinct `created_at`, `accepted_at`, `payment_deadline_at`, `completed_at`, `expired_at`, `cancelled_at` |
| `order_items` | Exactly one immutable Course/Section snapshot per Order, target IDs, containing Course, acquired approved revision, title/scope evidence, `access_ends_at`, subtotal/discount/total |
| `payment_attempts` | Order, scoped idempotency reference, provider account/reference, mutable lifecycle, requested amount/currency, verified capture/occurrence and receipt times |
| `payment_attempt_state_history` | Immutable old/new Attempt state, source provider event/local observation, reason, timestamp |
| `payment_provider_events` | Verified/deduplicated immutable callback/API evidence, payload digest, provider event/reference, authenticity result |
| `payment_reconciliation_cases` | Ambiguous, conflicting, late, or invalid-sequence outcomes requiring retry or operator resolution |
| `refunds` | Admin request, positive amount/currency, reason, policy evidence, idempotency/provider references, lifecycle |
| `refund_provider_events` | Verified/deduplicated immutable refund callback/API evidence |
| `dispute_events` | Immutable provider dispute/chargeback evidence; policy effects remain gated by `LG-017` |
| `commercial_documents` | Immutable receipt/invoice/refund-document data boundary; required fields/numbering remain gated by `LG-016` |

Only a Student Account may own an Order. `order_items` uses separate Course/Section foreign keys plus
an exclusive-target check, and `UNIQUE(order_id)` enforces at most one item. A deferred constraint
trigger or transaction-end invariant requires exactly one item before an Order commits as payable or
free-granted.

All monetary columns are nonnegative integer fils, share the Order currency, and satisfy:

```text
subtotal_amount - discount_amount = total_amount
0 <= discount_amount <= subtotal_amount
```

`order_items.access_ends_at`, target identity, acquired revision, catalog price, Coupon snapshot,
accepted policies, amount, and currency never change after Order creation.
`payment_deadline_at < order_items.access_ends_at` is required.

### 8.2 Order and Payment Attempt lifecycles

```text
Order:
PENDING_PAYMENT → PAID → PARTIALLY_REFUNDED → REFUNDED
       ├────────→ CANCELLED
       ├────────→ EXPIRED
       ├────────→ RECONCILIATION_REQUIRED
       └────────→ FREE_GRANTED

CANCELLED/EXPIRED ── verified evidence money moved ─→ RECONCILIATION_REQUIRED

Payment Attempt:
CREATED → PENDING → SUCCEEDED | FAILED | CANCELLED
                    └→ TIMED_OUT → SUCCEEDED | FAILED | CANCELLED | UNKNOWN
                                     UNKNOWN → definitive provider outcome
```

Order refund state is updated from confirmed Refund totals but never replaces Refund rows. A zero-
value Coupon Order enters `FREE_GRANTED`; it has no Payment Attempt or refundable captured amount.
`CANCELLED` and `EXPIRED` are separate outcomes, not a sequence. `RECONCILIATION_REQUIRED` is a
non-success exception state resolved only from preserved evidence: safe completion to `PAID`, proof
of no capture to the applicable terminal outcome, or Refund handling when money moved but access
cannot be granted.

An Order is accepted only after rechecking Student role/status, target sellability, current price,
Coupon reservation, future expiry, live conflicting Order/Entitlement coverage, current approved
revision, and retirement. `accepted_at` is copied into the later Entitlement as
`retirement_eligibility_at`; `payment_succeeded_at` and `entitlement.acquired_at` remain separate.
The immutable revision snapshot prevents later graph changes from rewriting the accepted purchase.

Retirement blocks new Orders immediately but does not invalidate an Order accepted before
`retired_at` that remains within its deadline. Verified provider capture within that deadline may
grant even when its callback is received later. A capture occurring after deadline or a success
against a cancelled/expired/conflicting Order records Payment evidence and enters reconciliation;
it cannot automatically grant or be discarded.

A partial unique index permits only one active `CREATED`/`PENDING`/`UNKNOWN` Attempt per Order. A new
Attempt is permitted only after the prior one is definitive. `SUCCEEDED` means verified captured
payment, not authorization. `FAILED`/`CANCELLED` are provider-confirmed terminal outcomes;
`TIMED_OUT` is a local observation and remains reconcilable; `UNKNOWN` blocks another Attempt.
Provider occurrence—not event arrival—controls deadline eligibility. Immutable provider events and
Attempt history preserve every observation/transition.

### 8.3 Refund state

Only an Admin creates a Refund. The command locks the Order and confirmed Refund total and rejects:

- zero/negative or wrong-currency amounts;
- an amount above remaining captured balance;
- an unsupported provider/method capability;
- a request not supported by the accepted policy configuration.

`REQUESTED → PENDING → SUCCEEDED | FAILED | CANCELLED`. External calls occur outside the state
transaction; verified results return through idempotent provider events. Pending/failed/cancelled
Refunds do not change access or revenue.

The exact eligibility policy remains versioned/configurable under `LG-002`. Tax/document treatment
remains gated by `LG-016`; dispute/chargeback effects remain gated by `LG-017`.

### 8.4 Commerce indexes

Indexes cover:

- Student Order history by `(student_id, created_at DESC)`;
- one live pending Order per exact Student/acquisition scope, backed by access-guard locking for
  cross-level Course/Section conflicts;
- operational queues by Order/Attempt/Refund state and age;
- unique scoped idempotency and provider references;
- reconciliation cases by state/next-check time;
- remaining-balance reads by Order;
- policy/document lookup without exposing provider payloads to ordinary queries.

## 9. Coupons

### 9.1 Relational targets and history

| Table | Purpose |
|---|---|
| `coupons` | Normalized code, type/value, validity window, active flag, global cap, `reserved_count`, historical `consumed_count`, creator, revision |
| `coupon_course_targets` | Relational Course target with `(coupon_id, course_id)` uniqueness |
| `coupon_section_targets` | Relational Section target with `(coupon_id, section_id)` uniqueness |
| `coupon_redemptions` | Coupon, Student, Order, discount, `RESERVED/CONSUMED/RELEASED_*` lifecycle, reservation deadline, consume/release evidence |

No target rows in either target table means platform-wide. Targets do not use polymorphic
`item_type/item_id` columns.

Coupon code is normalized uppercase/trimmed and unique. Percentage is `1..100`; fixed discount is
positive integer fils. Validity bounds, cap, and discount math use database checks plus command
validation. Once any Redemption exists, code/type/value are immutable. A Coupon with history
deactivates rather than deletes.

### 9.2 Reservation and exact capacity invariants

- `UNIQUE(order_id)` ensures one Redemption per Order.
- A partial unique index on `(coupon_id, student_id)` for `RESERVED/CONSUMED` enforces one
  capacity-affecting use per Student.
- Capacity is exact under a Coupon row lock:
  `reserved_count + consumed_count < max_redemptions`.
- Paid Order acceptance increments `reserved_count` and inserts `RESERVED` with
  `reservation_expires_at = order.payment_deadline_at`.
- Timely paid success atomically changes `RESERVED → CONSUMED`, decrements `reserved_count`, and
  increments historical `consumed_count`.
- Order expiry/cancellation changes `RESERVED → RELEASED_UNUSED` and decrements `reserved_count`.
- Cumulative full Refund changes `CONSUMED → RELEASED_AFTER_FULL_REFUND` for Student eligibility
  only; it never decrements `consumed_count` or restores global quota. Partial Refund does nothing.
- Zero-value Order creation inserts directly as `CONSUMED` and increments `consumed_count`.
- Coupon preview has no side effect. Order creation revalidates and snapshots the result.

The Coupon row and Student eligibility are locked at Order acceptance and again at paid completion.
Duplicate acceptance/payment/free-grant processing returns the existing Redemption and cannot
reserve or consume twice.

## 10. Entitlements and Learning

### 10.1 Entitlement-owned tables

| Table | Purpose and important constraints |
|---|---|
| `entitlements` | Required unique `source_order_id`, Student, exact Course/Section scope, containing Course, acquired revision, `acquired_at`, `retirement_eligibility_at`, original/effective expiry, constrained revocation evidence, revision |
| `entitlement_adjustments` | Immutable old/new expiry, elevated Admin, reason, support/refund reference, time |
| `student_course_access_guards` | One `(student_id, course_id)` row used to serialize grant, adjustment, revocation, and duplicate-coverage checks |

Every Entitlement has exactly one Course or Section target, and its source Order Item must carry the
same Student, scope, containing Course, acquired revision, and original expiry.
`UNIQUE(source_order_id)` enforces exactly one Entitlement per successful Order.

Expiry is derived from the authoritative instant; no midnight job mutates rows merely to mark them
expired:

```text
revoked_at IS NULL AND current_timestamp < access_ends_at
```

`REVOKED` is an explicit persisted outcome. `EXPIRED` is a query/runtime classification. Account
suspension and explicit emergency Course access suspension deny access without rewriting the
Entitlement. Catalog delisting, retirement, and archival do not independently deny qualifying
existing access.

The access predicate additionally requires:

- Account is active and role is Student;
- Course has no active emergency access suspension;
- target/lesson is in the current graph or qualifying acquired historical graph;
- `retirement_eligibility_at` predates applicable retirement and the source Order met its deadline;
- Course or Section scope covers the requested Lesson.

### 10.2 Duplicate and overlap control

Current-time and cross-level Course/Section coverage cannot be safely enforced with one partial
unique index. The Student/Course access-guard row is locked at Order acceptance and again at paid
success, Refund/reversal, and expiry adjustment.

At Order acceptance the transaction rejects a conflicting active Entitlement or live pending Order,
reserves Coupon capacity, and snapshots the graph/terms. At paid success it rechecks coverage before
consuming the reservation and inserting the source-unique Entitlement. If another successful Order
already produced conflicting coverage, Payment evidence is retained and the second Order enters
reconciliation—no duplicate Entitlement is created.

Coverage rules are:

- an active Course Entitlement blocks a Course or any contained Section purchase;
- an active Section Entitlement blocks that Section only;
- a Course purchase remains allowed when only Section Entitlements are active, with no credit,
  proration, automatic Refund, or expiry combination;
- after that purchase both Entitlements remain independent and access is their union;
- refund/revocation of the Course Entitlement leaves the Section Entitlement untouched;
- extending an older Entitlement is rejected when it would overlap a later Entitlement covering the
  same or broader/narrower conflicting scope.

Indexes cover `(student_id, containing_course_id, access_ends_at)` for non-revoked rows and a
Section-specific lookup. Database foreign keys and the guard-lock transaction together enforce the
invariant; an application-only preflight check is insufficient.

### 10.3 Learning-owned tables

| Table | Purpose and important constraints |
|---|---|
| `enrollments` | Durable unique Student/Course relationship, first enrollment/acquisition times, lifecycle-independent learning history |
| `lesson_progress` | Enrollment + stable Lesson, last/max position milliseconds, permanent `completed_at`, first completing Media Asset Version, last-watched time, revision |

`UNIQUE(student_id, course_id)` creates or reuses one Enrollment even for a Section purchase.
Enrollment has no access-authority state and remains after expiry, revocation, refund, retirement,
delisting, access suspension, or Account suspension.

`UNIQUE(enrollment_id, lesson_id)` is the Progress identity. Each write requires current runtime
access and a Lesson reachable through the current approved or qualifying acquired graph. The server
uses the trusted duration of the exact played Media Asset Version; client percentages are ignored.
Reported positions are bounded/validated before `GREATEST` updates. `completed_at` and
`completed_media_asset_version_id` are written once at the server-calculated 90% threshold and never
cleared. Video replacement preserves completion and maximum historical position.

Progress updates use compare-and-swap/monotonic SQL expressions so reordered or retried writes cannot
reduce maximum position or completion. Expiry/revocation preserves Progress but blocks further
playback-derived writes. The Instructor roster joins only Course-scoped Enrollment/Progress
aggregates to the Account display name contract; it never exposes email, payment data, or
cross-Course activity.

## 11. Critical Commerce Transactions

### 11.1 Paid Order acceptance

One PostgreSQL transaction:

1. locks the Student/Course access guard and rejects conflicting active Entitlements or live pending
   Orders for the acquisition scope;
2. locks/revalidates Course state, current approved graph, retirement, price, expiry, and policies;
3. locks the Coupon when applicable, validates exact capacity/Student eligibility, and inserts a
   deadline-matched `RESERVED` Redemption;
4. inserts the `PENDING_PAYMENT` Order and immutable one-item snapshot with
   `payment_deadline_at < access_ends_at`;
5. writes required Audit/outbox intent.

The provider session is created only after this commit. Failure to create it is recorded against the
Order/Attempt and the reservation is released on cancellation/expiry.

### 11.2 Paid grant

Provider signature/authenticity, payload shape, and evidence normalization occur before the state
transaction. One PostgreSQL transaction then:

1. stores/deduplicates the verified provider event and locks Order, Attempt, Coupon reservation, and
   the same Student/Course access guard;
2. verifies captured amount/currency/reference, provider occurrence within deadline, grandfathered
   retirement eligibility, and that no conflicting Entitlement now exists;
3. resolves the Attempt as captured and completes the Order as paid;
4. moves Coupon Redemption `RESERVED → CONSUMED` when applicable;
5. creates or reuses the Learning Enrollment;
6. creates exactly one `UNIQUE(source_order_id)` Entitlement from the immutable Order Item, copying
   `accepted_at → retirement_eligibility_at` and recording separate `acquired_at`;
7. writes required Audit evidence and stable-ID outbox events for receipt, reporting/payout, and
   notification.

Any invariant failure rolls back all authoritative grant effects. A duplicate callback returns the
existing result. A second successful payment that now conflicts, or any late/unsafe capture, retains
Payment evidence and moves the Order to reconciliation rather than creating duplicate access.

### 11.3 Zero-value Coupon grant

Order creation and grant occur in one transaction with no gateway:

1. lock the access guard and Coupon capacity;
2. revalidate target, expiry, price, Coupon, coverage, pending Orders, and retirement;
3. insert the Order/one immutable Order Item as `FREE_GRANTED`;
4. insert Coupon Redemption directly as `CONSUMED`;
5. create/reuse Enrollment and create the Entitlement;
6. write Audit and stable-ID outbox events.

The scoped Order-creation idempotency key returns the same Order, Redemption, and Entitlement on
retry.

### 11.4 Confirmed Refund

External provider verification occurs first. A verified successful Refund result transaction:

1. deduplicates the provider event and locks Refund, Order, Entitlement, Coupon Redemption, and
   access guard as applicable;
2. marks the Refund successful and recomputes cumulative confirmed amount;
3. updates the Order's derived refund state;
4. leaves access active for a partial Refund;
5. on cumulative full Refund, revokes only the Entitlement whose `source_order_id` is the refunded
   Order, records the exact successful Refund IDs causing the threshold, and changes Coupon
   Redemption to `RELEASED_AFTER_FULL_REFUND` exactly once without restoring historical quota;
6. emits immutable reporting/payout adjustment, Audit, and notification events.

It never deletes Enrollment/Progress or alters any other Entitlement. Coupon reuse still requires an
active applicable Coupon with remaining global capacity. A later dispute/chargeback uses separate
immutable events and cannot inherit an invented Entitlement policy before `LG-017`.

### 11.5 Entitlement expiry adjustment

The elevated Admin command locks the access guard and Entitlement, checks compare-and-swap and
overlap rules, updates effective expiry, inserts the immutable adjustment, and writes Audit plus
Student-notification outbox evidence. The original expiry and Order Item never change.

### 11.6 Constrained Entitlement reversal

There is no generic Admin “revoke Entitlement” command. A zero-value Entitlement has no Refund path
and normally ends only through expiry. Any earlier reversal must reference a defined and authorized
source workflow—reconciliation correction, approved fraud/abuse policy, future chargeback-equivalent
policy, or legal action—with constrained reason, actor, evidence, Audit, and notification/outbox.
Unavailable/unapproved workflows cannot be selected as free-form reasons.

## 12. Section 1–3 Approval Record

The developer/product owner approved:

- the relational/versioned-content approach and Section 1 foundations;
- exact Entitlement expiry authority and audited shortening/extension;
- pre-retirement commercial-event eligibility;
- mutable-draft/frozen-submission revision semantics;
- Account creation on invitation acceptance;
- the same-Course approved-pointer invariant;
- relational taxonomy kinds with independent MVP Major/Subject dimensions;
- explicit privileged retirement and checkout blocking;
- type-specific Media state and immutable attempt/object evidence;
- strict price/asset target and checksum constraints;
- immutable single-role Accounts with no Student capability for Instructor/Admin roles.
- every ordinary Entitlement originating from a paid or zero-value Coupon Order; there is no
  separate manual Admin grant.
- exact Coupon reservation/consumption/release capacity;
- explicit Order cancellation, expiry, and reconciliation state/timestamps;
- separate Order acceptance, provider capture, Entitlement acquisition, and retirement eligibility;
- one active Attempt across `CREATED/PENDING/UNKNOWN` with immutable observation history;
- access-guard locking at both Order acceptance and paid success;
- independent Section-to-Course Entitlements and union/refund behavior;
- catalog delisting separated from retirement and emergency Course access suspension;
- source-Order-only full-Refund revocation and constrained non-Refund reversal;
- access-checked, exact-Asset-Version Progress evidence;
- external provider verification before atomic PostgreSQL result application.

Conversational Section 3 is approved and locked after these corrections.

Sections covering Moderation, Office Hours, Notifications, Reporting/Payouts, Audit, retention,
remaining asynchronous failure state, and migration sequencing remain in progress.
