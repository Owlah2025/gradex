# Student device rotation — schema 41 → 42 → 43 release plan

**State:** Offline candidate. Independent re-review and Product Owner release approval are required before any release-window mutation. This plan uses the first live read-only preflight as its observed input; a future preflight must rediscover current production facts.

## 1. Observed starting point

The first live read-only preflight observed srv1900125, selected SHA 98e88fcc1105e8c638bb638d3f1c46630bcc51b2, and clean schema 41. API and frontend were healthy, readiness returned OK, and the worker was running. All three application image labels matched 98e88fcc1105e8c638bb638d3f1c46630bcc51b2.

This explicitly prepares schema 41. It replaces the prior schema-38/schema-42 start assumptions. If a future read-only preflight observes a dirty marker or a different release/schema path, stop and rederive before maintenance.

The first preflight did not verify the host release.env, backup/recovery evidence, or media, queue, session, OTP, email, device-invariant, and coherence counts. Those remain blocking preflight items.

## 2. Four artifact roles and schema compatibility

These floors and ceilings are read from the exact application sources and schema readiness code:

| Artifact | Revision | API floor / ceiling | Worker floor / ceiling | Role |
| --- | --- | --- | --- | --- |
| Current recovery | 98e88fcc1105e8c638bb638d3f1c46630bcc51b2 | 38 / 41 | 41 / 41 | Only application for clean 41. |
| Frozen 3C-B candidate | 3383f46d0e9e6379c3bd166d39622c3659ae3d86 | 38 / 42 | 42 / 42 | Cross only the reviewed 41 → 42 boundary; also serves clean 42. |
| Student device release | f41f9c28d67aa06ec370ba4f1a8c7d1192d5986d | 43 / 43 | 42 / 43 | Serve after clean 43. |
| Schema-43 application rollback | 54115fd6029d5d80af63640ac6f0bfe31be22d67 | 43 / 43 | 42 / 43 | Restore prior application behavior on unchanged schema 43. |

At clean 42, 98e88 is too old; 3383 is the exact safe application, both before and after device cutover. The cutover only revokes historical session families and supersedes legacy OTPs using schema-42-compatible values; it does not write `AUTO_REPLACED`. At schema 43, 3383 is too old; f41f9c28 and 54115fd are safe. Dirty schema markers admit no application.

The local schema-42 artifact was built from the exact 3383 commit and exported with release.sh. Its manifest, image archive and tooling checksums verify; the bundle declares SCHEMA42_CAPABILITY=manual-enhancement-v1; labels and IDs agree; backend max-version is 42; 0042 hashes agree; gradex-enhancement-drain exists. It was not imported to production. The local 98e88 copy matches the first preflight’s selected SHA and image IDs, but the next preflight must verify the actual host manifest and checksums. Host staging of 3383, f41f9c28 and 54115fd remains a deployment prerequisite.

## 3. Reuse the reviewed schema-42 boundary unchanged

The approved forward entry remains the imported 3383 command:

    host.sh up-core-schema-42-enhancement-recovery

It validates production scope, exact bundle capability and SHA, OCI revision labels and image IDs, max-version exactly 42, 0042 UP/DOWN image hashes, the drain executable, and the complete 98e88 schema-41 recovery floor. It requires API and worker absent, inventories local worker producers targeting gradex_production, starts PostgreSQL/Redis and waits healthy, requires clean schema 41 and no work_claim_token, then runs the migrate Compose service with gradex-migrate up. Migration 0042 itself refuses SCANNING, PROCESSING, or any claimed row. The wrapper verifies clean schema 42 and starts API, worker and frontend privately. It does not start the edge; it also does not verify that an edge left running by the prior deployment is stopped.

The separate enhancement-drain command is for supervised 42 → 41 rollback, not forward 41 → 42. Rollback also requires the 3383 capability/floor, clean 42, healthy PostgreSQL/Redis, absent API/worker, producer inventory clear, zero active claims and a successful read-only queue/outbox drain. It then invokes only gradex-migrate rollback-schema-42 -confirm-production=schema-42-to-41, verifies clean 41 and stops. Generic production DOWN remains prohibited. The drain reports pending, active, scheduled, retry, archived and aggregating work; completed work is reported separately. It deletes no work.

