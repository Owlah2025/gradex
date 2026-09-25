#!/usr/bin/env bash
#
# verify-application-rollback.sh — prove the schema46 release can actually be
# rolled back by switching REAL applications on ONE real database.
#
# WHAT THIS REPLACES, AND WHY
#
#   The first version of this file was a facade. It ran two other scripts,
#   grepped one line out of each, and then printed eleven unconditional
#   "[PASS]" lines claiming things it had never done — including "rollback
#   healthy on SAME clean46 database" and "candidate restarted and candidate
#   re-smoke completed". No API process was ever started. An independent G0
#   review found it. Fabricated evidence is worse than absent evidence, because
#   absent evidence is at least visible.
#
#   Every PASS printed below is emitted only after the condition immediately
#   above it has been checked, and any failure aborts under `set -e`.
#
# WHAT THE RELEASE ACTUALLY NEEDS PROVEN
#
#   There is no production DOWN and no supervised 46 -> 44 command. The entire
#   recovery posture for this release is: leave the database at clean schema 46
#   and roll the APPLICATION back to the schema-46-compatible old-behaviour
#   artifact. That claim is only worth the proof that a real previous-behaviour
#   API and worker can serve a real schema-46 database over real HTTP — and
#   that the candidate can then be brought back on the very same data.
#
#   So this drill establishes clean 44 by running the previous release's own
#   migrator, migrates 44 -> 45 -> 46 with the candidate's targeted commands,
#   and then switches real containers:
#
#       candidate API+worker -> rollback API+worker -> candidate API+worker
#
#   all against one PostgreSQL instance, one database, one data directory, with
#   no recreation, restore or reseed between switches.
#
# WHAT IT IS NOT
#
#   It is not production evidence. It is a disposable local drill and it proves
#   the release mechanism, not the production database. Fresh schema-44 backup
#   and proven restore evidence remain mandatory before any production
#   deployment, and are checked separately by the host wrapper.
#
# ISOLATION
#
#   Its own Compose project, its own volumes, its own generated secrets, its own
#   state directory, all destroyed on exit. It never reads production runtime
#   configuration and never contacts production PostgreSQL, Redis or storage.
#
# USAGE
#   deploy/schema46/verify-application-rollback.sh
#
#   Requires Docker, a Go-capable builder image (pulled by the Dockerfile) and
#   roughly 15 minutes: it builds three real backend images from source.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COMPOSE_FILE="$ROOT/deploy/compose/compose.production-like.yml"

# The running production revision the rollback artifact's behaviour comes from,
# and the reviewed compatibility patch. Both are pinned; a drill built from an
# unpinned patch proves nothing about the artifact that would actually be
# selected.
BASE=0fee657897c939cb679c9d804d184542bb2f692f
PATCH="$ROOT/deploy/schema46/rollback-compat.patch"
PATCH_SHA256=f2d3c6bb0294c4b5982c035529b0341d8c5b1fc346ad85a80765d5e73d1405b3

PROJECT=gradex-schema46-drill
STATE_DIR="$ROOT/deploy/.state/schema46-drill"
SCRATCH="$(mktemp -d /tmp/gradex-schema46-drill.XXXXXX)"

BASE44_IMAGE=gradex-backend:schema46-drill-base44
ROLLBACK_IMAGE=gradex-backend:schema46-drill-rollback
CANDIDATE_IMAGE=gradex-backend:schema46-drill-candidate
PROOF_IMAGE=gradex-backend-proof:schema46-drill
FRONTEND_IMAGE=gradex-frontend:schema46-drill-unused

# The isolated identity the reused release scripts must adopt. They default to
# the shared local S12 environment; these exports are what keep this drill off
# it entirely.
export S12_PROJECT="$PROJECT"
export S12_STATE_DIR="$STATE_DIR"
export S12_ENV_FILE="$STATE_DIR/production-like.env"
# api and worker are the schema-bearing roles and the ones a rollback has to
# prove. The frontend is a static build with no database dependency, so it is
# deliberately out of scope here and is not started at all.
export S12_APPLICATION_ROLES="api worker"

ADMIN_EMAIL=schema46-drill-admin@example.test
STUDENT_EMAIL=schema46-drill-student@example.test
# Disposable, generated per run, never written to a tracked file, and only ever
# valid inside a container set that is destroyed on exit.
ACCOUNT_PASSPHRASE="$(openssl rand -hex 24)Aa1!"

