#!/usr/bin/env bash

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_COMPOSE_FILE="$S12_ROOT/deploy/compose/compose.production-like.yml"
S12_STATE_DIR="$S12_ROOT/deploy/.state"
S12_ENV_FILE="$S12_STATE_DIR/production-like.env"
S12_PROJECT="gradex-s12"
S12_NETWORK="${S12_PROJECT}_app"
S12_PROOF_DB="gradex_playwright_e2e_s12media01"
S12_ASSET_VERSION="90000000-0000-0000-0000-00000000e002"
S12_EVENT="90000000-0000-0000-0000-00000000e004"
S12_DUPLICATE_EVENT="90000000-0000-0000-0000-00000000e005"
S12_TEMPORARY=""

note() { printf 's12-worker-media: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

cleanup() {
  if [ -n "$S12_TEMPORARY" ] && [ -d "$S12_TEMPORARY" ]; then
    rm -rf -- "$S12_TEMPORARY"
  fi
}

require_tools() {
  local tool
  for tool in docker grep jq mktemp sed; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done
  docker info >/dev/null 2>&1 || die "Docker is not reachable"
}

compose() {
  sed -n '1,999p' "$S12_COMPOSE_FILE" |
    docker compose --file - --project-name "$S12_PROJECT" --profile media-proof "$@"
}

service_id() {
  compose ps --all --quiet "$1"
}

wait_for_status() {
  local service="$1" wanted="$2" attempts=0 container status
  container="$(service_id "$service")"
  [ -n "$container" ] || die "$service container is absent"
  while [ "$attempts" -lt 90 ]; do
    status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container")"
    if [ "$status" = "$wanted" ]; then
      return
    fi
    if [ "$status" = "exited" ] || [ "$status" = "dead" ] || [ "$status" = "unhealthy" ]; then
      compose logs --no-color "$service" >&2 || true
      die "$service reached $status while waiting for $wanted"
    fi
    attempts=$((attempts + 1))
    sleep 2
  done
  die "$service did not reach $wanted"
}

wait_for_completion() {
  local service="$1" attempts=0 container status exit_code
  container="$(service_id "$service")"
  [ -n "$container" ] || die "$service container is absent"
  while [ "$attempts" -lt 90 ]; do
    status="$(docker inspect --format '{{.State.Status}}' "$container")"
    if [ "$status" = "exited" ]; then
      exit_code="$(docker inspect --format '{{.State.ExitCode}}' "$container")"
      [ "$exit_code" = "0" ] || {
        compose logs --no-color "$service" >&2 || true
        die "$service exited $exit_code"
      }
      return
    fi
    attempts=$((attempts + 1))
    sleep 2
  done
  die "$service did not complete"
}

psql_value() {
  local sql="$1" postgres_id
  postgres_id="$(service_id postgres)"
  [ -n "$postgres_id" ] || die "postgres container is absent"
  docker exec "$postgres_id" psql --no-psqlrc --username gradex --dbname "$S12_PROOF_DB" \
    --tuples-only --no-align --command "$sql"
}

wait_for_query() {
  local sql="$1" wanted="$2" check="$3" attempts=0 got
  while [ "$attempts" -lt 90 ]; do
    got="$(psql_value "$sql")"
    if [ "$got" = "$wanted" ]; then
      return
    fi
    attempts=$((attempts + 1))
    sleep 2
  done
  die "$check did not converge (last value: $got)"
}

put_object() {
  local object_url="$1" key="$2"
  docker run --rm --interactive --network "$S12_NETWORK" \
    --env "MC_HOST_local=$object_url" minio/mc:RELEASE.2025-08-13T08-35-41Z \
    pipe "local/gradex-private-media/$key" >/dev/null
}

main() {
  require_tools
  [ -f "$S12_ENV_FILE" ] || die "run deploy/scripts/environment.sh prepare first"
  set -a
  # shellcheck disable=SC1090
  . "$S12_ENV_FILE"
  set +a
  docker image inspect "$GRADEX_BACKEND_IMAGE" "$GRADEX_PROOF_IMAGE" >/dev/null 2>&1 ||
    die "run deploy/scripts/environment.sh build first"

  local temporary worker_logs readiness_log session_json unentitled_session_json playback_json manifest_file manifest_headers segment_file denial_log
  local postgres_id api_id worker_id object_url object_stat object_version object_size
  local receipt_count state_counts cookie_name cookie_value unentitled_cookie manifest_url segment_url
  temporary="$(mktemp -d "$S12_STATE_DIR/worker-media.XXXXXX")"
  S12_TEMPORARY="$temporary"
  chmod 700 "$temporary"
  trap cleanup EXIT
  worker_logs="$temporary/worker.log"
  readiness_log="$temporary/readiness.log"
  session_json="$temporary/session.json"
  unentitled_session_json="$temporary/unentitled-session.json"
  playback_json="$temporary/playback.json"
  manifest_file="$temporary/playlist.m3u8"
  manifest_headers="$temporary/manifest.headers"
  segment_file="$temporary/segment.ts"
  denial_log="$temporary/denial.log"
  touch "$worker_logs" "$readiness_log" "$session_json" "$unentitled_session_json" \
    "$playback_json" "$manifest_file" "$manifest_headers" "$segment_file" "$denial_log"
  chmod 600 "$worker_logs" "$readiness_log" "$session_json" "$unentitled_session_json" \
    "$playback_json" "$manifest_file" "$manifest_headers" "$segment_file" "$denial_log"

  compose rm --stop --force api-media-proof worker-media-proof media-proof-tool media-proof-redis >/dev/null 2>&1 || true
  compose up --detach media-proof-tool media-proof-redis
  wait_for_completion media-proof-tool
  wait_for_status media-proof-redis healthy
  postgres_id="$(service_id postgres)"
  object_url="http://${S3_ACCESS_KEY}:${S3_SECRET_KEY}@minio:9000"

  docker run --rm --entrypoint ffmpeg "$GRADEX_BACKEND_IMAGE" \
    -nostdin -v error -f lavfi -i 'testsrc=size=320x240:rate=24' -t 1 \
    -c:v libx264 -pix_fmt yuv420p -movflags frag_keyframe+empty_moov -f mp4 pipe:1 |
    put_object "$object_url" 'quarantine/s12-worker/source.mp4'
  object_stat="$(docker run --rm --network "$S12_NETWORK" \
    --env "MC_HOST_local=$object_url" minio/mc:RELEASE.2025-08-13T08-35-41Z \
    stat --json local/gradex-private-media/quarantine/s12-worker/source.mp4)"
  object_version="$(printf '%s' "$object_stat" | jq --exit-status --raw-output '.versionID // .versionId | select(length > 0)')" ||
    die "source object did not report a version ID"
  object_size="$(printf '%s' "$object_stat" | jq --exit-status --raw-output '.size | select(type == "number" and . > 0)')" ||
    die "source object did not report a positive size"

  docker exec --interactive "$postgres_id" psql --no-psqlrc --username gradex --dbname "$S12_PROOF_DB" \
    --set="object_version=$object_version" --set="object_size=$object_size" \
    <"$S12_ROOT/deploy/proof/media-worker-fixture.sql" >/dev/null

  compose up --detach api-media-proof worker-media-proof
  wait_for_status api-media-proof healthy
  wait_for_status worker-media-proof running
  api_id="$(service_id api-media-proof)"
  worker_id="$(service_id worker-media-proof)"

  compose stop media-proof-redis >/dev/null
  if docker exec "$api_id" wget -S -O /dev/null http://127.0.0.1:8080/readyz \
    >"$readiness_log" 2>&1; then
    die "proof API stayed ready while Redis was unavailable"
  fi
  grep --fixed-strings --quiet '503 Service Unavailable' "$readiness_log" ||
    die "proof API did not report HTTP 503 while Redis was unavailable"

  docker exec --interactive "$postgres_id" psql --no-psqlrc --username gradex --dbname "$S12_PROOF_DB" \
    <"$S12_ROOT/deploy/proof/media-worker-event.sql" >/dev/null
  receipt_count="$(psql_value "SELECT count(*) FROM media_outbox_dispatches WHERE event_id = '$S12_EVENT'::uuid;")"
  [ "$receipt_count" = "0" ] || die "Redis outage incorrectly recorded an outbox dispatch"

  local attempts=0
  while [ "$attempts" -lt 30 ]; do
    compose logs --no-color worker-media-proof >"$worker_logs"
    if grep --fixed-strings --quiet 'media outbox dispatch failed' "$worker_logs"; then
      break
    fi
    attempts=$((attempts + 1))
    sleep 1
  done
  [ "$attempts" -lt 30 ] || die "worker did not expose the Redis dispatch failure"
  [ "$(docker inspect --format '{{.State.Status}}' "$worker_id")" = "running" ] ||
    die "worker stopped before Redis recovery"

  compose start media-proof-redis >/dev/null
  wait_for_status media-proof-redis healthy
  compose restart worker-media-proof >/dev/null
  wait_for_status worker-media-proof running
  wait_for_query \
    "SELECT state::text FROM media_asset_versions WHERE id = '$S12_ASSET_VERSION'::uuid;" \
    "READY" "worker transcode"
  wait_for_status api-media-proof healthy

  state_counts="$(psql_value "
    SELECT mav.state::text || '|' ||
      (SELECT count(*) FROM media_outbox_dispatches WHERE event_id = '$S12_EVENT'::uuid) || '|' ||
      (SELECT count(*) FROM processing_attempts WHERE asset_version_id = mav.id) || '|' ||
      (SELECT count(*) FROM video_renditions WHERE asset_version_id = mav.id)
    FROM media_asset_versions mav WHERE mav.id = '$S12_ASSET_VERSION'::uuid;")"
  [ "$state_counts" = "READY|1|1|1" ] ||
    die "worker result was $state_counts, expected READY|1|1|1"

  docker exec --interactive "$postgres_id" psql --no-psqlrc --username gradex --dbname "$S12_PROOF_DB" \
    <"$S12_ROOT/deploy/proof/media-worker-duplicate-event.sql" >/dev/null
  wait_for_query \
    "SELECT count(*) FROM media_outbox_dispatches WHERE event_id = '$S12_DUPLICATE_EVENT'::uuid;" \
    "1" "duplicate outbox dispatch"
  wait_for_query \
    "SELECT count(*) FROM processing_attempts WHERE asset_version_id = '$S12_ASSET_VERSION'::uuid;" \
    "1" "idempotent worker consumption"

  docker run --rm --network "$S12_NETWORK" --env "MC_HOST_local=$object_url" \
    minio/mc:RELEASE.2025-08-13T08-35-41Z \
    stat "local/gradex-private-media/media/$S12_ASSET_VERSION/hls/240p/playlist.m3u8" >/dev/null
  if docker run --rm --network "$S12_NETWORK" --entrypoint wget "$GRADEX_BACKEND_IMAGE" \
    -qO- 'http://minio:9000/gradex-private-media/quarantine/s12-worker/source.mp4' \
    >/dev/null 2>&1; then
    die "source media object was anonymously readable"
  fi
  if docker run --rm --network "$S12_NETWORK" --entrypoint wget "$GRADEX_BACKEND_IMAGE" \
    -qO- "http://minio:9000/gradex-private-media/media/$S12_ASSET_VERSION/hls/240p/playlist.m3u8" \
    >/dev/null 2>&1; then
    die "derived media playlist was anonymously readable"
  fi

  psql_value "UPDATE course_lessons SET video_asset_version_id = '$S12_ASSET_VERSION'::uuid WHERE id = '40000000-0000-0000-0000-000000000001'::uuid RETURNING 1;" >/dev/null
  compose run --rm --no-deps media-proof-tool \
    -issue-session -email student-active@example.test >"$session_json"
  cookie_name="$(jq --exit-status --raw-output '.cookie_name | select(length > 0)' "$session_json")" ||
    die "session proof omitted the cookie name"
  cookie_value="$(jq --exit-status --raw-output '.cookie_value | select(length > 0)' "$session_json")" ||
    die "session proof omitted the cookie value"

  docker exec "$api_id" wget -qO- --header "Cookie: $cookie_name=$cookie_value" --post-data '' \
    http://127.0.0.1:8080/api/v1/learn/lessons/30000000-0000-0000-0000-000000000001/playback \
    >"$playback_json"
  manifest_url="$(jq --exit-status --raw-output '.manifest_url | select(length > 0)' "$playback_json")" ||
    die "protected playback issuance omitted the manifest URL"
  case "$manifest_url" in
    /api/v1/media/playback-manifests/*/index.m3u8) ;;
    *) die "protected playback did not issue the same-origin manifest route" ;;
  esac
  docker exec "$api_id" wget -S -O- --header "Cookie: $cookie_name=$cookie_value" \
    "http://127.0.0.1:8080$manifest_url" >"$manifest_file" 2>"$manifest_headers"
  grep --extended-regexp --ignore-case --quiet 'Content-Type: application/vnd\.apple\.mpegurl' \
    "$manifest_headers" || die "playback manifest used the wrong content type"
  grep --extended-regexp --ignore-case --quiet 'Cache-Control: no-store' "$manifest_headers" ||
    die "playback manifest was cacheable"
  grep --extended-regexp --ignore-case --quiet 'Referrer-Policy: no-referrer' "$manifest_headers" ||
    die "playback manifest omitted referrer suppression"
  grep --fixed-strings --quiet '#EXTM3U' "$manifest_file" || die "signed playlist was not valid HLS"
  segment_url="$(sed -n '/^[^#][^[:space:]]*\.ts?/ {p;q;}' "$manifest_file")"
  [ -n "$segment_url" ] || die "rewritten playlist did not name a signed media segment"
  case "$segment_url" in
    http://minio:9000/*) ;;
    *) die "media segment was proxied by the API instead of signed for private storage" ;;
  esac
  if ! docker run --rm --network "$S12_NETWORK" --entrypoint wget "$GRADEX_BACKEND_IMAGE" \
    -qO- "$segment_url" >"$segment_file" 2>/dev/null; then
    die "signed HLS playlist did not authorize its private segment"
  fi
  [ -s "$segment_file" ] || die "signed media segment contained no bytes"

  compose run --rm --no-deps media-proof-tool \
    -issue-session -email student-unentitled@example.test >"$unentitled_session_json"
  unentitled_cookie="$(jq --exit-status --raw-output '.cookie_value | select(length > 0)' \
    "$unentitled_session_json")" || die "unentitled session proof omitted its cookie value"
  if docker exec "$api_id" wget -S -O /dev/null \
    --header "Cookie: $cookie_name=$unentitled_cookie" --post-data '' \
    http://127.0.0.1:8080/api/v1/learn/lessons/30000000-0000-0000-0000-000000000001/playback \
    >"$denial_log" 2>&1; then
    die "unentitled Student received protected playback"
  fi
  grep --fixed-strings --quiet '404 Not Found' "$denial_log" ||
    die "unentitled Student did not receive the uniform protected refusal"

  note "Redis outage/recovery, durable outbox, worker idempotency, private transcode, protected playback, and unrelated-Student denial passed"
}

main "$@"
