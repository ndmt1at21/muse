// Package widget serves the demo game widget — a buildless, dependency-free
// HTML/CSS/JS bundle embedded into the bff-consumer binary. It is the
// presentation layer the architecture keeps out of Core: the pages call the
// same public /api/v1 gameplay endpoints (start/play/eligibility) any embedder
// would, so the widget is a pure client of the BFF with no privileged access.
//
// Three pages cover the three built-in game shapes:
//
//	spin.html  → spin_wheel   (none + probability + basic)
//	egg.html   → egg_catcher  (none + score_to_tier + time_and_score_range)
//	gift.html  → gift_catcher (drop_sequence + collect_items + drop_plan)
//
// index.html is a lobby that captures the demo scope (tenant/merchant/player)
// and the three game ids — seed.sh prints a URL that prefills them.
package widget

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
)

//go:embed web
var webFS embed.FS

// Routes mounts the widget at /play (static files) and redirects the bare root
// to it, so opening the consumer BFF in a browser lands on the lobby. These are
// top-level routes — mount them on the root router, not the /api/v1 group.
func Routes(r chi.Router) {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic("widget: embedded web/ missing: " + err.Error())
	}
	fileServer := http.StripPrefix("/play", http.FileServer(http.FS(sub)))

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/play/", http.StatusFound)
	})
	r.Get("/play", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/play/", http.StatusFound)
	})
	r.Handle("/play/*", fileServer)
}
