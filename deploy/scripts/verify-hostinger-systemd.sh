#!/usr/bin/env bash

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_SYSTEMD_DIR="$S12_ROOT/deploy/hostinger/systemd"
S12_README="$S12_ROOT/deploy/hostinger/README.md"
# Anchor the first-production-launch regression to the reviewed staging contract.
S12_BASE_COMMIT=087fff32cbe598669b5a8fb3e0e506172469948a
S12_TEMPORARY=""

note() { printf 'hostinger-systemd: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }

cleanup() {
  if [ -n "$S12_TEMPORARY" ] && [ -d "$S12_TEMPORARY" ]; then
    rm -rf -- "$S12_TEMPORARY"
  fi
}

assert_line() {
  local file="$1" expected="$2"
  grep --quiet --fixed-strings --line-regexp "$expected" "$file" ||
    die "$(basename "$file") is missing: $expected"
}

assert_unit_set() {
  local directory="$1" profile="$2" file actual
  local -a expected
  case "$profile" in
    staging)
      expected=(
        gradex-monitor.service
        gradex-monitor.timer
        gradex-backup.service
        gradex-backup.timer
      )
      ;;
    production)
      expected=(
        gradex-production-monitor.service
        gradex-production-monitor.timer
        gradex-production-backup.service
        gradex-production-backup.timer
      )
      ;;
    *) die "unknown systemd verification profile: $profile" ;;
  esac

  for file in "${expected[@]}"; do
    [ -f "$directory/$file" ] || die "$profile render is missing $file"
  done
  for actual in "$directory"/*; do
    case "$profile" in
      staging)
        case "${actual##*/}" in
          gradex-monitor.service|gradex-monitor.timer|gradex-backup.service|gradex-backup.timer) ;;
          *) die "$profile render contains an unexpected unit filename" ;;
        esac
        ;;
      production)
        case "${actual##*/}" in
          gradex-production-monitor.service|gradex-production-monitor.timer|gradex-production-backup.service|gradex-production-backup.timer) ;;
          *) die "$profile render contains an unexpected unit filename" ;;
        esac
        ;;
    esac
  done
}

assert_no_environment_file() {
  local file="$1"
  if grep --quiet --ignore-case --extended-regexp '^EnvironmentFile[[:space:]]*=' "$file"; then
    die "$(basename "$file") contains an EnvironmentFile= secret-loading surface"
  fi
}

assert_no_environment_assignment() {
  local file="$1"
  if grep --quiet --extended-regexp '^Environment=' "$file"; then
    die "$(basename "$file") contains an unexpected Environment= assignment"
  fi
}

assert_staging_environment_surface() {
  local file="$1"
  assert_no_environment_file "$file"
  assert_no_environment_assignment "$file"
  if grep --quiet --fixed-strings 'GRADEX_HOST_' "$file"; then
    die "$(basename "$file") contains production routing metadata"
  fi
}

assert_production_environment_surface() {
  local file="$1" environment_count expected environment_line
  assert_no_environment_file "$file"
  environment_count="$(awk '/^Environment=/{count++} END {print count+0}' "$file")"
  [ "$environment_count" = 3 ] ||
    die "$(basename "$file") does not contain exactly three routing Environment= assignments"
  for expected in \
    'Environment=GRADEX_HOST_STATE_DIR=/home/deploy/gradex-production' \
    'Environment=GRADEX_HOST_ENV_FILE=/home/deploy/gradex-production/runtime.env' \
    'Environment=GRADEX_HOST_PROJECT=gradex-production'; do
    assert_line "$file" "$expected"
  done
  if grep --quiet --ignore-case --extended-regexp \
    '^Environment=.*(password|token|secret|webhook|database|credential|api[_-]?key)' "$file"; then
    die "$(basename "$file") contains a credential-bearing environment assignment"
  fi
  while IFS= read -r environment_line; do
    case "$environment_line" in
      'Environment=GRADEX_HOST_STATE_DIR=/home/deploy/gradex-production'|'Environment=GRADEX_HOST_ENV_FILE=/home/deploy/gradex-production/runtime.env'|'Environment=GRADEX_HOST_PROJECT=gradex-production') ;;
      *) die "$(basename "$file") contains an unexpected Environment= assignment" ;;
    esac
  done < <(grep --extended-regexp '^Environment=' "$file" || true)
}

