# Bundles V1 and Catalog Offers V1 Design

**Date:** 2026-09-09

**Base:** `ef733f758cf7d58e2689898a70715e2fd2102e24`

**Base tree:** `eabc437f7a93a86eae995e5f64c3fe3d51c64b0d`

**Expected schema:** `0035` -> `0036_bundles_and_offers`

**Review:** PENDING / UNASSIGNED

## 1. Scope and fixed product decisions

Bundles V1 adds an Admin-owned, bilingual catalogue resource containing an ordered set of at least
two published Courses. A published and currently eligible Bundle can receive a manual purchase
request. Admin confirmation of external payment directly and atomically grants normal Course access
for every Course in the request snapshot.

Catalog Offers V1 adds an optional Admin-managed offer price to Course and Bundle catalogue pricing.
It is not a coupon-code system. There is no code entry, redemption, scheduling, campaign, usage
limit, per-user discount, cart, gateway, refund, or online-payment behavior.

The following fulfillment split is intentional:

- An individual Course purchase keeps the existing flow unchanged: authenticated Student request,
  external payment, Admin payment confirmation, Course Access Invitation, Student acceptance,
  enrollment and entitlement, then `ACCESS_GRANTED`.
- A Bundle purchase uses direct fulfillment: authenticated Student request, external payment, then
  one Admin confirmation transaction that grants all snapshot Courses and moves the request directly
  from `WAITING_PAYMENT` to `ACCESS_GRANTED`.
- A Bundle never creates a Course Access Invitation, and it never creates one invitation per member.
- Future unification of Course and Bundle fulfillment is deferred.

Money remains outside Gradex. No payment instrument, provider transaction, gateway, webhook, stored
card, invoice, wallet, or refund record is introduced.

## 2. Existing domain reconstructed

### 2.1 Individual Course purchase

`POST /api/v1/me/purchase-requests` currently accepts only `course_id`. Session authentication,
session mutation protection, `LEARNING_ACCESS`, and the authenticated purchase rate policy guard the
route. `access.Repository.CreateStudentPurchaseRequest` locks the Account, revalidates an active and
verified Student, checks current access, and inserts or reuses the request in one transaction.

The insert reads the Course's current live published revision and latest Course-level
`course_price_changes` row in one statement. It snapshots the Course identity, Arabic and English
titles, integer price in fils, and `KWD`. Partial unique indexes enforce one active request per
Student/Course and per normalized email/Course. The request begins in `WAITING_PAYMENT`.

`POST /api/v1/admin/purchase-requests/:id/confirm-payment` is protected by session mutation security
and the Admin-only `COURSE_ACCESS_GRANT` capability. The transaction locks the request, revalidates
the Course, snapshots its configured access expiry, creates a `PENDING_STUDENT_ACCEPTANCE` Course
Access Invitation and action secret, writes audit evidence and an outbox email, then moves the
request to `INVITATION_CREATED`. Replays return the already-linked invitation.

The matching Student accepts through
`POST /api/v1/me/course-access-invitations/:id/accept`. The acceptance transaction verifies the
Student/email/secret relationship, creates or reuses `enrollments`, creates the Course entitlement,
approves the invitation, changes the request to `ACCESS_GRANTED`, writes audit evidence, and appends
the access-granted outbox event.

Bundles V1 must not alter this sequence or its existing response semantics.

### 2.2 Current Course pricing authority

Course regular price belongs to neither `courses` nor `course_revisions`. It is the newest
Course-level row in append-only `course_price_changes`, identified by `section_id IS NULL` and ordered
by `changed_at DESC, id DESC`. Values are `BIGINT` KWD fils. Admin writes it through
`PUT /api/v1/admin/courses/:id/price`, guarded by `CATALOG_PRICING`; Instructor and Student lack that
capability. Each mutation requires a reason and writes `COURSE_PRICE_CHANGED` audit evidence.

Publication requires a Course-level price row. The current rule permits zero. Public list/detail and
Course purchase creation read the same latest record. Section price history exists but is not a
purchasable catalogue authority.

Catalog Offers V1 extends this existing Course price stream; it does not create a second regular
Course price source.

### 2.3 Current learning authority

`enrollments` contains at most one row per Student/Course. `entitlements` remains the authorization
record, with Course or Section scope, Course identity, expiry, state, and provenance. A partial unique
index permits at most one state-`ACTIVE` Course entitlement per Student/Course.

