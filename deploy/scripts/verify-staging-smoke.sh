#!/usr/bin/env bash

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_COMPOSE_FILE="$S12_ROOT/deploy/compose/compose.production-like.yml"
S12_STATE_DIR="$S12_ROOT/deploy/.state"
S12_ENV_FILE="$S12_STATE_DIR/production-like.env"
S12_CA_FILE="$S12_STATE_DIR/caddy-root.crt"
S12_PROJECT="gradex-s12"
S12_SMOKE_DB="gradex_playwright_e2e_s12smoke01"
S12_TUNNEL_NAME="gradex-s12-smoke-db-tunnel"
S12_TEMPORARY=""
S12_TUNNEL_STARTED=0
S12_SMOKE_MODE="${GRADEX_STAGING_SMOKE_MODE:-s12}"

note() { printf 's12-staging-smoke: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

compose() {
  sed -n '1,999p' "$S12_COMPOSE_FILE" |
    docker compose --file - --project-name "$S12_PROJECT" "$@"
}

service_id() { compose ps --all --quiet "$1"; }

wait_for_status() {
  local service="$1" wanted="$2" attempts=0 container status
  container="$(service_id "$service")"
  [ -n "$container" ] || die "$service container is absent"
  while [ "$attempts" -lt 90 ]; do
    status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container")"
    [ "$status" = "$wanted" ] && return
    case "$status" in exited|dead|unhealthy) die "$service reached $status" ;; esac
    attempts=$((attempts + 1))
    sleep 2
  done
  die "$service did not reach $wanted"
}

put_object() {
  local object_url="$1" key="$2"
  docker run --rm --interactive --network "${S12_PROJECT}_app" \
    --env "MC_HOST_local=$object_url" minio/mc:RELEASE.2025-08-13T08-35-41Z \
    pipe "local/gradex-private-media/$key" >/dev/null
}

cleanup() {
  if [ "$S12_TUNNEL_STARTED" = "1" ]; then
    docker rm --force "$S12_TUNNEL_NAME" >/dev/null 2>&1 || true
  fi
  if [ -n "$S12_TEMPORARY" ] && [ -d "$S12_TEMPORARY" ]; then
    rm -rf -- "$S12_TEMPORARY"
  fi
}

