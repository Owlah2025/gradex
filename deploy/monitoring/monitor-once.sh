#!/usr/bin/env bash

set -euo pipefail

note() { printf 'gradex-monitor: %s\n' "$*" >&2; }
die() { note "$*"; exit 2; }

require_value() {
  local name="$1"
  [ -n "${!name:-}" ] || die "$name is required"
}

for tool in awk curl date jq mktemp stat; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
done

require_value GRADEX_HEALTH_URL
require_value GRADEX_READY_URL
require_value GRADEX_ENVIRONMENT

GRADEX_BACKUP_MAX_AGE_SECONDS="${GRADEX_BACKUP_MAX_AGE_SECONDS:-7200}"
[[ "$GRADEX_BACKUP_MAX_AGE_SECONDS" =~ ^[1-9][0-9]*$ ]] ||
  die "GRADEX_BACKUP_MAX_AGE_SECONDS must be a positive integer"

GRADEX_MONITOR_EMAIL_STALE_SECONDS="${GRADEX_MONITOR_EMAIL_STALE_SECONDS:-3600}"
GRADEX_MONITOR_DISK_WARN_PERCENT="${GRADEX_MONITOR_DISK_WARN_PERCENT:-85}"
GRADEX_MONITOR_DISK_CRITICAL_PERCENT="${GRADEX_MONITOR_DISK_CRITICAL_PERCENT:-95}"
GRADEX_MONITOR_DISK_MIN_FREE_BYTES="${GRADEX_MONITOR_DISK_MIN_FREE_BYTES:-5368709120}"
[[ "$GRADEX_MONITOR_EMAIL_STALE_SECONDS" =~ ^[1-9][0-9]*$ ]] ||
  die "GRADEX_MONITOR_EMAIL_STALE_SECONDS must be a positive integer"
[[ "$GRADEX_MONITOR_DISK_WARN_PERCENT" =~ ^[0-9]+$ ]] ||
  die "GRADEX_MONITOR_DISK_WARN_PERCENT must be a non-negative integer"
[[ "$GRADEX_MONITOR_DISK_CRITICAL_PERCENT" =~ ^[0-9]+$ ]] ||
  die "GRADEX_MONITOR_DISK_CRITICAL_PERCENT must be a non-negative integer"
[[ "$GRADEX_MONITOR_DISK_MIN_FREE_BYTES" =~ ^[1-9][0-9]*$ ]] ||
  die "GRADEX_MONITOR_DISK_MIN_FREE_BYTES must be a positive integer"
[ "$GRADEX_MONITOR_DISK_WARN_PERCENT" -lt "$GRADEX_MONITOR_DISK_CRITICAL_PERCENT" ] ||
  die "GRADEX_MONITOR_DISK_WARN_PERCENT must be lower than GRADEX_MONITOR_DISK_CRITICAL_PERCENT"
[ "$GRADEX_MONITOR_DISK_CRITICAL_PERCENT" -le 100 ] ||
  die "GRADEX_MONITOR_DISK_CRITICAL_PERCENT must be at most 100"
[ "$GRADEX_MONITOR_DISK_WARN_PERCENT" -le 100 ] ||
  die "GRADEX_MONITOR_DISK_WARN_PERCENT must be at most 100"

require_value GRADEX_MONITOR_RUNTIME_REPORT
require_value GRADEX_MONITOR_DISK_PATHS
[ -f "$GRADEX_MONITOR_RUNTIME_REPORT" ] || die "GRADEX_MONITOR_RUNTIME_REPORT is not a regular file"
[ ! -L "$GRADEX_MONITOR_RUNTIME_REPORT" ] || die "GRADEX_MONITOR_RUNTIME_REPORT must not be a symlink"

declare -A runtime_status=()
declare -A runtime_detail=()
declare -A seen_devices=()
declare -A failure_detail=()
declare -A disk_fixture_device=()
declare -A disk_fixture_available=()
declare -A disk_fixture_total=()
declare -a monitored_paths=()
failures=()
warnings=()

