#!/usr/bin/env bash

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_SYSTEMD_DIR="$S12_ROOT/deploy/hostinger/systemd"
S12_TEMPORARY=""

note() { printf 'hostinger-systemd: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

cleanup() {
  if [ -n "$S12_TEMPORARY" ] && [ -d "$S12_TEMPORARY" ]; then
    rm -rf -- "$S12_TEMPORARY"
  fi
}

assert_line() {
  local file="$1" expected="$2"
  grep --quiet --fixed-strings --line-regexp "$expected" "$file" ||
    die "$(basename "$file") is missing: $expected"
}

main() {
  local tool file operator group fake_bin backup_marker runtime_report curl_args_log monitor_log monitor_status now ca_file
  for tool in cat chmod date grep id mkdir mktemp systemd-analyze; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done

  for file in \
    gradex-monitor.service.in gradex-monitor.timer \
    gradex-backup.service.in gradex-backup.timer install.sh; do
    [ -f "$S12_SYSTEMD_DIR/$file" ] || die "$file is absent"
  done

  if "$S12_SYSTEMD_DIR/install.sh" install --user root --repo "$S12_ROOT" >/dev/null 2>&1; then
    die "the installer accepted root as the scheduled operator"
  fi
  if grep --extended-regexp 'systemctl[[:space:]]+(enable|start)|--now' \
    "$S12_SYSTEMD_DIR/install.sh"; then
    die "the installer enables or starts scheduled work as a side effect"
  fi

  S12_TEMPORARY="$(mktemp -d)"
  trap cleanup EXIT
  operator="$(id -un)"
  group="$(id -gn)"
  "$S12_SYSTEMD_DIR/install.sh" render \
    --output "$S12_TEMPORARY" --user "$operator" --group "$group" --repo "$S12_ROOT"

  for file in gradex-monitor.service gradex-monitor.timer gradex-backup.service gradex-backup.timer; do
    [ -f "$S12_TEMPORARY/$file" ] || die "rendered $file is absent"
  done

  assert_line "$S12_TEMPORARY/gradex-monitor.service" 'Type=oneshot'
  assert_line "$S12_TEMPORARY/gradex-monitor.service" "User=$operator"
  assert_line "$S12_TEMPORARY/gradex-monitor.service" "Group=$group"
  assert_line "$S12_TEMPORARY/gradex-monitor.service" "WorkingDirectory=$S12_ROOT"
  assert_line "$S12_TEMPORARY/gradex-monitor.service" "ExecStart=$S12_ROOT/deploy/hostinger/host.sh monitor"
  assert_line "$S12_TEMPORARY/gradex-monitor.service" 'TimeoutStartSec=120s'
  assert_line "$S12_TEMPORARY/gradex-monitor.service" 'UMask=0077'
  assert_line "$S12_TEMPORARY/gradex-monitor.service" 'NoNewPrivileges=true'
  assert_line "$S12_TEMPORARY/gradex-monitor.service" 'PrivateTmp=true'
  assert_line "$S12_TEMPORARY/gradex-monitor.service" 'ProtectSystem=full'
  assert_line "$S12_TEMPORARY/gradex-monitor.timer" 'OnCalendar=*:0/5'
  assert_line "$S12_TEMPORARY/gradex-monitor.timer" 'Persistent=true'

  assert_line "$S12_TEMPORARY/gradex-backup.service" 'Type=oneshot'
  assert_line "$S12_TEMPORARY/gradex-backup.service" 'TimeoutStartSec=360s'
  assert_line "$S12_TEMPORARY/gradex-backup.service" "User=$operator"
  assert_line "$S12_TEMPORARY/gradex-backup.service" "Group=$group"
  assert_line "$S12_TEMPORARY/gradex-backup.service" "WorkingDirectory=$S12_ROOT"
  assert_line "$S12_TEMPORARY/gradex-backup.service" "ExecStart=$S12_ROOT/deploy/hostinger/host.sh backup"
  assert_line "$S12_TEMPORARY/gradex-backup.timer" 'OnCalendar=hourly'
  assert_line "$S12_TEMPORARY/gradex-backup.timer" 'Persistent=true'
  assert_line "$S12_TEMPORARY/gradex-backup.service" 'UMask=0077'
  assert_line "$S12_TEMPORARY/gradex-backup.service" 'NoNewPrivileges=true'
  assert_line "$S12_TEMPORARY/gradex-backup.service" 'PrivateTmp=true'
  assert_line "$S12_TEMPORARY/gradex-backup.service" 'ProtectSystem=full'
  assert_line "$S12_TEMPORARY/gradex-backup.service" 'ReadWritePaths=/var/lib/gradex'
  assert_line "$S12_TEMPORARY/gradex-backup.service" 'RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6'

  if grep --ignore-case --extended-regexp \
    '(runtime\.env|webhook|token|password|database_url|secret|Environment(File)?=)' \
    "$S12_TEMPORARY"/*.service "$S12_TEMPORARY"/*.timer; then
    die "rendered units contain a secret or duplicate environment-loading surface"
  fi

  grep --quiet --fixed-strings 'GRADEX_BACKUP_MAX_AGE_SECONDS="${GRADEX_BACKUP_MAX_AGE_SECONDS:-7200}"' \
    "$S12_ROOT/deploy/monitoring/monitor-once.sh" ||
    die "monitor default backup freshness is not two hours"
  assert_line "$S12_ROOT/deploy/monitoring/monitor.env.example" 'GRADEX_BACKUP_MAX_AGE_SECONDS=7200'
  assert_line "$S12_ROOT/deploy/monitoring/monitor.env.example" 'GRADEX_BACKUP_MAX_FUTURE_SECONDS=300'
  grep --quiet --fixed-strings 'maximum_age_seconds: 7200' "$S12_ROOT/deploy/monitoring/rules.yml" ||
    die "monitoring rules do not match the two-hour freshness contract"
  grep --quiet --fixed-strings 'stale_due_seconds: 3600' "$S12_ROOT/deploy/monitoring/rules.yml" ||
    die "monitoring rules do not match the one-hour email staleness contract"
  grep --quiet --fixed-strings 'warning_used_percent: 85' "$S12_ROOT/deploy/monitoring/rules.yml" ||
    die "monitoring rules do not match the disk warning contract"
  grep --quiet --fixed-strings 'critical_used_percent: 95' "$S12_ROOT/deploy/monitoring/rules.yml" ||
    die "monitoring rules do not match the disk critical contract"
  grep --quiet --fixed-strings 'maximum_future_seconds: 300' "$S12_ROOT/deploy/monitoring/rules.yml" ||
    die "monitoring rules do not match the backup future-clock tolerance"
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_BACKUP_MAX_AGE_SECONDS=7200'
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_BACKUP_MAX_FUTURE_SECONDS=300'
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_MONITOR_COMPOSE_PROJECT='
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_MONITOR_WORKER_CONTAINER='
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_MONITOR_POSTGRES_CONTAINER='
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_MONITOR_API_CONTAINER='
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_MONITOR_EMAIL_STALE_SECONDS=3600'
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_MONITOR_DISK_WARN_PERCENT=85'
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_MONITOR_DISK_CRITICAL_PERCENT=95'
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_MONITOR_DISK_MIN_FREE_BYTES=5368709120'
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_MONITOR_CA_FILE='
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_ALERT_RESEND_API_KEY='
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_ALERT_EMAIL_TO='
  grep --quiet --fixed-strings 'optional_environment: GRADEX_ALERT_WEBHOOK_URL' \
    "$S12_ROOT/deploy/monitoring/rules.yml" ||
    die "monitoring rules do not describe the optional webhook accurately"
  grep --quiet --fixed-strings 'type: resend_email' "$S12_ROOT/deploy/monitoring/rules.yml" ||
    die "monitoring rules do not describe the optional Resend destination"
  grep --quiet --fixed-strings 'endpoint: https://api.resend.com/emails' "$S12_ROOT/deploy/monitoring/rules.yml" ||
    die "monitoring rules do not document the Resend endpoint"
  grep --quiet --fixed-strings 'optional_bearer_secret: GRADEX_ALERT_WEBHOOK_TOKEN' \
    "$S12_ROOT/deploy/monitoring/rules.yml" ||
    die "monitoring rules do not describe the optional bearer credential accurately"

  fake_bin="$S12_TEMPORARY/fake-bin"
  backup_marker="$S12_TEMPORARY/latest.completed-at"
  runtime_report="$S12_TEMPORARY/runtime-report"
  curl_args_log="$S12_TEMPORARY/curl.args"
  monitor_log="$S12_TEMPORARY/monitor.log"
  mkdir -p "$fake_bin"
  cat >"$fake_bin/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
output=""
write_status=0
[ -z "${FAKE_CURL_ARGS_LOG:-}" ] || printf '%s\n' "$@" >>"$FAKE_CURL_ARGS_LOG"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) output="$2"; shift 2 ;;
    --write-out) write_status=1; shift 2 ;;
    *) shift ;;
  esac
done
[ -z "$output" ] || : >"$output"
[ "$write_status" = 0 ] || printf '200'
[ "$write_status" = 1 ] || [ "${FAKE_CURL_FAIL:-0}" != 1 ] || exit 22
EOF
  chmod 0755 "$fake_bin/curl"
  printf 'version=1\nworker|PASS|fixture worker\npostgres_schema|PASS|fixture PostgreSQL schema\nemail_outbox|METRICS|terminal=0;oldest_due_age=-1;stale_lease_age=-1\ndisk_roots|PASS|fixture paths\n' >"$runtime_report"
  chmod 600 "$runtime_report"

  now="$(date +%s)"
  printf '%s\n' "$((now - 7199))" >"$backup_marker"
  PATH="$fake_bin:$PATH" \
    GRADEX_HEALTH_URL=https://health.example \
    GRADEX_READY_URL=https://ready.example \
    GRADEX_PUBLIC_URL=https://staging.example/ \
    GRADEX_ENVIRONMENT=systemd-proof \
    GRADEX_BACKUP_COMPLETED_AT_FILE="$backup_marker" \
    GRADEX_BACKUP_MAX_AGE_SECONDS= \
    GRADEX_MONITOR_RUNTIME_REPORT="$runtime_report" \
    GRADEX_MONITOR_DISK_PATHS=/tmp \
    GRADEX_MONITOR_DISK_MIN_FREE_BYTES=1 \
    "$S12_ROOT/deploy/monitoring/monitor-once.sh" >"$monitor_log" 2>&1 ||
    die "a backup younger than two hours was reported stale"

  printf '%s\n' "$((now - 7201))" >"$backup_marker"
  set +e
  PATH="$fake_bin:$PATH" \
    GRADEX_HEALTH_URL=https://health.example \
    GRADEX_READY_URL=https://ready.example \
    GRADEX_PUBLIC_URL=https://staging.example/ \
    GRADEX_ENVIRONMENT=systemd-proof \
    GRADEX_BACKUP_COMPLETED_AT_FILE="$backup_marker" \
    GRADEX_BACKUP_MAX_AGE_SECONDS= \
    GRADEX_MONITOR_RUNTIME_REPORT="$runtime_report" \
    GRADEX_MONITOR_DISK_PATHS=/tmp \
    GRADEX_MONITOR_DISK_MIN_FREE_BYTES=1 \
    GRADEX_ALERT_WEBHOOK_URL=https://alerts.example/systemd-proof-url-secret-sentinel \
    GRADEX_ALERT_WEBHOOK_TOKEN=systemd-proof-secret-sentinel \
    GRADEX_ALERT_WEBHOOK_TOKEN=proofcredential \
    FAKE_CURL_ARGS_LOG="$curl_args_log" \
    "$S12_ROOT/deploy/monitoring/monitor-once.sh" >"$monitor_log" 2>&1
  monitor_status=$?
  set -e
  [ "$monitor_status" = 1 ] || die "a backup older than two hours did not fail monitoring"
  grep --quiet --fixed-strings 'alert delivery succeeded' "$monitor_log" ||
    die "the stale-backup path did not attempt alert delivery"
  if grep --quiet --fixed-strings 'systemd-proof-secret-sentinel' "$curl_args_log"; then
    die "the alert bearer credential reached curl command arguments"
  fi
  if grep --quiet --fixed-strings 'systemd-proof-url-secret-sentinel' "$curl_args_log"; then
    die "the alert webhook URL reached curl command arguments"
  fi
  if grep --quiet --fixed-strings 'proofcredential' "$curl_args_log"; then
    die "the alert bearer credential reached curl command arguments"
  fi
  if grep --quiet --fixed-strings 'systemd-proof-secret-sentinel' "$monitor_log" ||
    grep --quiet --fixed-strings 'systemd-proof-url-secret-sentinel' "$monitor_log"; then
    die "the monitor log exposed an alert credential"
  fi
  if grep --quiet --fixed-strings 'proofcredential' "$monitor_log"; then
    die "the monitor log exposed an alert bearer credential"
  fi

  ca_file="$S12_TEMPORARY/monitor-ca.pem"
  printf 'fixture monitor CA\n' >"$ca_file"
  : >"$curl_args_log"
  set +e
  PATH="$fake_bin:$PATH" \
    GRADEX_PUBLIC_URL=https://staging.example/ \
    GRADEX_HEALTH_URL=https://health.example \
    GRADEX_READY_URL=https://ready.example \
    GRADEX_ENVIRONMENT=systemd-proof \
    GRADEX_BACKUP_COMPLETED_AT_FILE="$backup_marker" \
    GRADEX_MONITOR_RUNTIME_REPORT="$runtime_report" \
    GRADEX_MONITOR_DISK_PATHS=/tmp \
    GRADEX_MONITOR_DISK_MIN_FREE_BYTES=1 \
    GRADEX_MONITOR_CA_FILE="$ca_file" \
    GRADEX_ALERT_WEBHOOK_URL=https://alerts.example/systemd-proof-url-secret-sentinel \
    GRADEX_ALERT_WEBHOOK_TOKEN=proofcredential \
    FAKE_CURL_FAIL=1 \
    FAKE_CURL_ARGS_LOG="$curl_args_log" \
    "$S12_ROOT/deploy/monitoring/monitor-once.sh" >"$monitor_log" 2>&1
  monitor_status=$?
  set -e
  [ "$monitor_status" = 1 ] || die "a rejected webhook response did not fail monitoring (status=$monitor_status)"
  grep --quiet --fixed-strings 'alert delivery failed' "$monitor_log" ||
    die "webhook delivery failure was not distinguishable"
  [ "$(grep -c --fixed-strings -- '--cacert' "$curl_args_log")" = 4 ] ||
    die "custom monitoring CA was not supplied to every HTTPS probe and webhook request"
  if grep --quiet --fixed-strings 'systemd-proof-secret-sentinel' "$monitor_log" ||
    grep --quiet --fixed-strings 'systemd-proof-url-secret-sentinel' "$monitor_log"; then
    die "failed webhook delivery exposed an alert credential"
  fi
  if grep --quiet --fixed-strings 'proofcredential' "$monitor_log"; then
    die "failed webhook delivery exposed an alert bearer credential"
  fi

  set +e
  PATH="$fake_bin:$PATH" \
    GRADEX_PUBLIC_URL=https://staging.example/ \
    GRADEX_HEALTH_URL=https://health.example \
    GRADEX_READY_URL=https://ready.example \
    GRADEX_ENVIRONMENT=systemd-proof \
    GRADEX_MONITOR_RUNTIME_REPORT="$runtime_report" \
    GRADEX_MONITOR_DISK_PATHS=/tmp \
    GRADEX_MONITOR_DISK_MIN_FREE_BYTES=1 \
    GRADEX_ALERT_WEBHOOK_URL=http://alerts.example/insecure \
    "$S12_ROOT/deploy/monitoring/monitor-once.sh" >"$monitor_log" 2>&1
  monitor_status=$?
  set -e
  [ "$monitor_status" = 2 ] || die "an insecure webhook URL was accepted outside monitor-test"
  grep --quiet --fixed-strings 'GRADEX_ALERT_WEBHOOK_URL must be an HTTPS URL outside monitor-test' "$monitor_log" ||
    die "insecure webhook rejection was not reported"

  systemd-analyze verify \
    "$S12_TEMPORARY/gradex-monitor.service" "$S12_TEMPORARY/gradex-monitor.timer" \
    "$S12_TEMPORARY/gradex-backup.service" "$S12_TEMPORARY/gradex-backup.timer"

  note "rendering, cadence, entrypoints, persistence, secret isolation, freshness, and unit syntax passed"
}

main "$@"
