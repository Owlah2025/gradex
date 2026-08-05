//go:build integration

package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type d5Fixture struct {
	p         *pgxpool.Pool
	repo      *Repository
	validator AssetVersionValidator
	ctx       context.Context

	ownerID  string
	adminID  string
	courseID string
	liveID   string

	sectionIdentityID string
	lessonIdentityID  string
	legacyLessonID    string

	majorOld   string
	majorNew   string
	subjectOld string
	subjectNew string

	videoOld    string
	videoNew    string
	previewOld  string
	previewNew  string
	resourceOld string
	resourceNew string
	labOld      string
	labNew      string
}

func newD5Fixture(t *testing.T) *d5Fixture {
	t.Helper()
	freshSchema(t)
	p, _ := pool(t)
	ctx := context.Background()

	ownerID, courseID := seedInstructorAndCourse(t, p, ctx)
	repo, err := NewRepository(p, testOutboxWriter(t))
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}

	f := &d5Fixture{
		p:           p,
		repo:        repo,
		validator:   NewDBAssetVersionValidator(p),
		ctx:         ctx,
		ownerID:     ownerID,
		adminID:     "99999999-9999-9999-9999-999999999999",
		courseID:    courseID,
		majorOld:    "10000000-0000-0000-0000-000000000001",
		majorNew:    "10000000-0000-0000-0000-000000000002",
		subjectOld:  "10000000-0000-0000-0000-000000000003",
		subjectNew:  "10000000-0000-0000-0000-000000000004",
		videoOld:    "20000000-0000-0000-0000-000000000001",
		videoNew:    "20000000-0000-0000-0000-000000000002",
		previewOld:  "20000000-0000-0000-0000-000000000003",
		previewNew:  "20000000-0000-0000-0000-000000000004",
		resourceOld: "20000000-0000-0000-0000-000000000005",
		resourceNew: "20000000-0000-0000-0000-000000000006",
		labOld:      "20000000-0000-0000-0000-000000000007",
		labNew:      "20000000-0000-0000-0000-000000000008",
	}

	f.seedDependencies(t)
	f.publishInitialRevision(t)
	return f
}

func (f *d5Fixture) seedDependencies(t *testing.T) {
	t.Helper()
	if _, err := f.p.Exec(f.ctx, `
		INSERT INTO accounts (id, normalized_email, email, role, status, display_name)
		VALUES ($1, 'admin@example.com', 'admin@example.com', 'ADMIN', 'ACTIVE', 'Admin')
	`, f.adminID); err != nil {
		t.Fatalf("seeding admin: %v", err)
	}
	if _, err := f.p.Exec(f.ctx, `
		INSERT INTO taxonomy_terms (id, kind, label_ar, label_en, academic_code) VALUES
		($1, 'MAJOR', 'تخصص قديم', 'Old major', NULL),
		($2, 'MAJOR', 'تخصص جديد', 'New major', NULL),
		($3, 'SUBJECT', 'مادة قديمة', 'Old subject', 'OLD-1'),
		($4, 'SUBJECT', 'مادة جديدة', 'New subject', 'NEW-1')
	`, f.majorOld, f.majorNew, f.subjectOld, f.subjectNew); err != nil {
		t.Fatalf("seeding taxonomy: %v", err)
	}

	dummyCourseID := "60000000-0000-0000-0000-000000000001"
	dummySectionID := "70000000-0000-0000-0000-000000000001"
	if _, err := f.p.Exec(f.ctx,
		`INSERT INTO courses (id, owner_account_id, lifecycle) VALUES ($1, $2, 'DRAFT')`,
		dummyCourseID, f.ownerID,
	); err != nil {
		t.Fatalf("seeding legacy asset course: %v", err)
	}
	if _, err := f.p.Exec(f.ctx,
		`INSERT INTO sections (id, course_id, title, "order") VALUES ($1, $2, 'Asset owners', 0)`,
		dummySectionID, dummyCourseID,
	); err != nil {
		t.Fatalf("seeding legacy asset section: %v", err)
	}

	assets := []string{
		f.videoOld, f.videoNew, f.previewOld, f.previewNew,
		f.resourceOld, f.resourceNew, f.labOld, f.labNew,
	}
	for i, assetID := range assets {
		lessonID := fmt.Sprintf("80000000-0000-0000-0000-%012d", i+1)
		if i == 0 {
			f.legacyLessonID = lessonID
		}
		if _, err := f.p.Exec(f.ctx, `
			INSERT INTO lessons (id, section_id, title, "order")
			VALUES ($1, $2, $3, $4)
		`, lessonID, dummySectionID, fmt.Sprintf("Asset %d", i+1), i); err != nil {
			t.Fatalf("seeding legacy lesson for asset %s: %v", assetID, err)
		}
		if _, err := f.p.Exec(f.ctx, `
			INSERT INTO videos (id, lesson_id, status)
			VALUES ($1, $2, 'READY')
		`, assetID, lessonID); err != nil {
			t.Fatalf("seeding asset %s: %v", assetID, err)
		}
	}
}

