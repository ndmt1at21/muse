// Package campaign implements the consumer BFF's public campaign config endpoint
// for the FE widget. It is unauthenticated and resolves a campaign by its
// globally-unique id alone (the widget knows only the campaign id); the response
// is a curated render config with no internal ids.
package campaign

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/muse/bffkit/cache"
	"github.com/muse/bffkit/coreclient"
	"github.com/muse/bffkit/envelope"
	"github.com/muse/bffkit/middleware"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

// Handler holds the Core client and the read-model cache (nil = no caching).
type Handler struct {
	core  *coreclient.Client
	cache *cache.Cache
}

// New builds the public campaign handler. rmcache may be nil (caching disabled).
func New(core *coreclient.Client, rmcache *cache.Cache) *Handler {
	return &Handler{core: core, cache: rmcache}
}

// Routes mounts the public campaign config endpoint.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/public/campaigns/{campaignId}", h.publicConfig)
}

// publicConfig serves the widget render config, cache-aside: the assembled view
// model is cached under a campaign-scoped key (busted by the admin BFF on
// UpdateCampaign), so the hot widget read avoids a Core round-trip.
func (h *Handler) publicConfig(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	ctx := coreclient.WithTrace(r.Context(), tid)
	campaignID := chi.URLParam(r, "campaignId")

	data, err := h.cache.Fetch(ctx, cache.PublicCampaignKey(campaignID), func() (any, error) {
		resp, gErr := h.core.Campaign.GetPublicConfig(ctx, &gamev1.GetPublicConfigRequest{CampaignId: campaignID})
		if gErr != nil {
			return nil, gErr
		}
		return publicView(resp.GetCampaign()), nil
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, data)
}
