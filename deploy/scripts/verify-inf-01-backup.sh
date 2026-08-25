#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
MINIO_IMAGE="minio/minio@sha256:14cea493d9a34af32f524e538b8346cf79f3321eff8e708c1e2960462bd8936e"
MC_IMAGE="minio/mc@sha256:a7fe349ef4bd8521fb8497f55c6042871b2ae640607cf99d9bede5e9bdf11727"
POSTGRES_IMAGE="postgres@sha256:57c72fd2a128e416c7fcc499958864df5301e940bca0a56f58fddf30ffc07777"
MINIO_PORT=19000
MINIO_ACCESS_KEY=gradex-inf01-access
MINIO_SECRET_KEY=gradex-inf01-secret-sentinel
MINIO_BUCKET=gradex-inf01-backups

WORK_DIR=""
MINIO_NAME="gradex-inf01-minio-$$"
SOURCE_NAME="gradex-inf01-source-$$"
RESTORE_NAME="gradex-inf01-restore-$$"

note() { printf 'inf-01-proof: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

cleanup() {
  local exit_status=$?
  docker rm --force "$RESTORE_NAME" "$SOURCE_NAME" "$MINIO_NAME" >/dev/null 2>&1 || true
  if [ -n "$WORK_DIR" ] && [ -d "$WORK_DIR" ]; then
    rm -rf -- "$WORK_DIR"
  fi
  exit "$exit_status"
}
trap cleanup EXIT

require_tools() {
  local tool
  for tool in curl docker jq mktemp sha256sum stat timeout; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done
}

wait_for_http() {
  local attempts=0
  while [ "$attempts" -lt 60 ]; do
    curl --fail --silent "http://127.0.0.1:$MINIO_PORT/minio/health/live" >/dev/null 2>&1 && return
    attempts=$((attempts + 1))
    sleep 1
  done
  die "disposable MinIO did not become ready"
}

wait_for_postgres() {
  local container="$1" attempts=0
  while [ "$attempts" -lt 60 ]; do
    if docker exec "$container" pg_isready -U gradex -d gradex >/dev/null 2>&1 &&
      docker exec "$container" psql --username gradex --dbname gradex --command 'SELECT 1' >/dev/null 2>&1; then
      return
    fi
    attempts=$((attempts + 1))
    sleep 1
  done
  die "disposable PostgreSQL did not become ready: $container"
}

configure_minio_bucket() {
  local mc_host="http://$MINIO_ACCESS_KEY:$MINIO_SECRET_KEY@127.0.0.1:$MINIO_PORT"
  docker run --rm --network host --env "MC_HOST_inf01=$mc_host" --entrypoint mc "$MC_IMAGE" \
    mb --ignore-existing inf01/$MINIO_BUCKET >/dev/null
}

start_minio() {
  docker run --rm --detach --name "$MINIO_NAME" --publish "127.0.0.1:$MINIO_PORT:9000" \
    --env MINIO_ROOT_USER="$MINIO_ACCESS_KEY" --env MINIO_ROOT_PASSWORD="$MINIO_SECRET_KEY" \
    "$MINIO_IMAGE" server /data >/dev/null
  wait_for_http
  configure_minio_bucket
}

start_source_database() {
  docker run --rm --detach --name "$SOURCE_NAME" \
    --env POSTGRES_USER=gradex --env POSTGRES_PASSWORD=inf01-source-password \
    --env POSTGRES_DB=gradex "$POSTGRES_IMAGE" >/dev/null
  wait_for_postgres "$SOURCE_NAME"
  docker exec "$SOURCE_NAME" psql --username gradex --dbname gradex --command "
    CREATE TABLE schema_migrations (version integer NOT NULL, dirty boolean NOT NULL);
    INSERT INTO schema_migrations VALUES (15, false);
    CREATE TABLE accounts (id uuid PRIMARY KEY, email text NOT NULL);
    INSERT INTO accounts VALUES
      ('00000000-0000-4000-8000-000000000001', 'admin@example.test'),
      ('00000000-0000-4000-8000-000000000002', 'instructor@example.test'),
      ('00000000-0000-4000-8000-000000000003', 'student@example.test');
    CREATE TABLE courses (id uuid PRIMARY KEY, title text NOT NULL);
    INSERT INTO courses VALUES ('00000000-0000-4000-8000-000000000010', 'Synthetic course');
    CREATE TABLE course_access_invitations (id uuid PRIMARY KEY, state text NOT NULL);
    INSERT INTO course_access_invitations VALUES ('00000000-0000-4000-8000-000000000020', 'APPROVED');
    CREATE TABLE entitlements (id uuid PRIMARY KEY, state text NOT NULL, source_invitation_id uuid NOT NULL, note text NOT NULL);
    INSERT INTO entitlements VALUES ('00000000-0000-4000-8000-000000000030', 'ACTIVE', '00000000-0000-4000-8000-000000000020', 'synthetic-pii-sentinel');
    CREATE TABLE enrollments (id uuid PRIMARY KEY, student_account_id uuid NOT NULL, course_id uuid NOT NULL);
    INSERT INTO enrollments VALUES ('00000000-0000-4000-8000-000000000040', '00000000-0000-4000-8000-000000000003', '00000000-0000-4000-8000-000000000010');" >/dev/null
}

write_dump_stage() {
  local stage_dir="$1" stamp="$2" dump_file schema_file schema_before schema_after
  mkdir -p -- "$stage_dir"
  chmod 700 "$stage_dir"
  dump_file="$stage_dir/gradex-$stamp.dump"
  schema_file="$dump_file.schema-state"
  schema_before="$(docker exec "$SOURCE_NAME" psql --no-psqlrc --username gradex --dbname gradex \
    --tuples-only --no-align --command "SELECT version::text || '|' || dirty::text FROM schema_migrations;")"
  [ "$schema_before" = '15|false' ] || die "synthetic source schema is unexpected: $schema_before"
  docker exec "$SOURCE_NAME" pg_dump --format=custom --no-owner --no-acl \
    --username gradex --dbname gradex >"$stage_dir/$stamp.partial"
  [ -s "$stage_dir/$stamp.partial" ] || die "synthetic custom dump is empty"
  schema_after="$(docker exec "$SOURCE_NAME" psql --no-psqlrc --username gradex --dbname gradex \
    --tuples-only --no-align --command "SELECT version::text || '|' || dirty::text FROM schema_migrations;")"
  [ "$schema_after" = "$schema_before" ] || die "synthetic source schema changed during dump"
  mv -- "$stage_dir/$stamp.partial" "$dump_file"
  printf '%s\n' "$schema_before" >"$schema_file"
  (cd "$stage_dir" && sha256sum "$(basename "$dump_file")" >"$(basename "$dump_file").sha256")
  (cd "$stage_dir" && sha256sum "$(basename "$schema_file")" >"$(basename "$schema_file").sha256")
  chmod 600 "$stage_dir"/*
}

create_snapshot() {
  local stage_dir="$1" log_file="$2" snapshot_id check_log
  snapshot_id="$(backup_snapshot_directory "$stage_dir" "$log_file")"
  check_log="$WORK_DIR/snapshot-$snapshot_id.json"
  backup_snapshot_exists "$snapshot_id" "$check_log" || die "snapshot is not visible in repository"
  backup_check_repository >/dev/null || die "structural repository check failed"
  backup_prune_repository >/dev/null || die "retention/prune failed"
  backup_assert_repository_has_snapshot "$check_log" || die "retention removed every snapshot"
  backup_snapshot_exists "$snapshot_id" "$check_log" || die "retention removed the newly-created snapshot"
  printf '%s\n' "$snapshot_id"
}

restore_snapshot_to_disposable_database() {
  local snapshot_id="$1"
  local listing_log="$WORK_DIR/restore-$snapshot_id.jsonl"
  local dump_path schema_path dump_checksum_path schema_checksum_path dump_name schema_name restored_state
  backup_restic ls --json "$snapshot_id" >"$listing_log"
  dump_path="$(backup_snapshot_file_path "$listing_log" .dump)"
  schema_path="$(backup_snapshot_file_path "$listing_log" .schema-state)"
  dump_checksum_path="$(backup_snapshot_file_path "$listing_log" .dump.sha256)"
  schema_checksum_path="$(backup_snapshot_file_path "$listing_log" .schema-state.sha256)"
  dump_name="${dump_path##*/}"
  schema_name="${schema_path##*/}"
  backup_extract_snapshot_file "$snapshot_id" "$dump_path" "$WORK_DIR/$dump_name"
  backup_extract_snapshot_file "$snapshot_id" "$schema_path" "$WORK_DIR/$schema_name"
  backup_extract_snapshot_file "$snapshot_id" "$dump_checksum_path" "$WORK_DIR/$dump_name.sha256"
  backup_extract_snapshot_file "$snapshot_id" "$schema_checksum_path" "$WORK_DIR/$schema_name.sha256"
  (cd "$WORK_DIR" && sha256sum --check "$dump_name.sha256" "$schema_name.sha256") >/dev/null
  docker rm --force "$RESTORE_NAME" >/dev/null 2>&1 || true
  docker run --rm --detach --name "$RESTORE_NAME" \
    --env POSTGRES_USER=gradex --env POSTGRES_PASSWORD=inf01-restore-password \
    --env POSTGRES_DB=gradex "$POSTGRES_IMAGE" >/dev/null
  wait_for_postgres "$RESTORE_NAME"
  docker exec --interactive "$RESTORE_NAME" pg_restore --exit-on-error --single-transaction \
    --no-owner --no-acl --username gradex --dbname gradex <"$WORK_DIR/$dump_name"
  restored_state="$(docker exec "$RESTORE_NAME" psql --no-psqlrc --username gradex --dbname gradex \
    --tuples-only --no-align --command "
      SELECT
        (SELECT version::text || '|' || dirty::text FROM schema_migrations) || '|' ||
        (SELECT count(*) FROM accounts) || '|' ||
        (SELECT count(*) FROM courses) || '|' ||
        (SELECT count(*) FROM course_access_invitations WHERE state = 'APPROVED') || '|' ||
        (SELECT count(*) FROM entitlements WHERE state = 'ACTIVE' AND source_invitation_id IS NOT NULL) || '|' ||
        (SELECT count(*) FROM enrollments);")"
  [ "$restored_state" = '15|false|3|1|1|1|1' ] || die "remote restore invariants failed: $restored_state"
  note "snapshot $snapshot_id restored from the encrypted repository into a disposable PostgreSQL container"
}