func (f *d5Fixture) publishInitialRevision(t *testing.T) {
	t.Helper()
	var revisionID string
	if err := f.p.QueryRow(f.ctx,
		`SELECT id FROM course_revisions WHERE course_id = $1::uuid`,
		f.courseID,
	).Scan(&revisionID); err != nil {
		t.Fatalf("querying initial revision: %v", err)
	}

	year := StudyYearYear1
	if _, err := f.repo.UpdateCourseRevision(f.ctx, f.validator, UpdateRevisionRequest{
		CourseID:              f.courseID,
		RevisionID:            revisionID,
		OwnerAccountID:        f.ownerID,
		TitleAr:               "النسخة القديمة",
		TitleEn:               "OLD",
		DescriptionAr:         "وصف قديم",
		DescriptionEn:         "OLD DESCRIPTION",
		MajorTermID:           &f.majorOld,
		SubjectTermID:         &f.subjectOld,
		StudyYear:             &year,
		PreviewAssetVersionID: &f.previewOld,
	}, f.ownerID); err != nil {
		t.Fatalf("UpdateCourseRevision: %v", err)
	}

	section, err := f.repo.AddSection(f.ctx, AddSectionRequest{
		CourseID: f.courseID, RevisionID: revisionID, OwnerAccountID: f.ownerID,
		TitleAr: "قسم قديم", TitleEn: "OLD SECTION",
	}, f.ownerID)
	if err != nil {
		t.Fatalf("AddSection: %v", err)
	}
	f.sectionIdentityID = section.SectionIdentityID

	lesson, err := f.repo.AddLesson(f.ctx, AddLessonRequest{
		CourseID: f.courseID, RevisionID: revisionID, SectionID: section.SectionIdentityID,
		OwnerAccountID: f.ownerID, TitleAr: "درس قديم", TitleEn: "OLD LESSON",
	}, f.ownerID)
	if err != nil {
		t.Fatalf("AddLesson: %v", err)
	}
	f.lessonIdentityID = lesson.LessonIdentityID

	if _, err := f.repo.SetLessonVideo(f.ctx, f.validator, SetVideoRequest{
		CourseID: f.courseID, RevisionID: revisionID, LessonID: lesson.LessonIdentityID,
		VideoAssetVersionID: f.videoOld, OwnerAccountID: f.ownerID,
	}, f.ownerID); err != nil {
		t.Fatalf("SetLessonVideo: %v", err)
	}
	for _, file := range []LessonFileRequest{
		{
			CourseID: f.courseID, RevisionID: revisionID, LessonID: lesson.LessonIdentityID,
			Kind: FileKindResource, AssetVersionID: f.resourceOld,
			DisplayNameAr: "مرجع قديم", DisplayNameEn: "OLD RESOURCE", OwnerAccountID: f.ownerID,
		},
		{
			CourseID: f.courseID, RevisionID: revisionID, LessonID: lesson.LessonIdentityID,
			Kind: FileKindLabMaterial, AssetVersionID: f.labOld,
			DisplayNameAr: "مختبر قديم", DisplayNameEn: "OLD LAB", OwnerAccountID: f.ownerID,
		},
	} {
		if _, err := f.repo.AddLessonFile(f.ctx, f.validator, file, f.ownerID); err != nil {
			t.Fatalf("AddLessonFile(%s): %v", file.Kind, err)
		}
	}

	if _, err := f.repo.SubmitCourse(f.ctx, f.validator, SubmitCourseRequest{
		CourseID: f.courseID, RevisionID: revisionID,
		OwnerAccountID: f.ownerID, ActorDescriptor: f.ownerID,
	}); err != nil {
		t.Fatalf("SubmitCourse initial revision: %v", err)
	}
	if _, err := f.repo.ApproveCourse(f.ctx, f.validator, ApproveCourseRequest{
		CourseID: f.courseID, RevisionID: revisionID,
		AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
	}); err != nil {
		t.Fatalf("ApproveCourse initial revision: %v", err)
	}
	f.liveID = revisionID
}

func (f *d5Fixture) candidate(t *testing.T) *CourseRevision {
	t.Helper()
	candidate, err := f.repo.CreateCandidate(f.ctx, f.courseID, f.ownerID, f.ownerID)
	if err != nil {
		t.Fatalf("CreateCandidate: %v", err)
	}
	return candidate
}

func (f *d5Fixture) submittedCandidate(t *testing.T) *CourseRevision {
	t.Helper()
	candidate := f.candidate(t)
	if _, err := f.repo.SubmitCourse(f.ctx, f.validator, SubmitCourseRequest{
		CourseID: f.courseID, RevisionID: candidate.ID,
		OwnerAccountID: f.ownerID, ActorDescriptor: f.ownerID,
	}); err != nil {
		t.Fatalf("SubmitCourse candidate: %v", err)
	}
	return candidate
}

func authoredFingerprint(revision *CourseRevision) string {
	if revision == nil {
		return "<nil>"
	}
	type fileView struct {
		Kind     LessonFileKind
		Asset    string
		NameAr   string
		NameEn   string
		Position int
	}
	type lessonView struct {
		Identity string
		TitleAr  string
		TitleEn  string
		Position int
		Video    *string
		Files    []fileView
	}
	type sectionView struct {
		Identity string
		TitleAr  string
		TitleEn  string
		Position int
		Price    *int64
		Lessons  []lessonView
	}
	view := struct {
		TitleAr, TitleEn, DescriptionAr, DescriptionEn string
		Major, Subject, Preview                        *string
		StudyYear                                      *StudyYear
		Sections                                       []sectionView
	}{
		TitleAr: revision.TitleAr, TitleEn: revision.TitleEn,
		DescriptionAr: revision.DescriptionAr, DescriptionEn: revision.DescriptionEn,
		Major: revision.MajorTermID, Subject: revision.SubjectTermID,
		Preview: revision.PreviewAssetVersionID, StudyYear: revision.StudyYear,
	}
	for _, section := range revision.Sections {
		sv := sectionView{
			Identity: section.SectionIdentityID, TitleAr: section.TitleAr, TitleEn: section.TitleEn,
			Position: section.Position, Price: section.PriceMinorUnits,
		}
		for _, lesson := range section.Lessons {
			lv := lessonView{
				Identity: lesson.LessonIdentityID, TitleAr: lesson.TitleAr, TitleEn: lesson.TitleEn,
				Position: lesson.Position, Video: lesson.VideoAssetVersionID,
			}
			for _, file := range lesson.Files {
				lv.Files = append(lv.Files, fileView{
					Kind: file.Kind, Asset: file.AssetVersionID,
					NameAr: file.DisplayNameAr, NameEn: file.DisplayNameEn, Position: file.Position,
				})
			}
			sv.Lessons = append(sv.Lessons, lv)
		}
		view.Sections = append(view.Sections, sv)
	}
	encoded, _ := json.Marshal(view)
	return string(encoded)
}

