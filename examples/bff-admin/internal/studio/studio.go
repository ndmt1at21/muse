// Package studio serves the demo "Game Studio": a buildless config-and-play
// page embedded into the bff-admin binary. It is a dev/demo tool for tuning the
// example games from the browser — set prize values and odds, click Apply, and
// the page creates the game via the admin BFF (same origin) and plays it inline
// against the consumer BFF (which allows cross-origin via its widget CORS).
//
// It is intentionally on the admin BFF: creating/configuring games is an
// internal operation, so it lives behind the same surface as the rest of the
// management API. Gameplay (start/play) still goes to the public consumer BFF.
package studio

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
)

//go:embed web
var webFS embed.FS

// Routes mounts the studio at /studio (static files). Mount on the root router,
// outside the role-gated /api/v1 group — the page itself is public; the admin
// calls it makes carry the caller's admin JWT / X-Roles dev header.
func Routes(r chi.Router) {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic("studio: embedded web/ missing: " + err.Error())
	}
	fileServer := http.StripPrefix("/studio", http.FileServer(http.FS(sub)))

	r.Get("/studio", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/studio/", http.StatusFound)
	})
	r.Handle("/studio/*", fileServer)
}
