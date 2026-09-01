#!/usr/bin/env bash
#
# Proves the managed Hostinger topology can compose a real production
# deployment, and that preflight refuses a composition the application would
# reject at startup.
#
# The regression this guards was observed on the first real production
# bring-up. `deploy/hostinger/compose.yml` hard-coded
#
#     APP_ENV: staging
#     PASSWORD_SCREEN_MODE: unavailable
#
# and never exposed COMPROMISED_PASSWORD_ADAPTER_APPROVED at all. Provider
# preflight passed, PostgreSQL and Redis started, a fresh database migrated
# cleanly to schema 28, and only then did the API refuse to build:
#
#     building production router foundations:
#     building compromised-password source:
#     compromised-password screening is not configured
#
# Screening is not a student-registration concern. Staff invitation and
# onboarding set passwords, so `validateStaffComposition` in cmd/api requires
# PasswordScreenMode == adapter even while public student registration stays
# closed. A composition the application will reject must be rejected before
# anything starts.
#
# The first repair fixed production and left staging asserting the same broken
# values, which only moved the trap: `validateStaffComposition` exempts
# development ALONE, and a managed host is never development, so staging carries
# the identical contract. Both managed environments are covered below.
#
# The invariant: if preflight says a managed composition is valid, the API must
# not then fail on a static environment precondition preflight could have
# checked. The final section enforces that forward, by reading the backend's own
# precondition list and failing if preflight does not mirror it.
#
# This renders only. It starts nothing, needs no provider, and prints no value.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COMPOSE_FILE="$ROOT/deploy/hostinger/compose.yml"
HOST_SCRIPT="$ROOT/deploy/hostinger/host.sh"
RUNTIME_EXAMPLE="$ROOT/deploy/hostinger/runtime.env.example"
API_MAIN="$ROOT/backend/cmd/api/main.go"

note() { printf 'hostinger-production-render: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

command -v docker >/dev/null 2>&1 || die "docker is required"

# Non-secret placeholders standing in for protected host state. None is a
# credential and none is printed.
export PUBLIC_ORIGIN=https://gradexcourses.test
export STAGING_HOSTNAME=gradexcourses.test
export ACME_EMAIL=ops@gradex.test
export POSTGRES_DB=gradex_production
export POSTGRES_PASSWORD=placeholder
export DATABASE_URL='postgres://placeholder:placeholder@postgres:5432/gradex_production?sslmode=disable'
export RESTORE_DATABASE_URL='postgres://placeholder:placeholder@restore-postgres:5432/gradex_restore?sslmode=disable'
export RESTORE_POSTGRES_PASSWORD=placeholder
export GRADEX_E2E_ADMIN_DB_URL='postgres://placeholder:placeholder@postgres:5432/postgres?sslmode=disable'
export REDIS_PASSWORD=placeholder
export REDIS_TLS_CA_CERT_FILE_HOST=/dev/null
export REDIS_TLS_SERVER_CERT_FILE_HOST=/dev/null
export REDIS_TLS_SERVER_KEY_FILE_HOST=/dev/null
export S3_ENDPOINT=https://example.r2.cloudflarestorage.com
export S3_BUCKET=gradex-production-media
export S3_ACCESS_KEY=placeholder
export S3_SECRET_KEY=placeholder
export PLAYBACK_TOKEN_SECRET=placeholder
export SALES_WHATSAPP_NUMBER=96500000000
export SESSION_CSRF_KEY=placeholder
export ANONYMOUS_COOKIE_SIGNING_KEY=placeholder
export ANONYMOUS_CSRF_KEY=placeholder
export IDENTITY_OTP_PEPPER=placeholder
export ADMISSION_LIMITER_HMAC_KEY=placeholder
export OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION=hostinger-render-v1
export OUTBOX_PROTECTED_PAYLOAD_KEY=placeholder
export LEGAL_OPERATOR_NAME='Gradex Courses'
export LEGAL_REGISTRATION_NUMBER=RENDER-ONLY
export LEGAL_REGISTERED_ADDRESS='Render verification address'
export PRIVACY_EMAIL=privacy@gradex.test
export SUPPORT_EMAIL=support@gradex.test
export SECURITY_EMAIL=security@gradex.test
export EMAIL_API_KEY=placeholder
export EMAIL_FROM_ADDRESS=no-reply@updates.gradex.test
export GRADEX_BACKEND_IMAGE=gradex-backend:render-check
export GRADEX_FRONTEND_IMAGE=gradex-frontend:render-check
export GRADEX_PROOF_IMAGE=gradex-backend-proof:render-check
export GRADEX_RELEASE_SHA=0123456789abcdef0123456789abcdef01234567

render() {
  docker compose --file "$COMPOSE_FILE" --project-name hostinger-mode-render-check config
}

api_environment() {
  printf '%s\n' "$1" | sed -n '/^  api:/,/^  [a-z]/p'
}

# --- 1-4. the production render composes the approved production shape ------

production_render="$(
  APP_ENV=production \
  PASSWORD_SCREEN_MODE=adapter \
  COMPROMISED_PASSWORD_ADAPTER_APPROVED=true \
  EMAIL_ENABLED=true \
  EMAIL_PROVIDER=resend \
    render
)" || die "the Hostinger topology does not render in production mode"

