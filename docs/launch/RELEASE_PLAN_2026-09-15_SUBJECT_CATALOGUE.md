# Production Release Plan — Subject Catalogue / Demand (D-106)

> **SUPERSEDED for current release execution.** Live production is already at revision
> `776f02543b105fe428e80627ed52342304dc01f2`, clean schema 38, with 15 institutions. Do not run
> the schema-37 → 38 procedure in this document. Use
> [`RELEASE_PLAN_2026-09-18_STUDENT_UI_EMAIL.md`](RELEASE_PLAN_2026-09-18_STUDENT_UI_EMAIL.md) for
> the current Student UI/Auth/Email release framing and the same existing Hostinger tooling.

**Status:** RE-DERIVED FROM THE LIVE PRODUCTION BASELINE. AWAITING INDEPENDENT RELEASE-INTEGRITY
REVIEW AND A FRESH G0b.

One execution attempt has been made against the previous revision of this document. CP-1 passed its
identity and docs-only gate and the offline manifest validation, then **stopped fail-closed** at the
production baseline check: this plan assumed production schema 34, and live read-only evidence
proves production is already at clean schema **37**. That stop is the gate working as designed, not
a failed deployment. **No production mutation occurred** — CP-2 was never started, CP-3 was never
taken, and the application was never stopped.

Every command below is written to be read, approved, and then run by a human operator.

**Approved software head:** `e2223d7f01197c79a38f3568dfd25495fc5af163`
**Branch:** `catalog-seed-ibntohamy-20260914`
**Worktree at planning time:** clean

### Operative production baseline (live read-only evidence, 2026-09-15)

This table, not `STATUS.md`'s prior narrative, is the baseline every expected value below is derived
from. Where documentation and live production disagreed, **live production was treated as
authoritative**.

| Fact | Value |
|---|---|
| Production application revision | `4e7ddcdbadda86d535f7a3663d9405a628218e5f` |
| Production schema | **37, clean** (`37 \| f`) |
| Migrations already applied | `0035`, `0036`, `0037` |
| Migration pending | `0038_subject_demand_signals` only |
| `subject_demand_signals` | absent |
| Institutions | 1 (`kuwait-university`) |
| Kuwait University Subjects | 84 |
| `identity_trusted_devices` rows | 8 — Device Trust has been used; see §Rollback matrix for what this does and does not prove about the schema-37 floor |
| `/healthz`, `/readyz` | `200`; postgres / redis / schema all `ok` |

**This is a schema 37 → 38 release applying exactly one pending migration.** `0035`, `0036` and
`0037` are already in production. They are neither pending nor newly applied by this release, and no
command in this document applies any of them by hand.

### Release identity — two different things, deliberately

This document names **two** identities and never conflates them.

| Identity | Value | What it is for |
|---|---|---|
| **Approved software head** | `e2223d7f01197c79a38f3568dfd25495fc5af163` (fixed) | The commit whose *deployable content* is independently approved. Used only as the baseline for the CP-1 docs-only allowlist audit. Never used to build, tag, label, or select an artifact. |
| **`RELEASE_SHA`** | `$(git rev-parse HEAD)`, captured at CP-1 | The immutable identity of the artifact actually built and deployed. Every build, tag, provenance label, export path, host drop path, `release.env` and `apply-release` check resolves to this one value. |

**Why this plan contains no literal release SHA.** `release.sh build` derives the release identity
from the checked-out Git HEAD (`current_revision()` is `git rev-parse HEAD` behind a clean-worktree
refusal). Any literal final SHA written into this document becomes stale the moment the document
itself is committed — the commit changes HEAD, so the document would name a revision the tooling
would not build. That is a self-referential trap with no fixed point, and the previous revision of
this plan fell into it twice. The fix is structural: the document derives the identity from the same
source the tooling does, and pins only the *approved software head*, which is fixed and never
changes when documentation is committed.

**What makes that safe.** Documentation commits move HEAD but must not move the artifact. CP-1
proves that fail-closed with an explicit allowlist: everything between the approved software head
and HEAD must be one of exactly two documentation files, or the release stops.

**What it contains, and the verdict on each part:**

| Part | Range | Independent verdict |
|---|---|---|
| D-106 Subject Catalogue / Demand tranche | `4e7ddcd6..c908168a` (15 commits, first `7f18674`) | APPROVED WITH FINDINGS — 0 Critical / 0 High / 0 Medium / 2 Low (non-blocking) |
| D-103 remediation, first pass | `c908168..7d3ae73` | REJECTED (superseded) |
| D-103 remediation, second pass | `7d3ae73..e2223d7` | **APPROVED** — H1, H2 and M1 confirmed closed |

The 2 Low D-106 findings are **not** remediated in this release.

Historical review SHAs (`c908168`, `7d3ae73`, `e2223d7`, `4e7ddcd`, `61142de`) appear below as
evidence of what was reviewed. Only `e2223d7`, as the approved software head, carries any operative
meaning — and only as the CP-1 audit baseline. No command in this document selects an artifact by a
literal SHA.

---

## 0. Three findings that change the shape of this release

These were discovered during preflight tracing and must be resolved by the Product Owner before the
release can proceed as written.

### F-1 — This is a 37 → 38 release. The earlier 34 → 38 framing is superseded by live evidence.

The previous revision of this plan, and `STATUS.md` alongside it, recorded the production base as
`b8dea967196de68914440b2092cd80daf85d9546` at schema **34**, application-only, no migration applied.
**That record was stale.** CP-1's read-only baseline check found production at
`4e7ddcdbadda86d535f7a3663d9405a628218e5f`, schema **37 clean**, healthy, and serving.

#### Reconciling the production history

What `4e7ddcd` is, established from repository evidence alone:

- It is the tip of the range `b8dea96..4e7ddcd` (18 commits) and an ancestor of the current
  release-packaging HEAD.
- Its own subject is `feat(landing): polish navbar and hero presentation`, but the range beneath it
  carries three schema-bearing tranches: **D-103** media work leases (`0035`), **D-104** Bundles V1
  and Catalogue Offers V1 (`0036`), and **D-105** Student device trust (`0037`).
- At `4e7ddcd`, `backend/internal/db/schema.go` has
  `MaxSchemaVersion = StudentTrustedDeviceSchemaVersion` = **37**, and the embedded migration set
  ends at `0037`. `0038_subject_demand_signals` first enters history one commit series later, at
  `c7ab787` on 2026-09-14.

So schema 37 *was* the intended resulting schema of that payload, and the live production state is
internally consistent with it in both bookkeeping and physical shape: the `0035` columns on
`media_asset_versions`, the `0036` `bundles` table, and the `0037` `identity_trusted_devices` and
`identity_device_replacement_state` tables are all present, while `subject_demand_signals` is
absent. There is no evidence of a falsified or hand-edited `schema_migrations` marker.

**Authorization cannot be established from repository evidence, and is not invented here.** No
`STATUS.md` entry, no file under `docs/launch/evidence/`, no decision record in `docs/DECISIONS.md`
and no commit in this repository records a production deployment of `4e7ddcd`, an approval verdict
against it, or a migration run to 37. The opposite is recorded: the `2026-09-11` entry states that
the combined Bundles/Offers + D-105 candidate "requires fresh independent approval before push or
production deployment", and **every** `STATUS.md` entry from `2026-09-11` through the fourth pass of
`2026-09-15` asserts "Production has not been touched."

That assertion was false from approximately 2026-09-13 onward. Host timestamps
(`incoming/` and `runtime.env` last written 2026-09-13) and the container uptime observed at CP-1
(~46 hours) place the deployment on 2026-09-13, which is also `4e7ddcd`'s commit date.

**Why `STATUS.md` still described production as `b8dea96` / schema 34:** it was simply never updated
after that deployment, and each subsequent entry restated the stale "production has not been
touched" line unchanged. This is documentation drift. Whether the deployment itself was authorized
is a question this document **cannot** answer from repository evidence, and it is recorded here as
open rather than assumed either way.

**What this changes for the release, and what it does not.**

- The migration path is now `37 → 0038 → 38`. One pending migration, not four.
- `0035`, `0036` and `0037` must never be described as pending or newly applied by this release.
- The D-106 software being shipped is unchanged, and so is its approval status.
- The schema-37 rollback floor moves from hypothetical to *credible*: Device Trust has been used in
  production (`identity_trusted_devices` has rows). That is not the same as proving the floor is
  currently in force — see §Rollback matrix for the mechanism, the evidence, and the gap between
  them.

**What this does not resolve.** The reconciliation above is release *planning*, not approval. The
re-derived 37 → 38 procedure has not been independently reviewed, and the prior Product Owner G0b
approved a 34 → 38 payload that does not exist. Both are re-established at §GATE G0.

### F-2 — There is still no zero-downtime path. A hard outage window remains mandatory.

The compatibility analysis is re-derived against the **actually deployed** binary, not the one
`STATUS.md` assumed. The conclusion does not change; only the numbers do.

- `backend/cmd/api/main.go` — `requiredSchemaVersion()` returns `SubjectDemandSignalSchemaVersion` = **38**.
  The D-106 routes are mounted in this candidate and query `subject_demand_signals`, which arrives in
  38, so the floor is 38 and not 37.
- `backend/internal/db/schema.go` at the release candidate — `MaxSchemaVersion` = **38**.
- `backend/cmd/worker/main.go` — worker requires `MediaWorkLeaseSchemaVersion` = **35**, already met.
- The **currently deployed** production binary (`4e7ddcd`) has `MaxSchemaVersion` = **37** and
  refuses readiness against anything higher (`schema.go` `CheckSchemaAtLeast`, fails closed).

