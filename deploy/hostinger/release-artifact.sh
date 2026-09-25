#!/usr/bin/env bash
# Shared by the trusted importer and the release-local host wrapper.

artifact_value() {
  local value
  value="$(awk -F= -v key="$2" '$1 == key { count++; value=substr($0,length(key)+2) } END { if(count != 1) exit 1; print value }' "$1")" ||
    die "missing or duplicate artifact field $2"
  [[ "$value" =~ ^[A-Za-z0-9:._/-]+$ ]] || die "invalid artifact field $2"
  printf '%s' "$value"
}

# A release bundle declares exactly one boundary capability marker, drawn from a
# closed set. Each schema cutover is a one-release boundary with its own
# supervised rollback, so the marker says which boundary this bundle is FOR, and
# the per-boundary command asserts the value it requires. A bundle declaring
# none, more than one, or an unrecognized value is not a release this tooling can
# reason about — that is a mixed or stale bundle, and it refuses here.
SCHEMA41_BUNDLE_CAPABILITY=SCHEMA41_CAPABILITY=supervised-41-to-40-v1
SCHEMA42_BUNDLE_CAPABILITY=SCHEMA42_CAPABILITY=manual-enhancement-v1
SCHEMA46_BUNDLE_CAPABILITY=SCHEMA46_CAPABILITY=auto-enhancement-lesson-preview-v1

bundle_capability() {
  local declared
  declared="$(awk -F= '$1 ~ /^SCHEMA4[0-9]_CAPABILITY$/ { count++; line=$0 } END { if (count != 1) exit 1; print line }' \
    "$1/release-tooling.env")" || die "bundle must declare exactly one release capability marker"
  case "$declared" in
    "$SCHEMA41_BUNDLE_CAPABILITY"|"$SCHEMA42_BUNDLE_CAPABILITY"|"$SCHEMA46_BUNDLE_CAPABILITY") ;;
    *) die "unrecognized bundle capability marker" ;;
  esac
  printf '%s' "$declared"
}

image_schema_range() {
  local image="$1" range minimum maximum
  range="$(docker run --rm --network none --entrypoint gradex-migrate "$image" schema-range)" ||
    die "backend image cannot report its supported schema range"
  read -r minimum maximum <<<"$range"
  [[ "$minimum" =~ ^[0-9]+$ && "$maximum" =~ ^[0-9]+$ && "$minimum" -le "$maximum" ]] ||
    die "backend image reported an invalid schema range"
  printf '%s %s' "$minimum" "$maximum"
}

require_image_schema_range() {
  local image="$1" expected_minimum="$2" expected_maximum="$3" actual_minimum actual_maximum
  read -r actual_minimum actual_maximum <<<"$(image_schema_range "$image")"
  [ "$actual_minimum" = "$expected_minimum" ] || die "backend image minimum schema is $actual_minimum, expected $expected_minimum"
  [ "$actual_maximum" = "$expected_maximum" ] || die "backend image maximum schema is $actual_maximum, expected $expected_maximum"
}

verify_release_bundle() {
  local release_dir="$1" tooling="$1/tooling" manifest="$1/release.env" revision="$2" digest
  [ -f "$tooling/release-tooling.env" ] || die "missing bundle metadata"
  [ "$(artifact_value "$manifest" GRADEX_RELEASE_SHA)" = "$revision" ] || die "manifest release mismatch"
  [ "$(artifact_value "$tooling/release-tooling.env" RELEASE_SHA)" = "$revision" ] || die "bundle release mismatch"
  [ "$(artifact_value "$tooling/release-tooling.env" DEPLOY_BUNDLE_FORMAT)" = 1 ] || die "unsupported bundle format"
  bundle_capability "$tooling" >/dev/null
  digest="$(artifact_value "$manifest" GRADEX_DEPLOY_BUNDLE_SHA256)"
  [[ "$digest" =~ ^[0-9a-f]{64}$ ]] || die "invalid bundle digest"
  [ "$(cat "$release_dir/deploy-bundle.tar.gz.sha256")" = "$digest  deploy-bundle.tar.gz" ] || die "bundle checksum disagrees with manifest"
  (cd "$release_dir" && sha256sum --check --status deploy-bundle.tar.gz.sha256) || die "bundle checksum failed"
  tar -xOzf "$release_dir/deploy-bundle.tar.gz" tooling.sha256 | cmp -s - "$tooling/tooling.sha256" || die "bundle inventory drift"
  [ -z "$(find "$tooling" -type l -print)" ] || die "bundle contains symlinks"
  (cd "$tooling" && sha256sum --check --status tooling.sha256) || die "extracted tooling drift"
  [ "$(find "$tooling" -type f | awk 'END { print NR }')" = "$(awk 'END { print NR+1 }' "$tooling/tooling.sha256")" ] ||
    die "unexpected files in extracted tooling"
}

