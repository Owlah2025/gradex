#!/usr/bin/env bash
# Shared by the trusted importer and the release-local host wrapper.

artifact_value() {
  local value
  value="$(awk -F= -v key="$2" '$1 == key { count++; value=substr($0,length(key)+2) } END { if(count != 1) exit 1; print value }' "$1")" ||
    die "missing or duplicate artifact field $2"
  [[ "$value" =~ ^[A-Za-z0-9:._/-]+$ ]] || die "invalid artifact field $2"
  printf '%s' "$value"
}

verify_release_bundle() {
  local release_dir="$1" tooling="$1/tooling" manifest="$1/release.env" revision="$2" digest
  [ -f "$tooling/release-tooling.env" ] || die "missing bundle metadata"
  [ "$(artifact_value "$manifest" GRADEX_RELEASE_SHA)" = "$revision" ] || die "manifest release mismatch"
  [ "$(artifact_value "$tooling/release-tooling.env" RELEASE_SHA)" = "$revision" ] || die "bundle release mismatch"
  [ "$(artifact_value "$tooling/release-tooling.env" DEPLOY_BUNDLE_FORMAT)" = 1 ] || die "unsupported bundle format"
  [ "$(artifact_value "$tooling/release-tooling.env" SCHEMA41_CAPABILITY)" = supervised-41-to-40-v1 ] || die "bundle lacks supervised rollback capability"
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

require_release_artifact() {
  local release_dir="$S12_HOST_STATE_DIR/releases/$GRADEX_RELEASE_SHA" key
  [ "$S12_ROOT" = "$release_dir/tooling" ] || die "execute host.sh from the selected imported release tooling"
  verify_release_bundle "$release_dir" "$GRADEX_RELEASE_SHA"
  for key in GRADEX_BACKEND_IMAGE GRADEX_FRONTEND_IMAGE GRADEX_PROOF_IMAGE; do
    [ "${!key}" = "$(artifact_value "$release_dir/release.env" "$key")" ] || die "runtime image selection disagrees with manifest"
  done
  verify_release_images "$release_dir/release.env" "$GRADEX_RELEASE_SHA"
}

require_schema41_image_capability() {
  local usage status=0 migration expected actual
  usage="$(docker run --rm --network none --entrypoint gradex-migrate "$GRADEX_BACKEND_IMAGE" 2>&1)" || status=$?
  [ "$status" = 1 ] && [ "$usage" = 'migrate: usage: migrate <up|down|version|max-version|rollback-schema-41> [steps]' ] ||
    die "backend lacks supervised rollback capability"
  for migration in 0041_enhancement_recovery_foundation.{up,down}.sql; do
    expected="$(sha256sum "$S12_ROOT/backend/internal/db/migrations/$migration")"
    actual="$(docker run --rm --network none --entrypoint sha256sum "$GRADEX_BACKEND_IMAGE" "internal/db/migrations/$migration")" || die "backend lacks migration 0041"
    [ "${actual%% *}" = "${expected%% *}" ] || die "backend migration 0041 differs from tooling bundle"
  done
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
