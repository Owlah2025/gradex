#!/usr/bin/env bash

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
S12_SYSTEMD_SOURCE="$S12_ROOT/deploy/hostinger/systemd"
S12_SYSTEMD_TARGET=/etc/systemd/system
S12_HOST_STATE=/var/lib/gradex
S12_RUNTIME_ENV="$S12_HOST_STATE/runtime.env"
S12_TEMPORARY=""

note() { printf 'gradex-systemd-install: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

usage() {
  cat >&2 <<'EOF'
usage:
  install.sh render --output DIR --user USER [--group GROUP] [--repo PATH]
  sudo install.sh install --user USER [--group GROUP] [--repo PATH]

The install command copies validated units and reloads systemd. It does not
enable timers or run monitoring/backup jobs.
EOF
  exit 2
}

cleanup() {
  if [ -n "$S12_TEMPORARY" ] && [ -d "$S12_TEMPORARY" ]; then
    rm -rf -- "$S12_TEMPORARY"
  fi
}

require_tool() {
  command -v "$1" >/dev/null 2>&1 || die "$1 is required"
}

validate_identity_and_repo() {
  local uid
  [ -n "$S12_OPERATOR" ] || die "--user is required"
  [[ "$S12_OPERATOR" =~ ^[a-z_][a-z0-9_-]*[$]?$ ]] || die "--user is invalid"
  id "$S12_OPERATOR" >/dev/null 2>&1 || die "operator user does not exist"
  uid="$(id -u "$S12_OPERATOR")"
  if [ -z "$S12_GROUP" ]; then
    S12_GROUP="$(id -gn "$S12_OPERATOR")"
  fi
  [[ "$S12_GROUP" =~ ^[a-z_][a-z0-9_-]*[$]?$ ]] || die "--group is invalid"
  getent group "$S12_GROUP" >/dev/null 2>&1 || die "operator group does not exist"

  S12_REPO="$(readlink -f -- "$S12_REPO")"
  [[ "$S12_REPO" =~ ^/[A-Za-z0-9._/-]+$ ]] ||
    die "--repo must be an absolute path containing only letters, digits, dot, underscore, slash, or hyphen"
  [ -e "$S12_REPO/.git" ] || die "--repo is not a Gradex worktree"
  [ -x "$S12_REPO/deploy/hostinger/host.sh" ] || die "host.sh is absent or not executable"

  if [ "$S12_COMMAND" = install ] && [ "$uid" = 0 ]; then
    die "the scheduled operator must be non-root"
  fi
}

render_service() {
  local source="$1" target="$2" repo_escaped
  repo_escaped="${S12_REPO//&/\\&}"
  sed \
    -e "s|@GRADEX_USER@|$S12_OPERATOR|g" \
    -e "s|@GRADEX_GROUP@|$S12_GROUP|g" \
    -e "s|@GRADEX_REPO@|$repo_escaped|g" \
    "$source" >"$target"
}

render_units() {
  local destination="$1" file
  mkdir -p -- "$destination"
  render_service "$S12_SYSTEMD_SOURCE/gradex-monitor.service.in" "$destination/gradex-monitor.service"
  render_service "$S12_SYSTEMD_SOURCE/gradex-backup.service.in" "$destination/gradex-backup.service"
  for file in gradex-monitor.timer gradex-backup.timer; do
    install -m 0644 "$S12_SYSTEMD_SOURCE/$file" "$destination/$file"
  done
  if grep --quiet --extended-regexp '@GRADEX_(USER|GROUP|REPO)@' "$destination"/*; then
    die "a systemd template placeholder was not rendered"
  fi
}

validate_install_state() {
  local mode owner_uid operator_uid
  [ "$EUID" = 0 ] || die "install must run as root"
  [ -f "$S12_RUNTIME_ENV" ] || die "$S12_RUNTIME_ENV is absent"
  mode="$(stat -c '%a' "$S12_RUNTIME_ENV")"
  case "$mode" in 400|600) ;; *) die "$S12_RUNTIME_ENV must have mode 0400 or 0600" ;; esac
  owner_uid="$(stat -c '%u' "$S12_RUNTIME_ENV")"
  operator_uid="$(id -u "$S12_OPERATOR")"
  [ "$owner_uid" = "$operator_uid" ] || die "$S12_RUNTIME_ENV must be owned by the scheduled operator"
  runuser -u "$S12_OPERATOR" -- test -r "$S12_RUNTIME_ENV" ||
    die "the scheduled operator cannot read $S12_RUNTIME_ENV"
  runuser -u "$S12_OPERATOR" -- test -x "$S12_REPO/deploy/hostinger/host.sh" ||
    die "the scheduled operator cannot execute host.sh"
  runuser -u "$S12_OPERATOR" -- docker info >/dev/null 2>&1 ||
    die "the scheduled operator cannot reach Docker"
}

main() {
  [ "$#" -gt 0 ] || usage
  S12_COMMAND="$1"
  shift
  case "$S12_COMMAND" in render|install) ;; *) usage ;; esac

  S12_OUTPUT=""
  S12_OPERATOR=""
  S12_GROUP=""
  S12_REPO="$S12_ROOT"
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --output) [ "$#" -ge 2 ] || usage; S12_OUTPUT="$2"; shift 2 ;;
      --user) [ "$#" -ge 2 ] || usage; S12_OPERATOR="$2"; shift 2 ;;
      --group) [ "$#" -ge 2 ] || usage; S12_GROUP="$2"; shift 2 ;;
      --repo) [ "$#" -ge 2 ] || usage; S12_REPO="$2"; shift 2 ;;
      *) usage ;;
    esac
  done

  for tool in getent grep id install mkdir readlink sed stat; do
    require_tool "$tool"
  done
  validate_identity_and_repo

  if [ "$S12_COMMAND" = render ]; then
    [ -n "$S12_OUTPUT" ] || die "render requires --output"
    [ "$EUID" != 0 ] || [ "$S12_OUTPUT" != "$S12_SYSTEMD_TARGET" ] ||
      die "render cannot target the systemd installation directory"
    render_units "$S12_OUTPUT"
    note "rendered units into $S12_OUTPUT without installing or starting them"
    return
  fi

  [ -z "$S12_OUTPUT" ] || die "install does not accept --output"
  for tool in docker mktemp runuser systemctl systemd-analyze; do
    require_tool "$tool"
  done
  validate_install_state
  S12_TEMPORARY="$(mktemp -d)"
  trap cleanup EXIT
  render_units "$S12_TEMPORARY"
  systemd-analyze verify "$S12_TEMPORARY"/*.service "$S12_TEMPORARY"/*.timer
  install -o root -g root -m 0644 "$S12_TEMPORARY"/*.service "$S12_TEMPORARY"/*.timer "$S12_SYSTEMD_TARGET/"
  systemctl daemon-reload
  note "installed and reloaded Gradex units; timers remain disabled until explicitly enabled"
}

main "$@"
