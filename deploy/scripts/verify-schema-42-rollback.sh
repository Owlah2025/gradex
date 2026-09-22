#!/usr/bin/env bash
# Exercise the Hostinger schema-42 cutover and rollback gates without starting a
# container, reaching Redis, or touching a database.
#
# The point of this guard is the ORDER of refusals. Every gate below must decide
# before Compose is asked to run anything, because a rollback that reaches
# golang-migrate has already left the schema marker dirty, and one that reaches
# the migration with enhancement work still pending would produce a schema-41
# database whose media dispatcher is permanently blocked.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOST_SCRIPT="$ROOT/deploy/hostinger/host.sh"

die() { printf 'schema-42-rollback-guard: %s\n' "$*" >&2; exit 1; }
note() { :; }

extract() {
  awk -v name="$1" '
    $0 ~ "^" name "\\(\\)" { capture = 1 }
    capture { print }
    capture && /^}/ { exit }
  ' "$HOST_SCRIPT"
}

for function_name in assert_production_project_scope start_core require_absent \
  require_production_migration_target require_schema42_production_target \
  require_schema42_release_identity read_schema_state require_schema_clean_at \
  require_no_active_media_claims require_enhancement_drain \
  rollback_schema_42_enhancement_recovery; do
  definition="$(extract "$function_name")"
  [ -n "$definition" ] || die "missing $function_name in host.sh"
  eval "$definition"
done
grep --quiet --fixed-strings 'up-core-schema-42-enhancement-recovery) [ "$#" = 1 ] || usage; start_core schema-42-enhancement-recovery' "$HOST_SCRIPT" ||
  die "guarded forward command is not dispatched"
grep --quiet --fixed-strings 'rollback-schema-42-enhancement-recovery) [ "$#" = 1 ] || usage; rollback_schema_42_enhancement_recovery' "$HOST_SCRIPT" ||
  die "dedicated rollback command is not dispatched"
# No generic production downgrade may be forwarded from this wrapper. Comments
# are excluded: the prohibition is documented in host.sh precisely because it is
# deliberate, and the guard is about executable lines.
! grep --extended-regexp --quiet '^[^#]*gradex-migrate ([a-z-]*[^a-z-])?(down|migrate)([^a-z-]|$)' "$HOST_SCRIPT" ||
  die "host.sh forwards a generic migration command"
! grep --quiet --fixed-strings 'ALLOW_DOWN' "$HOST_SCRIPT" ||
  die "host.sh introduces a generic downgrade escape hatch"

# The exact external seams used by the extracted functions. Their fixtures are
# values only; no docker, migration, Redis or production connection runs.
require_tools() { :; }
load_environment() { :; }
prepare() { :; }
wait_for_status() { :; }
wait_for_completion() { :; }
validate_environment() {
  assert_production_project_scope
  [ "$MOCK_IMAGE_SHA" = "$GRADEX_RELEASE_SHA" ] || die "image revision mismatch"
}
require_status() {
  case "$1 $2" in
    'postgres healthy'|'redis healthy') [ "${MOCK_UNHEALTHY:-}" != "$1" ] || die "$1 is unhealthy" ;;
    *) die "unexpected status probe $1 $2" ;;
  esac
}
image_max_schema_version() { printf '%s\n' "$MOCK_MAX_SCHEMA"; }
# Artifact identity and the executable no-Git paths are exercised end-to-end by
# verify-deploy-bundle.py; this guard retains the scope, quiescence and drain set.
require_release_artifact() {
  [ "$1" = "$SCHEMA42_BUNDLE_CAPABILITY" ] || die "wrong release boundary capability requested"
  [ "$MOCK_ARTIFACT_VALID" = true ] || die "invalid release artifact"
}
require_schema42_image_capability() { [ "$MOCK_IMAGE_CAPABLE" = true ] || die "backend lacks the supervised schema 42 rollback command"; }
require_schema41_application_floor() { [ "$MOCK_FLOOR_STAGED" = true ] || die "the schema-41 application rollback floor $SCHEMA41_APPLICATION_FLOOR is not staged"; }
require_no_local_production_workers() { :; }
service_id() {
  [ "${MOCK_PS_FAILURE:-}" != "$1" ] || return 1
  case "$1" in
    api) printf '%s' "$MOCK_API_ID" ;;
    worker) printf '%s' "$MOCK_WORKER_ID" ;;
    postgres) printf '%s' "$MOCK_POSTGRES_ID" ;;
    *) return 1 ;;
  esac
}
docker() {
  [ "$1" = exec ] || die "unexpected docker call: $*"
  case "$*" in
    *schema_migrations*) printf '%s\n' "$MOCK_SCHEMA_STATE" ;;
    *work_claim_token*) printf '%s\n' "$MOCK_ACTIVE_CLAIMS" ;;
    *) die "unexpected docker exec: $*" ;;
  esac
}
compose() {
  COMPOSE_CALLS="${COMPOSE_CALLS:+$COMPOSE_CALLS
}$*"
  case "$*" in
    'run --rm --no-deps migrate gradex-enhancement-drain')
      [ "$MOCK_DRAIN_CLEAN" = true ] || return 1
      ;;
    'run --rm --no-deps migrate gradex-migrate rollback-schema-42 -confirm-production=schema-42-to-41')
      [ "$MOCK_DOWN_SUCCEEDS" = true ] || return 1
      # Only a completed DOWN moves the marker; a refused one must leave 42.
      MOCK_SCHEMA_STATE="$MOCK_SCHEMA_STATE_AFTER_DOWN"
      ;;
    'up --detach migrate') MOCK_SCHEMA_STATE="$MOCK_SCHEMA_STATE_AFTER_UP" ;;
    'up --detach --no-deps api worker frontend') MOCK_WORKER_ID=worker ;;
  esac
  printf 'COMPOSE %s\n' "$*"
}