So the compatibility truth for this release is:

```
old API (4e7ddcd, deployed): schema 37 only  — floor 37, ceiling 37
new API (this candidate):    schema 38 only  — floor 38, ceiling 38
```

At `4e7ddcd`, `cmd/api/main.go`'s `requiredSchemaVersion()` returns `StudentTrustedDeviceSchemaVersion`
= 37 and `MaxSchemaVersion` is also 37, so the deployed API's floor and ceiling are both 37: it
serves **37 only**.

**Do not read the worker's range as the API's.** The deployed worker's floor is
`MediaWorkLeaseSchemaVersion` = 35 (with a ceiling of 37), so the worker tolerates 35..37. That is
worker compatibility, and it has no bearing on whether the API can serve a given schema. An earlier
revision of this document conflated the two and stated the API range as `35..37`. Readiness is
decided per binary; the API is the binding constraint here.

The two ranges are adjacent but **disjoint**. The new API's floor and ceiling are both 38, so no
ordering of binary and migration steps produces a rolling overlap. Zero downtime is **not**
available and is not claimed. `host.sh apply-release` remains the wrong tool to reach 38 — it uses
`--no-deps` and never runs migrations, so it would recreate the new API against schema 37 and die at
`wait_for_status api healthy`.

`docs/launch/RUNBOOK.md` (D-103 section) states that **the old worker and the new worker must never
run concurrently**. That rule is unchanged and remains binding: the old worker at `4e7ddcd` and the
new worker share the `0035` media work-lease tables, so an overlap would put two worker generations
on the same lease rows. The stricter rule stands.

**The safe order, stated explicitly:**

```
verified backup (CP-3)
  -> stop old worker, api, frontend (CP-4; outage begins)
  -> apply 0038 (CP-5)
  -> verify clean schema 38 (CP-5)
  -> start new backend (CP-6)
  -> health / readiness / Subject API (CP-7)
```

Infrastructure — `postgres`, `redis`, `edge` — stays up throughout. The public edge answers 502/503
for application requests for the duration; no maintenance page is configured.

**One genuine improvement over the 34 → 38 framing.** Clean schema 37 is now a *servable* state: the
deployed `4e7ddcd` binaries serve it. If CP-5 fails with the marker still clean at 37, restarting the
old application ends the outage with no data change and no schema rollback. Under the old 34 → 38
plan the equivalent failure left the database at a version no deployed binary could serve.

### F-3 — `catalog-import` is not present in any production image.

`backend/Dockerfile` builds `gradex-api`, `gradex-worker`, `gradex-migrate` into the runtime image,
and `gradex-e2e-seed`, `gradex-storage-fixture`, `gradex-bootstrap-admin` into the proof image.
`cmd/catalog-import` is built into **none** of them.

Adding it would be a production change outside the reviewed tranche and would require its own
review. Do not do that for this release.

**The reviewed importer is still reachable in production**, through the Admin HTTP route that runs
the identical `importer.Run` code path:

```
GET  /api/v1/admin/academic/manifests
POST /api/v1/admin/academic/institutions/{institutionId}/import   {"manifest":"<id>","mode":"dry_run"|"apply"}
```

For an institution that does not exist yet, `{institutionId}` is the nil UUID
`00000000-0000-0000-0000-000000000000` — the established convention in
`backend/internal/httpapi/academic_import_integration_test.go`. The handler treats "institution not
found" as the normal first-import case and lets the manifest create it. All 14 new institutions use
the nil UUID.

Difference from the CLI: the audited actor is the acting **Admin** account, not the
`system:catalog-import` SYSTEM principal. This is deliberate in the reviewed code and is the correct
audit record for an operator-driven import.

---

## 1. What would be deployed

### Images (built locally, loaded on the host — see CP-2)

| Image | Tag | Contents |
|---|---|---|
| `gradex-backend` | `hostinger-$SHORT` | `gradex-api`, `gradex-worker`, `gradex-migrate`, embedded migrations `0001`–`0038` (of which only `0038` is pending in production), embedded manifests |
| `gradex-frontend` | `hostinger-$SHORT` | Next.js build incl. the new Subject routes |
| `gradex-backend-proof` | `hostinger-$SHORT` | proof/seed tooling; not started by this release |

All three are tagged `hostinger-$SHORT` and carry
`org.opencontainers.image.revision=$RELEASE_SHA`. `release.sh record` and `host.sh apply-release`
both verify that label against the release SHA they derive, so a mismatch fails the release rather
than shipping a mislabelled image.

### Backend components new or changed in the tranche

- `backend/internal/db/migrations/0038_subject_demand_signals.{up,down}.sql` — one additive table
  `subject_demand_signals`, one partial unique index, two partial read indexes. Touches no existing
  table, column, type or index.
- `backend/internal/db/schema.go` — `SubjectDemandSignalSchemaVersion` = 38, `MaxSchemaVersion` = 38.
- `backend/internal/httpapi/catalog_public_routes.go` — anonymous
  `GET /api/v1/catalog/subjects` and `GET /api/v1/catalog/subjects/:institutionSlug/:value`.
- `backend/internal/httpapi/academic_routes.go` —
  `GET /api/v1/admin/academic/subject-demand` (Admin counts),
  `GET|POST /api/v1/me/subject-demand`, `DELETE /api/v1/me/subject-demand/:subjectId` (Student).
- `backend/internal/academic/manifest/data/**` — 14 new `manifest.yaml` + `sources.yaml` pairs.
  `kuwait-university/` is **unchanged** across the whole deployed range
  (`git diff --stat 4e7ddcd.."$RELEASE_SHA" -- backend/internal/academic/manifest/data/kuwait-university`
  reports no changes). Note that `4e7ddcd` is not merely a review landmark here: it is the revision
  actually running in production, so this diff is exactly "what changes for Kuwait University
  between what is deployed and what would be deployed", and the answer is nothing.

### Frontend routes new in the tranche

- `frontend/src/app/[locale]/subjects/page.tsx`
- `frontend/src/app/[locale]/subjects/[institutionSlug]/[value]/page.tsx`
- `frontend/src/app/[locale]/admin/subject-demand/page.tsx`

Locales are `ar` and `en` (`frontend/src/lib/identity/restricted-navigation.ts`). The locale prefix
is optional at the edge — `/subjects` and `/en/subjects` both serve.

### Deploy tooling

`git diff --stat 61142de "$RELEASE_SHA" -- deploy` reports a single changed file,
`deploy/env/production-like.env.example` (+4 lines), which belongs to the S12 production-like
topology and is not used by the Hostinger host. `deploy/hostinger/compose.yml` and
`deploy/hostinger/host.sh` are **byte-identical** to the pinned project root, so the existing
compose project root stays valid.

---

## 2. Host facts assumed by every command below

```
host            deploy@186.241.16.111   (gradex-vps)
compose project gradex-production
project root    /home/deploy/gradex-release-61142dedf146     (pinned; do not move)
host state      /home/deploy/gradex-production
runtime env     /home/deploy/gradex-production/runtime.env   (mode 0600)
release drop    /home/deploy/gradex-production/incoming/<full-sha>/
database        gradex_production        role gradex
public origin   https://gradexcourses.com
```

Production state as verified read-only at CP-1, which every expected value below assumes:

```
deployed application   4e7ddcdbadda86d535f7a3663d9405a628218e5f   (MaxSchemaVersion 37)
schema_migrations      37 | f
pending migration      0038_subject_demand_signals   (the only one)
institutions           1        (kuwait-university)
Kuwait University      84 Subjects
identity_trusted_devices   8 rows   -> Device Trust in use (see Rollback matrix)
```

Every `host.sh` invocation must export all four of `GRADEX_HOST_STATE_DIR`, `GRADEX_HOST_ENV_FILE`,
`GRADEX_HOST_PROJECT`, `APP_ENV=production`. The script defaults to `/var/lib/gradex` and project
`gradex-staging`; omitting them silently targets the wrong stack.

Shared prelude for every step, run once per shell on the host:

```bash
export GRADEX_HOST_STATE_DIR=/home/deploy/gradex-production
export GRADEX_HOST_ENV_FILE=/home/deploy/gradex-production/runtime.env
export GRADEX_HOST_PROJECT=gradex-production
export APP_ENV=production
# The exact value CP-1 captured and CP-2 verified. Carried across, never re-derived here: the
# host checkout is pinned at 61142de and its HEAD is NOT the release identity.
export RELEASE_SHA=<value captured at CP-1>
export SHORT="${RELEASE_SHA:0:12}"
cd /home/deploy/gradex-release-61142dedf146
```

Raw compose invocations mirror `host.sh`'s own wrapper exactly:

```bash
gxcompose() {
  sed -n '1,999p' deploy/hostinger/compose.yml |
    docker compose --file - \
      --project-directory /home/deploy/gradex-release-61142dedf146/deploy/hostinger \
      --project-name gradex-production "$@"
}
```

---

## 3. Execution order

