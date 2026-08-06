# API Contract: Course Access Invitation and Entitlement Grant

**Date**: 2026-07-29 | **Plan**: [../plan.md](../plan.md) | **Data model**: [../data-model.md](../data-model.md)

Binding contract for S6's HTTP surface. All routes are under `/api/v1`, use the existing RFC 9457
Problem Details envelope for errors, and are covered by the derived authorization sweep in
`backend/internal/httpapi/authorization_test.go` — a new route that does not carry its guard fails
that test. *(Verified 2026-08-06: that file exists.)*

> **Reconciled 2026-08-06 against the router at the S5 closure head `d5ce557`. No status code, response
> class, or route semantic changed.** Four facts about how these paths land in the live router:
>
> - **`/admin/…` has precedent; `/me/…` does not.** `catalog_routes.go` already mounts
>   `v1.Group("/admin/review")`, `/admin/courses/:id`, and `/admin/taxonomy/terms`, so
>   `/admin/course-access-invitations` follows the house convention. There is **no `/me` prefix anywhere
>   in the backend** — the existing Student surface is `v1.Group("/learn")`. `/me` is therefore a **new
>   top-level prefix this slice introduces.** It is kept rather than folded into `/learn`, because
>   `/learn` carries the protected-learning guard and an invited Student holds no Entitlement yet.
> - **Path parameters are `:id`, not `{id}`.** The router is Gin. `{id}` below is documentation notation;
>   the mounted paths read `/admin/course-access-invitations/:id/approve` and
>   `/me/course-access-invitations/:id/accept`.
> - **The Student routes must not reuse `requireProtectedLearningAccess`.** That middleware
>   (`media_delivery_handlers.go:112`, used by `/learn/*` and the protected media paths) writes a uniform
>   `writeProtectedUnavailable` refusal for every failure. Reusing it would collapse the 403/404/409/410/422
>   classes below into one response and would defeat FR-009's byte-identical-404 requirement by making
>   *everything* byte-identical. S6 supplies its own Student guard in `access_foundation.go`, emitting the
>   `internal/problem` envelope. `CapLearningAccess` remains the correct capability class: `Authorize`
>   grants it to every Active Student independently of any Entitlement.
> - **`GET /admin/entitlements/:id` is a new route.** S4 mounted no admin entitlement read model. The
>   `entitlement_adjustments` table exists in `0012` and supplies this route's adjustment history, so the
>   read model has a real source.

---

## Admin surface

Every route below requires an **Active Admin session**, the **`COURSE_ACCESS_GRANT`** capability, and
CSRF for mutations. `POST …/approve` additionally requires **valid recent authentication**.

Absent any of these the request is **refused**. It does not proceed with a default, a fallback, or
reduced authority (FR-014).

### `POST /admin/course-access-invitations`

Create an invitation. Grants nothing.

**Body**: `course_id` (required), `email` (required), `admin_note` (optional),
`external_reference` (optional).

| Outcome | Status | Notes |
|---|---|---|
| Created | `201` | Body returns the invitation; acceptance link is delivered by outbox, never in the response |
| Non-terminal invitation already exists for the pair | `409` | `duplicate-invitation` |
| Email belongs to a non-Student Account | `409` | `ineligible-recipient` (FR-004, BR-082) |
| Course not found | `404` | |
| Missing or malformed field | `422` | `validation-failed` |

The acceptance secret is **never** returned in an API response, logged, or included in a non-protected
outbox payload — the same boundary the staff-invitation path holds.

### `GET /admin/course-access-invitations`

Queue. Filterable by `state` and `course_id`, paginated.

### `POST /admin/course-access-invitations/{id}/approve`

**The only route in the product that creates course access.**