record_failure() {
  local name="$1" detail="$2"
  if [ -n "${failure_detail[$name]:-}" ]; then
    failure_detail[$name]+="; $detail"
  else
    failure_detail[$name]="$detail"
    failures+=("$name")
  fi
}

record_warning() {
  warnings+=("$1")
}

validate_runtime_report_line() {
  local check="$1" status="$2" detail="$3" extra="$4"
  [ -z "$extra" ] || die "GRADEX_MONITOR_RUNTIME_REPORT has too many fields"
  case "$check" in worker|email_outbox|disk_roots) ;; *) die "GRADEX_MONITOR_RUNTIME_REPORT has an unknown check: $check" ;; esac
  [ -z "${runtime_status[$check]:-}" ] || die "GRADEX_MONITOR_RUNTIME_REPORT duplicates $check"
  case "$status" in PASS|FAIL|METRICS) ;; *) die "GRADEX_MONITOR_RUNTIME_REPORT has an invalid status" ;; esac
  case "$detail" in *$'\r'*|*$'\n'*) die "GRADEX_MONITOR_RUNTIME_REPORT contains an invalid line break" ;; esac
}

require_runtime_checks() {
  local check
  for check in worker email_outbox disk_roots; do
    [ -n "${runtime_status[$check]:-}" ] || die "GRADEX_MONITOR_RUNTIME_REPORT is missing $check"
  done
}

load_runtime_report() {
  local check status detail extra line_count=0
  while IFS='|' read -r check status detail extra || [ -n "${check:-}" ]; do
    [ -n "${check:-}" ] || continue
    line_count=$((line_count + 1))
    if [ "$check" = version=1 ]; then
      [ -z "$status" ] && [ -z "$detail" ] && [ -z "$extra" ] || die "GRADEX_MONITOR_RUNTIME_REPORT version line is malformed"
      continue
    fi
    validate_runtime_report_line "$check" "$status" "$detail" "$extra"
    runtime_status[$check]="$status"
    runtime_detail[$check]="${detail:-}"
  done <"$GRADEX_MONITOR_RUNTIME_REPORT"
  [ "$line_count" -gt 0 ] || die "GRADEX_MONITOR_RUNTIME_REPORT is empty"
  require_runtime_checks
}

evaluate_worker_report() {
  if [ "${runtime_status[worker]}" = PASS ]; then
    note "PASS worker: ${runtime_detail[worker]}"
  else
    record_failure worker "${runtime_detail[worker]}"
  fi
}

evaluate_disk_root_report() {
  if [ "${runtime_status[disk_roots]}" = PASS ]; then
    note "PASS disk_paths: ${runtime_detail[disk_roots]}"
  else
    record_failure disk "${runtime_detail[disk_roots]}"
  fi
}

parse_email_metrics() {
  local detail="${runtime_detail[email_outbox]}"
  if ! [[ "$detail" =~ ^terminal=([0-9]+)\;oldest_due_age=(-?[0-9]+)\;stale_lease_age=(-?[0-9]+)$ ]]; then
    return 1
  fi
  email_terminal_count="${BASH_REMATCH[1]}"
  email_oldest_due_age="${BASH_REMATCH[2]}"
  email_stale_lease_age="${BASH_REMATCH[3]}"
  [ "$email_oldest_due_age" -ge -1 ] && [ "$email_stale_lease_age" -ge -1 ]
}

email_failure_reasons() {
  local -a reasons=()
  [ "$email_terminal_count" -eq 0 ] || reasons+=("terminal_failures=$email_terminal_count")
  [ "$email_stale_lease_age" -lt 0 ] || reasons+=("oldest_stale_lease_age=${email_stale_lease_age}s")
  [ "$email_oldest_due_age" -lt "$GRADEX_MONITOR_EMAIL_STALE_SECONDS" ] ||
    reasons+=("oldest_due_age=${email_oldest_due_age}s>${GRADEX_MONITOR_EMAIL_STALE_SECONDS}s")
  local reason joined=""
  for reason in "${reasons[@]}"; do
    [ -z "$joined" ] || joined+="; "
    joined+="$reason"
  done
  printf '%s' "$joined"
}