```
CP-1  preflight + SHA verification + live baseline gate   (local + host, read-only)
CP-2  build, export, transfer, load images    (local + host, no production mutation)
GATE  G0a — independent release-integrity review of this re-derived 37→38 procedure
GATE  G0b — Product Owner approves the re-derived release (only after G0a)
CP-3  production backup + artifact proof      (first host write; reversible)
GATE  G1 — Product Owner approves the irreversible sequence
CP-4  stop old application                    (start of outage)
CP-5  migration 37 → 38, one migration        (IRREVERSIBLE)
CP-6  start new backend (api + worker)
CP-7  backend health / readiness / Subject API smoke
GATE  G2 — data mutation approval
CP-8  manifest import: 14 × dry-run, then 14 × apply   (IRREVERSIBLE)
CP-9  import verification + idempotence + KU isolation
CP-10 frontend deploy                         (end of outage)
CP-11 browser smoke, EN + AR
CP-12 Student demand request/withdraw
CP-13 Admin demand count
CP-14 negative paths: malformed/unknown Subject → 404
CP-15 served vs unserved Subject behaviour
CP-16 final health/readiness + log inspection
```

---

## CP-1 — Preflight, release identity capture, and docs-only audit (local, read-only)

This is where the release gets its identity. Nothing downstream may name a SHA that did not come
from here.

**1. Capture the identity from the checked-out HEAD**, the same source `release.sh build` uses:

```bash
cd /home/owlah/worktrees/gradex-catalog-seed
export APPROVED_SOFTWARE_HEAD=e2223d7f01197c79a38f3568dfd25495fc5af163
export RELEASE_SHA="$(git rev-parse HEAD)"
export SHORT="${RELEASE_SHA:0:12}"
printf 'release identity: %s (short %s)\n' "$RELEASE_SHA" "$SHORT"
```

**2. Run the fail-closed gate.**

> **How to run every gate block in this document.** Save it and run it with `bash`, or paste it into
> a non-interactive shell. Each gate ends in `exit 1` on refusal, so the block's own exit status is
> the verdict — there is no trailing diagnostic to inspect, and nothing after a refusal executes.
> The exports from step 1 are `export`ed, so a child `bash` inherits them. Pasting a gate directly
> into an interactive shell also fails closed; it simply ends that shell.

```bash
if ! (
  set -euo pipefail

  # (a) A dirty worktree has no releasable identity. release.sh refuses one too; this
  #     refuses earlier, before anything has been built.
  [ -z "$(git status --porcelain=v1)" ] || { echo 'REFUSE: worktree is not clean'; exit 1; }

  # (b) The captured identity must be a full 40-hex commit that actually resolves.
  [[ "$RELEASE_SHA" =~ ^[0-9a-f]{40}$ ]] || { echo 'REFUSE: RELEASE_SHA is not a full SHA'; exit 1; }
  [ "$(git rev-parse --verify "$RELEASE_SHA^{commit}")" = "$RELEASE_SHA" ] ||
    { echo 'REFUSE: RELEASE_SHA does not resolve to a commit'; exit 1; }
  [ "$SHORT" = "${RELEASE_SHA:0:12}" ] || { echo 'REFUSE: SHORT is not derived from RELEASE_SHA'; exit 1; }

  # (c) The deployable content must be identical to the independently approved software head.
  #     An explicit ALLOWLIST, not an exclusion list: anything not named here stops the release,
  #     including a path nobody thought to exclude.
  ALLOWED='docs/launch/RELEASE_PLAN_2026-09-15_SUBJECT_CATALOGUE.md
docs/launch/STATUS.md'
  UNEXPECTED="$(git diff --name-only "$APPROVED_SOFTWARE_HEAD".."$RELEASE_SHA" |
                  grep -vxF "$ALLOWED" || true)"
  [ -z "$UNEXPECTED" ] || {
    printf 'REFUSE: non-documentation change since the approved software head:\n%s\n' "$UNEXPECTED"
    exit 1
  }

  # (d) Belt and braces on the artifact-bearing trees, stated positively so the intent is
  #     readable even if the allowlist is ever edited carelessly.
  ARTIFACT="$(git diff --name-only "$APPROVED_SOFTWARE_HEAD".."$RELEASE_SHA" |
                grep -E '^(backend/|frontend/|deploy/|scripts/|tools/|specs/)' || true)"
  [ -z "$ARTIFACT" ] || {
    printf 'REFUSE: artifact-bearing path changed since the approved software head:\n%s\n' "$ARTIFACT"
    exit 1
  }

  # (e) The one deploy-tooling fact the pinned compose root depends on.
  git diff --stat 61142de "$RELEASE_SHA" -- deploy

  echo "PROCEED: $RELEASE_SHA carries the approved software content of $APPROVED_SOFTWARE_HEAD"
); then
  echo 'REFUSE: CP-1 preflight failed' >&2
  exit 1
fi
```

Expected on success — note that no line names a literal release SHA, because the identity is
whatever HEAD is at execution time, and that the block exits `0`:
```
 deploy/env/production-like.env.example | 4 ++++
 1 file changed, 4 insertions(+)
PROCEED: <RELEASE_SHA> carries the approved software content of e2223d7…
```

On refusal the block prints the specific `REFUSE:` line, then `REFUSE: CP-1 preflight failed`, and
**exits non-zero**. There is deliberately no trailing `echo` of `$?`: a successful diagnostic
command after a failed gate would reset the block's exit status to zero and turn a refusal into a
silent pass.

**STOP on any `REFUSE`.** In particular, a non-empty `UNEXPECTED` or `ARTIFACT` list means the tree
has moved beyond the independently approved software content: the approval no longer covers what
would be built, and the release requires a new review rather than a new build.

The two allowlisted files are the release documentation itself. They are expected to differ, because
committing this plan is what moves HEAD past the approved software head in the first place.

Validate every manifest offline (pure function of checked-in data, no database):

```bash
cd backend && for id in $(go run ./cmd/catalog-import -list); do
  go run ./cmd/catalog-import -mode=validate -manifest="$id"
done
```

Expected: 15 lines, each ending `is valid: …`. Counts must match §CP-8's table exactly.

Read the live production schema and catalogue baseline, and **gate on it fail-closed**. The previous
revision of this document only printed these values for a human to eyeball; that is how a stale
baseline survived to execution time. It is now an assertion.

```bash
if ! (
  set -euo pipefail

  BASELINE="$(ssh deploy@186.241.16.111 \
    'docker exec -i gradex-production-postgres-1 psql -U gradex -d gradex_production \
       --no-psqlrc --tuples-only --no-align --set ON_ERROR_STOP=1' <<'SQL'
SELECT version || '|' || dirty FROM schema_migrations;   -- boolean::text => 'false'/'true'
SELECT 'institutions|' || count(*) FROM institutions;
SELECT 'catalogue|' || i.slug || '|' || count(s.id)
  FROM institutions i LEFT JOIN subjects s ON s.institution_id = i.id
 GROUP BY i.slug ORDER BY i.slug;
SELECT 'demand_table|' || coalesce(to_regclass('public.subject_demand_signals')::text, 'absent');
SQL
  )"

  EXPECTED='37|false
institutions|1
catalogue|kuwait-university|84
demand_table|absent'

  [ "$BASELINE" = "$EXPECTED" ] || {
    printf 'REFUSE: production is not at the re-derived baseline.\n--- expected ---\n%s\n--- actual ---\n%s\n' \
      "$EXPECTED" "$BASELINE"
    exit 1
  }

  REV="$(ssh deploy@186.241.16.111 \
    'docker inspect --format "{{index .Config.Labels \"org.opencontainers.image.revision\"}}" gradex-production-api-1')"
  [ "$REV" = "4e7ddcdbadda86d535f7a3663d9405a628218e5f" ] || {
    echo "REFUSE: deployed API revision is '$REV', not the recorded baseline 4e7ddcd…"
    exit 1
  }

  echo 'PROCEED: production is at clean schema 37 with exactly one pending migration (0038)'
); then
  echo 'REFUSE: CP-1 production baseline check failed' >&2
  exit 1
fi
```

Expected on success:
```
PROCEED: production is at clean schema 37 with exactly one pending migration (0038)
```

**On the `false` rather than `f`.** The expected marker here is `37|false`, not `37|f`. The two
renderings are both correct and they are not interchangeable: psql's own column formatter prints a
boolean as `f`/`t`, but `version || '|' || dirty` concatenates, which casts the boolean to `text`,
and PostgreSQL's `boolean::text` is `false`/`true`. This gate concatenates, so it must expect
`false`. The read-only baseline query further down uses separate columns and still shows `37 | f`.
Comparing against the wrong rendering would make a correct production baseline refuse, which is a
fail-closed failure but a false one. Do not "normalise" these two spellings into one; each matches
the query that produces it.

**STOP on any `REFUSE`, and re-derive the plan again rather than reinterpreting it.** Every expected
count downstream — the migration count at CP-5, the `84` Subject baseline at CP-7, the `15`/`329`
totals at CP-9 — is derived from exactly this baseline. In particular:

- `dirty` is `t` → production is mid-migration. Stop; this is an incident, not a release.
- version is **below** 37 → some part of `0035`–`0037` is missing; this plan does not cover applying
  them and must be re-derived.
- version is **38 or above** → `0038` is not pending; either this release already ran, or something
  else did. Stop.
- `subject_demand_signals` already exists at version 37 → bookkeeping and physical shape disagree.
  Stop; do not migrate.
- institutions is not 1, or any institution other than `kuwait-university` exists → the import
  expectations at CP-8/CP-9 are invalid.
- the deployed revision is not `4e7ddcd…` → production moved again since this plan was re-derived,
  and the compatibility analysis in F-2 no longer describes the running binary.

**No production mutation occurs at CP-1.** Every command in this checkpoint is read-only.

---

## CP-2 — Build, export, transfer, load images

Local build (refuses a dirty worktree, stamps and verifies the revision label). `release.sh build`
derives its own revision with `git rev-parse HEAD` behind the same clean-worktree refusal CP-1 used,
so it and `$RELEASE_SHA` agree by construction — and the assertion below proves it rather than
assuming it:

```bash
cd /home/owlah/worktrees/gradex-catalog-seed

# No pipe: `release.sh build | tee` would report tee's exit status, so a failed build would
# read as success. Redirect, then show the log.
./deploy/hostinger/release.sh build >/tmp/gradex-release-build.log 2>&1 || {
  echo "REFUSE: release.sh build failed; see /tmp/gradex-release-build.log" >&2
  exit 1
}
cat /tmp/gradex-release-build.log

grep -qF "built release $RELEASE_SHA" /tmp/gradex-release-build.log || {
  echo "REFUSE: release.sh built a revision other than $RELEASE_SHA" >&2
  exit 1
}

./deploy/hostinger/release.sh export "$RELEASE_SHA" || {
  echo "REFUSE: release.sh export failed for $RELEASE_SHA" >&2
  exit 1
}
```

Expected, with `$RELEASE_SHA` standing for the identity CP-1 captured — the tooling prints the real
value, and this document deliberately does not predict it:
```
s12-hostinger-release: recorded checksum-addressed local images for release $RELEASE_SHA
s12-hostinger-release: built release $RELEASE_SHA
s12-hostinger-release: exported release $RELEASE_SHA with checksum into ignored state
```

The identity assertion is **fatal, not advisory**: on a mismatch the block exits non-zero and the
`export` step is never reached, so no artifact bearing the wrong revision can be produced or
transferred. That matters because everything downstream — tags, labels, `release.env`,
`apply-release` — trusts this one stamp.

The exported artifacts land in `deploy/.state/hostinger/releases/$RELEASE_SHA/`.

Transfer and load. **`apply-release` does not load images** — loading is a separate, explicit step:

```bash
ssh deploy@186.241.16.111 "mkdir -p /home/deploy/gradex-production/incoming/$RELEASE_SHA"
scp deploy/.state/hostinger/releases/$RELEASE_SHA/{release.env,images.tar.gz,images.tar.gz.sha256} \
    deploy@186.241.16.111:/home/deploy/gradex-production/incoming/$RELEASE_SHA/
ssh deploy@186.241.16.111 "cd /home/deploy/gradex-production/incoming/$RELEASE_SHA && \
  sha256sum --check images.tar.gz.sha256 && gunzip -c images.tar.gz | docker load"
```

Expected: `images.tar.gz: OK`, then three `Loaded image:` lines.

Verify the revision labels and the schema ceiling on the host:

```bash
ssh deploy@186.241.16.111 "docker image inspect --format '{{index .Config.Labels \"org.opencontainers.image.revision\"}}' \
  gradex-backend:hostinger-$SHORT gradex-frontend:hostinger-$SHORT gradex-backend-proof:hostinger-$SHORT; \
  docker run --rm --entrypoint gradex-migrate gradex-backend:hostinger-$SHORT max-version"
```

Expected: `$RELEASE_SHA` three times, then `38`. Any other value means the loaded images are not
the ones CP-2 built and audited — **STOP**.

Nothing in production has been mutated at this point.

---

## GATE G0 — independent release-integrity review, then Product Owner approval

G0 has two parts, in this order. Both are required before CP-3.

### G0a — independent review of the re-derived procedure (BLOCKING, NOT SATISFIED)

**The software position is unchanged and remains historically satisfied.** D-103 / migration `0035`
is independently **APPROVED**, with H1 (stale-operation lock ordering), H2 (database-time lease
authority) and M1 (per-cycle retry attempt accounting) confirmed closed. The D-106 Subject
Catalogue / Demand tranche is independently **APPROVED WITH FINDINGS** (2 Low, non-blocking). The
deployable software content at the approved software head `e2223d7` is approved, and everything
between it and the release-packaging HEAD is documentation-only, proven by the CP-1 allowlist audit.

**Nothing in this re-derivation changes a line of application code, migration, deploy tooling,
manifest, test, frontend or runtime configuration.** It is a documentation correction only.

**What is newly unreviewed is the procedure, not the product.** The previously reviewed release
procedure described a 34 → 38 migration from a baseline that does not exist. The procedure in this
revision is materially different in ways that bear directly on production safety:

1. The migration step applies one migration, not four.
2. The CP-1 baseline check is now a fail-closed gate rather than a printed value.
3. The failure semantics at CP-5 changed: clean 37 is a servable resting state, so a clean-marker
   migration failure is now fully recoverable by restarting the deployed application.
4. The rollback model changed: the schema-37 floor is active rather than prospective, and the
   generic 38 → 34 chain is withdrawn as a release recovery path.

G0a is satisfied only by a recorded independent reviewer verdict against one exact commit range,
with every Critical and High finding resolved. A review that produces no retrievable verdict is
`UNAVAILABLE`, not approval. **The builder does not grant G0a**, and the builder's own reconciliation
of the production history is not a verdict.

**Current state: NOT SATISFIED.**

### G0b — Product Owner release decision

**The previous G0b is superseded and must not be reused.** It approved, in writing, "applying
migrations `0035` `0036` `0037` `0038` sequentially" from schema 34. Three of those four were
already applied in production before that approval was given. The approval's factual premise does
not hold, so it is void as to this release — **not** because anything failed, but because the
baseline it described was discovered to be stale before any mutation occurred. Recording it as
superseded is bookkeeping, not blame: the fail-closed stop that discovered this is the control
working correctly.

A **fresh** G0b is required. Reached only after G0a is satisfied, the Product Owner confirms in
writing:

1. The actual production baseline is **clean schema 37** at application revision `4e7ddcd…`, and the
   prior 34 → 38 approval is superseded by that discovery.
2. **Exactly one migration is pending: `0038_subject_demand_signals`.** This release ships D-106
   only; D-103, D-104 and D-105 are already in production.
3. A mandatory application outage is accepted for 37 → 38 (F-2). Zero downtime is not available.
4. **Ordinary rollback below schema 37 must not be assumed available.** Device Trust has been used
   in production (`identity_trusted_devices` has rows), which makes the documented `0037`-down
   refusal conditions credible — though the captured preflight evidence did not prove either
   specific condition is currently present. Below-37 recovery therefore plans on the reviewed
   verified-backup → fresh-database restore → verify → repoint route (§Rollback matrix).
5. The 14-manifest catalogue import runs through the Admin HTTP route under an Admin audit actor,
   not the CLI SYSTEM actor (F-3): 14 institutions, 245 Subjects, `kuwait-university-launch-v1`
   excluded.
6. The reviewed non-blocking findings — the 2 Low D-106 findings and the classified test-harness
   flakes — ship unremediated.

Item 1 additionally requires a Product Owner decision this plan cannot make for them: **the
authorization status of the `4e7ddcd` production deployment is unestablished in repository
evidence** (F-1). That is an open governance question, and shipping on top of that baseline does not
retroactively settle it.

These are business and risk decisions, which is exactly the scope Product Owner approval covers.

**Without both G0a and a fresh G0b, stop here.**

---

## CP-3 — Production PostgreSQL backup with artifact verification

```bash
./deploy/hostinger/host.sh backup
```

What `create_backup` already enforces, so it does not need to be re-checked by hand
(`deploy/hostinger/host.sh:955`–`1017`):

- refuses to start from a dirty or invalid schema marker;
- `pg_dump --format=custom --no-owner --no-acl`, then **`[ -s "$partial_file" ] || die "backup is empty"`** — the explicit non-empty check;
- re-reads the schema marker after the dump and aborts if it moved mid-dump;
- records source record counts (`accounts|courses|course_access_invitations|entitlements|enrollments`) and refuses if unreadable;
- writes `sha256sum` sidecars for both the dump and the schema-state file, mode 0600;
- uploads an encrypted restic snapshot, confirms it is visible, runs a repository integrity check, applies retention, and re-confirms the snapshot survived retention.

Expected final line:
```
s12-hostinger: successful offsite backup marker updated for snapshot <SNAPSHOT_ID>
```

**Backup location:**
- local protected staging is cleaned on success; the durable artifact is the **encrypted offsite restic snapshot** `<SNAPSHOT_ID>`;
- snapshot id recorded at `/home/deploy/gradex-production/backups/latest.offsite.snapshot`;
- completion time at `/home/deploy/gradex-production/backups/latest.completed-at`.

**Independent readability proof of the artifact** — restore it into a disposable isolated
PostgreSQL container and assert against the source's recorded state:

```bash
./deploy/hostinger/host.sh restore
./deploy/hostinger/host.sh verify-restore
```

Expected:
```
s12-hostinger: restored encrypted offsite snapshot <SNAPSHOT_ID> into a fresh isolated PostgreSQL volume
s12-hostinger: restored encrypted snapshot <SNAPSHOT_ID>, schema 37|false, identity, Course, invitation provenance, Entitlement, and Enrollment passed
```

The schema reported here is **37**, matching the live production baseline at capture time. It is
read from the snapshot, not asserted by this document: `verify_restore` fails closed if the restored
schema state, or any of the five record counts, differs from what the source held at capture time.
That is the non-empty/readable proof. A restore reporting `34|false` would mean the snapshot is not
of this production database — **STOP**.

**Restore procedure (recovery, not drill).** `restore` targets the standalone
`gradex-restore-verify` container and its own volume — never the live database. To recover
production from this snapshot you restore into a fresh database and repoint, never `--clean` over
the live one:

```bash
./deploy/hostinger/host.sh restore <SNAPSHOT_ID>
./deploy/hostinger/host.sh verify-restore
# then, under supervision, promote the verified target or pg_restore its dump
# into a fresh database and repoint DATABASE_URL. Never restore over the live database.
```

