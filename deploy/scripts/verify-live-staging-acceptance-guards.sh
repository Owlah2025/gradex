#!/usr/bin/env bash
#
# Proves deploy/scripts/verify-live-staging-acceptance.sh is safe to point at a
# real, populated deployment.
#
# A smoke that observes production-shaped data is only worth having if it cannot
# damage it. These checks pin the four properties that make it safe — it refuses
# production without deliberate authorization, it holds no destructive path, it
# owns no Compose project, and it removes what it creates — plus the two it
# exists to detect: a broken health gate and an auth boundary that has started
# answering anonymous callers.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TARGET="$ROOT/deploy/scripts/verify-live-staging-acceptance.sh"

die() {
  printf 'live-staging-acceptance-guards: %s\n' "$*" >&2
  exit 1
}

TEMPORARY="$(mktemp -d)"
trap 'rm -rf -- "$TEMPORARY"' EXIT

# The smoke guards its own entry point, so sourcing it defines the real
# functions without running any check.
# shellcheck disable=SC1090
. "$TARGET"

LIVE_TEMPORARY="$TEMPORARY"
LIVE_ORIGIN="https://staging.example.test"
LIVE_RUN_ID="guardtest"

# --- production is refused unless deliberately authorized -------------------

for production in "${LIVE_PRODUCTION_HOSTNAMES[@]}"; do
  if (assert_production_authorized "$production" "" >/dev/null 2>&1); then
    die "$production ran without the production authorization flag"
  fi
  if (assert_production_authorized "$production" "yes" >/dev/null 2>&1); then
    die "$production ran on an approximate authorization value"
  fi
  assert_production_authorized "$production" "$LIVE_PRODUCTION_AUTHORIZATION" ||
    die "$production was refused despite an exact authorization"
done

assert_production_authorized staging.gradex.network "" ||
  die "a staging hostname required production authorization"

# --- the smoke holds no destructive path ------------------------------------

# The scans below read the executable body only. The file's header deliberately
# names the destructive mechanisms it replaces, and a comment that explains why
# a path is unsafe is not a call to it.
BODY="$TEMPORARY/body.sh"
grep -v '^[[:space:]]*#' "$TARGET" >"$BODY"

# Text that must never appear in a script pointed at a live database. Each entry
# is a mechanism that resets, reseeds, or repoints real state.
for forbidden in \
  'seed-smoke' \
  'GRADEX_E2E_ALLOW_DATABASE_RESET' \
  'gradex-e2e-seed' \
  'DROP TABLE' \
  'TRUNCATE' \
  'DELETE FROM' \
  'pg_restore' \
  'restore-postgres' \
  '--force-recreate' \
  'docker compose up' \
  'docker compose down' \
  'compose up --detach' \
  'compose down'; do
  if grep --fixed-strings --quiet -e "$forbidden" "$BODY"; then
    die "the live smoke contains the destructive or environment-owning text: $forbidden"
  fi
done

# --- it does not depend on the isolated s12 topology ------------------------

for foreign in 'gradex-s12' 'compose.production-like.yml' 'production-like.env' 'caddy-root.crt'; do
  if grep --fixed-strings --quiet -e "$foreign" "$BODY"; then
    die "the live smoke depends on the isolated topology through: $foreign"
  fi
done

# The Compose project is supplied by the operator, never assumed.
grep --fixed-strings --quiet 'GRADEX_LIVE_SMOKE_COMPOSE_PROJECT' "$TARGET" ||
  die "the live smoke does not take its Compose project from the operator"

# --- a broken health gate fails ---------------------------------------------

expect_code 200 200 "health" || die "a matching status was rejected"
if (expect_code 503 200 "health" >/dev/null 2>&1); then
  die "a non-200 health response was accepted"
fi
if (expect_code 000 200 "readiness" >/dev/null 2>&1); then
  die "an unreachable readiness endpoint was accepted"
fi
if (expect_one_of 200 "anonymous protected media" 401 403 404 >/dev/null 2>&1); then
  die "expect_one_of accepted a status outside its allowed set"
fi
expect_one_of 404 "anonymous protected media" 401 403 404 ||
  die "expect_one_of rejected an allowed status"

# --- an auth boundary that starts answering anonymously fails ---------------

# Every admin, authoring, and account route rejecting, and protected media not
# being served, is the passing shape.
http_code() {
  case "$1" in
    *"/api/v1/media/playback-manifests/"*) printf '404' ;;
    *) printf '401' ;;
  esac
}
check_authorization_boundaries >/dev/null 2>&1 ||
  die "correctly refused anonymous requests were reported as a failure"