Protected learning resolves a Lesson's Course identity and evaluates applicable entitlements by
Student and Course. A later published Course revision keeps the same Course identity. Bundle grants
therefore produce normal Course enrollments and Course-scoped entitlements; protected-learning code
will not read Bundle membership or understand a Bundle scope.

## 3. Domain boundaries

Add a focused `catalog` Bundle repository/service surface beside current Course catalogue behavior,
and extend `access` purchase handling with an explicit target discriminator. Shared domain pricing
helpers return regular, optional offer, and effective values. Public read models expose Bundles as
Bundles rather than coercing them into Course records.

Purchase confirmation dispatches once, after locking the request:

- Course target: call the existing invitation-producing path without semantic changes.
- Bundle target: call the new direct atomic Bundle grant path.

The target-specific behavior is owned by the backend domain, not frontend branching.

## 4. Schema 0036

Use one additive migration pair named:

- `backend/internal/db/migrations/0036_bundles_and_offers.up.sql`
- `backend/internal/db/migrations/0036_bundles_and_offers.down.sql`

### 4.1 Course offers

Extend `course_price_changes` with nullable offer history fields:

- `old_offer_price_minor_units BIGINT NULL`
- `offer_price_minor_units BIGINT NULL`

Constraints enforce positive offer values and `offer_price_minor_units < new_value_minor_units` when
an offer is present. Every existing row remains unchanged with both new columns `NULL`; its
`new_value_minor_units` keeps exactly the same commercial value.

The latest Course-level row becomes the single Course catalogue price record: regular is
`new_value_minor_units`, offer is `offer_price_minor_units`, and effective is offer when present,
otherwise regular. An explicit clear writes a new history row with a `NULL` offer.

### 4.2 Bundle aggregate

Create `bundles` with:

- UUID primary key and server-generated immutable `bundle-<uuid-without-hyphens>` slug
- Arabic and English title and description
- lifecycle constrained to `DRAFT`, `PUBLISHED`, `DELISTED`, or `ARCHIVED`
- positive revision counter used for optimistic mutation and purchase snapshot provenance
- creator/updater Admin foreign keys and created/updated timestamps

Create `bundle_courses` with `bundle_id`, `course_id`, and nonnegative `position`. Primary/unique
constraints prohibit duplicate membership and duplicate positions within a Bundle. Foreign keys use
Course identity, not revisions. Membership rows are ordered by `position`, then Course ID as a stable
tie-breaker.

Create append-only `bundle_price_changes` using the Course price-history vocabulary: old/new regular
fils, old/new optional offer fils, Admin actor, reason, and timestamp. Constraints enforce
nonnegative regular price, positive offer price, and offer below regular price. Bundle price and
membership mutations lock the Bundle row and increment its revision in the same transaction.

### 4.3 Purchase target and quote

Extend `purchase_requests` without invalidating historical rows:

- `target_kind TEXT NOT NULL DEFAULT 'COURSE'`
- make existing `course_id` nullable
- `bundle_id UUID NULL REFERENCES bundles(id)`
- `bundle_revision BIGINT NULL`
- `bundle_title_ar TEXT NULL`
- `bundle_title_en TEXT NULL`
- `regular_price_minor_units BIGINT NULL`

An exact-one check requires a Course target to have only `course_id` and a Bundle target to have only
`bundle_id`. Existing rows become Course targets through the default and retain all existing values.
Their new regular-price snapshot remains `NULL`, because no historical data needs to be rewritten or
invented. New Course and Bundle requests persist regular price; existing `price_minor_units` remains
the effective quoted amount; `currency` remains `KWD`.

Course invitation-coherence constraints continue to require invitation fields for Course targets.
For Bundle targets, the legal state transition is `WAITING_PAYMENT` directly to `ACCESS_GRANTED`,
with payment confirmer/time and access-granted time present and all invitation fields absent.

Create target-specific partial unique indexes for active Bundle requests by requester and normalized
email. Existing Course indexes remain in force.

### 4.4 Immutable Bundle purchase snapshot

Create `purchase_request_bundle_items` with:

- `purchase_request_id`
- `course_id`
- `position`

Its primary/unique constraints prohibit duplicate Courses and positions per request. A foreign-key or
trigger check ensures rows belong only to a Bundle-target Purchase Request. An immutability trigger
rejects update or delete after insertion. Purchase cancellation and Bundle edits never mutate these
rows.

Create `bundle_purchase_grants` with one row per Purchase Request/Course result:

- Purchase Request, Bundle, Course, and Entitlement identities
- disposition constrained to `GRANTED`, `PRESERVED`, or `EXTENDED`
- prior and resulting access expiry where applicable
- grant timestamp