production_api="$(api_environment "$production_render")"

for expectation in \
  'APP_ENV: production' \
  'PASSWORD_SCREEN_MODE: adapter' \
  'COMPROMISED_PASSWORD_ADAPTER_APPROVED: "true"' \
  'STUDENT_REGISTRATION_ENABLED: "false"' \
  'AUTH_FAKE_MODE: "false"' \
  'EMAIL_ENABLED: "true"' \
  'EMAIL_PROVIDER: resend'; do
  printf '%s' "$production_api" | grep --quiet --fixed-strings "$expectation" ||
    die "the production API environment is missing: $expectation"
done
note "production render: APP_ENV=production, adapter screening approved, registration defaults closed, no fake authentication, Resend email"

printf '%s' "$production_api" | grep --quiet 'S3_ENDPOINT: https://example.r2.cloudflarestorage.com' ||
  die "the production render did not take its S3 endpoint from protected configuration"
printf '%s' "$production_api" | grep --quiet 'S3_USE_PATH_STYLE: "false"' ||
  die "the production render lost virtual-host-style addressing for R2"
if printf '%s' "$production_render" | grep --quiet --ignore-case 'minio'; then
  die "MinIO entered the live production topology"
fi
note "production render uses external private R2 and introduces no MinIO"

# --- 5. only the edge publishes host ports ----------------------------------

published="$(
  printf '%s' "$production_render" |
    python3 -c '
import sys, yaml
model = yaml.safe_load(sys.stdin)
for name, service in sorted(model.get("services", {}).items()):
    for entry in service.get("ports", []) or []:
        if isinstance(entry, dict):
            target = entry.get("published")
            protocol = entry.get("protocol", "tcp")
        else:
            target, _, protocol = str(entry).partition("/")
            target = target.split(":")[-2] if ":" in target else target
            protocol = protocol or "tcp"
        print(f"{name} {target}/{protocol}")
'
)" || die "could not read published ports from the production render"

while read -r service port; do
  [ -n "$service" ] || continue
  [ "$service" = edge ] || die "$service publishes $port; only the edge may publish host ports"
  case "$port" in
    80/tcp | 443/tcp | 443/udp) ;;
    *) die "the edge publishes unexpected host port $port" ;;
  esac
done <<<"$published"
[ -n "$published" ] || die "the production render publishes no host port at all"
note "only the edge publishes host ports, and only 80/tcp, 443/tcp and 443/udp"

# --- 6. staging still renders its intended staging shape --------------------

# A managed host is never development, and validateStaffComposition exempts
# development alone, so staging carries the same composition contract as
# production. Only the LG-021 approval flag differs.
staging_render="$(
  APP_ENV=staging \
  PASSWORD_SCREEN_MODE=adapter \
  COMPROMISED_PASSWORD_ADAPTER_APPROVED=false \
  EMAIL_ENABLED=true \
  EMAIL_PROVIDER=resend \
    render
)" || die "the Hostinger topology no longer renders in staging mode"

