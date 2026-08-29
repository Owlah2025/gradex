#!/usr/bin/env bash
#
# verify-live-staging-acceptance.sh — release acceptance smoke for a *deployed*
# staging environment.
#
# WHY
#   The existing smoke entry points cannot verify the environment we actually
#   ship. Each of them owns its own environment instead of observing one:
#
#     deploy/scripts/verify-staging-smoke.sh   binds to project gradex-s12,
#                                              compose.production-like.yml and a
#                                              local CA under deploy/.state
#     deploy/hostinger/host.sh seed-smoke      resets the staging database
#     deploy/hostinger/verify-provider-smoke.sh seeds and asserts its own
#                                              database identity, which the
#                                              deployed API is not connected to
#     deploy/hostinger/host.sh verify          requires a full release runtime
#                                              environment file the deployed
#                                              host does not keep
#
#   So a real deployment could be healthy and still have no canonical
#   acceptance evidence. That is a verification-harness defect, not a product
#   defect, and this script is the fix: it observes a running deployment
#   instead of constructing one.
#
# WHAT IT CHECKS
#   Public   — frontend root, locale routes, catalogue, a course detail page
#              discovered through the public API, privacy and terms.
#   API      — /healthz, /readyz, public catalogue, academic options.
#   Boundaries — unauthenticated admin and authoring routes reject, protected
#              media cannot be fetched anonymously, and the anonymous
#              session/admission boundary stays fail-closed exactly as
#              verify-edge-security.sh pins it for the isolated topology.
#   Runtime  — optional, needs Docker on the host: worker running, no restart
#              loop, clean PostgreSQL schema, authenticated Redis, clean
#              outbox.
#   Provider — optional, needs Docker and the release proof image: one
#              run-scoped object under the disposable capacity namespace,
#              removed again before exit.
#
# WHAT IT DOES NOT DO
#   It never resets, seeds, or repoints a database, never brings a Compose
#   project up or down, never writes outside the run-scoped provider prefix,
#   and never mutates an existing record. Everything it creates carries this
#   run's identifier and is removed by the exit trap. Authenticated product
#   journeys stay with the isolated E2E suite, which owns disposable data.
#
# USAGE
#   deploy/scripts/verify-live-staging-acceptance.sh https://staging.example.com
#
#   Optional, each opt-in and each additive:
#     GRADEX_LIVE_SMOKE_COMPOSE_PROJECT=gradex-founder-beta   runtime checks
#     GRADEX_LIVE_SMOKE_POSTGRES_DB=gradex_founder_beta       runtime checks
#     GRADEX_LIVE_SMOKE_PROVIDER_IMAGE=gradex-backend-proof:… provider check
#     GRADEX_LIVE_SMOKE_PROVIDER_ENV_FILE=/path/r2.env        provider check
#
#   Running against a production hostname additionally requires
#   GRADEX_LIVE_SMOKE_ALLOW_PRODUCTION=i-have-authorized-production-smoke.

set -euo pipefail

LIVE_ORIGIN=""
LIVE_HOSTNAME=""
LIVE_RUN_ID=""
LIVE_TEMPORARY=""
LIVE_PROVIDER_OBJECT=""
LIVE_PASSED=0
LIVE_SKIPPED=0

# Names that must never be accepted by a staging-shaped smoke on the strength of
# an omitted environment variable. Extend this list, never shorten it.
LIVE_PRODUCTION_HOSTNAMES=(
  gradexcourses.com
  www.gradexcourses.com
)

LIVE_PRODUCTION_AUTHORIZATION="i-have-authorized-production-smoke"

