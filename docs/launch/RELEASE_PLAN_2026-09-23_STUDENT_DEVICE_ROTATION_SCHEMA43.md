# Student automatic device rotation — schema 43 release plan

**State:** local candidate; independent G0 re-review and Product Owner release approval are pending. This is an operating plan, not authorization to run it. No production inspection or mutation was performed while preparing it.

## Frozen boundary and artifacts

`3383f46d0e9e6379c3bd166d39622c3659ae3d86` is the independently approved schema-42 / 3C-B software baseline. Its media semantics, migrations 0039–0042, and supervised 41 ↔ 42 tooling are frozen. C1 remains `UNRESOLVED_INTERMITTENT_NONREPRODUCIBLE` and is outside this release.

The new boundary is **42 → 43**. Schema 43 adds `AUTO_REPLACED` to the trusted-device and session-revocation enums. The new API requires a clean 43. The prior API has a ceiling of 42, so a controlled maintenance window is mandatory.

The rollback application is the separate Git commit `54115fd6029d5d80af63640ac6f0bfe31be22d67` on `rollback/schema43-device-compat`, derived from `3383f46`. Its exact patch is [`deploy/device43/rollback-compat.patch`](../../deploy/device43/rollback-compat.patch), SHA-256 `9bd3e47288efbd8c78081dd2694ce94dea4fa3ba0c2525c460be6966266996f7`. It retains the prior emailed-device-challenge application behavior while accepting clean schema 43 and displaying `AUTO_REPLACED` history. It does not change media behavior. The rollback build's API floor and ceiling are both 43; its media worker floor remains 42. Schema stays 43 during application rollback.

`deploy/device43/verify-rollback-compat.sh` reconstructs the rollback source from the frozen base plus the patch and runs the cross-build integration proof. The test makes a real automatic rotation with the current code, then runs login, session resolution, Student/Admin device listing, manual removal, and protected-learning authorization with the older compatible code against the same disposable schema-43 database containing `AUTO_REPLACED` rows.

## Artifact preparation before any production window

From clean Git worktrees, after the exact candidate receives independent approval:

```bash
: "${REVIEWED_RELEASE_SHA:?set the exact SHA from the independent verdict}"
test "$(git rev-parse HEAD)" = "$REVIEWED_RELEASE_SHA"
bash deploy/device43/verify-rollback-compat.sh
bash deploy/device43/build-artifact.sh /var/tmp/gradex-device43-rollback rollback /var/tmp/device43-artifacts
bash deploy/device43/build-artifact.sh "$PWD" current /var/tmp/device43-artifacts
```

The builder checks that neither source changes the frozen media paths or migrations 0039–0042, builds three revision-labelled images per commit, proves backend ceiling 43, binds both 0043 migration hashes to the backend image, and writes checksum files. Transfer both complete artifact directories by the existing controlled artifact-transfer channel. On the host, the reviewed `import-artifact.sh` from each transferred directory verifies checksums, image IDs, OCI revision labels, ceiling 43, and 0043 hashes, then stages both under `/home/deploy/gradex-production/releases/<SHA>/`. Do not stage an unreviewed or locally rebuilt binary. The rollback artifact must be staged and validated **before** the schema moves.

The existing release-local `host.sh apply-release MANIFEST` command performs an application-only selection and refuses a backend whose schema ceiling is below the live schema. It is not modified for this release. The new device43 artifact builder/importer is separate from the frozen schema-42 bundle mechanism.

## Read-only production discovery and preflight

At release time, read the running API image's `org.opencontainers.image.revision` label, `schema_migrations.version, dirty`, `/healthz`, `/readyz`, Postgres health, media-worker state, and the current release manifest. Record exact values and timestamps. **Stop and re-derive this plan if the actual production SHA/schema differs from the prepared path.** The 2026-09-18 repository observation of schema 38 is historical, not a release-time assertion.

Before stopping writers, and again after they stop, run these read-only SQL queries against the discovered production database. Keep results in the release evidence packet; do not print credential digests or bearer values.

```sql
SELECT version, dirty FROM schema_migrations;
SELECT count(*) AS active_legacy_unbound
FROM sessions
WHERE state = 'ACTIVE' AND device_trust_state = 'LEGACY_UNBOUND';
SELECT count(*) AS active_pending_device_trust
FROM sessions
WHERE state = 'ACTIVE' AND device_trust_state = 'PENDING_DEVICE_TRUST';
SELECT s.id, s.account_id, s.created_at, s.authenticated_at,
       s.last_activity_at, s.idle_expires_at, s.absolute_expires_at,
       s.device_trust_state::text, a.role::text AS account_role,
       a.status::text AS account_status
FROM sessions s JOIN accounts a ON a.id = s.account_id
WHERE s.state = 'ACTIVE'
  AND s.device_trust_state IN ('LEGACY_UNBOUND', 'PENDING_DEVICE_TRUST')
ORDER BY s.account_id, s.id;
SELECT count(*) AS outstanding_device_otps,
       count(*) FILTER (WHERE expires_at > clock_timestamp()) AS unexpired_device_otps
FROM identity_action_secrets
WHERE purpose = 'DEVICE_TRUST_OTP'
  AND consumed_at IS NULL AND superseded_at IS NULL;
SELECT id, account_id, trusted_device_id, issued_at, expires_at, attempt_count
FROM identity_action_secrets
WHERE purpose = 'DEVICE_TRUST_OTP'
  AND consumed_at IS NULL AND superseded_at IS NULL
ORDER BY account_id, issued_at;
SELECT count(*) AS queued_or_sending_device_emails
FROM outbox_events e
LEFT JOIN transactional_email_deliveries d ON d.event_id = e.id
WHERE e.event_type = 'identity.device_trust_code_requested'
  AND (d.event_id IS NULL OR d.status IN ('QUEUED', 'SENDING'));
```

