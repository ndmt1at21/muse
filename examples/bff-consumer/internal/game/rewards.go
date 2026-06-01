package game

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/muse/bffkit/auth"
	"github.com/muse/bffkit/coreclient"
	"github.com/muse/bffkit/envelope"
	"github.com/muse/bffkit/middleware"
	"github.com/muse/pkg/enumx"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

// RewardRoutes mounts the player-facing reward endpoints.
func (h *Handler) RewardRoutes(r chi.Router) {
	r.Get("/players/me/rewards", h.listMyRewards)
	r.Post("/players/me/rewards/{rewardId}/claim", h.claimReward)
}

func (h *Handler) listMyRewards(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Reward.ListRewards(ctx, &gamev1.ListRewardsRequest{
		TenantId: tenant, MerchantId: merchant, PlayerId: auth.PlayerID(r),
		Limit: int32(parseLimit(r)), Cursor: r.URL.Query().Get("cursor"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	items := make([]any, 0, len(resp.GetRewards()))
	for _, rec := range resp.GetRewards() {
		items = append(items, rewardItem(rec))
	}
	envelope.WriteSuccess(w, tid, map[string]any{
		"items": items,
		"pagination": map[string]any{
			"next_cursor": emptyToNil(resp.GetNextCursor()),
			"has_more":    resp.GetNextCursor() != "",
		},
	})
}

func (h *Handler) claimReward(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Reward.ClaimReward(ctx, &gamev1.ClaimRewardRequest{
		TenantId: tenant, MerchantId: merchant, RewardId: chi.URLParam(r, "rewardId"),
		PlayerId: auth.PlayerID(r),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, rewardItem(resp.GetReward()))
}

func rewardItem(rec *gamev1.RewardRecord) map[string]any {
	return map[string]any{
		"reward_id":  rec.GetId(),
		"prize_id":   rec.GetPrizeId(),
		"name":       rec.GetName(),
		"type":       rec.GetType(),
		"value":      rec.GetValue(),
		"code":       emptyToNil(rec.GetCode()),
		"status":     enumx.Name(rec.GetStatus()),
		"created_at": tsString(rec.GetCreatedAt()),
	}
}
