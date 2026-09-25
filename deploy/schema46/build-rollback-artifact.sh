#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: %s OUTPUT_PARENT\n' "$0" >&2
  exit 2
}

[ "$#" = 1 ] || usage

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=../hostinger/release-closure.sh
. "$ROOT/deploy/hostinger/release-closure.sh"
OUTPUT_PARENT="$1"
BASE=0fee657897c939cb679c9d804d184542bb2f692f
PATCH="$ROOT/deploy/schema46/rollback-compat.patch"
PATCH_SHA256="$(sha256sum "$PATCH" | awk '{print $1}')"
# The identity is derived from the immutable base and patch, never presented as
# either one. Docker's revision label and the staged manifest use this value.
ROLLBACK_SHA="$(printf '%s\n%s\n' "$BASE" "$PATCH_SHA256" | sha256sum | awk '{print substr($1,1,40)}')"
SHORT="${ROLLBACK_SHA:0:12}"
BACKEND="gradex-backend:schema46-rollback-$SHORT"
FRONTEND="gradex-frontend:schema46-rollback-$SHORT"
PROOF="gradex-backend-proof:schema46-rollback-$SHORT"
ARTIFACT="$OUTPUT_PARENT/$ROLLBACK_SHA"
SCRATCH="$(mktemp -d)"
TOOLING="$SCRATCH/tooling"

cleanup() { rm -rf -- "$SCRATCH"; }
trap cleanup EXIT

[ ! -e "$ARTIFACT" ] || { printf 'schema46 rollback artifact already exists: %s\n' "$ARTIFACT" >&2; exit 1; }
git -C "$ROOT" cat-file -e "${BASE}^{commit}"
git -C "$ROOT" archive "$BASE" | tar -xf - -C "$SCRATCH"
(cd "$SCRATCH" && git apply "$PATCH")

git -C "$ROOT" diff --quiet -- "$PATCH" || true
mkdir -p "$ARTIFACT"
tar -C "$SCRATCH/backend" -cf - . | docker build --build-arg "GRADEX_REVISION=$ROLLBACK_SHA" --tag "$BACKEND" -
tar -C "$SCRATCH/backend" -cf - . | docker build --target proof --build-arg "GRADEX_REVISION=$ROLLBACK_SHA" --tag "$PROOF" -
tar -C "$SCRATCH/frontend" -cf - . | docker build --build-arg "GRADEX_REVISION=$ROLLBACK_SHA" --tag "$FRONTEND" -

for image in "$BACKEND" "$FRONTEND" "$PROOF"; do
  [ "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")" = "$ROLLBACK_SHA" ]
done
[ "$(docker run --rm --network none --entrypoint gradex-migrate "$BACKEND" schema-range)" = '44 46' ]

umask 077
mkdir -p "$TOOLING"
# The canonical release tooling closure, identical in shape to the forward
# candidate's. This used to be `cp -R "$ROOT/deploy/hostinger" "$TOOLING/deploy"`,
# which flattened `deploy/hostinger/*` onto `deploy/*` and produced an artifact
# `import-release.sh` could not import: it sources
# `tooling/deploy/hostinger/release-artifact.sh` out of the artifact itself. The
# schema46 production release hit exactly that and had to stage the rollback
# artifact by hand. One list, one layout, asserted before the bundle is sealed.
stage_release_tooling "$ROOT" "$TOOLING"
assert_release_tooling_importable "$TOOLING"
printf 'RELEASE_SHA=%s\nDEPLOY_BUNDLE_FORMAT=1\nSCHEMA46_CAPABILITY=auto-enhancement-lesson-preview-v1\n' "$ROLLBACK_SHA" >"$TOOLING/release-tooling.env"
(cd "$TOOLING" && find . -type f ! -name tooling.sha256 -print0 | sort -z | xargs -0 sha256sum >tooling.sha256)
tar -czf "$ARTIFACT/deploy-bundle.tar.gz" -C "$TOOLING" --transform='s,^./,,' .
(cd "$ARTIFACT" && sha256sum deploy-bundle.tar.gz >deploy-bundle.tar.gz.sha256)
BUNDLE_SHA256="$(cut -d' ' -f1 "$ARTIFACT/deploy-bundle.tar.gz.sha256")"
{
  printf 'GRADEX_RELEASE_SHA=%s\n' "$ROLLBACK_SHA"
  printf 'GRADEX_SCHEMA46_ROLLBACK_BASE_SHA=%s\n' "$BASE"
  printf 'GRADEX_SCHEMA46_ROLLBACK_PATCH_SHA256=%s\n' "$PATCH_SHA256"
  printf 'GRADEX_SCHEMA_MIN_VERSION=44\nGRADEX_SCHEMA_MAX_VERSION=46\n'
  printf 'GRADEX_BACKEND_IMAGE=%s\nGRADEX_FRONTEND_IMAGE=%s\nGRADEX_PROOF_IMAGE=%s\n' "$BACKEND" "$FRONTEND" "$PROOF"
  printf 'GRADEX_BACKEND_IMAGE_ID=%s\n' "$(docker image inspect --format '{{.Id}}' "$BACKEND")"
  printf 'GRADEX_FRONTEND_IMAGE_ID=%s\n' "$(docker image inspect --format '{{.Id}}' "$FRONTEND")"
  printf 'GRADEX_PROOF_IMAGE_ID=%s\n' "$(docker image inspect --format '{{.Id}}' "$PROOF")"
  printf 'GRADEX_DEPLOY_BUNDLE_SHA256=%s\n' "$BUNDLE_SHA256"
} >"$ARTIFACT/release.env"
(cd "$ARTIFACT" && sha256sum release.env >release.env.sha256)
cp "$PATCH" "$ARTIFACT/rollback-compat.patch"
(cd "$ARTIFACT" && sha256sum rollback-compat.patch >schema46-rollback-compat.sha256)
docker save "$BACKEND" "$FRONTEND" "$PROOF" | gzip --best >"$ARTIFACT/images.tar.gz"
(cd "$ARTIFACT" && sha256sum images.tar.gz >images.tar.gz.sha256)
printf 'schema46 rollback artifact built: release=%s base=%s patch=%s path=%s\n' "$ROLLBACK_SHA" "$BASE" "$PATCH_SHA256" "$ARTIFACT"
