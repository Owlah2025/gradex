#!/usr/bin/env bash
#
# verify-rollback-artifact-import.sh — prove a genuinely built schema46 rollback
# artifact can be imported by the ordinary release importer.
#
# WHY THIS EXISTS
#
#   The schema46 production release could not import its rollback artifact.
#   `build-rollback-artifact.sh` staged host tooling with
#
#       cp -R "$ROOT/deploy/hostinger" "$TOOLING/deploy"
#
#   which flattens `deploy/hostinger/*` onto `deploy/*`, while
#   `import-release.sh` sources `tooling/deploy/hostinger/release-artifact.sh`
#   out of the artifact it is importing. The importer failed with
#
#       bash: line 39: tooling/deploy/hostinger/release-artifact.sh: No such file or directory
#
#   and the artifact had to be staged on the production host by a hand-written
#   mirror of the importer's verification logic. That workaround was verified and
#   safe, and it must never be needed again.
#
#   The existing no-Git fixture did not catch it, because it staged the rollback
#   bundle by copying the CANDIDATE's tooling tree — so it tested a layout the
#   rollback builder never produces. That fixture now stages through the shared
#   `stage_release_tooling` and imports with the real importer, which closes the
#   gap for packaging and import. This script closes the remaining distance: it
#   runs the actual builder, with real Docker and real images, and feeds its
#   exact output to the real importer with nothing rearranged in between.
#
# WHAT IT PROVES
#
#   build-rollback-artifact.sh
#     -> import-release.sh, unmodified, on the artifact exactly as built
#     -> bundle inventory, extracted-tooling checksums and manifest bindings
#     -> image identity and OCI revision labels
#     -> compiled schema range 44..46 queried from the imported image
#     -> rollback identity distinct from both the base and the forward candidate
#
# WHAT IT IS NOT
#
#   It is not production evidence and it never contacts production. It creates a
#   disposable host-state directory and a temporary artifact tree, both removed
#   on exit. It loads real images into the local Docker daemon, which is the
#   point: the importer's `docker load` and label checks must run for real.
#
# USAGE
#   deploy/schema46/verify-rollback-artifact-import.sh
#
#   Requires Docker and roughly 10 minutes on a cold cache: it builds the
#   rollback backend, proof and frontend images from the patched base tree.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BASE=0fee657897c939cb679c9d804d184542bb2f692f
CANDIDATE="$(git -C "$ROOT" rev-parse HEAD)"

SCRATCH="$(mktemp -d /tmp/gradex-rollback-import.XXXXXX)"
# import-release.sh requires a dedicated Gradex state directory by name.
STATE="$SCRATCH/gradex-rollback-import"
ARTIFACTS="$SCRATCH/artifacts"

cleanup() { chmod -R u+w "$SCRATCH" 2>/dev/null || true; rm -rf -- "$SCRATCH"; }
trap cleanup EXIT

