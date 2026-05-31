package redisstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/muse/gamekit/ports"
	"github.com/redis/go-redis/v9"
)

// --- RankBoard: the real-time leaderboard sorted set (Phase 7) ---
//
// Scores are stored as float64 in a Redis ZSET; we keep integer scores so the
// round-trip is lossless within int64 range used by metrics. Ranking is
// descending (highest score = rank 1) via ZREVRANK / ZREVRANGE.

func (c *Cache) zkey(key string) string { return c.key("lb:" + key) }

// Update sets a member's score (ZADD). Implements ports.RankBoard.
func (c *Cache) Update(ctx context.Context, key, member string, score int64) error {
	if err := c.rdb.ZAdd(ctx, c.zkey(key), redis.Z{Score: float64(score), Member: member}).Err(); err != nil {
		return fmt.Errorf("redisstore: zadd: %w", err)
	}
	return nil
}

// Remove drops a member from the set (ZREM). Implements ports.RankBoard.
func (c *Cache) Remove(ctx context.Context, key, member string) error {
	if err := c.rdb.ZRem(ctx, c.zkey(key), member).Err(); err != nil {
		return fmt.Errorf("redisstore: zrem: %w", err)
	}
	return nil
}

// Reset deletes the whole set (DEL). Implements ports.RankBoard.
func (c *Cache) Reset(ctx context.Context, key string) error {
	if err := c.rdb.Del(ctx, c.zkey(key)).Err(); err != nil {
		return fmt.Errorf("redisstore: del: %w", err)
	}
	return nil
}

// Rank returns a member's 1-based rank and score (ZREVRANK + ZSCORE).
func (c *Cache) Rank(ctx context.Context, key, member string) (int64, int64, bool, error) {
	zk := c.zkey(key)
	rank, err := c.rdb.ZRevRank(ctx, zk, member).Result()
	if errors.Is(err, redis.Nil) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, fmt.Errorf("redisstore: zrevrank: %w", err)
	}
	score, err := c.rdb.ZScore(ctx, zk, member).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return 0, 0, false, fmt.Errorf("redisstore: zscore: %w", err)
	}
	return rank + 1, int64(score), true, nil
}

// Top returns members ranked [offset, offset+limit), highest first, with the
// total set size (ZREVRANGE WITHSCORES + ZCARD).
func (c *Cache) Top(ctx context.Context, key string, offset, limit int) ([]ports.RankMember, int64, error) {
	zk := c.zkey(key)
	if limit <= 0 {
		limit = 20
	}
	zs, err := c.rdb.ZRevRangeWithScores(ctx, zk, int64(offset), int64(offset+limit-1)).Result()
	if err != nil {
		return nil, 0, fmt.Errorf("redisstore: zrevrange: %w", err)
	}
	total, err := c.rdb.ZCard(ctx, zk).Result()
	if err != nil {
		return nil, 0, fmt.Errorf("redisstore: zcard: %w", err)
	}
	out := make([]ports.RankMember, 0, len(zs))
	for i, z := range zs {
		m, _ := z.Member.(string)
		out = append(out, ports.RankMember{Member: m, Score: int64(z.Score), Rank: int64(offset + i + 1)})
	}
	return out, total, nil
}

// Around returns members within radius positions of the member (for around-me).
func (c *Cache) Around(ctx context.Context, key, member string, radius int) ([]ports.RankMember, error) {
	zk := c.zkey(key)
	rank, err := c.rdb.ZRevRank(ctx, zk, member).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redisstore: zrevrank: %w", err)
	}
	start := max(rank-int64(radius), 0)
	stop := rank + int64(radius)
	zs, err := c.rdb.ZRevRangeWithScores(ctx, zk, start, stop).Result()
	if err != nil {
		return nil, fmt.Errorf("redisstore: zrevrange: %w", err)
	}
	out := make([]ports.RankMember, 0, len(zs))
	for i, z := range zs {
		m, _ := z.Member.(string)
		out = append(out, ports.RankMember{Member: m, Score: int64(z.Score), Rank: start + int64(i) + 1})
	}
	return out, nil
}

// --- Locker: a simple SetNX-based distributed lock (Phase 7 finalize) ---

// Acquire grabs the lock for key with a TTL, returning a release func and
// whether it was obtained. Implements ports.Locker. The release deletes the key
// only if it still holds our token (best-effort check-and-delete).
func (c *Cache) Acquire(ctx context.Context, key string, ttl time.Duration) (func(context.Context) error, bool, error) {
	lk := c.key("lock:" + key)
	token := fmt.Sprintf("%d", time.Now().UnixNano())
	ok, err := c.rdb.SetNX(ctx, lk, token, ttl).Result()
	if err != nil {
		return nil, false, fmt.Errorf("redisstore: lock setnx: %w", err)
	}
	if !ok {
		return nil, false, nil
	}
	release := func(ctx context.Context) error {
		// Only release if we still own it (avoid freeing a re-acquired lock).
		cur, gErr := c.rdb.Get(ctx, lk).Result()
		if gErr == nil && cur == token {
			return c.rdb.Del(ctx, lk).Err()
		}
		return nil
	}
	return release, true, nil
}

// Compile-time checks that Cache satisfies the leaderboard infra ports.
var (
	_ ports.RankBoard = (*Cache)(nil)
	_ ports.Locker    = (*Cache)(nil)
)
