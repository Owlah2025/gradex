#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
HOST="$ROOT/deploy/hostinger/host.sh"

die() { printf 'schema41-gate-test: %s\n' "$*" >&2; exit 1; }
note() { :; }
read_schema_state() { printf '%s' "$MOCK_SCHEMA_STATE"; }

definition="$(awk '
  $0 ~ /^require_schema_clean_at\(\)/ { capture=1 }
  capture { print }
  capture && /^}/ { exit }
' "$HOST")"
[ -n "$definition" ] || die "require_schema_clean_at is missing"
eval "$definition"

MOCK_SCHEMA_STATE='41|false'
require_schema_clean_at 41
for case_value in '41|true' '40|false' '42|false'; do
  if output="$(
    ( MOCK_SCHEMA_STATE="$case_value"; require_schema_clean_at 41 ) 2>&1
  )"; then
    die "accepted unexpected schema marker $case_value"
  fi
  case "$case_value" in
    '41|true') [[ "$output" = *"schema is dirty"* ]] || die "dirty-41 refusal was unexpected: $output" ;;
    *) [[ "$output" = *"expected a clean 41"* ]] || die "wrong-version refusal was unexpected: $output" ;;
  esac
done
printf 'schema41-forward marker gate PASS\n'
