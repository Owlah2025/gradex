# Production Release Plan — Subject Catalogue / Demand (D-106)

**Status:** PLANNING AND PREFLIGHT ONLY. Nothing in this document has been executed. No production
system has been contacted. Every command below is written to be read, approved, and then run by a
human operator.

**Release SHA:** `c908168ac1a0acc2071279cdb37bad916241e47e`
**Branch:** `catalog-seed-ibntohamy-20260914`
**Worktree at planning time:** clean
**Tranche commit range:** `4e7ddcd6..c908168a` (15 commits, first `7f18674`)
**Independent verdict:** APPROVED WITH FINDINGS — 0 Critical / 0 High / 0 Medium / 2 Low
(non-blocking). The 2 Low findings are **not** remediated in this release.

---

## 0. Three findings that change the shape of this release

These were discovered during preflight tracing and must be resolved by the Product Owner before the
release can proceed as written.

### F-1 — This is not a 37 → 38 release. It is a 34 → 38 release.

`docs/launch/STATUS.md` records the production base as `b8dea967196de68914440b2092cd80daf85d9546`,
schema **34**, application-only, no migration. Nothing after it has been deployed.

`gradex-migrate up` applies **every** pending migration. From schema 34, it applies four:

| Migration | Feature | Decision | Approval state recorded in STATUS.md |
|---|---|---|---|
| `0035_media_work_leases` | D-103 media work leases | D-103 | IMPLEMENTED, **pending independent review** |
| `0036_bundles_and_offers` | Bundles V1 / Catalogue Offers V1 | D-104 | Head `8ee8d42` approved |
| `0037_student_trusted_devices` | Student device trust | D-105 | Head `e4993d6` approved |
| `0038_subject_demand_signals` | Subject demand (this tranche) | D-106 | **APPROVED WITH FINDINGS** |

The `APPROVED WITH FINDINGS` verdict covers the Subject Catalogue / Demand tranche only. It does not
approve D-103, and STATUS.md states that the combined Bundles + D-105 candidate "requires fresh
independent approval before push or production deployment".

**Consequence:** deploying `c908168` to production ships D-103, D-104 and D-105 alongside D-106.
Two options, and the choice is the Product Owner's:

**This is not a Product Owner waiver.** D-103 has since been independently reviewed and
**REJECTED**, and the combined 34 → 38 payload was **REJECTED** with it. A Product Owner decision
cannot substitute for a passing technical review: Product Owner approval governs release and
business decisions — timing, scope, risk acceptance, whether to ship at all — and has no authority
over whether an engineering review passed. Both are required, and the technical one comes first.

The only two paths forward are therefore:

- **Option A:** D-103 obtains an independent technical **approval** on a re-reviewed range, after
  which the Product Owner may decide whether to accept a combined 34 → 38 release. Both are
  required; neither is sufficient alone.
- **Option B:** first ship a separately and independently approved intermediate release that brings
  production to schema 37, then run this plan as a true 37 → 38 step. This still needs the combined
  candidate's own independent approval.

Everything below is written for a 34 → 38 step. Under Option B, only `0038` is pending at CP-5 and
the expected `migrate up` output changes accordingly; nothing else in the plan changes.

**Remediation status.** The D-103 and combined-candidate review findings have been remediated on
this branch (see the re-review ranges recorded in `docs/launch/STATUS.md`). That remediation is
builder work and is explicitly **not** self-approval: it does not clear the REJECTED verdict, and it
does not unblock G0. The candidate remains unauthorized for production until an independent
reviewer records an approval verdict against the exact re-review range.

### F-2 — There is no zero-downtime path. A hard outage window is mandatory.

- `backend/cmd/api/main.go` — `requiredSchemaVersion()` returns `SubjectDemandSignalSchemaVersion` = **38**.
  The D-106 routes are mounted in this candidate and query `subject_demand_signals`, which arrives in
  38, so the floor is 38 and not 37.
- `backend/internal/db/schema.go` — `MaxSchemaVersion` = **38**.
- `backend/cmd/worker/main.go` — worker requires `MediaWorkLeaseSchemaVersion` = **35**.
- The currently-deployed production binary (`b8dea96`) has `MaxSchemaVersion` = **34** and refuses
  readiness against anything higher (`schema.go` `CheckSchemaAtLeast`, fails closed).

So the compatibility truth for this candidate is:

