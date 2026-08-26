#!/usr/bin/env bash

set -Eeuo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_MONITOR="$S12_ROOT/deploy/monitoring/monitor-once.sh"
S12_HOST="$S12_ROOT/deploy/hostinger/host.sh"
S12_TEMPORARY=""

note() { printf 'med-monitoring: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

cleanup() {
  if [ -n "$S12_TEMPORARY" ] && [ -d "$S12_TEMPORARY" ]; then
    rm -rf -- "$S12_TEMPORARY"
  fi
}

assert_log_contains() {
  local expected="$1"
  grep --fixed-strings --quiet "$expected" "$monitor_log" || {
    sed -n '1,160p' "$monitor_log" >&2
    die "monitor output is missing: $expected"
  }
}

assert_log_absent() {
  local forbidden="$1"
  if grep --fixed-strings --quiet "$forbidden" "$monitor_log"; then
    sed -n '1,160p' "$monitor_log" >&2
    die "monitor output exposed forbidden data: $forbidden"
  fi
}

write_report() {
  local worker_status="$1" worker_detail="$2" email_mode="$3" email_detail="$4" disk_status="$5" disk_detail="$6"
  printf 'version=1\n' >"$runtime_report"
  printf 'worker|%s|%s\n' "$worker_status" "$worker_detail" >>"$runtime_report"
  printf 'postgres_schema|PASS|fixture PostgreSQL schema\n' >>"$runtime_report"
  if [ "$email_mode" = FAIL ]; then
    printf 'email_outbox|FAIL|%s\n' "$email_detail" >>"$runtime_report"
  else
    printf 'email_outbox|METRICS|%s\n' "$email_detail" >>"$runtime_report"
  fi
  printf 'disk_roots|%s|%s\n' "$disk_status" "$disk_detail" >>"$runtime_report"
  chmod 600 "$runtime_report"
}

write_disk_fixture() {
  : >"$disk_fixture"
  local line
  for line in "$@"; do
    printf '%s\n' "$line" >>"$disk_fixture"
  done
  chmod 600 "$disk_fixture"
}

run_direct_monitor() {
  local expected_status="$1"
  set +e
  PATH="$fake_bin:$PATH" \
    FAKE_ROOT_STATUS="${FAKE_ROOT_STATUS:-200}" \
    FAKE_HEALTH_STATUS="${FAKE_HEALTH_STATUS:-200}" \
    FAKE_READY_STATUS="${FAKE_READY_STATUS:-200}" \
    GRADEX_PUBLIC_URL=https://monitor.test/ \
    GRADEX_HEALTH_URL=https://monitor.test/healthz \
    GRADEX_READY_URL=https://monitor.test/readyz \
    GRADEX_ENVIRONMENT=monitor-test \
    GRADEX_MONITOR_RUNTIME_REPORT="$runtime_report" \
    GRADEX_MONITOR_DISK_PATHS="$disk_paths" \
    GRADEX_MONITOR_DISK_FIXTURE="$disk_fixture" \
    GRADEX_MONITOR_EMAIL_STALE_SECONDS="${GRADEX_MONITOR_EMAIL_STALE_SECONDS:-3600}" \
    GRADEX_MONITOR_DISK_WARN_PERCENT="${GRADEX_MONITOR_DISK_WARN_PERCENT:-85}" \
    GRADEX_MONITOR_DISK_CRITICAL_PERCENT="${GRADEX_MONITOR_DISK_CRITICAL_PERCENT:-95}" \
    GRADEX_MONITOR_DISK_MIN_FREE_BYTES="$disk_min_free_bytes" \
    GRADEX_BACKUP_COMPLETED_AT_FILE="${GRADEX_BACKUP_COMPLETED_AT_FILE:-}" \
    GRADEX_BACKUP_MAX_FUTURE_SECONDS="${GRADEX_BACKUP_MAX_FUTURE_SECONDS:-300}" \
    GRADEX_ALERT_WEBHOOK_URL= \
    "$S12_MONITOR" >"$monitor_log" 2>&1
  monitor_status=$?
  set -e
  [ "$monitor_status" = "$expected_status" ] || {
    sed -n '1,160p' "$monitor_log" >&2
    die "monitor status=$monitor_status, expected=$expected_status"
  }
}

write_fake_curl() {
  cat >"$fake_bin/curl" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
output=""
write_status=0
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) output="$2"; shift 2 ;;
    --write-out) write_status=1; shift 2 ;;
    --config|--header|--data|--cacert) shift 2 ;;
    *) url="$1"; shift ;;
  esac
