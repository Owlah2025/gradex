#!/usr/bin/env bash

set -euo pipefail

note() { printf 's12-public-verify: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

[ "$#" = 1 ] || die "usage: $0 https://staging.example.com"
origin="${1%/}"
[[ "$origin" =~ ^https://[A-Za-z0-9.-]+$ ]] || die "origin must be a credential-free HTTPS origin"
hostname="${origin#https://}"

for tool in curl getent jq mktemp openssl; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
done

getent ahosts "$hostname" >/dev/null || die "public DNS did not resolve"
redirect_headers="$(mktemp)"
tls_headers="$(mktemp)"
trap 'rm -f -- "$redirect_headers" "$tls_headers"' EXIT

curl --silent --show-error --max-time 15 --output /dev/null --dump-header "$redirect_headers" "http://$hostname/"
grep --extended-regexp --ignore-case --quiet '^location: https://' "$redirect_headers" ||
  die "HTTP did not redirect intentionally to HTTPS"

curl --fail --silent --show-error --max-time 15 --output /dev/null --dump-header "$tls_headers" "$origin/"
curl --fail --silent --show-error --max-time 15 "$origin/healthz" |
  jq --exit-status '.status == "ok"' >/dev/null
curl --fail --silent --show-error --max-time 15 "$origin/readyz" |
  jq --exit-status '.status == "ok" and .checks.postgres == "ok" and .checks.redis == "ok" and .checks.schema == "ok"' >/dev/null
openssl s_client -connect "$hostname:443" -servername "$hostname" </dev/null 2>/dev/null |
  openssl x509 -noout -checkend 86400 >/dev/null || die "public certificate is invalid or expires within 24 hours"

grep --extended-regexp --ignore-case --quiet '^cf-ray:' "$tls_headers" || die "Cloudflare proxy response header is absent"
if grep --extended-regexp --ignore-case --quiet '^cf-cache-status:[[:space:]]*HIT' "$tls_headers"; then
  die "Cloudflare cached the frontend unexpectedly during the conservative staging rollout"
fi
note "public DNS, Cloudflare edge, HTTPS certificate, frontend, health, and readiness passed"
