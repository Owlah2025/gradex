# Gradex Domain, Data, and State Design

> Status: Independently approved — advisory dispositions pending exact-commit verification
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
- Section 4: §§13–19.
- Section 5: §§21–24.
- Section 6: §§25–28.

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
| Deletion | Financial ledger, Statement, payout, and privileged Audit evidence is never hard-deleted. Other deletion/anonymization requires an approved owner workflow and retention policy; only history-free drafts/unreferenced vocabulary may be deleted directly. |
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

Every `outbox_events.id` is the stable operation/event ID created in the originating transaction.
Consumers record `UNIQUE(operation_id, consumer_name)` or enforce an equivalent domain idempotency
key. Different consumers may process one operation once each; the same consumer cannot apply it
twice. Redis/asynq delivery may duplicate or disappear without losing PostgreSQL intent.

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
   required Audit evidence, and creates stable-ID scan/processing outbox work.
3. The successfully verified provider object is non-overwritable; replacement creates a new Asset
   Version.

Duplicate completion uses the scoped idempotency record and returns the original result. Conflicting
object identity or request fingerprint is rejected.

The current `videos` table and direct-asynq handoff are migration inputs, not a second production
source of truth. The §§21–24 sequence preserves the working slice while moving it into Asset
Versions and the PostgreSQL outbox.

## 8. Commerce and Payment State

### 8.1 Owned tables

| Table | Purpose and important constraints |
|---|---|
| `orders` | One Student, commercial lifecycle, accepted policy/version, currency/totals, revision, distinct `created_at`, `accepted_at`, nullable `payment_deadline_at`, `completed_at`, `expired_at`, `cancelled_at` |
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
`payment_deadline_at` is required for `PENDING_PAYMENT` Orders and must satisfy
`payment_deadline_at < order_items.access_ends_at`; it is `NULL` for a directly completed
`FREE_GRANTED` Order. The status-specific nullability and inequality are enforced together.

### 8.2 Order and Payment Attempt lifecycles

```text
Order:
PENDING_PAYMENT → PAID
PAID → PARTIALLY_REFUNDED → REFUNDED
PAID ─────────────────────→ REFUNDED
PENDING_PAYMENT → CANCELLED
PENDING_PAYMENT → EXPIRED
PENDING_PAYMENT → RECONCILIATION_REQUIRED
accepted zero-value Order → FREE_GRANTED

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
`item_type/item_id` columns. A restricted Coupon may contain any valid combination of Course and
Section targets; MVP has no hidden “one target type” rule. At validation, the purchase target must
match at least one relational target, and duplicate, nonexistent, or otherwise invalid target rows
are rejected by their owning constraints/guarded command.

Coupon code is normalized uppercase/trimmed and unique. Percentage is `1..100`; fixed discount is
positive integer fils. Validity bounds, cap, and discount math use database checks plus command
validation. Once any Redemption exists, code/type/value are immutable. A Coupon with history
deactivates rather than deletes.

### 9.2 Reservation and exact capacity invariants

- `UNIQUE(order_id)` ensures one Redemption per Order.
- A partial unique index on `(coupon_id, student_id)` for `RESERVED/CONSUMED` enforces one
  capacity-affecting use per Student.
- Capacity is exact under a Coupon row lock. `max_redemptions IS NULL` means unlimited; otherwise
  acceptance requires `reserved_count + consumed_count < max_redemptions`.
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

1. validates that the Account is an active eligible Student not suspended from checkout;
2. locks/revalidates the Catalog target/current graph, retirement, price, future expiry, and
   policies;
3. locks the Commerce acquisition scope and Coupon when applicable, rejecting another live
   conflicting Order and validating exact capacity/Student eligibility;
4. locks the Student/Course Entitlement access guard and rejects conflicting active coverage;
5. inserts the `PENDING_PAYMENT` Order, immutable one-item snapshot, and deadline-matched
   `RESERVED` Redemption when applicable, with
   `payment_deadline_at < access_ends_at`;
6. writes required Audit/outbox intent.

The provider session is created only after this commit. Failure to create it is recorded against the
Order/Attempt and the reservation is released on cancellation/expiry.

### 11.2 Paid grant

Provider signature/authenticity, payload shape, and evidence normalization occur before the state
transaction. One PostgreSQL transaction then:

1. validates the Account reference and locks the Catalog Course snapshot needed to preserve the
   owner at the authoritative completion instant;
2. stores/deduplicates the verified provider event and locks Order, Attempt, and Coupon reservation;
3. locks the same Student/Course Entitlement access guard and verifies amount/currency/reference,
   provider occurrence within deadline, grandfathered retirement eligibility, and absence of
   conflicting coverage;
4. resolves the Attempt as captured, completes the Order, and moves Coupon Redemption
   `RESERVED → CONSUMED` when applicable;
5. invokes Entitlements to create exactly one `UNIQUE(source_order_id)` Entitlement from the
   immutable Order Item, copying `accepted_at → retirement_eligibility_at` and recording separate
   `acquired_at`, then invokes Learning to create/reuse Enrollment;
6. invokes Reporting's earning-snapshot contract, which locks/validates the effective share
   configuration and returns a typed completion-time snapshot containing Course owner,
   commissionable base, discount-funding treatment, currency, source Order Item, share version, and
   stable earning-operation ID;
7. writes required Audit evidence and stable-ID outbox events. The reporting event is the
   authoritative pending carrier of that immutable earning snapshot until Reporting creates the
   source-linked ledger entry; receipt and notification events carry their own immutable inputs.

Any invariant failure rolls back all authoritative grant effects. A duplicate callback returns the
existing result. A second successful payment that now conflicts, or any late/unsafe capture, retains
Payment evidence and moves the Order to reconciliation rather than creating duplicate access.

### 11.3 Zero-value Coupon grant

Order creation and grant occur in one transaction with no gateway:

1. validate the active eligible Student and lock/revalidate Catalog target, expiry, price, graph,
   and retirement;
2. lock the Commerce acquisition scope and Coupon capacity, validate Coupon/pending Orders, and
   reject unavailable capacity;
3. lock the Entitlement access guard and reject conflicting coverage;
4. insert the Order/one immutable Order Item as `FREE_GRANTED` and Redemption as `CONSUMED`;
5. invoke Entitlements to create the source-linked Entitlement, then Learning to create/reuse
   Enrollment;
6. write Audit and stable-ID receipt/notification outbox events. Zero value creates no positive
   earning intent.

The scoped Order-creation idempotency key returns the same Order, Redemption, and Entitlement on
retry.

### 11.4 Confirmed Refund

External provider verification occurs first. A verified successful Refund result transaction:

1. deduplicates the provider event and locks the Commerce Refund, Order, and Coupon Redemption;
2. locks the Entitlement access guard and source Entitlement as applicable;
3. marks the Refund successful, recomputes cumulative confirmed amount, and updates the Order's
   derived refund state;
4. leaves access active for a partial Refund;
5. on cumulative full Refund, revokes only the Entitlement whose `source_order_id` is the refunded
   Order, records the exact successful Refund IDs causing the threshold, and changes Coupon
   Redemption to `RELEASED_AFTER_FULL_REFUND` exactly once without restoring historical quota;
6. invokes Reporting's immutable original-earning adjustment intent, then emits Audit and
   notification events.

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

Conversational Section 3 is approved and locked after these corrections. Sections 4–6 and their
approval records follow below.

## 13. Moderation and Content Reports

### 13.1 Owned records and lifecycle

| Table | Purpose and important constraints |
|---|---|
| `content_reports` | Origin/reporter, stable logical target, exact reported version/revision, constrained reason, optional note, lifecycle, assigned Admin, revision |
| `content_report_events` | Immutable submission, assignment, review, decision, action reference, actor, reason, and timestamp |

An entitled Student may report a Course, Lesson, video, resource, lab material, or Office-Hours
Session. A report preserves both the stable logical target and the exact Course Revision, Course/
Lesson Version, Media Asset Version, Office-Hours Session revision, or equivalent version visible
when reported. The target and version are relational: constrained nullable foreign keys plus
exclusive-target checks, or equivalent type-specific target tables. An unchecked
`target_type/target_id` pair or current-pointer-only reference is not authoritative.

Automated systems may create a typed report or scan finding with immutable source evidence and no
reporting Account. Automated findings do not perform moderation hiding, retirement, or Account
suspension. Media quarantine/rejection and emergency security suspension remain separate,
constrained safety workflows rather than implicit report resolution.

```text
OPEN → UNDER_REVIEW → RESOLVED_DISMISSED
                    └→ RESOLVED_ACTIONED
