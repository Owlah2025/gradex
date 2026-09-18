#!/usr/bin/env bash
#
# Student UI preview stack — DEVELOPMENT ONLY.
#
# Brings up a long-lived local stack so Student-facing screens can be reviewed
# interactively in a browser. It reuses the repository's real seeder
# (`backend/cmd/e2e-seed`, the same one the Playwright harness compiles) rather
# than introducing a second fixture architecture: the data below is the data the
# E2E suite asserts against, so a screen that looks right here is looking at
# rows the product actually produces.
#
# What this is NOT:
#   - It is not a production or production-like topology. See
#     `deploy/compose/compose.student-preview.yml` for the specific relaxations.
#   - It does not inject fake data into application code. Every row is written
#     into PostgreSQL by the seeder and read back through the real Go API.
#   - It never touches another worktree's containers. The Compose project name
#     is pinned below and every port is loopback-bound in a dedicated range.
#
# Usage:
#   scripts/student-preview.sh up       # infra + seed + API + worker
#   scripts/student-preview.sh seed     # re-seed (drops and recreates the DB)
#   scripts/student-preview.sh session  # mint a signed-in Student cookie
#   scripts/student-preview.sh status   # what is running, and where
#   scripts/student-preview.sh logs     # tail the API log
#   scripts/student-preview.sh down     # stop everything this script started
#
# The frontend dev server is deliberately started separately, so that HMR can be
# restarted without tearing down the backing stack:
#   cd frontend && GRADEX_API_ORIGIN=http://127.0.0.1:58080 npm run dev

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKEND_DIR="${REPO_ROOT}/backend"
COMPOSE_FILE="${REPO_ROOT}/deploy/compose/compose.student-preview.yml"
COMPOSE_PROJECT="gradex-student-preview"

# Run state lives outside the worktree so that `git status` stays clean and a
# stray `git clean` cannot orphan running processes.
STATE_DIR="/var/tmp/gradex-student-preview"
API_BIN="${STATE_DIR}/api"
WORKER_BIN="${STATE_DIR}/worker"
SEED_BIN="${STATE_DIR}/e2e-seed"
API_PID_FILE="${STATE_DIR}/api.pid"
WORKER_PID_FILE="${STATE_DIR}/worker.pid"
API_LOG="${STATE_DIR}/api.log"
WORKER_LOG="${STATE_DIR}/worker.log"

# Dedicated loopback ports. Chosen well away from 5432/6379/8080/3000 so this
# stack can coexist with anything else on the host.
PG_PORT=55432
REDIS_PORT=56379
MINIO_PORT=59000
API_PORT=58080
FRONTEND_PORT=3000

# The seeder's safety gate (backend/internal/db/e2esafety) refuses any database
# whose name does not match ^gradex_playwright_e2e_[a-z0-9]{8,24}$, and refuses
# the protected `gradex`/`postgres` names outright. This name is stable rather
# than per-run because the stack is long-lived and the browser session should
# survive a re-seed.
DB_NAME="gradex_playwright_e2e_studentui01"
ADMIN_DSN="postgres://gradex:gradex@localhost:${PG_PORT}/postgres?sslmode=disable"
TARGET_DSN="postgres://gradex:gradex@localhost:${PG_PORT}/${DB_NAME}?sslmode=disable"

# The nominal application DSN, passed to the seeder as DATABASE_URL.
#
# The safety gate compares the reset target against this and refuses when they
# match, so that a mistyped target can never drop the real application database.
# Passing the target here defeats that check by making the two identical — so
# this names the application database, which this stack deliberately never
# creates.
APP_DSN="postgres://gradex:gradex@localhost:${PG_PORT}/gradex?sslmode=disable"

PUBLIC_ORIGIN="http://localhost:${FRONTEND_PORT}"

compose() {
  docker compose -p "${COMPOSE_PROJECT}" -f "${COMPOSE_FILE}" "$@"
}

