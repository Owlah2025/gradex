#!/usr/bin/env bash
#
# Deterministic proof of the ordering and the failure behaviour of
# `host.sh apply-schema-release`. Nothing here reaches Docker, a database, or a
# deployment: the command's real code is extracted from host.sh and run against
# a fake Compose runner and a fake Docker that record every action in order and
# model just enough container and schema state to make the ordering observable.
#
# What this proves is the property the D-103 release depends on: the old worker
# is stopped and proven stopped before the migration runs, and the new worker is
# never created while any other worker is running.

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_HOST_SCRIPT="$S12_ROOT/deploy/hostinger/host.sh"
# The reviewed D-103 head this tooling was written against. The regression
# assertions compare today's guards against exactly that tree.
S12_BASE_COMMIT=ef733f758cf7d58e2689898a70715e2fd2102e24
S12_TEMPORARY=""
S12_FAILURES=0

note() { printf 'schema-release-proof: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

cleanup() {
  if [ -n "$S12_TEMPORARY" ] && [ -d "$S12_TEMPORARY" ]; then
    rm -rf -- "$S12_TEMPORARY"
  fi
}

pass() { printf 'schema-release-proof: PASS %s\n' "$*" >&2; }
fail() {
  printf 'schema-release-proof: FAIL %s\n' "$*" >&2
  S12_FAILURES=$((S12_FAILURES + 1))
}

check() {
  local label="$1"
  shift
  if "$@"; then pass "$label"; else fail "$label"; fi
}

extract_host_function() {
  local function_name="$1"
  awk -v function_name="$function_name" '
    $0 ~ "^" function_name "\\(\\) \\{" { capture = 1 }
    capture { print }
    capture && /^}/ { exit }
  ' "$S12_HOST_SCRIPT"
}

# ---------------------------------------------------------------------------
# Ordering assertions over the recorded action log.
# ---------------------------------------------------------------------------

log_has() { grep --quiet --fixed-strings -- "$2" "$1"; }
log_absent() { ! grep --quiet --fixed-strings -- "$2" "$1"; }

log_index() {
  local file="$1" entry="$2" index
  index="$(grep --line-number --fixed-strings -- "$entry" "$file" | head -n 1 | cut -d: -f1)"
  [ -n "$index" ] || return 1
  printf '%s' "$index"
}

log_before() {
  local file="$1" first="$2" second="$3" a b
  a="$(log_index "$file" "$first")" || return 1
  b="$(log_index "$file" "$second")" || return 1
  [ "$a" -lt "$b" ]
}

# ---------------------------------------------------------------------------
# The fake deployment. One scenario directory holds the modelled container and
# schema state plus the ordered action log.
# ---------------------------------------------------------------------------

FAKE_DIR=""
ACTION_LOG=""

record() { printf '%s\n' "$*" >>"$ACTION_LOG"; }

fake_state() { cat "$FAKE_DIR/state/$1"; }
fake_set_state() { printf '%s' "$2" >"$FAKE_DIR/state/$1"; }

# Stubs. Defined after the real functions are eval'd so they win.
stub_note() { record "note: $*"; printf 'host: %s\n' "$*" >>"$FAKE_DIR/notes"; }
stub_sleep() { :; }

stub_require_tools() { record "require_tools"; }
stub_load_environment() { record "load_environment"; }
stub_validate_environment() { record "validate_environment"; }

stub_create_backup() {
  record "create_backup"
  if [ "$(fake_state backup_fails)" = true ]; then
    record "create_backup:FAILED"
    return 1
  fi
  date +%s >"$FAKE_DIR/backups/latest.completed-at"
}

