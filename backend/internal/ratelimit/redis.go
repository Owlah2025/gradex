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
local clock = redis.call("TIME")
local now = (tonumber(clock[1]) * 1000) + math.floor(tonumber(clock[2]) / 1000)
local next_tokens = {}
for i, key in ipairs(KEYS) do
  local arg = ((i - 1) * 3) + 1
  local limit = tonumber(ARGV[arg])
  local window = tonumber(ARGV[arg + 1])
  local burst = tonumber(ARGV[arg + 2])
  if burst > 0 then
    local state = redis.call("HMGET", key, "tokens", "updated_at")
    local tokens = tonumber(state[1])
    local updated = tonumber(state[2])
    if tokens == nil or updated == nil then
      tokens = burst
      updated = now
    end
    tokens = math.min(burst, tokens + (math.max(0, now - updated) / window) * limit)
    if tokens < 1 then
      return 0
    end
    next_tokens[i] = tokens - 1
  else
    local count = tonumber(redis.call("GET", key)) or 0
    if count + 1 > limit then
      return 0
    end
  end
end

for i, key in ipairs(KEYS) do
  local arg = ((i - 1) * 3) + 1
  local limit = tonumber(ARGV[arg])
  local window = tonumber(ARGV[arg + 1])
  local burst = tonumber(ARGV[arg + 2])
  if burst > 0 then
    redis.call("HSET", key, "tokens", next_tokens[i], "updated_at", now)
    redis.call("PEXPIRE", key, math.max(window, math.ceil((burst / limit) * window)))
  else
    local count = redis.call("INCR", key)
    if count == 1 then
      redis.call("PEXPIRE", key, window)
    end
  end
end
return 1
`)

func (s *RedisStore) Decide(ctx context.Context, entries []Entry) (bool, error) {
	if s == nil || s.client == nil {
		return false, errors.New("Redis rate-limit store is unavailable")
	}
	if len(entries) == 0 {
		return false, errors.New("rate-limit decision has no entries")
	}
	keys := make([]string, 0, len(entries))
	args := make([]any, 0, len(entries)*3)
	for _, entry := range entries {
		keys = append(keys, entry.Key)
		args = append(args, entry.Limit, entry.Window.Milliseconds(), entry.Burst)
	}
	result, err := layeredDecisionScript.Run(ctx, s.client, keys, args...).Int64()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}
