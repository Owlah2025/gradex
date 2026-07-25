package main

import (
	"context"
	"log"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/db"
	"github.com/Owlah2025/gradex/backend/internal/httpapi"
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

	svc := video.NewService(pool, storageClient, queueClient, cfg)

	// Belt and braces: config validation already refuses AUTH_FAKE_MODE when
	// APP_ENV=production, and the real authenticator does not exist yet, so the
	// API still refuses to start without the development seam.
	if !cfg.AuthFakeMode() {
		log.Fatal("real auth is not implemented yet — set AUTH_FAKE_MODE=true (dev/test only)")
	}
	authenticator := auth.NewFakeAuthenticator()
	entitlements := auth.NewFakeEntitlementChecker(pool)

	router := httpapi.NewRouter(svc, authenticator, entitlements)

	log.Printf("gradex API listening on :%s (env=%s, payments=%v, email=%v, fake_auth=%v)",
		cfg.Port(), cfg.Environment(), cfg.Payments().Enabled(), cfg.Email().Enabled(), cfg.AuthFakeMode())
	if err := router.Run(":" + cfg.Port()); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
