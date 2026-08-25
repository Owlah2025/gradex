//go:build integration

package learning

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// T062 evidence against real PostgreSQL and the production migrations.
//
// The property under test is that a report stays meaningful after the content moves: it stores the
// stable logical target *and* the exact instance the Student was shown, and the stored instance
// never follows a later revision or a replacement Asset Version.

const (
	reportInstructorID  = "44444444-4444-4444-4444-444444444444"
	reportLiveRevision  = "55555555-5555-5555-5555-555555555555"
	reportSectionRowID  = "77777777-7777-7777-7777-777777777777"
	reportOtherStudent  = "99999999-9999-9999-9999-999999999999"
	reportOtherCourseID = "aaaaaaaa-0000-0000-0000-00000000aaaa"
	reportOtherLessonID = "bbbbbbbb-0000-0000-0000-00000000bbbb"
)

func fixedReportClock() ReportClock {
	instant := time.Date(2026, 8, 4, 9, 30, 0, 0, time.UTC)
	return func() time.Time { return instant }
}

type storedReport struct {
	id            string
	reporter      string
	targetKind    string
	targetID      string
	revisionRef   *string
	reason        string
	explanation   *string
	resolvedAt    *time.Time
	createdAt     time.Time
	totalReported int
}

func readStoredReport(t *testing.T, ctx context.Context, fixture learningFixture, id string) storedReport {
	t.Helper()
	var stored storedReport
	err := fixture.repository.pool.QueryRow(ctx, `
		SELECT id::text, reporter_account_id::text, target_kind, target_id::text,
		       target_revision_ref::text, reason, explanation, resolved_at, created_at
		FROM content_reports WHERE id = $1::uuid
	`, id).Scan(&stored.id, &stored.reporter, &stored.targetKind, &stored.targetID,
		&stored.revisionRef, &stored.reason, &stored.explanation, &stored.resolvedAt, &stored.createdAt)
	if err != nil {
		t.Fatalf("reading stored report %s: %v", id, err)
	}
	return stored
}

func countReports(t *testing.T, ctx context.Context, fixture learningFixture) int {
	t.Helper()
	var total int
	if err := fixture.repository.pool.QueryRow(ctx, `SELECT count(*) FROM content_reports`).Scan(&total); err != nil {
		t.Fatalf("counting reports: %v", err)
	}
	return total
}

// seedReportMedia attaches a READY video and one Resource and Lab Material to the fixture Lesson,
// so every reportable target kind exists.
func seedReportMedia(t *testing.T, ctx context.Context, fixture learningFixture) (video, resource, lab string) {
	t.Helper()
	video = seedLessonMaterial(t, ctx, fixture, "VIDEO", "video-a")
	resource = seedLessonMaterial(t, ctx, fixture, "RESOURCE", "resource-a")
	lab = seedLessonMaterial(t, ctx, fixture, "LAB_MATERIAL", "lab-a")

	if _, err := fixture.repository.pool.Exec(ctx, `
		UPDATE course_lessons SET video_asset_version_id = $1::uuid WHERE lesson_identity_id = $2::uuid
	`, video, fixture.lessonID); err != nil {
		t.Fatalf("binding lesson video: %v", err)
	}
	return video, resource, lab
}

// seedLessonMaterial creates one logical Asset and one version of the given kind. Non-video kinds
// are also attached to the Lesson through `lesson_files`, which is how a material becomes visible.
func seedLessonMaterial(t *testing.T, ctx context.Context, fixture learningFixture, kind, objectKey string) string {
	t.Helper()
	var assetID string
	if err := fixture.repository.pool.QueryRow(ctx, `
		INSERT INTO media_assets (id, kind, owner_account_id, course_id, lesson_id, visibility)
		VALUES (gen_random_uuid(), $1::media_asset_kind, $2::uuid, $3::uuid, $4::uuid, 'PROTECTED')
		RETURNING id::text
	`, kind, reportInstructorID, fixture.courseID, fixture.lessonID).Scan(&assetID); err != nil {
		t.Fatalf("seeding %s asset: %v", kind, err)
	}

	var versionID string
	if err := fixture.repository.pool.QueryRow(ctx, `
		INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes)
		VALUES (gen_random_uuid(), $1::uuid, $2::media_asset_kind, 'READY', $3, 'v1', 'application/octet-stream', 1)
		RETURNING id::text
	`, assetID, kind, objectKey).Scan(&versionID); err != nil {
		t.Fatalf("seeding %s version: %v", kind, err)
	}

	if kind != "VIDEO" {
		var lessonRowID string
		if err := fixture.repository.pool.QueryRow(ctx,
			`SELECT id::text FROM course_lessons WHERE lesson_identity_id = $1::uuid`, fixture.lessonID).Scan(&lessonRowID); err != nil {
			t.Fatalf("resolving lesson row: %v", err)
		}
		if _, err := fixture.repository.pool.Exec(ctx, `
			INSERT INTO lesson_files (lesson_id, kind, asset_version_id, display_name_ar, display_name_en, position)
			VALUES ($1::uuid, $2::lesson_file_kind, $3::uuid, 'ملف', 'File', 0)
		`, lessonRowID, kind, versionID); err != nil {
			t.Fatalf("attaching %s to lesson: %v", kind, err)
		}
	}
	return versionID
}

