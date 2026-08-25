//go:build integration

package catalogpublic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	catalogPublicAdminDSN = "postgres://gradex:gradex@localhost:5432/postgres?sslmode=disable"
	catalogPublicTestDB   = "gradex_catalogpublic_test"
	catalogPublicTestDSN  = "postgres://gradex:gradex@localhost:5432/" + catalogPublicTestDB + "?sslmode=disable"
)

func TestDetailSecondaryReadsRecheckPublishedOnly(t *testing.T) {
	freshCatalogPublicSchema(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, catalogPublicTestDSN)
	if err != nil {
		t.Fatalf("opening test database: %v", err)
	}
	t.Cleanup(pool.Close)
	courseID := seedVisibleDetailCourse(t, pool, ctx)
	repository, err := NewRepository(pool, PublishedOnly)
	if err != nil {
		t.Fatalf("constructing repository: %v", err)
	}

	rows, err := pool.Query(ctx, repository.projectionQuery(repository.visibility("c", "cr"), publicCourseIdentifierPredicate(courseID), ``), true, courseID)
	if err != nil {
		t.Fatalf("reading initial detail projection: %v", err)
	}
	items, err := scanCourses(rows, true)
	rows.Close()
	if err != nil || len(items) != 1 {
		t.Fatalf("initial public detail projection = %#v, %v; want one visible course", items, err)
	}

	if _, err := pool.Exec(ctx, `UPDATE courses SET lifecycle = 'DELISTED' WHERE id = $1::uuid`, courseID); err != nil {
		t.Fatalf("hiding course between detail reads: %v", err)
	}
	description, descriptionVisible, err := repository.description(ctx, courseID, true)
	if err != nil {
		t.Fatalf("loading hidden description: %v", err)
	}
	if descriptionVisible || description != "" {
		t.Fatalf("hidden secondary description = %q, visible=%t; want no data", description, descriptionVisible)
	}
	sections, sectionsVisible, err := repository.sections(ctx, courseID, true)
	if err != nil {
		t.Fatalf("loading hidden sections: %v", err)
	}
	if sectionsVisible || len(sections) != 0 {
		t.Fatalf("hidden secondary sections = %#v, visible=%t; want no data", sections, sectionsVisible)
	}
}

