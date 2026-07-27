# Quickstart — Validating S3 Public Catalogue

**Slice**: S3 | **Plan**: [plan.md](plan.md) | **Tasks**: [tasks.md](tasks.md)

Every scenario below is a **required** acceptance proof, and each must **fail under a deliberate
mutation**. A test that passes against broken code is not evidence.

## Prerequisites

No new dependency:

- PostgreSQL at schema **10**, Redis, MinIO
- `GOCACHE=/tmp/gradex-go-cache` — the workspace sandbox refuses the default
- A fixture catalogue containing **every** lifecycle state: Published, Draft, Pending Review, Changes
  Requested, Delisted, Archived, a Published Course with an active emergency suspension, a Published
  Course with a pending revision, and a Published Course whose owning Instructor is suspended
- At least one Course with a public preview asset and one without

The fixture is the test. Most leaks in this slice are invisible against a catalogue containing only
Published Courses.

## Gate commands

```bash
# Backend
cd backend
gofmt -l .                                  # must be empty
go build ./... && go vet ./... && go vet -tags=integration ./...
go test -count=1 -race ./...
go test -count=1 -race -tags=integration ./...

# Frontend
cd frontend
npm run typecheck && npm run lint && npm test
rm -rf .next && npm run build                # clean build or it is not evidence

# Guards
./scripts/docs-guard.sh && ./scripts/expose-guard.sh
```

**`rm -rf .next` is not optional.** A build claim that does not say "clean" is to be read as not
having been made (Decisions in Force, in effect since D1).

## Scenario 1 — Nothing unpublished is visible, anywhere (FR-002–FR-006, SC-001)

1. Request the catalogue list as an anonymous caller. Only Published Courses appear; the count matches
   the fixture exactly.
2. Request **every** non-Published Course by exact identifier. Each returns `404`.
3. Enumerate every route under the public prefix from `r.Routes()` and repeat step 2 against each one.

**Verify by**: integration test deriving its route list from the live router — not a chosen subset.
**Mutations**: remove one of the four exclusions from `PublishedOnly`; add a public route that queries
the catalog tables directly. Each must turn a test red, and the second must **name the offending
route**.

## Scenario 2 — A hidden Course is indistinguishable from a missing one (FR-003, SC-002)

Request a Draft Course by exact identifier and a never-existing identifier. Compare the full
responses: status, body, **and headers**.

**They must be byte-identical.** Assert on the whole response, not on the status code — a differing
`detail` field or `Cache-Control` is the leak.

**Mutation**: return `403` for the hidden case. The assertion must fail.

**Also check timing**: confirm the predicate is in the `WHERE` clause rather than fetch-then-check in
application code. A hidden row and an absent row must take the same path.

## Scenario 3 — Arabic by default, and the layout follows (FR-015–FR-017, SC-003)

1. Open any public page with no stored preference → Arabic, `dir="rtl"`.
2. Switch to English, navigate, close the session, return → English persists, `dir="ltr"`.
3. Repeat both as an authenticated user.

**Mutation**: default the locale to English. Step 1 must fail.

## Scenario 4 — No PII, no protected content (FR-006, FR-011, SC-005, SC-007)

Assert against the **full serialized response body**, not the rendered page:

- No Instructor email, phone, or legal identity — display name only.
- No Lesson titles, Resource filenames, or Lab Material references.
- No lifecycle state field.

Then search for a Lesson title, a Resource filename, and a Draft Course's title. Each returns nothing.

**Mutation**: add the owner's email to the detail projection. The body assertion must fail — this is
why it runs against the payload rather than the page, where an unrendered field would pass.

## Scenario 5 — Search behaves under real input (FR-021–FR-025)

1. A query matching title, description, Instructor display name, and taxonomy code each return the
   right Published Courses.
2. **Subject to [OD-001](spec.md#open-decisions)**: `احياء` matches a Course titled `أحياء`; a query
   with diacritics matches text without them; Arabic-Indic digits match Western ones.
3. Empty, whitespace-only, 10 KB, and `%' OR 1=1 --` queries each return a well-formed result and
   never an error disclosing internals.

**Mutation**: apply normalization on write but not on query. Case 2 must fail — this is the asymmetry
the shared function exists to prevent, and it passes every naive test that only searches English.

## Scenario 6 — The suspended-Instructor case (FR-007, BR-065)

A Published Course whose owning Instructor is suspended **stays publicly visible**. Suspension blocks
authoring, not Student access.

Listed separately because it is the one case where the correct behaviour is to *show* something, and
an implementer hardening the visibility filter is likely to over-correct.

## Scenario 7 — Responsive and performance (FR-018, SC-004, SC-006)

- Phone, tablet, desktop widths × RTL and LTR: no clipping, mirroring, or overlap.
- List and detail pages meet p95 LCP under 2.5s on representative Kuwait 4G.

A missed performance target is recorded as a finding with an owner. It is not waived silently.

## Closing the slice

- Full gate suite green, including a clean frontend build.
- Hosted CI green on the exact frozen head.
- Independent **Tier 1** review by Claude against one frozen exact commit range, with every critical
  and high finding resolved.
- **A builder never closes its own slice.**