```
old API (b8dea96): schema 34 only
new API (this candidate): schema 38 only
```

The two ranges do not touch — the new API's floor and ceiling are both 38 — so there is **no
rolling-overlap schema**, and no ordering of binary and migration steps produces one. `host.sh apply-release` is the wrong tool
here — it uses `--no-deps` and never runs migrations, so it would recreate the new API against
schema 34 and die at `wait_for_status api healthy`.

`docs/launch/RUNBOOK.md` (D-103 section) already states the required ordering, and it also states
that **the old worker and the new worker must never run concurrently**. That rule is binding here.

**Consequence:** the requested order `backup → migration → backend → …` becomes
`backup → stop application → migration → backend → …`. The public edge stays up and answers 502/503
for the duration; no maintenance page is configured.

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
| `gradex-backend` | `hostinger-c908168ac1a0` | `gradex-api`, `gradex-worker`, `gradex-migrate`, embedded migrations `0001`–`0038`, embedded manifests |
| `gradex-frontend` | `hostinger-c908168ac1a0` | Next.js build incl. the new Subject routes |
| `gradex-backend-proof` | `hostinger-c908168ac1a0` | proof/seed tooling; not started by this release |

All three carry `org.opencontainers.image.revision=c908168ac1a0acc2071279cdb37bad916241e47e`;
`release.sh record` and `host.sh apply-release` both verify that label against the release SHA.

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
  `kuwait-university/` is **unchanged** by this tranche (`git diff --stat 4e7ddcd..c908168` lists no
  `kuwait-university` path).

### Frontend routes new in the tranche

- `frontend/src/app/[locale]/subjects/page.tsx`
- `frontend/src/app/[locale]/subjects/[institutionSlug]/[value]/page.tsx`
- `frontend/src/app/[locale]/admin/subject-demand/page.tsx`

Locales are `ar` and `en` (`frontend/src/lib/identity/restricted-navigation.ts`). The locale prefix
is optional at the edge — `/subjects` and `/en/subjects` both serve.

### Deploy tooling

`git diff --stat 61142de c908168 -- deploy` reports a single changed file,
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

Every `host.sh` invocation must export all four of `GRADEX_HOST_STATE_DIR`, `GRADEX_HOST_ENV_FILE`,
`GRADEX_HOST_PROJECT`, `APP_ENV=production`. The script defaults to `/var/lib/gradex` and project
`gradex-staging`; omitting them silently targets the wrong stack.

Shared prelude for every step, run once per shell on the host:

```bash
export GRADEX_HOST_STATE_DIR=/home/deploy/gradex-production
export GRADEX_HOST_ENV_FILE=/home/deploy/gradex-production/runtime.env
export GRADEX_HOST_PROJECT=gradex-production
export APP_ENV=production
export RELEASE_SHA=c908168ac1a0acc2071279cdb37bad916241e47e
export SHORT=c908168ac1a0
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
CP-1  preflight + SHA verification            (local, read-only)
CP-2  build, export, transfer, load images    (local + host, no production mutation)
GATE  G0a — independent technical approval of D-103 + the 34→38 payload
GATE  G0b — Product Owner approves the release (only after G0a)
CP-3  production backup + artifact proof      (first host write; reversible)
GATE  G1 — Product Owner approves the irreversible sequence
CP-4  stop old application                    (start of outage)
CP-5  migration 34 → 38                       (IRREVERSIBLE)
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

## CP-1 — Preflight and SHA verification (local, read-only)

```bash
git -C /home/owlah/worktrees/gradex-catalog-seed rev-parse HEAD
git -C /home/owlah/worktrees/gradex-catalog-seed status --porcelain=v1
git -C /home/owlah/worktrees/gradex-catalog-seed diff --stat 61142de c908168 -- deploy
```

Expected:
```
c908168ac1a0acc2071279cdb37bad916241e47e
(no output from status)
 deploy/env/production-like.env.example | 4 ++++
 1 file changed, 4 insertions(+)
```

Validate every manifest offline (pure function of checked-in data, no database):

```bash
cd backend && for id in $(go run ./cmd/catalog-import -list); do
  go run ./cmd/catalog-import -mode=validate -manifest="$id"
