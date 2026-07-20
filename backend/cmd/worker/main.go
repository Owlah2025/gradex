package main

import (
	"context"
	"log"

	"github.com/hibiken/asynq"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/db"
	"github.com/Owlah2025/gradex/backend/internal/queue"
	"github.com/Owlah2025/gradex/backend/internal/storage"
	"github.com/Owlah2025/gradex/backend/internal/video"
)

func main() {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connecting to database: %v", err)
	}
	defer pool.Close()

	storageClient, err := storage.New(ctx, storage.Options{
		Endpoint:     cfg.S3Endpoint,
		AccessKey:    cfg.S3AccessKey,
		SecretKey:    cfg.S3SecretKey,
		Bucket:       cfg.S3Bucket,
		Region:       cfg.S3Region,
		UsePathStyle: cfg.S3UsePathStyle,
	})
	if err != nil {
		log.Fatalf("connecting to storage: %v", err)
	}

	queueClient := queue.NewClient(cfg.RedisAddr)
	defer queueClient.Close()

	ffmpeg := video.NewFFmpeg(cfg.FFmpegBinaryPath, cfg.FFprobeBinaryPath)
	worker := video.NewWorker(pool, storageClient, queueClient, ffmpeg)

	mux := asynq.NewServeMux()
	worker.Register(mux)

	server := queue.NewServer(cfg.RedisAddr)
	log.Println("gradex video worker starting")
	if err := server.Run(mux); err != nil {
		log.Fatalf("worker error: %v", err)
	}
}
