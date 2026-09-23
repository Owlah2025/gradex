#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCHEMA42_SHA=3383f46d0e9e6379c3bd166d39622c3659ae3d86
DEVICE_BASE_SHA=f41f9c28d67aa06ec370ba4f1a8c7d1192d5986d
CAPABILITY=SCHEMA42_CAPABILITY=manual-enhancement-v1

[ "$#" = 1 ] || {
  printf 'usage: %s SCHEMA42_RELEASE_ARTIFACT_DIR\n' "$0" >&2
  exit 2
}
ARTIFACT="$(cd "$1" && pwd)"
manifest_value() {
  local value
  value="$(awk -F= -v key="$1" '$1 == key { count++; value=substr($0,length(key)+2) }
    END { if (count != 1) exit 1; print value }' "$ARTIFACT/release.env")" || {
    printf 'schema41-to43: missing or duplicate manifest field %s\n' "$1" >&2
    exit 1
  }
  [[ "$value" =~ ^[A-Za-z0-9:._/-]+$ ]] || {
    printf 'schema41-to43: invalid manifest field %s\n' "$1" >&2
    exit 1
  }
  printf '%s' "$value"
}

git -C "$ROOT" cat-file -e "$SCHEMA42_SHA^{commit}"
git -C "$ROOT" cat-file -e "$DEVICE_BASE_SHA^{commit}"
git -C "$ROOT" merge-base --is-ancestor "$SCHEMA42_SHA" "$(git -C "$ROOT" rev-parse HEAD)"
[ -z "$(git -C "$ROOT" diff --name-only "$SCHEMA42_SHA..HEAD" -- \
  backend/internal/media backend/internal/playback \
  'backend/internal/db/migrations/0039*' 'backend/internal/db/migrations/0040*' \
  'backend/internal/db/migrations/0041*' 'backend/internal/db/migrations/0042*')" ] || {
  printf 'schema41-to43: reviewed media boundary changed\n' >&2
  exit 1
}

bash "$ROOT/deploy/scripts/verify-schema-42-rollback.sh"
bash "$ROOT/deploy/device43/verify-schema41-gate.sh"
bash "$ROOT/deploy/device43/verify-enhancement-queue-readonly.sh"

[ "$(manifest_value GRADEX_RELEASE_SHA)" = "$SCHEMA42_SHA" ]
(cd "$ARTIFACT" && sha256sum --check --status images.tar.gz.sha256 deploy-bundle.tar.gz.sha256)
[[ "$(manifest_value GRADEX_DEPLOY_BUNDLE_SHA256)" =~ ^[0-9a-f]{64}$ ]]
[ "$(awk '{print $1}' "$ARTIFACT/deploy-bundle.tar.gz.sha256")" = \
  "$(manifest_value GRADEX_DEPLOY_BUNDLE_SHA256)" ]
BACKEND="$(manifest_value GRADEX_BACKEND_IMAGE)"
FRONTEND="$(manifest_value GRADEX_FRONTEND_IMAGE)"
PROOF="$(manifest_value GRADEX_PROOF_IMAGE)"
for pair in \
  "$BACKEND $(manifest_value GRADEX_BACKEND_IMAGE_ID)" \
  "$FRONTEND $(manifest_value GRADEX_FRONTEND_IMAGE_ID)" \
  "$PROOF $(manifest_value GRADEX_PROOF_IMAGE_ID)"; do
  image="${pair%% *}"
  image_id="${pair#* }"
  [ "$(docker image inspect --format '{{.Id}}' "$image")" = "$image_id" ]
  [ "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")" = "$SCHEMA42_SHA" ]
done
[ "$(docker run --rm --network none --entrypoint gradex-migrate "$BACKEND" max-version)" = 42 ]
usage="$(docker run --rm --network none --entrypoint gradex-migrate "$BACKEND" 2>&1 || true)"
[ "$usage" = 'migrate: usage: migrate <up|down|version|max-version|rollback-schema-41|rollback-schema-42> [steps]' ]
docker run --rm --network none --entrypoint test "$BACKEND" -x /usr/local/bin/gradex-enhancement-drain

SCRATCH="$(mktemp -d)"
trap 'rm -rf -- "$SCRATCH"' EXIT
mkdir -p "$SCRATCH/schema42" "$SCRATCH/tooling"
git -C "$ROOT" archive "$SCHEMA42_SHA" | tar -xf - -C "$SCRATCH/schema42"
tar -xzf "$ARTIFACT/deploy-bundle.tar.gz" -C "$SCRATCH/tooling"
(cd "$SCRATCH/tooling" && sha256sum --check --status tooling.sha256)
[ "$(awk -F= '$1 == "RELEASE_SHA" {print $2}' "$SCRATCH/tooling/release-tooling.env")" = "$SCHEMA42_SHA" ]
[ "$(awk -F= '$1 == "SCHEMA42_CAPABILITY" {print $2}' "$SCRATCH/tooling/release-tooling.env")" = manual-enhancement-v1 ]

for direction in up down; do
  filename="0042_active_processing_attempt_kind.$direction.sql"
  source_hash="$(sha256sum "$SCRATCH/tooling/backend/internal/db/migrations/$filename" | awk '{print $1}')"
  image_hash="$(docker run --rm --network none --entrypoint sha256sum "$BACKEND" "internal/db/migrations/$filename" | awk '{print $1}')"
  [ "$source_hash" = "$image_hash" ]
done

mkdir -p "$SCRATCH/schema42/backend/cmd/api"
cp "$ROOT/backend/cmd/api/schema42_recovery_probe_integration_test.go" \
  "$SCRATCH/schema42/backend/cmd/api/"
(
  cd "$SCRATCH/schema42/backend"
  go test -c -tags integration -o "$SCRATCH/schema42-api.test" ./cmd/api
)

(
  cd "$ROOT/backend"
  GRADEX_SCHEMA42_RECOVERY_PROBE="$SCRATCH/schema42-api.test" \
    go test -tags integration ./internal/identity \
      -run '^TestStudentDeviceReleasePathFromClean41Through43$' \
      -count=1 -timeout 300s
  go test -tags integration ./internal/identity \
    -run '^TestSchema43MigrationFailureLeavesDirtySchemaUnservable$' \
    -count=1 -timeout 120s
  go test -tags integration ./internal/db \
    -run '^(TestSchema42RefusesUnquiescedMediaWork|TestSchema43DownRefusesAutomaticReplacementEvidence)$' \
    -count=1 -timeout 180s
)

printf 'schema41-to43 local proof PASS: 41 -> frozen 42 -> cutover -> 43; failure gates and frozen media boundary verified\n'
