#!/usr/bin/env bash

set -Eeuo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_MONITOR="$S12_ROOT/deploy/monitoring/monitor-once.sh"
S12_TEMPORARY=""

note() { printf 'monitor-resend-alerts: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

cleanup() {
  [ -z "$S12_TEMPORARY" ] || rm -rf -- "$S12_TEMPORARY"
}

assert_log_contains() {
  grep --fixed-strings --quiet "$1" "$monitor_log" || die "monitor output is missing: $1"
}

assert_log_absent() {
  if grep --fixed-strings --quiet "$1" "$monitor_log"; then
    die "monitor output exposed protected data"
  fi
}

write_fake_curl() {
  cat >"$fake_bin/curl" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
output=""
write_out=0
data=""
config=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) output="$2"; shift 2 ;;
    --write-out) write_out=1; shift 2 ;;
    --data) data="$2"; shift 2 ;;
    --config) config="$2"; shift 2 ;;
    --cacert) printf 'custom_ca\n' >>"$FAKE_EVENTS"; shift 2 ;;
    --header)
      case "$2" in
        @*) grep --fixed-strings --quiet 'Authorization: Bearer ' "${2#@}" && printf 'resend_auth_header\n' >>"$FAKE_EVENTS" ;;
      esac
      shift 2
      ;;
    *) url="$1"; shift ;;
  esac
done
if [ "$url" = https://api.resend.com/emails ]; then
  printf 'resend\n' >>"$FAKE_EVENTS"
  if [[ "$data" == *"$FAKE_RESEND_API_KEY"* ]]; then
    printf 'resend_payload_leaked_key\n' >>"$FAKE_EVENTS"
  fi
  if jq --exit-status -e '
      .subject == "[Gradex] Monitoring alert delivery test" and
      (.text | contains("synthetic")) and
      (.text | contains("Correlation ID:")) and
      (.to | length == 1)
    ' <<<"$data" >/dev/null; then
    printf 'resend_payload_safe\n' >>"$FAKE_EVENTS"
  fi
  case "$FAKE_RESEND_MODE" in
    success) printf '%s\n' '{"id":"fixture-resend-id"}' >"$output"; [ "$write_out" = 0 ] || printf 202 ;;
    non2xx) printf '%s\n' '{"message":"rate limited"}' >"$output"; [ "$write_out" = 0 ] || printf 429 ;;
    network) exit 7 ;;
    timeout) exit 28 ;;
    *) exit 99 ;;
  esac
  exit 0
fi
if [ -n "$config" ]; then
  grep --fixed-strings --quiet 'url = "https://alerts.test/webhook"' "$config" && printf 'webhook\n' >>"$FAKE_EVENTS"
  exit 0
fi
[ -z "$output" ] || : >"$output"
[ "$write_out" = 0 ] || printf 200
EOF
  chmod 0755 "$fake_bin/curl"
}

run_monitor() {
  local expected_status="$1"
  set +e
  PATH="$fake_bin:$PATH" \
    FAKE_EVENTS="$events" \
    FAKE_RESEND_MODE="${FAKE_RESEND_MODE:-success}" \
    FAKE_RESEND_API_KEY="$test_api_key" \
    GRADEX_PUBLIC_URL=https://monitor.test/ \
    GRADEX_HEALTH_URL=https://monitor.test/healthz \
    GRADEX_READY_URL=https://monitor.test/readyz \
    GRADEX_ENVIRONMENT=monitor-test \
    GRADEX_MONITOR_RUNTIME_REPORT="$runtime_report" \
    GRADEX_MONITOR_DISK_PATHS=/gradex \
    GRADEX_MONITOR_DISK_FIXTURE="$disk_fixture" \
    GRADEX_MONITOR_DISK_MIN_FREE_BYTES=1 \
    GRADEX_MONITOR_CA_FILE="${GRADEX_MONITOR_CA_FILE:-}" \
    GRADEX_MONITOR_SYNTHETIC_ALERT_TEST="${GRADEX_MONITOR_SYNTHETIC_ALERT_TEST:-}" \
    GRADEX_ALERT_RESEND_API_KEY="${GRADEX_ALERT_RESEND_API_KEY:-}" \
    GRADEX_ALERT_EMAIL_TO="${GRADEX_ALERT_EMAIL_TO:-}" \
    EMAIL_FROM_ADDRESS="${EMAIL_FROM_ADDRESS:-}" \
    EMAIL_FROM_NAME="${EMAIL_FROM_NAME:-}" \
    GRADEX_ALERT_WEBHOOK_URL="${GRADEX_ALERT_WEBHOOK_URL:-}" \
    GRADEX_ALERT_WEBHOOK_TOKEN="${GRADEX_ALERT_WEBHOOK_TOKEN:-}" \
    "$S12_MONITOR" >"$monitor_log" 2>&1
  monitor_status=$?
  set -e
  [ "$monitor_status" = "$expected_status" ] || die "monitor status=$monitor_status, expected=$expected_status"
}

event_count() {
  grep --fixed-strings --line-regexp --count "$1" "$events" 2>/dev/null || true
}