// renderContext mints a report context exactly as the read that produced the page would, then
// verifies it — so every test below exercises the real mint → verify → create chain rather than
// hand-building a trusted binding.
func renderContext(t *testing.T, at time.Time, request ReportContextRequest) VerifiedReportBinding {
	t.Helper()
	signer := testSigner(t, at)
	token, err := signer.Mint(request)
	if err != nil {
		t.Fatalf("minting report context: %v", err)
	}
	binding, err := signer.Verify(token, request.ReporterAccountID, request.SessionID)
	if err != nil {
		t.Fatalf("verifying report context: %v", err)
	}
	return binding
}

func renderTime() time.Time { return time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC) }

func contextFor(t *testing.T, fixture learningFixture, kind ReportTargetKind, target, revision, version string) VerifiedReportBinding {
	t.Helper()
	return renderContext(t, renderTime(), ReportContextRequest{
		ReporterAccountID:       fixture.studentID,
		SessionID:               testSession,
		CourseID:                fixture.courseID,
		TargetKind:              kind,
		StableTargetID:          target,
		VisibleCourseRevisionID: revision,
		VisibleAssetVersionID:   version,
	})
}

// publishRevisionB makes a second approved revision live, carrying the same stable Lesson identity.
func publishRevisionB(t *testing.T, ctx context.Context, fixture learningFixture) (string, string) {
	t.Helper()
	const revisionB = "5b5b5b5b-0000-0000-0000-00000000b0b0"
	const sectionRowB = "7b7b7b7b-0000-0000-0000-00000000b0b0"
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, 'APPROVED', 2, 'دورة', 'Course v2')`, []any{revisionB, fixture.courseID}},
		{`INSERT INTO course_sections (id, revision_id, course_id, section_identity_id, title_ar, title_en, position) SELECT $1::uuid, $2::uuid, course_id, section_identity_id, title_ar, title_en, position FROM course_sections WHERE id = $3::uuid`, []any{sectionRowB, revisionB, reportSectionRowID}},
		{`INSERT INTO course_lessons (section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position, video_asset_version_id) SELECT $1::uuid, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position, video_asset_version_id FROM course_lessons WHERE lesson_identity_id = $2::uuid AND section_id = $3::uuid`, []any{sectionRowB, fixture.lessonID, reportSectionRowID}},
		{`UPDATE courses SET live_revision_id = $1::uuid WHERE id = $2::uuid`, []any{revisionB, fixture.courseID}},
	} {
		if _, err := fixture.repository.pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("publishing revision B: %v\n%s", err, statement.query)
		}
	}
	var lessonRowB string
	if err := fixture.repository.pool.QueryRow(ctx,
		`SELECT id::text FROM course_lessons WHERE section_id = $1::uuid AND lesson_identity_id = $2::uuid`,
		sectionRowB, fixture.lessonID).Scan(&lessonRowB); err != nil {
		t.Fatalf("resolving revision B lesson row: %v", err)
	}
	return revisionB, lessonRowB
}

// seedMaterialForLessonRow attaches a material to one specific per-revision lesson row, which is
// how authoring actually replaces content: revision B gets new rows and revision A keeps its own.
func seedMaterialForLessonRow(t *testing.T, ctx context.Context, fixture learningFixture, kind, objectKey, lessonRowID string) string {
	t.Helper()
	var assetID string
	if err := fixture.repository.pool.QueryRow(ctx, `
		INSERT INTO media_assets (id, kind, owner_account_id, course_id, lesson_id, visibility)
		VALUES (gen_random_uuid(), $1::media_asset_kind, $2::uuid, $3::uuid, $4::uuid, 'PROTECTED')
		RETURNING id::text
	`, kind, reportInstructorID, fixture.courseID, fixture.lessonID).Scan(&assetID); err != nil {
		t.Fatalf("seeding %s asset: %v", kind, err)
	}
	var versionID string
	if err := fixture.repository.pool.QueryRow(ctx, `
		INSERT INTO media_asset_versions (id, logical_asset_id, kind, state, storage_object_key, storage_object_version, content_type, size_bytes)
		VALUES (gen_random_uuid(), $1::uuid, $2::media_asset_kind, 'READY', $3, 'v1', 'application/octet-stream', 1)
		RETURNING id::text
	`, assetID, kind, objectKey).Scan(&versionID); err != nil {
		t.Fatalf("seeding %s version: %v", kind, err)
	}
	if kind != "VIDEO" {
		if _, err := fixture.repository.pool.Exec(ctx, `
			INSERT INTO lesson_files (lesson_id, kind, asset_version_id, display_name_ar, display_name_en, position)
			VALUES ($1::uuid, $2::lesson_file_kind, $3::uuid, 'ملف', 'File', 0)
		`, lessonRowID, kind, versionID); err != nil {
			t.Fatalf("attaching %s: %v", kind, err)
		}
	}
	return versionID
}

// TestReportStoresTheInstanceRenderedNotTheInstanceCurrentAtSubmission is the remediation's core
// evidence (D-065): a page rendered from A must report A after B becomes current.
func TestReportStoresTheInstanceRenderedNotTheInstanceCurrentAtSubmission(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	videoA, resourceA, labA := seedReportMedia(t, ctx, fixture)
	clock := fixedReportClock()

	// 1. The page renders revision A with version A, and mints its contexts.
	bindings := map[ReportTargetKind]VerifiedReportBinding{
		ReportTargetCourse:      contextFor(t, fixture, ReportTargetCourse, fixture.courseID, reportLiveRevision, ""),
		ReportTargetLesson:      contextFor(t, fixture, ReportTargetLesson, fixture.lessonID, reportLiveRevision, ""),
		ReportTargetVideo:       contextFor(t, fixture, ReportTargetVideo, fixture.lessonID, reportLiveRevision, videoA),
		ReportTargetResource:    contextFor(t, fixture, ReportTargetResource, fixture.lessonID, reportLiveRevision, resourceA),
		ReportTargetLabMaterial: contextFor(t, fixture, ReportTargetLabMaterial, fixture.lessonID, reportLiveRevision, labA),
	}

	// 2. B becomes current while the page stays open: a new revision, and replacement media.
	revisionB, lessonRowB := publishRevisionB(t, ctx, fixture)
	videoB := seedMaterialForLessonRow(t, ctx, fixture, "VIDEO", "video-b", lessonRowB)
	if _, err := fixture.repository.pool.Exec(ctx,
		`UPDATE course_lessons SET video_asset_version_id = $1::uuid WHERE id = $2::uuid`,
		videoB, lessonRowB); err != nil {
		t.Fatalf("replacing the video in revision B: %v", err)
	}
	resourceB := seedMaterialForLessonRow(t, ctx, fixture, "RESOURCE", "resource-b", lessonRowB)
	labB := seedMaterialForLessonRow(t, ctx, fixture, "LAB_MATERIAL", "lab-b", lessonRowB)

	// 3. The Student submits from the still-open A page. Each report must store A, not B.
	wantA := map[ReportTargetKind]string{
		ReportTargetCourse:      reportLiveRevision,
		ReportTargetLesson:      reportLiveRevision,
		ReportTargetVideo:       videoA,
		ReportTargetResource:    resourceA,
		ReportTargetLabMaterial: labA,
	}
	notB := map[ReportTargetKind]string{
		ReportTargetCourse:      revisionB,
		ReportTargetLesson:      revisionB,
		ReportTargetVideo:       videoB,
		ReportTargetResource:    resourceB,
		ReportTargetLabMaterial: labB,
	}

	for _, kind := range []ReportTargetKind{
		ReportTargetCourse, ReportTargetLesson, ReportTargetVideo, ReportTargetResource, ReportTargetLabMaterial,
	} {
		t.Run(string(kind), func(t *testing.T) {
			report, err := fixture.repository.CreateReport(ctx, bindings[kind],
				ReportContent{Reason: ReasonInaccurate}, clock)
			if err != nil {
				t.Fatalf("reporting %s from the stale page: %v", kind, err)
			}
			stored := readStoredReport(t, ctx, fixture, report.ID)
			if stored.revisionRef == nil {
				t.Fatalf("%s stored no instance", kind)
			}
			if *stored.revisionRef == notB[kind] {
				t.Fatalf("%s recorded the instance current at submission (%s); it must record what the Student saw (%s)",
					kind, notB[kind], wantA[kind])
			}
			if *stored.revisionRef != wantA[kind] {
				t.Fatalf("%s stored %s, want the rendered instance %s", kind, *stored.revisionRef, wantA[kind])
			}
			if stored.targetID != bindings[kind].StableTargetID {
				t.Fatalf("%s stable target %s, want %s", kind, stored.targetID, bindings[kind].StableTargetID)
			}
		})
	}
}

// TestAContextMintedAfterReplacementRecordsTheNewInstance is the other half: a page rendered after
// B is live reports B, and the earlier A report never moves.
func TestAContextMintedAfterReplacementRecordsTheNewInstance(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	videoA, _, _ := seedReportMedia(t, ctx, fixture)
	clock := fixedReportClock()

	firstReport, err := fixture.repository.CreateReport(ctx,
		contextFor(t, fixture, ReportTargetVideo, fixture.lessonID, reportLiveRevision, videoA),
		ReportContent{Reason: ReasonBrokenUnavailable}, clock)
	if err != nil {
		t.Fatalf("reporting video A: %v", err)
	}

	revisionB, lessonRowB := publishRevisionB(t, ctx, fixture)
	videoB := seedMaterialForLessonRow(t, ctx, fixture, "VIDEO", "video-b", lessonRowB)
	if _, err := fixture.repository.pool.Exec(ctx,
		`UPDATE course_lessons SET video_asset_version_id = $1::uuid WHERE id = $2::uuid`,
		videoB, lessonRowB); err != nil {
		t.Fatalf("binding video B in revision B: %v", err)
	}

	// A different Student renders the page now and reports what *they* see (D-066 keeps the first
	// Student's own second report refused while theirs is open — proved separately).
	seedSecondStudent(t, ctx, fixture, true)
	newBinding := renderContext(t, renderTime(), ReportContextRequest{
		ReporterAccountID:       reportOtherStudent,
		SessionID:               testSession,
		CourseID:                fixture.courseID,
		TargetKind:              ReportTargetVideo,
		StableTargetID:          fixture.lessonID,
		VisibleCourseRevisionID: revisionB,
		VisibleAssetVersionID:   videoB,
	})
	secondReport, err := fixture.repository.CreateReport(ctx, newBinding,
		ReportContent{Reason: ReasonBrokenUnavailable}, clock)
	if err != nil {
		t.Fatalf("reporting video B: %v", err)
	}

	if ref := readStoredReport(t, ctx, fixture, secondReport.ID).revisionRef; ref == nil || *ref != videoB {
		t.Fatalf("the new report stored %v, want B %s", ref, videoB)
	}
	// The old context never silently maps to B, and the stored report never rewrites.
	if ref := readStoredReport(t, ctx, fixture, firstReport.ID).revisionRef; ref == nil || *ref != videoA {
		t.Fatalf("the original report changed to %v; it must stay bound to A %s", ref, videoA)
	}
}

// TestReportBindingMustBeRelationallyCoherent proves a signature alone is not enough: the values
// inside a binding must form a real target, so a mis-issued or cross-wired context is still refused.
func TestReportBindingMustBeRelationallyCoherent(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	videoA, resourceA, labA := seedReportMedia(t, ctx, fixture)
	clock := fixedReportClock()
	seedSecondStudent(t, ctx, fixture, false)

	const foreignRevision = "cccccccc-0000-0000-0000-0000000000cc"
	const foreignCourse = "aaaaaaaa-0000-0000-0000-00000000aaaa"
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1::uuid, $2::uuid, 'DRAFT')`, []any{foreignCourse, reportInstructorID}},
		{`INSERT INTO course_revisions (id, course_id, state, revision_number, title_ar, title_en) VALUES ($1::uuid, $2::uuid, 'APPROVED', 1, 'أخرى', 'Other')`, []any{foreignRevision, foreignCourse}},
		{`UPDATE courses SET live_revision_id = $1::uuid, lifecycle = 'PUBLISHED' WHERE id = $2::uuid`, []any{foreignRevision, foreignCourse}},
	} {
		if _, err := fixture.repository.pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seeding foreign course: %v", err)
		}
	}

	before := countReports(t, ctx, fixture)

	cases := []struct {
		name    string
		binding VerifiedReportBinding
	}{
		{"revision from another Course", contextFor(t, fixture, ReportTargetLesson, fixture.lessonID, foreignRevision, "")},
		{"Resource version submitted as Lab Material", contextFor(t, fixture, ReportTargetLabMaterial, fixture.lessonID, reportLiveRevision, resourceA)},
		{"Lab Material version submitted as Resource", contextFor(t, fixture, ReportTargetResource, fixture.lessonID, reportLiveRevision, labA)},
		{"video version submitted as Resource", contextFor(t, fixture, ReportTargetResource, fixture.lessonID, reportLiveRevision, videoA)},
		{"Resource version submitted as the video", contextFor(t, fixture, ReportTargetVideo, fixture.lessonID, reportLiveRevision, resourceA)},
		{"Lesson identity reported as a Course", contextFor(t, fixture, ReportTargetCourse, fixture.lessonID, reportLiveRevision, "")},
		{"Course identity reported as a Lesson", contextFor(t, fixture, ReportTargetLesson, fixture.courseID, reportLiveRevision, "")},
		{
			"a Student with no Enrollment",
			renderContext(t, renderTime(), ReportContextRequest{
				ReporterAccountID: reportOtherStudent, SessionID: testSession, CourseID: fixture.courseID,
				TargetKind: ReportTargetLesson, StableTargetID: fixture.lessonID, VisibleCourseRevisionID: reportLiveRevision,
			}),
		},
		{
			"an Asset Version belonging to no lesson file",
			contextFor(t, fixture, ReportTargetResource, fixture.lessonID, reportLiveRevision, "0f0f0f0f-0000-0000-0000-00000000000f"),
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := fixture.repository.CreateReport(ctx, testCase.binding,
				ReportContent{Reason: ReasonInaccurate}, clock); !errors.Is(err, ErrReportTargetUnavailable) {
				t.Fatalf("expected ErrReportTargetUnavailable, got %v", err)
			}
		})
	}

	if after := countReports(t, ctx, fixture); after != before {
		t.Fatalf("a refused binding created %d rows", after-before)
	}
}

