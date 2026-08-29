#!/usr/bin/env bash
#
# verify-public.sh — verify a public Gradex edge against the topology it is
# actually deployed behind.
#
# WHY THE MODE IS EXPLICIT
#   This check used to assume one topology: it required a Cloudflare proxy
#   response header on every origin it was pointed at. Staging is served
#   directly by the VPS Caddy with its own Let's Encrypt certificate, so the
#   check failed on a correctly deployed environment and reported nothing about
#   the many properties that did hold.
#
#   Removing the Cloudflare assertion would have been the wrong repair: it is
#   the assertion production most needs. Auto-detecting the topology would have
#   been worse, because a production origin that silently lost its proxy would
#   then quietly verify under the weaker policy. So the topology is named on the
#   command line, and a production hostname is refused in direct mode unless the
#   operator supplies the one-shot Stage-A acknowledgement described below.
#
# MODES
#   --mode direct
#     DNS, HTTPS certificate validity and hostname, HTTP to HTTPS redirect,
#     health and readiness, security headers, and closed internal ports.
#     Makes no claim about Cloudflare, and says so.
#
#   --mode cloudflare
#     Everything direct requires, plus Cloudflare proxy evidence and the
#     conservative rollout expectation that the frontend is not being served
#     from the Cloudflare cache.
#
# USAGE
#   deploy/hostinger/verify-public.sh --mode direct https://staging.example.com
#   deploy/hostinger/verify-public.sh --mode cloudflare https://example.com
#
#   Stage A only, inside the intentional DNS-only production window:
#     GRADEX_PUBLIC_VERIFY_ALLOW_PRODUCTION_DIRECT=i-have-authorized-production-direct-verification \
#       deploy/hostinger/verify-public.sh --mode direct https://gradexcourses.com

set -euo pipefail

EDGE_MODE=""
EDGE_ORIGIN=""
EDGE_HOSTNAME=""

# The exit trap runs after main returns, so the captured header files must
# outlive its scope.
EDGE_TEMPORARY=""

# Hostnames whose steady-state verification must include the Cloudflare-facing
# policy. Direct mode is refused for these unless the operator supplies the
# one-shot Stage-A acknowledgement below.
EDGE_PRODUCTION_HOSTNAMES=(
  gradexcourses.com
  www.gradexcourses.com
)

# The production cutover is deliberately two-stage. In Stage A the apex points
# straight at the VPS with the Cloudflare proxy still off, so Caddy can complete
# an ACME challenge and obtain a publicly valid origin certificate; a proxied
# origin cannot, because Always Use HTTPS redirects the HTTP-01 challenge to a
# TLS endpoint that has no certificate yet. Direct verification of the
# production origin is therefore a real, intended operation — but only inside
# that window, and only when the operator says so.
#
# The value is a sentence, not a boolean, for the same reason the live smoke
# uses one: `1` or `true` is a value an environment can acquire by accident, and
# this decision must be typed on purpose. It is an operator acknowledgement, not
# a secret, it is read from the environment and never written anywhere, and it
# relaxes exactly one thing — the hostname refusal. Every other direct-mode
# check still runs, and Cloudflare mode is untouched by it.
EDGE_PRODUCTION_DIRECT_AUTHORIZATION="i-have-authorized-production-direct-verification"

# Ports that belong to internal services and must never answer on the public
# hostname.
EDGE_INTERNAL_PORTS=(3000 8080 5432 6379 9000)