staging_api="$(api_environment "$staging_render")"
for expectation in \
  'APP_ENV: staging' \
  'PASSWORD_SCREEN_MODE: adapter' \
  'COMPROMISED_PASSWORD_ADAPTER_APPROVED: "false"' \
  'EMAIL_ENABLED: "true"' \
  'EMAIL_PROVIDER: resend' \
  'STUDENT_REGISTRATION_ENABLED: "false"' \
  'AUTH_FAKE_MODE: "false"'; do
  printf '%s' "$staging_api" | grep --quiet --fixed-strings "$expectation" ||
    die "the staging API environment is missing: $expectation"
done
if printf '%s' "$staging_render" | grep --quiet --ignore-case 'minio'; then
  die "MinIO entered the managed staging topology"
fi
note "staging render is application-startup-compatible: adapter screening, Resend email, registration defaults closed"

# --- the environment must be declared, never defaulted ----------------------

if (
  unset APP_ENV
  PASSWORD_SCREEN_MODE=adapter COMPROMISED_PASSWORD_ADAPTER_APPROVED=true \
    EMAIL_ENABLED=true EMAIL_PROVIDER=resend render >/dev/null 2>&1
); then
  die "the topology rendered without APP_ENV; the environment must be declared"
fi
if (
  unset PASSWORD_SCREEN_MODE
  APP_ENV=production COMPROMISED_PASSWORD_ADAPTER_APPROVED=true \
    EMAIL_ENABLED=true EMAIL_PROVIDER=resend render >/dev/null 2>&1
); then
  die "the topology rendered without PASSWORD_SCREEN_MODE"
fi
if (
  unset COMPROMISED_PASSWORD_ADAPTER_APPROVED
  APP_ENV=production PASSWORD_SCREEN_MODE=adapter \
    EMAIL_ENABLED=true EMAIL_PROVIDER=resend render >/dev/null 2>&1
); then
  die "the topology rendered without COMPROMISED_PASSWORD_ADAPTER_APPROVED"
fi
note "APP_ENV, PASSWORD_SCREEN_MODE and adapter approval are all required, never defaulted"

# --- 7-8. preflight refuses an invalid production composition ---------------

# The preflight predicate is extracted rather than reimplemented, so this test
# fails if the real rule changes.
extract() {
  awk -v name="$1" '
    $0 ~ "^" name "\\(\\)" { capture = 1 }
    capture { print }
    capture && /^}/ { exit }
  ' "$HOST_SCRIPT"
}
eval "$(extract validate_application_composition)"
# The extracted rule calls `require_value`, which lives elsewhere in host.sh.
# Mirror it exactly. `note` and `die` are deliberately NOT stubbed: a stubbed
# `note` would silence `die` and turn a real failure into a bare exit code.
require_value() {
  local name="$1"
  [ -n "${!name:-}" ] || die "$name is required in the protected runtime environment"
}

compose_case() {
  APP_ENV="$1" PASSWORD_SCREEN_MODE="$2" COMPROMISED_PASSWORD_ADAPTER_APPROVED="$3" \
    EMAIL_ENABLED="${4:-true}" EMAIL_PROVIDER="${5:-resend}" \
    EMAIL_API_KEY=placeholder EMAIL_FROM_ADDRESS=no-reply@updates.gradex.test \
    AUTH_FAKE_MODE=false STUDENT_REGISTRATION_ENABLED=false \
    validate_application_composition
}

# Positive: both managed environments. Staging differs only in the approval
# flag, exactly as config.go gates it on environment.IsProduction().
compose_case production adapter true ||
  die "the approved production composition was rejected"
compose_case staging adapter false ||
  die "the approved staging composition was rejected"

# `die` exits, so every negative runs in a subshell.

# The environment must be a managed one.
(compose_case development adapter false >/dev/null 2>&1) &&
  die "a managed host accepted development"
(compose_case acceptance adapter false >/dev/null 2>&1) &&
  die "a managed host accepted an unknown environment"

