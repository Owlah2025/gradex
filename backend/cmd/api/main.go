package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Owlah2025/gradex/backend/internal/auth"
	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/db"
	"github.com/Owlah2025/gradex/backend/internal/health"
	"github.com/Owlah2025/gradex/backend/internal/httpapi"
	"github.com/Owlah2025/gradex/backend/internal/identity"
	"github.com/Owlah2025/gradex/backend/internal/logging"
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

	logger := logging.New(os.Stdout, "gradex-api", string(cfg.Environment()), logging.LevelFromString(cfg.LogLevel()))

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

	redisHealth := queue.NewHealthClient(cfg.RedisAddr())
	defer redisHealth.Close()

	svc := video.NewService(pool, storageClient, queueClient, cfg)

	// Belt and braces: config validation already refuses AUTH_FAKE_MODE when
	// APP_ENV=production, and the real authenticator does not exist yet, so the
	// API still refuses to start without the development seam.
	if !cfg.AuthFakeMode() {
		log.Fatal("real auth is not implemented yet — set AUTH_FAKE_MODE=true (dev/test only)")
	}
	authenticator := auth.NewFakeAuthenticator()
	entitlements := auth.NewFakeEntitlementChecker(pool)

	reporter := health.New(cfg.ReadinessTimeout(),
		health.Check{
			Name:     "postgres",
			Required: true,
			Probe:    func(ctx context.Context) error { return db.Ping(ctx, pool) },
		},
		health.Check{
			Name:     "schema",
			Required: true,
			Probe:    func(ctx context.Context) error { return db.CheckSchema(ctx, pool) },
		},
		health.Check{
			Name: "redis",
			// Required by role, not by a generic flag: this becomes false for
			// the API once command admission is durable in PostgreSQL alone.
			Required: cfg.ServiceRole().RequiresRedis(),
			Probe:    redisHealth.Ping,
		},
	)

	// Authorization reads Account and credential state from PostgreSQL on every
	// protected request rather than trusting anything carried in the session.
	principals := identity.NewDBPrincipalResolver(pool)

	router, err := httpapi.NewRouter(cfg, logger, reporter, svc, authenticator, entitlements, principals)
	if err != nil {
		log.Fatalf("building router: %v", err)
	}

	server := &http.Server{
		Addr:         ":" + cfg.Port(),
		Handler:      router,
		ReadTimeout:  cfg.HTTPReadTimeout(),
		WriteTimeout: cfg.HTTPWriteTimeout(),
		IdleTimeout:  cfg.HTTPIdleTimeout(),
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("gradex API listening on :%s (env=%s, role=%s, payments=%v, email=%v, fake_auth=%v)",
			cfg.Port(), cfg.Environment(), cfg.ServiceRole(),
			cfg.Payments().Enabled(), cfg.Email().Enabled(), cfg.AuthFakeMode())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// Readiness opens only once configuration, dependencies, and routing are
	// all constructed. Before this the process answers liveness but takes no
	// traffic.
	reporter.MarkStarted()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		log.Fatalf("server error: %v", err)
	case sig := <-shutdown:
		log.Printf("received %s, draining", sig)
	}

	// Fail readiness first so the load balancer stops sending new requests,
	// then finish what is already in flight. Liveness stays healthy throughout
	// — a draining process should be removed from the pool, not killed.
	reporter.MarkDraining()

	drainCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(drainCtx); err != nil {
		log.Printf("graceful shutdown did not complete: %v", err)
	}
	log.Println("gradex API stopped")
}