evaluate_email_outbox() {
  local status="${runtime_status[email_outbox]}" reasons
  if [ "$status" = FAIL ]; then
    record_failure email_outbox "${runtime_detail[email_outbox]}"
    return
  fi
  [ "$status" = METRICS ] && parse_email_metrics || {
    record_failure email_outbox "invalid email health metrics"
    return
  }
  reasons="$(email_failure_reasons)"
  if [ -n "$reasons" ]; then
    record_failure email_outbox "$reasons"
  else
    note "PASS email_outbox: terminal_failures=0 oldest_due_age=${email_oldest_due_age}s stale_lease_age=${email_stale_lease_age}s"
  fi
}

evaluate_runtime_report() {
  evaluate_worker_report
  evaluate_disk_root_report
  evaluate_email_outbox
}

format_gib() {
  awk -v bytes="$1" 'BEGIN { printf "%.1f GiB", bytes / 1073741824 }'
}

evaluate_disk_path() {
  local path="$1" device="$2" available_bytes="$3" total_bytes="$4"
  local used_percent free_display
  if [ -n "${seen_devices[$device]:-}" ]; then
    note "INFO disk: path=$path shares device=$device already evaluated"
    return
  fi
  seen_devices[$device]=1
  if ! [[ "$available_bytes" =~ ^[0-9]+$ ]] || ! [[ "$total_bytes" =~ ^[1-9][0-9]*$ ]] ||
    ! awk -v available="$available_bytes" -v total="$total_bytes" 'BEGIN { exit !(available <= total) }'; then
    record_failure disk "path=$path has invalid filesystem statistics"
    return
  fi
  used_percent="$(awk -v available="$available_bytes" -v total="$total_bytes" \
    'BEGIN { printf "%.1f", ((total - available) / total) * 100 }')"
  free_display="$(format_gib "$available_bytes")"
  if awk -v used="$used_percent" -v critical="$GRADEX_MONITOR_DISK_CRITICAL_PERCENT" \
    'BEGIN { exit !(used >= critical) }' ||
    awk -v free="$available_bytes" -v minimum="$GRADEX_MONITOR_DISK_MIN_FREE_BYTES" 'BEGIN { exit !(free < minimum) }'; then
    record_failure disk "path=$path device=$device free=$free_display used=${used_percent}% (critical threshold ${GRADEX_MONITOR_DISK_CRITICAL_PERCENT}% or ${GRADEX_MONITOR_DISK_MIN_FREE_BYTES} bytes)"
  elif awk -v used="$used_percent" -v warning="$GRADEX_MONITOR_DISK_WARN_PERCENT" \
    'BEGIN { exit !(used >= warning) }'; then
    record_warning "path=$path device=$device free=$free_display used=${used_percent}%"
    note "WARN disk: path=$path device=$device free=$free_display used=${used_percent}%"
  else
    note "PASS disk: path=$path device=$device free=$free_display used=${used_percent}%"
  fi
}

parse_disk_paths() {
  IFS=: read -r -a monitored_paths <<<"$GRADEX_MONITOR_DISK_PATHS"
  [ "${#monitored_paths[@]}" -gt 0 ] || die "GRADEX_MONITOR_DISK_PATHS is empty"
  case "$GRADEX_MONITOR_DISK_PATHS" in
    :*|*:|*::* ) die "GRADEX_MONITOR_DISK_PATHS contains an empty path" ;;
  esac
}

