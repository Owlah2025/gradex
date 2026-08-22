#!/usr/bin/env bash

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_TEMPORARY=""

note() { printf 'gradex-loadtest-fixtures: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

cleanup() {
  if [ -n "$S12_TEMPORARY" ] && [ -d "$S12_TEMPORARY" ]; then
    rm -rf -- "$S12_TEMPORARY"
  fi
}

require_value() {
  [ -n "${!1:-}" ] || die "$1 is required in the protected runner environment"
}

main() {
  [ "$#" = 1 ] || die "usage: prepare-fixtures.sh OUTPUT_DIRECTORY"
  local output parent seed_binary
  for tool in go jq mktemp readlink stat; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done
  for name in GRADEX_LOADTEST_PASSWORD GRADEX_E2E_ADMIN_DB_URL GRADEX_E2E_TARGET_DB_NAME \
    GRADEX_E2E_TARGET_DB_URL DATABASE_URL SESSION_CSRF_KEY; do
    require_value "$name"
  done
  [ "${GRADEX_E2E_ALLOW_DATABASE_RESET:-}" = 1 ] ||
    die "GRADEX_E2E_ALLOW_DATABASE_RESET=1 is required"

  output="$(readlink -m -- "$1")"
  [ "$output" != / ] || die "output directory cannot be root"
  [ ! -e "$output" ] && [ ! -L "$output" ] || die "output directory already exists"
  parent="$(dirname "$output")"
  [ -d "$parent" ] || die "output parent directory is absent"
  S12_TEMPORARY="$(mktemp -d "$parent/.gradex-loadtest-fixtures.XXXXXX")"
  chmod 700 "$S12_TEMPORARY"
  trap cleanup EXIT

  seed_binary="$S12_TEMPORARY/gradex-e2e-seed"
  (cd "$S12_ROOT/backend" && go test -c -o "$seed_binary" ./cmd/e2e-seed)
  (cd "$S12_ROOT/backend" && "$seed_binary" -loadtest >"$S12_TEMPORARY/fixture.json")
  chmod 600 "$S12_TEMPORARY/fixture.json"
  jq --exit-status \
    '.schema_version == 1 and .registered_accounts == 5000 and .active_students == 500 and (.students | length) == 500' \
    "$S12_TEMPORARY/fixture.json" >/dev/null || die "fixture manifest cardinality validation failed"

  export GRADEX_LOADTEST_SESSION_FILE="$S12_TEMPORARY/sessions.json"
  (cd "$S12_ROOT/backend" && "$seed_binary" -issue-loadtest-sessions)
  [ "$(stat -c '%a' "$S12_TEMPORARY/sessions.json")" = 600 ] ||
    die "protected session manifest does not have mode 0600"
  jq --exit-status '.schema_version == 1 and (.sessions | length) == 500' \
    "$S12_TEMPORARY/sessions.json" >/dev/null || die "session manifest cardinality validation failed"

  rm -f -- "$seed_binary"
  mv -- "$S12_TEMPORARY" "$output"
  S12_TEMPORARY=""
  trap - EXIT
  note "prepared 5,000 disposable Accounts and 500 protected sessions in a private directory"
}

main "$@"
