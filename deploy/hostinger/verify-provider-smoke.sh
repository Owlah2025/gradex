#!/usr/bin/env bash

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_DB_NAME="gradex_playwright_e2e_s12provider01"

note() { printf 's12-provider-smoke: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }
require_value() { [ -n "${!1:-}" ] || die "$1 is required in the protected runner environment"; }

for tool in curl go jq mktemp psql sed; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
done
for name in GRADEX_PROVIDER_ORIGIN GRADEX_E2E_ADMIN_DB_URL GRADEX_E2E_TARGET_DB_URL \
  GRADEX_E2E_APPLICATION_DB_URL GRADEX_PROVIDER_DB_HOST GRADEX_PROVIDER_DB_PORT \
  GRADEX_PROVIDER_DB_USER GRADEX_PROVIDER_DB_PASSWORD; do
  require_value "$name"
done
[[ "$GRADEX_PROVIDER_ORIGIN" =~ ^https://[A-Za-z0-9.-]+$ ]] ||
  die "GRADEX_PROVIDER_ORIGIN must be a credential-free HTTPS origin"
[ "$GRADEX_PROVIDER_DB_HOST" = 127.0.0.1 ] && [ "$GRADEX_PROVIDER_DB_PORT" = 15432 ] ||
  die "provider database proof must use the loopback SSH tunnel on port 15432"
case "$GRADEX_E2E_ADMIN_DB_URL" in
  *"@127.0.0.1:15432/postgres?"*) ;;
  *) die "provider admin database URL must use the loopback SSH tunnel and postgres database" ;;
esac
for name in GRADEX_E2E_TARGET_DB_URL GRADEX_E2E_APPLICATION_DB_URL; do
  case "${!name}" in
    *"@127.0.0.1:15432/$S12_DB_NAME?"*) ;;
    *) die "$name must use the loopback SSH tunnel and target only $S12_DB_NAME" ;;
  esac
done

temporary="$(mktemp -d)"
chmod 700 "$temporary"
cleanup() { rm -rf -- "$temporary"; }
trap cleanup EXIT

seed_binary="$temporary/gradex-s5-e2e-seed"
run_state="$temporary/gradex-s5-e2e-run-state.json"
playback_json="$temporary/playback.json"
manifest_file="$temporary/playlist.m3u8"
segment_file="$temporary/segment.ts"
cookie_header="$temporary/cookie.header"
segment_config="$temporary/segment.curl"
playwright="$S12_ROOT/frontend/node_modules/.bin/playwright"
[ -x "$playwright" ] || die "run npm ci in frontend before the provider smoke"

curl --fail --silent --show-error --max-time 15 "$GRADEX_PROVIDER_ORIGIN/readyz" |
  jq --exit-status '.status == "ok" and .checks.postgres == "ok" and .checks.redis == "ok" and .checks.schema == "ok"' >/dev/null

(cd "$S12_ROOT/backend" && go test -c -o "$seed_binary" ./cmd/e2e-seed)
jq --null-input --arg dbName "$S12_DB_NAME" \
  '{runId:"s12provider",pid:0,port:0,apiExecPath:"external",apiListenAddr:"external",frontendPort:0,processStartTime:"external",dbName:$dbName,lockOwner:"external",startedAt:"2026-08-08T00:00:00Z"}' \
  >"$run_state"
chmod 600 "$run_state"

(
  export GRADEX_E2E_TMP_DIR="$temporary"
  export GRADEX_E2E_EXTERNAL_ORIGIN="$GRADEX_PROVIDER_ORIGIN"
  export PLAYWRIGHT_HTML_OUTPUT_DIR="$temporary/playwright-report"
  cd "$S12_ROOT/frontend"
  "$playwright" test \
    e2e/s5-infrastructure-smoke.spec.ts e2e/s6-course-access-grant-launch.spec.ts \
    --grep 'authenticates real Student via Go API session|Complete 30-Step End-to-End Course Access Grant' \
    --workers=1
)

