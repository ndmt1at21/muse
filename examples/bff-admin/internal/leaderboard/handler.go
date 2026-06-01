// Package leaderboard implements the admin BFF's leaderboard management over
// Core's LeaderboardService: config CRUD plus the operational actions —
// finalize (lock + batch award), reset (new window), and the anti-cheat
// disqualify / score-adjust. Scope comes from the caller's JWT/header seam.
package leaderboard

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/muse/bffkit/auth"
	"github.com/muse/bffkit/coreclient"
	"github.com/muse/bffkit/envelope"
	"github.com/muse/bffkit/middleware"
	"github.com/muse/pkg/enumx"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

// Handler holds the Core client.
type Handler struct {
	core *coreclient.Client
}

// New builds the leaderboard admin handler.
func New(core *coreclient.Client) *Handler { return &Handler{core: core} }

// Routes mounts the leaderboard admin endpoints.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/admin/leaderboards", h.create)
	r.Get("/admin/leaderboards", h.list)
	r.Put("/admin/leaderboards/{lbId}", h.update)
	r.Post("/admin/leaderboards/{lbId}/finalize", h.finalize)
	r.Post("/admin/leaderboards/{lbId}/reset", h.reset)
	r.Post("/admin/leaderboards/{lbId}/entries/{playerId}/disqualify", h.disqualify)
	r.Post("/admin/leaderboards/{lbId}/entries/{playerId}/adjust", h.adjust)
}

type lbBody struct {
	CampaignID string          `json:"campaign_id"`
	Name       string          `json:"name"`
	Metric     string          `json:"metric"`
	TimeWindow json.RawMessage `json:"time_window"`
	PrizeTiers json.RawMessage `json:"prize_tiers"`
	AntiCheat  json.RawMessage `json:"anti_cheat"`
}

func (b lbBody) toProto(id string) *gamev1.Leaderboard {
	return &gamev1.Leaderboard{
		Id:         id,
		CampaignId: b.CampaignID,
		Name:       b.Name,
		Metric:     enumx.Parse[gamev1.LeaderboardMetric](b.Metric, gamev1.LeaderboardMetric_value),
		TimeWindow: orEmptyObj(b.TimeWindow),
		PrizeTiers: orEmptyArr(b.PrizeTiers),
		AntiCheat:  orEmptyObj(b.AntiCheat),
	}
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	var body lbBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		envelope.WriteError(w, tid, invalidArg("malformed request body"))
		return
	}
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Leaderboard.CreateLeaderboard(ctx, &gamev1.CreateLeaderboardRequest{
		TenantId: tenant, MerchantId: merchant, Leaderboard: body.toProto(""),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteCreated(w, tid, lbView(resp.GetLeaderboard()))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Leaderboard.ListLeaderboards(ctx, &gamev1.ListLeaderboardsRequest{
		TenantId:      tenant, MerchantId: merchant,
		CampaignId: r.URL.Query().Get("campaignId"),
		Limit:      int32(parseLimit(r.URL.Query().Get("limit"))),
		Cursor:     r.URL.Query().Get("cursor"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	items := make([]any, 0, len(resp.GetLeaderboards()))
	for _, lb := range resp.GetLeaderboards() {
		items = append(items, lbView(lb))
	}
	envelope.WriteSuccess(w, tid, paged(items, resp.GetNextCursor()))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	var body lbBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		envelope.WriteError(w, tid, invalidArg("malformed request body"))
		return
	}
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Leaderboard.UpdateLeaderboard(ctx, &gamev1.UpdateLeaderboardRequest{
		TenantId: tenant, MerchantId: merchant, Leaderboard: body.toProto(chi.URLParam(r, "lbId")),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, lbView(resp.GetLeaderboard()))
}

func (h *Handler) finalize(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Leaderboard.FinalizeLeaderboard(ctx, &gamev1.FinalizeLeaderboardRequest{
		TenantId: tenant, MerchantId: merchant, LeaderboardId: chi.URLParam(r, "lbId"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	awards := make([]any, 0, len(resp.GetAwards()))
	for _, a := range resp.GetAwards() {
		awards = append(awards, map[string]any{
			"rank": a.GetRank(), "player_id": a.GetPlayerId(), "prize_id": a.GetPrizeId(), "reward_id": a.GetRewardId(),
		})
	}
	envelope.WriteSuccess(w, tid, map[string]any{"awarded": resp.GetAwarded(), "awards": awards})
}

func (h *Handler) reset(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Leaderboard.ResetLeaderboard(ctx, &gamev1.ResetLeaderboardRequest{
		TenantId: tenant, MerchantId: merchant, LeaderboardId: chi.URLParam(r, "lbId"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, map[string]any{"cleared": resp.GetCleared()})
}

func (h *Handler) disqualify(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Leaderboard.DisqualifyEntry(ctx, &gamev1.DisqualifyEntryRequest{
		TenantId: tenant, MerchantId: merchant, LeaderboardId: chi.URLParam(r, "lbId"), PlayerId: chi.URLParam(r, "playerId"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, entryView(resp.GetEntry()))
}

func (h *Handler) adjust(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	var body struct {
		Delta int64 `json:"delta"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		envelope.WriteError(w, tid, invalidArg("delta is required"))
		return
	}
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Leaderboard.AdjustEntry(ctx, &gamev1.AdjustEntryRequest{
		TenantId: tenant, MerchantId: merchant, LeaderboardId: chi.URLParam(r, "lbId"),
		PlayerId: chi.URLParam(r, "playerId"), Delta: body.Delta,
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, entryView(resp.GetEntry()))
}