# An admin route answering 200 must fail.
http_code() {
  case "$1" in
    *"/api/v1/admin/courses") printf '200' ;;
    *"/api/v1/media/playback-manifests/"*) printf '404' ;;
    *) printf '401' ;;
  esac
}
if (check_authorization_boundaries >/dev/null 2>&1); then
  die "an admin route answering anonymously was accepted"
fi

# Protected media served anonymously must fail.
http_code() {
  case "$1" in
    *"/api/v1/media/playback-manifests/"*) printf '200' ;;
    *) printf '401' ;;
  esac
}
if (check_authorization_boundaries >/dev/null 2>&1); then
  die "protected media served to an anonymous caller was accepted"
fi
unset -f http_code

# --- the anonymous admission boundary stays fail-closed ---------------------

BOOTSTRAP_HEADERS="$TEMPORARY/bootstrap.headers"
printf 'HTTP/2 200\r\nset-cookie: __Host-gradex_anon=abc; Path=/; HttpOnly; Secure; SameSite=Strict\r\n' \
  >"$BOOTSTRAP_HEADERS"
grep --extended-regexp --ignore-case --quiet \
  '^set-cookie: __Host-gradex_anon=.*; Path=/;.*HttpOnly;.*Secure;.*SameSite=Strict' "$BOOTSTRAP_HEADERS" ||
  die "the pinned fail-closed cookie shape no longer matches a compliant response"

DOMAIN_SCOPED_HEADERS="$TEMPORARY/domain.headers"
printf 'HTTP/2 200\r\nset-cookie: __Host-gradex_anon=abc; Path=/; Domain=example.test; HttpOnly; Secure; SameSite=Strict\r\n' \
  >"$DOMAIN_SCOPED_HEADERS"
grep --extended-regexp --ignore-case --quiet \
  '^set-cookie: __Host-gradex_anon=.*;.*Domain=' "$DOMAIN_SCOPED_HEADERS" ||
  die "a domain-scoped anonymous cookie would no longer be detected"

# The smoke must still assert every leg of the boundary.
for expectation in \
  'foreign-origin login' \
  'invalid-CSRF login' \
  'CSRF_VALIDATION_FAILED' \
  'AUTHENTICATION_FAILED' \
  'cross-origin preflight'; do
  grep --fixed-strings --quiet -e "$expectation" "$TARGET" ||
    die "the live smoke no longer asserts: $expectation"
done

# --- run-scoped provider objects are removed --------------------------------

S3_BUCKET="gradex-guard-test"
S3_ENDPOINT="https://provider.example.test"
S3_ACCESS_KEY="guard"
S3_SECRET_KEY="guard"

# A provider that deletes on request, and then reports the object gone.
provider_client() {
  case "$1" in
    rm) return 0 ;;
    stat) return 1 ;;
    *) return 1 ;;
  esac
}
LIVE_PROVIDER_OBJECT="capacity/live-smoke-guardtest/test/master.m3u8"
remove_provider_object >/dev/null 2>&1 ||
  die "a successful run-scoped cleanup was reported as a failure"
[ -z "$LIVE_PROVIDER_OBJECT" ] ||
  die "the run-scoped object was not cleared after removal"

# A provider where the object survives must report the failure.
provider_client() {
  case "$1" in
    rm) return 0 ;;
    stat) return 0 ;;
    *) return 1 ;;
  esac
}
LIVE_PROVIDER_OBJECT="capacity/live-smoke-guardtest/test/master.m3u8"
if (remove_provider_object >/dev/null 2>&1); then
  die "a surviving run-scoped provider object was reported as cleaned"
fi
unset -f provider_client
LIVE_PROVIDER_OBJECT=""

# The provider write is confined to the disposable capacity namespace and
# carries the run identifier.
grep --fixed-strings --quiet 'capacity/live-smoke-$LIVE_RUN_ID/' "$TARGET" ||
  die "the provider write is no longer confined to a run-scoped capacity prefix"

# --- discovery, not hardcoded database identity -----------------------------

if grep --extended-regexp --quiet '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' "$BODY"; then
  die "the live smoke hardcodes a database identity instead of discovering one"
fi

printf 'live-staging-acceptance-guards: production refusal, non-destructiveness, topology independence, health gating, auth-boundary detection, and run-scoped cleanup verified\n' >&2
