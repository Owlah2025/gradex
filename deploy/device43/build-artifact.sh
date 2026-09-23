#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: %s SOURCE_WORKTREE {current|rollback} OUTPUT_PARENT\n' "$0" >&2
  exit 2
}
[ "$#" = 3 ] || usage
SOURCE="$(cd "$1" && pwd)"
ROLE="$2"
OUTPUT_PARENT="$3"
case "$ROLE" in current|rollback) ;; *) usage ;; esac
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
[ -z "$(git -C "$SOURCE" status --porcelain=v1)" ] || {
  printf 'device43 artifact: source worktree must be clean\n' >&2
  exit 1
}
REVISION="$(git -C "$SOURCE" rev-parse HEAD)"
[[ "$REVISION" =~ ^[0-9a-f]{40}$ ]]
if [ "$ROLE" = rollback ]; then
  [ "$REVISION" = 54115fd6029d5d80af63640ac6f0bfe31be22d67 ] || {
    printf 'device43 artifact: rollback source is not the tested compatibility commit\n' >&2
    exit 1
  }
  git -C "$SOURCE" diff --binary \
    3383f46d0e9e6379c3bd166d39622c3659ae3d86.."$REVISION" |
    cmp -s - "$SCRIPT_DIR/rollback-compat.patch" || {
      printf 'device43 artifact: rollback source differs from the reviewed patch\n' >&2
      exit 1
    }
fi
git -C "$SOURCE" merge-base --is-ancestor \
  3383f46d0e9e6379c3bd166d39622c3659ae3d86 "$REVISION"
[ -z "$(git -C "$SOURCE" diff --name-only \
  3383f46d0e9e6379c3bd166d39622c3659ae3d86.."$REVISION" -- \
  backend/internal/media backend/internal/playback \
  'backend/internal/db/migrations/0039*' 'backend/internal/db/migrations/0040*' \
  'backend/internal/db/migrations/0041*' 'backend/internal/db/migrations/0042*')" ] || {
  printf 'device43 artifact: frozen media files changed\n' >&2
  exit 1
}

SHORT="${REVISION:0:12}"
BACKEND="gradex-backend:device43-$SHORT"
FRONTEND="gradex-frontend:device43-$SHORT"
PROOF="gradex-backend-proof:device43-$SHORT"
mkdir -p "$OUTPUT_PARENT"
ARTIFACT="$OUTPUT_PARENT/$REVISION"
[ ! -e "$ARTIFACT" ] || {
  printf 'device43 artifact: output already exists\n' >&2
  exit 1
}
mkdir "$ARTIFACT"
git -C "$SOURCE" archive "$REVISION:backend" |
  docker build --build-arg "GRADEX_REVISION=$REVISION" --tag "$BACKEND" -
git -C "$SOURCE" archive "$REVISION:backend" |
  docker build --target proof --build-arg "GRADEX_REVISION=$REVISION" --tag "$PROOF" -
git -C "$SOURCE" archive "$REVISION:frontend" |
  docker build --build-arg "GRADEX_REVISION=$REVISION" --tag "$FRONTEND" -

[ "$(docker run --rm --network none --entrypoint gradex-migrate "$BACKEND" max-version)" = 43 ] || {
  printf 'device43 artifact: backend schema ceiling is not 43\n' >&2
  exit 1
}
if [ "$ROLE" = current ]; then
  docker run --rm --network none --entrypoint test "$BACKEND" \
    -x /usr/local/bin/gradex-device-cutover
fi
for IMAGE in "$BACKEND" "$FRONTEND" "$PROOF"; do
  [ "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$IMAGE")" = "$REVISION" ]
done
for DIRECTION in up down; do
  FILE="0043_auto_device_replacement.$DIRECTION.sql"
  EXPECTED="$(sha256sum "$SOURCE/backend/internal/db/migrations/$FILE")"
  ACTUAL="$(docker run --rm --network none --entrypoint sha256sum "$BACKEND" "internal/db/migrations/$FILE")"
  [ "${EXPECTED%% *}" = "${ACTUAL%% *}" ] || {
    printf 'device43 artifact: migration %s differs from image\n' "$FILE" >&2
    exit 1
  }
done
umask 077
{
  printf 'GRADEX_RELEASE_SHA=%s\n' "$REVISION"
  printf 'GRADEX_DEVICE43_ROLE=%s\n' "$ROLE"
  printf 'GRADEX_SCHEMA_VERSION=43\n'
  if [ "$ROLE" = rollback ]; then
    printf 'GRADEX_ROLLBACK_BASE_SHA=%s\n' 3383f46d0e9e6379c3bd166d39622c3659ae3d86
    printf 'GRADEX_ROLLBACK_PATCH_SHA256=%s\n' "$(sha256sum "$SCRIPT_DIR/rollback-compat.patch" | cut -d' ' -f1)"
  fi
  printf 'GRADEX_MIGRATION_0043_UP_SHA256=%s\n' "$(sha256sum "$SOURCE/backend/internal/db/migrations/0043_auto_device_replacement.up.sql" | cut -d' ' -f1)"
  printf 'GRADEX_MIGRATION_0043_DOWN_SHA256=%s\n' "$(sha256sum "$SOURCE/backend/internal/db/migrations/0043_auto_device_replacement.down.sql" | cut -d' ' -f1)"
  printf 'GRADEX_BACKEND_IMAGE=%s\n' "$BACKEND"
  printf 'GRADEX_FRONTEND_IMAGE=%s\n' "$FRONTEND"
  printf 'GRADEX_PROOF_IMAGE=%s\n' "$PROOF"
  printf 'GRADEX_BACKEND_IMAGE_ID=%s\n' "$(docker image inspect --format '{{.Id}}' "$BACKEND")"
  printf 'GRADEX_FRONTEND_IMAGE_ID=%s\n' "$(docker image inspect --format '{{.Id}}' "$FRONTEND")"
  printf 'GRADEX_PROOF_IMAGE_ID=%s\n' "$(docker image inspect --format '{{.Id}}' "$PROOF")"
} >"$ARTIFACT/release.env"
(cd "$ARTIFACT" && sha256sum release.env >release.env.sha256)
cp "$SCRIPT_DIR/import-artifact.sh" "$ARTIFACT/import-artifact.sh"
(cd "$ARTIFACT" && sha256sum import-artifact.sh >import-artifact.sh.sha256)
if [ "$ROLE" = rollback ]; then
  cp "$SCRIPT_DIR/rollback-compat.patch" "$ARTIFACT/rollback-compat.patch"
  (cd "$ARTIFACT" && sha256sum rollback-compat.patch >rollback-compat.patch.sha256)
fi
docker save "$BACKEND" "$FRONTEND" "$PROOF" | gzip --best >"$ARTIFACT/images.tar.gz"
(cd "$ARTIFACT" && sha256sum images.tar.gz >images.tar.gz.sha256)
printf 'device43 artifact built: revision=%s role=%s path=%s\n' "$REVISION" "$ROLE" "$ARTIFACT"
