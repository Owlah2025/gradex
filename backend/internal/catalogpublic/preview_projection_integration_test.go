//go:build integration

package catalogpublic

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPublicPreviewProjectionRequiresReadyExactVersionProvenance(t *testing.T) {
	freshCatalogPublicSchema(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, catalogPublicTestDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	courseID := seedVisibleDetailCourse(t, pool, ctx)
	var liveRevisionID string
	if err := pool.QueryRow(ctx, `
		SELECT live_revision_id::text FROM courses WHERE id = $1::uuid
	`, courseID).Scan(&liveRevisionID); err != nil {
		t.Fatalf("reading live revision: %v", err)
	}
	repository, err := NewRepository(pool, PublishedOnly)
	if err != nil {
		t.Fatal(err)
	}

	scannerPreviewID := seedPublicPreview(t, pool, ctx, courseID, liveRevisionID)
	selectPublicPreview(t, pool, ctx, liveRevisionID, scannerPreviewID)
	assertProjectedHasPreview(t, repository, ctx, courseID, true, "scanner-backed READY preview")

	trustedPreviewID := seedValidatedPublicPreview(t, pool, ctx, courseID, liveRevisionID)
	selectPublicPreview(t, pool, ctx, liveRevisionID, trustedPreviewID)
	assertProjectedHasPreview(t, repository, ctx, courseID, false, "trusted preview before READY")

	makeValidatedPublicPreviewReady(t, pool, ctx, trustedPreviewID)
	assertProjectedHasPreview(t, repository, ctx, courseID, true, "validation-backed READY preview")

	unprovenPreviewID := seedUnprovenReadyPublicPreview(t, pool, ctx, courseID, liveRevisionID, "fixture-unproven")
	selectPublicPreview(t, pool, ctx, liveRevisionID, unprovenPreviewID)
	assertProjectedHasPreview(t, repository, ctx, courseID, false, "READY preview without provenance")

	mismatchedPreviewID := seedMismatchedValidationPublicPreview(t, pool, ctx, courseID, liveRevisionID)
	selectPublicPreview(t, pool, ctx, liveRevisionID, mismatchedPreviewID)
	assertProjectedHasPreview(t, repository, ctx, courseID, false, "READY preview with validation for different bytes")
}

func assertProjectedHasPreview(
	t *testing.T,
	repository *Repository,
	ctx context.Context,
	courseID string,
	want bool,
	stage string,
) {
	t.Helper()
	detail, err := repository.Detail(ctx, courseID, false)
	if err != nil || detail == nil {
		t.Fatalf("%s Detail = %#v, %v; want a public Course", stage, detail, err)
	}
	if detail.HasPreview != want {
		t.Fatalf("%s Detail has_preview = %t, want %t", stage, detail.HasPreview, want)
	}

	list, err := repository.List(ctx, false, 1, 10)
	if err != nil {
		t.Fatalf("%s List: %v", stage, err)
	}
	if len(list.Items) != 1 || list.Items[0].ID != courseID {
		t.Fatalf("%s List items = %#v, want only Course %s", stage, list.Items, courseID)
	}
	if list.Items[0].HasPreview != want {
		t.Fatalf("%s List has_preview = %t, want %t", stage, list.Items[0].HasPreview, want)
	}
}

func selectPublicPreview(
	t *testing.T,
	pool *pgxpool.Pool,
	ctx context.Context,
	revisionID string,
	versionID string,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		UPDATE course_revisions SET preview_asset_version_id = $1::uuid WHERE id = $2::uuid
	`, versionID, revisionID); err != nil {
		t.Fatalf("selecting public preview: %v", err)
	}
}

func seedValidatedPublicPreview(
	t *testing.T,
	pool *pgxpool.Pool,
	ctx context.Context,
	courseID string,
	revisionID string,
) string {
	t.Helper()
	versionID, key := insertPublicPreviewVersion(
		t, pool, ctx, courseID, revisionID, "QUARANTINED", "fixture-trusted",
	)
	if _, err := pool.Exec(ctx, `
		INSERT INTO upload_intents (
			asset_version_id, expected_object_key, expected_content_type,
			expected_size_bytes, max_size_bytes, expires_at
		) VALUES ($1::uuid, $2, 'video/mp4', 1024, 2048, now() + interval '15 minutes')
	`, versionID, key); err != nil {
		t.Fatalf("seeding trusted preview upload intent: %v", err)
	}

	attemptID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		INSERT INTO validation_attempts (
			id, asset_version_id, attempt_number, work_id, storage_object_version, outcome,
			validator_identity, profile, declared_content_type, verified_size_bytes,
			max_size_bytes, sha256_hex
		) VALUES ($1::uuid, $2::uuid, 1, $3, 'fixture-trusted', 'PASSED',
			'gradex-media-exact-version-validator', 'D-088-TRUSTED-INSTRUCTOR',
			'video/mp4', 1024, 2048, repeat('a', 64))
	`, attemptID, versionID, "validation:"+versionID); err != nil {
		t.Fatalf("seeding trusted preview validation: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE media_asset_versions
		SET state = 'VALIDATED', successful_validation_attempt_id = $1::uuid
		WHERE id = $2::uuid
	`, attemptID, versionID); err != nil {
		t.Fatalf("validating trusted preview: %v", err)
	}
	return versionID
}

func makeValidatedPublicPreviewReady(
	t *testing.T,
	pool *pgxpool.Pool,
	ctx context.Context,
	versionID string,
) {
	t.Helper()
	attemptID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		INSERT INTO processing_attempts (
			id, asset_version_id, operation_id, state, output_prefix,
			rendition_count, trusted_duration_ms
		) VALUES ($1::uuid, $2::uuid, $3, 'SUCCEEDED', 'preview/hls', 1, 45000)
	`, attemptID, versionID, "processing:"+versionID); err != nil {
		t.Fatalf("seeding trusted preview processing: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE media_asset_versions SET state = 'PROCESSING' WHERE id = $1::uuid
	`, versionID); err != nil {
		t.Fatalf("starting trusted preview processing: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE media_asset_versions
		SET state = 'READY', successful_processing_attempt_id = $1::uuid,
			trusted_duration_ms = 45000
		WHERE id = $2::uuid
	`, attemptID, versionID); err != nil {
		t.Fatalf("making trusted preview READY: %v", err)
	}
}

func seedUnprovenReadyPublicPreview(
	t *testing.T,
	pool *pgxpool.Pool,
	ctx context.Context,
	courseID string,
	revisionID string,
	storageObjectVersion string,
) string {
	t.Helper()
	versionID, _ := insertPublicPreviewVersion(
		t, pool, ctx, courseID, revisionID, "READY", storageObjectVersion,
	)
	return versionID
}

func seedMismatchedValidationPublicPreview(
	t *testing.T,
	pool *pgxpool.Pool,
	ctx context.Context,
	courseID string,
	revisionID string,
) string {
	t.Helper()
	versionID := seedUnprovenReadyPublicPreview(
		t, pool, ctx, courseID, revisionID, "fixture-current",
	)

	// The production schema rejects this stale evidence before it can be linked.
	// Bypass its triggers only inside this transaction so the catalogue's own
	// exact-version predicate is proved independently. SET LOCAL is reset by
	// commit, while the deferred rollback covers every failure path; the FK and
	// immutable trigger definitions therefore remain installed and enabled.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning corrupt-state fixture transaction: %v", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
		t.Fatalf("scoping corrupt-state fixture bypass: %v", err)
	}
	attemptID := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		INSERT INTO validation_attempts (
			id, asset_version_id, attempt_number, work_id, storage_object_version, outcome,
			validator_identity, profile, declared_content_type, verified_size_bytes,
			max_size_bytes, sha256_hex
		) VALUES ($1::uuid, $2::uuid, 1, $3, 'fixture-stale', 'PASSED',
			'fixture', 'D-088-TRUSTED-INSTRUCTOR', 'video/mp4', 1024, 2048,
			repeat('a', 64))
	`, attemptID, versionID, "stale-validation:"+versionID); err != nil {
		t.Fatalf("seeding stale validation evidence: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE media_asset_versions SET successful_validation_attempt_id = $1::uuid
		WHERE id = $2::uuid
	`, attemptID, versionID); err != nil {
		t.Fatalf("linking stale validation evidence: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing corrupt-state fixture: %v", err)
	}
	return versionID
}

