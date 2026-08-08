#!/usr/bin/env bash

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_COMPOSE_FILE="$S12_ROOT/deploy/compose/compose.production-like.yml"
S12_STATE_DIR="$S12_ROOT/deploy/.state"
S12_ENV_FILE="$S12_STATE_DIR/production-like.env"
S12_CA_FILE="$S12_STATE_DIR/caddy-root.crt"
S12_PROJECT="gradex-s12"

note() { printf 's12-environment: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

require_tools() {
  local tool
  for tool in curl docker openssl sed tar; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done
  docker info >/dev/null 2>&1 || die "Docker is not reachable"
}

prepare() {
  require_tools
  mkdir -p "$S12_STATE_DIR"
  chmod 700 "$S12_STATE_DIR"
  if [ -f "$S12_ENV_FILE" ]; then
    note "using existing ignored environment state"
    return
  fi

  umask 077
  local postgres_password s3_access_key s3_secret_key minio_root_user minio_root_password
  postgres_password="$(openssl rand -hex 24)"
  s3_access_key="s12$(openssl rand -hex 8)"
  s3_secret_key="$(openssl rand -hex 24)"
  minio_root_user="root$(openssl rand -hex 8)"
  minio_root_password="$(openssl rand -hex 24)"

  {
    printf 'PUBLIC_ORIGIN=https://gradex.localhost:18443\n'
    printf 'POSTGRES_PASSWORD=%s\n' "$postgres_password"
    printf 'DATABASE_URL=postgres://gradex:%s@postgres:5432/gradex?sslmode=disable\n' "$postgres_password"
    printf 'S3_ACCESS_KEY=%s\n' "$s3_access_key"
    printf 'S3_SECRET_KEY=%s\n' "$s3_secret_key"
    printf 'MINIO_ROOT_USER=%s\n' "$minio_root_user"
    printf 'MINIO_ROOT_PASSWORD=%s\n' "$minio_root_password"
    printf 'PLAYBACK_TOKEN_SECRET=%s\n' "$(openssl rand -hex 32)"
    printf 'SESSION_CSRF_KEY=%s\n' "$(openssl rand -hex 32)"
    printf 'ANONYMOUS_COOKIE_SIGNING_KEY=%s\n' "$(openssl rand -hex 32)"
    printf 'ANONYMOUS_CSRF_KEY=%s\n' "$(openssl rand -hex 32)"
    printf 'ADMISSION_LIMITER_HMAC_KEY=%s\n' "$(openssl rand -hex 32)"
    printf 'OUTBOX_PROTECTED_PAYLOAD_KEY=%s\n' "$(openssl rand -hex 16)"
    printf 'GRADEX_BACKEND_IMAGE=gradex-backend:s12-local\n'
    printf 'GRADEX_FRONTEND_IMAGE=gradex-frontend:s12-local\n'
    printf 'GRADEX_EDGE_IMAGE=gradex-edge:s12-local\n'
  } >"$S12_ENV_FILE"
  note "created ignored environment state"
}

load_environment() {
  [ -f "$S12_ENV_FILE" ] || die "run prepare first"
  set -a
  # shellcheck disable=SC1090
  . "$S12_ENV_FILE"
  set +a
}

compose() {
  sed -n '1,999p' "$S12_COMPOSE_FILE" |
    docker compose --file - --project-name "$S12_PROJECT" "$@"
}

build_images() {
  prepare
  load_environment
  tar --exclude=.git --exclude=.env --exclude='.env.*' --exclude='*.out' --exclude=coverage \
    -C "$S12_ROOT/backend" -cf - . |
    docker build --tag "$GRADEX_BACKEND_IMAGE" -
  tar --exclude=node_modules --exclude=.next --exclude=coverage \
    -C "$S12_ROOT/frontend" -cf - . |
    docker build --tag "$GRADEX_FRONTEND_IMAGE" -
  tar -C "$S12_ROOT/deploy/compose" -cf - Caddyfile Dockerfile |
    docker build --tag "$GRADEX_EDGE_IMAGE" -
}

service_id() {
  compose ps --all --quiet "$1"
}

wait_for_status() {
  local service="$1" wanted="$2" attempts=0 container status
  container="$(service_id "$service")"
  [ -n "$container" ] || die "$service container is absent"
  while [ "$attempts" -lt 90 ]; do
    status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container")"
    if [ "$status" = "$wanted" ]; then
      return
    fi
    if [ "$status" = "exited" ] || [ "$status" = "dead" ] || [ "$status" = "unhealthy" ]; then
      compose logs --no-color "$service" >&2 || true
      die "$service reached $status while waiting for $wanted"
    fi
    attempts=$((attempts + 1))
    sleep 2
  done
  die "$service did not reach $wanted"
}

wait_for_completion() {
  local service="$1" attempts=0 container status exit_code
  container="$(service_id "$service")"
  [ -n "$container" ] || die "$service container is absent"
  while [ "$attempts" -lt 90 ]; do
    status="$(docker inspect --format '{{.State.Status}}' "$container")"
    if [ "$status" = "exited" ]; then
      exit_code="$(docker inspect --format '{{.State.ExitCode}}' "$container")"
      [ "$exit_code" = "0" ] || {
        compose logs --no-color "$service" >&2 || true
        die "$service exited $exit_code"
      }
      return
    fi
    attempts=$((attempts + 1))
    sleep 2
  done
  die "$service did not complete"
}

start_environment() {
  prepare
  load_environment
  compose up --detach
  wait_for_status postgres healthy
  wait_for_status redis healthy
  wait_for_status minio healthy
  wait_for_completion minio-init
  wait_for_completion migrate
  wait_for_status api healthy
  wait_for_status frontend healthy
  wait_for_status worker running
  wait_for_status edge running
  note "production-like environment is running"
}

verify_environment() {
  load_environment
  local edge_id
  edge_id="$(service_id edge)"
  [ -n "$edge_id" ] || die "edge container is absent"
  docker exec "$edge_id" cat /data/caddy/pki/authorities/local/root.crt >"$S12_CA_FILE"
  chmod 600 "$S12_CA_FILE"
  curl --fail --silent --show-error --cacert "$S12_CA_FILE" \
    --resolve gradex.localhost:18443:127.0.0.1 \
    https://gradex.localhost:18443/ >/dev/null
  curl --fail --silent --show-error --cacert "$S12_CA_FILE" \
    --resolve gradex.localhost:18443:127.0.0.1 \
    https://gradex.localhost:18443/healthz
  printf '\n'
  curl --fail --silent --show-error --cacert "$S12_CA_FILE" \
    --resolve gradex.localhost:18443:127.0.0.1 \
    https://gradex.localhost:18443/readyz
  printf '\n'
  note "frontend and API probes passed through the TLS edge"
}

verify_data_plane() {
  load_environment
  local postgres_id redis_id object_url
  postgres_id="$(service_id postgres)"
  redis_id="$(service_id redis)"
  [ -n "$postgres_id" ] || die "postgres container is absent"
  [ -n "$redis_id" ] || die "redis container is absent"

  docker exec "$postgres_id" psql --no-psqlrc --username gradex --dbname gradex \
    --tuples-only --command 'SELECT version, dirty FROM schema_migrations;'
  docker exec "$redis_id" redis-cli ping

  object_url="http://${S3_ACCESS_KEY}:${S3_SECRET_KEY}@minio:9000"
  printf 'gradex-s12-storage-proof\n' |
    docker run --rm --interactive --network "${S12_PROJECT}_app" \
      --env "MC_HOST_local=$object_url" minio/mc:RELEASE.2025-08-13T08-35-41Z \
      pipe local/gradex-private-media/operations/connectivity-proof.txt >/dev/null
  docker run --rm --network "${S12_PROJECT}_app" \
    --env "MC_HOST_local=$object_url" minio/mc:RELEASE.2025-08-13T08-35-41Z \
    stat local/gradex-private-media/operations/connectivity-proof.txt
  docker run --rm --network "${S12_PROJECT}_app" \
    --env "MC_HOST_local=$object_url" minio/mc:RELEASE.2025-08-13T08-35-41Z \
    cat local/gradex-private-media/operations/connectivity-proof.txt >/dev/null
  if docker run --rm --network "${S12_PROJECT}_app" --entrypoint wget \
    "$GRADEX_BACKEND_IMAGE" -qO- \
    http://minio:9000/gradex-private-media/operations/connectivity-proof.txt >/dev/null 2>&1; then
    die "private storage proof object was anonymously readable"
  fi
  docker run --rm --network "${S12_PROJECT}_app" \
    --env "MC_HOST_local=$object_url" minio/mc:RELEASE.2025-08-13T08-35-41Z \
    anonymous get local/gradex-private-media
  docker run --rm --network "${S12_PROJECT}_app" \
    --env "MC_HOST_local=$object_url" minio/mc:RELEASE.2025-08-13T08-35-41Z \
    rm local/gradex-private-media/operations/connectivity-proof.txt >/dev/null
  note "database, Redis, and private object-storage operations passed"
}

stop_environment() {
  load_environment
  compose down
}

reset_environment() {
  if [ -f "$S12_ENV_FILE" ]; then
    load_environment
    compose down --volumes --remove-orphans
  fi
  rm -f "$S12_ENV_FILE" "$S12_CA_FILE"
  note "removed only $S12_PROJECT containers, networks, volumes, and generated state"
}

usage() {
  printf 'usage: %s {prepare|build|up|verify|data-plane|status|logs|stop|reset}\n' "$0" >&2
  exit 2
}

case "${1:-}" in
  prepare) prepare ;;
  build) build_images ;;
  up) start_environment ;;
  verify) verify_environment ;;
  data-plane) verify_data_plane ;;
  status) load_environment; compose ps ;;
  logs)
    load_environment
    if [ -n "${2:-}" ]; then compose logs --no-color "$2"; else compose logs --no-color; fi
    ;;
  stop) stop_environment ;;
  reset) reset_environment ;;
  *) usage ;;
esac
