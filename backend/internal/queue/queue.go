// Package queue wraps asynq (Redis-backed job queue) client/server setup
// shared by the API (enqueues jobs) and the worker (consumes them).
package queue

import "github.com/hibiken/asynq"

// Job type names, colon-namespaced per asynq convention so future job types
// (thumbnail:generate, subtitle:generate, watermark:apply, virus:scan) don't collide.
const (
	TypeMetadataExtract = "video:metadata_extract"
	TypeTranscode        = "video:transcode"
)

func NewClient(redisAddr string) *asynq.Client {
	return asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})
}

func NewServer(redisAddr string) *asynq.Server {
	return asynq.NewServer(
		asynq.RedisClientOpt{Addr: redisAddr},
		asynq.Config{
			Concurrency: 10,
			Queues: map[string]int{
				"default": 1,
			},
		},
	)
}