This table is fulfillment provenance, not learning authority. It records why a Course was granted or
preserved without making protected learning bundle-aware.

Extend `entitlements` with nullable `source_purchase_request_id` and add `BUNDLE_PURCHASE` to the
grant-source constraint. A `BUNDLE_PURCHASE` entitlement requires the Bundle Purchase Request
reference and forbids an invitation reference. Existing `MANUAL_INVITATION` and Course
`PURCHASE_REQUEST` rows retain their present invitation requirements and values.

### 4.5 Rollback posture

The down migration may remove 0036 only when no Bundle, Bundle purchase, Bundle grant, or Course offer
data exists. It must fail closed instead of destroying commerce history. Clean-schema up/down/up
remains testable. Deployment scripting and migration rollout tooling are explicitly outside this
feature; the release handoff will state that schema 0036 needs separate release-tooling support.

## 5. Bundle lifecycle and eligibility

Only Admin can create or mutate Bundles. A draft may be incomplete while being edited. Publishing
requires:

- bilingual nonblank title and description;
- at least two distinct ordered Course identities;
- a Bundle regular-price record;
- every member currently satisfying the existing `catalogpublic.PublishedOnly` Course predicate.

A public Bundle is eligible only while it is `PUBLISHED`, has at least two members and a price, and
every current member remains publicly eligible. Public list/detail and purchase creation apply this
predicate in SQL. If a member becomes delisted, archived, retired, or access-suspended, the Bundle
remains stored but disappears from public discovery and cannot receive a new request. Admin reads
show the failing members and reason.

`DELISTED` is the reversible hide/unpublish state, matching Course lifecycle vocabulary. `ARCHIVED`
is terminal for new purchase. Historical requests, snapshots, grants, enrollments, and entitlements
are never deleted or revoked by later Bundle edits or lifecycle changes.

Adding or removing members affects only future requests. There is no retroactive grant or revocation.
Every membership write accepts only Courses satisfying the existing public Course predicate at that
moment; a draft may omit members while incomplete, but it cannot store an ineligible selected Course.

## 6. Pricing rules

All money uses signed 64-bit integer KWD fils; JSON carries integer minor units. No float participates
in persistence, comparison, quote creation, or formatting.

One domain calculation applies to Course and Bundle read models and request creation:

`effective = offer when offer is present; otherwise regular`

An offer must be greater than zero and strictly less than regular. Equal, zero, negative, fractional,
overflowing, or nonnumeric values fail validation. V1 has no time window; Admin sets or clears an
offer explicitly.

Bundle price is Admin-authored and never derived from member prices. Existing ownership of some
members does not reduce the quote.

Course purchase creation will snapshot both new regular and effective values. Its invitation-based
fulfillment behavior remains unchanged. Bundle purchase creation snapshots Bundle revision, titles,
regular price, effective price, currency, and ordered member Course IDs under one coherent lock.

The existing Course pricing endpoint remains compatible: omission of the new offer field preserves
the current offer when it remains valid; an explicit JSON `null` clears it; an integer sets it. A
regular-price change that would make a preserved offer invalid is rejected unless that same request
changes or clears the offer. New UI calls always send the intended offer state explicitly.

## 7. Concurrency and transactions

Every Bundle mutation takes a `FOR UPDATE` lock on the Bundle row before changing metadata,
membership, lifecycle, or price. Bundle purchase creation takes a compatible row lock before reading
Bundle metadata, latest price, and ordered members. It therefore sees the state entirely before or
entirely after a concurrent edit.

Course pricing keeps the Course row as its serialization lock. A Course purchase quote is derived by
one SQL statement; adding the offer fields preserves its single-statement snapshot. Course price
changes and Course publication continue to follow existing Course locking.

Bundle Admin confirmation performs one PostgreSQL transaction:

1. Lock the Purchase Request `FOR UPDATE`.
2. Return the completed result without new side effects when it is already `ACCESS_GRANTED`.
3. Require a Bundle target in `WAITING_PAYMENT`, a valid authenticated Student owner, a complete
   immutable snapshot, and no invitation relationship.
4. Lock snapshot Courses in deterministic Course-ID order and apply existing Course grantability and
   default-access-expiry rules. Current Bundle membership and price are not read.