# Screening: the contract validateStaffComposition enforces for every
# non-development environment, so both managed environments must satisfy it.
for environment in staging production; do
  approved=true
  [ "$environment" = staging ] && approved=false
  (compose_case "$environment" unavailable "$approved" >/dev/null 2>&1) &&
    die "$environment accepted unavailable password screening"
  (compose_case "$environment" deterministic "$approved" >/dev/null 2>&1) &&
    die "$environment accepted deterministic password screening"
  (compose_case "$environment" adapter "$approved" false resend >/dev/null 2>&1) &&
    die "$environment accepted disabled transactional email"
  (compose_case "$environment" adapter "$approved" true smtp >/dev/null 2>&1) &&
    die "$environment accepted a transactional email provider other than Resend"
  (compose_case "$environment" adapter "$approved" true fake >/dev/null 2>&1) &&
    die "$environment accepted the fake transactional email provider"
done

# Approval is production-only, and never a substitute for the adapter.
(compose_case production adapter false >/dev/null 2>&1) &&
  die "production accepted the adapter without COMPROMISED_PASSWORD_ADAPTER_APPROVED=true"
compose_case staging adapter true ||
  die "staging rejected an approval flag the backend simply ignores outside production"
(
  APP_ENV=production PASSWORD_SCREEN_MODE=adapter COMPROMISED_PASSWORD_ADAPTER_APPROVED=true \
    EMAIL_ENABLED=true EMAIL_PROVIDER=resend EMAIL_API_KEY= \
    EMAIL_FROM_ADDRESS=no-reply@updates.gradex.test AUTH_FAKE_MODE=false \
    STUDENT_REGISTRATION_ENABLED=false validate_application_composition >/dev/null 2>&1
) && die "production accepted a missing transactional email key"
(
  APP_ENV=production PASSWORD_SCREEN_MODE=adapter COMPROMISED_PASSWORD_ADAPTER_APPROVED=true \
    EMAIL_ENABLED=true EMAIL_PROVIDER=resend EMAIL_API_KEY=placeholder \
    EMAIL_FROM_ADDRESS=no-reply@updates.gradex.test AUTH_FAKE_MODE=true \
    STUDENT_REGISTRATION_ENABLED=false validate_application_composition >/dev/null 2>&1
) && die "a managed host accepted fake authentication"
(
  APP_ENV=production PASSWORD_SCREEN_MODE=adapter COMPROMISED_PASSWORD_ADAPTER_APPROVED=true \
    EMAIL_ENABLED=true EMAIL_PROVIDER=resend EMAIL_API_KEY=placeholder \
    EMAIL_FROM_ADDRESS=no-reply@updates.gradex.test AUTH_FAKE_MODE=false \
    STUDENT_REGISTRATION_ENABLED=true \
    REGISTRATION_POLICY_SET_ID=gradex-legal-2026-08-09-v1 \
    REGISTRATION_POLICY_APPROVED=true \
    validate_application_composition
) || die "production rejected approved student registration"

(
  APP_ENV=production PASSWORD_SCREEN_MODE=adapter COMPROMISED_PASSWORD_ADAPTER_APPROVED=true \
    EMAIL_ENABLED=true EMAIL_PROVIDER=resend EMAIL_API_KEY=placeholder \
    EMAIL_FROM_ADDRESS=no-reply@updates.gradex.test AUTH_FAKE_MODE=false \
    STUDENT_REGISTRATION_ENABLED=true \
    REGISTRATION_POLICY_SET_ID= \
    REGISTRATION_POLICY_APPROVED=true \
    validate_application_composition >/dev/null 2>&1
) && die "production registration accepted a missing policy-set id"

(
  APP_ENV=production PASSWORD_SCREEN_MODE=adapter COMPROMISED_PASSWORD_ADAPTER_APPROVED=true \
    EMAIL_ENABLED=true EMAIL_PROVIDER=resend EMAIL_API_KEY=placeholder \
    EMAIL_FROM_ADDRESS=no-reply@updates.gradex.test AUTH_FAKE_MODE=false \
    STUDENT_REGISTRATION_ENABLED=true \
    REGISTRATION_POLICY_SET_ID=gradex-legal-2026-08-09-v1 \
    REGISTRATION_POLICY_APPROVED=false \
    validate_application_composition >/dev/null 2>&1
) && die "production registration accepted an unapproved policy set"