# The fake Compose runner. It performs the state transitions a real
# `docker compose` would, so the log reflects genuine cause and effect.
stub_compose() {
  local args="$*"
  case "$args" in
    *"ps --all --quiet"*)
      printf 'cid-%s\n' "${*: -1}"
      return 0
      ;;
    "logs "*) return 0 ;;
    "stop worker")
      record "compose stop worker"
      [ "$(fake_state worker_stop_refuses)" = true ] && return 0
      fake_set_state worker_running false
      return 0
      ;;
    "stop api")
      record "compose stop api"
      [ "$(fake_state api_stop_refuses)" = true ] && return 0
      fake_set_state api_running false
      return 0
      ;;
    *"--force-recreate migrate")
      record "compose up migrate image=${GRADEX_BACKEND_IMAGE}"
      # The property under test: a migration must never observe a live worker.
      if [ "$(fake_state worker_running)" = true ]; then
        record "VIOLATION: migration started while a worker was running"
      fi
      if [ "$(fake_state api_running)" = true ]; then
        record "VIOLATION: migration started while the API was running"
      fi
      if [ "$(fake_state worker_revives_after_migration)" = true ]; then
        fake_set_state worker_running true
      fi
      if [ "$(fake_state migrate_fails)" = true ]; then
        fake_set_state migrate_status 1
        return 0
      fi
      fake_set_state migrate_status 0
      fake_set_state schema "$(fake_state migrate_result)"
      return 0
      ;;
    *"--force-recreate api")
      record "compose up api image=${GRADEX_BACKEND_IMAGE}"
      fake_set_state api_running true
      return 0
      ;;
    *"--force-recreate worker")
      record "compose up worker image=${GRADEX_BACKEND_IMAGE}"
      if [ "$(fake_state worker_running)" = true ]; then
        record "VIOLATION: a second worker was created while a worker was running"
      fi
      fake_set_state worker_running true
      return 0
      ;;
    *"--force-recreate frontend")
      record "compose up frontend image=${GRADEX_FRONTEND_IMAGE}"
      fake_set_state frontend_running true
      return 0
      ;;
  esac
  record "compose $args"
  return 0
}

fake_service_running() {
  case "$1" in
    worker) fake_state worker_running ;;
    api) fake_state api_running ;;
    frontend) fake_state frontend_running ;;
    postgres | redis | edge) printf 'true' ;;
    migrate) printf 'false' ;;
    *) printf 'false' ;;
  esac
}