// TestDuplicateOpenReportFollowsVersionIndependentPolicy proves D-066 exactly.
func TestDuplicateOpenReportFollowsVersionIndependentPolicy(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	videoA, _, _ := seedReportMedia(t, ctx, fixture)
	clock := fixedReportClock()

	// 1. Student A reports version A.
	first, err := fixture.repository.CreateReport(ctx,
		contextFor(t, fixture, ReportTargetVideo, fixture.lessonID, reportLiveRevision, videoA),
		ReportContent{Reason: ReasonBrokenUnavailable}, clock)
	if err != nil {
		t.Fatalf("first report: %v", err)
	}

	// 2. Version B becomes current.
	revisionB, lessonRowB := publishRevisionB(t, ctx, fixture)
	videoB := seedMaterialForLessonRow(t, ctx, fixture, "VIDEO", "video-b", lessonRowB)
	if _, err := fixture.repository.pool.Exec(ctx,
		`UPDATE course_lessons SET video_asset_version_id = $1::uuid WHERE id = $2::uuid`,
		videoB, lessonRowB); err != nil {
		t.Fatalf("binding video B: %v", err)
	}
	bindingB := contextFor(t, fixture, ReportTargetVideo, fixture.lessonID, revisionB, videoB)

	// 3. The same Student cannot open a second report for the same stable target, even for B.
	if _, err := fixture.repository.CreateReport(ctx, bindingB,
		ReportContent{Reason: ReasonBrokenUnavailable}, clock); !errors.Is(err, ErrReportDuplicate) {
		t.Fatalf("D-066: a second open report for the same target must be refused, got %v", err)
	}

	// 5. Another Student may report B while A's report is open.
	seedSecondStudent(t, ctx, fixture, true)
	otherBinding := renderContext(t, renderTime(), ReportContextRequest{
		ReporterAccountID: reportOtherStudent, SessionID: testSession, CourseID: fixture.courseID,
		TargetKind: ReportTargetVideo, StableTargetID: fixture.lessonID,
		VisibleCourseRevisionID: revisionB, VisibleAssetVersionID: videoB,
	})
	if _, err := fixture.repository.CreateReport(ctx, otherBinding,
		ReportContent{Reason: ReasonBrokenUnavailable}, clock); err != nil {
		t.Fatalf("another Student must be able to report B: %v", err)
	}

	// 6. A different target kind on the same stable Lesson is not a duplicate.
	if _, err := fixture.repository.CreateReport(ctx,
		contextFor(t, fixture, ReportTargetLesson, fixture.lessonID, revisionB, ""),
		ReportContent{Reason: ReasonInaccurate}, clock); err != nil {
		t.Fatalf("a different target kind must not be a duplicate: %v", err)
	}

	// 4. Once the first report is resolved — S8's behaviour, simulated here as fixture setup only —
	// the partial index no longer covers it and the Student may report B.
	if _, err := fixture.repository.pool.Exec(ctx, `
		UPDATE content_reports
		SET resolved_at = now(), resolved_by_account_id = $2::uuid,
		    resolution_action = 'DISMISSED', resolution_reason = 'fixture setup'
		WHERE id = $1::uuid
	`, first.ID, fixture.studentID); err != nil {
		t.Fatalf("resolving the first report: %v", err)
	}
	second, err := fixture.repository.CreateReport(ctx, bindingB,
		ReportContent{Reason: ReasonBrokenUnavailable}, clock)
	if err != nil {
		t.Fatalf("after resolution the Student may report B: %v", err)
	}
	if ref := readStoredReport(t, ctx, fixture, second.ID).revisionRef; ref == nil || *ref != videoB {
		t.Fatalf("the post-resolution report stored %v, want B %s", ref, videoB)
	}
	if ref := readStoredReport(t, ctx, fixture, first.ID).revisionRef; ref == nil || *ref != videoA {
		t.Fatalf("the resolved report changed to %v", ref)
	}
}

