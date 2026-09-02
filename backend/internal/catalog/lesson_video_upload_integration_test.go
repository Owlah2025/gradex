//go:build integration

package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Owlah2025/gradex/backend/internal/media"
)

type lessonVideoScanner struct{}

func (lessonVideoScanner) Scan(_ context.Context, object media.ObjectVersion) (media.ScanObservation, error) {
	return media.ScanObservation{
		AssetVersionID: object.AssetVersionID, StorageObjectVersion: object.StorageObjectVersion,
		Outcome: media.ScanPassed, ScannerIdentity: "lesson-video-claim-test",
	}, nil
}

type lessonVideoProcessor struct{ failure error }

func (p lessonVideoProcessor) Transcode(_ context.Context, object media.ObjectVersion) (media.TranscodeResult, error) {
	if p.failure != nil {
		return media.TranscodeResult{}, p.failure
	}
	return media.TranscodeResult{
		OutputPrefix: "media/" + object.AssetVersionID + "/hls", TrustedDurationMS: 60000,
		Renditions: []media.Rendition{{
			Name: "720p", StorageObjectKey: "media/" + object.AssetVersionID + "/hls/720p/index.m3u8",
			Width: 1280, Height: 720, BitrateKbps: 2800, DurationMS: 60000,
		}},
	}, nil
}

func seedLessonVideoUpload(
	t *testing.T,
	f *d5Fixture,
	createdAt time.Time,
	completed bool,
) string {
	t.Helper()
	assetID, versionID := uuid.NewString(), uuid.NewString()
	key := "quarantine/" + f.courseID + "/" + versionID + "/source"
	if _, err := f.p.Exec(f.ctx, `
		INSERT INTO media_assets (id, kind, owner_account_id, course_id, lesson_id, visibility, created_at)
		VALUES ($1::uuid, 'VIDEO', $2::uuid, $3::uuid, $4::uuid, 'PROTECTED', $5)
	`, assetID, f.ownerID, f.courseID, f.lessonIdentityID, createdAt); err != nil {
		t.Fatalf("seeding Lesson video asset: %v", err)
	}
	state := "UPLOADED"
	if completed {
		state = "QUARANTINED"
	}
	if _, err := f.p.Exec(f.ctx, `
		INSERT INTO media_asset_versions (
			id, logical_asset_id, kind, state, storage_object_key, storage_object_version,
			content_type, size_bytes, sha256_hex, created_at
		) VALUES ($1::uuid, $2::uuid, 'VIDEO', $3::media_asset_version_state, $4, 'fixture-v1',
			'video/mp4', 1024, $5, $6)
	`, versionID, assetID, state, key, func() any {
		if completed {
			return strings.Repeat("a", 64)
		}
		return nil
	}(), createdAt); err != nil {
		t.Fatalf("seeding Lesson video version: %v", err)
	}

	var completedAt any
	var fingerprint any
	if completed {
		completedAt = createdAt.Add(time.Minute)
		fingerprint = "completed:" + versionID
	}
	if _, err := f.p.Exec(f.ctx, `
		INSERT INTO upload_intents (
			asset_version_id, expected_object_key, expected_content_type, expected_size_bytes,
			max_size_bytes, expires_at, completed_at, completion_fingerprint, created_at
		) VALUES ($1::uuid, $2, 'video/mp4', 1024, 2048, $3, $4, $5, $6)
	`, versionID, key, createdAt.Add(time.Hour), completedAt, fingerprint, createdAt); err != nil {
		t.Fatalf("seeding Lesson video upload intent: %v", err)
	}
	return versionID
}

func claimLessonVideo(t *testing.T, f *d5Fixture, revisionID, versionID string) *LessonVideoUploadClaim {
	t.Helper()
	claim, err := f.repo.ClaimLessonVideoUpload(f.ctx, ClaimLessonVideoUploadRequest{
		CourseID: f.courseID, RevisionID: revisionID, LessonID: f.lessonIdentityID,
		VideoAssetVersionID: versionID, OwnerAccountID: f.ownerID,
	}, f.ownerID)
	if err != nil {
		t.Fatalf("ClaimLessonVideoUpload: %v", err)
	}
	return claim
}