5. For each snapshot Course, preserve an active unrevoked Course entitlement exactly when its expiry
   is at least the new access expiry. If its expiry is earlier or already elapsed, extend only to the
   later new expiry with the existing entitlement-adjustment audit pattern; never change its original
   expiry, grant origin, or earlier retirement-eligibility instant. Otherwise create/reuse the normal
   enrollment and insert a Course-scoped `BUNDLE_PURCHASE` entitlement.
6. Resolve a concurrent entitlement insertion through the existing unique constraint and re-read it
   as a preserved grant; never duplicate it.
7. Write one `bundle_purchase_grants` provenance row per snapshot Course.
8. Write Bundle-specific audit events containing Bundle/request identifiers, Course identifiers,
   and dispositions but no email or unnecessary PII.
9. Mark the request `ACCESS_GRANTED` with Admin/payment/grant timestamps.
10. Append one Bundle-level access-granted outbox notification.
11. Commit.

Any validation, entitlement, enrollment, provenance, audit, request transition, or outbox failure
rolls back the complete operation. Locking the request makes concurrent Admin confirmations converge
to one logical fulfillment.

The access expiry is not part of the immutable commercial snapshot. At confirmation, each snapshot
Course must still be grantable under the existing Course lifecycle and must have a valid future
default access expiry, matching current Course confirmation semantics. A failed check leaves the
request in `WAITING_PAYMENT`; it never substitutes another Course or rereads current Bundle members.

## 8. Audit and notification

New catalog audit actions will distinguish Bundle creation, metadata/membership replacement, price
change, publication, delisting, archival, and Course offer change. Access audit actions will
distinguish Bundle request creation, external payment confirmation, Bundle access granted, and each
Course disposition. The exact identifiers are finalized with the implementation and added to the
privileged-route audit matrix.

The Bundle confirmation writes one outbox event and one Student delivery, following the existing
transactional email pattern. It does not create invitation secrets and does not send member-level
invitation emails. Email delivery remains asynchronous and outside the request transaction; only the
outbox append co-commits with grants.

## 9. HTTP API

Public routes:

- `GET /api/v1/catalog/bundles`
- `GET /api/v1/catalog/bundles/:idOrSlug`

Both apply server-side Bundle and member eligibility. List responses are bounded/paginated and
contain localized public fields, price, ordered member previews, and no internal snapshots, Student,
Admin, or audit data.

Admin routes:

- `POST /api/v1/admin/bundles`
- `GET /api/v1/admin/bundles`
- `GET /api/v1/admin/bundles/:id`
- `PUT /api/v1/admin/bundles/:id`
- `POST /api/v1/admin/bundles/:id/publish`
- `POST /api/v1/admin/bundles/:id/delist`
- `POST /api/v1/admin/bundles/:id/archive`
- existing `PUT /api/v1/admin/courses/:id/price`, extended with explicit offer set/clear semantics

Bundle mutations require session mutation security and Admin-only catalogue capabilities:
`CATALOG_PUBLISH` for aggregate/lifecycle/membership operations and `CATALOG_PRICING` for price
changes. Instructor and Student requests are denied server-side; `CONTENT_MANAGEMENT` alone is never
sufficient.

Purchase API:

- Existing `POST /api/v1/me/purchase-requests` accepts exactly one of `course_id` or `bundle_id`.
  Existing `{course_id: ...}` callers remain compatible. Price, currency, Bundle revision, and member
  IDs are never accepted as authority.
- Add an authenticated Student read for owned Purchase Requests so pending and completed Bundle
  purchases can be rendered without exposing another Student's rows.
- Existing Admin list and confirmation URLs remain stable. Existing Course response fields and
  Course confirmation semantics remain unchanged; Bundle rows/results add only optional Bundle and
  grant fields, allowing typed clients to discriminate by the exact-one target.

Malformed identifiers and nonpublic targets use existing not-found behavior. Student-owned reads
filter by the authenticated Account in SQL. No Student endpoint accepts another requester identity.

## 10. Frontend

### 10.1 Shared prices

Add a typed `PriceDisplay` component used by landing Course cards, catalogue Course cards, Course
detail, purchase confirmation/state, Bundle cards/detail, and Admin summaries. It formats integer
fils with the existing currency formatter. With an offer it renders labelled regular and offer
prices, using semantic deletion for the regular price plus visible/screen-reader context; without an
offer it renders one price. Missing values render no fabricated `0`, `undefined`, or `NaN`.

### 10.2 Admin

The existing Admin Courses directory has a `PUBLISHED` filter and responsive list layout. Its page
header will expose `Create bundle`; published Course rows will expose `Edit pricing`. These actions
lead to a dedicated bilingual Bundle management workspace and an accessible pricing dialog/page,
instead of requiring Admin to reopen submitted Course review.

