#!/usr/bin/env bash

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_HOST_DIR="$S12_ROOT/deploy/hostinger"
S12_COMPOSE_FILE="$S12_HOST_DIR/compose.yml"
# Whether the operator named the deployment, captured before the staging
# fallbacks below hide the difference. A production command that inherits
# `gradex-staging` and `/var/lib/gradex` by omission would operate on the wrong
# deployment while reporting success, so production requires both to be said out
# loud. See assert_production_project_scope.
S12_STATE_DIR_DECLARED="${GRADEX_HOST_STATE_DIR+declared}"
S12_PROJECT_DECLARED="${GRADEX_HOST_PROJECT+declared}"

S12_HOST_STATE_DIR="${GRADEX_HOST_STATE_DIR:-/var/lib/gradex}"
S12_ENV_FILE="${GRADEX_HOST_ENV_FILE:-$S12_HOST_STATE_DIR/runtime.env}"
S12_REDIS_TLS_DIR="$S12_HOST_STATE_DIR/redis-tls"
S12_BACKUP_DIR="$S12_HOST_STATE_DIR/backups"
S12_PROJECT_STAGING_DEFAULT="gradex-staging"
S12_PROJECT="${GRADEX_HOST_PROJECT:-$S12_PROJECT_STAGING_DEFAULT}"
S12_DB_TUNNEL_NAME="${S12_PROJECT}-proof-db-tunnel"
S12_BACKUP_STAGING_DIR=""
S12_RESTORED_SOURCE_FILE="$S12_BACKUP_DIR/restored-source"
S12_RESTORED_SCHEMA_STATE_FILE="$S12_BACKUP_DIR/restored-schema-state"
S12_RESTORED_RECORD_COUNTS_FILE="$S12_BACKUP_DIR/restored-record-counts"
S12_RESTORE_STAGING_DIR=""
# The restore verification database is deliberately not a Compose service. A
# deployment whose live topology is driven by its own Compose file (Founder Beta
# is) has no `restore-postgres` service to bring up, and reconciling this
# repository's model against that project would inject verification services into
# the running application. The restore target is therefore a standalone container
# with its own identity and volume, owned by nothing but this verification.
S12_RESTORE_VERIFY_CONTAINER="gradex-restore-verify"
S12_RESTORE_VERIFY_VOLUME="gradex-restore-verify-data"
S12_RESTORE_POSTGRES_IMAGE="${GRADEX_RESTORE_POSTGRES_IMAGE:-postgres:16.14-alpine}"

cleanup_backup_staging_notice() {
  local exit_status=$?
  if [ "$exit_status" -ne 0 ] && [ -n "$S12_BACKUP_STAGING_DIR" ]; then
    note "encrypted offsite backup failed; protected staging retained at $S12_BACKUP_STAGING_DIR"
  fi
  if [ "$exit_status" -ne 0 ] && [ -n "$S12_RESTORE_STAGING_DIR" ]; then
    note "restore verification failed; protected extracted staging retained at $S12_RESTORE_STAGING_DIR"
  fi
}

note() { printf 's12-hostinger: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }
trap cleanup_backup_staging_notice EXIT

# shellcheck source=backup-restic.sh
. "$S12_HOST_DIR/backup-restic.sh"

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

# The deployment declares its own environment and its coupled admission
# composition. Nothing here is inferred from the hostname: a managed host is
# staging or production because its protected runtime says so.
#
# THE RULE THIS MIRRORS
#   backend/cmd/api/main.go composes the staff lifecycle for every environment
#   that has sessions and is development, staging, OR production, and
#   validateStaffComposition there exempts development ALONE. Every other
#   environment must satisfy, at startup:
#
#       real sessions · AUTH_FAKE_MODE=false · PostgreSQL · Redis
#       PasswordScreenMode == adapter · email enabled · EMAIL_PROVIDER=resend
#
#   A managed host is never development, so staging is bound by exactly the same
#   composition contract as production. The one genuine difference lives in
#   backend/internal/config/config.go, where the LG-021 approval flag is required
#   only when the environment IsProduction(); staging runs the same real HIBP
#   adapter with the flag unset, because NewRuntimeCompromisedSource switches on
#   the mode alone and never reads the approval.
#
# THE REGRESSION THIS GUARDS
#   The first production bring-up got as far as a clean schema-28 migration
#   before the API refused to build: "compromised-password screening is not
#   configured". The Compose file had hard-coded PASSWORD_SCREEN_MODE=unavailable
#   because screening was read as a student-registration concern. It is not —
#   staff invitation and onboarding set passwords, so the adapter is required
#   while public registration stays closed.
#
#   The first repair fixed production and left staging asserting the same broken
#   values, which merely moved the trap. A composition the application will
#   reject must be rejected here, before anything starts, in EITHER environment.
validate_application_composition() {
  case "$APP_ENV" in
    staging | production) ;;
    development) die "development is not a managed-host environment; APP_ENV must be staging or production" ;;
    *) die "APP_ENV must be exactly staging or production; got \"$APP_ENV\"" ;;
  esac

  case "$PASSWORD_SCREEN_MODE" in
    adapter) ;;
    deterministic) die "deterministic PASSWORD_SCREEN_MODE is permitted only in development" ;;
    unavailable)
      die "PASSWORD_SCREEN_MODE=adapter is required on every managed host; staff invitation and onboarding set passwords, so the API composes real compromised-password screening in staging as well as production"
      ;;
    *) die "PASSWORD_SCREEN_MODE must be adapter on a managed host; got \"$PASSWORD_SCREEN_MODE\"" ;;
  esac

  case "$COMPROMISED_PASSWORD_ADAPTER_APPROVED" in
    true | false) ;;
    *) die "COMPROMISED_PASSWORD_ADAPTER_APPROVED must be exactly true or false" ;;
  esac

  # Real sessions. Sessions().Enabled() is exactly a non-empty SESSION_CSRF_KEY,
  # and the staff surface cannot be composed without them.
  require_value SESSION_CSRF_KEY

  [ "${AUTH_FAKE_MODE:-false}" = false ] ||
    die "AUTH_FAKE_MODE must never be enabled on a managed host"
  case "${STUDENT_REGISTRATION_ENABLED:-false}" in
    false) ;;
    true)
      require_value REGISTRATION_POLICY_SET_ID
      case "${REGISTRATION_POLICY_APPROVED:-false}" in
        true | false) ;;
        *) die "REGISTRATION_POLICY_APPROVED must be exactly true or false" ;;
      esac
      if [ "$APP_ENV" = production ]; then
        [ "${REGISTRATION_POLICY_APPROVED:-false}" = true ] ||
          die "production student registration requires REGISTRATION_POLICY_APPROVED=true (LG-011)"
      fi
      ;;
    *) die "STUDENT_REGISTRATION_ENABLED must be exactly true or false" ;;
  esac

  # Transactional email is a staff-composition dependency, not a production
  # nicety: invitation delivery is how an Admin account ever comes to exist.
  [ "$EMAIL_ENABLED" = true ] ||
    die "EMAIL_ENABLED=true is required on every managed host; staff invitation depends on transactional email"
  [ "$EMAIL_PROVIDER" = resend ] ||
    die "EMAIL_PROVIDER=resend is required on every managed host; got \"$EMAIL_PROVIDER\""
  # config.go rejects an empty key for the Resend provider regardless of
  # environment, so preflight must too.
  require_value EMAIL_API_KEY
  require_value EMAIL_FROM_ADDRESS

  if [ "$APP_ENV" = production ]; then
    # The single production-only rule, and the only place the approval flag is
    # consulted: config.go gates LG-021 on environment.IsProduction().
    [ "$COMPROMISED_PASSWORD_ADAPTER_APPROVED" = true ] ||
      die "production PASSWORD_SCREEN_MODE=adapter requires COMPROMISED_PASSWORD_ADAPTER_APPROVED=true (LG-021)"
  fi

  note "$APP_ENV composition accepted: real sessions, adapter screening, Resend transactional email, no fake authentication, registration policy validated"
}