**STOP if:** any line above fails. Do not proceed to CP-4 on an unverified backup.

---

## GATE G1 — Product Owner approval before the first irreversible mutation

CP-4 begins the outage and CP-5 is irreversible without the supervised schema-rollback procedure.
Required confirmations: backup snapshot id recorded and `verify-restore` green; outage window agreed;
operator present for the whole CP-4 → CP-10 sequence.

**Without this, stop here.**

---

## CP-4 — Stop the old application (outage begins)

Per `docs/launch/RUNBOOK.md`, old and new workers must never run concurrently.

```bash
set -a; . /home/deploy/gradex-production/runtime.env; set +a
gxcompose stop worker api frontend
gxcompose ps
```

Expected: `worker`, `api`, `frontend` show `exited`; `postgres`, `redis`, `edge` remain `running`
(or `healthy`).

The edge stays up and returns 502 for application requests for the duration. This is the
outage window.

**Rollback at this point:** `gxcompose start worker api frontend` — full, instant, no data change.
The schema is still clean 37, which the deployed `4e7ddcd` binaries serve, so this is a complete
return to the pre-release state.

---

## CP-5 — Migration 37 → 38 (IRREVERSIBLE)

**Exactly one migration is pending: `0038_subject_demand_signals`.** `0035`, `0036` and `0037` are
already applied in production and are not re-applied here; `migrate up` skips them as already
recorded. No command in this checkpoint names or applies an individual migration by hand — the
normal `migrate up` mechanism is used unchanged, and its expected behaviour is one applied migration.

Run the migration as a one-off release job against the **new** backend image:

```bash
set -a; . /home/deploy/gradex-production/runtime.env; set +a
export GRADEX_BACKEND_IMAGE=gradex-backend:hostinger-$SHORT
gxcompose up --detach migrate

# The logs are shown either way, but a failed migration is fatal here: CP-6 starts the new
# backend, and it must never run against a half-applied or dirty schema.
if ! gxcompose wait migrate; then
  docker logs "$(gxcompose ps --all --quiet migrate)"
  echo 'REFUSE: migrate did not complete successfully; do not proceed to CP-6' >&2
  exit 1
fi
docker logs "$(gxcompose ps --all --quiet migrate)"
```

Expected (`migrate` exits 0), with exactly one migration applied:
```
migrate up: version=38 dirty=false (supported; this build supports 2..38)
```

**Migration verification — both bookkeeping and physical shape:**

```bash
docker exec gradex-production-postgres-1 psql -U gradex -d gradex_production \
  --no-psqlrc --tuples-only --no-align \
  -c "SELECT version, dirty FROM schema_migrations;" \
  -c "SELECT to_regclass('public.subject_demand_signals');" \
  -c "SELECT count(*) FROM pg_indexes WHERE tablename = 'subject_demand_signals';" \
  -c "SELECT count(*) FROM subject_demand_signals;"
```

Expected:
```
38|f
subject_demand_signals
4
0
```

(4 = the primary-key index plus the three indexes the migration declares. `0` rows is the correct
initial state — no Student has raised demand yet.)

**STOP if the schema is not exactly clean 38.** Do not start CP-6 on anything else.

**STOP if `dirty` is `t`, and do not run `migrate up` again.** `requireClean` will refuse, and
stacking migrations onto a half-applied state is how a recoverable failure becomes an unrecoverable
one. **Do not automatically retry a failed migration** under any circumstances. Go to the CP-5 rows
in §Rollback matrix.

**A clean-marker failure is fully recoverable here.** If `migrate` exits non-zero but the marker is
still `37 | f`, the database is exactly where it started and the deployed `4e7ddcd` application
serves that schema. `gxcompose start worker api frontend` ends the outage with no data change and no
schema rollback. Diagnose afterwards, out of the outage window. This is a real improvement over the
superseded 34 → 38 framing, where the equivalent failure could strand the database at a version no
deployed binary could serve.

---

## CP-6 — Start the new backend

The manifest identity is verified **before** `apply-release` runs, in the same block, so a mismatch
terminates non-zero and the mutation never executes:

```bash
ssh deploy@186.241.16.111 \
  "grep -qxF 'GRADEX_RELEASE_SHA=$RELEASE_SHA' /home/deploy/gradex-production/incoming/$RELEASE_SHA/release.env" || {
  echo "REFUSE: release.env does not declare $RELEASE_SHA; apply-release NOT run" >&2
  exit 1
}

./deploy/hostinger/host.sh apply-release /home/deploy/gradex-production/incoming/$RELEASE_SHA/release.env
```

`apply_release` verifies the three revision labels against the release SHA, refuses `:latest` tags,
asserts the schema is clean and not newer than the image ceiling (38 ≤ 38), captures the Entitlement
provenance count, recreates `api`, `worker` and `frontend`, waits for health, re-asserts provenance
is unchanged, and persists the selection into `runtime.env`.

The manifest check above pins `apply-release` to the value CP-1 captured rather than to whatever
happens to be in the drop directory.

Expected:
```
s12-hostinger: application release $RELEASE_SHA is healthy on unchanged schema 38 (target max 38) and provenance
```

Note this single command also brings up the new frontend, which is CP-10. If the Product Owner
requires the frontend held back until after the import (CP-8), stop `frontend` immediately
afterwards and restart it at CP-10:

```bash
gxcompose stop frontend      # optional; defers CP-10
```

Holding the frontend back is the stricter reading of "minimise public exposure of a partially
configured feature", at the cost of a longer outage. The API alone exposes only
`/api/v1/catalog/subjects`, which would return the 84 Kuwait University Subjects — a correct, not
a broken, state. **Recommended: hold the frontend back.**

---

## CP-7 — Backend health, readiness, Subject API smoke

```bash
./deploy/hostinger/host.sh verify-core
```

Expected:
```
s12-hostinger: private verification passed: clean schema 38, application services healthy, authenticated verified-TLS Redis, and API readiness over the private network
```

Then, over the public edge:

```bash
curl -sS https://gradexcourses.com/healthz | jq .
curl -sS https://gradexcourses.com/readyz  | jq .
curl -sS -o /dev/null -w '%{http_code}\n' 'https://gradexcourses.com/api/v1/catalog/subjects?page=1&page_size=1'
curl -sS 'https://gradexcourses.com/api/v1/catalog/subjects?page=1&page_size=1' | jq '{total, returned: (.items|length)}'
```

Expected:
```
{"status":"ok", ...}
{"status":"ok","checks":{"postgres":"ok","redis":"ok","schema":"ok"}}
200
{"total":84,"returned":1}
```

`total` is the pre-import baseline from CP-1. Record the exact field names the API returns on the
first call and reuse them; the shape above is the expected shape, not a contract assertion.

**STOP if:** `/readyz` reports anything other than `ok` on all three checks, or the Subject browse
route is not `200`.

---

## GATE G2 — Product Owner approval before the catalogue data mutation

CP-8 writes 14 institutions and 245 Subjects. The importer **never deletes or retires by omission**,
so there is no un-import; reversing it means restoring the CP-3 backup.

**Without this, stop here.**

---

## CP-8 — Apply the 14 manifests through the reviewed importer

Obtain an Admin session and CSRF token through the normal login flow. Every mutation needs
`Origin: https://gradexcourses.com` and `X-CSRF-Token`; anonymous or wrong-origin probes answer
`403 ORIGIN_NOT_ALLOWED` before authentication, which is the correct refusal, not a fault.

```bash
ORIGIN=https://gradexcourses.com
NIL=00000000-0000-0000-0000-000000000000
MANIFESTS="
abdullah-al-salem-university-catalog-v1
american-college-middle-east-catalog-v1
american-international-university-catalog-v1
american-university-kuwait-catalog-v1
american-university-middle-east-catalog-v1
arab-open-university-kuwait-catalog-v1
australian-university-kuwait-catalog-v1
box-hill-college-kuwait-catalog-v1
canadian-college-kuwait-catalog-v1
gulf-university-science-technology-catalog-v1
international-university-kuwait-catalog-v1
kuwait-college-science-technology-catalog-v1
kuwait-technical-college-catalog-v1
paaet-catalog-v1
"
```

Confirm the server offers exactly the 15 expected identifiers:

```bash
curl -sS -b cookies.txt "$ORIGIN/api/v1/admin/academic/manifests" | jq -r '.[].manifest' | sort
```

Expected: the 14 above plus `kuwait-university-launch-v1`. **`kuwait-university-launch-v1` is
deliberately absent from `MANIFESTS` and must never be applied in this release.**

**Pass 1 — dry run all 14. Writes nothing.**

```bash
for m in $MANIFESTS; do
  printf '%s ' "$m"
  curl -sS -b cookies.txt -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF" \
    -H 'Content-Type: application/json' \
    -d "{\"manifest\":\"$m\",\"mode\":\"dry_run\"}" \
    "$ORIGIN/api/v1/admin/academic/institutions/$NIL/import" |
    jq -c '{applied, counts}'
done
```

Expected, per manifest: `{"applied":false,"counts":{"Create":<C>,"Update":0,"Noop":0,"Drift":0}}`
where `<C>` is `1 + subjects` from this table:

| Manifest | Institution slug | Subjects | Expected create |
|---|---|---:|---:|
| `abdullah-al-salem-university-catalog-v1` | abdullah-al-salem-university | 26 | 27 |
| `american-college-middle-east-catalog-v1` | american-college-middle-east | 9 | 10 |
| `american-international-university-catalog-v1` | american-international-university | 19 | 20 |
| `american-university-kuwait-catalog-v1` | american-university-kuwait | 22 | 23 |
| `american-university-middle-east-catalog-v1` | american-university-middle-east | 9 | 10 |
| `arab-open-university-kuwait-catalog-v1` | arab-open-university-kuwait | 23 | 24 |
| `australian-university-kuwait-catalog-v1` | australian-university-kuwait | 17 | 18 |
| `box-hill-college-kuwait-catalog-v1` | box-hill-college-kuwait | 9 | 10 |
| `canadian-college-kuwait-catalog-v1` | canadian-college-kuwait | 20 | 21 |
| `gulf-university-science-technology-catalog-v1` | gulf-university-science-technology | 25 | 26 |
| `international-university-kuwait-catalog-v1` | international-university-kuwait | 22 | 23 |
| `kuwait-college-science-technology-catalog-v1` | kuwait-college-science-technology | 15 | 16 |
| `kuwait-technical-college-catalog-v1` | kuwait-technical-college | 3 | 4 |
| `paaet-catalog-v1` | paaet | 26 | 27 |
| **Total** | **14 institutions** | **245** | **259** |

None of the 14 declares academic units, programs, curricula or mappings, so those are 0 throughout.

Confirm the dry run wrote nothing:

```bash
docker exec gradex-production-postgres-1 psql -U gradex -d gradex_production \
  --no-psqlrc --tuples-only --no-align -c "SELECT count(*) FROM institutions;"
```
Expected: `1`.

**STOP if:** any `Create` differs from the table, any `Update`/`Drift` is non-zero, or the
institution count moved.

**Pass 2 — apply all 14.**

```bash
for m in $MANIFESTS; do
  printf '%s ' "$m"
  curl -sS -b cookies.txt -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF" \
    -H 'Content-Type: application/json' \
    -d "{\"manifest\":\"$m\",\"mode\":\"apply\"}" \
    "$ORIGIN/api/v1/admin/academic/institutions/$NIL/import" |
    jq -c '{applied, institution: .institution_slug, counts}'
done
```

Expected, per manifest: `applied: true`, the slug from the table, and the same
`Create=<C>, Update=0, Noop=0, Drift=0`. Each import is a single transaction — a mid-import failure
rolls back that whole institution and leaves the others untouched.

---

## CP-9 — Import verification

**Institution count:**
```bash
docker exec gradex-production-postgres-1 psql -U gradex -d gradex_production \
  --no-psqlrc --tuples-only --no-align -c "SELECT count(*) FROM institutions;"
```
Expected: `15` (1 pre-existing Kuwait University + 14 imported).

**Subject counts per institution:**
```bash
docker exec gradex-production-postgres-1 psql -U gradex -d gradex_production --no-psqlrc -c "
  SELECT i.slug, count(s.id) AS subjects
    FROM institutions i LEFT JOIN subjects s ON s.institution_id = i.id
   GROUP BY i.slug ORDER BY i.slug;"
```
Expected: exactly the 14 rows of the CP-8 table, plus `kuwait-university | 84`. Total Subjects
`84 + 245 = 329`:
```bash
docker exec gradex-production-postgres-1 psql -U gradex -d gradex_production \
  --no-psqlrc --tuples-only --no-align \
  -c "SELECT count(*) FROM subjects;" \
  -c "SELECT count(*) FROM subjects WHERE retired_at IS NOT NULL;" \
  -c "SELECT count(*) FROM (SELECT institution_id, code_normalized FROM subjects WHERE official_code IS NOT NULL GROUP BY 1,2 HAVING count(*) > 1) d;"
```
Expected: `329`, `0`, `0`.

**Idempotent second run — NOOP evidence.** Re-run the dry run over all 14:
```bash
for m in $MANIFESTS; do
  printf '%s ' "$m"
  curl -sS -b cookies.txt -H "Origin: $ORIGIN" -H "X-CSRF-Token: $CSRF" \
    -H 'Content-Type: application/json' \
    -d "{\"manifest\":\"$m\",\"mode\":\"dry_run\"}" \
    "$ORIGIN/api/v1/admin/academic/institutions/$NIL/import" |
    jq -c '{applied, counts}'
done
```
Expected: for every manifest `Create=0, Update=0, Noop=<C>, Drift=0`, with `<C>` the same value as
the first pass (27, 10, 20, 23, 10, 24, 18, 10, 21, 26, 23, 16, 4, 27). Institution count still `15`,
Subject count still `329`.

**Kuwait University isolation — no scraped duplicate import.** Three independent proofs:

1. `kuwait-university-launch-v1` was never in `MANIFESTS`; the CP-8 command log shows 14 calls.
2. No other manifest addresses that institution:
   ```bash
   grep -rl 'slug: kuwait-university$' backend/internal/academic/manifest/data/*/manifest.yaml
   ```
   Expected: exactly one path, `.../data/kuwait-university/manifest.yaml`.
3. The live rows are unchanged and still carry their official-catalogue provenance:
   ```bash
   docker exec gradex-production-postgres-1 psql -U gradex -d gradex_production --no-psqlrc -c "
     SELECT count(*) AS ku_subjects,
            count(*) FILTER (WHERE s.updated_at > now() - interval '2 hours') AS touched_today
       FROM subjects s JOIN institutions i ON i.id = s.institution_id
      WHERE i.slug = 'kuwait-university';"
   ```
   Expected: `ku_subjects = 84`, `touched_today = 0`. Kuwait University also retains its
   `units=11 / programs=5 / curricula=5 / mappings=112` structure, which no imported manifest
   declares — so any structural change would be immediately visible.

Additionally, `gradex-catalogue-ibntohamy` is the declared source for the 14 scraped manifests and
appears in **none** of Kuwait University's 20 sources.

**STOP if:** any count differs, any Kuwait University row was touched, or any second-pass run reports
a non-zero `Create`, `Update` or `Drift`.

---

## CP-10 — Frontend deploy (outage ends)

If the frontend was held back at CP-6:

```bash
gxcompose up --detach --no-deps --force-recreate frontend
docker inspect --format '{{.State.Health.Status}}' "$(gxcompose ps --quiet frontend)"
```

Expected: `healthy`. A `--force-recreate` frontend swap costs a **~4 second 502 window** at the edge
(`dial tcp: lookup frontend … server misbehaving`). One replica, no drain — expected and
self-healing.

```bash
./deploy/hostinger/host.sh verify
```
Expected:
```
s12-hostinger: public probes, clean schema 38, worker, and authenticated verified-TLS Redis passed
```

---

## CP-11 → CP-15 — Smoke-test matrix

The locale prefix is optional; `/subjects` and `/en/subjects` both serve. A 404 on a
route-shape that never existed (for example `/en`) is not a regression.

| # | Check | Command / action | Expected |
|---|---|---|---|
| S-1 | English Subject browse | `GET /en/subjects` | 200; grid renders; institution filter offers 15 institutions |
| S-2 | Arabic Subject browse | `GET /ar/subjects` | 200; `dir="rtl"`; Arabic titles render |
| S-3 | Browse pagination | `GET /en/subjects?page=2` | 200; distinct second page; no stale-response flicker |
| S-4 | English Subject detail (unserved) | `GET /en/subjects/kuwait-technical-college/CSEC%20102` | 200; title "Introduction to Cybersecurity"; demand control present |
| S-5 | Arabic Subject detail (same) | `GET /ar/subjects/kuwait-technical-college/CSEC%20102` | 200; Arabic title "مقدمة في الأمن السيبراني" |
| S-6 | API browse | `GET /api/v1/catalog/subjects?page=1&page_size=20` | 200; `total = 329` |
| S-7 | API browse, institution filter | `…/subjects?institution=paaet` | 200; `total = 26` |
| S-8 | API browse, availability filter | `…/subjects?availability=unserved` then `=served` | 200 both; the two totals sum to 329 |
| S-9 | API browse, unknown filter value | `…/subjects?institution=not-a-real-slug` | 200 with an empty list — **not** an error (a stale shared link is an ordinary state) |
| S-10 | API detail, valid | `GET /api/v1/catalog/subjects/paaet/<real code>` | 200 |
| S-11 | **Unknown Subject → 404** | `GET /api/v1/catalog/subjects/paaet/ZZZ%20999` | **404** problem+json; never 500 |
| S-12 | **Unknown institution → 404** | `GET /api/v1/catalog/subjects/no-such-institution/CSEC%20102` | **404**; never 500 |
| S-13 | **Malformed value → 404** | `…/subjects/paaet/%2E%2E%2F%2E%2E%2Fetc%2Fpasswd`, `…/subjects/paaet/'%20OR%201=1--`, `…/subjects/paaet/%00` | **404** each; never 500. `c908168` moved identifier parsing into Go precisely so shape is never inferred from SQL |
| S-14 | Frontend unknown Subject page | `GET /en/subjects/paaet/ZZZ%20999` | 404 page, resolved server-side; no client crash |
| S-15 | Served Subject resolves to a real Course | pick a Subject that has a published Course; open its detail page | Course link present and navigates to a live Course; demand control **absent or disabled** |
| S-16 | Unserved Subject stays requestable | open an unserved Subject (for example `kuwait-technical-college/CSEC 102`) | demand control present and enabled for a signed-in Student |
| S-17 | Anonymous demand mutation | `POST /api/v1/me/subject-demand` with no session | `403 ORIGIN_NOT_ALLOWED` without an `Origin` header; `401`/`403` with one — never 500 |
| S-18 | Non-Admin demand counts | `GET /api/v1/admin/academic/subject-demand` as Student | 403 |

