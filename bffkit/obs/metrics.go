// Package obs is the BFF observability kit: Prometheus RED metrics for the HTTP
// edge (rate/errors/duration per route) plus cache hit/miss counters, a
// middleware that records them, and the /metrics handler. Each BFF process owns
// one *Metrics; a nil *Metrics makes the middleware a pass-through.
package obs

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds the BFF's Prometheus instruments on a private registry.
type Metrics struct {
	reg *prometheus.Registry

	httpRequests *prometheus.CounterVec   // http_requests_total{route,method,code}
	httpDuration *prometheus.HistogramVec // http_request_duration_seconds{route,method}
	cacheOps     *prometheus.CounterVec   // bff_cache_ops_total{result}
}

// New builds and registers the BFF metrics.
func New() *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m := &Metrics{
		reg: reg,
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total", Help: "HTTP requests by route, method, and status code.",
		}, []string{"route", "method", "code"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "http_request_duration_seconds", Help: "HTTP request latency by route and method.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route", "method"}),
		cacheOps: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bff_cache_ops_total", Help: "Read-model cache lookups by result (hit|miss).",
		}, []string{"result"}),
	}
	reg.MustRegister(m.httpRequests, m.httpDuration, m.cacheOps)
	return m
}

// Handler returns the Prometheus /metrics HTTP handler.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// IncCache records a cache lookup result. Safe on a nil receiver.
func (m *Metrics) IncCache(hit bool) {
	if m == nil {
		return
	}
	result := "miss"
	if hit {
		result = "hit"
	}
	m.cacheOps.WithLabelValues(result).Inc()
}

// Middleware records RED metrics per request. The route label uses the matched
// chi pattern (e.g. "/api/v1/games/{gameId}/play") so cardinality stays bounded.
// A nil *Metrics is a pass-through.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	if m == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)

		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		m.httpRequests.WithLabelValues(route, r.Method, strconv.Itoa(sw.status)).Inc()
		m.httpDuration.WithLabelValues(route, r.Method).Observe(time.Since(start).Seconds())
	})
}

// statusWriter captures the response status code for the RED counter.
type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusWriter) WriteHeader(code int) {
	if !s.wroteHeader {
		s.status = code
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	s.wroteHeader = true
	return s.ResponseWriter.Write(b)
}