pass() { printf '[PASS] %s\n' "$*"; }
note() { printf 'schema46-drill: %s\n' "$*" >&2; }
die() { printf '[FAIL] %s\n' "$*" >&2; exit 1; }

compose() {
  sed -n '1,999p' "$COMPOSE_FILE" |
    docker compose --file - --project-name "$PROJECT" "$@"
}

cleanup() {
  local status=$?
  note "tearing down the disposable topology"
  compose down --volumes --remove-orphans --timeout 5 >/dev/null 2>&1 || true
  chmod -R u+w "$STATE_DIR" >/dev/null 2>&1 || true
  rm -rf -- "$STATE_DIR" "$SCRATCH"
  [ "$status" = 0 ] || note "drill failed with status $status"
  return $status
}
trap cleanup EXIT

require_tools() {
  local tool
  for tool in docker git openssl psql tar jq; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
  done
  docker info >/dev/null 2>&1 || die "Docker is not reachable"
}

# ---------------------------------------------------------------- reading state

postgres_container() {
  local id
  id="$(compose ps --all --quiet postgres)"
  [ -n "$id" ] || die "PostgreSQL container is absent"
  printf '%s' "$id"
}

psql_query() {
  docker exec "$(postgres_container)" psql --no-psqlrc --username gradex --dbname gradex \
    --tuples-only --no-align --command "$1"
}

psql_exec() {
  docker exec --interactive "$(postgres_container)" psql --no-psqlrc --username gradex \
    --dbname gradex --quiet --set ON_ERROR_STOP=1 >/dev/null
}

schema_state() {
  psql_query 'SELECT version::text || '"'"'|'"'"' || dirty::text FROM schema_migrations;'
}

require_schema() {
  local want="$1" got
  got="$(schema_state)"
  [ "$got" = "$want" ] || die "schema marker is $got, want $want"
}

# The database's own identity, not the container's name.
#
# system_identifier is stamped into pg_control when initdb runs and survives
# restarts; the database OID is assigned by CREATE DATABASE. Together they make
# a recreated, restored or reseeded database impossible to pass off as the same
# one — which is the whole point of asserting them across every application
# switch below.
db_identity() {
  psql_query "SELECT system_identifier::text || '|' || (SELECT oid::text FROM pg_database WHERE datname = current_database()) || '|' || current_database() FROM pg_control_system();"
}

require_db_identity() {
  local want="$1" got
  got="$(db_identity)"
  [ "$got" = "$want" ] || die "database identity changed: $got, want $want (a switch recreated, restored or reseeded the database)"
}

# ------------------------------------------------------------------ real HTTP

CURL_IMAGE=curlimages/curl:8.11.1

# Real HTTP over the application network: a separate container talking to the
# API over TCP. Not `docker exec` into the API, and not a Go test calling a
# handler in process.
#
# COOKIES is carried by hand rather than by curl's jar because the session and
# anonymous cookies are correctly marked Secure, and curl will not store a
# Secure cookie received over plain HTTP. Refusing to weaken the cookie to suit
# the drill is the point; the drill adapts instead.
COOKIES=""

http() {
  if [ -n "$COOKIES" ]; then
    docker run --rm --network "${PROJECT}_app" "$CURL_IMAGE" \
      --silent --show-error --header "Cookie: $COOKIES" "$@"
  else
    docker run --rm --network "${PROJECT}_app" "$CURL_IMAGE" --silent --show-error "$@"
  fi
}

api_url() { printf 'http://api:8080%s' "$1"; }

