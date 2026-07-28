# Phase 0 Research — S2 Course Authoring and Review

**Date**: 2026-07-28 | **Plan**: [plan.md](plan.md)

The Technical Context carried no `NEEDS CLARIFICATION` markers: every technology choice is inherited
from the approved M1 architecture and the four slices already shipped. Research therefore resolves
**design questions the source documents left open to the implementer**, not technology selection.

Each entry below was resolved against the repository and the source documents rather than by
preference.

---

## R1 — How is "the live approved version" represented?

**Decision**: `courses.live_revision_id`, a nullable same-Course foreign key to
`course_revisions`. Publishing updates that pointer inside the approving transaction. A graph read
captures it once and never re-resolves it during assembly.

**Rationale**: BR-017 and BR-090 require that the live graph is untouched while a revision is pending
and that approval is a "pointer swap". A pointer is therefore the literal requirement, not an
interpretation of one. It also makes the privacy property structural: a Student-visible read joins
through `live_revision_id`, so a draft graph is unreachable by construction rather than by a `WHERE`
clause that a future query might omit. `NULL` means never published, which is a single readable
definition of "invisible".

**Alternatives considered**:

- *Status flags on each row of the graph* — rejected. Atomicity would then depend on updating many
  rows consistently, and FR-020's "no reader observes a partial graph" would become a claim rather
  than a property.
- *Copy-on-publish into a separate published table* — rejected. Doubles the schema and creates two
  places where structure can drift apart, for no gain over a pointer.

---

## R2 — Does a revision copy the whole graph, or store a delta?

**Decision**: whole-graph. A revision owns version rows for its Sections and Lessons; approval swaps
which set is live. Stable logical Section/Lesson identities are preserved across copies, while the
version rows receive new IDs.

**Rationale**: BR-090 says the current approved graph "stays live until pointer swap", and BR-066
requires the approved live file to remain available until approval. A delta would have to be
materialized against the live graph to be reviewable, which is precisely the mutation the rule
forbids. At launch scale (8–12 Courses, tens of Lessons) the duplication cost is irrelevant, and
Constitution VI forbids optimizing for volume that does not exist. Preserving stable identities is
not a scale optimization: BR-059 keys progress to the Lesson rather than its video, and BR-019 makes
a Section a durable purchasable scope. Only a genuinely new or explicitly deleted-and-recreated
entity gets a new logical identity. The clone copies revision-owned rows and references, never the
referenced media or any commerce, access, or learning record.

**Alternatives considered**:

- *Delta/patch records* — rejected as above, and it makes Admin review harder: a reviewer must see
  what the Course *will be*, not a diff they have to apply mentally.

---

## R3 — Is emergency access suspension a lifecycle state?

**Decision**: **No.** It is a separate, orthogonal, reason-bearing attribute of the Course.

**Rationale**: BR-090 explicitly separates them — "Emergency Course access suspension is a separate
elevated command". If it were a lifecycle value it would have to displace `PUBLISHED`, and restoring
it would need to remember which state to return to. Worse, it would make delisting and emergency
suspension look interchangeable, when their whole difference is that delisting preserves existing
access and suspension denies it. That confusion is the specific failure this decision prevents.

**Alternatives considered**:

- *A `SUSPENDED` lifecycle value* — rejected. Conflates two controls with opposite effects on
  entitled Students.

---

## R4 — Where does suspension get enforced, given S2 does not own access?

**Decision**: S2 owns the *state* and its audit and notification evidence. Enforcement lives at the
access decision point that S4/S5 build, which reads this state live on every protected request.

**Rationale**: FR-035 requires denial "immediately, not at next token expiry". The S1B2 session
repository already establishes the pattern — it re-reads `accounts.status` live rather than trusting
a cached claim — and S1C extended it to sessions. Following the same pattern makes S2's obligation a
live read for the downstream consumer, not a revocation sweep.

**Consequence recorded for S4/S5**: the media/learning access check must include the Course
suspension read. This is a cross-slice obligation and belongs in the S4 plan; it is written here so
it is not discovered late.

---

## R5 — Does submission validation stop at the first failure?

**Decision**: No. Validation collects **every** failure — missing Sections, empty Sections, Lessons
without processed video, and each unassigned taxonomy dimension — and returns them together.

**Rationale**: BR-012 requires the Instructor to see "what's missing", and BR-159 puts the taxonomy
check "in the same validation". SC-008 makes it measurable. Fail-fast validation here produces a
submit-fix-submit loop that is a genuine usability defect for a 20-Lesson Course, and the rules were
written to prevent it.

---

## R6 — How is the ownership check kept from drifting?

**Decision**: one middleware, plus a test that derives the route list from the live router and
asserts every owned-resource route carries it.

**Rationale**: S1C shipped a hand-maintained authorization matrix and it was a high finding precisely
because it could not detect drift; the remediation derived the matrix from `r.Routes()` and
immediately found two real gaps. Repeating the hand-maintained approach in the next slice would be
repeating a known defect with the evidence still on the page.

**Alternatives considered**:

- *Per-handler ownership checks* — rejected. Forty handlers means forty chances to forget one, and no
  mechanism to notice.

---

## R7 — Where do notifications go, with `LG-018` unresolved?

**Decision**: durable outbox **intent** through the existing `outbox` package's protected-payload
boundary. No live mail.

**Rationale**: `LG-018` (verified sender) is open and is a launch gate. S1B already established
intent-plus-evidence as the pattern for exactly this situation, and BR-120 requires that delivery
failure never rolls back the business action. Writing intent inside the business transaction and
dispatching separately satisfies both.

**Guard**: the intent is **mandatory**, not optional. Round two of the S1C review had to close an
"optional outbox intent" finding; the same construction must not reappear here.

---

## R8 — Does S2 need any new infrastructure?

**Decision**: none. No new service, queue, cache, search index, or external dependency.

**Rationale**: Constitution VI forbids new moving parts without a demonstrated current requirement.
Every need this slice has is met by PostgreSQL, the existing outbox, the existing audit table, the
existing authorization gate, and the existing migration and CI rails. The `CATALOG_AND_AUTHORING`
audit module value already exists in migration `0003` and requires no schema change.

---

## Open question carried to `/speckit-tasks`

**Ownership reassignment with a pending revision** (BR-014). Admin reassignment is in scope, and a
revision authored by the previous owner is an edge case the source documents do not settle. The plan's
position — the revision follows the Course, because it is the Course's pending state rather than the
author's property — is a reasonable default and is recorded as an assumption rather than presented as
a cited rule. It should be confirmed by the developer during implementation, and it changes no other
requirement if overturned.