stub_docker() {
  local args="$*" service target sql
  case "$args" in
    "image inspect --format"*)
      target="${*: -1}"
      case "$target" in
        "$FAKE_TARGET_BACKEND" | "$FAKE_TARGET_FRONTEND" | "$FAKE_TARGET_PROOF")
          printf '%s\n' "$FAKE_TARGET_REVISION"
          ;;
        *) printf '%s\n' "$FAKE_CURRENT_REVISION" ;;
      esac
      return 0
      ;;
    "image inspect "*)
      record "docker image inspect"
      [ "$(fake_state target_images_present)" = true ] || return 1
      return 0
      ;;
    "run --rm --entrypoint gradex-migrate "*)
      target="$5"
      if [ "$target" = "$FAKE_TARGET_BACKEND" ]; then
        fake_state target_max_version
      else
        fake_state current_max_version
      fi
      printf '\n'
      return 0
      ;;
    "ps --quiet "*)
      service="${args##*compose.service=}"
      [ "$(fake_service_running "$service")" = true ] && printf 'cid-%s\n' "$service"
      return 0
      ;;
    "inspect --format {{.State.Running}} "*)
      fake_service_running "${*: -1}" | sed 's/^/&/'
      printf '\n'
      return 0
      ;;
    "inspect --format {{.Config.Image}} "*)
      service="${*: -1}"
      service="${service#cid-}"
      case "$service" in
        frontend) printf '%s\n' "$(fake_state frontend_image)" ;;
        *) printf '%s\n' "$(fake_state backend_image)" ;;
      esac
      return 0
      ;;
    "inspect --format {{.RestartCount}} "*) printf '0\n'; return 0 ;;
    "inspect --format {{.State.Status}} "*)
      service="${*: -1}"
      service="${service#cid-}"
      if [ "$service" = migrate ]; then printf 'exited\n'; else
        [ "$(fake_service_running "$service")" = true ] && printf 'running\n' || printf 'exited\n'
      fi
      return 0
      ;;
    "inspect --format {{.State.ExitCode}} "*)
      fake_state migrate_status
      printf '\n'
      return 0
      ;;
    "inspect --format {{if .State.Health}}"*)
      service="${*: -1}"
      service="${service#cid-}"
      case "$service" in
        api)
          if [ "$(fake_service_running api)" != true ]; then printf 'exited\n'
          elif [ "$(fake_state api_healthy)" = true ]; then printf 'healthy\n'
          else printf 'unhealthy\n'; fi
          ;;
        frontend)
          [ "$(fake_service_running frontend)" = true ] && printf 'healthy\n' || printf 'exited\n'
          ;;
        worker | edge)
          [ "$(fake_service_running "$service")" = true ] && printf 'running\n' || printf 'exited\n'
          ;;
        *) printf 'healthy\n' ;;
      esac
      return 0
      ;;
    "exec cid-api wget"*)
      record "docker exec api readiness probe"
      [ "$(fake_state api_ready)" = true ] || return 1
      printf '{"status":"ok","checks":{"postgres":"ok","redis":"ok","schema":"ok"}}\n'
      return 0
      ;;
    "exec cid-postgres psql"*)
      sql="${*: -1}"
      case "$sql" in
        *schema_migrations*) fake_state schema; printf '\n' ;;
        *information_schema.columns*) record "docker exec column assertion"; printf '1\n' ;;
        *media_asset_versions*) record "docker exec media in-flight observation"; printf '0|0\n' ;;
        *) printf '\n' ;;
      esac
      return 0
      ;;
  esac
  record "docker $args"
  return 0
}

# ---------------------------------------------------------------------------
# Scenario driver.
# ---------------------------------------------------------------------------

FAKE_TARGET_BACKEND=gradex-backend:d103
FAKE_TARGET_FRONTEND=gradex-frontend:d103
FAKE_TARGET_PROOF=gradex-proof:d103
FAKE_TARGET_REVISION=ef733f758cf7d58e2689898a70715e2fd2102e24
FAKE_CURRENT_REVISION=b8dea967196de68914440b2092cd80daf85d9546

new_scenario() {
  local name="$1"
  FAKE_DIR="$S12_TEMPORARY/$name"
  ACTION_LOG="$FAKE_DIR/actions.log"
  mkdir -p "$FAKE_DIR/state" "$FAKE_DIR/backups" "$FAKE_DIR/gradex-proof"
  : >"$ACTION_LOG"
  : >"$FAKE_DIR/notes"

  fake_set_state worker_running true
  fake_set_state api_running true
  fake_set_state frontend_running true
  fake_set_state worker_stop_refuses false
  fake_set_state worker_revives_after_migration false
  fake_set_state api_stop_refuses false
  fake_set_state backup_fails false
  fake_set_state migrate_fails false
  fake_set_state migrate_status 0
  fake_set_state migrate_result '35|false'
  fake_set_state schema '34|false'
  fake_set_state target_images_present true
  fake_set_state current_max_version 34
  fake_set_state target_max_version 35
  fake_set_state api_healthy true
  fake_set_state api_ready true
  fake_set_state backend_image "$FAKE_TARGET_BACKEND"
  fake_set_state frontend_image "$FAKE_TARGET_FRONTEND"

  cat >"$FAKE_DIR/release.env" <<EOF
GRADEX_RELEASE_SHA=$FAKE_TARGET_REVISION
GRADEX_BACKEND_IMAGE=$FAKE_TARGET_BACKEND
GRADEX_FRONTEND_IMAGE=$FAKE_TARGET_FRONTEND
GRADEX_PROOF_IMAGE=$FAKE_TARGET_PROOF
EOF
  cat >"$FAKE_DIR/runtime.env" <<EOF
GRADEX_RELEASE_SHA=$FAKE_CURRENT_REVISION
GRADEX_BACKEND_IMAGE=gradex-backend:d102
GRADEX_FRONTEND_IMAGE=gradex-frontend:d102
GRADEX_PROOF_IMAGE=gradex-proof:d102
EOF
  chmod 600 "$FAKE_DIR/runtime.env"
}

