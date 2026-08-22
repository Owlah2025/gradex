#!/usr/bin/env bash

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_COMPOSE_FILE="$S12_ROOT/deploy/compose/compose.production-like.yml"
S12_STATE_DIR="$S12_ROOT/deploy/.state"
S12_ENV_FILE="$S12_STATE_DIR/production-like.env"
S12_BACKUP_DIR="$S12_STATE_DIR/backups"
S12_BACKUP_FILE="$S12_BACKUP_DIR/gradex-s12.dump"
S12_CHECKSUM_FILE="$S12_BACKUP_FILE.sha256"
S12_SCHEMA_STATE_FILE="$S12_BACKUP_FILE.schema-state"
S12_SCHEMA_STATE_CHECKSUM_FILE="$S12_SCHEMA_STATE_FILE.sha256"
S12_RESTORED_SCHEMA_STATE_FILE="$S12_BACKUP_DIR/restored-schema-state"
S12_COMPLETED_AT_FILE="$S12_BACKUP_FILE.completed-at"
S12_PROJECT="gradex-s12"

note() { printf 's12-recovery: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

load_environment() {
  [ -f "$S12_ENV_FILE" ] || die "run environment.sh up first"
  set -a
  # shellcheck disable=SC1090
  . "$S12_ENV_FILE"
  set +a
}

compose() {
  sed -n '1,999p' "$S12_COMPOSE_FILE" |
    docker compose --file - --project-name "$S12_PROJECT" "$@"
}

service_id() {
  compose --profile restore ps --all --quiet "$1"
}

seed_known_records() {
  load_environment
  local source_id
  source_id="$(service_id postgres)"
  [ -n "$source_id" ] || die "source PostgreSQL is absent"
  docker exec --interactive "$source_id" psql --no-psqlrc --set ON_ERROR_STOP=1 \
    --username gradex --dbname gradex <<'SQL'
BEGIN;
INSERT INTO accounts (id, normalized_email, email, role, status, display_name, email_verified_at)
VALUES
  ('00000000-0000-4000-8000-00000000a001', 's12-admin@example.test', 's12-admin@example.test', 'ADMIN', 'ACTIVE', 'S12 Restore Admin', now()),
  ('00000000-0000-4000-8000-00000000a002', 's12-instructor@example.test', 's12-instructor@example.test', 'INSTRUCTOR', 'ACTIVE', 'S12 Restore Instructor', now()),
  ('00000000-0000-4000-8000-00000000a003', 's12-student@example.test', 's12-student@example.test', 'STUDENT', 'ACTIVE', 'S12 Restore Student', now())
ON CONFLICT (id) DO NOTHING;

INSERT INTO courses (id, owner_account_id, lifecycle, default_access_ends_at)
VALUES ('00000000-0000-4000-8000-00000000c001', '00000000-0000-4000-8000-00000000a002', 'DRAFT', '2030-08-15T00:00:00Z')
ON CONFLICT (id) DO NOTHING;

INSERT INTO course_access_invitations
  (id, normalized_email, email, course_id, created_by_account_id, decided_by_account_id,
   accepted_by_account_id, state, accepted_at, decided_at)
VALUES
  ('00000000-0000-4000-8000-00000000d001', 's12-student@example.test', 's12-student@example.test',
   '00000000-0000-4000-8000-00000000c001', '00000000-0000-4000-8000-00000000a001',
   '00000000-0000-4000-8000-00000000a001', '00000000-0000-4000-8000-00000000a003',
   'APPROVED', now(), now())
ON CONFLICT (id) DO NOTHING;

INSERT INTO entitlements
  (id, student_account_id, scope_kind, scope_id, course_id, grant_source, source_invitation_id,
   original_access_ends_at, access_ends_at, retirement_eligibility_at, state)
VALUES
  ('00000000-0000-4000-8000-00000000e001', '00000000-0000-4000-8000-00000000a003', 'COURSE',
   '00000000-0000-4000-8000-00000000c001', '00000000-0000-4000-8000-00000000c001',
   'MANUAL_INVITATION', '00000000-0000-4000-8000-00000000d001', '2030-08-15T00:00:00Z',
   '2030-08-15T00:00:00Z', '2030-08-15T00:00:00Z', 'ACTIVE')
ON CONFLICT (id) DO NOTHING;

INSERT INTO enrollments (id, student_account_id, course_id)
VALUES ('00000000-0000-4000-8000-00000000f001', '00000000-0000-4000-8000-00000000a003',
        '00000000-0000-4000-8000-00000000c001')
ON CONFLICT (id) DO NOTHING;
COMMIT;
SQL
  note "known identity/access records are present in the source database"
}

create_backup() {
  load_environment
  local source_id partial_file schema_before schema_after
  source_id="$(service_id postgres)"
  [ -n "$source_id" ] || die "source PostgreSQL is absent"
  mkdir -p "$S12_BACKUP_DIR"
  chmod 700 "$S12_BACKUP_DIR"
  partial_file="$S12_BACKUP_FILE.partial"
  rm -f "$partial_file"

  schema_before="$(docker exec "$source_id" psql --no-psqlrc --username gradex --dbname gradex \
    --tuples-only --no-align --command "SELECT version::text || '|' || dirty::text FROM schema_migrations;")"
  [[ "$schema_before" =~ ^[0-9]+\|false$ ]] ||
    die "refusing backup from invalid or non-clean schema state: $schema_before"

  docker exec "$source_id" pg_dump --format=custom --no-owner --no-acl \
    --username gradex --dbname gradex >"$partial_file"
  [ -s "$partial_file" ] || die "backup is empty"

  schema_after="$(docker exec "$source_id" psql --no-psqlrc --username gradex --dbname gradex \
    --tuples-only --no-align --command "SELECT version::text || '|' || dirty::text FROM schema_migrations;")"
  [ "$schema_after" = "$schema_before" ] ||
    die "schema changed while backup was being created: $schema_before -> $schema_after"

  mv "$partial_file" "$S12_BACKUP_FILE"
  printf '%s\n' "$schema_before" >"$S12_SCHEMA_STATE_FILE"
  sha256sum "$S12_BACKUP_FILE" >"$S12_CHECKSUM_FILE"
  sha256sum "$S12_SCHEMA_STATE_FILE" >"$S12_SCHEMA_STATE_CHECKSUM_FILE"
  date +%s >"$S12_COMPLETED_AT_FILE"
  chmod 600 \
    "$S12_BACKUP_FILE" \
    "$S12_CHECKSUM_FILE" \
    "$S12_SCHEMA_STATE_FILE" \
    "$S12_SCHEMA_STATE_CHECKSUM_FILE" \
    "$S12_COMPLETED_AT_FILE"

  rm -f -- "$S12_RESTORED_SCHEMA_STATE_FILE"

  note "backup, schema metadata, and checksums created in ignored state at schema $schema_before"
}

wait_for_healthy() {
  local service="$1" attempts=0 container status
  container="$(service_id "$service")"
  [ -n "$container" ] || die "$service container is absent"
  while [ "$attempts" -lt 90 ]; do
    status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container")"
    [ "$status" = "healthy" ] && return
    if [ "$status" = "exited" ] || [ "$status" = "dead" ] || [ "$status" = "unhealthy" ]; then
      compose --profile restore logs --no-color "$service" >&2 || true
      die "$service reached $status"
    fi
    attempts=$((attempts + 1))
    sleep 2
  done
  die "$service did not become healthy"
}

restore_backup() {
  load_environment
  [ -s "$S12_BACKUP_FILE" ] || die "backup is absent"
  [ -s "$S12_SCHEMA_STATE_FILE" ] || die "backup schema metadata is absent"
  [ -s "$S12_SCHEMA_STATE_CHECKSUM_FILE" ] || die "backup schema metadata checksum is absent"

  (cd "$S12_BACKUP_DIR" && sha256sum --check "$(basename "$S12_CHECKSUM_FILE")") >/dev/null
  (cd "$S12_BACKUP_DIR" && sha256sum --check "$(basename "$S12_SCHEMA_STATE_CHECKSUM_FILE")") >/dev/null

  local expected_schema_state source_id target_id source_volume target_volume
  expected_schema_state="$(cat "$S12_SCHEMA_STATE_FILE")"
  [[ "$expected_schema_state" =~ ^[0-9]+\|false$ ]] ||
    die "backup schema metadata is invalid: $expected_schema_state"

  rm -f -- "$S12_RESTORED_SCHEMA_STATE_FILE"

  source_id="$(service_id postgres)"
  [ -n "$source_id" ] || die "source PostgreSQL is absent"
  source_volume="${S12_PROJECT}_postgres-data"
  target_volume="${S12_PROJECT}_restore-data"
  [ "$source_volume" != "$target_volume" ] || die "source and restore volumes resolve to the same name"

  compose --profile restore rm --stop --force api-restore restore-postgres >/dev/null 2>&1 || true
  if docker volume inspect "$target_volume" >/dev/null 2>&1; then
    docker volume rm "$target_volume" >/dev/null
  fi
  compose --profile restore up --detach restore-postgres
  wait_for_healthy restore-postgres
  target_id="$(service_id restore-postgres)"
  [ "$source_id" != "$target_id" ] || die "source and restore containers are identical"

  docker exec --interactive "$target_id" pg_restore --exit-on-error --single-transaction \
    --no-owner --no-acl --username gradex_restore --dbname gradex_restore <"$S12_BACKUP_FILE"

  printf '%s\n' "$expected_schema_state" >"$S12_RESTORED_SCHEMA_STATE_FILE"
  chmod 600 "$S12_RESTORED_SCHEMA_STATE_FILE"

  note "backup restored into the fresh restore-postgres database at schema $expected_schema_state without cleaning the source"
}

usage() {
  printf 'usage: %s {seed|backup|restore}\n' "$0" >&2
  exit 2
}

case "${1:-}" in
  seed) seed_known_records ;;
  backup) create_backup ;;
  restore) restore_backup ;;
  *) usage ;;
esac
