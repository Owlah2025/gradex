#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
POSTGRES_DB=gradex_test

die() {
  printf 'hostinger-backup-dump: %s\n' "$*" >&2
  exit 1
}

docker() {
  case "${1:-} ${2:-}" in
    "exec fake-postgres")
      case "$*" in
        *"psql "*"FROM schema_migrations;"*)
          printf '%s\n' '28|false'
          ;;
        *"psql "*"FROM enrollments);"*)
          printf '%s\n' '8|3|2|0|2'
          ;;
        *"pg_dump "*)
          printf '%s\n' 'fake-postgres-custom-dump'
          ;;
        *)
          printf 'unexpected PostgreSQL command: %s\n' "$*" >&2
          exit 98
          ;;
      esac
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
  [ "$(sed -n '1p' "$schema_file")" = '28|false' ] ||
    die "schema metadata is incorrect"
}

eval "$(
  awk '
    /^source_schema_state\(\)/ { capture=1 }
    capture { print }
    capture && /^}/ { exit }
  ' "$ROOT/deploy/hostinger/host.sh"
)"

eval "$(
  awk '
    /^source_record_counts\(\)/ { capture=1 }
    capture { print }
    capture && /^}/ { exit }
  ' "$ROOT/deploy/hostinger/host.sh"
)"

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

# Line 1 must stay byte-identical to the pre-record-count artifact so a snapshot
# captured by either version restores and validates its schema the same way.
[ "$(sed -n '1p' "$TMP/gradex-20260826T000000Z.dump.schema-state")" = "28|false" ] ||
  die "final schema state is incorrect"

[ "$(sed -n '2p' "$TMP/gradex-20260826T000000Z.dump.schema-state")" = "counts=8|3|2|0|2" ] ||
  die "source record counts were not recorded for restore comparison"

printf '%s\n' \
  'hostinger-backup-dump: strict-unset dump capture and recorded source-count regression passed'
