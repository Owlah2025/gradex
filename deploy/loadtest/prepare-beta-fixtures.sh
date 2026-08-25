#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TEMP_DIR=""

note() { printf 'gradex-beta-fixtures: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }
require_value() { [ -n "${!1:-}" ] || die "$1 is required in the protected runner environment"; }

cleanup() {
  if [ -n "$TEMP_DIR" ] && [ -d "$TEMP_DIR" ]; then
    rm -rf -- "$TEMP_DIR"
  fi
}

main() {
  [ "$#" = 1 ] || die "usage: prepare-beta-fixtures.sh OUTPUT_DIRECTORY"
  local output parent seed_binary run_id expected_prefix
  for tool in go jq mktemp readlink stat node; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done
  for name in GRADEX_LOADTEST_PASSWORD GRADEX_LOADTEST_RUN_ID GRADEX_E2E_ADMIN_DB_URL \
    GRADEX_E2E_TARGET_DB_NAME GRADEX_E2E_TARGET_DB_URL DATABASE_URL SESSION_CSRF_KEY; do
    require_value "$name"
  done
  [ "${GRADEX_E2E_ALLOW_DATABASE_RESET:-}" = 1 ] || die "GRADEX_E2E_ALLOW_DATABASE_RESET=1 is required"
  run_id="$GRADEX_LOADTEST_RUN_ID"
  [[ "$run_id" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{2,63}$ ]] || die "GRADEX_LOADTEST_RUN_ID is invalid"
  expected_prefix="capacity/${run_id}/"
  [ "${GRADEX_LOADTEST_STORAGE_PREFIX:-$expected_prefix}" = "$expected_prefix" ] || die "GRADEX_LOADTEST_STORAGE_PREFIX must be the exact run prefix $expected_prefix"
  export GRADEX_LOADTEST_STORAGE_PREFIX="$expected_prefix"

  output="$(readlink -m -- "$1")"
  [ "$output" != / ] || die "output directory cannot be root"
  [ ! -e "$output" ] && [ ! -L "$output" ] || die "output directory already exists"
  parent="$(dirname "$output")"
  [ -d "$parent" ] || die "output parent directory is absent"
  TEMP_DIR="$(mktemp -d "$parent/.gradex-beta-fixtures.XXXXXX")"
  chmod 700 "$TEMP_DIR"
  trap cleanup EXIT

  seed_binary="$TEMP_DIR/gradex-e2e-seed"
  (cd "$ROOT/backend" && go test -c -o "$seed_binary" ./cmd/e2e-seed)
  (cd "$ROOT/backend" && "$seed_binary" -beta-loadtest >"$TEMP_DIR/fixture.json")
  chmod 600 "$TEMP_DIR/fixture.json"
  jq --exit-status \
    '.schema_version == 2 and .profile == "limited-paid-beta" and .run_id == $run_id and
     .registered_accounts == 110 and (.students | length) == 104 and
     ([.students[] | select(.entitled == true)] | length) == 50 and
     (.courses | length) == 8 and (.operators | length) == 6 and
     (.fingerprint | startswith("sha256:")) and
     (tostring | contains("password") | not) and (tostring | contains("cookie") | not)'
    --arg run_id "$run_id" "$TEMP_DIR/fixture.json" >/dev/null || die "beta fixture manifest validation failed"

  export GRADEX_LOADTEST_FIXTURE_FILE="$TEMP_DIR/fixture.json"
  export GRADEX_LOADTEST_SESSION_FILE="$TEMP_DIR/sessions.json"
  (cd "$ROOT/backend" && "$seed_binary" -issue-beta-loadtest-sessions)
  [ "$(stat -c '%a' "$TEMP_DIR/sessions.json")" = 600 ] || die "beta session manifest does not have mode 0600"
  jq --exit-status \
    '.schema_version == 2 and .profile == "limited-paid-beta" and
     (.students | length) == 104 and (.operators | length) == 6 and
     (tostring | contains("password") | not)'
    "$TEMP_DIR/sessions.json" >/dev/null || die "beta session manifest shape validation failed"

  rm -f -- "$seed_binary"
  mv -- "$TEMP_DIR" "$output"
  TEMP_DIR=""
  trap - EXIT
  note "prepared exactly 110 disposable Accounts: 104 Students (50 entitled), 1 Admin, 5 Instructors"
}

main "$@"