fixture() {
  S12_ROOT="$ROOT"
  SCHEMA41_BUNDLE_CAPABILITY=SCHEMA41_CAPABILITY=supervised-41-to-40-v1
  SCHEMA42_BUNDLE_CAPABILITY=SCHEMA42_CAPABILITY=manual-enhancement-v1
  SCHEMA41_APPLICATION_FLOOR=98e88fcc1105e8c638bb638d3f1c46630bcc51b2
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
  GRADEX_BACKEND_IMAGE=gradex-backend:hostinger-candidate
  MOCK_IMAGE_SHA="$GRADEX_RELEASE_SHA"
  MOCK_ARTIFACT_VALID=true
  MOCK_IMAGE_CAPABLE=true
  MOCK_FLOOR_STAGED=true
  MOCK_MAX_SCHEMA=42
  MOCK_API_ID=
  MOCK_WORKER_ID=
  MOCK_POSTGRES_ID=postgres-container
  MOCK_PS_FAILURE=
  MOCK_UNHEALTHY=
  MOCK_SCHEMA_STATE='42|false'
  MOCK_SCHEMA_STATE_AFTER_DOWN='41|false'
  MOCK_SCHEMA_STATE_AFTER_UP='42|false'
  MOCK_ACTIVE_CLAIMS=0
  MOCK_DRAIN_CLEAN=true
  MOCK_DOWN_SUCCEEDS=true
  COMPOSE_CALLS=
}

# The forward cutover starts from the running 3C-A release, so its database is a
# clean 41 rather than the 42 the rollback fixture describes.
forward_fixture() { fixture; MOCK_SCHEMA_STATE='41|false'; }

# A refusal must not have reached the migration one-off. Reaching the read-only
# drain proof is acceptable for gates that sit after it; reaching
# `gradex-migrate` never is.
expect_refusal() {
  local description="$1" expected="$2" output
  shift 2
  if output="$(fixture; "$@"; rollback_schema_42_enhancement_recovery 2>&1)"; then
    die "$description was accepted"
  fi
  case "$output" in *"$expected"*) ;; *) die "$description failed for the wrong reason: $output" ;; esac
  case "$output" in *gradex-migrate*) die "$description reached the migration command despite the refusal" ;; esac
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
set_staging_env() { APP_ENV=staging; }
set_image_mismatch() { MOCK_IMAGE_SHA=2222222222222222222222222222222222222222; }
set_mixed_artifact() { MOCK_ARTIFACT_VALID=false; }
set_missing_floor() { MOCK_FLOOR_STAGED=false; }
set_incapable_image() { MOCK_IMAGE_CAPABLE=false; }
set_schema41_ceiling() { MOCK_MAX_SCHEMA=41; }
set_schema43_ceiling() { MOCK_MAX_SCHEMA=43; }
set_unhealthy_postgres() { MOCK_UNHEALTHY=postgres; }
set_unhealthy_redis() { MOCK_UNHEALTHY=redis; }
set_schema41() { MOCK_SCHEMA_STATE='41|false'; }
set_schema_dirty() { MOCK_SCHEMA_STATE='42|true'; }
set_schema43() { MOCK_SCHEMA_STATE='43|false'; }
set_active_claims() { MOCK_ACTIVE_CLAIMS=3; }
set_pending_enhancement_work() { MOCK_DRAIN_CLEAN=false; }
set_down_failure() { MOCK_DOWN_SUCCEEDS=false; }
set_down_lands_wrong() { MOCK_SCHEMA_STATE_AFTER_DOWN='40|false'; }
set_down_lands_dirty() { MOCK_SCHEMA_STATE_AFTER_DOWN='41|true'; }

# --- production scope and release identity ---------------------------------
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
expect_refusal "mixed or stale artifact" "invalid release artifact" set_mixed_artifact
expect_refusal "missing application rollback floor" "rollback floor 98e88fcc1105e8c638bb638d3f1c46630bcc51b2 is not staged" set_missing_floor
expect_refusal "image without the supervised command" "lacks the supervised schema 42 rollback command" set_incapable_image
expect_refusal "baseline image schema ceiling" "selected backend image must target schema 42" set_schema41_ceiling
expect_refusal "future image schema ceiling" "selected backend image must target schema 42" set_schema43_ceiling