verify_release_images() {
  local manifest="$1" revision="$2" role key image
  for role in BACKEND FRONTEND PROOF; do
    key="GRADEX_${role}_IMAGE"
    image="$(artifact_value "$manifest" "$key")"
    [ "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")" = "$revision" ] || die "$role image revision mismatch"
    [ "$(docker image inspect --format '{{.Id}}' "$image")" = "$(artifact_value "$manifest" "${key}_ID")" ] || die "$role image ID mismatch"
  done
}

# The caller names the boundary it is executing, so a schema-41 command can never
# accept a schema-42 bundle and the reverse is equally impossible.
require_release_artifact() {
  local expected_capability="$1" release_dir="$S12_HOST_STATE_DIR/releases/$GRADEX_RELEASE_SHA" key
  [ -n "$expected_capability" ] || die "the release boundary capability must be named"
  [ "$S12_ROOT" = "$release_dir/tooling" ] || die "execute host.sh from the selected imported release tooling"
  verify_release_bundle "$release_dir" "$GRADEX_RELEASE_SHA"
  [ "$(bundle_capability "$release_dir/tooling")" = "$expected_capability" ] ||
    die "bundle capability is not $expected_capability"
  for key in GRADEX_BACKEND_IMAGE GRADEX_FRONTEND_IMAGE GRADEX_PROOF_IMAGE; do
    [ "${!key}" = "$(artifact_value "$release_dir/release.env" "$key")" ] || die "runtime image selection disagrees with manifest"
  done
  verify_release_images "$release_dir/release.env" "$GRADEX_RELEASE_SHA"
}

require_migration_hash_binding() {
  local prefix="$1" migration expected actual
  for migration in "$prefix".{up,down}.sql; do
    expected="$(sha256sum "$S12_ROOT/backend/internal/db/migrations/$migration")"
    actual="$(docker run --rm --network none --entrypoint sha256sum "$GRADEX_BACKEND_IMAGE" "internal/db/migrations/$migration")" ||
      die "backend lacks migration $prefix"
    [ "${actual%% *}" = "${expected%% *}" ] || die "backend migration $prefix differs from tooling bundle"
  done
}

# The schema-42 candidate's own capability proof, deliberately separate from the
# schema-41 one. The usage text is the exact compiled command surface, so an
# image that predates the supervised 42 -> 41 command, or one that has drifted
# past this boundary, is refused rather than trusted because its version label
# looks right.
require_schema42_image_capability() {
  local usage status=0
  usage="$(docker run --rm --network none --entrypoint gradex-migrate "$GRADEX_BACKEND_IMAGE" 2>&1)" || status=$?
  [ "$status" = 1 ] &&
    [ "$usage" = 'migrate: usage: migrate <up|down|version|max-version|rollback-schema-41|rollback-schema-42> [steps]' ] ||
    die "backend lacks the supervised schema 42 rollback command"
  # The rollback's queue-side drain proof has to exist in the same image that
  # performs the downgrade; the schema-41 baseline image does not carry it.
  docker run --rm --network none --entrypoint test "$GRADEX_BACKEND_IMAGE" -x /usr/local/bin/gradex-enhancement-drain ||
    die "backend lacks the enhancement drain proof"
  require_migration_hash_binding 0042_active_processing_attempt_kind
}

require_schema46_image_capability() {
  require_image_schema_range "$GRADEX_BACKEND_IMAGE" 44 46
  require_migration_hash_binding 0045_auto_enhancement_recovery
  require_migration_hash_binding 0046_lesson_public_preview
}

# The exact deployed 3C-A artifact set, which is the application rollback target
# for the whole 3C-B release. It cannot be rebuilt on the host — there is no
# production Git checkout — so if any part of it is missing or no longer
# validates, 3C-B has no way back and must not go forward.
SCHEMA41_APPLICATION_FLOOR=98e88fcc1105e8c638bb638d3f1c46630bcc51b2

