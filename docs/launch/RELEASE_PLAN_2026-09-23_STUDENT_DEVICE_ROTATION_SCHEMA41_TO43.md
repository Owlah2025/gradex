# Student device rotation — schema 41 → 42 → 43 release plan

**State:** Offline candidate. Independent re-review and Product Owner release approval are required before any release-window mutation. This plan uses the first live read-only preflight as its observed input; a future preflight must rediscover current production facts.

## 1. Observed starting point

The first live read-only preflight observed srv1900125, selected SHA 98e88fcc1105e8c638bb638d3f1c46630bcc51b2, and clean schema 41. API and frontend were healthy, readiness returned OK, and the worker was running. All three application image labels matched 98e88fcc1105e8c638bb638d3f1c46630bcc51b2.

This explicitly prepares schema 41. It replaces the prior schema-38/schema-42 start assumptions. If a future read-only preflight observes a dirty marker or a different release/schema path, stop and rederive before maintenance.

The revised read-only preflight verified the selected host manifest and image bindings, clean schema marker, database health, media/claim and enhancement queue state, historical sessions/OTP/email, trusted-device counts, and session/device coherence. Its observed counts are not reusable at maintenance time; the deployment run must repeat role-annotated discovery and derive fresh cleanup counts. The newer backup snapshot and isolated restore verification are recorded in Section 8 and its evidence file.

## 2. Required artifacts and schema compatibility

These floors and ceilings are read from the exact application sources and schema readiness code:

| Artifact | Revision | API floor / ceiling | Worker floor / ceiling | Role |
| --- | --- | --- | --- | --- |
| Current recovery | 98e88fcc1105e8c638bb638d3f1c46630bcc51b2 | 38 / 41 | 41 / 41 | Only application for clean 41. |
| Frozen 3C-B candidate | 3383f46d0e9e6379c3bd166d39622c3659ae3d86 | 38 / 42 | 42 / 42 | Cross only the reviewed 41 → 42 boundary; also serves clean 42. |
| Previously reviewed device base | f41f9c28d67aa06ec370ba4f1a8c7d1192d5986d | 43 / 43 | 42 / 43 | Preserve and stage as requested; its image predates the separately approved expired-Staff cleanup CLI and must not execute that mode. |
| Cleanup-capable device candidate | `DEVICE_RELEASE_SHA` (built from the committed remediation source and recorded in the artifact evidence) | 43 / 43 | 42 / 43 | Execute both schema-42 cutover modes and serve after clean 43; device-admission behavior remains the reviewed f41 behavior. |
| Schema-43 application rollback | 54115fd6029d5d80af63640ac6f0bfe31be22d67 | 43 / 43 | 42 / 43 | Restore prior application behavior on unchanged schema 43. |

The operative four-artifact matrix is 98e88 recovery, frozen 3383 transition, `DEVICE_RELEASE_SHA`, and 54115fd rollback; f41f9c28 is preserved as the reviewed behavior base and separately staged. At clean 42, 98e88 is too old and 3383 is the only application safe to serve. One-off Staff/Student cutover and migration commands use the cleanup-capable backend image with `--no-deps`; its API is never started on schema 42 because its readiness floor is 43. The cutover only revokes authorized historical session families and supersedes Student device OTPs using schema-42-compatible values; it does not write `AUTO_REPLACED`. At schema 43, use `DEVICE_RELEASE_SHA` or 54115fd. Dirty schema markers admit no application.

The local schema-42 artifact was built from the exact 3383 commit and exported with release.sh. Its manifest, image archive and tooling checksums verify; the bundle declares SCHEMA42_CAPABILITY=manual-enhancement-v1; labels and IDs agree; backend max-version is 42; 0042 hashes agree; gradex-enhancement-drain exists. The local 98e88 copy matches the previously observed selected SHA and image IDs. The previous live preflight verified the actual 98e88 manifest; staging of 3383, f41f9c28, 54115fd, and the cleanup-capable `DEVICE_RELEASE_SHA` remains part of this preparation.

## 3. Reuse the reviewed schema-42 boundary unchanged

The approved forward entry remains the imported 3383 command:

    host.sh up-core-schema-42-enhancement-recovery

