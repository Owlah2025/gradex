// Command enhancement-drain proves that no schema-42 manual enhancement work
// remains in any durable store, so a supervised 42 -> 41 downgrade may proceed.
//
// It exists as its own entrypoint because the proof spans two stores.
// cmd/migrate speaks only to PostgreSQL by design — a migration command has no
// business holding a Redis client — yet a dispatched enhancement intent lives in
// Redis as an asynq task while its outbox row already carries a dispatch
// receipt. Asking only the database would pass a rollback that is not safe.
//
// It is strictly read-only. It deletes nothing, cancels nothing, and reschedules
// nothing: rollback must never silently discard work an Administrator requested.
// Exit status 0 means drained; any non-zero status, including a failure to obtain
// the proof, means the rollback must not run.
package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/hibiken/asynq"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/db"
	"github.com/Owlah2025/gradex/backend/internal/media"
	"github.com/Owlah2025/gradex/backend/internal/queue"
)

// inspectTimeout bounds the whole proof. A hung Redis or database must fail the
// gate rather than leave an operator waiting mid-rollback.
const inspectTimeout = 30 * time.Second

func main() {
	// The DSN carries the database password and a refused drain check is exactly
	// when output gets pasted into a ticket, so credentials are removed at the
	// single place every message exits through, as cmd/migrate does.
	scrub := newScrubber()
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "enhancement-drain: %s\n", scrub(err.Error()))
		os.Exit(1)
	}
}

func newScrubber() func(string) string {
	var secrets []string
	for _, key := range []string{"DATABASE_URL", "REDIS_PASSWORD"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		secrets = append(secrets, value)
		if u, err := url.Parse(value); err == nil && u.User != nil {
			if password, set := u.User.Password(); set && password != "" {
				secrets = append(secrets, password)
			}
		}
	}
	return func(s string) string {
		for _, secret := range secrets {
			s = strings.ReplaceAll(s, secret, "[REDACTED]")
		}
		return s
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), inspectTimeout)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}
	pool, err := db.Connect(ctx, cfg.DatabaseURL().Expose())
	if err != nil {
		return fmt.Errorf("connecting to the database: %w", err)
	}
	defer pool.Close()

	connection, err := queue.NewConnection(cfg.Redis())
	if err != nil {
		return fmt.Errorf("configuring the queue connection: %w", err)
	}
	redis := connection.NewRedisClient()
	defer func() { _ = redis.Close() }()
	inspector := asynq.NewInspectorFromRedisClient(redis)
	defer func() { _ = inspector.Close() }()

	report, err := media.InspectEnhancementDrain(ctx, pool, inspector)
	if err != nil {
		return fmt.Errorf("inspecting enhancement work: %w", err)
	}
	fmt.Println(report.Summary())
	return report.Err()
}