done
```

Expected: 15 lines, each ending `is valid: …`. Counts must match §CP-8's table exactly.

Read the live production schema and catalogue baseline (read-only over ssh):

```bash
ssh deploy@186.241.16.111 "docker exec gradex-production-postgres-1 psql -U gradex -d gradex_production \
  --no-psqlrc --tuples-only --no-align \
  -c 'SELECT version, dirty FROM schema_migrations;' \
  -c 'SELECT count(*) FROM institutions;' \
  -c 'SELECT i.slug, count(s.id) FROM institutions i LEFT JOIN subjects s ON s.institution_id = i.id GROUP BY i.slug ORDER BY i.slug;'"
```

Expected (confirming F-1) — record the actual values as the release baseline:
```
34|f
1
kuwait-university|84
```

**STOP if:** `dirty` is `t`; the version is not what this plan assumes; `institutions` is not 1; or
any institution other than `kuwait-university` already exists. Any of those invalidates every
expected count downstream and the plan must be re-derived before proceeding.

---

## CP-2 — Build, export, transfer, load images

Local build (refuses a dirty worktree, stamps and verifies the revision label):

```bash
cd /home/owlah/worktrees/gradex-catalog-seed
./deploy/hostinger/release.sh build
./deploy/hostinger/release.sh export "$RELEASE_SHA"
```

Expected:
```
s12-hostinger-release: recorded checksum-addressed local images for release c908168ac1a0acc2071279cdb37bad916241e47e
s12-hostinger-release: built release c908168ac1a0acc2071279cdb37bad916241e47e
s12-hostinger-release: exported release c908168ac1a0acc2071279cdb37bad916241e47e with checksum into ignored state
```

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

Expected: the release SHA three times, then `38`.

Nothing in production has been mutated at this point.

---

## GATE G0 — independent technical approval, then Product Owner approval

G0 has two parts, in this order. Both are required before CP-3.

### G0a — independent technical approval (BLOCKING, currently NOT SATISFIED)

D-103 / migration `0035` and the combined 34 → 38 payload both hold a **REJECTED** independent
review verdict. G0 **may not be granted** while that stands.

G0a is satisfied only by a recorded independent reviewer verdict of approval against one exact
commit range, with every Critical and High finding resolved. A review that produces no retrievable
verdict is `UNAVAILABLE`, not approval. The builder's own assessment of its remediation is not a
verdict, and no amount of Product Owner authority converts a failed engineering review into a passed
one.

**Current state: NOT SATISFIED.** The findings have been remediated on this branch and the candidate
is awaiting independent re-review.

### G0b — Product Owner release decision

Reached only after G0a is satisfied. The Product Owner then confirms in writing:

1. This release carries **D-103, D-104, D-105 and D-106** (schema 34 → 38), not D-106 alone (F-1).
2. A mandatory application outage is accepted (F-2).
3. The catalogue import will run through the Admin HTTP route under an Admin audit actor, not the
   CLI SYSTEM actor (F-3).
4. The 2 Low D-106 findings ship unremediated.

These are business and risk decisions, which is exactly the scope Product Owner approval covers.

**Without both G0a and G0b, stop here.**

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
s12-hostinger: restored encrypted snapshot <SNAPSHOT_ID>, schema 34|false, identity, Course, invitation provenance, Entitlement, and Enrollment passed
```

`verify_restore` fails closed if the restored schema state, or any of the five record counts, differs
from what the source held at capture time. That is the non-empty/readable proof.

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

---

## CP-5 — Migration 34 → 38 (IRREVERSIBLE)

Run the migration as a one-off release job against the **new** backend image:

```bash
set -a; . /home/deploy/gradex-production/runtime.env; set +a
export GRADEX_BACKEND_IMAGE=gradex-backend:hostinger-$SHORT
gxcompose up --detach migrate
gxcompose wait migrate || docker logs "$(gxcompose ps --all --quiet migrate)"
docker logs "$(gxcompose ps --all --quiet migrate)"
```

Expected (`migrate` exits 0):
```
migrate up: version=38 dirty=false (supported; this build supports 2..38)
```

Under Option B (production already at 37) the same line is expected; only the number of applied
migrations differs. Note that schema 37 is a staging point for the *migration*, never a servable
state for this candidate's API: its readiness floor is 38, so the new API refuses 37 and the old API
refuses anything above 34.

**Migration-version verification — both bookkeeping and physical shape:**

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

(4 = the primary-key index plus the three indexes the migration declares.)