It validates production scope, exact bundle capability and SHA, OCI revision labels and image IDs, max-version exactly 42, 0042 UP/DOWN image hashes, the drain executable, and the complete 98e88 schema-41 recovery floor. It requires API and worker absent, inventories local worker producers targeting gradex_production, starts PostgreSQL/Redis and waits healthy, requires clean schema 41 and no work_claim_token, then runs the migrate Compose service with gradex-migrate up. Migration 0042 itself refuses SCANNING, PROCESSING, or any claimed row. The wrapper verifies clean schema 42 and starts API, worker and frontend privately. It does not start the edge; it also does not verify that an edge left running by the prior deployment is stopped.

The separate enhancement-drain command is for supervised 42 → 41 rollback, not forward 41 → 42. Rollback also requires the 3383 capability/floor, clean 42, healthy PostgreSQL/Redis, absent API/worker, producer inventory clear, zero active claims and a successful read-only queue/outbox drain. It then invokes only gradex-migrate rollback-schema-42 -confirm-production=schema-42-to-41, verifies clean 41 and stops. Generic production DOWN remains prohibited. The drain reports pending, active, scheduled, retry, archived and aggregating work; completed work is reported separately. It deletes no work.

### Migration-by-migration requirements

The observed clean schema-41 start already contains migrations 0039, 0040, and 0041. They are not replayed in this release; the details below establish why the reviewed 41 → 42 boundary is the correct next step and which lower rollback boundaries remain supervised.

| Migration | Forward effect and exact precondition | Reverse behavior / release rule |
| --- | --- | --- |
| 0039 `media_playable_enum` | From clean 38, adds `PLAYABLE` after `PROCESSING` to `media_asset_version_state` with `ADD VALUE IF NOT EXISTS`; no row-state predicate. | Down is intentionally a no-op because PostgreSQL cannot drop an enum value in place; the unused label remains. |
| 0040 `playable_state_foundation` | From clean 39, widens the work-claim constraint/index and replaces the media immutability trigger to represent `PLAYABLE`; no explicit drain query. | Down restores the narrower schema-39 rules. Recreating the old claim constraint fails if an active `PLAYABLE` row still carries a claim. Do not use generic DOWN. |
| 0041 `enhancement_recovery_foundation` | From clean 40, adds attempt kind with existing attempts defaulted to `FULL`, result-coherence rules, and nullable rendition operation IDs. The migration itself has no active-work predicate and introduces no producer/runtime behavior. | Down refuses before DDL if any attempt is not `FULL` (including failed attempts), or if a succeeded row has evidence schema 40 cannot represent. Keep the schema-41 recovery floor; do not force this downgrade. |
| 0042 `active_processing_attempt_kind` | From clean 41, refuses if any media version is `SCANNING`, `PROCESSING`, **or** has non-NULL `work_claim_token`; then adds the active-attempt-kind column and coherence constraint. The frozen host wrapper additionally requires the exact 3383 artifact/capability and 98e88 recovery floor, clean 41, healthy Postgres/Redis, absent API/worker, and no local production worker producer. | The SQL down refuses `PROCESSING` or `PLAYABLE` with a live claim. The supervised host rollback is stricter: it requires no active claims of any state, no producer, and the successful read-only enhancement outbox/queue drain before invoking only `rollback-schema-42 -confirm-production=schema-42-to-41`. |
| 0043 `auto_device_replacement` | From clean 42, adds `AUTO_REPLACED` to both trusted-device and session revocation enums with `ADD VALUE IF NOT EXISTS`; no data predicate. Apply only after historical-session cutover, with all writers absent and the exact cleanup-capable image/migration hashes verified. | Down refuses if either table contains `AUTO_REPLACED`; it will not rewrite audit evidence. After that evidence exists, keep schema 43 and use the schema-43-compatible application rollback. |

For this observed starting point, 0039–0041 remain installed. Only 0042 is crossed by the frozen schema-42 command; after clean 42 and the device cutovers, 0043 is the only pending migration in the verified cleanup-capable image. No generic production `gradex-migrate up` substitutes for the 0042 wrapper.

## 4. Combined maintenance sequence: closed-edge composition

