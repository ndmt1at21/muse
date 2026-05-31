// Package redisstore implements the ephemeral-state ports over a Redis-protocol
// store (Dragonfly in deploy; any Redis works). For the vertical slice it backs
// idempotency keys and read-model caching; sessions/rate-limit/leaderboard ZSETs
// build on the same client in later phases. SQL remains the source of truth for
// stock/rewards — Redis is cache + ephemeral state, never authoritative money.
package redisstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache is a thin TTL key/value store over Redis.
type Cache struct {
	rdb    *redis.Client
	prefix string
}

// Open dials Redis at addr (host:port). prefix namespaces all keys.
func Open(ctx context.Context, addr, prefix string) (*Cache, error) {
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redisstore: ping %s: %w", addr, err)
	}
	return &Cache{rdb: rdb, prefix: prefix}, nil
}

// Close closes the client.
func (c *Cache) Close() error { return c.rdb.Close() }

// Ping verifies connectivity (used by /readyz).
func (c *Cache) Ping(ctx context.Context) error { return c.rdb.Ping(ctx).Err() }

func (c *Cache) key(k string) string { return c.prefix + ":" + k }

// ErrCacheMiss is returned by Get when the key is absent.
var ErrCacheMiss = errors.New("redisstore: cache miss")

// Get returns the raw value for k, or ErrCacheMiss.
func (c *Cache) Get(ctx context.Context, k string) ([]byte, error) {
	b, err := c.rdb.Get(ctx, c.key(k)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrCacheMiss
	}
	if err != nil {
		return nil, fmt.Errorf("redisstore: get: %w", err)
	}
	return b, nil
}

// Set writes k=v with a TTL (0 = no expiry).
func (c *Cache) Set(ctx context.Context, k string, v []byte, ttl time.Duration) error {
	if err := c.rdb.Set(ctx, c.key(k), v, ttl).Err(); err != nil {
		return fmt.Errorf("redisstore: set: %w", err)
	}
	return nil
}

// SetNX writes k=v only if absent, returning whether it was set. Used for
// idempotency: the first Play with a key wins, retries observe the stored result.
func (c *Cache) SetNX(ctx context.Context, k string, v []byte, ttl time.Duration) (bool, error) {
	ok, err := c.rdb.SetNX(ctx, c.key(k), v, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("redisstore: setnx: %w", err)
	}
	return ok, nil
}

// Del removes keys (used to invalidate read-model cache entries). Missing keys
// are ignored.
func (c *Cache) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	full := make([]string, len(keys))
	for i, k := range keys {
		full[i] = c.key(k)
	}
	if err := c.rdb.Del(ctx, full...).Err(); err != nil {
		return fmt.Errorf("redisstore: del: %w", err)
	}
	return nil
}

// rateLimitScript is an atomic fixed-window counter: INCR the key, set the
// window TTL on first hit, and return the current count plus remaining TTL (ms).
// Atomicity (single round-trip Lua) makes the limit hold across BFF replicas.
var rateLimitScript = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
local ttl = redis.call('PTTL', KEYS[1])
return {n, ttl}
`)

// Allow applies a distributed fixed-window rate limit: it counts requests for
// key within window and reports whether this one is under limit. When the limit
// is exceeded, retryAfter is the time until the window resets. A fresh window is
// opened lazily on the first request and expires automatically.
func (c *Cache) Allow(ctx context.Context, key string, limit int, window time.Duration) (allowed bool, retryAfter time.Duration, err error) {
	res, err := rateLimitScript.Run(ctx, c.rdb, []string{c.key("rl:" + key)}, window.Milliseconds()).Slice()
	if err != nil {
		return false, 0, fmt.Errorf("redisstore: allow: %w", err)
	}
	count, _ := res[0].(int64)
	ttlMS, _ := res[1].(int64)
	if count > int64(limit) {
		if ttlMS < 0 {
			ttlMS = 0
		}
		return false, time.Duration(ttlMS) * time.Millisecond, nil
	}
	return true, 0, nil
}
