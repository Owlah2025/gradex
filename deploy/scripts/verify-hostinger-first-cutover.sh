#!/usr/bin/env bash
#
# Proves the Hostinger harness can bring a production deployment up privately on
# a VPS whose public ports are still owned by another deployment, and that the
# port handover stays one deliberate act.
#
# THE PROBLEM THIS SOLVES
#   `up` started the whole stack including the edge, and the edge is the only
#   service that binds host ports. On this VPS the staging edge already owns
#   80/tcp, 443/tcp and 443/udp, so a first production `up` could not succeed —
#   and it would fail *after* migrating the production database, which is the
#   worst possible moment to discover a port conflict.
#
#   `verify` probes PUBLIC_ORIGIN, so it cannot verify a deployment that has no
#   hostname and no certificate yet either. Neither command was wrong; both
#   assume a deployment that already owns the public edge.
#
#   So the tier and the edge are separable now: up-core / verify-core prove the
#   application privately, up-edge takes the ports once the operator has
#   released them. `up` and `verify` keep their previous meaning by composing
#   the same pieces.
#
# This is a static and structural check. It starts no container, needs no
# provider, and prints no value.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOST_SCRIPT="$ROOT/deploy/hostinger/host.sh"
COMPOSE_FILE="$ROOT/deploy/hostinger/compose.yml"

note() { printf 'hostinger-first-cutover: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

extract() {
  awk -v name="$1" '
    $0 ~ "^" name "\\(\\)" { capture = 1 }
    capture { print }
    capture && /^}/ { exit }
  ' "$HOST_SCRIPT"
}

core="$(extract start_core)"
edge="$(extract start_edge)"
full="$(extract start_environment)"
core_verify="$(extract verify_core)"
public_verify="$(extract verify_environment)"
scope="$(extract assert_production_project_scope)"
ports="$(extract assert_edge_ports_available)"
bootstrap="$(extract bootstrap_admin)"

for name in core edge full core_verify public_verify scope ports bootstrap; do
  [ -n "${!name}" ] || die "could not extract $name from host.sh"
done

# --- 1. the pre-edge start excludes the edge --------------------------------

printf '%s' "$core" | grep --quiet -e 'compose up --detach --no-deps api worker frontend' ||
  die "start_core no longer starts the application services without dependencies"
# Read the services named on every `compose up` line rather than pattern-matching
# the whole line: an earlier version of this check used `[^\n]*`, which in a
# POSIX bracket expression excludes a literal `n`, so `... frontend edge` never
# matched and the mutation that adds the edge back slipped through.
compose_up_services() {
  printf '%s\n' "$1" | sed -n 's/^[[:space:]]*compose up //p' | tr ' ' '\n' |
    grep --extended-regexp --invert-match '^(--[a-z-]+|)$' || true
}
if compose_up_services "$core" | grep --quiet --line-regexp edge; then
  die "start_core starts the edge; the pre-edge bring-up must never bind host ports"
fi
if printf '%s' "$core" | grep --quiet 'wait_for_status edge'; then
  die "start_core waits on the edge, so it cannot complete before the port handover"
fi
core_services="$(compose_up_services "$core" | sort -u | tr '\n' ' ')"
[ "$core_services" = "api frontend migrate postgres redis worker " ] ||
  die "start_core brings up an unexpected service set: $core_services"
note "pre-edge start brings up exactly postgres, redis, migrate, api, worker and frontend — never the edge"

# --- 2. it still migrates, and gates on migration success -------------------

printf '%s' "$core" | grep --quiet -e 'compose up --detach migrate' ||
  die "start_core no longer runs migrations"
printf '%s' "$core" | grep --quiet 'wait_for_completion migrate' ||
  die "start_core no longer requires the migration to succeed"
printf '%s' "$core" | grep --quiet '^  prepare$' ||
  die "start_core no longer runs preflight before touching the deployment"
note "pre-edge start runs preflight, migrates, and requires the migration to succeed"

# --- 3. every application service is health-gated ---------------------------

for gate in 'wait_for_status postgres healthy' 'wait_for_status redis healthy' \
  'wait_for_status api healthy' 'wait_for_status worker running' \
  'wait_for_status frontend healthy'; do
  printf '%s' "$core" | grep --quiet --fixed-strings "$gate" ||
    die "start_core lost the gate: $gate"
done
note "postgres, redis, api, worker and frontend are all health-gated before the tier is declared up"