note() { printf 's12-public-verify: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

usage() {
  die "usage: $0 --mode {direct|cloudflare} https://staging.example.com"
}

parse_arguments() {
  EDGE_MODE=""
  EDGE_ORIGIN=""
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --mode)
        [ "$#" -ge 2 ] || usage
        [ -z "$EDGE_MODE" ] || die "--mode was given more than once"
        EDGE_MODE="$2"
        shift 2
        ;;
      --mode=*)
        [ -z "$EDGE_MODE" ] || die "--mode was given more than once"
        EDGE_MODE="${1#--mode=}"
        shift
        ;;
      -*) usage ;;
      *)
        [ -z "$EDGE_ORIGIN" ] || usage
        EDGE_ORIGIN="${1%/}"
        shift
        ;;
    esac
  done
  [ -n "$EDGE_ORIGIN" ] || usage
  # The mode is required. Defaulting it would reintroduce exactly the silent
  # downgrade this interface exists to prevent.
  [ -n "$EDGE_MODE" ] || die "--mode is required and must be direct or cloudflare"
  case "$EDGE_MODE" in
    direct | cloudflare) ;;
    *) die "--mode must be direct or cloudflare, not $EDGE_MODE" ;;
  esac
  [[ "$EDGE_ORIGIN" =~ ^https://[A-Za-z0-9.-]+$ ]] ||
    die "origin must be a credential-free HTTPS origin"
  EDGE_HOSTNAME="${EDGE_ORIGIN#https://}"
}

# Returns 0 when this run is the authorized Stage-A production-direct probe, so
# the caller can apply the extra check that only makes sense in that window.
EDGE_PRODUCTION_DIRECT=0

assert_mode_allowed_for_hostname() {
  local hostname="$1" mode="$2" authorization="${3:-}" candidate
  EDGE_PRODUCTION_DIRECT=0
  [ "$mode" = direct ] || return 0
  for candidate in "${EDGE_PRODUCTION_HOSTNAMES[@]}"; do
    [ "$hostname" = "$candidate" ] || continue
    # Exact match only. A near-miss is a refusal, never a warning.
    [ "$authorization" = "$EDGE_PRODUCTION_DIRECT_AUTHORIZATION" ] ||
      die "$hostname is a production hostname and must be verified with --mode cloudflare; for the Stage-A DNS-only window only, set GRADEX_PUBLIC_VERIFY_ALLOW_PRODUCTION_DIRECT=$EDGE_PRODUCTION_DIRECT_AUTHORIZATION"
    EDGE_PRODUCTION_DIRECT=1
    note "production hostname $hostname was explicitly authorized for one Stage-A direct probe"
    return 0
  done
}

# Stage A claims the proxy is off. If the origin answers through Cloudflare, that
# claim is false and the operator is not in the window they think they are in.
# This runs only for the authorized production-direct probe: an ordinary direct
# verification makes no claim about Cloudflare either way.
assert_stage_a_proxy_absent() {
  local headers="$1"
  [ "$EDGE_PRODUCTION_DIRECT" = 1 ] || return 0
  if grep --extended-regexp --ignore-case --quiet '^cf-ray:' "$headers"; then
    die "the authorized Stage-A probe reached a Cloudflare-proxied origin; the proxy must stay off during the DNS-only window, and a proxied origin is verified with --mode cloudflare"
  fi
  note "Stage-A probe confirmed the origin is served directly, with no Cloudflare proxy in front"
}

assert_cloudflare_evidence() {
  local headers="$1"
  grep --extended-regexp --ignore-case --quiet '^cf-ray:' "$headers" ||
    die "Cloudflare proxy response header is absent"
  if grep --extended-regexp --ignore-case --quiet \
    '^cf-cache-status:[[:space:]]*HIT' "$headers"; then
    die "Cloudflare cached the frontend unexpectedly during the conservative rollout"
  fi
}

assert_internal_ports_closed() {
  local hostname="$1" port
  for port in "${EDGE_INTERNAL_PORTS[@]}"; do
    if timeout 5 bash -c "exec 3<>/dev/tcp/$hostname/$port" 2>/dev/null; then
      die "internal service port $port answered on the public hostname"
    fi
  done
}

require_header() {
  local headers="$1" pattern="$2" check="$3"
  grep --extended-regexp --ignore-case --quiet "$pattern" "$headers" ||
    die "$check response is missing the required header"
}

main() {
  parse_arguments "$@"
  assert_mode_allowed_for_hostname "$EDGE_HOSTNAME" "$EDGE_MODE" \
    "${GRADEX_PUBLIC_VERIFY_ALLOW_PRODUCTION_DIRECT:-}"

  local tool
  for tool in curl getent jq mktemp openssl timeout; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done

  getent ahosts "$EDGE_HOSTNAME" >/dev/null || die "public DNS did not resolve"

  local redirect_headers tls_headers probe_headers problem_headers certificate
  EDGE_TEMPORARY="$(mktemp -d)"
  chmod 700 "$EDGE_TEMPORARY"
  trap 'rm -rf -- "$EDGE_TEMPORARY"' EXIT
  redirect_headers="$EDGE_TEMPORARY/redirect.headers"
  tls_headers="$EDGE_TEMPORARY/tls.headers"
  probe_headers="$EDGE_TEMPORARY/probe.headers"
  problem_headers="$EDGE_TEMPORARY/problem.headers"

  curl --silent --show-error --max-time 15 --output /dev/null \
    --dump-header "$redirect_headers" "http://$EDGE_HOSTNAME/"
  grep --extended-regexp --ignore-case --quiet '^location: https://' "$redirect_headers" ||
    die "HTTP did not redirect intentionally to HTTPS"

  curl --fail --silent --show-error --max-time 15 --output /dev/null \
    --dump-header "$tls_headers" "$EDGE_ORIGIN/"
  curl --fail --silent --show-error --max-time 15 "$EDGE_ORIGIN/healthz" |
    jq --exit-status '.status == "ok"' >/dev/null
  curl --fail --silent --show-error --max-time 15 --dump-header "$probe_headers" \
    "$EDGE_ORIGIN/readyz" |
    jq --exit-status '.status == "ok" and .checks.postgres == "ok" and .checks.redis == "ok" and .checks.schema == "ok"' >/dev/null

  # An unauthenticated admin route is the API's problem+json path, which is
  # where content-type sniffing actually matters. Successful JSON bodies do not
  # carry nosniff, so asserting it on /readyz would have pinned a guarantee the
  # API does not make.
  curl --silent --show-error --max-time 15 --output /dev/null \
    --dump-header "$problem_headers" "$EDGE_ORIGIN/api/v1/admin/courses"

  certificate="$(openssl s_client -connect "$EDGE_HOSTNAME:443" -servername "$EDGE_HOSTNAME" \
    </dev/null 2>/dev/null)"
  printf '%s' "$certificate" | openssl x509 -noout -checkend 86400 >/dev/null ||
    die "public certificate is invalid or expires within 24 hours"
  printf '%s' "$certificate" | openssl x509 -noout -checkhost "$EDGE_HOSTNAME" >/dev/null ||
    die "public certificate does not cover the verified hostname"

  require_header "$probe_headers" '^cache-control:.*no-store' "readiness probe"
  require_header "$problem_headers" '^x-content-type-options:[[:space:]]*nosniff' "API problem"
  require_header "$problem_headers" '^content-type:[[:space:]]*application/problem\+json' "API problem"

  assert_internal_ports_closed "$EDGE_HOSTNAME"

  case "$EDGE_MODE" in
    cloudflare)
      assert_cloudflare_evidence "$tls_headers"
      note "public DNS, Cloudflare edge, HTTPS certificate, frontend, health, readiness, security headers, and closed internal ports passed"
      ;;
    direct)
      assert_stage_a_proxy_absent "$tls_headers"
      note "public DNS, HTTPS certificate, frontend, health, readiness, security headers, and closed internal ports passed"
      note "direct mode verified an origin-served edge and made NO claim about Cloudflare proxying"
      ;;
  esac
}

if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
  main "$@"
fi