done
[ -z "$output" ] || : >"$output"
if [ "$write_status" = 1 ]; then
  case "$url" in
    https://monitor.test/) printf '%s' "${FAKE_ROOT_STATUS:-200}" ;;
    */healthz) printf '%s' "${FAKE_HEALTH_STATUS:-200}" ;;
    */readyz) printf '%s' "${FAKE_READY_STATUS:-200}" ;;
    *) printf '200' ;;
  esac
fi
EOF
  chmod 0755 "$fake_bin/curl"
}

write_fake_docker() {
  cat >"$fake_bin/docker" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'docker %s\n' "$*" >>"${FAKE_DOCKER_LOG:-/dev/null}"
case "${1:-}" in
  info)
    case "$*" in
      *--format*) printf '%s\n' "${FAKE_DOCKER_ROOT:?FAKE_DOCKER_ROOT is required}" ;;
    esac
    ;;
  compose)
    service=""
    for argument in "$@"; do service="$argument"; done
    case "$service" in
      worker) [ -z "${FAKE_WORKER_ID-workercontainer}" ] || printf '%s\n' "${FAKE_WORKER_ID-workercontainer}" ;;
      postgres) [ -z "${FAKE_POSTGRES_ID-postgrescontainer}" ] || printf '%s\n' "${FAKE_POSTGRES_ID-postgrescontainer}" ;;
      api) [ -z "${FAKE_API_ID-apicontainer}" ] || printf '%s\n' "${FAKE_API_ID-apicontainer}" ;;
      *) exit 1 ;;
    esac
    ;;
  inspect)
    format=""
    target=""
    while [ "$#" -gt 0 ]; do
      if [ "$1" = --format ]; then format="$2"; shift 2; else target="$1"; shift; fi
    done
    [ "$target" = "${FAKE_ABSENT_CONTAINER:-}" ] && exit 1
    if [ "$format" = '{{.Id}}' ]; then
      case "$target" in
        *postgres*) printf 'postgrescontainer\n' ;;
        *api*) printf 'apicontainer\n' ;;
        *) printf 'workercontainer\n' ;;
      esac
    elif [[ "$format" == *Config.Labels* ]]; then
      case "$target" in
        *postgres*) cat "$FAKE_DOCKER_POSTGRES_LABELS_FILE" ;;
        *api*) cat "$FAKE_DOCKER_API_LABELS_FILE" ;;
        *) cat "$FAKE_DOCKER_WORKER_LABELS_FILE" ;;
      esac
    elif [ "$format" = '{{.Config.Image}}' ]; then
      printf '%s\n' "${FAKE_DOCKER_BACKEND_IMAGE:-fixture-backend}"
    else
      exit 1
    fi
    ;;
  exec)
    case "$*" in
      *schema_migrations*)
        [ "${FAKE_DOCKER_SCHEMA_FAILURE:-0}" = 1 ] && exit 1
        printf '%s\n' "${FAKE_DOCKER_SCHEMA_STATE:-28|false}"
        ;;
      *)
        [ "${FAKE_DOCKER_EMAIL_FAILURE:-0}" = 1 ] && exit 1
        printf '%s\n' "${FAKE_DOCKER_EMAIL_METRICS:-0|-1|-1}"
        ;;
    esac
    ;;
  run)
    [ "${FAKE_DOCKER_SCHEMA_VERSION_FAILURE:-0}" = 1 ] && exit 1
    printf '%s\n' "${FAKE_DOCKER_SCHEMA_VERSION:-28}"
    ;;
  *) exit 1 ;;
esac
EOF
  chmod 0755 "$fake_bin/docker"
}

