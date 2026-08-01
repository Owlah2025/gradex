package main

import (
	"context"
	"log"
	"time"

	"github.com/hibiken/asynq"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/db"
	"github.com/Owlah2025/gradex/backend/internal/media"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
	"github.com/Owlah2025/gradex/backend/internal/queue"
	"github.com/Owlah2025/gradex/backend/internal/storage"
)

func main() {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	pool, err := db.Connect(ctx, cfg.DatabaseURL().Expose())
	if err != nil {
		log.Fatalf("connecting to database: %v", err)
	}
	defer pool.Close()

	storageClient, err := storage.New(ctx, storage.Options{
		Endpoint:     cfg.S3Endpoint(),
		AccessKey:    cfg.S3AccessKey().Expose(),
		SecretKey:    cfg.S3SecretKey().Expose(),
		Bucket:       cfg.S3Bucket(),
		Region:       cfg.S3Region(),
		UsePathStyle: cfg.S3UsePathStyle(),
	})
	if err != nil {
		log.Fatalf("connecting to storage: %v", err)
	}

	queueClient := queue.NewClient(cfg.RedisAddr())
	defer queueClient.Close()

	writer, err := outbox.NewWriter(cfg.Admission().ProtectedPayloadKeyVersion(), []byte(cfg.Admission().ProtectedPayloadKey().Expose()))
	if err != nil {
		log.Fatalf("building media outbox writer: %v", err)
	}
	unavailable, err := media.NewUnavailableScanner("LG-014 scanner is not configured")
	if err != nil {
		log.Fatalf("building scanner capability: %v", err)
	}
	scanner, err := media.NewScannerAdapter(unavailable)
	if err != nil {
		log.Fatalf("building scanner adapter: %v", err)
	}
	processor, err := media.NewFFmpegProcessor(storageClient, cfg.FFmpegBinaryPath(), cfg.FFprobeBinaryPath(), cfg.MediaProcessingTimeout())
	if err != nil {
		log.Fatalf("building media processor: %v", err)
	}
	worker, err := media.NewWorker(media.WorkerOptions{DB: pool, Scanner: scanner, Process: processor, Outbox: writer, ProcessingTimeout: cfg.MediaProcessingTimeout()})
	if err != nil {
		log.Fatalf("building media worker: %v", err)
	}
	dispatcher, err := media.NewDispatcher(pool, queueClient, cfg.MediaProcessingTimeout())
	if err != nil {
		log.Fatalf("building media dispatcher: %v", err)
	}

	mux := asynq.NewServeMux()
	if err := worker.Register(mux); err != nil {
		log.Fatalf("registering media worker: %v", err)
	}
	go runMediaDispatcher(ctx, dispatcher)

	server := queue.NewServer(cfg.RedisAddr())
	log.Println("gradex media worker starting")
	if err := server.Run(mux); err != nil {
		log.Fatalf("worker error: %v", err)
	}
}

func runMediaDispatcher(ctx context.Context, dispatcher *media.Dispatcher) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := dispatcher.DispatchPending(ctx, 50); err != nil {
			log.Printf("media outbox dispatch failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