assert_production_readme_contract() {
  local production_section
  assert_line "$S12_README" 'cd /home/deploy/gradex-backup-runtime'
  assert_line "$S12_README" '  --repo /home/deploy/gradex-backup-runtime'
  if ! grep --quiet --fixed-strings -- '  --instance production ' "$S12_README"; then
    die "the production README procedure is missing the production instance flag"
  fi
  if grep --quiet --fixed-strings 'cd /home/deploy/gradex-production' "$S12_README" ||
    grep --quiet --fixed-strings -- '--repo /home/deploy/gradex-production' "$S12_README"; then
    die "the production README procedure collapses state and repository paths"
  fi
  production_section="$(awk '
    /^### Production scheduler$/ { capture = 1 }
    capture { print }
    capture && /^For staging,/ { exit }
  ' "$S12_README")"
  [ "$(printf '%s\n' "$production_section" | grep -c --fixed-strings '```bash')" = 3 ] ||
    die "the production README procedure does not contain three bash blocks"
  [ "$(printf '%s\n' "$production_section" | grep -c --line-regexp '```')" = 3 ] ||
    die "the production README procedure has malformed code fences"
}

extract_host_function() {
  local function_name="$1" source="$2"
  awk -v function_name="$function_name" '
    $0 ~ "^" function_name "\\(\\) \\{" { capture = 1 }
    capture { print }
    capture && /^}/ { exit }
  ' "$source"
}

verify_host_monitor_environment() {
  local host_script="$S12_ROOT/deploy/hostinger/host.sh"
  local real_monitor_once="$S12_ROOT/deploy/monitoring/monitor-once.sh"
  local routing_root="$S12_TEMPORARY/monitor-routing"
  local state_dir="$routing_root/gradex-monitor-routing"
  local environment_file="$state_dir/runtime.env"
  local environment_log="$routing_root/environment.log"
  local invocation_log="$routing_root/invocation.log"
  local payload_log="$routing_root/payload.json"
  local curl_log="$routing_root/curl.args"
  local monitor_log="$routing_root/monitor.log"
  local app_environment monitor_status
  local S12_ROOT="$routing_root"
  local S12_HOST_STATE_DIR="$state_dir"
  local S12_ENV_FILE="$environment_file"
  local S12_BACKUP_DIR="$state_dir/backups"
  local S12_PROJECT=gradex-monitor-routing

  mkdir -p "$state_dir/backups" "$routing_root/deploy/monitoring"
  cat >"$routing_root/deploy/monitoring/monitor-once.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "${GRADEX_ENVIRONMENT:-}" >"$FAKE_HOST_MONITOR_ENVIRONMENT_LOG"
: >"$FAKE_HOST_MONITOR_INVOCATION_LOG"
exec "$FAKE_HOST_MONITOR_REAL_ONCE"
EOF
  chmod 0755 "$routing_root/deploy/monitoring/monitor-once.sh"

  eval "$(extract_host_function validate_local_targets "$host_script")"
  eval "$(extract_host_function load_environment "$host_script")"
  eval "$(extract_host_function run_monitor "$host_script")"
  collect_monitor_runtime_report() {
    printf '%s\n' \
      'version=1' \
      'worker|PASS|routing fixture worker' \
      'postgres_schema|PASS|routing fixture schema' \
      'email_outbox|METRICS|terminal=0;oldest_due_age=-1;stale_lease_age=-1' \
      'disk_roots|PASS|routing fixture disk' >"$1"
  }

  write_monitor_runtime() {
    local app_environment="$1"
    printf '%s\n' \
      "APP_ENV=$app_environment" \
      'PUBLIC_ORIGIN=https://monitor-routing.example' \
      "GRADEX_MONITOR_DISK_PATHS=$state_dir" \
      'GRADEX_MONITOR_DISK_MIN_FREE_BYTES=1' \
      'GRADEX_ALERT_WEBHOOK_URL=https://alerts.example/routing' >"$environment_file"
    chmod 0600 "$environment_file"
  }

  reset_monitor_fixture_logs() {
    : >"$environment_log"
    : >"$payload_log"
    : >"$curl_log"
    rm -f -- "$invocation_log"
  }

  run_monitor_fixture() {
    set +e
    (
      export PATH="$S12_TEMPORARY/fake-bin:$PATH"
      export FAKE_CURL_ARGS_LOG="$curl_log"
      export FAKE_CURL_DATA_LOG="$payload_log"
      export FAKE_HOST_MONITOR_ENVIRONMENT_LOG="$environment_log"
      export FAKE_HOST_MONITOR_INVOCATION_LOG="$invocation_log"
      export FAKE_HOST_MONITOR_REAL_ONCE="$real_monitor_once"
      run_monitor
    ) >"$monitor_log" 2>&1
    monitor_status=$?
    set -e
  }

  for app_environment in staging production; do
    write_monitor_runtime "$app_environment"
    reset_monitor_fixture_logs
    run_monitor_fixture
    [ "$monitor_status" = 1 ] ||
      die "host monitor routing fixture returned status $monitor_status for $app_environment"
    assert_line "$environment_log" "$app_environment"
    [ -f "$invocation_log" ] || die "monitor-once was not reached for $app_environment"
    jq --exit-status --arg expected "$app_environment" \
      '.event == "gradex_monitor_failure" and .environment == $expected' \
      "$payload_log" >/dev/null ||
      die "monitor-once alert payload did not preserve $app_environment"
  done

  for expected_url in \
    https://monitor-routing.example/ \
    https://monitor-routing.example/healthz \
    https://monitor-routing.example/readyz; do
    grep --quiet --fixed-strings "$expected_url" "$curl_log" ||
      die "host monitor changed its public/health/readiness URL routing"
  done

  for app_environment in invalid ""; do
    write_monitor_runtime "$app_environment"
    reset_monitor_fixture_logs
    run_monitor_fixture
    [ "$monitor_status" != 0 ] ||
      die "host monitor accepted invalid APP_ENV=$app_environment"
    [ ! -e "$invocation_log" ] ||
      die "monitor-once ran before rejecting invalid APP_ENV=$app_environment"
    grep --quiet --fixed-strings \
      "APP_ENV must be exactly staging or production for monitoring; got \"$app_environment\"" \
      "$monitor_log" ||
      die "invalid APP_ENV=$app_environment was rejected without the monitoring identity error"
  done

  if grep --quiet --fixed-strings 'export GRADEX_ENVIRONMENT=staging' "$host_script"; then
    die "host.sh still hard-codes staging monitoring identity"
  fi
  if grep --quiet --fixed-strings 'monitor-test' "$host_script"; then
    die "host.sh selected the monitor-test environment"
  fi
  grep --quiet --fixed-strings 'monitor-test' "$real_monitor_once" ||
    die "monitor-once no longer contains its test-only monitor-test behavior"
  note "host monitor uses protected APP_ENV identity, rejects invalid values, preserves URLs, and leaves monitor-test test-only"
}