# A production deployment must name itself. Both selectors fall back to the
# staging values, so an operator who exports neither would drive the staging
# project from the staging state directory while believing they were in
# production — and on a shared VPS that is the same host, so nothing external
# would contradict them. Requiring the declaration is the fail-closed form of
# "remember to set it".
assert_production_project_scope() {
  [ "${APP_ENV:-}" = production ] || return 0
  [ -n "$S12_PROJECT_DECLARED" ] ||
    die "a production deployment must declare GRADEX_HOST_PROJECT; it must never inherit the staging default \"$S12_PROJECT_STAGING_DEFAULT\""
  [ -n "$S12_STATE_DIR_DECLARED" ] ||
    die "a production deployment must declare GRADEX_HOST_STATE_DIR; it must never inherit the staging default"
  [ "$S12_PROJECT" != "$S12_PROJECT_STAGING_DEFAULT" ] ||
    die "a production deployment may not use the staging Compose project \"$S12_PROJECT_STAGING_DEFAULT\""
  note "production deployment scoped to project $S12_PROJECT"
}

validate_environment() {
  local name image_revision
  validate_local_targets
  for name in GRADEX_RELEASE_SHA GRADEX_BACKEND_IMAGE GRADEX_FRONTEND_IMAGE GRADEX_PROOF_IMAGE \
    STAGING_HOSTNAME ACME_EMAIL PUBLIC_ORIGIN POSTGRES_DB POSTGRES_PASSWORD DATABASE_URL \
    GRADEX_E2E_ADMIN_DB_URL RESTORE_POSTGRES_PASSWORD RESTORE_DATABASE_URL REDIS_PASSWORD S3_ENDPOINT S3_BUCKET \
	S3_ACCESS_KEY S3_SECRET_KEY PLAYBACK_TOKEN_SECRET SALES_WHATSAPP_NUMBER SESSION_CSRF_KEY \
    ANONYMOUS_COOKIE_SIGNING_KEY ANONYMOUS_CSRF_KEY ADMISSION_LIMITER_HMAC_KEY \
    IDENTITY_OTP_PEPPER \
    OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION OUTBOX_PROTECTED_PAYLOAD_KEY \
    APP_ENV PASSWORD_SCREEN_MODE COMPROMISED_PASSWORD_ADAPTER_APPROVED \
    EMAIL_ENABLED EMAIL_PROVIDER; do
    require_value "$name"
  done
  validate_application_composition
  assert_production_project_scope
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
  backup_validate_configuration || die "encrypted offsite backup configuration is invalid"
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

backup_postgres_id() {
  local configured="${GRADEX_BACKUP_POSTGRES_CONTAINER:-}" container_id
  if [ -z "$configured" ]; then
    service_id postgres
    return
  fi
  [[ "$configured" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$ ]] ||
    die "GRADEX_BACKUP_POSTGRES_CONTAINER is invalid"
  container_id="$(docker inspect --type container --format '{{.Id}}' "$configured" 2>/dev/null)" ||
    die "configured backup PostgreSQL container is absent"
  [ "$(docker inspect --format '{{.State.Running}}' "$container_id")" = true ] ||
    die "configured backup PostgreSQL container is not running"
  printf '%s\n' "$container_id"
}

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

# The application tier, without the public edge.
#
# The edge is the one service that binds host ports, and on this VPS the staging
# edge already owns 80, 443, and 443/udp. A first production cutover therefore
# cannot start the whole stack at once: the production edge would collide, and
# `up` would fail after the database was already migrated. Separating the tier
# from the edge lets production be brought up and proven privately, while
# staging keeps serving, and leaves the port handover as one deliberate step.
#
# `--no-deps` on the application services keeps Compose from reconciling the
# edge through the dependency graph; migrate has already run as a one-shot, so
# the api dependency it satisfies is met.
start_core() {
  prepare
  compose up --detach postgres redis
  wait_for_status postgres healthy
  wait_for_status redis healthy
  compose up --detach migrate
  wait_for_completion migrate
  compose up --detach --no-deps api worker frontend
  wait_for_status api healthy
  wait_for_status worker running
  wait_for_status frontend healthy
  note "application tier is running privately; the public edge has not been started"
}

require_status() {
  local service="$1" wanted="$2" container status
  container="$(service_id "$service")"
  [ -n "$container" ] || die "$service is not running; start the application tier with up-core first"
  status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container")"
  [ "$status" = "$wanted" ] || die "$service is $status, expected $wanted; the edge may not be started over an unproven application tier"
}

# Read-only. The repository must never stop, remove, or reconcile a container it
# does not own, so a conflicting publisher is reported by name and the operator
# releases it themselves.
assert_edge_ports_available() {
  local conflicts
  conflicts="$(
    docker ps --format '{{.ID}}\t{{.Names}}\t{{.Label "com.docker.compose.project"}}\t{{.Ports}}' |
      awk -F '\t' -v project="$S12_PROJECT" '
        $3 != project && $4 ~ /(^|,)[^,]*:(80|443)->/ { print $2 " (project " ($3 == "" ? "none" : $3) ")" }
      '
  )"
  [ -n "$conflicts" ] || return 0
  note "another deployment already publishes the public ports:"
  printf '%s\n' "$conflicts" >&2
  die "release 80/tcp, 443/tcp and 443/udp from the container(s) above before starting this edge; this command will not stop another project's containers"
}

# The cutover boundary. Deliberately separate from up-core so that binding the
# public ports is an act, not a side effect.
start_edge() {
  require_tools
  load_environment
  validate_environment
  require_status api healthy
  require_status frontend healthy
  assert_edge_ports_available
  compose up --detach --no-deps edge
  wait_for_status edge running
  note "public edge is running and now owns 80/tcp, 443/tcp and 443/udp"
}