func TestD5CandidateCloneIsDeepAtomicAndIdentityStable(t *testing.T) {
	f := newD5Fixture(t)
	live, err := f.repo.GetLiveCourseGraph(f.ctx, f.courseID)
	if err != nil {
		t.Fatalf("GetLiveCourseGraph: %v", err)
	}

	var externalBefore, externalAfter [7]int
	countSQL := `
		SELECT
			(SELECT count(*) FROM videos),
			(SELECT count(*) FROM sections),
			(SELECT count(*) FROM lessons),
			(SELECT count(*) FROM progress),
			(SELECT count(*) FROM fake_entitlements),
			(SELECT count(*) FROM audit_events),
			(SELECT count(*) FROM outbox_events)
	`
	if err := f.p.QueryRow(f.ctx, countSQL).Scan(
		&externalBefore[0], &externalBefore[1], &externalBefore[2], &externalBefore[3],
		&externalBefore[4], &externalBefore[5], &externalBefore[6],
	); err != nil {
		t.Fatalf("counting external rows: %v", err)
	}

	candidate := f.candidate(t)
	if candidate.ID == f.liveID {
		t.Fatal("candidate reused the live revision row")
	}
	if candidate.BasedOnRevisionID == nil || *candidate.BasedOnRevisionID != f.liveID {
		t.Fatalf("based_on_revision_id = %v, want %s", candidate.BasedOnRevisionID, f.liveID)
	}
	if authoredFingerprint(candidate) != authoredFingerprint(live.LiveRevision) {
		t.Fatalf("candidate graph differs from captured live graph\nlive=%s\ncandidate=%s",
			authoredFingerprint(live.LiveRevision), authoredFingerprint(candidate))
	}
	if candidate.Sections[0].ID == live.LiveRevision.Sections[0].ID {
		t.Fatal("candidate reused a revision-owned Section row")
	}
	if candidate.Sections[0].Lessons[0].ID == live.LiveRevision.Sections[0].Lessons[0].ID {
		t.Fatal("candidate reused a revision-owned Lesson row")
	}
	if candidate.Sections[0].SectionIdentityID != live.LiveRevision.Sections[0].SectionIdentityID ||
		candidate.Sections[0].Lessons[0].LessonIdentityID != live.LiveRevision.Sections[0].Lessons[0].LessonIdentityID {
		t.Fatal("candidate regenerated stable Section or Lesson identity")
	}
	if candidate.Sections[0].Lessons[0].Files[0].ID == live.LiveRevision.Sections[0].Lessons[0].Files[0].ID {
		t.Fatal("candidate reused a revision-owned Lesson file row")
	}

	second := f.candidate(t)
	if second.ID != candidate.ID {
		t.Fatalf("idempotent candidate call returned %s, want %s", second.ID, candidate.ID)
	}
	var candidateAuditCount int
	if err := f.p.QueryRow(f.ctx, `
		SELECT count(*)
		FROM audit_events
		WHERE action = 'COURSE_CANDIDATE_CREATED'
		  AND target_id = $1::text
	`, candidate.ID).Scan(&candidateAuditCount); err != nil || candidateAuditCount != 1 {
		t.Fatalf("candidate creation audit count = %d (err=%v), want 1", candidateAuditCount, err)
	}

	if err := f.p.QueryRow(f.ctx, countSQL).Scan(
		&externalAfter[0], &externalAfter[1], &externalAfter[2], &externalAfter[3],
		&externalAfter[4], &externalAfter[5], &externalAfter[6],
	); err != nil {
		t.Fatalf("recounting external rows: %v", err)
	}
	externalBefore[5]++ // The newly-created candidate is itself an audited privileged mutation.
	if externalAfter != externalBefore {
		t.Fatalf("candidate clone changed externally owned rows or emitted unexpected evidence: before=%v after=%v",
			externalBefore, externalAfter)
	}

	updatedLesson, err := f.repo.SetLessonVideo(f.ctx, f.validator, SetVideoRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, LessonID: f.lessonIdentityID,
		VideoAssetVersionID: f.videoNew, OwnerAccountID: f.ownerID,
	}, f.ownerID)
	if err != nil {
		t.Fatalf("SetLessonVideo candidate: %v", err)
	}
	if updatedLesson.LessonIdentityID != f.lessonIdentityID {
		t.Fatal("video replacement changed the stable Lesson identity")
	}

	if err := f.repo.DeleteSection(f.ctx, DeleteSectionRequest{
		CourseID: f.courseID, RevisionID: candidate.ID,
		SectionID: f.sectionIdentityID, OwnerAccountID: f.ownerID,
	}, f.ownerID); err != nil {
		t.Fatalf("DeleteSection candidate: %v", err)
	}
	recreatedSection, err := f.repo.AddSection(f.ctx, AddSectionRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, OwnerAccountID: f.ownerID,
		TitleAr: "قسم بديل", TitleEn: "RECREATED SECTION",
	}, f.ownerID)
	if err != nil {
		t.Fatalf("recreating section: %v", err)
	}
	if recreatedSection.SectionIdentityID == f.sectionIdentityID {
		t.Fatal("delete/recreate reused the old stable Section identity")
	}
	recreatedLesson, err := f.repo.AddLesson(f.ctx, AddLessonRequest{
		CourseID: f.courseID, RevisionID: candidate.ID,
		SectionID: recreatedSection.SectionIdentityID, OwnerAccountID: f.ownerID,
		TitleAr: "درس بديل", TitleEn: "RECREATED LESSON",
	}, f.ownerID)
	if err != nil {
		t.Fatalf("recreating lesson: %v", err)
	}
	if recreatedLesson.LessonIdentityID == f.lessonIdentityID {
		t.Fatal("delete/recreate reused the old stable Lesson identity")
	}
}

func TestD5ActiveCandidateUniqueIndexRejectsDirectConcurrentInserts(t *testing.T) {
	f := newD5Fixture(t)
	start := make(chan struct{})
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		revisionNumber := 100 + i
		go func() {
			<-start
			_, err := f.p.Exec(f.ctx, `
				INSERT INTO course_revisions (
					course_id, based_on_revision_id, state, revision_number,
					title_ar, title_en, description_ar, description_en
				) VALUES ($1, $2, 'DRAFT', $3, 'مباشر', 'DIRECT', '', '')
			`, f.courseID, f.liveID, revisionNumber)
			errs <- err
		}()
	}
	close(start)

	successes, uniqueFailures := 0, 0
	for i := 0; i < 2; i++ {
		err := <-errs
		if err == nil {
			successes++
			continue
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
			pgErr.ConstraintName == "course_revisions_one_active_candidate" {
			uniqueFailures++
			continue
		}
		t.Fatalf("unexpected direct insert error: %v", err)
	}
	if successes != 1 || uniqueFailures != 1 {
		t.Fatalf("direct inserts: successes=%d unique failures=%d", successes, uniqueFailures)
	}
}

