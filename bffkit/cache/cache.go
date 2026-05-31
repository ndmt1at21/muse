// Package cache is the BFF read-model cache: a cache-aside wrapper over a
// Redis-protocol store for assembled, UI-facing view models (public campaign
// config, leaderboard top-N, winners marquee). It caches the JSON `data` payload
// keyed (and namespaced) per tenant, with short TTLs and explicit invalidation
// on the relevant admin mutation. SQL/Core stay the source of truth; this only
// shaves read latency at the edge. A nil *Cache is a valid no-op (cache disabled
// when Redis is absent), so handlers need no branching.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/muse/adapters/redisstore"
)

// Store is the subset of redisstore.Cache the read-model cache needs. Defining
// it here keeps bffkit decoupled from a concrete Redis type and lets tests fake it.
type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, v []byte, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
}

// Cache is a cache-aside read-model cache with a default TTL.
type Cache struct {
	store    Store
	ttl      time.Duration
	observer func(hit bool) // optional metrics hook
}

// New builds a Cache. A nil store yields a no-op Cache (every Fetch is a miss
// that just runs the loader; Bust is a no-op).
func New(store Store, ttl time.Duration) *Cache {
	return &Cache{store: store, ttl: ttl}
}

// Observe attaches a hit/miss observer (e.g. a Prometheus counter). Safe to call
// on a nil cache; returns the receiver for chaining.
func (c *Cache) Observe(fn func(hit bool)) *Cache {
	if c != nil {
		c.observer = fn
	}
	return c
}

func (c *Cache) record(hit bool) {
	if c.observer != nil {
		c.observer(hit)
	}
}

// Fetch returns the cached `data` payload for key, or runs load, caches its
// JSON, and returns it. The returned bytes are the marshaled view model, ready
// to embed as the envelope `data`. On any cache error it transparently falls
// back to load (availability over cache freshness). load runs at most once.
func (c *Cache) Fetch(ctx context.Context, key string, load func() (any, error)) (json.RawMessage, error) {
	if c == nil || c.store == nil {
		return loadJSON(load)
	}
	if b, err := c.store.Get(ctx, key); err == nil {
		c.record(true)
		return json.RawMessage(b), nil
	} else if !errors.Is(err, redisstore.ErrCacheMiss) {
		return loadJSON(load) // backend hiccup → serve from origin (not counted)
	}
	c.record(false)
	b, err := loadJSON(load)
	if err != nil {
		return nil, err
	}
	_ = c.store.Set(ctx, key, b, c.ttl) // best-effort populate
	return b, nil
}

// Bust invalidates keys (call from the admin mutation that changed the source).
// Safe on a nil/no-op cache.
func (c *Cache) Bust(ctx context.Context, keys ...string) {
	if c == nil || c.store == nil || len(keys) == 0 {
		return
	}
	_ = c.store.Del(ctx, keys...)
}

func loadJSON(load func() (any, error)) (json.RawMessage, error) {
	v, err := load()
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// PublicCampaignKey is the shared cache key for a campaign's public widget
// config. Campaign ids are globally unique (the widget knows only the campaign
// id), so the key is campaign-scoped; both BFFs derive it the same way so the
// admin BFF can invalidate what the consumer BFF cached (shared Redis namespace).
func PublicCampaignKey(campaignID string) string {
	return "rm:public_campaign:" + campaignID
}

// LeaderboardRankingsKey is the cache key for a top-N rankings page. Rankings
// shift on every play, so this is paired with a short TTL (no explicit bust)
// rather than invalidation — the cache just sheds repeated reads of the hot
// "Đua Top" view between refreshes.
func LeaderboardRankingsKey(lbID string, limit, offset int) string {
	return "rm:lb_rankings:" + lbID + ":" + strconv.Itoa(limit) + ":" + strconv.Itoa(offset)
}