load_disk_fixture() {
  local fixture_path fixture_device_value fixture_available_value fixture_total_value fixture_extra
  [ "$GRADEX_ENVIRONMENT" = monitor-test ] || die "disk fixtures are allowed only in monitor-test"
  [ -f "$GRADEX_MONITOR_DISK_FIXTURE" ] || die "GRADEX_MONITOR_DISK_FIXTURE is not a regular file"
  disk_fixture_device=()
  disk_fixture_available=()
  disk_fixture_total=()
  while IFS='|' read -r fixture_path fixture_device_value fixture_available_value fixture_total_value fixture_extra ||
    [ -n "${fixture_path:-}" ]; do
    [ -n "${fixture_path:-}" ] || continue
    [ -z "${fixture_extra:-}" ] || die "disk fixture has too many fields"
    [ -z "${disk_fixture_device[$fixture_path]:-}" ] || die "disk fixture duplicates $fixture_path"
    disk_fixture_device[$fixture_path]="${fixture_device_value:-}"
    disk_fixture_available[$fixture_path]="${fixture_available_value:-}"
    disk_fixture_total[$fixture_path]="${fixture_total_value:-}"
  done <"$GRADEX_MONITOR_DISK_FIXTURE"
}

evaluate_fixture_disks() {
  local path device
  for path in "${monitored_paths[@]}"; do
    [ -n "${disk_fixture_device[$path]:-}" ] || {
      record_failure disk "path=$path is missing from the disk fixture"
      continue
    }
    device="${disk_fixture_device[$path]}"
    if [ "$device" = ERROR ]; then
      record_failure disk "path=$path is unreadable"
      continue
    fi
    evaluate_disk_path "$path" "$device" "${disk_fixture_available[$path]}" "${disk_fixture_total[$path]}"
  done
}

