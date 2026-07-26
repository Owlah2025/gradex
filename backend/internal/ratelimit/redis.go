package ratelimit

import (
	"context"
	"errors"

	"github.com/redis/go-redis/v9"
)

// Store performs one atomic decision across every key in an endpoint policy.
type Store interface {
	Decide(context.Context, []Entry) (bool, error)
}

type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(client *redis.Client) *RedisStore {
	return &RedisStore{client: client}
}

var layeredDecisionScript = redis.NewScript(`
local allowed = 1
for i, key in ipairs(KEYS) do
  local arg = ((i - 1) * 2) + 1
  local count = redis.call("INCR", key)
  if count == 1 then
    redis.call("PEXPIRE", key, ARGV[arg + 1])
  end
  if count > tonumber(ARGV[arg]) then
    allowed = 0
  end
end
return allowed
`)

func (s *RedisStore) Decide(ctx context.Context, entries []Entry) (bool, error) {
	if s == nil || s.client == nil {
		return false, errors.New("Redis rate-limit store is unavailable")
	}
	if len(entries) == 0 {
		return false, errors.New("rate-limit decision has no entries")
	}
	keys := make([]string, 0, len(entries))
	args := make([]any, 0, len(entries)*2)
	for _, entry := range entries {
		keys = append(keys, entry.Key)
		args = append(args, entry.Limit, entry.Window.Milliseconds())
	}
	result, err := layeredDecisionScript.Run(ctx, s.client, keys, args...).Int64()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}
