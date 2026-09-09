#!/usr/bin/env bash
#
# Deterministic proof of the ordering and the failure behaviour of
# `host.sh apply-schema-release`. Nothing here reaches Docker, a database, or a
# deployment: the command's real code is extracted from host.sh and run against
# a fake Compose runner and a fake Docker that record every action in order and
# model container and schema state well enough to make the ordering observable.
#
# What this proves is the property the D-103 release depends on: the old worker
# is stopped and proven stopped before the migration runs, and the new worker is
# never created while any other worker is running. It also proves that giving up
# waiting on a migration never becomes a silent success, and that container
# resolution distinguishes stale history from a live container.

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

notes_have() { grep --quiet --fixed-strings -- "$1" "$FAKE_DIR/notes"; }

# No application service may have been created.
assert_no_application_started() {
  log_absent "$FAKE_DIR/actions.log" "compose up api" &&
    log_absent "$FAKE_DIR/actions.log" "compose up worker" &&
    log_absent "$FAKE_DIR/actions.log" "compose up frontend"
}

# ---------------------------------------------------------------------------
# The fake deployment.
#
# Containers are modelled as records rather than as a single id per service,
# because the defects this proof exists to catch are precisely the ones a
# one-id-per-service model cannot express: a stale exited container sitting
# beside a live one, two running workers, or a migrate one-shot from a previous
# run. Each record is
#
#     service <TAB> id <TAB> running <TAB> exit-code <TAB> image
#
# and every ordered action is appended to the scenario's action log.
# ---------------------------------------------------------------------------

FAKE_DIR=""
ACTION_LOG=""

record() { printf '%s\n' "$*" >>"$ACTION_LOG"; }

fake_state() { cat "$FAKE_DIR/state/$1"; }
fake_set_state() { printf '%s' "$2" >"$FAKE_DIR/state/$1"; }

fc_store() { printf '%s' "$FAKE_DIR/state/containers"; }

fc_add() {
  printf '%s\t%s\t%s\t%s\t%s\n' "$1" "$2" "$3" "$4" "$5" >>"$(fc_store)"
}

fc_next_id() {
  local service="$1" n
  n="$(( $(cat "$FAKE_DIR/state/counter") + 1 ))"
  printf '%s' "$n" >"$FAKE_DIR/state/counter"
  printf 'cid-%s-%s' "$service" "$n"
}

fc_remove_service() {
  local service="$1" tmp="$FAKE_DIR/state/containers.tmp"
  awk -F '\t' -v s="$service" '$1 != s' "$(fc_store)" >"$tmp"
  mv -- "$tmp" "$(fc_store)"
}

fc_stop_service() {
  local service="$1" tmp="$FAKE_DIR/state/containers.tmp"
  awk -F '\t' -v OFS='\t' -v s="$service" '$1 == s { $3 = "false" } { print }' \
    "$(fc_store)" >"$tmp"
  mv -- "$tmp" "$(fc_store)"
}

fc_set_container() {
  local id="$1" running="$2" exit_code="$3" tmp="$FAKE_DIR/state/containers.tmp"
  awk -F '\t' -v OFS='\t' -v want="$id" -v r="$running" -v e="$exit_code" \
    '$2 == want { $3 = r; $4 = e } { print }' "$(fc_store)" >"$tmp"
  mv -- "$tmp" "$(fc_store)"
}

# Ids of a service. Scope is `running` or `all`.
fc_ids() {
  local service="$1" scope="$2"
  awk -F '\t' -v s="$service" -v scope="$scope" \
    '$1 == s && (scope == "all" || $3 == "true") { print $2 }' "$(fc_store)"
}

fc_field() {
  local id="$1" field="$2"
  awk -F '\t' -v want="$id" -v f="$field" '$2 == want { print $f; exit }' "$(fc_store)"
}

fc_service_of() { fc_field "$1" 1; }
fc_running() { fc_field "$1" 3; }
fc_exit() { fc_field "$1" 4; }
fc_image() { fc_field "$1" 5; }

# Creates the container a `--force-recreate` would, honouring the scenario's
# choice about whether the previous container is left behind as history.
fc_recreate() {
  local service="$1" image="$2" id
  [ "$(fake_state "keep_stale_$service")" = true ] || fc_remove_service "$service"
  id="$(fc_next_id "$service")"
  fc_add "$service" "$id" true 0 "$image"
  printf '%s' "$id"
}

# --- stubs. Defined after the real functions are eval'd so they win. ---

stub_note() {
  record "note: $*"
  printf 'host: %s\n' "$*" >>"$FAKE_DIR/notes"
}
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