# Development fixture keys for a throwaway loopback database.
#
# These are not secrets and must never be reused anywhere else. They exist
# because the API's configuration loader is fail-closed: it refuses to start
# without them rather than silently generating a weak default, which is the
# behaviour we want in production and therefore the behaviour we keep here.
api_env() {
  cat <<ENV
DATABASE_URL=${TARGET_DSN}
APP_ENV=development
REDIS_ADDR=127.0.0.1:${REDIS_PORT}
S3_ENDPOINT=http://127.0.0.1:${MINIO_PORT}
S3_BUCKET=gradex-test
S3_USE_PATH_STYLE=true
S3_ACCESS_KEY=gradexminio
S3_SECRET_KEY=gradexminio
AUTH_FAKE_MODE=false
STUDENT_REGISTRATION_ENABLED=true
REGISTRATION_POLICY_SET_ID=student-preview-v1
PASSWORD_SCREEN_MODE=deterministic
SESSION_CSRF_KEY=0123456789abcdef0123456789abcdef
ANONYMOUS_COOKIE_SIGNING_KEY=1123456789abcdef0123456789abcdef
ANONYMOUS_CSRF_KEY=2123456789abcdef0123456789abcdef
IDENTITY_OTP_PEPPER=3123456789abcdef0123456789abcdef
ADMISSION_LIMITER_HMAC_KEY=4123456789abcdef0123456789abcdef
PLAYBACK_TOKEN_SECRET=5123456789abcdef0123456789abcdef
OUTBOX_PROTECTED_PAYLOAD_KEY=6123456789abcdef0123456789abcdef
OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION=student-preview-v1
PUBLIC_ORIGIN=${PUBLIC_ORIGIN}
CORS_ALLOWED_ORIGINS=${PUBLIC_ORIGIN}
CORS_ALLOW_CREDENTIALS=true
ENV
}

run_with_api_env() {
  local role="$1"; shift
  local -a envs=()
  while IFS= read -r line; do
    [ -n "${line}" ] && envs+=("${line}")
  done < <(api_env)
  env "${envs[@]}" SERVICE_ROLE="${role}" "$@"
}

wait_for_http() {
  local url="$1" label="$2" deadline=$((SECONDS + 90))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if curl -fsS "${url}" >/dev/null 2>&1; then
      echo "  ${label} is up"
      return 0
    fi
    sleep 1
  done
  echo "ERROR: timed out waiting for ${label} at ${url}" >&2
  return 1
}

process_alive() {
  local pid_file="$1"
  [ -f "${pid_file}" ] || return 1
  local pid; pid="$(cat "${pid_file}" 2>/dev/null || true)"
  [ -n "${pid}" ] || return 1
  kill -0 "${pid}" 2>/dev/null
}

# Only ever signals a PID this script wrote, and only after confirming the
# process is still the executable we launched. A recycled PID belonging to
# something else must not be killed.
stop_tracked_process() {
  local pid_file="$1" expected_bin="$2" label="$3"
  process_alive "${pid_file}" || { rm -f "${pid_file}"; return 0; }
  local pid; pid="$(cat "${pid_file}")"
  local actual; actual="$(readlink -f "/proc/${pid}/exe" 2>/dev/null || true)"
  if [ "${actual}" != "$(readlink -f "${expected_bin}" 2>/dev/null)" ]; then
    echo "  refusing to signal PID ${pid}: it is no longer ${label}"
    rm -f "${pid_file}"
    return 0
  fi
  kill "${pid}" 2>/dev/null || true
  for _ in $(seq 1 20); do
    kill -0 "${pid}" 2>/dev/null || break
    sleep 0.25
  done
  kill -9 "${pid}" 2>/dev/null || true
  rm -f "${pid_file}"
  echo "  stopped ${label}"
}

cmd_build() {
  mkdir -p "${STATE_DIR}"
  echo "Compiling API, worker and seeder from the working tree..."
  (cd "${BACKEND_DIR}" && go build -o "${API_BIN}" ./cmd/api)
  (cd "${BACKEND_DIR}" && go build -o "${WORKER_BIN}" ./cmd/worker)
  # The seeder has no main.go by design: it is a test binary, so that the
  # fixture helpers can live behind `//go:build !production` and never link
  # into a shipped artifact.
  (cd "${BACKEND_DIR}" && go test -c -o "${SEED_BIN}" ./cmd/e2e-seed)
}

