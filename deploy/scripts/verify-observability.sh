#!/usr/bin/env bash

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_COMPOSE_FILE="$S12_ROOT/deploy/compose/compose.production-like.yml"
S12_STATE_DIR="$S12_ROOT/deploy/.state"
S12_ENV_FILE="$S12_STATE_DIR/production-like.env"
S12_CA_FILE="$S12_STATE_DIR/caddy-root.crt"
S12_PROJECT="gradex-s12"
S12_TEMPORARY=""
S12_SINK_PID=""
S12_REDIS_STOPPED=0

note() { printf 's12-observability: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

compose() {
  sed -n '1,999p' "$S12_COMPOSE_FILE" |
    docker compose --file - --project-name "$S12_PROJECT" "$@"
}

cleanup() {
  if [ "$S12_REDIS_STOPPED" = "1" ]; then
    compose start redis >/dev/null 2>&1 || true
  fi
  if [ -n "$S12_SINK_PID" ]; then
    kill "$S12_SINK_PID" >/dev/null 2>&1 || true
    wait "$S12_SINK_PID" >/dev/null 2>&1 || true
  fi
  if [ -n "$S12_TEMPORARY" ] && [ -d "$S12_TEMPORARY" ]; then
    rm -rf -- "$S12_TEMPORARY"
  fi
}

main() {
  local tool
  for tool in curl docker jq mktemp python3; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done
  [ -f "$S12_ENV_FILE" ] || die "run environment.sh up first"
  [ -s "$S12_CA_FILE" ] || die "run verify-edge-security.sh first"
  set -a
  # shellcheck disable=SC1090
  . "$S12_ENV_FILE"
  set +a

  "$S12_ROOT/deploy/scripts/database-recovery.sh" backup
  local backup_marker="$S12_STATE_DIR/backups/gradex-s12.dump.completed-at"
  [ -s "$backup_marker" ] || die "backup completion marker is absent"

  S12_TEMPORARY="$(mktemp -d "$S12_STATE_DIR/observability.XXXXXX")"
  chmod 700 "$S12_TEMPORARY"
  trap cleanup EXIT
  local port_file="$S12_TEMPORARY/sink.port"
  local alerts_file="$S12_TEMPORARY/alerts.jsonl"
  local monitor_log="$S12_TEMPORARY/monitor.log"
  local worker_log="$S12_TEMPORARY/worker.log"
  local headers="$S12_TEMPORARY/request.headers"
  touch "$port_file" "$alerts_file" "$monitor_log" "$worker_log" "$headers"
  chmod 600 "$port_file" "$alerts_file" "$monitor_log" "$worker_log" "$headers"

  local proof_token="s12-disposable-alert-token"
  SINK_EXPECTED_TOKEN="$proof_token" \
    python3 "$S12_ROOT/deploy/monitoring/disposable-alert-sink.py" "$port_file" "$alerts_file" &
  S12_SINK_PID=$!
  local attempts=0
  while [ "$attempts" -lt 50 ] && [ ! -s "$port_file" ]; do
    attempts=$((attempts + 1))
    sleep 0.1
  done
  [ -s "$port_file" ] || die "disposable alert sink did not start"
  local sink_port
  sink_port="$(tr -d '[:space:]' <"$port_file")"
  [[ "$sink_port" =~ ^[0-9]+$ ]] || die "disposable alert sink returned an invalid port"

  local proof_started
  proof_started="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  compose stop redis >/dev/null
  S12_REDIS_STOPPED=1

  set +e
  GRADEX_HEALTH_URL="https://gradex.localhost:18443/healthz" \
    GRADEX_READY_URL="https://gradex.localhost:18443/readyz" \
    GRADEX_ENVIRONMENT="production-like" \
    GRADEX_MONITOR_CA_FILE="$S12_CA_FILE" \
    GRADEX_BACKUP_COMPLETED_AT_FILE="$backup_marker" \
    GRADEX_ALERT_WEBHOOK_URL="http://127.0.0.1:$sink_port/alerts" \
    GRADEX_ALERT_WEBHOOK_TOKEN="$proof_token" \
    "$S12_ROOT/deploy/monitoring/monitor-once.sh" >"$monitor_log" 2>&1
  local monitor_status=$?
  set -e
  [ "$monitor_status" = "1" ] || die "failed dependency monitor exited $monitor_status, want 1"

  attempts=0
  while [ "$attempts" -lt 50 ] && [ ! -s "$alerts_file" ]; do
    attempts=$((attempts + 1))
    sleep 0.1
  done
  if [ ! -s "$alerts_file" ]; then
    sed -n '1,80p' "$monitor_log" >&2
    die "disposable alert was not delivered"
  fi
  jq --exit-status --slurp '
    length == 1 and .[0].event == "gradex_monitor_failure" and
    .[0].environment == "production-like" and
    (. [0].failures | index("api_readiness") != null) and
    (. [0].correlation_id | startswith("monitor-"))
  ' "$alerts_file" >/dev/null || die "delivered alert did not match the fixed contract"

  attempts=0
  while [ "$attempts" -lt 40 ]; do
    compose logs --since "$proof_started" --no-color worker >"$worker_log"
    if grep --fixed-strings --quiet '"operation":"redis_health"' "$worker_log"; then
      break
    fi
    attempts=$((attempts + 1))
    sleep 0.5
  done
  [ "$attempts" -lt 40 ] || die "worker emitted no structured dependency failure"
  local worker_event
  worker_event="$(grep --fixed-strings '"operation":"redis_health"' "$worker_log" |
    sed -n 's/^[^{]*\({.*\)$/\1/p' | tail -1)"
  printf '%s\n' "$worker_event" | jq --exit-status '
    .msg == "worker_failure" and .service == "gradex-worker" and
    .environment == "production" and .error_class != null and
    (has("error") | not) and (has("payload") | not)
  ' >/dev/null || die "worker failure was not a safe structured event"

  if grep --fixed-strings --quiet "$proof_token" "$alerts_file" "$monitor_log" "$worker_log"; then
    die "alert credential appeared in observable output"
  fi

  compose start redis >/dev/null
  S12_REDIS_STOPPED=0
  "$S12_ROOT/deploy/scripts/environment.sh" verify

  GRADEX_HEALTH_URL="https://gradex.localhost:18443/healthz" \
    GRADEX_READY_URL="https://gradex.localhost:18443/readyz" \
    GRADEX_ENVIRONMENT="production-like" \
    GRADEX_MONITOR_CA_FILE="$S12_CA_FILE" \
    GRADEX_BACKUP_COMPLETED_AT_FILE="$backup_marker" \
    "$S12_ROOT/deploy/monitoring/monitor-once.sh" >/dev/null
  [ "$(wc -l <"$alerts_file" | tr -d '[:space:]')" = "1" ] ||
    die "healthy recovery emitted another alert"

  curl --silent --show-error --cacert "$S12_CA_FILE" --dump-header "$headers" \
    --output /dev/null https://gradex.localhost:18443/api/v1/s12-observability-not-found
  local request_id
  request_id="$(sed -n 's/^[Xx]-[Rr]equest-[Ii][Dd]:[[:space:]]*\([^[:space:]\r]*\).*/\1/p' "$headers" | tail -1)"
  [ -n "$request_id" ] || die "edge response omitted request correlation"
  compose logs --no-color api | grep --fixed-strings '"msg":"http_request"' |
    grep --fixed-strings --quiet "\"request_id\":\"$request_id\"" ||
    die "API log did not carry the response request ID"

  note "structured correlation, redaction, readiness/backup monitoring, and disposable alert delivery passed"
}

main "$@"
