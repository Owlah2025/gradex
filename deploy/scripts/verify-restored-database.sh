#!/usr/bin/env bash

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_COMPOSE_FILE="$S12_ROOT/deploy/compose/compose.production-like.yml"
S12_STATE_DIR="$S12_ROOT/deploy/.state"
S12_ENV_FILE="$S12_STATE_DIR/production-like.env"
S12_BACKUP_DIR="$S12_STATE_DIR/backups"
S12_RESTORED_SCHEMA_STATE_FILE="$S12_BACKUP_DIR/restored-schema-state"
S12_PROJECT="gradex-s12"

note() { printf 's12-restore-verify: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

[ -f "$S12_ENV_FILE" ] || die "production-like environment state is absent"
set -a
# shellcheck disable=SC1090
. "$S12_ENV_FILE"
set +a

compose() {
  sed -n '1,999p' "$S12_COMPOSE_FILE" |
    docker compose --file - --project-name "$S12_PROJECT" --profile restore "$@"
}

service_id() {
  compose ps --all --quiet "$1"
}

restore_id="$(service_id restore-postgres)"
[ -n "$restore_id" ] || die "restore-postgres is absent"

[ -s "$S12_RESTORED_SCHEMA_STATE_FILE" ] || die "restored schema provenance is absent"
expected_schema_state="$(cat "$S12_RESTORED_SCHEMA_STATE_FILE")"
[[ "$expected_schema_state" =~ ^[0-9]+\|false$ ]] ||
  die "restored schema provenance is invalid: $expected_schema_state"
expected_schema_result="${expected_schema_state/|/:}"

result="$(docker exec "$restore_id" psql --no-psqlrc --tuples-only --no-align \
  --username gradex_restore --dbname gradex_restore --command "
    SELECT
      (SELECT version::text || ':' || dirty::text FROM schema_migrations),
      (SELECT count(*) FROM accounts WHERE id IN
        ('00000000-0000-4000-8000-00000000a001','00000000-0000-4000-8000-00000000a002','00000000-0000-4000-8000-00000000a003')),
      (SELECT count(*) FROM courses WHERE id = '00000000-0000-4000-8000-00000000c001'),
      (SELECT count(*) FROM course_access_invitations
        WHERE id = '00000000-0000-4000-8000-00000000d001' AND state = 'APPROVED'),
      (SELECT count(*) FROM entitlements
        WHERE id = '00000000-0000-4000-8000-00000000e001'
          AND source_invitation_id = '00000000-0000-4000-8000-00000000d001' AND state = 'ACTIVE'),
      (SELECT count(*) FROM enrollments WHERE id = '00000000-0000-4000-8000-00000000f001');")"

[ "$result" = "$expected_schema_result|3|1|1|1|1" ] ||
  die "restored critical-record assertion failed: $result"
note "schema $expected_schema_state, identity, invitation provenance, entitlement, and enrollment assertions passed"

compose up --detach api-restore
api_id="$(service_id api-restore)"
[ -n "$api_id" ] || die "api-restore is absent"

attempts=0
while [ "$attempts" -lt 90 ]; do
  status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$api_id")"
  [ "$status" = "healthy" ] && break
  [ "$status" = "unhealthy" ] && die "api-restore became unhealthy"
  attempts=$((attempts + 1))
  sleep 2
done
[ "${status:-}" = "healthy" ] || die "api-restore did not become healthy"

docker exec "$api_id" wget -qO- http://127.0.0.1:8080/healthz
printf '\n'
docker exec "$api_id" wget -qO- http://127.0.0.1:8080/readyz
printf '\n'
docker exec "$api_id" wget -qO- http://127.0.0.1:8080/api/v1/catalog/courses
printf '\n'
note "Gradex health, readiness, and representative read passed against only the restored database"
