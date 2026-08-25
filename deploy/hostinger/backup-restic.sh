#!/usr/bin/env bash
# shellcheck shell=bash

GRADEX_BACKUP_SNAPSHOT_TAG=${GRADEX_BACKUP_SNAPSHOT_TAG:-gradex-production}
GRADEX_BACKUP_DUMP_TAG=${GRADEX_BACKUP_DUMP_TAG:-postgresql-custom}

backup_note() {
  printf 'gradex-backup: %s\n' "$*" >&2
}

backup_fail() {
  backup_note "$*"
  return 1
}

backup_require_values() {
  local name
  for name in GRADEX_BACKUP_S3_ENDPOINT GRADEX_BACKUP_S3_BUCKET \
    GRADEX_BACKUP_S3_ACCESS_KEY GRADEX_BACKUP_S3_SECRET_KEY GRADEX_BACKUP_PASSWORD_FILE; do
    if [ -z "${!name:-}" ]; then
      backup_fail "$name is required in the protected runtime environment"
      return
    fi
  done
}

backup_validate_endpoint() {
  case "$GRADEX_BACKUP_S3_ENDPOINT" in
    https://*)
      if ! [[ "$GRADEX_BACKUP_S3_ENDPOINT" =~ ^https://[A-Za-z0-9._:-]+(/[A-Za-z0-9._/-]+)?$ ]]; then
        backup_fail "GRADEX_BACKUP_S3_ENDPOINT is malformed"
        return
      fi
      ;;
    http://*)
      if [ "${GRADEX_BACKUP_TEST_MODE:-false}" != true ]; then
        backup_fail "HTTP backup endpoints are allowed only in explicit test mode"
        return
      fi
      if ! [[ "$GRADEX_BACKUP_S3_ENDPOINT" =~ ^http://(127\.0\.0\.1|localhost)(:[0-9]+)?$ ]]; then
        backup_fail "test-mode HTTP backup endpoint must target localhost"
        return
      fi
      ;;
    *) backup_fail "GRADEX_BACKUP_S3_ENDPOINT must use HTTPS"; return ;;
  esac
  if [[ "$GRADEX_BACKUP_S3_ENDPOINT" == *[$'\r\n\"\\?#@ ']* ]]; then
    backup_fail "GRADEX_BACKUP_S3_ENDPOINT contains unsafe characters"
    return
  fi
}

backup_validate_storage_names() {
  if ! [[ "$GRADEX_BACKUP_S3_BUCKET" =~ ^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$ ]]; then
    backup_fail "GRADEX_BACKUP_S3_BUCKET is invalid"
    return
  fi
  if ! [[ "$GRADEX_BACKUP_S3_PREFIX" =~ ^[A-Za-z0-9][A-Za-z0-9._/-]*$ ]]; then
    backup_fail "GRADEX_BACKUP_S3_PREFIX is invalid"
    return
  fi
  case "$GRADEX_BACKUP_S3_PREFIX" in
    */*//*|/*|*/|*..*) backup_fail "GRADEX_BACKUP_S3_PREFIX is unsafe"; return ;;
  esac
  case "$GRADEX_BACKUP_S3_BUCKET_LOOKUP" in
    auto|dns|path) ;;
    *) backup_fail "GRADEX_BACKUP_S3_BUCKET_LOOKUP is invalid"; return ;;
  esac
}

backup_validate_positive_integer() {
  local name="$1"
  local minimum="$2"
  local number="${!name:-}"
  if ! [[ "$number" =~ ^[1-9][0-9]*$ ]]; then
    backup_fail "$name must be a positive integer"
    return
  fi
  if [ "$number" -lt "$minimum" ]; then
    backup_fail "$name must be at least $minimum"
    return
  fi
}

backup_validate_retention() {
  backup_validate_positive_integer GRADEX_BACKUP_RETENTION_LAST 1 || return
  backup_validate_positive_integer GRADEX_BACKUP_RETENTION_HOURLY 1 || return
  backup_validate_positive_integer GRADEX_BACKUP_RETENTION_DAILY 1 || return
  backup_validate_positive_integer GRADEX_BACKUP_RETENTION_WEEKLY 1 || return
}

backup_validate_timeout() {
  backup_validate_positive_integer GRADEX_BACKUP_TIMEOUT_SECONDS 1
}

