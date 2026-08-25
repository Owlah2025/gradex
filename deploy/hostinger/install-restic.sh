#!/usr/bin/env bash
set -euo pipefail

RESTIC_VERSION=0.19.1
RESTIC_TARGET=/usr/local/bin/restic
RESTIC_RELEASE_BASE="https://github.com/restic/restic/releases/download/v$RESTIC_VERSION"
RESTIC_TEMP_DIR=""
RESTIC_TARGET_TEMP=""

note() {
  printf 'gradex-restic-install: %s\n' "$*" >&2
}

die() {
  note "$*"
  exit 1
}

cleanup() {
  if [ -n "$RESTIC_TARGET_TEMP" ]; then
    rm -f -- "$RESTIC_TARGET_TEMP"
  fi
  if [ -n "$RESTIC_TEMP_DIR" ] && [ -d "$RESTIC_TEMP_DIR" ]; then
    rm -rf -- "$RESTIC_TEMP_DIR"
  fi
}

require_tools() {
  local tool
  for tool in bunzip2 cmp curl install mktemp rm sha256sum uname; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done
}

architecture_asset() {
  case "$(uname -m)" in
    x86_64|amd64)
      printf '%s %s\n' \
        restic_0.19.1_linux_amd64.bz2 \
        f415415624dcc452f2a02b8c33641791a8c6d6d3b65bbb3543fcf9a25151585c2
      ;;
    aarch64|arm64)
      printf '%s %s\n' \
        restic_0.19.1_linux_arm64.bz2 \
        a5f64aaab53d51e311fa3829124c5b703f2d14cf187d8640b6be3b2b49376465
      ;;
    *) die "unsupported Linux architecture: $(uname -m)" ;;
  esac
}

main() {
  [ "$EUID" = 0 ] || die "install must run as root"
  [ "$(uname -s)" = Linux ] || die "restic installation supports Linux only"
  require_tools
  local asset checksum archive checksum_file
  read -r asset checksum < <(architecture_asset)
  RESTIC_TEMP_DIR="$(mktemp -d)"
  trap cleanup EXIT
  archive="$RESTIC_TEMP_DIR/$asset"
  checksum_file="$RESTIC_TEMP_DIR/SHA256SUMS"
  curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 \
    --output "$archive" "$RESTIC_RELEASE_BASE/$asset"
  printf '%s  %s\n' "$checksum" "$archive" >"$checksum_file"
  sha256sum --check "$checksum_file"
  bunzip2 -c "$archive" >"$RESTIC_TEMP_DIR/restic"
  chmod 0755 "$RESTIC_TEMP_DIR/restic"

  if [ -x "$RESTIC_TARGET" ] && cmp --silent "$RESTIC_TEMP_DIR/restic" "$RESTIC_TARGET"; then
    note "restic $RESTIC_VERSION is already installed and matches the pinned release"
    return
  fi
  RESTIC_TARGET_TEMP="$RESTIC_TARGET.new.$$"
  install -o root -g root -m 0755 "$RESTIC_TEMP_DIR/restic" "$RESTIC_TARGET_TEMP"
  mv -- "$RESTIC_TARGET_TEMP" "$RESTIC_TARGET"
  RESTIC_TARGET_TEMP=""
  note "installed pinned restic $RESTIC_VERSION at $RESTIC_TARGET"
}

main "$@"
