# Gradex Domain, Data, and State Design

> Status: In progress — conversational Sections 1–2 approved and locked
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
- Later sections will add Commerce/Entitlements/Learning and the remaining operational modules.

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

A retired Course, Section, Lesson, or authored version remains accessible only through an
otherwise-active Entitlement whose effective commercial grant event predates `retired_at`. The
effective event is verified payment success or the zero-value/manual-grant event—not a delayed
webhook or row-insertion time.

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

The Course presentation lifecycle remains the canonical
`DRAFT/PENDING_REVIEW/CHANGES_REQUESTED/PUBLISHED/UNPUBLISHED/ARCHIVED` model. A Published Course
remains Published while another revision is in review.

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
uses a Coupon/manual grant, not a zero catalog price.

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

## 8. Section 1–2 Approval Record

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

Sections covering Commerce, Coupons, Entitlements, Learning, Moderation, Office Hours,
Notifications, Reporting/Payouts, Audit, retention, transactions, indexes, and migration sequencing
remain in progress.