**STOP if:** `dirty` is `t`. Do not run `migrate up` again on a dirty marker — `requireClean` will
refuse, and stacking migrations onto a half-applied state is how a recoverable failure becomes an
unrecoverable one. Go to the CP-5 rollback row in §Rollback matrix.

---

## CP-6 — Start the new backend

```bash
./deploy/hostinger/host.sh apply-release /home/deploy/gradex-production/incoming/$RELEASE_SHA/release.env
```

`apply_release` verifies the three revision labels against the release SHA, refuses `:latest` tags,
asserts the schema is clean and not newer than the image ceiling (38 ≤ 38), captures the Entitlement
provenance count, recreates `api`, `worker` and `frontend`, waits for health, re-asserts provenance
is unchanged, and persists the selection into `runtime.env`.

Expected:
```
s12-hostinger: application release c908168ac1a0acc2071279cdb37bad916241e47e is healthy on unchanged schema 38 (target max 38) and provenance
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
- worker logs: no repeated lease or claim errors from the newly-applied `0035`;
- edge logs: 502s confined to the CP-4 → CP-10 window and the ~4s frontend recreate.

---

## Rollback matrix

| Stage | Failure | Rollback | Data loss | Downtime |
|---|---|---|---|---|
| CP-1 | Preflight mismatch | Abandon; re-derive the plan | none | none |
| CP-2 | Build / label / checksum failure | Abandon; `docker image rm` the loaded tags | none | none |
| CP-3 | Backup or `verify-restore` fails | **Abandon the release.** Do not proceed on an unverified backup | none | none |
| CP-4 | Services will not stop | `gxcompose start worker api frontend` | none | seconds |
| CP-5 | `migrate` exits non-zero, marker **clean** at 34 | `gxcompose start worker api frontend` (old images still selected in `runtime.env`) | none | outage continues until restart |
| CP-5 | `migrate` exits non-zero, marker **dirty** | **Do not re-run `migrate up`.** Old binaries refuse a dirty marker and stay stopped. Diagnose, or restore the CP-3 snapshot into a fresh database and repoint | back to snapshot time | extended |
| CP-5 | Schema reached 38, decision to revert | **Supervised, backup-first, one file, one transaction.** See "Schema rollback" below. Never `migrate down`, never a step count | `subject_demand_signals` rows are dropped by `0038` down | extended |
| CP-6 | `apply-release` refuses or `api`/`worker` never healthy | Read `api` logs. If schema-related, the schema is ahead of the old binaries — application-only rollback is **not available**; schema rollback first | none yet | outage continues |
| CP-7 | Readiness or Subject API fails | Same as CP-6 | none | outage continues |
| CP-8 | An import fails mid-way | That institution's transaction rolled back whole; the others are unaffected. Fix and re-apply only the failed manifest — re-applying a succeeded one is a NOOP | none | none (API already up) |
| CP-8/9 | Imported data is wrong | **There is no un-import.** The importer never retires or deletes by omission. Reversal = restore the CP-3 snapshot | back to snapshot time | extended |
| CP-10 | Frontend unhealthy | `GRADEX_FRONTEND_IMAGE=<previous>` and recreate `frontend` alone. Backend and schema are unaffected | none | ~4s per swap |
| CP-11–15 | A smoke test fails | Triage by severity. Frontend-only → frontend rollback. Backend behaviour → full rollback requires schema rollback first | depends | depends |
| CP-16 | Log anomalies | Investigate before declaring the release complete | none | none |

### Schema rollback (supervised emergency operation only)

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

**Schema 37 is not a servable resting state for this release.** This candidate's API floor is 38 and
the previously-deployed API's ceiling is 34, so at 37 neither binary set will pass readiness. A
38 → 37 rollback is therefore only ever a *step* on the way to 34, never a destination: plan to
continue through the remaining three steps below, or do not start. The application stays stopped for
the whole sequence.

### There is no single 38 → 34 rollback

Going back from 38 to 34 is **four separate supervised transactions**, in reverse order, each taken
on its own decision, each preceded by its own fresh verified backup, and each verified for both
bookkeeping and physical shape before the next is considered. Never chain them into one command, and
never describe the reversal as one generic rollback: each step destroys a different class of real
data, and two of them can refuse outright.

| Step | Migration | What the down migration destroys | Refusal / precondition |
|---|---|---|---|
| 38 → 37 | `0038_subject_demand_signals` | **Drops `subject_demand_signals` entirely.** Every Student demand signal, live and withdrawn, is lost — including the withdrawal history that makes a demand count a count of Students rather than a count of clicks. Nothing else references the table and no access decision reads it, so the drop is structurally safe; the loss is product-prioritisation input, and it is unrecoverable except from backup. | None. The step always succeeds, which is exactly why the backup is the only protection. Export the counts first if the demand data has any decision value: `SELECT subject_id, count(*) FROM subject_demand_signals WHERE withdrawn_at IS NULL GROUP BY 1;` |
| 37 → 36 | `0037_student_trusted_devices` | **Four separate classes of row are deleted, not one.** See the itemised list below the table. | **Refuses outright once device trust has been used at all** — two independent conditions, either sufficient. See "Schema 37 is a floor" below. |
| 36 → 35 | `0036_bundles_and_offers` | Drops `bundles`, `bundle_courses`, `bundle_price_changes`, `purchase_request_bundle_items`, and `bundle_purchase_grants`. | **Refuses once commerce data exists.** The Bundle purchase snapshot is immutable by database constraint and `bundle_purchase_grants` records real fulfilled grants; dropping them would destroy purchase provenance. Check before attempting: `SELECT (SELECT count(*) FROM bundle_purchase_grants), (SELECT count(*) FROM purchase_request_bundle_items);` If either is non-zero, **do not roll 36 back** — retain schema 36 and roll back the application only. |
| 35 → 34 | `0035_media_work_leases` | Drops the six work-lease and attempt-accounting columns, their four constraints, and the expired-lease recovery index. Media state, provenance, trusted duration, and rendition data are untouched, so existing `READY` media stays deliverable. | **In-flight media work must be settled inside the same transaction, before the down SQL** — see `docs/launch/RUNBOOK.md`. Without it, every Asset Version in `SCANNING` or `PROCESSING` is stranded permanently: Admin Retry refuses those states and the D-103 recovery pass no longer exists. Assets carrying D-088 trusted-validation provenance are not retryable at all in a scanner-mode deployment. |

### What rolling back `0037` actually deletes

Summarising this as "device state" understates it. `0037_student_trusted_devices.down.sql` removes,
in this order:

1. **Device security event history.** `DELETE FROM identity_security_events` for all nine device
   event types: `DEVICE_TRUST_CHALLENGED`, `DEVICE_TRUST_ATTEMPTS_EXHAUSTED`, `DEVICE_TRUSTED`,
   `DEVICE_ADOPTED_LEGACY_SESSION`, `DEVICE_REVOKED`, `DEVICE_LIMIT_REACHED`,
   `DEVICE_REPLACEMENT_BLOCKED`, `ADMIN_DEVICE_REVOKED`, `ADMIN_DEVICE_COOLDOWN_RESET`. This is the
   audit trail of every device trust, revocation, lockout and Admin intervention. It is deleted
   because the restored pre-0037 `identity_security_events_type` constraint would otherwise be
   violated by history this feature produced — so the deletion is structurally required, not
   optional, and it is irreversible outside the backup.
2. **Live device-trust OTP challenges.** `DELETE FROM identity_action_secrets WHERE purpose =
   'DEVICE_TRUST_OTP'`. Any Student part-way through confirming a device loses that challenge.
3. **Trusted-device registrations.** `DROP TABLE identity_trusted_devices`, with its live-credential
   unique index and both account indexes. Every Student's trusted devices are gone.
4. **Replacement cooldown state.** `DROP TABLE identity_device_replacement_state`. The 24-hour
   replacement cooldown is erased, so a Student part-way through one is silently released from it.

#### Schema 37 is a floor once device trust has been used

Rehearsal in disposable infrastructure found that the `0037` down migration
**cannot run at all** against a database where the feature has been exercised. Two
independent conditions, either one sufficient:

1. **Any device security event row.** `identity_security_events` carries an
   append-only `BEFORE UPDATE OR DELETE` trigger from `0005`. The `0037` down
   migration *must* `DELETE` the nine device event types, because the pre-`0037`
   `identity_security_events_type` CHECK constraint it restores would otherwise be
   violated by history the feature produced. The two requirements are
   irreconcilable, and the migration fails with
   `identity_security_events is append-only (attempted DELETE)`.
2. **Any live `DEVICE_TRUST_OTP` row.** The down migration deletes those rows and
   then `ALTER`s `identity_action_secrets` in the same transaction. PostgreSQL
   refuses to `ALTER` a table carrying pending trigger events from earlier DML in
   that transaction, and the migration fails with
   `cannot ALTER TABLE "identity_action_secrets" because it has pending trigger events`.

Both arise the first time any Student trusts, challenges, or revokes a device.
So in any deployment where device trust is live, **schema 37 is a hard floor**:
reversing past it is a restore-from-backup operation, not a migration. Check
before planning a rollback past 37:

```sql
SELECT (SELECT count(*) FROM identity_security_events
         WHERE event_type LIKE 'DEVICE%' OR event_type LIKE 'ADMIN_DEVICE%') AS device_events,
       (SELECT count(*) FROM identity_action_secrets
         WHERE purpose = 'DEVICE_TRUST_OTP') AS live_device_otps;