run_host_monitor() {
  local expected_status="$1"
  set +e
  PATH="$fake_bin:$PATH" \
    FAKE_DOCKER_ROOT="${FAKE_DOCKER_ROOT-$host_state}" \
    FAKE_DOCKER_WORKER_LABELS_FILE="$docker_labels_file" \
    FAKE_DOCKER_POSTGRES_LABELS_FILE="$postgres_labels_file" \
    FAKE_DOCKER_API_LABELS_FILE="$api_labels_file" \
    FAKE_DOCKER_STATUS_FILE="$docker_status_file" \
    FAKE_DOCKER_LOG="$fake_docker_log" \
    FAKE_DOCKER_EMAIL_FAILURE="${FAKE_DOCKER_EMAIL_FAILURE:-0}" \
    FAKE_DOCKER_SCHEMA_FAILURE="${FAKE_DOCKER_SCHEMA_FAILURE:-0}" \
    FAKE_DOCKER_SCHEMA_STATE="${FAKE_DOCKER_SCHEMA_STATE:-28|false}" \
    FAKE_DOCKER_SCHEMA_VERSION="${FAKE_DOCKER_SCHEMA_VERSION:-28}" \
    FAKE_DOCKER_SCHEMA_VERSION_FAILURE="${FAKE_DOCKER_SCHEMA_VERSION_FAILURE:-0}" \
    FAKE_WORKER_ID="${FAKE_WORKER_ID-workercontainer}" \
    FAKE_POSTGRES_ID="${FAKE_POSTGRES_ID-postgrescontainer}" \
    FAKE_API_ID="${FAKE_API_ID-apicontainer}" \
    FAKE_DOCKER_BACKEND_IMAGE="${FAKE_DOCKER_BACKEND_IMAGE:-fixture-backend}" \
    FAKE_ABSENT_CONTAINER="${FAKE_ABSENT_CONTAINER:-}" \
    GRADEX_HOST_STATE_DIR="$host_state" \
    GRADEX_HOST_ENV_FILE="${GRADEX_HOST_ENV_FILE-$host_runtime_env}" \
    GRADEX_HOST_PROJECT=gradex-monitor-test \
    GRADEX_MONITOR_COMPOSE_PROJECT="${GRADEX_MONITOR_COMPOSE_PROJECT:-}" \
    GRADEX_MONITOR_WORKER_CONTAINER="${GRADEX_MONITOR_WORKER_CONTAINER:-}" \
    GRADEX_MONITOR_POSTGRES_CONTAINER="${GRADEX_MONITOR_POSTGRES_CONTAINER:-}" \
    GRADEX_MONITOR_API_CONTAINER="${GRADEX_MONITOR_API_CONTAINER:-}" \
    "$S12_HOST" monitor >"$host_monitor_log" 2>&1
  monitor_status=$?
  set -e
  [ "$monitor_status" = "$expected_status" ] || {
    [ -f "$fake_docker_log" ] && sed -n '1,80p' "$fake_docker_log" >&2 || true
    sed -n '1,160p' "$host_monitor_log" >&2
    die "host monitor status=$monitor_status, expected=$expected_status"
  }
}

