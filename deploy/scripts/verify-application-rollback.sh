#!/usr/bin/env bash

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_STATE_DIR="$S12_ROOT/deploy/.state"
S12_ENV_FILE="$S12_STATE_DIR/production-like.env"
S12_BASE_RELEASE="15f7ec294d524b866cfee9ce8d46d1844962c2c9"
S12_CURRENT_RELEASE="$(git -C "$S12_ROOT" rev-parse HEAD)"
S12_PROJECT="gradex-s12"
S12_TEMPORARY=""

note() { printf 's12-rollback-proof: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

cleanup() {
  if [ -n "$S12_TEMPORARY" ] && [ -d "$S12_TEMPORARY" ]; then
    rm -rf -- "$S12_TEMPORARY"
  fi
}

compose() {
  sed -n '1,999p' "$S12_ROOT/deploy/compose/compose.production-like.yml" |
    docker compose --file - --project-name "$S12_PROJECT" "$@"
}

database_state() {
  local postgres_id
  postgres_id="$(compose ps --all --quiet postgres)"
  [ -n "$postgres_id" ] || die "PostgreSQL is absent"
  docker exec "$postgres_id" psql --no-psqlrc --username gradex --dbname gradex \
    --tuples-only --no-align --command "
      SELECT version::text || '|' || dirty::text || '|' ||
        (SELECT count(*) FROM entitlements)::text || '|' ||
        (SELECT count(*) FROM entitlements
          WHERE grant_source = 'MANUAL_INVITATION' AND source_invitation_id IS NOT NULL)::text
      FROM schema_migrations;"
}

container_image() {
  local service="$1" container
  container="$(compose ps --all --quiet "$service")"
  docker inspect --format '{{.Image}}' "$container"
}

image_max_schema_version() {
  local image="$1" version
  version="$(docker run --rm --entrypoint gradex-migrate "$image" max-version)" ||
    die "could not read max schema version from backend image $image"
  [[ "$version" =~ ^[0-9]+$ ]] ||
    die "backend image $image returned an invalid max schema version"
  printf '%s' "$version"
}

write_manifest() {
  local file="$1" release="$2" backend="$3" frontend="$4"
  {
    printf 'GRADEX_RELEASE_ID=%s\n' "$release"
    printf 'GRADEX_BACKEND_IMAGE=%s\n' "$backend"
    printf 'GRADEX_FRONTEND_IMAGE=%s\n' "$frontend"
  } >"$file"
  chmod 600 "$file"
}

main() {
  local tool
  for tool in docker git mktemp tar; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done
  [ -f "$S12_ENV_FILE" ] || die "run environment.sh up first"
  [ "$S12_CURRENT_RELEASE" = "$(git -C "$S12_ROOT" rev-parse HEAD)" ] || die "current release changed during proof setup"
  set -a
  # shellcheck disable=SC1090
  . "$S12_ENV_FILE"
  set +a

  S12_TEMPORARY="$(mktemp -d "$S12_STATE_DIR/rollback.XXXXXX")"
  chmod 700 "$S12_TEMPORARY"
  trap cleanup EXIT
  local n_context="$S12_TEMPORARY/n" n_manifest="$S12_TEMPORARY/release-n.env"
  local next_manifest="$S12_TEMPORARY/release-n-plus-1.env"
  mkdir -p "$n_context"

  git -C "$S12_ROOT" archive "$S12_BASE_RELEASE" backend frontend | tar -x -C "$n_context"
  [ -f "$n_context/backend/Dockerfile" ] || die "N backend Dockerfile was not extracted"
  [ -f "$n_context/frontend/Dockerfile" ] || die "N frontend Dockerfile was not extracted"
  tar -C "$n_context/backend" -cf - . |
    docker build --quiet --tag gradex-backend:s12-rollback-n - >/dev/null
  tar -C "$n_context/frontend" -cf - . |
    docker build --quiet --tag gradex-frontend:s12-rollback-n - >/dev/null
  docker tag "$GRADEX_BACKEND_IMAGE" gradex-backend:s12-rollback-n-plus-1
  docker tag "$GRADEX_FRONTEND_IMAGE" gradex-frontend:s12-rollback-n-plus-1

  local live_state live_schema base_max_schema
  live_state="$(database_state)"
  IFS='|' read -r live_schema _ <<<"$live_state"
  [[ "$live_schema" =~ ^[0-9]+$ ]] || die "live schema state is invalid: $live_state"
  base_max_schema="$(image_max_schema_version gradex-backend:s12-rollback-n)"
  [ "$live_schema" -le "$base_max_schema" ] ||
    die "rollback proof base release $S12_BASE_RELEASE supports schema through $base_max_schema, but the live schema is $live_schema; choose a schema-compatible N release"

  write_manifest "$n_manifest" "$S12_BASE_RELEASE" \
    gradex-backend:s12-rollback-n gradex-frontend:s12-rollback-n
  write_manifest "$next_manifest" "$S12_CURRENT_RELEASE" \
    gradex-backend:s12-rollback-n-plus-1 gradex-frontend:s12-rollback-n-plus-1

  local before after_n after_next after_rollback n_backend next_backend rollback_backend
  before="$(database_state)"
  "$S12_ROOT/deploy/scripts/application-rollback.sh" apply "$n_manifest"
  after_n="$(database_state)"
  n_backend="$(container_image api)"
  "$S12_ROOT/deploy/scripts/application-rollback.sh" apply "$next_manifest"
  after_next="$(database_state)"
  next_backend="$(container_image api)"
  [ "$n_backend" != "$next_backend" ] || die "N and N+1 resolved to the same backend image"
  "$S12_ROOT/deploy/scripts/application-rollback.sh" apply "$n_manifest"
  after_rollback="$(database_state)"
  rollback_backend="$(container_image api)"

  [ "$n_backend" = "$rollback_backend" ] || die "rollback did not restore the N backend image"
  [ "$before" = "$after_n" ] && [ "$before" = "$after_next" ] && [ "$before" = "$after_rollback" ] ||
    die "schema or Entitlement provenance counts changed: $before / $after_n / $after_next / $after_rollback"
  case "$after_rollback" in
    "$live_schema"\|false\|*\|*) ;;
    *) die "rollback did not retain clean schema $live_schema" ;;
  esac

  note "N=$S12_BASE_RELEASE N+1=$S12_CURRENT_RELEASE N restored; probes passed and schema/provenance state stayed $after_rollback"
  note "backend images N=$n_backend N+1=$next_backend rollback=$rollback_backend"
}

main "$@"