cmd_infra() {
  echo "Starting isolated infrastructure (project ${COMPOSE_PROJECT})..."
  compose up -d --wait
  echo "  postgres 127.0.0.1:${PG_PORT}  redis 127.0.0.1:${REDIS_PORT}  minio 127.0.0.1:${MINIO_PORT}"
}

cmd_seed() {
  [ -x "${SEED_BIN}" ] || cmd_build
  echo "Seeding ${DB_NAME}..."
  (cd "${BACKEND_DIR}" && env \
    GRADEX_E2E_ADMIN_DB_URL="${ADMIN_DSN}" \
    GRADEX_E2E_TARGET_DB_NAME="${DB_NAME}" \
    GRADEX_E2E_TARGET_DB_URL="${TARGET_DSN}" \
    GRADEX_E2E_ALLOW_DATABASE_RESET=1 \
    DATABASE_URL="${APP_DSN}" \
    APP_ENV=development \
    SERVICE_ROLE=api \
    REDIS_ADDR="127.0.0.1:${REDIS_PORT}" \
    S3_ENDPOINT="http://127.0.0.1:${MINIO_PORT}" \
    S3_BUCKET=gradex-test \
    S3_ACCESS_KEY=gradexminio \
    S3_SECRET_KEY=gradexminio \
    AUTH_FAKE_MODE=false \
    STUDENT_REGISTRATION_ENABLED=true \
    REGISTRATION_POLICY_SET_ID=student-preview-v1 \
    PASSWORD_SCREEN_MODE=deterministic \
    SESSION_CSRF_KEY=0123456789abcdef0123456789abcdef \
    ANONYMOUS_COOKIE_SIGNING_KEY=1123456789abcdef0123456789abcdef \
    ANONYMOUS_CSRF_KEY=2123456789abcdef0123456789abcdef \
    IDENTITY_OTP_PEPPER=3123456789abcdef0123456789abcdef \
    ADMISSION_LIMITER_HMAC_KEY=4123456789abcdef0123456789abcdef \
    PLAYBACK_TOKEN_SECRET=5123456789abcdef0123456789abcdef \
    OUTBOX_PROTECTED_PAYLOAD_KEY=6123456789abcdef0123456789abcdef \
    OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION=student-preview-v1 \
    PUBLIC_ORIGIN="${PUBLIC_ORIGIN}" \
    CORS_ALLOWED_ORIGINS="${PUBLIC_ORIGIN}" \
    CORS_ALLOW_CREDENTIALS=true \
    "${SEED_BIN}")
  echo "  seeded"
}

# Mints a real session for a seeded Student and prints its cookie.
#
# The session is issued by the same production code path the API uses — it is
# not a bypass, and AUTH_FAKE_MODE stays off. This exists so a browser can be
# put into a signed-in Student state for visual review without a human typing
# credentials into a form on every restart.
#
# Usage: scripts/student-preview.sh session [email] [device-slot]
cmd_session() {
  local email="${1:-student1@example.test}"
  local slot="${2:-0}"
  [ -x "${SEED_BIN}" ] || cmd_build
  (cd "${BACKEND_DIR}" && env \
    GRADEX_E2E_ADMIN_DB_URL="${ADMIN_DSN}" \
    GRADEX_E2E_TARGET_DB_NAME="${DB_NAME}" \
    GRADEX_E2E_TARGET_DB_URL="${TARGET_DSN}" \
    GRADEX_E2E_ALLOW_DATABASE_RESET=1 \
    DATABASE_URL="${APP_DSN}" \
    APP_ENV=development \
    SERVICE_ROLE=api \
    REDIS_ADDR="127.0.0.1:${REDIS_PORT}" \
    S3_ENDPOINT="http://127.0.0.1:${MINIO_PORT}" \
    S3_BUCKET=gradex-test \
    S3_ACCESS_KEY=gradexminio \
    S3_SECRET_KEY=gradexminio \
    AUTH_FAKE_MODE=false \
    STUDENT_REGISTRATION_ENABLED=true \
    REGISTRATION_POLICY_SET_ID=student-preview-v1 \
    PASSWORD_SCREEN_MODE=deterministic \
    SESSION_CSRF_KEY=0123456789abcdef0123456789abcdef \
    ANONYMOUS_COOKIE_SIGNING_KEY=1123456789abcdef0123456789abcdef \
    ANONYMOUS_CSRF_KEY=2123456789abcdef0123456789abcdef \
    IDENTITY_OTP_PEPPER=3123456789abcdef0123456789abcdef \
    ADMISSION_LIMITER_HMAC_KEY=4123456789abcdef0123456789abcdef \
    PLAYBACK_TOKEN_SECRET=5123456789abcdef0123456789abcdef \
    OUTBOX_PROTECTED_PAYLOAD_KEY=6123456789abcdef0123456789abcdef \
    OUTBOX_PROTECTED_PAYLOAD_KEY_VERSION=student-preview-v1 \
    PUBLIC_ORIGIN="${PUBLIC_ORIGIN}" \
    CORS_ALLOWED_ORIGINS="${PUBLIC_ORIGIN}" \
    CORS_ALLOW_CREDENTIALS=true \
    "${SEED_BIN}" -issue-session -email "${email}" -device-slot "${slot}")
}

