package platform

import (
	"context"
	"net/http"
	"time"
)

// Checker is a named readiness probe (DB, Redis, ...).
type Checker func(ctx context.Context) error

// HealthServer serves /healthz (liveness), /readyz (readiness), and /metrics
// (Prometheus). metrics may be nil (the endpoint then 404s).
type HealthServer struct {
	checks  map[string]Checker
	metrics http.Handler
}

// NewHealthServer builds a health server with the given readiness checks.
func NewHealthServer(checks map[string]Checker) *HealthServer {
	return &HealthServer{checks: checks}
}

// WithMetrics attaches the Prometheus /metrics handler.
func (h *HealthServer) WithMetrics(handler http.Handler) *HealthServer {
	h.metrics = handler
	return h
}

// Handler returns the HTTP mux for health endpoints.
func (h *HealthServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		for name, check := range h.checks {
			if err := check(ctx); err != nil {
				w.Header().Set("X-Failed-Check", name)
				http.Error(w, name+": "+err.Error(), http.StatusServiceUnavailable)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	if h.metrics != nil {
		mux.Handle("/metrics", h.metrics)
	}
	return mux
}