# --- 4. private verification never depends on the public origin -------------

if printf '%s' "$core_verify" | grep --quiet 'PUBLIC_ORIGIN'; then
  die "verify_core reads PUBLIC_ORIGIN; private verification must not depend on a public hostname"
fi
if printf '%s' "$core_verify" | grep --quiet 'curl'; then
  die "verify_core uses curl; readiness must be reached over the private Compose path"
fi
printf '%s' "$core_verify" | grep --quiet 'docker exec "$api_id" wget -qO- http://127.0.0.1:8080/readyz' ||
  die "verify_core no longer reaches API readiness over the container's private loopback"
note "private verification reaches readiness inside the container, with no hostname, DNS or certificate"

# --- 5/6. it proves schema cleanliness and the Redis TLS posture ------------

printf '%s' "$core_verify" | grep --quiet 'image_max_schema_version "$GRADEX_BACKEND_IMAGE"' ||
  die "verify_core no longer compares the schema against the selected backend image"
printf '%s' "$core_verify" | grep --quiet 'expected_schema|false' ||
  die "verify_core no longer requires a clean schema"
printf '%s' "$core_verify" | grep --quiet 'die "Redis accepted plaintext"' ||
  die "verify_core no longer rejects plaintext Redis"
printf '%s' "$core_verify" | grep --quiet 'die "Redis did not reject unauthenticated TLS"' ||
  die "verify_core no longer requires unauthenticated TLS to be refused"
printf '%s' "$core_verify" | grep --quiet 'die "authenticated Redis TLS failed"' ||
  die "verify_core no longer proves authenticated Redis TLS"
note "private verification pins clean schema, plaintext refusal, unauthenticated-TLS refusal, and authenticated Redis TLS"

# --- 7/8. the edge start is isolated and gated ------------------------------

printf '%s' "$edge" | grep --quiet -e 'compose up --detach --no-deps edge' ||
  die "start_edge no longer starts only the edge"
edge_services="$(compose_up_services "$edge")"
[ "$edge_services" = edge ] ||
  die "start_edge brings up more than the edge: $(printf '%s' "$edge_services" | tr '\n' ' ')"
printf '%s' "$edge" | grep --quiet 'require_status api healthy' ||
  die "start_edge no longer requires a healthy API before taking the ports"
printf '%s' "$edge" | grep --quiet 'require_status frontend healthy' ||
  die "start_edge no longer requires a healthy frontend before taking the ports"
printf '%s' "$edge" | grep --quiet 'validate_environment' ||
  die "start_edge no longer validates the environment and release identity"
note "edge start is a separate act: it validates, requires a healthy tier, and starts only the edge"

# --- 9. the ordinary commands keep their meaning ----------------------------

printf '%s' "$full" | grep --quiet 'start_core' ||
  die "up no longer brings up the application tier"
printf '%s' "$full" | grep --quiet 'start_edge' ||
  die "up no longer brings up the edge; the ordinary command must still produce a serving deployment"
printf '%s' "$public_verify" | grep --quiet 'PUBLIC_ORIGIN' ||
  die "verify no longer probes the public origin; the public check must not be replaced by the private one"
for command in up up-core up-edge verify verify-core; do
  grep --quiet --fixed-strings "  $command) " "$HOST_SCRIPT" ||
    die "host.sh does not dispatch $command"
  grep --quiet --fixed-strings "|$command|" "$HOST_SCRIPT" ||
    grep --quiet --fixed-strings "{prepare|$command|" "$HOST_SCRIPT" ||
    die "host.sh usage does not document $command"
done
note "up and verify keep their previous meaning, and every command is dispatched and documented"

# --- 10. no command reaches into another Compose project --------------------

# Every Compose invocation goes through the one project-scoped helper.
grep --quiet --fixed-strings -e '--project-name "$S12_PROJECT"' "$HOST_SCRIPT" ||
  die "the compose helper is no longer scoped to the configured project"
# host.sh may remove only the two containers it creates itself: its own
# project-scoped database tunnel and the standalone restore-verification
# database. Anything else would be reaching into a deployment it does not own.
while read -r line; do
  [ -n "$line" ] || continue
  case "$line" in
    *'"$S12_DB_TUNNEL_NAME"'* | *'"$S12_RESTORE_VERIFY_CONTAINER"'*) ;;
    *) die "host.sh stops or removes a container it does not own: $line" ;;
  esac
