# Contract — Admin Catalogue, Pricing, and Taxonomy API

**Slice**: S2 | **Plan**: [../plan.md](../plan.md) | **Base**: `/api/v1/admin`

Capabilities, all Admin-only and all new members of the closed set (FR-041):

- `CATALOG_PUBLISH` — lifecycle transitions, ownership reassignment, emergency suspension
- `CATALOG_PRICING` — price setting
- `CATALOG_TAXONOMY` — term administration

## Lifecycle

| Method | Path | Purpose | Rules |
|---|---|---|---|
| `POST` | `/courses/{id}/delist` | Leave catalogue and block new checkout | **Existing access continues** — BR-090 |
| `POST` | `/courses/{id}/relist` | Return to `PUBLISHED` | BR-090 |
| `POST` | `/courses/{id}/retire` | Block future acquisition | Qualifying existing access preserved — BR-027 |
| `POST` | `/courses/{id}/archive` | Remove from catalogue and new purchase | BR-018, BR-090 |
| `DELETE` | `/courses/{id}` | Permanent deletion | **Refused at ≥1 enrollment** — BR-018 |
| `POST` | `/courses/{id}/owner` | Reassign the owning Instructor | Admin only — BR-014 |

`DELETE` returns `409` naming archiving as the available alternative when any enrollment exists. The
count is checked inside the deleting transaction; this is a refusal, never an `ON DELETE CASCADE`
(Constitution VII).

Delisting and retirement are **different controls** and neither denies existing access. Only emergency
suspension does.

## Emergency access suspension

| Method | Path | Rules |
|---|---|---|
| `POST` | `/courses/{id}/access-suspension` | Reason mandatory and constrained to legal, security, malware, or severe-moderation cause |
| `DELETE` | `/courses/{id}/access-suspension` | Restoration, equally audited and notified |

Both write an audit row and a notification intent in the same transaction as the state change
(FR-036). Neither mutates any Entitlement (FR-035).

Denial is **immediate** — the downstream access decision reads `courses.access_suspended_at` live on
every protected request, exactly as the session repository reads `accounts.status` live. It is not a
revocation sweep and it does not wait for expiry. *This places a read obligation on S4/S5; it is
recorded in [research R4](../research.md) so it is not discovered late.*

## Pricing

| Method | Path | Rules |
|---|---|---|
| `PUT` | `/courses/{id}/price` | Admin only, reason required |
| `PUT` | `/courses/{id}/sections/{sectionId}/price` | Section prices are independent — BR-019 |
| `GET` | `/courses/{id}/price-history` | Append-only history |

Every change appends `old`, `new`, actor, reason, and time (FR-028). The Instructor's read-only view
of the current price lives in the authoring surface (FR-027); the Instructor has no write route here
at all — refusal is the absence of the capability, not a check inside a shared handler.
`{sectionId}` is the stable Section identity introduced by D5, never a revision-owned version-row ID.
T039 remains the first task allowed to implement this route.

**A price change mutates no existing Order, Entitlement, Refund, or payout snapshot** (FR-029). S6
owns Order pricing and snapshots; this contract sets the catalogue price and nothing else.

Money is integer minor units.

## Taxonomy

| Method | Path | Rules |
|---|---|---|
| `POST` | `/taxonomy/terms` | Create a bilingual term; Subject may carry an academic code |
| `PATCH` | `/taxonomy/terms/{id}` | Rename — changes every display, rewrites no assignment (BR-159) |
| `POST` | `/taxonomy/terms/{id}/retire` | Retired terms are unassignable but stay on Courses carrying them (BR-160) |
| `DELETE` | `/taxonomy/terms/{id}` | **Refused when referenced by ≥1 Course**; retirement offered instead (BR-160) |
| `PUT` | `/courses/{id}/taxonomy` | Admin override of any Course's assignment (BR-158) |

Every mutation is audited like other privileged catalogue actions (BR-158, FR-037).

## Applies to every route in this contract

- Decision through `identity.Authorize` only. No second decision point (FR-041).
- Audit row in the same transaction as the change, `module = CATALOG_AND_AUTHORING` (FR-043).
- Non-empty reason where the rule requires one — enforced by schema, not by handler goodwill.
- **No control degrades silently.** A missing dependency refuses the request; it does not proceed
  with a default (FR-044).