start_environment() {
  start_core
  start_edge
}

# The one-off Administrator bootstrap.
#
# Deliberately not part of up/up-core: creating the platform's only
# Administrator is a human act, and cmd/bootstrap-admin already refuses to be
# anything else — it has no HTTP endpoint, no worker task, and no migration.
#
# It needs PostgreSQL, the protected configuration, and outbound HTTPS for the
# HIBP screening lookup. It needs no Redis, sends no email, and does not touch
# the edge, so it belongs after verify-core and before the public cutover.
#
# Inputs arrive through the environment so nothing lands in this script's own
# arguments. The non-secret ones become flags on the one-shot; the passphrase is
# forwarded by name only, so its value never reaches the Compose model, a
# rendered config, or any container that outlives the command.
bootstrap_admin() {
  require_tools
  load_environment
  validate_environment

  local name
  for name in BOOTSTRAP_ADMIN_EMAIL BOOTSTRAP_ADMIN_OPERATION_ID BOOTSTRAP_ADMIN_PRINCIPAL; do
    [ -n "${!name:-}" ] || die "$name is required in the operator environment"
  done
  [ -n "${BOOTSTRAP_ADMIN_PASSWORD:-}" ] ||
    die "BOOTSTRAP_ADMIN_PASSWORD is required in the operator environment; the initial passphrase is read only through the secret boundary, never a flag"
  local display_name="${BOOTSTRAP_ADMIN_DISPLAY_NAME:-Platform Administrator}"

  # The bootstrap writes to the live database, so the deployment must already be
  # the verified one rather than a half-started stack.
  require_status postgres healthy
  require_status api healthy

  # cmd/bootstrap-admin requires the acknowledgement when APP_ENV=production and
  # refuses it otherwise, so the flag follows the declared environment exactly.
  local confirmation=()
  [ "$APP_ENV" = production ] && confirmation=(-confirm-production)

  note "creating the bootstrap Administrator in project $S12_PROJECT ($APP_ENV)"
  compose --profile bootstrap run --rm --no-deps \
    --env BOOTSTRAP_ADMIN_PASSWORD \
    bootstrap-admin \
    -email "$BOOTSTRAP_ADMIN_EMAIL" \
    -display-name "$display_name" \
    -operation-id "$BOOTSTRAP_ADMIN_OPERATION_ID" \
    -principal "$BOOTSTRAP_ADMIN_PRINCIPAL" \
    "${confirmation[@]}" ||
    die "the Administrator bootstrap failed; no second Administrator was created"
  note "bootstrap Administrator command completed; the credential is CHANGE_REQUIRED and must be changed at first sign-in"
}

# Everything provable before the hostname resolves and before a certificate
# exists. It reaches the API over the private Compose network by executing the
# container's own health probe, so it never depends on PUBLIC_ORIGIN, public
# DNS, or TLS. verify_environment remains the public, post-edge check and is not
# replaced by this.
verify_core() {
  require_tools
  load_environment
  validate_environment
  local service postgres_id redis_id api_id schema_state expected_schema
  local unauthenticated authenticated readiness

  for service in postgres redis api worker frontend; do
    [ -n "$(service_id "$service")" ] || die "$service is absent from project $S12_PROJECT"
  done
  require_status postgres healthy
  require_status redis healthy
  require_status api healthy
  require_status frontend healthy
  require_status worker running

  postgres_id="$(service_id postgres)"
  schema_state="$(docker exec "$postgres_id" psql --no-psqlrc --username gradex --dbname "$POSTGRES_DB" \
    --tuples-only --no-align --command "SELECT version::text || '|' || dirty::text FROM schema_migrations;")"
  expected_schema="$(image_max_schema_version "$GRADEX_BACKEND_IMAGE")"
  [ "$schema_state" = "$expected_schema|false" ] ||
    die "schema is $schema_state, expected clean version $expected_schema for the selected backend image"

  redis_id="$(service_id redis)"
  if timeout 5 docker exec "$redis_id" redis-cli -h redis -p 6379 ping >/dev/null 2>&1; then
    die "Redis accepted plaintext"
  fi
  unauthenticated="$(docker exec --env REDISCLI_AUTH= "$redis_id" redis-cli --tls \
    --cacert /run/gradex/redis/ca.crt -h redis -p 6379 ping 2>&1 || true)"
  case "$unauthenticated" in *NOAUTH*) ;; *) die "Redis did not reject unauthenticated TLS" ;; esac
  authenticated="$(docker exec "$redis_id" redis-cli --tls --cacert /run/gradex/redis/ca.crt \
    -h redis -p 6379 ping 2>&1)"
  [ "$authenticated" = PONG ] || die "authenticated Redis TLS failed"

  # Readiness over the private loopback inside the API container. No public
  # origin, no DNS, no certificate.
  api_id="$(service_id api)"
  readiness="$(docker exec "$api_id" wget -qO- http://127.0.0.1:8080/readyz)" ||
    die "the API did not answer its own readiness probe"
  printf '%s' "$readiness" |
    jq --exit-status '.status == "ok" and .checks.postgres == "ok" and .checks.redis == "ok" and .checks.schema == "ok"' >/dev/null ||
    die "API readiness did not report every dependency ok"

  note "private verification passed: clean schema $expected_schema, application services healthy, authenticated verified-TLS Redis, and API readiness over the private network"
  note "public DNS, certificate, and edge behaviour are NOT covered here; run verify and verify-public.sh after the edge is started"
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
  printf '#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXTINF:1.0,\nsegment000.ts\n#EXT-X-ENDLIST\n' |
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

monitor_append_report() {
  local report="$1" check="$2" status="$3" detail="$4"
  printf '%s|%s|%s\n' "$check" "$status" "$detail" >>"$report"
}

MONITOR_CONTAINER_ID=""
MONITOR_CONTAINER_ERROR=""
MONITOR_BACKEND_IMAGE=""

