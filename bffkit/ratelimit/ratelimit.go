// Package ratelimit provides a distributed per-player/per-IP rate-limit
// middleware for the BFF edge. The actual counting is delegated to a Limiter
// (the Redis fixed-window counter in adapters/redisstore), so limits hold across
// BFF replicas. A nil Limiter disables limiting (the middleware is a pass-through
// — useful for dev/e2e without Redis).
package ratelimit

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/muse/bffkit/auth"
	"github.com/muse/bffkit/envelope"
	"github.com/muse/bffkit/middleware"
	"github.com/muse/gamekit/gkerr"
	"github.com/muse/pkg/apierr"
)

// Limiter counts requests for a key within a window and reports whether the
// current request is under limit (and, if not, when the window resets).
type Limiter interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (allowed bool, retryAfter time.Duration, err error)
}

// Config is one rate-limit rule: at most Limit requests per Window, keyed under
// Name (so different routes count independently).
type Config struct {
	Name   string
	Limit  int
	Window time.Duration
}

// Middleware returns a chi-compatible middleware enforcing cfg. The key combines
// the rule name with the caller's player id (when authenticated) and client IP,
// so a single abusive player or IP is throttled without affecting others. A nil
// limiter (or a limiter error) fails open — availability over strictness at the
// edge; correctness-critical limits (stock) live in Core.
func Middleware(lim Limiter, cfg Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if lim == nil || cfg.Limit <= 0 {
				next.ServeHTTP(w, r)
				return
			}
			key := cfg.Name + ":" + caller(r)
			allowed, retryAfter, err := lim.Allow(r.Context(), key, cfg.Limit, cfg.Window)
			if err != nil {
				next.ServeHTTP(w, r) // fail open
				return
			}
			if !allowed {
				secs := max(int(retryAfter.Seconds()), 1)
				w.Header().Set("Retry-After", strconv.Itoa(secs))
				envelope.WriteError(w, middleware.TraceIDFrom(r.Context()),
					apierr.FromDomainError(gkerr.New(gkerr.ReasonRateLimited, "rate limit exceeded; retry later").
						WithMeta("retry_after_seconds", strconv.Itoa(secs))).Err())
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// caller identifies the subject of a limit: the player id when a JWT is present,
// else the client IP (so anonymous/widget traffic is still bounded).
func caller(r *http.Request) string {
	if pid := auth.PlayerID(r); pid != "" {
		return "p:" + pid
	}
	return "ip:" + clientIP(r)
}

// clientIP extracts the client IP, honoring a single X-Forwarded-For hop (the
// BFF sits behind one trusted proxy/ingress).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := indexComma(xff); i >= 0 {
			return trimSpace(xff[:i])
		}
		return trimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func indexComma(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			return i
		}
	}
	return -1
}

func trimSpace(s string) string {
	for len(s) > 0 && s[0] == ' ' {
		s = s[1:]
	}
	for len(s) > 0 && s[len(s)-1] == ' ' {
		s = s[:len(s)-1]
	}
	return s
}