The detailed session query must contain only Student target families. The cutover command refuses any non-Student historical state and leaves it for a separate operator decision. The queued/sending device-email count must reach zero before OTP invalidation and email-worker restart. Drain through the existing delivery system or stop for an operator decision; do not delete append-only outbox events to force the gate.

## Media and schema 38 → 43 prerequisites

Quiesce the current media producers and let in-flight work settle. Record worker state and queue status without changing the approved media policy. The exact precondition enforced by 0042 is:

```sql
SELECT count(*) AS incompatible_active_media
FROM media_asset_versions
WHERE state IN ('SCANNING', 'PROCESSING')
   OR work_claim_token IS NOT NULL;
```

It **must be zero** immediately before migration 0042. A PLAYABLE row with any claim is included by the token predicate. Do not clear claims or discard queued work merely to satisfy the count. When starting from clean schema 42, run the existing read-only enhancement-drain proof and require its established gates. Keep its pending, active, scheduled, retry, archived, aggregating, and undispatched-outbox observations; completed work is historical. Do not alter its behavior.

| Migration | Forward effect | Gate |
| --- | --- | --- |
| 0039 | Adds inert `PLAYABLE` enum label. | Clean predecessor, quiesced producers; no application transition before 0040. |
| 0040 | Replaces media work-claim constraint, expiry index, and immutability trigger. | Quiesced processing and no concurrent media writes during DDL. |
| 0041 | Adds processing-attempt kind and rendition provenance representation. | Clean 40, quiesced writers; existing attempts remain `FULL`. |
| 0042 | Adds active attempt kind and claim-coherence constraint. | **Zero** rows from the SQL above; the migration itself refuses otherwise. |
| 0043 | Adds `AUTO_REPLACED` to two enums with `IF NOT EXISTS`. | Clean 42; historical-session and OTP cutover complete; rollback artifact staged. |

The generic migrator's dirty marker remains authoritative: `IF NOT EXISTS` does not authorize forcing or automatically retrying a dirty migration. If the discovered starting schema is not the prepared clean 38 or clean 42 path, stop and derive the precise sequence before proceeding.

## Controlled session and OTP cutover

After stopping the old API, worker, and frontend under maintenance, rerun the counts above. The new candidate image contains `gradex-device-cutover`, a command independent of API readiness. Run its read-only `-mode=check` first. Supply its exact fresh counts and actual clean pre-43 schema to `-mode=apply`, along with a named operator, request ID, and `-confirm=REVOKE_HISTORICAL_DEVICE_SESSIONS`.

The apply transaction uses the canonical session-family revoker for **only** active `LEGACY_UNBOUND` and `PENDING_DEVICE_TRUST` families, sets `ADMIN_REVOKED`, self-supersedes all outstanding `DEVICE_TRUST_OTP` secrets, and writes append-only privileged Audit events for each target and the cutover summary. It does not advance any Account session epoch or revoke trusted families. A count mismatch rolls back everything. This command is not run during implementation.

After apply, rerun the read-only counts and require **zero** active legacy, zero active pending, and zero outstanding device OTP rows. Historical pending browsers must sign in again with their password; no old code-entry route remains. A stranded or unexpected legacy state stays restricted and cannot silently adopt a device. An existing trusted browser remains signed in.

## Maintenance ordering and availability

The known old API cannot become ready at schema 43, while the new API requires 43. Public unavailability begins when old application services are stopped and ends only after the new API, worker, and frontend pass health/readiness and smoke checks. No zero-downtime claim is made.

