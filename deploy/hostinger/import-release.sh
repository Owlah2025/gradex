#!/usr/bin/env bash
# Run via trusted SSH stdin from the frozen builder tree; no host checkout needed.
set -euo pipefail
die() { printf 'gradex-import: %s\n' "$*" >&2; exit 1; }
[ "$#" = 1 ] || die "usage: import-release.sh RELEASE_SHA"
revision="$1"
[[ "$revision" =~ ^[0-9a-f]{40}$ ]] || die "invalid release SHA"
state="${GRADEX_HOST_STATE_DIR:-/home/deploy/gradex-production}"
[[ "$state" =~ ^/[A-Za-z0-9._/-]+/gradex(-[A-Za-z0-9._-]+)?$ ]] || die "invalid host state directory"
for tool in tar gzip sha256sum docker awk cmp find; do
  command -v "$tool" >/dev/null || die "$tool is required"
done
incoming="$state/incoming/$revision"
destination="$state/releases/$revision"
[ ! -e "$destination" ] || die "release already imported; never overwrite immutable tooling"
umask 077
mkdir -p "$state/releases"
staging="$(mktemp -d "$state/releases/.import.XXXXXX")"
trap 'chmod -R u+w "$staging"; rm -rf -- "$staging"' EXIT
for file in release.env images.tar.gz images.tar.gz.sha256 deploy-bundle.tar.gz deploy-bundle.tar.gz.sha256; do
  [ -f "$incoming/$file" ] && [ ! -L "$incoming/$file" ] || die "missing regular artifact $file"
  cp "$incoming/$file" "$staging/$file"
done
cd "$staging"
for archive in images.tar.gz deploy-bundle.tar.gz; do
  # Restrict checksum filenames before asking sha256sum to read any path.
  [[ "$(cat "$archive.sha256")" =~ ^[0-9a-f]{64}\ \ $archive$ ]] || die "invalid checksum file for $archive"
  sha256sum --check --status "$archive.sha256" || die "$archive checksum failed"
done
# Bind the archive before executing even its validation helper.
[ "$(awk -F= '$1 == "GRADEX_RELEASE_SHA" { print $2 }' release.env)" = "$revision" ] || die "manifest release mismatch"
bundle_digest="$(awk -F= '$1 == "GRADEX_DEPLOY_BUNDLE_SHA256" { print $2 }' release.env)"
[ "$(cat deploy-bundle.tar.gz.sha256)" = "$bundle_digest  deploy-bundle.tar.gz" ] || die "bundle checksum disagrees with manifest"
tar -tzf deploy-bundle.tar.gz | awk '/^\// || /(^|\/)\.\.(\/|$)/ || /(^|\/)\.git(\/|$)/ { exit 1 }' || die "unsafe bundle path"
tar -tvzf deploy-bundle.tar.gz | awk 'substr($0,1,1) != "-" && substr($0,1,1) != "d" { exit 1 }' || die "bundle must contain only files and directories"
mkdir tooling
tar -xzf deploy-bundle.tar.gz --no-same-owner -C tooling
[ "$(awk -F= '$1 == "RELEASE_SHA" { print $2 }' tooling/release-tooling.env)" = "$revision" ] || die "bundle release mismatch"
. tooling/deploy/hostinger/release-artifact.sh
verify_release_bundle "$staging" "$revision"
gzip -dc images.tar.gz | docker load
verify_release_images "$staging/release.env" "$revision"
chmod -R a-w "$staging"
mv -T "$staging" "$destination"
trap - EXIT
printf 'gradex-import: verified release %s at %s/tooling\n' "$revision" "$destination"