stub_compose() {
  local args="$*" id
  case "$args" in
    "logs "*) return 0 ;;
    "stop worker")
      record "compose stop worker"
      [ "$(fake_state worker_stop_refuses)" = true ] && return 0
      fc_stop_service worker
      if [ "$(fake_state rogue_worker_survives_stop)" = true ]; then
        fc_set_container cid-worker-rogue-pre true 0
      fi
      return 0
      ;;
    "stop api")
      record "compose stop api"
      [ "$(fake_state api_stop_refuses)" = true ] && return 0
      fc_stop_service api
      return 0
      ;;
    *"--force-recreate migrate")
      record "compose up migrate image=${GRADEX_BACKEND_IMAGE}"
      if [ -n "$(fc_ids worker running)" ]; then
        record "VIOLATION: migration started while a worker was running"
      fi
      if [ -n "$(fc_ids api running)" ]; then
        record "VIOLATION: migration started while the API was running"
      fi
      id="$(fc_recreate migrate "$GRADEX_BACKEND_IMAGE")"
      record "migrate execution created: $id"
      if [ "$(fake_state worker_revives_after_migration)" = true ]; then
        fc_add worker cid-worker-revived true 0 gradex-backend:d102
      fi
      if [ "$(fake_state migrate_hangs)" = true ]; then
        # Still running when the wait bound elapses. The scenario may also
        # decide what the schema reads while that is true.
        fake_set_state schema "$(fake_state schema_while_hanging)"
        return 0
      fi
      if [ "$(fake_state migrate_fails)" = true ]; then
        fc_set_container "$id" false 1
        return 0
      fi
      fc_set_container "$id" false 0
      fake_set_state schema "$(fake_state migrate_result)"
      return 0
      ;;
    *"--force-recreate api")
      record "compose up api image=${GRADEX_BACKEND_IMAGE}"
      id="$(fc_recreate api "$(fake_state api_created_image)")"
      record "api created: $id"
      return 0
      ;;
    *"--force-recreate worker")
      record "compose up worker image=${GRADEX_BACKEND_IMAGE}"
      if [ -n "$(fc_ids worker running)" ]; then
        record "VIOLATION: a second worker was created while a worker was running"
      fi
      id="$(fc_recreate worker "$(fake_state worker_created_image)")"
      record "worker created: $id"
      if [ "$(fake_state rogue_worker_restarts_with_new_worker)" = true ]; then
        fc_set_container cid-worker-rogue-pre true 0
      fi
      return 0
      ;;
    *"--force-recreate frontend")
      record "compose up frontend image=${GRADEX_FRONTEND_IMAGE}"
      id="$(fc_recreate frontend "$GRADEX_FRONTEND_IMAGE")"
      record "frontend created: $id"
      return 0
      ;;
  esac
  record "compose $args"
  return 0
}