func TestD5MutationsRefuseLiveRowsAndCrossCandidateChildren(t *testing.T) {
	f := newD5Fixture(t)
	liveCourse, err := f.repo.GetLiveCourseGraph(f.ctx, f.courseID)
	if err != nil {
		t.Fatalf("GetLiveCourseGraph: %v", err)
	}
	liveFingerprint := authoredFingerprint(liveCourse.LiveRevision)
	liveSection := liveCourse.LiveRevision.Sections[0]
	liveLesson := liveSection.Lessons[0]
	liveFile := liveLesson.Files[0]
	candidate := f.candidate(t)

	_, err = f.repo.UpdateSection(f.ctx, UpdateSectionRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, SectionID: liveSection.ID,
		OwnerAccountID: f.ownerID, TitleEn: "INTERNAL ROW ATTACK",
	}, f.ownerID)
	if !errors.Is(err, ErrCourseNotFound) {
		t.Fatalf("live Section version-row ID error = %v, want ErrCourseNotFound", err)
	}
	_, err = f.repo.UpdateLesson(f.ctx, UpdateLessonRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, LessonID: liveLesson.ID,
		OwnerAccountID: f.ownerID, TitleEn: "INTERNAL ROW ATTACK",
	}, f.ownerID)
	if !errors.Is(err, ErrCourseNotFound) {
		t.Fatalf("live Lesson version-row ID error = %v, want ErrCourseNotFound", err)
	}
	err = f.repo.DeleteLessonFile(f.ctx, DeleteLessonFileRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, LessonID: f.lessonIdentityID,
		FileID: liveFile.ID, OwnerAccountID: f.ownerID,
	}, f.ownerID)
	if !errors.Is(err, ErrCourseNotFound) {
		t.Fatalf("live file row ID error = %v, want ErrCourseNotFound", err)
	}

	_, err = f.repo.UpdateCourseRevision(f.ctx, f.validator, UpdateRevisionRequest{
		CourseID: f.courseID, RevisionID: f.liveID, OwnerAccountID: f.ownerID,
		TitleEn: "LIVE MUTATION ATTACK",
	}, f.ownerID)
	requireConflict(t, err)
	afterLive, err := f.repo.GetLiveCourseGraph(f.ctx, f.courseID)
	if err != nil {
		t.Fatalf("GetLiveCourseGraph after refused writes: %v", err)
	}
	if authoredFingerprint(afterLive.LiveRevision) != liveFingerprint {
		t.Fatal("a refused candidate-scoped mutation changed the live graph")
	}

	otherCourse, err := f.repo.CreateCourse(f.ctx, CreateCourseRequest{
		OwnerAccountID: f.ownerID,
		TitleAr:        "دورة أخرى",
		TitleEn:        "OTHER COURSE",
	}, f.ownerID)
	if err != nil {
		t.Fatalf("CreateCourse other: %v", err)
	}
	otherRevision := otherCourse.EditableRevision
	otherSection, err := f.repo.AddSection(f.ctx, AddSectionRequest{
		CourseID: otherCourse.ID, RevisionID: otherRevision.ID, OwnerAccountID: f.ownerID,
		TitleAr: "قسم آخر", TitleEn: "OTHER SECTION",
	}, f.ownerID)
	if err != nil {
		t.Fatalf("AddSection other: %v", err)
	}
	otherLesson, err := f.repo.AddLesson(f.ctx, AddLessonRequest{
		CourseID: otherCourse.ID, RevisionID: otherRevision.ID,
		SectionID: otherSection.SectionIdentityID, OwnerAccountID: f.ownerID,
		TitleAr: "درس آخر", TitleEn: "OTHER LESSON",
	}, f.ownerID)
	if err != nil {
		t.Fatalf("AddLesson other: %v", err)
	}

	_, err = f.repo.AddLesson(f.ctx, AddLessonRequest{
		CourseID: f.courseID, RevisionID: candidate.ID,
		SectionID: otherSection.SectionIdentityID, OwnerAccountID: f.ownerID,
		TitleAr: "تهريب", TitleEn: "CROSS COURSE",
	}, f.ownerID)
	if !errors.Is(err, ErrCourseNotFound) {
		t.Fatalf("cross-Course Section error = %v, want ErrCourseNotFound", err)
	}
	_, err = f.repo.UpdateLesson(f.ctx, UpdateLessonRequest{
		CourseID: f.courseID, RevisionID: candidate.ID,
		LessonID: otherLesson.LessonIdentityID, OwnerAccountID: f.ownerID,
		TitleEn: "CROSS COURSE",
	}, f.ownerID)
	if !errors.Is(err, ErrCourseNotFound) {
		t.Fatalf("cross-Course Lesson error = %v, want ErrCourseNotFound", err)
	}
	_, err = f.repo.UpdateCourseRevision(f.ctx, f.validator, UpdateRevisionRequest{
		CourseID: f.courseID, RevisionID: otherRevision.ID,
		OwnerAccountID: f.ownerID, TitleEn: "CROSS COURSE",
	}, f.ownerID)
	if !errors.Is(err, ErrCourseNotFound) {
		t.Fatalf("cross-Course candidate error = %v, want ErrCourseNotFound", err)
	}
}

type approvalSnapshot struct {
	lifecycle   string
	livePointer string
	liveState   string
	candidate   string
	audits      int
	outbox      int
}

func (f *d5Fixture) approvalSnapshot(t *testing.T, candidateID string) approvalSnapshot {
	t.Helper()
	var snapshot approvalSnapshot
	if err := f.p.QueryRow(f.ctx, `
		SELECT c.lifecycle, c.live_revision_id::text, live.state, candidate.state
		FROM courses c
		JOIN course_revisions live ON live.id = c.live_revision_id
		JOIN course_revisions candidate ON candidate.id = $2::uuid
		WHERE c.id = $1::uuid
	`, f.courseID, candidateID).Scan(
		&snapshot.lifecycle, &snapshot.livePointer, &snapshot.liveState, &snapshot.candidate,
	); err != nil {
		t.Fatalf("reading approval state snapshot: %v", err)
	}
	if err := f.p.QueryRow(f.ctx, `
		SELECT count(*) FROM audit_events
		WHERE action = 'COURSE_PUBLISHED' AND metadata->>'revision_id' = $1
	`, candidateID).Scan(&snapshot.audits); err != nil {
		t.Fatalf("counting approval audit: %v", err)
	}
	if err := f.p.QueryRow(f.ctx, `
		SELECT count(*) FROM outbox_events
		WHERE event_type = 'catalog.course_published' AND aggregate_id = $1::uuid
	`, f.courseID).Scan(&snapshot.outbox); err != nil {
		t.Fatalf("counting publication outbox: %v", err)
	}
	return snapshot
}

func TestD5ApprovalRollbackIsAtomicAfterEveryLoadBearingStage(t *testing.T) {
	f := newD5Fixture(t)
	candidate := f.submittedCandidate(t)
	baseline := f.approvalSnapshot(t, candidate.ID)

	for _, stage := range []approvalFailureStage{
		approvalAfterSupersede,
		approvalAfterApprove,
		approvalAfterPointer,
		approvalAfterAudit,
		approvalAfterOutbox,
	} {
		t.Run(string(stage), func(t *testing.T) {
			_, err := f.repo.ApproveCourse(
				withApprovalFailure(f.ctx, stage), f.validator, ApproveCourseRequest{
					CourseID: f.courseID, RevisionID: candidate.ID,
					AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
				})
			if err == nil {
				t.Fatalf("approval unexpectedly succeeded at injected stage %s", stage)
			}
			after := f.approvalSnapshot(t, candidate.ID)
			if after != baseline {
				t.Fatalf("partial approval escaped rollback at %s: before=%+v after=%+v",
					stage, baseline, after)
			}
		})
	}
}

func assertSubmissionFailure(t *testing.T, err error) {
	t.Helper()
	var validation *SubmissionValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %v, want SubmissionValidationError (422 semantics)", err)
	}
	var conflict *LifecycleConflictError
	if errors.As(err, &conflict) {
		t.Fatalf("business validation was collapsed into conflict: %v", err)
	}
	if len(validation.Violations) == 0 {
		t.Fatal("submission validation returned no violations")
	}
}