// TestReportingChangesNothingAndGrantsNothing covers FR-031/BR-146 and proves the context is
// evidence, not capability.
func TestReportingChangesNothingAndGrantsNothing(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	video, _, _ := seedReportMedia(t, ctx, fixture)
	clock := fixedReportClock()

	type snapshot struct {
		lifecycle, liveRevision, revisionState, lessonTitle string
		position                                            int
		videoVersion, videoState, accountStatus             string
		enrollments, entitlements, progressRows             int
	}
	read := func() snapshot {
		var s snapshot
		if err := fixture.repository.pool.QueryRow(ctx, `
			SELECT c.lifecycle::text, c.live_revision_id::text, cr.state::text, cl.title_en, cl.position,
			       mav.id::text, mav.state::text, a.status::text,
			       (SELECT count(*) FROM enrollments), (SELECT count(*) FROM entitlements), (SELECT count(*) FROM progress)
			FROM courses c
			JOIN course_revisions cr ON cr.id = c.live_revision_id
			JOIN course_lessons cl ON cl.lesson_identity_id = $2::uuid
			JOIN media_asset_versions mav ON mav.id = cl.video_asset_version_id
			JOIN accounts a ON a.id = $3::uuid
			WHERE c.id = $1::uuid
		`, fixture.courseID, fixture.lessonID, fixture.studentID).Scan(&s.lifecycle, &s.liveRevision, &s.revisionState,
			&s.lessonTitle, &s.position, &s.videoVersion, &s.videoState, &s.accountStatus,
			&s.enrollments, &s.entitlements, &s.progressRows); err != nil {
			t.Fatalf("reading snapshot: %v", err)
		}
		return s
	}

	before := read()
	for _, binding := range []VerifiedReportBinding{
		contextFor(t, fixture, ReportTargetCourse, fixture.courseID, reportLiveRevision, ""),
		contextFor(t, fixture, ReportTargetLesson, fixture.lessonID, reportLiveRevision, ""),
		contextFor(t, fixture, ReportTargetVideo, fixture.lessonID, reportLiveRevision, video),
	} {
		if _, err := fixture.repository.CreateReport(ctx, binding, ReportContent{Reason: ReasonInappropriate}, clock); err != nil {
			t.Fatalf("reporting: %v", err)
		}
	}

	if after := read(); before != after {
		t.Fatalf("reporting changed state:\nbefore %+v\nafter  %+v", before, after)
	}

	// The context grants nothing: a Student holding one still resolves no Enrollment they lack, and
	// the report created no Entitlement, Enrollment, or Progress row.
	var granted int
	if err := fixture.repository.pool.QueryRow(ctx,
		`SELECT (SELECT count(*) FROM entitlements) + (SELECT count(*) FROM progress)`).Scan(&granted); err != nil {
		t.Fatalf("counting granted authority: %v", err)
	}
	if granted != 0 {
		t.Fatalf("reporting created %d authority rows", granted)
	}
}