# --- quiescence -------------------------------------------------------------
for state in running restarting paused created exited dead removing unknown; do
  expect_refusal "worker container in state $state" "worker container still exists" set_worker_present "$state"
  expect_refusal "api container in state $state" "api container still exists" set_api_present "$state"
done
expect_refusal "Compose ps failure" "could not inspect worker containers" set_ps_failure
expect_refusal "unhealthy postgres" "postgres is unhealthy" set_unhealthy_postgres
expect_refusal "unhealthy redis" "redis is unhealthy" set_unhealthy_redis
expect_refusal "media work still claimed" "still hold a work claim" set_active_claims

# --- schema preconditions ---------------------------------------------------
expect_refusal "schema already at 41" "expected a clean 42" set_schema41
expect_refusal "dirty schema 42" "schema is dirty" set_schema_dirty
expect_refusal "schema beyond 42" "expected a clean 42" set_schema43

# --- enhancement drain: the hard gate --------------------------------------
expect_refusal "pending enhancement work" "enhancement work is not drained" set_pending_enhancement_work
# The drain is a proof, not a cleanup: rollback must never discard work an
# Administrator asked for, so no executable line may delete or purge queue or
# outbox state.
! grep --extended-regexp --quiet '^[^#]*(DeleteTask|DeleteAllPendingTasks|asynq.*(delete|purge)|DELETE FROM (outbox_events|media_outbox_dispatches))' "$HOST_SCRIPT" ||
  die "host.sh deletes enhancement work as part of a rollback"

# --- the DOWN step itself ---------------------------------------------------
expect_failure_after_down() {
  local description="$1" expected="$2" output
  shift 2
  if output="$(fixture; "$@"; rollback_schema_42_enhancement_recovery 2>&1)"; then
    die "$description was accepted"
  fi
  case "$output" in *"$expected"*) ;; *) die "$description failed for the wrong reason: $output" ;; esac
  case "$output" in *rollback-schema-41*) die "$description cascaded toward schema 40" ;; esac
}
expect_failure_after_down "a failed DOWN" "do not start the schema-41 application until the marker reads a clean 41" set_down_failure
expect_failure_after_down "a DOWN that overshot to 40" "expected a clean 41" set_down_lands_wrong
expect_failure_after_down "a DOWN that left the marker dirty" "schema is dirty" set_down_lands_dirty

# --- the successful path ----------------------------------------------------
(
  fixture
  rollback_schema_42_enhancement_recovery >/dev/null
  [ "$COMPOSE_CALLS" = 'run --rm --no-deps migrate gradex-enhancement-drain
run --rm --no-deps migrate gradex-migrate rollback-schema-42 -confirm-production=schema-42-to-41' ] ||
    die "the successful path did not run exactly the drain proof then the dedicated rollback: $COMPOSE_CALLS"
)

# --- the forward cutover ----------------------------------------------------
(
  # No way back means no way forward.
  if output="$(forward_fixture; MOCK_FLOOR_STAGED=false; start_core schema-42-enhancement-recovery 2>&1)"; then
    die "the forward entry accepted a missing schema-41 application rollback floor"
  fi
  case "$output" in *COMPOSE*) die "the forward entry touched Compose before rejecting a missing rollback floor" ;; esac
)
(
  if output="$(forward_fixture; MOCK_MAX_SCHEMA=41; start_core schema-42-enhancement-recovery 2>&1)"; then
    die "the forward entry accepted a schema-41 backend image"
  fi
  case "$output" in *COMPOSE*) die "the forward entry touched Compose before rejecting a schema-41 image" ;; esac
)
(
  # Migration must not run against anything but a clean 41.
  if output="$(forward_fixture; MOCK_SCHEMA_STATE='40|false'; start_core schema-42-enhancement-recovery 2>&1)"; then
    die "the forward entry migrated from an unexpected schema"
  fi
  case "$output" in *'up --detach migrate'*) die "the forward entry migrated despite the wrong starting schema" ;; esac
)
(
  # A migration that does not land on a clean 42 must not start the application:
  # the 3C-B worker requires 42 and the 3C-A application must never see it.
  if output="$(forward_fixture; MOCK_SCHEMA_STATE_AFTER_UP='41|false'; start_core schema-42-enhancement-recovery 2>&1)"; then
    die "the forward entry started the application on an unverified schema"
  fi
  case "$output" in *'up --detach --no-deps api worker frontend'*) die "the forward entry started the application tier without a clean 42" ;; esac
)
(
  output="$(forward_fixture; MOCK_WORKER_ID=; start_core schema-42-enhancement-recovery)"
  case "$output" in
    *'COMPOSE up --detach postgres redis'*'COMPOSE up --detach migrate'*'COMPOSE up --detach --no-deps api worker frontend'*) ;;
    *) die "the guarded forward entry did not migrate before starting application services" ;;
  esac
)

printf 'schema-42-rollback-guard: identity, scope, quiescence, schema, drain and cutover-ordering gates passed\n' >&2
