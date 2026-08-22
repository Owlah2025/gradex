#!/usr/bin/env bash

# Proves the disposable production-like topology renders without any
# bootstrap-only configuration, that the bootstrap profile still renders when it
# is explicitly selected, that the services which need outbound internet keep
# it, and that every supported media operating mode is accepted.
#
# The regressions this guards are specific and were each observed:
#
#   1. Compose interpolates the entire model before it filters services by
#      profile, so a `${VAR:?}` expression inside a profiled service breaks
#      ordinary `config` and `up` for everyone who does not set that variable.
#      LG-018 acceptance needs a bootstrap-admin service; the ordinary stack
#      must not inherit its configuration requirements.
#
#   2. `worker` and `bootstrap-admin` were attached to the internal `app`
#      network only. Both need outbound internet — the worker to reach Resend
#      for transactional email, bootstrap-admin to reach HIBP for the
#      compromised-password check — and both failed silently without it.
#
#   3. A secret must never be rendered into a service's command arguments,
#      where it would be visible in `docker inspect` and in process listings.
#
# It renders only. It starts nothing, needs no images, and prints no value.

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_COMPOSE_FILE="$S12_ROOT/deploy/compose/compose.production-like.yml"
S12_CADDY_FILE="$S12_ROOT/deploy/compose/Caddyfile"
S12_HOSTINGER_COMPOSE_FILE="$S12_ROOT/deploy/hostinger/compose.yml"
S12_HOSTINGER_RUNTIME_EXAMPLE="$S12_ROOT/deploy/hostinger/runtime.env.example"

note() { printf 'compose-render: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

command -v docker >/dev/null 2>&1 || die "docker is required"

# Non-secret placeholders. They stand in for the generated environment state so
# the check runs on a clean machine; none is a credential and none is printed.
export PUBLIC_ORIGIN=https://gradex.localhost:18443
export DATABASE_URL='postgres://placeholder:placeholder@postgres:5432/gradex?sslmode=disable'
export RESTORE_DATABASE_URL='postgres://placeholder:placeholder@restore-postgres:5432/gradex_restore?sslmode=disable'
export MEDIA_PROOF_DATABASE_URL='postgres://placeholder:placeholder@postgres:5432/gradex_playwright_e2e_s12media01?sslmode=disable'
export POSTGRES_PASSWORD=placeholder
export RESTORE_POSTGRES_PASSWORD=placeholder
export REDIS_PASSWORD=placeholder
export REDIS_TLS_CA_CERT_FILE_HOST=/dev/null
export REDIS_TLS_SERVER_CERT_FILE_HOST=/dev/null
export REDIS_TLS_SERVER_KEY_FILE_HOST=/dev/null
export S3_ACCESS_KEY=placeholder
export S3_SECRET_KEY=placeholder
export S3_ENDPOINT=https://example.r2.cloudflarestorage.com
export S3_BUCKET=gradex-render
export MINIO_ROOT_USER=placeholder
export MINIO_ROOT_PASSWORD=placeholder
export PLAYBACK_TOKEN_SECRET=placeholder
export SALES_WHATSAPP_NUMBER=15550000000
export SESSION_CSRF_KEY=placeholder
export ANONYMOUS_COOKIE_SIGNING_KEY=placeholder
export ANONYMOUS_CSRF_KEY=placeholder
export ADMISSION_LIMITER_HMAC_KEY=placeholder
export OUTBOX_PROTECTED_PAYLOAD_KEY=placeholder
export EMAIL_API_KEY=placeholder
export GRADEX_BACKEND_IMAGE=gradex-backend:s12-local
export GRADEX_FRONTEND_IMAGE=gradex-frontend:s12-local
export GRADEX_EDGE_IMAGE=gradex-edge:s12-local
export GRADEX_PROOF_IMAGE=gradex-backend-proof:s12-local
export GRADEX_RELEASE_SHA=0123456789abcdef0123456789abcdef01234567
export STAGING_HOSTNAME=staging.gradex.test
export ACME_EMAIL=ops@gradex.test
export POSTGRES_DB=gradex
export GRADEX_E2E_ADMIN_DB_URL='postgres://placeholder:placeholder@postgres:5432/gradex_e2e?sslmode=disable'
export OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION=hostinger-render-v1
export LEGAL_OPERATOR_NAME='Gradex Courses'
export LEGAL_REGISTRATION_NUMBER=TEST-REG
export LEGAL_REGISTERED_ADDRESS='Render verification address'
export PRIVACY_EMAIL=privacy@gradex.test
export SUPPORT_EMAIL=support@gradex.test
export SECURITY_EMAIL=security@gradex.test

# The variables under test are absent for every case below except the last.
unset BOOTSTRAP_ADMIN_EMAIL BOOTSTRAP_ADMIN_DISPLAY_NAME \
  BOOTSTRAP_ADMIN_OPERATION_ID BOOTSTRAP_ADMIN_PRINCIPAL BOOTSTRAP_ADMIN_PASSWORD

render() { docker compose --file - --project-name compose-render-check "$@" <"$S12_COMPOSE_FILE"; }

# 1. The ordinary topology renders with no bootstrap configuration at all.
render config >/dev/null ||
  die "ordinary production-like render failed without BOOTSTRAP_ADMIN_* variables"
note "ordinary render succeeds with no BOOTSTRAP_ADMIN_* variable set"

# 2. That render must not contain the bootstrap service.
default_services="$(render config --services)"
if grep --quiet --line-regexp bootstrap-admin <<<"$default_services"; then
  die "bootstrap-admin appears in the default render; it must stay behind its profile"
fi
note "bootstrap-admin is absent from the default service list"

# 3. The ordinary services the founder actually starts still resolve.
for service in api worker frontend migrate; do
  grep --quiet --line-regexp "$service" <<<"$default_services" ||
    die "$service disappeared from the default render"
done
note "api, worker, frontend and migrate all still render"

# 4. Selecting the profile with no configuration still renders, so the failure
#    is the command's own named error rather than an interpolation crash.
render --profile bootstrap config >/dev/null ||
  die "bootstrap profile render failed with empty configuration"
note "bootstrap profile renders even when unconfigured"

# 5. Configured, the bootstrap service renders and keeps the passphrase out of
#    the argument list. Only the presence of the flag is asserted, never a value.
bootstrap_render="$(
  BOOTSTRAP_ADMIN_EMAIL=admin@example.test \
  BOOTSTRAP_ADMIN_OPERATION_ID=compose-render-check \
  BOOTSTRAP_ADMIN_PASSWORD=placeholder-not-a-real-passphrase \
    render --profile bootstrap config
)"
printf '%s' "$bootstrap_render" | grep --quiet -- '-confirm-production' ||
  die "bootstrap-admin lost its production acknowledgement"
