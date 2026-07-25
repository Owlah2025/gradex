# M1 — Combined Architecture Baseline

> Milestone: M1 — Platform architecture baseline
> Assembled: 2026-07-28 (Day 6, Must 1)
> Builder verdict: **RECOMMEND APPROVE**
> Developer approval: _pending — see [Sign-off](#sign-off)_

This record combines three separately approved designs into one baseline and states what July 29
onward may treat as frozen. It does not restate the designs; it names their exact approved commits,
records the cross-design reconciliation, and carries forward the obligations that reconciliation
exposed.

## 1. Constituent designs

| Design | Document | Approved commit | Independent review |
|---|---|---|---|
| Platform architecture | [2026-07-25](../superpowers/specs/2026-07-25-platform-architecture-design.md) | `c9c2238` | 0 critical, 0 high |
| Domain, data, and state | [2026-07-26](../superpowers/specs/2026-07-26-domain-data-state-design.md) | `2e4f3e1` | 0 critical, 0 high; every disposition resolved |
| API, security, and integration | [2026-07-27](../superpowers/specs/2026-07-27-api-security-integration-design.md) | `6862db5` | Range `1a388cb..d6b4991`, verdict `APPROVE`, 0 findings |

Together these are the baseline. Where a later implementation decision disagrees with them, the
design wins until the design is explicitly amended and re-approved.

## 2. Cross-design reconciliation

Checked for conflicts between the three documents rather than re-reviewing each in isolation. The
layering holds: the architecture assigns responsibility to containers and modules, the domain design
assigns authority to tables and state machines, and the API design assigns externally visible
contracts to those same boundaries. No case was found where two documents assign the same authority
to different owners, or where the API design exposes a contract the domain design cannot satisfy.

Two clarifications are recorded because they are load-bearing for July 29 and are currently implied
rather than stated.

### 2.1 The bootstrap Administrator needs an execution home

§4.5 requires the bootstrap operation to run "never by an HTTP endpoint or ordinary application
worker." The architecture's runtime-container table (§5) lists only the frontend, API, worker,
PostgreSQL, Redis, and object storage/CDN — none of which may host it. The architecture does provide
the right execution class elsewhere: §10 states that "schema migrations run as a controlled one-off
release job."

**Clarification:** the bootstrap Administrator operation executes in that same controlled one-off
release-job class, as its own entrypoint (a third `cmd/`), not as an API route and not as a worker
task. It is not itself a migration — §4.5 forbids the plaintext password from entering one. This
resolves an ambiguity that would otherwise be resolved by whoever writes the code first.

### 2.2 Three pieces of state that §4.5 requires and the domain design does not enumerate

These are gaps in the July 26 table inventory, not contradictions in §4.5. They are recorded as
implementation obligations for the Identity slice so July 29 builds them deliberately.

| Obligation | Why it is not already covered |
|---|---|
| A constrained `PASSWORD_CHANGE_REQUIRED` marker | §4.5 creates the Admin as "verified `ACTIVE` ... and `PASSWORD_CHANGE_REQUIRED`", so this is *not* an `accounts.status` value — the Account is already `ACTIVE`. Neither `accounts` nor `password_credentials` in domain §4.2 carries a forced-change flag. Domain §2 requires constrained state values owned by migrations, so this needs an explicit constrained column. |
| A singleton bootstrap-operation marker | §4.5 must "prove no bootstrap operation has completed" under lock. Domain §4.2 lists no such table. `idempotency_records` cannot serve: domain §2.2 defines it as "shared infrastructure, not a substitute for domain uniqueness," and singleton-ness is exactly domain uniqueness. This needs its own constrained record. |
| Capability restriction for the temporary credential | §4.5 requires the bootstrap credential to authenticate "only into a restricted password-change session." Domain §4.2 `sessions` carries no capability-restriction column. **Resolution: derive the restriction from the account marker above, not from new session state.** Every module policy under §6.1 must therefore consult it and deny by default, which makes this an authorization-surface requirement of the July 29 slice rather than a schema one. |

## 3. Focused implementation-readiness review — §4.5 and §7.1

Scope was deliberately narrow, per the developer's Day 6 decision: check the two Codex-authored,
post-`6862db5` sections for contradiction with the locked architecture, insecure defaults, or
unimplementable invariants. Not a reopening of the design.

### 3.1 §4.5 Secure bootstrap Administrator

| Required property | Result | Evidence in §4.5 |
|---|---|---|
| No permanent default password | **Pass** | Initial password comes only from the approved secret manager or equivalent one-time injection; plaintext never reaches Git, migrations, process arguments, logs, telemetry, Audit, support tooling, or the database; Identity stores only the Argon2id hash. |
| Disabled after bootstrap, or idempotently unusable | **Pass** | One transaction locks a singleton marker and proves both that no bootstrap operation has completed and that no Admin Account exists. A retry with the same operation ID returns the recorded result; any later or differently fingerprinted attempt fails closed and "cannot mint another Admin." |
| Requires external secret/configuration input | **Pass** | Requires the dedicated deployment principal, an explicit production configuration gate, a stable operation ID, and the normalized Admin email. |
| Fails closed in production when misconfigured | **Pass** | The production configuration gate is explicit, and any failure rolls the entire transaction back rather than leaving a partial Admin. |
| Immutable single-role Admin via an explicit privileged path | **Pass** | Creates an immutable `ADMIN` role, consistent with domain §4.1: no `account_roles` table, no role-update command, `accounts.role` excluded from ordinary updates and protected by an immutable-role trigger. |
| Produces security/Audit evidence | **Pass** | Writes immutable Identity security evidence and required Audit inside the same transaction. |
| Does not bypass session, recent-auth, or normal Admin creation afterwards | **Pass** | Password change "uses the normal recent-authentication, credential/session rotation, Audit, and notification rules"; additional Admins come only through the approved invitation workflow. Until the change succeeds, the Account cannot use Admin, financial, security, retention, provider, or content-management capabilities. |

**No contradiction, insecure default, or unimplementable invariant found. §4.5 stays locked as
written.** The three obligations in §2.2 travel with it into the Identity slice.

### 3.2 §7.1 Malware-scanning adapter

| Required property | Result | Evidence |
|---|---|---|
| Objects stay quarantined until required scanning succeeds | **Pass** | §7.1: no public preview, protected download, HLS capability, Course approval, or `READY` transition is possible until the exact quarantined object has verified clean scan evidence. Matches domain §7.2 `AWAITING_UPLOAD → QUARANTINED → SCANNING → READY`. |
| Missing scanner configuration fails closed for publication/readiness | **Pass** | §7.1: scanner outage, unknown result, exhausted retry, or missing `LG-014` configuration all leave the Asset Version quarantined and unavailable. |
| Scan attempts immutable, externally retried through the PostgreSQL outbox | **Pass** | Domain §7.1 `scan_attempts` holds immutable result evidence; domain §7.3 step 2 creates stable-ID scan/processing outbox work inside the completion transaction. |
| `READY` references successful scan and processing evidence | **Pass** | Domain §7.2: "`READY` references the successful scan and, where applicable, processing attempt." |
| Scan failure or ambiguity cannot silently publish | **Pass** | Timeout or ambiguous response becomes `UNKNOWN` and is reconciled before resubmission; terminal rejection cannot be regressed by duplicate, delayed, or reordered observations; "no feature flag, Admin action, or direct storage URL can bypass successful scan evidence." |
| Scanner availability is outside the upload HTTP transaction | **Pass** | Domain §7.3 explicitly refuses to describe remote verification and the PostgreSQL transaction as one atomic operation; the scan is outbox work dispatched by the worker, which architecture §5 names as the owner of scanning. |

**No contradiction, insecure default, or unimplementable invariant found. §7.1 stays locked as
written.**

One related fact, recorded as a known migration input rather than a finding: the current
`backend/internal/video` slice enqueues transcoding directly to asynq with no scan step and no
outbox. Domain §7.3 already anticipates this — "the current `videos` table and direct-asynq handoff
are migration inputs, not a second production source of truth" — and §§21–24 define the cutover. It
belongs to the Media slice, not to a §7.1 amendment.

## 4. Baseline status

Both provisionally accepted sections are now reviewed and remain locked. No design amendment was
required, so the approved commits in §1 are unchanged by this review.

Carried forward into the slice register and the Identity slice:

1. constrained `PASSWORD_CHANGE_REQUIRED` marker (§2.2);
2. singleton bootstrap-operation marker (§2.2);
3. bootstrap capability restriction derived from the marker and enforced by every §6.1 module policy
   (§2.2);
4. bootstrap Administrator implemented as a controlled one-off release job in its own `cmd/`
   entrypoint (§2.1).

## Sign-off

- **Builder (Claude):** recommend approve. Cross-design reconciliation found no conflicting
  authority; both focused reviews passed every required property; four obligations are recorded
  above rather than left to be discovered during implementation.
- **Developer/product owner:** _pending._ M1 is not approved until this line is signed. A builder
  recommendation is not an approval.
- **Independent reviewer (`agy`):** this record is inside the Day 6 commit range and is reviewed with
  it at end of day.
