package game

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/muse/bffkit/auth"
	"github.com/muse/bffkit/coreclient"
	"github.com/muse/bffkit/envelope"
	"github.com/muse/bffkit/middleware"
	gamev1 "github.com/muse/pkg/gen/game/v1"
)

func prizeView(p *gamev1.Prize) map[string]any {
	return map[string]any{
		"prize_id":          p.GetId(),
		"name":              p.GetName(),
		"type":              p.GetType(),
		"image":             p.GetImage(),
		"value":             p.GetValue(),
		"total":             p.GetTotal(),
		"remaining":         p.GetRemaining(),
		"award_constraints": rawJSON(p.GetAwardConstraints()),
		"fulfillment":       rawJSON(p.GetFulfillment()),
		"metadata":          rawJSON(p.GetMetadata()),
	}
}

func rewardView(r *gamev1.RewardRecord) map[string]any {
	return map[string]any{
		"reward_id": r.GetId(),
		"game_id":   r.GetGameId(),
		"player_id": r.GetPlayerId(),
		"prize_id":  r.GetPrizeId(),
		"name":      r.GetName(),
		"type":      r.GetType(),
		"value":     r.GetValue(),
		"code":      emptyToNil(r.GetCode()),
		"status":    r.GetStatus(),
	}
}

func emptyToNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (h *Handler) listPrizes(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Reward.ListPrizes(ctx, &gamev1.ListPrizesRequest{
		Scope: coreclient.Scope(tenant, merchant), GameId: r.URL.Query().Get("game_id"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	items := make([]any, 0, len(resp.GetPrizes()))
	for _, p := range resp.GetPrizes() {
		items = append(items, prizeView(p))
	}
	envelope.WriteSuccess(w, tid, map[string]any{"items": items})
}

func (h *Handler) getPrize(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Reward.GetPrize(ctx, &gamev1.GetPrizeRequest{
		Scope: coreclient.Scope(tenant, merchant), PrizeId: chi.URLParam(r, "prizeId"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, prizeView(resp.GetPrize()))
}

func (h *Handler) updatePrize(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	var body createPrizeBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		envelope.WriteError(w, tid, invalidArg("malformed request body"))
		return
	}
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Reward.UpdatePrize(ctx, &gamev1.UpdatePrizeRequest{
		Scope: coreclient.Scope(tenant, merchant),
		Prize: &gamev1.Prize{
			Id:               chi.URLParam(r, "prizeId"),
			Name:             body.Name,
			Type:             body.Type,
			Image:            body.Image,
			Value:            body.Value,
			AwardConstraints: string(orEmptyObj(body.Constraints)),
			Fulfillment:      string(orEmptyObj(body.Fulfillment)),
			Metadata:         string(orEmptyObj(body.Metadata)),
		},
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, prizeView(resp.GetPrize()))
}

func (h *Handler) deletePrize(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	if _, err := h.core.Reward.DeletePrize(ctx, &gamev1.DeletePrizeRequest{
		Scope: coreclient.Scope(tenant, merchant), PrizeId: chi.URLParam(r, "prizeId"),
	}); err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, map[string]any{"deleted": true})
}

func (h *Handler) importCodes(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	var body struct {
		Codes []string `json:"codes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		envelope.WriteError(w, tid, invalidArg("malformed request body"))
		return
	}
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Reward.ImportCodes(ctx, &gamev1.ImportCodesRequest{
		Scope: coreclient.Scope(tenant, merchant), PrizeId: chi.URLParam(r, "prizeId"), Codes: body.Codes,
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, map[string]any{"imported": resp.GetImported()})
}

func (h *Handler) prizeSummary(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Reward.GetPrizeSummary(ctx, &gamev1.GetPrizeSummaryRequest{
		Scope: coreclient.Scope(tenant, merchant), GameId: r.URL.Query().Get("game_id"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	items := make([]any, 0, len(resp.GetItems()))
	for _, it := range resp.GetItems() {
		items = append(items, map[string]any{
			"prize_id": it.GetPrizeId(), "name": it.GetName(), "total": it.GetTotal(),
			"remaining": it.GetRemaining(), "awarded": it.GetAwarded(), "codes_available": it.GetCodesAvailable(),
		})
	}
	envelope.WriteSuccess(w, tid, map[string]any{"items": items})
}

func (h *Handler) fulfillReward(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Reward.FulfillReward(ctx, &gamev1.FulfillRewardRequest{
		Scope: coreclient.Scope(tenant, merchant), RewardId: chi.URLParam(r, "rewardId"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, rewardView(resp.GetReward()))
}

func (h *Handler) revokeReward(w http.ResponseWriter, r *http.Request) {
	tid := middleware.TraceIDFrom(r.Context())
	tenant, merchant := auth.Scope(r)
	ctx := coreclient.WithTrace(r.Context(), tid)
	resp, err := h.core.Reward.RevokeReward(ctx, &gamev1.RevokeRewardRequest{
		Scope: coreclient.Scope(tenant, merchant), RewardId: chi.URLParam(r, "rewardId"),
	})
	if err != nil {
		envelope.WriteError(w, tid, err)
		return
	}
	envelope.WriteSuccess(w, tid, rewardView(resp.GetReward()))
}