done <<<"$(grep --extended-regexp '^[^#]*docker (stop|kill|rm) ' "$HOST_SCRIPT" || true)"
printf '%s' "$ports" | grep --quiet '$3 != project' ||
  die "the port-conflict check no longer excludes this project's own containers"
if printf '%s' "$ports" | grep --extended-regexp --quiet '(docker (stop|kill|rm)|compose down)'; then
  die "the port-conflict check tries to release the ports itself; the operator must do that deliberately"
fi
note "compose access stays project-scoped, and a conflicting publisher is reported rather than stopped"

# --- 11. production cannot silently inherit the staging deployment ----------

printf '%s' "$scope" | grep --quiet 'S12_PROJECT_DECLARED' ||
  die "production no longer requires GRADEX_HOST_PROJECT to be declared"
printf '%s' "$scope" | grep --quiet 'S12_STATE_DIR_DECLARED' ||
  die "production no longer requires GRADEX_HOST_STATE_DIR to be declared"
printf '%s' "$scope" | grep --quiet 'S12_PROJECT_STAGING_DEFAULT' ||
  die "production no longer refuses the staging Compose project by name"
grep --quiet --fixed-strings 'S12_STATE_DIR_DECLARED="${GRADEX_HOST_STATE_DIR+declared}"' "$HOST_SCRIPT" ||
  die "the state-directory declaration is no longer captured before the default is applied"
grep --quiet --fixed-strings 'S12_PROJECT_DECLARED="${GRADEX_HOST_PROJECT+declared}"' "$HOST_SCRIPT" ||
  die "the project declaration is no longer captured before the default is applied"
grep --quiet --fixed-strings 'assert_production_project_scope' "$HOST_SCRIPT" ||
  die "the project-scope guard is never called"

# Exercise the real predicate.
S12_PROJECT_STAGING_DEFAULT=gradex-staging
eval "$scope"

(
  APP_ENV=production S12_PROJECT_DECLARED="" S12_STATE_DIR_DECLARED=declared \
    S12_PROJECT=gradex-production assert_production_project_scope >/dev/null 2>&1
) && die "production ran without declaring its Compose project"
(
  APP_ENV=production S12_PROJECT_DECLARED=declared S12_STATE_DIR_DECLARED="" \
    S12_PROJECT=gradex-production assert_production_project_scope >/dev/null 2>&1
) && die "production ran without declaring its state directory"
(
  APP_ENV=production S12_PROJECT_DECLARED=declared S12_STATE_DIR_DECLARED=declared \
    S12_PROJECT=gradex-staging assert_production_project_scope >/dev/null 2>&1
) && die "production ran against the staging Compose project"
APP_ENV=production S12_PROJECT_DECLARED=declared S12_STATE_DIR_DECLARED=declared \
  S12_PROJECT=gradex-production assert_production_project_scope >/dev/null 2>&1 ||
  die "a correctly declared production deployment was refused"
APP_ENV=staging S12_PROJECT_DECLARED="" S12_STATE_DIR_DECLARED="" \
  S12_PROJECT=gradex-staging assert_production_project_scope >/dev/null 2>&1 ||
  die "staging was forced to declare what it already defaults to"
note "production must name its project and state directory, and may never inherit the staging defaults"

