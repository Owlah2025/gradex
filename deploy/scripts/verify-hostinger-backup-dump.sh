#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
POSTGRES_DB=gradex_test

die() {
  printf 'hostinger-backup-dump: %s\n' "$*" >&2
  exit 1
}

source_schema_state() {
  printf '%s\n' '28|false'
}

docker() {
  case "${1:-} ${2:-}" in
    "exec fake-postgres")
      printf '%s\n' 'fake-postgres-custom-dump'
      ;;
    *)
      printf 'unexpected docker call: %s\n' "$*" >&2
      exit 99
      ;;
  esac
}

write_backup_metadata() {
  local staging_dir="$1" dump_file="$2" schema_file="$3"
  [ -s "$dump_file" ] ||
    die "dump file is empty"
  [ "$(cat "$schema_file")" = '28|false' ] ||
    die "schema metadata is incorrect"
}

eval "$(
  awk '
    /^capture_backup_dump\(\)/ { capture=1 }
    capture { print }
    capture && /^}/ { exit }
  ' "$ROOT/deploy/hostinger/host.sh"
)"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

capture_backup_dump fake-postgres "$TMP" 20260826T000000Z

[ -s "$TMP/gradex-20260826T000000Z.dump" ] ||
  die "final dump is absent"

[ "$(cat "$TMP/gradex-20260826T000000Z.dump.schema-state")" = "28|false" ] ||
  die "final schema state is incorrect"

printf '%s\n' \
  'hostinger-backup-dump: strict-unset dump capture regression passed'
