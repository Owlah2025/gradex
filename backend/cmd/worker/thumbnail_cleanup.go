package main

import (
	"context"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/media"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

func runThumbnailCleanup(ctx context.Context, pool *pgxpool.Pool, store media.ThumbnailCleanupStore, logger *logging.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		workCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		err := media.CleanupThumbnails(workCtx, pool, store, time.Now())
		cancel()
		if err != nil && ctx.Err() == nil {
			logger.WorkerFailed(logging.WorkerFailureEvent{Operation: "thumbnail_cleanup", ErrorClass: logging.ErrorClassOf(err), RetryCount: -1, MaxRetry: -1})
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
