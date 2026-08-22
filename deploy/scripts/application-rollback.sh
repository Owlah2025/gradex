#!/usr/bin/env bash

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_COMPOSE_FILE="$S12_ROOT/deploy/compose/compose.production-like.yml"
S12_STATE_DIR="$S12_ROOT/deploy/.state"
S12_ENV_FILE="$S12_STATE_DIR/production-like.env"
S12_PROJECT="gradex-s12"

note() { printf 's12-application-release: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

compose() {
  sed -n '1,999p' "$S12_COMPOSE_FILE" |
    docker compose --file - --project-name "$S12_PROJECT" "$@"
}

manifest_value() {
  local file="$1" key="$2" value count
  count="$(grep --count --extended-regexp "^${key}=" "$file" || true)"
  [ "$count" = "1" ] || die "$file must contain exactly one $key entry"
  value="$(sed -n "s/^${key}=//p" "$file")"
  [ -n "$value" ] || die "$key is empty"
  case "$value" in
    *[!A-Za-z0-9._@/:+-]*) die "$key contains unsupported characters" ;;
  esac
  printf '%s' "$value"
}

service_id() {
  compose ps --all --quiet "$1"
}

image_max_schema_version() {
  local image="$1" version
  version="$(docker run --rm --entrypoint gradex-migrate "$image" max-version)" ||
    die "could not read max schema version from target backend image"
  [[ "$version" =~ ^[0-9]+$ ]] ||
    die "target backend image returned an invalid max schema version"
  printf '%s' "$version"
}

wait_for_status() {
  local service="$1" wanted="$2" attempts=0 container status
  container="$(service_id "$service")"
  [ -n "$container" ] || die "$service container is absent"
  while [ "$attempts" -lt 90 ]; do
    status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container")"
    [ "$status" = "$wanted" ] && return
    case "$status" in exited|dead|unhealthy) die "$service reached $status" ;; esac
    attempts=$((attempts + 1))
    sleep 2
  done
  die "$service did not reach $wanted"
}

apply_release() {
  local manifest="$1"
  [ -f "$S12_ENV_FILE" ] || die "run environment.sh up first"
  [ -f "$manifest" ] || die "release manifest is absent: $manifest"
  set -a
  # shellcheck disable=SC1090
  . "$S12_ENV_FILE"
  set +a

  local release_id backend_image frontend_image postgres_id
  local schema_state schema_version schema_dirty target_max_schema
  release_id="$(manifest_value "$manifest" GRADEX_RELEASE_ID)"
  backend_image="$(manifest_value "$manifest" GRADEX_BACKEND_IMAGE)"
  frontend_image="$(manifest_value "$manifest" GRADEX_FRONTEND_IMAGE)"
  docker image inspect "$backend_image" "$frontend_image" >/dev/null 2>&1 ||
    die "one or more release images are unavailable locally"

  postgres_id="$(service_id postgres)"
  [ -n "$postgres_id" ] || die "PostgreSQL is absent"
  schema_state="$(docker exec "$postgres_id" psql --no-psqlrc --username gradex --dbname gradex \
    --tuples-only --no-align --command 'SELECT version::text || '\''|'\'' || dirty::text FROM schema_migrations;')"
  IFS='|' read -r schema_version schema_dirty <<<"$schema_state"
  [[ "$schema_version" =~ ^[0-9]+$ ]] || die "schema version is invalid: $schema_state"
  [ "$schema_dirty" = false ] || die "schema is dirty: $schema_state"

  target_max_schema="$(image_max_schema_version "$backend_image")"
  [ "$schema_version" -le "$target_max_schema" ] ||
    die "schema $schema_version is newer than target release maximum $target_max_schema"

  export GRADEX_BACKEND_IMAGE="$backend_image"
  export GRADEX_FRONTEND_IMAGE="$frontend_image"
  compose up --detach --no-deps --force-recreate api worker frontend
  wait_for_status api healthy
  wait_for_status frontend healthy
  wait_for_status worker running
  "$S12_ROOT/deploy/scripts/environment.sh" verify

  umask 077
  local selection="$S12_STATE_DIR/current-application-release"
  {
    printf 'GRADEX_RELEASE_ID=%s\n' "$release_id"
    printf 'GRADEX_BACKEND_IMAGE=%s\n' "$backend_image"
    printf 'GRADEX_FRONTEND_IMAGE=%s\n' "$frontend_image"
  } >"$selection.partial"
  mv "$selection.partial" "$selection"
  note "application release $release_id is healthy on unchanged schema $schema_version (target max $target_max_schema); no migration command ran"
}

usage() {
  printf 'usage: %s apply RELEASE_MANIFEST\n' "$0" >&2
  printf 'schema downgrade and database rollback are intentionally unsupported\n' >&2
  exit 2
}

case "${1:-}" in
  apply) [ "$#" = 2 ] || usage; apply_release "$2" ;;
  migrate|downgrade|schema|database) die "application rollback never runs schema or database rollback" ;;
  *) usage ;;
esac