evaluate_live_disk() {
  local path="$1" device stat_values available_blocks total_blocks block_size available_bytes total_bytes
  case "$path" in /*) ;; *) record_failure disk "path=$path is not absolute"; return ;; esac
  if ! device="$(stat -c '%d' -- "$path" 2>/dev/null)" || [ -z "$device" ]; then
    record_failure disk "path=$path is unreadable"
    return
  fi
  if ! stat_values="$(stat -f -c '%a %b %s' -- "$path" 2>/dev/null)"; then
    record_failure disk "path=$path filesystem statistics are unavailable"
    return
  fi
  read -r available_blocks total_blocks block_size <<<"$stat_values"
  if ! [[ "$available_blocks" =~ ^[0-9]+$ ]] || ! [[ "$total_blocks" =~ ^[1-9][0-9]*$ ]] ||
    ! [[ "$block_size" =~ ^[1-9][0-9]*$ ]]; then
    record_failure disk "path=$path returned invalid filesystem statistics"
    return
  fi
  available_bytes="$(awk -v blocks="$available_blocks" -v size="$block_size" 'BEGIN { printf "%.0f", blocks * size }')"
  total_bytes="$(awk -v blocks="$total_blocks" -v size="$block_size" 'BEGIN { printf "%.0f", blocks * size }')"
  evaluate_disk_path "$path" "$device" "$available_bytes" "$total_bytes"
}

evaluate_live_disks() {
  local path
  for path in "${monitored_paths[@]}"; do
    evaluate_live_disk "$path"
  done
}

check_disk_filesystems() {
  parse_disk_paths
  if [ -n "${GRADEX_MONITOR_DISK_FIXTURE:-}" ]; then
    load_disk_fixture
    evaluate_fixture_disks
  else
    evaluate_live_disks
  fi
}

temporary="$(mktemp -d)"
trap 'rm -rf -- "$temporary"' EXIT
chmod 700 "$temporary"

curl_args=(--silent --show-error --connect-timeout 5 --max-time 10)
if [ -n "${GRADEX_MONITOR_CA_FILE:-}" ]; then
  [ -s "$GRADEX_MONITOR_CA_FILE" ] || die "GRADEX_MONITOR_CA_FILE is unreadable"
  curl_args+=(--cacert "$GRADEX_MONITOR_CA_FILE")
fi

probe() {
  local name="$1" url="$2" body status
  body="$temporary/$name.body"
  status="$(curl "${curl_args[@]}" --output "$body" --write-out '%{http_code}' "$url" || true)"
  if [ "$status" != "200" ]; then
    record_failure "$name" "HTTP status $status"
  else
    note "PASS $name"
  fi
}

load_runtime_report
probe api_health "$GRADEX_HEALTH_URL"
probe api_readiness "$GRADEX_READY_URL"
evaluate_runtime_report
check_disk_filesystems

if [ -n "${GRADEX_BACKUP_COMPLETED_AT_FILE:-}" ]; then
  if [ ! -s "$GRADEX_BACKUP_COMPLETED_AT_FILE" ]; then
    record_failure backup "completion marker is missing"
  else
    completed_at="$(tr -d '[:space:]' <"$GRADEX_BACKUP_COMPLETED_AT_FILE")"
    now="$(date +%s)"
    if ! [[ "$completed_at" =~ ^[0-9]+$ ]] || [ "$completed_at" -gt "$now" ] ||
      [ $((now - completed_at)) -gt "$GRADEX_BACKUP_MAX_AGE_SECONDS" ]; then
      record_failure backup "completion marker is stale or invalid"
    else
      note "PASS backup: completion marker age=$((now - completed_at))s"
    fi
  fi
fi

if [ "${#failures[@]}" -eq 0 ]; then
  if [ "${#warnings[@]}" -gt 0 ]; then
    note "all configured checks passed with warnings"
  else
    note "all configured checks passed"
  fi
  exit 0
fi

for failure in "${failures[@]}"; do
  note "FAIL $failure: ${failure_detail[$failure]}"
done

correlation_id="monitor-$(date -u +%Y%m%dT%H%M%SZ)-$$"
failures_json="$(printf '%s\n' "${failures[@]}" | jq --raw-input --slurp 'split("\n") | map(select(length > 0))')"
payload="$(jq --compact-output --null-input \
  --arg environment "$GRADEX_ENVIRONMENT" \
  --arg correlation_id "$correlation_id" \
  --arg observed_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --argjson failures "$failures_json" \
  '{event:"gradex_monitor_failure",environment:$environment,correlation_id:$correlation_id,observed_at:$observed_at,failures:$failures}')"

if [ -z "${GRADEX_ALERT_WEBHOOK_URL:-}" ]; then
  note "checks failed and no alert webhook is configured (correlation_id=$correlation_id)"
  exit 1
fi

case "$GRADEX_ALERT_WEBHOOK_URL" in
  *$'\r'*|*$'\n'*|*'"'*|*'\'*) die "GRADEX_ALERT_WEBHOOK_URL contains a character unsafe for curl configuration" ;;
esac
webhook_url_config="$temporary/webhook-url.curl-config"
printf 'url = "%s"\n' "$GRADEX_ALERT_WEBHOOK_URL" >"$webhook_url_config"
chmod 600 "$webhook_url_config"

webhook_args=(--fail --silent --show-error --connect-timeout 5 --max-time 10
  --header 'Content-Type: application/json' --data "$payload")
if [ -n "${GRADEX_ALERT_WEBHOOK_TOKEN:-}" ]; then
  case "$GRADEX_ALERT_WEBHOOK_TOKEN" in
    *$'\r'*|*$'\n'*) die "GRADEX_ALERT_WEBHOOK_TOKEN contains an invalid line break" ;;
  esac
  authorization_header="$temporary/webhook-authorization.header"
  printf 'Authorization: Bearer %s\n' "$GRADEX_ALERT_WEBHOOK_TOKEN" >"$authorization_header"
  chmod 600 "$authorization_header"
  webhook_args+=(--header "@$authorization_header")
fi
if ! curl "${webhook_args[@]}" --config "$webhook_url_config" >/dev/null; then
  note "checks failed and alert delivery failed (correlation_id=$correlation_id)"
  exit 1
fi
note "checks failed and alert delivery succeeded (correlation_id=$correlation_id)"
exit 1