require_schema41_application_floor() {
  local floor="$S12_HOST_STATE_DIR/releases/$SCHEMA41_APPLICATION_FLOOR" file
  [ -d "$floor" ] ||
    die "the schema-41 application rollback floor $SCHEMA41_APPLICATION_FLOOR is not staged; refusing to deploy schema 42 without a way back"
  for file in release.env images.tar.gz images.tar.gz.sha256 deploy-bundle.tar.gz deploy-bundle.tar.gz.sha256 \
    tooling/release-tooling.env tooling/deploy/hostinger/host.sh tooling/tooling.sha256; do
    [ -f "$floor/$file" ] && [ ! -L "$floor/$file" ] ||
      die "the schema-41 application rollback floor is missing regular artifact $file"
  done
  (cd "$floor" && sha256sum --check --status images.tar.gz.sha256) ||
    die "the schema-41 application rollback floor image archive checksum failed"
  verify_release_bundle "$floor" "$SCHEMA41_APPLICATION_FLOOR"
  [ "$(bundle_capability "$floor/tooling")" = "$SCHEMA41_BUNDLE_CAPABILITY" ] ||
    die "the schema-41 application rollback floor does not declare the supervised 41 to 40 capability"
  verify_release_images "$floor/release.env" "$SCHEMA41_APPLICATION_FLOOR"
  [ "$(image_max_schema_version "$(artifact_value "$floor/release.env" GRADEX_BACKEND_IMAGE)")" = 41 ] ||
    die "the schema-41 application rollback floor backend image does not target schema 41"
  note "schema-41 application rollback floor $SCHEMA41_APPLICATION_FLOOR is complete and validates"
}

require_schema41_image_capability() {
  local usage status=0
  usage="$(docker run --rm --network none --entrypoint gradex-migrate "$GRADEX_BACKEND_IMAGE" 2>&1)" || status=$?
  [ "$status" = 1 ] && [ "$usage" = 'migrate: usage: migrate <up|down|version|max-version|rollback-schema-41> [steps]' ] ||
    die "backend lacks supervised rollback capability"
  require_migration_hash_binding 0041_enhancement_recovery_foundation
}

# Inspect all local containers, including stopped/restarting ones. DB names
# that cannot be parsed conservatively refuse; never print credential-bearing env.
require_no_local_production_workers() {
  local expected_worker="${1:-}" found_expected=false containers container inspection worker db_url db_target
  [[ "$expected_worker" =~ ^[A-Za-z0-9]*$ ]] || die "production must have exactly one worker container"
  containers="$(docker ps --all --quiet --no-trunc)" || die "could not enumerate local containers"
  for container in $containers; do
    inspection="$(docker inspect --type container "$container")" || die "could not inspect local container $container"
    worker="$(jq -er '.[0] | ((.Config.Env // [] | any(. == "SERVICE_ROLE=worker")) or
      ((.Config.Labels // {})["com.docker.compose.service"] == "worker") or
      ([.Config.Entrypoint // [], .Config.Cmd // []] | flatten | join(" ") | contains("gradex-worker")) or
      (((.Config.Image // "") | test("gradex.*backend"; "i")) and
       ((.Config.Cmd // []) != ["gradex-migrate", "up"]) and
       ((.Config.Cmd // []) != ["gradex-api"]))) | tostring' <<<"$inspection")" || die "invalid container inspection"
    [ "$worker" = true ] || continue
    db_url="$(jq -er '.[0].Config.Env // [] | map(select(startswith("DATABASE_URL="))) |
      if length == 1 then .[0] | ltrimstr("DATABASE_URL=") else error("ambiguous database") end' <<<"$inspection")" ||
      die "cannot prove database isolation for local GradeX/worker container $container"
    # Other URI options can override connection parameters; do not infer isolation
    # from the path when query parameters could select a different database.
    [[ "$db_url" =~ ^postgres(ql)?://[^/]+/([A-Za-z0-9_]+)(\?sslmode=(disable|allow|prefer|require|verify-ca|verify-full))?$ ]] || die "cannot parse worker database target for $container"
    db_target="${BASH_REMATCH[2]}"
    if [ "$container" = "$expected_worker" ] && [ "$db_target" = "$POSTGRES_DB" ]; then
      found_expected=true
      continue
    fi
    [ "$db_target" != "$POSTGRES_DB" ] || die "local GradeX/worker container $container targets the production database; remove the producer before migration"
  done
  [ -z "$expected_worker" ] || [ "$found_expected" = true ] || die "expected production worker absent from local inventory"
  note "local worker inventory accepted: only the expected production worker topology exists"
}
