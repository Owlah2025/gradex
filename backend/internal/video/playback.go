package video

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const completionThreshold = 0.9
const manifestContentType = "application/vnd.apple.mpegurl"

// GetPlaybackURL returns a URL to Gradex's own manifest-proxy endpoint
// (ServeManifest), not a raw presigned S3 URL for hls_master_key. A plain S3
// presigned URL signs exactly one object; the HLS player then follows the
// master playlist's relative references to child playlists and segments,
// which would hit storage unsigned and get 403'd (confirmed live during
// Phase 4 verification). The manifest proxy rewrites those references as it
// serves each file — see ServeManifest below.
func (s *videoService) GetPlaybackURL(ctx context.Context, lessonID, viewerID string) (SignedURL, error) {
	v, err := s.repo.getVideoByLesson(ctx, lessonID)
	if err != nil {
		return SignedURL{}, err
	}
	if v.Status != StatusPublished {
		return SignedURL{}, fmt.Errorf("%w: lesson %s video is not PUBLISHED (currently %s)", ErrConflict, lessonID, v.Status)
	}
	if v.HLSMasterKey == nil {
		return SignedURL{}, fmt.Errorf("%w: video %s has no hls_master_key", ErrNotFound, v.ID)
	}

	expiry := time.Duration(s.cfg.PlaybackURLExpiryMinutes) * time.Minute
	expiresAt := time.Now().Add(expiry)
	token := signPlaybackToken(s.cfg.PlaybackTokenSecret, v.ID, expiresAt)
	manifestURL := fmt.Sprintf("/api/v1/videos/%s/manifest/master.m3u8?token=%s", v.ID, token)

	lastPosition := 0.0
	progress, err := s.repo.getProgress(ctx, viewerID, lessonID)
	if err == nil {
		lastPosition = progress.LastPositionSeconds
	} else if err != ErrNotFound {
		return SignedURL{}, err
	}

	return SignedURL{
		URL:                 manifestURL,
		ExpiresAt:           expiresAt,
		LastPositionSeconds: lastPosition,
	}, nil
}

// ServeManifest backs the manifest-proxy endpoint. path is either
// "master.m3u8" or a child playlist like "720p/playlist.m3u8" (asynq/ffmpeg
// naming — see ffmpeg.go). Master playlist child-playlist references are
// rewritten to include the same token (so the player's next request is still
// authorized); child playlist segment references are rewritten to real
// presigned S3 GET URLs, since segments are the actual video bytes and
// should stream directly from storage, not proxy through this backend.
func (s *videoService) ServeManifest(ctx context.Context, videoID, path, token string) ([]byte, string, error) {
	if err := verifyPlaybackToken(s.cfg.PlaybackTokenSecret, videoID, token); err != nil {
		return nil, "", err
	}

	v, err := s.repo.getVideoByID(ctx, videoID)
	if err != nil {
		return nil, "", err
	}
	if v.Status != StatusPublished || v.HLSMasterKey == nil {
		return nil, "", fmt.Errorf("%w: video %s is not playable", ErrConflict, videoID)
	}
	hlsPrefix := strings.TrimSuffix(*v.HLSMasterKey, "master.m3u8")

	content, err := s.storage.DownloadObject(ctx, hlsPrefix+path)
	if err != nil {
		return nil, "", fmt.Errorf("%w: manifest file %s not found", ErrNotFound, path)
	}

	if path == "master.m3u8" {
		rewritten := rewriteMasterPlaylist(content, token)
		return rewritten, manifestContentType, nil
	}

	segmentExpiry := time.Duration(s.cfg.PlaybackURLExpiryMinutes) * time.Minute
	renditionDir := filepath.Dir(path) // e.g. "720p" from "720p/playlist.m3u8"
	rewritten, err := s.rewriteChildPlaylist(ctx, content, hlsPrefix, renditionDir, segmentExpiry)
	if err != nil {
		return nil, "", err
	}
	return rewritten, manifestContentType, nil
}

// rewriteMasterPlaylist appends the manifest token to every child-playlist
// reference line so the player's follow-up request is still authorized.
func rewriteMasterPlaylist(content []byte, token string) []byte {
	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines[i] = trimmed + "?token=" + token
	}
	return []byte(strings.Join(lines, "\n"))
}

// rewriteChildPlaylist replaces every segment reference with a real presigned
// S3 GET URL for that segment.
func (s *videoService) rewriteChildPlaylist(ctx context.Context, content []byte, hlsPrefix, renditionDir string, expiry time.Duration) ([]byte, error) {
	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		segmentKey := hlsPrefix + renditionDir + "/" + trimmed
		signedURL, err := s.storage.PresignGetURL(ctx, segmentKey, expiry)
		if err != nil {
			return nil, fmt.Errorf("presigning segment %s: %w", segmentKey, err)
		}
		lines[i] = signedURL
	}
	return []byte(strings.Join(lines, "\n")), nil
}

// UpdateProgress applies max()/90%-completion semantics per
// docs/superpowers/specs/2026-07-17-video-streaming-design.md §5. Not part of
// VideoService — progress tracking is "gradex still owns" regardless of which
// transcoding provider is behind the interface (see service.go's Service type).
func (s *videoService) UpdateProgress(ctx context.Context, lessonID, viewerID string, positionSeconds float64) (Progress, error) {
	if positionSeconds < 0 {
		return Progress{}, fmt.Errorf("%w: position_seconds must be >= 0", ErrConflict)
	}

	lesson, err := s.repo.getLesson(ctx, lessonID)
	if err != nil {
		return Progress{}, err
	}

	completed := false
	if lesson.DurationSeconds != nil && *lesson.DurationSeconds > 0 {
		completed = positionSeconds >= completionThreshold*(*lesson.DurationSeconds)
	}

	return s.repo.upsertProgress(ctx, viewerID, lessonID, positionSeconds, positionSeconds, completed)
}