1. Confirm independent verdict, operator approval, exact live SHA/schema, clean marker, DB health, both staged artifacts, and the verified backup/recovery point. Use the existing supervised `host.sh backup` and `host.sh verify-restore` procedures and record snapshot ID, time, and restore evidence. A restore after traffic would lose later writes and is disaster recovery, not normal rollback.
2. Quiesce media producers; settle claims and run the media checks above. Stop old API, worker, and frontend. Keep PostgreSQL and Redis healthy. Recheck media, session, OTP, email-outbox, and schema counts.
3. Run the candidate image's `gradex-device-cutover -mode=check`, then the acknowledged `-mode=apply` with exact expected counts. Require the post-cutover zero counts.
4. Select the candidate backend image only for the one-off Compose `migrate` service and run `gradex-migrate up`. This applies the prepared pending chain in order through 43. Verify a clean 43 marker and the 0043 enum labels. A dirty marker or 0042 gate refusal stops the cutover; never force it.
5. Use the frozen host wrapper's `apply-release` with the staged **current** schema-43 manifest. It recreates API, worker, and frontend with `--no-deps`; migration is performed only in the preceding explicit step. Require API health/readiness, worker running, frontend healthy, unchanged entitlement provenance, Student login/device rotation, old-device denial, protected-learning authorization, and one-playback enforcement before reopening traffic.

For a clean schema-43 application regression after traffic, keep schema **43**. Quiesce producers, then run the same unmodified `host.sh apply-release` against the staged **rollback** manifest. The prior Student login/device-email behavior returns; `AUTO_REPLACED` device and session history remains readable, and the media worker still follows the frozen 3C-B code. Verify readiness and the rollback probe journeys. Do **not** run 0043 down as normal incident response. Its down migration refuses while automatic-replacement evidence exists. Full database restore is a separately authorized disaster-recovery decision.

If forward migration ends dirty or at an unprepared intermediate schema, no application is selected automatically. Inspect the marker and the failing migration evidence, preserve the backup and both artifacts, and resolve under a new operator decision. The schema-41 ↔ schema-42 supervised rollback protocol remains in its frozen release plan and is not modified here.

## Exact host command shape — for the later approved window only

Set `HOST_STATE=/home/deploy/gradex-production` and `FROZEN_TOOLING_SHA` to the separately verified installed schema-42 tooling revision. Use its release-local `deploy/hostinger/host.sh` for backup, restore verification, and `apply-release`. Set `CURRENT_SHA` to the reviewed device-release commit and `ROLLBACK_SHA=54115fd6029d5d80af63640ac6f0bfe31be22d67`. The imported manifests are `$HOST_STATE/releases/$CURRENT_SHA/release.env` and `$HOST_STATE/releases/$ROLLBACK_SHA/release.env`.

For one-off Compose commands, set `TOOLING_ROOT` to the verified installed checkout of the frozen schema-42 tooling and `COMPOSE_FILE=$TOOLING_ROOT/deploy/hostinger/compose.yml`. Use `--project-name gradex-production` and `--env-file $HOST_STATE/runtime.env`. Export `GRADEX_BACKEND_IMAGE`, `GRADEX_FRONTEND_IMAGE`, and `GRADEX_PROOF_IMAGE` from the **reviewed current manifest**; verify each OCI label first. The read-only check is:

```bash
: "${TOOLING_ROOT:?set the verified frozen tooling checkout}"
: "${HOST_STATE:?set the production state directory}"
COMPOSE_FILE="$TOOLING_ROOT/deploy/hostinger/compose.yml"
docker compose --project-name gradex-production --file "$COMPOSE_FILE" \
  --project-directory "$(dirname "$COMPOSE_FILE")" \
  --env-file "$HOST_STATE/runtime.env" run --rm --no-deps \
  --entrypoint gradex-device-cutover migrate -mode=check
```

After stopping application writers, the gated mutation is the same command with `-mode=apply -expected-schema=$START_SCHEMA -expected-legacy=$LEGACY_COUNT -expected-pending=$PENDING_COUNT -expected-otp=$OTP_COUNT -operator=$OPERATOR -request-id=$TICKET -confirm=REVOKE_HISTORICAL_DEVICE_SESSIONS`. The migration command is the same Compose invocation of `migrate gradex-migrate up` without an entrypoint override. After verifying clean schema 43, the exact application selection commands are:

```bash
export GRADEX_HOST_STATE_DIR="$HOST_STATE" GRADEX_HOST_PROJECT=gradex-production
"$TOOLING_ROOT/deploy/hostinger/host.sh" apply-release \
  "$HOST_STATE/releases/$CURRENT_SHA/release.env"
```

Only for an approved application rollback, while schema remains 43:

```bash
export GRADEX_HOST_STATE_DIR="$HOST_STATE" GRADEX_HOST_PROJECT=gradex-production
"$TOOLING_ROOT/deploy/hostinger/host.sh" apply-release \
  "$HOST_STATE/releases/$ROLLBACK_SHA/release.env"
```

These mutation commands require the approved release window and every gate above; the rollback command is reserved for an incident decision.

## Frozen-range proof required at review

```bash
git diff --stat 3383f46..HEAD
git diff 3383f46..HEAD -- backend/internal/media backend/internal/playback
git diff 3383f46..HEAD -- backend/internal/db/migrations/0039\* backend/internal/db/migrations/0040\* backend/internal/db/migrations/0041\* backend/internal/db/migrations/0042\*
```

The latter two commands must show no unintended frozen media change. Any newly discovered need to change that range stops this device release for a separate decision.