func selectedLessonVideo(t *testing.T, f *d5Fixture, revisionID string) string {
	t.Helper()
	var selected string
	if err := f.p.QueryRow(f.ctx, `
		SELECT cl.video_asset_version_id::text
		FROM course_lessons cl
		JOIN course_sections cs ON cs.id = cl.section_id
		WHERE cs.revision_id = $1::uuid AND cl.lesson_identity_id = $2::uuid
	`, revisionID, f.lessonIdentityID).Scan(&selected); err != nil {
		t.Fatalf("reading selected Lesson video: %v", err)
	}
	return selected
}

func videoFromEditableReadModel(t *testing.T, f *d5Fixture) Lesson {
	t.Helper()
	course, err := f.repo.GetOwnedCourse(f.ctx, f.courseID, f.ownerID)
	if err != nil {
		t.Fatalf("GetOwnedCourse: %v", err)
	}
	if course.EditableRevision == nil || len(course.EditableRevision.Sections) != 1 || len(course.EditableRevision.Sections[0].Lessons) != 1 {
		t.Fatalf("editable Course graph is incomplete: %+v", course.EditableRevision)
	}
	return course.EditableRevision.Sections[0].Lessons[0]
}

func lessonVideoWorker(t *testing.T, f *d5Fixture, processor lessonVideoProcessor) *media.Worker {
	t.Helper()
	scanner, err := media.NewScannerAdapter(lessonVideoScanner{})
	if err != nil {
		t.Fatalf("NewScannerAdapter: %v", err)
	}
	worker, err := media.NewWorker(media.WorkerOptions{
		DB: f.p, Scanner: scanner, Process: processor, Outbox: testOutboxWriter(t),
	})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	return worker
}

func runLessonVideoWorker(t *testing.T, f *d5Fixture, versionID string, processor lessonVideoProcessor) error {
	t.Helper()
	worker := lessonVideoWorker(t, f, processor)
	if err := worker.Scan(f.ctx, versionID); err != nil {
		t.Fatalf("Worker.Scan: %v", err)
	}
	var operationID string
	if err := f.p.QueryRow(f.ctx, `
		SELECT safe_payload->>'operation_id'
		FROM outbox_events
		WHERE event_type = 'media.transcode_requested' AND aggregate_id = $1::uuid
		ORDER BY occurred_at DESC, id DESC LIMIT 1
	`, versionID).Scan(&operationID); err != nil {
		t.Fatalf("loading transcode operation: %v", err)
	}
	return worker.Transcode(f.ctx, versionID, operationID)
}

func TestLessonVideoUploadClaimIsDurableAndOrdered(t *testing.T) {
	t.Run("incomplete replacement leaves existing video selected", func(t *testing.T) {
		f := newD5Fixture(t)
		candidate := f.candidate(t)
		incomplete := seedLessonVideoUpload(t, f, time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC), false)

		_, err := f.repo.ClaimLessonVideoUpload(f.ctx, ClaimLessonVideoUploadRequest{
			CourseID: f.courseID, RevisionID: candidate.ID, LessonID: f.lessonIdentityID,
			VideoAssetVersionID: incomplete, OwnerAccountID: f.ownerID,
		}, f.ownerID)
		if !errors.Is(err, ErrAssetVersionInvalid) {
			t.Fatalf("incomplete claim error = %v, want %v", err, ErrAssetVersionInvalid)
		}
		if got := selectedLessonVideo(t, f, candidate.ID); got != f.videoOld {
			t.Fatalf("candidate video = %s, want existing %s", got, f.videoOld)
		}

		wrongLesson := seedLessonVideoUpload(t, f, time.Date(2026, 9, 2, 10, 1, 0, 0, time.UTC), true)
		if _, err := f.p.Exec(f.ctx, `
			UPDATE media_assets SET lesson_id = $1::uuid
			WHERE id = (SELECT logical_asset_id FROM media_asset_versions WHERE id = $2::uuid)
		`, uuid.NewString(), wrongLesson); err != nil {
			t.Fatalf("moving upload to another Lesson provenance: %v", err)
		}
		_, err = f.repo.ClaimLessonVideoUpload(f.ctx, ClaimLessonVideoUploadRequest{
			CourseID: f.courseID, RevisionID: candidate.ID, LessonID: f.lessonIdentityID,
			VideoAssetVersionID: wrongLesson, OwnerAccountID: f.ownerID,
		}, f.ownerID)
		if !errors.Is(err, ErrAssetVersionInvalid) {
			t.Fatalf("cross-Lesson claim error = %v, want %v", err, ErrAssetVersionInvalid)
		}
	})

	t.Run("newer completed intent wins despite reverse claim arrival", func(t *testing.T) {
		f := newD5Fixture(t)
		candidate := f.candidate(t)
		older := seedLessonVideoUpload(t, f, time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC), true)
		newer := seedLessonVideoUpload(t, f, time.Date(2026, 9, 2, 10, 1, 0, 0, time.UTC), true)

		if claim := claimLessonVideo(t, f, candidate.ID, newer); !claim.Selected {
			t.Fatal("newer completed upload was not selected")
		}
		if claim := claimLessonVideo(t, f, candidate.ID, older); claim.Selected {
			t.Fatal("older late claim unexpectedly replaced the newer upload")
		}
		if got := selectedLessonVideo(t, f, candidate.ID); got != newer {
			t.Fatalf("candidate video = %s, want newer %s", got, newer)
		}
		if got := selectedLessonVideo(t, f, f.liveID); got != f.videoOld {
			t.Fatalf("live video = %s, want unchanged %s", got, f.videoOld)
		}
	})
}