main() {
  local tool file operator group fake_bin backup_marker runtime_report curl_args_log monitor_log monitor_status now ca_file
  local staging_omitted staging_explicit production shared baseline_root baseline_render directory
  for tool in awk cat chmod cmp date git grep id jq mkdir mktemp rm stat systemd-analyze; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done

  for file in \
    gradex-monitor.service.in gradex-monitor.timer \
    gradex-backup.service.in gradex-backup.timer install.sh; do
    [ -f "$S12_SYSTEMD_DIR/$file" ] || die "$file is absent"
  done

  if "$S12_SYSTEMD_DIR/install.sh" install --user root --repo "$S12_ROOT" >/dev/null 2>&1; then
    die "the installer accepted root as the scheduled operator"
  fi
  if "$S12_SYSTEMD_DIR/install.sh" install --instance production \
    --user root --repo "$S12_ROOT" >/dev/null 2>&1; then
    die "the production installer accepted root as the scheduled operator"
  fi
  if grep --extended-regexp \
    'systemctl[[:space:]]+(enable|start|disable|stop|restart|try-restart)|--now' \
    "$S12_SYSTEMD_DIR/install.sh"; then
    die "the installer mutates scheduled work as a side effect"
  fi
  if grep --quiet --extended-regexp 'host\.sh[[:space:]]+(backup|monitor)' \
    "$S12_SYSTEMD_DIR/install.sh"; then
    die "the installer runs a backup or monitor job as a side effect"
  fi
  grep --quiet --fixed-strings \
    'install -o root -g root -m 0644 "${S12_UNIT_PATHS[@]}" "$S12_SYSTEMD_TARGET/"' \
    "$S12_SYSTEMD_DIR/install.sh" ||
    die "the installer does not use a filename-scoped unit selection"
  if grep --quiet --extended-regexp \
    'S12_TEMPORARY/\*\.(service|timer)' "$S12_SYSTEMD_DIR/install.sh"; then
    die "the installer uses an unscoped service/timer glob"
  fi
  if grep --quiet --extended-regexp -- '--(state-dir|project|env-file|unit-prefix)' \
    "$S12_SYSTEMD_DIR/install.sh"; then
    die "the installer exposes an operator-controlled instance override"
  fi
  for expectation in \
    'S12_HOST_STATE=/var/lib/gradex' \
    'S12_RUNTIME_ENV=/var/lib/gradex/runtime.env' \
    'S12_PROJECT=gradex-staging' \
    'S12_HOST_STATE=/home/deploy/gradex-production' \
    'S12_RUNTIME_ENV=/home/deploy/gradex-production/runtime.env' \
    'S12_PROJECT=gradex-production' \
    '[ -f "$S12_RUNTIME_ENV" ] || die "$S12_RUNTIME_ENV is absent"' \
    'case "$mode" in 400|600) ;; *) die "$S12_RUNTIME_ENV must have mode 0400 or 0600" ;; esac' \
    'owner_uid="$(stat -c '\''%u'\'' "$S12_RUNTIME_ENV")"' \
    'operator_uid="$(id -u "$S12_OPERATOR")"' \
    '[ "$owner_uid" = "$operator_uid" ] ||' \
    'runuser -u "$S12_OPERATOR" -- test -r "$S12_RUNTIME_ENV"' \
    'runuser -u "$S12_OPERATOR" -- test -x "$S12_REPO/deploy/hostinger/host.sh"' \
    'runuser -u "$S12_OPERATOR" -- docker info'; do
    grep --quiet --fixed-strings "$expectation" "$S12_SYSTEMD_DIR/install.sh" ||
      die "the installer is missing its instance/install-state contract: $expectation"
  done
  assert_line "$S12_ROOT/deploy/hostinger/host.sh" \
    'S12_HOST_STATE_DIR="${GRADEX_HOST_STATE_DIR:-/var/lib/gradex}"'
  assert_line "$S12_ROOT/deploy/hostinger/host.sh" \
    'S12_PROJECT_STAGING_DEFAULT="gradex-staging"'
  assert_line "$S12_ROOT/deploy/hostinger/host.sh" \
    'S12_PROJECT="${GRADEX_HOST_PROJECT:-$S12_PROJECT_STAGING_DEFAULT}"'
  assert_production_readme_contract

  S12_TEMPORARY="$(mktemp -d)"
  trap cleanup EXIT
  operator="$(id -un)"
  group="$(id -gn)"
  staging_omitted="$S12_TEMPORARY/staging-omitted"
  staging_explicit="$S12_TEMPORARY/staging-explicit"
  production="$S12_TEMPORARY/production"
  shared="$S12_TEMPORARY/shared"
  baseline_root="$S12_TEMPORARY/base"
  baseline_render="$S12_TEMPORARY/staging-baseline"
  mkdir -p \
    "$staging_omitted" "$staging_explicit" "$production" "$shared" \
    "$baseline_root/deploy/hostinger/systemd" "$baseline_render"
  if "$S12_SYSTEMD_DIR/install.sh" render \
    --output "$S12_TEMPORARY/invalid-repo" --user "$operator" --group "$group" \
    --repo "$S12_TEMPORARY/repo;unsafe" >/dev/null 2>&1; then
    die "the installer accepted an unsafe repository path"
  fi
  "$S12_SYSTEMD_DIR/install.sh" render \
    --output "$staging_omitted" --user "$operator" --group "$group" --repo "$S12_ROOT"
  "$S12_SYSTEMD_DIR/install.sh" render \
    --instance staging --output "$staging_explicit" \
    --user "$operator" --group "$group" --repo "$S12_ROOT"
  "$S12_SYSTEMD_DIR/install.sh" render \
    --instance production --output "$production" \
    --user "$operator" --group "$group" --repo "$S12_ROOT"

  for file in \
    gradex-monitor.service.in gradex-monitor.timer \
    gradex-backup.service.in gradex-backup.timer install.sh; do
    git show "$S12_BASE_COMMIT:deploy/hostinger/systemd/$file" \
      >"$baseline_root/deploy/hostinger/systemd/$file" ||
      die "the canonical staging baseline is unavailable for $file"
  done
  bash "$baseline_root/deploy/hostinger/systemd/install.sh" render \
    --output "$baseline_render" --user "$operator" --group "$group" --repo "$S12_ROOT"

  assert_unit_set "$staging_omitted" staging
  assert_unit_set "$staging_explicit" staging
  assert_unit_set "$production" production
  for file in \
    gradex-monitor.service gradex-monitor.timer \
    gradex-backup.service gradex-backup.timer; do
    cmp --silent "$staging_omitted/$file" "$staging_explicit/$file" ||
      die "omitted --instance does not byte-match --instance staging for $file"
  done
  for file in \
    gradex-monitor.service gradex-monitor.timer \
    gradex-backup.service gradex-backup.timer; do
    cmp --silent "$staging_omitted/$file" "$baseline_render/$file" ||
      die "the default staging render changed from canonical base $S12_BASE_COMMIT for $file"
  done

  for file in "$staging_omitted"/*; do
    assert_staging_environment_surface "$file"
  done
  assert_line "$staging_omitted/gradex-monitor.service" 'Type=oneshot'
  assert_line "$staging_omitted/gradex-monitor.service" "User=$operator"
  assert_line "$staging_omitted/gradex-monitor.service" "Group=$group"
  assert_line "$staging_omitted/gradex-monitor.service" "WorkingDirectory=$S12_ROOT"
  assert_line "$staging_omitted/gradex-monitor.service" "ExecStart=$S12_ROOT/deploy/hostinger/host.sh monitor"
  assert_line "$staging_omitted/gradex-monitor.service" 'TimeoutStartSec=120s'
  assert_line "$staging_omitted/gradex-monitor.service" 'UMask=0077'
  assert_line "$staging_omitted/gradex-monitor.service" 'NoNewPrivileges=true'
  assert_line "$staging_omitted/gradex-monitor.service" 'PrivateTmp=true'
  assert_line "$staging_omitted/gradex-monitor.service" 'ProtectSystem=full'
  assert_line "$staging_omitted/gradex-monitor.timer" 'OnCalendar=*:0/5'
  assert_line "$staging_omitted/gradex-monitor.timer" 'Persistent=true'
  assert_line "$staging_omitted/gradex-monitor.timer" 'Unit=gradex-monitor.service'
  assert_line "$staging_omitted/gradex-backup.service" 'Type=oneshot'
  assert_line "$staging_omitted/gradex-backup.service" 'TimeoutStartSec=360s'
  assert_line "$staging_omitted/gradex-backup.service" "User=$operator"
  assert_line "$staging_omitted/gradex-backup.service" "Group=$group"
  assert_line "$staging_omitted/gradex-backup.service" "WorkingDirectory=$S12_ROOT"
  assert_line "$staging_omitted/gradex-backup.service" "ExecStart=$S12_ROOT/deploy/hostinger/host.sh backup"
  assert_line "$staging_omitted/gradex-backup.timer" 'OnCalendar=hourly'
  assert_line "$staging_omitted/gradex-backup.timer" 'Persistent=true'
  assert_line "$staging_omitted/gradex-backup.timer" 'Unit=gradex-backup.service'
  assert_line "$staging_omitted/gradex-backup.service" 'UMask=0077'
  assert_line "$staging_omitted/gradex-backup.service" 'NoNewPrivileges=true'
  assert_line "$staging_omitted/gradex-backup.service" 'PrivateTmp=true'
  assert_line "$staging_omitted/gradex-backup.service" 'ProtectSystem=full'
  assert_line "$staging_omitted/gradex-backup.service" 'ReadWritePaths=/var/lib/gradex'
  assert_line "$staging_omitted/gradex-backup.service" 'RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6'

  for file in "$production"/*; do
    assert_no_environment_file "$file"
  done
  assert_no_environment_assignment "$production/gradex-production-monitor.timer"
  assert_no_environment_assignment "$production/gradex-production-backup.timer"
  assert_production_environment_surface "$production/gradex-production-monitor.service"
  assert_production_environment_surface "$production/gradex-production-backup.service"
  assert_line "$production/gradex-production-monitor.service" 'Type=oneshot'
  assert_line "$production/gradex-production-monitor.service" "User=$operator"
  assert_line "$production/gradex-production-monitor.service" "Group=$group"
  assert_line "$production/gradex-production-monitor.service" "WorkingDirectory=$S12_ROOT"
  assert_line "$production/gradex-production-monitor.service" "ExecStart=$S12_ROOT/deploy/hostinger/host.sh monitor"
  assert_line "$production/gradex-production-monitor.service" 'TimeoutStartSec=120s'
  assert_line "$production/gradex-production-monitor.service" 'UMask=0077'
  assert_line "$production/gradex-production-monitor.service" 'NoNewPrivileges=true'
  assert_line "$production/gradex-production-monitor.service" 'PrivateTmp=true'
  assert_line "$production/gradex-production-monitor.service" 'ProtectSystem=full'
  assert_line "$production/gradex-production-monitor.service" 'SyslogIdentifier=gradex-production-monitor'
  assert_line "$production/gradex-production-monitor.timer" 'OnCalendar=*:0/5'
  assert_line "$production/gradex-production-monitor.timer" 'Persistent=true'
  assert_line "$production/gradex-production-monitor.timer" 'Unit=gradex-production-monitor.service'
  assert_line "$production/gradex-production-backup.service" 'Type=oneshot'
  assert_line "$production/gradex-production-backup.service" 'TimeoutStartSec=360s'
  assert_line "$production/gradex-production-backup.service" "User=$operator"
  assert_line "$production/gradex-production-backup.service" "Group=$group"
  assert_line "$production/gradex-production-backup.service" "WorkingDirectory=$S12_ROOT"
  assert_line "$production/gradex-production-backup.service" "ExecStart=$S12_ROOT/deploy/hostinger/host.sh backup"
  assert_line "$production/gradex-production-backup.timer" 'OnCalendar=hourly'
  assert_line "$production/gradex-production-backup.timer" 'Persistent=true'
  assert_line "$production/gradex-production-backup.timer" 'Unit=gradex-production-backup.service'
  assert_line "$production/gradex-production-backup.service" 'UMask=0077'
  assert_line "$production/gradex-production-backup.service" 'NoNewPrivileges=true'
  assert_line "$production/gradex-production-backup.service" 'PrivateTmp=true'
  assert_line "$production/gradex-production-backup.service" 'ProtectSystem=full'
  assert_line "$production/gradex-production-backup.service" 'ReadWritePaths=/home/deploy/gradex-production'
  assert_line "$production/gradex-production-backup.service" 'RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6'

  "$S12_SYSTEMD_DIR/install.sh" render \
    --instance staging --output "$shared" \
    --user "$operator" --group "$group" --repo "$S12_ROOT"
  for file in \
    gradex-monitor.service gradex-monitor.timer \
    gradex-backup.service gradex-backup.timer; do
    cmp --silent "$shared/$file" "$staging_explicit/$file" ||
      die "the shared render changed the staging $file before production rendering"
  done
  "$S12_SYSTEMD_DIR/install.sh" render \
    --instance production --output "$shared" \
    --user "$operator" --group "$group" --repo "$S12_ROOT"
  for file in \
    gradex-monitor.service gradex-monitor.timer \
    gradex-backup.service gradex-backup.timer; do
    cmp --silent "$shared/$file" "$staging_explicit/$file" ||
      die "production rendering changed the staging $file in a shared output"
  done
  for file in "$production"/*; do
    [ ! -e "$staging_explicit/${file##*/}" ] ||
      die "staging and production renders overlap on ${file##*/}"
  done

  if "$S12_SYSTEMD_DIR/install.sh" render --instance acceptance \
    --output "$S12_TEMPORARY/invalid-instance" >/dev/null 2>&1; then
    die "the installer accepted an invalid instance"
  fi

  grep --quiet --fixed-strings 'GRADEX_BACKUP_MAX_AGE_SECONDS="${GRADEX_BACKUP_MAX_AGE_SECONDS:-7200}"' \
    "$S12_ROOT/deploy/monitoring/monitor-once.sh" ||
    die "monitor default backup freshness is not two hours"
  assert_line "$S12_ROOT/deploy/monitoring/monitor.env.example" 'GRADEX_BACKUP_MAX_AGE_SECONDS=7200'
  assert_line "$S12_ROOT/deploy/monitoring/monitor.env.example" 'GRADEX_BACKUP_MAX_FUTURE_SECONDS=300'
  grep --quiet --fixed-strings 'maximum_age_seconds: 7200' "$S12_ROOT/deploy/monitoring/rules.yml" ||
    die "monitoring rules do not match the two-hour freshness contract"
  grep --quiet --fixed-strings 'stale_due_seconds: 3600' "$S12_ROOT/deploy/monitoring/rules.yml" ||
    die "monitoring rules do not match the one-hour email staleness contract"
  grep --quiet --fixed-strings 'warning_used_percent: 85' "$S12_ROOT/deploy/monitoring/rules.yml" ||
    die "monitoring rules do not match the disk warning contract"
  grep --quiet --fixed-strings 'critical_used_percent: 95' "$S12_ROOT/deploy/monitoring/rules.yml" ||
    die "monitoring rules do not match the disk critical contract"
  grep --quiet --fixed-strings 'maximum_future_seconds: 300' "$S12_ROOT/deploy/monitoring/rules.yml" ||
    die "monitoring rules do not match the backup future-clock tolerance"
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_BACKUP_MAX_AGE_SECONDS=7200'
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_BACKUP_MAX_FUTURE_SECONDS=300'
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_MONITOR_COMPOSE_PROJECT='
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_MONITOR_WORKER_CONTAINER='
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_MONITOR_POSTGRES_CONTAINER='
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_MONITOR_API_CONTAINER='
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_MONITOR_EMAIL_STALE_SECONDS=3600'
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_MONITOR_DISK_WARN_PERCENT=85'
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_MONITOR_DISK_CRITICAL_PERCENT=95'
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_MONITOR_DISK_MIN_FREE_BYTES=5368709120'
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_MONITOR_CA_FILE='
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_ALERT_RESEND_API_KEY='
  assert_line "$S12_ROOT/deploy/hostinger/runtime.env.example" 'GRADEX_ALERT_EMAIL_TO='
  grep --quiet --fixed-strings 'optional_environment: GRADEX_ALERT_WEBHOOK_URL' \
    "$S12_ROOT/deploy/monitoring/rules.yml" ||
    die "monitoring rules do not describe the optional webhook accurately"
  grep --quiet --fixed-strings 'type: resend_email' "$S12_ROOT/deploy/monitoring/rules.yml" ||
    die "monitoring rules do not describe the optional Resend destination"
  grep --quiet --fixed-strings 'endpoint: https://api.resend.com/emails' "$S12_ROOT/deploy/monitoring/rules.yml" ||
    die "monitoring rules do not document the Resend endpoint"
  grep --quiet --fixed-strings 'optional_bearer_secret: GRADEX_ALERT_WEBHOOK_TOKEN' \
    "$S12_ROOT/deploy/monitoring/rules.yml" ||
    die "monitoring rules do not describe the optional bearer credential accurately"

  fake_bin="$S12_TEMPORARY/fake-bin"
  backup_marker="$S12_TEMPORARY/latest.completed-at"
  runtime_report="$S12_TEMPORARY/runtime-report"
  curl_args_log="$S12_TEMPORARY/curl.args"
  monitor_log="$S12_TEMPORARY/monitor.log"
  mkdir -p "$fake_bin"
  cat >"$fake_bin/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