state_assertion="$(PGPASSWORD="$GRADEX_PROVIDER_DB_PASSWORD" psql --no-psqlrc \
  --host "$GRADEX_PROVIDER_DB_HOST" --port "$GRADEX_PROVIDER_DB_PORT" \
  --username "$GRADEX_PROVIDER_DB_USER" --dbname "$S12_DB_NAME" --tuples-only --no-align --command "
    SELECT
      (SELECT count(*) FROM course_access_invitations
        WHERE normalized_email = 'student-unentitled@example.test' AND state = 'APPROVED') || '|' ||
      (SELECT count(*) FROM entitlements
        WHERE student_account_id = 'a0000000-0000-0000-0000-000000000099'::uuid
          AND course_id = 'c0000000-0000-0000-0000-000000000001'::uuid
          AND state = 'ACTIVE' AND source_invitation_id IS NOT NULL) || '|' ||
      (SELECT count(*) FROM enrollments
        WHERE student_account_id = 'a0000000-0000-0000-0000-000000000099'::uuid
          AND course_id = 'c0000000-0000-0000-0000-000000000001'::uuid) || '|' ||
      (SELECT count(*) FROM progress p JOIN enrollments e ON e.id = p.enrollment_id
        WHERE e.student_account_id = 'a0000000-0000-0000-0000-000000000099'::uuid
          AND p.course_lesson_identity_id = '30000000-0000-0000-0000-000000000001'::uuid
          AND p.max_position_seconds >= 15);")"
[ "$state_assertion" = "1|1|1|1" ] || die "provider journey database cardinality/provenance assertion failed"

export DATABASE_URL="$GRADEX_E2E_APPLICATION_DB_URL"
session_json="$("$seed_binary" -issue-session -email student-unentitled@example.test)"
cookie_name="$(printf '%s' "$session_json" | jq --exit-status --raw-output '.cookie_name | select(length > 0)')"
cookie_value="$(printf '%s' "$session_json" | jq --exit-status --raw-output '.cookie_value | select(length > 0)')"
printf 'Cookie: %s=%s\n' "$cookie_name" "$cookie_value" >"$cookie_header"
chmod 600 "$cookie_header"
curl --fail --silent --show-error --max-time 15 --header "@$cookie_header" --request POST \
  "$GRADEX_PROVIDER_ORIGIN/api/v1/learn/lessons/30000000-0000-0000-0000-000000000001/playback" \
  >"$playback_json"
manifest_url="$(jq --exit-status --raw-output '.manifest_url | select(length > 0)' "$playback_json")" ||
  die "provider playback issuance omitted the protected manifest"
curl --fail --silent --show-error --max-time 15 --header "@$cookie_header" \
  "$GRADEX_PROVIDER_ORIGIN$manifest_url" >"$manifest_file"
variant_url="$(sed -n '/^[^#][^[:space:]]*\/renditions\/[^/]*\/index\.m3u8$/ {p;q;}' "$manifest_file")"
[ -n "$variant_url" ] || die "provider protected master omitted a same-origin rendition"
curl --fail --silent --show-error --max-time 15 --header "@$cookie_header" \
  "$GRADEX_PROVIDER_ORIGIN$variant_url" >"$manifest_file"
segment_url="$(sed -n '/^[^#][^[:space:]]*\.ts?/ {p;q;}' "$manifest_file")"
[ -n "$segment_url" ] || die "provider protected rendition omitted a signed segment"
printf 'url = "%s"\nfail\nsilent\nshow-error\nmax-time = 30\n' "$segment_url" >"$segment_config"
chmod 600 "$segment_config"
curl --config "$segment_config" >"$segment_file"
[ -s "$segment_file" ] || die "provider protected R2 segment was empty"

note "public HTTPS 30-step S6/S5 journey, provenance, progress, denial, and protected R2 retrieval passed"
