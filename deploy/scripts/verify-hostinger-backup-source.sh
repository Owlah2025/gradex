#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

die() {
  printf 'hostinger-backup-source: %s\n' "$*" >&2
  exit 1
}

service_id() {
  [ "$1" = postgres ]
  printf '%s\n' fallback-postgres-id
}

docker() {
  case "$*" in
    "inspect --type container --format {{.Id}} gradex-founder-beta-postgres-1")
      printf '%s\n' explicit-container-id
      ;;
    "inspect --format {{.State.Running}} explicit-container-id")
      printf '%s\n' true
      ;;
    "inspect --type container --format {{.Id}} stopped-postgres")
      printf '%s\n' stopped-container-id
      ;;
    "inspect --format {{.State.Running}} stopped-container-id")
      printf '%s\n' false
      ;;
    "inspect --type container --format {{.Id}} missing-postgres")
      return 1
      ;;
    *)
      printf 'unexpected docker call: %s\n' "$*" >&2
      return 99
      ;;
  esac
}

eval "$(
  awk '
    /^backup_postgres_id\(\)/ { capture=1 }
    capture { print }
    capture && /^}/ { exit }
  ' "$ROOT/deploy/hostinger/host.sh"
)"

unset GRADEX_BACKUP_POSTGRES_CONTAINER
[ "$(backup_postgres_id)" = fallback-postgres-id ] ||
  die "default Compose PostgreSQL resolution failed"

GRADEX_BACKUP_POSTGRES_CONTAINER=gradex-founder-beta-postgres-1
[ "$(backup_postgres_id)" = explicit-container-id ] ||
  die "explicit PostgreSQL container resolution failed"

if (
  GRADEX_BACKUP_POSTGRES_CONTAINER="../bad"
  backup_postgres_id >/dev/null 2>&1
); then
  die "unsafe PostgreSQL container name was accepted"
fi

if (
  GRADEX_BACKUP_POSTGRES_CONTAINER=missing-postgres
  backup_postgres_id >/dev/null 2>&1
); then
  die "missing PostgreSQL container was accepted"
fi

if (
  GRADEX_BACKUP_POSTGRES_CONTAINER=stopped-postgres
  backup_postgres_id >/dev/null 2>&1
); then
  die "stopped PostgreSQL container was accepted"
fi

grep --quiet --fixed-strings \
  'GRADEX_BACKUP_POSTGRES_CONTAINER=' \
  "$ROOT/deploy/hostinger/runtime.env.example" ||
  die "runtime example does not document the explicit backup PostgreSQL source"

printf '%s\n' \
  'hostinger-backup-source: fallback, explicit source, validation, presence, and running-state checks passed'
