package obs

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func scrape(t *testing.T, m *Metrics) string {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	b, _ := io.ReadAll(rec.Result().Body)
	return string(b)
}

func TestMiddlewareRecordsREDWithRoutePattern(t *testing.T) {
	m := New()
	r := chi.NewRouter()
	r.Use(m.Middleware)
	r.Get("/games/{gameId}/play", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.Get("/boom", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })

	for _, path := range []string{"/games/abc/play", "/games/xyz/play", "/boom"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}

	out := scrape(t, m)
	// The route label must be the templated pattern (bounded cardinality), not the raw path.
	if !strings.Contains(out, `route="/games/{gameId}/play"`) {
		t.Fatalf("expected templated route label; got:\n%s", out)
	}
	if strings.Contains(out, `route="/games/abc/play"`) {
		t.Fatal("route label leaked the concrete path id (cardinality risk)")
	}
	if !strings.Contains(out, `code="500"`) {
		t.Fatalf("expected a 500 to be recorded; got:\n%s", out)
	}
}

func TestNilMetricsMiddlewarePassThrough(t *testing.T) {
	var m *Metrics // nil
	called := false
	h := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	if !called {
		t.Fatal("nil metrics should pass through to the next handler")
	}
	m.IncCache(true) // must not panic on nil
}

func TestIncCacheCounts(t *testing.T) {
	m := New()
	m.IncCache(true)
	m.IncCache(true)
	m.IncCache(false)
	out := scrape(t, m)
	if !strings.Contains(out, `bff_cache_ops_total{result="hit"} 2`) {
		t.Fatalf("expected 2 hits; got:\n%s", out)
	}
	if !strings.Contains(out, `bff_cache_ops_total{result="miss"} 1`) {
		t.Fatalf("expected 1 miss; got:\n%s", out)
	}
}