For S-11 → S-13, capture the status codes explicitly:

```bash
for p in 'paaet/ZZZ%20999' 'no-such-institution/CSEC%20102' \
         'paaet/%2E%2E%2F%2E%2E%2Fetc%2Fpasswd' "paaet/'%20OR%201%3D1--" 'paaet/%00'; do
  printf '%s -> ' "$p"
  curl -sS -o /dev/null -w '%{http_code}\n' "https://gradexcourses.com/api/v1/catalog/subjects/$p"
done
```
Expected: `404` on every line. **Any `500` is a stop condition.**

### CP-12 — Student demand request / withdraw (production test account)

Permitted only if a dedicated production test Student account already exists under the established
release process. **Do not create a new production account as part of this release** — account
creation is outside this tranche's authority. If no such account exists, record CP-12 as
`NOT_EXERCISED — no safe production Student account`, and rely on the E2E coverage committed at
`3776b75` / `3006c12`. Do not substitute code inspection for an observed run.

If the account exists:

1. Sign in as that Student; note the device-trust flow (`fbb6bb5` preserves the safe return path).
2. Open `/en/subjects/kuwait-technical-college/CSEC 102` and raise demand.
   Expect: control flips to the raised state; `POST /api/v1/me/subject-demand` → 2xx.
3. `GET /api/v1/me/subject-demand` → the Subject appears exactly once.
4. Re-raise the same Subject: refused or a no-op — the partial unique index
   `subject_demand_signals_live_unique` permits one live signal per Student per Subject.
5. Withdraw: `DELETE /api/v1/me/subject-demand/{subjectId}` → 2xx; control returns to requestable.
6. Malformed withdrawal: `DELETE /api/v1/me/subject-demand/not-a-uuid` → **422** (asserted by `792143c`).
7. Database check:
   ```bash
   docker exec gradex-production-postgres-1 psql -U gradex -d gradex_production --no-psqlrc -c "
     SELECT count(*) FILTER (WHERE withdrawn_at IS NULL)   AS live,
            count(*) FILTER (WHERE withdrawn_at IS NOT NULL) AS withdrawn
       FROM subject_demand_signals;"
   ```
   Expected after withdrawal: `live = 0`, `withdrawn = 1`.

**Residue:** the withdrawn row is retained by design — that is what makes a demand count a count of
Students rather than a count of clicks. It is not user-visible and is not an entitlement,
reservation, waitlist position or purchase intent. Nothing in the access, entitlement, enrollment,
purchase or media-playback path reads it.

### CP-13 — Admin demand count

With CP-12's signal **raised** (before step 5):

```bash
curl -sS -b admin-cookies.txt "$ORIGIN/api/v1/admin/academic/subject-demand" | jq .
```
Expected: one entry for `CSEC 102`, count `1`, carrying **both** institution names (English and
Arabic — `2d7c850`). Then open `/en/admin/subject-demand` and `/ar/admin/subject-demand`: the same
row renders in both locales. After CP-12 step 5 the count returns to 0 / the row disappears.

Counts only. No per-Student roster is exposed, by design.

---

## CP-16 — Final health, readiness and log inspection

```bash
./deploy/hostinger/host.sh verify
./deploy/hostinger/host.sh status
./deploy/hostinger/host.sh logs api      | tail -n 200
./deploy/hostinger/host.sh logs worker   | tail -n 200
./deploy/hostinger/host.sh logs frontend | tail -n 100
./deploy/hostinger/host.sh logs edge     | tail -n 100
./deploy/hostinger/host.sh monitor
curl -sS https://gradexcourses.com/readyz | jq .
docker exec gradex-production-postgres-1 psql -U gradex -d gradex_production \
  --no-psqlrc --tuples-only --no-align -c "SELECT version, dirty FROM schema_migrations;"
```

Expected:
- `verify` passes with `clean schema 38`;
- `postgres`, `redis`, `api`, `frontend` healthy; `worker` running; `edge` running;
- `/readyz` → all three checks `ok`;
- `38|f`;
- API logs: no panics, no `column does not exist`, no `schema version is not supported`;
- worker logs: no repeated media lease or claim errors;
- edge logs: 502s confined to the CP-4 → CP-10 window and the ~4s frontend recreate.

---

## Rollback matrix

The baseline change reshapes this table. Read the two framing facts first.

**Framing fact 1 — clean 37 is a servable resting state.** The deployed `4e7ddcd` binaries serve
schema 37. Any failure that leaves the marker clean at 37 is recovered by restarting the existing
application: no schema rollback, no data loss, no restore.

**Framing fact 2 — treat schema 37 as the rollback floor, without overclaiming why.** Two things
must be kept apart:

- **Mechanism (independently proven).** Schema 37 *becomes* a hard rollback floor once either
  `0037`-down refusal condition exists — device security-event history, or a live `DEVICE_TRUST_OTP`
  row. Both are proven by `TestSchema37RollbackIsRefusedByRealDeviceData`.
- **Current production evidence (what was actually captured).** Production is at schema 37 and
  Device Trust has been used: `identity_trusted_devices` contains 8 rows. **That does not prove
  either refusal condition is present right now.** A trusted-device row count is not a
  security-event count and not a live-OTP count, and the preflight did not query those.

So the honest statement is: the floor is **credible and unverified**, not demonstrated. The release
policy is the same either way — **do not assume ordinary 37 → 36 rollback is available** — because
planning a rollback on an unverified precondition is the failure mode this distinction exists to
prevent. This release neither creates nor removes that condition.

| Stage | Failure | Rollback | Data loss | Downtime |
|---|---|---|---|---|
| CP-1 | Identity, docs-only, manifest or **live baseline** mismatch | Abandon; re-derive the plan against the actual baseline | none | none |
| CP-2 | Build / label / checksum failure | Abandon; `docker image rm` the loaded tags | none | none |
| CP-3 | Backup or `verify-restore` fails | **Abandon the release.** Do not proceed on an unverified backup | none | none |
| CP-4 | Services will not stop, or state ambiguous | `gxcompose start worker api frontend`; do not migrate | none | seconds |
| CP-5 | `migrate` exits non-zero, marker **clean at 37** | `gxcompose start worker api frontend` — the deployed binaries serve 37. **Complete recovery.** Diagnose outside the window | none | outage ends at restart |
| CP-5 | `migrate` exits non-zero, marker **dirty** | **Do not re-run `migrate up`; do not retry.** Restore the CP-3 snapshot into a fresh database, verify, and repoint | back to snapshot time | extended |
| CP-5 | Schema reached 38, decision to revert | `38 → 37` only: supervised, backup-first, one file, one transaction. See "Schema rollback" below | `subject_demand_signals` rows are dropped | minutes; the deployed application then serves 37 again |
| CP-6 | `apply-release` refuses, or `api`/`worker` never healthy | Read `api` logs. The schema is at 38 and the deployed binaries cap at 37, so application-only rollback to `4e7ddcd` requires the `38 → 37` schema step first | none yet | outage continues |
| CP-7 | Readiness or Subject API fails | Same as CP-6 | none | outage continues |
| CP-8 | An import fails mid-way | That institution's transaction rolled back whole; the others are unaffected. Fix and re-apply only the failed manifest — re-applying a succeeded one is a NOOP | none | none (API already up) |
| CP-8/9 | Imported data is wrong | **There is no un-import.** The importer never retires or deletes by omission. Reversal = restore the CP-3 snapshot | back to snapshot time | extended |
| CP-10 | Frontend unhealthy | `GRADEX_FRONTEND_IMAGE=<previous>` and recreate `frontend` alone. Backend and schema are unaffected | none | ~4s per swap |
| CP-11–15 | A smoke test fails | Triage by severity. Frontend-only → frontend rollback. Backend behaviour → `38 → 37` schema step first, then the deployed application | depends | depends |
| CP-16 | Log anomalies | Investigate before declaring the release complete | none | none |

### Schema rollback (supervised emergency operation only)

**For this release the schema rollback question is exactly one step: `38 → 37`.** It is the only
step available by migration, and it is the only one this release makes necessary.

**`migrate down N` takes a STEP COUNT, not a target version.** `down 37` means thirty-seven steps
back — it lands on version 1 and destroys the schema. Never issue it. The command also refuses to
run at all when `APP_ENV=production`, and the production image exposes only `gradex-migrate up`,
`version` and `max-version`.

The only supported reversal is per-file, under supervision, with a fresh verified backup taken
immediately beforehand, applying **one** down file in **one** transaction and correcting the
bookkeeping in the same transaction:

```bash
# 38 -> 37 ONLY. One file. One transaction. Backup taken immediately before.
docker exec -i gradex-production-postgres-1 psql -U gradex -d gradex_production \
  --set ON_ERROR_STOP=1 --single-transaction \
  -f /path/to/0038_subject_demand_signals.down.sql \
  -c 'UPDATE schema_migrations SET version = 37, dirty = false;' \
  -c 'SELECT version, dirty FROM schema_migrations;'
```

Expected: `37 | f`. Applying the SQL alone is **not** sufficient — readiness reads the marker.
`schema_migrations` holds exactly one row, so the correction is an `UPDATE`, never an `INSERT`.

**What `38 → 37` costs.** `0038_subject_demand_signals.down.sql` drops `subject_demand_signals`
entirely. Every Student demand signal, live and withdrawn, is lost — including the withdrawal
history that makes a demand count a count of Students rather than a count of clicks. Nothing else
references the table and no access decision reads it, so the drop is structurally safe; the loss is
product-prioritisation input, unrecoverable except from backup. The step always succeeds, which is
exactly why the backup is the only protection. Export the counts first if the demand data has any
decision value:

```sql
SELECT subject_id, count(*) FROM subject_demand_signals WHERE withdrawn_at IS NULL GROUP BY 1;
```

**Unlike the superseded 34 → 38 plan, schema 37 is a servable destination.** The deployed `4e7ddcd`
binaries have `MaxSchemaVersion` 37 and serve it. A `38 → 37` rollback followed by restarting the
existing application is therefore a complete return to the pre-release production state. It is a
destination, not a step on the way to somewhere else.

### Going below 37 is a restore operation, not a migration

**Do not plan a 38 → 34 chain as this release's recovery path.** It is withdrawn as a normal option.
Production is already past 37 and Device Trust is in use, so the migration path below 37 cannot be
assumed to exist.

The `0037` down migration **fails whenever either of the following holds**. Two independent
conditions, either one sufficient — and whether production currently satisfies either was **not**
established by the captured preflight evidence:

1. **Any device security event row.** `identity_security_events` carries an append-only
   `BEFORE UPDATE OR DELETE` trigger from `0005`. The `0037` down migration *must* `DELETE` the nine
   device event types, because the pre-`0037` `identity_security_events_type` CHECK constraint it
   restores would otherwise be violated by history the feature produced. The two requirements are
   irreconcilable, and the migration fails with
   `identity_security_events is append-only (attempted DELETE)`.
2. **Any live `DEVICE_TRUST_OTP` row.** The down migration deletes those rows and then `ALTER`s
   `identity_action_secrets` in the same transaction. PostgreSQL refuses to `ALTER` a table carrying
   pending trigger events from earlier DML in that transaction, and the migration fails with
   `cannot ALTER TABLE "identity_action_secrets" because it has pending trigger events`.

Both refusals are proven by `TestSchema37RollbackIsRefusedByRealDeviceData`. A failed migration
leaves the marker dirty and destroys nothing, so the refusal is safe — but it is a dead end, not a
retry.

**What production evidence actually supports.** `identity_trusted_devices` holds 8 rows, so Device
Trust has been used and the refusal conditions are plausible. It is **not** proof of either one: the
preflight did not count device security events or live `DEVICE_TRUST_OTP` rows. Treat the floor as
**credible and unproven**. If a below-37 reversal is ever genuinely required, prove the
preconditions at emergency time with the diagnostic below, under supervision — and if they are not
explicitly proven safe, use the restore route instead.

**The only route below 37 is the already-reviewed recovery strategy**, and it requires explicit
Product Owner emergency approval:

```
verified backup snapshot
  -> restore into a FRESH database (never over the live one)
  -> verify-restore against the recorded source schema and counts
  -> promote / repoint DATABASE_URL under supervision
```

Do not improvise destructive SQL, and never run a generic `migrate down` to get there.

Diagnostic, to be run at emergency time if a below-37 question is ever raised. **This was not run
during preflight**, so the floor's current status is unestablished. Non-zero in either column
confirms the floor is in force; zero in both is the only evidence that would make an ordinary
`0037` down even worth attempting:

```sql
SELECT (SELECT count(*) FROM identity_security_events
         WHERE event_type LIKE 'DEVICE%' OR event_type LIKE 'ADMIN_DEVICE%') AS device_events,
       (SELECT count(*) FROM identity_action_secrets
         WHERE purpose = 'DEVICE_TRUST_OTP') AS live_device_otps;
```

#### Historical reference — what the lower steps would have destroyed

Retained as reviewed evidence of *why* the floor exists. **These are not available steps for this
release** and must not be read as a recovery path.

| Step | Migration | What the down migration destroys | Status |
|---|---|---|---|
| 37 → 36 | `0037_student_trusted_devices` | Device security event history (nine event types), live `DEVICE_TRUST_OTP` challenges, `identity_trusted_devices`, and `identity_device_replacement_state` — the device audit trail and the active 24-hour replacement cooldown. No session row is deleted and no Student is logged out. | **DO NOT ASSUME AVAILABLE.** Refuses once either condition above holds; Device Trust is in use in production, and neither condition was proven present or absent at preflight |
| 36 → 35 | `0036_bundles_and_offers` | Drops `bundles`, `bundle_courses`, `bundle_price_changes`, `purchase_request_bundle_items`, `bundle_purchase_grants`. | Refuses once commerce data exists; unreachable while 37 → 36 refuses |
| 35 → 34 | `0035_media_work_leases` | Drops the six work-lease and attempt-accounting columns, their four constraints, and the expired-lease recovery index. Media state, provenance, trusted duration and rendition data are untouched. | Requires in-flight media settled in the same transaction; unreachable while 37 → 36 refuses |

The forward chain and this reverse path are rehearsed in disposable infrastructure by
`backend/internal/db/schema_34_to_38_chain_integration_test.go`. That rehearsal covers the only
shape that can complete the reverse walk — device trust migrated in but never used — which is
**not** the production shape.

Prove both bookkeeping and physical shape before starting the deployed application after any
schema rollback:

```bash
docker exec gradex-production-postgres-1 psql -U gradex -d gradex_production \
  --no-psqlrc --tuples-only --no-align \
  -c 'SELECT version, dirty FROM schema_migrations;' \
  -c "SELECT to_regclass('public.subject_demand_signals');"
```
Required: `37|f` and an empty second result.

---

## Estimated downtime

| Window | Cause | Estimate |
|---|---|---|
| CP-4 → CP-6 | application stopped for the migration | 1–3 min |
| CP-5 alone | **one** additive migration (`0038`) on a pre-launch dataset | < 10 s |
| CP-6 | container recreate + health waits (`api` healthy, `worker` running, `frontend` healthy) | 30–90 s |
| CP-6 → CP-10 | **only if the frontend is held back for the import** | + 5–15 min |
| CP-10 | `--force-recreate` frontend swap | ~4 s 502 |
| **Total, frontend not held back** | | **≈ 2–5 min** |
| **Total, frontend held back (recommended)** | | **≈ 10–20 min** |

During the window the edge stays up and returns 502/503. No maintenance page is configured. Static
edge-served assets are unaffected.

---

## Product Owner approval points

| Gate | Position | Decision required |
|---|---|---|
| **G0a** | after CP-2, before G0b | **Independent release-integrity review of this re-derived 37 → 38 procedure.** The software itself remains historically approved (D-103 `7d3ae73..e2223d7`; D-106 APPROVED WITH FINDINGS), and this re-derivation changes no code. What is unreviewed is the changed procedure: one migration instead of four, a fail-closed baseline gate, changed CP-5 failure semantics, and a rewritten rollback model. **NOT SATISFIED.** Not a Product Owner decision and not waivable by one |
| **G0b** | after G0a, before CP-3 | **Fresh** Product Owner approval. The prior 34 → 38 G0b is **superseded** — its premise (schema 34, four pending migrations) was disproved before any mutation. Approve: baseline clean 37 at `4e7ddcd…`; one pending migration `0038`; mandatory outage; the **already-active** schema-37 hard floor; the 14-manifest import; and shipping the reviewed non-blocking findings |
| **G1** | after CP-3, before CP-4 | **THE POINT OF NO EASY RETURN.** Approve beginning the outage and the irreversible migration, on a recorded and `verify-restore`-proven backup snapshot |
| **G2** | after CP-7, before CP-8 | Approve the irreversible catalogue data mutation: 14 institutions, 245 Subjects, no un-import |
| **G3** | after CP-9, before CP-10 | Approve public exposure of the feature once the data is verified correct |

**G1 is the final explicit approval required before any irreversible production mutation.** CP-1
through CP-3 are read-only or reversible; CP-5 is the first irreversible step.

**G0a is unsatisfied, so no gate after it may be granted.** Product Owner approval at G0b, G1, G2 or
G3 has no effect until G0a is recorded.

**On the superseded G0b, stated plainly.** It was not withdrawn for cause and nothing failed review.
It approved a payload — four sequential migrations from schema 34 — that does not correspond to
production reality. Execution stopped fail-closed at CP-1 *before* any mutation precisely so that
this would be caught here rather than discovered mid-outage. Reusing it would mean executing an
approval whose stated contents cannot happen.

**The governance rule stands unchanged.** Product Owner authority covers release and business
decisions and does not override an engineering review. D-103 / migration `0035` is independently
**APPROVED** with H1, H2 and M1 closed; there is no failed D-103 technical review outstanding and
none for a Product Owner to remediate.

**One open item neither gate closes.** Repository evidence does not establish that the `4e7ddcd`
production deployment was authorized or reviewed (F-1). Proceeding on top of that baseline is a
Product Owner decision; it does not retroactively authorize the earlier deployment, and this
document does not treat it as authorized.

---

## Out of scope for this release

- The 2 Low findings (not remediated, by instruction).
- Adding `gradex-catalog-import` to the production image.
- Re-importing or upgrading `kuwait-university-launch-v1`.
- Applying, re-applying or rolling back `0035`, `0036` or `0037`. They are already in production and
  this release does not touch them.
- Establishing or ratifying the authorization status of the `4e7ddcd` production deployment. That is
  an open governance question recorded in F-1, deliberately not answered here.
- Any remediation of the D-103 review status — **none remains outstanding**. D-103 / migration
  `0035` is independently **APPROVED**, with H1, H2 and M1 closed. As a matter of governance Product
  Owner authority never remediates or overrides a failed technical review, and there is no failed
  D-103 technical review left in this candidate; G0b is a release and business decision only.
