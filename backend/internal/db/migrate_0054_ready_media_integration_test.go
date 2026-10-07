//go:build integration

package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// Production reached schema 46 holding READY media asset versions bound to
// lessons, lesson files and course previews. 0054 backfills a derived course_id
// onto every version, and media_asset_versions_immutable rejects any UPDATE of a
// READY version, so the first 0054 aborted against real data while passing on
// every fresh test database. This starts where production starts and walks the
// whole 46 -> 57 path.
func TestCourseAssetFKMigrationUpgradesReadyMediaFromSchema46(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	if err := m.Migrate(uint(LessonPublicPreviewSchemaVersion)); err != nil {
		t.Fatalf("migrating to the production schema 46: %v", err)
	}
	pool := openPool(t)
	ctx := context.Background()

	instructorID := "10000000-0000-0000-0000-000000005401"
	otherInstructorID := "10000000-0000-0000-0000-000000005402"
	courseID := "10000000-0000-0000-0000-000000005403"
	otherCourseID := "10000000-0000-0000-0000-000000005404"
	revisionID := "10000000-0000-0000-0000-000000005405"
	sectionIdentityID := "10000000-0000-0000-0000-000000005406"
	lessonIdentityID := "10000000-0000-0000-0000-000000005407"
	sectionID := "10000000-0000-0000-0000-000000005408"
	lessonID := "10000000-0000-0000-0000-000000005409"
	videoAssetID := "10000000-0000-0000-0000-000000005410"
	videoVersionID := "10000000-0000-0000-0000-000000005411"
	resourceAssetID := "10000000-0000-0000-0000-000000005412"
	resourceVersionID := "10000000-0000-0000-0000-000000005413"
	previewAssetID := "10000000-0000-0000-0000-000000005414"
	previewVersionID := "10000000-0000-0000-0000-000000005415"
	uploadedAssetID := "10000000-0000-0000-0000-000000005416"
	uploadedVersionID := "10000000-0000-0000-0000-000000005417"
	otherAssetID := "10000000-0000-0000-0000-000000005418"
	otherVersionID := "10000000-0000-0000-0000-000000005419"

	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
			VALUES ($1::uuid, 'fk54-instructor@example.test', 'fk54-instructor@example.test', 'INSTRUCTOR', 'ACTIVE', 'FK54 Instructor'),
			       ($2::uuid, 'fk54-other@example.test', 'fk54-other@example.test', 'INSTRUCTOR', 'ACTIVE', 'FK54 Other')`, []any{instructorID, otherInstructorID}},
		{`INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1::uuid, $2::uuid, 'DRAFT'), ($3::uuid, $4::uuid, 'DRAFT')`,
			[]any{courseID, instructorID, otherCourseID, otherInstructorID}},
		{`INSERT INTO media_assets (id, kind, owner_account_id, course_id, visibility)
			VALUES ($1::uuid, 'VIDEO', $4::uuid, $5::uuid, 'PROTECTED'),
			       ($2::uuid, 'RESOURCE', $4::uuid, $5::uuid, 'PROTECTED'),
			       ($3::uuid, 'PREVIEW', $4::uuid, $5::uuid, 'PUBLIC_PREVIEW'),
			       ($6::uuid, 'VIDEO', $4::uuid, $5::uuid, 'PROTECTED'),
			       ($7::uuid, 'VIDEO', $8::uuid, $9::uuid, 'PROTECTED')`,
			[]any{videoAssetID, resourceAssetID, previewAssetID, instructorID, courseID, uploadedAssetID, otherAssetID, otherInstructorID, otherCourseID}},
		{`INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes)
			VALUES ($1::uuid, $2::uuid, 'VIDEO', 'READY', 'fk54/lesson.mp4', 'v1', 'video/mp4', 1),
			       ($3::uuid, $4::uuid, 'RESOURCE', 'READY', 'fk54/notes.pdf', 'v1', 'application/pdf', 1),
			       ($5::uuid, $6::uuid, 'PREVIEW', 'READY', 'fk54/preview.mp4', 'v1', 'video/mp4', 1),
			       ($7::uuid, $8::uuid, 'VIDEO', 'UPLOADED', 'fk54/pending.mp4', 'v1', 'video/mp4', 1),
			       ($9::uuid, $10::uuid, 'VIDEO', 'READY', 'fk54/other.mp4', 'v1', 'video/mp4', 1)`,
			[]any{videoVersionID, videoAssetID, resourceVersionID, resourceAssetID, previewVersionID, previewAssetID, uploadedVersionID, uploadedAssetID, otherVersionID, otherAssetID}},
		{`INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en, preview_asset_version_id)
			VALUES ($1::uuid, $2::uuid, 'DRAFT', 1, 'مقرر', 'FK54 Course', $3::uuid)`, []any{revisionID, courseID, previewVersionID}},
		{`INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)`, []any{sectionIdentityID, courseID}},
		{`INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`, []any{lessonIdentityID, courseID, sectionIdentityID}},
		{`INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'قسم', 'Section', 0)`, []any{sectionID, revisionID, courseID, sectionIdentityID}},
		{`INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position, video_asset_version_id)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'درس', 'Lesson', 0, $6::uuid)`,
			[]any{lessonID, sectionID, courseID, sectionIdentityID, lessonIdentityID, videoVersionID}},
		{`INSERT INTO lesson_files (lesson_id, kind, asset_version_id, display_name_ar, display_name_en, position)
			VALUES ($1::uuid, 'RESOURCE', $2::uuid, 'ملف', 'Notes', 0)`, []any{lessonID, resourceVersionID}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seeding schema-46 READY media: %v\n%s", err, statement.query)
		}
	}

	// The precondition that broke the first 0054: READY versions refuse UPDATE.
	assertReadyVersionImmutable(t, ctx, pool, videoVersionID)

	if err := m.Migrate(uint(courseAssetFKSchemaVersion)); err != nil {
		t.Fatalf("0054 against READY media: %v", err)
	}
	if err := m.Up(); err != nil {
		t.Fatalf("continuing to schema %d: %v", MaxSchemaVersion, err)
	}
	state, err := ReadSchemaState(ctx, pool)
	if err != nil {
		t.Fatalf("reading schema state: %v", err)
	}
	if state.Version != MaxSchemaVersion || state.Dirty {
		t.Fatalf("schema after 46 -> %d is %+v", MaxSchemaVersion, state)
	}

	var enabled string
	if err := pool.QueryRow(ctx, `
		SELECT tgenabled::text FROM pg_trigger
		WHERE tgrelid = 'media_asset_versions'::regclass AND tgname = 'media_asset_versions_immutable'`).Scan(&enabled); err != nil {
		t.Fatalf("reading immutability trigger: %v", err)
	}
	if enabled != "O" {
		t.Fatalf("media_asset_versions_immutable is %q after migration, want enabled (O)", enabled)
	}
	assertReadyVersionImmutable(t, ctx, pool, videoVersionID)
	assertReadyVersionImmutable(t, ctx, pool, resourceVersionID)

	var mismatched, nullVersions, nullFiles int
	if err := pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM media_asset_versions mav JOIN media_assets ma ON ma.id = mav.logical_asset_id
			  WHERE mav.course_id IS DISTINCT FROM ma.course_id),
			(SELECT count(*) FROM media_asset_versions WHERE course_id IS NULL),
			(SELECT count(*) FROM lesson_files WHERE course_id IS NULL)`).Scan(&mismatched, &nullVersions, &nullFiles); err != nil {
		t.Fatalf("checking backfill: %v", err)
	}
	if mismatched != 0 || nullVersions != 0 || nullFiles != 0 {
		t.Fatalf("backfill mismatched=%d null versions=%d null lesson files=%d", mismatched, nullVersions, nullFiles)
	}
	for versionID, wantCourse := range map[string]string{
		videoVersionID: courseID, resourceVersionID: courseID, previewVersionID: courseID,
		uploadedVersionID: courseID, otherVersionID: otherCourseID,
	} {
		var got string
		if err := pool.QueryRow(ctx, `SELECT course_id::text FROM media_asset_versions WHERE id = $1::uuid`, versionID).Scan(&got); err != nil {
			t.Fatalf("reading course of %s: %v", versionID, err)
		}
		if got != wantCourse {
			t.Fatalf("version %s course_id = %s, want %s", versionID, got, wantCourse)
		}
	}

	// The new composite keys now refuse a cross-course binding.
	assertConstraintViolation(t, pool, ctx, "course_lessons_video_asset_course_fk",
		`UPDATE course_lessons SET video_asset_version_id = $1::uuid WHERE id = $2::uuid`, otherVersionID, lessonID)
}