```

A partial unique constraint prevents the same reporter from opening a second unresolved report
against the exact target identity and reported Version where that duplicate rule applies. It does
not combine different reporters/Versions, block a new report after resolution, or constrain an
authorized system finding. Rate limiting and abuse controls are additional July 27 security policy,
not a replacement for that invariant. “Other” requires an explanation. Reports never auto-hide,
auto-retire, or auto-suspend content.

### 13.2 Resolution and source-module authority

An Admin may dismiss a report or select an authorized source-module command: request Catalog
changes, delist, explicitly retire, invoke emergency Course access suspension, cancel an
Office-Hours Session, or suspend an Account. Moderation never updates Catalog, Identity, Media, or
Office Hours tables directly.

Following §25.1, the application coordinator validates the Report reference, acquires the affected
owner-module locks before the Moderation Report lock, validates the Admin/revisions, invokes the
owner command, then atomically records the report resolution/event, required Audit evidence, and
stable-ID notification/outbox events. Failure of the source command leaves the report unresolved.
The resulting source action ID is retained so
`RESOLVED_ACTIONED` always identifies the exact action taken. Decision explanations and action
results are appended as immutable events; editing the report status never overwrites them.

Indexes support the Admin queue by `(state, created_at)`, assigned-Admin work, target history, and
the unresolved-report uniqueness check.

## 14. Office Hours

### 14.1 Session data and lifecycle

| Table | Purpose and important constraints |
|---|---|
| `office_hour_sessions` | Stable Course/creator identity, current Version pointer, cancellation state, revision |
| `office_hour_session_versions` | Immutable title/description, UTC start/end, encrypted external-link ciphertext/key version, creator/time |
| `office_hour_session_events` | Immutable create, material reschedule, cancellation, and moderation history |
| `office_hour_delivery_evidence` | Separate attendance, Instructor check-in, meeting-provider, recording, no-show, or dispute evidence when a supported workflow supplies it |

```text
Derived time phase: UPCOMING → LIVE → ENDED
Explicit mutation: ACTIVE → CANCELLED
```

An uncancelled Session is `UPCOMING` before `starts_at`, `LIVE` from `starts_at` while
`current_timestamp < ends_at`, and `ENDED` afterward. Time proves only that the scheduled window
ended; it never asserts delivery or attendance. No scheduled write persists `ENDED`. Start must
precede end. Cancellation and rescheduling are explicit compare-and-swap mutations with immutable
events. Rescheduling creates an immutable Session Version and atomically changes the current pointer;
reports and delivery evidence retain the exact Version they observed.

The owning active Instructor may create/materially reschedule only for an owned Published Course.
Ownership at creation remains evidence, while the current owner controls later Instructor changes.
The current owner may cancel after delisting or archival; an Admin may cancel only through the
audited moderation path.

Join access is distinct from historical access. The external meeting link is disclosed only during
the permitted `LIVE` window and still requires an active Student Account, current Course or
contained-Section Entitlement, uncancelled Session, and no active emergency Course access
suspension. The Session page and any separately authorized materials/recording may remain available
after `ENDED` to qualifying Students. Delisting, retirement, or archival alone does not remove that
historical access. Cancellation blocks joining but never deletes Session, notification, attendance,
delivery, or Audit history. Admin moderation preview follows its separate audited path.

The external link is decrypted and returned only by the specialized authorized join command. It is
never included in catalog/list responses, notification payloads, Audit metadata, application logs,
or public pages. Indexes support upcoming Sessions by `(course_id, starts_at)` and operational
cancellation/history queries.

Creation, material rescheduling, and cancellation atomically write the Session/event, required
Audit evidence, and one deduplicated notification outbox intent. Delivery never controls the
Session transaction.

## 15. Notifications

### 15.1 Durable record and delivery evidence

| Table | Purpose and important constraints |
|---|---|
| `notification_events` | Stable source event, category/type, template version, safe render parameters, occurrence time |
| `notification_recipients` | Event, exact Account, channel, locale/destination snapshot, durable in-app recorded/read state |
| `notification_delivery_attempts` | Recipient row, provider reference, attempt number, lifecycle, reason/timestamps |

Through Notifications' transaction-aware command, the source transaction relationally inserts the
exact Account audience; a delayed worker never recalculates recipients from the current roster.
`UNIQUE(notification_event_id, account_id, channel)` prevents duplicate channel records. The in-app
recipient row is the durable Notification. Its `read_at` is monotonic and never cleared. Email is a
best-effort mirror:

```text
PENDING → SENT
    └───→ FAILED