**Decision: option A.** The frozen 41 → 42 command can participate safely without changing frozen tooling if the edge is stopped before invocation and remains stopped. Its automatic API/worker/frontend start is private; the edge is not started by the command. The operator then quiesces that private tier before device cutover. No new production orchestration command is required; this release plan composes the approved primitive explicitly.

### Mandatory public-edge guard

Immediately before `up-core-schema-42-enhancement-recovery`, record the output of these read-only checks. The edge must be absent or stopped, Docker must show no running container publishing TCP 80 or 443, and the host must have no TCP listener on either port. Any unexpected listener is a stop condition.

~~~bash
docker ps -a --filter label=com.docker.compose.project=gradex-production \
  --filter label=com.docker.compose.service=edge --format '{{.Names}} {{.State}}'
docker ps --filter publish=80 --format '{{.Names}} {{.Ports}}'
docker ps --filter publish=443 --format '{{.Names}} {{.Ports}}'
sudo ss -ltnp '( sport = :80 or sport = :443 )'
~~~

The Compose-label query must return zero rows or only stopped/exited edge containers; a running edge fails. (Do not rely on the literal `gradex-production-edge` container name; Compose may suffix it.) The two published-port `docker ps` commands and `ss` must return no listener rows. Repeat the same guard after 3383's private startup and before cleanup, then record API/frontend port bindings by Compose service labels:

~~~bash
for service in api frontend; do
  docker ps -a --filter label=com.docker.compose.project=gradex-production \
    --filter "label=com.docker.compose.service=$service" --format '{{.Names}}' |
    while IFS= read -r container; do
      [ -n "$container" ] && docker inspect --type container \
        --format '{{.Name}} {{json .HostConfig.PortBindings}}' "$container"
    done
done
~~~

The edge and host TCP listeners must still be absent, and each API/frontend binding must be empty/null. This adds an operator-recorded guard around frozen tooling; it does not modify `host.sh`.

1. Complete the refreshed read-only preflight, obtain independent review and Product Owner approval, and verify 98e88 recovery, 3383, the staged f41 behavior base, `DEVICE_RELEASE_SHA`, 54115fd, backup and restore evidence.
2. In the approved maintenance window, set HOST_STATE=/home/deploy/gradex-production and TOOLING_ROOT=$HOST_STATE/releases/3383f46d0e9e6379c3bd166d39622c3659ae3d86/tooling. Use the frozen 3383 Compose file/project and protected runtime.env:

~~~bash
HOST_STATE=/home/deploy/gradex-production
TOOLING_ROOT="$HOST_STATE/releases/3383f46d0e9e6379c3bd166d39622c3659ae3d86/tooling"
COMPOSE_FILE="$TOOLING_ROOT/deploy/hostinger/compose.yml"
docker compose --file "$COMPOSE_FILE" --project-directory "$TOOLING_ROOT/deploy/hostinger" \
  --project-name gradex-production --env-file "$HOST_STATE/runtime.env" \
  stop edge api worker frontend
docker compose --file "$COMPOSE_FILE" --project-directory "$TOOLING_ROOT/deploy/hostinger" \
  --project-name gradex-production --env-file "$HOST_STATE/runtime.env" \
  rm --force api worker
~~~

Confirm edge is stopped and API/worker containers are absent; keep PostgreSQL and Redis healthy. Atomically select the exact 3383 manifest in runtime.env by replacing only `GRADEX_RELEASE_SHA`, `GRADEX_BACKEND_IMAGE`, `GRADEX_FRONTEND_IMAGE`, and `GRADEX_PROOF_IMAGE`. Do not use apply-release here because it would start the application before 0042. Keep 98e88 staged.
3. From the imported 3383 tooling root, run the frozen command with the production environment explicitly named:

~~~bash
GRADEX_HOST_STATE_DIR=/home/deploy/gradex-production \
GRADEX_HOST_ENV_FILE=/home/deploy/gradex-production/runtime.env \
GRADEX_HOST_PROJECT=gradex-production APP_ENV=production \
/home/deploy/gradex-production/releases/3383f46d0e9e6379c3bd166d39622c3659ae3d86/tooling/deploy/hostinger/host.sh \
  up-core-schema-42-enhancement-recovery