monitor_resolve_container() {
  local service="$1" override_name="$2" configured expected_project candidate identity
  local actual_project actual_service actual_state extra
  MONITOR_CONTAINER_ID=""
  MONITOR_CONTAINER_ERROR=""
  if ! command -v docker >/dev/null 2>&1; then
    MONITOR_CONTAINER_ERROR="Docker runtime is unavailable"
    return 1
  fi
  if ! docker info >/dev/null 2>&1; then
    MONITOR_CONTAINER_ERROR="Docker runtime is unreachable"
    return 1
  fi
  expected_project="${GRADEX_MONITOR_COMPOSE_PROJECT:-$S12_PROJECT}"
  if ! [[ "$expected_project" =~ ^[a-z0-9][a-z0-9_-]{2,62}$ ]]; then
    MONITOR_CONTAINER_ERROR="monitor Compose project is invalid"
    return 1
  fi
  configured="${!override_name:-}"
  if [ -n "$configured" ]; then
    if ! [[ "$configured" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$ ]]; then
      MONITOR_CONTAINER_ERROR="configured monitor $service container name is invalid"
      return 1
    fi
    candidate="$(docker inspect --type container --format '{{.Id}}' "$configured" 2>/dev/null || true)"
  else
    candidate="$(service_id "$service" 2>/dev/null || true)"
  fi
  if ! [[ "$candidate" =~ ^[[:alnum:]]+$ ]]; then
    if [ -n "$configured" ]; then
      MONITOR_CONTAINER_ERROR="configured monitor $service container is absent"
    else
      MONITOR_CONTAINER_ERROR="owned Compose $service container is absent"
    fi
    return 1
  fi
  identity="$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}|{{index .Config.Labels "com.docker.compose.service"}}|{{.State.Status}}' "$candidate" 2>/dev/null || true)"
  IFS='|' read -r actual_project actual_service actual_state extra <<<"$identity"
  if [ -n "${extra:-}" ] || [ "$actual_project" != "$expected_project" ] || [ "$actual_service" != "$service" ]; then
    if [ -n "$configured" ]; then
      MONITOR_CONTAINER_ERROR="configured monitor $service labels do not match the configured project/service"
    else
      MONITOR_CONTAINER_ERROR="owned Compose labels do not match the configured project"
    fi
    return 1
  fi
  if [ "$actual_state" != running ]; then
    if [ -n "$configured" ]; then
      MONITOR_CONTAINER_ERROR="configured monitor $service state=${actual_state:-unavailable}"
    else
      MONITOR_CONTAINER_ERROR="owned Compose $service state=${actual_state:-unavailable}"
    fi
    return 1
  fi
  MONITOR_CONTAINER_ID="$candidate"
}

monitor_probe_worker() {
  local report="$1"
  if monitor_resolve_container worker GRADEX_MONITOR_WORKER_CONTAINER; then
    monitor_append_report "$report" worker PASS "intended Compose worker is running"
  else
    monitor_append_report "$report" worker FAIL "$MONITOR_CONTAINER_ERROR"
  fi
}

monitor_expected_schema_version() {
  local image="$1" version
  version="$(timeout 30 docker run --rm --entrypoint gradex-migrate "$image" max-version 2>/dev/null)" || return 1
  [[ "$version" =~ ^[0-9]+$ ]] || return 1
  printf '%s' "$version"
}

monitor_resolve_backend_image() {
  MONITOR_BACKEND_IMAGE="${GRADEX_BACKEND_IMAGE:-}"
  [ -n "$MONITOR_BACKEND_IMAGE" ] && return
  if ! monitor_resolve_container api GRADEX_MONITOR_API_CONTAINER; then
    return 1
  fi
  MONITOR_BACKEND_IMAGE="$(docker inspect --format '{{.Config.Image}}' "$MONITOR_CONTAINER_ID" 2>/dev/null || true)"
  if [ -z "$MONITOR_BACKEND_IMAGE" ]; then
    MONITOR_CONTAINER_ERROR="selected backend image is unavailable"
    return 1
  fi
}

monitor_probe_postgres_schema() {
  local report="$1" postgres_id schema_state expected_schema
  if ! monitor_resolve_container postgres GRADEX_MONITOR_POSTGRES_CONTAINER; then
    monitor_append_report "$report" postgres_schema FAIL "$MONITOR_CONTAINER_ERROR"
    return 0
  fi
  postgres_id="$MONITOR_CONTAINER_ID"
  if [ -z "${POSTGRES_DB:-}" ]; then
    monitor_append_report "$report" postgres_schema FAIL "PostgreSQL database name is not configured"
    return 0
  fi
  if ! monitor_resolve_backend_image; then
    monitor_append_report "$report" postgres_schema FAIL "$MONITOR_CONTAINER_ERROR"
    return 0
  fi
  if ! schema_state="$(timeout 15 docker exec "$postgres_id" psql --no-psqlrc --username gradex --dbname "$POSTGRES_DB" \
    --tuples-only --no-align --command "SELECT version::text || '|' || dirty::text FROM schema_migrations;" 2>/dev/null)"; then
    monitor_append_report "$report" postgres_schema FAIL "PostgreSQL schema query failed"
    return 0
  fi
  if ! [[ "$schema_state" =~ ^[0-9]+\|(true|false)$ ]]; then
    monitor_append_report "$report" postgres_schema FAIL "PostgreSQL returned malformed schema state"
    return 0
  fi
  if ! expected_schema="$(monitor_expected_schema_version "$MONITOR_BACKEND_IMAGE")"; then
    monitor_append_report "$report" postgres_schema FAIL "selected backend schema version is unavailable"
    return 0
  fi
  if [ "$schema_state" != "$expected_schema|false" ]; then
    monitor_append_report "$report" postgres_schema FAIL "PostgreSQL schema does not match the selected backend image"
    return 0
  fi
  monitor_append_report "$report" postgres_schema PASS "intended Compose PostgreSQL schema is clean"
}

monitor_email_health_query() {
  local postgres_id="$1" database_name="$2"
  local query="WITH terminal AS (
  SELECT 1
    FROM transactional_email_deliveries
   WHERE status IN ('PERMANENT_FAILED', 'EXHAUSTED')
   LIMIT 1
), due AS (
  SELECT floor(EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - next_attempt_at)))::bigint AS age
    FROM transactional_email_deliveries
   WHERE status = 'QUEUED' AND next_attempt_at <= CURRENT_TIMESTAMP
   ORDER BY next_attempt_at, event_id
   LIMIT 1
), stale AS (
  SELECT floor(EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - lease_expires_at)))::bigint AS age
    FROM transactional_email_deliveries
   WHERE status = 'SENDING' AND lease_expires_at <= CURRENT_TIMESTAMP
   ORDER BY lease_expires_at, event_id
   LIMIT 1
)
SELECT CASE WHEN EXISTS (SELECT 1 FROM terminal) THEN '1' ELSE '0' END || '|' ||
       COALESCE((SELECT age::text FROM due), '-1') || '|' ||
       COALESCE((SELECT age::text FROM stale), '-1');"
  timeout 15 docker exec "$postgres_id" psql --no-psqlrc --username gradex --dbname "$database_name" \
    --tuples-only --no-align --command "$query" 2>/dev/null
}

monitor_record_email_metrics() {
  local report="$1" metrics="$2" terminal_count oldest_due_age stale_lease_age
  metrics="${metrics//$'\n'/}"
  metrics="${metrics//$'\r'/}"
  if ! [[ "$metrics" =~ ^([0-9]+)\|(-?[0-9]+)\|(-?[0-9]+)$ ]]; then
    monitor_append_report "$report" email_outbox FAIL "PostgreSQL returned invalid transactional email metrics"
    return 0
  fi
  terminal_count="${BASH_REMATCH[1]}"
  oldest_due_age="${BASH_REMATCH[2]}"
  stale_lease_age="${BASH_REMATCH[3]}"
  monitor_append_report "$report" email_outbox METRICS \
    "terminal=$terminal_count;oldest_due_age=$oldest_due_age;stale_lease_age=$stale_lease_age"
}

