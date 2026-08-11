package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/db"
	"github.com/Owlah2025/gradex/backend/internal/media/e2ediagnostic"
	"github.com/Owlah2025/gradex/backend/internal/queue"
	"github.com/hibiken/asynq"
)

func main() {
	inputPath, outputPath := os.Getenv("GRADEX_MEDIA_E2E_DIAGNOSTIC_INPUT"), os.Getenv("GRADEX_MEDIA_E2E_DIAGNOSTIC_OUTPUT")
	in, err := e2ediagnostic.ReadInput(inputPath)
	if err != nil {
		fail("input")
		return
	}
	cfg, err := config.Load()
	if err != nil {
		fail("config")
		return
	}
	pool, err := db.Connect(context.Background(), cfg.DatabaseURL().Expose())
	if err != nil {
		fail("database")
		return
	}
	defer pool.Close()
	connection, err := queue.NewConnection(cfg.Redis())
	if err != nil {
		fail("redis")
		return
	}
	redis := connection.NewRedisClient()
	defer redis.Close()
	inspector := asynq.NewInspectorFromRedisClient(redis)
	defer inspector.Close()
	collector, err := e2ediagnostic.New(pool, inspector)
	if err != nil {
		fail("collector")
		return
	}
	if err := e2ediagnostic.Write(outputPath, collector.Capture(context.Background(), in)); err != nil {
		fail("write")
		return
	}
}

func fail(stage string) { fmt.Fprintln(os.Stderr, "media diagnostic failed:", stage); os.Exit(1) }