# Issues a request, remembers any cookie it sets, and leaves the status code in
# HTTP_STATUS and the response body in HTTP_BODY.
HTTP_STATUS=""
HTTP_BODY=""
http_call() {
  local path="$1" raw headers name value
  shift
  raw="$(http --dump-header - --write-out '\n%{http_code}' "$@" "$(api_url "$path")")" ||
    die "request to $path failed at the transport level"
  HTTP_STATUS="$(printf '%s' "$raw" | tail -n 1)"
  headers="$(printf '%s' "$raw" | sed -n '1,/^\r\{0,1\}$/p')"
  HTTP_BODY="$(printf '%s' "$raw" | sed '1,/^\r\{0,1\}$/d' | sed '$d')"
  while IFS= read -r value; do
    [ -n "$value" ] || continue
    name="${value%%=*}"
    # grep exits 1 on an empty cookie list, which is not an error here.
    COOKIES="$(printf '%s' "$COOKIES" | tr ';' '\n' | sed 's/^ *//' | grep -v "^${name}=" || true)"
    COOKIES="$(printf '%s' "$COOKIES" | paste -sd'; ' -)"
    COOKIES="${COOKIES:+$COOKIES; }$value"
  done < <(printf '%s' "$headers" | grep -i '^set-cookie:' | sed 's/^[Ss]et-[Cc]ookie: *//' | cut -d';' -f1)
}

require_http() {
  local label="$1" want="$2" path="$3"
  shift 3
  http_call "$path" "$@"
  [ "$HTTP_STATUS" = "$want" ] ||
    die "$label: HTTP $HTTP_STATUS, want $want ($path) body=$(printf '%s' "$HTTP_BODY" | head -c 300)"
}

worker_running() {
  local id
  id="$(compose ps --all --quiet worker)"
  [ -n "$id" ] || die "worker container is absent"
  [ "$(docker inspect --format '{{.State.Status}}' "$id")" = running ] ||
    die "worker is not running"
  [ "$(docker inspect --format '{{.State.Restarting}}' "$id")" = false ] ||
    die "worker is restarting"
}

backend_image_of() {
  local service="$1" id
  id="$(compose ps --all --quiet "$service")"
  [ -n "$id" ] || die "$service container is absent"
  docker inspect --format '{{.Image}}' "$id"
}

# ------------------------------------------------------------------- assertions

# One active purchase request per Course and email is a product rule, so each
# application turn mints its own rather than the fixture pre-seeding several.
new_purchase_request() {
  local reference="$1" id
  id="$(psql_query "INSERT INTO purchase_requests
      (reference_code, target_kind, course_id, email, normalized_email, requester_account_id,
       course_title_ar, course_title_en, price_minor_units, currency, state)
    VALUES ('$reference', 'COURSE', '$COURSE_ID', '$STUDENT_EMAIL', '$STUDENT_EMAIL', '$STUDENT_ID',
            'مقرر التجربة', 'Rollback Drill Course', 25000, 'KWD', 'WAITING_PAYMENT')
    RETURNING id;")"
  [ -n "$id" ] || die "could not mint the purchase request $reference"
  printf '%s' "$id"
}


# Liveness and readiness over real HTTP against whichever application is
# currently selected, plus proof the worker is still up. Container health status
# is deliberately NOT accepted as the answer here: the drill asks the routes.
candidate_health() {
  local label="$1"
  COOKIES=""
  require_http "$label /healthz" 200 /healthz
  pass "$label API /healthz"
  require_http "$label /readyz" 200 /readyz
  pass "$label API /readyz"
  # Alive now, and still alive a few seconds later — a worker that boots and
  # then crash-loops would otherwise read as healthy.
  worker_running
  sleep 5
  worker_running
  pass "$label worker alive"
}

# An authenticated Administrator session over real HTTP: anonymous bootstrap for
# the CSRF token, then a real password login. Both applications must do this.
admin_session() {
  local csrf
  COOKIES=""
  require_http "session bootstrap" 200 /api/v1/session/bootstrap
  csrf="$(printf '%s' "$HTTP_BODY" | jq -r '.csrf_token // empty')"
  [ -n "$csrf" ] || die "session bootstrap returned no CSRF token"
  require_http "admin login" 200 /api/v1/sessions \
    --request POST \
    --header 'Content-Type: application/json' \
    --header "X-CSRF-Token: $csrf" \
    --data "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ACCOUNT_PASSPHRASE\"}"
  ADMIN_CSRF="$(printf '%s' "$HTTP_BODY" | jq -r '.csrf_token // empty')"
  [ -n "$ADMIN_CSRF" ] || ADMIN_CSRF="$csrf"
}

