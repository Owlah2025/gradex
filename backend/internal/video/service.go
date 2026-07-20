// Package video is the sole bounded context for upload, transcode, and
// playback logic. Nothing outside this package should construct storage
// keys, touch the videos/progress tables directly, or know that S3/FFmpeg
// are the current implementation — everything goes through VideoService.
package video

import (
	"context"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/storage"
)

// VideoService is the only entry point other packages use for video logic.
// Gradex's own backend is the transcoding provider in this build (no external
// vendor), so there is no external asset ID handed back and no HTTP webhook
// to receive — asynq jobs run in-process and the worker writes to Postgres
// directly (see worker.go). See docs/superpowers/specs/2026-07-20-mediakit-spike-plan.md
// §7 for why HandleWebhookEvent and a providerAssetID param were dropped from
// the interface as originally sketched for a third-party-vendor design.
type VideoService interface {
	RequestUpload(ctx context.Context, lessonID, filename, contentType string) (UploadTicket, error)
	CompleteUpload(ctx context.Context, lessonID string) error
	GetPlaybackURL(ctx context.Context, lessonID, viewerID string) (SignedURL, error)
	Retranscode(ctx context.Context, lessonID string) error
}

// Service is the full surface httpapi wires against: VideoService plus the
// operations that were never a transcoding vendor's job in the first place
// (publish is a Gradex lifecycle decision, progress tracking is "gradex still
// owns" per the spike-doc eval) so they stay off the vendor-facing interface.
type Service interface {
	VideoService
	Publish(ctx context.Context, lessonID string) error
	UpdateProgress(ctx context.Context, lessonID, viewerID string, positionSeconds float64) (Progress, error)
	ServeManifest(ctx context.Context, videoID, path, token string) (content []byte, contentType string, err error)
}

type videoService struct {
	repo    repo
	storage *storage.Client
	queue   *asynq.Client
	cfg     *config.Config
}

func NewService(db *pgxpool.Pool, storageClient *storage.Client, queueClient *asynq.Client, cfg *config.Config) Service {
	return &videoService{
		repo:    newRepo(db),
		storage: storageClient,
		queue:   queueClient,
		cfg:     cfg,
	}
}
