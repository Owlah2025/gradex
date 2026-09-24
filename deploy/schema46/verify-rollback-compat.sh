#!/usr/bin/env bash
#
# verify-rollback-compat.sh — prove the schema-46-compatible old-behaviour
# application artifact is reproducible and actually serves schema 46.
#
# WHY THIS EXISTS
#
#   Production runs 0fee657897c939cb679c9d804d184542bb2f692f with
#   MaxSchemaVersion 44. Once 0045 and 0046 are applied that exact binary refuses
#   to start, because its supported range ends at 44 — so the release would have
#   no application rollback at all, and the only way back would be a destructive
#   database downgrade.
#
#   There is no generic production DOWN, and no supervised 46 -> 44 command. The
#   rollback path is an application rollback onto an already-migrated database,
#   which requires an artifact whose BEHAVIOUR is 0fee657's and whose
#   compatibility ceiling is 46.
#
# WHAT THE ARTIFACT IS, EXACTLY
#
#   0fee657 behaviour + a schema-46 compatibility patch. It is NOT the original
#   0fee657 artifact and must never be labelled as one. The patch:
#
#     * adds the 0045 and 0046 migration files, byte-identical to the current
#       tree, so the migrator source matches the database it will serve;
#     * raises MaxSchemaVersion to 46 with the two named constants;
#     * adds a probe that exercises old behaviour against schema 46.
#
#   It deliberately does NOT raise requiredSchemaVersion to 44. That correction
#   belongs to the forward release; cherry-picking it would make this something
#   other than 0fee657 behaviour, and it cannot matter for a rollback target,
#   because the database being rolled back onto is at 46 and satisfies either
#   floor.
#
# WHAT THIS IS NOT
#
#   It is not a substitute for fresh schema-44 backup and proven restore
#   evidence. That remains mandatory before any production deployment of this
#   release. This script proves an application rollback is possible; it proves
#   nothing about data recovery.
#
# USAGE
#   deploy/schema46/verify-rollback-compat.sh
#
#   Requires a local PostgreSQL reachable at the DSN below and a Go toolchain.
#   It creates and drops one disposable database and touches nothing else.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# The running production revision this artifact's behaviour is derived from.
BASE=0fee657897c939cb679c9d804d184542bb2f692f
PATCH="$ROOT/deploy/schema46/rollback-compat.patch"
PATCH_SHA256=290ececcd67842996ca5007f8c0d101f9a9dbe972c6479f230fb883505093f4b

ADMIN_DSN="postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable"
PROBE_DB=gradex_schema46_rollback_compat
PROBE_DSN="postgres://gradex:gradex@localhost:5432/${PROBE_DB}?sslmode=disable"

SCRATCH="$(mktemp -d)"
cleanup() {
    psql "$ADMIN_DSN" -q \
        -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='${PROBE_DB}'" \
        -c "DROP DATABASE IF EXISTS ${PROBE_DB}" >/dev/null 2>&1 || true
    rm -rf -- "$SCRATCH"
}
trap cleanup EXIT

# 1. The patch is the one that was reviewed. An artifact built from an unpinned
#    patch is not an auditable artifact.
printf '%s  %s\n' "$PATCH_SHA256" "$PATCH" | sha256sum --check --status
echo "patch checksum: ok"

# 2. The base commit is the running production revision, present in this history.
git -C "$ROOT" cat-file -e "${BASE}^{commit}"
echo "base commit: ${BASE} present"

# 3. Reproduce the artifact tree.
git -C "$ROOT" archive "$BASE" | tar -xf - -C "$SCRATCH"
(cd "$SCRATCH" && git apply "$PATCH")
echo "artifact tree: reproduced from ${BASE}"

# 4. The migration SQL the artifact carries must be byte-identical to the current
#    tree's. If it drifted, the rollback target would migrate or validate against
#    different SQL than the release that created the database.
for migration in \
    0045_auto_enhancement_recovery.up.sql \
    0045_auto_enhancement_recovery.down.sql \
    0046_lesson_public_preview.up.sql \
    0046_lesson_public_preview.down.sql
do
    cmp "$ROOT/backend/internal/db/migrations/$migration" \
        "$SCRATCH/backend/internal/db/migrations/$migration"
done
echo "migration SQL: identical to the current tree"

# 5. The artifact's own tests. This is where the ceiling and the deliberately
#    unchanged API floor are asserted.
(cd "$SCRATCH/backend" && go build ./...)
(cd "$SCRATCH/backend" && go test ./internal/db ./cmd/api -count=1)
echo "artifact tests: pass"

# 6. A disposable database migrated to 46 by the CURRENT release's migrations —
#    the same thing production would be left holding after the release.
psql "$ADMIN_DSN" -q \
    -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='${PROBE_DB}'" \
    -c "DROP DATABASE IF EXISTS ${PROBE_DB}" \
    -c "CREATE DATABASE ${PROBE_DB}" >/dev/null
(cd "$ROOT/backend" && go run -tags=integration ./cmd/migrate-to-version "file://internal/db/migrations" "$PROBE_DSN" 46)
echo "probe database: migrated to schema 46 by the current release"

# 7. Old behaviour against that database. Startup/readiness for both processes,
#    the previous build's statement shapes against every changed relation, the
#    legacy course preview, the current direct purchase grant, and the negative:
#    it writes no scheduler row and marks no Lesson previewable.
(cd "$SCRATCH/backend" && GRADEX_COMPAT46_DSN="$PROBE_DSN" \
    go run -tags=integration ./cmd/schema46-rollback-probe)

printf 'schema46 rollback compatibility PASS: base=%s patch_sha256=%s ceiling=46 api_floor=43(unchanged)\n' \
    "$BASE" "$PATCH_SHA256"