~~~

It verifies schema 41, media claims, producer inventory, artifact floor and migration binding, applies 0042 and verifies clean 42, then starts API/worker/frontend. Traffic remains closed because edge is stopped.
4. Let the private 3383 worker finish existing work. Use the read-only queue/outbox snapshots until default-queue pending, active, scheduled, retry, and aggregating counts are zero; no media claim or undispatched media event remains; and no transactional-email delivery is QUEUED or SENDING. Completed and archived counts remain recorded. Then stop/remove the idle worker and stop/remove API/frontend. Recheck the public-edge guard, application-writer absence, media claims, and historical-session counts. If work or a claim remains, do not cut over; remain at clean 42 and let 3383 settle it only while public traffic remains closed.

### 4a. One-time expired Staff legacy cleanup

With all application writers absent and the public-edge guard passing, take a fresh role-annotated discovery. The Founder-authorized Staff cleanup is limited to ACTIVE Admin/Instructor `LEGACY_UNBOUND` sessions where `absolute_expires_at` is no later than the database decision time. Any unexpired Admin/Instructor legacy session, any active non-Student `PENDING_DEVICE_TRUST` session, or any historical session under an unexpected role stops the cleanup and requires a new Product Owner disposition. These states are not included in the authorization and remain untouched.

Set the cleanup-capable schema-43 backend image override (the exact revision is recorded in the artifact matrix) and run a new read-only Staff check:

~~~bash
DEVICE_RELEASE_SHA=DEVICE_RELEASE_SHA
DEVICE_MANIFEST="$HOST_STATE/releases/$DEVICE_RELEASE_SHA/release.env"
DEVICE_BACKEND="$(awk -F= '$1 == "GRADEX_BACKEND_IMAGE" {print substr($0,index($0,"=")+1)}' "$DEVICE_MANIFEST")"
GRADEX_BACKEND_IMAGE="$DEVICE_BACKEND" docker compose \
  --file "$COMPOSE_FILE" --project-directory "$TOOLING_ROOT/deploy/hostinger" \
  --project-name gradex-production --env-file "$HOST_STATE/runtime.env" \
  run --rm --no-deps --entrypoint gradex-device-cutover migrate \
  -mode=check-expired-staff-legacy
~~~

Only if the three blocker counts are zero and the eligible count matches the reviewed discovery may the approved maintenance execution apply that exact count. The operation locks the session write set and target families, rechecks the predicates/count inside one transaction, calls the canonical family revoker, and writes per-session `EXPIRED_LEGACY_SESSION_CLEANUP` audit evidence. It never changes trusted devices or `accounts.session_epoch`.

~~~bash
: "${EXPIRED_STAFF_LEGACY_COUNT:?copy exact eligible count from the immediately preceding check}"
: "${OPERATOR:?set named change operator}"
: "${TICKET:?set approved change ticket}"
GRADEX_BACKEND_IMAGE="$DEVICE_BACKEND" docker compose \
  --file "$COMPOSE_FILE" --project-directory "$TOOLING_ROOT/deploy/hostinger" \
  --project-name gradex-production --env-file "$HOST_STATE/runtime.env" \
  run --rm --no-deps --entrypoint gradex-device-cutover migrate \
  -mode=apply-expired-staff-legacy -expected-schema=42 \
  -expected-expired-staff-legacy="$EXPIRED_STAFF_LEGACY_COUNT" \
  -operator="$OPERATOR" -request-id="$TICKET" \
  -confirm=REVOKE_EXPIRED_STAFF_LEGACY_SESSIONS
~~~

Verify afterward that no non-Student historical session remains. Never reuse the Staff count for the independent Student cutover gate.

5. With every application writer absent, perform a fresh Student cutover check. Any count drift, device invariant violation, outstanding device email or non-Student historical residue stops before Student apply. Use the same cleanup-capable manifest/image and compare this result to the immediately preceding clean-42 discovery:

Using the cleanup-capable backend image override for this one-off only, run `gradex-device-cutover -mode=check`. Compare returned schema/counts to the fresh clean-42 discovery after Staff cleanup. Only the later approved window may repeat the command with `-mode=apply`, `-expected-schema=42`, exact `-expected-legacy/-expected-pending/-expected-otp` counts, named `-operator`, change-ticket `-request-id`, and `-confirm=REVOKE_HISTORICAL_DEVICE_SESSIONS`. The transaction revokes only Student historical families and supersedes outstanding device OTPs. Require zero residue afterward. The runtime selection remains 3383 until the application-only release step later.

~~~bash
GRADEX_BACKEND_IMAGE="$DEVICE_BACKEND" docker compose \
  --file "$COMPOSE_FILE" --project-directory "$TOOLING_ROOT/deploy/hostinger" \
  --project-name gradex-production --env-file "$HOST_STATE/runtime.env" \
  run --rm --no-deps --entrypoint gradex-device-cutover migrate -mode=check
~~~

For the later approved apply, copy the exact schema/counts from the immediately preceding `-mode=check` output and set the operator/ticket fields from the approved change record:

~~~bash
: "${LEGACY_COUNT:?copy exact legacy count from cutover check}"
: "${PENDING_COUNT:?copy exact pending count from cutover check}"
: "${OTP_COUNT:?copy exact outstanding OTP count from cutover check}"
: "${OPERATOR:?set named change operator}"
: "${TICKET:?set approved change ticket}"
GRADEX_BACKEND_IMAGE="$DEVICE_BACKEND" docker compose \
  --file "$COMPOSE_FILE" --project-directory "$TOOLING_ROOT/deploy/hostinger" \
  --project-name gradex-production --env-file "$HOST_STATE/runtime.env" \
  run --rm --no-deps --entrypoint gradex-device-cutover migrate \
  -mode=apply -expected-schema=42 \
  -expected-legacy="$LEGACY_COUNT" -expected-pending="$PENDING_COUNT" \
  -expected-otp="$OTP_COUNT" -operator="$OPERATOR" -request-id="$TICKET" \
  -confirm=REVOKE_HISTORICAL_DEVICE_SESSIONS
~~~

6. Recheck clean 42, healthy Postgres/Redis, edge/API/worker absent, zero active media claims, and the cleanup-capable manifest, revision label, image ID, schema ceiling 43, and 0043 UP/DOWN hashes. The 0043 hashes must match between the verified manifest/image and the selected source artifact. No app writer runs between the check and migration.

7. Run the cleanup-capable image's gradex-migrate up only after the exact clean-42 check and successful cutover. This does not replace the frozen 41 → 42 command: the approved 3383 command already crossed that boundary with all artifact/media/producer gates. At clean 42, 0043 is the only pending migration in the verified image. This one-off uses --no-deps and starts no application service:

~~~bash
GRADEX_BACKEND_IMAGE="$DEVICE_BACKEND" docker compose \
  --file "$COMPOSE_FILE" --project-directory "$TOOLING_ROOT/deploy/hostinger" \
  --project-name gradex-production --env-file "$HOST_STATE/runtime.env" \
  run --rm --no-deps migrate gradex-migrate up
~~~

Verify schema 43 is clean and both AUTO_REPLACED labels exist before selecting an app.
8. Use the frozen host wrapper’s application-only apply-release with the staged cleanup-capable manifest. It starts API/worker/frontend with --no-deps, verifies health/readiness and then persists the new device release selection. Keep edge closed through core verification and private smoke checks; reopen it only after approved public smoke tests.

~~~bash
GRADEX_HOST_STATE_DIR="$HOST_STATE" \
GRADEX_HOST_ENV_FILE="$HOST_STATE/runtime.env" \
GRADEX_HOST_PROJECT=gradex-production APP_ENV=production \
"$TOOLING_ROOT/deploy/hostinger/host.sh" apply-release \
  "$HOST_STATE/releases/$DEVICE_RELEASE_SHA/release.env"
~~~

If `DEVICE_RELEASE_SHA` cannot become ready while the marker is clean 43, keep the edge closed and use the staged 54115fd manifest with the same `apply-release` command. Do not select f41f9c28, 3383, or 98e88 against schema 43. If the schema marker is dirty, start no application and enter operator recovery.

