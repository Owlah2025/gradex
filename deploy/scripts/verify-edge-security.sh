#!/usr/bin/env bash

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_COMPOSE_FILE="$S12_ROOT/deploy/compose/compose.production-like.yml"
S12_STATE_DIR="$S12_ROOT/deploy/.state"
S12_ENV_FILE="$S12_STATE_DIR/production-like.env"
S12_CA_FILE="$S12_STATE_DIR/caddy-root.crt"
S12_PROJECT="gradex-s12"
S12_HOST="gradex.localhost"
S12_HTTPS_PORT="18443"
S12_HTTP_PORT="18081"
S12_TEMPORARY=""

note() { printf 's12-edge-security: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

require_tools() {
  local tool
  for tool in curl docker grep jq mktemp openssl sed timeout tr; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done
  docker info >/dev/null 2>&1 || die "Docker is not reachable"
}

compose() {
  sed -n '1,999p' "$S12_COMPOSE_FILE" |
    docker compose --file - --project-name "$S12_PROJECT" "$@"
}

service_id() {
  compose ps --all --quiet "$1"
}

expect_status() {
  local got="$1" want="$2" check="$3"
  [ "$got" = "$want" ] || die "$check returned HTTP $got, expected $want"
}

require_header() {
  local headers="$1" pattern="$2" check="$3"
  grep --extended-regexp --ignore-case --quiet "$pattern" "$headers" ||
    die "$check response is missing the required header"
}

reject_header() {
  local headers="$1" pattern="$2" check="$3"
  if grep --extended-regexp --ignore-case --quiet "$pattern" "$headers"; then
    die "$check response exposed a forbidden header"
  fi
}

cleanup() {
  if [ -n "$S12_TEMPORARY" ] && [ -d "$S12_TEMPORARY" ]; then
    rm -rf -- "$S12_TEMPORARY"
  fi
}

scan_runtime_secrets() {
  local logs="$1" name value scan_status
  for name in \
    POSTGRES_PASSWORD RESTORE_POSTGRES_PASSWORD S3_ACCESS_KEY S3_SECRET_KEY \
    MINIO_ROOT_USER MINIO_ROOT_PASSWORD PLAYBACK_TOKEN_SECRET SESSION_CSRF_KEY \
    ANONYMOUS_COOKIE_SIGNING_KEY ANONYMOUS_CSRF_KEY ADMISSION_LIMITER_HMAC_KEY \
    OUTBOX_PROTECTED_PAYLOAD_KEY; do
    value="${!name:-}"
    [ -n "$value" ] || die "$name is absent from the ignored environment state"
    if grep --fixed-strings --quiet -- "$value" "$logs"; then
      die "$name was present in service logs"
    fi
    set +e
    docker run --rm --entrypoint sh --env "S12_SCAN_VALUE=$value" \
      "$GRADEX_FRONTEND_IMAGE" -ec \
      'if grep -R -F -q -- "$S12_SCAN_VALUE" .next/static; then exit 42; fi'
    scan_status=$?
    set -e
    case "$scan_status" in
      0) ;;
      42) die "$name was present in the frontend static bundle" ;;
      *) die "frontend static-bundle scan failed while checking $name" ;;
    esac
  done
}

