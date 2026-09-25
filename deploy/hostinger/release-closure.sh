#!/usr/bin/env bash
# The canonical release tooling closure, shared by every artifact builder.
#
# A release artifact carries exactly one shape of tooling tree, and both the
# forward candidate builder (`release.sh`) and the schema46 rollback-compatible
# builder (`deploy/schema46/build-rollback-artifact.sh`) stage it from this one
# list. That is not tidiness: `import-release.sh` sources
# `tooling/deploy/hostinger/release-artifact.sh` out of the artifact it is
# importing, so an artifact whose tooling sits at any other path cannot be
# imported at all. The schema46 production release discovered that the hard way
# — the rollback artifact had been staged with `cp -R deploy/hostinger
# "$TOOLING/deploy"`, which flattens `deploy/hostinger/*` onto `deploy/*`, and
# the importer failed with `tooling/deploy/hostinger/release-artifact.sh: No
# such file or directory`. Staging had to be done by hand.
#
# Paths are repository-relative and are reproduced verbatim inside the bundle,
# so `deploy/hostinger/host.sh` in this list is `tooling/deploy/hostinger/host.sh`
# in an imported release.
#
# This file is build-side only. It is deliberately NOT part of the closure it
# describes: an imported artifact never rebuilds another artifact.
#
# Explicit runtime closure: host.sh sources backup/artifact helpers, invokes the
# monitor, prepares TLS/CORS, and resolves Compose's Caddy bind beside itself.
# The migration SQL is present so the host can bind a candidate image's
# migrations to bytes it can checksum without a Git checkout.
RELEASE_TOOLING_PATHS=(
  deploy/hostinger/host.sh
  deploy/hostinger/compose.yml
  deploy/hostinger/Caddyfile
  deploy/hostinger/backup-restic.sh
  deploy/hostinger/release-artifact.sh
  deploy/hostinger/r2-cors.json.template
  deploy/compose/redis-server.ext
  deploy/monitoring/monitor-once.sh
  deploy/scripts/verify-schema-41-rollback.sh
  deploy/scripts/verify-schema-42-rollback.sh
  backend/internal/db/migrations/0041_enhancement_recovery_foundation.up.sql
  backend/internal/db/migrations/0041_enhancement_recovery_foundation.down.sql
  backend/internal/db/migrations/0042_active_processing_attempt_kind.up.sql
  backend/internal/db/migrations/0042_active_processing_attempt_kind.down.sql
  backend/internal/db/migrations/0045_auto_enhancement_recovery.up.sql
  backend/internal/db/migrations/0045_auto_enhancement_recovery.down.sql
  backend/internal/db/migrations/0046_lesson_public_preview.up.sql
  backend/internal/db/migrations/0046_lesson_public_preview.down.sql
)

# The one file inside a staged tooling tree that `import-release.sh` sources.
# Builders assert it is present at exactly this path before they seal a bundle,
# so an artifact that cannot be imported is never produced in the first place.
RELEASE_TOOLING_IMPORT_ENTRYPOINT=deploy/hostinger/release-artifact.sh

# stage_release_tooling SOURCE_ROOT DESTINATION
#
# Copies the closure out of a plain directory tree, preserving repository
# relative paths. `release.sh` uses `git archive` instead because it stages from
# an immutable commit; this is for builders whose source is a working tree that
# has already been reproduced on disk.
stage_release_tooling() {
  local source_root="$1" destination="$2" path
  for path in "${RELEASE_TOOLING_PATHS[@]}"; do
    [ -f "$source_root/$path" ] || {
      printf 'release-closure: missing closure member %s\n' "$path" >&2
      return 1
    }
    mkdir -p "$destination/$(dirname "$path")"
    cp "$source_root/$path" "$destination/$path"
  done
}

# assert_release_tooling_importable DESTINATION
#
# The packaging invariant the production release proved was worth enforcing at
# build time rather than discovering on the host.
assert_release_tooling_importable() {
  local destination="$1"
  [ -f "$destination/$RELEASE_TOOLING_IMPORT_ENTRYPOINT" ] || {
    printf 'release-closure: staged tooling is not importable: %s is absent\n' \
      "$RELEASE_TOOLING_IMPORT_ENTRYPOINT" >&2
    return 1
  }
}
