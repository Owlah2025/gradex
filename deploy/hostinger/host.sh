#!/usr/bin/env bash

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_HOST_DIR="$S12_ROOT/deploy/hostinger"
S12_COMPOSE_FILE="$S12_HOST_DIR/compose.yml"
S12_HOST_STATE_DIR="${GRADEX_HOST_STATE_DIR:-/var/lib/gradex}"
S12_ENV_FILE="${GRADEX_HOST_ENV_FILE:-$S12_HOST_STATE_DIR/runtime.env}"
S12_REDIS_TLS_DIR="$S12_HOST_STATE_DIR/redis-tls"
S12_BACKUP_DIR="$S12_HOST_STATE_DIR/backups"
S12_PROJECT="${GRADEX_HOST_PROJECT:-gradex-staging}"
S12_DB_TUNNEL_NAME="${S12_PROJECT}-proof-db-tunnel"

note() { printf 's12-hostinger: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

require_tools() {
  local tool
  for tool in awk curl date docker flock grep jq mktemp openssl readlink sed sha256sum stat timeout; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done
  docker info >/dev/null 2>&1 || die "Docker is not reachable"
}

validate_local_targets() {
  [[ "$S12_HOST_STATE_DIR" =~ ^/[A-Za-z0-9._/-]+/gradex(-[A-Za-z0-9._-]+)?$ ]] ||
    die "GRADEX_HOST_STATE_DIR must be an absolute dedicated Gradex directory"
  case "$S12_ENV_FILE" in "$S12_HOST_STATE_DIR"/*) ;; *) die "runtime environment must remain inside host state" ;; esac
  [[ "$S12_PROJECT" =~ ^[a-z0-9][a-z0-9_-]{2,62}$ ]] || die "GRADEX_HOST_PROJECT is invalid"
}

load_environment() {
  validate_local_targets
  [ -f "$S12_ENV_FILE" ] || die "create $S12_ENV_FILE from runtime.env.example"
  local mode
  mode="$(stat -c '%a' "$S12_ENV_FILE")"
  case "$mode" in 400|600) ;; *) die "$S12_ENV_FILE must have mode 0400 or 0600" ;; esac
  set -a
  # shellcheck disable=SC1090
  . "$S12_ENV_FILE"
  set +a
}

require_value() {
  local name="$1"
  [ -n "${!name:-}" ] || die "$name is required in the protected runtime environment"
}

validate_environment() {
  local name image_revision
  validate_local_targets
  for name in GRADEX_RELEASE_SHA GRADEX_BACKEND_IMAGE GRADEX_FRONTEND_IMAGE GRADEX_PROOF_IMAGE \
    STAGING_HOSTNAME ACME_EMAIL PUBLIC_ORIGIN POSTGRES_DB POSTGRES_PASSWORD DATABASE_URL \
    GRADEX_E2E_ADMIN_DB_URL RESTORE_POSTGRES_PASSWORD RESTORE_DATABASE_URL REDIS_PASSWORD S3_ENDPOINT S3_BUCKET \
	S3_ACCESS_KEY S3_SECRET_KEY PLAYBACK_TOKEN_SECRET SALES_WHATSAPP_NUMBER SESSION_CSRF_KEY \
    ANONYMOUS_COOKIE_SIGNING_KEY ANONYMOUS_CSRF_KEY ADMISSION_LIMITER_HMAC_KEY \
    OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION OUTBOX_PROTECTED_PAYLOAD_KEY; do
    require_value "$name"
  done
  [[ "$GRADEX_RELEASE_SHA" =~ ^[0-9a-f]{40}$ ]] || die "GRADEX_RELEASE_SHA must be a full Git SHA"
  [[ "$STAGING_HOSTNAME" =~ ^[A-Za-z0-9.-]+$ ]] || die "STAGING_HOSTNAME is invalid"
  [ "$PUBLIC_ORIGIN" = "https://$STAGING_HOSTNAME" ] || die "PUBLIC_ORIGIN must exactly match the HTTPS staging hostname"
  [[ "$S3_ENDPOINT" =~ ^https://[A-Za-z0-9]+\.r2\.cloudflarestorage\.com$ ]] ||
    die "S3_ENDPOINT must be the credential-free Cloudflare R2 S3 API origin"
	[[ "$SALES_WHATSAPP_NUMBER" =~ ^[0-9]{7,15}$ ]] ||
		die "SALES_WHATSAPP_NUMBER must contain 7 to 15 digits"
  case "$GRADEX_BACKEND_IMAGE $GRADEX_FRONTEND_IMAGE $GRADEX_PROOF_IMAGE" in
    *:latest*|*' 'latest*) die "provider releases may not use latest image tags" ;;
  esac
  for name in REDIS_TLS_CA_CERT_FILE_HOST REDIS_TLS_SERVER_CERT_FILE_HOST REDIS_TLS_SERVER_KEY_FILE_HOST; do
    require_value "$name"
  done
  [ "$REDIS_TLS_CA_CERT_FILE_HOST" = "$S12_REDIS_TLS_DIR/ca.crt" ] || die "Redis CA path does not match host state"
  [ "$REDIS_TLS_SERVER_CERT_FILE_HOST" = "$S12_REDIS_TLS_DIR/server.crt" ] || die "Redis certificate path does not match host state"
  [ "$REDIS_TLS_SERVER_KEY_FILE_HOST" = "$S12_REDIS_TLS_DIR/server.key" ] || die "Redis key path does not match host state"

  docker image inspect "$GRADEX_BACKEND_IMAGE" "$GRADEX_FRONTEND_IMAGE" "$GRADEX_PROOF_IMAGE" >/dev/null 2>&1 ||
    die "load the selected release images before deployment"
  image_revision="$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$GRADEX_BACKEND_IMAGE")"
  [ "$image_revision" = "$GRADEX_RELEASE_SHA" ] || die "backend image revision label does not match GRADEX_RELEASE_SHA"
  image_revision="$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$GRADEX_FRONTEND_IMAGE")"
  [ "$image_revision" = "$GRADEX_RELEASE_SHA" ] || die "frontend image revision label does not match GRADEX_RELEASE_SHA"
  image_revision="$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$GRADEX_PROOF_IMAGE")"
  [ "$image_revision" = "$GRADEX_RELEASE_SHA" ] || die "proof image revision label does not match GRADEX_RELEASE_SHA"
}

compose() {
  sed -n '1,999p' "$S12_COMPOSE_FILE" |
    docker compose --file - --project-directory "$S12_HOST_DIR" --project-name "$S12_PROJECT" "$@"
}

image_max_schema_version() {
  local image="$1" version
  version="$(docker run --rm --entrypoint gradex-migrate "$image" max-version)" ||
    die "could not read max schema version from backend image $image"
  [[ "$version" =~ ^[0-9]+$ ]] ||
    die "backend image $image returned an invalid max schema version"
  printf '%s' "$version"
}

prepare_redis_tls() {
  if [ -s "$S12_REDIS_TLS_DIR/ca.crt" ] && [ -s "$S12_REDIS_TLS_DIR/server.crt" ] && \
    [ -s "$S12_REDIS_TLS_DIR/server.key" ]; then
    chmod 644 "$S12_REDIS_TLS_DIR/ca.crt" "$S12_REDIS_TLS_DIR/server.crt"
    chmod 600 "$S12_REDIS_TLS_DIR/server.key"
    return
  fi
  local ca_key request
  ca_key="$S12_REDIS_TLS_DIR/ca.key"
  request="$S12_REDIS_TLS_DIR/server.csr"
  mkdir -p "$S12_REDIS_TLS_DIR"
  chmod 700 "$S12_REDIS_TLS_DIR"
  umask 077
  openssl req -x509 -newkey rsa:3072 -sha256 -nodes -days 90 \
    -subj '/CN=Gradex Hostinger Redis CA' \
    -addext 'basicConstraints=critical,CA:TRUE' \
    -addext 'keyUsage=critical,keyCertSign,cRLSign' \
    -keyout "$ca_key" -out "$S12_REDIS_TLS_DIR/ca.crt" >/dev/null 2>&1
  openssl req -new -newkey rsa:3072 -sha256 -nodes -subj '/CN=redis' \
    -keyout "$S12_REDIS_TLS_DIR/server.key" -out "$request" >/dev/null 2>&1
  openssl x509 -req -sha256 -days 90 -in "$request" \
    -CA "$S12_REDIS_TLS_DIR/ca.crt" -CAkey "$ca_key" -CAcreateserial \
    -extfile "$S12_ROOT/deploy/compose/redis-server.ext" \
    -out "$S12_REDIS_TLS_DIR/server.crt" >/dev/null 2>&1
  rm -f -- "$ca_key" "$request" "$S12_REDIS_TLS_DIR/ca.srl"
  chmod 644 "$S12_REDIS_TLS_DIR/ca.crt" "$S12_REDIS_TLS_DIR/server.crt"
  chmod 600 "$S12_REDIS_TLS_DIR/server.key"
  note "created Redis TLS material in protected host state"
}

prepare() {
  require_tools
  validate_local_targets
  mkdir -p "$S12_HOST_STATE_DIR" "$S12_BACKUP_DIR"
  chmod 700 "$S12_HOST_STATE_DIR" "$S12_BACKUP_DIR"
  load_environment
  prepare_redis_tls
  validate_environment
  jq --arg origin "$PUBLIC_ORIGIN" '.[0].AllowedOrigins = [$origin]' \
    "$S12_HOST_DIR/r2-cors.json.template" >"$S12_HOST_STATE_DIR/r2-cors.json"
  chmod 600 "$S12_HOST_STATE_DIR/r2-cors.json"
  compose config --quiet
  note "provider configuration, release identity, Redis TLS, and Compose contract are valid"
}

service_id() { compose --profile restore ps --all --quiet "$1"; }

wait_for_status() {
  local service="$1" wanted="$2" attempts=0 container status
  container="$(service_id "$service")"
  [ -n "$container" ] || die "$service container is absent"
  while [ "$attempts" -lt 120 ]; do
    status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container")"
    [ "$status" = "$wanted" ] && return
    case "$status" in exited|dead|unhealthy) compose logs --no-color "$service" >&2 || true; die "$service reached $status" ;; esac
    attempts=$((attempts + 1))
    sleep 2
  done
  die "$service did not reach $wanted"
}

wait_for_completion() {
  local service="$1" attempts=0 container status exit_code
  container="$(service_id "$service")"
  [ -n "$container" ] || die "$service container is absent"
  while [ "$attempts" -lt 120 ]; do
    status="$(docker inspect --format '{{.State.Status}}' "$container")"
    if [ "$status" = "exited" ]; then
      exit_code="$(docker inspect --format '{{.State.ExitCode}}' "$container")"
      [ "$exit_code" = 0 ] || { compose logs --no-color "$service" >&2 || true; die "$service exited $exit_code"; }
      return
    fi
    attempts=$((attempts + 1))
    sleep 2
  done
  die "$service did not complete"
}

start_environment() {
  prepare
  compose up --detach postgres redis
  wait_for_status postgres healthy
  wait_for_status redis healthy
  compose up --detach migrate
  wait_for_completion migrate
  compose up --detach api worker frontend edge
  wait_for_status api healthy
  wait_for_status worker running
  wait_for_status frontend healthy
  wait_for_status edge running
  note "Hostinger staging processes are running"
}

verify_environment() {
  require_tools
  load_environment
  validate_environment
  local postgres_id redis_id worker_id schema_state expected_schema unauthenticated authenticated
  postgres_id="$(service_id postgres)"
  redis_id="$(service_id redis)"
  worker_id="$(service_id worker)"
  [ -n "$postgres_id" ] && [ -n "$redis_id" ] && [ -n "$worker_id" ] || die "required service is absent"

  schema_state="$(docker exec "$postgres_id" psql --no-psqlrc --username gradex --dbname "$POSTGRES_DB" \
    --tuples-only --no-align --command "SELECT version::text || '|' || dirty::text FROM schema_migrations;")"
  expected_schema="$(image_max_schema_version "$GRADEX_BACKEND_IMAGE")"
  [ "$schema_state" = "$expected_schema|false" ] ||
    die "schema is $schema_state, expected clean version $expected_schema for selected backend image"
  [ "$(docker inspect --format '{{.State.Status}}' "$worker_id")" = running ] || die "worker is not running"

  if timeout 5 docker exec "$redis_id" redis-cli -h redis -p 6379 ping >/dev/null 2>&1; then
    die "Redis accepted plaintext"
  fi
  unauthenticated="$(docker exec --env REDISCLI_AUTH= "$redis_id" redis-cli --tls \
    --cacert /run/gradex/redis/ca.crt -h redis -p 6379 ping 2>&1 || true)"
  case "$unauthenticated" in *NOAUTH*) ;; *) die "Redis did not reject unauthenticated TLS" ;; esac
  authenticated="$(docker exec "$redis_id" redis-cli --tls --cacert /run/gradex/redis/ca.crt \
    -h redis -p 6379 ping 2>&1)"
  [ "$authenticated" = PONG ] || die "authenticated Redis TLS failed"

  curl --fail --silent --show-error "$PUBLIC_ORIGIN/" >/dev/null
  curl --fail --silent --show-error "$PUBLIC_ORIGIN/healthz" | jq --exit-status '.status == "ok"' >/dev/null
  curl --fail --silent --show-error "$PUBLIC_ORIGIN/readyz" |
    jq --exit-status '.status == "ok" and .checks.postgres == "ok" and .checks.redis == "ok" and .checks.schema == "ok"' >/dev/null
  note "public probes, clean schema $expected_schema, worker, and authenticated verified-TLS Redis passed"
}

seed_smoke() {
  require_tools
  load_environment
  validate_environment
  compose stop api worker >/dev/null 2>&1 || true
  compose --profile proof run --rm proof-tool
  printf '#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:1.0,\nsegment000.ts\n#EXT-X-ENDLIST\n' |
    compose --profile proof run --rm --no-deps -T --entrypoint gradex-storage-fixture proof-tool \
      -key test/master.m3u8
  docker run --rm --entrypoint ffmpeg "$GRADEX_BACKEND_IMAGE" \
    -nostdin -v error -f lavfi -i 'testsrc=size=320x240:rate=24' -t 1 \
    -c:v libx264 -pix_fmt yuv420p -f mpegts pipe:1 |
    compose --profile proof run --rm --no-deps -T --entrypoint gradex-storage-fixture proof-tool \
      -key test/segment000.ts
  compose up --detach --no-deps --force-recreate api worker
  wait_for_status api healthy
  wait_for_status worker running
  note "staging database and private R2 playback fixture were reset through the guarded proof image"
}

run_monitor() {
  load_environment
  export GRADEX_ENVIRONMENT=staging
  export GRADEX_HEALTH_URL="$PUBLIC_ORIGIN/healthz"
  export GRADEX_READY_URL="$PUBLIC_ORIGIN/readyz"
  export GRADEX_BACKUP_COMPLETED_AT_FILE="$S12_BACKUP_DIR/latest.completed-at"
  "$S12_ROOT/deploy/monitoring/monitor-once.sh"
}

open_db_tunnel() {
  require_tools
  validate_local_targets
  docker network inspect "${S12_PROJECT}_app" "${S12_PROJECT}_edge" >/dev/null 2>&1 ||
    die "start the provider environment before opening the proof tunnel"
  docker rm --force "$S12_DB_TUNNEL_NAME" >/dev/null 2>&1 || true
  docker run --rm --detach --name "$S12_DB_TUNNEL_NAME" --network "${S12_PROJECT}_edge" \
    --publish 127.0.0.1:15432:5432 \
    alpine/socat:1.8.0.3@sha256:beb4a68d9e4fe6b0f21ea774a0fde6c31f580dde6368939ed70100c5385b015e \
    tcp-listen:5432,fork,reuseaddr tcp-connect:postgres:5432 >/dev/null
  docker network connect "${S12_PROJECT}_app" "$S12_DB_TUNNEL_NAME"
  note "temporary proof database tunnel is listening only on host loopback port 15432"
}

close_db_tunnel() {
  require_tools
  validate_local_targets
  docker rm --force "$S12_DB_TUNNEL_NAME" >/dev/null 2>&1 || true
  note "temporary proof database tunnel is closed"
}

create_backup() {
  require_tools
  load_environment
  local postgres_id stamp backup partial schema_file schema_before schema_after backup_artifact backup_lock_fd

  mkdir -p "$S12_BACKUP_DIR"
  chmod 700 "$S12_BACKUP_DIR"

  umask 077
  exec {backup_lock_fd}>"$S12_BACKUP_DIR/.backup.lock"
  flock --nonblock "$backup_lock_fd" ||
    die "another provider backup is already running"

  postgres_id="$(service_id postgres)"
  [ -n "$postgres_id" ] || die "source PostgreSQL is absent"
  stamp="$(date -u +%Y%m%dT%H%M%SZ)"
  backup="$S12_BACKUP_DIR/gradex-$stamp.dump"
  partial="$backup.partial"
  schema_file="$backup.schema-state"

  for backup_artifact in "$backup" "$partial" "$backup.sha256" "$schema_file" "$schema_file.sha256"; do
    [ ! -e "$backup_artifact" ] && [ ! -L "$backup_artifact" ] ||
      die "refusing to reuse provider backup identity $stamp"
  done

  schema_before="$(docker exec "$postgres_id" psql --no-psqlrc --username gradex --dbname "$POSTGRES_DB" \
    --tuples-only --no-align --command "SELECT version::text || '|' || dirty::text FROM schema_migrations;")"
  [[ "$schema_before" =~ ^[0-9]+\|false$ ]] ||
    die "refusing backup from invalid or non-clean schema state: $schema_before"

  umask 077
  docker exec "$postgres_id" pg_dump --format=custom --no-owner --no-acl \
    --username gradex --dbname "$POSTGRES_DB" >"$partial"
  [ -s "$partial" ] || die "backup is empty"

  schema_after="$(docker exec "$postgres_id" psql --no-psqlrc --username gradex --dbname "$POSTGRES_DB" \
    --tuples-only --no-align --command "SELECT version::text || '|' || dirty::text FROM schema_migrations;")"
  [ "$schema_after" = "$schema_before" ] ||
    die "schema changed while backup was being created: $schema_before -> $schema_after"

  mv "$partial" "$backup"
  printf '%s\n' "$schema_before" >"$schema_file"
  sha256sum "$backup" >"$backup.sha256"
  sha256sum "$schema_file" >"$schema_file.sha256"
  date +%s >"$S12_BACKUP_DIR/latest.completed-at"

  chmod 600     "$backup"     "$backup.sha256"     "$schema_file"     "$schema_file.sha256"     "$S12_BACKUP_DIR/latest.completed-at"

  ln -sfn "$(basename "$backup")" "$S12_BACKUP_DIR/latest.dump"
  note "created checksum-protected provider-hosted database backup $stamp at schema $schema_before"
}

restore_backup() {
  require_tools
  load_environment
  local backup schema_file source_id target_id target_volume
  backup="$(readlink -f "$S12_BACKUP_DIR/latest.dump")"
  [ -f "$backup" ] || die "latest backup is absent"
  schema_file="$backup.schema-state"

  [ -f "$schema_file" ] || die "backup schema metadata is absent"
  [ -f "$schema_file.sha256" ] || die "backup schema metadata checksum is absent"

  (cd "$S12_BACKUP_DIR" && sha256sum --check "$(basename "$backup").sha256") >/dev/null
  (cd "$S12_BACKUP_DIR" && sha256sum --check "$(basename "$schema_file").sha256") >/dev/null

  rm -f -- "$S12_BACKUP_DIR/restored-source"

  source_id="$(service_id postgres)"
  [ -n "$source_id" ] || die "source PostgreSQL is absent"
  target_volume="${S12_PROJECT}_restore-data"
  compose --profile restore rm --stop --force api-restore restore-postgres >/dev/null 2>&1 || true
  if docker volume inspect "$target_volume" >/dev/null 2>&1; then
    docker volume rm "$target_volume" >/dev/null
  fi
  compose --profile restore up --detach restore-postgres
  wait_for_status restore-postgres healthy
  target_id="$(service_id restore-postgres)"
  [ -n "$target_id" ] && [ "$source_id" != "$target_id" ] || die "restore target is not isolated from source"
  docker exec --interactive "$target_id" pg_restore --exit-on-error --single-transaction \
    --no-owner --no-acl --username gradex_restore --dbname gradex_restore <"$backup"

  local restored_marker restored_marker_tmp
  restored_marker="$S12_BACKUP_DIR/restored-source"
  restored_marker_tmp="$(mktemp "$S12_BACKUP_DIR/restored-source.XXXXXX")"
  chmod 600 "$restored_marker_tmp"
  printf '%s\n' "$(basename "$backup")" >"$restored_marker_tmp"
  mv -- "$restored_marker_tmp" "$restored_marker"

  note "restored provider backup $(basename "$backup") into a fresh isolated PostgreSQL volume"
}

verify_restore() {
  require_tools
  load_environment
  local backup schema_file restored_marker restored_source expected_schema_state target_id restored_state
  local version dirty accounts courses invitations provenanced enrollments

  restored_marker="$S12_BACKUP_DIR/restored-source"
  [ -f "$restored_marker" ] || die "restored backup identity is absent"
  restored_source="$(cat "$restored_marker")"
  [[ "$restored_source" =~ ^gradex-[0-9]{8}T[0-9]{6}Z\.dump$ ]] ||
    die "restored backup identity is invalid: $restored_source"

  backup="$S12_BACKUP_DIR/$restored_source"
  [ -f "$backup" ] || die "restored source backup is absent: $restored_source"
  schema_file="$backup.schema-state"
  [ -f "$schema_file" ] || die "backup schema metadata is absent"
  [ -f "$schema_file.sha256" ] || die "backup schema metadata checksum is absent"

  (cd "$S12_BACKUP_DIR" && sha256sum --check "$(basename "$backup").sha256") >/dev/null
  (cd "$S12_BACKUP_DIR" && sha256sum --check "$(basename "$schema_file").sha256") >/dev/null

  expected_schema_state="$(cat "$schema_file")"
  [[ "$expected_schema_state" =~ ^[0-9]+\|false$ ]] ||
    die "backup schema metadata is invalid: $expected_schema_state"

  target_id="$(service_id restore-postgres)"
  [ -n "$target_id" ] || die "restore target is absent"
  restored_state="$(docker exec "$target_id" psql --no-psqlrc --username gradex_restore --dbname gradex_restore \
    --tuples-only --no-align --command "
      SELECT
        (SELECT version::text || '|' || dirty::text FROM schema_migrations) || '|' ||
        (SELECT count(*) FROM accounts) || '|' ||
        (SELECT count(*) FROM courses) || '|' ||
        (SELECT count(*) FROM course_access_invitations) || '|' ||
        (SELECT count(*) FROM entitlements WHERE state = 'ACTIVE' AND source_invitation_id IS NOT NULL) || '|' ||
        (SELECT count(*) FROM enrollments);")"

  IFS='|' read -r version dirty accounts courses invitations provenanced enrollments <<<"$restored_state"
  [ "$version|$dirty" = "$expected_schema_state" ] ||
    die "restored schema $version|$dirty does not match backup schema $expected_schema_state"

  for value in "$accounts" "$courses" "$invitations" "$provenanced" "$enrollments"; do
    [ "$value" -gt 0 ] || die "restore is missing an identity/access-critical record class"
  done

  compose --profile restore up --detach api-restore
  wait_for_status api-restore healthy
  note "restored schema $expected_schema_state, identity, Course, invitation provenance, Entitlement, Enrollment, and API readiness passed"
}

manifest_value() {
  local file="$1" key="$2" value count
  count="$(grep --count --extended-regexp "^${key}=" "$file" || true)"
  [ "$count" = 1 ] || die "$file must contain exactly one $key"
  value="$(sed -n "s/^${key}=//p" "$file")"
  [ -n "$value" ] || die "$key is empty"
  case "$value" in *[!A-Za-z0-9._@/:+-]*) die "$key contains unsupported characters" ;; esac
  printf '%s' "$value"
}

persist_release_selection() {
  local release="$1" backend="$2" frontend="$3" proof="$4" temporary
  temporary="$(mktemp "$S12_HOST_STATE_DIR/runtime.env.next.XXXXXX")"
  chmod 600 "$temporary"
  if ! awk -v release="$release" -v backend="$backend" -v frontend="$frontend" -v proof="$proof" '
    BEGIN { release_count = backend_count = frontend_count = proof_count = 0 }
    /^GRADEX_RELEASE_SHA=/ { print "GRADEX_RELEASE_SHA=" release; release_count++; next }
    /^GRADEX_BACKEND_IMAGE=/ { print "GRADEX_BACKEND_IMAGE=" backend; backend_count++; next }
    /^GRADEX_FRONTEND_IMAGE=/ { print "GRADEX_FRONTEND_IMAGE=" frontend; frontend_count++; next }
    /^GRADEX_PROOF_IMAGE=/ { print "GRADEX_PROOF_IMAGE=" proof; proof_count++; next }
    { print }
    END { if (release_count != 1 || backend_count != 1 || frontend_count != 1 || proof_count != 1) exit 42 }
  ' "$S12_ENV_FILE" >"$temporary"; then
    rm -f -- "$temporary"
    die "runtime environment release keys are missing or duplicated"
  fi
  mv -- "$temporary" "$S12_ENV_FILE"
  chmod 600 "$S12_ENV_FILE"
}

apply_release() {
  [ "$#" = 1 ] || die "apply-release requires one release manifest"
  require_tools
  load_environment
  local manifest="$1" release backend frontend proof postgres_id state provenance image
  local schema_version schema_dirty target_max_schema
  [ -f "$manifest" ] || die "release manifest is absent"
  release="$(manifest_value "$manifest" GRADEX_RELEASE_SHA)"
  backend="$(manifest_value "$manifest" GRADEX_BACKEND_IMAGE)"
  frontend="$(manifest_value "$manifest" GRADEX_FRONTEND_IMAGE)"
  proof="$(manifest_value "$manifest" GRADEX_PROOF_IMAGE)"
  [[ "$release" =~ ^[0-9a-f]{40}$ ]] || die "release SHA is invalid"
  [ "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$backend")" = "$release" ] ||
    die "backend image does not match release SHA"
  [ "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$frontend")" = "$release" ] ||
    die "frontend image does not match release SHA"
  [ "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$proof")" = "$release" ] ||
    die "proof image does not match release SHA"
  for image in "$backend" "$frontend" "$proof"; do
    case "$image" in *:latest*) die "application rollback refuses latest image tags" ;; esac
  done
  postgres_id="$(service_id postgres)"
  state="$(docker exec "$postgres_id" psql --no-psqlrc --username gradex --dbname "$POSTGRES_DB" \
    --tuples-only --no-align --command "SELECT version::text || '|' || dirty::text FROM schema_migrations;")"
  IFS='|' read -r schema_version schema_dirty <<<"$state"
  [[ "$schema_version" =~ ^[0-9]+$ ]] || die "schema version is invalid: $state"
  [ "$schema_dirty" = false ] || die "schema is dirty: $state"

  target_max_schema="$(image_max_schema_version "$backend")"
  [ "$schema_version" -le "$target_max_schema" ] ||
    die "schema $schema_version is newer than target release maximum $target_max_schema"
  provenance="$(docker exec "$postgres_id" psql --no-psqlrc --username gradex --dbname "$POSTGRES_DB" \
    --tuples-only --no-align --command "SELECT count(*) FROM entitlements WHERE source_invitation_id IS NOT NULL;")"
  export GRADEX_BACKEND_IMAGE="$backend" GRADEX_FRONTEND_IMAGE="$frontend"
  compose up --detach --no-deps --force-recreate api worker frontend
  wait_for_status api healthy
  wait_for_status worker running
  wait_for_status frontend healthy
  [ "$(docker exec "$postgres_id" psql --no-psqlrc --username gradex --dbname "$POSTGRES_DB" \
    --tuples-only --no-align --command "SELECT count(*) FROM entitlements WHERE source_invitation_id IS NOT NULL;")" = "$provenance" ] ||
    die "Entitlement provenance changed during application release selection"
  persist_release_selection "$release" "$backend" "$frontend" "$proof"
  note "application release $release is healthy on unchanged schema $schema_version (target max $target_max_schema) and provenance"
}

usage() {
  printf 'usage: %s {prepare|up|verify|seed-smoke|monitor|open-db-tunnel|close-db-tunnel|backup|restore|verify-restore|apply-release MANIFEST|status|logs [SERVICE]|stop}\n' "$0" >&2
  exit 2
}

case "${1:-}" in
  prepare) [ "$#" = 1 ] || usage; prepare ;;
  up) [ "$#" = 1 ] || usage; start_environment ;;
  verify) [ "$#" = 1 ] || usage; verify_environment ;;
  seed-smoke) [ "$#" = 1 ] || usage; seed_smoke ;;
  monitor) [ "$#" = 1 ] || usage; run_monitor ;;
  open-db-tunnel) [ "$#" = 1 ] || usage; open_db_tunnel ;;
  close-db-tunnel) [ "$#" = 1 ] || usage; close_db_tunnel ;;
  backup) [ "$#" = 1 ] || usage; create_backup ;;
  restore) [ "$#" = 1 ] || usage; restore_backup ;;
  verify-restore) [ "$#" = 1 ] || usage; verify_restore ;;
  apply-release) shift; apply_release "$@" ;;
  status) load_environment; compose --profile restore ps ;;
  logs) load_environment; if [ -n "${2:-}" ]; then compose logs --no-color "$2"; else compose logs --no-color; fi ;;
  stop) load_environment; compose down ;;
  *) usage ;;
esac
