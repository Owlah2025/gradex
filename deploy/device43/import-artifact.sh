#!/usr/bin/env bash
set -euo pipefail

[ "$#" = 2 ] || {
  printf 'usage: %s ARTIFACT_DIR HOST_STATE_DIR\n' "$0" >&2
  exit 2
}
ARTIFACT="$(cd "$1" && pwd)"
STATE="$2"
manifest_value() {
  local value
  value="$(awk -F= -v key="$1" '$1 == key { count++; value=substr($0,length(key)+2) }
    END { if (count != 1) exit 1; print value }' "$ARTIFACT/release.env")" || {
    printf 'device43 import: missing or duplicate %s\n' "$1" >&2
    exit 1
  }
  [[ "$value" =~ ^[A-Za-z0-9:._/-]+$ ]] || {
    printf 'device43 import: invalid manifest value for %s\n' "$1" >&2
    exit 1
  }
  printf '%s' "$value"
}
(cd "$ARTIFACT" && sha256sum --check --status \
  release.env.sha256 images.tar.gz.sha256 import-artifact.sh.sha256)
REVISION="$(manifest_value GRADEX_RELEASE_SHA)"
ROLE="$(manifest_value GRADEX_DEVICE43_ROLE)"
[[ "$REVISION" =~ ^[0-9a-f]{40}$ ]]
[ "$(manifest_value GRADEX_SCHEMA_VERSION)" = 43 ]
case "$ROLE" in current|rollback) ;; *) exit 1 ;; esac
if [ "$ROLE" = rollback ]; then
  [ "$REVISION" = 54115fd6029d5d80af63640ac6f0bfe31be22d67 ]
  [ "$(manifest_value GRADEX_ROLLBACK_BASE_SHA)" = 3383f46d0e9e6379c3bd166d39622c3659ae3d86 ]
  [ "$(manifest_value GRADEX_ROLLBACK_PATCH_SHA256)" = 9bd3e47288efbd8c78081dd2694ce94dea4fa3ba0c2525c460be6966266996f7 ]
  (cd "$ARTIFACT" && sha256sum --check --status rollback-compat.patch.sha256)
fi

docker load <"$ARTIFACT/images.tar.gz" >/dev/null
for KIND in BACKEND FRONTEND PROOF; do
  IMAGE="$(manifest_value "GRADEX_${KIND}_IMAGE")"
  IMAGE_ID="$(manifest_value "GRADEX_${KIND}_IMAGE_ID")"
  [ "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$IMAGE")" = "$REVISION" ]
  [ "$(docker image inspect --format '{{.Id}}' "$IMAGE")" = "$IMAGE_ID" ]
done
BACKEND="$(manifest_value GRADEX_BACKEND_IMAGE)"
[ "$(docker run --rm --network none --entrypoint gradex-migrate "$BACKEND" max-version)" = 43 ]
if [ "$ROLE" = current ]; then
  docker run --rm --network none --entrypoint test "$BACKEND" \
    -x /usr/local/bin/gradex-device-cutover
fi
for DIRECTION in UP DOWN; do
  FILE="0043_auto_device_replacement.${DIRECTION,,}.sql"
  EXPECTED="$(manifest_value "GRADEX_MIGRATION_0043_${DIRECTION}_SHA256")"
  ACTUAL="$(docker run --rm --network none --entrypoint sha256sum "$BACKEND" "internal/db/migrations/$FILE")"
  [ "${ACTUAL%% *}" = "$EXPECTED" ]
done

umask 077
mkdir -p "$STATE/releases"
DEST="$STATE/releases/$REVISION"
[ ! -e "$DEST" ] || {
  printf 'device43 import: release already staged: %s\n' "$REVISION" >&2
  exit 1
}
mkdir "$DEST"
cp "$ARTIFACT/release.env" "$ARTIFACT/release.env.sha256" \
  "$ARTIFACT/images.tar.gz" "$ARTIFACT/images.tar.gz.sha256" \
  "$ARTIFACT/import-artifact.sh" "$ARTIFACT/import-artifact.sh.sha256" "$DEST/"
if [ "$ROLE" = rollback ]; then
  cp "$ARTIFACT/rollback-compat.patch" "$ARTIFACT/rollback-compat.patch.sha256" "$DEST/"
fi
chmod 600 "$DEST"/*
printf 'device43 artifact imported: revision=%s role=%s staged=%s\n' "$REVISION" "$ROLE" "$DEST"