# --- the Administrator bootstrap is explicit, isolated and credential-safe ---
# Non-secret placeholders so the model renders here. None is a credential, none
# is printed, and BOOTSTRAP_ADMIN_PASSWORD is deliberately absent: the whole
# point below is that the model does not reference it.
export APP_ENV=staging PASSWORD_SCREEN_MODE=adapter \
  COMPROMISED_PASSWORD_ADAPTER_APPROVED=false EMAIL_ENABLED=true EMAIL_PROVIDER=resend \
  PUBLIC_ORIGIN=https://gradex.test STAGING_HOSTNAME=gradex.test ACME_EMAIL=ops@gradex.test \
  POSTGRES_DB=gradex POSTGRES_PASSWORD=placeholder \
  DATABASE_URL='postgres://placeholder:placeholder@postgres:5432/gradex?sslmode=disable' \
  RESTORE_DATABASE_URL='postgres://placeholder:placeholder@restore-postgres:5432/r?sslmode=disable' \
  RESTORE_POSTGRES_PASSWORD=placeholder \
  GRADEX_E2E_ADMIN_DB_URL='postgres://placeholder:placeholder@postgres:5432/postgres?sslmode=disable' \
  REDIS_PASSWORD=placeholder REDIS_TLS_CA_CERT_FILE_HOST=/dev/null \
  REDIS_TLS_SERVER_CERT_FILE_HOST=/dev/null REDIS_TLS_SERVER_KEY_FILE_HOST=/dev/null \
  S3_ENDPOINT=https://example.r2.cloudflarestorage.com S3_BUCKET=gradex-media \
  S3_ACCESS_KEY=placeholder S3_SECRET_KEY=placeholder PLAYBACK_TOKEN_SECRET=placeholder \
  SALES_WHATSAPP_NUMBER=96500000000 SESSION_CSRF_KEY=placeholder \
  ANONYMOUS_COOKIE_SIGNING_KEY=placeholder ANONYMOUS_CSRF_KEY=placeholder \
  ADMISSION_LIMITER_HMAC_KEY=placeholder OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION=v1 \
  OUTBOX_PROTECTED_PAYLOAD_KEY=placeholder LEGAL_OPERATOR_NAME=Gradex \
  LEGAL_REGISTRATION_NUMBER=RENDER-ONLY LEGAL_REGISTERED_ADDRESS=Address \
  PRIVACY_EMAIL=p@gradex.test SUPPORT_EMAIL=s@gradex.test SECURITY_EMAIL=x@gradex.test \
  EMAIL_API_KEY=placeholder EMAIL_FROM_ADDRESS=no-reply@gradex.test \
  GRADEX_BACKEND_IMAGE=gradex-backend:check GRADEX_FRONTEND_IMAGE=gradex-frontend:check \
  GRADEX_PROOF_IMAGE=gradex-backend-proof:check \
  GRADEX_RELEASE_SHA=0123456789abcdef0123456789abcdef01234567
unset BOOTSTRAP_ADMIN_PASSWORD BOOTSTRAP_ADMIN_EMAIL BOOTSTRAP_ADMIN_OPERATION_ID \
  BOOTSTRAP_ADMIN_PRINCIPAL BOOTSTRAP_ADMIN_DISPLAY_NAME


# 1/2/3. It is profiled, so it is absent from the default service set and can
#        never be reached by `up`, `up-core`, `up-edge`, or dependency
#        reconciliation.
default_services="$(
  docker compose --file "$COMPOSE_FILE" --project-name hostinger-bootstrap-check config --services 2>/dev/null
)" || die "the Hostinger topology no longer renders"
if printf '%s\n' "$default_services" | grep --quiet --line-regexp bootstrap-admin; then
  die "bootstrap-admin appears in the default service list; it must stay behind its profile"
fi
profiled_services="$(
  docker compose --file "$COMPOSE_FILE" --project-name hostinger-bootstrap-check --profile bootstrap config --services 2>/dev/null
)" || die "the bootstrap profile does not render"
printf '%s\n' "$profiled_services" | grep --quiet --line-regexp bootstrap-admin ||
  die "bootstrap-admin does not render even when its profile is selected"
for startup in core edge full; do
  if compose_up_services "${!startup}" | grep --quiet --line-regexp bootstrap-admin; then
    die "the $startup startup path brings up bootstrap-admin; it must be a deliberate act only"
  fi
  if printf '%s' "${!startup}" | grep --quiet -- '--profile bootstrap'; then
    die "the $startup startup path selects the bootstrap profile"
  fi
done
note "bootstrap-admin is profiled, absent from normal startup, and never reached by up, up-core or up-edge"

# 4/5. It publishes no port and does not depend on the edge service.
bootstrap_model="$(
  docker compose --file "$COMPOSE_FILE" --project-name hostinger-bootstrap-check --profile bootstrap config 2>/dev/null |
    sed -n '/^  bootstrap-admin:/,/^  [a-z]/p'
)"
[ -n "$bootstrap_model" ] || die "could not render the bootstrap-admin service"
if printf '%s' "$bootstrap_model" | grep --quiet '^    ports:'; then
  die "bootstrap-admin publishes a host port"
fi
if printf '%s' "$bootstrap_model" | grep --quiet 'depends_on'; then
  die "bootstrap-admin declares dependencies; reconciliation must never start it or anything else"