If a 3383 worker claim appears after its private startup, do not interrupt or clear it to continue. Stay on clean 42, recover with 3383, and wait for the media operation to settle. If the private 3383 start fails but schema remains clean 42, 3383 remains the recovery application. If clean 42 cannot be confirmed, start no application.

### Clean-42 service and resumption rule

If 3383 is selected on clean schema 42 and public traffic is reopened, its legacy login behavior can create new `PENDING_DEVICE_TRUST` sessions and `DEVICE_TRUST_OTP` rows. Any later attempt to resume 42 → 43 must close public traffic and writers again, rerun role-annotated historical-session and OTP discovery, derive fresh counts, rerun the authorized expired-Staff cleanup if eligible rows exist, rerun the Student cutover, and verify zero residue before attempting 0043. Counts from an earlier cutover are never reusable after 3383 has served traffic.

## 5. Device cutover position and role-specific gates

Run the one-time expired Staff cleanup after clean 42, then run the Student cutover after clean 42 and before 0043. Both are separate operations with independent exact-count gates; neither shares or reuses the other's counts. The CLI accepts expected schemas only through 42. This order makes clean 42 with the 3383 application a valid recovery point if either cutover completes but 0043 does not commit. All application writers are absent, and the cleanup's session table lock plus row locks make its eligible set stable through commit.

`ApplyExpiredStaffLegacyCleanup` is narrowly scoped to active Admin/Instructor `LEGACY_UNBOUND` sessions whose absolute expiry has passed at one database timestamp. It refuses if any active Admin/Instructor legacy session is unexpired, if any non-Student `PENDING_DEVICE_TRUST` session remains, or if an unexpected role has historical state. It revokes through `revokeSessionFamily`, records `RELEASE_OPERATOR` audit evidence, changes no trusted device, and does not increment any session epoch.

The Student `ApplyDeviceCutover` behavior is unchanged: its read counts active historical sessions and outstanding device OTPs across roles, locks Student family targets, and fails closed if locked Student rows do not match the all-role counts. Therefore the expired Staff cleanup must complete first. A fresh Student `-mode=check` then supplies the independent expected legacy/pending/OTP counts; after Student apply, verify zero Student residue and zero non-Student historical sessions before 0043.

The Product Owner authorized only the one-time expired Admin/Instructor legacy cleanup in this plan. Any non-expired Staff legacy family, any pending non-Student family, or any other non-Student historical family remains outside that authorization and stops cutover pending a new disposition.

No cleanup changes trusted families, trusted devices, or `accounts.session_epoch`.

## 6. Recovery matrix

| Database state | Safe application | Recovery |
| --- | --- | --- |
| Clean 41 | 98e88fcc | Keep/select the verified current release. |
| Dirty 41 | None | Operator recovery only. |
| Clean 42 before 0043, before or after device cutover | 3383f46 | Keep schema 42 and restore/select the exact 3383 artifact. The completed cutover remains durable and safe for 3383. To return to 41, use only the supervised 42 → 41 command and all drain gates, then select 98e88. |
| Dirty 42 | None | Operator recovery only; never force a marker. |
| Clean 43 before traffic | `DEVICE_RELEASE_SHA` or 54115fd | Either schema-43 application can serve. |
| Clean 43 with AUTO_REPLACED evidence | `DEVICE_RELEASE_SHA` or 54115fd | Keep schema 43; 54115fd restores the previous application behavior. |
| Dirty 43 | None | Operator recovery only. |

0043 DOWN refuses while AUTO_REPLACED device/session evidence exists. Schema 43 application rollback is not a schema downgrade.

After any migration command exits nonzero, query `schema_migrations` read-only and use the observed marker, not the command's exit status, to choose recovery: clean 41 → 98e88, clean 42 (including after the device cutover) → 3383, clean 43 only after verifying both enum labels → f41 or 54115, and any dirty marker → no application/operator recovery. Never force or hand-edit the schema marker and never run generic DOWN.

## 7. Resumable read-only production preflight

The next preflight must recheck local source/worktree, production hostname/time, selected release manifest, running API/worker/frontend image labels/IDs/status, health/readiness and schema. Clean schema 41 is the expected start and continues preflight. Dirty markers or a different path stop and require rederivation.