main() {
  require_tools
  [ -f "$S12_ENV_FILE" ] || die "run deploy/scripts/environment.sh prepare first"
  set -a
  # shellcheck disable=SC1090
  . "$S12_ENV_FILE"
  set +a

  local edge_id api_id base_url temporary
  local -a curl_limits resolve_https resolve_http
  local redirect_headers bootstrap_headers bootstrap_body cookie_jar request_body request_headers logs
  local status location csrf_token request_id cors_status
  edge_id="$(service_id edge)"
  api_id="$(service_id api)"
  [ -n "$edge_id" ] || die "edge container is absent"
  [ -n "$api_id" ] || die "API container is absent"

  temporary="$(mktemp -d "$S12_STATE_DIR/edge-security.XXXXXX")"
  S12_TEMPORARY="$temporary"
  chmod 700 "$temporary"
  trap cleanup EXIT
  redirect_headers="$temporary/redirect.headers"
  bootstrap_headers="$temporary/bootstrap.headers"
  bootstrap_body="$temporary/bootstrap.json"
  cookie_jar="$temporary/cookies.txt"
  request_body="$temporary/response.json"
  request_headers="$temporary/response.headers"
  logs="$temporary/services.log"
  touch "$redirect_headers" "$bootstrap_headers" "$bootstrap_body" "$cookie_jar" \
    "$request_body" "$request_headers" "$logs"
  chmod 600 "$redirect_headers" "$bootstrap_headers" "$bootstrap_body" "$cookie_jar" \
    "$request_body" "$request_headers" "$logs"

  docker exec "$edge_id" cat /data/caddy/pki/authorities/local/root.crt >"$S12_CA_FILE"
  chmod 600 "$S12_CA_FILE"
  base_url="https://$S12_HOST:$S12_HTTPS_PORT"
  [ "$PUBLIC_ORIGIN" = "$base_url" ] || die "PUBLIC_ORIGIN does not match the edge verification origin"
  curl_limits=(--connect-timeout 5 --max-time 15)
  resolve_https=(--resolve "$S12_HOST:$S12_HTTPS_PORT:127.0.0.1")
  resolve_http=(--resolve "$S12_HOST:$S12_HTTP_PORT:127.0.0.1")

  status="$(curl --silent --output /dev/null --dump-header "$redirect_headers" \
    --write-out '%{http_code}' "${curl_limits[@]}" "${resolve_http[@]}" \
    "http://$S12_HOST:$S12_HTTP_PORT/s12-edge-check")"
  expect_status "$status" "308" "HTTP redirect"
  location="$(sed -n 's/^[Ll]ocation:[[:space:]]*//p' "$redirect_headers" | tr -d '\r')"
  [ "$location" = "$base_url/s12-edge-check" ] ||
    die "HTTP redirect target was not the canonical HTTPS origin"

  timeout 15s openssl s_client -connect "127.0.0.1:$S12_HTTPS_PORT" -servername "$S12_HOST" \
    -CAfile "$S12_CA_FILE" -verify_return_error </dev/null 2>/dev/null |
    openssl x509 -noout -checkhost "$S12_HOST" >/dev/null

  curl --fail --silent --show-error --cacert "$S12_CA_FILE" \
    "${curl_limits[@]}" "${resolve_https[@]}" \
    "$base_url/healthz" >/dev/null
  curl --fail --silent --show-error --cacert "$S12_CA_FILE" \
    "${curl_limits[@]}" "${resolve_https[@]}" \
    "$base_url/readyz" >/dev/null

  status="$(curl --silent --show-error --cacert "$S12_CA_FILE" \
    "${curl_limits[@]}" "${resolve_https[@]}" \
    --cookie-jar "$cookie_jar" --dump-header "$bootstrap_headers" \
    --output "$bootstrap_body" --write-out '%{http_code}' \
    "$base_url/api/v1/session/bootstrap")"
  expect_status "$status" "200" "anonymous session bootstrap"
  require_header "$bootstrap_headers" \
    '^set-cookie: __Host-gradex_anon=.*; Path=/;.*HttpOnly;.*Secure;.*SameSite=Strict' \
    "anonymous session bootstrap"
  reject_header "$bootstrap_headers" '^set-cookie: __Host-gradex_anon=.*;.*Domain=' \
    "anonymous session bootstrap"
  csrf_token="$(jq --exit-status --raw-output '.csrf_token | select(type == "string" and length > 0)' \
    "$bootstrap_body")" || die "bootstrap response did not contain a CSRF token"

  status="$(curl --silent --show-error --cacert "$S12_CA_FILE" \
    "${curl_limits[@]}" "${resolve_https[@]}" \
    --cookie "$cookie_jar" --header 'Content-Type: application/json' \
    --header 'Origin: https://attacker.example' --header "X-CSRF-Token: $csrf_token" \
    --data '{"email":"nobody@example.invalid","password":"invalid-password"}' \
    --output "$request_body" --write-out '%{http_code}' "$base_url/api/v1/sessions")"
  expect_status "$status" "403" "foreign-origin login"
  [ "$(jq --raw-output '.code' "$request_body")" = "CSRF_VALIDATION_FAILED" ] ||
    die "foreign-origin login did not fail at browser security"

  status="$(curl --silent --show-error --cacert "$S12_CA_FILE" \
    "${curl_limits[@]}" "${resolve_https[@]}" \
    --cookie "$cookie_jar" --header 'Content-Type: application/json' \
    --header "Origin: $base_url" --header 'X-CSRF-Token: invalid' \
    --data '{"email":"nobody@example.invalid","password":"invalid-password"}' \
    --output "$request_body" --write-out '%{http_code}' "$base_url/api/v1/sessions")"
  expect_status "$status" "403" "invalid-CSRF login"
  [ "$(jq --raw-output '.code' "$request_body")" = "CSRF_VALIDATION_FAILED" ] ||
    die "invalid-CSRF login did not fail at browser security"

  status="$(curl --silent --show-error --cacert "$S12_CA_FILE" \
    "${curl_limits[@]}" "${resolve_https[@]}" \
    --cookie "$cookie_jar" --header 'Content-Type: application/json' \
    --header "Origin: $base_url" --header "X-CSRF-Token: $csrf_token" \
    --data '{"email":"nobody@example.invalid","password":"invalid-password"}' \
    --output "$request_body" --write-out '%{http_code}' "$base_url/api/v1/sessions")"
  expect_status "$status" "401" "trusted-origin login"
  [ "$(jq --raw-output '.code' "$request_body")" = "AUTHENTICATION_FAILED" ] ||
    die "trusted-origin login did not pass browser security before authentication"

  cors_status="$(curl --silent --show-error --cacert "$S12_CA_FILE" \
    "${curl_limits[@]}" "${resolve_https[@]}" \
    --request OPTIONS --header 'Origin: https://attacker.example' \
    --header 'Access-Control-Request-Method: POST' --dump-header "$request_headers" \
    --output "$request_body" --write-out '%{http_code}' "$base_url/api/v1/sessions")"
  expect_status "$cors_status" "405" "cross-origin preflight"
  reject_header "$request_headers" '^access-control-allow-origin:' "cross-origin preflight"
  reject_header "$request_headers" '^access-control-allow-credentials:' "cross-origin preflight"

  status="$(curl --silent --show-error --cacert "$S12_CA_FILE" \
    "${curl_limits[@]}" "${resolve_https[@]}" \
    --header 'X-Request-ID: attacker-controlled' --dump-header "$request_headers" \
    --output /dev/null --write-out '%{http_code}' "$base_url/healthz")"
  expect_status "$status" "200" "forwarded request"
  request_id="$(sed -n 's/^[Xx]-[Rr]equest-[Ii][Dd]:[[:space:]]*//p' "$request_headers" | tr -d '\r')"
  [ -n "$request_id" ] || die "TLS edge response did not contain a request ID"
  [ "$request_id" != "attacker-controlled" ] || die "client request ID replaced the trusted request ID"

  compose logs --no-color api worker frontend edge >"$logs"
  if grep --fixed-strings --quiet 'fake_auth=true' "$logs"; then
    die "production-like service logs report fake authentication enabled"
  fi
  grep --fixed-strings --quiet 'fake_auth=false' "$logs" ||
    die "production-like API startup did not report fake authentication disabled"
  scan_runtime_secrets "$logs"

  note "TLS, redirect, cookies, origin/CSRF, same-origin CORS denial, request IDs, and secret scans passed"
}

main "$@"