Bundle create/edit includes bilingual title and description, keyboard-operable Course search and
selection from public-eligible Courses, ordered selected members with move-up/move-down controls,
count, regular/offer price, draft save, and publish. Labels, descriptions, validation errors, and
status/problem details come from both dictionaries. Bundle list rows show status, count, regular,
offer/effective price, timestamps, failing eligibility, and lifecycle actions.

### 10.3 Public and Student

Landing adds a visually distinct Course Bundles section immediately below Featured Courses. The
catalogue adds a separate Course Bundles section rather than mixing Bundle records into the Course
model. Cards show up to three ordered member thumbnails using the existing Course fallback, Course
count, localized title, authoritative prices, and `View bundle`.

Add `/{locale}/catalog/bundles/{idOrSlug}` for localized Bundle detail. It shows Bundle title,
description, ordered Courses with thumbnail/title/instructor/public academic metadata and links,
prices, Course count, and manual-payment explanation and CTA.

Bundle purchase reuses the existing confirmation-before-WhatsApp interaction, pending/success/error
language, and manual-payment instructions. It identifies the Bundle, quote, Course count, and
snapshot members. After Admin confirmation, normal dashboard reads show the granted Courses; no
Bundle learning page is introduced.

All layouts use logical spacing, existing locale direction, visible focus treatment, labelled form
controls, associated errors, and responsive stacking from a 375px viewport. No thumbnail upload,
Bundle video, media processing, R2, FFmpeg, or playback code changes are allowed.

## 11. Query and performance design

Public Bundle list selects only eligible published Bundles at the database boundary. It loads the
bounded page and all ordered member previews with one aggregate or one batched second query, never a
query per Bundle or per Course. Detail uses a bounded Bundle read plus one ordered member query.
Indexes cover lifecycle/list ordering, Bundle membership order, active Bundle purchase uniqueness,
snapshot ordering, and grant provenance.

Landing requests Course and Bundle lists in parallel. Archived/delisted/invalid Bundles are excluded
in SQL rather than loaded and filtered in application code.

## 12. Verification strategy

Backend unit/integration coverage will exercise Admin-only Bundle and offer mutations, member
eligibility/minimum/duplicates/order, lifecycle/public filtering, price validation/effective price,
Course and Bundle quote snapshots, exact-one targets, snapshot immutability, edit-after-request,
direct atomic Bundle grants, partial ownership, expired-grant extension, idempotency, simultaneous
confirmation, concurrent entitlement creation, rollback on injected failure, IDOR, migration data
preservation, and clean-schema up/down/up.

An explicit regression test will prove an individual Course confirmation still produces
`INVITATION_CREATED`, creates one Course Access Invitation, grants no entitlement before Student
acceptance, and reaches `ACCESS_GRANTED` only after acceptance.

Frontend tests cover normal/offer prices, semantic old-price treatment, cards, sections, detail,
Admin builder and pricing, member selection/order, manual purchase states, loading/empty/API-error
states, and both dictionaries.

A dedicated isolated Playwright suite covers:

- Course offer quote retention after the offer is cleared;
- Bundle creation/public discovery/detail/direct fulfillment/dashboard access;
- immutable A/B/C request followed by A/D Bundle edit;
- preservation of existing A while B/C are granted;
- concurrent Bundle confirmation convergence;
- Arabic RTL, English LTR, keyboard access, accessibility checks, and realistic mobile/desktop
  viewports.

Existing Course authoring/publication, public catalogue, landing, Course detail, manual Course
purchase, invitation approval/acceptance, entitlement, Student learning, D-102 ordering, D-103 media,
authorization, and bilingual regressions remain required.

Scratch mutations M1-M8 from the feature brief must each make its named load-bearing test fail and
must be immediately reverted. Final gates run against the final source without piped exit masking.

## 13. Documentation and release boundary

Implementation updates `docs/BUSINESS_RULES.md`, `docs/DECISIONS.md`, feature documentation, and the
appropriate launch/status record. It will state the intentional two fulfillment mechanisms, manual
payment only, immutable Bundle commercial snapshots, non-retroactive membership, atomic Course
grants, pricing authority, offers rather than coupons, no pro-rata adjustment, archival behavior,
schema 0036, and the separate deployment-migration requirement.

Actual coupon codes and every deferred commerce capability in the brief remain unimplemented.
Deployment scripts, production, existing active worktrees, and remote branches remain untouched.
