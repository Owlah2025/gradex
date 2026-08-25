#!/usr/bin/env bash

set -euo pipefail

S12_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
S12_K6_IMAGE="grafana/k6:0.55.0"

note() { printf 'gradex-loadtest: %s\n' "$*" >&2; }
die() { note "$*"; exit 1; }
require_value() { [ -n "${!1:-}" ] || die "$1 is required"; }

main() {
  [ "$#" = 1 ] || die "usage: run.sh SCENARIO"
  local scenario="$1" target fixture_dir results_dir run_id result_file status profile_name profile_file repetition script upload_fixture
  local -a docker_args
  case "$scenario" in
    api-surge|login-surge|playback-surge|mixed-student-sustained|mixed-student-burst|public-catalogue|privileged-operators|upload-contention) ;;
    *) die "unsupported scenario" ;;
  esac
  for tool in docker id mkdir readlink stat; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done
  for name in GRADEX_LOADTEST_TARGET_URL GRADEX_LOADTEST_FIXTURE_DIR \
    GRADEX_LOADTEST_RESULTS_DIR GRADEX_LOADTEST_RUN_ID; do
    require_value "$name"
  done
  if [ "$scenario" = login-surge ]; then
    require_value GRADEX_LOADTEST_PASSWORD
  fi

  profile_name="${GRADEX_LOADTEST_PROFILE:-}"
  case "$profile_name" in
    "")
      script=/work/loadtest.mjs
      case "$scenario" in api-surge|login-surge|playback-surge) ;; *) die "beta scenarios require GRADEX_LOADTEST_PROFILE=limited-paid-beta" ;; esac
      ;;
    limited-paid-beta)
      script=/work/beta-loadtest.mjs
      case "$scenario" in mixed-student-sustained|mixed-student-burst|public-catalogue|login-surge|playback-surge|privileged-operators|upload-contention) ;; *) die "unsupported limited-paid-beta scenario" ;; esac
      for name in GRADEX_LOADTEST_PROFILE_FILE GRADEX_LOADTEST_REPETITION \
        GRADEX_LOADTEST_RELEASE_ID GRADEX_LOADTEST_CONTAINER_IMAGE_ID \
        GRADEX_LOADTEST_COMPOSE_PROJECT GRADEX_LOADTEST_HOST_CLASS \
        GRADEX_LOADTEST_STORAGE_PROVIDER; do
        require_value "$name"
      done
      repetition="$GRADEX_LOADTEST_REPETITION"
      [[ "$repetition" =~ ^[12]$ ]] || die "GRADEX_LOADTEST_REPETITION must be 1 or 2"
      profile_file="$(readlink -f -- "$GRADEX_LOADTEST_PROFILE_FILE")"
      case "$profile_file" in "$S12_ROOT/deploy/loadtest"/*) ;; *) die "beta profile must be inside deploy/loadtest" ;; esac
      [ -f "$profile_file" ] || die "beta profile file is absent"
      ;;
    *) die "unsupported GRADEX_LOADTEST_PROFILE" ;;
  esac

  target="$GRADEX_LOADTEST_TARGET_URL"
  case "$target" in
    http://127.0.0.1:*|http://localhost:*|http://\[::1\]:*)
      [ "${GRADEX_LOADTEST_ALLOW_INSECURE_LOCAL:-}" = 1 ] ||
        die "HTTP is accepted only for an explicitly acknowledged local smoke"
      ;;
    https://127.0.0.1:*|https://localhost:*|https://\[::1\]:*) ;;
    https://*)
      [ "${GRADEX_LOADTEST_ALLOW_REMOTE:-}" = I_UNDERSTAND_THIS_GENERATES_REMOTE_LOAD ] ||
        die "remote load requires GRADEX_LOADTEST_ALLOW_REMOTE=I_UNDERSTAND_THIS_GENERATES_REMOTE_LOAD"
      [ "${GRADEX_LOADTEST_EXTERNAL_GENERATOR:-}" = 1 ] ||
        die "remote load must be launched from a separate load generator"
      ;;
    *) die "target must be a credential-free HTTPS origin or explicitly acknowledged local HTTP origin" ;;
  esac
  case "$target" in */) target="${target%/}" ;; esac
  case "$target" in *'/'*'/'*'/'*) die "target must not contain a path, query, or fragment" ;; esac
  export GRADEX_LOADTEST_TARGET_URL="$target"

  run_id="$GRADEX_LOADTEST_RUN_ID"
  [[ "$run_id" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{2,63}$ ]] || die "GRADEX_LOADTEST_RUN_ID is invalid"
  fixture_dir="$(readlink -f -- "$GRADEX_LOADTEST_FIXTURE_DIR")"
  results_dir="$(readlink -m -- "$GRADEX_LOADTEST_RESULTS_DIR")"
  [ "$fixture_dir" != / ] && [ "$results_dir" != / ] || die "fixture and result directories cannot be root"
  [ -f "$fixture_dir/fixture.json" ] || die "fixture.json is absent"
  if [ "$scenario" != login-surge ]; then
    [ -f "$fixture_dir/sessions.json" ] || die "sessions.json is absent"
    [ "$(stat -c '%a' "$fixture_dir/sessions.json")" = 600 ] || die "sessions.json must have mode 0600"
  fi
  mkdir -p -- "$results_dir"
  chmod 700 "$results_dir"
  result_file="$results_dir/${scenario}-${run_id}.json"
  [ ! -e "$result_file" ] && [ ! -L "$result_file" ] || die "result identity already exists"

  export GRADEX_LOADTEST_SCENARIO="$scenario"
  export GRADEX_LOADTEST_FIXTURE_FILE=/fixtures/fixture.json
  export GRADEX_LOADTEST_SESSION_FILE=/fixtures/sessions.json
  export GRADEX_LOADTEST_RESULT_FILE="/results/${scenario}-${run_id}.json"
  export K6_NO_USAGE_REPORT=true
  if [ "$profile_name" = limited-paid-beta ]; then
    export GRADEX_LOADTEST_PROFILE_FILE="/work/$(basename "$profile_file")"
    export GRADEX_LOADTEST_PROFILE=limited-paid-beta
  fi

  if [ "$profile_name" = limited-paid-beta ] && [ "$scenario" = upload-contention ]; then
    require_value GRADEX_LOADTEST_UPLOAD_FIXTURE_FILE
    require_value GRADEX_LOADTEST_UPLOAD_SHA256_HEX
    upload_fixture="$(readlink -f -- "$GRADEX_LOADTEST_UPLOAD_FIXTURE_FILE")"
    [ -f "$upload_fixture" ] || die "upload fixture is absent"
  fi

  docker_args=(run --rm --read-only --tmpfs /tmp:rw,noexec,nosuid,size=64m --network host
    --user "$(id -u):$(id -g)"
    --volume "$S12_ROOT/deploy/loadtest:/work:ro"
    --volume "$fixture_dir:/fixtures:ro"
    --volume "$results_dir:/results"
    --env GRADEX_LOADTEST_SCENARIO --env GRADEX_LOADTEST_TARGET_URL
    --env GRADEX_LOADTEST_FIXTURE_FILE --env GRADEX_LOADTEST_SESSION_FILE
    --env GRADEX_LOADTEST_RESULT_FILE --env GRADEX_LOADTEST_SMOKE
    --env GRADEX_LOADTEST_PROFILE_API_RATE --env GRADEX_LOADTEST_PROFILE_LOGIN_COUNT
    --env K6_NO_USAGE_REPORT)
  if [ "$profile_name" = limited-paid-beta ]; then
    docker_args+=(--env GRADEX_LOADTEST_PROFILE --env GRADEX_LOADTEST_PROFILE_FILE --env GRADEX_LOADTEST_REPETITION
      --env GRADEX_LOADTEST_RELEASE_ID --env GRADEX_LOADTEST_CONTAINER_IMAGE_ID --env GRADEX_LOADTEST_COMPOSE_PROJECT
      --env GRADEX_LOADTEST_HOST_CLASS --env GRADEX_LOADTEST_STORAGE_PROVIDER)
    if [ "$scenario" = upload-contention ]; then
      docker_args+=(--volume "$upload_fixture:/upload-fixture:ro" --env GRADEX_LOADTEST_UPLOAD_FIXTURE_FILE=/upload-fixture
        --env GRADEX_LOADTEST_UPLOAD_SHA256_HEX)
    fi
  fi
  if [ "$scenario" = login-surge ]; then
    docker_args+=(--env GRADEX_LOADTEST_PASSWORD)
  fi
  if [ -n "${GRADEX_LOADTEST_CA_FILE:-}" ]; then
    [ -s "$GRADEX_LOADTEST_CA_FILE" ] || die "GRADEX_LOADTEST_CA_FILE is unreadable"
    docker_args+=(--volume "$GRADEX_LOADTEST_CA_FILE:/certs/ca.crt:ro" --env SSL_CERT_FILE=/certs/ca.crt)
  fi

  set +e
  docker "${docker_args[@]}" "$S12_K6_IMAGE" run "$script"
  status=$?
  set -e
  [ -f "$result_file" ] || die "k6 did not write the machine-readable result"
  chmod 600 "$result_file"
  if [ "$status" -ne 0 ]; then
    note "$scenario failed its correctness, capacity, or canonical latency thresholds"
    return "$status"
  fi
  note "$scenario passed; aggregate evidence is in $result_file"
}

main "$@"