monitor_probe_email_outbox() {
  local report="$1" metrics
  if [ -z "${POSTGRES_DB:-}" ]; then
    monitor_append_report "$report" email_outbox FAIL "PostgreSQL database name is not configured"
    return 0
  fi
  if ! monitor_resolve_container postgres GRADEX_MONITOR_POSTGRES_CONTAINER; then
    monitor_append_report "$report" email_outbox FAIL "$MONITOR_CONTAINER_ERROR"
    return 0
  fi
  if ! metrics="$(monitor_email_health_query "$MONITOR_CONTAINER_ID" "$POSTGRES_DB")"; then
    monitor_append_report "$report" email_outbox FAIL "PostgreSQL transactional email query failed"
    return 0
  fi
  monitor_record_email_metrics "$report" "$metrics"
}

monitor_probe_disk_paths() {
  local report="$1" docker_root
  if [ -n "${GRADEX_MONITOR_DISK_PATHS:-}" ]; then
    monitor_append_report "$report" disk_roots PASS "explicit monitored paths configured"
    return 0
  fi
  if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
    export GRADEX_MONITOR_DISK_PATHS="$S12_HOST_STATE_DIR"
    monitor_append_report "$report" disk_roots FAIL "Docker data-root could not be resolved"
    return 0
  fi
  docker_root="$(docker info --format '{{.DockerRootDir}}' 2>/dev/null || true)"
  case "$docker_root" in
    /*) ;;
    *)
      export GRADEX_MONITOR_DISK_PATHS="$S12_HOST_STATE_DIR"
      monitor_append_report "$report" disk_roots FAIL "Docker data-root is invalid"
      return 0
      ;;
  esac
  case "$docker_root" in *'|'*|*$'\r'*|*$'\n'*)
    export GRADEX_MONITOR_DISK_PATHS="$S12_HOST_STATE_DIR"
    monitor_append_report "$report" disk_roots FAIL "Docker data-root contains unsafe characters"
    return 0
    ;;
  esac
  export GRADEX_MONITOR_DISK_PATHS="$S12_HOST_STATE_DIR:$docker_root"
  monitor_append_report "$report" disk_roots PASS "derived host state and Docker data-root"
}

collect_monitor_runtime_report() {
  local report="$1"
  umask 077
  printf 'version=1\n' >"$report"
  monitor_probe_worker "$report"
  monitor_probe_postgres_schema "$report"
  monitor_probe_email_outbox "$report"
  monitor_probe_disk_paths "$report"
}

run_monitor() {
  load_environment
  case "$APP_ENV" in
    staging|production) ;;
    *) die "APP_ENV must be exactly staging or production for monitoring; got \"$APP_ENV\"" ;;
  esac
  export GRADEX_ENVIRONMENT="$APP_ENV"
  export GRADEX_PUBLIC_URL="$PUBLIC_ORIGIN/"
  export GRADEX_HEALTH_URL="$PUBLIC_ORIGIN/healthz"
  export GRADEX_READY_URL="$PUBLIC_ORIGIN/readyz"
  export GRADEX_BACKUP_COMPLETED_AT_FILE="$S12_BACKUP_DIR/latest.completed-at"
  local runtime_report monitor_status
  runtime_report="$(mktemp)"
  chmod 600 "$runtime_report"
  collect_monitor_runtime_report "$runtime_report"
  export GRADEX_MONITOR_RUNTIME_REPORT="$runtime_report"
  if "$S12_ROOT/deploy/monitoring/monitor-once.sh"; then
    monitor_status=0
  else
    monitor_status=$?
  fi
  rm -f -- "$runtime_report"
  unset GRADEX_MONITOR_RUNTIME_REPORT
  return "$monitor_status"
}

run_monitor_alert_test() {
  local monitor_status
  export GRADEX_MONITOR_SYNTHETIC_ALERT_TEST=1
  if run_monitor; then
    monitor_status=0
  else
    monitor_status=$?
  fi
  unset GRADEX_MONITOR_SYNTHETIC_ALERT_TEST
  return "$monitor_status"
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

acquire_backup_lock() {
  local lock_fd
  umask 077
  exec {lock_fd}> "$S12_BACKUP_DIR/.backup.lock"
  flock --nonblock "$lock_fd" || die "another provider backup or restore is already running"
}

validate_snapshot_id() {
  local snapshot_id="$1"
  [[ "$snapshot_id" =~ ^[0-9a-f]{64}$ ]] || die "snapshot ID is invalid"
}

restore_offsite_snapshot_files() {
  local snapshot_id="$1" staging_dir="$2"
  local listing_log dump_path schema_path dump_checksum_path schema_checksum_path
  listing_log="$staging_dir/restic-list.jsonl"
  backup_restic ls --json "$snapshot_id" >"$listing_log"
  dump_path="$(backup_snapshot_file_path "$listing_log" .dump)"
  schema_path="$(backup_snapshot_file_path "$listing_log" .schema-state)"
  dump_checksum_path="$(backup_snapshot_file_path "$listing_log" .dump.sha256)"
  schema_checksum_path="$(backup_snapshot_file_path "$listing_log" .schema-state.sha256)"
  backup_extract_snapshot_file "$snapshot_id" "$dump_path" "$staging_dir/${dump_path##*/}"
  backup_extract_snapshot_file "$snapshot_id" "$schema_path" "$staging_dir/${schema_path##*/}"
  backup_extract_snapshot_file "$snapshot_id" "$dump_checksum_path" "$staging_dir/${dump_checksum_path##*/}"
  backup_extract_snapshot_file "$snapshot_id" "$schema_checksum_path" "$staging_dir/${schema_checksum_path##*/}"
  (cd "$staging_dir" && sha256sum --check "${dump_path##*/}.sha256" "${schema_path##*/}.sha256") >/dev/null
  printf '%s\n' "${dump_path##*/}" >"$staging_dir/restored-dump-name"
  printf '%s\n' "${schema_path##*/}" >"$staging_dir/restored-schema-name"
}

cleanup_successful_backup_staging() {
  local staging_dir="$1"
  [ -n "$staging_dir" ] || die "backup staging directory is empty"
  case "$staging_dir" in
    "$S12_BACKUP_DIR"/.offsite-staging.*) ;;
    *) die "refusing to remove an unexpected backup staging path" ;;
  esac
  rm -rf -- "$staging_dir"
  S12_BACKUP_STAGING_DIR=""
}
cleanup_stale_backup_staging() {
  local candidate
  for candidate in "$S12_BACKUP_DIR"/.offsite-staging.*; do
    [ -d "$candidate" ] || continue
    case "$candidate" in
      "$S12_BACKUP_DIR"/.offsite-staging.*) rm -rf -- "$candidate" ;;
      *) die "refusing to remove an unexpected backup staging path" ;;
    esac
  done
}

