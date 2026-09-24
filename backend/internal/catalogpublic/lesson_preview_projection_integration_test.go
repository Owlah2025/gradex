//go:build integration

package catalogpublic

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedPreviewableLesson attaches a READY Lesson video with a canonical rendition
// to the given revision's first section, and returns the Lesson identity.
//
// It deliberately builds the Lesson's OWN protected VIDEO asset rather than a
// PREVIEW-kind one: the whole point of the new model is that an anonymous visitor
// watches the same asset a paying Student watches.
func seedPreviewableLesson(
	t *testing.T,
	pool *pgxpool.Pool,
	ctx context.Context,
	courseID, revisionID string,
	position int,
	allowPreview bool,
	ready bool,
) (lessonIdentityID string) {
	t.Helper()
	var sectionRow, sectionIdentityID string
	if err := pool.QueryRow(ctx, `
		SELECT cs.id::text, cs.section_identity_id::text FROM course_sections cs
		WHERE cs.revision_id = $1::uuid ORDER BY cs.position LIMIT 1
	`, revisionID).Scan(&sectionRow, &sectionIdentityID); err != nil {
		t.Fatalf("reading the section: %v", err)
	}

	assetID, versionID, scanID, processingID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := pool.Exec(ctx, `
		INSERT INTO media_assets (id, kind, owner_account_id, course_id, visibility)
		VALUES ($1::uuid, 'VIDEO', '11111111-1111-1111-1111-111111111111'::uuid, $2::uuid, 'PROTECTED')
	`, assetID, courseID); err != nil {
		t.Fatalf("seeding lesson video asset: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO media_asset_versions
		  (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes)
		VALUES ($1::uuid, $2::uuid, 'VIDEO', 'QUARANTINED', $3, 'v1', 'video/mp4', 12)
	`, versionID, assetID, "quarantine/"+versionID+"/source"); err != nil {
		t.Fatalf("seeding lesson video version: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO scan_attempts (id, asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity)
		VALUES ($1::uuid, $2::uuid, 1, $3, 'v1', 'PASSED', 'fixture')
	`, scanID, versionID, "scan:"+versionID); err != nil {
		t.Fatalf("seeding scan attempt: %v", err)
	}
	for _, statement := range []string{
		"UPDATE media_asset_versions SET state='SCANNING' WHERE id=$1::uuid",
		"UPDATE media_asset_versions SET successful_scan_attempt_id='" + scanID + "'::uuid, state='SCAN_PASSED' WHERE id=$1::uuid",
	} {
		if _, err := pool.Exec(ctx, statement, versionID); err != nil {
			t.Fatalf("advancing the version: %v", err)
		}
	}
	if ready {
		if _, err := pool.Exec(ctx, `
			INSERT INTO processing_attempts (id, asset_version_id, operation_id, state, output_prefix, rendition_count, trusted_duration_ms)
			VALUES ($1::uuid, $2::uuid, $3, 'SUCCEEDED', 'video/hls', 1, 60000)
		`, processingID, versionID, "process:"+versionID); err != nil {
			t.Fatalf("seeding processing attempt: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO video_renditions (asset_version_id, name, storage_object_key, width, height, bitrate_kbps, duration_ms)
			VALUES ($1::uuid, '720p', $2, 1280, 720, 2800, 60000)
		`, versionID, "video/hls/"+versionID+"/720p/playlist.m3u8"); err != nil {
			t.Fatalf("seeding rendition: %v", err)
		}
		if _, err := pool.Exec(ctx, "UPDATE media_asset_versions SET state='PROCESSING' WHERE id=$1::uuid", versionID); err != nil {
			t.Fatalf("advancing to PROCESSING: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			UPDATE media_asset_versions
			SET successful_processing_attempt_id=$1::uuid, trusted_duration_ms=60000, state='READY'
			WHERE id=$2::uuid
		`, processingID, versionID); err != nil {
			t.Fatalf("advancing to READY: %v", err)
		}
	}

	lessonIdentityID = uuid.NewString()
	if _, err := pool.Exec(ctx, `
		INSERT INTO course_lesson_identities (id, course_id, section_identity_id)
		VALUES ($1::uuid, $2::uuid, $3::uuid)
	`, lessonIdentityID, courseID, sectionIdentityID); err != nil {
		t.Fatalf("seeding lesson identity: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO course_lessons
		  (section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position,
		   video_asset_version_id, allow_public_preview)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7, $8::uuid, $9)
	`, sectionRow, courseID, sectionIdentityID, lessonIdentityID,
		"درس", "Lesson", position, versionID, allowPreview); err != nil {
		t.Fatalf("seeding lesson: %v", err)
	}
	return lessonIdentityID
}

func liveRevisionOf(t *testing.T, pool *pgxpool.Pool, ctx context.Context, courseID string) string {
	t.Helper()
	var revisionID string
	if err := pool.QueryRow(ctx,
		"SELECT live_revision_id::text FROM courses WHERE id = $1::uuid", courseID).Scan(&revisionID); err != nil {
		t.Fatalf("reading live revision: %v", err)
	}
	return revisionID
}

func publicRepository(t *testing.T, pool *pgxpool.Pool) *Repository {
	t.Helper()
	repository, err := NewRepository(pool, PublishedOnly)
	if err != nil {
		t.Fatal(err)
	}
	return repository
}

// TestPublicCurriculumExposesOnlyPreviewableLessons is the projection contract.
//
// A visitor who has not paid may see WHICH Lessons they can watch, and nothing
// more about the ones they cannot. Zero, one, and many previewable Lessons all
// have to work, because many is the intended model rather than an edge case.
func TestPublicCurriculumExposesOnlyPreviewableLessons(t *testing.T) {
	freshCatalogPublicSchema(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, catalogPublicTestDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	courseID := seedVisibleDetailCourse(t, pool, ctx)
	revisionID := liveRevisionOf(t, pool, ctx, courseID)
	repository := publicRepository(t, pool)

	// Zero previewable Lessons: the curriculum shows sections and counts, and no
	// Lesson identities at all.
	notPreviewable := seedPreviewableLesson(t, pool, ctx, courseID, revisionID, 0, false, true)
	detail, err := repository.Detail(ctx, courseID, false)
	if err != nil || detail == nil {
		t.Fatalf("Detail = %#v, %v", detail, err)
	}
	if len(detail.Sections) != 1 {
		t.Fatalf("sections = %d, want 1", len(detail.Sections))
	}
	if len(detail.Sections[0].Lessons) != 0 {
		t.Fatalf("an unflagged Lesson was exposed: %+v", detail.Sections[0].Lessons)
	}
	if detail.Sections[0].LessonCount != 1 {
		t.Fatalf("lesson_count = %d, want the whole curriculum count of 1", detail.Sections[0].LessonCount)
	}
	if detail.HasPreview {
		t.Fatal("has_preview is true with neither a legacy nor a Lesson preview")
	}

	// One previewable Lesson.
	first := seedPreviewableLesson(t, pool, ctx, courseID, revisionID, 1, true, true)
	detail, err = repository.Detail(ctx, courseID, false)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if len(detail.Sections[0].Lessons) != 1 || detail.Sections[0].Lessons[0].ID != first {
		t.Fatalf("previewable lessons = %+v, want only %s", detail.Sections[0].Lessons, first)
	}
	if !detail.HasPreview {
		t.Fatal("has_preview is false with one previewable Lesson")
	}

	// Many previewable Lessons, in curriculum order.
	second := seedPreviewableLesson(t, pool, ctx, courseID, revisionID, 2, true, true)
	detail, err = repository.Detail(ctx, courseID, false)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	lessons := detail.Sections[0].Lessons
	if len(lessons) != 2 || lessons[0].ID != first || lessons[1].ID != second {
		t.Fatalf("previewable lessons = %+v, want %s then %s", lessons, first, second)
	}
	if lessons[0].Position != 1 || lessons[1].Position != 2 {
		t.Fatalf("previewable lesson positions = %d,%d", lessons[0].Position, lessons[1].Position)
	}
	if detail.Sections[0].LessonCount != 3 {
		t.Fatalf("lesson_count = %d, want 3", detail.Sections[0].LessonCount)
	}
	// The unflagged Lesson is still absent.
	for _, lesson := range lessons {
		if lesson.ID == notPreviewable {
			t.Fatal("the unflagged Lesson appeared in the public curriculum")
		}
	}
}

// TestPublicCurriculumRequiresReadyVideoForPreviewability keeps the badge honest.
// Offering a preview the authorization endpoint then refuses is worse than no
// badge, so every condition the resolver re-proves is applied to the projection
// too.
func TestPublicCurriculumRequiresReadyVideoForPreviewability(t *testing.T) {
	freshCatalogPublicSchema(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, catalogPublicTestDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	courseID := seedVisibleDetailCourse(t, pool, ctx)
	revisionID := liveRevisionOf(t, pool, ctx, courseID)
	repository := publicRepository(t, pool)

	// Flagged, but the video never reached READY.
	seedPreviewableLesson(t, pool, ctx, courseID, revisionID, 0, true, false)
	detail, err := repository.Detail(ctx, courseID, false)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if len(detail.Sections[0].Lessons) != 0 {
		t.Fatalf("a non-READY Lesson was offered for preview: %+v", detail.Sections[0].Lessons)
	}
	if detail.HasPreview {
		t.Fatal("has_preview is true for a non-READY Lesson video")
	}

	// A READY one, then retire its logical asset.
	ready := seedPreviewableLesson(t, pool, ctx, courseID, revisionID, 1, true, true)
	detail, err = repository.Detail(ctx, courseID, false)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if len(detail.Sections[0].Lessons) != 1 {
		t.Fatalf("previewable lessons = %+v, want the READY one", detail.Sections[0].Lessons)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE media_assets SET retired_at = now()
		WHERE id = (
			SELECT mav.logical_asset_id FROM media_asset_versions mav
			JOIN course_lessons cl ON cl.video_asset_version_id = mav.id
			WHERE cl.lesson_identity_id = $1::uuid
		)
	`, ready); err != nil {
		t.Fatalf("retiring the asset: %v", err)
	}
	detail, err = repository.Detail(ctx, courseID, false)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if len(detail.Sections[0].Lessons) != 0 {
		t.Fatalf("a retired asset stayed previewable: %+v", detail.Sections[0].Lessons)
	}
	if detail.HasPreview {
		t.Fatal("has_preview survived retiring the only previewable asset")
	}
}

// TestPublicCurriculumIgnoresCandidateRevisionPreviewIntent proves candidate
// intent is not publicly reachable. The visibility predicate ties the revision to
// courses.live_revision_id, so this cannot leak even by accident — and this is the
// test that would notice if that tie were ever loosened.
func TestPublicCurriculumIgnoresCandidateRevisionPreviewIntent(t *testing.T) {
	freshCatalogPublicSchema(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, catalogPublicTestDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	courseID := seedVisibleDetailCourse(t, pool, ctx)
	liveRevision := liveRevisionOf(t, pool, ctx, courseID)
	repository := publicRepository(t, pool)

	// A candidate revision with its own section, carrying a flagged READY Lesson.
	var candidateRevision, candidateSectionIdentity string
	if err := pool.QueryRow(ctx, `
		INSERT INTO course_revisions (course_id, based_on_revision_id, state, revision_number, title_ar, title_en)
		VALUES ($1::uuid, $2::uuid, 'DRAFT', 2, 'عنوان', 'Title') RETURNING id::text
	`, courseID, liveRevision).Scan(&candidateRevision); err != nil {
		t.Fatalf("seeding candidate revision: %v", err)
	}
	if err := pool.QueryRow(ctx,
		"INSERT INTO course_section_identities (course_id) VALUES ($1::uuid) RETURNING id::text",
		courseID).Scan(&candidateSectionIdentity); err != nil {
		t.Fatalf("seeding candidate section identity: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO course_sections (revision_id, course_id, section_identity_id, title_ar, title_en, position)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'قسم', 'Section', 0)
	`, candidateRevision, courseID, candidateSectionIdentity); err != nil {
		t.Fatalf("seeding candidate section: %v", err)
	}
	seedPreviewableLesson(t, pool, ctx, courseID, candidateRevision, 0, true, true)

	detail, err := repository.Detail(ctx, courseID, false)
	if err != nil || detail == nil {
		t.Fatalf("Detail = %#v, %v", detail, err)
	}
	for _, section := range detail.Sections {
		if len(section.Lessons) != 0 {
			t.Fatalf("candidate preview intent reached the public curriculum: %+v", section.Lessons)
		}
	}
	if detail.HasPreview {
		t.Fatal("has_preview is true from candidate revision intent alone")
	}
}

// TestHasPreviewIsDerivedFromBothSources is the transition contract on the card.
//
// has_preview stays a single boolean the existing catalogue UI already reads, and
// it is true from either source. The legacy arm is not removed when a Lesson
// preview appears: the pointer stays populated as rollback safety, and its
// retirement is a separate later tranche.
func TestHasPreviewIsDerivedFromBothSources(t *testing.T) {
	freshCatalogPublicSchema(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, catalogPublicTestDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	courseID := seedVisibleDetailCourse(t, pool, ctx)
	revisionID := liveRevisionOf(t, pool, ctx, courseID)
	repository := publicRepository(t, pool)

	assertProjectedHasPreview(t, repository, ctx, courseID, false, "neither source")

	// Legacy only.
	legacyPreviewID := seedPublicPreview(t, pool, ctx, courseID, revisionID)
	selectPublicPreview(t, pool, ctx, revisionID, legacyPreviewID)
	assertProjectedHasPreview(t, repository, ctx, courseID, true, "legacy course preview only")
	detail, err := repository.Detail(ctx, courseID, false)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if len(detail.Sections) > 0 && len(detail.Sections[0].Lessons) != 0 {
		t.Fatalf("a legacy preview produced Lesson entries: %+v", detail.Sections[0].Lessons)
	}

	// Both. The legacy pointer is untouched by the new Lesson preview.
	lessonID := seedPreviewableLesson(t, pool, ctx, courseID, revisionID, 0, true, true)
	assertProjectedHasPreview(t, repository, ctx, courseID, true, "both sources")
	detail, err = repository.Detail(ctx, courseID, false)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if len(detail.Sections[0].Lessons) != 1 || detail.Sections[0].Lessons[0].ID != lessonID {
		t.Fatalf("previewable lessons = %+v, want %s", detail.Sections[0].Lessons, lessonID)
	}
	var pointer *string
	if err := pool.QueryRow(ctx,
		"SELECT preview_asset_version_id::text FROM course_revisions WHERE id = $1::uuid",
		revisionID).Scan(&pointer); err != nil {
		t.Fatalf("reading the legacy pointer: %v", err)
	}
	if pointer == nil || *pointer != legacyPreviewID {
		t.Fatalf("legacy pointer = %v, want it still populated as %s", pointer, legacyPreviewID)
	}

	// Lesson preview only: withdraw the legacy pointer the way a later tranche
	// eventually will, and the card keeps reporting a preview.
	if _, err := pool.Exec(ctx,
		"UPDATE course_revisions SET preview_asset_version_id = NULL WHERE id = $1::uuid",
		revisionID); err != nil {
		t.Fatalf("clearing the legacy pointer: %v", err)
	}
	assertProjectedHasPreview(t, repository, ctx, courseID, true, "Lesson preview only")

	// And withdrawing the Lesson flag leaves neither.
	if _, err := pool.Exec(ctx,
		"UPDATE course_lessons SET allow_public_preview = false WHERE lesson_identity_id = $1::uuid",
		lessonID); err != nil {
		t.Fatalf("withdrawing the Lesson flag: %v", err)
	}
	assertProjectedHasPreview(t, repository, ctx, courseID, false, "neither source again")
}
