# Release plan — schema 41 enhancement recovery foundation (Phase 3C-A)

**Date:** 2026-09-21
**Deployable revision:** the exact final G0-approved `RELEASE_SHA`, frozen at release time
**Production baseline:** `78ee4227e470014b768da2654c2395f52822385b`, schema 40 clean
**Schema:** 40 → **41** (migration `0041_enhancement_recovery_foundation`)
**Decision:** [D-107](../DECISIONS.md#d-107--the-hls-ladder-is-a-frozen-data-contract-and-phase-3c-a-adds-attempt-kind-and-rendition-provenance)

This is the first GradeX release since the Hostinger cutover that applies a migration, so the
deployment shape differs from every application-only release that came before it. Read this before
running anything. The sequence is **stop → migrate → start**, and it is not zero-downtime.

## Release identity freeze

After final independent G0 approval, from a clean worktree capture `RELEASE_SHA=$(git rev-parse HEAD)`.
No commit may follow that approval: any code or documentation commit changes HEAD, invalidates the
reviewed artifact, and requires a new G0 review and freeze. Verify the frozen tree contains
`49146e37f9b3a41380ada9aa31f63c929636539b` (the supervised rollback implementation), the
`rollback-schema-41` command, the Hostinger `rollback-schema-41-foundation` command, and migration
0041. Commit ancestry is an implementation floor, while the executable image and OCI-label checks
below prove which artifact was actually built.

Build and export with `release.sh` from that exact clean HEAD. Its release manifest supplies the
existing four keys: `GRADEX_RELEASE_SHA`, `GRADEX_BACKEND_IMAGE`, `GRADEX_FRONTEND_IMAGE`, and
`GRADEX_PROOF_IMAGE`. Require every backend, frontend, and proof OCI revision label to equal
`RELEASE_SHA`; retain the manifest and all three images. The Hostinger source checkout must also be
the Git tree at `RELEASE_SHA`, with no local changes. If HEAD, a manifest key, an image label, or
the selected runtime keys disagree, abort before migration or rollback.

There is no existing read-only CLI discovery command for `rollback-schema-41`. The Hostinger gate
therefore checks the reviewed implementation ancestor and command definitions in the clean source
tree, schema ceiling 41 from the selected binary, and OCI labels tying that binary to the same
`RELEASE_SHA`. It never probes capability by invoking the destructive rollback.

---

## 1. Why this is not an application-only release

`apply-release` never migrates. It verifies image revision labels, refuses a dirty schema, refuses a
schema **newer** than the target image's maximum, recreates `api`, `worker` and `frontend`, and
persists the release selection. That is the whole of it. A release that needs new schema must have
the migration applied by the `migrate` compose service first. This release uses the guarded
`up-core-schema-41-foundation` entry, which validates the frozen candidate before the existing
`up-core` sequence can run.

Two consequences follow, and both were wrong in an earlier draft of this contract:

1. **The baseline worker cannot run on schema 41.** `78ee4227` compiles `MaxSchemaVersion = 40`, and
   `CheckSchemaAtLeast` refuses any database version above the build's ceiling. It would not restart
   against schema 41; it would fail its startup schema check and exit.

2. **The baseline worker must not stay alive across the boundary either.** An already-running
   `78ee4227` worker does not re-read the schema version. If it kept processing media after 0041
   committed, it would insert `video_renditions` rows through its own SQL, which does not name
   `processing_operation_id` — persisting post-0041 canonical rows with NULL provenance. Those rows
   cannot be repaired: `video_renditions` is append-only, so the NULL would be permanent.

Neither is solved in code. 3C-A does not ship a dual-schema branch, a compatibility bridge, or a
temporary binary to preserve worker availability across a one-time foundation migration. It uses a
short controlled window instead.

---

## 2. Zero downtime is explicitly not a goal

A short media/backend maintenance window is preferred over every alternative: permanent NULL
provenance on post-0041 rows, dual-schema SQL branches, a throwaway compatibility image, or weakened
provenance guarantees. Do not design around continuous worker availability for this release.

Expected user-visible impact: the API and the worker are unavailable for the length of the window.
Its duration is not predicted here — measure it, do not assume it. The public edge can stay up,
serving errors for API routes, or be taken down with the rest of the stack; see §4.

---

## 3. Preconditions

| Check | Required value |
| --- | --- |
| Production revision | `78ee4227…` on `api`, `worker`, `frontend` |
| Schema | `40`, `dirty = false` |
| `/healthz`, `/readyz` | 200, postgres/redis/schema all ok |
| Active media claims | **zero** rows with `work_claim_token IS NOT NULL` |
| In-flight states | no `SCANNING`, no `PROCESSING`, no `PLAYABLE` with a live claim |
| `ffmpeg` / `ffprobe` | no processes inside the worker container |
| Candidate images | backend, frontend and proof labelled `org.opencontainers.image.revision=$RELEASE_SHA` |
| Candidate max schema | `41` (`gradex-migrate max-version`) |

Active-media guard query (read-only):

```sql
SELECT state, count(*) FILTER (WHERE work_claim_token IS NOT NULL) AS claimed, count(*)
FROM media_asset_versions GROUP BY state ORDER BY state;
```

If any legitimate media processing is active, **do not interrupt it.** Wait for it to finish or
fail, re-run the guard, and only then continue. Interrupting a live transcode is exactly the
condition the stale-recovery path exists to clean up, and it is avoidable here by waiting.

---

## 4. Stopped-service boundary

The smallest safe stopped set is **`api` + `worker`**. Keep `postgres` and `redis` running — the
migration needs the database, and stopping Redis would drop playback leases for no benefit. The
public `edge` may stay running.

Reasoning per service:

- **worker — must be stopped.** It is the only process that writes `video_renditions` and
  `processing_attempts`, so it is the only one that can create NULL-provenance rows after 0041. This
  is the non-negotiable part of the boundary.
- **api — stop it.** It cannot write either table, so it cannot corrupt provenance. But its readiness
  probe re-evaluates `CheckSchemaAtLeast` on **every** `/readyz` request, and `78ee4227` compiles a
  ceiling of 40 — so the moment 0041 commits, the old API starts reporting unready and never recovers
  on its own. Keeping it running buys no availability; it only produces a process that is up and
  failing. Stopping it makes the window explicit instead of ragged.
- **frontend — may stay up**, but it will be recreated by the start step anyway.
- **postgres, redis — must stay up.**
- **edge — may stay up.** Leaving it up avoids re-binding the public ports and keeps the
  `assert_edge_ports_available` check out of the critical path.

### How to stop exactly those services

`host.sh stop` is **not** the right command here. It runs `compose down`, which takes the whole
stack — Postgres included — and the migration needs Postgres. There is no `host.sh` wrapper for a
selective stop, so use Docker Compose directly against the same project and environment `host.sh`
itself uses:

```bash
docker compose --file deploy/hostinger/compose.yml --project-directory deploy/hostinger --project-name gradex-production --env-file /home/deploy/gradex-production/runtime.env stop api worker
docker compose --file deploy/hostinger/compose.yml --project-directory deploy/hostinger --project-name gradex-production --env-file /home/deploy/gradex-production/runtime.env rm --force api worker
```

Run it from the candidate release tree, so the compose file matches the release being deployed.
The second command removes only the stopped application containers. Confirm that neither `api` nor
`worker` has a container in the production Compose project; stopped, restarting and paused containers
are not quiescence proof. The rollback wrapper refuses any container in either service, whatever its
state. The production worker in this Compose project is the sole legitimate media producer connected
to this production database; Founder Beta and LG019 use separate stacks and databases. Verify at
release time that no additional producer points at the production database. Do not stop unrelated
stacks. Postgres and Redis remain running throughout.

`frontend` may also be stopped for a cleaner maintenance window — it serves a page that cannot reach
a stopped API either way. Never stop `postgres`. Redis may stay up.

The required **state transition** is what matters, not which command produces it:

> No process built from `78ee4227` may be running media work at any moment when the database is at
> schema 41.

---

## 5. Cutover sequence

The release-image selection must be in place **before** the migration runs, because the `migrate`
compose service takes `${GRADEX_BACKEND_IMAGE}` from the same runtime environment as `api` and
`worker`. Running generic `up-core` while `runtime.env` still names the `78ee4227` backend would
run the old migrate binary, whose image contains no `0041_*.sql`, and the schema would silently stay
at 40. The guarded schema-41 entry rejects that selection before running migration.

Do **not** attempt `apply-release` before the migration. With schema 40 and a candidate whose
ceiling is 41 its schema check passes, it recreates the containers, the candidate worker then
refuses its startup schema check and exits, `wait_for_status worker running` fails, and the command
dies *before* `persist_release_selection` — leaving containers moved and `runtime.env` not moved.

Required state transitions, in order:

1. **Transfer and load** the candidate release archive, verify its SHA-256 on the host before
   `docker load`, and re-verify the three image revision labels afterwards. Sync the candidate
   source checkout to the exact frozen `RELEASE_SHA`, including its Git history, so `host.sh` can
   verify the tree before it runs. Do not use an older pinned checkout.
2. **Record the pre-migration baseline**: schema version/dirty, media state counts, active claim
   count, `processing_attempts` count.
3. **Run the active-media guard.** Abort if anything is processing.
4. **Stop and remove the `78ee4227` media producer** (`worker`, and `api` per §4). Confirm both
   containers are absent, not merely unhealthy or exited.
5. **Select the candidate release** by setting the four release keys — `GRADEX_RELEASE_SHA`,
   `GRADEX_BACKEND_IMAGE`, `GRADEX_FRONTEND_IMAGE`, `GRADEX_PROOF_IMAGE` — in
   `/home/deploy/gradex-production/runtime.env` to the values in the frozen release's `release.env`.
   These are the same four keys `persist_release_selection` maintains, so the file shape is
   unchanged.
6. **Apply the migration** from the frozen `RELEASE_SHA` tree:

   ```bash
   GRADEX_HOST_STATE_DIR=/home/deploy/gradex-production \
   GRADEX_HOST_ENV_FILE=/home/deploy/gradex-production/runtime.env \
   GRADEX_HOST_PROJECT=gradex-production APP_ENV=production \
   ./deploy/hostinger/host.sh up-core-schema-41-foundation
   ```

   The command validates the production project, runtime selection, database URL target, OCI labels
   and source-tree revision before touching the database. It brings `postgres` and `redis` to healthy, runs the
   candidate `migrate` service to completion, and then starts `api`, `worker` and `frontend` on the
   candidate images. Compose additionally enforces the ordering: `api` and `worker` declare
   `depends_on: migrate: service_completed_successfully`.
7. **Verify schema 41 is clean before trusting anything else**: `SELECT version, dirty FROM
   schema_migrations` must read `41 | f`.
8. **Verify the application tier**: all three services report revision `RELEASE_SHA`, `/healthz` 200,
   `/readyz` ok with postgres/redis/schema ok, worker restart count 0, worker logs show
   `worker_lifecycle READY` rather than a `media_schema_check` exit.
9. **Start the edge** if it was stopped, and re-run the public verification.

The candidate worker's startup floor is the safety net for step 7: if the migration did not apply,
the worker refuses to start rather than running against schema 40 and stalling every upload on a
missing column. A worker that exits with `media_schema_check` after this sequence means the
migration did not happen — investigate the migration, do not restart the worker.

---

## 6. Rollback during 3C-A

Use precise language. There are two different operations and they are not independent here.

### Application rollback to `78ee4227` — requires schema rollback first

There is **no** application-only path from the frozen 3C-A release back to `78ee4227` while the
database is at schema 41. `78ee4227` compiles `MaxSchemaVersion = 40` and would refuse to start. This is enforced by
tooling, not only by documentation: `apply-release` computes `image_max_schema_version` for the
target backend and dies with `schema 41 is newer than target release maximum 40`. The wrong rollback
is refused before it recreates anything.

The generic `gradex-migrate down` cannot perform it. It is refused outright when `APP_ENV=production`
and that prohibition is deliberate and unchanged — see the D-103 procedure in
[RUNBOOK.md](RUNBOOK.md). What exists instead is one narrow supervised command that reverts exactly
this migration and nothing else:

```
gradex-migrate rollback-schema-41 -confirm-production=schema-41-to-40
```

It refuses unless every one of these holds, and every refusal happens **before** golang-migrate is
asked to do anything, so a refused rollback leaves the marker exactly as it found it — `41`, not
dirty:

- the acknowledgement is exactly `schema-41-to-40` (required in production, refused outside it);
- no positional argument is given — there is no step count and no target version to supply;
- the schema marker reads exactly `41` and is not dirty;
- every `processing_attempts` row is `attempt_kind = FULL`;
- no Asset Version holds a live `work_claim_token`.

After the step it verifies the marker landed on a clean `40` and fails loudly if it did not.

The host wrapper runs it as a one-off job on the **currently selected** backend image. Before it
launches the job, it validates the production project, runtime environment and database URL target,
release tree, all OCI
revision labels, the schema-41 implementation ancestor, and the image's schema ceiling. It requires
Postgres healthy and both `api` and `worker` containers absent:

```bash
GRADEX_HOST_STATE_DIR=/home/deploy/gradex-production \
GRADEX_HOST_ENV_FILE=/home/deploy/gradex-production/runtime.env \
GRADEX_HOST_PROJECT=gradex-production APP_ENV=production \
./deploy/hostinger/host.sh rollback-schema-41-foundation
```

The correct coordinated sequence is:

1. Run the active-media guard, then stop and remove `api` and `worker` using §4. Postgres stays up.
2. Verify no active media work, using the same guard as §3.
3. **Keep the frozen `RELEASE_SHA` backend selected.** Do not restore the baseline release selection yet: the
   baseline image contains no `0041_…down.sql` and cannot perform this rollback.
4. Run `host.sh rollback-schema-41-foundation` from the clean `RELEASE_SHA` tree, with the production
   project and runtime path explicitly declared as shown above.
5. Verify `schema_migrations` reads `40 | f`. If it does not, stop — do not start a schema-40
   application against a marker that is not a clean 40.
6. **Only now** switch the four release keys in `runtime.env` back to `78ee4227`.
7. Start the baseline application and run the usual health verification.

The ordering of steps 3 and 6 is load-bearing: selecting the baseline image before the rollback
would leave no binary on the host capable of reverting the migration.

### Schema rollback during 3C-A — open, with one accepted trade

The floor is open throughout 3C-A because **3C-A ships no producer for `ENHANCEMENT` or
`FINALIZATION`**. All current production insertion paths for `processing_attempts` rely on FULL
semantics: the two writers in `internal/media/worker.go` (successful completion and recorded
failure) and the two in `internal/media/recovery.go` (interrupted processing and interrupted
playable) all omit `attempt_kind` and take the column default, so every row written by this release
is `FULL`.

The accepted trade: renditions written on schema 41 carry `processing_operation_id`, and the down
migration drops that column. The rendition rows themselves, their storage keys, and their metadata
all survive — and the storage key still embeds a one-way hash of the writing operation, so the
association stays verifiable against a known operation ID even after the column is gone. Losing the
readable column during a foundation-only rollback is acceptable; refusing rollback because of it
would close the floor the instant the first video was processed, which is the opposite of what a
foundation release needs.

---

## 7. 3C-B closes the schema-40 floor — permanently

**The instant 3C-B persists its first `ENHANCEMENT` or `FINALIZATION` `processing_attempts` row —
`FAILED` or `SUCCEEDED`, it makes no difference — schema 41 → 40 becomes unavailable by design.**

From that moment `78ee4227` is permanently outside the normal rollback chain, and the deployed 3C-A
revision becomes the schema-41-compatible application rollback floor.

| Phase | Deployed | Rolls back to |
| --- | --- | --- |
| 3C-A | frozen `RELEASE_SHA` + schema 41 | remove producers → down 41 → 40 → `78ee4227` + schema 40 |
| 3C-B (after first non-FULL row) | new app + schema 41 | the exact deployed 3C-A `RELEASE_SHA` + **schema 41** — not to schema 40 |

### Prerequisite on the 3C-B release plan

Before 3C-B may deploy, its release plan must record the exact deployed 3C-A revision as the staged
schema-41-compatible rollback target, and that image, tree and release manifest must still be
available on the host. **Do not deploy 3C-B without it.** There is no other rollback target once the
schema-40 floor closes.

---

## 8. Preflight-to-down race — a 3C-B concern, not a 3C-A one

The `migrate` CLI preflight runs before golang-migrate executes the down step. Under a future 3C-B
system this ordering admits a race:

> preflight observes all-FULL → another process inserts an `ENHANCEMENT` or `FINALIZATION` row → the
> down step begins → the SQL defence in `0041_…down.sql` refuses → the evidence stays safe, but the
> migration marker can be left dirty.

This race **cannot occur during 3C-A**, because no non-FULL producer exists.

Before 3C-B, any schema-down procedure must quiesce every non-FULL processing-attempt producer
before running the preflight and keep them quiesced until the migration completes. The SQL refusal is
defence in depth against a direct runner — it is not concurrency synchronisation and must never be
relied on as such.

---

## 9. Failure matrix

| Situation | Action |
| --- | --- |
| **A.** Failure before the old services are removed | Schema remains clean 40; abort the cutover and keep `78ee4227` running. |
| **B.** Failure after removal but before migration | Schema remains clean 40; restore the baseline runtime selection if changed, then start `78ee4227`. The guarded forward command refuses an older or mismatched candidate before touching Compose. |
| **C.** UP migration fails | The old application remains removed and candidate services do not start. Inspect the real schema marker; restart `78ee4227` only if clean 40 is proven. A dirty marker requires supervised recovery. |
| **D.** Schema is 41 clean and the candidate fails to start or fails health checks | Fix the schema-41 candidate forward or follow §6: remove any producers, retain the candidate image through supervised DOWN, verify clean 40, then select and start `78ee4227`. |
| **E.** Candidate starts healthy, a regression is found later | Re-run the active-media guard, then use the same coordinated path as D if the all-FULL rollback floor is still open. |
| **F.** The supervised rollback **refuses** | **Do not start `78ee4227` while schema remains 41.** An active claim needs investigation and settlement with producers absent; a non-FULL attempt closes the floor. Keep the application stopped or fix forward with a schema-41-compatible build. |
| **G.** Rollback fails *after* migration began | Read the command's actual marker state. Do not force a version or rewrite evidence. Resolve a dirty or unverified schema under supervision before starting any application; `78ee4227` may start only after clean 40 is proven. |

---

## 10. Verification after cutover

Schema and release:

- Schema `41`, `dirty = false`.
- All three services report the frozen `RELEASE_SHA`; restart counts 0; worker logs show
  `worker_lifecycle READY`, not a `media_schema_check` exit.

Worker configuration — verify explicitly, they are the media pipeline's safety envelope:

- exactly **one** worker container running;
- `MEDIA_TRANSCODE_CONCURRENCY=1`;
- `MEDIA_PROCESSING_TIMEOUT=6h`.

Health:

- `/healthz` 200; `/readyz` 200 with postgres, redis and schema all ok;
- public edge verification passes.

Data:

- `SELECT count(*) FROM processing_attempts WHERE attempt_kind <> 'FULL'` returns **0**, confirming
  the schema-40 rollback floor is still open.
- No unexpected active media claim appears immediately after the release — release machinery itself
  starts no media work.
- **Record** the media state counts and the `processing_attempts` count rather than requiring them to
  match the pre-migration baseline. Once the worker is running, legitimate production activity can
  change them at any time; a changed count is a thing to explain, not a failure. Investigate only
  transitions that no legitimate activity accounts for.
- Existing `video_renditions` rows still read `processing_operation_id IS NULL`. The column is
  written only for rows created after the cutover, and nothing is backfilled.

---

## 11. Related

- [D-107](../DECISIONS.md#d-107--the-hls-ladder-is-a-frozen-data-contract-and-phase-3c-a-adds-attempt-kind-and-rendition-provenance) — the decision this release implements, including the rollback-floor rule.
- [RUNBOOK.md](RUNBOOK.md) — standing operational procedures.
- [STATUS.md](STATUS.md) — current delivery state.
