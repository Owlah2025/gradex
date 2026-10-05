//go:build integration

package catalog

import (
	"github.com/google/uuid"
	"testing"
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
	for _, id := range []string{f.videoOld, f.videoNew} {
		seedReadyCourseVideo(t, f, id)
	}
	f.publishInitialRevision(t)
	return f
}

func seedReadyCourseVideo(t *testing.T, f *d5Fixture, id string) {
	t.Helper()
	logical, scan := uuid.NewString(), uuid.NewString()
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO media_assets(id,kind,owner_account_id,course_id,visibility) VALUES($1::uuid,'VIDEO',$2::uuid,$3::uuid,'PROTECTED')`, []any{logical, f.ownerID, f.courseID}},
		{`INSERT INTO media_asset_versions(id,logical_asset_id,kind,state,storage_object_key,storage_object_version,content_type,size_bytes) VALUES($1::uuid,$2::uuid,'VIDEO','QUARANTINED',$3,'fixture-v1','video/mp4',1024)`, []any{id, logical, "quarantine/" + f.courseID + "/" + id + "/source"}},
		{`INSERT INTO scan_attempts(id,asset_version_id,attempt_number,work_id,storage_object_version,outcome,scanner_identity) VALUES($1::uuid,$2::uuid,1,$3,'fixture-v1','PASSED','fixture')`, []any{scan, id, "scan:" + id}},
		{`UPDATE media_asset_versions SET state='SCANNING' WHERE id=$1::uuid`, []any{id}},
		{`UPDATE media_asset_versions SET state='SCAN_PASSED',successful_scan_attempt_id=$2::uuid WHERE id=$1::uuid`, []any{id, scan}},
		{`UPDATE media_asset_versions SET state='PROCESSING',trusted_duration_ms=60000 WHERE id=$1::uuid`, []any{id}},
		{`INSERT INTO video_renditions(logical_asset_id,asset_version_id,rendition_name,storage_object_key,width,height,bitrate_kbps,duration_ms) VALUES($1::uuid,$2::uuid,'720p',$3,1280,720,2800,60000)`, []any{logical, id, "media/" + id + "/fixture/720p/playlist.m3u8"}},
		{`UPDATE media_asset_versions SET state='READY' WHERE id=$1::uuid`, []any{id}},
	}
	for _, statement := range statements {
		if _, err := f.p.Exec(f.ctx, statement.sql, statement.args...); err != nil {
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
}