Collect active historical sessions grouped by role and trust state; details must include account role/id, session id, created/authenticated/activity timestamps and idle/absolute expiry only. Never select session credentials or bearer values. The only non-Student state eligible for the authorized cleanup is expired Admin/Instructor `LEGACY_UNBOUND`; non-expired Staff legacy, any non-Student pending session, and unexpected-role historical state remain fail-closed decision gates. Collect OTP totals, unexpired totals, distinct accounts, oldest/newest issue time and latest expiry without selecting secret_digest. Count device-trust outbox/delivery work by missing ledger, queued, sending, accepted and terminal failure state, with recent failures separately. Stop maintenance if any device-trust email remains queued/sending.

Collect Student device counts 0/1/2/>2 and account IDs for >2; never heal them automatically. Check active Student sessions for TRUSTED with null device id, TRUSTED with missing/revoked/untrusted device, and non-TRUSTED with a device id.

Read media state counts and exact 0042 predicate:

~~~sql
SELECT state, count(*) AS versions,
       count(*) FILTER (WHERE work_claim_token IS NOT NULL) AS claimed
FROM media_asset_versions
GROUP BY state ORDER BY state;

SELECT count(*) AS incompatible_active_media
FROM media_asset_versions
WHERE state IN ('SCANNING','PROCESSING')
   OR work_claim_token IS NOT NULL;
~~~

The first live preflight stopped before these reads. This plan requires them now; do not clear claims or retry jobs.

The 98e88 worker image lacks gradex-enhancement-drain. The read-only preflight must not start a one-off container or import an image. Gradex configures the Asynq worker with only the `default` queue, and the media dispatcher enqueues `media:scan`, `media:transcode`, and `media:enhancement` task types there; aggregate queue lengths cannot isolate the enhancement type. The new inspector reads each default-queue task's serialized message through the already-running Redis container, decodes only the protobuf type in a local pipe, and prints per-state queue totals and media:enhancement counts. It reads state keys directly because the queue registry may omit a queue while its completed-task retention records remain. It never prints task IDs, payloads, or secrets. The companion verifier allows only Redis `HGET`, `LRANGE`, `ZRANGE`, and `SMEMBERS`; every other emitted command, including unapproved reads, fails the proof.

After rechecking the host identity and SSH target, stream the reviewed script to the existing host shell. This writes no host file and creates no container:

~~~bash
PRODUCTION_SSH_TARGET=deploy@186.241.16.111
ssh "$PRODUCTION_SSH_TARGET" 'bash -s' < deploy/device43/inspect-enhancement-queue-readonly.sh
~~~

The script discovers the Redis container by Compose labels; REDISCLI_AUTH and the CA are already available inside it. It uses only Docker ps/exec and Redis LRANGE, ZRANGE, SMEMBERS, and HGET reads. A failed lookup or malformed message exits non-zero; never treat a partial result as zero. It reports pending, active, scheduled, retry, archived, aggregating, and completed queue totals with the media:enhancement subset; completed is separate. Also count undispatched media.enhancement_requested outbox intents in PostgreSQL. Do not pause, drain, reschedule, or delete work.

Verify the actual selected host release.env, its SHA-256, release SHA, backend/frontend/proof image names and IDs, matching labels, image archive checksum, deploy bundle checksum and manifest bundle hash, capability, 0042 hashes and backend max-version. Verify these values match protected runtime.env. Do not edit, import or select a manifest during preflight.

## 8. Backup, restore and RPO gate

The maintenance window requires a verified offsite backup for production Postgres captured at clean schema 41 and a successful isolated restore check. The refreshed read-only preflight reads `backups/latest.completed-at`, `backups/latest.offsite.snapshot`, matching remote restic metadata, and the encrypted snapshot's schema-state sidecar; the sidecar must say `41|false`. The backup source is the PostgreSQL container selected by the production project and `pg_dump --dbname "$POSTGRES_DB"`, which must resolve to `gradex_production` in the protected runtime environment.

