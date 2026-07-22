package video

import (
	"context"
	"fmt"
)

// Retranscode is the manual-retry entry point (instructor-triggered, after
// the 3x automatic retry in worker.go is exhausted and the video is FAILED).
// It restarts the full job chain from metadata.extract, not just the
// transcode step, matching the auto-retry path's semantics in
// docs/superpowers/specs/2026-07-17-video-streaming-design.md §4
// ("Retry restarts the job chain from metadata.extract").
func (s *videoService) Retranscode(ctx context.Context, lessonID string) error {
	v, err := s.repo.getVideoByLesson(ctx, lessonID)
	if err != nil {
		return err
	}

	if v.Status != StatusFailed {
		return fmt.Errorf("%w: video %s is not FAILED (currently %s)", ErrConflict, v.ID, v.Status)
	}
	if v.RawKey == nil {
		return fmt.Errorf("%w: video %s has no raw_key to retranscode", ErrConflict, v.ID)
	}

	applied, err := s.repo.transitionVideoStatus(ctx, v.ID, StatusFailed, StatusQueued)
	if err != nil {
		return err
	}
	if !applied {
		return nil // lost a race with another retry call — already handled
	}

	if err := s.enqueueMetadataExtract(ctx, v.ID, lessonID, *v.RawKey); err != nil {
		// Same gap as CompleteUpload: video is already QUEUED but no job is
		// enqueued. Push back to FAILED so a further Retranscode call can
		// pick it up, instead of stranding it silently.
		s.markFailedOnEnqueueError(ctx, v.ID, err)
		return err
	}
	return nil
}