// TestCreateReportIssuesOneBoundedBindingQuery keeps resolution direct: one verification plus one
// insert, with no per-Section or per-Lesson scan and no live-pointer re-resolution.
func TestCreateReportIssuesOneBoundedBindingQuery(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	video, _, _ := seedReportMedia(t, ctx, fixture)

	var observed []string
	instrumented, err := NewRepositoryWithQueryObserver(fixture.repository.pool, func(name string) {
		observed = append(observed, name)
	})
	if err != nil {
		t.Fatalf("constructing instrumented repository: %v", err)
	}
	if _, err := instrumented.CreateReport(ctx,
		contextFor(t, fixture, ReportTargetVideo, fixture.lessonID, reportLiveRevision, video),
		ReportContent{Reason: ReasonBrokenUnavailable}, fixedReportClock()); err != nil {
		t.Fatalf("creating report: %v", err)
	}

	var bindingQueries, insertQueries int
	for _, name := range observed {
		switch name {
		case "learning.report.binding":
			bindingQueries++
		case "learning.report.insert":
			insertQueries++
		default:
			t.Fatalf("unexpected query %q during report creation", name)
		}
	}
	if bindingQueries != 1 || insertQueries != 1 {
		t.Fatalf("expected one binding query and one insert, got %d and %d (%v)", bindingQueries, insertQueries, observed)
	}
}