# The behaviour whose schema floor made schema 43 unsafe. Both applications are
# held to it, over the real Admin route, against the real database.
direct_course_grant() {
  local label="$1" request="$2" before after
  before="$(psql_query "SELECT count(*) FROM entitlements WHERE source_purchase_request_id = '$request';")"
  [ "$before" = 0 ] || die "$label: purchase request $request was already granted"
  require_http "$label direct Course grant" 200 \
    "/api/v1/admin/purchase-requests/$request/confirm-payment" \
    --request POST --header 'Content-Type: application/json' \
    --header "X-CSRF-Token: $ADMIN_CSRF" --data '{}'
  after="$(psql_query "SELECT count(*) FROM entitlements WHERE source_purchase_request_id = '$request' AND source_invitation_id IS NULL AND grant_source = 'PURCHASE_REQUEST';")"
  [ "$after" = 1 ] ||
    die "$label: confirm-payment returned 200 but wrote $after direct Entitlements"
  pass "$label direct Course grant (Entitlement with a purchase request and no invitation)"
}

# Anonymous surfaces both applications serve.
shared_public_smoke() {
  local label="$1"
  COOKIES=""
  require_http "$label public Course list" 200 /api/v1/catalog/courses
  printf '%s' "$HTTP_BODY" | grep -Fq "$COURSE_ID" ||
    die "$label: the published Course is absent from the public catalogue"
  require_http "$label public Course detail" 200 "/api/v1/catalog/courses/$COURSE_ID"
  pass "$label public Course read"

  require_http "$label legacy Course preview" 200 "/api/v1/media/courses/$COURSE_ID/preview"
  printf '%s' "$HTTP_BODY" | jq -e '.url' >/dev/null ||
    die "$label: the legacy Course preview returned no media URL"
  pass "$label legacy Course preview"
}

# Everything the candidate must serve, including the schema-46 surface the
# rollback artifact does not have.
candidate_smoke() {
  local label="$1" request="$2" session manifest
  shared_public_smoke "$label"

  COOKIES=""
  require_http "$label session bootstrap" 200 /api/v1/session/bootstrap
  pass "$label session/device bootstrap"

  # The new Lesson preview, end to end: authorization, then the manifest the
  # capability names.
  require_http "$label Lesson preview authorization" 200 \
    "/api/v1/media/courses/$COURSE_ID/lessons/$PREVIEWABLE_LESSON/preview-authorizations" \
    --request POST --header 'Content-Type: application/json' --data '{}'
  session="$(printf '%s' "$HTTP_BODY" | jq -r '.preview_session // empty')"
  [ -n "$session" ] || die "$label: no preview session was issued"
  # Follow the URL the server issued rather than reconstructing it. The player
  # does the same, and the capability is opaque to everything except the route
  # that minted it.
  manifest="$(printf '%s' "$HTTP_BODY" | jq -r '.manifest_url // empty')"
  [ -n "$manifest" ] || die "$label: the authorization carried no manifest URL"
  case "$manifest" in
    http://*|https://*) manifest="/${manifest#*://*/}" ;;
  esac
  require_http "$label Lesson preview manifest" 200 "$manifest"
  printf '%s' "$HTTP_BODY" | grep -q '#EXTM3U' ||
    die "$label: the Lesson preview manifest is not an HLS playlist"
  printf '%s' "$HTTP_BODY" | grep -q 'renditions/' ||
    die "$label: the Lesson preview master names no renditions"
  pass "$label new Lesson preview authorization and HLS manifest"

  # The flag is consulted, not assumed: a Lesson that is not previewable is
  # refused even though it carries the same video.
  http_call "/api/v1/media/courses/$COURSE_ID/lessons/$PRIVATE_LESSON/preview-authorizations" \
    --request POST --header 'Content-Type: application/json' --data '{}'
  [ "$HTTP_STATUS" != 200 ] ||
    die "$label: a Lesson that is not publicly previewable was authorized anyway"
  pass "$label non-previewable Lesson refused ($HTTP_STATUS)"

  admin_session
  direct_course_grant "$label" "$request"
  student_playback "$label"
}

