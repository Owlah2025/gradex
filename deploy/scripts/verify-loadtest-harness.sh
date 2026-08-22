#!/usr/bin/env bash

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_LOADTEST="$S12_ROOT/deploy/loadtest"

note() { printf 'loadtest-harness: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

assert_contains() {
  grep --quiet --fixed-strings "$2" "$1" || die "$(basename "$1") is missing: $2"
}

main() {
  local file
  for file in loadtest.mjs prepare-fixtures.sh run.sh capture-server.sh README.md; do
    [ -f "$S12_LOADTEST/$file" ] || die "$file is absent"
  done
  bash -n "$S12_LOADTEST/prepare-fixtures.sh" "$S12_LOADTEST/run.sh" "$S12_LOADTEST/capture-server.sh"
  if command -v node >/dev/null 2>&1; then
    node --check "$S12_LOADTEST/loadtest.mjs"
  else
    note "node is unavailable; JavaScript syntax check skipped"
  fi

  assert_contains "$S12_LOADTEST/loadtest.mjs" 'apiRequests: (PROFILE_API_RATE || 250) * 60'
  assert_contains "$S12_LOADTEST/loadtest.mjs" 'apiRate: PROFILE_API_RATE || 250'
  assert_contains "$S12_LOADTEST/loadtest.mjs" 'logins: PROFILE_LOGIN_COUNT || 500'
  assert_contains "$S12_LOADTEST/loadtest.mjs" 'executor: "constant-arrival-rate", rate: expected.logins'
  assert_contains "$S12_LOADTEST/loadtest.mjs" 'const index = exec.scenario.iterationInTest;'
  assert_contains "$S12_LOADTEST/loadtest.mjs" 'canonical_acceptance_run: !ENVELOPE_PROFILE'
  assert_contains "$S12_LOADTEST/loadtest.mjs" 'must be below the canonical threshold'
  assert_contains "$S12_LOADTEST/loadtest.mjs" 'timeUnit: `${expected.durationSeconds}s`, duration: `${expected.durationSeconds * 1000 - 1}ms`'
  assert_contains "$S12_LOADTEST/loadtest.mjs" 'thresholds.gradex_application_requests = [`count==${expected.apiRequests}`]'
  assert_contains "$S12_LOADTEST/loadtest.mjs" 'thresholds.gradex_login_attempts = [`count==${expected.logins}`]'
  assert_contains "$S12_LOADTEST/loadtest.mjs" 'thresholds.gradex_playback_attempts = [`count==${expected.playbacks}`]'
  assert_contains "$S12_LOADTEST/loadtest.mjs" 'name === "login" || name === "login_bootstrap" ? "60s" : "10s"'
  assert_contains "$S12_LOADTEST/loadtest.mjs" 'gradex_rate_limited'
  assert_contains "$S12_LOADTEST/loadtest.mjs" 'result.lg019_blocker'
  assert_contains "$S12_LOADTEST/loadtest.mjs" 'count("gradex_application_requests") / expected.durationSeconds'
  assert_contains "$S12_LOADTEST/loadtest.mjs" 'body.manifest_url.startsWith("/api/v1/media/playback-manifests/")'
  assert_contains "$S12_LOADTEST/loadtest.mjs" 'Signed segment URLs are deliberately neither followed nor recorded.'
  assert_contains "$S12_LOADTEST/run.sh" 'GRADEX_LOADTEST_ALLOW_REMOTE=I_UNDERSTAND_THIS_GENERATES_REMOTE_LOAD'
  assert_contains "$S12_LOADTEST/run.sh" 'GRADEX_LOADTEST_EXTERNAL_GENERATOR'
  assert_contains "$S12_LOADTEST/run.sh" 'sessions.json must have mode 0600'
  [ "$(grep --count --fixed-strings 'for service in postgres redis minio api worker frontend edge; do' \
    "$S12_LOADTEST/capture-server.sh")" = 2 ] ||
    die "server capture must sample and retain final state for every running target service"

  if grep --quiet --extended-regexp '/(healthz|readyz)' "$S12_LOADTEST/loadtest.mjs"; then
    die "health/readiness probes entered measured traffic"
  fi
  if grep --ignore-case --extended-regexp \
    '(insecureSkipTLSVerify|noTLSVerify|expected.*429|429.*success|http://gradex|https://gradex)' \
    "$S12_LOADTEST/loadtest.mjs" "$S12_LOADTEST/run.sh"; then
    die "harness weakens TLS, treats rate limiting as success, or embeds a target"
  fi
  if grep --extended-regexp 'while[[:space:]]+true|for[[:space:]]*\(;;\)' \
    "$S12_LOADTEST"/*.sh "$S12_LOADTEST/loadtest.mjs"; then
    die "harness contains an unlimited loop"
  fi
  if grep --quiet --extended-regexp \
    '(cookie_value|csrf_token|password).*result|result.*(cookie_value|csrf_token|password)' \
    "$S12_LOADTEST/loadtest.mjs"; then
    die "machine-readable results may contain credentials"
  fi

  note "syntax, fail-closed targeting, approved pacing, real routes, secret isolation, and bounded execution passed"
}

main "$@"
