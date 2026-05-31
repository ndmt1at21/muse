package platform

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func scrape(t *testing.T, m *Metrics) string {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	b, _ := io.ReadAll(rec.Result().Body)
	return string(b)
}

func TestEventAndFulfillmentCounters(t *testing.T) {
	m := NewMetrics()
	m.IncEvent("play_completed")
	m.IncEvent("play_completed")
	m.IncEvent("prize_won")
	m.IncFulfillment("dead")

	out := scrape(t, m)
	if !strings.Contains(out, `game_events_total{type="play_completed"} 2`) {
		t.Fatalf("play_completed count wrong:\n%s", out)
	}
	if !strings.Contains(out, `game_events_total{type="prize_won"} 1`) {
		t.Fatalf("prize_won count wrong:\n%s", out)
	}
	if !strings.Contains(out, `fulfillment_tasks_total{outcome="dead"} 1`) {
		t.Fatalf("fulfillment dead count wrong:\n%s", out)
	}
}

func TestObserveGRPC(t *testing.T) {
	m := NewMetrics()
	m.ObserveGRPC("Play", "OK", 5*time.Millisecond)
	m.ObserveGRPC("Play", "Aborted", 2*time.Millisecond)

	out := scrape(t, m)
	if !strings.Contains(out, `grpc_server_requests_total{code="OK",method="Play"} 1`) {
		t.Fatalf("missing OK count:\n%s", out)
	}
	if !strings.Contains(out, `grpc_server_requests_total{code="Aborted",method="Play"} 1`) {
		t.Fatalf("missing Aborted count:\n%s", out)
	}
	if !strings.Contains(out, `grpc_server_request_duration_seconds_count{method="Play"} 2`) {
		t.Fatalf("duration histogram count wrong:\n%s", out)
	}
}