cleanup_stale_restore_staging() {
  local candidate
  for candidate in "$S12_BACKUP_DIR"/.restore-staging.*; do
    [ -d "$candidate" ] || continue
    case "$candidate" in
      "$S12_BACKUP_DIR"/.restore-staging.*) rm -rf -- "$candidate" ;;
      *) die "refusing to remove an unexpected restore staging path" ;;
    esac
  done
}
source_schema_state() {
  local postgres_id="$1"
  docker exec "$postgres_id" psql --no-psqlrc --username gradex --dbname "$POSTGRES_DB" \
    --tuples-only --no-align --command "SELECT version::text || '|' || dirty::text FROM schema_migrations;"
}

# The record counts the source held at capture time, in the same order
# `restored_database_state` reports them. Recording them turns the restore check
# into an exact comparison against the source instead of a guess about which
# record classes a live deployment happens to be populating: a legitimately empty
# class stays legitimate, while a truncated or partial restore still fails.
source_record_counts() {
  local postgres_id="$1"
  docker exec "$postgres_id" psql --no-psqlrc --username gradex --dbname "$POSTGRES_DB" \
    --tuples-only --no-align --command "
      SELECT
        (SELECT count(*) FROM accounts) || '|' ||
        (SELECT count(*) FROM courses) || '|' ||
        (SELECT count(*) FROM course_access_invitations) || '|' ||
        (SELECT count(*) FROM entitlements WHERE state = 'ACTIVE' AND source_invitation_id IS NOT NULL) || '|' ||
        (SELECT count(*) FROM enrollments);"
}

write_backup_metadata() {
  local staging_dir="$1" dump_file="$2" schema_file="$3"
  (cd "$staging_dir" && sha256sum "$(basename "$dump_file")" >"$(basename "$dump_file").sha256")
  (cd "$staging_dir" && sha256sum "$(basename "$schema_file")" >"$(basename "$schema_file").sha256")
  chmod 600 "$dump_file" "$dump_file.sha256" "$schema_file" "$schema_file.sha256"
}

capture_backup_dump() {
  local postgres_id="$1" staging_dir="$2" stamp="$3"
  local dump_file partial_file schema_file schema_before schema_after record_counts
  dump_file="$staging_dir/gradex-$stamp.dump"
  partial_file="$dump_file.partial"
  schema_file="$dump_file.schema-state"
  schema_before="$(source_schema_state "$postgres_id")"
  [[ "$schema_before" =~ ^[0-9]+\|false$ ]] ||
    die "refusing backup from invalid or non-clean schema state: $schema_before"
  docker exec "$postgres_id" pg_dump --format=custom --no-owner --no-acl \
    --username gradex --dbname "$POSTGRES_DB" >"$partial_file"
  [ -s "$partial_file" ] || die "backup is empty"
  schema_after="$(source_schema_state "$postgres_id")"
  [ "$schema_after" = "$schema_before" ] ||
    die "schema changed while backup was being created: $schema_before -> $schema_after"
  record_counts="$(source_record_counts "$postgres_id")"
  [[ "$record_counts" =~ ^[0-9]+(\|[0-9]+){4}$ ]] ||
    die "refusing backup with unreadable source record counts: $record_counts"
  mv -- "$partial_file" "$dump_file"
  # Line 1 stays exactly the schema state older snapshots carry, so a snapshot
  # written before record counts existed still restores and still validates its
  # schema. Line 2 is additive.
  printf '%s\ncounts=%s\n' "$schema_before" "$record_counts" >"$schema_file"
  write_backup_metadata "$staging_dir" "$dump_file" "$schema_file"
}

upload_backup_snapshot() {
  local staging_dir="$1" snapshot_log="$2" snapshot_check_log="$3" snapshot_id
  snapshot_id="$(backup_snapshot_directory "$staging_dir" "$snapshot_log")"
  validate_snapshot_id "$snapshot_id"
  backup_snapshot_exists "$snapshot_id" "$snapshot_check_log" ||
    die "encrypted offsite snapshot was not visible after upload"
  backup_check_repository >&2 || die "encrypted offsite repository integrity check failed"
  printf '%s\n' "$snapshot_id"
}

apply_backup_retention() {
  local snapshot_id="$1" snapshot_check_log="$2" retention_status=0
  if backup_prune_repository; then
    note "encrypted offsite backup $snapshot_id created and retention applied"
  else
    retention_status=$?
    note "encrypted offsite backup $snapshot_id created but retention failed"
  fi
  backup_assert_repository_has_snapshot "$snapshot_check_log" ||
    die "retention left no encrypted backup snapshots"
  backup_snapshot_exists "$snapshot_id" "$snapshot_check_log" ||
    die "retention removed the newly-created encrypted snapshot"
  return "$retention_status"
}

finalize_backup_success() {
  local staging_dir="$1" snapshot_id="$2" retention_status="$3"
  [ "$retention_status" -eq 0 ] ||
    die "encrypted offsite backup succeeded but retention needs operator attention"
  cleanup_successful_backup_staging "$staging_dir"
  cleanup_stale_backup_staging
  backup_write_state_file "$S12_BACKUP_DIR/latest.offsite.snapshot" "$snapshot_id"
  backup_write_state_file "$S12_BACKUP_DIR/latest.completed-at" "$(date +%s)"
  note "successful offsite backup marker updated for snapshot $snapshot_id"
}

create_backup() {
  require_tools
  load_environment
  backup_validate_configuration || die "encrypted offsite backup configuration is invalid"
  backup_require_repository || die "encrypted offsite backup repository is unavailable"
  umask 077
  local postgres_id stamp staging_dir snapshot_log snapshot_check_log snapshot_id retention_status=0
  mkdir -p "$S12_BACKUP_DIR"
  chmod 700 "$S12_BACKUP_DIR"
  acquire_backup_lock

  postgres_id="$(backup_postgres_id)"
  [ -n "$postgres_id" ] || die "source PostgreSQL is absent"
  stamp="$(date -u +%Y%m%dT%H%M%SZ)"
  staging_dir="$(mktemp -d "$S12_BACKUP_DIR/.offsite-staging.XXXXXX")"
  chmod 700 "$staging_dir"
  S12_BACKUP_STAGING_DIR="$staging_dir"
  snapshot_log="$S12_BACKUP_DIR/.offsite-restic-$stamp.log"
  snapshot_check_log="$S12_BACKUP_DIR/.offsite-check-$stamp.log"
  capture_backup_dump "$postgres_id" "$staging_dir" "$stamp"
  snapshot_id="$(upload_backup_snapshot "$staging_dir" "$snapshot_log" "$snapshot_check_log")"
  validate_snapshot_id "$snapshot_id"
  if apply_backup_retention "$snapshot_id" "$snapshot_check_log"; then
    retention_status=0
  else
    retention_status=$?
  fi
  rm -f -- "$snapshot_log" "$snapshot_check_log"
  finalize_backup_success "$staging_dir" "$snapshot_id" "$retention_status"
}
restore_target_id() {
  docker inspect --type container --format '{{.Id}}' "$S12_RESTORE_VERIFY_CONTAINER" 2>/dev/null || true
}