# What the rollback application must still do on a schema-46 database.
rollback_smoke() {
  local request="$1" scheduler
  shared_public_smoke "rollback"

  COOKIES=""
  require_http "rollback session bootstrap" 200 /api/v1/session/bootstrap
  pass "rollback session/device bootstrap"

  admin_session
  direct_course_grant "rollback" "$request"
  student_playback "rollback"

  # The new Lesson preview is NOT expected to exist here; that is what a
  # rollback means. What must be true is that the old application does not run
  # the schema-46 automatic recovery scheduler.
  http_call "/api/v1/media/courses/$COURSE_ID/lessons/$PREVIEWABLE_LESSON/preview-authorizations" \
    --request POST --header 'Content-Type: application/json' --data '{}'
  [ "$HTTP_STATUS" != 200 ] ||
    die "the rollback application served the new Lesson preview route"
  pass "rollback application does not serve the new Lesson preview ($HTTP_STATUS)"

  scheduler="$(psql_query 'SELECT count(*) FROM media_auto_enhancement_recovery;')"
  [ "$scheduler" = 0 ] ||
    die "the rollback application wrote $scheduler automatic recovery rows; it must run no 3C-C scheduler"
  pass "rollback application ran no 3C-C automatic enhancement recovery"
}

# A real Student session and a real protected playback authorization.
student_playback() {
  local label="$1" csrf
  COOKIES=""
  require_http "$label student bootstrap" 200 /api/v1/session/bootstrap
  csrf="$(printf '%s' "$HTTP_BODY" | jq -r '.csrf_token // empty')"
  require_http "$label student login" 200 /api/v1/sessions \
    --request POST --header 'Content-Type: application/json' \
    --header "X-CSRF-Token: $csrf" \
    --data "{\"email\":\"$STUDENT_EMAIL\",\"password\":\"$ACCOUNT_PASSPHRASE\"}"
  local session_csrf
  session_csrf="$(printf '%s' "$HTTP_BODY" | jq -r '.csrf_token // empty')"
  [ -n "$session_csrf" ] || session_csrf="$csrf"
  require_http "$label Student playback authorization" 200 /api/v1/media/playback-authorizations \
    --request POST --header 'Content-Type: application/json' \
    --header "X-CSRF-Token: $session_csrf" \
    --data "{\"lesson_id\":\"$PREVIEWABLE_LESSON\"}"
  pass "$label Student protected playback authorization"
}

# An unhealthy candidate, and a real recovery from it.
#
# The failure is a runtime condition only: the candidate is pointed at a
# database that does not exist, so its readiness fails closed. No image is
# damaged, no schema is touched, and the real database keeps serving — which is
# why the rollback application can then be selected onto it unchanged.
candidate_failure_rollback() {
  local candidate_manifest="$1" rollback_manifest="$2" identity="$3" status
  local broken="postgres://gradex:${POSTGRES_PASSWORD}@postgres:5432/gradex_schema46_drill_absent?sslmode=disable"

  DATABASE_URL="$broken" GRADEX_BACKEND_IMAGE="$CANDIDATE_IMAGE" \
    compose up --detach --no-deps --force-recreate api >/dev/null 2>&1 || true
  sleep 15
  status="$(http --output /dev/null --write-out '%{http_code}' "$(api_url /readyz)" 2>/dev/null || true)"
  [ "$status" != 200 ] ||
    die "the deliberately misconfigured candidate still reported ready"
  pass "unhealthy candidate fails readiness (observed: ${status:-no response})"

  require_schema '46|false'
  require_db_identity "$identity"

  select_release "$rollback_manifest" ||
    die "the rollback application could not be selected while the candidate was unhealthy"
  COOKIES=""
  require_http "recovery /healthz" 200 /healthz
  require_http "recovery /readyz" 200 /readyz
  worker_running
  require_schema '46|false'
  require_db_identity "$identity"
  pass "unhealthy candidate rolled back to a healthy old-behaviour application on an unchanged clean46 database"

  select_release "$candidate_manifest" || die "the candidate would not return after the recovery"
  candidate_health "recovered candidate"
}