if printf '%s' "$bootstrap_render" | grep --quiet -- '-password'; then
  die "bootstrap-admin grew a password flag; the passphrase must stay in the environment"
fi
if printf '%s' "$bootstrap_render" |
  sed -n '/^  bootstrap-admin:/,/^  [a-z]/p' | sed -n '/command:/,/^    [a-z]/p' |
  grep --quiet 'placeholder-not-a-real-passphrase'; then
  die "the bootstrap passphrase reached the command arguments"
fi
note "configured bootstrap render keeps the passphrase out of the argument list"

# 6. No secret placeholder may appear anywhere in any rendered command. A
#    service's `command` is visible in `docker inspect` and in host process
#    listings, so a credential there leaks to anyone who can read either.
# service_networks prints one network name per line for a rendered service.
# `docker compose config` renders the attachment as a mapping, so a line looks
# like `      edge: null`; only the key is the network name.
service_networks() {
  printf '%s\n' "$1" | awk -v service="  $2:" '
    $0 == service { inservice = 1; next }
    inservice && /^  [^ ]/ { exit }
    inservice && /^    networks:$/ { innetworks = 1; next }
    inservice && innetworks && /^      [a-z]/ {
      sub(/^ +/, ""); sub(/:.*$/, ""); print; next
    }
    inservice && innetworks { innetworks = 0 }
  '
}

assert_command_has_no_secret() {
  local rendered="$1" label="$2"
  local secret
  for secret in placeholder placeholder-not-a-real-passphrase re_production_like_noncredential; do
    if printf '%s' "$rendered" | sed -n '/^    command:/,/^    [a-z]/p' |
      grep --quiet -- "$secret"; then
      die "$label rendered the secret placeholder %s into a command argument"
    fi
  done
}
assert_command_has_no_secret "$(render config)" "the ordinary topology"
assert_command_has_no_secret "$bootstrap_render" "the bootstrap profile"
note "no secret placeholder reaches any rendered command argument"

# 7. Outbound internet. `worker` reaches Resend to send transactional email and
#    `bootstrap-admin` reaches HIBP to screen the Administrator passphrase.
#    Both were proven necessary; on the internal `app` network alone they fail.
default_render="$(render config)"
printf '%s' "$default_render" | grep --quiet 'SALES_WHATSAPP_NUMBER: "15550000000"' ||
  die "SALES_WHATSAPP_NUMBER did not reach the API environment"