func TestLessonVideoUploadClaimSurvivesBrowserExitAndProjectsWorkerState(t *testing.T) {
	t.Run("worker success becomes READY in the reloaded authoring graph", func(t *testing.T) {
		f := newD5Fixture(t)
		candidate := f.candidate(t)
		versionID := seedLessonVideoUpload(t, f, time.Date(2026, 9, 2, 11, 0, 0, 0, time.UTC), true)
		claimLessonVideo(t, f, candidate.ID, versionID)

		if err := runLessonVideoWorker(t, f, versionID, lessonVideoProcessor{}); err != nil {
			t.Fatalf("Worker.Transcode: %v", err)
		}
		lesson := videoFromEditableReadModel(t, f)
		if lesson.VideoAssetVersionID == nil || *lesson.VideoAssetVersionID != versionID || lesson.VideoAssetState == nil || *lesson.VideoAssetState != "READY" {
			t.Fatalf("reloaded Lesson video = id:%v state:%v, want %s READY", lesson.VideoAssetVersionID, lesson.VideoAssetState, versionID)
		}
	})

	t.Run("worker failure becomes PROCESS_FAILED in the reloaded authoring graph", func(t *testing.T) {
		f := newD5Fixture(t)
		candidate := f.candidate(t)
		versionID := seedLessonVideoUpload(t, f, time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC), true)
		claimLessonVideo(t, f, candidate.ID, versionID)

		processingFailure := errors.New("fixture transcoder failed")
		if err := runLessonVideoWorker(t, f, versionID, lessonVideoProcessor{failure: processingFailure}); !errors.Is(err, processingFailure) {
			t.Fatalf("Worker.Transcode error = %v, want processing failure", err)
		}
		lesson := videoFromEditableReadModel(t, f)
		if lesson.VideoAssetState == nil || *lesson.VideoAssetState != "PROCESS_FAILED" {
			t.Fatalf("reloaded Lesson state = %v, want PROCESS_FAILED", lesson.VideoAssetState)
		}
	})
}

func TestNonReadySelectedLessonVideoCannotPassLifecycleGates(t *testing.T) {
	f := newD5Fixture(t)
	candidate := f.candidate(t)
	versionID := seedLessonVideoUpload(t, f, time.Date(2026, 9, 2, 13, 0, 0, 0, time.UTC), true)
	claimLessonVideo(t, f, candidate.ID, versionID)

	_, err := f.repo.SubmitCourse(f.ctx, f.validator, SubmitCourseRequest{
		CourseID: f.courseID, RevisionID: candidate.ID,
		OwnerAccountID: f.ownerID, ActorDescriptor: f.ownerID,
	})
	assertSubmissionFailure(t, err)

	if _, err := f.p.Exec(f.ctx, `UPDATE course_revisions SET state = 'PENDING_REVIEW' WHERE id = $1::uuid`, candidate.ID); err != nil {
		t.Fatalf("placing candidate under review: %v", err)
	}
	_, err = f.repo.ApproveCourse(f.ctx, f.validator, ApproveCourseRequest{
		CourseID: f.courseID, RevisionID: candidate.ID,
		AdminAccountID: f.adminID, ActorDescriptor: f.adminID,
	})
	assertSubmissionFailure(t, err)
	if got := selectedLessonVideo(t, f, f.liveID); got != f.videoOld {
		t.Fatalf("live video = %s, want unchanged %s", got, f.videoOld)
	}
}