func TestD5ApprovalRevalidatesEveryDependencyClass(t *testing.T) {
	tests := []struct {
		name       string
		invalidate func(*testing.T, *d5Fixture, *CourseRevision)
		restore    func(*testing.T, *d5Fixture)
	}{
		{
			name: "completeness",
			invalidate: func(t *testing.T, f *d5Fixture, candidate *CourseRevision) {
				if _, err := f.p.Exec(f.ctx,
					`DELETE FROM course_sections WHERE revision_id = $1::uuid`, candidate.ID,
				); err != nil {
					t.Fatalf("deleting candidate graph: %v", err)
				}
			},
			restore: func(*testing.T, *d5Fixture) {},
		},
		{
			name: "owner eligibility",
			invalidate: func(t *testing.T, f *d5Fixture, _ *CourseRevision) {
				if _, err := f.p.Exec(f.ctx,
					`UPDATE accounts SET status = 'SUSPENDED' WHERE id = $1::uuid`, f.ownerID,
				); err != nil {
					t.Fatalf("suspending owner: %v", err)
				}
			},
			restore: func(t *testing.T, f *d5Fixture) {
				if _, err := f.p.Exec(f.ctx,
					`UPDATE accounts SET status = 'ACTIVE' WHERE id = $1::uuid`, f.ownerID,
				); err != nil {
					t.Fatalf("restoring owner: %v", err)
				}
			},
		},
		{
			name: "taxonomy",
			invalidate: func(t *testing.T, f *d5Fixture, _ *CourseRevision) {
				if _, err := f.p.Exec(f.ctx,
					`UPDATE taxonomy_terms SET retired_at = now() WHERE id = $1::uuid`, f.subjectOld,
				); err != nil {
					t.Fatalf("retiring taxonomy term: %v", err)
				}
			},
			restore: func(t *testing.T, f *d5Fixture) {
				if _, err := f.p.Exec(f.ctx,
					`UPDATE taxonomy_terms SET retired_at = NULL WHERE id = $1::uuid`, f.subjectOld,
				); err != nil {
					t.Fatalf("restoring taxonomy term: %v", err)
				}
			},
		},
	}
	for _, asset := range []struct {
		name string
		id   func(*d5Fixture) string
	}{
		{name: "lesson video", id: func(f *d5Fixture) string { return f.videoOld }},
		{name: "preview asset", id: func(f *d5Fixture) string { return f.previewOld }},
		{name: "resource asset", id: func(f *d5Fixture) string { return f.resourceOld }},
		{name: "lab material asset", id: func(f *d5Fixture) string { return f.labOld }},
	} {
		asset := asset
		tests = append(tests, struct {
			name       string
			invalidate func(*testing.T, *d5Fixture, *CourseRevision)
			restore    func(*testing.T, *d5Fixture)
		}{
			name: asset.name,
			invalidate: func(t *testing.T, f *d5Fixture, _ *CourseRevision) {
				if _, err := f.p.Exec(f.ctx,
					`UPDATE videos SET status = 'FAILED' WHERE id = $1::uuid`, asset.id(f),
				); err != nil {
					t.Fatalf("invalidating %s: %v", asset.name, err)
				}
			},
			restore: func(t *testing.T, f *d5Fixture) {
				if _, err := f.p.Exec(f.ctx,
					`UPDATE videos SET status = 'READY' WHERE id = $1::uuid`, asset.id(f),
				); err != nil {
					t.Fatalf("restoring %s: %v", asset.name, err)
				}
			},
		})
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newD5Fixture(t)
			candidate := f.submittedCandidate(t)
			baseline := f.approvalSnapshot(t, candidate.ID)
			test.invalidate(t, f, candidate)

			_, err := f.repo.ApproveCourse(f.ctx, f.validator, ApproveCourseRequest{
				CourseID: f.courseID, RevisionID: candidate.ID,
				AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
			})
			assertSubmissionFailure(t, err)
			after := f.approvalSnapshot(t, candidate.ID)
			if after != baseline {
				t.Fatalf("failed approval changed durable state: before=%+v after=%+v", baseline, after)
			}
			test.restore(t, f)
		})
	}
}

func (f *d5Fixture) mutateCandidateToNewGraph(t *testing.T, candidate *CourseRevision) {
	t.Helper()
	year := StudyYearYear2
	if _, err := f.repo.UpdateCourseRevision(f.ctx, f.validator, UpdateRevisionRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, OwnerAccountID: f.ownerID,
		TitleAr: "النسخة الجديدة", TitleEn: "NEW",
		DescriptionAr: "وصف جديد", DescriptionEn: "NEW DESCRIPTION",
		MajorTermID: &f.majorNew, SubjectTermID: &f.subjectNew, StudyYear: &year,
		PreviewAssetVersionID: &f.previewNew,
	}, f.ownerID); err != nil {
		t.Fatalf("updating candidate metadata: %v", err)
	}
	if _, err := f.repo.UpdateSection(f.ctx, UpdateSectionRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, SectionID: f.sectionIdentityID,
		OwnerAccountID: f.ownerID, TitleAr: "قسم جديد", TitleEn: "NEW SECTION",
	}, f.ownerID); err != nil {
		t.Fatalf("updating candidate section: %v", err)
	}
	if _, err := f.repo.UpdateLesson(f.ctx, UpdateLessonRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, LessonID: f.lessonIdentityID,
		OwnerAccountID: f.ownerID, TitleAr: "درس جديد", TitleEn: "NEW LESSON",
	}, f.ownerID); err != nil {
		t.Fatalf("updating candidate lesson: %v", err)
	}
	if _, err := f.repo.SetLessonVideo(f.ctx, f.validator, SetVideoRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, LessonID: f.lessonIdentityID,
		VideoAssetVersionID: f.videoNew, OwnerAccountID: f.ownerID,
	}, f.ownerID); err != nil {
		t.Fatalf("updating candidate video: %v", err)
	}

	graph, err := f.repo.loadRevisionGraphByID(f.ctx, candidate.ID)
	if err != nil {
		t.Fatalf("loading candidate for file replacement: %v", err)
	}
	for _, file := range graph.Sections[0].Lessons[0].Files {
		if err := f.repo.DeleteLessonFile(f.ctx, DeleteLessonFileRequest{
			CourseID: f.courseID, RevisionID: candidate.ID, LessonID: f.lessonIdentityID,
			FileID: file.ID, OwnerAccountID: f.ownerID,
		}, f.ownerID); err != nil {
			t.Fatalf("deleting cloned candidate file: %v", err)
		}
	}
	for _, file := range []LessonFileRequest{
		{
			CourseID: f.courseID, RevisionID: candidate.ID, LessonID: f.lessonIdentityID,
			Kind: FileKindResource, AssetVersionID: f.resourceNew,
			DisplayNameAr: "مرجع جديد", DisplayNameEn: "NEW RESOURCE", OwnerAccountID: f.ownerID,
		},
		{
			CourseID: f.courseID, RevisionID: candidate.ID, LessonID: f.lessonIdentityID,
			Kind: FileKindLabMaterial, AssetVersionID: f.labNew,
			DisplayNameAr: "مختبر جديد", DisplayNameEn: "NEW LAB", OwnerAccountID: f.ownerID,
		},
	} {
		if _, err := f.repo.AddLessonFile(f.ctx, f.validator, file, f.ownerID); err != nil {
			t.Fatalf("adding replacement file %s: %v", file.Kind, err)
		}
	}
}

