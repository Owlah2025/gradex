#!/usr/bin/env bash
set -euo pipefail

die() {
  printf 'enhancement-queue-readonly: %s\n' "$*" >&2
  exit 1
}

[ "$#" = 0 ] || die "no arguments are accepted"

redis_container="$(docker ps \
  --filter label=com.docker.compose.project=gradex-production \
  --filter label=com.docker.compose.service=redis \
  --format '{{.Names}}')" || die "could not inspect the production Redis container"
[ "$(printf '%s\n' "$redis_container" | sed '/^$/d' | wc -l)" = 1 ] ||
  die "expected exactly one running production Redis container"

redis_read() {
  docker exec "$redis_container" redis-cli --tls \
    --cacert /run/gradex/redis/ca.crt -h redis --raw "$@" 2>/dev/null
}

task_type_match() {
  local key="$1"
  redis_read HGET "$key" msg |
    od -An -v -tu1 |
    awk '
      {
        for (i = 1; i <= NF; i++) bytes[++count] = $i
      }
      END {
        if (count < 2 || bytes[1] != 10) exit 2
        pos = 2
        multiplier = 1
        msg_len = 0
        while (1) {
          if (pos > count) exit 2
          byte = bytes[pos]
          msg_len += (byte % 128) * multiplier
          pos++
          if (byte < 128) break
          multiplier *= 128
          if (multiplier > 2147483648) exit 2
        }
        if (msg_len < 1 || pos + msg_len - 1 > count) exit 2
        task_type = ""
        for (i = pos; i < pos + msg_len; i++) task_type = task_type sprintf("%c", bytes[i])
        if (task_type == "media:enhancement") print 1
        else print 0
      }
    '
}

inspect_ids() {
  local state="$1" collection="$2" key="$3" ids task_id matches total=0 enhancement=0
  case "$collection" in
    list) ids="$(redis_read LRANGE "$key" 0 -1)" || die "could not inspect $state IDs" ;;
    zset) ids="$(redis_read ZRANGE "$key" 0 -1)" || die "could not inspect $state IDs" ;;
    *) die "unknown queue collection" ;;
  esac
  if [ -n "$ids" ]; then
    while IFS= read -r task_id; do
      [ -n "$task_id" ] || continue
      [[ "$task_id" =~ ^[A-Za-z0-9._:-]+$ ]] || die "task identifier is not in the supported safe format"
      matches="$(task_type_match "asynq:{default}:t:$task_id")" || die "could not decode a queued Asynq task type"
      case "$matches" in
        0) ;;
        1) enhancement=$((enhancement + 1)) ;;
        *) die "unexpected Asynq task-type result" ;;
      esac
      total=$((total + 1))
    done <<<"$ids"
  fi
  printf 'queue=default state=%s total=%d media_enhancement=%d\n' "$state" "$total" "$enhancement"
}

# Inspect the state keys directly. Asynq may remove a queue from its registry
# while retained completed-task records still exist.
inspect_ids pending list 'asynq:{default}:pending'
inspect_ids active list 'asynq:{default}:active'
inspect_ids scheduled zset 'asynq:{default}:scheduled'
inspect_ids retry zset 'asynq:{default}:retry'
inspect_ids archived zset 'asynq:{default}:archived'
inspect_ids completed zset 'asynq:{default}:completed'

groups="$(redis_read SMEMBERS 'asynq:{default}:groups')" || die "could not inspect Asynq groups"
group_total=0
enhancement_total=0
if [ -n "$groups" ]; then
  while IFS= read -r group; do
    [ -n "$group" ] || continue
    [[ "$group" =~ ^[A-Za-z0-9._:-]+$ ]] || die "Asynq group name is not in the supported safe format"
    ids="$(redis_read ZRANGE "asynq:{default}:g:$group" 0 -1)" || die "could not inspect aggregating task IDs"
    if [ -n "$ids" ]; then
      while IFS= read -r task_id; do
        [ -n "$task_id" ] || continue
        [[ "$task_id" =~ ^[A-Za-z0-9._:-]+$ ]] || die "task identifier is not in the supported safe format"
        matches="$(task_type_match "asynq:{default}:t:$task_id")" || die "could not decode an aggregating task type"
        case "$matches" in
          0) ;;
          1) enhancement_total=$((enhancement_total + 1)) ;;
          *) die "unexpected Asynq task-type result" ;;
        esac
        group_total=$((group_total + 1))
      done <<<"$ids"
    fi
  done <<<"$groups"
fi
printf 'queue=default state=aggregating total=%d media_enhancement=%d\n' "$group_total" "$enhancement_total"
