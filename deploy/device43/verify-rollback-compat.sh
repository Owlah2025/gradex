#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BASE=3383f46d0e9e6379c3bd166d39622c3659ae3d86
ROLLBACK_COMMIT=54115fd6029d5d80af63640ac6f0bfe31be22d67
PATCH_SHA256=9bd3e47288efbd8c78081dd2694ce94dea4fa3ba0c2525c460be6966266996f7
PATCH="$ROOT/deploy/device43/rollback-compat.patch"
SCRATCH="$(mktemp -d)"
trap 'rm -rf -- "$SCRATCH"' EXIT

printf '%s  %s\n' "$PATCH_SHA256" "$PATCH" | sha256sum --check --status
git -C "$ROOT" cat-file -e "$BASE^{commit}"
git -C "$ROOT" archive "$BASE" | tar -xf - -C "$SCRATCH"
(cd "$SCRATCH" && git apply "$PATCH")
cmp "$ROOT/backend/internal/db/migrations/0043_auto_device_replacement.up.sql" \
    "$SCRATCH/backend/internal/db/migrations/0043_auto_device_replacement.up.sql"
cmp "$ROOT/backend/internal/db/migrations/0043_auto_device_replacement.down.sql" \
    "$SCRATCH/backend/internal/db/migrations/0043_auto_device_replacement.down.sql"

(cd "$SCRATCH/backend" && go test ./internal/db ./internal/identity ./cmd/api)
(cd "$SCRATCH/backend" && go build -tags integration -o "$SCRATCH/rollback-probe" ./cmd/schema43-rollback-probe)
(cd "$ROOT/backend" && GRADEX_SCHEMA43_ROLLBACK_PROBE="$SCRATCH/rollback-probe" \
    go test -tags integration ./internal/identity \
      -run '^TestSchema43RollbackApplicationAfterAutomaticRotation$' -count=1 -v -timeout 120s)
printf 'schema43 rollback compatibility PASS: base=%s rollback_commit=%s patch_sha256=%s\n' \
    "$BASE" "$ROLLBACK_COMMIT" "$PATCH_SHA256"