## 4. Combined maintenance sequence: closed-edge composition

**Decision: option A.** The frozen 41 → 42 command can participate safely without changing frozen tooling if the edge is stopped before invocation and remains stopped. Its automatic API/worker/frontend start is private; the edge is not started by the command. The operator then quiesces that private tier before device cutover. No new production orchestration command is required; this release plan composes the approved primitive explicitly.

1. Complete the refreshed read-only preflight, obtain independent review and Product Owner approval, and verify the current recovery artifact, 3383, f41f9c28, 54115fd, backup and restore evidence.
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
4. Stop API and frontend again immediately. Let the private 3383 worker finish existing work. Use the read-only queue/outbox snapshots until default-queue pending, active, scheduled, retry, and aggregating counts are zero; no media claim or undispatched media event remains; and no transactional-email delivery is QUEUED or SENDING. Completed and archived counts remain recorded. Then stop/remove the idle worker and remove the stopped API/frontend. Recheck edge-down state, absence, claims and historical-session counts. If work or a claim remains, do not cut over; remain at clean 42 and let the 3383 recovery application settle it.
5. With every application writer absent, read the exact expected counts from the refreshed preflight and the post-42 recheck. Any non-Student target, count drift, device invariant violation, outstanding device email or nonzero historical residue stops before apply. Set the f41 manifest/image and frozen Compose paths:

~~~bash
DEVICE_SHA=f41f9c28d67aa06ec370ba4f1a8c7d1192d5986d
DEVICE_MANIFEST="$HOST_STATE/releases/$DEVICE_SHA/release.env"
DEVICE_BACKEND="$(awk -F= '$1 == "GRADEX_BACKEND_IMAGE" {print substr($0,index($0,"=")+1)}' "$DEVICE_MANIFEST")"
~~~

Using the f41 backend image override for this one-off only, run gradex-device-cutover -mode=check. Compare returned schema/counts to the exact clean-42 preflight. Only the later approved window may repeat the command with -mode=apply, -expected-schema=42, exact -expected-legacy/-expected-pending/-expected-otp counts, named -operator, change-ticket -request-id and -confirm=REVOKE_HISTORICAL_DEVICE_SESSIONS. The transaction revokes only Student historical families and supersedes outstanding device OTPs. Require zero residue afterward. The runtime selection remains 3383 until apply-release later.

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

6. Recheck clean 42, healthy Postgres/Redis, edge/API/worker absent, zero active media claims, and the f41 manifest, revision label, image ID, schema ceiling 43, and 0043 UP/DOWN hashes. The 0043 hashes must match between the verified f41 manifest/image and the selected source artifact. No app writer runs between the check and migration.

7. Run the f41 image's gradex-migrate up only after the exact clean-42 check and successful cutover. This does not replace the frozen 41 → 42 command: the approved 3383 command already crossed that boundary with all artifact/media/producer gates. At clean 42, 0043 is the only pending migration in the verified f41 image. This one-off uses --no-deps and starts no application service:

~~~bash
GRADEX_BACKEND_IMAGE="$DEVICE_BACKEND" docker compose \
  --file "$COMPOSE_FILE" --project-directory "$TOOLING_ROOT/deploy/hostinger" \
  --project-name gradex-production --env-file "$HOST_STATE/runtime.env" \
  run --rm --no-deps migrate gradex-migrate up
~~~

Verify schema 43 is clean and both AUTO_REPLACED labels exist before selecting an app.
8. Use the frozen host wrapper’s application-only apply-release with the staged f41 manifest. It starts API/worker/frontend with --no-deps, verifies health/readiness and then persists the f41 selection. Keep edge closed through core verification and private smoke checks; reopen it only after approved public smoke tests.

~~~bash
GRADEX_HOST_STATE_DIR="$HOST_STATE" \
GRADEX_HOST_ENV_FILE="$HOST_STATE/runtime.env" \
GRADEX_HOST_PROJECT=gradex-production APP_ENV=production \
"$TOOLING_ROOT/deploy/hostinger/host.sh" apply-release \
  "$HOST_STATE/releases/f41f9c28d67aa06ec370ba4f1a8c7d1192d5986d/release.env"
