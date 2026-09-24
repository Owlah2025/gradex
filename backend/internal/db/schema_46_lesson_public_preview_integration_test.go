//go:build integration

package db

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// legacyPreviewFixture is a Course whose live revision carries the legacy
// course-level preview: a separate PREVIEW asset and the
// course_revisions.preview_asset_version_id pointer at it. Production has real
// data in exactly this shape, and 0046 must leave every part of it alone.
type legacyPreviewFixture struct {
	courseID          string
	revisionID        string
	sectionID         string
	sectionIdentityID string
	lessonID          string
	previewAssetID    string
	previewVersion    string
	previewRendition  string
}

func seedLegacyPreviewCourse(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label string) legacyPreviewFixture {
	t.Helper()
	var f legacyPreviewFixture
	var accountID, videoAssetID, videoVersionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO accounts (normalized_email, email, role, status, display_name)
		VALUES ($1, $1, 'INSTRUCTOR', 'ACTIVE', 'Preview Owner') RETURNING id::text
	`, label+"-owner@example.test").Scan(&accountID); err != nil {
		t.Fatalf("seeding %s account: %v", label, err)
	}
	if err := pool.QueryRow(ctx,
		"INSERT INTO courses (owner_account_id, lifecycle) VALUES ($1::uuid, 'DRAFT') RETURNING id::text",
		accountID).Scan(&f.courseID); err != nil {
		t.Fatalf("seeding %s Course: %v", label, err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO course_revisions (course_id, state, revision_number, title_ar, title_en)
		VALUES ($1::uuid, 'APPROVED', 1, 'مقرر', 'Course') RETURNING id::text
	`, f.courseID).Scan(&f.revisionID); err != nil {
		t.Fatalf("seeding %s revision: %v", label, err)
	}
	// Sections and Lessons carry stable identities across revisions, so the
	// identity rows have to exist before the revision-scoped rows reference them.
	var sectionIdentityID, lessonIdentityID string
	if err := pool.QueryRow(ctx,
		"INSERT INTO course_section_identities (course_id) VALUES ($1::uuid) RETURNING id::text",
		f.courseID).Scan(&sectionIdentityID); err != nil {
		t.Fatalf("seeding %s section identity: %v", label, err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO course_sections (revision_id, course_id, section_identity_id, title_ar, title_en, position)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'قسم', 'Section', 0) RETURNING id::text
	`, f.revisionID, f.courseID, sectionIdentityID).Scan(&f.sectionID); err != nil {
		t.Fatalf("seeding %s section: %v", label, err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO course_lesson_identities (course_id, section_identity_id)
		VALUES ($1::uuid, $2::uuid) RETURNING id::text
	`, f.courseID, sectionIdentityID).Scan(&lessonIdentityID); err != nil {
		t.Fatalf("seeding %s lesson identity: %v", label, err)
	}

	// The Lesson's own protected VIDEO. This is the asset the new Lesson preview
	// serves from; it is not created or altered by 0046.
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_assets (kind, owner_account_id, course_id, visibility)
		VALUES ('VIDEO', $1::uuid, $2::uuid, 'PROTECTED') RETURNING id::text
	`, accountID, f.courseID).Scan(&videoAssetID); err != nil {
		t.Fatalf("seeding %s lesson video asset: %v", label, err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_asset_versions
		  (logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes)
		VALUES ($1::uuid, 'VIDEO', 'READY', 'video/k', 'v', 'video/mp4', 100) RETURNING id::text
	`, videoAssetID).Scan(&videoVersionID); err != nil {
		t.Fatalf("seeding %s lesson video version: %v", label, err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO course_lessons
		  (section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position, video_asset_version_id)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'درس', 'Lesson', 0, $5::uuid)
		RETURNING id::text
	`, f.sectionID, f.courseID, sectionIdentityID, lessonIdentityID, videoVersionID).Scan(&f.lessonID); err != nil {
		t.Fatalf("seeding %s lesson: %v", label, err)
	}
	f.sectionIdentityID = sectionIdentityID

	// The legacy separate PREVIEW asset and the revision pointer at it.
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_assets (kind, owner_account_id, course_id, visibility, preview_origin_revision_id)
		VALUES ('PREVIEW', $1::uuid, $2::uuid, 'PUBLIC_PREVIEW', $3::uuid) RETURNING id::text
	`, accountID, f.courseID, f.revisionID).Scan(&f.previewAssetID); err != nil {
		t.Fatalf("seeding %s legacy preview asset: %v", label, err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_asset_versions
		  (logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes)
		VALUES ($1::uuid, 'PREVIEW', 'READY', 'preview/k', 'v', 'video/mp4', 200) RETURNING id::text
	`, f.previewAssetID).Scan(&f.previewVersion); err != nil {
		t.Fatalf("seeding %s legacy preview version: %v", label, err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO video_renditions
		  (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms)
		VALUES ($1::uuid, '720p', 'preview/hls/720p/playlist.m3u8', 1280, 720, 2800, 1000) RETURNING id::text
	`, f.previewVersion).Scan(&f.previewRendition); err != nil {
		t.Fatalf("seeding %s legacy preview rendition: %v", label, err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE course_revisions SET preview_asset_version_id = $1::uuid WHERE id = $2::uuid
	`, f.previewVersion, f.revisionID); err != nil {
		t.Fatalf("seeding %s legacy preview pointer: %v", label, err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE courses SET lifecycle = 'PUBLISHED', live_revision_id = $1::uuid WHERE id = $2::uuid
	`, f.revisionID, f.courseID); err != nil {
		t.Fatalf("publishing %s Course: %v", label, err)
	}
	return f
}

// TestSchema46LessonPublicPreviewIsAdditiveAndLeavesLegacyIntact is the migration
// contract for 0046.
//
// The risk this test exists for is not that the column is missing. It is that
// introducing a Lesson-level preview quietly disturbs the legacy course-level
// preview that production is serving right now — by clearing the pointer, by
// retiring the PREVIEW asset, by deleting its rendition, or by backfilling the
// old preview onto a Lesson so it looks like an Instructor asked for it.
func TestSchema46LessonPublicPreviewIsAdditiveAndLeavesLegacyIntact(t *testing.T) {
	freshDatabase(t)
	m := openMigrator(t)
	if err := m.Migrate(uint(AutoEnhancementRecoverySchemaVersion)); err != nil {
		t.Fatalf("migrating to schema 45: %v", err)
	}
	pool := openPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	// Seeded BEFORE 0046, so these are genuinely pre-existing rows rather than
	// rows written by code that already knows about the column.
	legacy := seedLegacyPreviewCourse(t, ctx, pool, "preview46")

	if err := m.Migrate(uint(LessonPublicPreviewSchemaVersion)); err != nil {
		t.Fatalf("migrating to schema 46: %v", err)
	}

	// Existing Lessons default to false. An Instructor who has never opened the
	// new control has published nothing publicly.
	var allow bool
	if err := pool.QueryRow(ctx,
		"SELECT allow_public_preview FROM course_lessons WHERE id = $1::uuid", legacy.lessonID).Scan(&allow); err != nil {
		t.Fatalf("reading the new column on a pre-existing Lesson: %v", err)
	}
	if allow {
		t.Fatal("0046 backfilled an existing Lesson as publicly previewable")
	}
	var previewable int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM course_lessons WHERE allow_public_preview").Scan(&previewable); err != nil {
		t.Fatalf("counting previewable Lessons: %v", err)
	}
	if previewable != 0 {
		t.Fatalf("0046 marked %d Lessons previewable", previewable)
	}

	// The legacy pointer is still populated and still points at the same version.
	var pointer string
	if err := pool.QueryRow(ctx,
		"SELECT preview_asset_version_id::text FROM course_revisions WHERE id = $1::uuid", legacy.revisionID).Scan(&pointer); err != nil {
		t.Fatalf("legacy preview pointer after 0046: %v", err)
	}
	if pointer != legacy.previewVersion {
		t.Fatalf("legacy preview pointer = %q, want %q", pointer, legacy.previewVersion)
	}

	// The legacy PREVIEW asset, its version, and its rendition all survive
	// unretired and READY.
	var assetKind, visibility, versionState string
	var retiredAt *string
	var renditions int
	if err := pool.QueryRow(ctx, `
		SELECT ma.kind::text, ma.visibility::text, mav.state::text, ma.retired_at::text,
		       (SELECT count(*) FROM video_renditions vr WHERE vr.asset_version_id = mav.id)
		FROM media_asset_versions mav
		JOIN media_assets ma ON ma.id = mav.logical_asset_id
		WHERE mav.id = $1::uuid
	`, legacy.previewVersion).Scan(&assetKind, &visibility, &versionState, &retiredAt, &renditions); err != nil {
		t.Fatalf("legacy preview media after 0046: %v", err)
	}
	if assetKind != "PREVIEW" || visibility != "PUBLIC_PREVIEW" || versionState != "READY" ||
		retiredAt != nil || renditions != 1 {
		t.Fatalf("legacy preview media changed: kind=%s visibility=%s state=%s retired=%v renditions=%d",
			assetKind, visibility, versionState, retiredAt, renditions)
	}

	// Many previewable Lessons per Course is the intended model. There is no
	// uniqueness to trip over, and a Course with several is legal.
	var secondLesson, thirdLesson string
	for i, target := range []*string{&secondLesson, &thirdLesson} {
		var identityID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO course_lesson_identities (course_id, section_identity_id)
			VALUES ($1::uuid, $2::uuid) RETURNING id::text
		`, legacy.courseID, legacy.sectionIdentityID).Scan(&identityID); err != nil {
			t.Fatalf("seeding lesson identity %d: %v", i+1, err)
		}
		if err := pool.QueryRow(ctx, `
			INSERT INTO course_lessons
			  (section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position,
			   video_asset_version_id, allow_public_preview)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'درس', 'Lesson', $5,
			        (SELECT video_asset_version_id FROM course_lessons WHERE id = $6::uuid), true)
			RETURNING id::text
		`, legacy.sectionID, legacy.courseID, legacy.sectionIdentityID, identityID, i+1, legacy.lessonID).Scan(target); err != nil {
			t.Fatalf("marking Lesson %d previewable: %v", i+1, err)
		}
	}
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM course_lessons WHERE course_id = $1::uuid AND allow_public_preview",
		legacy.courseID).Scan(&previewable); err != nil {
		t.Fatalf("counting previewable Lessons: %v", err)
	}
	if previewable != 2 {
		t.Fatalf("previewable Lessons = %d, want 2; many previews per Course must be legal", previewable)
	}

	// Marking a Lesson previewable is authorization metadata and nothing else.
	// It must create no media row, no processing attempt, no rendition, and no
	// transcode intent. This is the whole reason there is no second upload.
	var assets, versions, attempts, renditionRows, transcodeEvents int
	if err := pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM media_assets WHERE course_id = $1::uuid),
		       (SELECT count(*) FROM media_asset_versions mav
		         JOIN media_assets ma ON ma.id = mav.logical_asset_id WHERE ma.course_id = $1::uuid),
		       (SELECT count(*) FROM processing_attempts pa
		         JOIN media_asset_versions mav ON mav.id = pa.asset_version_id
		         JOIN media_assets ma ON ma.id = mav.logical_asset_id WHERE ma.course_id = $1::uuid),
		       (SELECT count(*) FROM video_renditions vr
		         JOIN media_asset_versions mav ON mav.id = vr.asset_version_id
		         JOIN media_assets ma ON ma.id = mav.logical_asset_id WHERE ma.course_id = $1::uuid),
		       (SELECT count(*) FROM outbox_events
		         WHERE event_type IN ('media.transcode_requested', 'media.enhancement_requested', 'media.scan_requested'))
	`, legacy.courseID).Scan(&assets, &versions, &attempts, &renditionRows, &transcodeEvents); err != nil {
		t.Fatalf("counting media side effects: %v", err)
	}
	// Two assets and two versions: the Lesson video and the legacy preview, both
	// seeded before 0046. One rendition: the legacy preview's.
	if assets != 2 || versions != 2 || attempts != 0 || renditionRows != 1 || transcodeEvents != 0 {
		t.Fatalf("marking Lessons previewable had media side effects: assets=%d versions=%d attempts=%d renditions=%d transcode_events=%d",
			assets, versions, attempts, renditionRows, transcodeEvents)
	}

	// Down withdraws exposure and nothing else: the legacy preview keeps serving,
	// which is exactly why the legacy path is retained during the transition.
	if err := m.Migrate(uint(AutoEnhancementRecoverySchemaVersion)); err != nil {
		t.Fatalf("46 -> 45: %v", err)
	}
	var hasColumn bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.columns
		               WHERE table_name='course_lessons' AND column_name='allow_public_preview')
	`).Scan(&hasColumn); err != nil || hasColumn {
		t.Fatalf("allow_public_preview survived the rollback (present=%t err=%v)", hasColumn, err)
	}
	var lessons int
	if err := pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM course_lessons WHERE course_id = $1::uuid),
		       (SELECT preview_asset_version_id::text FROM course_revisions WHERE id = $2::uuid)
	`, legacy.courseID, legacy.revisionID).Scan(&lessons, &pointer); err != nil {
		t.Fatalf("state after rollback: %v", err)
	}
	if lessons != 3 || pointer != legacy.previewVersion {
		t.Fatalf("rollback disturbed content: lessons=%d pointer=%q", lessons, pointer)
	}

	// And forward again.
	if err := m.Migrate(uint(LessonPublicPreviewSchemaVersion)); err != nil {
		t.Fatalf("reapplying 0046: %v", err)
	}
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM course_lessons WHERE course_id = $1::uuid AND allow_public_preview",
		legacy.courseID).Scan(&previewable); err != nil {
		t.Fatalf("counting previewable Lessons after reapply: %v", err)
	}
	if previewable != 0 {
		t.Fatalf("reapplying 0046 restored %d previewable Lessons; the column default must be false", previewable)
	}
}