# The compiled supported range, enforced where it matters: in the selection
# path, before anything is started.
#
# This is the property the whole schema46 rollback design rests on. Production
# runs a build whose ceiling is 44; once 0045 and 0046 are applied that build
# must NOT be selectable, which is precisely why a schema-46-compatible
# old-behaviour artifact had to be produced at all. If selection accepted it,
# the operator would get a process that starts and then fails, instead of a
# refusal.
#
# It is exercised against the real clean46 database with the real previous
# release, through the real selection command.
#
# The mirror-image case — an artifact refusing a database BELOW its minimum,
# which is the schema-43 defect — is proven by deploy/schema46/verify-rollback-compat.sh
# against a real clean-43 database, and at the selection layer by the schema46
# negative matrix in deploy/scripts/verify-deploy-bundle.py. This drill does not
# restate either, because it has no clean-43 database to be honest with.
out_of_range_refusal() {
  local manifest="$SCRATCH/release-base44.env" output before
  write_manifest "$manifest" "$BASE" "$BASE44_IMAGE"
  before="$(backend_image_of api)"
  output="$(select_release "$manifest" 2>&1 || true)"
  case "$output" in
    *"newer than target release maximum 44"*) ;;
    *) die "selection did not refuse the ceiling-44 previous release on a clean46 database: $output" ;;
  esac
  [ "$(backend_image_of api)" = "$before" ] ||
    die "the refused selection still recreated the application"
  pass "previous release (ceiling 44) refused on clean46 before anything was started"
}

# ------------------------------------------------------------- building images

build_backend() {
  local context="$1" tag="$2" target="${3:-runtime}"
  tar --exclude=.git --exclude=.env --exclude='.env.*' --exclude='*.out' --exclude=coverage \
    -C "$context" -cf - . |
    docker build --quiet --target "$target" --tag "$tag" - >/dev/null
}

build_images() {
  printf '%s  %s\n' "$PATCH_SHA256" "$PATCH" | sha256sum --check --status ||
    die "the rollback compatibility patch is not the reviewed one"
  git -C "$ROOT" cat-file -e "${BASE}^{commit}" || die "base commit $BASE is absent"

  note "building the candidate backend from the working tree"
  build_backend "$ROOT/backend" "$CANDIDATE_IMAGE"
  build_backend "$ROOT/backend" "$PROOF_IMAGE" proof

  note "building the previous release ($BASE) that establishes clean 44"
  mkdir -p "$SCRATCH/base"
  git -C "$ROOT" archive "$BASE" | tar -xf - -C "$SCRATCH/base"
  build_backend "$SCRATCH/base/backend" "$BASE44_IMAGE"

  note "building the schema-46-compatible old-behaviour rollback artifact"
  mkdir -p "$SCRATCH/rollback"
  git -C "$ROOT" archive "$BASE" | tar -xf - -C "$SCRATCH/rollback"
  (cd "$SCRATCH/rollback" && git apply "$PATCH")
  build_backend "$SCRATCH/rollback/backend" "$ROLLBACK_IMAGE"

  # The frontend is never started, but Compose interpolates the whole model, so
  # the variable has to resolve to something. A scratch image makes it obvious
  # that nothing here depends on it.
  printf 'FROM scratch\n' | docker build --quiet --tag "$FRONTEND_IMAGE" - >/dev/null

  # Pull only when it is not already local. The registry is an external
  # dependency and a transient 401 or rate limit there is not a release finding.
  docker image inspect "$CURL_IMAGE" >/dev/null 2>&1 ||
    docker pull --quiet "$CURL_IMAGE" >/dev/null ||
    die "the HTTP client image $CURL_IMAGE is neither local nor pullable"
}

image_schema_range() {
  docker run --rm --entrypoint gradex-migrate "$1" schema-range
}

image_max_version() {
  docker run --rm --entrypoint gradex-migrate "$1" max-version
}

# --------------------------------------------------------------------- manifests

write_manifest() {
  local file="$1" release="$2" backend="$3"
  {
    printf 'GRADEX_RELEASE_ID=%s\n' "$release"
    printf 'GRADEX_BACKEND_IMAGE=%s\n' "$backend"
    printf 'GRADEX_FRONTEND_IMAGE=%s\n' "$FRONTEND_IMAGE"
  } >"$file"
  chmod 600 "$file"
}

select_release() {
  "$ROOT/deploy/scripts/application-rollback.sh" apply "$1"
}