output=""
write_status=0
data=""
[ -z "${FAKE_CURL_ARGS_LOG:-}" ] || printf '%s\n' "$@" >>"$FAKE_CURL_ARGS_LOG"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) output="$2"; shift 2 ;;
    --write-out) write_status=1; shift 2 ;;
    --data) data="$2"; shift 2 ;;
    *) shift ;;
  esac
done
[ -z "$output" ] || : >"$output"
[ -z "${FAKE_CURL_DATA_LOG:-}" ] || [ -z "$data" ] || printf '%s\n' "$data" >"$FAKE_CURL_DATA_LOG"
[ "$write_status" = 0 ] || printf '200'
[ "$write_status" = 1 ] || [ "${FAKE_CURL_FAIL:-0}" != 1 ] || exit 22
EOF
  chmod 0755 "$fake_bin/curl"
  verify_host_monitor_environment
  printf 'version=1\nworker|PASS|fixture worker\npostgres_schema|PASS|fixture PostgreSQL schema\nemail_outbox|METRICS|terminal=0;oldest_due_age=-1;stale_lease_age=-1\ndisk_roots|PASS|fixture paths\n' >"$runtime_report"
  chmod 600 "$runtime_report"

  now="$(date +%s)"
  printf '%s\n' "$((now - 7199))" >"$backup_marker"
  PATH="$fake_bin:$PATH" \
    GRADEX_HEALTH_URL=https://health.example \
    GRADEX_READY_URL=https://ready.example \
    GRADEX_PUBLIC_URL=https://staging.example/ \
    GRADEX_ENVIRONMENT=systemd-proof \
    GRADEX_BACKUP_COMPLETED_AT_FILE="$backup_marker" \
    GRADEX_BACKUP_MAX_AGE_SECONDS= \
    GRADEX_MONITOR_RUNTIME_REPORT="$runtime_report" \
    GRADEX_MONITOR_DISK_PATHS=/tmp \
    GRADEX_MONITOR_DISK_MIN_FREE_BYTES=1 \
    "$S12_ROOT/deploy/monitoring/monitor-once.sh" >"$monitor_log" 2>&1 ||
    die "a backup younger than two hours was reported stale"

  printf '%s\n' "$((now - 7201))" >"$backup_marker"
  set +e
  PATH="$fake_bin:$PATH" \
    GRADEX_HEALTH_URL=https://health.example \
    GRADEX_READY_URL=https://ready.example \
    GRADEX_PUBLIC_URL=https://staging.example/ \
    GRADEX_ENVIRONMENT=systemd-proof \
    GRADEX_BACKUP_COMPLETED_AT_FILE="$backup_marker" \
    GRADEX_BACKUP_MAX_AGE_SECONDS= \
    GRADEX_MONITOR_RUNTIME_REPORT="$runtime_report" \
    GRADEX_MONITOR_DISK_PATHS=/tmp \
    GRADEX_MONITOR_DISK_MIN_FREE_BYTES=1 \
    GRADEX_ALERT_WEBHOOK_URL=https://alerts.example/systemd-proof-url-secret-sentinel \
    GRADEX_ALERT_WEBHOOK_TOKEN=systemd-proof-secret-sentinel \
    GRADEX_ALERT_WEBHOOK_TOKEN=proofcredential \
    FAKE_CURL_ARGS_LOG="$curl_args_log" \
    "$S12_ROOT/deploy/monitoring/monitor-once.sh" >"$monitor_log" 2>&1
  monitor_status=$?
  set -e
  [ "$monitor_status" = 1 ] || die "a backup older than two hours did not fail monitoring"
  grep --quiet --fixed-strings 'alert delivery succeeded' "$monitor_log" ||
    die "the stale-backup path did not attempt alert delivery"
  if grep --quiet --fixed-strings 'systemd-proof-secret-sentinel' "$curl_args_log"; then
    die "the alert bearer credential reached curl command arguments"
  fi
  if grep --quiet --fixed-strings 'systemd-proof-url-secret-sentinel' "$curl_args_log"; then
    die "the alert webhook URL reached curl command arguments"
  fi
  if grep --quiet --fixed-strings 'proofcredential' "$curl_args_log"; then
    die "the alert bearer credential reached curl command arguments"
  fi
  if grep --quiet --fixed-strings 'systemd-proof-secret-sentinel' "$monitor_log" ||
    grep --quiet --fixed-strings 'systemd-proof-url-secret-sentinel' "$monitor_log"; then
    die "the monitor log exposed an alert credential"
  fi
  if grep --quiet --fixed-strings 'proofcredential' "$monitor_log"; then
    die "the monitor log exposed an alert bearer credential"
  fi

  ca_file="$S12_TEMPORARY/monitor-ca.pem"
  printf 'fixture monitor CA\n' >"$ca_file"
  : >"$curl_args_log"
  set +e
  PATH="$fake_bin:$PATH" \
    GRADEX_PUBLIC_URL=https://staging.example/ \
    GRADEX_HEALTH_URL=https://health.example \
    GRADEX_READY_URL=https://ready.example \
    GRADEX_ENVIRONMENT=systemd-proof \
    GRADEX_BACKUP_COMPLETED_AT_FILE="$backup_marker" \
    GRADEX_MONITOR_RUNTIME_REPORT="$runtime_report" \
    GRADEX_MONITOR_DISK_PATHS=/tmp \
    GRADEX_MONITOR_DISK_MIN_FREE_BYTES=1 \
    GRADEX_MONITOR_CA_FILE="$ca_file" \
    GRADEX_ALERT_WEBHOOK_URL=https://alerts.example/systemd-proof-url-secret-sentinel \
    GRADEX_ALERT_WEBHOOK_TOKEN=proofcredential \
    FAKE_CURL_FAIL=1 \
    FAKE_CURL_ARGS_LOG="$curl_args_log" \
    "$S12_ROOT/deploy/monitoring/monitor-once.sh" >"$monitor_log" 2>&1
  monitor_status=$?
  set -e
  [ "$monitor_status" = 1 ] || die "a rejected webhook response did not fail monitoring (status=$monitor_status)"
  grep --quiet --fixed-strings 'alert delivery failed' "$monitor_log" ||
    die "webhook delivery failure was not distinguishable"
  [ "$(grep -c --fixed-strings -- '--cacert' "$curl_args_log")" = 4 ] ||
    die "custom monitoring CA was not supplied to every HTTPS probe and webhook request"
  if grep --quiet --fixed-strings 'systemd-proof-secret-sentinel' "$monitor_log" ||
    grep --quiet --fixed-strings 'systemd-proof-url-secret-sentinel' "$monitor_log"; then
    die "failed webhook delivery exposed an alert credential"
  fi
  if grep --quiet --fixed-strings 'proofcredential' "$monitor_log"; then
    die "failed webhook delivery exposed an alert bearer credential"
  fi

  set +e
  PATH="$fake_bin:$PATH" \
    GRADEX_PUBLIC_URL=https://staging.example/ \
    GRADEX_HEALTH_URL=https://health.example \
    GRADEX_READY_URL=https://ready.example \
    GRADEX_ENVIRONMENT=systemd-proof \
    GRADEX_MONITOR_RUNTIME_REPORT="$runtime_report" \
    GRADEX_MONITOR_DISK_PATHS=/tmp \
    GRADEX_MONITOR_DISK_MIN_FREE_BYTES=1 \
    GRADEX_ALERT_WEBHOOK_URL=http://alerts.example/insecure \
    "$S12_ROOT/deploy/monitoring/monitor-once.sh" >"$monitor_log" 2>&1
  monitor_status=$?
  set -e
  [ "$monitor_status" = 2 ] || die "an insecure webhook URL was accepted outside monitor-test"
  grep --quiet --fixed-strings 'GRADEX_ALERT_WEBHOOK_URL must be an HTTPS URL outside monitor-test' "$monitor_log" ||
    die "insecure webhook rejection was not reported"

  for directory in "$staging_omitted" "$staging_explicit" "$production"; do
    systemd-analyze verify "$directory"/*.service "$directory"/*.timer
  done

  note "staging and production rendering, cadence, entrypoints, persistence, secret isolation, freshness, filename isolation, and unit syntax passed"
}

main "$@"
