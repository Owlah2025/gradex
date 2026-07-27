# Feature Specification: S2 — Course Authoring and Review

**Feature Branch**: `feature/003-course-authoring`

**Created**: 2026-07-28

**Status**: Draft — frozen for D3–D5 implementation under [D-040](../../docs/DECISIONS.md#d-040--august-15-restored-as-the-hard-mvp-launch-date-claude-plans-antigravity-implements-claude-reviews)

**Input**: S2 — Course authoring and review. Instructor creates and edits owned Course/Section/Lesson
structure with resources/labs and preview metadata (references to Asset Versions only, never
uploads); submission for review with completeness and taxonomy validation; Admin review queue with
publish, request-changes, delist/relist, retire, archive, and emergency access suspension/restoration;
private-draft protection; Admin-only Course/Section pricing with full audit history; pending-revision
handling that never mutates the live published graph until approval.

**Depends on**: S1C (staff lifecycle, enforcement, authorization matrix). **S2 implementation does
not begin until S1C closes** on an independent verdict — see
[STATUS.md](../../docs/launch/STATUS.md). This specification may be frozen while that review is open;
freezing a spec commits no behaviour.

**Governing rules**: BR-011 to BR-019, BR-027, BR-061, BR-065, BR-066, BR-067, BR-070 to BR-072,
BR-081, BR-090, BR-091, BR-143, BR-157 to BR-160. Traceability per Constitution Principle III is
carried per requirement below.

---

## Scope Boundaries

Stated first because three of them have already caused ordering mistakes elsewhere in this project.

| S2 owns | S2 must not acquire |
|---|---|
| **References** to an exact Asset Version | Any upload, quarantine, scan, or transcode path — that is S4 ([SLICES.md §3.2](../../docs/launch/SLICES.md#32-authoring-owns-media-metadata-the-media-slice-owns-media-bytes)) |
| Course/Section/Lesson structure and lifecycle | Entitlement evaluation or creation — S4 and S7 |
| Admin-only price *setting* with audit | Order pricing, snapshots, or checkout — S6 |
| Taxonomy *selection* by Instructors and full term administration by Admins | Catalogue browsing, search, or ranking — S3 |
| The authoring and review screens | The public catalogue shell — S3 |

**S2 creates no second authorization decision point.** Role and capability decisions route through
the existing `identity.Authorize` deny-by-default gate over its closed capability set. Where S2 needs
a capability that does not exist, the capability is **added to that closed set**, never checked
beside it. Ownership is a resource-scoped fact rather than a role fact, so it is enforced as an
explicit, uniformly applied precondition on every owned-resource route — and that uniformity must be
mechanically demonstrable, not maintained by hand. See FR-041 and FR-042, and the S1C precedent where
a hand-maintained matrix was a high finding.

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 — An Instructor builds a Course that nobody can see yet (Priority: P1)

An Instructor creates a Course, gives it a title and description in both languages, adds Sections,
adds Lessons inside those Sections, attaches an already-processed video to each Lesson, optionally
attaches resources and lab materials, optionally nominates one preview asset, and classifies the
Course on all three required dimensions. Throughout, the Course is invisible to every Student
regardless of how complete it is.

**Why this priority**: Nothing else in S2 exists without authored content, and private-draft
protection is the security property that makes authoring safe to ship before review exists.

**Independent Test**: Create a Course through the Instructor UI, fill it completely, and confirm by
direct API call as a Student, as a second Instructor, and as an anonymous caller that none of them
can read it. Delivers a usable authoring tool on its own.

**Acceptance Scenarios**:

1. **Given** an authenticated Instructor, **When** they create a Course, **Then** it exists in
   `DRAFT` and is absent from every Student-visible read, however complete its content. *(BR-011)*
2. **Given** a `DRAFT` Course owned by Instructor A, **When** Instructor B requests it by its exact
   identifier through the API, **Then** the response is a denial that does not disclose whether the
   Course exists.
3. **Given** a Lesson, **When** the Instructor attaches a video, **Then** the attachment is a
   reference to an exact existing Asset Version and the request is refused if that version is absent
   or not successfully processed. *(BR-013, BR-091, SLICES §3.2)*
4. **Given** a Course, **When** the Instructor nominates a preview asset, **Then** at most one exists
   and it is stored and authorized separately from protected Lesson content. *(BR-143)*
5. **Given** a Lesson, **When** the Instructor attaches resources or lab materials, **Then** both
   categories are optional, independent, and distinctly typed. *(BR-067)*
6. **Given** a suspended Instructor, **When** they attempt any edit to an owned Course, **Then** it
   is refused, while already-enrolled Students keep access to their Published Courses. *(BR-065)*

---

### User Story 2 — An Instructor submits, and only an Admin can publish (Priority: P1)

The Instructor submits a complete Course for review. The system refuses incomplete submissions and
names precisely what is missing. A submitted Course becomes read-only to its Instructor. An Admin
sees it in a queue, previews its content including video, and either publishes it or requests changes
with a reason.

**Why this priority**: This is the control that stops an Instructor from publishing to the public
catalogue unilaterally. It is a launch-blocking authorization property, not a workflow nicety.

**Independent Test**: Submit an incomplete Course and read the validation; complete it, submit,
attempt an edit as the Instructor, then publish as Admin and request changes on another.

**Acceptance Scenarios**:

1. **Given** a Course with zero Sections, a Section with zero Lessons, or any Lesson without a
   successfully processed video, **When** the Instructor submits it, **Then** submission is refused
   and the response identifies each missing item specifically. *(BR-012, BR-013)*
2. **Given** a Course missing any of Major, Subject, or Study Year, **When** the Instructor submits
   it, **Then** submission is refused and the response names the missing dimension, in the same
   validation as the completeness check. *(BR-157, BR-159)*
3. **Given** a Course in `PENDING_REVIEW`, **When** its Instructor attempts any content edit,
   **Then** it is refused as read-only until the Admin acts. *(BR-016)*
4. **Given** a Course in `PENDING_REVIEW`, **When** an Admin previews its video content, **Then**
   playback is authorized through an audited path distinct from Student playback, recording Admin,
   Lesson, and timestamp, and creating no enrollment or Entitlement. *(BR-081)*
5. **Given** any Instructor, **When** they attempt to publish directly by any route including a
   direct API call, **Then** it is refused. *(BR-061)*
6. **Given** a Course in `PENDING_REVIEW`, **When** an Admin requests changes, **Then** a reason is
   mandatory, the Course moves to `CHANGES_REQUESTED`, it stays hidden, and the Instructor may revise
   and resubmit. *(BR-072)*
7. **Given** a Course in `PENDING_REVIEW`, **When** an Admin approves it, **Then** it becomes
   `PUBLISHED`, becomes catalogue-visible, and the Instructor is notified through durable delivery
   intent. *(BR-071, BR-120)*

---

### User Story 3 — Editing a Published Course never disturbs what Students are using (Priority: P1)

An Instructor edits a Course that is already Published. The edit does not touch the live approved
version. It accumulates as a pending revision that goes through the same review flow, and the live
Course stays exactly as it is until an Admin approves the revision — at which point the swap is
atomic.

**Why this priority**: This is the data-integrity property of the slice. A partially applied revision
is visible corruption to paying Students, and it is the failure this design exists to prevent.

**Independent Test**: Publish a Course, enrol against it, edit it as the Instructor, and confirm the
Student's view is unchanged until approval — then confirm the swap is all-or-nothing under concurrent
reads.

**Acceptance Scenarios**:

1. **Given** a `PUBLISHED` Course, **When** its Instructor edits structure, video, or protected
   attachments, **Then** a pending revision is created and the live approved version is not modified.
   *(BR-017)*
2. **Given** a pending revision, **When** a Student reads the Course, **Then** they see the live
   approved graph, with no element of the revision visible. *(BR-090)*
3. **Given** a pending revision, **When** an Admin approves it, **Then** the pointer swap applies the
   entire revision atomically — no reader observes a partial graph. *(BR-090)*
4. **Given** a pending revision, **When** an Admin rejects it, **Then** the currently Published
   version is unchanged and remains live. *(BR-072)*
5. **Given** a pending revision that replaces a Resource or Lab Material, **When** it is still
   pending, **Then** the approved live file remains available and is superseded only on approval.
   *(BR-066)*
6. **Given** a Lesson video replacement, **When** the replacement is processed, **Then** it cannot
   interrupt or pre-empt the approved live video, and `READY` never bypasses Admin approval.
   *(BR-091)*

---

### User Story 4 — An Admin controls price, and every change is answerable (Priority: P2)

An Admin sets or changes a Course or Section catalogue price. The Instructor can see the price but
cannot change it. Every change records what it was, what it became, who did it, why, and when.

**Why this priority**: P2 because authoring and review are usable without it, but it is launch
critical — `LG-012` launch prices depend on this path existing and being audited.

**Independent Test**: Set a price as Admin, attempt to change it as the owning Instructor by direct
API call, and read the resulting audit history.

**Acceptance Scenarios**:

1. **Given** an Admin, **When** they set or change a Course or Section price, **Then** it succeeds
   and records old value, new value, acting Admin, reason, and timestamp. *(BR-019)*
2. **Given** the owning Instructor, **When** they attempt a price change by direct API call, **Then**
   it is refused; price remains readable to them. *(BR-019)*
3. **Given** an existing Order, Entitlement, Refund, or payout snapshot, **When** a price changes,
   **Then** none of them is mutated and the change affects future Orders only. *(BR-019)*

---

### User Story 5 — An Admin can take a Course off the market, and out of danger (Priority: P2)

An Admin delists a Course so it stops being discoverable and purchasable while enrolled Students keep
learning; relists it; retires it so it can no longer be acquired; archives it; and — separately and
under constrained cause — invokes emergency access suspension that immediately denies even entitled
Students, then restores it.

**Why this priority**: P2 for authoring, but the emergency path is a safety control the platform
cannot launch without, and its distinction from delisting is exactly what gets confused.

**Independent Test**: Walk one Course through delist → relist → retire → archive, verifying an
entitled Student's access at each step; then invoke emergency suspension and confirm access stops
immediately without any Entitlement being mutated.

**Acceptance Scenarios**:

1. **Given** a `PUBLISHED` Course with an entitled Student, **When** an Admin delists it, **Then** it
   leaves catalogue discovery and blocks new checkout, and **the Student's existing access
   continues**. *(BR-090)*
2. **Given** a `DELISTED` Course, **When** an Admin relists it, **Then** it returns to `PUBLISHED`.
   *(BR-090)*
3. **Given** a retired Course, Section, Lesson, or authored version, **When** a Student holds an
   otherwise-active Entitlement, **Then** access continues only where the Order's
   `retirement_eligibility_at` precedes the relevant `retired_at`, and delayed or retried payment
   delivery cannot bypass retirement. *(BR-027)*
4. **Given** a Course with at least one enrollment, **When** deletion is attempted, **Then** it is
   refused and only archiving is available; a Course with zero enrollments may be deleted outright.
   *(BR-018)*
5. **Given** any Course, **When** an Admin invokes emergency access suspension for a constrained
   legal, security, malware, or severe-moderation cause, **Then** existing Student access is denied
   **immediately**, **no Entitlement is mutated**, and a reason, an audit record, and a durable
   notification intent are all written. *(BR-090)*
6. **Given** an emergency-suspended Course, **When** an Admin restores access, **Then** entitled
   Students regain access and the restoration is equally audited and notified. *(BR-090, BR-122)*

---

### User Story 6 — An Admin curates the vocabulary Instructors choose from (Priority: P3)

An Admin creates, renames, retires, and deletes bilingual Taxonomy Terms. An Instructor selects among
existing terms but cannot create or alter one.

**Why this priority**: P3 because the launch taxonomy is seeded by an audited migration and changes
rarely — but Instructors cannot submit at all without assignable terms, so it cannot be omitted.

**Independent Test**: Administer terms as Admin, attempt each mutation as an Instructor, and confirm
a retired term stays on the Courses already carrying it.

**Acceptance Scenarios**:

1. **Given** an Instructor, **When** they attempt to create, rename, retire, or delete a term,
   **Then** it is refused; selection among existing terms succeeds for an owned Course. *(BR-158)*
2. **Given** an Admin, **When** they perform any term mutation, **Then** it is audited like other
   privileged catalogue actions. *(BR-158)*
3. **Given** a renamed term, **When** it is displayed anywhere, **Then** the new label appears and no
   Course assignment is rewritten. *(BR-159)*
4. **Given** a retired term, **When** a new assignment is attempted, **Then** it is refused, while
   Courses already carrying it keep it and stay filterable. *(BR-160)*
5. **Given** a term referenced by at least one Course, **When** deletion is attempted, **Then** it is
   refused and only retirement is available; a term with zero references may be deleted. *(BR-160)*

---

### Edge Cases

- A Lesson's referenced Asset Version is deleted or fails after submission but before approval —
  approval must refuse rather than publish a dangling reference.
- Two Admins act on the same `PENDING_REVIEW` Course concurrently (one publishes, one requests
  changes) — exactly one wins, the loser gets a defined conflict outcome, and no state is half-applied.
- An Instructor submits the same Course twice concurrently — one `PENDING_REVIEW`, no duplicate queue
  entry.
- A revision is approved while its own Instructor is being suspended.
- A Course is delisted while a revision is pending; the revision's target state must be unambiguous.
- Emergency suspension is invoked while a revision approval is in flight.
- The last Lesson is removed from a Section of a Published Course through a revision, making the
  revision fail its own submission validation.
- An Admin retires a taxonomy term that a `PENDING_REVIEW` Course depends on for a required dimension.
- A price change lands between an Order being priced and being completed — S6 owns the snapshot, but
  S2 must not mutate anything already snapshotted.
- Ownership is reassigned by an Admin (BR-014) while the Course has a pending revision authored by
  the previous owner.

---

## Requirements *(mandatory)*

### Functional Requirements

**Authoring and private-draft protection**

- **FR-001**: System MUST let an authenticated Instructor create a Course they own, starting in
  `DRAFT`. *(BR-011, BR-014)*
- **FR-002**: System MUST keep every non-`PUBLISHED` Course invisible to Students, other Instructors,
  and anonymous callers, on **every** read route, regardless of content completeness. *(BR-011)*
- **FR-003**: System MUST enforce exactly one owning Instructor per Course, reassignable only by an
  Admin. *(BR-014)*
- **FR-004**: Users MUST be able to manage Sections and Lessons within an owned Course while it is
  `DRAFT` or `CHANGES_REQUESTED`. *(BR-015)*
- **FR-005**: System MUST accept Lesson video attachment only as a reference to an exact, existing,
  successfully processed Asset Version, and MUST NOT provide any upload, scan, or transcode path.
  *(BR-013, BR-091, SLICES §3.2)*
- **FR-006**: System MUST support optional per-Lesson resources and lab materials as two distinct,
  independently managed categories. *(BR-067)*
- **FR-007**: System MUST permit at most one optional Course preview asset, authorized separately
  from protected Lesson content, and MUST never expose a protected Resource or Lab Material as a
  public sample. *(BR-143)*
- **FR-008**: System MUST refuse all Course editing by a suspended Instructor while leaving enrolled
  Students' access to that Instructor's Published Courses intact. *(BR-065)*

**Submission and review**

- **FR-009**: System MUST refuse submission when the Course has zero Sections, any Section has zero
  Lessons, or any Lesson lacks a successfully processed video — and MUST name each missing item
  specifically rather than returning a generic failure. *(BR-012, BR-013)*
- **FR-010**: System MUST refuse submission when any of Major, Subject, or Study Year is unassigned,
  naming the missing dimension in the same validation response as FR-009. *(BR-157, BR-159)*
- **FR-011**: System MUST move a submitted Course to `PENDING_REVIEW`, place it in the Admin queue,
  and keep it hidden from the Student catalogue unless a previously approved live version exists.
  *(BR-070)*
- **FR-012**: System MUST make a `PENDING_REVIEW` Course read-only to its Instructor until an Admin
  acts. *(BR-016)*
- **FR-013**: System MUST refuse Course publication by an Instructor through every route, including
  direct API calls. *(BR-061)*
- **FR-014**: System MUST let an Admin approve a Course or revision, moving it to `PUBLISHED` and
  writing a durable notification intent to the Instructor. *(BR-071, BR-120)*
- **FR-015**: System MUST require a reason for an Admin change request, move a first-publication
  Course to `CHANGES_REQUESTED`, keep it hidden, and permit revise-and-resubmit. *(BR-072)*
- **FR-016**: System MUST authorize Admin preview of any Course's video — including `PENDING_REVIEW`
  and Draft Lessons — through a path distinct from Student playback, recording Admin, Lesson, and
  timestamp, and creating no enrollment or Entitlement. *(BR-081)*
- **FR-017**: System MUST write a durable notification intent to Admin operations when an Instructor
  submits a Course or revision. *(BR-122)*

**Revision integrity**

- **FR-018**: System MUST route every edit to a `PUBLISHED` Course's structure, video, or protected
  attachments into a pending revision, leaving the live approved version unmodified. *(BR-017)*
- **FR-019**: System MUST keep the live approved graph the only Student-visible graph while a
  revision is pending. *(BR-090)*
- **FR-020**: System MUST apply an approved revision as one atomic pointer swap, such that no reader
  observes a partially applied graph. *(BR-090)*
- **FR-021**: System MUST leave the currently Published version unchanged when a pending revision is
  rejected. *(BR-072)*
- **FR-022**: System MUST keep an approved live Resource or Lab Material available until its
  replacement revision is approved, then supersede it. *(BR-066)*
- **FR-023**: System MUST prevent a replacement video from interrupting the approved live video, and
  MUST NOT let processing readiness bypass Admin approval. *(BR-091)*
- **FR-024**: System MUST subject a revision to the same submission validation as a first
  publication. *(BR-012, BR-017)*
- **FR-025**: System MUST refuse approval when any referenced Asset Version is no longer present and
  successfully processed at approval time — approval revalidates rather than trusting submission-time
  state.

**Pricing**

- **FR-026**: System MUST restrict Course and Section catalogue price setting to Admins. *(BR-019)*
- **FR-027**: System MUST give Instructors read-only price visibility on owned Courses. *(BR-019)*
- **FR-028**: System MUST record old value, new value, acting Admin, reason, and timestamp for every
  price change, as immutable audit history. *(BR-019)*
- **FR-029**: System MUST apply a price change to future Orders only, mutating no existing Order,
  Entitlement, Refund, or payout snapshot. *(BR-019)*

**Catalogue lifecycle and emergency control**

- **FR-030**: System MUST implement the lifecycle `DRAFT → PENDING_REVIEW → PUBLISHED`, with
  `PENDING_REVIEW → CHANGES_REQUESTED → PENDING_REVIEW`, `PUBLISHED ↔ DELISTED`, and
  `PUBLISHED/DELISTED → ARCHIVED`, refusing every transition outside it. *(BR-090)*
- **FR-031**: System MUST make `DELISTED` remove catalogue discovery and new checkout **without**
  denying qualifying existing access. *(BR-090)*
- **FR-032**: System MUST treat retirement as a future-acquisition block, preserving access where the
  Order's `retirement_eligibility_at` precedes `retired_at`, and MUST NOT let retried or delayed
  payment delivery bypass it. *(BR-027)*
- **FR-033**: System MUST refuse permanent deletion of a Course with at least one enrollment, offering
  archiving instead, and MUST permit outright deletion only at zero enrollments. *(BR-018)*
- **FR-034**: System MUST provide emergency Course access suspension as an elevated, separately
  authorized command, restricted to constrained legal, security, malware, or severe-moderation
  causes. *(BR-090)*
- **FR-035**: Emergency suspension MUST deny existing Student access immediately — not at next token
  or session expiry — and MUST mutate no Entitlement. *(BR-090)*
- **FR-036**: Emergency suspension and its restoration MUST each require a reason and write an audit
  record and a durable notification intent. *(BR-090, BR-122)*

**Taxonomy**

- **FR-037**: System MUST restrict Taxonomy Term creation, renaming, retirement, and deletion to
  Admins, with every action audited. *(BR-158)*
- **FR-038**: System MUST let an Instructor select among existing terms for an owned Course, and let
  an Admin override any Course's assignment. *(BR-158)*
- **FR-039**: System MUST apply a term rename to every display without rewriting any Course
  assignment, and MUST refuse new assignment of a retired term while preserving it on Courses already
  carrying it. *(BR-159, BR-160)*
- **FR-040**: System MUST refuse deletion of a term referenced by at least one Course, offering
  retirement instead. *(BR-160)*

**Authorization, audit, and enforcement**

- **FR-041**: Every protected S2 route MUST reach a decision through the existing `identity.Authorize`
  deny-by-default gate over its closed capability set. Any new capability is **added to that set**.
  No second decision point may exist. *(Constitution II)*
- **FR-042**: Ownership MUST be enforced server-side on every owned-resource route as an explicit
  precondition, and the coverage MUST be **derived from the live route table** rather than from a
  hand-maintained list — an unenforced route must fail a test, not a review reading. *(S1C precedent)*
- **FR-043**: Every privileged action in this slice — publish, request changes, delist, relist,
  retire, archive, delete, price change, ownership reassignment, emergency suspension and
  restoration, taxonomy mutation, and Admin preview — MUST write an audit record identifying actor,
  target, action, reason where required, and time. *(Constitution II, BR-081, BR-133, BR-158)*
- **FR-044**: Every required dependency of an S2 component MUST be validated at construction, and the
  component MUST refuse to be built without it. **No security-relevant control may silently degrade,
  default, or become optional.** *(Standing clause, carried from the S1C closeout where six instances
  of this defect class appeared in one slice)*
- **FR-045**: Notification delivery failure MUST NOT block, roll back, or alter any publish, price,
  lifecycle, or suspension action. *(BR-120)*

### Key Entities

- **Course**: The purchasable unit. Exactly one owning Instructor, one lifecycle state, three
  taxonomy assignments, an optional preview asset, an optional catalogue price, and at most one
  pending revision.
- **Section**: An ordered division of a Course, independently purchasable and independently priced.
- **Lesson**: An ordered unit inside a Section, referencing exactly one Asset Version as its video,
  plus optional resources and lab materials.
- **Course Revision**: A pending, reviewable, whole-graph alternative to the live approved version.
  Becomes live only by atomic pointer swap on approval.
- **Asset Version Reference**: A pointer to an exact media artefact owned by S4. S2 validates and
  references; it never produces.
- **Taxonomy Term**: A bilingual Admin-managed vocabulary entry in the Major or Subject dimension,
  with an optional academic code. Study Year is a fixed enumeration, not a term.
- **Price Change Record**: An immutable audit entry — old, new, actor, reason, time.
- **Emergency Access Suspension**: A reason-bearing, audited, notified state that denies access
  without touching Entitlements.

---

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An Instructor can take a Course from creation to submitted-for-review, including
  structure, media references, and classification, in under 30 minutes for a 5-Lesson Course.
- **SC-002**: **100% of non-Published content is unreachable** by Students, non-owning Instructors,
  and anonymous callers, verified by direct request to exact identifiers on every read route — not by
  absence from a listing.
- **SC-003**: **Zero** direct API calls can publish, price, delist, retire, archive, emergency-suspend,
  or administer taxonomy without the required role, proven by an authorization sweep derived from the
  live route table.
- **SC-004**: **100% of pending revisions leave the live Student-visible graph unchanged** until
  approval, and every approval is observed as all-or-nothing under concurrent reads.
- **SC-005**: An Admin can review and publish a submitted Course, including video preview, in under
  10 minutes.
- **SC-006**: **Every** privileged action in this slice produces a retrievable audit record naming
  actor, target, reason where required, and time — verified by enumerating the privileged routes, not
  by sampling.
- **SC-007**: Emergency access suspension denies an entitled Student's next protected request
  immediately, with no Entitlement record altered.
- **SC-008**: An incomplete submission names **every** missing item in one response, so an Instructor
  never has to submit repeatedly to discover the next problem.
- **SC-009**: Authoring and review screens are fully usable in Arabic and English with correct RTL and
  LTR behaviour, on tablet, laptop, and desktop widths. *(Constitution X — complex authoring may be
  optimized for larger layouts)*

---

## Assumptions

- **S1C is closed before implementation starts.** This spec may be frozen while S1C's independent
  review is open, but no S2 code lands until S1 closes.
- **Asset Versions exist as a referenceable concept before S2 completes.** S2 is dependency-ordered
  after S1C and alongside S4's ownership of media bytes. Where S4 has not yet produced real Asset
  Versions, S2 is developed and tested against the reference contract, not against an invented upload
  path.
- Enrollment and Entitlement are **read** by S2's delete-safeguard and access-continuity rules and
  **written** by S4/S7. S2 introduces no Entitlement creation path of any kind.
- Bilingual authored content is captured per Course in Arabic and English; the Course detail
  presentation of that content belongs to S3.
- The launch taxonomy is seeded through an audited migration; the administration UI exists for
  correction rather than bulk authoring.
- Ordering of Sections and Lessons is Instructor-controlled and explicit rather than derived from
  creation time.
- "Immediately" in FR-035 means at the next protected request, consistent with the S1C suspension
  enforcement precedent — not at next token expiry.

## Dependencies

- **S1C** — role and ownership enforcement, the authorization matrix, and the suspension enforcement
  precedent this slice extends.
- **S4** — Asset Versions. S2 references; S4 produces.
- **S9 delivery adapter** — durable notification intent. `LG-018` is unresolved, so S2 writes intent
  and evidence, never live mail.
- **`LG-012`** — launch prices are entered through the audited Admin path this slice builds.
