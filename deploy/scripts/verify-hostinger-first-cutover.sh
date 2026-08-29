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

for name in core edge full core_verify public_verify scope ports; do
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