```

Each retry creates a new attempt or immutable attempt-history record; earlier evidence is not
overwritten. Exact provider, suppression, bounce, and retry policy remain blocked on `LG-018`.

Notification Events have an explicit category:

- **mandatory transactional/security:** receipts, Refund/reconciliation status, security, invitation,
  Entitlement expiry adjustment, and emergency access suspension/restoration; preferences cannot
  suppress these;
- **operational:** Course review outcomes, Office Hours creation/cancellation/material reschedule,
  and Instructor Media-processing completion, using the fixed product channel policy;
- **optional marketing:** consent/preference controlled and outside MVP.

Lifecycle automation, marketing delivery, preference controls, SMS, WhatsApp, and push remain
outside MVP. Email failure never removes, invalidates, or changes the durable in-app recipient row.

The source business transaction creates the stable outbox event. The Notifications consumer
idempotently renders/delivers the already-fixed recipients and records attempts. Failure or
exhaustion never rolls back or changes the source action. Payloads carry IDs and safe rendering
inputs, not protected links, credentials, provider payloads, or unnecessary personal data.

Indexes support recipient history/unread reads, source-event deduplication, and delivery work by
state and next-attempt time.

## 16. Reporting, Earnings, and Payouts

### 16.1 Versioned share and append-only ledger

| Table | Purpose and important constraints |
|---|---|
| `revenue_share_configurations` | Required version, percentage/basis points, effective range, creator/approver, evidence; no code default |
| `earning_ledger_entries` | Immutable earning, fee, Refund, chargeback, payout adjustment, carry-forward, or correction with stable source and commercial snapshots |
| `reporting_reconciliation_cases` | Scoped missing/ambiguous Order, Payment, Refund, chargeback, fee, earning, or ledger evidence |
| `payout_statements` | Instructor, half-open statement period, currency, lifecycle, frozen totals, revision |
| `payout_statement_items` | Exact ledger entries included once in a Statement |
| `payout_statement_events` | Immutable lifecycle, review, approval, block, and exception evidence |
| `payout_transfer_attempts` | Immutable destination snapshot, amount/currency, idempotency, transfer/reference, payer, lifecycle, paid time, supporting document |

The numeric share remains blocked on `LG-001`; no earning can be calculated without one effective,
non-overlapping approved configuration. Tax, fee, invoice, and statement-field treatment remains
blocked on `LG-016`. Dispute/chargeback effects remain blocked on `LG-017`.

Gradex uses the approved durable-outbox alternative for pending earnings rather than a separate
`earning_intents` table. During paid completion, Reporting's transaction-aware command locks and
validates the effective `revenue_share_configurations` row and returns the typed immutable snapshot;
the source transaction persists that snapshot in the stable-ID reporting outbox event. That event
is the authoritative pending earning carrier until an idempotent Reporting consumer creates the
source-linked ledger entry and receipt. The event may be minimized only after those authoritative
records and source references satisfy §28. Reporting owns the snapshot/event contract; Shared Work
owns the outbox row.

Each paid Order completion creates exactly one positive ledger entry, deduplicated by stable source
event ID. It snapshots:

- the Instructor owning the Course at Order completion;
- the effective revenue-share configuration version;
- Order/Course identity, source Order Item, stable earning-operation ID, currency, gross price,
  Coupon discount, discount-funding treatment, confirmed payment fees, commissionable/net-collected
  basis, share calculation, and final Instructor amount.

Zero-value grants create no positive earning. Course reassignment and share changes affect later
Orders only. A confirmed Refund or future approved chargeback creates an immutable negative
adjustment linked to the original earning and original Instructor using the applicable snapshotted
commercial/share policy; it never rewrites the positive line or moves the adjustment to the current
Course owner.

Every earning, provider fee, Refund, chargeback, payout adjustment, carry-forward, and approved
manual accounting correction is an immutable ledger entry. Corrections append compensating entries;
existing lines are never edited or deleted. Every adjustment relationally identifies its exact
source—original earning, Refund, chargeback, provider fee, payout, or approved manual-correction
record—and carries stable source-event uniqueness.

The paid-grant/Refund transaction emits the stable reporting event; it does not depend on the
Reporting consumer to complete access or a Refund. In one idempotent consumer transaction,
Reporting deduplicates that event, rechecks the immutable Order/Payment/Refund evidence and effective
configuration version referenced by the completion snapshot, inserts the ledger entry, and records
its consumer receipt. It never resolves history from the current Catalog owner or latest share
configuration. Missing or contradictory evidence creates a reconciliation case instead of
inventing a value. A periodic reconciliation compares completed paid Orders and confirmed Refunds
with source-linked ledger entries. Reporting projections may be rebuilt; the immutable ledger is
authoritative for Statement calculation.

### 16.2 Statements and late adjustments

```text
DRAFT → READY_FOR_REVIEW → APPROVED → PAYMENT_PENDING → PAID
DRAFT / READY_FOR_REVIEW → BLOCKED → DRAFT
PAYMENT_PENDING → PAYMENT_FAILED → PAYMENT_PENDING
```

`UNIQUE(instructor_account_id, currency, period_start, period_end)` enforces one Statement per
Instructor/currency/accounting period. Statements cover monthly, half-open, non-overlapping
periods. The exact accounting cutoff timezone and closing procedure remain configured with the
approved accounting treatment rather than hidden in code. `UNIQUE(ledger_entry_id)` on Statement
Items prevents one ledger line from appearing in two Statements.

Approval freezes the exact ledger-entry set, calculated totals, and approved bank/payment
destination; approved and paid Statements are never recalculated or rewritten. A reconciliation
case blocks only a Statement that contains or may depend on its affected Instructor, currency,
source transaction, or accounting period. Unrelated cases cannot block other payouts.

`APPROVED → PAYMENT_PENDING` creates an immutable transfer Attempt using the approved destination.
`PAID` requires verified evidence of the exact approved amount/currency, destination snapshot, bank
reference, payer, paid time, supporting document, elevated actor, Audit, and statement-notification
outbox. Partial Statement payment is prohibited in MVP. A failed/ambiguous transfer retains its
immutable Attempt; retry first reconciles bank/provider state and uses idempotency so it cannot
create duplicate evidence or double payment.

A negative adjustment arriving after approval enters a later open Statement and never rewrites the
approved one. A nonpositive payable balance cannot enter `PAYMENT_PENDING`; its approved balance is
carried forward through immutable ledger entries and never produces a negative bank transfer.

Indexes cover Instructor/occurrence period, source-event uniqueness, unassigned ledger entries,
Statement lifecycle/period, and bank-reference reconciliation. The Instructor receives the
statement by email; there is no MVP earnings dashboard, withdrawal control, or automated
settlement.

## 17. Audit

`audit_events` is an append-only privileged-action record containing:

- stable event ID, actor Account and immutable role snapshot;
- constrained action, owning module, exact target type/ID, and target revision where applicable;
- required reason, safe before/after or immutable evidence metadata;
- correlation ID plus applicable idempotency, source action, support, policy, and provider
  references;
- authoritative occurrence timestamp.

Audit records evidence; they do not own workflow state. Every privileged change commits its required
Audit row in the same PostgreSQL transaction as the source change. This includes identity/security,
invitation, taxonomy, price/expiry, submission/review/publishing/preview, retirement/access
suspension, refund/reconciliation, Coupon, moderation, Office Hours, payout, retention, and Admin
display-name reset operations. Every Admin protected-content preview is recorded separately.

The application database role cannot update or delete Audit rows. Indexes support time, actor,
module/target, action, and correlation/reference investigations. Partitioning/export mechanics and
retention duration remain configurable; no Audit deletion job is activated before `LG-003`.
Credentials, bearer secrets, protected links, full provider payloads, and unnecessary personal data
must not enter Audit metadata.

## 18. Outbox, Scheduled Work, and Failure Recovery

### 18.1 PostgreSQL authority

| Table | Purpose and important constraints |
|---|---|
| `outbox_events` | Stable UUID, source module/aggregate, constrained type/schema, safe payload or authoritative source reference, state, availability, attempts/lease, last error, completion/exhaustion evidence |
| `outbox_dispatch_attempts` | Immutable attempt number, worker/transport, start/end, outcome, reason |
| `consumer_event_receipts` | Operation ID + consumer name, processing/result reference; unique pair |
| `scheduled_jobs` | Durable job kind/subject, due time, lifecycle, lease/attempt/evidence; only where a derived clock check is insufficient |

Source state and its outbox intent commit atomically. PostgreSQL preserves the stable work/event ID,
payload or source reference, state, availability time, attempt count, lease owner/expiry, last
error, and completion evidence. Redis/asynq is disposable acceleration and never the only record
that work is due; a dispatcher can reconstruct every pending item from PostgreSQL alone. Delivery
is at least once, so every consumer uses `UNIQUE(operation_id, consumer_name)` or equivalent domain
idempotency. Transport acceptance is not consumer completion.

Retries use bounded backoff and immutable attempt evidence. After the configured bound, the event
records `exhausted_at` and remains visible in an operational queue/alert; it is never silently
dropped. Manual retry is a sensitive idempotent command with actor, reason, Audit, and a new
immutable attempt linked to the exhausted work item; it never resets or erases earlier evidence and
does not duplicate the source event. Before retrying an ambiguous email, transfer, Refund, or other
external side effect, the command reconciles provider state. If success cannot be safely excluded,
the item remains reconciliation-required rather than being resent blindly. A reconciliation sweep
republishes uncompleted intent after worker/Redis loss.

Durable scheduled work uses `PENDING → RUNNING → SUCCEEDED | RETRYABLE_FAILED | EXHAUSTED`.
Workers claim due rows through a bounded lease; an expired lease returns retryable work to the
queue without creating a second source effect. Clock-derived states such as Entitlement expiry and
Office-Hours ending do not get artificial scheduled jobs.

Domain modules keep their own authoritative attempt/history records for Payments, Refunds, Media,
email, and reconciliation. The shared outbox does not collapse those lifecycles into a generic job
status. Optional projection/report/notification failures do not change successful source state.

Indexes cover available unpublished work `(published_at, available_at)`, exhausted work, aggregate
history, scheduled due/lease recovery, and consumer receipt lookup.

### 18.2 Recovery order

After database/worker failure:

1. restore and verify PostgreSQL authority;
2. restart outbox/scheduled-work leases and reconcile stale attempts;
3. rebuild Redis queues from PostgreSQL intent;
4. reconcile provider-side Payments, Refunds, and Media operations from immutable references;
5. retry projections, reporting, and notifications idempotently;
6. verify exhausted queues and Audit/reconciliation evidence.

Redis loss may delay work but cannot lose an Order, Entitlement, upload intent, payout line,
Notification record, or required Audit event.

## 19. Retention and Privacy-Control Records

### 19.1 Configurable policy boundary

| Table | Purpose and important constraints |
|---|---|
| `retention_policy_versions` | Data class, action (`RETAIN/DELETE/ANONYMIZE`), duration/rule, effective range, approval evidence |
| `legal_holds` | Exact subject/data class/scope, authority, reason, start/end, active state |
| `retention_scope_guards` | Subject/scope serialization row shared by hold creation and deletion claims |
| `retention_actions` | Immutable lifecycle plus planned/executed/skipped/failed database/object-store evidence |
| `data_subject_requests` | Requester/subject, verification evidence, requested scope, lifecycle, outcome/action references |

There is no default retention duration and no destructive retention job is enabled while `LG-003`
is open. Applicable privacy rights, identity verification, response deadline, cross-border
treatment, and notice wording remain blocked on `LG-004`; the model preserves evidence without
inventing legal conclusions.

The policy inventory covers Identity/profile, credentials/security/session evidence, authored
content and revisions, Learning/Progress, Commerce/provider evidence, Entitlements, Media source and
derived objects, Moderation, Notifications, earnings/payouts, Audit/outbox, and backups.

Data-subject requests use an operational lifecycle that does not presume which legal rights apply:

```text
RECEIVED → VERIFYING_IDENTITY → IN_REVIEW → ACTION_PENDING → COMPLETED
                        └───────────────→ REJECTED