# Runs the real apply_schema_release in a subshell so an abort is catchable and
# the scenario's stubs cannot leak into the next one.
run_scenario() {
  local from="${1:-34}" to="${2:-35}" status=0
  (
    set -euo pipefail
    S12_PROJECT=gradex-production
    S12_HOST_STATE_DIR="$FAKE_DIR"
    S12_ENV_FILE="$FAKE_DIR/runtime.env"
    S12_BACKUP_DIR="$FAKE_DIR/backups"
    POSTGRES_DB=gradex
    GRADEX_RELEASE_SHA="$FAKE_CURRENT_REVISION"
    GRADEX_BACKEND_IMAGE=gradex-backend:d102
    GRADEX_FRONTEND_IMAGE=gradex-frontend:d102
    GRADEX_PROOF_IMAGE=gradex-proof:d102
    GRADEX_SCHEMA_RELEASE_EXPECTED_COLUMNS=media_asset_versions.work_claim_token

    die() { note "$*"; exit 1; }
    eval "$(extract_host_function manifest_value)"
    eval "$(extract_host_function service_id)"
    eval "$(extract_host_function image_max_schema_version)"
    eval "$(extract_host_function require_status)"
    eval "$(extract_host_function persist_release_selection)"
    eval "$(extract_host_function schema_release_note)"
    eval "$(extract_host_function schema_release_abort)"
    eval "$(extract_host_function schema_release_image_revision)"
    eval "$(extract_host_function schema_release_container_image_revision)"
    eval "$(extract_host_function schema_release_running_service_containers)"
    eval "$(extract_host_function schema_release_assert_stopped)"
    eval "$(extract_host_function schema_release_wait_status)"
    eval "$(extract_host_function schema_release_wait_completion)"
    eval "$(extract_host_function schema_release_schema_state)"
    eval "$(extract_host_function schema_release_assert_expected_columns)"
    eval "$(extract_host_function schema_release_report_media_in_flight)"
    eval "$(extract_host_function apply_schema_release)"

    note() { stub_note "$@"; }
    sleep() { stub_sleep "$@"; }
    require_tools() { stub_require_tools "$@"; }
    load_environment() { stub_load_environment "$@"; }
    validate_environment() { stub_validate_environment "$@"; }
    create_backup() { stub_create_backup "$@"; }
    compose() { stub_compose "$@"; }
    docker() { stub_docker "$@"; }

    apply_schema_release "$FAKE_DIR/release.env" "$from" "$to"
  ) >>"$FAKE_DIR/output.log" 2>&1 || status=$?
  printf '%s' "$status" >"$FAKE_DIR/status"
  return 0
}

scenario_status() { cat "$FAKE_DIR/status"; }

# ---------------------------------------------------------------------------
# Static regressions: nothing this change touches may have weakened a guard.
# ---------------------------------------------------------------------------

