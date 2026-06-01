package studio

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func newServer() http.Handler {
	r := chi.NewRouter()
	Routes(r)
	return r
}

func get(t *testing.T, srv http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestRedirectAndServe(t *testing.T) {
	srv := newServer()
	if rec := get(t, srv, "/studio"); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/studio/" {
		t.Fatalf("GET /studio = %d %q, want 302 -> /studio/", rec.Code, rec.Header().Get("Location"))
	}
	rec := get(t, srv, "/studio/")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Game Studio") {
		t.Fatalf("GET /studio/ = %d, want 200 serving index.html", rec.Code)
	}
}

func TestServesModuleWithJSMime(t *testing.T) {
	rec := get(t, newServer(), "/studio/assets/studio.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("studio.js status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("studio.js Content-Type = %q, want a javascript type", ct)
	}
}