assert_no_plaintext_remote_objects() {
  local mc_host="http://$MINIO_ACCESS_KEY:$MINIO_SECRET_KEY@127.0.0.1:$MINIO_PORT"
  mkdir -p -- "$WORK_DIR/remote-objects"
  docker run --rm --network host --env "MC_HOST_inf01=$mc_host" \
    --volume "$WORK_DIR/remote-objects:/out" --entrypoint mc "$MC_IMAGE" \
    mirror --quiet inf01/$MINIO_BUCKET/gradex-production-backups /out
  if grep --recursive --binary-files=without-match --fixed-strings 'synthetic-pii-sentinel' "$WORK_DIR/remote-objects"; then
    die "plaintext synthetic content appeared in a remote object"
  fi
}

main() {
  require_tools
  WORK_DIR="$(mktemp -d /tmp/gradex-inf01-proof.XXXXXX)"
  chmod 700 "$WORK_DIR"
  printf '%s\n' inf01-correct-restic-password >"$WORK_DIR/restic-password"
  chmod 600 "$WORK_DIR/restic-password"
  printf '%s\n' inf01-wrong-restic-password >"$WORK_DIR/wrong-password"
  chmod 600 "$WORK_DIR/wrong-password"
  export GRADEX_BACKUP_TEST_MODE=true
  export GRADEX_BACKUP_S3_ENDPOINT="http://127.0.0.1:$MINIO_PORT"
  export GRADEX_BACKUP_S3_BUCKET="$MINIO_BUCKET"
  export GRADEX_BACKUP_S3_PREFIX=gradex-production-backups
  export GRADEX_BACKUP_S3_REGION=us-east-1
  export GRADEX_BACKUP_S3_BUCKET_LOOKUP=path
  export GRADEX_BACKUP_S3_ACCESS_KEY="$MINIO_ACCESS_KEY"
  export GRADEX_BACKUP_S3_SECRET_KEY="$MINIO_SECRET_KEY"
  export GRADEX_BACKUP_PASSWORD_FILE="$WORK_DIR/restic-password"
  export GRADEX_BACKUP_RESTIC_BINARY="${GRADEX_BACKUP_RESTIC_BINARY:-/usr/local/bin/restic}"
  export GRADEX_BACKUP_RETENTION_LAST=2 GRADEX_BACKUP_RETENTION_HOURLY=48
  export GRADEX_BACKUP_RETENTION_DAILY=14 GRADEX_BACKUP_RETENTION_WEEKLY=8
  export GRADEX_BACKUP_TIMEOUT_SECONDS=15
  source "$ROOT/deploy/hostinger/backup-restic.sh"
  backup_validate_configuration || die "local proof configuration rejected"
  export GRADEX_BACKUP_TEST_MODE=false
  if backup_validate_configuration >"$WORK_DIR/insecure-endpoint.log" 2>&1; then
    die "production validation accepted an insecure backup endpoint"
  fi
  export GRADEX_BACKUP_TEST_MODE=true
  start_minio
  backup_initialize_repository >/dev/null
  start_source_database

  local first_stage second_stage retry_stage first_snapshot second_snapshot retry_snapshot marker marker_before snapshot_count
  first_stage="$WORK_DIR/first-stage"
  second_stage="$WORK_DIR/second-stage"
  write_dump_stage "$first_stage" 20260824T000001Z
  first_snapshot="$(create_snapshot "$first_stage" "$WORK_DIR/first-backup.json")"
  rm -rf -- "$first_stage"
  [ ! -e "$first_stage" ] || die "successful backup retained plaintext first staging"
  marker="$WORK_DIR/latest.completed-at"
  backup_write_state_file "$marker" "$first_snapshot"
  marker_before="$(cat "$marker")"
  restore_snapshot_to_disposable_database "$first_snapshot"
  assert_no_plaintext_remote_objects

  write_dump_stage "$second_stage" 20260824T000002Z
  second_snapshot="$(create_snapshot "$second_stage" "$WORK_DIR/second-backup.json")"
  rm -rf -- "$second_stage"
  [ "$second_snapshot" != "$first_snapshot" ] || die "repeated backup did not create a new snapshot"
  snapshot_count="$(backup_snapshot_count "$WORK_DIR/retention.json")"
  [ "$snapshot_count" -ge 2 ] || die "retention did not keep two generations: $snapshot_count"
  restore_snapshot_to_disposable_database "$second_snapshot"
  backup_deep_check_repository >/dev/null

  export GRADEX_BACKUP_PASSWORD_FILE="$WORK_DIR/wrong-password"
  if backup_restic snapshots --json >"$WORK_DIR/wrong-key.log" 2>&1; then
    die "wrong restic password opened the repository"
  fi
  if grep --quiet --fixed-strings 'inf01-correct-restic-password' "$WORK_DIR/wrong-key.log"; then
    die "correct encryption password appeared in wrong-key output"
  fi
  export GRADEX_BACKUP_PASSWORD_FILE="$WORK_DIR/restic-password"
  export GRADEX_BACKUP_S3_ACCESS_KEY=""
  if backup_validate_configuration >"$WORK_DIR/missing-credential.log" 2>&1; then
    die "missing remote credential was accepted"
  fi
  export GRADEX_BACKUP_S3_ACCESS_KEY="$MINIO_ACCESS_KEY"
  export GRADEX_BACKUP_PASSWORD_FILE="$WORK_DIR/missing-password"
  if backup_validate_configuration >"$WORK_DIR/missing-key.log" 2>&1; then
    die "missing encryption key was accepted"
  fi
  export GRADEX_BACKUP_PASSWORD_FILE="$WORK_DIR/restic-password"

  marker_before="$(cat "$marker")"
  export GRADEX_BACKUP_S3_ENDPOINT=http://127.0.0.1:19001
  if backup_restic snapshots --json >"$WORK_DIR/remote-unavailable.log" 2>&1; then
    die "unavailable remote endpoint returned success"
  fi
  export GRADEX_BACKUP_S3_ENDPOINT="http://127.0.0.1:$MINIO_PORT"
  [ "$(cat "$marker")" = "$marker_before" ] || die "remote failure changed the success marker"
  snapshot_count="$(backup_snapshot_count "$WORK_DIR/after-failure.json")"
  [ "$snapshot_count" -ge 2 ] || die "remote failure damaged prior snapshots"
  retry_stage="$WORK_DIR/retry-stage"
  write_dump_stage "$retry_stage" 20260824T000003Z
  retry_snapshot="$(create_snapshot "$retry_stage" "$WORK_DIR/retry-backup.json")"
  rm -rf -- "$retry_stage"
  [ "$retry_snapshot" != "$second_snapshot" ] || die "retry after remote failure did not create a new snapshot"
  restore_snapshot_to_disposable_database "$retry_snapshot"
  snapshot_count="$(backup_snapshot_count "$WORK_DIR/after-retry.json")"
  [ "$snapshot_count" -ge 2 ] || die "retry after remote failure lost required generations"
  if grep --quiet --fixed-strings 'inf01-correct-restic-password' "$WORK_DIR"/*.log; then
    die "secret appeared in disposable proof logs"
  fi
  note "local S3-compatible encrypted backup, wrong-key rejection, restore invariants, repeatability, retention, and remote-failure behavior passed (snapshots=$snapshot_count)"
}

main "$@"