package video

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/queue"
	"github.com/Owlah2025/gradex/backend/internal/storage"
)

// Worker holds every job handler for the video pipeline. Every handler
// follows the same idempotency shape: read current status, no-op if it's not
// the expected prior state (asynq redelivery / concurrent processing already
// handled it), otherwise do the work behind a guarded transitionVideoStatus
// compare-and-swap write. See repo.go's transitionVideoStatus for the guard
// itself.
type Worker struct {
	repo    repo
	storage *storage.Client
	queue   *asynq.Client
	ffmpeg  *FFmpeg
}

func NewWorker(db *pgxpool.Pool, storageClient *storage.Client, queueClient *asynq.Client, ffmpeg *FFmpeg) *Worker {
	return &Worker{
		repo:    newRepo(db),
		storage: storageClient,
		queue:   queueClient,
		ffmpeg:  ffmpeg,
	}
}

func (w *Worker) Register(mux *asynq.ServeMux) {
	mux.HandleFunc(queue.TypeMetadataExtract, w.HandleMetadataExtract)
	mux.HandleFunc(queue.TypeTranscode, w.HandleTranscode)
}

// HandleMetadataExtract downloads the raw upload, runs ffprobe, writes the
// technical metadata columns, then enqueues video:transcode. Metadata
// extraction itself doesn't own a distinct lifecycle status — it's a
// sub-step of QUEUED before video:transcode moves things to PROCESSING (see
// worker.go's future HandleTranscode in Phase 4).
func (w *Worker) HandleMetadataExtract(ctx context.Context, t *asynq.Task) error {
	var p MetadataExtractPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("unmarshaling metadata extract payload: %w", err)
	}

	v, err := w.repo.getVideoByID(ctx, p.VideoID)
	if err != nil {
		return fmt.Errorf("loading video %s: %w", p.VideoID, err)
	}

	// Idempotency guard: if this video has moved past QUEUED already (a
	// previous delivery of this exact job completed, or a later stage ran),
	// there's nothing to do — asynq redelivery after a crash/visibility
	// timeout must not re-extract metadata it already wrote.
	if v.Status != StatusQueued {
		return nil
	}

	localPath, cleanup, err := w.downloadToTemp(ctx, p.RawKey)
	if err != nil {
		w.markFailed(ctx, v.ID, fmt.Sprintf("downloading raw upload: %v", err))
		return err
	}
	defer cleanup()

	info, err := os.Stat(localPath)
	if err != nil {
		w.markFailed(ctx, v.ID, fmt.Sprintf("stat local file: %v", err))
		return err
	}

	meta, err := w.ffmpeg.ExtractMetadata(ctx, localPath)
	if err != nil {
		w.markFailed(ctx, v.ID, fmt.Sprintf("ffprobe: %v", err))
		return err
	}

	if err := w.repo.updateVideoMetadata(ctx, v.ID, meta.Resolution, meta.Bitrate, meta.Codec, meta.FPS, info.Size()); err != nil {
		return fmt.Errorf("saving metadata for video %s: %w", v.ID, err)
	}
	if meta.DurationSeconds > 0 {
		if err := w.repo.updateLessonDuration(ctx, p.LessonID, meta.DurationSeconds); err != nil {
			return fmt.Errorf("saving lesson duration for lesson %s: %w", p.LessonID, err)
		}
	}

	payload, err := json.Marshal(TranscodeJobPayload{
		VideoID:    v.ID,
		LessonID:   p.LessonID,
		RawKey:     p.RawKey,
		Resolution: meta.Resolution,
	})
	if err != nil {
		return fmt.Errorf("marshaling transcode payload: %w", err)
	}
	task := asynq.NewTask(queue.TypeTranscode, payload, asynq.MaxRetry(3))
	if _, err := w.queue.EnqueueContext(ctx, task); err != nil {
		return fmt.Errorf("enqueueing transcode job: %w", err)
	}

	return nil
}

