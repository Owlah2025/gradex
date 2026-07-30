# Contract — Public Catalogue API

**Spec**: [../spec.md](../spec.md) | **Plan**: [../plan.md](../plan.md)

**This contract is the authority.** In S1C every mounted staff route diverged from its frozen contract
on path, method, or both; the reviewer found it, and the **routes moved rather than the contract being
rewritten to match what shipped**. The same rule applies here.

## Conventions

- Base path `/api/v1/catalog`. All routes are **anonymous** — no session, no CSRF, no capability.
- Errors use the existing RFC 9457 Problem Details envelope.
- Money is integer minor units (fils) with an explicit currency, never a formatted string. Formatting
  is the frontend's job; a pre-formatted price in the payload is a second source of truth for money.
- All responses `Cache-Control: public` with a short max-age. **No public response may be `private` or
  carry a `Set-Cookie`** — these routes are cacheable by construction and must not become
  visitor-specific.

## `GET /api/v1/catalog/courses`

Paginated list of Published Courses.

**Query**: `page`, `page_size` (bounded; an over-large value is clamped, not an error), `q` (optional
search, see below).

**200** — items carrying: identifier, slug, title, Instructor **display name only**, the three
taxonomy dimensions with localized labels and the optional Subject code, full-Course price, whether a
public preview exists, and pagination metadata.

**Never present**: lifecycle state, owner identifier, any Instructor or Student PII beyond display
name, Lesson titles, Resource or Lab Material references, pending-revision content, internal
timestamps beyond what the UI renders.

> Absence of a `status` field is not incidental. A public payload that carries `"status":"PUBLISHED"`
> invites a client to filter on it, which is how a second visibility decision point is born.

## `GET /api/v1/catalog/courses/{idOrSlug}`

One Published Course.

**200** — the list fields plus authored description, the Section outline (Section titles, ordering,
and per-Section prices where individually priced), and the public preview reference if present.

**404** — for a non-existent identifier **and** for every non-public Course: `DRAFT`,
`PENDING_REVIEW`, `CHANGES_REQUESTED`, `DELISTED`, `ARCHIVED`, and any Course under an active
emergency access suspension.

**The two 404s are identical in status, headers, response schema, and body** — no cause-varying
`detail`. Produced by one constructor; a handler may not build another. See
[FR-003](../spec.md#requirements-mandatory).

**Timing is a separate and weaker claim, and this contract does not overstate it.** The predicate sits
inside the query boundary rather than in a fetch-then-check application branch, which removes a real
oracle but does not make the two paths provably equal. Timing is **measured** against a documented
tolerance and reported as a statistical observation — see
[plan.md](../plan.md#the-timing-claim-stated-honestly) and SC-008. No part of this contract guarantees
timing indistinguishability.

**No 403 exists on this surface.** A `403` would answer the question a `404` exists to refuse.

## Search

Search is the `q` parameter on the list route, **not a separate endpoint**. One route, one predicate,
one projection — a separate search endpoint would be a second place to forget the visibility filter.

- Matches title, description, Instructor display name, and taxonomy labels/code *(BR-161)*.
- Case-insensitive; matches Arabic and English simultaneously regardless of interface language,
  through **one** `IMMUTABLE` SQL normalization function applied identically to stored text and to the
  incoming query *(BR-162 matching behaviour; [OD-001](../spec.md#resolved-decisions) resolved
  `ADJUST`)*. Normalization covers alef/hamza folding, alef maqsura, taa marbuta, Arabic-Indic digits,
  tashkeel and tatweel removal, case folding, and whitespace collapse.
- A query that **normalizes to empty** — only diacritics, tatweel, or whitespace — behaves exactly as
  an absent `q`: the unfiltered published list, not an error and not an empty result.
- **No stemming, fuzzy or edit-distance matching, weighted ranking, or external search service.**
  BR-162's *"ranked by relevance"* clause is **not** implemented in S3; ranking is deferred to S18, so
  this surface claims **partial** BR-162 compliance — complete on matching, absent on ranking.
- Restricted to Published Courses through the **same** `PublishedOnly` predicate the unfiltered list
  uses — never a separate status condition in the search query *(FR-022)*.
- Matches only the Course's **live revision**, resolved through its live-revision pointer. A Course is
  never surfaced through a pending or superseded revision, even though every revision holds stored
  searchable text — storage is not a visibility control on this surface *(FR-005; BR-017)*.
- Empty, whitespace-only, over-long, and metacharacter-bearing queries return a well-formed result,
  never an error disclosing internals *(FR-024)*.
- No ranking, personalization, or paid placement *(FR-025)*. Ordering is stable and documented — not
  relevance: results are ordered by Course UUID ascending.

## Localization

- The interface language is a **client** concern; the API returns localized taxonomy labels for the
  requested locale and returns authored Course content **as authored** *(BR-150)*.
- No endpoint machine-translates. A Course authored in Arabic returns Arabic under an English
  interface.

## What this contract does not contain

No write route. No authenticated route. No entitlement, order, or checkout route. No protected-media
route. If implementation appears to need one, that is a finding against
[spec.md](../spec.md), not an extension to make here.