| Outcome | Status | Notes |
|---|---|---|
| Granted | `200` | Returns the invitation and the created Entitlement |
| **Already approved** | `200` | Returns the **existing** grant. Idempotent — not an error (FR-016, race 1) |
| Invitation not in `PENDING_ADMIN_APPROVAL` | `409` | `invitation-state-conflict`, naming the state found |
| Student already holds active access | `409` | `already-has-active-access` (race 6) |
| Course archived, delisted, or retired | `409` | `course-not-grantable`, naming the state |
| Course has no future configured expiry instant | `422` | `validation-failed`, naming the missing configuration (FR-017). **The column this reads did not exist at the S5 closure head** — see [D-073](../../../docs/DECISIONS.md#d-073--s6-owns-the-course-default-access-expiry-column-because-no-closed-slice-created-it) |
| Missing capability | `403` | `insufficient-capability` |
| Stale authentication | `403` | `recent-authentication-required` |

A Course under **emergency access suspension is grantable** — the grant is valid and simply unusable
until the suspension lifts. See [plan.md §Course-state outcomes](../plan.md#course-state-outcomes-at-approval).

### `POST /admin/course-access-invitations/{id}/reject`

**Body**: `reason` (required).

| Outcome | Status |
|---|---|
| Rejected | `200` |
| Missing reason | `422` `validation-failed` |
| Not in `PENDING_ADMIN_APPROVAL` | `409` `invitation-state-conflict` |

### `POST /admin/course-access-invitations/{id}/cancel`

Permitted from either non-terminal state. Invalidates any outstanding acceptance secret.

### `POST /admin/course-access-invitations/{id}/resend`

Issues a fresh acceptance link and supersedes every prior secret for this invitation.

| Outcome | Status |
|---|---|
| Reissued | `200` |
| Invitation already accepted or terminal | `409` `invitation-state-conflict` (FR-025) |

### `GET /admin/entitlements/{id}`

Read model for AD07. Returns grant source, source invitation, `original_access_ends_at`, effective
`access_ends_at`, adjustment history, and revocation state.

**S6 adds no expiry-adjustment or revocation mutation.** Those belong to **S8 Admin Operations,
exclusively** — one owner, not two. S4 owns Entitlement *evaluation*, which is a different thing from
mutating one. This route is read-only, and S6 ships no write path to an Entitlement other than the
approval transaction that creates it.

---

## Student surface

Requires an **Active Student session**.

### `GET /me/course-access-invitations`

Returns only invitations whose `normalized_email` equals the caller's. Never another Student's.

Response **excludes** `admin_note`, `external_reference`, `decided_by_account_id`, and any approval
evidence (FR-036).

### `GET /me/course-access-invitations/{id}`

| Outcome | Status |
|---|---|
| Found and addressed to the caller | `200` |
| Exists but addressed to a different identity | **`404`** |
| Does not exist | **`404`** |

**The two 404s are byte-identical.** Holding a link reveals nothing about whether an invitation exists
(FR-009).

### `POST /me/course-access-invitations/{id}/accept`

**Body**: `acceptance_token` (the single-use secret).

| Outcome | Status | Notes |
|---|---|---|
| Accepted | `200` | State → `PENDING_ADMIN_APPROVAL`. **No access is granted** (FR-010) |
| Caller's email does not match | `404` | Identical to not-found (FR-008, FR-009) |
| Token expired, consumed, or superseded | `410` | `acceptance-link-expired` — the **invitation is unaffected** and a new link can be issued (FR-012) |
| Invitation not in `PENDING_STUDENT_ACCEPTANCE` | `409` | `invitation-state-conflict` |

### `GET /me/course-access`

Access history for ST10 — per Course, invitation state, timestamps, and the access-until instant
where active.

---

## Contract-level invariants

These are properties of the surface as a whole. Each is separately testable, and each corresponds to a
success criterion.

1. **No route creates an Entitlement except `POST …/approve`.** Proven by enumerating the live route
   table, not by inspection. *(FR-013, FR-020, SC-006)*
2. **No route reads Course Access Invitation state to make an authorization decision.** Playback,
   downloads, progress, and roster authorise against the Entitlement alone. *(FR-026, SC-007)*
3. **No request or response body anywhere carries an amount, currency, payment status, gateway
   identifier, or payer instrument.** *(FR-005, SC-012)*
4. **Every mutation is audited before its transaction commits.** *(FR-031, SC-008)*
5. **Every mutation carries CSRF and strict body-limit binding**, matching the S1C correction where
   two suspension routes bypassed strict binding with their declared limits unreferenced.
6. **Wrong-identity access returns 404, never 403.** *(FR-009)*