// HandleTranscode runs the full HLS ladder transcode for a video and uploads
// the result, then flips status PROCESSING -> READY. Idempotency guard: if
// the video isn't QUEUED or PROCESSING, a previous delivery of this job (or a
// later stage) already handled it — no-op. If it's already PROCESSING (a
// mid-flight redelivery after a worker crash), the ffmpeg work is safely
// re-run: writing the same hls/ output twice to the same keys is a no-op in
// effect, per docs/superpowers/specs/2026-07-20-mediakit-spike-plan.md §7.
func (w *Worker) HandleTranscode(ctx context.Context, t *asynq.Task) error {
	var p TranscodeJobPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("unmarshaling transcode payload: %w", err)
	}

	v, err := w.repo.getVideoByID(ctx, p.VideoID)
	if err != nil {
		return fmt.Errorf("loading video %s: %w", p.VideoID, err)
	}

	if v.Status != StatusQueued && v.Status != StatusProcessing {
		return nil // already handled — safe no-op
	}
	if v.Status == StatusQueued {
		if _, err := w.repo.transitionVideoStatus(ctx, v.ID, StatusQueued, StatusProcessing); err != nil {
			return fmt.Errorf("transitioning video %s to PROCESSING: %w", v.ID, err)
		}
	}

	lesson, err := w.repo.getLesson(ctx, p.LessonID)
	if err != nil {
		w.markFailed(ctx, v.ID, fmt.Sprintf("loading lesson: %v", err))
		return err
	}

	localPath, cleanup, err := w.downloadToTemp(ctx, p.RawKey)
	if err != nil {
		w.markFailed(ctx, v.ID, fmt.Sprintf("downloading raw upload: %v", err))
		return err
	}
	defer cleanup()

	outDir, err := os.MkdirTemp("", "gradex-hls-*")
	if err != nil {
		w.markFailed(ctx, v.ID, fmt.Sprintf("creating scratch dir: %v", err))
		return err
	}
	defer os.RemoveAll(outDir)

	renditions := RenditionsForSourceHeight(parseHeight(p.Resolution))
	relFiles, err := w.ffmpeg.Transcode(ctx, localPath, outDir, renditions)
	if err != nil {
		w.markFailed(ctx, v.ID, fmt.Sprintf("ffmpeg transcode: %v", err))
		return err
	}

	hlsPrefix := fmt.Sprintf("courses/%s/%s/hls", lesson.CourseID, lesson.ID)
	for _, rel := range relFiles {
		localFilePath := filepath.Join(outDir, rel)
		data, err := os.ReadFile(localFilePath)
		if err != nil {
			w.markFailed(ctx, v.ID, fmt.Sprintf("reading transcoded file %s: %v", rel, err))
			return err
		}
		key := hlsPrefix + "/" + filepath.ToSlash(rel)
		if err := w.storage.PutObject(ctx, key, data, contentTypeFor(rel)); err != nil {
			w.markFailed(ctx, v.ID, fmt.Sprintf("uploading %s: %v", key, err))
			return err
		}
	}

	if err := w.repo.updateVideoHLSMasterKey(ctx, v.ID, hlsPrefix+"/master.m3u8"); err != nil {
		return fmt.Errorf("saving hls_master_key for video %s: %w", v.ID, err)
	}

	if _, err := w.repo.transitionVideoStatus(ctx, v.ID, StatusProcessing, StatusReady); err != nil {
		return fmt.Errorf("transitioning video %s to READY: %w", v.ID, err)
	}
	return nil
}

func parseHeight(resolution string) int {
	parts := strings.SplitN(resolution, "x", 2)
	if len(parts) != 2 {
		return 0
	}
	var h int
	fmt.Sscanf(parts[1], "%d", &h)
	return h
}

func contentTypeFor(relPath string) string {
	switch filepath.Ext(relPath) {
	case ".m3u8":
		return "application/vnd.apple.mpegurl"
	case ".ts":
		return "video/mp2t"
	default:
		return "application/octet-stream"
	}
}

func (w *Worker) markFailed(ctx context.Context, videoID, reason string) {
	if _, err := w.repo.setVideoFailed(ctx, videoID, reason); err != nil {
		fmt.Printf("worker: failed to mark video %s FAILED after error %q: %v\n", videoID, reason, err)
	}
}

func (w *Worker) downloadToTemp(ctx context.Context, key string) (path string, cleanup func(), err error) {
	data, err := w.storage.DownloadObject(ctx, key)
	if err != nil {
		return "", nil, fmt.Errorf("downloading %s: %w", key, err)
	}

	f, err := os.CreateTemp("", "gradex-video-*.mp4")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", nil, fmt.Errorf("writing temp file: %w", err)
	}
	f.Close()

	return f.Name(), func() { os.Remove(f.Name()) }, nil
}
