#!/usr/bin/env bash

set -euo pipefail

note() { printf 'gradex-monitor: %s\n' "$*" >&2; }
die() { note "$*"; exit 2; }

require_value() {
  local name="$1"
  [ -n "${!name:-}" ] || die "$name is required"
}

for tool in curl date jq mktemp; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
done

require_value GRADEX_HEALTH_URL
require_value GRADEX_READY_URL
require_value GRADEX_ENVIRONMENT

GRADEX_BACKUP_MAX_AGE_SECONDS="${GRADEX_BACKUP_MAX_AGE_SECONDS:-93600}"
[[ "$GRADEX_BACKUP_MAX_AGE_SECONDS" =~ ^[1-9][0-9]*$ ]] ||
  die "GRADEX_BACKUP_MAX_AGE_SECONDS must be a positive integer"

temporary="$(mktemp -d)"
trap 'rm -rf -- "$temporary"' EXIT
chmod 700 "$temporary"

curl_args=(--silent --show-error --connect-timeout 5 --max-time 10)
if [ -n "${GRADEX_MONITOR_CA_FILE:-}" ]; then
  [ -s "$GRADEX_MONITOR_CA_FILE" ] || die "GRADEX_MONITOR_CA_FILE is unreadable"
  curl_args+=(--cacert "$GRADEX_MONITOR_CA_FILE")
fi

failures=()
probe() {
  local name="$1" url="$2" body status
  body="$temporary/$name.body"
  status="$(curl "${curl_args[@]}" --output "$body" --write-out '%{http_code}' "$url" || true)"
  if [ "$status" != "200" ]; then
    failures+=("$name")
  fi
}

probe api_health "$GRADEX_HEALTH_URL"
probe api_readiness "$GRADEX_READY_URL"

if [ -n "${GRADEX_BACKUP_COMPLETED_AT_FILE:-}" ]; then
  if [ ! -s "$GRADEX_BACKUP_COMPLETED_AT_FILE" ]; then
    failures+=(backup_missing)
  else
    completed_at="$(tr -d '[:space:]' <"$GRADEX_BACKUP_COMPLETED_AT_FILE")"
    now="$(date +%s)"
    if ! [[ "$completed_at" =~ ^[0-9]+$ ]] || [ "$completed_at" -gt "$now" ] ||
      [ $((now - completed_at)) -gt "$GRADEX_BACKUP_MAX_AGE_SECONDS" ]; then
      failures+=(backup_stale)
    fi
  fi
fi

[ "${#failures[@]}" -gt 0 ] || {
  note "all configured checks passed"
  exit 0
}

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

webhook_args=(--fail --silent --show-error --connect-timeout 5 --max-time 10
  --header 'Content-Type: application/json' --data "$payload")
if [ -n "${GRADEX_ALERT_WEBHOOK_TOKEN:-}" ]; then
  webhook_args+=(--header "Authorization: Bearer $GRADEX_ALERT_WEBHOOK_TOKEN")
fi
if ! curl "${webhook_args[@]}" "$GRADEX_ALERT_WEBHOOK_URL" >/dev/null; then
  note "checks failed and alert delivery failed (correlation_id=$correlation_id)"
  exit 1
fi
note "checks failed and alert delivery succeeded (correlation_id=$correlation_id)"
exit 1
