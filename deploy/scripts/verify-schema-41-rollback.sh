#!/usr/bin/env bash
# Exercise the Hostinger rollback gates without starting a container or touching a database.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOST_SCRIPT="$ROOT/deploy/hostinger/host.sh"

die() { printf 'schema-41-rollback-guard: %s\n' "$*" >&2; exit 1; }
note() { :; }

extract() {
  awk -v name="$1" '
    $0 ~ "^" name "\\(\\)" { capture = 1 }
    capture { print }
    capture && /^}/ { exit }
  ' "$HOST_SCRIPT"
}

for function_name in assert_production_project_scope start_core require_absent \
  require_schema41_production_target require_schema41_release_identity rollback_schema_41_foundation; do
  definition="$(extract "$function_name")"
  [ -n "$definition" ] || die "missing $function_name in host.sh"
  eval "$definition"
done
grep --quiet --fixed-strings 'up-core-schema-41-foundation) [ "$#" = 1 ] || usage; start_core schema-41-foundation' "$HOST_SCRIPT" ||
  die "guarded forward command is not dispatched"
grep --quiet --fixed-strings 'rollback-schema-41-foundation) [ "$#" = 1 ] || usage; rollback_schema_41_foundation' "$HOST_SCRIPT" ||
  die "dedicated rollback command is not dispatched"

# These are the exact external seams used by the extracted functions. Their
# fixtures are values only; no docker, migration, or production connection runs.
require_tools() { :; }
load_environment() { :; }
prepare() { :; }
wait_for_status() { :; }
wait_for_completion() { :; }
validate_environment() {
  assert_production_project_scope
  [ "$MOCK_IMAGE_SHA" = "$GRADEX_RELEASE_SHA" ] || die "image revision mismatch"
}
require_status() { [ "$1" = postgres ] && [ "$2" = healthy ] || die "unexpected status probe"; }
image_max_schema_version() { printf '%s\n' "$MOCK_MAX_SCHEMA"; }
service_id() {
  [ "${MOCK_PS_FAILURE:-}" != "$1" ] || return 1
  case "$1" in
    api) printf '%s' "$MOCK_API_ID" ;;
    worker) printf '%s' "$MOCK_WORKER_ID" ;;
    *) return 1 ;;
  esac
}
git() {
  case "$3" in
    rev-parse) printf '%s\n' "$MOCK_TREE_SHA" ;;
    status) [ "$MOCK_TREE_CLEAN" = true ] || printf ' M changed\n' ;;
    merge-base) [ "$MOCK_ANCESTOR" = true ] ;;
    *) return 1 ;;
  esac
}
compose() { COMPOSE_CALL="$*"; printf 'COMPOSE %s\n' "$*"; }

fixture() {
  S12_ROOT="$ROOT"
  S12_PROJECT=gradex-production
  S12_PROJECT_STAGING_DEFAULT=gradex-staging
  S12_PROJECT_DECLARED=declared
  S12_STATE_DIR_DECLARED=declared
  S12_HOST_STATE_DIR=/home/deploy/gradex-production
  S12_ENV_FILE="$S12_HOST_STATE_DIR/runtime.env"
  APP_ENV=production
  POSTGRES_DB=gradex_production
  DATABASE_URL='postgres://gradex:placeholder@postgres:5432/gradex_production?sslmode=disable'
  GRADEX_RELEASE_SHA=1111111111111111111111111111111111111111
  GRADEX_BACKEND_IMAGE=gradex-backend:hostinger-reviewed
  MOCK_IMAGE_SHA="$GRADEX_RELEASE_SHA"
  MOCK_TREE_SHA="$GRADEX_RELEASE_SHA"
  MOCK_TREE_CLEAN=true
  MOCK_ANCESTOR=true
  MOCK_MAX_SCHEMA=41
  MOCK_API_ID=
  MOCK_WORKER_ID=
  MOCK_PS_FAILURE=
  COMPOSE_CALL=
}

expect_refusal() {
  local description="$1" expected="$2" output
  shift 2
  if output="$(fixture; "$@"; rollback_schema_41_foundation 2>&1)"; then
    die "$description was accepted"
  fi
  case "$output" in *"$expected"*) ;; *) die "$description failed for the wrong reason: $output" ;; esac
  case "$output" in *COMPOSE*) die "$description reached Compose despite the refusal" ;; esac
}