wait_for_restore_target() {
  local attempts=0
  while [ "$attempts" -lt 90 ]; do
    if docker exec "$S12_RESTORE_VERIFY_CONTAINER" \
      pg_isready --username gradex_restore --dbname gradex_restore >/dev/null 2>&1; then
      return
    fi
    attempts=$((attempts + 1))
    sleep 2
  done
  docker logs --tail 50 "$S12_RESTORE_VERIFY_CONTAINER" >&2 || true
  die "restore target PostgreSQL did not become ready"
}

prepare_restore_database() {
  local source_id target_id
  # The restore source is the same PostgreSQL the backup was captured from, so it
  # resolves through the explicit container identity rather than a Compose service
  # lookup. `service_id postgres` only ever answered for deployments whose live
  # topology is this repository's Compose project, which is exactly the case the
  # backup path already stopped assuming.
  source_id="$(backup_postgres_id)"
  [ -n "$source_id" ] || die "source PostgreSQL is absent"
  [ -n "${RESTORE_POSTGRES_PASSWORD:-}" ] ||
    die "RESTORE_POSTGRES_PASSWORD is required to create the restore verification database"
  docker rm --force "$S12_RESTORE_VERIFY_CONTAINER" >/dev/null 2>&1 || true
  if docker volume inspect "$S12_RESTORE_VERIFY_VOLUME" >/dev/null 2>&1; then
    docker volume rm "$S12_RESTORE_VERIFY_VOLUME" >/dev/null
  fi
  docker volume create "$S12_RESTORE_VERIFY_VOLUME" >/dev/null
  # No published port and no application network: the restored copy is reachable
  # only through `docker exec` for the duration of the verification.
  docker run --detach --name "$S12_RESTORE_VERIFY_CONTAINER" \
    --env POSTGRES_USER=gradex_restore \
    --env POSTGRES_PASSWORD="$RESTORE_POSTGRES_PASSWORD" \
    --env POSTGRES_DB=gradex_restore \
    --volume "$S12_RESTORE_VERIFY_VOLUME":/var/lib/postgresql/data \
    "$S12_RESTORE_POSTGRES_IMAGE" >/dev/null
  wait_for_restore_target
  target_id="$(restore_target_id)"
  [ -n "$target_id" ] && [ "$source_id" != "$target_id" ] ||
    die "restore target is not isolated from source"
  printf '%s\n' "$target_id"
}

discard_restore_database() {
  docker rm --force "$S12_RESTORE_VERIFY_CONTAINER" >/dev/null 2>&1 || true
  docker volume rm "$S12_RESTORE_VERIFY_VOLUME" >/dev/null 2>&1 || true
}

restore_dump_into_database() {
  local target_id="$1" staging_dir="$2" dump_name="$3"
  docker exec --interactive "$target_id" pg_restore --exit-on-error --single-transaction \
    --no-owner --no-acl --username gradex_restore --dbname gradex_restore <"$staging_dir/$dump_name"
}

restore_backup() {
  [ "$#" -le 1 ] || die "restore accepts at most one snapshot ID"
  require_tools
  load_environment
  backup_validate_configuration || die "encrypted offsite backup configuration is invalid"
  backup_require_repository || die "encrypted offsite backup repository is unavailable"
  local snapshot_id="${1:-}" snapshot_log="$S12_BACKUP_DIR/.restore-snapshots.json"
  local staging_dir dump_name schema_name expected_schema_state expected_record_counts target_id
  mkdir -p "$S12_BACKUP_DIR"
  chmod 700 "$S12_BACKUP_DIR"
  acquire_backup_lock
  if [ -z "$snapshot_id" ]; then
    snapshot_id="$(backup_latest_snapshot_id "$snapshot_log")"
  fi
  validate_snapshot_id "$snapshot_id"
  backup_snapshot_exists "$snapshot_id" "$snapshot_log" ||
    die "requested encrypted offsite snapshot is absent"
  rm -f -- "$S12_RESTORED_SOURCE_FILE" "$S12_RESTORED_SCHEMA_STATE_FILE" \
    "$S12_RESTORED_RECORD_COUNTS_FILE"
  staging_dir="$(mktemp -d "$S12_BACKUP_DIR/.restore-staging.XXXXXX")"
  chmod 700 "$staging_dir"
  S12_RESTORE_STAGING_DIR="$staging_dir"
  restore_offsite_snapshot_files "$snapshot_id" "$staging_dir"
  dump_name="$(cat "$staging_dir/restored-dump-name")"
  schema_name="$(cat "$staging_dir/restored-schema-name")"
  [[ "$dump_name" =~ ^gradex-[0-9]{8}T[0-9]{6}Z\.dump$ ]] ||
    die "restored remote dump identity is invalid"

  [[ "$schema_name" =~ ^gradex-[0-9]{8}T[0-9]{6}Z\.dump\.schema-state$ ]] ||
    die "restored remote schema identity is invalid"
  expected_schema_state="$(sed -n '1p' "$staging_dir/$schema_name")"
  [[ "$expected_schema_state" =~ ^[0-9]+\|false$ ]] ||
    die "restored remote schema metadata is invalid"
  # Absent for snapshots captured before record counts were recorded; those still
  # restore, and still prove their schema, but cannot be compared count for count.
  expected_record_counts="$(sed -n '2s/^counts=//p' "$staging_dir/$schema_name")"
  if [ -n "$expected_record_counts" ]; then
    [[ "$expected_record_counts" =~ ^[0-9]+(\|[0-9]+){4}$ ]] ||
      die "restored remote record-count metadata is invalid"
  fi

  target_id="$(prepare_restore_database)"
  restore_dump_into_database "$target_id" "$staging_dir" "$dump_name"
  backup_write_state_file "$S12_RESTORED_SOURCE_FILE" "$snapshot_id"
  backup_write_state_file "$S12_RESTORED_SCHEMA_STATE_FILE" "$expected_schema_state"
  backup_write_state_file "$S12_RESTORED_RECORD_COUNTS_FILE" "$expected_record_counts"
  rm -rf -- "$staging_dir"
  S12_RESTORE_STAGING_DIR=""
  cleanup_stale_restore_staging
  rm -f -- "$snapshot_log"
  note "restored encrypted offsite snapshot $snapshot_id into a fresh isolated PostgreSQL volume"
}
restored_database_state() {
  local target_id="$1"
  docker exec "$target_id" psql --no-psqlrc --username gradex_restore --dbname gradex_restore \
    --tuples-only --no-align --command "
      SELECT
        (SELECT version::text || '|' || dirty::text FROM schema_migrations) || '|' ||
        (SELECT count(*) FROM accounts) || '|' ||
        (SELECT count(*) FROM courses) || '|' ||
        (SELECT count(*) FROM course_access_invitations) || '|' ||
        (SELECT count(*) FROM entitlements WHERE state = 'ACTIVE' AND source_invitation_id IS NOT NULL) || '|' ||
        (SELECT count(*) FROM enrollments);"
}

