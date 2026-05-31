// Package campaign implements the admin BFF's campaign management over Core's
// CampaignService. A campaign is a container aggregate under a merchant; its
// tenant_id + merchant_id come from the caller's scope (JWT/header seam), never
// the path — matching the PLAN's isolation rule.
package campaign

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/muse/bffkit/auth"
	"github.com/muse/bffkit/cache"
	"github.com/muse/bffkit/coreclient"
	"github.com/muse/bffkit/envelope"
	"github.com/muse/bffkit/middleware"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

// Handler holds the Core client and the shared read-model cache (nil = none),
// which it invalidates when a campaign's public config changes.
type Handler struct {
	core  *coreclient.Client
	cache *cache.Cache
}

// New builds the campaign handler. rmcache is the shared read-model cache used to
// bust the consumer BFF's public campaign config on update (may be nil).
func New(core *coreclient.Client, rmcache *cache.Cache) *Handler {
	return &Handler{core: core, cache: rmcache}
}

// Routes mounts the campaign admin endpoints.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/admin/campaigns", h.createCampaign)
	r.Get("/admin/campaigns", h.listCampaigns)
	r.Get("/admin/campaigns/{campaignId}", h.getCampaign)
	r.Put("/admin/campaigns/{campaignId}", h.updateCampaign)
	r.Post("/admin/campaigns/{campaignId}/duplicate", h.duplicateCampaign)
	r.Get("/admin/campaigns/{campaignId}/analytics", h.analytics)
}

type campaignBody struct {
	Name      string          `json:"name"`
	Status    string          `json:"status"`
	StartDate string          `json:"start_date"`
	EndDate   string          `json:"end_date"`
	Channels  []string        `json:"channels"`
	Games     []string        `json:"games"`
	Quests    []string        `json:"quests"`
	Settings  json.RawMessage `json:"settings"`
}

func (b campaignBody) toProto(id, tenant, merchant string) *gamev1.Campaign {
	return &gamev1.Campaign{
		Id:         id,
		TenantId:   tenant,
		MerchantId: merchant,
		Name:       b.Name,
		Status:     b.Status,
		StartDate:  parseDate(b.StartDate),
		EndDate:    parseDate(b.EndDate),
		Channels:   b.Channels,
		Games:      b.Games,
		Quests:     b.Quests,
		Settings:   string(orEmptyObj(b.Settings)),
	}
}

func (h *Handler) createCampaign(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	var body campaignBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		envelope.WriteError(w, tid, invalidArg("malformed request body"))
		return
	}
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Campaign.CreateCampaign(ctx, &gamev1.CreateCampaignRequest{
		Scope:    coreclient.Scope(tenant, merchant),
		Campaign: body.toProto("", tenant, merchant),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteCreated(w, tid, campaignView(resp.GetCampaign()))
}

func (h *Handler) listCampaigns(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Campaign.ListCampaigns(ctx, &gamev1.ListCampaignsRequest{
		Scope:  coreclient.Scope(tenant, merchant),
		Limit:  int32(parseLimit(r.URL.Query().Get("limit"))),
		Cursor: r.URL.Query().Get("cursor"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	items := make([]any, 0, len(resp.GetCampaigns()))
	for _, c := range resp.GetCampaigns() {
		items = append(items, campaignView(c))
	}
	envelope.WriteSuccess(w, tid, paged(items, resp.GetNextCursor()))
}

func (h *Handler) getCampaign(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Campaign.GetCampaign(ctx, &gamev1.GetCampaignRequest{
		Scope:      coreclient.Scope(tenant, merchant),
		CampaignId: chi.URLParam(r, "campaignId"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, campaignView(resp.GetCampaign()))
}

func (h *Handler) updateCampaign(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	var body campaignBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		envelope.WriteError(w, tid, invalidArg("malformed request body"))
		return
	}
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Campaign.UpdateCampaign(ctx, &gamev1.UpdateCampaignRequest{
		Scope:    coreclient.Scope(tenant, merchant),
		Campaign: body.toProto(chi.URLParam(r, "campaignId"), tenant, merchant),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	// Invalidate the consumer BFF's cached public config (shared Redis namespace).
	h.cache.Bust(ctx, cache.PublicCampaignKey(chi.URLParam(r, "campaignId")))
	envelope.WriteSuccess(w, tid, campaignView(resp.GetCampaign()))
}

func (h *Handler) duplicateCampaign(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	var body struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body) // name is optional (defaults to "… (copy)")
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Campaign.DuplicateCampaign(ctx, &gamev1.DuplicateCampaignRequest{
		Scope:      coreclient.Scope(tenant, merchant),
		CampaignId: chi.URLParam(r, "campaignId"),
		Name:       body.Name,
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteCreated(w, tid, campaignView(resp.GetCampaign()))
}

func (h *Handler) analytics(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Campaign.GetCampaignAnalytics(ctx, &gamev1.GetCampaignAnalyticsRequest{
		Scope:      coreclient.Scope(tenant, merchant),
		CampaignId: chi.URLParam(r, "campaignId"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, analyticsView(resp.GetAnalytics()))
}