func TestPublishedOnlyHidesSuspendedAndRetiredCourses(t *testing.T) {
	freshCatalogPublicSchema(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, catalogPublicTestDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	courseID := seedVisibleDetailCourse(t, pool, ctx)
	repository, err := NewRepository(pool, PublishedOnly)
	if err != nil {
		t.Fatal(err)
	}

	assertVisible := func(label string, wantVisible bool) {
		t.Helper()
		detail, err := repository.Detail(ctx, courseID, false)
		if err != nil {
			t.Fatalf("%s public detail error: %v", label, err)
		}
		if got := detail != nil; got != wantVisible {
			t.Fatalf("%s public detail visible=%t, want %t", label, got, wantVisible)
		}
	}

	assertVisible("published", true)
	if _, err := pool.Exec(ctx, `UPDATE courses SET access_suspended_at = now(), access_suspension_reason = 'MED-01 fixture' WHERE id = $1::uuid`, courseID); err != nil {
		t.Fatalf("suspending Course access: %v", err)
	}
	assertVisible("access-suspended", false)

	if _, err := pool.Exec(ctx, `UPDATE courses SET access_suspended_at = NULL, access_suspension_reason = NULL WHERE id = $1::uuid`, courseID); err != nil {
		t.Fatalf("restoring Course access: %v", err)
	}
	assertVisible("restored", true)

	if _, err := pool.Exec(ctx, `UPDATE courses SET retired_at = now() WHERE id = $1::uuid`, courseID); err != nil {
		t.Fatalf("retiring Course: %v", err)
	}
	assertVisible("retired", false)
}

func TestPublicProjectionExposesOnlyReadyLiveRevisionPreview(t *testing.T) {
	freshCatalogPublicSchema(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, catalogPublicTestDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	courseID := seedVisibleDetailCourse(t, pool, ctx)
	var liveRevisionID string
	if err := pool.QueryRow(ctx, `SELECT live_revision_id::text FROM courses WHERE id = $1::uuid`, courseID).Scan(&liveRevisionID); err != nil {
		t.Fatal(err)
	}
	livePreviewID := seedPublicPreview(t, pool, ctx, courseID, liveRevisionID)
	if _, err := pool.Exec(ctx, `UPDATE course_revisions SET preview_asset_version_id = $1::uuid WHERE id = $2::uuid`, livePreviewID, liveRevisionID); err != nil {
		t.Fatal(err)
	}

	candidateRevisionID := "22222222-2222-2222-2222-222222222222"
	if _, err := pool.Exec(ctx, `INSERT INTO course_revisions (id, course_id, based_on_revision_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, $3::uuid, 'DRAFT', 2, 'مرشح', 'Candidate')`, candidateRevisionID, courseID, liveRevisionID); err != nil {
		t.Fatal(err)
	}
	candidatePreviewID := seedPublicPreview(t, pool, ctx, courseID, candidateRevisionID)
	if _, err := pool.Exec(ctx, `UPDATE course_revisions SET preview_asset_version_id = $1::uuid WHERE id = $2::uuid`, candidatePreviewID, candidateRevisionID); err != nil {
		t.Fatal(err)
	}

	repository, err := NewRepository(pool, PublishedOnly)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := repository.Detail(ctx, courseID, false)
	if err != nil || detail == nil || !detail.HasPreview {
		t.Fatalf("live ready preview projection=%#v err=%v", detail, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE media_assets SET retired_at = now() WHERE id = (SELECT logical_asset_id FROM media_asset_versions WHERE id = $1::uuid)`, livePreviewID); err != nil {
		t.Fatal(err)
	}
	detail, err = repository.Detail(ctx, courseID, false)
	if err != nil || detail == nil || detail.HasPreview {
		t.Fatalf("retired live preview or candidate leaked into projection=%#v err=%v", detail, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE media_assets SET retired_at = NULL WHERE id = (SELECT logical_asset_id FROM media_asset_versions WHERE id = $1::uuid)`, livePreviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE course_revisions SET preview_asset_version_id = NULL WHERE id = $1::uuid`, liveRevisionID); err != nil {
		t.Fatal(err)
	}
	detail, err = repository.Detail(ctx, courseID, false)
	if err != nil || detail == nil || detail.HasPreview {
		t.Fatalf("no-preview live revision projected a candidate preview=%#v err=%v", detail, err)
	}
}

func seedPublicPreview(t *testing.T, pool *pgxpool.Pool, ctx context.Context, courseID, revisionID string) string {
	t.Helper()
	var ownerID string
	if err := pool.QueryRow(ctx, `SELECT owner_account_id::text FROM courses WHERE id = $1::uuid`, courseID).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	assetID := "30000000-0000-0000-0000-" + revisionID[24:]
	versionID := "40000000-0000-0000-0000-" + revisionID[24:]
	scanID := "50000000-0000-0000-0000-" + revisionID[24:]
	if _, err := pool.Exec(ctx, `INSERT INTO media_assets (id, kind, owner_account_id, course_id, preview_origin_revision_id, visibility) VALUES ($1::uuid, 'PREVIEW', $2::uuid, $3::uuid, $4::uuid, 'PUBLIC_PREVIEW')`, assetID, ownerID, courseID, revisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes) VALUES ($1::uuid, $2::uuid, 'PREVIEW', 'QUARANTINED', $3, 'fixture-v1', 'video/mp4', 1024)`, versionID, assetID, "quarantine/"+courseID+"/"+versionID+"/source"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO scan_attempts (id, asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity) VALUES ($1::uuid, $2::uuid, 1, $3, 'fixture-v1', 'PASSED', 'fixture')`, scanID, versionID, "scan:"+versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE media_asset_versions SET state = 'SCANNING' WHERE id = $1::uuid`, versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE media_asset_versions SET successful_scan_attempt_id = $1::uuid, state = 'SCAN_PASSED' WHERE id = $2::uuid`, scanID, versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE media_asset_versions SET state = 'READY' WHERE id = $1::uuid`, versionID); err != nil {
		t.Fatal(err)
	}
	return versionID
}

func freshCatalogPublicSchema(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, catalogPublicAdminDSN)
	if err != nil {
		t.Fatalf("opening PostgreSQL admin connection: %v", err)
	}
	defer admin.Close()
	_, _ = admin.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, catalogPublicTestDB)
	_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+catalogPublicTestDB)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+catalogPublicTestDB); err != nil {
		t.Fatalf("creating test database: %v", err)
	}
	migrator, err := migrate.New("file://../db/migrations", catalogPublicTestDSN)
	if err != nil {
		t.Fatalf("creating migrator: %v", err)
	}
	defer migrator.Close()
	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrating test database: %v", err)
	}
}

func seedVisibleDetailCourse(t *testing.T, pool *pgxpool.Pool, ctx context.Context) string {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO accounts (id, normalized_email, email, role, status, display_name) VALUES ('11111111-1111-1111-1111-111111111111', 'owner@example.test', 'owner@example.test', 'INSTRUCTOR', 'ACTIVE', 'Owner')`); err != nil {
		t.Fatalf("seeding owner: %v", err)
	}
	var courseID, revisionID, sectionIdentityID string
	if err := pool.QueryRow(ctx, `INSERT INTO courses (owner_account_id, lifecycle) VALUES ('11111111-1111-1111-1111-111111111111', 'DRAFT') RETURNING id::text`).Scan(&courseID); err != nil {
		t.Fatalf("seeding course: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO course_revisions (course_id, state, revision_number, title_ar, title_en, description_ar, description_en) VALUES ($1::uuid, 'APPROVED', 1, 'عنوان', 'Title', 'وصف', 'Description') RETURNING id::text`, courseID).Scan(&revisionID); err != nil {
		t.Fatalf("seeding revision: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE courses SET lifecycle = 'PUBLISHED', live_revision_id = $1::uuid WHERE id = $2::uuid`, revisionID, courseID); err != nil {
		t.Fatalf("publishing course: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO course_section_identities (course_id) VALUES ($1::uuid) RETURNING id::text`, courseID).Scan(&sectionIdentityID); err != nil {
		t.Fatalf("seeding section identity: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO course_sections (revision_id, course_id, section_identity_id, title_ar, title_en, position) VALUES ($1::uuid, $2::uuid, $3::uuid, 'قسم', 'Section', 0)`, revisionID, courseID, sectionIdentityID); err != nil {
		t.Fatalf("seeding section: %v", err)
	}
	return courseID
}
