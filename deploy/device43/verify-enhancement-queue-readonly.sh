#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRATCH="$(mktemp -d)"
trap 'rm -rf -- "$SCRATCH"' EXIT
cat >"$SCRATCH/docker" <<'MOCK_DOCKER'
#!/usr/bin/env bash
set -euo pipefail
if [ "$1" = ps ]; then
  printf 'fixture-redis\n'
  exit 0
fi
[ "$1" = exec ] || exit 90
shift 2
[ "$1" = redis-cli ] || exit 91
shift
while [ "$#" -gt 0 ]; do
  case "$1" in
    --tls|--raw) shift ;;
    --cacert|-h) shift 2 ;;
    *) break ;;
  esac
done
  operation="$1"
  shift
  printf '%s\n' "$operation" >>"$DOCKER_READS_LOG"
  case "$operation" in
    # The registry can say absent while completed retention data remains.
    SISMEMBER) printf '0\n' ;;
  LRANGE)
    case "$1" in
      'asynq:{default}:pending') printf 'enhancement-pending\nscan-pending\n' ;;
      'asynq:{default}:active') printf 'enhancement-active\n' ;;
      *) exit 92 ;;
    esac
    ;;
  ZRANGE)
    case "$1" in
      'asynq:{default}:scheduled') ;;
      'asynq:{default}:retry') printf 'enhancement-retry\n' ;;
      'asynq:{default}:archived') printf 'scan-archived\n' ;;
      'asynq:{default}:completed') printf 'enhancement-completed\n' ;;
      'asynq:{default}:g:fixture-group') printf 'enhancement-aggregating\n' ;;
      *) exit 93 ;;
    esac
    ;;
  SMEMBERS)
    [ "$1" = 'asynq:{default}:groups' ] || exit 94
    printf 'fixture-group\n'
    ;;
  HGET)
    case "$1 $2" in
      'asynq:{default}:t:enhancement-pending msg'|\
      'asynq:{default}:t:enhancement-active msg'|\
      'asynq:{default}:t:enhancement-retry msg'|\
      'asynq:{default}:t:enhancement-completed msg'|\
      'asynq:{default}:t:enhancement-aggregating msg')
        printf '\x0a\x11media:enhancement'
        ;;
      'asynq:{default}:t:scan-pending msg'|\
      'asynq:{default}:t:scan-archived msg')
        printf '\x0a\x0amedia:scan'
        ;;
      *) exit 95 ;;
    esac
    ;;
  *) exit 96 ;;
esac
MOCK_DOCKER
chmod 700 "$SCRATCH/docker"

output="$(DOCKER_READS_LOG="$SCRATCH/redis-commands" PATH="$SCRATCH:$PATH" \
  bash "$ROOT/deploy/device43/inspect-enhancement-queue-readonly.sh")"
for expected in \
  'queue=default state=pending total=2 media_enhancement=1' \
  'queue=default state=active total=1 media_enhancement=1' \
  'queue=default state=scheduled total=0 media_enhancement=0' \
  'queue=default state=retry total=1 media_enhancement=1' \
  'queue=default state=archived total=1 media_enhancement=0' \
  'queue=default state=completed total=1 media_enhancement=1' \
  'queue=default state=aggregating total=1 media_enhancement=1'; do
  grep --fixed-strings --line-regexp --quiet "$expected" <<<"$output"
done
if grep --extended-regexp --quiet '^(DEL|UNLINK|LTRIM|LREM|ZREM|SREM|FLUSH|EVAL|EVALSHA|SET|HSET|LPUSH|RPUSH)$' "$SCRATCH/redis-commands"; then
  printf 'enhancement queue verifier issued a Redis write command\n' >&2
  exit 1
fi
printf 'enhancement-queue-readonly verification PASS\n'
