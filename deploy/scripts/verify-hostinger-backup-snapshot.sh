#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SNAPSHOT_ID=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
CHECK_STATUS=0
RETURNED_SNAPSHOT_ID="$SNAPSHOT_ID"
TMP=""

note() { printf 'hostinger-backup-snapshot: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

cleanup() {
  if [ -n "$TMP" ] && [ -d "$TMP" ]; then
    rm -rf -- "$TMP"
  fi
}

extract_host_function() {
  local function_name="$1"
  awk -v function_name="$function_name" '
    $0 == function_name "() {" { capture=1 }
    capture { print }
    capture && /^}/ { exit }
  ' "$ROOT/deploy/hostinger/host.sh"
}

extract_backup_function() {
  local function_name="$1"
  awk -v function_name="$function_name" '
    $0 == function_name "() {" { capture=1 }
    capture { print }
    capture && /^}/ { exit }
  ' "$ROOT/deploy/hostinger/backup-restic.sh"
}

backup_snapshot_directory() {
  printf 'snapshot upload diagnostic\n' >&2
  printf '%s\n' "$RETURNED_SNAPSHOT_ID"
}

backup_snapshot_exists() {
  local snapshot_id="$1" snapshot_log="$2"
  [ "$snapshot_id" = "$SNAPSHOT_ID" ] ||
    die "snapshot assertion received a contaminated ID"
  printf '%s\n' "$snapshot_id" >>"$TMP/snapshot-exists.calls"
  : >"$snapshot_log"
}

backup_check_repository() {
  printf 'repository check progress\n'
  printf 'repository check diagnostic\n' >&2
  return "$CHECK_STATUS"
}

backup_prune_repository() {
  printf 'retention progress\n'
}

backup_assert_repository_has_snapshot() {
  : >"$1"
}

eval "$(extract_host_function validate_snapshot_id)"
eval "$(extract_host_function upload_backup_snapshot)"
eval "$(extract_host_function apply_backup_retention)"

TMP="$(mktemp -d)"
trap cleanup EXIT
mkdir -p "$TMP/staging"

snapshot_stdout="$(
  upload_backup_snapshot "$TMP/staging" "$TMP/upload.json" "$TMP/snapshot.json" \
    2>"$TMP/upload.stderr"
)"

[ "$snapshot_stdout" = "$SNAPSHOT_ID" ] ||
  die "upload stdout was not exactly one valid snapshot ID"
[[ "$snapshot_stdout" =~ ^[0-9a-f]{64}$ ]] ||
  die "upload stdout was not a 64-character lowercase hexadecimal snapshot ID"
grep --quiet --fixed-strings 'repository check progress' "$TMP/upload.stderr" ||
  die "repository check progress was not preserved on stderr"
grep --quiet --fixed-strings 'repository check diagnostic' "$TMP/upload.stderr" ||
  die "repository check diagnostic was not preserved on stderr"

RETURNED_SNAPSHOT_ID=invalid
if (upload_backup_snapshot "$TMP/staging" "$TMP/invalid-upload.json" \
  "$TMP/invalid-snapshot.json") >"$TMP/invalid.stdout" 2>"$TMP/invalid.stderr"; then
  die "upload accepted an invalid snapshot ID"
fi
RETURNED_SNAPSHOT_ID="$SNAPSHOT_ID"

CHECK_STATUS=1
if (upload_backup_snapshot "$TMP/staging" "$TMP/failed-upload.json" \
  "$TMP/failed-snapshot.json") >"$TMP/failed.stdout" 2>"$TMP/failed.stderr"; then
  die "repository integrity failure did not propagate"
fi
[ ! -s "$TMP/failed.stdout" ] ||
  die "failed repository check emitted a machine-readable snapshot ID"
grep --quiet --fixed-strings 'repository check diagnostic' "$TMP/failed.stderr" ||
  die "failed repository check lost its diagnostic output"
CHECK_STATUS=0