The backup tooling does not persist a dedicated successful-restore timestamp. `restored-source` identifies a restore attempt's snapshot, and a running `gradex-restore-verify` container alone is not proof of a successful verification. Require a timestamped operator-retained transcript or journal record of `host.sh verify-restore` tied to the same snapshot ID, plus the resulting schema/record-count verification. The latest completed schema-41 snapshot observed before staging, `be759dc254199749ac8ae3a22d47a4f604abea83d50d41db64c576c0797d7222`, passed isolated restore verification; details and transcript digest are recorded in [the schema-41 restore evidence](evidence/2026-09-23-schema41-isolated-restore-verification.md). It used a dedicated no-published-port restore container/volume on the default bridge, separate from the live PostgreSQL container and application networks.

Under [D-112](../DECISIONS.md#d-112--founder-approved-postgresql-recovery-target-for-launch), the PostgreSQL RPO target is one hour only when the scheduled hourly backup completes successfully. The operational RTO target is four hours, not a contractual SLA. WAL/PITR is not implemented, and a 15-minute RPO is not an approved launch commitment. The selected snapshot was about 26 minutes old at restore start, restored to schema `41|false`, and passed isolated verification in 33 seconds. Recheck snapshot age immediately before maintenance; stale/missing backup, missing database coverage, or absent/failed restore evidence blocks maintenance.

## 9. Canonical catalogue-import E2E classification

[D-111](../DECISIONS.md#d-111--canonical-catalogue-import-e2e-409-classification-accepted-narrowly) is accepted by the Product Owner only for the exact canonical catalogue-import HTTP 409 / `importer.ErrIdentityRebind` reproduced on f41f9c28 and frozen 3383f46. This evidence shows the same failure on both artifacts and establishes that it predates the device/schema43 remediation.

Accepted classification: PRE_EXISTING_DETERMINISTIC_FIXTURE_OR_TEST_ENVIRONMENT_DEFECT.

Scope: track this exact deterministic fixture/test-environment defect separately. The acceptance does not waive any future or changed E2E failure signature, unrelated E2E regressions, or the importer identity-rebind protection.

## 10. Offline proof and frozen boundary

The new local verifier validates the exact 3383 artifact and its 0042 capability/hash bindings, compiles the 3383 API readiness probe, and drives a disposable database through clean 41 → 42 → device cutover → 43. It does not contact production and does not prove the full Hostinger Docker orchestration. In particular, it mocks the edge lifecycle, real Compose orchestration, the private worker startup window, production Redis state, and some producer-inventory behavior. Those production gates remain mandatory in the maintenance runbook.

~~~bash
bash deploy/scripts/verify-schema-42-rollback.sh
bash deploy/device43/verify-schema41-to43.sh /path/to/verified/3383f46/release
git diff --stat 3383f46d0e9e6379c3bd166d39622c3659ae3d86..HEAD
git diff 3383f46d0e9e6379c3bd166d39622c3659ae3d86..HEAD -- backend/internal/media backend/internal/playback
git diff 3383f46d0e9e6379c3bd166d39622c3659ae3d86..HEAD -- \
  backend/internal/db/migrations/0039* \
  backend/internal/db/migrations/0040* \
  backend/internal/db/migrations/0041* \
  backend/internal/db/migrations/0042*
~~~

Migrations 0039–0042, their release tooling, media/queue/outbox/claim behavior and playback concurrency remain frozen. Any semantic diff in those paths stops this release.

## 11. Production monitor note

`gradex-production-monitor.service` is the working production monitor and remains authoritative. The separate old `gradex-monitor.service` fails before running probes because `APP_ENV` is unset; inspection found no service-control or notification side effect before that failure, only repeated unit failure logs. Leave it unchanged for this release and track its retirement as post-release cleanup. The working monitor may emit its normal health alert while the edge is intentionally closed; treat that as the planned maintenance outage, not as an effect of the duplicate unit.

## 12. Approval boundary

Preparation includes non-serving artifact staging and an isolated restore verification; it does not select a release or change live application/database state. Repeat the production preflight immediately before maintenance. Migration, Staff/Student cutover apply, runtime release selection, application service stop/start, edge reopening, and smoke traffic require the later independent review and explicitly approved maintenance window. The sequence includes user-visible downtime.
