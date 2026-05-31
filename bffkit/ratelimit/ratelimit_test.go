package ratelimit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeLimiter denies once the call count for a key exceeds limit, and can be set
// to error to exercise the fail-open path.
type fakeLimiter struct {
	counts map[string]int
	err    error
}

func (f *fakeLimiter) Allow(_ context.Context, key string, limit int, _ time.Duration) (bool, time.Duration, error) {
	if f.err != nil {
		return false, 0, f.err
	}
	f.counts[key]++
	if f.counts[key] > limit {
		return false, 3 * time.Second, nil
	}
	return true, 0, nil
}

func newReq() *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/games/g1/play", nil)
	r.RemoteAddr = "203.0.113.7:5555"
	return r
}

func okHandler() (http.Handler, *int) {
	n := 0
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n++
		w.WriteHeader(http.StatusOK)
	}), &n
}

func TestNilLimiterPassesThrough(t *testing.T) {
	h, calls := okHandler()
	mw := Middleware(nil, Config{Name: "play", Limit: 1, Window: time.Minute})
	for range 5 {
		rec := httptest.NewRecorder()
		mw(h).ServeHTTP(rec, newReq())
		if rec.Code != http.StatusOK {
			t.Fatalf("nil limiter should pass through, got %d", rec.Code)
		}
	}
	if *calls != 5 {
		t.Fatalf("handler should run every time, ran %d", *calls)
	}
}

func TestAllowThenDeny(t *testing.T) {
	lim := &fakeLimiter{counts: map[string]int{}}
	h, calls := okHandler()
	mw := Middleware(lim, Config{Name: "play", Limit: 2, Window: time.Minute})

	codes := []int{}
	for i := range 3 {
		rec := httptest.NewRecorder()
		mw(h).ServeHTTP(rec, newReq())
		codes = append(codes, rec.Code)
		if i == 2 { // third is over the limit
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("3rd request: want 429, got %d", rec.Code)
			}
			if ra := rec.Header().Get("Retry-After"); ra != "3" {
				t.Fatalf("want Retry-After 3, got %q", ra)
			}
		}
	}
	if codes[0] != 200 || codes[1] != 200 {
		t.Fatalf("first two should pass: %v", codes)
	}
	if *calls != 2 {
		t.Fatalf("handler should run twice (denied request blocked), ran %d", *calls)
	}
}

func TestLimiterErrorFailsOpen(t *testing.T) {
	lim := &fakeLimiter{counts: map[string]int{}, err: errors.New("redis down")}
	h, calls := okHandler()
	mw := Middleware(lim, Config{Name: "play", Limit: 1, Window: time.Minute})
	rec := httptest.NewRecorder()
	mw(h).ServeHTTP(rec, newReq())
	if rec.Code != http.StatusOK || *calls != 1 {
		t.Fatalf("limiter error should fail open: code=%d calls=%d", rec.Code, *calls)
	}
}

func TestClientIPFromXForwardedFor(t *testing.T) {
	r := newReq()
	r.Header.Set("X-Forwarded-For", "198.51.100.9, 10.0.0.1")
	if got := clientIP(r); got != "198.51.100.9" {
		t.Fatalf("want first XFF hop, got %q", got)
	}
}
