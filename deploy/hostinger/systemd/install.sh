#!/usr/bin/env bash

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
S12_SYSTEMD_SOURCE="$S12_ROOT/deploy/hostinger/systemd"
S12_SYSTEMD_TARGET=/etc/systemd/system
S12_INSTANCE=staging
S12_HOST_STATE=""
S12_RUNTIME_ENV=""
S12_PROJECT=""
S12_UNIT_PREFIX=""
S12_MONITOR_SERVICE=""
S12_MONITOR_TIMER=""
S12_BACKUP_SERVICE=""
S12_BACKUP_TIMER=""
S12_UNIT_FILES=()
S12_UNIT_PATHS=()
S12_TEMPORARY=""

note() { printf 'gradex-systemd-install: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

usage() {
  cat >&2 <<'EOF'
usage:
  install.sh render --output DIR --user USER [--group GROUP] [--repo PATH]
                     [--instance staging|production]
  sudo install.sh install --user USER [--group GROUP] [--repo PATH]
                          [--instance staging|production]

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

validate_instance_paths() {
  [[ "$S12_HOST_STATE" =~ ^/[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*/gradex(-[A-Za-z0-9._-]+)?$ ]] ||
    die "the instance state directory is invalid"
  case "$S12_HOST_STATE" in
    *//* | */./* | */../* | */. | */..) die "the instance state directory is unsafe" ;;
  esac
  case "$S12_RUNTIME_ENV" in
    "$S12_HOST_STATE/runtime.env") ;;
    *) die "the instance runtime environment must remain at state-dir/runtime.env" ;;
  esac
}

validate_instance_names() {
  local unit
  [[ "$S12_PROJECT" =~ ^[a-z0-9][a-z0-9_-]{2,62}$ ]] ||
    die "the instance project is invalid"
  [[ "$S12_UNIT_PREFIX" =~ ^[a-z0-9][a-z0-9-]{0,62}$ ]] ||
    die "the instance unit name is invalid"

  for unit in "$S12_MONITOR_SERVICE" "$S12_MONITOR_TIMER" \
    "$S12_BACKUP_SERVICE" "$S12_BACKUP_TIMER"; do
    [[ "$unit" =~ ^[a-z0-9][a-z0-9-]{0,62}\.(service|timer)$ ]] ||
      die "the instance unit name is invalid"
  done
}

validate_profile_contract() {
  case "$S12_INSTANCE" in
    staging)
      [ "$S12_HOST_STATE" = /var/lib/gradex ] || die "the staging state directory is invalid"
      [ "$S12_RUNTIME_ENV" = /var/lib/gradex/runtime.env ] || die "the staging runtime environment is invalid"
      [ "$S12_PROJECT" = gradex-staging ] || die "the staging project is invalid"
      [ "$S12_UNIT_PREFIX" = gradex ] || die "the staging unit name is invalid"
      ;;
    production)
      [ "$S12_HOST_STATE" = /home/deploy/gradex-production ] ||
        die "the production state directory is invalid"
      [ "$S12_RUNTIME_ENV" = /home/deploy/gradex-production/runtime.env ] ||
        die "the production runtime environment is invalid"
      [ "$S12_PROJECT" = gradex-production ] || die "the production project is invalid"
      [ "$S12_UNIT_PREFIX" = gradex-production ] || die "the production unit name is invalid"
      ;;
    *)
      die "--instance must be staging or production"
      ;;
  esac
}

validate_non_overlapping_units() {
  local unit
  [ "$S12_MONITOR_SERVICE" != "$S12_BACKUP_SERVICE" ] || die "instance service names overlap"
  [ "$S12_MONITOR_TIMER" != "$S12_BACKUP_TIMER" ] || die "instance timer names overlap"
  if [ "$S12_INSTANCE" = production ]; then
    for unit in "$S12_MONITOR_SERVICE" "$S12_MONITOR_TIMER" \
      "$S12_BACKUP_SERVICE" "$S12_BACKUP_TIMER"; do
      case "$unit" in
        gradex-monitor.service|gradex-monitor.timer|gradex-backup.service|gradex-backup.timer)
          die "production unit overlaps staging"
          ;;
        gradex-*.service|gradex-*.timer) ;;
        *) die "production unit name is invalid" ;;
      esac
    done
  fi
}

validate_instance_config() {
  [[ "$S12_INSTANCE" =~ ^[a-z0-9][a-z0-9-]{0,62}$ ]] ||
    die "--instance is invalid"
  validate_instance_paths
  validate_instance_names
  validate_profile_contract
  validate_non_overlapping_units
}

configure_staging_instance() {
  S12_HOST_STATE=/var/lib/gradex
  S12_RUNTIME_ENV=/var/lib/gradex/runtime.env
  S12_PROJECT=gradex-staging
  S12_UNIT_PREFIX=gradex
}

configure_production_instance() {
  S12_HOST_STATE=/home/deploy/gradex-production
  S12_RUNTIME_ENV=/home/deploy/gradex-production/runtime.env
  S12_PROJECT=gradex-production
  S12_UNIT_PREFIX=gradex-production
}