func insertPublicPreviewVersion(
	t *testing.T,
	pool *pgxpool.Pool,
	ctx context.Context,
	courseID string,
	revisionID string,
	state string,
	storageObjectVersion string,
) (string, string) {
	t.Helper()
	var ownerID string
	if err := pool.QueryRow(ctx, `
		SELECT owner_account_id::text FROM courses WHERE id = $1::uuid
	`, courseID).Scan(&ownerID); err != nil {
		t.Fatalf("reading Course owner: %v", err)
	}
	assetID, versionID := uuid.NewString(), uuid.NewString()
	key := "quarantine/" + courseID + "/" + versionID + "/source"
	if _, err := pool.Exec(ctx, `
		INSERT INTO media_assets (
			id, kind, owner_account_id, course_id, preview_origin_revision_id, visibility
		) VALUES ($1::uuid, 'PREVIEW', $2::uuid, $3::uuid, $4::uuid, 'PUBLIC_PREVIEW')
	`, assetID, ownerID, courseID, revisionID); err != nil {
		t.Fatalf("seeding public preview asset: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO media_asset_versions (
			id, logical_asset_id, kind, state, storage_object_key, storage_object_version,
			content_type, size_bytes, sha256_hex
		) VALUES ($1::uuid, $2::uuid, 'PREVIEW', $3::media_asset_version_state,
			$4, $5, 'video/mp4', 1024, repeat('a', 64))
	`, versionID, assetID, state, key, storageObjectVersion); err != nil {
		t.Fatalf("seeding public preview version: %v", err)
	}
	return versionID, key
}