stub_docker() {
  local args="$*" service target sql id scope
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
    "ps "*)
      scope=running
      case "$args" in *" --all "*) scope=all ;; esac
      service="${args##*compose.service=}"
      fc_ids "$service" "$scope"
      return 0
      ;;
    "inspect --format {{.Config.Image}} "*)
      fc_image "${*: -1}"
      printf '\n'
      return 0
      ;;
    "inspect --format {{.RestartCount}} "*) printf '0\n'; return 0 ;;
    "inspect --format {{.State.ExitCode}} "*)
      fc_exit "${*: -1}"
      printf '\n'
      return 0
      ;;
    "inspect --format {{.State.Status}} "*)
      id="${*: -1}"
      [ -n "$(fc_service_of "$id")" ] || return 1
      [ "$(fc_running "$id")" = true ] && printf 'running\n' || printf 'exited\n'
      return 0
      ;;
    "inspect --format {{if .State.Health}}"*)
      id="${*: -1}"
      service="$(fc_service_of "$id")"
      [ -n "$service" ] || return 1
      if [ "$(fc_running "$id")" != true ]; then
        printf 'exited\n'
        return 0
      fi
      case "$service" in
        api)
          [ "$(fake_state api_healthy)" = true ] && printf 'healthy\n' || printf 'unhealthy\n'
          ;;
        frontend | postgres | redis) printf 'healthy\n' ;;
        *) printf 'running\n' ;;
      esac
      return 0
      ;;
    "exec "*wget*)
      id="$2"
      record "docker exec readiness probe on $id"
      [ "$(fake_state api_ready)" = true ] || return 1
      printf '{"status":"ok","checks":{"postgres":"ok","redis":"ok","schema":"ok"}}\n'
      return 0
      ;;
    "exec "*psql*)
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
  mkdir -p "$FAKE_DIR/state" "$FAKE_DIR/backups"
  : >"$ACTION_LOG"
  : >"$FAKE_DIR/notes"
  : >"$FAKE_DIR/state/containers"
  printf '0' >"$FAKE_DIR/state/counter"

  fake_set_state worker_stop_refuses false
  fake_set_state api_stop_refuses false
  fake_set_state backup_fails false
  fake_set_state migrate_fails false
  fake_set_state migrate_hangs false
  fake_set_state migrate_result '35|false'
  fake_set_state schema_while_hanging '34|false'
  fake_set_state schema '34|false'
  fake_set_state target_images_present true
  fake_set_state current_max_version 34
  fake_set_state target_max_version 35
  fake_set_state api_healthy true
  fake_set_state api_ready true
  fake_set_state api_created_image "$FAKE_TARGET_BACKEND"
  fake_set_state worker_created_image "$FAKE_TARGET_BACKEND"
  fake_set_state rogue_worker_restarts_with_new_worker false
  fake_set_state rogue_worker_survives_stop false
  fake_set_state worker_revives_after_migration false
  fake_set_state keep_stale_migrate false
  fake_set_state keep_stale_api false
  fake_set_state keep_stale_worker false
  fake_set_state keep_stale_frontend false
  fake_set_state migration_timeout ''

  # The live D-102 deployment this release replaces.
  fc_add postgres cid-postgres true 0 postgres:16
  fc_add redis cid-redis true 0 redis:7
  fc_add edge cid-edge true 0 caddy:2
  fc_add api cid-api-live true 0 gradex-backend:d102
  fc_add worker cid-worker-live true 0 gradex-backend:d102
  fc_add frontend cid-frontend-live true 0 gradex-frontend:d102

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
    if [ -n "$(fake_state migration_timeout)" ]; then
      GRADEX_SCHEMA_RELEASE_MIGRATION_TIMEOUT_SECONDS="$(fake_state migration_timeout)"
      export GRADEX_SCHEMA_RELEASE_MIGRATION_TIMEOUT_SECONDS
    fi

    die() { note "$*"; exit 1; }
    eval "$(extract_host_function manifest_value)"
    eval "$(extract_host_function image_max_schema_version)"
    eval "$(extract_host_function persist_release_selection)"
    eval "$(extract_host_function schema_release_note)"
    eval "$(extract_host_function schema_release_abort)"
    eval "$(extract_host_function schema_release_set_migration_timeout)"
    eval "$(extract_host_function schema_release_image_revision)"
    eval "$(extract_host_function schema_release_container_image_revision)"
    eval "$(extract_host_function schema_release_list_containers)"
    eval "$(extract_host_function schema_release_join_containers)"
    eval "$(extract_host_function schema_release_assert_stopped)"
    eval "$(extract_host_function schema_release_resolve_running_singleton)"
    eval "$(extract_host_function schema_release_resolve_created_container)"
    eval "$(extract_host_function schema_release_wait_container_status)"
    eval "$(extract_host_function schema_release_wait_migration)"
    eval "$(extract_host_function schema_release_report_migration_timeout)"
    eval "$(extract_host_function schema_release_schema_state)"
    eval "$(extract_host_function schema_release_assert_expected_columns)"
    eval "$(extract_host_function schema_release_report_media_in_flight)"
    eval "$(extract_host_function apply_schema_release)"

    # The wait bounds and their defaults come from host.sh, not from this proof.
    eval "$(grep '^S12_SCHEMA_RELEASE_MIGRATION_TIMEOUT_[A-Z]*=' "$S12_HOST_SCRIPT")"

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
  local base_apply current_apply path default_timeout

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

  # M-1 — the migration wait bound is its own value and is production-sized.
  default_timeout="$(sed -n 's/^S12_SCHEMA_RELEASE_MIGRATION_TIMEOUT_DEFAULT=//p' "$S12_HOST_SCRIPT")"
  check "the migration wait bound has a dedicated default" test -n "$default_timeout"
  check "the migration wait default is far longer than the generic 240s service wait" \
    test "${default_timeout:-0}" -gt 240
}

# ---------------------------------------------------------------------------