set_api_present() { MOCK_API_ID="api-${1:-present}"; }
set_worker_present() { MOCK_WORKER_ID="worker-${1:-present}"; }
set_ps_failure() { MOCK_PS_FAILURE=worker; }
set_missing_project() { S12_PROJECT_DECLARED=; }
set_staging_project() { S12_PROJECT=gradex-staging; }
set_wrong_project() { S12_PROJECT=gradex-other; }
set_wrong_state_dir() { S12_HOST_STATE_DIR=/home/deploy/gradex-other; S12_ENV_FILE="$S12_HOST_STATE_DIR/runtime.env"; }
set_wrong_runtime_file() { S12_ENV_FILE=/home/deploy/gradex-production/other.env; }
set_wrong_database() { POSTGRES_DB=gradex_other; }
set_wrong_database_url() { DATABASE_URL='postgres://gradex:placeholder@postgres:5432/gradex_other?sslmode=disable'; }
set_wrong_database_host() { DATABASE_URL='postgres://gradex:placeholder@other-postgres:5432/gradex_production?sslmode=disable'; }
set_image_mismatch() { MOCK_IMAGE_SHA=2222222222222222222222222222222222222222; }
set_tree_mismatch() { MOCK_TREE_SHA=2222222222222222222222222222222222222222; }
set_wrong_tree_directory() { S12_ROOT="$ROOT/deploy/hostinger"; }
set_old_release() {
  GRADEX_RELEASE_SHA=a272011620296569f180a02c11c33fbbc8d97c73
  MOCK_IMAGE_SHA="$GRADEX_RELEASE_SHA"
  MOCK_TREE_SHA="$GRADEX_RELEASE_SHA"
  MOCK_ANCESTOR=false
}
set_dirty_tree() { MOCK_TREE_CLEAN=false; }
set_wrong_schema_ceiling() { MOCK_MAX_SCHEMA=40; }
set_staging_env() { APP_ENV=staging; }

# Presence alone refuses, regardless of the container's reported state.
for state in running restarting paused created exited dead removing unknown; do
  expect_refusal "worker container in state $state" "worker container still exists" set_worker_present "$state"
  expect_refusal "api container in state $state" "api container still exists" set_api_present "$state"
done
expect_refusal "Compose ps failure" "could not inspect worker containers" set_ps_failure
expect_refusal "missing production project" "must declare GRADEX_HOST_PROJECT" set_missing_project
expect_refusal "staging project" "may not use the staging Compose project" set_staging_project
expect_refusal "wrong project" "requires project gradex-production" set_wrong_project
expect_refusal "wrong state directory" "requires the production host state directory" set_wrong_state_dir
expect_refusal "wrong runtime file" "requires the production runtime.env" set_wrong_runtime_file
expect_refusal "wrong database" "requires the production database" set_wrong_database
expect_refusal "wrong database URL" "production DATABASE_URL names a different database" set_wrong_database_url
expect_refusal "wrong database host" "production DATABASE_URL must use this project's postgres service" set_wrong_database_host
expect_refusal "staging environment" "production-only" set_staging_env
expect_refusal "backend revision mismatch" "image revision mismatch" set_image_mismatch
expect_refusal "release tree mismatch" "release tree HEAD does not match" set_tree_mismatch
expect_refusal "wrong project source tree" "selected release lacks the dedicated rollback command" set_wrong_tree_directory
expect_refusal "older artifact without rollback" "predates the supervised schema 41 rollback" set_old_release
expect_refusal "dirty release tree" "release tree is not clean" set_dirty_tree
expect_refusal "wrong image schema ceiling" "selected backend image must target schema 41" set_wrong_schema_ceiling

(
  fixture
  rollback_schema_41_foundation >/dev/null
  [ "$COMPOSE_CALL" = 'run --rm --no-deps migrate gradex-migrate rollback-schema-41 -confirm-production=schema-41-to-40' ] ||
    die "the successful path did not invoke only the dedicated one-off rollback"
)

if output="$(fixture; set_old_release; start_core schema-41-foundation 2>&1)"; then
  die "the forward schema-41 entry accepted an older release"
fi
case "$output" in *COMPOSE*) die "the forward entry touched Compose before rejecting an older release" ;; esac

output="$(fixture; start_core schema-41-foundation)"
case "$output" in
  *'COMPOSE up --detach postgres redis'*'COMPOSE up --detach migrate'*'COMPOSE up --detach --no-deps api worker frontend'*) ;;
  *) die "the guarded forward entry did not migrate before starting application services" ;;
esac

printf 'schema-41-rollback-guard: forward identity, project, artifact and absent-container gates passed\n' >&2
