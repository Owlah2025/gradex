#!/usr/bin/env bash

# Proves the canonical restore path resolves its source PostgreSQL by explicit
# container identity and builds its verification target without touching the live
# application Compose project.
#
# The regression this guards was observed on the real Hostinger staging host:
# `prepare_restore_database` resolved its source through `service_id postgres`,
# which only answers when the live topology is this repository's Compose project.
# Founder Beta drives its own Compose file, so the canonical restore died with
# "source PostgreSQL is absent", and the only way to make that lookup answer would
# have been to reconcile this repository's model against the running application —
# injecting restore-postgres and api-restore into it.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

die() {
  printf 'hostinger-restore-source: %s\n' "$*" >&2
  exit 1
}

S12_RESTORE_VERIFY_CONTAINER=gradex-restore-verify
S12_RESTORE_VERIFY_VOLUME=gradex-restore-verify-data
S12_RESTORE_POSTGRES_IMAGE=postgres:16.14-alpine
RESTORE_POSTGRES_PASSWORD=restore-verification-password

DOCKER_CALL_LOG="$(mktemp)"
trap 'rm -f -- "$DOCKER_CALL_LOG"' EXIT

note() { :; }

# Present so a regression that reintroduces a Compose lookup is caught rather than
# silently falling back to it.
compose() {
  printf 'compose %s\n' "$*" >>"$DOCKER_CALL_LOG"
  printf 'compose must not be reached by the restore path: %s\n' "$*" >&2
  return 99
}

service_id() {
  printf 'service_id %s\n' "$*" >>"$DOCKER_CALL_LOG"
  printf 'service_id must not be reached by the restore path: %s\n' "$*" >&2
  return 99
}

wait_for_status() {
  printf 'wait_for_status %s\n' "$*" >>"$DOCKER_CALL_LOG"
  printf 'wait_for_status must not be reached by the restore path: %s\n' "$*" >&2
  return 99
}

docker() {
  printf 'docker %s\n' "$*" >>"$DOCKER_CALL_LOG"
  case "$*" in
    "inspect --type container --format {{.Id}} gradex-founder-beta-postgres-1")
      printf '%s\n' live-source-id
      ;;
    "inspect --format {{.State.Running}} live-source-id")
      printf '%s\n' true
      ;;
    "inspect --type container --format {{.Id}} missing-postgres")
      return 1
      ;;
    "inspect --type container --format {{.Id}} $S12_RESTORE_VERIFY_CONTAINER")
      printf '%s\n' restore-target-id
      ;;
    "volume inspect $S12_RESTORE_VERIFY_VOLUME") return 1 ;;
    "volume create $S12_RESTORE_VERIFY_VOLUME") printf '%s\n' "$S12_RESTORE_VERIFY_VOLUME" ;;
    "rm --force $S12_RESTORE_VERIFY_CONTAINER") return 0 ;;
    run*) return 0 ;;
    "exec $S12_RESTORE_VERIFY_CONTAINER pg_isready --username gradex_restore --dbname gradex_restore")
      return 0
      ;;
    *)
      printf 'unexpected docker call: %s\n' "$*" >&2
      return 99
      ;;
  esac
}

extract() {
  awk -v name="$1" '
    $0 ~ "^" name "\\(\\)" { capture = 1 }
    capture { print }
    capture && /^}/ { exit }
  ' "$ROOT/deploy/hostinger/host.sh"
}

eval "$(extract backup_postgres_id)"
eval "$(extract restore_target_id)"
eval "$(extract wait_for_restore_target)"
eval "$(extract prepare_restore_database)"

GRADEX_BACKUP_POSTGRES_CONTAINER=gradex-founder-beta-postgres-1

target="$(prepare_restore_database)"
[ "$target" = restore-target-id ] ||
  die "restore target resolution failed: $target"

grep --quiet --fixed-strings "docker run --detach --name $S12_RESTORE_VERIFY_CONTAINER" "$DOCKER_CALL_LOG" ||
  die "restore target was not created as a standalone container"

if grep --quiet --extended-regexp '^(compose|service_id|wait_for_status) ' "$DOCKER_CALL_LOG"; then
  die "restore path reconciled the application Compose project"
fi

if grep --quiet --fixed-strings 'restore-data' "$DOCKER_CALL_LOG"; then
  die "restore path still targets the Compose project restore volume"
fi

if grep --quiet --extended-regexp 'docker run .*(--publish|-p )' "$DOCKER_CALL_LOG"; then
  die "restore target published a port"
fi

# The isolation assertion must still hold: a target that resolved to the source
# container is a failure, not a pass.
if (
  docker() {
    case "$*" in
      "inspect --type container --format {{.Id}} gradex-founder-beta-postgres-1") printf '%s\n' same-id ;;
      "inspect --format {{.State.Running}} same-id") printf '%s\n' true ;;
      "inspect --type container --format {{.Id}} $S12_RESTORE_VERIFY_CONTAINER") printf '%s\n' same-id ;;
      "volume inspect $S12_RESTORE_VERIFY_VOLUME") return 1 ;;
      *) return 0 ;;
    esac
  }
  prepare_restore_database >/dev/null 2>&1
); then
  die "restore target identical to the source was accepted"
fi

# Negative: a configured source container that does not exist must fail safely.
if (
  GRADEX_BACKUP_POSTGRES_CONTAINER=missing-postgres
  prepare_restore_database >/dev/null 2>&1
); then
  die "missing source PostgreSQL container was accepted"
fi

# Negative: an unsafe container name must be refused before any docker call.
if (
  GRADEX_BACKUP_POSTGRES_CONTAINER="../bad"
  prepare_restore_database >/dev/null 2>&1
); then
  die "unsafe source PostgreSQL container name was accepted"
fi

# Negative: the verification database must refuse to start without a password.
if (
  unset RESTORE_POSTGRES_PASSWORD
  prepare_restore_database >/dev/null 2>&1
); then
  die "restore verification database was created without a password"
fi

grep --quiet --fixed-strings 'GRADEX_RESTORE_POSTGRES_IMAGE=' \
  "$ROOT/deploy/hostinger/runtime.env.example" ||
  die "runtime example does not document the restore verification image"

printf '%s\n' \
  'hostinger-restore-source: explicit source identity, standalone isolated target, no Compose reconciliation, and negative source/password checks passed'