main() {
  require_tools
  build_images

  # ---------------------------------------------------------------- Section 1
  # Compiled ranges, read from the artifacts themselves.
  local candidate_range rollback_range base_max
  candidate_range="$(image_schema_range "$CANDIDATE_IMAGE")"
  rollback_range="$(image_schema_range "$ROLLBACK_IMAGE")"
  base_max="$(image_max_version "$BASE44_IMAGE")"
  [ "$candidate_range" = "44 46" ] || die "candidate compiled range is '$candidate_range', want '44 46'"
  [ "$rollback_range" = "44 46" ] || die "rollback compiled range is '$rollback_range', want '44 46'"
  [ "$base_max" = 44 ] || die "previous release ceiling is $base_max, want 44"
  pass "compiled ranges: candidate 44..46, rollback artifact 44..46, previous release ceiling 44"

  # ---------------------------------------------------------------- Section 2
  # Disposable topology. environment.sh generates the secrets and Redis TLS into
  # this drill's own state directory because of the exports above.
  mkdir -p "$STATE_DIR"
  chmod 700 "$STATE_DIR"
  "$ROOT/deploy/scripts/environment.sh" prepare >/dev/null
  # Point the generated environment at this drill's images.
  {
    printf 'GRADEX_BACKEND_IMAGE=%s\n' "$BASE44_IMAGE"
    printf 'GRADEX_FRONTEND_IMAGE=%s\n' "$FRONTEND_IMAGE"
    printf 'GRADEX_PROOF_IMAGE=%s\n' "$PROOF_IMAGE"
  } >>"$S12_ENV_FILE"

  # Any variable the Compose model marks required (${VAR:?...}) that the shared
  # generator does not produce. Compose interpolates the entire model before it
  # filters by profile, so a value is needed even for services this drill never
  # starts. Generated disposable values, never a production secret, and never
  # written anywhere outside this run's own state directory.
  local required missing
  for required in $(grep -oE '\$\{[A-Z0-9_]+:\?' "$COMPOSE_FILE" | tr -d '${:?' | sort -u); do
    grep -q "^${required}=" "$S12_ENV_FILE" && continue
    missing="$(openssl rand -hex 32)"
    printf '%s=%s\n' "$required" "$missing" >>"$S12_ENV_FILE"
    note "generated a disposable value for the required variable $required"
  done

  set -a
  # shellcheck disable=SC1090
  . "$S12_ENV_FILE"
  set +a

  compose up --detach postgres redis minio >/dev/null
  local attempts=0
  until [ "$(docker inspect --format '{{.State.Health.Status}}' "$(postgres_container)" 2>/dev/null)" = healthy ]; do
    attempts=$((attempts + 1))
    [ "$attempts" -lt 90 ] || die "PostgreSQL did not become healthy"
    sleep 2
  done
  compose up --detach minio-init >/dev/null
  pass "disposable topology up: project $PROJECT, dedicated volumes and generated secrets"

  # ---------------------------------------------------------------- Section 3
  # Real clean 44, established by the previous release's own migrator rather
  # than by editing a marker or restoring a prepared dump. Its ceiling is 44, so
  # `up` lands exactly where production is today.
  GRADEX_BACKEND_IMAGE="$BASE44_IMAGE" compose run --rm --no-deps migrate gradex-migrate up >/dev/null
  require_schema '44|false'
  local identity
  identity="$(db_identity)"
  pass "clean44 established by the previous release's migrator"

  # 44 -> 45 -> 46 using the candidate's exact targeted commands, each verified.
  GRADEX_BACKEND_IMAGE="$CANDIDATE_IMAGE" compose run --rm --no-deps migrate gradex-migrate up-schema-45 >/dev/null
  require_schema '45|false'
  require_db_identity "$identity"
  pass "migrated to clean45"

  GRADEX_BACKEND_IMAGE="$CANDIDATE_IMAGE" compose run --rm --no-deps migrate gradex-migrate up-schema-46 >/dev/null
  require_schema '46|false'
  require_db_identity "$identity"
  pass "migrated to clean46"

  # ---------------------------------------------------------------- Section 4
  # Accounts, created by the real bootstrap binary, and the fixture the HTTP
  # smoke reads. Seeded once, into this same database, never re-seeded.
  local admin_id
  export BOOTSTRAP_ADMIN_PASSWORD="$ACCOUNT_PASSPHRASE"
  # The single Administrator, created by the real binary. The Student is made by
  # the fixture, because this tool exists to create exactly one Administrator.
  BOOTSTRAP_ADMIN_EMAIL="$ADMIN_EMAIL" \
  BOOTSTRAP_ADMIN_OPERATION_ID="$(uuidgen 2>/dev/null || openssl rand -hex 16)" \
  GRADEX_BACKEND_IMAGE="$CANDIDATE_IMAGE" \
    compose run --rm --no-deps bootstrap-admin >/dev/null ||
    die "bootstrap-admin failed for $ADMIN_EMAIL"
  unset BOOTSTRAP_ADMIN_PASSWORD
  admin_id="$(psql_query "SELECT id FROM accounts WHERE normalized_email = '$ADMIN_EMAIL';")"
  [ -n "$admin_id" ] || die "the bootstrapped Administrator is absent"

  local fixture_ids
  fixture_ids="$(docker exec --interactive "$(postgres_container)" \
    psql --no-psqlrc --username gradex --dbname gradex --tuples-only --no-align \
      --set ON_ERROR_STOP=1 \
      --set admin_email="$ADMIN_EMAIL" --set student_email="$STUDENT_EMAIL" \
      <"$ROOT/deploy/schema46/drill-fixture.sql" | tail -n 1)" ||
    die "seeding the drill fixture failed"
  read -r COURSE_ID PREVIEWABLE_LESSON PRIVATE_LESSON VIDEO_VERSION PREVIEW_VERSION STUDENT_ID \
    <<<"$fixture_ids"
  [ -n "$STUDENT_ID" ] || die "the fixture did not emit its identifiers: $fixture_ids"
  require_schema '46|false'
  require_db_identity "$identity"
  pass "fixture seeded once into the same database (course $COURSE_ID)"

  # ---------------------------------------------------------------- Section 5
  # Real candidate application, selected through the release mechanism.
  local candidate_manifest rollback_manifest
  candidate_manifest="$SCRATCH/release-candidate.env"
  rollback_manifest="$SCRATCH/release-rollback.env"
  write_manifest "$candidate_manifest" "$(git -C "$ROOT" rev-parse HEAD)" "$CANDIDATE_IMAGE"
  write_manifest "$rollback_manifest" "schema46-rollback-compat" "$ROLLBACK_IMAGE"

  select_release "$candidate_manifest" || die "the candidate application would not start"
  local candidate_backend
  candidate_backend="$(backend_image_of api)"
  pass "candidate application selected and recreated (api image $candidate_backend)"

  candidate_health "candidate"
  candidate_smoke "candidate" "$(new_purchase_request SCHEMA46-DRILL-1)" full
  require_schema '46|false'
  require_db_identity "$identity"
  pass "database remained clean46 under the candidate"

  # ---------------------------------------------------------------- Section 6
  # Real rollback application, same database, no migration.
  select_release "$rollback_manifest" || die "the rollback application would not start"
  local rollback_backend
  rollback_backend="$(backend_image_of api)"
  [ "$rollback_backend" != "$candidate_backend" ] ||
    die "the rollback selection resolved to the candidate image; no switch occurred"
  pass "rollback application selected on the SAME clean46 database (api image $rollback_backend)"

  candidate_health "rollback"
  rollback_smoke "$(new_purchase_request SCHEMA46-DRILL-2)"
  require_schema '46|false'
  require_db_identity "$identity"
  pass "database remained clean46 under the rollback application; no migration ran"

  # ---------------------------------------------------------------- Section 7
  # Back to the candidate, same database, on top of the rollback's writes.
  select_release "$candidate_manifest" || die "the candidate would not restart after the rollback"
  [ "$(backend_image_of api)" = "$candidate_backend" ] ||
    die "the restarted application is not the candidate image"
  pass "candidate restarted on the same database after the rollback"

  candidate_health "candidate restart"
  candidate_smoke "candidate restart" "$(new_purchase_request SCHEMA46-DRILL-3)" full
  require_schema '46|false'
  require_db_identity "$identity"
  pass "candidate re-smoke passed; the rollback application's writes did not poison it"

  # ---------------------------------------------------------------- Section 8
  # A real runtime health failure, and a real recovery from it.
  candidate_failure_rollback "$candidate_manifest" "$rollback_manifest" "$identity"

  # ---------------------------------------------------------------- Section 9
  # The supported range, enforced by the real selection path rather than
  # asserted about it.
  out_of_range_refusal

  printf 'SCHEMA46 APPLICATION ROLLBACK DRILL PASS\n'
  printf 'database identity held throughout: %s\n' "$identity"
  printf 'candidate api image: %s\n' "$candidate_backend"
  printf 'rollback api image:  %s\n' "$rollback_backend"
}

main "$@"