func assertReadyVersionImmutable(t *testing.T, ctx context.Context, pool interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, versionID string) {
	t.Helper()
	_, err := pool.Exec(ctx, `UPDATE media_asset_versions SET storage_object_version = 'tampered' WHERE id = $1::uuid`, versionID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("mutating READY version %s: got %v, want the immutability trigger to refuse", versionID, err)
	}
	if pgErr.Code != "23001" || !strings.Contains(pgErr.Message, "READY media asset version is immutable") {
		t.Fatalf("mutating READY version %s: got %s %q, want restrict_violation from the immutability trigger", versionID, pgErr.Code, pgErr.Message)
	}
}

// courseAssetFKSchemaVersion is 0054; schema.go names the versions
// around it but not 0054 itself.
const courseAssetFKSchemaVersion = CatalogSearchAnalyticsSchemaVersion + 1

// The DISABLE TRIGGER in 0054 is safe only because the whole file is one
// transaction. A cross-course lesson binding makes 0054 fail after the trigger
// was disabled and re-enabled in the same file; nothing of 0054 may survive,
// and the trigger must be enabled on the untouched schema-53 shape.
func TestCourseAssetFKMigrationFailureLeavesImmutabilityTriggerEnabled(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	if err := m.Migrate(uint(courseAssetFKSchemaVersion - 1)); err != nil {
		t.Fatalf("migrating to schema 53: %v", err)
	}
	pool := openPool(t)
	ctx := context.Background()
	instructorID := "10000000-0000-0000-0000-000000005501"
	courseID := "10000000-0000-0000-0000-000000005502"
	otherCourseID := "10000000-0000-0000-0000-000000005503"
	revisionID := "10000000-0000-0000-0000-000000005504"
	sectionIdentityID := "10000000-0000-0000-0000-000000005505"
	lessonIdentityID := "10000000-0000-0000-0000-000000005506"
	sectionID := "10000000-0000-0000-0000-000000005507"
	lessonID := "10000000-0000-0000-0000-000000005508"
	assetID := "10000000-0000-0000-0000-000000005509"
	versionID := "10000000-0000-0000-0000-000000005510"
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
			VALUES ($1::uuid, 'fk54-fail@example.test', 'fk54-fail@example.test', 'INSTRUCTOR', 'ACTIVE', 'FK54 Fail')`, []any{instructorID}},
		{`INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1::uuid, $3::uuid, 'DRAFT'), ($2::uuid, $3::uuid, 'DRAFT')`, []any{courseID, otherCourseID, instructorID}},
		{`INSERT INTO media_assets (id, kind, owner_account_id, course_id, visibility) VALUES ($1::uuid, 'VIDEO', $2::uuid, $3::uuid, 'PROTECTED')`, []any{assetID, instructorID, otherCourseID}},
		{`INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes)
			VALUES ($1::uuid, $2::uuid, 'VIDEO', 'READY', 'fk54/fail.mp4', 'v1', 'video/mp4', 1)`, []any{versionID, assetID}},
		{`INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, 'DRAFT', 1, 'مقرر', 'Course')`, []any{revisionID, courseID}},
		{`INSERT INTO course_section_identities (id, course_id) VALUES ($1::uuid, $2::uuid)`, []any{sectionIdentityID, courseID}},
		{`INSERT INTO course_lesson_identities (id, course_id, section_identity_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`, []any{lessonIdentityID, courseID, sectionIdentityID}},
		{`INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'قسم', 'Section', 0)`, []any{sectionID, revisionID, courseID, sectionIdentityID}},
		{`INSERT INTO course_lessons (id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position, video_asset_version_id)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'درس', 'Lesson', 0, $6::uuid)`,
			[]any{lessonID, sectionID, courseID, sectionIdentityID, lessonIdentityID, versionID}},
	} {
		if _, err := pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seeding cross-course binding: %v\n%s", err, statement.query)
		}
	}

	if err := m.Steps(1); err == nil {
		t.Fatal("0054 accepted a cross-course lesson video binding")
	}
	var columnExists bool
	var enabled string
	if err := pool.QueryRow(ctx, `
		SELECT
			EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'media_asset_versions' AND column_name = 'course_id'),
			(SELECT tgenabled::text FROM pg_trigger WHERE tgrelid = 'media_asset_versions'::regclass AND tgname = 'media_asset_versions_immutable')`).
		Scan(&columnExists, &enabled); err != nil {
		t.Fatalf("inspecting schema after failed 0054: %v", err)
	}
	if columnExists {
		t.Fatal("failed 0054 left media_asset_versions.course_id behind; the file did not run as one transaction")
	}
	if enabled != "O" {
		t.Fatalf("failed 0054 left media_asset_versions_immutable %q, want enabled (O)", enabled)
	}
	assertReadyVersionImmutable(t, ctx, pool, versionID)
}