// TestRefusedReportRollsBackCleanly proves the transaction closes and nothing partial survives.
func TestRefusedReportRollsBackCleanly(t *testing.T) {
	fixture := newLearningFixture(t)
	ctx := context.Background()
	clock := fixedReportClock()
	binding := contextFor(t, fixture, ReportTargetLesson, fixture.lessonID, reportLiveRevision, "")

	if _, err := fixture.repository.CreateReport(ctx, binding, ReportContent{Reason: ReasonInaccurate}, clock); err != nil {
		t.Fatalf("first report: %v", err)
	}
	if _, err := fixture.repository.CreateReport(ctx, binding, ReportContent{Reason: ReasonInaccurate}, clock); !errors.Is(err, ErrReportDuplicate) {
		t.Fatalf("expected a duplicate refusal, got %v", err)
	}
	if total := countReports(t, ctx, fixture); total != 1 {
		t.Fatalf("rolled-back report left %d rows, want 1", total)
	}
	if err := fixture.repository.pool.QueryRow(ctx, `SELECT 1`).Scan(new(int)); err != nil {
		t.Fatalf("pool unusable after rollback: %v", err)
	}
	_ = pgx.ErrNoRows
}

func seedSecondStudent(t *testing.T, ctx context.Context, fixture learningFixture, enrol bool) {
	t.Helper()
	if _, err := fixture.repository.pool.Exec(ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
		VALUES ($1::uuid, 'other-student@example.test', 'other-student@example.test', 'STUDENT', 'ACTIVE', 'Other student')
		ON CONFLICT (id) DO NOTHING
	`, reportOtherStudent); err != nil {
		t.Fatalf("seeding the other student: %v", err)
	}
	if enrol {
		if _, err := fixture.repository.pool.Exec(ctx, `
			INSERT INTO enrollments (student_account_id, course_id) VALUES ($1::uuid, $2::uuid)
			ON CONFLICT DO NOTHING
		`, reportOtherStudent, fixture.courseID); err != nil {
			t.Fatalf("enrolling the other student: %v", err)
		}
	}
}