main() {
  local tool
  for tool in chmod grep jq mktemp rm; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done
  [ -x "$S12_MONITOR" ] || die "monitor script is not executable"

  S12_TEMPORARY="$(mktemp -d)"
  chmod 700 "$S12_TEMPORARY"
  trap cleanup EXIT
  fake_bin="$S12_TEMPORARY/fake-bin"
  runtime_report="$S12_TEMPORARY/runtime-report"
  disk_fixture="$S12_TEMPORARY/disk-fixture"
  monitor_log="$S12_TEMPORARY/monitor.log"
  events="$S12_TEMPORARY/events"
  ca_file="$S12_TEMPORARY/monitor-ca.pem"
  test_api_key=unit-monitor-resend-credential
  mkdir -p "$fake_bin"
  write_fake_curl
  printf 'version=1\nworker|PASS|fixture worker\npostgres_schema|PASS|fixture schema\nemail_outbox|METRICS|terminal=0;oldest_due_age=-1;stale_lease_age=-1\ndisk_roots|PASS|fixture disk\n' >"$runtime_report"
  printf '/gradex|dev-a|90|100\n' >"$disk_fixture"
  printf 'fixture monitor CA\n' >"$ca_file"

  : >"$events"
  run_monitor 0
  [ "$(event_count resend)" = 0 ] || die "healthy monitoring sent a Resend alert"

  : >"$events"
  GRADEX_MONITOR_SYNTHETIC_ALERT_TEST=1 \
    GRADEX_ALERT_RESEND_API_KEY="$test_api_key" \
    GRADEX_ALERT_EMAIL_TO=operator@example.test \
    EMAIL_FROM_ADDRESS=monitor@example.test \
    EMAIL_FROM_NAME=Gradex \
    GRADEX_MONITOR_CA_FILE="$ca_file" \
    run_monitor 1
  [ "$(event_count resend)" = 1 ] || die "synthetic failure did not send exactly one Resend alert"
  [ "$(event_count resend_auth_header)" = 1 ] || die "Resend Authorization header was not supplied"
  [ "$(event_count resend_payload_safe)" = 1 ] || die "Resend payload did not match the safe synthetic contract"
  [ "$(event_count resend_payload_leaked_key)" = 0 ] || die "Resend payload exposed the API credential"
  [ "$(event_count custom_ca)" = 4 ] || die "custom CA was not supplied to each HTTPS probe and Resend request"
  assert_log_contains 'Resend alert delivery succeeded'
  assert_log_absent "$test_api_key"
  assert_log_absent 'operator@example.test'

  for mode in non2xx network timeout; do
    : >"$events"
    FAKE_RESEND_MODE="$mode" \
      GRADEX_MONITOR_SYNTHETIC_ALERT_TEST=1 \
      GRADEX_ALERT_RESEND_API_KEY="$test_api_key" \
      GRADEX_ALERT_EMAIL_TO=operator@example.test \
      EMAIL_FROM_ADDRESS=monitor@example.test \
      EMAIL_FROM_NAME=Gradex \
      run_monitor 1
    [ "$(event_count resend)" = 1 ] || die "$mode Resend failure did not attempt one request"
    assert_log_contains 'Resend alert delivery failed'
    case "$mode" in
      non2xx) assert_log_contains 'Resend HTTP status 429' ;;
      network) assert_log_contains 'Resend connection failed' ;;
      timeout) assert_log_contains 'Resend request timed out' ;;
    esac
    assert_log_absent "$test_api_key"
  done

  GRADEX_MONITOR_SYNTHETIC_ALERT_TEST=1 \
    GRADEX_ALERT_EMAIL_TO=operator@example.test \
    EMAIL_FROM_ADDRESS=monitor@example.test \
    EMAIL_FROM_NAME=Gradex \
    run_monitor 1
  assert_log_contains 'Resend API key is missing'

  GRADEX_MONITOR_SYNTHETIC_ALERT_TEST=1 \
    GRADEX_ALERT_RESEND_API_KEY="$test_api_key" \
    EMAIL_FROM_ADDRESS=monitor@example.test \
    EMAIL_FROM_NAME=Gradex \
    run_monitor 1
  assert_log_contains 'Resend recipient is missing'

  GRADEX_MONITOR_SYNTHETIC_ALERT_TEST=1 \
    GRADEX_ALERT_RESEND_API_KEY="$test_api_key" \
    GRADEX_ALERT_EMAIL_TO=not-an-email \
    EMAIL_FROM_ADDRESS=monitor@example.test \
    EMAIL_FROM_NAME=Gradex \
    run_monitor 1
  assert_log_contains 'Resend recipient is invalid'

  : >"$events"
  GRADEX_MONITOR_SYNTHETIC_ALERT_TEST=1 \
    GRADEX_ALERT_RESEND_API_KEY="$test_api_key" \
    GRADEX_ALERT_EMAIL_TO=operator@example.test \
    EMAIL_FROM_ADDRESS=monitor@example.test \
    EMAIL_FROM_NAME=Gradex \
    GRADEX_ALERT_WEBHOOK_URL=https://alerts.test/webhook \
    GRADEX_ALERT_WEBHOOK_TOKEN=unit-webhook-credential \
    run_monitor 1
  [ "$(event_count resend)" = 1 ] || die "dual destination delivery skipped Resend"
  [ "$(event_count webhook)" = 1 ] || die "dual destination delivery skipped the webhook"
  assert_log_contains 'webhook alert delivery succeeded'
  assert_log_contains 'Resend alert delivery succeeded'
  assert_log_absent 'unit-webhook-credential'

  note "synthetic Resend, dual delivery, failure handling, and protected output passed"
}

main "$@"