configure_instance() {
  case "$S12_INSTANCE" in
    staging) configure_staging_instance ;;
    production) configure_production_instance ;;
    *) die "--instance must be staging or production" ;;
  esac

  S12_MONITOR_SERVICE="$S12_UNIT_PREFIX-monitor.service"
  S12_MONITOR_TIMER="$S12_UNIT_PREFIX-monitor.timer"
  S12_BACKUP_SERVICE="$S12_UNIT_PREFIX-backup.service"
  S12_BACKUP_TIMER="$S12_UNIT_PREFIX-backup.timer"
  S12_UNIT_FILES=(
    "$S12_MONITOR_SERVICE"
    "$S12_MONITOR_TIMER"
    "$S12_BACKUP_SERVICE"
    "$S12_BACKUP_TIMER"
  )
  validate_instance_config
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

  [ -n "$S12_REPO" ] || die "--repo is required"
  S12_REPO="$(readlink -f -- "$S12_REPO")" || die "--repo could not be resolved"
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

decorate_production_service() {
  local target="$1" kind="$2" syslog_identifier="$3" replace_state_path=false
  [ "$kind" = backup ] && replace_state_path=true
  awk \
    -v state_dir="$S12_HOST_STATE" \
    -v env_file="$S12_RUNTIME_ENV" \
    -v project="$S12_PROJECT" \
    -v syslog_identifier="$syslog_identifier" \
    -v replace_state_path="$replace_state_path" \
    '
      /^ExecStart=/ {
        print "Environment=GRADEX_HOST_STATE_DIR=" state_dir
        print "Environment=GRADEX_HOST_ENV_FILE=" env_file
        print "Environment=GRADEX_HOST_PROJECT=" project
        saw_routing = 1
      }
      /^ReadWritePaths=\/var\/lib\/gradex$/ {
        if (replace_state_path == "true") {
          print "ReadWritePaths=" state_dir
          saw_state_path = 1
          next
        }
      }
      /^SyslogIdentifier=gradex-(backup|monitor)$/ {
        print "SyslogIdentifier=" syslog_identifier
        saw_syslog = 1
        next
      }
      { print }
      END {
        if (!saw_routing || !saw_syslog ||
            (replace_state_path == "true" && !saw_state_path)) {
          exit 1
        }
      }
    ' "$target" >"$target.production"
  mv -- "$target.production" "$target"
}

render_timer() {
  local source="$1" target="$2" kind="$3" service="$4" source_service
  case "$kind" in
    monitor) source_service=gradex-monitor.service ;;
    backup) source_service=gradex-backup.service ;;
    *) die "the timer kind is invalid" ;;
  esac
  sed -e "s|^Unit=$source_service$|Unit=$service|" \
    "$source" >"$target"
  grep --quiet --fixed-strings --line-regexp "Unit=$service" "$target" ||
    die "the rendered timer does not link to $service"
}

render_units() {
  local destination="$1" file
  mkdir -p -- "$destination"
  render_service "$S12_SYSTEMD_SOURCE/gradex-monitor.service.in" "$destination/$S12_MONITOR_SERVICE"
  render_service "$S12_SYSTEMD_SOURCE/gradex-backup.service.in" "$destination/$S12_BACKUP_SERVICE"
  if [ "$S12_INSTANCE" = production ]; then
    decorate_production_service "$destination/$S12_MONITOR_SERVICE" monitor gradex-production-monitor
    decorate_production_service "$destination/$S12_BACKUP_SERVICE" backup gradex-production-backup
    render_timer "$S12_SYSTEMD_SOURCE/gradex-monitor.timer" \
      "$destination/$S12_MONITOR_TIMER" monitor "$S12_MONITOR_SERVICE"
    render_timer "$S12_SYSTEMD_SOURCE/gradex-backup.timer" \
      "$destination/$S12_BACKUP_TIMER" backup "$S12_BACKUP_SERVICE"
  else
    for file in "$S12_MONITOR_TIMER" "$S12_BACKUP_TIMER"; do
      install -m 0644 "$S12_SYSTEMD_SOURCE/$file" "$destination/$file"
    done
  fi
  S12_UNIT_PATHS=()
  for file in "${S12_UNIT_FILES[@]}"; do
    S12_UNIT_PATHS+=("$destination/$file")
  done
  if grep --quiet --extended-regexp '@GRADEX_(USER|GROUP|REPO)@' "${S12_UNIT_PATHS[@]}"; then
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
      --instance) [ "$#" -ge 2 ] || usage; S12_INSTANCE="$2"; shift 2 ;;
      *) usage ;;
    esac
  done

  configure_instance
  for tool in getent grep id install mkdir readlink sed stat; do
    require_tool "$tool"
  done
  if [ "$S12_INSTANCE" = production ]; then
    for tool in awk mv; do
      require_tool "$tool"
    done
  fi
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
  systemd-analyze verify "${S12_UNIT_PATHS[@]}"
  install -o root -g root -m 0644 "${S12_UNIT_PATHS[@]}" "$S12_SYSTEMD_TARGET/"
  systemctl daemon-reload
  note "installed and reloaded Gradex units; timers remain disabled until explicitly enabled"
}

main "$@"