```

Non-zero in either column means the `0037` down migration will fail. A failed
migration leaves the marker dirty and destroys nothing, so the refusal is safe —
but it is a dead end, not a retry.

Both refusals are proven by
`TestSchema37RollbackIsRefusedByRealDeviceData`, and the structural reverse walk
covers the only shape that can complete: device trust migrated in but never used.

It also drops `sessions.trusted_device_id` and `sessions.device_trust_state` (with the
`sessions_device_trust_coherent` constraint and the trusted-device index),
`identity_action_secrets.trusted_device_id`, and the `session_device_trust_state` and
`trusted_device_revocation_reason` types.

What it does **not** do: no session row is deleted and no Student is logged out. Session families
survive with their credentials intact and simply stop carrying a device binding, which is the state
they were in before `0037` was applied.

Items 1 and 4 are the ones to weigh. Deleting the device audit trail destroys the evidence an Admin
would need to investigate a device-related incident, and erasing cooldown state removes an active
throttle rather than merely losing a record. Neither is recoverable except from the backup taken
before the step.

Two consequences worth stating plainly:

- **The reverse path can stop partway, and usually will.** If `0037` refuses because device trust has
  been used, the database stays at 37. If 36 → 35 refuses because Bundle commerce data exists, it
  stays at 36. Either way no binary set can serve that schema alongside a D-102 application. Plan the
  rollback decision knowing **37 is the realistic floor** in a live deployment, and 36 the floor
  after that.
- **Application-only rollback is unavailable at every step of this range.** The old and new binaries
  have disjoint servable schema ranges (F-2), so the schema must move first in both directions.

The forward chain and this reverse path, including which steps destroy what, are rehearsed in
disposable infrastructure by
`backend/internal/db/schema_34_to_38_chain_integration_test.go`.

Prove both bookkeeping and physical shape before starting any older binary:

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
| CP-5 alone | four additive migrations on a pre-launch dataset | < 30 s |
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
| **G0a** | after CP-2, before G0b | **Independent technical approval** of D-103 and the combined 34 → 38 payload. Currently **REJECTED / NOT SATISFIED**. Not a Product Owner decision and not waivable by one |
| **G0b** | after G0a, before CP-3 | Product Owner approves the **34 → 38** payload (D-103 + D-104 + D-105 + D-106), the mandatory outage, the HTTP import path, and shipping the 2 Low D-106 findings unremediated |
| **G1** | after CP-3, before CP-4 | **THE POINT OF NO EASY RETURN.** Approve beginning the outage and the irreversible migration, on a recorded and `verify-restore`-proven backup snapshot |
| **G2** | after CP-7, before CP-8 | Approve the irreversible catalogue data mutation: 14 institutions, 245 Subjects, no un-import |
| **G3** | after CP-9, before CP-10 | Approve public exposure of the feature once the data is verified correct |

**G1 is the final explicit approval required before any irreversible production mutation.** CP-1
through CP-3 are read-only or reversible; CP-5 is the first irreversible step.

**G0a is currently unsatisfied, so no gate after it may be granted.** Product Owner approval at G0b,
G1, G2, or G3 has no effect while the independent technical verdict on D-103 stands at REJECTED.
Product Owner authority covers release and business decisions; it does not override a failed
engineering review.

---

## Out of scope for this release

- The 2 Low findings (not remediated, by instruction).
- Adding `gradex-catalog-import` to the production image.
- Re-importing or upgrading `kuwait-university-launch-v1`.
- Any remediation of the D-103 review status — that is a Product Owner decision at G0, not a change
  this plan makes.
