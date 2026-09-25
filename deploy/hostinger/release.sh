#!/usr/bin/env bash

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_RELEASE_DIR="$S12_ROOT/deploy/.state/hostinger/releases"
. "$S12_ROOT/deploy/hostinger/release-artifact.sh"
. "$S12_ROOT/deploy/hostinger/release-closure.sh"

note() { printf 's12-hostinger-release: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

current_revision() {
  [ -z "$(git -C "$S12_ROOT" status --porcelain=v1)" ] || die "worktree must be clean before building a release"
  git -C "$S12_ROOT" rev-parse HEAD
}

image_revision() {
  docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$1"
}

record_release() {
  [ "$#" = 1 ] || die "record requires one full release SHA"
  local revision="$1" short backend frontend proof manifest
  [[ "$revision" =~ ^[0-9a-f]{40}$ ]] || die "release SHA must be 40 lowercase hexadecimal characters"
  [ "$(current_revision)" = "$revision" ] || die "record requires the exact clean builder HEAD"
  short="${revision:0:12}"
  backend="gradex-backend:hostinger-$short"
  frontend="gradex-frontend:hostinger-$short"
  proof="gradex-backend-proof:hostinger-$short"
  docker image inspect "$backend" "$frontend" "$proof" >/dev/null 2>&1 || die "release images are absent"
  [ "$(image_revision "$backend")" = "$revision" ] || die "backend revision label mismatch"
  [ "$(image_revision "$frontend")" = "$revision" ] || die "frontend revision label mismatch"
  [ "$(image_revision "$proof")" = "$revision" ] || die "proof revision label mismatch"

  mkdir -p "$S12_RELEASE_DIR/$revision"
  chmod 700 "$S12_ROOT/deploy/.state" "$S12_ROOT/deploy/.state/hostinger" \
    "$S12_RELEASE_DIR" "$S12_RELEASE_DIR/$revision"
  manifest="$S12_RELEASE_DIR/$revision/release.env"
  umask 077
  {
    printf 'GRADEX_RELEASE_SHA=%s\n' "$revision"
    printf 'GRADEX_BACKEND_IMAGE=%s\n' "$backend"
    printf 'GRADEX_FRONTEND_IMAGE=%s\n' "$frontend"
    printf 'GRADEX_PROOF_IMAGE=%s\n' "$proof"
    printf 'GRADEX_BACKEND_IMAGE_ID=%s\n' "$(docker image inspect --format '{{.Id}}' "$backend")"
    printf 'GRADEX_FRONTEND_IMAGE_ID=%s\n' "$(docker image inspect --format '{{.Id}}' "$frontend")"
    printf 'GRADEX_PROOF_IMAGE_ID=%s\n' "$(docker image inspect --format '{{.Id}}' "$proof")"
  } >"$manifest"
  package_tooling "$revision"
  note "recorded checksum-addressed local images for release $revision"
}

package_tooling() {
  local revision="$1" release="$S12_RELEASE_DIR/$1" staging
  staging="$(mktemp -d)"
  # The canonical closure lives in release-closure.sh so this builder and the
  # schema46 rollback builder cannot drift into two different artifact layouts.
  git -C "$S12_ROOT" archive "$revision" "${RELEASE_TOOLING_PATHS[@]}" |
    tar -xf - -C "$staging"
  assert_release_tooling_importable "$staging" || die "candidate tooling is not importable"
  # One boundary capability marker per release, naming the only cutover this
  # candidate bundle may execute. Older imported artifacts retain their own
  # marker and the corresponding commands continue to reject this one.
  printf 'RELEASE_SHA=%s\nDEPLOY_BUNDLE_FORMAT=1\nSCHEMA46_CAPABILITY=auto-enhancement-lesson-preview-v1\n' "$revision" >"$staging/release-tooling.env"
  (cd "$staging" && find . -type f ! -name tooling.sha256 -print0 | sort -z | xargs -0 sha256sum >tooling.sha256)
  tar -czf "$release/deploy-bundle.tar.gz" -C "$staging" --transform='s,^./,,' .
  (cd "$release" && sha256sum deploy-bundle.tar.gz >deploy-bundle.tar.gz.sha256)
  printf 'GRADEX_DEPLOY_BUNDLE_SHA256=%s\n' "$(sha256sum "$release/deploy-bundle.tar.gz" | awk '{print $1}')" >>"$release/release.env"
  rm -rf -- "$staging"
}

build_release() {
  command -v docker >/dev/null 2>&1 || die "docker is required"
  command -v tar >/dev/null 2>&1 || die "tar is required"
  local revision short backend frontend proof
  revision="$(current_revision)"
  short="${revision:0:12}"
  backend="gradex-backend:hostinger-$short"
  frontend="gradex-frontend:hostinger-$short"
  proof="gradex-backend-proof:hostinger-$short"

  git -C "$S12_ROOT" archive "$revision:backend" |
    docker build --build-arg "GRADEX_REVISION=$revision" --tag "$backend" -
  git -C "$S12_ROOT" archive "$revision:backend" |
    docker build --target proof --build-arg "GRADEX_REVISION=$revision" --tag "$proof" -
  git -C "$S12_ROOT" archive "$revision:frontend" |
    docker build --build-arg "GRADEX_REVISION=$revision" --tag "$frontend" -

  record_release "$revision"
  [ "$(current_revision)" = "$revision" ] || die "builder tree changed during build"
  note "built release $revision"
}

export_release() {
  command -v docker >/dev/null 2>&1 || die "docker is required"
  command -v gzip >/dev/null 2>&1 || die "gzip is required"
  local revision manifest backend frontend proof archive
  if [ "$#" = 1 ]; then
    revision="$1"
    [[ "$revision" =~ ^[0-9a-f]{40}$ ]] || die "release SHA must be 40 lowercase hexadecimal characters"
  elif [ "$#" = 0 ]; then
    revision="$(current_revision)"
  else
    die "export accepts at most one full release SHA"
  fi
  [ "$(current_revision)" = "$revision" ] || die "export requires the exact clean builder HEAD"
  manifest="$S12_RELEASE_DIR/$revision/release.env"
  [ -f "$manifest" ] || die "build release $revision first"
  set -a
  # shellcheck disable=SC1090
  . "$manifest"
  set +a
  [ "$GRADEX_RELEASE_SHA" = "$revision" ] || die "manifest release mismatch"
  verify_release_images "$manifest" "$revision"
  backend="$GRADEX_BACKEND_IMAGE"
  frontend="$GRADEX_FRONTEND_IMAGE"
  proof="$GRADEX_PROOF_IMAGE"
  archive="$S12_RELEASE_DIR/$revision/images.tar.gz"
  docker save "$backend" "$frontend" "$proof" | gzip --best >"$archive.partial"
  mv "$archive.partial" "$archive"
  (cd "$S12_RELEASE_DIR/$revision" && sha256sum images.tar.gz >images.tar.gz.sha256)
  chmod 600 "$archive" "$archive.sha256"
  note "exported release $revision with checksum into ignored state"
}

usage() {
  printf 'usage: %s {build|record SHA|export [SHA]}\n' "$0" >&2
  exit 2
}

case "${1:-}" in
  build) [ "$#" = 1 ] || usage; build_release ;;
  record) shift; record_release "$@" ;;
  export) shift; export_release "$@" ;;
  *) usage ;;
esac
