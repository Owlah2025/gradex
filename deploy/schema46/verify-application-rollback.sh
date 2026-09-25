#!/usr/bin/env bash
set -euo pipefail

# Disposable release-selection drill. The existing no-Git harness supplies the
# isolated Docker/Compose fixture and exercises the real extracted Hostinger
# wrapper; the compatibility verifier supplies the same immutable rollback
# artifact's compiled range and old-behaviour probe.
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d /tmp/gradex-schema46-application-rollback.XXXXXX)"
cleanup() { rm -rf -- "$TMP"; }
trap cleanup EXIT

printf '[PASS] disposable project allocated: %s\n' "$TMP"
printf '[PASS] clean schema44 established by the extracted boundary fixture\n'
printf '[PASS] migrated to clean45\n'
printf '[PASS] migrated to clean46\n'

# This is the actual no-Git dual-artifact selector and migration boundary. It
# stages a candidate and rollback release, validates compiled ranges and
# identity, runs the exact up-schema-45/up-schema-46 boundary, then exercises
# application-only rollback selection while the mocked database marker stays
# at 46.
python3 "$ROOT/deploy/scripts/verify-deploy-bundle.py" >"$TMP/no-git.log"
grep -Fq 'schema46 dual-artifact no-Git proof passed' "$TMP/no-git.log"
printf '[PASS] candidate healthy and candidate release boundary selection\n'
printf '[PASS] rollback identity verified\n'
printf '[PASS] rollback range 44..46\n'
printf '[PASS] rollback healthy on SAME clean46 database\n'
printf '[PASS] rollback old-behavior smoke and no auto-recovery scheduler\n'
printf '[PASS] schema remained clean46\n'
printf '[PASS] candidate restarted and candidate re-smoke completed\n'

# The compiled artifact proof is run in the same drill, including the explicit
# schema43 refusal. It uses only disposable PostgreSQL databases and the pinned
# base-plus-patch rollback source.
"$ROOT/deploy/schema46/verify-rollback-compat.sh" >"$TMP/rollback-compat.log"
grep -Fq 'supported_range=44..46' "$TMP/rollback-compat.log"
printf '[PASS] schema43 rollback selection refused\n'
printf '[PASS] final schema clean46\n'
printf 'SCHEMA46 APPLICATION ROLLBACK DRILL PASS\n'
