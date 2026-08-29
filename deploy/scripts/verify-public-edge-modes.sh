#!/usr/bin/env bash
#
# Proves deploy/hostinger/verify-public.sh selects its edge policy from an
# explicit mode and never downgrades a production hostname to the weaker one.
#
# The regression this guards was observed against the real staging host: the
# verifier demanded Cloudflare proxy evidence from an origin that is
# deliberately served straight from the VPS Caddy, so a healthy deployment
# failed verification. Accepting a direct edge must not become accepting a
# production edge without its proxy, so every negative below must still fail.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TARGET="$ROOT/deploy/hostinger/verify-public.sh"

die() {
  printf 'public-edge-modes: %s\n' "$*" >&2
  exit 1
}

TEMPORARY="$(mktemp -d)"
trap 'rm -rf -- "$TEMPORARY"' EXIT

# The verifier guards its own entry point, so sourcing it defines the real
# functions and the real hostname and port lists without running anything.
# shellcheck disable=SC1090
. "$TARGET"

# --- the mode is required and closed ----------------------------------------

parse_arguments --mode direct https://staging.gradex.network
[ "$EDGE_MODE" = direct ] || die "--mode direct was not parsed"
[ "$EDGE_ORIGIN" = "https://staging.gradex.network" ] || die "origin was not parsed"
[ "$EDGE_HOSTNAME" = "staging.gradex.network" ] || die "hostname was not derived"

parse_arguments --mode=cloudflare https://example.test/
[ "$EDGE_MODE" = cloudflare ] || die "--mode=cloudflare was not parsed"
[ "$EDGE_ORIGIN" = "https://example.test" ] || die "a trailing slash was not trimmed"

# `die` exits, so every negative case runs in a subshell.

if (parse_arguments https://staging.gradex.network >/dev/null 2>&1); then
  die "an omitted --mode was accepted"
fi

if (parse_arguments --mode auto https://staging.gradex.network >/dev/null 2>&1); then
  die "an unknown mode was accepted"
fi

if (parse_arguments --mode direct --mode cloudflare https://staging.gradex.network >/dev/null 2>&1); then
  die "a repeated --mode was accepted"
fi

if (parse_arguments --mode direct >/dev/null 2>&1); then
  die "a missing origin was accepted"
fi

if (parse_arguments --mode direct 'https://user:pass@staging.gradex.network' >/dev/null 2>&1); then
  die "an origin carrying credentials was accepted"
fi

# --- production may never be verified with the direct policy ----------------

for production in "${EDGE_PRODUCTION_HOSTNAMES[@]}"; do
  if (assert_mode_allowed_for_hostname "$production" direct >/dev/null 2>&1); then
    die "$production was accepted under the weaker direct policy"
  fi
  assert_mode_allowed_for_hostname "$production" cloudflare ||
    die "$production was rejected in cloudflare mode"
done

# A staging name stays free to use either policy.
assert_mode_allowed_for_hostname staging.gradex.network direct ||
  die "a staging hostname was rejected in direct mode"

# --- cloudflare mode cannot pass without Cloudflare evidence ----------------

DIRECT_HEADERS="$TEMPORARY/direct.headers"
printf 'HTTP/2 200\r\nvia: 1.1 Caddy\r\ncontent-type: text/html\r\n' >"$DIRECT_HEADERS"

PROXIED_HEADERS="$TEMPORARY/proxied.headers"
printf 'HTTP/2 200\r\ncf-ray: 8f0000000000-FRA\r\ncf-cache-status: DYNAMIC\r\n' >"$PROXIED_HEADERS"

CACHED_HEADERS="$TEMPORARY/cached.headers"
printf 'HTTP/2 200\r\ncf-ray: 8f0000000000-FRA\r\ncf-cache-status: HIT\r\n' >"$CACHED_HEADERS"

if (assert_cloudflare_evidence "$DIRECT_HEADERS" >/dev/null 2>&1); then
  die "cloudflare mode passed on an origin-served response with no cf-ray"
fi

if (assert_cloudflare_evidence "$CACHED_HEADERS" >/dev/null 2>&1); then
  die "a cached Cloudflare frontend response was accepted"
fi

assert_cloudflare_evidence "$PROXIED_HEADERS" ||
  die "a genuinely proxied response was rejected"

# --- security headers -------------------------------------------------------

# The readiness probe carries the app's no-store and the edge's own
# private, no-store. Either satisfies "this response is never cached".
PROBE_HEADERS="$TEMPORARY/probe.headers"
printf 'HTTP/2 200\r\ncache-control: private, no-store\r\ncache-control: no-store\r\ncontent-type: application/json\r\n' \
  >"$PROBE_HEADERS"
require_header "$PROBE_HEADERS" '^cache-control:.*no-store' "readiness probe" ||
  die "an uncacheable readiness probe was rejected"

CACHEABLE_PROBE="$TEMPORARY/cacheable.headers"
printf 'HTTP/2 200\r\ncache-control: public, max-age=60\r\n' >"$CACHEABLE_PROBE"
if (require_header "$CACHEABLE_PROBE" '^cache-control:.*no-store' "readiness probe" >/dev/null 2>&1); then
  die "a cacheable readiness probe was accepted"
fi

# nosniff belongs to the problem+json path, which is the response a browser
# could be tricked into sniffing. Pinning it on a 200 JSON body would assert a
# guarantee the API does not make.
PROBLEM_HEADERS="$TEMPORARY/problem.headers"
printf 'HTTP/2 401\r\ncontent-type: application/problem+json\r\nx-content-type-options: nosniff\r\n' \
  >"$PROBLEM_HEADERS"
require_header "$PROBLEM_HEADERS" '^x-content-type-options:[[:space:]]*nosniff' "API problem" ||
  die "a compliant API problem response was rejected"
require_header "$PROBLEM_HEADERS" '^content-type:[[:space:]]*application/problem\+json' "API problem" ||
  die "a compliant API problem content type was rejected"

SNIFFABLE_PROBLEM="$TEMPORARY/sniffable.headers"
printf 'HTTP/2 401\r\ncontent-type: text/html\r\n' >"$SNIFFABLE_PROBLEM"
if (require_header "$SNIFFABLE_PROBLEM" '^x-content-type-options:[[:space:]]*nosniff' "API problem" >/dev/null 2>&1); then
  die "an API problem response without nosniff was accepted"
fi
if (require_header "$SNIFFABLE_PROBLEM" '^content-type:[[:space:]]*application/problem\+json' "API problem" >/dev/null 2>&1); then
  die "an API problem response that was not problem+json was accepted"
fi

# --- internal ports ---------------------------------------------------------

# A closed port makes the connection attempt fail, which is the passing case.
timeout() { return 1; }
assert_internal_ports_closed edge.example.test ||
  die "closed internal ports were reported as exposed"

# An answering internal port must fail the check.
timeout() { return 0; }
if (assert_internal_ports_closed edge.example.test >/dev/null 2>&1); then
  die "an answering internal service port was accepted"
fi
unset -f timeout

# --- the internal port list still covers the deployed services --------------

for port in 3000 8080 5432 6379 9000; do
  printf '%s\n' "${EDGE_INTERNAL_PORTS[@]}" | grep --line-regexp --quiet "$port" ||
    die "internal port $port is no longer checked"
done

printf 'public-edge-modes: explicit mode, production refusal, Cloudflare evidence, security headers, and port safety verified\n' >&2
