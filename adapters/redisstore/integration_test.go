//go:build integration

// Integration test for the redisstore adapter against a REAL Redis container.
// Run with: go test -tags integration ./adapters/redisstore/...
package redisstore_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/muse/adapters/redisstore"
	"github.com/testcontainers/testcontainers-go"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

func TestMain(m *testing.M) {
	_ = os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
	os.Exit(m.Run())
}

func newCache(t *testing.T) *redisstore.Cache {
	t.Helper()
	ctx := context.Background()
	c, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Fatalf("start redis: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })

	uri, err := c.ConnectionString(ctx) // "redis://host:port"
	if err != nil {
		t.Fatalf("redis uri: %v", err)
	}
	addr := strings.TrimPrefix(uri, "redis://")

	cache, err := redisstore.Open(ctx, addr, "test")
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	t.Cleanup(func() { _ = cache.Close() })
	return cache
}

func TestRedisCache(t *testing.T) {
	cache := newCache(t)
	ctx := context.Background()

	t.Run("miss then set then get", func(t *testing.T) {
		if _, err := cache.Get(ctx, "k1"); err != redisstore.ErrCacheMiss {
			t.Fatalf("expected ErrCacheMiss, got %v", err)
		}
		if err := cache.Set(ctx, "k1", []byte("v1"), time.Minute); err != nil {
			t.Fatalf("Set: %v", err)
		}
		b, err := cache.Get(ctx, "k1")
		if err != nil || string(b) != "v1" {
			t.Fatalf("Get: %q err=%v", b, err)
		}
	})

	t.Run("setnx idempotency semantics", func(t *testing.T) {
		ok, err := cache.SetNX(ctx, "idem", []byte("first"), time.Minute)
		if err != nil || !ok {
			t.Fatalf("first SetNX should win: ok=%v err=%v", ok, err)
		}
		ok, err = cache.SetNX(ctx, "idem", []byte("second"), time.Minute)
		if err != nil || ok {
			t.Fatalf("second SetNX should lose: ok=%v err=%v", ok, err)
		}
		b, _ := cache.Get(ctx, "idem")
		if string(b) != "first" {
			t.Errorf("expected stored value 'first', got %q", b)
		}
	})

	t.Run("ttl expiry", func(t *testing.T) {
		if err := cache.Set(ctx, "ttl", []byte("x"), 50*time.Millisecond); err != nil {
			t.Fatalf("Set: %v", err)
		}
		time.Sleep(120 * time.Millisecond)
		if _, err := cache.Get(ctx, "ttl"); err != redisstore.ErrCacheMiss {
			t.Errorf("expected miss after TTL, got %v", err)
		}
	})
}