RECEIVED/VERIFYING_IDENTITY ───────────→ CANCELLED
```

Rejection requires an approved legal/policy basis and evidence. The exact response deadline and
available request types remain gated.

Retention execution is idempotent and fail-closed:

- an active legal hold or absent/ambiguous approved policy blocks automated and manual mutation;
- financial ledger entries, Statements, payout evidence, and privileged-action Audit records are
  append-only and never rewritten or hard-deleted;
- financial, access, learning, authored, moderation, payout, and Audit relationships are not hard
  deleted merely because an Account is deactivated;
- anonymization preserves referential integrity and immutable commercial/operational evidence while
  removing approved personal identifiers;
- durable object deletion is provider-version-specific, records verification, and cannot silently
  overwrite or detach an Asset Version;
- backup expiry follows the approved backup-copy policy envelope rather than pretending immediate
  physical deletion;
- every execution/skip/failure produces retention evidence and required Audit/outbox records.

Remote object deletion is not claimed to be atomic with PostgreSQL:

1. **Authorize transaction:** lock the retention scope guard, revalidate the effective approved
   policy and legal gates, prove no active hold, identify the exact database/object version, mark it
   `DELETION_PENDING`, record policy/reason/actor or job/request time, and emit stable deletion work.
2. **External step:** re-lock/recheck the scope immediately before claiming work, then delete the
   exact provider, bucket, key, and provider object version. Verify absence or provider-confirmed
   permanent deletion; a delete marker is insufficient when an underlying version remains.
3. **Confirm transaction:** record provider evidence and mark remote deletion confirmed, preserving
   only the minimum policy-authorized tombstone and required Audit evidence. Failure or
   unverifiable deletion remains `DELETION_PENDING` or becomes `DELETION_FAILED`, never completed.

Legal-hold creation locks the same scope guard. It blocks an unclaimed pending deletion. If an
external deletion is already claimed, hold creation surfaces and records the conflict for immediate
resolution rather than silently claiming the object is protected. Database anonymization/deletion
commits only under the approved policy and hold checks, with immutable action evidence in the same
transaction.

Operational expiry prevents use of expired tokens, sessions, invitations, reservations, and signed
URLs immediately; their eventual cleanup timing still follows an approved policy. Indexes support
active holds, effective policies by data class, due actions, request queues, and failed object
deletions.

## 20. Section 4 Approval Record

The developer/product owner approved and locked §§13–19 after these corrections:

- Office Hours uses time-derived `UPCOMING/LIVE/ENDED`; delivery and attendance are separate
  evidence, and cancellation never deletes history.
- Moderation preserves both stable targets and exact reported versions; automated findings cannot
  silently apply moderation actions.
- Notification Events snapshot Account/channel recipients relationally at source-event time;
  mandatory notices cannot be suppressed and email failure cannot invalidate in-app records.
- every financial change is an immutable source-linked ledger entry; corrections compensate rather
  than rewrite.
- Statement approval freezes its items/totals; payout uses immutable transfer attempts/evidence,
  prohibits partial payment, carries negative balances forward, and scopes reconciliation blocks.
- PostgreSQL contains enough durable work/lease/attempt/completion state to rebuild Redis queues;
  ambiguous external retries reconcile before resend.
- remote deletion requires exact-version authorization, external verification, and a confirming
  transaction serialized with legal-hold checks.

## 21. Migration Governing Rule and Controls

The existing applied `backend/internal/db/migrations/0001_init.up.sql` remains unchanged. Gradex
uses in-place expand–backfill–cutover–contract:

> Expand without changing legacy authority; shadow-convert authentic state through resumable
> mappings; converge live source changes; validate and harden each context; fence writes; switch
> authority exactly once; reject legacy writes permanently; observe and reconcile; then remove
> legacy structures through later destructive but forward-only contract migrations.

Authentic stable identity, authored content, Media, and learning state are preserved. Migration
never fabricates commercial, legal, moderation, approval, or financial provenance.

### 21.1 Migration-owned records

| Table | Purpose and important constraints |
|---|---|
| `migration_batches` | Context/kind, code/schema version, initial and final/fenced high-water marks, source-journal range, final sweep time, lifecycle, lease, counts, last error |
| `migration_batch_events` | Immutable start, lease/takeover, pause, retry, validation, failure, and completion evidence |
| typed legacy mapping tables | Source ID/version/journal position, target IDs, conversion/final fingerprints, latest-state confirmation, batch |
| `migration_review_items` | Exact source/context, reason, preserved evidence, current effective disposition pointer |
| `migration_review_dispositions` | Immutable disposition, approver, reason/evidence/time, optional superseded-disposition reference |
| `migration_context_cutovers` | Context, authority, monotonically increasing `authority_epoch`, fence/high-water mark, application version, validation/approval evidence |
| `migration_reconciliation_reports` | Immutable counts, fingerprints, semantic checks, blocker summary, code version |
| `contract_migration_approvals` | Observation, recovery, rehearsal, adapter-independence, and authorization evidence for destructive forward work |

Context-wide high-water marks belong to the Batch/cutover record. Individual typed mapping rows
carry source identity/version, conversion fingerprint, final source fingerprint, target IDs, and
confirmation that the target reflects the latest authoritative source state. Target-row existence
alone never proves successful conversion.

Only one Batch per context/kind may be `CONVERTING` or `VALIDATING`, enforced with a partial unique
constraint plus a PostgreSQL advisory lock or equivalent durable lease. Explicit parallelism
requires non-overlapping partition ownership. Lease takeover appends evidence and uses
compare-and-swap; typed mapping uniqueness prevents two workers from choosing different targets for
one source.

### 21.2 Live-write convergence and authority epochs

While legacy authority remains active, every context converges post-inventory changes through a
trusted source revision/high-water mark, a temporary change journal, compatibility shadow
projection, repeated fingerprint sweep, or a combination. Before cutover:

1. establish a brief context write fence or route every command through the compatibility handler;
2. process the final source-journal/high-water delta;
3. compare current source fingerprints with mapped final fingerprints;
4. validate semantic invariants and promote constraints;
5. switch authority and increment the context epoch atomically;
6. reject independent legacy writes permanently.

The serving API/command handler or worker—not the browser or untrusted caller—attaches its expected
epoch from trusted deployment/database state. The database compares it with the current epoch in
the same transaction or guarded command as the authoritative mutation. A worker checks before or
while leasing and immediately before committing. An epoch change makes the attempt stale: it is
preserved and rejected/relinquished, never silently retried under different authority.

Automated conversion writes migration evidence only. It does not emit receipts, Student
Notifications, earnings, payout events, moderation/publication actions, or ordinary Audit claims
that historical business actions occurred. Real privileged migration decisions—cutover approval,
manual remapping, exclusion, quarantine release, and disposition—write normal append-only Audit
evidence through Audit's contract.

## 22. Expand–Backfill–Cutover–Contract Sequence

### 22.1 Inventory and recovery checkpoint

- Inventory schema/source counts, UUID relationships, legacy status/evidence, Redis queues, object
  references, fingerprints, and orphan/ambiguity candidates.
- Record database backup ID and recovery timestamp/LSN, schema/application version, current context
  epochs, outbox high-water mark, active leases/queue inventory, provider-event boundary, and the
  relevant object-reference manifest.
- A complete object inventory is mandatory for a Media/object-reference switch; other contexts
  record only their relevant object references.
- Verify recovery evidence before mutation. Legacy authority remains unchanged.

### 22.2 Expand

Add the approved structures, Shared Work/Audit/migration controls, nullable compatibility columns,
and `NOT VALID` constraints without changing read or write authority. Deploy new-model command
handlers and compatibility adapters in shadow mode. Before cutover, new-table writes are projections
or migration shadows, never independent business authority.

### 22.3 Catalog shadow conversion

- Preserve every safe Course, Section, and Lesson UUID.
- Create exactly one mapped mutable Draft graph per convertible Course.
- Retain legacy presentation flags as migration evidence only.
- Do not fabricate Instructor submission, Admin approval, approval time, policy acceptance,
  publication history, or Audit evidence.
- An unconvertible Course/descendant receives an explicit linked disposition; it never silently
  disappears or remains referenced by an authoritative target.

### 22.4 Media shadow conversion

`legacy videos.id → media_assets.id`; the initial `media_asset_versions.id` is distinct and recorded
in immutable `legacy_media_mappings`. Each mapping preserves Lesson association, object/provider
location, legacy status/duration/technical evidence, source row/fingerprint, batch, and verification
outcome.

Schema migration transactions only record candidates and verification intent. Resumable migration
workers externally copy/verify object identity, version, size, checksum, scan, and processing
evidence, then commit an idempotent result:

- `READY` only with all required verified evidence;
- `QUARANTINED` when an object exists but verification/scan is incomplete;
- `FAILED` or migration review when missing/inconsistent.

Legacy status alone never proves readiness. Absent historical malware evidence requires a fresh
scan. Immediately before `READY`, Media reconfirms that the source/provider identity has not
changed. A mutable key must gain an immutable provider version ID or be copied to a non-overwritable
location.

### 22.5 Identity and Learning shadow conversion

The current `0001_init` schema contains no Account, credential, or Session table, so Identity is
greenfield and its authentic legacy conversion inventory is empty today. A Course owner, Progress
user, or `fake_entitlements` UUID reference alone does not prove Account existence or role. The
following compatibility rules apply only if an authentic transitional Identity source exists before
the recorded cutover: preserve compatible Account UUIDs; send duplicate normalized emails,
malformed/unverified Accounts, and ambiguous staff roles to review; preserve password hashes only
when their algorithm/parameters meet the new credential contract; and revoke refresh Sessions
unless token-family, hashing, rotation, and reuse-detection semantics are proven compatible.
Identity cutover initializes/increments `session_epoch` and requires reauthentication wherever
Sessions cannot migrate safely.

Progress converts only when Student Account, containing Course, stable Lesson, Enrollment
create/reuse, position, and completion evidence resolve authentically. Values convert to bounded
milliseconds under monotonic rules. Enrollment created from learning activity grants no access.
When an authentic legacy completion lacks an exact completing Asset Version, retain explicit
legacy-migration completion evidence rather than inventing the Version. Invalid/ambiguous rows
enter review.

### 22.6 Work Delivery fence and dependency-aware context cutovers

Cutover order is:

1. Identity;
2. Work Delivery;
3. Catalog;
4. Media;
5. Learning;
6. Commerce and Entitlements together;
7. operational projections and Reporting.

Work Delivery cuts before new contexts rely on the outbox. Compatibility workers map legacy
task/payload IDs to stable operation IDs. The fence disables all direct-enqueue producers; every
post-fence operation originates in PostgreSQL. A legacy and outbox representation of one operation
cannot both apply a successful transition.

A legacy queue is drained only when every inventoried operation is terminal, cancelled with
evidence, or mapped to authoritative PostgreSQL work; producers are disabled; active leases
completed/expired; delayed, retry, archived, and dead-letter locations are empty/reconciled; and no
ambiguous external effect remains. The assessment includes maximum scheduled delay, retry horizon,
lease/visibility timeout, worker clock skew, and archived/dead-letter locations—not merely visible
queue count.

Catalog may cut before Media because all converted graphs remain Draft. Publishing requires exact
verified `READY` Asset Versions. Commerce and Entitlements switch as one access boundary because
verified completion transactionally creates the source-linked Entitlement.

### 22.7 Development access and constraint promotion

`fake_entitlements` creates no Account proof, Order, Item, Entitlement, Redemption, payment,
earning, or access history. After Commerce/Entitlement tables exist—but before entitlement smoke
tests/authorization cutover—non-production environments may run deterministic idempotent seeds that
create new valid zero-value Coupon Orders. Unit tests use isolated fixtures, not deployed database
state. No production environment runs these seeds.

Before each context switch, required columns are backfilled, invalid rows resolved/quarantined,
foreign keys/checks validated, partial/exclusion indexes built, required columns promoted to
`NOT NULL`, ownership/authorization constraints verified, and the new command path proven unable to
create invalid state. Hardening is not deferred to final Contract.

### 22.8 Observation and Contract

After every explicit authority switch, legacy requests may flow only one way:

```text
legacy request → new authoritative command → optional read-only legacy projection
```

Independent dual writes are prohibited. Legacy writes and direct enqueue paths stay rejected.
Observe/reconcile the new authority, then remove legacy structures only after §24 gates pass through
later destructive but forward-only migrations. There are no destructive down migrations.

## 23. Bounded-Context Authority Matrix

| Context | Legacy authority and convergence | Fence/promotion blockers | Post-cutover authority and exit |
|---|---|---|---|
| Identity | Greenfield under current `0001_init`: no authentic Account/credential/Session rows. UUID refs alone are not identity. If a transitional source exists before cutover, map revisions/fingerprints, preserve only compatible credentials, review email/role/status ambiguity, and revoke incompatible Sessions. | Empty current-source reconciliation or final transitional identity delta; email/role/credential/status constraints; password-reset, verification-token, suspended-Account, revocation, and reauthentication tests. | New Identity only. Compatible rollback versions use it. No unresolved authentication/authorization ambiguity. |
| Work Delivery | Direct Redis/asynq producer plus legacy operation state. Map payload/task IDs to stable operation IDs; shadow outbox does not dispatch. | Disable producers; inventory every queue location/lease; validate event, lease, attempt, receipt, and epoch constraints; reconcile external ambiguity. | PostgreSQL work authority. Compatibility workers only drain mapped legacy work. Exit after the full drain rule in §22.6. |
| Catalog | Legacy Course/Section/Lesson columns; temporary journal/repeated fingerprint convergence; stable IDs map to non-public Draft graphs. | Final authoring delta; owner/disposition resolution; same-Course pointer, graph, ordering, Draft, taxonomy, and ownership constraints. | Revision graph authority. Legacy columns become read-only projections. Contract waits for post-migration review of content intended for publication and dependent adapter release. |
| Media | Legacy `videos`, keys, status, `sync_version`, Lesson status/duration, and object identity; external verification workers converge row/object deltas. | Fence upload/replacement/retranscode/publish; settle/map work; final identity recheck; promote exact owner/version/object/attempt/state constraints. | Media Asset/Version authority. Old endpoints call Media. Unverified content stays quarantined; old objects remain until relocation/retention exit evidence. |
| Learning | Legacy `progress.updated_at`/fingerprint plus temporary journal; authentic rows map to Enrollment/Progress. | Final Progress protocol in §24.2; Student/Course/Lesson identity, uniqueness, bounds, monotonicity, and evidence constraints. | Learning authority. No writes return to `progress`; explicitly disposed evidence-only anomalies may remain quarantined. |
| Commerce + Entitlements | No authentic legacy commerce; `fake_entitlements` is disposable test state, not shadow input. | Prove no fake authorization path; validate money/scope/provider/Coupon/Order/guard/grant constraints; run deterministic non-production access smoke tests. | Coordinated Order-completion/Entitlement authority. Fake access is permanently disabled and later removed. |
| Operational + Reporting | No authentic legacy history. Consume only authentic post-fence domain events; migration evidence triggers no business side effects. | Notification, Audit, report/version, Office Hours, ledger/Statement, payout, hold/deletion constraints; unresolved gates remain configuration blockers. | New owners become authoritative when enabled. Ledger is authoritative; projections remain rebuildable and never drive access/money. |

## 24. Recovery, Reconciliation, Observation, and Contract Exit

### 24.1 Recovery boundaries

| Boundary | Permitted recovery |
|---|---|
| Before context fence | Roll application back to legacy authority; retain/rebuild shadow rows idempotently. |
| After fence, before epoch switch | Abort and resume legacy after reconciliation. Rebuildable shadow rows may be retained, marked abandoned, or rebuilt. External objects/scans/emails/provider requests/completed jobs are reconciled, reused, compensated, or cleaned up with preserved evidence—never assumed absent. |
| After epoch switch | Database authority never rolls back; legacy writes never resume. Application rollback is allowed only to a new-authority-compatible version. Repair is forward-only. |
| After Contract | Restore tested new-authority backups or repair forward; legacy structures are not recovery mechanisms. |

An aborted attempt preserves fence, operation IDs, external effects, cleanup/disposition, and
immutable events. A pre-switch backup is disaster-recovery evidence, not permission to restore
legacy authority after a switch.

### 24.2 Learning fence and buffered checkpoints

During the brief fence, new playback authorizations pause. Existing playback continues only when
the client durably buffers rejected checkpoints using the same idempotency identity. Resubmission
requires a short configured deadline, signed/server-issued playback Session, sequence/checkpoint
nonce, and original observation time within that authorization window.

The new command validates Student, Enrollment, stable Lesson, exact Media Version where available,
trusted duration, monotonicity, and plausible advancement from server-known Session evidence.
Current access is required for new playback/new observations, but an authentic bounded checkpoint
observed before natural Entitlement expiry may be accepted idempotently afterward. Checkpoints
observed after cancellation, emergency suspension, Account suspension, or authorization expiry are
rejected. A client-supplied “buffered” flag grants no trust.

### 24.3 Reconciliation cardinalities and dispositions

For every source type:

```text
converted source records + explicitly disposed source records = inventoried source records
```

- Identity: the current `0001_init` authentic Account/credential/Session inventory is exactly zero.
  If a transitional source exists before cutover, every authentic Account maps once or has
  disposition, each compatible credential maps at most once, email/role ambiguity affecting
  authorization is zero, and Sessions are mapped or revoked.
- Work: every pre-fence operation is terminal/cancelled/mapped once; every queue location/lease is
  reconciled; no ambiguous external side effect remains.
- Catalog: every Course maps to one stable identity/Draft or one approved disposition; every
  retained Section/Lesson maps once or has linked disposition. An excluded Course's descendants
  receive explicit linked dispositions; no excluded record remains referenced.
- Media: every legacy Video maps to its same-ID Asset plus distinct Version or disposition. `READY`
  evidence is complete; missing/inconsistent objects remain visible as review outcomes.
- Learning: each semantically valid row maps to one Enrollment/Progress; invalid/ambiguous rows have
  dispositions; source total equals converted plus disposed.
- Commerce/Entitlements: legacy commercial conversion count is exactly zero; fake authorization
  references are zero; fixture activity is deterministic and non-production.
- Operational/Reporting: migration conversion produces zero business Notifications, moderation,
  publication, earning, payout, or receipt records.

Allowed dispositions are `CONVERTED_VERIFIED`, `EXCLUDED_INVALID`, `QUARANTINED`,
`EVIDENCE_ONLY`, `DISPOSABLE_DEV_FIXTURE`, and `MANUALLY_REMAPPED`. Later dispositions may supersede
earlier ones without rewriting them—for example
`QUARANTINED → MANUALLY_REMAPPED → CONVERTED_VERIFIED`.

No unresolved item may affect authentication, authorization, current visibility, Media safety,
commercial access, or authoritative Progress. Noncritical historical anomalies may remain
quarantined only with explicit approved disposition.

### 24.4 Predeclared observation periods

Before observation begins, each context records duration, smoke suite, queue thresholds, divergence
tolerances, alert criteria, start time, and responsible approver. A material change invalidates the
run and starts a new recorded period.

- **Context observation:** starts after that context epoch switch and constraint promotion.
- **Final integrated observation:** starts after all required contexts are authoritative and
  cross-context workflows operate together.

Success requires no accepted legacy write, old-epoch commit, unexplained divergence, duplicate
external/financial effect, critical unresolved review item, failed core workflow, unreconciled
backlog, or rollback dependency on legacy authority. Contract removal waits for dependent contexts
whose workflow still needs the adapter.

### 24.5 Recovery and contract evidence

A production restore after cutover uses a backup containing new authority, then reconciles provider
and object events after its recovery point. Restoring an older checkpoint directly could lose
accepted transactions, duplicate effects, reuse operation IDs, or conflict with providers and is
prohibited.

Before Contract, an isolated restored populated database must prove the full chain from `0001_init`,
source/target cardinality, exact Media resolution, PostgreSQL-to-Redis reconstruction, and absence
of legacy-authority dependency. The destructive operation itself is rehearsed with expected
runtime, lock scope, statement timeout, failure behavior, disk impact, and post-change
reconciliation.

Every contract approval contains:

- completed Batch, final/fenced high-water mark, journal range, reconciliation counts/fingerprints;
- validated constraints/indexes/required columns and current epoch;
- zero critical unresolved items plus all disposition evidence;
- completed context/integrated observation reports;
- legacy route/write/enqueue rejection and full queue-drain evidence;
- external-effect, object relocation/deletion, and provider reconciliation evidence;
- backup ID/LSN, schema/app versions, epochs, outbox boundary, lease/queue and object manifests;
- isolated restore plus destructive-operation rehearsal;
- proof supported rollback versions use new authority;
- migration checksum/code version and authorized approvals.

Only then may later forward migrations remove compatibility adapters, legacy columns/tables,
workers, handlers, or obsolete objects. Object relocation requires size/checksum equality,
immutable provider identity, reference cutover, and observation; deletion then follows §19 and
cannot treat a versioned-storage delete marker as permanent removal.

## 25. Final Module Ownership and Integration Matrix

| Owner | Authoritative records | Approved external use; prohibited substitution |
|---|---|---|
| Identity | Accounts, credentials, Sessions/tokens, staff invitations, profiles, acceptances | Query Account identity, immutable role/status, and eligibility to act. Course ownership is Catalog state. No direct credential/Account mutation. |
| Catalog | Taxonomy, stable Course/Section/Lesson identities, revision graph, prices, retirement/delisting, emergency Course access suspension | Resolve snapshots, ancestry, owner, approved/history graph, and suspension. Moderation/security/legal may invoke Catalog suspension; they never set it directly. |
| Media | Assets/Versions, upload/object identity, scan/processing Attempts, renditions, delivery evidence | Query exact technical readiness. Delivery accepts a trusted authorization decision/claim; Media verifies Version/rendition and signs delivery but never infers Student access. |
| Commerce | Orders/Items, Coupons/Redemptions, Student Payment Attempts, Refund/Dispute evidence, reconciliation, commercial documents | Coordinate verified paid/free completion; never grant access, alter Catalog price, or write financial ledger directly. |
| Entitlements | Source-Order Entitlements, expiry/adjustment/revocation, access guards | Other modules invoke source-validated grant, adjustment, or revocation commands through an approved coordinator. |
| Learning | Enrollment, Progress/completion, roster/learning analytics | Other modules invoke Learning's create-or-reuse Enrollment command during valid grant/authentic migration. Only Learning writes Enrollment/Progress. |
| Moderation | exact-version Reports, review assignment, immutable decisions/action references | Invoke owner commands; never update Catalog, Media, Identity, or Office Hours directly. |
| Office Hours | stable Sessions/Versions, cancellation/reschedule/delivery evidence | Use Identity/Catalog/Entitlement authorization contracts; never grant Course access or infer attendance from time. |
| Notifications | Events, relational Recipients, delivery Attempts/evidence | Source transaction fixes Account, channel, purpose, template/version, and destination evidence. Failure never rolls back source state. |
| Reporting/Payouts | revenue-share versions, immutable ledger/reconciliation, Statements/items/events, `payout_transfer_attempts` | Consume immutable completion-time earning snapshots; never query current owner/latest share to recalculate history. |
| Audit | append-only privileged-action evidence | Participate through an Audit command/transaction-aware repository. Other modules never insert directly or use Audit as workflow authority. |
| Retention/Privacy | policy versions, holds/scope guards, actions, data-subject requests | Invoke approved owner anonymization/deletion commands; never bypass holds or delete owner tables directly. |
| Shared Work | idempotency, outbox/attempts/receipts, scheduled work | Carry stable intent/deduplication; never replace domain Attempt/history state or business authority. |
| Migration Control | permanent batches/mappings/reviews/dispositions/cutovers/reconciliation/contract evidence | Coordinate conversion/cutover. Active alternative business authority ends; permanent authority/history evidence remains. |

### 25.1 Coordinator and cross-module transaction rule

An application coordinator may invoke transaction-aware commands exposed by multiple owners inside
one shared PostgreSQL transaction only for an explicitly approved cross-module invariant. It owns
orchestration, not records, and never mutates module tables directly. Each owner validates its own
invariants, locks its rows, returns typed results, writes its records, and contributes Audit/outbox
through approved contracts.

Applicable locks follow this common order:

```text
Identity/reference
→ Catalog snapshot/guard
→ Commerce Order/Payment/Coupon
→ Entitlement access guard
→ Learning Enrollment
→ Reporting share configuration / earning snapshot
→ Moderation / Office Hours / Retention / Migration owner records
→ Audit
→ Outbox
```

Parent/guard rows precede children; multiple same-type rows lock in stable UUID order. A Moderation
flow acquires the affected Identity/Catalog owner locks before the Report lock, never the reverse.
Audit/outbox append after domain validation and introduce no reverse dependency. External provider,
storage, email, Redis, or bank operations never run inside this transaction; only verified evidence
or durable intent commits.

Catalog owns suspension state/reason reference; Audit owns the privileged decision evidence;
Notifications receives intent; Entitlements remain unchanged. Runtime authorization combines
Catalog suspension with Entitlement coverage.

### 25.2 Cross-module read rules

Allowed:

- explicit owner query/command contracts;
- cross-schema foreign keys/integrity constraints required for identity/provenance;
- named read-only operational/reporting projections with one declared owner.

Prohibited:

- undocumented cross-module joins as business authorization;
- reading owner-internal tables instead of a contract;
- projections driving payment, access, Refund, or another authoritative decision;
- writes through projections/views.

A foreign key never transfers record ownership. The referenced owner alone mutates the referenced
business state.

Notification in-app identity remains `account_id`. Mandatory email Attempts preserve the exact
destination used; later Account email changes never rewrite delivery history. Optional marketing
preferences may suppress only optional marketing—not receipts, Refunds, Entitlement changes,
security alerts, or other mandatory notices.

## 26. Critical Transition Matrix

| Transition | Preconditions, locks, and atomic PostgreSQL result | External/asynchronous boundary |
|---|---|---|
| Staff invitation acceptance | Identity locks Invitation/token and normalized email; creates one Account/credential/profile/acceptance plus Audit/outbox. | Email is independent; replay creates nothing. |
| Course submission/approval | Catalog validates/freezes complete graph. Approval revalidates exact `READY` Media Versions and changes pointers with immutable decision, Audit, outbox. | Media processing is external; partial publication is impossible. |
| Upload acceptance | Verify provider object identity, size, type, checksum evidence first. Media locks Intent/Version, completes Intent, moves Version to `QUARANTINED`, records evidence/Audit, emits scan/processing work. | Storage verification is external; acceptance never moves directly to `READY`. |
| Readiness promotion | Idempotent successful scan/processing result locks exact Version/Attempt, validates every required Attempt, reconfirms immutable provider identity, moves Version to `READY`, writes Audit/outbox. | Ambiguous/missing evidence stays quarantined/failed/reviewed. |
| Paid Order acceptance | Account exists with immutable Student role, is active/eligible/not checkout-suspended; Catalog graph/price/future expiry/retirement valid; no conflicting live Order/Entitlement; Coupon reservation available. Create immutable Order Item/reservation plus Audit/outbox. | Provider checkout session is created only after commit. |
| Verified paid completion | Verify provider capture first; lock Order/Attempt/Coupon, Entitlement guard, Enrollment; complete Order, consume Redemption, create source-linked Entitlement, create/reuse Enrollment, write Audit and immutable earning/receipt/notification intents. | Duplicate/conflict/late capture enters reconciliation without duplicate access. |
| Earning snapshot/outbox intent | Reporting locks/validates the effective share configuration and returns the completion-time Course owner, revenue-share version, commissionable base, discount-funding treatment, currency, Order Item, and stable earning-operation ID. The source transaction persists it in the stable reporting outbox event; no separate `earning_intents` table exists. | Reporting consumes the authoritative pending snapshot idempotently into the ledger; it never queries current owner/share for history. |
| Confirmed Refund | Verify provider result; update Refund/Order; on cumulative full success revoke only source Entitlement, release Student Coupon eligibility without decrementing historical consumption, preserve Enrollment/Progress, emit original-Instructor negative adjustment, Audit/notice. | Later ledger entry links original earning/Instructor. |
| Verified dispute/chargeback | Commerce records/deduplicates provider evidence and dispute lifecycle/fees, then invokes only configured approved access/financial consequences. | Not an Admin Refund. Until `LG-016`/`LG-017`, automatic revocation/recovery stays blocked or reconciliation-driven. |
| Progress checkpoint | Trusted authorization decision; Learning validates Enrollment/Lesson/Media evidence/duration/bounds and performs monotonic CAS. | Retry cannot reduce Progress or fabricate completion. |
| Moderation resolution | Acquire affected owner locks before Report. Owner action and immutable Moderation resolution commit together with Audit/outbox; owner failure leaves Report unresolved. | No automatic action from report/finding. |
| Office Hours mutation | Identity/Catalog authorization, Session CAS, immutable Version/event, exact relational Notification audience, Audit/outbox. | Meeting/email remain external. |
| Statement approval | `READY_FOR_REVIEW → APPROVED`; Reporting freezes ledger items/totals, snapshots approved destination, records approver/Audit. | No transfer yet. |
| Transfer initiation | `APPROVED → PAYMENT_PENDING`; create immutable `payout_transfer_attempt`, commit transfer outbox. | Bank transfer occurs externally. |
| Transfer confirmation | Verify external evidence; resolve exact Attempt; record bank reference, amount/currency, payer/time/document; atomically mark `PAID`. | Ambiguous remains pending/reconciliation; retry reconciles provider first. |
| Manual work retry | Lock exhausted work, check epoch, reconcile ambiguous effect, append Attempt/Audit. | Prior evidence is never reset. |
| Retention deletion | Lock scope/policy/holds; record exact pending action/outbox. | Delete/verify exact object externally; second transaction confirms. |
| Authority cutover | Fence, final delta, constraint promotion, reconciliation/approval/Audit; atomically increment epoch. | Old API/worker commits fail epoch guard. |

## 27. Database Protection and Index Matrix

Protection mechanisms are explicit: `FK`, `CHECK`, `UNIQUE/PARTIAL UNIQUE`, `EXCLUSION`,
immutable-column trigger/restricted permission, serialized guard-row transaction, compare-and-swap
(`CAS`), deferred constraint trigger, and database-role permission.

| Risk/invariant | Protection class and supporting access path |
|---|---|
| Account identity/role | `UNIQUE(normalized_email)`; role/status `CHECK`; immutable-role trigger/restricted update permission; one credential `UNIQUE`; status/session indexes. |
| Course graph | Same-Course composite `FK`; one Draft `PARTIAL UNIQUE`; position `UNIQUE`; ancestry `FK/CHECK`; frozen-row restricted update; pointer-swap `CAS`. |
| Catalog safety | Retirement/suspension source rows with `FK/CHECK/CAS`; checkout/current-graph indexes; authorization remains owner-contract logic. |
| Media identity/readiness | owner/kind `FK/CHECK`; object identity `UNIQUE`; immutable-column trigger/permission; attempt-result `FK`; readiness guarded command plus `CHECK`; Lesson-video scope `UNIQUE`. |
| Order money/scope | integer/currency/amount `CHECK`; exclusive target `CHECK/FK`; one Item `UNIQUE`; payable completeness via deferred constraint trigger; immutable snapshot permission. |
| Payment/Refund | provider event/reference `UNIQUE`; active Attempt `PARTIAL UNIQUE`; cumulative Refund bound via Order lock plus aggregate validation—not row `CHECK`. |
| Coupon | normalized code `UNIQUE`; Redemption Order `UNIQUE`; active Student use `PARTIAL UNIQUE`; capacity via serialized Coupon lock. No targets means platform-wide; otherwise relational Course/Section target rows may coexist and follow a guarded target-set command, with child `FK`/`UNIQUE` protection. There is no one-target-type constraint. |
| Entitlement overlap | source Order `UNIQUE`; target `CHECK/FK`; cross-scope overlap via serialized Student/Course guard; expiry adjustment `CAS`; lookup indexes. |
| Learning | Enrollment Student/Course `UNIQUE`; Progress Enrollment/Lesson `UNIQUE`; bounds `CHECK`; monotonicity through CAS SQL/database function and trusted duration. |
| Moderation | At most one unresolved report per reporter + exact target + exact reported Version using applicable `PARTIAL UNIQUE`; system findings use separate source uniqueness; queue/target indexes. |
| Office Hours | current Version `FK`; immutable Version permission; Session mutation `CAS`; upcoming/history indexes. |
| Notifications | `UNIQUE(notification_event_id, account_id, channel)`; immutable destination Attempt evidence; unread/retry indexes. |
| Ledger/Statements | source event `UNIQUE`; append-only role/permission or trigger; Statement Instructor/currency/period `UNIQUE`; Statement Item `UNIQUE(ledger_entry_id)`; later adjustments are new lines; immutable transfer Attempt IDs. |
| Audit | append-only database-role permission/trigger; actor/target/action/correlation indexes; owner modules use Audit contract. |
| Work | stable operation/event `UNIQUE`; `UNIQUE(operation_id, consumer_name)` receipts; due/lease/exhaustion indexes; transactional epoch guard. |
| Migration | one active context Batch `PARTIAL UNIQUE` plus advisory lock/lease; typed mapping `UNIQUE`; disposition/event append-only; cutover context/epoch `UNIQUE`; batch `CAS`. |

## 28. Retention Boundary Matrix

Exact periods/actions remain blocked on `LG-003`/`LG-004`; destructive jobs remain disabled until an
approved policy exists.

| Data class | Boundary while unresolved and under future approved policy |
|---|---|
| Credentials/tokens/Sessions | Operational expiry/revocation denies use. Later deletion/minimization follows Identity policy while required security evidence remains. |
| Account/profile | Deactivation preserves stable surrogate identity required by Orders, Courses, Entitlements, Learning, and Audit. Approved anonymization removes eligible identifiers without breaking provenance. |
| Catalog revisions/reviews | Preserve submitted/published/history-bearing records; only already-allowed unreferenced Draft deletion. Policy cannot rewrite approval/ownership evidence. |
| Media/objects | Preserve exact Versions, quarantine, and verification evidence. Exact-version deletion requires policy/hold serialization, provider verification, tombstone, Audit; delete marker is insufficient. |
| Commerce/provider evidence | Preserve immutable Orders, Payment/Refund/Dispute evidence, documents, and policy snapshots; approved anonymization retains financial provenance. |
| Entitlements/Learning | Preserve access origin, Enrollment, and Progress after expiry/revocation; anonymization keeps stable relationships. |
| Moderation/Office Hours | Preserve exact reported/Session Versions, decisions/actions, cancellation/delivery history; no deletion that erases action evidence. |
| Notifications | Payloads may be minimized, but mandatory evidence remains sufficient for recipient, purpose, destination used, Attempt, and result. |
| Financial ledger/Statements/payout/Audit | Append-only; never rewritten or hard-deleted. Policy may restrict access, archive, minimize separable payloads, or anonymize eligible references without destroying financial/action provenance. |
| Outbox/provider Attempts | Payload may be removed only after stable source references and `consumer_event_receipts` remain sufficient for reconciliation. |
| Migration evidence | Preserve batches, mappings, cutovers, dispositions, reconciliations, and approvals after adapters disappear; later archival/minimization must retain explainable authority history. |
| Backups | Follow approved copy envelope and holds; expire through version-aware backup process, never an immediate-deletion claim. |

## 29. Sections 5–6 Approval Record

The developer/product owner approved:

- preservation of stable Catalog UUIDs, authentic Media/object evidence, compatible Account identity,
  and semantically valid Learning state without fabricating commercial/legal history;
- `videos.id → media_assets.id` with a distinct mapped initial Asset Version;
- resumable delta-convergent typed mappings, one active Batch/partition lease, review disposition
  history, and transactional trusted authority epochs;
- context order Identity → Work Delivery → Catalog → Media → Learning → coordinated
  Commerce/Entitlements → Operational/Reporting;
- observation-time-valid buffered Progress, full legacy queue/side-effect reconciliation, and
  explicit constraint promotion before each authority switch;
- forward-only post-switch repair, predeclared context/integrated observation, new-authority
  recovery evidence, and rehearsed destructive Contract migrations;
- one owner per authoritative record, owner-defined contracts, constrained coordinators, global
  lock order, and no external operations inside state transactions;
- split Media acceptance/readiness and payout approval/initiation/confirmation transitions;
- distinct Refund versus dispute consequences, relational Coupon targets, exact consumer/report/
  Statement uniqueness, and classified database protections;
- permanent append-only financial/Audit provenance and configurable fail-closed retention.

The complete owner-approved design passed documentation self-review and proceeded to the
independent review recorded below.

## 30. Independent Review Record

Claude Code 2.1.218 independently reviewed exact commit `5ba126c` read-only against the canonical
rules, platform architecture, launch gates, and current schema/code seams. It confirmed no tracked
worktree modifications during review and returned:

- Critical: 0;
- High: 0;
- Medium: 1;
- Low: 4;
- verdict: **APPROVE DOMAIN DESIGN**.

All advisory precision findings are incorporated:

- the stable reporting outbox event is explicitly the authoritative pending earning-snapshot
  carrier; no unowned `earning_intents` record is implied;
- the Order lifecycle shows direct full Refund from `PAID`;
- Identity migration is explicitly greenfield under current `0001_init`, with compatibility rules
  conditional on an authentic transitional source;
- uncapped Coupon capacity uses `max_redemptions IS NULL`;
- paid versus `FREE_GRANTED` Order deadline nullability is explicit.
