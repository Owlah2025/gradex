//go:build integration

package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newLessonMediaFixture(t *testing.T) *d5Fixture {
	t.Helper()
	freshSchema(t)
	p, ctx := pool(t)
	owner, course := seedInstructorAndCourse(t, p, ctx)
	repo, err := NewRepository(p, testOutboxWriter(t))
	if err != nil {
		t.Fatal(err)
	}
	f := newD5FixtureValue(repo, p, ctx, owner, course)
	f.seedDependencies(t)
	f.publishInitialRevision(t)
	return f
}

func TestLessonVideoBindingRejectsForeignCourseOwnerLessonAndRetirementWithoutMutation(t *testing.T) {
	f := newLessonMediaFixture(t)
	candidate := f.candidate(t)
	foreignCourse := "60000000-0000-0000-0000-000000000001"
	foreignOwner := uuid.NewString()
	if _, err := f.p.Exec(f.ctx, `INSERT INTO accounts(id,normalized_email,email,role,status,display_name) VALUES($1::uuid,'foreign@example.com','foreign@example.com','INSTRUCTOR','ACTIVE','Foreign')`, foreignOwner); err != nil {
		t.Fatal(err)
	}
	otherLesson, err := f.repo.AddLesson(f.ctx, AddLessonRequest{CourseID: f.courseID, RevisionID: candidate.ID, SectionID: f.sectionIdentityID, OwnerAccountID: f.ownerID, TitleAr: "درس", TitleEn: "Other"}, f.ownerID)
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range []struct {
		name, courseID, ownerID, lessonID string
		retired                           bool
	}{
		{name: "course", courseID: foreignCourse, ownerID: f.ownerID},
		{name: "owner", courseID: f.courseID, ownerID: foreignOwner},
		{name: "lesson", courseID: f.courseID, ownerID: f.ownerID, lessonID: otherLesson.LessonIdentityID},
		{name: "retired", courseID: f.courseID, ownerID: f.ownerID, retired: true},
	} {
		t.Run(binding.name, func(t *testing.T) {
			id := uuid.NewString()
			seedCourseVideo(t, f.p, f.ctx, binding.ownerID, binding.courseID, id, true)
			if binding.lessonID != "" {
				if _, err := f.p.Exec(f.ctx, `UPDATE media_assets SET lesson_id=$2::uuid WHERE id=(SELECT logical_asset_id FROM media_asset_versions WHERE id=$1::uuid)`, id, binding.lessonID); err != nil {
					t.Fatal(err)
				}
			}
			if binding.retired {
				if _, err := f.p.Exec(f.ctx, `UPDATE media_assets SET retired_at=now() WHERE id=(SELECT logical_asset_id FROM media_asset_versions WHERE id=$1::uuid)`, id); err != nil {
					t.Fatal(err)
				}
			}
			var before, after int
			if err := f.p.QueryRow(f.ctx, `SELECT count(*) FROM audit_events`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			_, err := f.repo.SetLessonVideo(f.ctx, f.validator, SetVideoRequest{CourseID: f.courseID, RevisionID: candidate.ID, LessonID: f.lessonIdentityID, OwnerAccountID: f.ownerID, VideoAssetVersionID: id}, f.ownerID)
			if !errors.Is(err, ErrAssetVersionInvalid) {
				t.Fatalf("binding refusal=%v", err)
			}
			if selectedLessonVideo(t, f, candidate.ID) != f.videoOld {
				t.Fatal("denial changed lesson video")
			}
			if err := f.p.QueryRow(f.ctx, `SELECT count(*) FROM audit_events`).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if before != after {
				t.Fatal("denial committed mutation audit")
			}
			if binding.name == "course" {
				_, err := f.p.Exec(f.ctx, `UPDATE course_lessons SET video_asset_version_id=$1::uuid WHERE section_id IN (SELECT id FROM course_sections WHERE revision_id=$2::uuid) AND lesson_identity_id=$3::uuid`, id, candidate.ID, f.lessonIdentityID)
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != "23503" || pgErr.ConstraintName != "course_lessons_video_asset_course_fk" {
					t.Fatalf("direct cross-course write=%v", err)
				}
				if selectedLessonVideo(t, f, candidate.ID) != f.videoOld {
					t.Fatal("direct denial changed lesson video")
				}
			}
		})
	}
}

// seedCourseVideo creates exact-version scan and trusted processing evidence.
// Legacy videos rows do not satisfy the authoring ownership contract.
func seedCourseVideo(t *testing.T, p *pgxpool.Pool, ctx context.Context, ownerID, courseID, id string, ready bool) {
	t.Helper()
	logical, scan := uuid.NewString(), uuid.NewString()
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO media_assets(id,kind,owner_account_id,course_id,visibility) VALUES($1::uuid,'VIDEO',$2::uuid,$3::uuid,'PROTECTED')`, []any{logical, ownerID, courseID}},
		{`INSERT INTO media_asset_versions(id,logical_asset_id,kind,state,storage_object_key,storage_object_version,content_type,size_bytes) VALUES($1::uuid,$2::uuid,'VIDEO','QUARANTINED',$3,'fixture-v1','video/mp4',1024)`, []any{id, logical, "quarantine/" + courseID + "/" + id + "/source"}},
		{`INSERT INTO scan_attempts(id,asset_version_id,attempt_number,work_id,storage_object_version,outcome,scanner_identity) VALUES($1::uuid,$2::uuid,1,$3,'fixture-v1','PASSED','fixture')`, []any{scan, id, "scan:" + id}},
		{`UPDATE media_asset_versions SET state='SCANNING' WHERE id=$1::uuid`, []any{id}},
		{`UPDATE media_asset_versions SET state='SCAN_PASSED',successful_scan_attempt_id=$2::uuid WHERE id=$1::uuid`, []any{id, scan}},
		{`UPDATE media_asset_versions SET state='PROCESSING' WHERE id=$1::uuid`, []any{id}},
	}
	for _, statement := range statements {
		if _, err := p.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if ready {
		finishFixtureVideoProcessing(t, p, ctx, id)
	}
}

func finishFixtureVideoProcessing(t *testing.T, p *pgxpool.Pool, ctx context.Context, id string) {
	t.Helper()
	processing := uuid.NewString()
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO processing_attempts(id,asset_version_id,operation_id,state,output_prefix,rendition_count,trusted_duration_ms) VALUES($1::uuid,$2::uuid,$3,'SUCCEEDED',$4,1,60000)`, []any{processing, id, "process:" + id, "media/" + id + "/fixture"}},
		{`INSERT INTO video_renditions(asset_version_id,name,storage_object_key,width,height,bitrate_kbps,duration_ms) VALUES($1::uuid,'720p',$2,1280,720,2800,60000)`, []any{id, "media/" + id + "/fixture/720p/playlist.m3u8"}},
		{`UPDATE media_asset_versions SET state='READY',successful_processing_attempt_id=$2::uuid,trusted_duration_ms=60000 WHERE id=$1::uuid`, []any{id, processing}},
	} {
		if _, err := p.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLessonFileAttachmentRetryConvergesAndWrongKindDoesNotMutateVideo(t *testing.T) {
	f := newLessonMediaFixture(t)
	candidate := f.candidate(t)
	req := LessonFileRequest{CourseID: f.courseID, RevisionID: candidate.ID, LessonID: f.lessonIdentityID, Kind: FileKindResource, AssetVersionID: f.resourceNew, DisplayNameAr: "ملف", DisplayNameEn: "File", OwnerAccountID: f.ownerID}
	first, err := f.repo.AddLessonFile(f.ctx, f.validator, req, f.ownerID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.repo.AddLessonFile(f.ctx, f.validator, req, f.ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatal("retry created another attachment")
	}
	var attachments, audits int
	if err := f.p.QueryRow(f.ctx, `SELECT count(*) FROM lesson_files WHERE lesson_id=$1::uuid AND asset_version_id=$2::uuid`, first.LessonID, f.resourceNew).Scan(&attachments); err != nil {
		t.Fatal(err)
	}
	if err := f.p.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE action='LESSON_FILE_ATTACHED' AND target_id=$1`, first.ID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if attachments != 1 || audits != 1 {
		t.Fatalf("attachments=%d audits=%d", attachments, audits)
	}
	_, err = f.repo.SetLessonVideo(f.ctx, f.validator, SetVideoRequest{CourseID: f.courseID, RevisionID: candidate.ID, LessonID: f.lessonIdentityID, OwnerAccountID: f.ownerID, VideoAssetVersionID: f.resourceNew}, f.ownerID)
	if err != ErrAssetVersionInvalid {
		t.Fatalf("wrong-kind video=%v", err)
	}
	if got := selectedLessonVideo(t, f, candidate.ID); got != f.videoOld {
		t.Fatalf("wrong-kind mutation changed selected video: %s", got)
	}
	if err := f.validator.ValidateLessonVideoForPublication(f.ctx, f.resourceNew); !errors.Is(err, ErrAssetVersionNotReady) {
		t.Fatalf("publication admitted a READY resource as video: %v", err)
	}
}