backup_validate_password_file() {
  if [ ! -f "$GRADEX_BACKUP_PASSWORD_FILE" ]; then
    backup_fail "GRADEX_BACKUP_PASSWORD_FILE is absent"
    return
  fi
  local mode
  mode="$(stat -c '%a' "$GRADEX_BACKUP_PASSWORD_FILE")"
  case "$mode" in
    400|600) ;;
    *) backup_fail "backup password file must have mode 0400 or 0600"; return ;;
  esac
  if [ ! -s "$GRADEX_BACKUP_PASSWORD_FILE" ]; then
    backup_fail "backup password file is empty"
    return
  fi
  if [ ! -r "$GRADEX_BACKUP_PASSWORD_FILE" ]; then
    backup_fail "backup password file is not readable by the backup user"
    return
  fi
  if [ -n "${S12_HOST_STATE_DIR:-}" ]; then
    case "$GRADEX_BACKUP_PASSWORD_FILE" in
      "$S12_HOST_STATE_DIR"/*) ;;
      *) backup_fail "backup password file must remain inside host state"; return ;;
    esac
  fi
}

backup_validate_binary() {
  if ! command -v timeout >/dev/null 2>&1; then
    backup_fail "timeout is required for bounded backup failure handling"
    return
  fi
  if [ ! -x "$GRADEX_BACKUP_RESTIC_BINARY" ]; then
    backup_fail "restic binary is absent or not executable: $GRADEX_BACKUP_RESTIC_BINARY"
    return
  fi
}

backup_validate_configuration() {
  GRADEX_BACKUP_TEST_MODE="${GRADEX_BACKUP_TEST_MODE:-false}"
  GRADEX_BACKUP_S3_REGION="${GRADEX_BACKUP_S3_REGION:-auto}"
  GRADEX_BACKUP_S3_PREFIX="${GRADEX_BACKUP_S3_PREFIX:-gradex-production-backups}"
  GRADEX_BACKUP_S3_BUCKET_LOOKUP="${GRADEX_BACKUP_S3_BUCKET_LOOKUP:-auto}"
  GRADEX_BACKUP_RESTIC_BINARY="${GRADEX_BACKUP_RESTIC_BINARY:-/usr/local/bin/restic}"
  GRADEX_BACKUP_RETENTION_LAST="${GRADEX_BACKUP_RETENTION_LAST:-2}"
  GRADEX_BACKUP_RETENTION_HOURLY="${GRADEX_BACKUP_RETENTION_HOURLY:-48}"
  GRADEX_BACKUP_RETENTION_DAILY="${GRADEX_BACKUP_RETENTION_DAILY:-14}"
  GRADEX_BACKUP_RETENTION_WEEKLY="${GRADEX_BACKUP_RETENTION_WEEKLY:-8}"
  GRADEX_BACKUP_TIMEOUT_SECONDS="${GRADEX_BACKUP_TIMEOUT_SECONDS:-300}"
  backup_require_values || return
  backup_validate_endpoint || return
  backup_validate_storage_names || return
  backup_validate_retention || return
  backup_validate_timeout || return
  backup_validate_password_file || return
  backup_validate_binary
}

backup_repository_uri() {
  local endpoint="${GRADEX_BACKUP_S3_ENDPOINT%/}"
  printf 's3:%s/%s/%s\n' "$endpoint" "$GRADEX_BACKUP_S3_BUCKET" "$GRADEX_BACKUP_S3_PREFIX"
}

backup_restic() {
  unset RESTIC_PASSWORD RESTIC_PASSWORD_COMMAND
  RESTIC_REPOSITORY="$(backup_repository_uri)" \
    RESTIC_PASSWORD_FILE="$GRADEX_BACKUP_PASSWORD_FILE" \
    AWS_ACCESS_KEY_ID="$GRADEX_BACKUP_S3_ACCESS_KEY" \
    AWS_SECRET_ACCESS_KEY="$GRADEX_BACKUP_S3_SECRET_KEY" \
    AWS_DEFAULT_REGION="$GRADEX_BACKUP_S3_REGION" \
    timeout --signal=TERM "$GRADEX_BACKUP_TIMEOUT_SECONDS" \
    "$GRADEX_BACKUP_RESTIC_BINARY" --no-cache \
    -o "s3.bucket-lookup=$GRADEX_BACKUP_S3_BUCKET_LOOKUP" "$@"
}

backup_require_repository() {
  backup_restic cat config >/dev/null 2>&1 ||
    backup_fail "encrypted backup repository is unavailable or uninitialized"
}

backup_initialize_repository() {
  if backup_restic cat config >/dev/null 2>&1; then
    backup_fail "encrypted backup repository is already initialized"
    return
  fi
  backup_restic init --repository-version=2
}

backup_snapshot_directory() {
  local input_directory="$1"
  local output_log="$2"
  local snapshot_id
  if [ ! -d "$input_directory" ]; then
    backup_fail "backup input directory is absent"
    return
  fi
  backup_restic backup --json --tag "$GRADEX_BACKUP_SNAPSHOT_TAG" \
    --tag "$GRADEX_BACKUP_DUMP_TAG" "$input_directory" >"$output_log" || return
  snapshot_id="$(jq -r 'select(.message_type == "summary") | .snapshot_id' "$output_log")"
  if ! [[ "$snapshot_id" =~ ^[0-9a-f]{64}$ ]]; then
    backup_fail "restic did not return a valid snapshot ID"
    return
  fi
  printf '%s\n' "$snapshot_id"
}

backup_snapshot_exists() {
  local snapshot_id="$1"
  local snapshot_log="$2"
  backup_restic snapshots --json --tag "$GRADEX_BACKUP_SNAPSHOT_TAG" "$snapshot_id" >"$snapshot_log" || return
  jq -e --arg snapshot_id "$snapshot_id" --arg tag "$GRADEX_BACKUP_SNAPSHOT_TAG" \
    'any(.[]; .id == $snapshot_id and ((.tags // []) | index($tag) != null))' \
    "$snapshot_log" >/dev/null
}

backup_latest_snapshot_id() {
  local snapshot_log="$1"
  backup_restic snapshots --json --tag "$GRADEX_BACKUP_SNAPSHOT_TAG" >"$snapshot_log" || return
  jq -r 'sort_by(.time) | last.id // empty' "$snapshot_log"
}

backup_check_repository() {
  backup_restic check
}

backup_deep_check_repository() {
  backup_restic check --read-data
}

backup_snapshot_file_path() {
  local listing_log="$1"
  local suffix="$2"
  jq -s -r --arg suffix "$suffix" '
    [.[] | select(.message_type == "node" and .type == "file" and (.path | endswith($suffix))) | .path]
    | if length == 1 then .[0] else error("snapshot file selection is not unique") end
  ' "$listing_log"
}

backup_extract_snapshot_file() {
  local snapshot_id="$1"
  local snapshot_path="$2"
  local output_file="$3"
  if ! [[ "$snapshot_path" = /* && "$snapshot_path" != *[$'\r\n']* ]]; then
    backup_fail "restic returned an unsafe snapshot path"
    return
  fi
  backup_restic dump "$snapshot_id" "$snapshot_path" >"$output_file"
}

backup_write_state_file() {
  local state_file="$1"
  local state_value="$2"
  local temporary_file
  temporary_file="$(mktemp "$state_file.XXXXXX")" || return
  chmod 600 "$temporary_file"
  printf '%s\n' "$state_value" >"$temporary_file"
  mv -- "$temporary_file" "$state_file"
}

backup_snapshot_count() {
  local snapshot_log="$1"
  backup_restic snapshots --json --tag "$GRADEX_BACKUP_SNAPSHOT_TAG" >"$snapshot_log" || return
  jq 'length' "$snapshot_log"
}

backup_prune_repository() {
  backup_restic forget --tag "$GRADEX_BACKUP_SNAPSHOT_TAG" \
    --keep-last "$GRADEX_BACKUP_RETENTION_LAST" \
    --keep-hourly "$GRADEX_BACKUP_RETENTION_HOURLY" \
    --keep-daily "$GRADEX_BACKUP_RETENTION_DAILY" \
    --keep-weekly "$GRADEX_BACKUP_RETENTION_WEEKLY" \
    --group-by host,paths,tags --prune
}

backup_assert_repository_has_snapshot() {
  local snapshot_log="$1"
  local snapshot_count
  snapshot_count="$(backup_snapshot_count "$snapshot_log")" || return
  [[ "$snapshot_count" =~ ^[1-9][0-9]*$ ]] ||
    backup_fail "retention left no encrypted backup snapshots"
}