apply_backup_retention "$SNAPSHOT_ID" "$TMP/retention.json" \
  >"$TMP/retention.stdout" 2>"$TMP/retention.stderr" ||
  die "new snapshot did not survive the retention assertion path"
[ "$(tail -n 1 "$TMP/snapshot-exists.calls")" = "$SNAPSHOT_ID" ] ||
  die "retention checked a snapshot other than the newly-created one"
grep --quiet --fixed-strings 'retention progress' "$TMP/retention.stdout" ||
  die "retention progress output was lost"
grep --quiet --fixed-strings "encrypted offsite backup $SNAPSHOT_ID created and retention applied" \
  "$TMP/retention.stderr" || die "retention success diagnostic was lost"

backup_restic() {
  printf '%s\n' "$@" >"$TMP/restic-arguments"
}
GRADEX_BACKUP_SNAPSHOT_TAG=gradex-test
GRADEX_BACKUP_RETENTION_LAST=2
GRADEX_BACKUP_RETENTION_HOURLY=48
GRADEX_BACKUP_RETENTION_DAILY=14
GRADEX_BACKUP_RETENTION_WEEKLY=8
eval "$(extract_backup_function backup_prune_repository)"
backup_prune_repository
[ "$(awk '$0 == "--group-by" { getline; print }' "$TMP/restic-arguments")" = host,tags ] ||
  die "retention does not group snapshots by stable host and tag identity"
if grep --quiet --fixed-strings 'host,paths,tags' "$TMP/restic-arguments"; then
  die "retention still groups snapshots by unique staging paths"
fi

eval "$(extract_host_function cleanup_successful_backup_staging)"
eval "$(extract_host_function cleanup_stale_backup_staging)"
eval "$(extract_host_function finalize_backup_success)"
eval "$(extract_backup_function backup_write_state_file)"

S12_BACKUP_DIR="$TMP/backups"
mkdir -p "$S12_BACKUP_DIR/.offsite-staging.failed"
printf '%s\n' previous-snapshot >"$S12_BACKUP_DIR/latest.offsite.snapshot"
printf '%s\n' 100 >"$S12_BACKUP_DIR/latest.completed-at"
if (finalize_backup_success "$S12_BACKUP_DIR/.offsite-staging.failed" \
  "$SNAPSHOT_ID" 23) >"$TMP/finalize-failed.stdout" 2>"$TMP/finalize-failed.stderr"; then
  die "retention failure was finalized as a successful backup"
fi
[ -d "$S12_BACKUP_DIR/.offsite-staging.failed" ] ||
  die "retention failure removed protected staging"
[ "$(cat "$S12_BACKUP_DIR/latest.offsite.snapshot")" = previous-snapshot ] ||
  die "retention failure changed the latest snapshot marker"
[ "$(cat "$S12_BACKUP_DIR/latest.completed-at")" = 100 ] ||
  die "retention failure refreshed the completion marker"

mkdir -p "$S12_BACKUP_DIR/.offsite-staging.success"
S12_BACKUP_STAGING_DIR="$S12_BACKUP_DIR/.offsite-staging.success"
finalize_backup_success "$S12_BACKUP_STAGING_DIR" "$SNAPSHOT_ID" 0 \
  >"$TMP/finalize-success.stdout" 2>"$TMP/finalize-success.stderr"
[ ! -e "$S12_BACKUP_DIR/.offsite-staging.success" ] ||
  die "successful backup retained current plaintext staging"
[ ! -e "$S12_BACKUP_DIR/.offsite-staging.failed" ] ||
  die "successful backup retained stale plaintext staging"
[ "$(cat "$S12_BACKUP_DIR/latest.offsite.snapshot")" = "$SNAPSHOT_ID" ] ||
  die "successful backup did not update the snapshot marker"
[[ "$(cat "$S12_BACKUP_DIR/latest.completed-at")" =~ ^[0-9]+$ ]] ||
  die "successful backup did not update the completion marker"

note 'snapshot stdout, diagnostics, stable retention, retention failure, and success finalization passed'