~~~

If f41 cannot become ready while the marker is clean 43, keep the edge closed and use the staged 54115fd manifest with the same `apply-release` command. Do not select 3383 or 98e88 against schema 43. If the schema marker is dirty, start no application and enter operator recovery.

If a 3383 worker claim appears after its private startup, do not interrupt or clear it to continue. Stay on clean 42, recover with 3383, and wait for the media operation to settle. If the private 3383 start fails but schema remains clean 42, 3383 remains the recovery application. If clean 42 cannot be confirmed, start no application.

## 5. Device cutover position and non-Student rows

Run cutover after clean 42 and before 0043. The CLI accepts expected schemas only through 42. This order makes clean 42 with the 3383 application a valid recovery point if cutover completes but 0043 does not commit. API/worker are absent, so counts cannot drift from ordinary application traffic.

Current ApplyDeviceCutover counts legacy/pending sessions across all roles, locks Student targets only, and fails closed when those sets differ. Preserve that behavior. It prevents a Student release operation from silently revoking Instructor/Admin sessions.

Product Owner options for any non-Student historical row:

- **A — Student-only cutover:** count and clean only Student historical session families; exclude non-Student families from the expected counts and leave them untouched. This requires a separately reviewed code change because current `ApplyDeviceCutover` counts all roles and fails closed.
- **B — Retain fail-closed behavior:** stop release on any non-Student historical family. Require an explicit Product Owner-approved disposition and separately authorized, audited remediation before retrying. This is the current code behavior and recommended posture until the Product Owner decides otherwise.

No option silently revokes a non-Student family. No code change is made merely to get past the gate.

## 6. Recovery matrix

| Database state | Safe application | Recovery |
| --- | --- | --- |
| Clean 41 | 98e88fcc | Keep/select the verified current release. |
| Dirty 41 | None | Operator recovery only. |
| Clean 42 before 0043, before or after device cutover | 3383f46 | Keep schema 42 and restore/select the exact 3383 artifact. The completed cutover remains durable and safe for 3383. To return to 41, use only the supervised 42 → 41 command and all drain gates, then select 98e88. |
| Dirty 42 | None | Operator recovery only; never force a marker. |
| Clean 43 before traffic | f41f9c28 or 54115fd | Either schema-43 application can serve. |
| Clean 43 with AUTO_REPLACED evidence | f41f9c28 or 54115fd | Keep schema 43; 54115fd restores the previous application behavior. |
| Dirty 43 | None | Operator recovery only. |

0043 DOWN refuses while AUTO_REPLACED device/session evidence exists. Schema 43 application rollback is not a schema downgrade.

## 7. Resumable read-only production preflight

The next preflight must recheck local source/worktree, production hostname/time, selected release manifest, running API/worker/frontend image labels/IDs/status, health/readiness and schema. Clean schema 41 is the expected start and continues preflight. Dirty markers or a different path stop and require rederivation.

Collect active historical sessions grouped by role and trust state; details must include account role/id, session id, created/authenticated/activity timestamps and idle/absolute expiry only. Never select session credentials or bearer values. Any non-Student historical family is a disposition decision. Collect OTP totals, unexpired totals, distinct accounts, oldest/newest issue time and latest expiry without selecting secret_digest. Count device-trust outbox/delivery work by missing ledger, queued, sending, accepted and terminal failure state, with recent failures separately. Stop maintenance if any device-trust email remains queued/sending.

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

The 98e88 worker image lacks gradex-enhancement-drain. The read-only preflight must not start a one-off container or import an image. Gradex configures the Asynq worker with only the `default` queue, and the media dispatcher enqueues `media:scan`, `media:transcode`, and `media:enhancement` task types there; aggregate queue lengths cannot isolate the enhancement type. The new inspector reads each default-queue task's serialized message through the already-running Redis container, decodes only the protobuf type in a local pipe, and prints per-state queue totals and media:enhancement counts. It reads state keys directly because the queue registry may omit a queue while its completed-task retention records remain. It never prints task IDs, payloads, or secrets and uses no Redis write command.