func TestD5LiveReadersObserveCompleteOldOrNewGraph(t *testing.T) {
	f := newD5Fixture(t)
	oldCourse, err := f.repo.GetLiveCourseGraph(f.ctx, f.courseID)
	if err != nil {
		t.Fatalf("loading old graph: %v", err)
	}
	oldFingerprint := authoredFingerprint(oldCourse.LiveRevision)

	candidate := f.candidate(t)
	f.mutateCandidateToNewGraph(t, candidate)
	if _, err := f.repo.SubmitCourse(f.ctx, f.validator, SubmitCourseRequest{
		CourseID: f.courseID, RevisionID: candidate.ID,
		OwnerAccountID: f.ownerID, ActorDescriptor: f.ownerID,
	}); err != nil {
		t.Fatalf("submitting new graph: %v", err)
	}
	candidateGraph, err := f.repo.loadRevisionGraphByID(f.ctx, candidate.ID)
	if err != nil {
		t.Fatalf("loading candidate graph: %v", err)
	}
	newFingerprint := authoredFingerprint(candidateGraph)

	start := make(chan struct{})
	results := make(chan string, 800)
	errs := make(chan error, 1)
	var readers sync.WaitGroup
	for i := 0; i < 8; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			<-start
			for j := 0; j < 100; j++ {
				course, err := f.repo.GetLiveCourseGraph(f.ctx, f.courseID)
				if err != nil {
					select {
					case errs <- err:
					default:
					}
					return
				}
				results <- authoredFingerprint(course.LiveRevision)
			}
		}()
	}
	approvalErr := make(chan error, 1)
	go func() {
		<-start
		_, err := f.repo.ApproveCourse(f.ctx, f.validator, ApproveCourseRequest{
			CourseID: f.courseID, RevisionID: candidate.ID,
			AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
		})
		approvalErr <- err
	}()
	close(start)
	readers.Wait()
	close(results)
	if err := <-approvalErr; err != nil {
		t.Fatalf("ApproveCourse: %v", err)
	}
	select {
	case err := <-errs:
		t.Fatalf("concurrent live read: %v", err)
	default:
	}
	for fingerprint := range results {
		if fingerprint != oldFingerprint && fingerprint != newFingerprint {
			t.Fatalf("reader observed a mixed graph:\nold=%s\nnew=%s\nobserved=%s",
				oldFingerprint, newFingerprint, fingerprint)
		}
	}
}

func TestD5ApprovalDependencyLocksSerializeConflictingWrites(t *testing.T) {
	f := newD5Fixture(t)
	candidate := f.submittedCandidate(t)

	reached := make(chan struct{}, 1)
	release := make(chan struct{})
	approvalDone := make(chan error, 1)
	go func() {
		_, err := f.repo.ApproveCourse(
			withApprovalHold(f.ctx, approvalHold{reached: reached, release: release}),
			f.validator, ApproveCourseRequest{
				CourseID: f.courseID, RevisionID: candidate.ID,
				AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
			})
		approvalDone <- err
	}()
	<-reached

	type dependencyWrite struct {
		name string
		sql  string
		arg  string
	}
	writes := []dependencyWrite{
		{name: "owner", sql: `UPDATE accounts SET status = 'SUSPENDED' WHERE id = $1::uuid`, arg: f.ownerID},
		{name: "taxonomy", sql: `UPDATE taxonomy_terms SET retired_at = now() WHERE id = $1::uuid`, arg: f.subjectOld},
		{name: "asset", sql: `UPDATE videos SET status = 'FAILED' WHERE id = $1::uuid`, arg: f.videoOld},
	}
	writeDone := make(chan string, len(writes))
	writeErr := make(chan error, len(writes))
	for _, write := range writes {
		write := write
		go func() {
			_, err := f.p.Exec(f.ctx, write.sql, write.arg)
			if err != nil {
				writeErr <- fmt.Errorf("%s dependency write: %w", write.name, err)
				return
			}
			writeDone <- write.name
		}()
	}

	select {
	case name := <-writeDone:
		t.Fatalf("%s dependency write escaped the approval share lock", name)
	case err := <-writeErr:
		t.Fatal(err)
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	if err := <-approvalDone; err != nil {
		t.Fatalf("ApproveCourse: %v", err)
	}
	for range writes {
		select {
		case <-writeDone:
		case err := <-writeErr:
			t.Fatal(err)
		case <-time.After(5 * time.Second):
			t.Fatal("dependency write stayed blocked after approval commit")
		}
	}
}

func TestD5PublishedCandidateRejectionPreservesLiveStateAndAccess(t *testing.T) {
	f := newD5Fixture(t)
	candidate := f.submittedCandidate(t)

	var enrollmentID string
	if err := f.p.QueryRow(f.ctx, `
		INSERT INTO enrollments (student_account_id, course_id)
		VALUES ($1, $2)
		RETURNING id::text
	`, f.ownerID, f.courseID).Scan(&enrollmentID); err != nil {
		t.Fatalf("seeding enrollment: %v", err)
	}
	if _, err := f.p.Exec(f.ctx, `
		INSERT INTO progress (enrollment_id, course_lesson_identity_id, max_position_seconds, last_position_seconds)
		VALUES ($1, $2, 10, 5)
	`, enrollmentID, f.lessonIdentityID); err != nil {
		t.Fatalf("seeding stable progress row: %v", err)
	}
	if _, err := f.p.Exec(f.ctx, `
		INSERT INTO fake_entitlements (user_id, lesson_id, role)
		VALUES ($1, $2, 'instructor')
	`, f.ownerID, f.legacyLessonID); err != nil {
		t.Fatalf("seeding entitlement row: %v", err)
	}

	beforeCourse, err := f.repo.GetLiveCourseGraph(f.ctx, f.courseID)
	if err != nil {
		t.Fatalf("GetLiveCourseGraph before rejection: %v", err)
	}
	beforeFingerprint := authoredFingerprint(beforeCourse.LiveRevision)
	var beforeProgress, beforeEntitlements int
	if err := f.p.QueryRow(f.ctx, `
		SELECT
			(SELECT count(*) FROM progress),
			(SELECT count(*) FROM fake_entitlements)
	`).Scan(&beforeProgress, &beforeEntitlements); err != nil {
		t.Fatalf("counting access rows: %v", err)
	}

	const reason = "Keep the bilingual lab instructions complete."
	rejected, err := f.repo.RequestChanges(f.ctx, RequestChangesRequest{
		CourseID: f.courseID, RevisionID: candidate.ID, AdminAccountID: f.adminID,
		Reason: reason, ActorDescriptor: f.adminID,
	})
	if err != nil {
		t.Fatalf("RequestChanges: %v", err)
	}
	if rejected.Lifecycle != LifecyclePublished || rejected.LiveRevisionID == nil ||
		*rejected.LiveRevisionID != f.liveID {
		t.Fatalf("rejection changed returned live state: %+v", rejected)
	}

	var candidateState, storedReason, lifecycle, pointer, liveState string
	var afterProgress, afterEntitlements int
	if err := f.p.QueryRow(f.ctx, `
		SELECT candidate.state, candidate.review_reason, c.lifecycle,
		       c.live_revision_id::text, live.state,
		       (SELECT count(*) FROM progress),
		       (SELECT count(*) FROM fake_entitlements)
		FROM courses c
		JOIN course_revisions candidate ON candidate.id = $2::uuid
		JOIN course_revisions live ON live.id = c.live_revision_id
		WHERE c.id = $1::uuid
	`, f.courseID, candidate.ID).Scan(
		&candidateState, &storedReason, &lifecycle, &pointer, &liveState,
		&afterProgress, &afterEntitlements,
	); err != nil {
		t.Fatalf("reading rejection state: %v", err)
	}
	if candidateState != string(RevisionRejected) || storedReason != reason {
		t.Fatalf("candidate state/reason = %s/%q, want REJECTED/%q", candidateState, storedReason, reason)
	}
	if lifecycle != string(LifecyclePublished) || pointer != f.liveID ||
		liveState != string(RevisionApproved) {
		t.Fatalf("live state changed: lifecycle=%s pointer=%s live=%s", lifecycle, pointer, liveState)
	}
	if afterProgress != beforeProgress || afterEntitlements != beforeEntitlements {
		t.Fatalf("access rows changed: before=%d/%d after=%d/%d",
			beforeProgress, beforeEntitlements, afterProgress, afterEntitlements)
	}
	afterCourse, err := f.repo.GetLiveCourseGraph(f.ctx, f.courseID)
	if err != nil {
		t.Fatalf("GetLiveCourseGraph after rejection: %v", err)
	}
	if authoredFingerprint(afterCourse.LiveRevision) != beforeFingerprint {
		t.Fatal("rejection changed the published graph")
	}

	var audits, notifications int
	if err := f.p.QueryRow(f.ctx, `
		SELECT
			(SELECT count(*) FROM audit_events
			 WHERE action = 'COURSE_REVISION_REJECTED' AND metadata->>'revision_id' = $1),
			(SELECT count(*) FROM outbox_events
			 WHERE event_type = 'catalog.course_revision_rejected' AND aggregate_id = $2::uuid)
	`, candidate.ID, f.courseID).Scan(&audits, &notifications); err != nil {
		t.Fatalf("counting rejection evidence: %v", err)
	}
	if audits != 1 || notifications != 1 {
		t.Fatalf("rejection evidence = audit %d, notification %d; want 1/1", audits, notifications)
	}
}

func requireConflict(t *testing.T, err error) {
	t.Helper()
	var conflict *LifecycleConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("error = %v, want LifecycleConflictError", err)
	}
}

