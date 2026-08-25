#!/usr/bin/env bash

set -euo pipefail

note() { printf 'gradex-service-capture: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

main() {
  [ "$#" = 1 ] || die "usage: capture-services.sh OUTPUT_DIRECTORY"
  local output timeout dsn redis_addr redis_password redis_tls redis_ca
  for tool in date jq mkdir psql redis-cli readlink timeout; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done
  output="$(readlink -m -- "$1")"
  [ "$output" != / ] || die "output directory cannot be root"
  [ ! -e "$output" ] && [ ! -L "$output" ] || die "output directory already exists"
  mkdir -m 700 -- "$output"
  timeout="${GRADEX_LOADTEST_DIAGNOSTIC_TIMEOUT_SECONDS:-10}"
  [[ "$timeout" =~ ^[1-9][0-9]*$ ]] && [ "$timeout" -le 60 ] || die "diagnostic timeout must be between 1 and 60 seconds"

  dsn="${GRADEX_LOADTEST_POSTGRES_DSN:-}"
  [ -n "$dsn" ] || die "GRADEX_LOADTEST_POSTGRES_DSN is required"
  timeout "$timeout" psql "$dsn" -X -v ON_ERROR_STOP=1 -At -F $'\t' <<'SQL' >"$output/postgres.tsv"
SELECT 'connections', count(*)::text FROM pg_stat_activity WHERE datname = current_database();
SELECT 'active', count(*)::text FROM pg_stat_activity WHERE datname = current_database() AND state = 'active';
SELECT 'idle', count(*)::text FROM pg_stat_activity WHERE datname = current_database() AND state = 'idle';
SELECT 'waiting', count(*)::text FROM pg_stat_activity WHERE datname = current_database() AND wait_event IS NOT NULL;
SELECT 'long_running', count(*)::text FROM pg_stat_activity WHERE datname = current_database() AND state = 'active' AND now() - query_start > interval '5 seconds';
SQL
  chmod 600 "$output/postgres.tsv"

  redis_addr="${GRADEX_LOADTEST_REDIS_ADDR:-}"
  redis_password="${GRADEX_LOADTEST_REDIS_PASSWORD:-}"
  redis_tls="${GRADEX_LOADTEST_REDIS_TLS:-1}"
  redis_ca="${GRADEX_LOADTEST_REDIS_CA_FILE:-}"
  [ -n "$redis_addr" ] && [ -n "$redis_password" ] || die "Redis address and password are required through protected environment variables"
  local -a redis_args
  export REDISCLI_AUTH="$redis_password"
  redis_args=( -h "${redis_addr%:*}" -p "${redis_addr##*:}" --no-auth-warning )
  [ "$redis_tls" = 1 ] && redis_args+=( --tls )
  [ -n "$redis_ca" ] && redis_args+=( --cacert "$redis_ca" )
  timeout "$timeout" redis-cli "${redis_args[@]}" INFO >"$output/redis-info.txt"
  chmod 600 "$output/redis-info.txt"
  timeout "$timeout" redis-cli "${redis_args[@]}" INFO commandstats >"$output/redis-commandstats.txt"
  chmod 600 "$output/redis-commandstats.txt"

  # The Go capture helper is the authoritative Asynq/worker snapshot. This script only records
  # aggregate Redis INFO; it never scans keys, dumps payloads, flushes Redis, or writes a probe key.
  note "captured bounded PostgreSQL and Redis aggregate diagnostics; run the Asynq helper for queue metrics"
}

main "$@"