note() { printf 'live-staging-acceptance: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }
pass() { LIVE_PASSED=$((LIVE_PASSED + 1)); note "PASS $*"; }
skip() { LIVE_SKIPPED=$((LIVE_SKIPPED + 1)); note "SKIP $*"; }

cleanup() {
  remove_provider_object
  if [ -n "$LIVE_TEMPORARY" ] && [ -d "$LIVE_TEMPORARY" ]; then
    rm -rf -- "$LIVE_TEMPORARY"
  fi
}

require_tools() {
  local tool
  for tool in curl jq mktemp openssl sed tr; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done
}

parse_origin() {
  [ "$#" = 1 ] || die "usage: $0 https://staging.example.com"
  LIVE_ORIGIN="${1%/}"
  [[ "$LIVE_ORIGIN" =~ ^https://[A-Za-z0-9.-]+$ ]] ||
    die "origin must be a credential-free HTTPS origin"
  LIVE_HOSTNAME="${LIVE_ORIGIN#https://}"
}

# A production name reaches the deliberate, named authorization or it does not
# run at all. Absence of the variable must never mean "staging policy applies".
assert_production_authorized() {
  local hostname="$1" authorization="${2:-}" candidate
  for candidate in "${LIVE_PRODUCTION_HOSTNAMES[@]}"; do
    [ "$hostname" = "$candidate" ] || continue
    [ "$authorization" = "$LIVE_PRODUCTION_AUTHORIZATION" ] ||
      die "$hostname is a production hostname; set GRADEX_LIVE_SMOKE_ALLOW_PRODUCTION=$LIVE_PRODUCTION_AUTHORIZATION to run against it deliberately"
    note "production hostname $hostname was explicitly authorized"
    return
  done
}

http_code() {
  local url="$1" body="${2:-/dev/null}"
  shift 2 2>/dev/null || shift $(($#))
  curl --silent --show-error --connect-timeout 5 --max-time 20 \
    --output "$body" --write-out '%{http_code}' "$@" "$url"
}

expect_code() {
  local got="$1" want="$2" check="$3"
  [ "$got" = "$want" ] || die "$check returned HTTP $got, expected $want"
}

expect_one_of() {
  local got="$1" check="$2" want
  shift 2
  for want in "$@"; do
    [ "$got" = "$want" ] && return
  done
  die "$check returned HTTP $got, expected one of: $*"
}

check_public_pages() {
  local path status
  for path in / /login /register /ar/catalog /en/catalog /ar/privacy /ar/terms /en/privacy /en/terms; do
    status="$(http_code "$LIVE_ORIGIN$path")"
    expect_code "$status" "200" "public page $path"
  done
  pass "public frontend root, locale, catalogue, privacy, and terms routes"
}

check_api_health() {
  local body status
  body="$LIVE_TEMPORARY/health.json"
  status="$(http_code "$LIVE_ORIGIN/healthz" "$body")"
  expect_code "$status" "200" "/healthz"
  jq --exit-status '.status == "ok"' "$body" >/dev/null ||
    die "/healthz did not report ok"
  status="$(http_code "$LIVE_ORIGIN/readyz" "$body")"
  expect_code "$status" "200" "/readyz"
  jq --exit-status '.status == "ok" and .checks.postgres == "ok" and .checks.redis == "ok" and .checks.schema == "ok"' \
    "$body" >/dev/null || die "/readyz did not report every dependency ok"
  pass "API health and readiness with postgres, redis, and schema ok"
}

# The catalogue is read through the public API and the detail page is addressed
# by whatever the deployment actually holds. Nothing here may hardcode a
# database identity: the s12 fixture UUIDs do not exist in a real deployment.
check_catalogue() {
  local body status slug count
  body="$LIVE_TEMPORARY/catalogue.json"
  status="$(http_code "$LIVE_ORIGIN/api/v1/catalog/courses" "$body")"
  expect_code "$status" "200" "public catalogue"
  jq --exit-status 'has("items") and (.items | type == "array")' "$body" >/dev/null ||
    die "public catalogue did not return an items array"
  count="$(jq '.items | length' "$body")"
  pass "public catalogue returned $count course(s)"

  status="$(http_code "$LIVE_ORIGIN/api/v1/catalog/academic-options/institutions" "$body")"
  expect_code "$status" "200" "academic options"
  jq --exit-status 'has("items") and (.items | type == "array")' "$body" >/dev/null ||
    die "academic options did not return an items array"
  pass "academic options"

  status="$(http_code "$LIVE_ORIGIN/api/v1/catalog/courses" "$body")"
  expect_code "$status" "200" "public catalogue"
  slug="$(jq --raw-output '.items[0].slug // empty' "$body")"
  if [ -z "$slug" ]; then
    skip "course detail: the deployment publishes no public course"
    return
  fi
  case "$slug" in
    *[!A-Za-z0-9._-]*) die "discovered course slug is not URL-safe" ;;
  esac
  status="$(http_code "$LIVE_ORIGIN/api/v1/catalog/courses/$slug" "$body")"
  expect_code "$status" "200" "course detail for the discovered slug"
  jq --exit-status '.slug | type == "string" and length > 0' "$body" >/dev/null ||
    die "course detail did not return the course"
  pass "course detail for a course discovered through the public API"
}

# Anonymous access must be refused, not merely unhelpful. A 200 here is the
# failure this check exists to catch.
check_authorization_boundaries() {
  local path status body
  body="$LIVE_TEMPORARY/denied.json"
  for path in \
    /api/v1/admin/courses \
    /api/v1/admin/review/queue \
    /api/v1/admin/course-access-invitations \
    /api/v1/admin/reports \
    /api/v1/courses \
    /api/v1/authoring/academic/subject-requests; do
    status="$(http_code "$LIVE_ORIGIN$path" "$body")"
    expect_code "$status" "401" "unauthenticated request to $path"
  done
  pass "unauthenticated admin, authoring, and account routes reject with 401"

  status="$(http_code "$LIVE_ORIGIN/api/v1/media/playback-manifests/live-smoke-$LIVE_RUN_ID/index.m3u8" "$body")"
  [ "$status" != "200" ] ||
    die "protected media manifest was served to an anonymous caller"
  expect_one_of "$status" "anonymous protected media" 401 403 404
  pass "protected media is not served anonymously (HTTP $status)"
}

# The same fail-closed contract verify-edge-security.sh pins for the isolated
# topology, asserted against the deployed edge. Every request is a rejected or
# failed anonymous attempt; none of them writes a product record.
check_admission_boundary() {
  local headers body cookies status token
  headers="$LIVE_TEMPORARY/bootstrap.headers"
  body="$LIVE_TEMPORARY/bootstrap.json"
  cookies="$LIVE_TEMPORARY/cookies.txt"

  status="$(http_code "$LIVE_ORIGIN/api/v1/session/bootstrap" "$body" \
    --cookie-jar "$cookies" --dump-header "$headers")"
  expect_code "$status" "200" "anonymous session bootstrap"
  grep --extended-regexp --ignore-case --quiet \
    '^set-cookie: __Host-gradex_anon=.*; Path=/;.*HttpOnly;.*Secure;.*SameSite=Strict' "$headers" ||
    die "anonymous session cookie is missing its host-only, HttpOnly, Secure, SameSite=Strict attributes"
  if grep --extended-regexp --ignore-case --quiet \
    '^set-cookie: __Host-gradex_anon=.*;.*Domain=' "$headers"; then
    die "anonymous session cookie was scoped to a domain"
  fi
  token="$(jq --exit-status --raw-output '.csrf_token | select(type == "string" and length > 0)' "$body")" ||
    die "bootstrap response did not contain a CSRF token"
  pass "anonymous session bootstrap issues a host-only fail-closed cookie"

  local login="$LIVE_TEMPORARY/login.json"
  local payload="{\"email\":\"live-smoke-$LIVE_RUN_ID@example.invalid\",\"password\":\"invalid-password\"}"

  status="$(http_code "$LIVE_ORIGIN/api/v1/sessions" "$login" \
    --request POST --cookie "$cookies" --header 'Content-Type: application/json' \
    --header 'Origin: https://attacker.example' --header "X-CSRF-Token: $token" \
    --data "$payload")"
  expect_code "$status" "403" "foreign-origin login"
  [ "$(jq --raw-output '.code' "$login")" = "CSRF_VALIDATION_FAILED" ] ||
    die "foreign-origin login did not fail at browser security"

  status="$(http_code "$LIVE_ORIGIN/api/v1/sessions" "$login" \
    --request POST --cookie "$cookies" --header 'Content-Type: application/json' \
    --header "Origin: $LIVE_ORIGIN" --header 'X-CSRF-Token: invalid' \
    --data "$payload")"
  expect_code "$status" "403" "invalid-CSRF login"
  [ "$(jq --raw-output '.code' "$login")" = "CSRF_VALIDATION_FAILED" ] ||
    die "invalid-CSRF login did not fail at browser security"

  status="$(http_code "$LIVE_ORIGIN/api/v1/sessions" "$login" \
    --request POST --cookie "$cookies" --header 'Content-Type: application/json' \
    --header "Origin: $LIVE_ORIGIN" --header "X-CSRF-Token: $token" \
    --data "$payload")"
  expect_code "$status" "401" "trusted-origin login for an account that does not exist"
  [ "$(jq --raw-output '.code' "$login")" = "AUTHENTICATION_FAILED" ] ||
    die "trusted-origin login did not reach authentication"
  pass "admission boundary rejects foreign origin and invalid CSRF, and fails authentication closed"

  local cors_headers="$LIVE_TEMPORARY/cors.headers"
  status="$(http_code "$LIVE_ORIGIN/api/v1/sessions" /dev/null \
    --request OPTIONS --header 'Origin: https://attacker.example' \
    --header 'Access-Control-Request-Method: POST' --dump-header "$cors_headers")"
  expect_code "$status" "405" "cross-origin preflight"
  if grep --extended-regexp --ignore-case --quiet \
    '^access-control-allow-(origin|credentials):' "$cors_headers"; then
    die "cross-origin preflight exposed a CORS allowance"
  fi
  pass "cross-origin preflight is refused without a CORS allowance"
}

# Observation only. Nothing below starts, stops, recreates, or removes a
# service, and every command names the project the operator passed in.
check_runtime() {
  local project="${GRADEX_LIVE_SMOKE_COMPOSE_PROJECT:-}"
  local database="${GRADEX_LIVE_SMOKE_POSTGRES_DB:-}"
  if [ -z "$project" ]; then
    skip "runtime checks: set GRADEX_LIVE_SMOKE_COMPOSE_PROJECT to enable them"
    return
  fi
  [[ "$project" =~ ^[a-z0-9][a-z0-9_-]{2,62}$ ]] || die "compose project name is invalid"
  command -v docker >/dev/null 2>&1 || die "docker is required for runtime checks"

  local service container status restarts
  for service in api worker frontend postgres redis; do
    container="$(docker ps --quiet --filter "label=com.docker.compose.project=$project" \
      --filter "label=com.docker.compose.service=$service" | head -1)"
    [ -n "$container" ] || die "$service is not running in project $project"
    status="$(docker inspect --format '{{.State.Status}}' "$container")"
    [ "$status" = running ] || die "$service is $status"
    restarts="$(docker inspect --format '{{.RestartCount}}' "$container")"
    [ "$restarts" = "0" ] || die "$service has restarted $restarts times"
  done
  pass "api, worker, frontend, postgres, and redis run without a restart loop"

  if [ -z "$database" ]; then
    skip "schema and outbox checks: set GRADEX_LIVE_SMOKE_POSTGRES_DB to enable them"
    return
  fi
  [[ "$database" =~ ^[a-z_][a-z0-9_]{0,62}$ ]] || die "database name is invalid"
  local postgres backend expected schema
  postgres="$(docker ps --quiet --filter "label=com.docker.compose.project=$project" \
    --filter 'label=com.docker.compose.service=postgres' | head -1)"
  backend="$(docker ps --quiet --filter "label=com.docker.compose.project=$project" \
    --filter 'label=com.docker.compose.service=api' | head -1)"
  schema="$(docker exec "$postgres" psql --no-psqlrc --username gradex --dbname "$database" \
    --tuples-only --no-align --command 'SELECT version::text || '"'"'|'"'"' || dirty::text FROM schema_migrations;')"
  expected="$(docker run --rm --entrypoint gradex-migrate \
    "$(docker inspect --format '{{.Config.Image}}' "$backend")" max-version)"
  [ "$schema" = "$expected|false" ] ||
    die "schema is $schema, expected clean version $expected for the deployed backend image"
  pass "PostgreSQL schema is clean at version $expected for the deployed backend image"

  local outbox
  outbox="$(docker exec "$postgres" psql --no-psqlrc --username gradex --dbname "$database" \
    --tuples-only --no-align --command \
    "SELECT count(*) FROM transactional_email_attempts WHERE outcome <> 'ACCEPTED';")"
  [ "$outbox" = "0" ] || die "the transactional email outbox holds $outbox non-accepted attempts"
  pass "transactional email outbox holds no non-accepted attempt"

  local redis pong
  redis="$(docker ps --quiet --filter "label=com.docker.compose.project=$project" \
    --filter 'label=com.docker.compose.service=redis' | head -1)"
  pong="$(docker exec "$redis" redis-cli --tls --cacert /run/gradex/redis/ca.crt ping 2>&1 || true)"
  [ "$pong" = PONG ] || die "authenticated Redis TLS did not answer PONG"
  pass "authenticated Redis over verified TLS"
}

remove_provider_object() {
  [ -n "$LIVE_PROVIDER_OBJECT" ] || return 0
  local object="$LIVE_PROVIDER_OBJECT"
  LIVE_PROVIDER_OBJECT=""
  provider_client rm "r2/$S3_BUCKET/$object" >/dev/null 2>&1 || true
  if provider_client stat "r2/$S3_BUCKET/$object" >/dev/null 2>&1; then
    note "FAIL run-scoped provider object $object survived cleanup"
    return 1
  fi
  note "removed run-scoped provider object $object"
}

provider_client() {
  docker run --rm \
    --env "MC_HOST_r2=https://$S3_ACCESS_KEY:$S3_SECRET_KEY@${S3_ENDPOINT#https://}" \
    minio/mc:RELEASE.2025-08-13T08-35-41Z "$@"
}

# One object, under the disposable capacity namespace the release proof image
# already restricts itself to, named for this run and removed by the trap.
check_provider() {
  local image="${GRADEX_LIVE_SMOKE_PROVIDER_IMAGE:-}"
  local env_file="${GRADEX_LIVE_SMOKE_PROVIDER_ENV_FILE:-}"
  if [ -z "$image" ] || [ -z "$env_file" ]; then
    skip "provider check: set GRADEX_LIVE_SMOKE_PROVIDER_IMAGE and GRADEX_LIVE_SMOKE_PROVIDER_ENV_FILE to enable it"
    return
  fi
  command -v docker >/dev/null 2>&1 || die "docker is required for the provider check"
  command -v md5sum >/dev/null 2>&1 || die "md5sum is required for the provider check"
  [ -f "$env_file" ] || die "provider environment file is absent"
  set -a
  # shellcheck disable=SC1090
  . "$env_file"
  set +a
  local name
  for name in S3_ENDPOINT S3_BUCKET S3_ACCESS_KEY S3_SECRET_KEY; do
    [ -n "${!name:-}" ] || die "$name is required in the provider environment file"
  done
  [[ "$S3_ENDPOINT" =~ ^https://[A-Za-z0-9.-]+$ ]] ||
    die "S3_ENDPOINT must be a credential-free HTTPS origin"

  local prefix="capacity/live-smoke-$LIVE_RUN_ID/"
  local key="test/master.m3u8"
  local fixture="$LIVE_TEMPORARY/master.m3u8"
  printf '#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:1.0,\nsegment000.ts\n#EXT-X-ENDLIST\n' >"$fixture"
  local digest
  digest="$(md5sum "$fixture" | cut -d' ' -f1)"

  LIVE_PROVIDER_OBJECT="$prefix$key"
  docker run --rm --interactive \
    --env "S3_ENDPOINT=$S3_ENDPOINT" --env "S3_BUCKET=$S3_BUCKET" \
    --env "S3_ACCESS_KEY=$S3_ACCESS_KEY" --env "S3_SECRET_KEY=$S3_SECRET_KEY" \
    --env "S3_USE_PATH_STYLE=${S3_USE_PATH_STYLE:-false}" \
    --entrypoint gradex-storage-fixture "$image" \
    -key "$key" -prefix "$prefix" <"$fixture" ||
    die "the release image could not write the run-scoped provider object"

  local etag
  etag="$(provider_client --json stat "r2/$S3_BUCKET/$LIVE_PROVIDER_OBJECT" | jq --raw-output '.etag')" ||
    die "the run-scoped provider object could not be read back"
  [ "$etag" = "$digest" ] ||
    die "provider ETag $etag does not match the written content digest $digest"
  pass "release image wrote, and an authenticated read confirmed, one run-scoped provider object"

  remove_provider_object || die "run-scoped provider cleanup failed"
  pass "run-scoped provider object was removed and its absence confirmed"
}

main() {
  require_tools
  parse_origin "$@"
  assert_production_authorized "$LIVE_HOSTNAME" "${GRADEX_LIVE_SMOKE_ALLOW_PRODUCTION:-}"
  LIVE_RUN_ID="$(date -u +%Y%m%dt%H%M%SZ)-$$"
  LIVE_TEMPORARY="$(mktemp -d)"
  chmod 700 "$LIVE_TEMPORARY"
  trap cleanup EXIT
  note "run $LIVE_RUN_ID against $LIVE_ORIGIN"

  check_api_health
  check_public_pages
  check_catalogue
  check_authorization_boundaries
  check_admission_boundary
  check_runtime
  check_provider

  note "$LIVE_PASSED check group(s) passed, $LIVE_SKIPPED skipped"
}

if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
  main "$@"
fi