main() {
  local tool
  for tool in curl docker git jq mktemp openssl sed tar; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done
  case "$S12_SMOKE_MODE" in
    s12|s11) ;;
    *) die "GRADEX_STAGING_SMOKE_MODE must be s12 or s11" ;;
  esac
  [ -f "$S12_ENV_FILE" ] || die "run environment.sh up first"
  [ -s "$S12_CA_FILE" ] || die "run verify-edge-security.sh first"
  set -a
  # shellcheck disable=SC1090
  . "$S12_ENV_FILE"
  set +a

  local postgres_id redis_id redis_ip tunnel_port=15432 admin_url target_url application_url internal_target_url
  postgres_id="$(service_id postgres)"
  redis_id="$(service_id redis)"
  [ -n "$postgres_id" ] && [ -n "$redis_id" ] || die "PostgreSQL or Redis is absent"
  redis_ip="$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$redis_id")"
  [[ "$redis_ip" =~ ^[0-9.]+$ ]] || die "could not resolve the disposable Redis address"
  docker rm --force "$S12_TUNNEL_NAME" >/dev/null 2>&1 || true
  docker run --rm --detach --name "$S12_TUNNEL_NAME" --network "${S12_PROJECT}_edge" \
    --publish "127.0.0.1:${tunnel_port}:5432" \
    alpine/socat:1.8.0.3@sha256:beb4a68d9e4fe6b0f21ea774a0fde6c31f580dde6368939ed70100c5385b015e \
    tcp-listen:5432,fork,reuseaddr tcp-connect:postgres:5432 >/dev/null
  S12_TUNNEL_STARTED=1
  docker network connect "${S12_PROJECT}_app" "$S12_TUNNEL_NAME"
  sleep 0.5
  admin_url="postgres://gradex:${POSTGRES_PASSWORD}@127.0.0.1:${tunnel_port}/postgres?sslmode=disable"
  target_url="postgres://gradex:${POSTGRES_PASSWORD}@127.0.0.1:${tunnel_port}/${S12_SMOKE_DB}?sslmode=disable"
  application_url="postgres://gradex:${POSTGRES_PASSWORD}@127.0.0.1:${tunnel_port}/gradex?sslmode=disable"
  internal_target_url="postgres://gradex:${POSTGRES_PASSWORD}@postgres:5432/${S12_SMOKE_DB}?sslmode=disable"

  S12_TEMPORARY="$(mktemp -d "$S12_STATE_DIR/staging-smoke.XXXXXX")"
  chmod 700 "$S12_TEMPORARY"
  trap cleanup EXIT
  local seed_binary="$S12_TEMPORARY/gradex-s5-e2e-seed"
  local run_state="$S12_TEMPORARY/gradex-s5-e2e-run-state.json"
  local playback_json="$S12_TEMPORARY/playback.json"
  local manifest_file="$S12_TEMPORARY/playlist.m3u8"
  local segment_file="$S12_TEMPORARY/segment.ts"
  chmod 700 "$S12_TEMPORARY"

  (cd "$S12_ROOT/backend" && go test -c -o "$seed_binary" ./cmd/e2e-seed)

  export APP_ENV=production SERVICE_ROLE=api LOG_LEVEL=info
  export PUBLIC_ORIGIN=https://gradex.localhost:18443
  export CORS_ALLOWED_ORIGINS="$PUBLIC_ORIGIN" CORS_ALLOW_CREDENTIALS=true TRUSTED_PROXIES=172.16.0.0/12
  export DATABASE_URL="$application_url" REDIS_ADDR="${redis_ip}:6379"
  export REDIS_TLS_ENABLED=true REDIS_TLS_SERVER_NAME=redis REDIS_TLS_CA_CERT_FILE="$REDIS_TLS_CA_CERT_FILE_HOST"
  export S3_ENDPOINT=http://minio:9000 S3_BUCKET=gradex-private-media S3_REGION=us-east-1 S3_USE_PATH_STYLE=true
  export OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION=production-like-v1 MEDIA_OPERATING_MODE=ADMIN_CATALOGUE
  if [ "$S12_SMOKE_MODE" = "s11" ]; then
    export GRADEX_E2E_REGISTRATION_PASSWORD="KuwaitStudy!2026"
    export STUDENT_REGISTRATION_ENABLED=true
    export REGISTRATION_POLICY_SET_ID=gradex-legal-2026-08-09-v1
    export REGISTRATION_POLICY_APPROVED=true
    export PASSWORD_SCREEN_MODE=adapter
    export COMPROMISED_PASSWORD_ADAPTER_APPROVED=true
    export LEGAL_IDENTITY_MODE=controlled-staging
    export LEGAL_OPERATOR_NAME="Gradex Courses"
    export LEGAL_REGISTRATION_NUMBER="STAGING-NOT-REGISTERED"
    export LEGAL_REGISTERED_ADDRESS="STAGING ONLY — LEGAL ENTITY DETAILS PENDING"
    export PRIVACY_EMAIL=ahmedhazemelmelegy11@gmail.com
    export SUPPORT_EMAIL=ahmedhazemelmelegy11@gmail.com
    export SECURITY_EMAIL=ahmedhazemelmelegy11@gmail.com
  else
    export STUDENT_REGISTRATION_ENABLED=false
    export REGISTRATION_POLICY_APPROVED=false
    export PASSWORD_SCREEN_MODE=unavailable
    export COMPROMISED_PASSWORD_ADAPTER_APPROVED=false
  fi
  export AUTH_FAKE_MODE=false
  export GRADEX_E2E_ALLOW_DATABASE_RESET=1 GRADEX_E2E_ADMIN_DB_URL="$admin_url"
  export GRADEX_E2E_TARGET_DB_NAME="$S12_SMOKE_DB" GRADEX_E2E_TARGET_DB_URL="$target_url"
  export GRADEX_E2E_APPLICATION_DB_URL="$application_url"
  (cd "$S12_ROOT/backend" && "$seed_binary" >/dev/null)

  local object_url="http://${S3_ACCESS_KEY}:${S3_SECRET_KEY}@minio:9000"
  printf '#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:1.0,\nsegment000.ts\n#EXT-X-ENDLIST\n' |
    put_object "$object_url" test/master.m3u8
  docker run --rm --entrypoint ffmpeg "$GRADEX_BACKEND_IMAGE" \
    -nostdin -v error -f lavfi -i 'testsrc=size=320x240:rate=24' -t 1 \
    -c:v libx264 -pix_fmt yuv420p -f mpegts pipe:1 |
    put_object "$object_url" test/segment000.ts

  export DATABASE_URL="$internal_target_url"
  compose up --detach --no-deps --force-recreate api worker frontend
  wait_for_status api healthy
  wait_for_status worker running
  wait_for_status frontend healthy
  "$S12_ROOT/deploy/scripts/environment.sh" verify

  jq --null-input \
    --arg dbName "$S12_SMOKE_DB" \
    '{runId:"s12staging",pid:0,port:0,apiExecPath:"external",apiListenAddr:"external",frontendPort:0,processStartTime:"external",dbName:$dbName,lockOwner:"external",startedAt:"2026-08-08T00:00:00Z"}' \
    >"$run_state"
  chmod 600 "$run_state"

  local tls_spki
  tls_spki="$(openssl s_client -connect 127.0.0.1:18443 -servername gradex.localhost </dev/null 2>/dev/null |
    openssl x509 -pubkey -noout |
    openssl pkey -pubin -outform DER 2>/dev/null |
    openssl dgst -sha256 -binary |
    openssl base64 -A)"
  [ -n "$tls_spki" ] || die "could not derive the pinned internal-CA SPKI"

  (
    export GRADEX_E2E_TMP_DIR="$S12_TEMPORARY"
    export GRADEX_E2E_EXTERNAL_ORIGIN="$PUBLIC_ORIGIN"
    export GRADEX_E2E_TLS_SPKI="$tls_spki"
    export NODE_EXTRA_CA_CERTS="$S12_CA_FILE"
    export GRADEX_E2E_ADMIN_DB_URL="$admin_url"
    export GRADEX_E2E_TARGET_DB_URL="$target_url"
    export GRADEX_E2E_APPLICATION_DB_URL="$application_url"
    cd "$S12_ROOT/frontend"
    if [ "$S12_SMOKE_MODE" = "s11" ]; then
      npx playwright test e2e/legal-policy-pages.spec.ts e2e/s11-release-acceptance.spec.ts --workers=1
    else
      npx playwright test \
        e2e/s5-infrastructure-smoke.spec.ts e2e/s6-course-access-grant-launch.spec.ts \
        --grep 'authenticates real Student via Go API session|Complete 30-Step End-to-End Course Access Grant' \
        --workers=1
    fi
  )

  local state_assertion
  local journey_student_email="student-unentitled@example.test"
  if [ "$S12_SMOKE_MODE" = "s11" ]; then
    journey_student_email="s11-release-student@example.test"
  fi
  state_assertion="$(docker exec "$postgres_id" psql --no-psqlrc --username gradex --dbname "$S12_SMOKE_DB" \
    --tuples-only --no-align --command "
      SELECT
        (SELECT count(*) FROM course_access_invitations
          WHERE normalized_email = '$journey_student_email' AND state = 'APPROVED') || '|' ||
        (SELECT count(*) FROM entitlements e JOIN accounts a ON a.id = e.student_account_id
          WHERE a.normalized_email = '$journey_student_email'
            AND e.course_id = 'c0000000-0000-0000-0000-000000000001'::uuid
            AND e.state = 'ACTIVE' AND e.grant_source = 'MANUAL_INVITATION'
            AND e.source_invitation_id IS NOT NULL) || '|' ||
        (SELECT count(*) FROM enrollments e JOIN accounts a ON a.id = e.student_account_id
          WHERE a.normalized_email = '$journey_student_email'
            AND e.course_id = 'c0000000-0000-0000-0000-000000000001'::uuid) || '|' ||
        (SELECT count(*) FROM progress p
          JOIN enrollments e ON e.id = p.enrollment_id
          JOIN accounts a ON a.id = e.student_account_id
          WHERE a.normalized_email = '$journey_student_email'
            AND p.course_lesson_identity_id = '30000000-0000-0000-0000-000000000001'::uuid
            AND p.max_position_seconds >= 15);"
  )"
  [ "$state_assertion" = "1|1|1|1" ] || die "deployed journey database assertion was $state_assertion"

  if [ "$S12_SMOKE_MODE" = "s11" ]; then
    local policy_assertion
    policy_assertion="$(docker exec "$postgres_id" psql --no-psqlrc --username gradex --dbname "$S12_SMOKE_DB" \
      --tuples-only --no-align --command "
        SELECT count(*) FROM policy_acceptances p
        JOIN accounts a ON a.id = p.account_id
        WHERE a.normalized_email = '$journey_student_email'
          AND p.policy_set_id = 'gradex-legal-2026-08-09-v1'
          AND p.policy_version = '2026-08-09-v1'
          AND p.locale = 'en';"
    )"
    [ "$policy_assertion" = "2" ] || die "deployed registration policy assertion was $policy_assertion"
  fi

  local session_json cookie_name cookie_value manifest_url segment_url
  export DATABASE_URL="$application_url"
  if [ "$S12_SMOKE_MODE" = "s11" ]; then
    session_json="$("$seed_binary" -issue-session -use-registration-password -email "$journey_student_email")"
  else
    session_json="$("$seed_binary" -issue-session -email "$journey_student_email")"
  fi
  cookie_name="$(printf '%s' "$session_json" | jq --raw-output '.cookie_name')"
  cookie_value="$(printf '%s' "$session_json" | jq --raw-output '.cookie_value')"
  curl --fail --silent --show-error --cacert "$S12_CA_FILE" \
    --header "Cookie: $cookie_name=$cookie_value" --request POST \
    "$PUBLIC_ORIGIN/api/v1/learn/lessons/30000000-0000-0000-0000-000000000001/playback" \
    >"$playback_json"
  manifest_url="$(jq --exit-status --raw-output '.manifest_url | select(length > 0)' "$playback_json")" ||
    die "deployed playback issuance omitted the protected manifest"
  curl --fail --silent --show-error --cacert "$S12_CA_FILE" \
    --header "Cookie: $cookie_name=$cookie_value" "$PUBLIC_ORIGIN$manifest_url" >"$manifest_file"
  segment_url="$(sed -n '/^[^#][^[:space:]]*\.ts?/ {p;q;}' "$manifest_file")"
  [ -n "$segment_url" ] || die "deployed protected manifest omitted a signed segment"
  docker run --rm --network "${S12_PROJECT}_app" --entrypoint wget "$GRADEX_BACKEND_IMAGE" \
    -qO- "$segment_url" >"$segment_file"
  [ -s "$segment_file" ] || die "deployed protected segment was empty"

  if [ "$S12_SMOKE_MODE" = "s11" ]; then
    note "HTTPS S11 deployed login-to-learning journey, zero-before-approval, exact provenance/cardinality, progress, unrelated denial, authorized replay, and protected media passed"
    note "S11 production-like origin=$PUBLIC_ORIGIN database=$S12_SMOKE_DB schema=15 state=$state_assertion"
  else
    note "HTTPS S6 invitation-to-learning journey, zero-before-approval, exact grant counts, progress, unrelated denial, and protected media passed"
    note "production-like origin=$PUBLIC_ORIGIN database=$S12_SMOKE_DB state=$state_assertion"
  fi
}

main "$@"