main() {
  local tool ordered
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
  ordered="$FAKE_DIR/actions.log"
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
    log_before "$ordered" "docker exec readiness probe" "compose up worker"
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
  check "the maintenance window is announced" notes_have 'MAINTENANCE WINDOW'
  check "the migration wait bound is announced" notes_have 'migration wait bound:'
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

  # --- the configured migration wait bound is validated before anything stops.
  new_scenario timeout_not_a_number
  fake_set_state migration_timeout 'abc'
  run_scenario 34 35
  check "a non-numeric migration wait bound is refused" test "$(scenario_status)" != 0
  check "a non-numeric wait bound stops nothing" log_absent "$FAKE_DIR/actions.log" "compose stop"
  check "a non-numeric wait bound backs nothing up" log_absent "$FAKE_DIR/actions.log" "create_backup"

  new_scenario timeout_too_small
  fake_set_state migration_timeout '5'
  run_scenario 34 35
  check "an implausibly short migration wait bound is refused" test "$(scenario_status)" != 0
  check "a too-short wait bound stops nothing" log_absent "$FAKE_DIR/actions.log" "compose stop"

  new_scenario timeout_too_large
  fake_set_state migration_timeout '999999'
  run_scenario 34 35
  check "an unbounded migration wait bound is refused" test "$(scenario_status)" != 0

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
  check "T17 an unstoppable old worker is reported as still RUNNING, not as ambiguous history" \
    notes_have 'are still RUNNING'

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
  check "T11 a failed migration starts nothing" assert_no_application_started
  check "T11 the operator is pointed at the runbook" notes_have 'docs/launch/RUNBOOK.md'

  # --- T18: the wait bound elapses while the migration is still going.
  new_scenario migration_timeout
  fake_set_state migrate_hangs true
  fake_set_state migration_timeout 60
  run_scenario 34 35
  check "T18 a migration that outlasts the wait bound aborts" test "$(scenario_status)" != 0
  check "T18 a migration timeout starts no application service" assert_no_application_started
  check "T18 the operator is told the migration may still be running" \
    notes_have 'THE MIGRATION MAY STILL BE RUNNING.'
  check "T18 the operator is told nothing was killed or removed" \
    notes_have 'has NOT stopped, killed or removed the migrate container'
  check "T18 the operator is told to inspect before acting" \
    notes_have 'inspect the migrate one-shot and the schema state before taking ANY recovery action'
  check "T18 no down migration is suggested or run" \
    log_absent "$FAKE_DIR/actions.log" "compose up migrate-down"
  check "T18 the migrate container is not removed" \
    log_absent "$FAKE_DIR/actions.log" "docker rm"
  check "T18 the migrate container is not killed" \
    log_absent "$FAKE_DIR/actions.log" "docker kill"
  check "T18 the operator is told how to allow longer migrations" \
    notes_have 'GRADEX_SCHEMA_RELEASE_MIGRATION_TIMEOUT_SECONDS'

  # --- T19: the diagnostic identifies the migrate container as still running.
  check "T19 the diagnostic reports the migrate container as running" \
    notes_have 'is currently running'
  check "T19 the timeout still starts nothing" assert_no_application_started

  # --- T20: the schema still reads the FROM version at the timeout.
  new_scenario migration_timeout_schema_from
  fake_set_state migrate_hangs true
  fake_set_state schema_while_hanging '34|false'
  fake_set_state migration_timeout 60
  run_scenario 34 35
  check "T20 a timeout with the schema still at FROM aborts" test "$(scenario_status)" != 0
  check "T20 the diagnostic reports the observed schema" notes_have 'schema_migrations currently reads 34|false'
  check "T20 nothing is started" assert_no_application_started

  # --- T21: the schema already reads TO at the timeout. The release is still
  # --- failed: an uncertain state must never become a silent success.
  new_scenario migration_timeout_schema_to
  fake_set_state migrate_hangs true
  fake_set_state schema_while_hanging '35|false'
  fake_set_state migration_timeout 60
  run_scenario 34 35
  check "T21 a timeout does not resume because the schema happens to read TO" \
    test "$(scenario_status)" != 0
  check "T21 a timeout with schema at TO starts nothing" assert_no_application_started
  check "T21 the diagnostic refuses to treat the reading as completion" \
    notes_have 'does NOT establish that the migration finished'
  check "T21 the release is still reported as failed" \
    notes_have 'this release remains FAILED regardless of what it says'

  # --- T13: the migration completed but left the wrong schema.
  new_scenario wrong_result_schema
  fake_set_state migrate_result '36|false'
  run_scenario 34 35
  check "T13 an unexpected resulting schema aborts" test "$(scenario_status)" != 0
  check "T13 an unexpected resulting schema starts nothing" assert_no_application_started

  # --- E: the migration left the schema dirty.
  new_scenario dirty_result_schema
  fake_set_state migrate_result '35|true'
  run_scenario 34 35
  check "a dirty resulting schema aborts" test "$(scenario_status)" != 0
  check "a dirty resulting schema starts nothing" assert_no_application_started

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

  # --- T27: the started container does not carry the target revision.
  new_scenario api_wrong_revision
  fake_set_state api_created_image gradex-backend:d102
  run_scenario 34 35
  check "T27 an API on the wrong revision fails" test "$(scenario_status)" != 0
  check "T27 the error names the revision mismatch" notes_have 'the running API carries revision'
  check "T27 a wrong-revision API starts no worker" \
    log_absent "$FAKE_DIR/actions.log" "compose up worker"

  new_scenario worker_wrong_revision
  fake_set_state worker_created_image gradex-backend:d102
  run_scenario 34 35
  check "T27 a worker on the wrong revision fails" test "$(scenario_status)" != 0
  check "T27 the error names the worker revision mismatch" \
    notes_have 'the running worker carries revision'

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

  # --- T22: several historical stopped workers are not a violation.
  new_scenario stale_stopped_workers
  fc_add worker cid-worker-history-1 false 0 gradex-backend:d100
  fc_add worker cid-worker-history-2 false 0 gradex-backend:d101
  fake_set_state keep_stale_worker true
  run_scenario 34 35
  check "T22 multiple stopped historical workers still prove stopped" \
    test "$(scenario_status)" = 0
  check "T22 stopped history is reported as history, not as a violation" \
    notes_have 'stopped container(s) remain'
  check "T22 the release still completed in order" \
    log_before "$FAKE_DIR/actions.log" "compose up migrate" "compose up worker"

  # --- T23: one stopped worker beside one running worker is a violation.
  new_scenario stale_plus_live_worker
  fc_add worker cid-worker-history-1 false 0 gradex-backend:d100
  fc_add worker cid-worker-rogue-pre true 0 gradex-backend:d102
  fake_set_state rogue_worker_survives_stop true
  run_scenario 34 35
  check "T23 a running worker beside stopped history fails the stopped proof" \
    test "$(scenario_status)" != 0
  check "T23 the error names the running container, not ambiguous history" \
    notes_have 'are still RUNNING'
  check "T23 no migration ran" log_absent "$FAKE_DIR/actions.log" "compose up migrate"

  # --- T24: more than one running worker after startup fails hard.
  new_scenario multiple_running_workers
  fc_add worker cid-worker-rogue-pre false 0 gradex-backend:d102
  fake_set_state keep_stale_worker true
  fake_set_state rogue_worker_restarts_with_new_worker true
  run_scenario 34 35
  check "T24 multiple running workers fail the release" test "$(scenario_status)" != 0
  check "T24 the error names the cardinality problem" \
    notes_have 'containers are running, which makes its identity ambiguous'
  check "T24 the operator is told the overlap must be resolved" \
    notes_have 'workers from two releases must never overlap'
  check "T24 the frontend is never started over an ambiguous worker topology" \
    log_absent "$FAKE_DIR/actions.log" "compose up frontend"

  # --- T25: a stale exited migrate container must not be read as this run's
  # --- result. The current execution hangs, so anything that inspected the
  # --- stale exited-zero container instead would report success.
  new_scenario stale_migrate_container
  fc_add migrate cid-migrate-stale false 0 gradex-backend:d102
  fake_set_state keep_stale_migrate true
  fake_set_state migrate_hangs true
  fake_set_state migration_timeout 60
  run_scenario 34 35
  check "T25 a stale exited migrate container is not mistaken for this execution" \
    test "$(scenario_status)" != 0
  check "T25 the timeout is reported rather than a false success" \
    notes_have 'THE MIGRATION MAY STILL BE RUNNING.'
  check "T25 the tracked execution is not the stale container" \
    log_absent "$FAKE_DIR/actions.log" "tracking migration execution cid-migrate-s"
  check "T25 nothing is started" assert_no_application_started

  # --- T26: a stale stopped API container must not be evaluated for readiness.
  new_scenario stale_api_container
  fc_add api cid-api-stale false 0 gradex-backend:d101
  fake_set_state keep_stale_api true
  run_scenario 34 35
  check "T26 a stale stopped API container does not break the release" \
    test "$(scenario_status)" = 0
  check "T26 readiness was evaluated against the container this command created" \
    log_has "$FAKE_DIR/actions.log" "docker exec readiness probe on cid-api-"
  check "T26 readiness was not evaluated against stale history" \
    log_absent "$FAKE_DIR/actions.log" "docker exec readiness probe on cid-api-stale"

  verify_unchanged_guards

  if [ "$S12_FAILURES" -ne 0 ]; then
    die "$S12_FAILURES schema-release assertion(s) failed"
  fi
  note "every schema-release ordering, failure-boundary, timeout and cardinality assertion passed"
}

main "$@"
