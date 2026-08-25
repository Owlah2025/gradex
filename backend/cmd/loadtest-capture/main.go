// Command loadtest-capture emits one bounded, read-only PostgreSQL, Redis, and
// Asynq worker snapshot for a future capacity run. It never writes application
// rows, Redis keys, queue tasks, or result secrets.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/queue"
)

type captureArtifact struct {
	SchemaVersion   int             `json:"schema_version"`
	CapturedAt      time.Time       `json:"captured_at"`
	PostgresMetrics postgresMetrics `json:"postgres_metrics"`
	RedisMetrics    redisMetrics    `json:"redis_metrics"`
	WorkerMetrics   workerMetrics   `json:"worker_metrics"`
}

type postgresMetrics struct {
	Safe                    bool  `json:"safe"`
	ActiveConnections       int   `json:"active_connections"`
	IdleConnections         int   `json:"idle_connections"`
	WaitingQueries          int   `json:"waiting_queries"`
	LongRunningQueries      int   `json:"long_running_queries"`
	PoolAcquiredConnections int32 `json:"pool_acquired_connections"`
	PoolIdleConnections     int32 `json:"pool_idle_connections"`
	PoolTotalConnections    int32 `json:"pool_total_connections"`
	PoolMaxConnections      int32 `json:"pool_max_connections"`
	PoolCanceledAcquires    int64 `json:"pool_canceled_acquires"`
	PoolEmptyAcquires       int64 `json:"pool_empty_acquires"`
}

type redisMetrics struct {
	Safe                bool  `json:"safe"`
	UsedMemoryBytes     int64 `json:"used_memory_bytes"`
	ConnectedClients    int64 `json:"connected_clients"`
	RejectedConnections int64 `json:"rejected_connections"`
	BlockedClients      int64 `json:"blocked_clients"`
	Evictions           int64 `json:"evictions"`
	CommandErrors       int64 `json:"command_errors"`
}

type workerMetrics struct {
	Safe                        bool    `json:"safe"`
	WorkerCount                 int     `json:"worker_count"`
	QueueDepth                  int     `json:"queue_depth"`
	OldestRelevantJobAgeSeconds float64 `json:"oldest_relevant_job_age_seconds"`
	ActiveJobs                  int     `json:"active_jobs"`
	ActiveTranscodes            int     `json:"active_transcodes"`
	RetryFailures               int     `json:"retry_failures"`
	TerminalFailures            int     `json:"terminal_failures"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "loadtest-capture: diagnostic capture failed")
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pools, err := pgxpool.New(ctx, cfg.DatabaseURL().Expose())
	if err != nil {
		return err
	}
	defer pools.Close()
	postgres, err := capturePostgres(ctx, pools)
	if err != nil {
		return err
	}
	connection, err := queue.NewConnection(cfg.Redis())
	if err != nil {
		return err
	}
	redisClient := connection.NewRedisClient()
	defer redisClient.Close()
	redis, err := captureRedis(ctx, redisClient)
	if err != nil {
		return err
	}
	inspector := asynq.NewInspectorFromRedisClient(redisClient)
	defer inspector.Close()
	worker, err := captureWorker(inspector)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(captureArtifact{
		SchemaVersion: 1, CapturedAt: time.Now().UTC(), PostgresMetrics: postgres,
		RedisMetrics: redis, WorkerMetrics: worker,
	})
}

func capturePostgres(ctx context.Context, pool *pgxpool.Pool) (postgresMetrics, error) {
	var metrics postgresMetrics
	err := pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE state = 'active'),
			count(*) FILTER (WHERE state = 'idle'),
			count(*) FILTER (WHERE wait_event IS NOT NULL),
			count(*) FILTER (WHERE state = 'active' AND now() - query_start > interval '5 seconds')
		FROM pg_stat_activity
		WHERE datname = current_database()
	`).Scan(&metrics.ActiveConnections, &metrics.IdleConnections, &metrics.WaitingQueries, &metrics.LongRunningQueries)
	if err != nil {
		return metrics, err
	}
	stat := pool.Stat()
	metrics.PoolAcquiredConnections = stat.AcquiredConns()
	metrics.PoolIdleConnections = stat.IdleConns()
	metrics.PoolTotalConnections = stat.TotalConns()
	metrics.PoolMaxConnections = stat.MaxConns()
	metrics.PoolCanceledAcquires = stat.CanceledAcquireCount()
	metrics.PoolEmptyAcquires = stat.EmptyAcquireCount()
	metrics.Safe = metrics.WaitingQueries == 0 && metrics.LongRunningQueries == 0 &&
		metrics.PoolCanceledAcquires == 0 && metrics.PoolEmptyAcquires == 0
	return metrics, nil
}

func captureRedis(ctx context.Context, client *redis.Client) (redisMetrics, error) {
	// This adapter keeps the capture logic testable without exposing the Redis
	// client or any credential-bearing option in the artifact.
	command := client.Info(ctx, "memory", "clients", "stats")
	info, err := command.Result()
	if err != nil {
		return redisMetrics{}, err
	}
	values := parseRedisInfo(info)
	metrics := redisMetrics{
		UsedMemoryBytes: values.int64("used_memory"), ConnectedClients: values.int64("connected_clients"),
		RejectedConnections: values.int64("rejected_connections"), BlockedClients: values.int64("blocked_clients"),
		Evictions: values.int64("evicted_keys"), CommandErrors: values.int64("total_error_replies"),
	}
	metrics.Safe = values.present("used_memory") && values.present("connected_clients") &&
		metrics.RejectedConnections == 0 && metrics.BlockedClients == 0 && metrics.Evictions == 0 && metrics.CommandErrors == 0
	return metrics, nil
}

type redisInfoValues map[string]string

func parseRedisInfo(raw string) redisInfoValues {
	values := redisInfoValues{}
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "#") || !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		values[parts[0]] = parts[1]
	}
	return values
}

func (values redisInfoValues) int64(key string) int64 {
	value, err := strconv.ParseInt(values[key], 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func (values redisInfoValues) present(key string) bool { _, ok := values[key]; return ok }

func captureWorker(inspector *asynq.Inspector) (workerMetrics, error) {
	metrics := workerMetrics{Safe: true}
	servers, err := inspector.Servers()
	if err != nil {
		return metrics, err
	}
	metrics.WorkerCount = len(servers)
	queues, err := inspector.Queues()
	if err != nil {
		return metrics, err
	}
	if len(queues) == 0 {
		metrics.Safe = false
		return metrics, nil
	}
	for _, queueName := range queues {
		info, err := inspector.GetQueueInfo(queueName)
		if err != nil {
			return metrics, err
		}
		metrics.QueueDepth += info.Pending + info.Scheduled + info.Retry + info.Aggregating
		metrics.ActiveJobs += info.Active
		metrics.RetryFailures += info.Retry
		metrics.TerminalFailures += info.Archived
		if info.Latency > 0 && info.Latency.Seconds() > metrics.OldestRelevantJobAgeSeconds {
			metrics.OldestRelevantJobAgeSeconds = info.Latency.Seconds()
		}
		active, err := inspector.ListActiveTasks(queueName, asynq.PageSize(1000))
		if err != nil {
			return metrics, err
		}
		for _, task := range active {
			if task.Type == queue.TypeMediaTranscode {
				metrics.ActiveTranscodes++
			}
		}
	}
	metrics.Safe = metrics.WorkerCount > 0 && metrics.RetryFailures == 0 && metrics.TerminalFailures == 0
	return metrics, nil
}
