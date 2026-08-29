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
#   command line, and a production hostname may never be verified in direct mode
#   at all.
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

set -euo pipefail

EDGE_MODE=""
EDGE_ORIGIN=""
EDGE_HOSTNAME=""

# The exit trap runs after main returns, so the captured header files must
# outlive its scope.
EDGE_TEMPORARY=""

# Hostnames whose verification must always include the Cloudflare-facing
# policy. Direct mode is refused for these outright: no environment variable,
# and no omission of one, may weaken them.
EDGE_PRODUCTION_HOSTNAMES=(
  gradexcourses.com
  www.gradexcourses.com
)

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

assert_mode_allowed_for_hostname() {
  local hostname="$1" mode="$2" candidate
  [ "$mode" = direct ] || return 0
  for candidate in "${EDGE_PRODUCTION_HOSTNAMES[@]}"; do
    [ "$hostname" = "$candidate" ] || continue
    die "$hostname is a production hostname and must be verified with --mode cloudflare"
  done
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
  assert_mode_allowed_for_hostname "$EDGE_HOSTNAME" "$EDGE_MODE"

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
      note "public DNS, HTTPS certificate, frontend, health, readiness, security headers, and closed internal ports passed"
      note "direct mode verified an origin-served edge and made NO claim about Cloudflare proxying"
      ;;
  esac
}

if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
  main "$@"
fi