cmd_backend() {
  mkdir -p "${STATE_DIR}"
  if process_alive "${API_PID_FILE}"; then
    echo "  API already running (PID $(cat "${API_PID_FILE}"))"
  else
    echo "Starting Go API on 127.0.0.1:${API_PORT}..."
    run_with_api_env api PORT="${API_PORT}" nohup "${API_BIN}" >"${API_LOG}" 2>&1 &
    echo $! >"${API_PID_FILE}"
    wait_for_http "http://127.0.0.1:${API_PORT}/readyz" "API"
  fi
  if process_alive "${WORKER_PID_FILE}"; then
    echo "  worker already running (PID $(cat "${WORKER_PID_FILE}"))"
  else
    echo "Starting media worker..."
    run_with_api_env worker nohup "${WORKER_BIN}" >"${WORKER_LOG}" 2>&1 &
    echo $! >"${WORKER_PID_FILE}"
  fi
}

cmd_up() {
  cmd_build
  cmd_infra
  cmd_seed
  cmd_backend
  echo
  cmd_status
  echo
  echo "Start the frontend in a separate terminal for HMR:"
  echo "  cd ${REPO_ROOT}/frontend && GRADEX_API_ORIGIN=http://127.0.0.1:${API_PORT} npm run dev"
}

cmd_status() {
  echo "Student preview stack:"
  compose ps --format '  {{.Service}}\t{{.Status}}' 2>/dev/null || echo "  infrastructure: not running"
  if process_alive "${API_PID_FILE}"; then
    echo "  api\trunning (PID $(cat "${API_PID_FILE}")) http://127.0.0.1:${API_PORT}"
  else
    echo "  api\tstopped"
  fi
  if process_alive "${WORKER_PID_FILE}"; then
    echo "  worker\trunning (PID $(cat "${WORKER_PID_FILE}"))"
  else
    echo "  worker\tstopped"
  fi
  echo "  frontend\t${PUBLIC_ORIGIN} (started separately)"
}

cmd_logs() { tail -n "${LINES:-80}" -f "${API_LOG}"; }

cmd_down() {
  echo "Stopping Student preview stack..."
  stop_tracked_process "${WORKER_PID_FILE}" "${WORKER_BIN}" "worker"
  stop_tracked_process "${API_PID_FILE}" "${API_BIN}" "API"
  compose down
  echo "  infrastructure stopped (volumes preserved; use 'compose down -v' to discard data)"
}

case "${1:-up}" in
  up)       cmd_up ;;
  build)    cmd_build ;;
  infra)    cmd_infra ;;
  seed)     cmd_seed ;;
  session)  shift; cmd_session "$@" ;;
  backend)  cmd_backend ;;
  status)   cmd_status ;;
  logs)     cmd_logs ;;
  down)     cmd_down ;;
  *) echo "usage: $0 {up|build|infra|seed|session|backend|status|logs|down}" >&2; exit 2 ;;
esac