note "configured sales WhatsApp number reaches the production-like API environment"
if env -u SALES_WHATSAPP_NUMBER docker compose --file "$S12_COMPOSE_FILE" --project-name compose-render-check config >/dev/null 2>&1; then
  die "production-like compose rendered without SALES_WHATSAPP_NUMBER"
fi
note "production-like compose rejects a missing sales WhatsApp number"

# The managed-host topology has an independent environment anchor. Render it
# too: a variable present only in the disposable compose file is not a deploy
# contract. The runtime example and host preflight remain separate checks so a
# future compose edit cannot silently drop either ownership boundary.
grep --quiet --fixed-strings --line-regexp 'SALES_WHATSAPP_NUMBER=' "$S12_HOSTINGER_RUNTIME_EXAMPLE" ||
  die "hostinger runtime example is missing SALES_WHATSAPP_NUMBER"
hostinger_render="$(docker compose --file "$S12_HOSTINGER_COMPOSE_FILE" --project-name hostinger-render-check config)"
printf '%s' "$hostinger_render" | grep --quiet 'SALES_WHATSAPP_NUMBER: "15550000000"' ||
  die "SALES_WHATSAPP_NUMBER did not reach the Hostinger API environment"
note "configured sales WhatsApp number reaches the Hostinger API environment"
for service in api worker frontend; do
  networks="$(service_networks "$default_render" "$service")"
  printf '%s\n' "$networks" | grep --quiet --line-regexp edge ||
    die "$service lost its egress-capable edge network attachment"
done
note "api, worker and frontend all keep the egress-capable edge network"

bootstrap_networks="$(service_networks "$bootstrap_render" bootstrap-admin)"
printf '%s\n' "$bootstrap_networks" | grep --quiet --line-regexp edge ||
  die "bootstrap-admin lost its egress-capable edge network attachment; HIBP screening needs it"
printf '%s\n' "$bootstrap_networks" | grep --quiet --line-regexp app ||
  die "bootstrap-admin lost its internal app network attachment; PostgreSQL needs it"
note "bootstrap-admin keeps both the internal app and egress-capable edge networks"

# 8. Every media operating mode the API accepts must render, and the default
#    must stay the scanner-gated fallback. Selecting the D-088 profile is a
#    deliberate act, never something an unconfigured stack falls into.
printf '%s' "$default_render" | grep --quiet 'MEDIA_OPERATING_MODE: ADMIN_CATALOGUE' ||
  die "the default media operating mode is no longer the scanner-gated ADMIN_CATALOGUE fallback"
for mode in SCANNER ADMIN_CATALOGUE TRUSTED_INSTRUCTOR; do
	mode_render="$(MEDIA_OPERATING_MODE="$mode" render config)" ||
		die "the production-like topology does not render with MEDIA_OPERATING_MODE=$mode"
	grep --quiet "MEDIA_OPERATING_MODE: $mode" <<<"$mode_render" ||
		die "MEDIA_OPERATING_MODE=$mode did not reach the rendered environment"
done
note "SCANNER, ADMIN_CATALOGUE and TRUSTED_INSTRUCTOR all render; the default stays ADMIN_CATALOGUE"

# 9. Internal object-store traffic stays private while browser presigning uses
# the dedicated HTTPS edge origin.
printf '%s' "$default_render" | grep --quiet 'S3_ENDPOINT: http://minio:9000' ||
  die "the internal S3 endpoint no longer targets private MinIO"
printf '%s' "$default_render" | grep --quiet 'S3_PRESIGN_ENDPOINT: https://storage.gradex.localhost:18443' ||
  die "the browser presign endpoint is not the HTTPS storage edge"
minio_networks="$(service_networks "$default_render" minio)"
printf '%s\n' "$minio_networks" | grep --quiet --line-regexp app ||
  die "MinIO lost its private app network"
printf '%s\n' "$minio_networks" | grep --quiet --line-regexp edge ||
  die "MinIO is unreachable from the storage edge"
grep --quiet --fixed-string 'https://storage.gradex.localhost:18443' "$S12_CADDY_FILE" ||
  die "Caddy does not serve the browser storage hostname"
grep --quiet --fixed-string 'reverse_proxy minio:9000' "$S12_CADDY_FILE" ||
  die "Caddy does not proxy the browser storage hostname to private MinIO"
minio_cors_origin="$(
  printf '%s\n' "$default_render" |
    sed -n '/^  minio:/,/^  [a-z]/p' |
    sed -n 's/^      MINIO_API_CORS_ALLOW_ORIGIN: //p'
)"
[[ "$minio_cors_origin" == 'https://gradex.localhost:18443' ]] ||
  die "MinIO API CORS is not restricted to the exact Gradex browser origin"
note "browser storage uses the HTTPS edge while internal S3 traffic stays private"

note "all compose render checks passed"