verify_unchanged_guards() {
  local base_apply current_apply path

  # T16 — apply-release stays application-only, byte for byte.
  base_apply="$S12_TEMPORARY/apply-release.base"
  current_apply="$S12_TEMPORARY/apply-release.current"
  git -C "$S12_ROOT" show "$S12_BASE_COMMIT:deploy/hostinger/host.sh" >"$S12_TEMPORARY/host.base.sh"
  awk '/^apply_release\(\) \{/ { c = 1 } c { print } c && /^}/ { exit }' \
    "$S12_TEMPORARY/host.base.sh" >"$base_apply"
  extract_host_function apply_release >"$current_apply"
  [ -s "$base_apply" ] || die "the base apply_release could not be extracted"
  check "T16 apply-release semantics are unchanged" cmp --silent "$base_apply" "$current_apply"
  check "T16 apply-release still runs no migration" \
    bash -c '! grep --quiet --fixed-strings "gradex-migrate" "$1"' _ "$current_apply"

  # T15 — every production down guard is untouched.
  for path in backend/cmd/migrate/main.go deploy/scripts/application-rollback.sh; do
    if git -C "$S12_ROOT" diff --quiet "$S12_BASE_COMMIT" -- "$path"; then
      pass "T15 $path is unchanged"
    else
      fail "T15 $path changed"
    fi
  done
  check "T15 the production down refusal is still present" \
    grep --quiet --fixed-strings 'down migrations are not permitted when APP_ENV=production' \
    "$S12_ROOT/backend/cmd/migrate/main.go"
  # Comment lines are stripped first: this command's own commentary explains why
  # `gradex-migrate down` is not a production path, and saying so must not read
  # as adding one.
  sed 's/#.*$//' "$S12_HOST_SCRIPT" >"$S12_TEMPORARY/host.code.sh"
  check "T15 no down-migration escape hatch was added to host.sh" \
    bash -c '! grep --quiet -E -- "--force-down|--skip-safety|migrate.{0,4}down" "$1"' _ "$S12_TEMPORARY/host.code.sh"
  extract_host_function apply_schema_release >"$S12_TEMPORARY/apply-schema-release.body"
  sed 's/#.*$//' "$S12_TEMPORARY/apply-schema-release.body" >"$S12_TEMPORARY/apply-schema-release.code"
  check "T15 the new command never invokes a down migration" \
    bash -c '! grep --quiet -E -- "gradex-migrate|migrate.{0,4}down|--force-down|--skip-safety" "$1"' _ \
    "$S12_TEMPORARY/apply-schema-release.code"
}

# ---------------------------------------------------------------------------

