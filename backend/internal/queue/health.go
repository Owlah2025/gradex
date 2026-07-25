package queue

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// HealthClient is a Redis connection used only for readiness probes.
//
// It is separate from the asynq client so a probe cannot consume a connection
// the job path needs, and so probing never enqueues, dequeues, or writes a
// key. asynq is built on this same driver, so the probe still exercises the
// real client path rather than a raw socket.
type HealthClient struct {
	rdb *redis.Client
}

func NewHealthClient(redisAddr string) *HealthClient {
	return &HealthClient{rdb: redis.NewClient(&redis.Options{Addr: redisAddr})}
}

// Ping issues one bounded, non-mutating command. It creates no persistent test
// key: a probe that writes is a probe that can fill a disk.
func (c *HealthClient) Ping(ctx context.Context) error {
	if err := c.rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("pinging redis: %w", err)
	}
	return nil
}

func (c *HealthClient) Close() error { return c.rdb.Close() }