fi
# Compose renders the value as `restart: 'no'`; the committed model writes it
# with double quotes. Accept either quoting, reject any restarting policy.
printf '%s' "$bootstrap_model" | grep --extended-regexp --quiet "restart: ['\"]no['\"]" ||
  die "bootstrap-admin may restart; a one-shot credential operation must run once"
printf '%s' "$bootstrap_model" | grep --quiet 'gradex-bootstrap-admin' ||
  die "bootstrap-admin no longer overrides the proof image entrypoint"
note "bootstrap-admin publishes no port, declares no dependency, never restarts, and overrides the entrypoint"

# 6. The canonical command runs only that one-shot, and removes it.
printf '%s' "$bootstrap" | grep --quiet -- '--profile bootstrap run --rm --no-deps' ||
  die "the bootstrap command no longer runs the profiled one-shot with --rm --no-deps"
bootstrap_up="$(compose_up_services "$bootstrap")"
[ -z "$bootstrap_up" ] ||
  die "the bootstrap command brings services up: $(printf '%s' "$bootstrap_up" | tr '\n' ' ')"
if printf '%s' "$bootstrap" | grep --extended-regexp --quiet '\bstart_edge\b|\bedge\b'; then
  die "the bootstrap command touches the edge"
fi

# 7/8/9. It gates on a healthy core, stays project-scoped, and inherits the
#        production scoping refusal through validate_environment.
printf '%s' "$bootstrap" | grep --quiet 'require_status postgres healthy' ||
  die "the bootstrap command no longer requires a healthy PostgreSQL"
printf '%s' "$bootstrap" | grep --quiet 'require_status api healthy' ||
  die "the bootstrap command no longer requires a healthy API"
printf '%s' "$bootstrap" | grep --quiet 'validate_environment' ||
  die "the bootstrap command no longer validates the protected runtime and release identity"
printf '%s' "$bootstrap" | grep --quiet -- '-confirm-production' ||
  die "the bootstrap command no longer passes the production acknowledgement"

# 11. A non-zero exit from the one-shot must fail the operator command.
printf '%s' "$bootstrap" | grep --quiet 'die "the Administrator bootstrap failed' ||
  die "the bootstrap command does not fail when the one-shot exits non-zero"

# 10. No credential may reach the committed model, a static render, or the
#     committed environment example. The passphrase is forwarded by name only.
# Forwarded by name, never by value: `--env NAME` inherits from the operator's
# environment, while `--env NAME=value` would write the passphrase into the
# container definition.
printf '%s' "$bootstrap" | grep --extended-regexp --quiet -- '--env BOOTSTRAP_ADMIN_PASSWORD([[:space:]]|\\|$)' ||
  die "the passphrase is no longer forwarded by name; a value here would enter the container definition"
# Scan the model, not its comments: the service's comment explains why the
# passphrase is absent, and an explanation is not a reference.
if grep -v '^[[:space:]]*#' "$COMPOSE_FILE" | grep --quiet 'BOOTSTRAP_ADMIN'; then
  die "the Compose model references BOOTSTRAP_ADMIN configuration; the passphrase must never be part of the committed model"
fi
if printf '%s' "$bootstrap_model" | grep --quiet 'BOOTSTRAP_ADMIN'; then
  die "a rendered bootstrap service carries BOOTSTRAP_ADMIN configuration"
fi
if grep --extended-regexp --quiet '^BOOTSTRAP_ADMIN' "$ROOT/deploy/hostinger/runtime.env.example"; then
  die "the runtime example invites a bootstrap credential into the protected runtime file"
fi
if printf '%s' "$bootstrap" | grep --extended-regexp --quiet -- '-password|BOOTSTRAP_ADMIN_PASSWORD='; then
  die "the bootstrap command puts the passphrase into an argument"
fi
note "no bootstrap credential reaches the committed model, a rendered config, or the runtime example"

# --- the edge is still the only service that publishes host ports -----------

published="$(
  awk '
    /^  [a-z][a-z-]*:$/ { service = $1; sub(/:$/, "", service) }
    /^    ports:$/ { inports = 1; next }
    inports && /^      - / { print service; next }
    inports { inports = 0 }
  ' "$COMPOSE_FILE" | sort -u
)"
[ "$published" = edge ] ||
  die "services other than the edge publish host ports: $published"
note "the edge remains the only service in the topology that publishes host ports"

printf 'hostinger-first-cutover: private tier bring-up, private verification, isolated edge handover, project scoping, and shared-host port safety verified\n' >&2
