// Package queue wraps asynq (Redis-backed job queue) client/server setup
// shared by the API (enqueues jobs) and the worker (consumes them).
package queue

import (
	"context"

	"github.com/hibiken/asynq"
	redis "github.com/redis/go-redis/v9"
)

// Job type names, colon-namespaced per asynq convention so future job types
// (thumbnail:generate, subtitle:generate, watermark:apply, virus:scan) don't collide.
const (
	TypeMediaScan      = "media:scan"
	TypeMediaTranscode = "media:transcode"
)

func NewClient(redisAddr string) *asynq.Client {
	return asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})
}

type ServerOptions struct {
	ErrorHandler    asynq.ErrorHandler
	HealthCheckFunc func(error)
	Logger          asynq.Logger
}

func NewServer(redisAddr string, options ServerOptions) *asynq.Server {
	return asynq.NewServer(
		asynq.RedisClientOpt{Addr: redisAddr},
		asynq.Config{
			Concurrency:     10,
			ErrorHandler:    options.ErrorHandler,
			HealthCheckFunc: options.HealthCheckFunc,
			Logger:          options.Logger,
			Queues: map[string]int{
				"default": 1,
			},
		},
	)
}

type discardRedisLogger struct{}

func (discardRedisLogger) Printf(context.Context, string, ...interface{}) {}

// SuppressRedisInternalLogs removes go-redis's free-form dependency messages.
// Worker-owned health and dispatch callbacks remain the structured operational
// record and intentionally exclude raw addresses and error text.
func SuppressRedisInternalLogs() { redis.SetLogger(discardRedisLogger{}) }
