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
  # 2. Unauthorized production direct is refused.
  if (assert_mode_allowed_for_hostname "$production" direct >/dev/null 2>&1); then
    die "$production was accepted in direct mode with no authorization"
  fi
  if (assert_mode_allowed_for_hostname "$production" direct "" >/dev/null 2>&1); then
    die "$production was accepted in direct mode with an empty authorization"
  fi

  # 4. Only the exact acknowledgement counts. A boolean, a truncation, a
  #    near-miss, and a differing case are all refusals, never warnings.
  for wrong in 1 true yes TRUE i-have-authorized-production-direct \
    I-HAVE-AUTHORIZED-PRODUCTION-DIRECT-VERIFICATION \
    " $EDGE_PRODUCTION_DIRECT_AUTHORIZATION" \
    "$EDGE_PRODUCTION_DIRECT_AUTHORIZATION "; do
    if (assert_mode_allowed_for_hostname "$production" direct "$wrong" >/dev/null 2>&1); then
      die "$production accepted the non-exact direct authorization \"$wrong\""
    fi
  done

  # 3. The exact acknowledgement passes the hostname gate, and records that this
  #    run is the Stage-A probe.
  EDGE_PRODUCTION_DIRECT=0
  assert_mode_allowed_for_hostname "$production" direct "$EDGE_PRODUCTION_DIRECT_AUTHORIZATION" \
    >/dev/null 2>&1 ||
    die "$production was refused despite the exact Stage-A authorization"
  [ "$EDGE_PRODUCTION_DIRECT" = 1 ] ||
    die "the authorized Stage-A probe was not recorded as production-direct"

  # 5/6. Cloudflare mode is unaffected by the flag in either direction, and the
  #      flag never marks a cloudflare run as a Stage-A probe.
  EDGE_PRODUCTION_DIRECT=0
  assert_mode_allowed_for_hostname "$production" cloudflare ||
    die "$production was rejected in cloudflare mode"
  [ "$EDGE_PRODUCTION_DIRECT" = 0 ] ||
    die "cloudflare mode was marked as a Stage-A direct probe"
  assert_mode_allowed_for_hostname "$production" cloudflare "$EDGE_PRODUCTION_DIRECT_AUTHORIZATION" ||
    die "the Stage-A flag changed cloudflare-mode acceptance"
  [ "$EDGE_PRODUCTION_DIRECT" = 0 ] ||
    die "the Stage-A flag leaked into cloudflare mode"
done

# 1. A non-production name needs no authorization and is never a Stage-A probe.
EDGE_PRODUCTION_DIRECT=0
assert_mode_allowed_for_hostname staging.gradex.network direct ||
  die "a staging hostname was rejected in direct mode"
[ "$EDGE_PRODUCTION_DIRECT" = 0 ] ||
  die "an ordinary direct verification was treated as a Stage-A production probe"
assert_mode_allowed_for_hostname staging.gradex.network direct "$EDGE_PRODUCTION_DIRECT_AUTHORIZATION" ||
  die "a staging hostname was rejected when the Stage-A flag happened to be set"
[ "$EDGE_PRODUCTION_DIRECT" = 0 ] ||
  die "the Stage-A flag marked a non-production hostname as a production probe"

# The authorization relaxes the hostname refusal and nothing else. Stage A still
# asserts the proxy really is off, so an operator who thinks they are in the
# DNS-only window but is not gets told.
PROXIED_STAGE_A="$TEMPORARY/stage-a-proxied.headers"
printf 'HTTP/2 200\r\ncf-ray: 8f0000000000-FRA\r\n' >"$PROXIED_STAGE_A"
DIRECT_STAGE_A="$TEMPORARY/stage-a-direct.headers"
printf 'HTTP/2 200\r\nvia: 1.1 Caddy\r\n' >"$DIRECT_STAGE_A"

EDGE_PRODUCTION_DIRECT=1
if (assert_stage_a_proxy_absent "$PROXIED_STAGE_A" >/dev/null 2>&1); then
  die "the authorized Stage-A probe accepted a Cloudflare-proxied origin"
fi
assert_stage_a_proxy_absent "$DIRECT_STAGE_A" >/dev/null 2>&1 ||
  die "the authorized Stage-A probe rejected a genuinely direct origin"

# An ordinary direct run makes no Cloudflare claim, so a proxied response is not
# its business and must not fail it.
EDGE_PRODUCTION_DIRECT=0
assert_stage_a_proxy_absent "$PROXIED_STAGE_A" >/dev/null 2>&1 ||
  die "an ordinary direct verification started asserting Cloudflare absence"

# 6. The flag must not have become a mode selector anywhere.
if (
  GRADEX_PUBLIC_VERIFY_ALLOW_PRODUCTION_DIRECT="$EDGE_PRODUCTION_DIRECT_AUTHORIZATION" \
    parse_arguments https://gradexcourses.com >/dev/null 2>&1
); then
  die "the Stage-A flag allowed the mode to be omitted"
fi

# The guard must still be enforced in main, not merely defined.
grep --quiet --fixed-strings 'assert_mode_allowed_for_hostname "$EDGE_HOSTNAME" "$EDGE_MODE"' "$TARGET" ||
  die "main no longer applies the production-hostname guard"
grep --quiet --fixed-strings 'GRADEX_PUBLIC_VERIFY_ALLOW_PRODUCTION_DIRECT' "$TARGET" ||
  die "main no longer reads the Stage-A authorization from the environment"
grep --quiet --fixed-strings 'assert_stage_a_proxy_absent "$tls_headers"' "$TARGET" ||
  die "direct mode no longer applies the Stage-A proxy-absence check"

# Internal service ports belong to the origin. Once the public hostname is
# Cloudflare-proxied, probing alternate ports on that hostname tests
# Cloudflare's edge rather than the origin and can produce false positives.
DIRECT_MODE_BRANCH="$(sed -n '/^[[:space:]]*direct)/,/^[[:space:]]*;;/p' "$TARGET")"
CLOUDFLARE_MODE_BRANCH="$(sed -n '/^[[:space:]]*cloudflare)/,/^[[:space:]]*;;/p' "$TARGET")"

printf '%s\n' "$DIRECT_MODE_BRANCH" |
  grep --quiet --fixed-strings 'assert_internal_ports_closed "$EDGE_HOSTNAME"' ||
  die "direct mode no longer checks the origin-facing internal ports"

if printf '%s\n' "$CLOUDFLARE_MODE_BRANCH" |
  grep --quiet --fixed-strings 'assert_internal_ports_closed "$EDGE_HOSTNAME"'; then
  die "cloudflare mode incorrectly probes internal ports on the proxied hostname"
fi

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