func assertPendingEditAbsent(t *testing.T, f *d5Fixture, candidateID string) {
	t.Helper()
	graph, err := f.repo.loadRevisionGraphByID(f.ctx, candidateID)
	if err != nil {
		t.Fatalf("loading approved candidate: %v", err)
	}
	if graph.TitleEn == "ILLEGAL PENDING EDIT" {
		t.Fatal("pending-candidate mutation entered the approved graph")
	}
}

func TestD5ExactFourRaces(t *testing.T) {
	f := newD5Fixture(t)

	t.Run("1 concurrent first edits", func(t *testing.T) {
		start := make(chan struct{})
		type result struct {
			candidate *CourseRevision
			err       error
		}
		results := make(chan result, 2)
		for i := 0; i < 2; i++ {
			go func() {
				<-start
				candidate, err := f.repo.CreateCandidate(f.ctx, f.courseID, f.ownerID, f.ownerID)
				results <- result{candidate: candidate, err: err}
			}()
		}
		close(start)
		first, second := <-results, <-results
		if first.err != nil || second.err != nil {
			t.Fatalf("first-edit errors: %v / %v", first.err, second.err)
		}
		if first.candidate.ID != second.candidate.ID {
			t.Fatalf("first edits returned different candidates: %s / %s",
				first.candidate.ID, second.candidate.ID)
		}
		var active int
		if err := f.p.QueryRow(f.ctx, `
			SELECT count(*) FROM course_revisions
			WHERE course_id = $1::uuid AND state IN ('DRAFT', 'CHANGES_REQUESTED', 'PENDING_REVIEW')
		`, f.courseID).Scan(&active); err != nil {
			t.Fatalf("counting active candidates: %v", err)
		}
		if active != 1 {
			t.Fatalf("active candidates = %d, want 1", active)
		}
	})

	candidate := f.candidate(t)
	if _, err := f.repo.SubmitCourse(f.ctx, f.validator, SubmitCourseRequest{
		CourseID: f.courseID, RevisionID: candidate.ID,
		OwnerAccountID: f.ownerID, ActorDescriptor: f.ownerID,
	}); err != nil {
		t.Fatalf("submitting race-2 candidate: %v", err)
	}
	t.Run("2 concurrent approvals", func(t *testing.T) {
		start := make(chan struct{})
		errs := make(chan error, 2)
		for i := 0; i < 2; i++ {
			go func() {
				<-start
				_, err := f.repo.ApproveCourse(f.ctx, f.validator, ApproveCourseRequest{
					CourseID: f.courseID, RevisionID: candidate.ID,
					AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
				})
				errs <- err
			}()
		}
		close(start)
		first, second := <-errs, <-errs
		if (first == nil) == (second == nil) {
			t.Fatalf("approvals did not produce one winner: %v / %v", first, second)
		}
		if first != nil {
			requireConflict(t, first)
		} else {
			requireConflict(t, second)
		}
		var liveCount, evidence int
		if err := f.p.QueryRow(f.ctx, `
			SELECT
				(SELECT count(*) FROM course_revisions WHERE course_id = $1::uuid AND state = 'APPROVED'),
				(SELECT count(*) FROM audit_events
				 WHERE action = 'COURSE_PUBLISHED' AND metadata->>'revision_id' = $2)
		`, f.courseID, candidate.ID).Scan(&liveCount, &evidence); err != nil {
			t.Fatalf("reading approval race evidence: %v", err)
		}
		if liveCount != 1 || evidence != 1 {
			t.Fatalf("approval race state/evidence = %d/%d, want 1/1", liveCount, evidence)
		}
	})

	t.Run("3 approval versus Instructor mutation", func(t *testing.T) {
		t.Run("mutation locks first", func(t *testing.T) {
			candidate := f.submittedCandidate(t)
			reached := make(chan struct{}, 1)
			release := make(chan struct{})
			mutationResult := make(chan error, 1)
			go func() {
				_, err := f.repo.UpdateCourseRevision(
					withCandidateMutationHold(f.ctx, candidateMutationHold{
						reached: reached,
						release: release,
					}),
					f.validator,
					UpdateRevisionRequest{
						CourseID: f.courseID, RevisionID: candidate.ID, OwnerAccountID: f.ownerID,
						TitleEn: "ILLEGAL PENDING EDIT",
					},
					f.ownerID,
				)
				mutationResult <- err
			}()
			<-reached

			approvalResult := make(chan error, 1)
			go func() {
				_, err := f.repo.ApproveCourse(f.ctx, f.validator, ApproveCourseRequest{
					CourseID: f.courseID, RevisionID: candidate.ID,
					AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
				})
				approvalResult <- err
			}()
			select {
			case err := <-approvalResult:
				t.Fatalf("approval bypassed mutation's Course lock: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			close(release)
			requireConflict(t, <-mutationResult)
			if err := <-approvalResult; err != nil {
				t.Fatalf("approval must commit after the refused mutation: %v", err)
			}
			assertPendingEditAbsent(t, f, candidate.ID)
		})

		t.Run("approval locks first", func(t *testing.T) {
			candidate := f.submittedCandidate(t)
			reached := make(chan struct{}, 1)
			release := make(chan struct{})
			approvalResult := make(chan error, 1)
			go func() {
				_, err := f.repo.ApproveCourse(
					withApprovalHold(f.ctx, approvalHold{reached: reached, release: release}),
					f.validator, ApproveCourseRequest{
						CourseID: f.courseID, RevisionID: candidate.ID,
						AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
					})
				approvalResult <- err
			}()
			<-reached

			mutationResult := make(chan error, 1)
			go func() {
				_, err := f.repo.UpdateCourseRevision(f.ctx, f.validator, UpdateRevisionRequest{
					CourseID: f.courseID, RevisionID: candidate.ID, OwnerAccountID: f.ownerID,
					TitleEn: "ILLEGAL PENDING EDIT",
				}, f.ownerID)
				mutationResult <- err
			}()
			select {
			case err := <-mutationResult:
				t.Fatalf("mutation bypassed approval's Course lock: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			close(release)
			if err := <-approvalResult; err != nil {
				t.Fatalf("approval must commit before the refused mutation: %v", err)
			}
			requireConflict(t, <-mutationResult)
			assertPendingEditAbsent(t, f, candidate.ID)
		})
	})

	candidate = f.submittedCandidate(t)
	t.Run("4 approval versus rejection", func(t *testing.T) {
		var approvalOutboxBefore, rejectionOutboxBefore int
		if err := f.p.QueryRow(f.ctx, `
			SELECT
				(SELECT count(*) FROM outbox_events
				 WHERE event_type = 'catalog.course_published' AND aggregate_id = $1::uuid),
				(SELECT count(*) FROM outbox_events
				 WHERE event_type = 'catalog.course_revision_rejected' AND aggregate_id = $1::uuid)
		`, f.courseID).Scan(&approvalOutboxBefore, &rejectionOutboxBefore); err != nil {
			t.Fatalf("reading terminal-race outbox baseline: %v", err)
		}
		start := make(chan struct{})
		errs := make(chan error, 2)
		go func() {
			<-start
			_, err := f.repo.ApproveCourse(f.ctx, f.validator, ApproveCourseRequest{
				CourseID: f.courseID, RevisionID: candidate.ID,
				AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
			})
			errs <- err
		}()
		go func() {
			<-start
			_, err := f.repo.RequestChanges(f.ctx, RequestChangesRequest{
				CourseID: f.courseID, RevisionID: candidate.ID, AdminAccountID: f.adminID,
				Reason: "Concurrent rejection", ActorDescriptor: f.adminID,
			})
			errs <- err
		}()
		close(start)
		first, second := <-errs, <-errs
		if (first == nil) == (second == nil) {
			t.Fatalf("terminal actions did not produce one winner: %v / %v", first, second)
		}
		if first != nil {
			requireConflict(t, first)
		} else {
			requireConflict(t, second)
		}
		var state string
		var approvals, rejections, approvalOutbox, rejectionOutbox int
		if err := f.p.QueryRow(f.ctx, `
			SELECT state,
			       (SELECT count(*) FROM audit_events
			        WHERE action = 'COURSE_PUBLISHED' AND metadata->>'revision_id' = $1),
			       (SELECT count(*) FROM audit_events
			        WHERE action = 'COURSE_REVISION_REJECTED' AND metadata->>'revision_id' = $1),
			       (SELECT count(*) FROM outbox_events
			        WHERE event_type = 'catalog.course_published'
			          AND safe_payload->>'course_id' = $2),
			       (SELECT count(*) FROM outbox_events
			        WHERE event_type = 'catalog.course_revision_rejected'
			          AND safe_payload->>'course_id' = $2)
			FROM course_revisions WHERE id = $1::uuid
		`, candidate.ID, f.courseID).Scan(
			&state, &approvals, &rejections, &approvalOutbox, &rejectionOutbox,
		); err != nil {
			t.Fatalf("reading terminal race evidence: %v", err)
		}
		if state != string(RevisionApproved) && state != string(RevisionRejected) {
			t.Fatalf("candidate terminal state = %s", state)
		}
		if approvals+rejections != 1 {
			t.Fatalf("contradictory terminal audits: approve=%d reject=%d", approvals, rejections)
		}
		if state == string(RevisionApproved) && approvals != 1 {
			t.Fatal("approved candidate lacks matching approval audit")
		}
		if state == string(RevisionRejected) && rejections != 1 {
			t.Fatal("rejected candidate lacks matching rejection audit")
		}
		approvalDelta := approvalOutbox - approvalOutboxBefore
		rejectionDelta := rejectionOutbox - rejectionOutboxBefore
		if approvalDelta+rejectionDelta != 1 {
			t.Fatalf("contradictory terminal outbox evidence: approval delta=%d rejection delta=%d",
				approvalDelta, rejectionDelta)
		}
		if state == string(RevisionApproved) && approvalDelta != 1 {
			t.Fatal("approved candidate lacks matching approval outbox event")
		}
		if state == string(RevisionRejected) && rejectionDelta != 1 {
			t.Fatal("rejected candidate lacks matching rejection outbox event")
		}
	})
}
