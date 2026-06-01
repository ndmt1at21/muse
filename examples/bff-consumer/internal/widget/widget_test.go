package widget

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// newServer mounts the widget on a fresh chi router, as main.go does.
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

func TestRootRedirectsToPlay(t *testing.T) {
	rec := get(t, newServer(), "/")
	if rec.Code != http.StatusFound {
		t.Fatalf("GET / status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/play/" {
		t.Fatalf("GET / Location = %q, want /play/", loc)
	}
}

func TestServesLobby(t *testing.T) {
	rec := get(t, newServer(), "/play/")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /play/ status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Muse Game Demos") {
		t.Fatalf("GET /play/ did not serve index.html (body missing lobby title)")
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("GET /play/ Content-Type = %q, want text/html", ct)
	}
}

// ES modules require a JavaScript MIME type; a wrong type silently breaks the
// widget in the browser, so pin it here.
func TestServesModuleScriptsWithJSMime(t *testing.T) {
	srv := newServer()
	for _, f := range []string{"client.js", "ui.js", "spin.js", "egg.js", "gift.js", "history.js"} {
		rec := get(t, srv, "/play/assets/"+f)
		if rec.Code != http.StatusOK {
			t.Errorf("GET /play/assets/%s status = %d, want 200", f, rec.Code)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
			t.Errorf("GET /play/assets/%s Content-Type = %q, want a javascript type", f, ct)
		}
	}
}

func TestServesGamePages(t *testing.T) {
	srv := newServer()
	for _, p := range []string{"spin.html", "egg.html", "gift.html", "history.html"} {
		rec := get(t, srv, "/play/"+p)
		if rec.Code != http.StatusOK {
			t.Errorf("GET /play/%s status = %d, want 200", p, rec.Code)
		}
	}
}

func TestUnknownAssetIs404(t *testing.T) {
	rec := get(t, newServer(), "/play/does-not-exist.js")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET missing asset status = %d, want 404", rec.Code)
	}
}