printf 'hostinger-production-render: production and staging renders, port safety, R2 isolation, and fail-closed preflight verified\n' >&2

# --- the preflight mirrors the backend's own precondition list ---------------

# Read validateStaffComposition from the API and require preflight to cover each
# precondition it enforces for non-development environments. If the backend
# grows a new one, this fails until preflight mirrors it, which is the whole
# point: preflight must not certify a composition the API will reject.
staff_rule="$(
  awk '
    /^func validateStaffComposition\(/ { capture = 1 }
    capture { print }
    capture && /^}/ { exit }
  ' "$API_MAIN"
)"
[ -n "$staff_rule" ] || die "validateStaffComposition could not be read from $API_MAIN"

# Development, and only development, is exempt. If that ever widens to staging,
# the shared contract below stops being true and must be re-derived.
printf '%s' "$staff_rule" |
  grep --quiet --fixed-strings 'cfg.Environment() == config.EnvDevelopment' ||
  die "validateStaffComposition no longer exempts development explicitly; re-derive the managed-host contract"
if printf '%s' "$staff_rule" | grep --quiet --fixed-strings 'EnvStaging'; then
  die "validateStaffComposition now treats staging specially; re-derive the managed-host contract"
fi

declare -A mirrored=(
  ["cfg.Sessions().Enabled()"]="require_value SESSION_CSRF_KEY"
  ["cfg.AuthFakeMode()"]="AUTH_FAKE_MODE"
  ["config.PasswordScreenAdapter"]="PASSWORD_SCREEN_MODE"
  ["email.Enabled()"]="EMAIL_ENABLED"
  ["config.EmailProviderResend"]="EMAIL_PROVIDER"
)
preflight="$(
  awk '
    /^validate_application_composition\(\)/ { capture = 1 }
    capture { print }
    capture && /^}/ { exit }
  ' "$HOST_SCRIPT"
)"
for predicate in "${!mirrored[@]}"; do
  printf '%s' "$staff_rule" | grep --quiet --fixed-strings "$predicate" ||
    die "validateStaffComposition no longer checks $predicate; the preflight mirror is stale"
  printf '%s' "$preflight" | grep --quiet --fixed-strings "${mirrored[$predicate]}" ||
    die "preflight does not mirror the backend precondition $predicate"
done

# PostgreSQL and Redis are the two preconditions preflight cannot check
# statically; the Compose topology supplies both as hard dependencies.
backend_preconditions="$(
  printf '%s' "$staff_rule" | grep --count 'staff composition precondition failed'
)"
[ "$backend_preconditions" = 8 ] ||
  die "validateStaffComposition now has $backend_preconditions preconditions, not the 8 this preflight was derived from; re-audit deploy/hostinger/host.sh"
note "preflight mirrors every statically checkable staff-composition precondition the API enforces"

# --- 9-10. the release and operator contracts are unchanged -----------------

for key in APP_ENV PASSWORD_SCREEN_MODE COMPROMISED_PASSWORD_ADAPTER_APPROVED; do
  grep --quiet --extended-regexp "^$key=" "$RUNTIME_EXAMPLE" ||
    die "the runtime example does not carry $key"
done

grep --quiet --fixed-strings 'die "backend image revision label does not match GRADEX_RELEASE_SHA"' "$HOST_SCRIPT" ||
  die "the backend image revision check was lost"
grep --quiet --fixed-strings 'die "frontend image revision label does not match GRADEX_RELEASE_SHA"' "$HOST_SCRIPT" ||
  die "the frontend image revision check was lost"
grep --quiet --fixed-strings 'die "proof image revision label does not match GRADEX_RELEASE_SHA"' "$HOST_SCRIPT" ||
  die "the proof image revision check was lost"
grep --quiet --fixed-strings 'die "provider releases may not use latest image tags"' "$HOST_SCRIPT" ||
  die "the pinned-tag check was lost"
grep --quiet --fixed-strings 'backup_validate_configuration || die "encrypted offsite backup configuration is invalid"' "$HOST_SCRIPT" ||
  die "the backup configuration check was lost from preflight"

printf 'hostinger-production-render: runtime contract, pinned revision-labelled images, and backup configuration checks intact\n' >&2