main() {
  local tool
  for tool in awk cat chmod date grep jq mktemp rm sed stat; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done
  [ -x "$S12_MONITOR" ] || die "monitor script is not executable"
  [ -x "$S12_HOST" ] || die "Hostinger wrapper is not executable"

  S12_TEMPORARY="$(mktemp -d)"
  chmod 700 "$S12_TEMPORARY"
  trap cleanup EXIT
  fake_bin="$S12_TEMPORARY/fake-bin"
  mkdir -p "$fake_bin"
  runtime_report="$S12_TEMPORARY/runtime-report"
  disk_fixture="$S12_TEMPORARY/disk-fixture"
  monitor_log="$S12_TEMPORARY/monitor.log"
  host_monitor_log="$S12_TEMPORARY/host-monitor.log"
  fake_docker_log="$S12_TEMPORARY/fake-docker.log"
  disk_paths=/gradex
  disk_min_free_bytes=5
  write_fake_curl
  write_fake_docker

  write_report PASS "fixture owned worker" metrics "terminal=0;oldest_due_age=-1;stale_lease_age=-1" PASS "fixture paths"
  write_disk_fixture '/gradex|dev-a|30|100'
  run_direct_monitor 0
  assert_log_contains 'PASS api_root'
  assert_log_contains 'PASS worker: fixture owned worker'
  assert_log_contains 'PASS email_outbox: terminal_failures=0 oldest_due_age=-1s stale_lease_age=-1s'

  FAKE_ROOT_STATUS=500 run_direct_monitor 1
  assert_log_contains 'FAIL api_root: HTTP status 500'
  assert_log_contains 'checks failed and no alert webhook is configured'
  unset FAKE_ROOT_STATUS

  FAKE_READY_STATUS=503 run_direct_monitor 1
  assert_log_contains 'FAIL api_readiness: HTTP status 503'
  unset FAKE_READY_STATUS

  write_report FAIL "owned Compose worker state=exited" metrics "terminal=0;oldest_due_age=-1;stale_lease_age=-1" PASS "fixture paths"
  run_direct_monitor 1
  assert_log_contains 'FAIL worker: owned Compose worker state=exited'

  write_report PASS "fixture owned worker" metrics "terminal=0;oldest_due_age=-1;stale_lease_age=-1" PASS "fixture paths"
  run_direct_monitor 0

  write_report PASS "fixture owned worker" metrics "terminal=0;oldest_due_age=3601;stale_lease_age=-1" PASS "fixture paths"
  run_direct_monitor 1
  assert_log_contains 'FAIL email_outbox: oldest_due_age=3601s>3600s'

  write_report PASS "fixture owned worker" metrics "terminal=1;oldest_due_age=-1;stale_lease_age=-1" PASS "fixture paths"
  run_direct_monitor 1
  assert_log_contains 'FAIL email_outbox: terminal_failures=1'

  write_report PASS "fixture owned worker" metrics "terminal=0;oldest_due_age=-1;stale_lease_age=1" PASS "fixture paths"
  run_direct_monitor 1
  assert_log_contains 'FAIL email_outbox: oldest_stale_lease_age=1s'

  write_report PASS "fixture owned worker" FAIL "PostgreSQL transactional email query failed" PASS "fixture paths"
  run_direct_monitor 1
  assert_log_contains 'FAIL email_outbox: PostgreSQL transactional email query failed'
  assert_log_absent 'recipient@example.test'
  assert_log_absent 'PRIVATE_CREDENTIAL_CANARY'

  write_report PASS "fixture owned worker" metrics "terminal=0;oldest_due_age=-1;stale_lease_age=-1" PASS "fixture paths"
  write_disk_fixture '/gradex|dev-a|15|100'
  run_direct_monitor 0
  assert_log_contains 'WARN disk: path=/gradex device=dev-a'

  write_disk_fixture '/gradex|dev-a|5|100'
  run_direct_monitor 1
  assert_log_contains 'FAIL disk: path=/gradex device=dev-a'

  disk_min_free_bytes=50
  write_disk_fixture '/gradex|dev-a|40|100'
  run_direct_monitor 1
  assert_log_contains 'FAIL disk: path=/gradex device=dev-a'
  disk_min_free_bytes=5

  disk_paths=/gradex:/docker:/media
  write_disk_fixture '/gradex|dev-a|30|100' '/docker|dev-a|30|100' '/media|dev-b|30|100'
  run_direct_monitor 0
  pass_count="$(grep -c 'PASS disk:' "$monitor_log" || true)"
  [ "$pass_count" = 2 ] || die "disk device deduplication evaluated $pass_count filesystems, want 2"

  disk_paths=/missing
  write_disk_fixture '/missing|ERROR|0|100'
  run_direct_monitor 1
  assert_log_contains 'FAIL disk: path=/missing is unreadable'

  disk_paths=/gradex
  write_report FAIL "owned worker unavailable" metrics "terminal=1;oldest_due_age=3601;stale_lease_age=4" PASS "fixture paths"
  write_disk_fixture '/gradex|dev-a|5|100'
  FAKE_HEALTH_STATUS=500 run_direct_monitor 1
  assert_log_contains 'FAIL api_health: HTTP status 500'
  assert_log_contains 'FAIL worker: owned worker unavailable'
  assert_log_contains 'FAIL email_outbox: terminal_failures=1; oldest_stale_lease_age=4s; oldest_due_age=3601s>3600s'
  assert_log_contains 'FAIL disk: path=/gradex device=dev-a'
  unset FAKE_HEALTH_STATUS

  write_report PASS "fixture owned worker" metrics "terminal=0;oldest_due_age=-1;stale_lease_age=-1" PASS "fixture paths"
  write_disk_fixture '/gradex|dev-a|30|100'
  backup_marker="$S12_TEMPORARY/direct-backup-marker"
  GRADEX_BACKUP_COMPLETED_AT_FILE="$backup_marker" run_direct_monitor 1
  assert_log_contains 'FAIL backup: completion marker is missing'

  printf 'not-a-timestamp\n' >"$backup_marker"
  GRADEX_BACKUP_COMPLETED_AT_FILE="$backup_marker" run_direct_monitor 1
  assert_log_contains 'FAIL backup: completion marker is malformed'

  printf '%s\n' "$(( $(date +%s) + 301 ))" >"$backup_marker"
  GRADEX_BACKUP_COMPLETED_AT_FILE="$backup_marker" run_direct_monitor 1
  assert_log_contains 'FAIL backup: completion marker is too far in the future'

  printf '%s\n' "$(( $(date +%s) - 7201 ))" >"$backup_marker"
  GRADEX_BACKUP_COMPLETED_AT_FILE="$backup_marker" run_direct_monitor 1
  assert_log_contains 'FAIL backup: completion marker is stale'

  GRADEX_MONITOR_DISK_WARN_PERCENT=95 GRADEX_MONITOR_DISK_CRITICAL_PERCENT=95 run_direct_monitor 2
  assert_log_contains 'GRADEX_MONITOR_DISK_WARN_PERCENT must be lower than GRADEX_MONITOR_DISK_CRITICAL_PERCENT'

  write_report PASS "fixture owned worker" metrics "terminal=0;oldest_due_age=-1;stale_lease_age=-1" PASS "fixture paths"
  write_disk_fixture '/gradex|dev-a|30|100'
  run_direct_monitor 0

  host_state="$S12_TEMPORARY/gradex-monitor"
  host_runtime_env="$host_state/runtime.env"
  docker_labels_file="$S12_TEMPORARY/docker-labels"
  postgres_labels_file="$S12_TEMPORARY/postgres-labels"
  api_labels_file="$S12_TEMPORARY/api-labels"
  docker_status_file="$S12_TEMPORARY/docker-status"
  mkdir -p "$host_state/backups"
  chmod 700 "$host_state" "$host_state/backups"
  printf 'GRADEX_MONITOR_DISK_PATHS=\nGRADEX_MONITOR_EMAIL_STALE_SECONDS=3600\nGRADEX_MONITOR_DISK_WARN_PERCENT=85\nGRADEX_MONITOR_DISK_CRITICAL_PERCENT=95\nGRADEX_MONITOR_DISK_MIN_FREE_BYTES=1\nPUBLIC_ORIGIN=https://monitor.test\nPOSTGRES_DB=gradex_test\nGRADEX_BACKEND_IMAGE=fixture-backend\n' >"$host_runtime_env"
  chmod 600 "$host_runtime_env"
  printf '%s\n' "$(date +%s)" >"$host_state/backups/latest.completed-at"
  printf 'gradex-monitor-test|worker|running\n' >"$docker_labels_file"
  printf 'gradex-monitor-test|postgres|running\n' >"$postgres_labels_file"
  printf 'gradex-monitor-test|api|running\n' >"$api_labels_file"
  printf 'running\n' >"$docker_status_file"
  run_host_monitor 0

  host_runtime_without_backend="$host_state/runtime-without-backend.env"
  grep --invert-match '^GRADEX_BACKEND_IMAGE=' "$host_runtime_env" >"$host_runtime_without_backend"
  chmod 600 "$host_runtime_without_backend"
  : >"$fake_docker_log"
  GRADEX_HOST_ENV_FILE="$host_runtime_without_backend" run_host_monitor 0
  grep --fixed-strings --quiet 'postgres_schema: intended Compose PostgreSQL schema is clean' "$host_monitor_log" ||
    die "backend image fallback through the owned API container was not accepted"
  grep --fixed-strings --quiet 'docker exec postgrescontainer psql' "$fake_docker_log" ||
    die "schema query did not remain on the selected PostgreSQL container"

  FAKE_WORKER_ID= run_host_monitor 1
  grep --fixed-strings --quiet 'FAIL worker: owned Compose worker container is absent' "$host_monitor_log" ||
    die "missing worker was not reported"

  FAKE_DOCKER_EMAIL_FAILURE=1 run_host_monitor 1
  grep --fixed-strings --quiet 'FAIL email_outbox: PostgreSQL transactional email query failed' "$host_monitor_log" ||
    die "database query failure was not reported"
  unset FAKE_DOCKER_EMAIL_FAILURE

  FAKE_DOCKER_SCHEMA_FAILURE=1 run_host_monitor 1
  grep --fixed-strings --quiet 'FAIL postgres_schema: PostgreSQL schema query failed' "$host_monitor_log" ||
    die "schema query failure was not reported"
  unset FAKE_DOCKER_SCHEMA_FAILURE

  FAKE_DOCKER_SCHEMA_STATE=not-a-schema-state run_host_monitor 1
  grep --fixed-strings --quiet 'FAIL postgres_schema: PostgreSQL returned malformed schema state' "$host_monitor_log" ||
    die "malformed schema state was not reported"
  unset FAKE_DOCKER_SCHEMA_STATE

  printf 'other-stack|worker|running\n' >"$docker_labels_file"
  run_host_monitor 1
  grep --fixed-strings --quiet 'FAIL worker: owned Compose labels do not match the configured project' "$host_monitor_log" ||
    die "unrelated Compose project was not rejected"

  printf 'gradex-monitor-test|worker|exited\n' >"$docker_labels_file"
  printf 'exited\n' >"$docker_status_file"
  run_host_monitor 1
  grep --fixed-strings --quiet 'FAIL worker: owned Compose worker state=exited' "$host_monitor_log" ||
    die "owned worker-down state was not rejected"

  printf 'gradex-monitor-test|worker|running\n' >"$docker_labels_file"
  printf 'running\n' >"$docker_status_file"
  run_host_monitor 0

  : >"$fake_docker_log"
  printf 'gradex-founder-beta|worker|running\n' >"$docker_labels_file"
  printf 'gradex-founder-beta|postgres|running\n' >"$postgres_labels_file"
  printf 'gradex-founder-beta|api|running\n' >"$api_labels_file"
  GRADEX_HOST_ENV_FILE="$host_runtime_without_backend" \
    GRADEX_MONITOR_COMPOSE_PROJECT=gradex-founder-beta \
    GRADEX_MONITOR_WORKER_CONTAINER=founder-worker \
    GRADEX_MONITOR_POSTGRES_CONTAINER=founder-postgres \
    GRADEX_MONITOR_API_CONTAINER=founder-api \
    run_host_monitor 0
  if grep --fixed-strings --quiet 'docker compose' "$fake_docker_log"; then
    die "explicit Founder Beta monitor sources unexpectedly used canonical Compose lookup"
  fi

  GRADEX_MONITOR_WORKER_CONTAINER='invalid;worker' run_host_monitor 1
  grep --fixed-strings --quiet 'FAIL worker: configured monitor worker container name is invalid' "$host_monitor_log" ||
    die "invalid worker override was accepted"

  GRADEX_MONITOR_WORKER_CONTAINER=missing-worker FAKE_ABSENT_CONTAINER=missing-worker run_host_monitor 1
  grep --fixed-strings --quiet 'FAIL worker: configured monitor worker container is absent' "$host_monitor_log" ||
    die "absent explicit worker was not reported"

  FAKE_DOCKER_ROOT=not-an-absolute-path run_host_monitor 1
  grep --fixed-strings --quiet 'FAIL disk: Docker data-root is invalid' "$host_monitor_log" ||
    die "invalid Docker data root was not reported"

  note "monitor health, topology, schema, backup, email, and disk fixtures passed"
}

main "$@"