After rechecking the host identity and SSH target, stream the reviewed script to the existing host shell. This writes no host file and creates no container:

~~~bash
PRODUCTION_SSH_TARGET=deploy@186.241.16.111
ssh "$PRODUCTION_SSH_TARGET" 'bash -s' < deploy/device43/inspect-enhancement-queue-readonly.sh
~~~

The script discovers the Redis container by Compose labels; REDISCLI_AUTH and the CA are already available inside it. It uses only Docker ps/exec and Redis LRANGE, ZRANGE, SMEMBERS, and HGET reads. A failed lookup or malformed message exits non-zero; never treat a partial result as zero. It reports pending, active, scheduled, retry, archived, aggregating, and completed queue totals with the media:enhancement subset; completed is separate. Also count undispatched media.enhancement_requested outbox intents in PostgreSQL. Do not pause, drain, reschedule, or delete work.

Verify the actual selected host release.env, its SHA-256, release SHA, backend/frontend/proof image names and IDs, matching labels, image archive checksum, deploy bundle checksum and manifest bundle hash, capability, 0042 hashes and backend max-version. Verify these values match protected runtime.env. Do not edit, import or select a manifest during preflight.

## 8. Backup, restore and RPO gate

The maintenance window requires a verified offsite backup for production Postgres captured at clean schema 41 and a successful isolated restore check. The refreshed read-only preflight reads `backups/latest.completed-at`, `backups/latest.offsite.snapshot`, matching remote restic metadata, and the encrypted snapshot's schema-state sidecar; the sidecar must say `41|false`. The backup source is the PostgreSQL container selected by the production project and `pg_dump --dbname "$POSTGRES_DB"`, which must resolve to `gradex_production` in the protected runtime environment.

The backup tooling does not persist a dedicated successful-restore timestamp. `restored-source` identifies a restore attempt's snapshot, and a running `gradex-restore-verify` container alone is not proof of a successful verification. Require a timestamped operator-retained transcript or journal record of `host.sh verify-restore` tied to the same snapshot ID, plus the resulting schema/record-count verification. If that evidence is absent, an isolated restore and `verify-restore` is a separate approved pre-window recovery-evidence prerequisite; record its completion time and result before opening maintenance. The read-only preflight does not create a backup, restore a snapshot, start a container, or mutate backup state.

The documented timer is hourly. Repository docs describe an approximately one-hour backup-based RPO only if scheduled snapshots complete; WAL/PITR is not implemented. LG-019 still requires explicit Founder approval of PostgreSQL RPO/RTO; the provisional 15-minute RPO is unsupported. The preflight records the approved RPO and snapshot age. Missing approval, stale snapshot, missing database coverage, or absent/failed restore evidence blocks maintenance.

## 9. Canonical catalogue-import E2E proposal

[D-111](../DECISIONS.md#d-111--proposed-classification-for-canonical-catalogue-import-e2e-409-not-accepted) is a proposal, not an accepted waiver. The independently reported canonical catalogue-import E2E HTTP 409 / importer.ErrIdentityRebind reproduces on f41f9c28 and frozen 3383f46, so it predates the device/schema43 work.

Proposed classification: PRE_EXISTING_DETERMINISTIC_FIXTURE_OR_TEST_ENVIRONMENT_DEFECT.

Scope: track the fixture issue separately. This proposal does not authorize changing the identity-rebind guard, waiving a future regression, or accepting the classification without explicit Product Owner approval.

## 10. Offline proof and frozen boundary

The new local verifier validates the exact 3383 artifact and its 0042 capability/hash bindings, compiles the 3383 API readiness probe, and drives a disposable database through clean 41 → 42 → device cutover → 43. It does not contact production.

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

## 11. Approval boundary

This is offline preparation only. Production preflight must be repeated later against the current host. Migration, device cutover apply, release selection/import, service stop/start, edge opening and smoke traffic require the later independently reviewed and explicitly approved maintenance window. The sequence includes user-visible downtime.
