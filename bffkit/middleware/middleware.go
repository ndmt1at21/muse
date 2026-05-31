// Package middleware holds the shared BFF HTTP middleware: trace-id generation,
// panic recovery, request logging, and CORS. Both BFFs compose these so edge
// behavior stays consistent. (JWT auth + Redis rate limiting land in Phase 9.)
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"github.com/muse/bffkit/envelope"
)

type traceIDKey struct{}

// TraceIDFrom returns the request's trace id (set by TraceID middleware).
func TraceIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(traceIDKey{}).(string); ok {
		return v
	}
	return ""
}

// TraceID generates (or honors an inbound) trace id per request and exposes it
// via context + X-Trace-Id response header. In a full OTel setup this is the
// span's trace id; here it is a standalone correlation id.
func TraceID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tid := r.Header.Get("X-Trace-Id")
		if tid == "" {
			tid = newTraceID()
		}
		w.Header().Set("X-Trace-Id", tid)
		ctx := context.WithValue(r.Context(), traceIDKey{}, tid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func newTraceID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Recover converts a panic into a clean 500 envelope instead of dropping the
// connection.
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					tid := TraceIDFrom(r.Context())
					log.Error("panic", "recover", rec, "trace_id", tid, "path", r.URL.Path)
					envelope.WriteError(w, tid, statusInternal())
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Logger logs each request with method, path, status, duration, trace id.
func Logger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			log.Info("request",
				"method", r.Method, "path", r.URL.Path, "status", sw.status,
				"dur_ms", time.Since(start).Milliseconds(), "trace_id", TraceIDFrom(r.Context()))
		})
	}
}

// CORS allows the widget to embed cross-origin (consumer BFF). Admin BFF does
// not mount this. For the slice it is permissive; restrict origins in prod.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Widget-Token, X-Tenant-Id, X-Merchant-Id, X-Player-Id, Idempotency-Key, X-Trace-Id")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