main() {
  local tool
  for tool in awk cmp git grep mktemp sed; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done
  bash -n "$S12_HOST_SCRIPT" || die "host.sh does not parse"

  S12_TEMPORARY="$(mktemp -d)"
  trap cleanup EXIT
  if [ -n "${GRADEX_PROOF_KEEP:-}" ]; then trap - EXIT; note "scenario directory: $S12_TEMPORARY"; fi

  # --- the happy path, which every ordering assertion reads.
  new_scenario ordered
  run_scenario 34 35
  local ordered="$FAKE_DIR/actions.log"
  check "the ordered release succeeds" test "$(scenario_status)" = 0
  check "T5 the old worker is stopped before the migration" \
    log_before "$ordered" "compose stop worker" "compose up migrate"
  check "T6 the old API is stopped before the migration" \
    log_before "$ordered" "compose stop api" "compose up migrate"
  check "T7 no migration ever ran while a worker was running" \
    log_absent "$ordered" "VIOLATION: migration started while a worker was running"
  check "T7 no migration ever ran while the API was running" \
    log_absent "$ordered" "VIOLATION: migration started while the API was running"
  check "T8 the migration completes before the new API is created" \
    log_before "$ordered" "compose up migrate" "compose up api"
  check "T9 the new API answers readiness before the new worker is created" \
    log_before "$ordered" "docker exec api readiness probe" "compose up worker"
  check "T10 the frontend is created after the worker" \
    log_before "$ordered" "compose up worker" "compose up frontend"
  check "T17 no second worker was ever created alongside a running one" \
    log_absent "$ordered" "VIOLATION: a second worker was created while a worker was running"
  check "the backup precedes every container stop" \
    log_before "$ordered" "create_backup" "compose stop worker"
  check "the migration used the target release image" \
    log_has "$ordered" "compose up migrate image=$FAKE_TARGET_BACKEND"
  check "the API was created from the target release image" \
    log_has "$ordered" "compose up api image=$FAKE_TARGET_BACKEND"
  check "the frontend was created from the target release image" \
    log_has "$ordered" "compose up frontend image=$FAKE_TARGET_FRONTEND"
  check "the post-migration schema objects were asserted" \
    log_has "$ordered" "docker exec column assertion"
  check "in-flight media was observed read-only before maintenance" \
    log_has "$ordered" "docker exec media in-flight observation"
  check "the maintenance window is announced" \
    grep --quiet --fixed-strings 'MAINTENANCE WINDOW' "$FAKE_DIR/notes"
  check "the release selection was persisted" \
    grep --quiet --fixed-strings "GRADEX_RELEASE_SHA=$FAKE_TARGET_REVISION" "$FAKE_DIR/runtime.env"

  # --- T1: the live schema is not the declared FROM schema.
  new_scenario wrong_schema
  fake_set_state schema '33|false'
  run_scenario 34 35
  check "T1 a wrong current schema is refused" test "$(scenario_status)" != 0
  check "T1 nothing was backed up" log_absent "$FAKE_DIR/actions.log" "create_backup"
  check "T1 nothing was stopped" log_absent "$FAKE_DIR/actions.log" "compose stop"

  # --- T2: a dirty schema is a previous failed migration.
  new_scenario dirty_schema
  fake_set_state schema '34|true'
  run_scenario 34 35
  check "T2 a dirty schema is refused" test "$(scenario_status)" != 0
  check "T2 nothing was stopped" log_absent "$FAKE_DIR/actions.log" "compose stop"

  # --- T3: the target artifacts are not loaded.
  new_scenario missing_images
  fake_set_state target_images_present false
  run_scenario 34 35
  check "T3 missing target artifacts are refused" test "$(scenario_status)" != 0
  check "T3 nothing was backed up" log_absent "$FAKE_DIR/actions.log" "create_backup"
  check "T3 nothing was stopped" log_absent "$FAKE_DIR/actions.log" "compose stop"

  # --- T14: the target images do not carry the declared release revision.
  new_scenario wrong_revision
  FAKE_TARGET_REVISION=0000000000000000000000000000000000000000
  run_scenario 34 35
  FAKE_TARGET_REVISION=ef733f758cf7d58e2689898a70715e2fd2102e24
  check "T14 a release identity mismatch fails closed" test "$(scenario_status)" != 0
  check "T14 nothing was stopped" log_absent "$FAKE_DIR/actions.log" "compose stop"

  # --- the single-forward-step guard.
  new_scenario two_steps
  run_scenario 34 36
  check "a two-version schema jump is refused" test "$(scenario_status)" != 0
  check "a two-version jump stops nothing" log_absent "$FAKE_DIR/actions.log" "compose stop"

  # --- the running release must actually be the FROM release.
  new_scenario wrong_running_release
  fake_set_state current_max_version 33
  run_scenario 34 35
  check "a running release that is not the FROM release is refused" test "$(scenario_status)" != 0
  check "that refusal stops nothing" log_absent "$FAKE_DIR/actions.log" "compose stop"

  # --- T4: backup failure must precede and prevent every stop.
  new_scenario backup_failure
  fake_set_state backup_fails true
  run_scenario 34 35
  check "T4 a failed backup aborts" test "$(scenario_status)" != 0
  check "T4 a failed backup stops no container" log_absent "$FAKE_DIR/actions.log" "compose stop"
  check "T4 a failed backup runs no migration" log_absent "$FAKE_DIR/actions.log" "compose up migrate"

  # --- B/T17: the old worker cannot be proven stopped.
  new_scenario worker_stop_failure
  fake_set_state worker_stop_refuses true
  run_scenario 34 35
  check "T17 an unstoppable old worker aborts" test "$(scenario_status)" != 0
  check "T17 an unstoppable old worker prevents the migration" \
    log_absent "$FAKE_DIR/actions.log" "compose up migrate"
  check "T17 an unstoppable old worker prevents the API stop" \
    log_absent "$FAKE_DIR/actions.log" "compose stop api"

  # --- C: the old API cannot be proven stopped.
  new_scenario api_stop_failure
  fake_set_state api_stop_refuses true
  run_scenario 34 35
  check "an unstoppable old API aborts" test "$(scenario_status)" != 0
  check "an unstoppable old API prevents the migration" \
    log_absent "$FAKE_DIR/actions.log" "compose up migrate"

  # --- T11: the migration fails.
  new_scenario migration_failure
  fake_set_state migrate_fails true
  run_scenario 34 35
  check "T11 a failed migration aborts" test "$(scenario_status)" != 0
  check "T11 a failed migration starts no API" log_absent "$FAKE_DIR/actions.log" "compose up api"
  check "T11 a failed migration starts no worker" log_absent "$FAKE_DIR/actions.log" "compose up worker"
  check "T11 the operator is pointed at the runbook" \
    grep --quiet --fixed-strings 'docs/launch/RUNBOOK.md' "$FAKE_DIR/notes"

  # --- T13: the migration completed but left the wrong schema.
  new_scenario wrong_result_schema
  fake_set_state migrate_result '36|false'
  run_scenario 34 35
  check "T13 an unexpected resulting schema aborts" test "$(scenario_status)" != 0
  check "T13 an unexpected resulting schema starts no API" \
    log_absent "$FAKE_DIR/actions.log" "compose up api"
  check "T13 an unexpected resulting schema starts no worker" \
    log_absent "$FAKE_DIR/actions.log" "compose up worker"

  # --- E: the migration left the schema dirty.
  new_scenario dirty_result_schema
  fake_set_state migrate_result '35|true'
  run_scenario 34 35
  check "a dirty resulting schema aborts" test "$(scenario_status)" != 0
  check "a dirty resulting schema starts no worker" \
    log_absent "$FAKE_DIR/actions.log" "compose up worker"

  # --- T12: the new API never becomes healthy.
  new_scenario api_unhealthy
  fake_set_state api_healthy false
  run_scenario 34 35
  check "T12 an unhealthy new API aborts" test "$(scenario_status)" != 0
  check "T12 an unhealthy new API starts no worker" \
    log_absent "$FAKE_DIR/actions.log" "compose up worker"

  # --- F: the new API is healthy but not ready.
  new_scenario api_not_ready
  fake_set_state api_ready false
  run_scenario 34 35
  check "an unready new API aborts" test "$(scenario_status)" != 0
  check "an unready new API starts no worker" \
    log_absent "$FAKE_DIR/actions.log" "compose up worker"

  # --- T17: an old worker that comes back to life after the migration must be
  # --- caught by the second overlap proof, immediately before the new worker.
  new_scenario worker_revives
  fake_set_state worker_revives_after_migration true
  run_scenario 34 35
  check "T17 a revived old worker aborts before the new worker" test "$(scenario_status)" != 0
  check "T17 a revived old worker means no new worker is created" \
    log_absent "$FAKE_DIR/actions.log" "compose up worker"
  check "T17 no overlapping worker was ever created" \
    log_absent "$FAKE_DIR/actions.log" "VIOLATION: a second worker was created while a worker was running"

  # --- G: the new worker carries the wrong release.
  new_scenario worker_wrong_revision
  fake_set_state backend_image gradex-backend:d102
  run_scenario 34 35
  check "a worker from another release fails the release" test "$(scenario_status)" != 0

  verify_unchanged_guards

  if [ "$S12_FAILURES" -ne 0 ]; then
    die "$S12_FAILURES schema-release assertion(s) failed"
  fi
  note "every schema-release ordering, failure-boundary and guard assertion passed"
}

main "$@"