assert_restored_database_invariants() {
  local target_id="$1" expected_schema_state="$2" expected_record_counts="${3:-}" restored_state
  local version dirty restored_counts missing_table
  restored_state="$(restored_database_state "$target_id")"
  # A restore that lost a critical table fails here rather than reporting a count,
  # because the query that produces the counts cannot run without those tables.
  [ -n "$restored_state" ] || die "restored database did not answer the invariant query"
  version="${restored_state%%|*}"
  dirty="$(printf '%s' "$restored_state" | cut -d'|' -f2)"
  restored_counts="$(printf '%s' "$restored_state" | cut -d'|' -f3-)"
  [ "$version|$dirty" = "$expected_schema_state" ] ||
    die "restored schema $version|$dirty does not match remote schema $expected_schema_state"
  for missing_table in accounts courses course_access_invitations entitlements enrollments; do
    docker exec "$target_id" psql --no-psqlrc --username gradex_restore --dbname gradex_restore \
      --tuples-only --no-align --command "SELECT to_regclass('public.$missing_table');" |
      grep --quiet --line-regexp "$missing_table" ||
      die "restored database is missing the $missing_table table"
  done
  if [ -z "$expected_record_counts" ]; then
    note "snapshot predates recorded source counts; restored counts $restored_counts were not compared"
    return
  fi
  # Exact equality with what the source held at capture time. Zero is a legitimate
  # source value and passes; a truncated, partial, or substituted restore does not.
  [ "$restored_counts" = "$expected_record_counts" ] ||
    die "restored record counts $restored_counts do not match source counts $expected_record_counts"
}

verify_restore() {
  require_tools
  load_environment
  backup_validate_configuration || die "encrypted offsite backup configuration is invalid"
  backup_require_repository || die "encrypted offsite backup repository is unavailable"
  local snapshot_id expected_schema_state expected_record_counts snapshot_log target_id
  mkdir -p "$S12_BACKUP_DIR"
  chmod 700 "$S12_BACKUP_DIR"
  acquire_backup_lock
  [ -f "$S12_RESTORED_SOURCE_FILE" ] || die "restored encrypted snapshot identity is absent"
  snapshot_id="$(cat "$S12_RESTORED_SOURCE_FILE")"
  validate_snapshot_id "$snapshot_id"
  snapshot_log="$S12_BACKUP_DIR/.verify-restore-snapshot.json"
  backup_snapshot_exists "$snapshot_id" "$snapshot_log" ||
    die "restored encrypted snapshot is no longer available"
  backup_deep_check_repository || die "deep encrypted repository integrity check failed"
  [ -f "$S12_RESTORED_SCHEMA_STATE_FILE" ] || die "restored schema provenance is absent"
  expected_schema_state="$(cat "$S12_RESTORED_SCHEMA_STATE_FILE")"
  [[ "$expected_schema_state" =~ ^[0-9]+\|false$ ]] ||
    die "restored schema provenance is invalid"
  expected_record_counts=""
  if [ -s "$S12_RESTORED_RECORD_COUNTS_FILE" ]; then
    expected_record_counts="$(cat "$S12_RESTORED_RECORD_COUNTS_FILE")"
    [[ "$expected_record_counts" =~ ^[0-9]+(\|[0-9]+){4}$ ]] ||
      die "restored record-count provenance is invalid"
  fi
  target_id="$(restore_target_id)"
  [ -n "$target_id" ] || die "restore target is absent"
  assert_restored_database_invariants "$target_id" "$expected_schema_state" "$expected_record_counts"
  rm -f -- "$snapshot_log"
  note "restored encrypted snapshot $snapshot_id, schema $expected_schema_state, identity, Course, invitation provenance, Entitlement, and Enrollment passed"
}
initialize_backup_repository() {
  require_tools
  load_environment
  backup_validate_configuration || die "encrypted offsite backup configuration is invalid"
  mkdir -p "$S12_BACKUP_DIR"
  chmod 700 "$S12_BACKUP_DIR"
  acquire_backup_lock
  backup_initialize_repository || die "could not initialize encrypted offsite backup repository"
  note "initialized encrypted offsite backup repository; keep the password copy off-host"
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
  printf 'usage: %s {prepare|up|up-core|up-edge|verify|verify-core|bootstrap-admin|seed-smoke|monitor|monitor-alert-test|open-db-tunnel|close-db-tunnel|backup-init|backup|restore [SNAPSHOT_ID]|verify-restore|apply-release MANIFEST|status|logs [SERVICE]|stop}\n' "$0" >&2
  exit 2
}

case "${1:-}" in
  prepare) [ "$#" = 1 ] || usage; prepare ;;
  up) [ "$#" = 1 ] || usage; start_environment ;;
  up-core) [ "$#" = 1 ] || usage; start_core ;;
  up-edge) [ "$#" = 1 ] || usage; start_edge ;;
  verify) [ "$#" = 1 ] || usage; verify_environment ;;
  verify-core) [ "$#" = 1 ] || usage; verify_core ;;
  bootstrap-admin) [ "$#" = 1 ] || usage; bootstrap_admin ;;
  seed-smoke) [ "$#" = 1 ] || usage; seed_smoke ;;
  monitor) [ "$#" = 1 ] || usage; run_monitor ;;
  monitor-alert-test) [ "$#" = 1 ] || usage; run_monitor_alert_test ;;
  open-db-tunnel) [ "$#" = 1 ] || usage; open_db_tunnel ;;
  close-db-tunnel) [ "$#" = 1 ] || usage; close_db_tunnel ;;
  backup-init) [ "$#" = 1 ] || usage; initialize_backup_repository ;;
  backup) [ "$#" = 1 ] || usage; create_backup ;;
  restore) [ "$#" -le 2 ] || usage; shift; restore_backup "$@" ;;
  verify-restore) [ "$#" = 1 ] || usage; verify_restore ;;
  apply-release) shift; apply_release "$@" ;;
  status) load_environment; compose --profile restore ps ;;
  logs) load_environment; if [ -n "${2:-}" ]; then compose logs --no-color "$2"; else compose logs --no-color; fi ;;
  stop) load_environment; compose down ;;
  *) usage ;;
esac
