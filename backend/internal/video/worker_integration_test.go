//go:build integration

package video

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/queue"
	"github.com/Owlah2025/gradex/backend/internal/storage"
)

// TestRedeliveredJobIsNoOp_OnAlreadyPublishedVideo simulates asynq redelivering
// a stale metadata_extract/transcode job for a video that another delivery
// (or a later stage) already pushed to PUBLISHED — the idempotency guard must
// leave it untouched rather than reprocessing or regressing status.
func TestRedeliveredJobIsNoOp_OnAlreadyPublishedVideo(t *testing.T) {
	ctx := context.Background()

	db, err := pgxpool.New(ctx, "postgres://gradex:gradex@localhost:5432/gradex?sslmode=disable")
	if err != nil {
		t.Fatalf("connecting to db: %v", err)
	}
	defer db.Close()

	sc, err := storage.New(ctx, storage.Options{
		Endpoint: "http://localhost:9000", AccessKey: "gradexminio", SecretKey: "gradexminio",
		Bucket: "gradex-video", Region: "us-east-1", UsePathStyle: true,
	})
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}

	qc := queue.NewClient("localhost:6379")
	defer qc.Close()

	ffmpeg := NewFFmpeg("ffmpeg", "ffprobe")
	w := NewWorker(db, sc, qc, ffmpeg)

	repo := newRepo(db)
	const lessonID = "00000000-0000-0000-0000-000000000030"

	before, err := repo.getVideoByLesson(ctx, lessonID)
	if err != nil {
		t.Fatalf("getVideoByLesson: %v", err)
	}
	if before.Status != StatusPublished {
		t.Skipf("expected fixture video to be PUBLISHED, got %s — run the manual upload/transcode/publish flow first", before.Status)
	}

	metaPayload, _ := json.Marshal(MetadataExtractPayload{VideoID: before.ID, LessonID: lessonID, RawKey: *before.RawKey})
	if err := w.HandleMetadataExtract(ctx, asynq.NewTask(queue.TypeMetadataExtract, metaPayload)); err != nil {
		t.Fatalf("HandleMetadataExtract on already-PUBLISHED video should no-op, got error: %v", err)
	}

	transcodePayload, _ := json.Marshal(TranscodeJobPayload{VideoID: before.ID, LessonID: lessonID, RawKey: *before.RawKey, Resolution: *before.Resolution})
	if err := w.HandleTranscode(ctx, asynq.NewTask(queue.TypeTranscode, transcodePayload)); err != nil {
		t.Fatalf("HandleTranscode on already-PUBLISHED video should no-op, got error: %v", err)
	}

	after, err := repo.getVideoByLesson(ctx, lessonID)
	if err != nil {
		t.Fatalf("getVideoByLesson after: %v", err)
	}
	if after.Status != StatusPublished {
		t.Fatalf("redelivered job regressed status: was PUBLISHED, now %s", after.Status)
	}
	if after.SyncVersion != before.SyncVersion {
		t.Fatalf("redelivered job wrote when it should have no-op'd: sync_version was %d, now %d", before.SyncVersion, after.SyncVersion)
	}
	if *after.HLSMasterKey != *before.HLSMasterKey {
		t.Fatalf("redelivered job changed hls_master_key: was %q, now %q", *before.HLSMasterKey, *after.HLSMasterKey)
	}
}
