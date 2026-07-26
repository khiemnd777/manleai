package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisKeyPrefix = "manleai:rate_limit:v1:"

var tokenBucketScript = redis.NewScript(`
local clock = redis.call('TIME')
local now_ms = (tonumber(clock[1]) * 1000) + math.floor(tonumber(clock[2]) / 1000)
local capacity = tonumber(ARGV[1])
local refill_per_ms = tonumber(ARGV[2])
local state = redis.call('HMGET', KEYS[1], 'tokens', 'updated_at_ms')
local tokens = tonumber(state[1])
local updated_at_ms = tonumber(state[2])

if tokens == nil or updated_at_ms == nil then
  tokens = capacity
  updated_at_ms = now_ms
else
  local elapsed_ms = math.max(0, now_ms - updated_at_ms)
  tokens = math.min(capacity, tokens + (elapsed_ms * refill_per_ms))
end

local allowed = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
end

local retry_after_ms = 0
if allowed == 0 then
  retry_after_ms = math.ceil((1 - tokens) / refill_per_ms)
end
local reset_after_ms = math.ceil((capacity - tokens) / refill_per_ms)
local ttl_ms = math.max(1000, math.ceil((capacity / refill_per_ms) * 2))

redis.call('HSET', KEYS[1], 'tokens', tostring(tokens), 'updated_at_ms', tostring(now_ms))
redis.call('PEXPIRE', KEYS[1], ttl_ms)
return {allowed, math.floor(tokens), retry_after_ms, reset_after_ms}
`)

type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(redisURL string) (*RedisStore, error) {
	options, err := redis.ParseURL(strings.TrimSpace(redisURL))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid Redis URL", ErrUnavailable)
	}
	return &RedisStore{client: redis.NewClient(options)}, nil
}

func (s *RedisStore) Ping(ctx context.Context) error {
	if s == nil || s.client == nil {
		return ErrUnavailable
	}
	if err := s.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return nil
}

func (s *RedisStore) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}

func (s *RedisStore) Take(ctx context.Context, identity string, policy Policy) (Decision, error) {
	if err := policy.Validate(); err != nil {
		return Decision{}, err
	}
	identity = strings.TrimSpace(identity)
	if s == nil || s.client == nil || identity == "" {
		return Decision{}, ErrUnavailable
	}
	refillPerMillisecond := float64(policy.Rate) / float64(policy.Window/time.Millisecond)
	result, err := tokenBucketScript.Run(ctx, s.client, []string{redisKeyPrefix + policy.Name + ":" + identity},
		policy.Burst, strconv.FormatFloat(refillPerMillisecond, 'g', -1, 64)).Result()
	if err != nil {
		return Decision{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	values, ok := result.([]any)
	if !ok || len(values) != 4 {
		return Decision{}, fmt.Errorf("%w: invalid Redis rate-limit result", ErrUnavailable)
	}
	allowed, err := redisInteger(values[0])
	if err != nil {
		return Decision{}, err
	}
	remaining, err := redisInteger(values[1])
	if err != nil {
		return Decision{}, err
	}
	retryAfterMilliseconds, err := redisInteger(values[2])
	if err != nil {
		return Decision{}, err
	}
	resetAfterMilliseconds, err := redisInteger(values[3])
	if err != nil {
		return Decision{}, err
	}
	return Decision{
		Allowed:    allowed == 1,
		Limit:      policy.Burst,
		Remaining:  max(0, int(remaining)),
		RetryAfter: time.Duration(max(0, retryAfterMilliseconds)) * time.Millisecond,
		ResetAfter: time.Duration(max(0, resetAfterMilliseconds)) * time.Millisecond,
	}, nil
}

func redisInteger(value any) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		if err == nil {
			return parsed, nil
		}
	case []byte:
		parsed, err := strconv.ParseInt(string(typed), 10, 64)
		if err == nil {
			return parsed, nil
		}
	}
	return 0, fmt.Errorf("%w: invalid Redis rate-limit value", ErrUnavailable)
}

func IsUnavailable(err error) bool {
	return errors.Is(err, ErrUnavailable)
}
