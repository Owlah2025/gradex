#!/usr/bin/env bash

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_RELEASE_DIR="$S12_ROOT/deploy/.state/hostinger/releases"

note() { printf 's12-hostinger-release: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

current_revision() {
  git -C "$S12_ROOT" status --porcelain=v1 | grep -q . && die "worktree must be clean before building a release"
  git -C "$S12_ROOT" rev-parse HEAD
}

image_revision() {
  docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$1"
}

build_release() {
  command -v docker >/dev/null 2>&1 || die "docker is required"
  local revision short backend frontend proof manifest
  revision="$(current_revision)"
  short="${revision:0:12}"
  backend="gradex-backend:hostinger-$short"
  frontend="gradex-frontend:hostinger-$short"
  proof="gradex-backend-proof:hostinger-$short"

  docker build --build-arg "GRADEX_REVISION=$revision" --tag "$backend" "$S12_ROOT/backend"
  docker build --target proof --build-arg "GRADEX_REVISION=$revision" --tag "$proof" "$S12_ROOT/backend"
  docker build --build-arg "GRADEX_REVISION=$revision" --tag "$frontend" "$S12_ROOT/frontend"

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
  note "built release $revision and wrote its ignored manifest"
}

export_release() {
  command -v docker >/dev/null 2>&1 || die "docker is required"
  command -v gzip >/dev/null 2>&1 || die "gzip is required"
  local revision manifest backend frontend proof archive
  revision="$(current_revision)"
  manifest="$S12_RELEASE_DIR/$revision/release.env"
  [ -f "$manifest" ] || die "build release $revision first"
  set -a
  # shellcheck disable=SC1090
  . "$manifest"
  set +a
  backend="$GRADEX_BACKEND_IMAGE"
  frontend="$GRADEX_FRONTEND_IMAGE"
  proof="$GRADEX_PROOF_IMAGE"
  archive="$S12_RELEASE_DIR/$revision/images.tar.gz"
  docker save "$backend" "$frontend" "$proof" | gzip --best >"$archive.partial"
  mv "$archive.partial" "$archive"
  sha256sum "$archive" >"$archive.sha256"
  chmod 600 "$archive" "$archive.sha256"
  note "exported release $revision with checksum into ignored state"
}

usage() {
  printf 'usage: %s {build|export}\n' "$0" >&2
  exit 2
}

case "${1:-}" in
  build) [ "$#" = 1 ] || usage; build_release ;;
  export) [ "$#" = 1 ] || usage; export_release ;;
  *) usage ;;
esac