note() { printf 'schema46-import: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }
pass() { printf '[PASS] %s\n' "$*"; }

command -v docker >/dev/null 2>&1 || die "docker is required"
mkdir -p "$STATE" "$ARTIFACTS"

note "building the rollback artifact with the real builder"
"$ROOT/deploy/schema46/build-rollback-artifact.sh" "$ARTIFACTS" >&2

ROLLBACK_SHA="$(find "$ARTIFACTS" -mindepth 1 -maxdepth 1 -type d -printf '%f\n')"
[[ "$ROLLBACK_SHA" =~ ^[0-9a-f]{40}$ ]] || die "the builder did not produce one release directory"
ARTIFACT="$ARTIFACTS/$ROLLBACK_SHA"
pass "builder produced rollback artifact $ROLLBACK_SHA"

[ "$ROLLBACK_SHA" != "$BASE" ] || die "rollback identity collides with its old-behaviour base"
[ "$ROLLBACK_SHA" != "$CANDIDATE" ] || die "rollback identity collides with the forward candidate"
pass "rollback identity is distinct from both $BASE and $CANDIDATE"

# The exact defect, asserted on the built bytes before the importer ever runs.
tar -tzf "$ARTIFACT/deploy-bundle.tar.gz" |
  grep -qx 'deploy/hostinger/release-artifact.sh' ||
  die "bundle does not carry deploy/hostinger/release-artifact.sh; the importer cannot source its own tooling"
pass "bundle exposes the importer entrypoint at the canonical path"

# Hand the importer the artifact exactly as built. No rearrangement, no copying
# of candidate tooling, no host-side fallback.
note "importing with the unmodified release importer"
mkdir -p "$STATE/incoming/$ROLLBACK_SHA"
cp "$ARTIFACT"/release.env "$ARTIFACT"/images.tar.gz "$ARTIFACT"/images.tar.gz.sha256 \
  "$ARTIFACT"/deploy-bundle.tar.gz "$ARTIFACT"/deploy-bundle.tar.gz.sha256 \
  "$STATE/incoming/$ROLLBACK_SHA/"
GRADEX_HOST_STATE_DIR="$STATE" bash "$ROOT/deploy/hostinger/import-release.sh" "$ROLLBACK_SHA" >&2 ||
  die "the standard importer could not consume the built rollback artifact"
pass "import-release.sh imported the artifact with no manual rearrangement"

IMPORTED="$STATE/releases/$ROLLBACK_SHA"
[ -f "$IMPORTED/tooling/deploy/hostinger/release-artifact.sh" ] ||
  die "imported tooling is not at the canonical path"
[ -f "$IMPORTED/tooling/deploy/hostinger/host.sh" ] || die "imported tooling lacks host.sh"
pass "imported tooling tree matches the canonical release layout"

# Immutability, as the importer leaves it.
[ -z "$(find "$IMPORTED" -type f -perm -u+w -print -quit)" ] ||
  die "imported artifact is writable"
pass "imported artifact is read-only"

# The identity and range guarantees the boundary will later re-verify.
manifest_value() { sed -n "s/^$2=//p" "$1"; }
[ "$(manifest_value "$IMPORTED/release.env" GRADEX_RELEASE_SHA)" = "$ROLLBACK_SHA" ] ||
  die "imported manifest names a different release"
[ "$(manifest_value "$IMPORTED/release.env" GRADEX_SCHEMA46_ROLLBACK_BASE_SHA)" = "$BASE" ] ||
  die "imported manifest names an unexpected old-behaviour base"
[ "$(manifest_value "$IMPORTED/release.env" GRADEX_SCHEMA_MIN_VERSION)" = 44 ] ||
  die "imported manifest declares an unexpected schema minimum"
[ "$(manifest_value "$IMPORTED/release.env" GRADEX_SCHEMA_MAX_VERSION)" = 46 ] ||
  die "imported manifest declares an unexpected schema maximum"
pass "imported manifest identity and declared range are intact"

ROLLBACK_BACKEND="$(manifest_value "$IMPORTED/release.env" GRADEX_BACKEND_IMAGE)"
[ "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$ROLLBACK_BACKEND")" = "$ROLLBACK_SHA" ] ||
  die "imported backend image revision label disagrees with the release identity"
[ "$(docker run --rm --network none --entrypoint gradex-migrate "$ROLLBACK_BACKEND" schema-range)" = '44 46' ] ||
  die "imported backend image does not compile the 44..46 range"
pass "imported backend image reports compiled schema range 44 46"

# The patch evidence the schema46 boundary requires, produced by the builder and
# carried beside the manifest.
[ -f "$ARTIFACT/schema46-rollback-compat.sha256" ] || die "builder produced no patch evidence"
[ "$(awk '{print $1}' "$ARTIFACT/schema46-rollback-compat.sha256")" = \
  "$(manifest_value "$ARTIFACT/release.env" GRADEX_SCHEMA46_ROLLBACK_PATCH_SHA256)" ] ||
  die "patch evidence disagrees with the manifest"
pass "rollback patch evidence agrees with the manifest"

note "schema46 rollback artifact imports through the standard release flow"
