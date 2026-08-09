#!/usr/bin/env bash

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_COMPOSE_FILE="$S12_ROOT/deploy/compose/compose.production-like.yml"
S12_STATE_DIR="$S12_ROOT/deploy/.state"
S12_ENV_FILE="$S12_STATE_DIR/production-like.env"
S12_CA_FILE="$S12_STATE_DIR/caddy-root.crt"
S12_REDIS_TLS_DIR="$S12_STATE_DIR/redis-tls"
S12_REDIS_TLS_CA_FILE="$S12_REDIS_TLS_DIR/ca.crt"
S12_REDIS_TLS_SERVER_CERT_FILE="$S12_REDIS_TLS_DIR/server.crt"
S12_REDIS_TLS_SERVER_KEY_FILE="$S12_REDIS_TLS_DIR/server.key"
S12_PROJECT="gradex-s12"

note() { printf 's12-environment: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

require_tools() {
  local tool
  for tool in curl docker grep openssl sed tar timeout; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done
  docker info >/dev/null 2>&1 || die "Docker is not reachable"
}

prepare_redis_tls() {
  if [ -s "$S12_REDIS_TLS_CA_FILE" ] && [ -s "$S12_REDIS_TLS_SERVER_CERT_FILE" ] && [ -s "$S12_REDIS_TLS_SERVER_KEY_FILE" ]; then
    chmod 644 "$S12_REDIS_TLS_CA_FILE" "$S12_REDIS_TLS_SERVER_CERT_FILE"
    chmod 600 "$S12_REDIS_TLS_SERVER_KEY_FILE"
    return
  fi
  local ca_key server_request
  ca_key="$S12_REDIS_TLS_DIR/ca.key"
  server_request="$S12_REDIS_TLS_DIR/server.csr"
  mkdir -p "$S12_REDIS_TLS_DIR"
  chmod 700 "$S12_REDIS_TLS_DIR"
  rm -f -- "$S12_REDIS_TLS_CA_FILE" "$S12_REDIS_TLS_SERVER_CERT_FILE" "$S12_REDIS_TLS_SERVER_KEY_FILE" "$ca_key" "$server_request"
  umask 077
  openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days 30 \
    -subj '/CN=Gradex S12 Redis CA' \
    -addext 'basicConstraints=critical,CA:TRUE' \
    -addext 'keyUsage=critical,keyCertSign,cRLSign' \
    -keyout "$ca_key" -out "$S12_REDIS_TLS_CA_FILE" >/dev/null 2>&1
  openssl req -new -newkey rsa:2048 -sha256 -nodes -subj '/CN=redis' \
    -keyout "$S12_REDIS_TLS_SERVER_KEY_FILE" -out "$server_request" >/dev/null 2>&1
  openssl x509 -req -sha256 -days 30 -in "$server_request" \
    -CA "$S12_REDIS_TLS_CA_FILE" -CAkey "$ca_key" -CAcreateserial \
    -extfile "$S12_ROOT/deploy/compose/redis-server.ext" \
    -out "$S12_REDIS_TLS_SERVER_CERT_FILE" >/dev/null 2>&1
  rm -f -- "$ca_key" "$server_request" "$S12_REDIS_TLS_DIR/ca.srl"
  chmod 644 "$S12_REDIS_TLS_CA_FILE" "$S12_REDIS_TLS_SERVER_CERT_FILE"
  chmod 600 "$S12_REDIS_TLS_SERVER_KEY_FILE"
  note "created ignored Redis TLS certificate state"
}

prepare() {
  require_tools
  local restore_postgres_password
  mkdir -p "$S12_STATE_DIR"
  chmod 700 "$S12_STATE_DIR"
  prepare_redis_tls
  if [ -f "$S12_ENV_FILE" ]; then
    set -a
    # shellcheck disable=SC1090
    . "$S12_ENV_FILE"
    set +a
    if grep -q 'gradex_playwright_e2e_s12_media' "$S12_ENV_FILE"; then
      sed -i 's/gradex_playwright_e2e_s12_media/gradex_playwright_e2e_s12media01/g' "$S12_ENV_FILE"
      note "corrected existing media-proof database name to the repository safety pattern"
    fi
    if ! grep -q '^RESTORE_POSTGRES_PASSWORD=' "$S12_ENV_FILE"; then
      umask 077
      restore_postgres_password="$(openssl rand -hex 24)"
      {
        printf 'RESTORE_POSTGRES_PASSWORD=%s\n' "$restore_postgres_password"
        printf 'RESTORE_DATABASE_URL=postgres://gradex_restore:%s@restore-postgres:5432/gradex_restore?sslmode=disable\n' \
          "$restore_postgres_password"
      } >>"$S12_ENV_FILE"
      note "upgraded existing ignored environment state for isolated restore"
    fi
    if ! grep -q '^MEDIA_PROOF_DATABASE_URL=' "$S12_ENV_FILE"; then
      {
        printf 'MEDIA_PROOF_DATABASE_URL=postgres://gradex:%s@postgres:5432/gradex_playwright_e2e_s12media01?sslmode=disable\n' \
          "$POSTGRES_PASSWORD"
        printf 'GRADEX_PROOF_IMAGE=gradex-backend-proof:s12-local\n'
      } >>"$S12_ENV_FILE"
      note "upgraded existing ignored environment state for media proof"
    fi
    if ! grep -q '^REDIS_PASSWORD=' "$S12_ENV_FILE"; then
      printf 'REDIS_PASSWORD=%s\n' "$(openssl rand -hex 24)" >>"$S12_ENV_FILE"
    fi
    if ! grep -q '^REDIS_TLS_CA_CERT_FILE_HOST=' "$S12_ENV_FILE"; then
      {
        printf 'REDIS_TLS_CA_CERT_FILE_HOST=%s\n' "$S12_REDIS_TLS_CA_FILE"
        printf 'REDIS_TLS_SERVER_CERT_FILE_HOST=%s\n' "$S12_REDIS_TLS_SERVER_CERT_FILE"
        printf 'REDIS_TLS_SERVER_KEY_FILE_HOST=%s\n' "$S12_REDIS_TLS_SERVER_KEY_FILE"
      } >>"$S12_ENV_FILE"
      note "upgraded existing ignored environment state for authenticated TLS Redis"
    fi
    note "using existing ignored environment state"
    return
  fi

  umask 077
  local postgres_password redis_password s3_access_key s3_secret_key minio_root_user minio_root_password
  postgres_password="$(openssl rand -hex 24)"
  redis_password="$(openssl rand -hex 24)"
  restore_postgres_password="$(openssl rand -hex 24)"
  s3_access_key="s12$(openssl rand -hex 8)"
  s3_secret_key="$(openssl rand -hex 24)"
  minio_root_user="root$(openssl rand -hex 8)"
  minio_root_password="$(openssl rand -hex 24)"

  {
    printf 'PUBLIC_ORIGIN=https://gradex.localhost:18443\n'
    printf 'POSTGRES_PASSWORD=%s\n' "$postgres_password"
    printf 'DATABASE_URL=postgres://gradex:%s@postgres:5432/gradex?sslmode=disable\n' "$postgres_password"
    printf 'REDIS_PASSWORD=%s\n' "$redis_password"
    printf 'REDIS_TLS_CA_CERT_FILE_HOST=%s\n' "$S12_REDIS_TLS_CA_FILE"
    printf 'REDIS_TLS_SERVER_CERT_FILE_HOST=%s\n' "$S12_REDIS_TLS_SERVER_CERT_FILE"
    printf 'REDIS_TLS_SERVER_KEY_FILE_HOST=%s\n' "$S12_REDIS_TLS_SERVER_KEY_FILE"
    printf 'RESTORE_POSTGRES_PASSWORD=%s\n' "$restore_postgres_password"
    printf 'RESTORE_DATABASE_URL=postgres://gradex_restore:%s@restore-postgres:5432/gradex_restore?sslmode=disable\n' \
      "$restore_postgres_password"
    printf 'MEDIA_PROOF_DATABASE_URL=postgres://gradex:%s@postgres:5432/gradex_playwright_e2e_s12media01?sslmode=disable\n' \
      "$postgres_password"
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
    printf 'EMAIL_API_KEY=re_production_like_noncredential\n'
    printf 'GRADEX_BACKEND_IMAGE=gradex-backend:s12-local\n'
    printf 'GRADEX_FRONTEND_IMAGE=gradex-frontend:s12-local\n'
    printf 'GRADEX_EDGE_IMAGE=gradex-edge:s12-local\n'
    printf 'GRADEX_PROOF_IMAGE=gradex-backend-proof:s12-local\n'
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
  tar --exclude=.git --exclude=.env --exclude='.env.*' --exclude='*.out' --exclude=coverage \
    -C "$S12_ROOT/backend" -cf - . |
    docker build --target proof --tag "$GRADEX_PROOF_IMAGE" -
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
  docker exec "$redis_id" redis-cli --tls --cacert /run/gradex/redis/ca.crt ping

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

verify_redis_security() {
  load_environment
  local redis_id plaintext_result unauthenticated_result authenticated_result
  redis_id="$(service_id redis)"
  [ -n "$redis_id" ] || die "redis container is absent"

  openssl verify -CAfile "$S12_REDIS_TLS_CA_FILE" "$S12_REDIS_TLS_SERVER_CERT_FILE" >/dev/null ||
    die "Redis server certificate failed CA verification"

  plaintext_result="$(timeout 5 docker exec "$redis_id" redis-cli -h redis -p 6379 ping 2>&1 || true)"
  [ "$plaintext_result" != "PONG" ] || die "Redis accepted a plaintext connection"

  unauthenticated_result="$(docker exec --env REDISCLI_AUTH= "$redis_id" \
    redis-cli --tls --cacert /run/gradex/redis/ca.crt -h redis -p 6379 ping 2>&1 || true)"
  case "$unauthenticated_result" in
    *NOAUTH*) ;;
    *) die "Redis did not reject an unauthenticated TLS connection" ;;
  esac

  authenticated_result="$(docker exec "$redis_id" \
    redis-cli --tls --cacert /run/gradex/redis/ca.crt -h redis -p 6379 ping 2>&1)"
  [ "$authenticated_result" = "PONG" ] || die "authenticated Redis TLS probe failed"

  note "Redis rejected plaintext and unauthenticated access; authenticated verified TLS passed"
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
  rm -rf -- "$S12_REDIS_TLS_DIR"
  note "removed only $S12_PROJECT containers, networks, volumes, and generated state"
}

usage() {
  printf 'usage: %s {prepare|build|up|verify|data-plane|redis-security|status|logs|stop|reset}\n' "$0" >&2
  exit 2
}

case "${1:-}" in
  prepare) prepare ;;
  build) build_images ;;
  up) start_environment ;;
  verify) verify_environment ;;
  data-plane) verify_data_plane ;;
  redis-security) verify_redis_security ;;
  status) load_environment; compose ps ;;
  logs)
    load_environment
    if [ -n "${2:-}" ]; then compose logs --no-color "$2"; else compose logs --no-color; fi
    ;;
  stop) stop_environment ;;
  reset) reset_environment ;;
  *) usage ;;
esac
