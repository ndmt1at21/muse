package platform

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics is Core's Prometheus instrumentation: RED (rate/errors/duration) for
// the gRPC surface plus business counters fed from domain events and the
// fulfillment dispatcher. It owns a private registry so the /metrics endpoint
// exposes exactly these series (plus Go runtime + process collectors).
type Metrics struct {
	reg *prometheus.Registry

	grpcRequests *prometheus.CounterVec   // grpc_server_requests_total{method,code}
	grpcDuration *prometheus.HistogramVec // grpc_server_request_duration_seconds{method}

	events      *prometheus.CounterVec // game_events_total{type}
	fulfillment *prometheus.CounterVec // fulfillment_tasks_total{outcome}
}

// NewMetrics builds and registers the Core metrics.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	m := &Metrics{
		reg: reg,
		grpcRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "grpc_server_requests_total", Help: "Total gRPC requests by method and status code.",
		}, []string{"method", "code"}),
		grpcDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "grpc_server_request_duration_seconds", Help: "gRPC request latency by method.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method"}),
		events: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "game_events_total", Help: "Domain events emitted, by type (play_completed, prize_won, ...).",
		}, []string{"type"}),
		fulfillment: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "fulfillment_tasks_total", Help: "Fulfillment delivery attempts by outcome (delivered, awaiting, retry, dead).",
		}, []string{"outcome"}),
	}
	reg.MustRegister(m.grpcRequests, m.grpcDuration, m.events, m.fulfillment)
	return m
}

// Handler returns the Prometheus /metrics HTTP handler.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// ObserveGRPC records one gRPC call's status code and latency. code is the
// canonical status string (e.g. "OK", "NotFound"); kept as a string so platform
// stays free of a grpc import.
func (m *Metrics) ObserveGRPC(method, code string, dur time.Duration) {
	m.grpcRequests.WithLabelValues(method, code).Inc()
	m.grpcDuration.WithLabelValues(method).Observe(dur.Seconds())
}

// IncEvent counts a domain event by type.
func (m *Metrics) IncEvent(eventType string) { m.events.WithLabelValues(eventType).Inc() }

// IncFulfillment counts a fulfillment delivery outcome.
func (m *Metrics) IncFulfillment(outcome string) { m.fulfillment.WithLabelValues(outcome).Inc() }
