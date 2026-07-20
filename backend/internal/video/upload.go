package video

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/hibiken/asynq"

	"github.com/Owlah2025/gradex/backend/internal/queue"
)

var allowedExtensions = map[string]bool{".mp4": true, ".mov": true}

// inFlightStatuses are the statuses a video can't be re-uploaded over without
// risking corrupting an active processing run.
var inFlightStatuses = map[Status]bool{
	StatusUploading:  true,
	StatusUploaded:   true,
	StatusQueued:     true,
	StatusProcessing: true,
}

func (s *videoService) RequestUpload(ctx context.Context, lessonID, filename, contentType string) (UploadTicket, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	if !allowedExtensions[ext] {
		return UploadTicket{}, fmt.Errorf("%w: file type %q not allowed (only .mp4/.mov)", ErrConflict, ext)
	}

	lesson, err := s.repo.getLesson(ctx, lessonID)
	if err != nil {
		return UploadTicket{}, err
	}

	rawKey := fmt.Sprintf("courses/%s/%s/raw/original%s", lesson.CourseID, lesson.ID, ext)

	existing, err := s.repo.getVideoByLesson(ctx, lessonID)
	switch {
	case err == nil:
		if inFlightStatuses[existing.Status] {
			return UploadTicket{}, fmt.Errorf("%w: video for lesson %s is already %s", ErrConflict, lessonID, existing.Status)
		}
		// DRAFT, FAILED, READY, or PUBLISHED (replace-over-published case,
		// see docs/superpowers/specs/2026-07-17-video-streaming-design.md §4 —
		// full dual-live-until-swap preservation of the old hls_master_key is
		// hardened in the Phase 6 pass; this call already leaves the old
		// hls_master_key in place until a new one is written in transcode.go).
		if err := s.repo.updateVideoForReupload(ctx, existing.ID, rawKey); err != nil {
			return UploadTicket{}, err
		}
	case err == ErrNotFound:
		if _, err := s.repo.insertVideo(ctx, lessonID, rawKey, StatusUploading); err != nil {
			return UploadTicket{}, err
		}
	default:
		return UploadTicket{}, err
	}

	expiry := time.Duration(s.cfg.UploadURLExpiryMinutes) * time.Minute
	uploadURL, err := s.storage.PresignPutURL(ctx, rawKey, contentType, expiry)
	if err != nil {
		return UploadTicket{}, err
	}

	return UploadTicket{
		UploadURL: uploadURL,
		RawKey:    rawKey,
		ExpiresAt: time.Now().Add(expiry),
	}, nil
}

// CompleteUpload is idempotent: calling it again while already
// QUEUED/PROCESSING/READY/PUBLISHED is a no-op success, per
// docs/superpowers/specs/2026-07-17-video-streaming-design.md §4.
func (s *videoService) CompleteUpload(ctx context.Context, lessonID string) error {
	v, err := s.repo.getVideoByLesson(ctx, lessonID)
	if err != nil {
		return err
	}

	switch v.Status {
	case StatusQueued, StatusProcessing, StatusReady, StatusPublished:
		return nil // already completed — idempotent no-op
	case StatusUploading:
		// proceed below
	default:
		return fmt.Errorf("%w: nothing to complete for video %s in status %s", ErrConflict, v.ID, v.Status)
	}

	if v.RawKey == nil {
		return fmt.Errorf("%w: video %s has no raw_key", ErrConflict, v.ID)
	}

	size, exists, err := s.storage.HeadObject(ctx, *v.RawKey)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%w: no object found at %s", ErrNotFound, *v.RawKey)
	}
	if size > s.cfg.MaxUploadSizeBytes {
		_, _ = s.repo.setVideoFailed(ctx, v.ID, fmt.Sprintf("uploaded file (%d bytes) exceeds max allowed size (%d bytes)", size, s.cfg.MaxUploadSizeBytes))
		return fmt.Errorf("%w: file too large", ErrConflict)
	}

	applied, err := s.repo.transitionVideoStatus(ctx, v.ID, StatusUploading, StatusQueued)
	if err != nil {
		return err
	}
	if !applied {
		return nil // lost a race with another completion call — already handled
	}

	return s.enqueueMetadataExtract(ctx, v.ID, lessonID, *v.RawKey)
}

func (s *videoService) enqueueMetadataExtract(ctx context.Context, videoID, lessonID, rawKey string) error {
	payload, err := json.Marshal(MetadataExtractPayload{VideoID: videoID, LessonID: lessonID, RawKey: rawKey})
	if err != nil {
		return fmt.Errorf("marshaling metadata extract payload: %w", err)
	}
	task := asynq.NewTask(queue.TypeMetadataExtract, payload, asynq.MaxRetry(3))
	if _, err := s.queue.EnqueueContext(ctx, task); err != nil {
		return fmt.Errorf("enqueueing metadata extract: %w", err)
	}
	return nil
}

// Publish moves a fully-transcoded lesson video live for students. Not part
// of VideoService (it's a Gradex catalog/lifecycle decision, not a video
// processing concern) — see Service in service.go.
func (s *videoService) Publish(ctx context.Context, lessonID string) error {
	v, err := s.repo.getVideoByLesson(ctx, lessonID)
	if err != nil {
		return err
	}
	applied, err := s.repo.transitionVideoStatus(ctx, v.ID, StatusReady, StatusPublished)
	if err != nil {
		return err
	}
	if !applied {
		return fmt.Errorf("%w: video %s is not READY (currently %s)", ErrConflict, v.ID, v.Status)
	}
	return nil
}
