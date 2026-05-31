package cache

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/muse/adapters/redisstore"
)

// fakeStore is an in-memory Store for hermetic tests. getErr forces a backend
// error to exercise the fail-open path.
type fakeStore struct {
	mu     sync.Mutex
	data   map[string][]byte
	getErr error
	sets   int
}

func newFakeStore() *fakeStore { return &fakeStore{data: map[string][]byte{}} }

func (f *fakeStore) Get(_ context.Context, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	if v, ok := f.data[key]; ok {
		return v, nil
	}
	return nil, redisstore.ErrCacheMiss
}

func (f *fakeStore) Set(_ context.Context, key string, v []byte, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[key] = v
	f.sets++
	return nil
}

func (f *fakeStore) Del(_ context.Context, keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		delete(f.data, k)
	}
	return nil
}

func TestFetchMissThenHit(t *testing.T) {
	st := newFakeStore()
	c := New(st, time.Minute)
	ctx := context.Background()

	calls := 0
	load := func() (any, error) {
		calls++
		return map[string]any{"n": calls}, nil
	}

	// Miss → runs loader, caches.
	b1, err := c.Fetch(ctx, "k", load)
	if err != nil {
		t.Fatalf("fetch1: %v", err)
	}
	if string(b1) != `{"n":1}` {
		t.Fatalf("unexpected payload: %s", b1)
	}
	// Hit → loader not run again, same bytes.
	b2, err := c.Fetch(ctx, "k", load)
	if err != nil {
		t.Fatalf("fetch2: %v", err)
	}
	if string(b2) != `{"n":1}` {
		t.Fatalf("expected cached payload, got: %s", b2)
	}
	if calls != 1 {
		t.Fatalf("loader ran %d times, want 1 (second call should hit cache)", calls)
	}
	if st.sets != 1 {
		t.Fatalf("store set %d times, want 1", st.sets)
	}
}

func TestFetchBackendErrorFailsOpen(t *testing.T) {
	st := newFakeStore()
	st.getErr = errors.New("redis down")
	c := New(st, time.Minute)

	calls := 0
	b, err := c.Fetch(context.Background(), "k", func() (any, error) {
		calls++
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(b) != `{"ok":true}` || calls != 1 {
		t.Fatalf("expected origin load on backend error, got %s calls=%d", b, calls)
	}
}

func TestFetchLoaderError(t *testing.T) {
	c := New(newFakeStore(), time.Minute)
	want := errors.New("core failed")
	_, err := c.Fetch(context.Background(), "k", func() (any, error) { return nil, want })
	if !errors.Is(err, want) {
		t.Fatalf("expected loader error, got %v", err)
	}
}

func TestBustInvalidates(t *testing.T) {
	st := newFakeStore()
	c := New(st, time.Minute)
	ctx := context.Background()

	load := func() (any, error) { return map[string]any{"v": 1}, nil }
	_, _ = c.Fetch(ctx, "k", load)
	c.Bust(ctx, "k")
	if _, ok := st.data["k"]; ok {
		t.Fatal("expected key evicted after Bust")
	}
}

func TestNilCacheIsNoOp(t *testing.T) {
	var c *Cache // nil receiver must be safe (caching disabled)
	calls := 0
	b, err := c.Fetch(context.Background(), "k", func() (any, error) {
		calls++
		return map[string]any{"v": 1}, nil
	})
	if err != nil || string(b) != `{"v":1}` || calls != 1 {
		t.Fatalf("nil cache should run loader once: b=%s calls=%d err=%v", b, calls, err)
	}
	c.Bust(context.Background(), "k") // must not panic
}

func TestObserverSeesHitAndMiss(t *testing.T) {
	st := newFakeStore()
	var hits, misses int
	c := New(st, time.Minute).Observe(func(hit bool) {
		if hit {
			hits++
		} else {
			misses++
		}
	})
	ctx := context.Background()
	load := func() (any, error) { return map[string]any{"v": 1}, nil }

	_, _ = c.Fetch(ctx, "k", load) // miss → populate
	_, _ = c.Fetch(ctx, "k", load) // hit
	if hits != 1 || misses != 1 {
		t.Fatalf("observer hits=%d misses=%d, want 1/1", hits, misses)
	}
}

func TestKeysAreStable(t *testing.T) {
	if got := PublicCampaignKey("camp_1"); got != "rm:public_campaign:camp_1" {
		t.Fatalf("PublicCampaignKey: %s", got)
	}
	if got := LeaderboardRankingsKey("lb_1", 20, 0); got != "rm:lb_rankings:lb_1:20:0" {
		t.Fatalf("LeaderboardRankingsKey: %s", got)
	}
}
